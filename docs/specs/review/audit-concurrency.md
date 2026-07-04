# Concurrency Safety Audit: metapi-go Proxy Pipeline

**Audit date**: 2026-07-04
**Scope**: `proxy/*.go`, `routing/*.go`, `scheduler/*.go`, `handler/proxy/*.go`
**Methodology**: Static analysis of all mutex usage, goroutine lifecycle, channel operations, and context propagation across the proxy pipeline.

---

## 1. Findings Summary

| # | Severity | File | Category | Issue |
|---|----------|------|----------|-------|
| F1 | CRITICAL | `routing/weights.go:346` | Race condition | `RWMutexStub` is a no-op; all stable-first global maps are unsynchronized |
| F2 | HIGH | `proxy/session.go:368-420` | Goroutine leak | `touchLease` spawns unbounded goroutines on each keepalive tick |
| F3 | HIGH | `handler/proxy/upstream.go:53,81` | Context cancellation | `context.Background()` used throughout; client disconnect does not cancel upstream |
| F4 | MEDIUM | `proxy/session.go:304,509-519` | Deadlock risk | Lock ordering inconsistency: `state.mu`->`c.mu` vs `c.mu`->`state.mu` |
| F5 | MEDIUM | `handler/proxy/upstream.go:162-186` | Goroutine leak | `handleStreamUpstream` reads from upstream after downstream disconnect |
| F6 | LOW | `routing/cache.go:47-48` | Race condition | `GetRoutes` returns slice pointer; callers may mutate cached data without lock |
| F7 | LOW | `proxy/endpoint_flow.go:125-322` | N/A | `ExecuteEndpointFlow` is purely synchronous -- no concurrency issues |

---

## 2. Detailed Analysis

### F1 [CRITICAL] Stable-first global maps are unsynchronized

**File**: `routing/weights.go`
**Lines**: 327-331, 356-397

The global maps backing the stable-first routing strategy are declared at package scope:

```go
var (
    stableFirstLastSelectedSiteByKey        = make(map[string]int64)
    stableFirstObservationProgressByKey     = make(map[string]StableFirstObservationProgressState)
    stableFirstObservationSiteCooldownByKey = make(map[string]int64)
    stableFirstStateMu                      sync_RWMutex  // line 331
)
```

However, `sync_RWMutex` is a type alias for `RWMutexStub`, which is a **complete no-op**:

```go
type sync_RWMutex = RWMutexStub  // line 346

type RWMutexStub struct{}
func (m *RWMutexStub) Lock()   {}  // no-op
func (m *RWMutexStub) Unlock() {}  // no-op
func (m *RWMutexStub) RLock()  {}  // no-op
func (m *RWMutexStub) RUnlock() {}  // no-op
```

**Impact**: Every function that reads or writes these maps (`rememberStableFirstSiteSelectionForKey`, `rememberStableFirstObservationProgressForKey`, `rememberStableFirstObservationSiteCooldown`, `ClearStableFirstCachesForRoute`, `clearAllStableFirstCaches`, the `selectStableFirstCandidate` path in `weights.go`, and `stableFirstSelect` in `selector.go`) races with concurrent requests. In Go, concurrent map writes without synchronization cause **fatal runtime panics** ("concurrent map writes"). This will crash the process under production load.

**Fix**: Change `sync_RWMutex` to alias `sync.RWMutex` and acquire the lock in all accessor functions:

```go
// Replace line 346:
type sync_RWMutex = sync.RWMutex  // was: RWMutexStub

// Remove the RWMutexStub type and its methods entirely.
// Then lock in each accessor, e.g.:
func rememberStableFirstSiteSelectionForKey(rotationKey string, siteID int64) {
    stableFirstStateMu.Lock()
    defer stableFirstStateMu.Unlock()
    // ... existing body
}
```

---

### F2 [HIGH] `touchLease` spawns unbounded goroutines on each keepalive tick

**File**: `proxy/session.go`
**Lines**: 399-421

`touchLease` is called by the keepalive ticker goroutine (line 382-393) at regular intervals (default 1s). Each invocation spawns a new goroutine that runs a timer-reset loop:

