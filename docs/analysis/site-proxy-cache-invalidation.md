# Site proxy cache invalidation (#216)

Last updated: 2026-07-17

## Problem

`service.InvalidateSiteProxyCache` was a no-op TODO(P8). Site create/update/delete called `InvalidateSiteCaches()`, which only partially cleared routing state via `InvalidateTokenRouterCache`, and never cleared the admin `GET /api/accounts` snapshot cache.

## Design

Hook registry in `service` (no import cycle):

- `RegisterSiteProxyCacheInvalidator(fn)`
- `InvalidateSiteProxyCache()` always calls `routing.InvalidateCache()`, then runs hooks

Admin registers in `handler/admin/accounts.go` `init()`:

```go
service.RegisterSiteProxyCacheInvalidator(func() { globalAccountsCache.clear() })
```

`InvalidateSiteCaches()` still calls both proxy + token-router invalidators (double routing clear is idempotent).

## Residual

- Multi-instance: process-local only (same as existing admin snapshot / route caches).
- No separate HTTP client proxy-config cache layer yet; when one is added, register another hook.

## Verify

```bash
go test ./service ./handler/admin -count=1 -run 'Invalidate|Cache|Site'
```

## Related: sites SELECT * drift

Shared CI Postgres may carry leftover columns (e.g. `sc1_test_probe_col`). All admin/service site loads should use `service.SiteSelectColumns` instead of `SELECT *`.
