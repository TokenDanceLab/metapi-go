# P4 admin test routes (#185)

**Date:** 2026-07-17  
**Issue:** [#185](https://github.com/TokenDanceLab/metapi-go/issues/185)  
**Peers:** [#119](https://github.com/TokenDanceLab/metapi-go/issues/119) forced-channel harness  
**Code:** `handler/admin/test.go`, reuses `handler/admin/channel_test_harness.go`

## Goal

Replace `handler/admin/test.go` fake `success:true` / stub-job responses with either:

1. the real forced-channel harness path, or
2. an honest **501** residual (never `success:true` with a not-implemented message).

## Behavior matrix

| Method | Path | Behavior |
|--------|------|----------|
| POST | `/api/test/proxy` | When `channelId` / `forcedChannelId` / `siteId` present → delegate to harness (`runChannelTest`). Otherwise **501** residual (full path/multipart matrix not ported). |
| POST | `/api/test/chat` | Same alias as proxy when a forced channel/site is provided. |
| POST | `/api/test/proxy/stream` | **501** residual (SSE matrix). |
| POST | `/api/test/chat/stream` | **501** residual (SSE). |
| POST | `/api/test/proxy/jobs` | **501** residual (no async job queue). |
| POST | `/api/test/chat/jobs` | **501** residual (no async job queue). |
| GET/DELETE | `/api/test/{proxy,chat}/jobs/{jobId}` | **404** `job not found` (no registry; no stub-job invent). |

## Request mapping

Frontend envelopes (`ProxyTestRequestEnvelope`, chat payloads) are accepted and projected onto harness fields:

| Input | Harness field |
|-------|---------------|
| `channelId` or `forcedChannelId` | `channelId` (first positive) |
| `siteId` | `siteId` |
| top-level `model` or `jsonBody.model` | `model` |
| top-level `prompt`, last user `messages[]`, or nested `jsonBody.messages/prompt/input` | `prompt` |
| `mode`, else path containing `/models` | `mode` (`chat` default) |
| `timeoutMs` | `timeoutMs` (clamped by harness) |

Probe execution, redaction, timeout clamps, and token resolution are **not duplicated** — they live in `channelTestHandler.runChannelTest` / `executeProbe`.

## Residual honesty

501 body shape:

```json
{
  "success": false,
  "message": "… is not implemented in Go",
  "residual": "… what is missing and which endpoint to use instead"
}
```

Rules:

- Never return HTTP 200 with `success:true` for unimplemented work.
- Never invent `jobId: "stub-job"`.
- Prefer pointing operators at `POST /api/admin/test-channel` for forced probes.

## Router wiring

`RegisterTestRoutes(r, db, cfg)` now requires DB + config so aliases can construct the shared harness handler. Called from `router/router.go` inside the admin-auth + DB-ready group.

## Tests

`handler/admin/test_routes_test.go`:

- route mount + invalid JSON → 400
- proxy/chat without forced id → 501 + residual, `success:false`
- forced channel / siteId → real harness probe (transport stub)
- stream + job create → 501; job get/delete → 404 without fake success

```bash
go test ./handler/admin -count=1 -run 'TestTest|TestRegisterTest|TestMapFlexible|TestChannelTest'
```
