# Error Recovery Paths Audit: metapi-go

**Date**: 2026-07-05  
**Auditor**: Automated audit of `D:/Code/TokenDance/metapi-go`  
**Scope**: Error recovery paths -- DB connection loss, upstream timeout, disk full, memory exhaustion, handler-level recovery, health check integrity, retry logic, circuit breaker

---

## 1. DB Connection Lost Mid-Request

### Current State

DB access is via a global singleton (`store.GetDB()`) stored in `store/bootstrap.go`. Every handler and scheduler that touches the DB retrieves the pointer through this call. There is **no connection validation on each access**. Once `activeDB` is set, it stays set until `CloseDatabase()` is explicitly called.

**Critical finding**: If the underlying PostgreSQL or SQLite connection is severed (network partition, PG restart, disk failure), the global pointer remains non-nil and handlers will attempt to execute queries against a dead connection. Chi's `middleware.Recoverer` is the only safety net -- it catches panics but does not catch DB errors that manifest as Go errors.

**Handler behavior under DB loss**:

| Handler type | Behavior | Severity |
|---|---|---|
| Admin handlers (e.g., `/api/accounts`) | `writeJSON(w, 500, {"error":"Failed to load accounts"})` -- explicit error check on `service.ListAccountsWithSites(h.db)`. Catches query failure, returns 500 to client. | Moderate |
| Proxy handlers (e.g., `/v1/chat/completions`) | Do not touch DB directly on the hot path. Channel selection, token routing happen in-memory. DB is accessed for route rebuild, settings refresh, proxy logging -- all fall through with logged warnings if `GetDB()` returns nil. | Low |
| Background schedulers | Each scheduler calls `store.GetDB()` on its own tick intervals. If nil, the sweep is skipped (no-op). If non-nil but dead, the query will error and the scheduler logs the error and moves on. No crash. | Low |
| Router initialization | `router.New()` checks `store.GetDB()`; if nil, P3 routes are skipped with a warning. But once set, never re-checked. | Moderate |

**Gap**: No `db.Ping()` before any query execution. If the connection pool is exhausted or the DB process is gone, the first DB call will fail with a Go error, which current handlers do catch -- but only at the individual query level. There is:
- No connection re-establishment attempt
- No health state transition (still "initialized")
- No circuit breaker on DB access

### SQLite-Specific Notes

SQLite has `PRAGMA busy_timeout = 5000` set in `applySQLitePragmas()` (store/open.go:155). If the WAL file can't be written (disk full), errors bubble up to handlers.

### PostgreSQL-Specific Notes

Connection pool: `MaxOpenConns=20, MaxIdleConns=5` (store/open.go:163). No `ConnMaxLifetime` or `ConnMaxIdleTime` is set. This means stale connections from a restarted PG server are never automatically recycled.

---

## 2. Upstream Timeout

### Current State

Upstream request handling is split across two code paths:

**Path A: `dispatchUpstream` (handler/proxy/upstream.go)**
- Uses `http.DefaultClient.Do(req)` -- **NO timeout configured**.
- Creates a `context.Background()` context, not a timeout context.
- The `req, err := http.NewRequestWithContext(context.Background(), ...)` on line 83 means the request has no deadline at all.
- If the upstream hangs indefinitely, the client goroutine blocks forever.

**Path B: `RuntimeExecutor` (proxy/executor.go)**
- Created via `NewRuntimeExecutor(requestTimeout)` which sets `http.Client{Timeout: requestTimeout}`.
- The `WithObservedFirstByte` method (line 87) adds an additional `context.WithTimeout` for first-byte latency, layered on top of the client timeout.
- This is used by the surface-based flows (chat, responses, etc.) via the conductor.
- **However**: the simpler `dispatchUpstream` in handler/proxy/upstream.go does NOT use RuntimeExecutor -- it uses `http.DefaultClient` directly.

