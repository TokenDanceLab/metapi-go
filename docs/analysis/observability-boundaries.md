# Observability Boundaries (R2 / #26)

**Date:** 2026-07-17  
**Lane:** reliability  
**SSOT code:** `app/health.go`, `app/prometheus.go`, `handler/shared/metrics.go`, `handler/proxy/upstream.go`, `handler/admin/accounts.go`, `handler/admin/token_routes.go`

## Purpose

Define the public semantics of liveness/readiness/metrics endpoints, document what proxy counters mean, and pin admin validation status-code rules so clients never treat silent HTTP 200 + `success:false` as “ok transport.”

## Endpoint semantics

| Path | Auth | Purpose | Success | Failure |
|:-----|:-----|:--------|:--------|:--------|
| `GET /health` | none | **Liveness** only. Process is up and can answer HTTP. | `200` `{"status":"ok"}` | Does **not** probe DB or routing. |
| `GET /ready` | none | **Readiness** for load balancers / rolling deploy. | `200` `{"status":"ok","database":"ok"}` | `503` when DB nil/ping fails (`status=degraded`) or shutdown drain started (`status=draining`, DB still `ok` if ping works). |
| `GET /metrics` | none | Prometheus text exposition (`text/plain; version=0.0.4`). | `200` with counters/gauges | Write errors are logged; body may be partial. |
| `GET /api/debug/vars` | admin | JSON process/memory snapshot (expvar-style). | `200` | Behind admin auth + rate limit. |

### `/health` vs `/ready`

- Orchestrators must use **`/ready`** for traffic admission.
- **`/health`** must stay cheap and dependency-free so a stuck DB does not kill the process under a liveness probe.
- During graceful shutdown, `/ready` flips to `draining` **before** in-flight requests are torn down.

### `/metrics` series (current)

| Metric | Type | When incremented / set |
|:-------|:-----|:-----------------------|
| `metapi_uptime_seconds` | gauge | Process age at scrape |
| `metapi_proxy_requests_total` | counter | Start of `dispatchUpstream` (hot path entered) |
| `metapi_proxy_errors_total` | counter | Terminal proxy failure after hot-path entry (no channel, upstream fail after retries, content failure, exhausted channels, unconfigured upstream) |
| `metapi_proxy_streams_active` | gauge | `+1` when SSE relay starts, `-1` when it ends |
| `metapi_active_channels` | gauge | Optional setter (`SetActiveChannels`) |
| `metapi_db_connections_open` | gauge | Refreshed from `sql.DB.Stats().OpenConnections` on scrape when DB is initialized |
| `metapi_route_rebuild_total{result="completed"}` | counter | Successful `POST /api/routes/rebuild` cache invalidate |

**Non-goals (R2):** histograms/P99, request-id propagation (separate), OpenTelemetry, auth on `/metrics`, lease gauges wired from coordinator.

## Proxy hot-path recording

```
dispatchUpstream
  RecordProxyRequest()                 // always once per entry
  ... selection / upstream ...
  on terminal failure:
    RecordProxyError()                 // once per terminal response
  on stream 2xx body:
    handleStreamUpstream
      RecordStreamStart()
      defer RecordStreamEnd()
```

Client-side validation failures that never reach `dispatchUpstream` (auth middleware, JSON parse in route handlers) do **not** increment proxy request counters.

## Admin validation status rules

### `POST /api/accounts/login`

| Condition | HTTP | Body |
|:----------|:-----|:-----|
| Invalid/missing JSON, siteId≤0, empty username/password | **400** | `{success:false, message}` |
| Site row not found | **404** | `{success:false, message:"site not found"}` |
| Unsupported platform / upstream login failure / empty token from upstream | **200** | `{success:false, message}` (business outcome, not request validation) |
| Persist / encrypt failures | **500** | `{success:false, message}` |
| Success | **200** | `{success:true, account, ...}` |

### `POST /api/accounts/verify-token`

| Condition | HTTP | Body |
|:----------|:-----|:-----|
| Invalid/missing JSON, siteId≤0 | **400** | `{success:false, message}` |
| Empty access token | **400** | `{success:false, error:"Token 不能为空"}` |
| Site row not found | **404** | `{success:false, message:"site not found"}` |
| Unsupported platform / upstream verify failure / unknown token type | **200** | `{success:false, ...}` |
| Success | **200** | `{success:true, tokenType, models, ...}` |

### `POST /api/routes/rebuild`

Current implementation only invalidates the in-process route cache (`routing.InvalidateCache`). It does **not** enqueue a background job.

| Condition | HTTP | Body |
|:----------|:-----|:-----|
| Cache invalidate completed | **200** | `{success:true, queued:false, status:"completed", message:...}` |

Response must **not** claim `queued:true` / `status:"pending"` / fake `jobId` when no async work was scheduled. Structured log: `routes rebuild: route cache invalidated`.

## Boundary tests

- `app` / `handler/shared` — Prometheus exposition contains required HELP/TYPE lines and counters move after record helpers.
- `handler/admin` — login/verify-token site-not-found → 404; empty verify token → 400.
- `handler/admin` — rebuild returns completed/not pending.

## Residual risks

1. `/metrics` is unauthenticated (same as many internal scrapers); bind network policy externally if exposed.
2. Proxy error counter is terminal-only; retries that later succeed still count as one request and zero errors.
3. Stream gauge can go negative if `RecordStreamEnd` is called without start (tests reset via `ResetMetricsForTest`).
4. Full route “rebuild from accounts/models” remains a future feature; R2 only makes the existing invalidate path truthful.