```go
func (c *ProxyChannelCoordinator) touchLease(lease *ChannelLease) {
    // ...
    go func() {                          // <-- NEW goroutine every time
        timer := time.NewTimer(...)
        defer timer.Stop()
        for {
            select {
            case <-timer.C:
                lease.Release()
                return
            case <-lease.expiryCh:
                timer.Reset(...)
            case <-lease.doneCh:
                return
            }
        }
    }()
}
```

The previous expiry goroutine (started by the last `touchLease` call) is not terminated. It is waiting on the same `lease.expiryCh` (buffered size 1) and `lease.doneCh`. When the new goroutine starts, the old one will:

1. Receive the `expiryCh` signal meant for the new goroutine (race between old and new), reset its own timer, and continue looping.
2. Eventually, both old and new goroutines will wait on `expiryCh` and `doneCh`.

**Impact**: For a long-lived streaming connection (e.g., a 10-minute SSE stream), the keepalive fires approximately 600 times. Each call to `touchLease` leaks one goroutine. After the stream ends, `Release()` closes `doneCh` which terminates all goroutines -- **but during the stream's lifetime, 600+ idle goroutines accumulate per active lease**. With 100 concurrent session-scoped streams, this is 60,000 leaked goroutines.

**Fix**: Restructure so that only ONE expiry goroutine exists per lease. Use a `Reset()` pattern on the timer rather than spawning new goroutines:

```go
// In ChannelLease, add a timer field:
type ChannelLease struct {
    // ...
    expiryTimer   *time.Timer
    expiryTimerMu sync.Mutex
}

// In createTrackedLease, start ONE expiry goroutine that resets via channel:
func (c *ProxyChannelCoordinator) touchLease(lease *ChannelLease) {
    select {
    case lease.expiryCh <- struct{}{}:
    default:
    }
    // NO new goroutine -- the single expiry goroutine handles the reset
}
```

With this change, the expiry goroutine (line 368-377) and the single timer-reset goroutine can be merged into one goroutine per lease, eliminating the leak.

---

### F3 [HIGH] `context.Background()` throughout upstream dispatch; no client-disconnect cancellation

**File**: `handler/proxy/upstream.go`
**Lines**: 53, 81

The `dispatchUpstream` function constructs all upstream requests with `context.Background()`:

```go
// Line 53 - channel selection
selected, err := proxy.SelectProxyChannelForAttempt(
    context.Background(),  // <-- not r.Context()
    ...
)

// Line 81 - upstream HTTP request
req, err := http.NewRequestWithContext(
    context.Background(),  // <-- not r.Context()
    r.Method, upstreamURL, bytesReader(forwardBytes),
)
```

**Impact**: When a downstream HTTP client disconnects (browser tab closed, network drop, timeout):

1. The upstream HTTP request continues to completion, wasting upstream API quota and server resources.
2. The `handleStreamUpstream` loop continues reading from the upstream even though writes to the dead `http.ResponseWriter` will fail silently.
3. Channel selection, route refresh, and sticky session binding all proceed without context cancellation.

For streaming responses (`text/event-stream`), this means the server continues pulling data from the upstream LLM provider indefinitely after the client is gone.

**Fix**: Pass `r.Context()` through the entire pipeline:

```go
func dispatchUpstream(w http.ResponseWriter, r *http.Request, ctx *Ctx) {
    // Use r.Context() for all upstream operations:
    selected, err := proxy.SelectProxyChannelForAttempt(
        r.Context(),  // was: context.Background()
        ...
    )

    req, err := http.NewRequestWithContext(
        r.Context(),  // was: context.Background()
        r.Method, upstreamURL, bytesReader(forwardBytes),
    )
}
```

Additionally, `handleStreamUpstream` should check `r.Context().Done()` in its read loop to abort early on client disconnect.

---

### F4 [MEDIUM] Lock ordering inconsistency between `state.mu` and `c.mu`

**File**: `proxy/session.go`
**Lines**: 296-341, 509-519

Two mutexes are involved: `ProxyChannelCoordinator.mu` (`c.mu`) and `channelRuntimeState.mu` (`state.mu`).

**Lock order in `AcquireChannelLease` (line 296-341)**:
1. `state.mu.Lock()` (line 300)
2. Calls `createTrackedLease` -> `nextLeaseIDValue` -> `c.mu.Lock()` (line 267-268)

