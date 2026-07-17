# Residual: Videos GET/DELETE honest passthrough (#225)

**Date**: 2026-07-17  
**Issue**: [#225](https://github.com/TokenDanceLab/metapi-go/issues/225)  
**Lane**: p86 / honest residual  
**SSOT code**: `handler/proxy/videos.go`

## Goal

Stop store-gated theater on video GET/DELETE:

- `HandleVideosCreate` already dispatches upstream but **does not** save
  `ProxyVideoTask` or rewrite the response `id` to a publicId.
- Clients that hold a real upstream video id were still hard-404'd with
  `"Video task not found"` solely because the in-memory map was empty.
- Prefer honest upstream passthrough over local-only mapping theater.
- Full multi-instance durable video store is **out of scope**.

## Behavior after #225

| Surface | Behavior |
|---------|----------|
| `POST /v1/videos` | `PrepareCtx` + `dispatchUpstream` (unchanged). No mapping save / publicId rewrite yet. |
| `GET /v1/videos/{id}` | Always `PrepareCtx` + `dispatchUpstream`. If local mapping exists and `UpstreamVideoID` differs from `{id}`, path uses `UpstreamVideoID`; otherwise `{id}` is passed through. **Missing mapping is not a 404.** |
| `DELETE /v1/videos/{id}` | Delete local mapping if present (idempotent). Always `PrepareCtx` + `dispatchUpstream` DELETE with the same path rewrite rule as GET. Prefer upstream status over a local-only 204 residual. |

### Path rewrite rule

```
upstreamPathID = publicID
if mapping[publicID] exists
  and UpstreamVideoID != ""
  and UpstreamVideoID != publicID:
    upstreamPathID = UpstreamVideoID

DownstreamPath = "/v1/videos/" + upstreamPathID
```

## What is still residual (not claimed)

1. **Create does not save `ProxyVideoTask`** — response `id` is whatever upstream
   returns (or stub). No publicId generation/rewrite on the write path.
2. **In-memory process-local store only** — no Redis / DB multi-instance video
   task store. Mapping is an optional rewrite aid when something else seeds it
   (tests, future create wiring).
3. **No sticky site/token restore from mapping** — even when a mapping row has
   `SiteURL` / `TokenValue` / channel ids, GET/DELETE still go through normal
   channel selection (`PrepareCtx` + router). Sticky pin is future work.
4. **Stub mode** — when upstream is unconfigured and proxy stub is enabled,
   GET/DELETE return the generic stub JSON (non-404), same as other surfaces.
   Production without upstream config still returns 503 from `dispatchUpstream`.

## TS parity note

TS MetAPI saves mapping on create and rewrites `id` in the create response, then
uses the mapping on GET/DELETE. Go currently only has the optional mapping
helpers + passthrough path rewrite. Closing the create-side mapping + id rewrite
is a separate follow-up, not required for honest GET/DELETE.

## Tests

| Case | Expectation |
|------|-------------|
| GET with mapping | 200 stub (or upstream); path may use UpstreamVideoID |
| GET without mapping (auth ok) | **not** hard 404; stub/upstream path proceeds |
| DELETE with mapping | local mapping cleared; dispatchUpstream (stub non-404) |
| DELETE without mapping | **not** hard 404; dispatchUpstream proceeds |
| Create model required / multipart | unchanged |

Verify:

```bash
go test ./handler/proxy -count=1 -run Video
```
