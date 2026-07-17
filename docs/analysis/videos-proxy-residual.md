# Residual: Videos proxy publicId mapping (#225 / #235)

**Date**: 2026-07-17  
**Issues**: [#225](https://github.com/TokenDanceLab/metapi-go/issues/225) (GET/DELETE passthrough), [#235](https://github.com/TokenDanceLab/metapi-go/issues/235) (create mapping)  
**Lane**: p87 / videos create rewrite  
**SSOT code**: `handler/proxy/videos.go`, success hook in `handler/proxy/upstream.go`

## Goal

1. **#225** — Stop store-gated theater on video GET/DELETE (missing map ≠ hard 404).
2. **#235** — On successful create, save process-local `ProxyVideoTask` and rewrite response `id` to opaque `publicId`.

## Behavior after #235

| Surface | Behavior |
|---------|----------|
| `POST /v1/videos` | `PrepareCtx` + `dispatchUpstream`. On **non-stream buffered 2xx**, parse upstream JSON `id`, generate `video_<hex>`, `SaveProxyVideoTask`, rewrite body `id` → publicId. Parse miss / non-JSON → body unchanged (best-effort). |
| `GET /v1/videos/{id}` | Always dispatch. If local mapping exists and `UpstreamVideoID` differs from `{id}`, path uses `UpstreamVideoID`; otherwise `{id}` passthrough. **Missing mapping is not a 404.** |
| `DELETE /v1/videos/{id}` | Delete local mapping if present. Always dispatch DELETE with same path rewrite rule. Prefer upstream status over local-only 204. |

### Create rewrite rule

```
on success POST /v1/videos (path equal, ignore trailing slash):
  upstreamID = JSON.body.id (string, non-empty)
  publicID   = "video_" + hex(16 random bytes)
  SaveProxyVideoTask({
    PublicID, UpstreamVideoID: upstreamID,
    SiteURL, TokenValue, RequestedModel, ActualModel,
    ChannelID, AccountID
  })
  body.id = publicID
```

### Path rewrite rule (GET/DELETE)

```
upstreamPathID = publicID
if mapping[publicID] exists
  and UpstreamVideoID != ""
  and UpstreamVideoID != publicID:
    upstreamPathID = UpstreamVideoID

DownstreamPath = "/v1/videos/" + upstreamPathID
```

## What is still residual (not claimed)

1. **In-memory process-local store only** — no Redis / DB multi-instance video task store. Schema table `proxy_video_tasks` exists for future durability but is **not** written this wave.
2. **No sticky site/token restore from mapping** — even when a mapping row has `SiteURL` / `TokenValue` / channel ids, GET/DELETE still go through normal channel selection (`PrepareCtx` + router). Sticky pin is future work.
3. **Stub mode** — when upstream is unconfigured and proxy stub is enabled, create returns generic stub JSON without mapping rewrite (no selected channel). Production without upstream config still returns 503 from `dispatchUpstream`.
4. **Response fields other than `id`** — only top-level `id` is rewritten; nested ids (if any) are left as upstream sent them.

## TS parity note

TS MetAPI saves mapping on create and rewrites `id` in the create response, then uses the mapping on GET/DELETE. Go now matches the create-side process-local mapping + id rewrite path; durable multi-instance store and sticky pin remain residual (same honesty bar as sticky/admin tasks).

## Tests

| Case | Expectation |
|------|-------------|
| Create with mock upstream | 200; body `id` is `video_*`; raw upstream id not present; mapping saved |
| Multipart create forwards form | same rewrite + mapping |
| GET with mapping | path may use UpstreamVideoID; no hard 404 |
| GET without mapping | not hard 404; stub/upstream proceeds |
| Non-videos success body | rewrite helper no-ops |

Verify:

```bash
go test ./handler/proxy -count=1 -run 'Video|Videos'
```