**Gap**: The handler/proxy layer has TWO execution paths. The `dispatchUpstream` function in `upstream.go` is the simpler, shorter path used by `HandleChatCompletions` and similar handlers. It uses `http.DefaultClient` (zero timeout). The `RuntimeExecutor` path in `proxy/executor.go` does set timeouts, but it appears to be used only for surface-based flows (via `proxy/conductor.go`), not the direct handler path.

### Analysis

`handler/proxy/chat.go` calls `handleChatSurfaceRequest()` which calls `dispatchUpstream()`. `dispatchUpstream()` uses `http.DefaultClient` with no timeout. This means the default chat completions proxy path has **no upstream timeout**.

### Other upstream timeouts found:

| Location | Timeout | Used by |
|---|---|---|
| `platform/site_proxy.go:181` | 30s client timeout | Site proxy detection |
| `platform/cliproxyapi.go:39` | 5s context timeout | CLI proxy API |
| `platform/detect.go:172` | 5s context timeout | Platform detection |
| `scheduler/oauth_loopback.go:79` | 2s context timeout | OAuth loopback |
| `service/notify/telegram.go:54` | 30s client timeout | Telegram notifications |
| `service/oauth/codex.go:313` | 30s client timeout | Codex OAuth |
| `proxy/executor.go:40` | Configurable via NewRuntimeExecutor | Surface-based proxy flows |
| `handler/proxy/upstream.go:97` | **NONE** (http.DefaultClient) | Chat/completions proxy handler |

---

## 3. Disk Full

### Current State

**No explicit disk-full detection or handling anywhere in the codebase.**

- SQLite writes (WAL, main DB) will fail with OS errors if the disk is full. These bubble up as Go errors to whichever handler triggered the write.
- Log files (slog) will fail silently or to stderr.
- Backup/WebDAV scheduler (`scheduler/backup_webdav.go`) writes backup files -- no pre-check of available disk space.
- Proxy file retention scheduler (`scheduler/file_retention.go`) deletes old files -- but no space-awareness.
- Migration tool (`cmd/migrate/main.go`) writes SQLite files -- no disk check.
- `EnsureDataDir` (`store/open.go:168`) calls `os.MkdirAll` which will fail if the disk is full.

**Gap**: Disk full is a catastrophic failure mode with no graceful degradation. The server would start returning 500s for any write operation, but with no specific error classification or alerting for disk space.

---

## 4. Memory Exhaustion

### Current State

**No explicit memory limit, memory monitoring, or OOM protection.**

Key risk areas:
- `dispatchUpstream`'s `handleNonStreamUpstream` (handler/proxy/upstream.go:222) calls `io.ReadAll(resp.Body)` on the full upstream response. If the upstream returns a very large response body, this loads it entirely into memory.
- `proxy/executor.go:72` also calls `io.ReadAll(resp.Body)` -- same issue.
- SSE streaming (`handleStreamUpstream`, line 168) uses `bytes.Buffer` (accumulated, line 178) to collect the entire stream for post-hoc analysis. This accumulates the full stream in memory even though it's being streamed to the client.
- Snapshot caching for accounts (`handler/admin/accounts.go:46`) holds the full JSON response in memory with a 30-second TTL.
- Background schedulers each have their own goroutines. The scheduler registry starts all 14+ schedulers at once.

**Gap**: No `MaxBytesReader` on request body reads. No response body size limits. The accumulated SSE buffer grows unbounded.

---

## 5. Handler-Level Error Recovery (Beyond middleware.Recoverer)

### Current State

**middleware.Recoverer** (`router/middleware.go:45`): Wraps chi's `middleware.Recoverer` which catches panics and returns 500. This is a global safety net.

**Per-handler panic recovery**: **NONE found.** A grep for `defer func() { recover() }` in `handler/admin/` and `handler/proxy/` returned zero matches.

**Background scheduler recovery**: Present. The `Registry.StartAll` method (scheduler/scheduler.go:38) wraps each scheduler start in a panic recovery block. The `cronRunner.addJob` method (scheduler/cron.go:45) also wraps each cron job execution with panic recovery.

