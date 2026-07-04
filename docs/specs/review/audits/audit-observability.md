# Observability Audit: metapi-go

**Date:** 2026-07-05
**Scope:** `router/middleware.go`, `handler/proxy/upstream.go`, `proxy/surface.go`, `proxy/session.go`, `proxy/conductor.go`, `scheduler/*.go`, `handler/admin/stats.go`, `app/*.go`, `cmd/server/main.go` -- cross-referenced against all `slog`, `metrics`, `/debug/vars`, goroutine, and request-ID patterns across the entire codebase.
**Predecessor:** [audit-logging.md](audit-logging.md) (2026-07-04) covers log-level appropriateness and slog-vs-legacy-log findings. This audit focuses on **metrics, tracing, health endpoints, and runtime instrumentation** -- dimensions that audit-logging.md does not address.
**Result:** 12 findings (1 Critical, 4 High, 5 Medium, 2 Low)

---

## Summary

The codebase uses `log/slog` pervasively for structural logging. The `/health` endpoint exists for K8s/load-balancer liveness probes. The admin stats API (`/api/stats/*`) provides user-facing dashboard data via DB queries. However, **there is no production-grade observability plumbing**: no request-ID tracing, no `net/http/pprof` or `expvar` endpoint, no Prometheus-style `/metrics` endpoint, no goroutine or memory statistics exposed, no DB query timing instrumentation, no scheduler health reporting endpoint, and latency metrics collected during proxy handling are recorded into DB proxy_logs but never exposed as time-series metrics.

The runtime health engine (`routing/runtime_health.go`, 1454 lines) has an extensive internal model for per-site/per-channel health scoring with EMA-based latency tracking, Bayesian success-rate estimation, and circuit-breaker logic. But none of its internal state surfaces through an HTTP endpoint -- it is walled off behind opaque routing decisions.

---

## Findings

### Finding 1 [CRITICAL] -- No production observability endpoint exists (`/metrics`, `/debug/vars`, or `pprof`)

**Files:** `cmd/server/main.go`, `app/app.go`, `router/router.go`
**Category:** missing-observability-endpoint

The standard library provides `net/http/pprof` and `expvar` with zero external dependencies. Neither is registered. The go.mod has no Prometheus client, no OpenTelemetry SDK, no metrics library whatsoever. Observability stops at `slog` text output.

```go
// cmd/server/main.go:86 -- router is created, no additional debug mux
r := router.New(cfg, web.Dist)
```

**Impact:** In production, operators have no way to:
- Inspect goroutine count, heap usage, GC stats.
- Track request rate, error rate, or latency percentiles in real time.
- Set up alerting on memory pressure, goroutine leaks, or error-rate spikes.
- Correlate proxy failure spikes with memory or CPU patterns.

**Recommendation:**
- Add `net/http/pprof` behind a `--debug` flag or a separate `:6060` listener gated by loopback-only binding.
- Add `expvar` (stdlib, zero-deps) for `NumGoroutine`, `MemStats.Alloc`, custom counters (requests, errors, channel selections).
- Evaluate adding `prometheus/client_golang` for histogram-based latency tracking on proxy requests, channel selection, and DB queries.

---

### Finding 2 [HIGH] -- Zero request-ID tracing; request context is opaque

**Files:** `router/middleware.go:24-35` (`RequestLogger`), entire codebase
**Category:** missing-request-id-tracing

No request ID is generated or propagated. The `RequestLogger` middleware logs method/path/remote but attaches no trace identifier:

```go
// router/middleware.go:26-34
func RequestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slog.Info("request",
            "method", r.Method,
            "path",   r.URL.Path,
            "remote", r.RemoteAddr,
        )
        next.ServeHTTP(w, r)
    })
}
```

There is no middleware to read `X-Request-ID` from incoming requests or generate a UUID if absent. Chi's built-in `middleware.RequestID` is available but not used. Downstream proxy calls (`handler/proxy/upstream.go:83`) use `context.Background()` instead of propagating the request context, severing any trace chain:

```go
// handler/proxy/upstream.go:83
req, err := http.NewRequestWithContext(context.Background(), r.Method, upstreamURL, bytesReader(forwardBytes))
```

**Impact:** In production, correlating a single user request through auth -> channel selection -> upstream proxy -> failure log is impossible without timestamps. Debugging a transient 502 involves grep-by-timestamp across all log lines.

