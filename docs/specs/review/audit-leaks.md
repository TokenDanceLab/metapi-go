# Resource Leak Audit Report

**Date**: 2026-07-04
**Scope**: `scheduler/*.go`, `proxy/*.go`, `handler/proxy/*.go`
**Checks**: unclosed HTTP response bodies, unclosed file handles, goroutine leaks, timer leaks (time.After in loops), context leaks (WithCancel without defer cancel)

---

## Findings Summary

| # | Severity | Category | File | Issue |
|---|----------|----------|------|-------|
| 1 | **CRITICAL** | Goroutine leak | `proxy/session.go:406-420` | `touchLease` spawns new expiry goroutine on every `Touch()` call without cleaning up old ones |
| 2 | **HIGH** | Goroutine leak | `handler/proxy/upstream.go:85,99` | `dispatchUpstream` uses `context.Background()` + `http.DefaultClient` (no timeout); upstream stall = goroutine hang |
| 3 | **HIGH** | Goroutine leak | `handler/proxy/upstream.go:182-196` | SSE stream `resp.Body.Read` loop has no deadline; upstream mid-stream stall hangs goroutine forever |
| 4 | **MEDIUM** | HTTP client | `handler/proxy/upstream.go:99` | Bypasses `RuntimeExecutor` (which has proper timeout), uses bare `http.DefaultClient` with zero timeout |
| 5 | **LOW** | Data race | `scheduler/site_announcement.go:78-82` | `inFlight` flag read/written without mutex; other schedulers protect equivalent fields with `mu.Lock()` |
| 6 | **LOW** | Data race | `scheduler/update_center.go:78-82` | Same `inFlight` race as site_announcement |

---

## Detailed Findings

### Finding 1 (CRITICAL): Goroutine leak in `touchLease`

**File**: `proxy/session.go`, lines 399-421

**Mechanism**:

`createTrackedLease` (line 350) starts one expiry goroutine:

```go
// line 368-377
go func() {
    timer := time.NewTimer(time.Duration(ttlMs) * time.Millisecond)
    defer timer.Stop()
    select {
    case <-timer.C:        // fires -> Release
        lease.Release()
    case <-lease.doneCh:   // closed on Release -> return
        return
    }
}()
```

`touchLease` (line 406) spawns a **new** goroutine on every call:

```go
// line 406-420
go func() {
    timer := time.NewTimer(time.Duration(ttlMs) * time.Millisecond)
    defer timer.Stop()
    for {
        select {
        case <-timer.C:
            lease.Release()
            return
        case <-lease.expiryCh:
            timer.Reset(time.Duration(ttlMs) * time.Millisecond)
        case <-lease.doneCh:
            return
        }
    }
}()
```

The keepalive goroutine (line 382-393) calls `lease.Touch()` → `touchLease` on every keepalive tick. Each tick spawns a new expiry goroutine. The **initial** expiry goroutine (from `createTrackedLease`) does NOT listen on `expiryCh` -- it waits for its timer to fire or `doneCh` to close. The `expiryCh` signal (buffer=1) may be consumed by one of the spawned goroutines or dropped via `default:`, but regardless, a new goroutine is spawned.

**Impact**: For a leased session lasting 60 seconds with 1s keepalive, ~60 goroutines accumulate per lease. Each holds a `time.Timer`. With multiple concurrent sessions, goroutine count grows linearly. This violates the intended design where a single expiry goroutine should be resettable via `expiryCh`.

**Fix**: Replace the per-touch goroutine spawn with a single long-lived expiry goroutine that is started once in `createTrackedLease` and listens on `expiryCh` for resets. The keepalive goroutine should signal the same expiry goroutine via `expiryCh` rather than spawning a new one.

---

### Finding 2 (HIGH): No upstream request timeout in `dispatchUpstream`

**File**: `handler/proxy/upstream.go`, lines 56, 85, 99

**Mechanism**:

```go
// line 56
selected, err := proxy.SelectProxyChannelForAttempt(
    context.Background(),  // <-- no cancellation
    ...
)

// line 85
req, err := http.NewRequestWithContext(
    context.Background(),  // <-- no timeout, no client-cancel propagation
    r.Method, upstreamURL, bytesReader(forwardBytes),
)

// line 99
resp, err := http.DefaultClient.Do(req)  // http.DefaultClient has Timeout: 0
```

`http.DefaultClient` has `Timeout: 0` (no timeout). Combined with `context.Background()`, this means: if the upstream server accepts the TCP connection but never sends a response header, the handler goroutine blocks on `client.Do(req)` **indefinitely**. The downstream client's `r.Context()` is ignored, so even if the client disconnects, the goroutine lives on.

**Impact**: A single unresponsive upstream channel can consume a handler goroutine permanently. Under load, this leads to goroutine exhaustion and eventual OOM / unresponsiveness.

**Fix**: Use `r.Context()` instead of `context.Background()` to propagate client cancellation. Additionally, use a `RuntimeExecutor` (which has a configured timeout) or wrap with `context.WithTimeout`.

---

### Finding 3 (HIGH): SSE stream read loop has no deadline

**File**: `handler/proxy/upstream.go`, lines 182-196

**Mechanism**:

```go
func handleStreamUpstream(w http.ResponseWriter, resp *http.Response, latencyMs int64) {
    // ...
    for {
        n, err := resp.Body.Read(buf)   // <-- blocks indefinitely
        if n > 0 { ... }
        if err != nil {
            break
        }
    }
}
```