**Startup bootstrap**: Present. `cmd/server/main.go:48-53` wraps the entire bootstrap sequence in a panic recovery deferred function. Failures only produce warnings, the server still starts.

**Summary**:

| Component | Per-handler recovery | Global recovery |
|---|---|---|
| Admin HTTP handlers | None | middleware.Recoverer |
| Proxy HTTP handlers | None | middleware.Recoverer |
| Background schedulers | Yes (Registry.StartAll) | N/A |
| Cron jobs within schedulers | Yes (cronRunner.addJob) | Registry.StartAll |
| Startup bootstrap | Yes (deferred recover) | N/A |

**Assessment**: The global `middleware.Recoverer` is sufficient to prevent a single panicking handler from crashing the server. However, individual handlers that do complex work (OAuth flow, multi-step account creation) have no internal recovery -- if a panic occurs halfway through a multi-step operation, partial state mutations are not rolled back.

---

## 6. DB Ping on Health Check

### Current State

The `/health` endpoint (`router/router.go:31-34`) returns a hardcoded `{"status":"ok"}` with **no database ping, no dependency check, no readiness evaluation**.

The `/api/desktop/health` endpoint (line 80-84) is equally hardcoded.

The `app/health.go` file defines a `Health()` function that is identical -- hardcoded 200 OK. This function is NOT even wired into the router; the router inlines its own `/health` handler.

**Gap**: The health check is a **lie**. It reports "ok" even when:
- The database is completely unavailable
- All background schedulers have crashed
- The routing engine has no channels loaded
- Disk is full and writes are failing

There is no `/ready` (readiness) vs `/health` (liveness) distinction. For Kubernetes deployments, this means the pod will keep receiving traffic even when it cannot serve requests.

---

## 7. Connection Retry Logic

### Current State: Rich retry infrastructure on the proxy path, none on the DB/admin path.

**Proxy retry (channel-level)**:

`proxy/retry_policy.go` defines sophisticated error pattern matching:

| Category | Patterns | Action |
|---|---|---|
| Model unsupported | 14 regex patterns (CN + EN) | Retry with different channel |
| Timeout | 4 regex patterns | Retryable |
| Channel-local failures | 20 regex patterns (rate limit, forbidden, bad gateway, etc.) | Retry with different channel |
| Non-retryable request errors | 10 regex patterns (validation, malformed, etc.) | Do not retry |
| Same-site endpoint abort | 17 regex patterns | Abort same-site endpoint fallback |

`proxy/conductor.go` implements the action-based retry loop:
- `ActionRetrySameChannel` -- retry immediately on same channel (408/429 with retryable pattern)
- `ActionRefreshAuth` -- refresh OAuth token and retry (401/403)
- `ActionFailover` -- exclude current channel, try next (5xx)
- `ActionTerminal` -- give up (4xx non-auth)

`proxy/channel_selection.go` (`SelectProxyChannelForAttempt`) implements:
- Sticky session preference
- Route refresh retry on empty selection
- Tester forced-channel bypass
- Exclusion list for already-tried channels

**Cooldown (backoff)**:

`routing/cooldown.go` implements Fibonacci backoff: 15s * fib(failCount), capped at 30 days max.

**DB/admin retry**: **None.** If a DB query fails in an admin handler, the handler returns a 500 error and does not retry. There is no retry middleware or retry wrapper for DB operations.

**DB connection retry on startup**: **None.** `EnsureRuntimeDatabase` in `store/bootstrap.go` calls `Open()` which calls `db.Ping()` once. If ping fails, the error is returned and logged as a warning, but the server continues to start without a database.

---

## 8. Circuit Breaker on Persistent Failures

### Current State: Present and sophisticated for upstream routing; absent for DB/admin paths.

**Upstream circuit breaker** (`routing/runtime_health.go`):

