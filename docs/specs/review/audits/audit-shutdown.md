# Graceful Shutdown Safety Audit

**Audit date:** 2026-07-04
**Scope:** `<repo>`
**Files examined:** `app/app.go`, `cmd/server/main.go`, `scheduler/scheduler.go`, `service/oauth/callback_server.go`, `app/services.go`, `store/open.go`, `store/bootstrap.go`, `store/switch.go`, `scheduler/*.go` (all 8 scheduler files), `handler/proxy/upstream.go`, `service/proxy_util.go`, `proxy/executor.go`

---

## Summary

| Dimension | Verdict | Severity |
|---|---|---|
| Signal handling | Pass | -- |
| Shutdown ordering | **Fail** -- DB never closed | CRITICAL |
| Scheduler stop sequence | **Warn** -- insertion-order, no dependency awareness | HIGH |
| Connection draining timeout | **Warn** -- hardcoded 5s, no drain-before-close | MEDIUM |
| Goroutine leak after shutdown | **Fail** -- proxy requests leak via context.Background() | HIGH |
| OAuth loopback cleanup | **Warn** -- race on startup vs stop | MEDIUM |

---

## 1. Signal Handling (SIGINT/SIGTERM)

**File:** `app/app.go:49-90`

The `App.Start()` method correctly:
- Registers for `syscall.SIGINT` and `syscall.SIGTERM` via `signal.Notify`.
- Uses a `select` over both `errCh` (listen errors) and `sigCh` (signals), so both crash-fast and graceful-stop paths are handled.
- The `errCh` is a buffered channel (capacity 1), preventing the ListenAndServe goroutine from leaking if nobody reads.

**Verdict: PASS.** No issues found.

---

## 2. Shutdown Ordering -- CRITICAL: DB Never Closed

**Files:** `app/app.go:82-86`, `cmd/server/main.go:81-96`

The actual shutdown sequence in `App.Start()` is:

```
1. signal received
2. a.OnClose() → StopBackgroundServices() → registry.StopAll()
3. a.Server.Shutdown(ctx) with 5s timeout
```

