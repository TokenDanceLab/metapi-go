# Route Decision Admin APIs (#171)

**Date**: 2026-07-17  
**Issue**: [#171](https://github.com/TokenDanceLab/metapi-go/issues/171)  
**Lane**: `lane:decision`

## Summary

Wire `handler/admin/token_routes.go` route-decision endpoints to the real routing
engine instead of empty maps / `stub-decision-refresh`.

| Endpoint | Behavior |
|----------|----------|
| `GET /api/routes/decision?model=` | `TokenRouter.ExplainSelection` → `{ success, decision }` with candidates/strategy summary |
| `POST /api/routes/decision/batch` | Bounded model list (max 500, deduped) → `{ success, decisions: { [model]: decision } }` |
| `POST /api/routes/decision/by-route/batch` | Bounded `items[{routeId,model}]` (max 500) → nested `{ [routeId]: { [model]: decision } }` |
| `POST /api/routes/decision/route-wide/batch` | Bounded `routeIds` (max 500, deduped) → `{ [routeId]: decision }` |
| `POST /api/routes/decision/refresh` | Background task via `StartBackgroundTask` that calls `RouteDecisionService.RefreshAllRouteDecisionSnapshots`; returns real `jobId`/`taskId`, never `stub-decision-refresh` |

## Wiring

1. `app.ConfigureProxyUpstream` builds one `routing.TokenRouter` +
   `routing.RouteDecisionService` on the proxy routing store and publishes them
   via `app.TokenRouteDecisionRuntime()`.
2. `router.New` registers `admin.RegisterTokenRoutesWithDeps` with deps from
   `app.TokenRouteDecisionRuntime()` (router package bridges to avoid import cycles).
3. When upstream is not configured, deps are nil and decision endpoints return
   **503** with a clear Chinese message (`路由决策引擎未配置`) — no silent empty success.
4. `DecisionDB.FindAllEnabledRoutes` reuses `[]store.TokenRoute` so the same
   `proxyRoutingStore` adapter satisfies both selector + snapshot refresh.

## Honest limits

1. Live explain endpoints do not currently persist snapshots unless the refresh
   job runs (`persistSnapshots` request flags are accepted for API compatibility
   but not yet applied on batch paths).
2. `refreshPricingCatalog` is accepted and forwarded into the refresh service
   signature; pricing catalog refresh itself remains a no-op inside
   `RouteDecisionService` today.
3. Background task registry is process-local (same as other admin tasks).

## Verify

```bash
go test ./handler/admin ./routing -count=1 -run Decision
```