The `SiteRuntimeHealthState` tracks per-site and per-(site, model) health:

| Mechanism | Detail |
|---|---|
| Breaker levels | 4 levels: [0ms, 60s, 5min, 30min] |
| Breaker trigger | 3 consecutive transient failures within 5-minute window |
| Breaker escalation | Each consecutive breaker trip increments the level |
| Breaker reset | On success: `BreakerLevel = 0`, `BreakerUntilMs = nil` |
| Penalty score | 0.25 (validation) to 2.5 (5xx/transient) |
| Decay | Half-life of 10 minutes (`SiteRuntimeHealthDecayHalfLifeMs`) |
| Latency tracking | EMA with alpha=0.3, baseline 2500ms, max penalty 35% |
| Historical health | Aggregated from channel success/failure counts, min multiplier 0.45 |
| Recent outcomes | Time-decayed success/failure counts, 30-min half-life |
| Persistence | Saved to `settings` table with 500ms debounce; restored on startup |
| Candidate filtering | `FilterSiteRuntimeBrokenCandidates` excludes broken sites from selection |
| Cooldown bypass | `IsChannelRecentlyFailed` checks Fibonacci backoff window |

The selector (`routing/selector.go`) integrates this: broken channels are filtered out during candidate ranking.

**Channel recovery** (`scheduler/channel_recovery.go`):
- Sweeps cooling channels every 30 seconds
- Probes active channels every 5 minutes
- Batch size limited to 4 candidates per sweep
- **Stub**: `probeCandidate` method has a TODO comment (line 314) and currently only logs -- actual probe execution is not implemented.

**DB/admin circuit breaker**: **None.** No circuit breaker protects DB access, admin handlers, or background scheduler DB queries.

---

## Summary of Findings

| # | Finding | Severity | Component |
|---|---|---|---|
| 1 | `/health` endpoint never pings DB -- hardcoded 200 OK | **CRITICAL** | Health check |
| 2 | `dispatchUpstream()` uses `http.DefaultClient` with NO timeout | **CRITICAL** | Proxy handler |
| 3 | Accumulated SSE buffer in `handleStreamUpstream` grows unbounded | **HIGH** | Proxy handler |
| 4 | No per-handler panic recovery in admin or proxy handlers | **HIGH** | All handlers |
| 5 | No DB connection retry or re-establishment on query failure | **HIGH** | DB layer |
| 6 | No PostgreSQL `ConnMaxLifetime` or `ConnMaxIdleTime` set | **HIGH** | DB pool |
| 7 | Channel recovery probe is a stub (TODO, not implemented) | **MODERATE** | Scheduler |
| 8 | No disk-full detection or graceful degradation | **MODERATE** | Global |
| 9 | No memory limit, MaxBytesReader, or OOM protection | **MODERATE** | Global |
| 10 | No circuit breaker for DB access or admin handlers | **MODERATE** | DB/Admin |
| 11 | `io.ReadAll` on unbounded upstream response bodies | **MODERATE** | Proxy handler |
| 12 | DB loss during active request returns 500s but server survives | **LOW** | All handlers |

---

## What Works Well

1. **Proxy-level circuit breaker**: The `routing/runtime_health.go` implementation is thorough -- multi-level breakers, EMA latency tracking, time-decayed penalties, persistence, candidate filtering.

2. **Proxy retry classification**: `proxy/retry_policy.go` has 60+ regex patterns covering model unsupported, timeout, channel-local, non-retryable, and same-site abort categories. This is comprehensive pattern matching.

3. **Background scheduler isolation**: `Registry.StartAll` wraps each scheduler goroutine with panic recovery, so a single scheduler crash does not take down others.

4. **Startup bootstrap resilience**: The bootstrap sequence is wrapped in panic recovery; individual step failures only produce warnings and the server still starts.

5. **Cooldown/backoff**: Fibonacci-based backoff with 30-day cap, tiered round-robin cooldown levels, rolling window for transient failure streaks.

