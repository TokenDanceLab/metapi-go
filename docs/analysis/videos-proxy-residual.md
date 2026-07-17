# Residual: Videos proxy publicId mapping (#225 / #235 / #244)

**Date**: 2026-07-17  
**Issues**: [#225](https://github.com/TokenDanceLab/metapi-go/issues/225) (GET/DELETE passthrough), [#235](https://github.com/TokenDanceLab/metapi-go/issues/235) (create rewrite), [#244](https://github.com/TokenDanceLab/metapi-go/issues/244) (durable store)  
**Lane**: p88 / videos durable mapping  
**SSOT code**: `handler/proxy/videos.go`, success hook in `handler/proxy/upstream.go`

## Goal

1. **#225** — Stop store-gated theater on video GET/DELETE (missing map ≠ hard 404).
2. **#235** — On successful create, generate opaque `publicId`, rewrite response `id`, save mapping.
3. **#244** — Dual-write mapping to `proxy_video_tasks` so multi-instance / restart can resolve publicId.

## Behavior after #244

| Surface | Behavior |
|---------|----------|
| `POST /v1/videos` | On **non-stream buffered 2xx**, parse upstream JSON `id`, generate `video_<hex>`, rewrite body `id`, `SaveProxyVideoTask` → memory + `INSERT … ON CONFLICT(public_id) DO UPDATE` when `store.GetDB()` set. |
| `GET /v1/videos/{id}` | Resolve via memory, then durable `proxy_video_tasks` cold load. Path rewrite if UpstreamVideoID differs. Missing map → passthrough (no hard 404). |
| `DELETE /v1/videos/{id}` | Clear memory + `DELETE FROM proxy_video_tasks`, then always dispatch DELETE. |

### Storage layers

```
Save:  memory map  +  best-effort DB upsert (log warn on DB failure; memory still set)
Get:   memory hit  → return; miss → SELECT by public_id → warm memory
Delete: memory delete + best-effort DB delete
```

When DB is nil (unit tests without OverrideDB / unbootstrapped process), behavior degrades to process-local memory only (#235).

### Create rewrite rule

```
on success POST /v1/videos:
  upstreamID = JSON.body.id
  publicID   = "video_" + hex(16 random bytes)
  SaveProxyVideoTask({PublicID, UpstreamVideoID, SiteURL, TokenValue, models, channel, account})
  body.id = publicID
```

## Sticky pin (#253)

When a mapping has `ChannelID > 0`, GET/DELETE set `Ctx.ForcedChannelID` so
`SelectProxyChannelForAttempt` uses `SelectPreferredChannel` (no cross-channel
fallback). `RequestedModel` is seeded from the mapping when the client omits
`model` (typical for GET/DELETE). Path rewrite still uses `UpstreamVideoID`.

`SiteURL` / `TokenValue` on the mapping remain informational — selection still
goes through the router preferred-channel path (token/site come from channel
resolution), not a raw URL override.

## What is still residual (not claimed)

1. **Raw site URL / token override** — mapping stores SiteURL/TokenValue but does not bypass router with a direct HTTP client pin.
2. **Stub mode** — unconfigured upstream + stub does not rewrite publicId (no selected channel).
3. **TTL / GC** — no retention job for stale `proxy_video_tasks` rows this wave (see `videos-proxy-retention-residual.md`).
4. **TokenValue in DB** — same sensitivity as process memory mapping; operators must protect DB backups.

## Tests

| Case | Expectation |
|------|-------------|
| Create with mock upstream | rewritten publicId + mapping |
| DB durable round-trip | save → clear memory → get loads from DB |
| GET without mapping | not hard 404 |
| Non-videos success body | rewrite helper no-ops |

```bash
go test ./handler/proxy -count=1 -run 'Video|Videos|ProxyVideo'
```