**Recommendation:**
- Add `middleware.RequestID` from chi to the middleware stack.
- Pass request context (not `context.Background()`) into upstream HTTP calls.
- Include `request_id` in all `slog` calls made within the request scope (use `slog.With` or pass a logger in context).
- Forward `X-Request-ID` to upstream services via the upstream request headers.

---

### Finding 3 [HIGH] -- Proxy latency is collected but not exposed as a metric; only persisted to DB

**Files:** `handler/proxy/upstream.go:98,112-114`, `proxy/surface.go:107-133`
**Category:** missing-latency-metrics

The upstream proxy handler measures latency with `time.Since(startedAt)`:

```go
// handler/proxy/upstream.go:98
latencyMs := time.Since(startedAt).Milliseconds()
```

But this value is only used in two ways:
1. Written into the `proxy_logs` DB table via `LogProxy` (async, can fail silently -- see audit-logging.md Finding 1).
2. Passed to `handleStreamUpstream` / `handleNonStreamUpstream` where it is logged at `slog.Warn` on failure only.

The `ProxyLogEntry` struct (`proxy/surface.go:108-133`) carries `LatencyMs` and `FirstByteLatencyMs` fields, but these are for DB persistence only. There is no in-memory histogram, no Prometheus summary, no expvar counter tracking latency distributions.

**Impact:** Alerting on proxy latency degradation requires polling the `proxy_logs` table. No real-time P50/P95/P99 latency visibility. Operators cannot answer "is the proxy getting slower?" without running SQL.

**Recommendation:**
- Wrap proxy latency measurement in a middleware that records a histogram (expvar or Prometheus).
- Record per-endpoint latency (`/v1/chat/completions` vs `/v1/embeddings` vs Gemini paths).
- Expose P50/P95/P99 via a new `/metrics` endpoint.

---

### Finding 4 [HIGH] -- No error-rate tracking in memory; failure recording is write-only to DB

**Files:** `proxy/surface.go:236-298` (`HandleUpstreamFailure`), `proxy/surface.go:368-420` (`HandleExecutionError`)
**Category:** missing-error-rate-metrics

Every proxy failure calls `ft.Router.RecordFailure(...)` and `ft.LogProxy(...)`, both of which write to DB. There is no in-memory counter tracking:
- Total requests (by model, by site, by channel)
- Error rate (by HTTP status code, by error type)
- Retry rate (how often retries succeed vs exhaust)
- Channel failover count

The routing engine's internal health model (`routing/runtime_health.go`) maintains per-channel success/fail counts for circuit-breaking decisions, but this data is not exposed through any endpoint.

**Impact:** Real-time error-rate alerts are impossible without external log/metric aggregation. A sudden spike in 502s from one upstream site is invisible until an operator checks the admin dashboard or proxy_logs table.

**Recommendation:**
- Add expvar counters: `proxy_requests_total`, `proxy_errors_total{status_code}`, `proxy_retries_total`.
- Add a simple sliding-window error rate (e.g., last 60s) exposed on a health-check endpoint.
- Wire the existing `routing/runtime_health.go` internal metrics to an HTTP endpoint.

---

### Finding 5 [HIGH] -- DB query timing is completely absent

**Files:** `handler/admin/stats.go`, `handler/admin/search.go:129-145` (`queryRows`), all `store/*.go`
**Category:** missing-db-query-timing

All DB queries are executed without timing instrumentation:

```go
// handler/admin/search.go:129-145
func queryRows(db *sqlx.DB, query string, args ...any) []map[string]any {
    rows, err := db.Queryx(query, args...)  // no timing
    if err != nil {
        return nil
    }
    defer rows.Close()
    // ...
}
```

The `store` package performs bootstrap, migrations, settings load, and CRUD operations -- none of which emit timing data. The scheduler package runs periodic DB queries (checkin, balance, backup, channel recovery, usage aggregation, admin snapshot) with no performance telemetry.

**Impact:** A slow DB query blocks the request handler with no visibility. Gradual DB degradation (e.g., proxy_logs table growing, missing index) cannot be detected from metrics. Scheduler queries that take longer than their tick interval silently overlap.

**Recommendation:**
- Add a `queryDuration` measurement in `queryRows` and expose via expvar per query label.
- Add `slog.Debug` with `elapsed_ms` for queries taking over a configurable threshold (e.g., 100ms).
- Consider wrapping `sqlx.DB` with a timed proxy or using `database/sql`'s `DB.Stats()` exposed on `/debug/vars`.

---

### Finding 6 [MEDIUM] -- Scheduler health is entirely invisible; 15 schedulers run with no status endpoint

