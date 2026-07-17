# Admin channel test harness (#119)

**Date:** 2026-07-17  
**Backlog:** [#119](https://github.com/TokenDanceLab/metapi-go/issues/119) · competitive learn L10  
**Peers:** all-api-hub multi-dimensional API verification (in-page, not browser extension)  
**Code:** `handler/admin/channel_test_harness.go`

## Goal

Give operators a **one-click, admin-auth** way to force a specific channel/site and verify that a model/key actually works — returning status, latency, and a **safe truncated** body/error summary. No browser extension, no end-user unauthenticated console, no storage of production prompt corpora.

## API

Both paths share one handler (admin auth via `/api/*` group):

| Method | Path | Notes |
|--------|------|-------|
| POST | `/api/admin/test-channel` | Canonical admin path |
| POST | `/api/debug/channel-probe` | Alias under debug namespace |

### Request body

```json
{
  "channelId": 12,
  "siteId": 3,
  "model": "gpt-4o-mini",
  "prompt": "ping",
  "mode": "chat",
  "timeoutMs": 15000
}
```

| Field | Required | Default | Notes |
|-------|----------|---------|-------|
| `channelId` | one of channel/site | — | Forces this `route_channels` row (preferred). |
| `siteId` | one of channel/site | — | Picks best enabled channel on the site (model match preferred). |
| `model` | no | channel `source_model` or `gpt-4o-mini` | Upstream model name. |
| `prompt` | no | `ping` | Chat user content only; max 256 runes; **not stored**. |
| `mode` | no | `chat` | `chat` → `POST /v1/chat/completions`; `models` → `GET /v1/models`. |
| `timeoutMs` | no | `15000` | Clamped to **[1000, 60000]**. |

### Response shape

Always HTTP **200** for a completed probe attempt (including upstream 4xx/5xx/timeouts). Validation/auth/not-found use normal admin error status codes (400/401/404).

```json
{
  "success": true,
  "statusCode": 200,
  "latencyMs": 142,
  "truncatedBody": "{\"id\":\"chatcmpl-…\",\"choices\":[…]}",
  "error": null,
  "channelId": 12,
  "routeId": 4,
  "siteId": 3,
  "siteName": "Relay-A",
  "accountId": 9,
  "model": "gpt-4o-mini",
  "mode": "chat",
  "upstreamPath": "/v1/chat/completions",
  "upstreamHost": "relay.example.com",
  "bodyTruncated": false
}
```

| Field | Meaning |
|-------|---------|
| `success` | Upstream HTTP status in **2xx**. |
| `statusCode` | Upstream status (0 on transport error). |
| `latencyMs` | Wall time for the forced attempt. |
| `truncatedBody` | Response body capped ~**2 KiB**, secret-like substrings redacted. |
| `error` | Safe summary when not successful (timeout / status / transport). |
| `bodyTruncated` | True when the body was cut for size. |
| `upstreamHost` | Host only (no credentials). |

## Forced channel semantics

1. Load channel → account → site (and optional `account_tokens` when `token_id` set).
2. Resolve credential like routing (token row → oauth access token → api_token → access_token fallback for harness).
3. Build **one** upstream request to that site URL (via `proxy.BuildUpstreamURL`).
4. **Do not** call weighted selection / failover / sticky / multi-protocol fallback.
5. Apply site custom headers / account-or-site proxy via `service.BuildPlatformProxyConfig` (Authorization never overwritten by site headers).
6. Return status/latency/truncated body; **do not** mutate channel cooldown or mark keys expired.

## Safety

| Rule | Implementation |
|------|----------------|
| Admin-only | Registered under `auth.AdminAuth` group in `router/router.go` |
| No full prompt storage | Prompt is request-scoped only; never written to DB/logs as corpus |
| Body cap | Read at most ~2 KiB; mark `bodyTruncated` |
| Secret redaction | `sk-…`, `Bearer …`, `api_key=…` patterns replaced with `[redacted]` |
| No auth header echo | Response does not include request Authorization |
| Timeout floor/ceiling | 1s–60s |

## Related `/api/test/*` aliases (#185 / #291)

Sync admin test routes reuse this harness when a channel/site is forced. Stream and async job surfaces stay residual.

| Method | Path | Status | Behavior | Invents? |
|--------|------|--------|----------|----------|
| POST | `/api/test/proxy` | **200** / **400** / **404** / **501** | Forced id → harness; else **501** residual matrix | **No** fake probe without forced channel |
| POST | `/api/test/chat` | **200** / **400** / **404** / **501** | Same as proxy | **No** fake chat success without forced channel |
| POST | `/api/test/proxy/stream` | **501** | SSE matrix residual | **No** fake stream chunks |
| POST | `/api/test/chat/stream` | **501** | SSE residual | **No** fake stream chunks |
| POST | `/api/test/proxy/jobs` | **501** | Async job queue residual | **No** `stub-job` |
| POST | `/api/test/chat/jobs` | **501** | Async job queue residual | **No** `stub-job` |
| GET/DELETE | `/api/test/{proxy,chat}/jobs/{jobId}` | **404** | No in-process job registry | **No** fake complete/cancel |

Details: `docs/analysis/p4-admin-test-routes.md` · code: `handler/admin/test.go`.

## Out of scope

- End-user unauthenticated console
- Multi-model matrix fan-out UI (can call this API repeatedly)
- Browser extension permissions / all-api-hub client shape
- Background health probing (see #114 / `scheduler/model_probe.go`)
- Large SPA redesign (optional thin button is a follow-up)
- Working `/api/test/*/stream` SSE matrix or `/api/test/*/jobs` async queues (honest 501/404 residuals only; see #291)

## Tests

`handler/admin/channel_test_harness_test.go`:

- validation (missing ids, bad mode, bad JSON)
- channel not found → 404
- forced channel chat success via stub `RoundTripper`
- debug alias + models mode
- siteId resolution to enabled channel
- upstream 401 + oversized body → truncated + redacted
- transport timeout → safe error summary

## Operator usage

```bash
curl -sS -X POST "http://localhost:4000/api/admin/test-channel" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"channelId":12,"model":"gpt-4o-mini","prompt":"ping"}'
```