6. **Rate limiting**: Per-IP token bucket with 5-minute idle cleanup, separate tiers for admin (100/s) and OAuth (10/s).

7. **Graceful shutdown**: 5-second timeout with `OnClose` hooks, DB cleanup, signal handling (SIGINT/SIGTERM).

---

## Recommended Actions (Prioritized)

### P0 -- Immediate

1. **Fix health check**: Make `/health` actually ping the database. Add a `/ready` endpoint that checks DB connectivity + critical dependencies. Consider returning 503 if DB is unavailable.

2. **Add upstream timeout to dispatchUpstream**: Replace `http.DefaultClient` with a configured client. Set a reasonable timeout (e.g., 120s for chat completions). Use `context.WithTimeout` on the request context.

### P1 -- Short Term

3. **Add per-handler panic recovery**: Wrap critical admin handlers (account creation, OAuth flow) with deferred recovery that rolls back partial state.

4. **Set DB pool lifetime limits**: Add `SetConnMaxLifetime(5 * time.Minute)` and `SetConnMaxIdleTime(1 * time.Minute)` for PostgreSQL connections.

5. **Implement channel recovery probe**: The `TODO` in `scheduler/channel_recovery.go:314` needs to be filled in with actual probe execution.

6. **Add MaxBytesReader on proxy request/response bodies**: Cap `io.ReadAll` at a configurable limit (e.g., 10MB).

### P2 -- Medium Term

7. **Add DB connection retry wrapper**: Wrap `store.GetDB()` calls with a retry helper that re-pings and retries on transient errors (3 attempts, exponential backoff).

8. **Add disk space monitoring**: Periodic check of `dataDir` available space; log warnings at 10% and errors at 5%.

9. **Do not accumulate full SSE stream in memory**: Parse SSE events incrementally instead of buffering the entire stream for post-hoc analysis.

10. **Add circuit breaker for background scheduler DB access**: If DB queries fail N times consecutively in a scheduler, back off exponentially before retrying.

---

## Files Examined

- `router/middleware.go` -- Middleware stack (Recoverer, CORS, logging, RealIP)
- `router/router.go` -- Route registration, health endpoint, SPA fallback
- `app/health.go` -- Unused health handler function
- `app/app.go` -- App lifecycle, graceful shutdown
- `app/services.go` -- Background scheduler registration
- `store/bootstrap.go` -- DB singleton, EnsureRuntimeDatabase
- `store/open.go` -- DB connection pool, SQLite pragmas, PG pool config
- `store/migrate.go` -- Schema auto-migration
- `store/settings.go` -- Runtime settings hydration
- `handler/proxy/upstream.go` -- `dispatchUpstream` (main proxy path)
- `handler/proxy/router.go` -- Proxy route registration, `writeJSONError`
- `handler/proxy/chat.go` -- Chat completions handler
- `handler/admin/accounts.go` -- Admin CRUD handler pattern
- `handler/admin/settings_database.go` -- DB settings handler (stubs)
- `proxy/retry_policy.go` -- Retry classification patterns
- `proxy/conductor.go` -- Action-based retry conductor
- `proxy/channel_selection.go` -- Channel selection with retry
- `proxy/executor.go` -- HTTP executor with timeouts
- `proxy/surface.go` -- Surface failure toolkit
- `routing/runtime_health.go` -- Circuit breaker and health tracking
- `routing/cooldown.go` -- Fibonacci backoff and cooldown filtering
- `scheduler/scheduler.go` -- Scheduler registry with panic recovery
- `scheduler/channel_recovery.go` -- Channel recovery probe scheduler (stub)
- `scheduler/cron.go` -- Cron runner with panic-safe job execution
- `auth/ratelimit.go` -- Per-IP token bucket rate limiter
- `cmd/server/main.go` -- Server entry point with bootstrap recovery
- `config/config.go` -- Configuration loading and validation