The `resp.Body.Read` loop has no timeout, no context, and no deadline. If the upstream starts sending an SSE stream but stalls mid-stream (e.g., TCP connection stays open but no data arrives), this goroutine blocks forever on `resp.Body.Read`.

**Impact**: Same as Finding 2 -- goroutine leak when upstream stalls mid-stream. Combined with Finding 2, two different pathways can leak goroutines per request.

**Fix**: Use `context.WithTimeout` or `http.NewRequestWithContext(r.Context(), ...)` so the `http.Response.Body` is backed by a context-aware reader. Alternatively, wrap reads with `SetReadDeadline` on the underlying connection.

---

### Finding 4 (MEDIUM): `http.DefaultClient` bypasses `RuntimeExecutor`

**File**: `handler/proxy/upstream.go`, line 99

**Mechanism**:

The codebase has a properly configured `RuntimeExecutor` (in `proxy/executor.go`) with a configurable timeout:

```go
// proxy/executor.go
func NewRuntimeExecutor(requestTimeout time.Duration) *RuntimeExecutor {
    return &RuntimeExecutor{
        client: &http.Client{Timeout: requestTimeout},
    }
}
```

However, `dispatchUpstream` does not use `RuntimeExecutor`. It creates requests manually and uses `http.DefaultClient.Do(req)` which has `Timeout: 0`. The `RuntimeExecutor` is passed in via `UpstreamConfig` (line 24 of upstream.go) but is **never referenced** in `dispatchUpstream`.

**Impact**: Even if `RuntimeExecutor` is configured with a proper timeout, all upstream proxy requests bypass it entirely. The `cfg.Executor` field in `UpstreamConfig` is dead code for the dispatch path.

**Fix**: Use `cfg.Executor.Dispatch(ctx, input)` or `cfg.Executor.WithObservedFirstByte(ctx, input, timeoutMs)` instead of manual `http.DefaultClient.Do(req)`.

---

### Finding 5 (LOW): `inFlight` race in `SiteAnnouncementScheduler`

**File**: `scheduler/site_announcement.go`, lines 77-82

**Mechanism**:

```go
func (s *SiteAnnouncementScheduler) runSync() {
    if s.inFlight {       // <-- read without mutex
        return
    }
    s.inFlight = true     // <-- write without mutex
    defer func() { s.inFlight = false }()
```

Compare with other schedulers that properly guard the equivalent field:

```go
// channel_recovery.go (correct)
func (s *ChannelRecoveryScheduler) runSweep() {
    s.mu.Lock()
    if s.sweepInFlight {
        s.mu.Unlock()
        return
    }
    s.sweepInFlight = true
    s.mu.Unlock()
```

**Impact**: Under concurrent ticker firings (possible if `runSync` takes longer than the tick interval), two passes may run concurrently. The impact is low because the pass is a read-only stub, but the pattern is inconsistent.

**Fix**: Wrap `inFlight` access with `s.mu.Lock()` / `s.mu.Unlock()`, consistent with `ChannelRecoveryScheduler`, `UsageAggregationScheduler`, `AdminSnapshotScheduler`, and `Sub2APIRefreshScheduler`.

---

### Finding 6 (LOW): `inFlight` race in `UpdateCenterScheduler`

**File**: `scheduler/update_center.go`, lines 77-82

Identical pattern to Finding 5. Same fix applies.

---

## Verified Non-Issues

The following were checked and found correct:

| Pattern | Files | Status |
|---------|-------|--------|
| `defer resp.Body.Close()` after `client.Do` | `backup_webdav.go:160`, `executor.go:73,138`, all `service/` files | CORRECT |
| `defer rows.Close()` after `dbw.Query` | `channel_recovery.go:173,209`, `checkin.go:193`, `model_probe.go:128`, `usage_aggregation.go:334`, `site_announcement.go:100`, `sub2api_refresh.go:129` | CORRECT |
| `defer tx.Rollback()` after `tx.Begin()` | `usage_aggregation.go:397` | CORRECT |
| `context.WithTimeout` + `defer cancel()` | `executor.go:109-110`, `oauth_loopback.go:79-81` | CORRECT |
| `r.Body.Close()` after `io.ReadAll(r.Body)` | `surface.go:67` | CORRECT |
| `time.After` in loops | (none found) | CLEAN |
| `context.WithCancel` without defer cancel | (none found except tests) | CLEAN |
| File handle leaks (`os.Open`/`os.Create`) | (none in scope) | CLEAN |
| `multipart.go:104` file close | Inside goroutine, `file.Close()` called | CORRECT |
| `backup_webdav.go` defer placement | `defer resp.Body.Close()` after nil-error guard | CORRECT |
| Ticker.Stop in all scheduler Stop() methods | All ticker-based schedulers call `s.ticker.Stop()` | CORRECT |
| `cronRunner.stop()` → `cron.Stop()` returns context for graceful shutdown | `cron.go:67-69` | CORRECT |

---

## Severity Summary

| Severity | Count | Goroutine Leaks | Data Races | Other |
|----------|-------|-----------------|------------|-------|
| CRITICAL | 1 | 1 | 0 | 0 |
| HIGH | 2 | 2 | 0 | 0 |
| MEDIUM | 1 | 0 | 0 | 1 (wrong client) |
| LOW | 2 | 0 | 2 | 0 |

**Conclusion**: Three goroutine leak vectors exist. The most impactful (Finding 1) is a design-level issue in the lease/touch mechanism. Findings 2-3 are path-level issues in the upstream dispatch that can be fixed by using `r.Context()` and `RuntimeExecutor`. The two LOW findings are cosmetic consistency issues.
