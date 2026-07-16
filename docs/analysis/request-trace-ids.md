# Request / Trace IDs across retries & failovers (#110)

**Date:** 2026-07-17  
**Lane:** competitive learn / observability  
**SSOT code:** `router/request_id.go`, `proxy/request_id.go`, `handler/proxy/upstream.go`, `handler/proxy/proxy_log.go`, `handler/shared/errors.go`

## Goal

Give operators one stable identifier for a single client proxy call even when MetAPI retries across channels or protocol endpoints. Correlate:

1. ingress access logs (`RequestLogger`)
2. per-attempt `slog` lines on the proxy hot path
3. `proxy_logs` rows (success path today)
4. client-facing JSON error bodies / `X-Request-Id` header

This is **not** OpenTelemetry / Langfuse. It reuses chi's `middleware.RequestID`.

## Ingress

| Layer | Behavior |
|:------|:---------|
| `router.WithRequestID` | Wraps chi `middleware.RequestID`. Accepts inbound `X-Request-Id` or generates one. Writes `X-Request-Id` on the response. Stores id in request context. |
| `router.RequestLogger` | Emits `request_id` on every access log line. |
| CORS | Exposes `X-Request-Id` so browsers can read the correlation header. |

## Proxy hot path

```
dispatchUpstream
  EnsureRequestID(r.Context())          // parent id for whole call
  for retry := 0..MaxRetries:
      SelectProxyChannelForAttempt(...)
      dispatchSelectedUpstream(..., requestID)
          dispatchEndpointAttemptWithContinue(..., requestID)
              slog fields: request_id, retry, channel_id, ...
              writeSuccessProxyLog(..., requestID)  // proxy_logs.request_id
  on terminal MetAPI JSON error:
      writeJSONErrorWithRequest(..., requestID)
```

Rules:

- **Parent id is immutable** for the client call. Channel retries and cross-protocol endpoint fallbacks share the same id.
- **`retry` / `retry_count`** is the channel-attempt index (0 = first selected channel).
- Endpoint-protocol fallbacks inside one channel do **not** mint a new request id.
- When the response is a **relayed upstream body**, MetAPI may not rewrite the body; operators still have the response `X-Request-Id` from ingress middleware and server logs.

## Persistence

| Surface | Field | Notes |
|:--------|:------|:------|
| `proxy_logs.request_id` | TEXT NULL | Additive migration `sc2_004_proxy_logs_request_id` + base DDL for fresh installs. Index `(request_id, created_at)`. |
| `proxy.ProxyLogEntry.RequestID` | string | In-memory / writer path; filled from context if empty. |
| Admin JSON error (`handler/shared`) | `request_id` | Optional; helpers `WriteErrorWithRequestID` / `WriteErrorDetailWithRequestID`. |
| Proxy OpenAI-shaped JSON error | `error.request_id` | From `writeJSONErrorWithRequest`. |

### Operator query examples

```sql
-- All success rows for one client call (after retries landed on a healthy channel)
SELECT id, channel_id, account_id, status, http_status, retry_count, created_at
FROM proxy_logs
WHERE request_id = 'req-...'
ORDER BY created_at;

-- Grep process logs
-- request_id=req-... retry=0|1 channel_id=...
```

## Client contract

| Artifact | Value |
|:---------|:------|
| Response header | `X-Request-Id: <id>` (ingress middleware; error helpers fill if still empty) |
| MetAPI JSON error body (proxy) | `{"error":{"message":"...","type":"...","request_id":"..."}}` |
| MetAPI JSON error body (admin shared) | `{"error":"...","detail":"...","request_id":"..."}` |
| Inbound override | Client may send `X-Request-Id`; chi reuses it when present. |

## Tests

- `proxy/request_id_test.go` — context helpers preserve parent id across attempts.
- `handler/shared/errors_test.go` — `request_id` on unified admin errors.
- `handler/proxy/helpers_test.go` — OpenAI-shaped error body includes `request_id`.
- `handler/proxy/upstream_test.go` — multi-channel retry then success keeps the same `proxy_logs.request_id` with `retry_count=1`.

## Out of scope

- Full OpenTelemetry exporter / vendor UI
- Storing raw prompts/responses in external APM
- Per-attempt failure rows in `proxy_logs` (still success-centric; use slog + optional `proxy_debug_*` for deep traces)
- Frontend log browser filters (future)

## Residual risks

1. Failed attempts that only appear in slog (no `proxy_logs` row yet) still share the request id in logs, not SQL.
2. Upstream-relayed error bodies may omit `request_id` JSON field; header + logs remain authoritative.
3. Existing DBs need additive migration; fresh installs get the column from base DDL.
