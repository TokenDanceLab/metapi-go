# Site proxy cache invalidation (#216)

**Date**: 2026-07-17  
**Issue**: [#216](https://github.com/TokenDanceLab/metapi-go/issues/216)  
**SSOT code**: `service/site_service.go`, `handler/admin/accounts.go`, `handler/admin/sites.go`, `routing/cache.go`

## Problem

`service.InvalidateSiteProxyCache` was a `TODO(P8)` no-op. Site create/update/delete
paths already called `service.InvalidateSiteCaches()` (or only
`routing.InvalidateCache()`), so:

1. Token-router invalidation was partial / duplicated depending on the path.
2. The admin `GET /api/accounts` snapshot (`globalAccountsCache`) was **not**
   cleared on site mutations, so the snapshot could keep serving stale site rows
   for up to the 30s TTL.
3. Wiring admin → service → admin would create an import cycle if service called
   into `handler/admin` directly.

## Design

Prefer a small **hook registry** in `service` (no new package required):

| API | Role |
|-----|------|
| `RegisterSiteProxyCacheInvalidator(fn)` | Append optional side-effect hook |
| `InvalidateSiteProxyCache()` | Run hooks, then always `routing.InvalidateCache()` |
| `InvalidateTokenRouterCache()` | Thin alias of `routing.InvalidateCache()` |
| `InvalidateSiteCaches()` | `InvalidateSiteProxyCache` + `InvalidateTokenRouterCache` (idempotent) |

`handler/admin` registers a once-only hook (package `init` +
`RegisterAccountsRoutes`) that clears `globalAccountsCache`. Service never
imports admin; admin already imports service.

```text
sites handler ──► service.InvalidateSiteCaches()
                        │
                        ├─ registered hooks (admin accounts snapshot clear)
                        └─ routing.InvalidateCache()
```

## Call sites

| Path | Before | After |
|------|--------|-------|
| `POST /api/sites` | `routing.InvalidateCache` only | `service.InvalidateSiteCaches` |
| `PUT /api/sites/{id}` | `InvalidateSiteCaches` only on status change; always `routing.InvalidateCache` | Always `InvalidateSiteCaches` after successful update |
| `DELETE /api/sites/{id}` | both (duplicate routing) | `InvalidateSiteCaches` only |
| batch site mutations | both (duplicate routing) | `InvalidateSiteCaches` only |
| account mutations | direct `globalAccountsCache.clear` + `routing.InvalidateCache` | unchanged (still local clear) |

## Why not move the snapshot into `service`?

The accounts list response is an HTTP-layer JSON snapshot (includes sites +
generatedAt). Keeping it package-private in admin matches existing maintenance
clear-cache code and avoids bloating service with handler response shapes.

## Verification

```bash
go test ./service ./handler/admin ./routing -count=1 -run 'Cache|Invalidate|Site'
```

## Follow-ups

- If a dedicated site-proxy HTTP client cache appears later, register another
  hook (or fold into the same registry) instead of reopening package boundaries.
- Account mutation paths could optionally call `InvalidateSiteCaches` for one
  entrypoint, but that is out of scope for #216.
