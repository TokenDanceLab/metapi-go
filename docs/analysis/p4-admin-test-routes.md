# P4 admin test routes (#185 / #291)

**Date:** 2026-07-17  
**Issues:** [#185](https://github.com/TokenDanceLab/metapi-go/issues/185), [#291](https://github.com/TokenDanceLab/metapi-go/issues/291)  
**Peers:** [#119](https://github.com/TokenDanceLab/metapi-go/issues/119) forced-channel harness  
**Code:** `handler/admin/test.go`, reuses `handler/admin/channel_test_harness.go`  
**Related residual docs:** `docs/analysis/admin-channel-test-harness.md`, `docs/analysis/ops-admin-stubs.md`

## Goal

Replace `handler/admin/test.go` fake `success:true` / stub-job responses with either:

1. the real forced-channel harness path, or
2. an honest **501** residual (never `success:true` with a not-implemented message).

#291 is a residual honesty pass only: keep stream/job surfaces residual, strengthen residual wording, and keep docs accurate. **Do not invent working SSE streams or job queues.**

## Current endpoint table (#291)

| Method | Path | Status | Behavior | Invents? |
|--------|------|--------|----------|----------|
| POST | `/api/test/proxy` | **200** / **400** / **404** / **501** | With `channelId` / `forcedChannelId` / `siteId` → forced-channel harness. Without forced id → **501** residual (full path/multipart matrix not ported). Bad JSON → **400**. Missing channel → **404**. | **No** fake probe success without a forced channel |
| POST | `/api/test/chat` | **200** / **400** / **404** / **501** | Same alias as proxy when a forced channel/site is provided. | **No** fake chat success without a forced channel |
| POST | `/api/test/proxy/stream` | **501** | SSE proxy stream matrix residual | **No** fake SSE chunks / stream success theater |
| POST | `/api/test/chat/stream` | **501** | SSE chat stream residual | **No** fake SSE chunks / stream success theater |
| POST | `/api/test/proxy/jobs` | **501** | Async proxy job queue residual | **No** `stub-job` / fake queued success |
| POST | `/api/test/chat/jobs` | **501** | Async chat job queue residual | **No** `stub-job` / fake queued success |
| GET | `/api/test/proxy/jobs/{jobId}` | **404** | No in-process job registry | **No** fake completed job |
| DELETE | `/api/test/proxy/jobs/{jobId}` | **404** | No in-process job registry | **No** fake cancel success |
| GET | `/api/test/chat/jobs/{jobId}` | **404** | No in-process job registry | **No** fake completed job |
| DELETE | `/api/test/chat/jobs/{jobId}` | **404** | No in-process job registry | **No** fake cancel success |

Canonical forced probe (implemented, not residual):

| Method | Path | Status | Behavior |
|--------|------|--------|----------|
| POST | `/api/admin/test-channel` | **200** / **400** / **404** | Forced-channel harness (`channel_test_harness.go`) |
| POST | `/api/debug/channel-probe` | **200** / **400** / **404** | Alias of the same harness |

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

404 job body shape (no registry):

```json
{
  "success": false,
  "error": {
    "message": "job not found",
    "type": "not_found"
  },
  "residual": "no in-process /api/test/{proxy|chat} job queue or job registry; POST jobs returns 501; use sync POST /api/test/{proxy|chat} or POST /api/admin/test-channel"
}
```

Rules:

- Never return HTTP 200 with `success:true` for unimplemented work.
- Never invent `jobId: "stub-job"`.
- Never invent fake SSE stream chunks or async job completion theater.
- Prefer pointing operators at `POST /api/admin/test-channel` for forced probes.
- Job GET/DELETE stay **404** (empty registry), not **200** success or fake cancel.

## Router wiring

`RegisterTestRoutes(r, db, cfg)` requires DB + config so aliases can construct the shared harness handler. Called from `router/router.go` inside the admin-auth + DB-ready group.

## Tests

`handler/admin/test_routes_test.go`:

- route mount + invalid JSON → 400
- proxy/chat without forced id → 501 + residual, `success:false`
- forced channel / siteId → real harness probe (transport stub)
- stream + job create → 501; job get/delete → 404 without fake success
- residual field present on 501 and job 404 surfaces

```bash
go test ./handler/admin -count=1 -run 'TestTest|TestRegisterTest|TestMapFlexible|TestChannelTest'
```