**Files:** `scheduler/scheduler.go`, `scheduler/*.go` (15 scheduler files), `app/services.go`
**Category:** missing-scheduler-health

The `Registry` starts 15 schedulers in goroutines. Each scheduler has `Start()`/`Stop()` but no health check:

```go
// scheduler/scheduler.go:35-54
func (r *Registry) StartAll(ctx context.Context) {
    for _, s := range r.schedulers {
        go func(s Scheduler) {
            defer func() {
                if rec := recover(); rec != nil {
                    slog.Error("scheduler panicked during start", ...)
                }
            }()
            if err := s.Start(ctx); err != nil {
                slog.Warn("scheduler start failed", ...)
            }
        }(s)
    }
}
```

After `StartAll`, there is no way to query:
- Which schedulers are currently running.
- When each scheduler last ran successfully.
- When each scheduler last failed and why.
- Whether a scheduler goroutine has exited (panic not caught by `Start()` itself; only `Start()`'s immediate return is guarded).

The `inFlight` mutex pattern in `ChannelRecoveryScheduler` and `AdminSnapshotScheduler` prevents overlapping sweeps but also silently drops concurrent triggers -- this is never reported.

**Impact:** A crashed scheduler goroutine (e.g., panic after `Start()` returns) is permanently dead with no detection mechanism. A scheduler that is stuck (DB deadlock, network hang) blocks with no timeout or alert.

**Recommendation:**
- Add `LastRunAt time.Time`, `LastError error`, `RunCount int64` to the `Scheduler` interface.
- Expose `/api/health/schedulers` returning per-scheduler status JSON.
- Add per-scheduler goroutine health checks with a watchdog pattern (each scheduler pings a shared channel every N seconds; a monitor goroutine alerts on missed pings).
- Log scheduler completion/failure at `slog.Info`/`slog.Error` on every tick, not just on startup.

---

### Finding 7 [MEDIUM] -- `/health` endpoint returns static JSON; no dependency or internal-state checks

**Files:** `router/router.go:31-35`, `app/health.go`
**Category:** shallow-health-check

```go
// router/router.go:31-35
r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
})
```

This always returns 200 regardless of:
- DB connectivity (nil `db` returns 200).
- Scheduler health (crashed schedulers return 200).
- Upstream site reachability.

A second `/api/desktop/health` endpoint (line 80) does the same static response.

**Impact:** A load balancer or Kubernetes probe gets a green light while the application is degraded. DB failure, scheduler crash, or upstream outage are invisible to orchestration health checks.

**Recommendation:**
- Add optional deep health checks: ping DB, check scheduler liveness, report last successful proxy request time.
- Return 503 with a JSON body listing the failing component when health is degraded.
- Keep the current static `/health` as a lightweight liveness probe and add `/health/ready` for readiness.

---

### Finding 8 [MEDIUM] -- `slog` is used without configuration; defaults to text format on stderr

**Files:** `cmd/server/main.go`, all files calling `slog.*`
**Category:** unconfigured-slog

The `log/slog` package defaults to `slog.NewTextHandler(os.Stderr, nil)` -- no JSON output, no level control, no source location. There is no `slog.SetDefault()` call in the entire codebase:

```go
// cmd/server/main.go:18 -- main() starts
func main() {
    _ = godotenv.Load()
    env := environMap()
    cfg := config.Load(env)
    // ... no slog configuration anywhere
```

**Impact:** In container environments, structured JSON log output is expected for log aggregation (Loki, ELK, Datadog). The default text handler produces human-readable but machine-unparseable output. Log level cannot be controlled at runtime.

**Recommendation:**
- Add `LOG_LEVEL` and `LOG_FORMAT` config/env vars (e.g., `debug`/`info`/`warn`, `text`/`json`).
- Call `slog.SetDefault(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level, AddSource: true}))` early in `main()`.
- Wire log level changes at runtime via an admin endpoint (e.g., `PUT /api/admin/log-level`).

---

### Finding 9 [MEDIUM] -- No memory or goroutine statistics endpoint

**Files:** Entire codebase
**Category:** missing-runtime-metrics

The standard library's `runtime` package provides `runtime.NumGoroutine()`, `runtime.ReadMemStats()`, and `runtime.NumCPU()` with zero overhead. The `expvar` package can expose these as JSON at `/debug/vars` with one import. Neither is used.

The codebase has 15 background scheduler goroutines plus per-channel lease-expiry goroutines (`proxy/session.go:367-384`, one per active lease), per-channel keepalive goroutines (`proxy/session.go:389-401`), and per-request proxy handling goroutines. The number of goroutines could balloon silently.

**Impact:** A goroutine leak (e.g., channel-leases with stuck `expiryCh`) is undetectable. Memory pressure from proxy_logs query result buffering is invisible. Capacity planning relies on external OS-level monitoring.

**Recommendation:**
- Import `expvar` and `net/http/pprof` in `cmd/server/main.go` with an init-time registration.
- Bind to a separate `:6060` listener (loopback-only) serving `/debug/vars` and `/debug/pprof/`.
- Add custom expvar counters for active leases, sticky sessions, scheduler run counts.

---

### Finding 10 [MEDIUM] -- Admin stats API queries DB directly; no caching or metrics

**Files:** `handler/admin/stats.go:40-80` (dashboard), `handler/admin/stats.go:84-198` (proxyLogs)
**Category:** missing-admin-metrics-caching

The admin dashboard (`GET /api/stats/dashboard`) runs multiple `COUNT(*)` queries on every request:

```go
// handler/admin/stats.go:54-55
h.db.Get(&siteCount, "SELECT COUNT(*) FROM sites")
h.db.Get(&accountCount, "SELECT COUNT(*) FROM accounts")
```

The proxy-logs endpoint runs up to 3 separate queries (query items, count, meta summary) with joined table scans. These are unbounded queries with no time-range filter by default. There is no Redis/memory caching layer, and no query timing instrumentation on any of them.

**Impact:** Repeated dashboard refreshes or polling by the frontend generates unnecessary DB load. A large `proxy_logs` table (no retention limit in the query itself, only in the log-cleanup scheduler) causes slow page loads with no visibility.

**Recommendation:**
- Add query timing (see Finding 5).
- Cache dashboard summary counts (site/account count, total tokens/cost) in memory with a TTL (the admin-snapshot scheduler already partially does this but the handler ignores it).
- Apply a default time-range filter on proxy-logs queries (e.g., last 24h) when no explicit filter is provided.

---

### Finding 11 [LOW] -- `slog` key naming is inconsistent

**Files:** Cross-cutting (all slog calls)
**Category:** inconsistent-log-keys

`slog` keys use a mix of conventions:
- `"error"` (e.g., `app/app.go:73`) vs `"err"` (e.g., `proxy/surface.go:257`)
- `"channel_id"` (e.g., `proxy/surface.go:257`) in some places, no prefix convention elsewhere
- Some keys use `camelCase`, others use `snake_case`, others use `lowercase` with no separator (e.g., `"interval_ms"` vs `"intervalMs"`)

A quick survey:
| Pattern | Count | Example |
|---------|-------|---------|
| `"error"` | 28 | `cfg validation`, `server listen failed` |
| `"err"` | 18 | `RecordFailure failed`, `LogProxy failed` |
| `"interval_ms"` | 6 | scheduler start messages |
| `"channel_id"` | 12 | surface.go failure handlers |
| `"account_id"` | scattered | various |

**Impact:** Log aggregation queries (e.g., `jsonPayload.error` vs `jsonPayload.err`) are fragile. Alert rules based on structured keys need to handle both forms.

**Recommendation:**
- Standardize on `"error"` for error values (Go convention: the `error` interface's string).
- Adopt consistent `snake_case` for all structured log keys.
- Define key constants in a shared `logkeys` package.

---

### Finding 12 [LOW] -- Scheduler stop failures are logged but not aggregated

**Files:** `scheduler/scheduler.go:58-67`
**Category:** missing-scheduler-stop-metrics

```go
func (r *Registry) StopAll() {
    for _, s := range r.schedulers {
        if err := s.Stop(); err != nil {
            slog.Warn("scheduler stop failed", "name", s.Name(), "error", err)
        }
    }
}
```

Stop failures are logged once during shutdown and never surfaced again. If a scheduler consistently fails to stop (leaking a goroutine or holding a DB connection), there is no persistent record.

**Impact:** Repeated restart cycles could accumulate leaked resources without detection.

**Recommendation:**
- Track stop failures in expvar: `scheduler_stop_failures_total{scheduler="name"}`.
- Consider adding a force-stop timeout (e.g., `Stop()` must return within 5s or be abandoned with a log).

---

## Consolidated Recommendations (Priority Order)

### P0 -- Must Add (before production deployment)
1. **Register `pprof` and `expvar` on a loopback debug port** (Finding 1, Finding 9).
   ```go
   // In cmd/server/main.go:
   import _ "net/http/pprof"
   import _ "expvar"
   
   go func() {
       debugMux := http.NewServeMux()
       debugMux.Handle("/debug/vars", expvar.Handler())
       // pprof registers on DefaultServeMux
       http.ListenAndServe("127.0.0.1:6060", nil)
   }()
   ```

2. **Add request-ID tracing** (Finding 2). Add `middleware.RequestID` to the chi middleware stack. Pass request context into upstream HTTP calls instead of `context.Background()`.

### P1 -- Should Add (before production scaling)
3. **Add in-memory error rate and latency counters** via expvar (Finding 3, Finding 4).
4. **Add scheduler health endpoint** (Finding 6). Expose per-scheduler `lastRunAt`, `lastError`, `runCount` at `GET /api/health/schedulers`.
5. **Configure slog for JSON output** with level control (Finding 8).

### P2 -- Nice to Have (post-launch improvement)
6. **Add DB query timing** with a slow-query log threshold (Finding 5).
7. **Deepen `/health`** with DB ping and scheduler check (Finding 7).
8. **Standardize slog key naming** with constants (Finding 11).

### P3 -- Future
9. **Evaluate Prometheus `client_golang`** for histogram-based latency tracking (histograms need more memory and a registry; expvar counters suffice for early production).
10. **Cache admin dashboard queries** (Finding 10).
11. **Add scheduler stop-failure tracking** (Finding 12).

---

## Quick-Start Implementation: Minimum Viable Observability

Add this block to `cmd/server/main.go` before `a.Start()`:

```go
import (
    _ "expvar"
    "expvar"
    _ "net/http/pprof"
    "runtime"
)

func init() {
    // Publish runtime stats
    expvar.Publish("goroutines", expvar.Func(func() any {
        return runtime.NumGoroutine()
    }))
}

// In main(), after creating the router:
go func() {
    slog.Info("debug listener starting", "addr", "127.0.0.1:6060")
    if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
        slog.Error("debug listener failed", "error", err)
    }
}()
```

This single change (zero dependencies, ~15 lines) provides:
- `/debug/vars` -- `cmdline`, `memstats`, `goroutines` counter
- `/debug/pprof/` -- heap, goroutine, CPU profiles, allocation tracing

For request-ID tracing, add to `router/router.go`'s `New()` function:

```go
import "github.com/go-chi/chi/v5/middleware"

r.Use(middleware.RequestID)  // adds request_id to context and X-Request-ID header
```

And update `RequestLogger` to include it:

```go
func RequestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slog.Debug("request",
            "request_id", middleware.GetReqID(r.Context()),
            "method", r.Method,
            "path", r.URL.Path,
        )
        next.ServeHTTP(w, r)
    })
}
```

---

## Appendix: What Already Exists

| Capability | Status | Detail |
|------------|--------|--------|
| Structured logging (slog) | Present | All packages use `log/slog`; one legacy `fmt.Fprintf(os.Stderr, ...)` in `cmd/migrate/main.go` |
| Log levels | Mostly appropriate | See [audit-logging.md](audit-logging.md) for known level issues |
| `/health` endpoint | Present (shallow) | Returns static `{"status":"ok"}`, no dependency checks |
| Admin stats API | Present (DB-backed) | `/api/stats/*` routes for dashboard, proxy-logs, debug-traces |
| Proxy log persistence | Present | Every proxy request/failure writes to `proxy_logs` table |
| Runtime health engine | Present (internal) | `routing/runtime_health.go` -- per-site/channel health scoring, not exposed via HTTP |
| Channel recovery probe | Present | `scheduler/channel_recovery.go` -- probes cooldown channels, no health endpoint |
| Admin snapshot warm | Present | `scheduler/admin_snapshot.go` -- pre-warms dashboard snapshots |
| Request-ID tracing | **Absent** | No `middleware.RequestID`, no `X-Request-ID` propagation |
| `/metrics` or `/debug/vars` | **Absent** | No expvar, no pprof, no Prometheus endpoint |
| DB query timing | **Absent** | No timing instrumentation on any DB operation |
| Scheduler health reporting | **Absent** | 15 schedulers run with no status query capability |
| Goroutine/memory stats | **Absent** | No `runtime.NumGoroutine()` or `runtime.ReadMemStats()` exposure |
| In-memory error rate | **Absent** | Failures go to DB only; no real-time counters |
| Latency metrics endpoint | **Absent** | Latency is measured and stored in DB, never exposed as metrics |
| Slog configuration | **Absent** | Default text handler, no JSON output, no level control |