Then in `main.go:93-95`, after `a.Start()` returns:
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
_ = a.Shutdown(ctx)
```

This redundant `a.Shutdown()` is dead code -- the `Server` was already shut down inside `a.Start()`, so `a.Shutdown()` is a no-op (it checks `a.Server == nil`? No, it checks nil but the Server field is not set to nil after shutdown).

### FINDING C1: Database connection is never closed

`store.CloseDatabase()` exists in `store/bootstrap.go:89-99` but is **never called** in any shutdown path. The only `RegisterOnClose` hook is:

```go
a.RegisterOnClose(func() {
    app.StopBackgroundServices()
})
```

There is no hook for `store.CloseDatabase()`.

**Impact:**
- **SQLite:** The WAL file is not checkpointed on exit. Uncommitted WAL frames are lost. The `-wal` and `-shm` files are left on disk. On next start, SQLite must replay the WAL from scratch (minor startup delay).
- **PostgreSQL:** Connections in the pool (`sqlx.DB`) are not gracefully closed. The OS will eventually close the TCP sockets, but the server-side will see connection resets rather than clean `pg_terminate_backend`.

**Fix:** Add `store.CloseDatabase()` to the shutdown sequence, after scheduler stop but before HTTP server shutdown:

```go
a.RegisterOnClose(func() {
    app.StopBackgroundServices()
})
a.RegisterOnClose(func() {
    if err := store.CloseDatabase(); err != nil {
        slog.Warn("failed to close database", "error", err)
    }
})
```

### FINDING C2: Shutdown runs OnClose BEFORE Server.Shutdown

`a.OnClose()` is called at line 82, **before** `a.Server.Shutdown(ctx)` at line 83. This means:
- Schedulers are stopped while the HTTP server is still accepting connections.
- In-flight HTTP handlers may try to access the DB (`store.GetDB()`) after `CloseDatabase()` returns nil (if C1 is fixed and DB close is added to OnClose).

**Fix:** Reverse the order: shut down the HTTP server first (stop accept + drain in-flight), then run OnClose hooks (stop schedulers + close DB):

```go
// 1. Stop accepting new connections
if err := a.Server.Shutdown(ctx); err != nil { ... }
// 2. Now safe to stop background services and close DB
a.OnClose()
```

---

## 3. Scheduler Stop Sequence

**Files:** `scheduler/scheduler.go:56-67`, `app/services.go`

`registry.StopAll()` iterates in registration (insertion) order:

```
1. usage-aggregation
2. checkin
3. balance
4. daily-summary
5. log-cleanup
6. backup-webdav
7. site-announcement
8. model-probe
9. channel-recovery
10. sub2api-refresh
11. update-center
12. admin-snapshot (depends on usage-aggregation)
13. proxy-file-retention
14. proxy-log-retention
15. oauth-loopback
```

### FINDING H1: No dependency-aware stop ordering

`admin-snapshot` references the `usage-aggregation` scheduler instance directly (passed via constructor). If `admin-snapshot` is stopped before `usage-aggregation`, there is no issue -- the reference just becomes unused. But if the order were reversed, `admin-snapshot` could call into a stopped scheduler. Currently, insertion order places `usage-aggregation` at position 1 and `admin-snapshot` at position 12, so `usage-aggregation` stops first. This happens to be safe by accident, not by design.

**Fix:** Document the dependency and/or reverse the stop order (LIFO) so dependents stop before their dependencies.

### FINDING H2: All schedulers get nil context

`registry.StartAll(nil)` at `app/services.go:68` passes `nil` as the context. Each scheduler's background goroutine checks both `<-s.stopCh` and `<-ctx.Done()`. With a nil context, `ctx.Done()` never fires -- only `stopCh` close terminates the loop. This works but means schedulers cannot be stopped via context cancellation, only via the explicit `Stop()` method.

**Impact:** LOW. The `Stop()` method is the intended termination path. The `ctx.Done()` branch is dead code in practice.

### FINDING H3: StopAll is sequential and blocking

Each `s.Stop()` call is synchronous. If a scheduler's `Stop()` hangs (e.g., waiting on a mutex held by a long-running sweep), the entire shutdown blocks. The channel-recovery scheduler's `runSweep()` can hold the mutex for the duration of a DB query. In normal operation this is milliseconds, but a slow DB could delay shutdown.

**Impact:** LOW-Medium. Mitigated by the fact that `Server.Shutdown` has its own 5s timeout.

---

## 4. Connection Draining Timeout

**Files:** `app/app.go:79`, `cmd/server/main.go:93`

- `app.Start()` uses a **hardcoded 5-second timeout** for `Server.Shutdown()`.
- `main.go` has its own 5-second timeout for the redundant `a.Shutdown()` call (dead code, see C1 discussion).
- `scheduler/oauth_loopback.go:79` uses a **hardcoded 2-second timeout** per loopback server.

### FINDING M1: No configurable drain timeout

The 5-second value is not configurable. Production deployments may need longer drain time for streaming connections (SSE responses can be open for minutes).

**Fix:** Add a `ShutdownTimeoutSeconds` field to `config.Config` with a default of 5.

### FINDING M2: No drain-before-close pattern

`Server.Shutdown()` in Go's standard library does:
1. Close all idle keep-alive connections.
2. Wait for active connections to become idle.
3. Then shut down.

However, for SSE streaming connections (used in `handleStreamUpstream`), the connection is never "idle" -- it's actively streaming. The 5-second timeout will forcibly terminate these connections mid-stream.

**Impact:** MEDIUM. Streaming clients will see abrupt disconnection instead of a clean EOF.

---

## 5. Goroutine Leak After Shutdown

### FINDING G1: Proxy requests use context.Background()

**File:** `handler/proxy/upstream.go:94`

```go
resp, err := http.DefaultClient.Do(req)
```

The request is created with `context.Background()` (line 81):
```go
req, err := http.NewRequestWithContext(context.Background(), r.Method, upstreamURL, ...)
```

During shutdown, `Server.Shutdown()` waits for active HTTP handlers to complete. But the upstream proxy request has no connection to the request context -- it uses `context.Background()`, so it will NOT be cancelled when the server shuts down. If the upstream is slow or unresponsive, the in-flight proxy request blocks the handler, which blocks `Server.Shutdown()`, which blocks the entire process exit until the 5-second timeout fires.

Additionally, `http.DefaultClient` has **no timeout** (its `Timeout` field is 0 = no timeout), so an unresponsive upstream will block indefinitely until the shutdown timeout.

**Impact:** HIGH. In-flight proxy requests during shutdown can block the process for up to 5 seconds (the shutdown timeout). If the shutown timeout were removed or made very large, this would be a hard hang.

**Fix:** Propagate the request context:
```go
req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, ...)
```

### FINDING G2: proxy_util.go HTTP helpers use context.Background()

**File:** `service/proxy_util.go:40-63`

`HTTPGet`, `HTTPPost`, and `HTTPPostJSON` all call:
```go
req, err := http.NewRequestWithContext(context.Background(), ...)
```

These are utility functions called from schedulers (checkin, balance refresh, etc.) and admin handlers. During shutdown, if a scheduler's in-flight work calls these, there is no cancellation mechanism.

**Impact:** MEDIUM. Affects background work, not request handlers.

**Fix:** Accept a `context.Context` parameter and propagate it:
```go
func HTTPGet(ctx context.Context, proxyURL, requestURL string, headers map[string]string) (*http.Response, error) {
```

### FINDING G3: Fire-and-forget goroutines in schedulers

Multiple schedulers use the pattern:
```go
case <-s.ticker.C:
    go s.runSweep()  // or go s.runPass()
```

During shutdown, after `Stop()` closes `stopCh`, the ticker loop exits, but any already-spawned `go s.runSweep()` goroutine continues running. This is safe because:
- `runSweep()` / `runPass()` check a `sweepInFlight` / `projectionInFlight` flag to prevent concurrent runs.
- They complete their work and exit.

No leak here. However, if the sweep involves a DB query that hangs, the goroutine leaks permanently with no timeout.

### FINDING G4: Channel recovery goroutine spawns without bound

**File:** `scheduler/channel_recovery.go:64`

```go
case <-s.ticker.C:
    go s.runSweep()
```

If `runSweep()` takes longer than the ticker interval (30s), multiple goroutines stack up. The `sweepInFlight` flag in `runSweep()` prevents actual concurrent execution, but each tick still spawns a goroutine that immediately returns when it sees `sweepInFlight == true`. These are short-lived but wasteful. Not a leak, but a design concern.

**Impact:** LOW. The 30-second interval and typical sub-second sweep time make this unlikely.

---

## 6. OAuth Loopback Server Cleanup

Two independent implementations exist:

### 6a. Package-level globals (`service/oauth/callback_server.go`)

**Cleanup function:** `StopLoopbackCallbackServers()` (line 236)

**Analysis:**
- Copies the server map under lock, clears globals, then iterates and calls `server.Close()` (not `Shutdown`).
- Uses `server.Close()` which immediately closes all connections without draining.
- Properly resets `callbackStates` and `callbackStartPromises`.
- The promise-based deduplication (`callbackStartPromises`) is properly cleaned -- the channel is closed after the mutex unlock.

**Verdict: PASS.** Cleanup is correct and thread-safe.

### 6b. Scheduler-owned (`scheduler/oauth_loopback.go`)

**Cleanup function:** `Stop()` (line 64)

**FINDING R1: Race between Start and Stop**

`startProviderServer()` (line 96) runs in a goroutine and appends to `s.servers` and `s.listeners`:
```go
s.mu.Lock()
s.listeners = append(s.listeners, listener)  // line 131
s.mu.Unlock()
// ...
s.mu.Lock()
s.servers = append(s.servers, srv)  // line 141
s.mu.Unlock()
```

If `Stop()` is called between the two lock sections (after listener is appended but before server is appended), the listener will be closed but the server will not be shut down -- it was never added to `s.servers`. The `srv.Serve(listener)` call on line 149 will then fail because the listener was closed.

**Impact:** LOW. This only matters if shutdown happens within microseconds of startup. In practice, `Stop()` is called minutes/hours after `Start()`.

**Fix:** Append both listener and server in a single lock section:
```go
s.mu.Lock()
s.listeners = append(s.listeners, listener)
s.servers = append(s.servers, srv)
s.mu.Unlock()
```

### 6c. Duplicate OAuth loopback functionality

The `service/oauth/callback_server.go` package has its own `StartLoopbackCallbackServers()` and `StopLoopbackCallbackServers()` with a completely separate set of globals and server management. The `scheduler/oauth_loopback.go` duplicates this functionality. There are now two parallel OAuth loopback server systems, and **neither is wired into the shutdown lifecycle**.

- `service/oauth/callback_server.go`: Not registered in `app/services.go` startup, not called in any OnClose hook.
- `scheduler/oauth_loopback.go`: Registered as a scheduler in `app/services.go:65`, so it is started via `StartBackgroundServices()` and stopped via `StopBackgroundServices()`.

**Impact:** MEDIUM. The `service/oauth/callback_server.go` servers, if started independently (e.g., by an API handler calling `StartLoopbackCallbackServer`), will never be cleaned up during graceful shutdown since `StopLoopbackCallbackServers()` is never called from the shutdown path.

**Fix:** Either:
1. Wire `StopLoopbackCallbackServers()` into the OnClose hooks, OR
2. Consolidate into a single implementation.

---

## 7. Additional Findings

### FINDING A1: Redundant shutdown in main.go

**File:** `cmd/server/main.go:92-95`

After `a.Start()` returns (which already called `OnClose()` and `Server.Shutdown()`), main.go calls `a.Shutdown(ctx)` again. This is dead code -- the server was already shut down.

**Fix:** Remove lines 92-95, or restructure so that `App.Start()` only blocks on signal and returns, leaving shutdown to the caller.

### FINDING A2: No health-check drain before shutdown

**File:** `app/app.go`

There is no mechanism to mark the service as "unhealthy" before beginning shutdown, which would allow load balancers to drain traffic before connections are rejected. This is a P2 feature, not a bug.

### FINDING A3: OAuth callback_server.go uses server.Close() not server.Shutdown()

**File:** `service/oauth/callback_server.go:248`

`server.Close()` immediately closes all connections without waiting for in-flight handlers. Since these are loopback OAuth callback servers (handling a single redirect), this is acceptable but inconsistent with the scheduler-owned version which uses `Shutdown(ctx)`.

---

## Recommendations Summary

| ID | Severity | Summary | File(s) |
|---|---|---|---|
| C1 | CRITICAL | DB connection never closed on shutdown | `store/bootstrap.go`, `cmd/server/main.go` |
| C2 | HIGH | OnClose runs before Server.Shutdown -- wrong order | `app/app.go:82-86` |
| G1 | HIGH | Proxy requests use context.Background(), no cancellation | `handler/proxy/upstream.go:81,94` |
| G2 | MEDIUM | HTTP helpers use context.Background() | `service/proxy_util.go:40,53` |
| M1 | MEDIUM | Hardcoded 5s drain timeout, not configurable | `app/app.go:79` |
| R1 | LOW | Race between Start and Stop in OAuth loopback | `scheduler/oauth_loopback.go:130-142` |
| 6c | MEDIUM | Duplicate OAuth loopback; package-level servers never cleaned up | `service/oauth/callback_server.go` |
| A1 | LOW | Redundant a.Shutdown() call after a.Start() returns | `cmd/server/main.go:92-95` |
| H2 | LOW | Nil context makes ctx.Done() dead code in scheduler loops | `scheduler/*.go` |
| A3 | LOW | callback_server.go uses Close() instead of Shutdown() | `service/oauth/callback_server.go:248` |

### Recommended shutdown sequence:

```
1. signal received (SIGINT/SIGTERM)
2. [P2] mark health check as failing (tell LB to drain)
3. [P2] wait drain_delay seconds
4. Server.Shutdown(ctx) -- stop accepting, drain in-flight
5. StopBackgroundServices() -- stop all schedulers
6. CloseDatabase() -- close DB pool
7. StopLoopbackCallbackServers() -- close OAuth loopback servers
8. exit
```