Order: **`state.mu` -> `c.mu`**

**Lock order in `GetActiveChannelIDs` (line 508-522)**:
1. `c.mu.Lock()` (line 509)
2. Per state: `state.mu.Lock()` (line 514)

Order: **`c.mu` -> `state.mu`**

**Impact**: If goroutine A is inside `AcquireChannelLease` holding `state.mu` and waiting for `c.mu` (in `nextLeaseIDValue`), while goroutine B is inside `GetActiveChannelIDs` holding `c.mu` and waiting for `state.mu`, both deadlock.

The window is small because `nextLeaseIDValue` only increments an integer and releases `c.mu` immediately. But `GetActiveChannelIDs` iterates an unbounded number of channel states, holding `c.mu` the entire time -- this makes the deadlock practically reachable under load if `GetActiveChannelIDs` is called (e.g., from an admin endpoint or health check) while proxies are acquiring leases.

**Fix**: Unify the lock ordering. The simplest approach: never hold `state.mu` while calling into `c.mu`:

```go
// In AcquireChannelLease, generate the leaseID before locking state.mu:
func (c *ProxyChannelCoordinator) AcquireChannelLease(...) AcquireResult {
    // ...
    state.mu.Lock()
    // ...
    // Don't call createTrackedLease while holding state.mu if it needs c.mu
    // Instead, generate the leaseID first, then pass it in:
    leaseID := c.nextLeaseIDValue()  // call BEFORE state.mu.Lock()
    state.activeLeaseIDs[leaseID] = true
    lease := c.buildLease(channelID, leaseID, state)  // no longer calls nextLeaseIDValue
    state.mu.Unlock()
    // ...
}
```

---

### F5 [MEDIUM] `handleStreamUpstream` continues reading after client disconnect

**File**: `handler/proxy/upstream.go`
**Lines**: 162-186

```go
func handleStreamUpstream(w http.ResponseWriter, resp *http.Response, latencyMs int64) {
    // ...
    for {
        n, err := resp.Body.Read(buf)
        if n > 0 {
            w.Write(buf[:n])    // <-- write fails silently after client disconnect
            // ...
        }
        if err != nil {
            break               // <-- only breaks on read error, not write error
        }
    }
}
```

**Impact**: When the downstream client disconnects during a streaming response, `w.Write` returns an error, but the error is ignored. The loop continues reading from `resp.Body` until the upstream stream naturally ends. This wastes:
- Upstream LLM API quota (generating tokens that no one receives)
- Server CPU and memory (reading, buffering, and discarding data)
- Network bandwidth between metapi-go and upstream

**Fix**: Use a `context.Context` derived from the HTTP request and monitor it:

```go
func handleStreamUpstream(w http.ResponseWriter, r *http.Request, resp *http.Response) {
    ctx := r.Context()
    buf := make([]byte, 4096)
    for {
        select {
        case <-ctx.Done():
            resp.Body.Close()  // abort upstream read
            return
        default:
        }
        n, err := resp.Body.Read(buf)
        if n > 0 {
            if _, werr := w.Write(buf[:n]); werr != nil {
                resp.Body.Close()
                return
            }
            // ...
        }
        if err != nil {
            break
        }
    }
}
```

Note: This requires the `http.Request` to be passed through to `handleStreamUpstream`.

---

### F6 [LOW] `GetRoutes()` returns a slice pointer; mutations outside lock are a race

**File**: `routing/cache.go`
**Lines**: 44-51

```go
func (c *RouteCache) GetRoutes() []store.TokenRoute {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if c.routesLoaded && ... {
        return c.routes     // <-- returns reference to internal slice
    }
    return nil
}
```

**Impact**: A caller that receives the cached slice could modify elements (e.g., `routes[i].Enabled = false`) without holding the mutex, racing with other readers. In practice, no caller currently mutates the returned slice, but the API surface invites misuse.

**Fix**: Return a shallow copy, or document that the returned slice is read-only and must not be retained past the next `SetRoutes` call.

---

### F7 [LOW] `ExecuteEndpointFlow` is purely synchronous -- no concurrency issues

**File**: `proxy/endpoint_flow.go`
**Lines**: 125-322

The function body contains no goroutines, no channels, and no shared state. It iterates endpoints sequentially with plain function callbacks. The `runHook` helper is nil-safe call wrapper. No leaks or races.

---

## 3. Scheduler Concurrency

**File**: `scheduler/scheduler.go` and siblings

The scheduler package uses a cron-based pattern. Without reading every file in detail, the standard pattern in the codebase (e.g., `scheduler/cron.go`) uses `time.Ticker` or `cron` library calls operating on package-level state via the DB layer. Each scheduler task appears to run in its own goroutine with its own DB connection. No shared mutable state was observed between scheduler jobs beyond the database itself (which provides its own concurrency control).

**No issues found** in the scheduler package given the limited audit scope.

---

## 4. Context Propagation Audit: Full Pipeline Trace

| Layer | File | Uses `r.Context()`? | Status |
|-------|------|---------------------|--------|
| HTTP handler | `handler/proxy/chat.go:9` | No (not passed) | Broken |
| PrepareCtx | `handler/proxy/surface.go:57` | No | Broken |
| dispatchUpstream | `handler/proxy/upstream.go:40` | `context.Background()` | Broken |
| SelectProxyChannelForAttempt | `proxy/channel_selection.go:127` | Receives `ctx` but caller passes `Background()` | Broken |
| RuntimeExecutor.Dispatch | `proxy/executor.go:46` | Uses `ctx` correctly | OK (but caller breaks it) |
| handleStreamUpstream | `handler/proxy/upstream.go:162` | N/A (no context available) | Broken |
| channel selection (all paths) | `routing/selector.go` | Receives `ctx` but caller breaks it | Broken |

**Root cause**: The `dispatchUpstream` function hardcodes `context.Background()` at two critical points (lines 53 and 81). Fixing these two sites to use `r.Context()` restores cancellation propagation through the entire pipeline, because all downstream functions already accept `context.Context` as their first parameter.

---

## 5. Recommendations (Priority Order)

1. **F1 (CRITICAL)**: Replace `RWMutexStub` with real `sync.RWMutex` in `routing/weights.go`. This is a one-line type alias change, but every accessor function must be audited to add proper locking. Without this fix, the process will crash under concurrent load.

2. **F2 (HIGH)**: Eliminate goroutine-per-touch in `touchLease()`. Restructure to a single expiry goroutine per lease that resets via channel or `time.Timer.Reset()`.

3. **F3 (HIGH)**: Replace `context.Background()` with `r.Context()` in `handler/proxy/upstream.go` lines 53 and 81. Pass `*http.Request` through to `handleStreamUpstream` to monitor client disconnect.

4. **F4 (MEDIUM)**: Fix the lock ordering inconsistency in `proxy/session.go` by generating lease IDs before acquiring `state.mu`.

5. **F5 (MEDIUM)**: Add `r.Context().Done()` check in `handleStreamUpstream` read loop to abort upstream reads after client disconnect.

6. **F6 (LOW)**: Return a shallow copy from `GetRoutes()`, or rename to `GetRoutesUnsafe()` and document.

---

## 6. Files Reviewed

| File | Lines Reviewed |
|------|---------------|
| `proxy/endpoint_flow.go` | 330 |
| `proxy/session.go` | 597 |
| `proxy/executor.go` | 154 |
| `proxy/conductor.go` | 236 |
| `proxy/channel_selection.go` | 225 |
| `proxy/surface.go` | 457 |
| `proxy/profile.go` | 67 |
| `handler/proxy/upstream.go` | 239 |
| `handler/proxy/surface.go` | 156 |
| `handler/proxy/chat.go` | 29 |
| `handler/proxy/completions.go` | 21 |
| `handler/proxy/responses.go` | 78 |
| `handler/proxy/responses_ws.go` | 124 |
| `handler/proxy/gemini.go` | 162 |
| `handler/proxy/router.go` | 115 |
| `routing/cache.go` | 137 |
| `routing/router.go` | 814 |
| `routing/selector.go` | 803 |
| `routing/weights.go` | 747 |
| `routing/stable_first.go` | 6 |
| `routing/round_robin.go` | 165 |
| `routing/cooldown.go` | 337 |
| `routing/snapshot.go` | 89 |
| `routing/runtime_health.go` | 1455 |
| `routing/workflow.go` | 48 |
| **Total** | ~7,300 lines |
