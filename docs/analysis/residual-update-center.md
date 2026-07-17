# Residual: update-center deploy/rollback + maintenance clear-cache honesty (#197)

**Date**: 2026-07-17  
**Issue**: [#197](https://github.com/TokenDanceLab/metapi-go/issues/197)  
**Lane**: residual honesty (admin ops)

## Goal

Stop success-theater for operations that are not implemented in-process:

| Endpoint | Previous | New |
|----------|----------|-----|
| `POST /api/update-center/deploy` | `202` + fake `task.id: "stub-deploy"` | **501** residual (`success:false`, no task id) after validation |
| `POST /api/update-center/rollback` | `202` + fake `task.id: "stub-rollback"` | **501** residual after validation |
| `GET /api/update-center/tasks/:id/stream` | SSE with immediate fake `done/stub` | **501** residual (no fake SSE) |
| `POST /api/settings/maintenance/clear-cache` | deleted rows + `jobId: "stub-clear-cache"` | delete rows + **real** in-process invalidation + **real** background task id |

## Safe local clear-cache behavior

1. Count + `DELETE` from shared tables:
   - `model_availability`
   - `route_channels`
   - `token_routes`
2. Invalidate **this process** caches immediately:
   - `routing.InvalidateCache()` (route match / stable-first caches)
   - `globalAccountsCache.clear()` (admin accounts list snapshot)
3. Queue a real in-memory background task (`maintenance-clear-cache`) that:
   - runs `service.RebuildRoutesBestEffort()`
   - re-invalidates local caches after rebuild
4. Return `202` with the real `jobId` / `taskId` (never `stub-clear-cache`)

After wiping availability tables, rebuild is intentionally best-effort and may insert few/no channels until models are re-probed. That is still honest work, not a fake job id.

## Deploy / rollback residual (out of scope)

Remote binary/Helm deploy and release rollback require a helper service, credentials, and host orchestration. That is **not** implemented in the Go runtime.

Honest residual shape (same helper as `#185` test routes):

```json
{
  "success": false,
  "message": "Update-center deploy is not implemented in Go",
  "residual": "remote binary/Helm deploy via helper service is out of scope; use external deploy tooling (CI/CD or helper) instead of inventing task ids"
}
```

Rules:

- Never return `success:true` for unimplemented deploy/rollback.
- Never invent `stub-deploy` / `stub-rollback` task ids.
- Validation still runs first (`targetTag` / `targetRevision` required → `400`).

## Multi-instance residual

| Surface | Shared across instances? | Notes |
|---------|--------------------------|-------|
| DB row deletes (availability / routes / channels / usage) | **Yes** (shared DB) | All instances see empty tables |
| `routing` in-process cache | **No** | Only the receiving process invalidates |
| accounts snapshot cache | **No** | Only the receiving process clears |
| background task registry (`jobId`) | **No** | Process-local; peer `/api/tasks/:id` will 404 |
| remote deploy/rollback | **N/A** | Residual 501 everywhere until helper is wired |

Operators running multi-instance should either:

1. call clear-cache on each instance (or restart pods), and/or  
2. rely on route-cache TTL + account-snapshot TTL for natural expiry.

## Tests

```bash
go test ./handler/admin -count=1 -run 'Update|Maintenance|Cache'
```

Coverage:

- deploy/rollback validation `400`
- deploy/rollback/stream honest `501` without stub task ids
- clear-cache wipes seeded `token_routes`, clears accounts + route caches, returns real `jobId`, task reaches `succeeded`

## Files

- `handler/admin/update_center.go`
- `handler/admin/settings_maintenance.go`
- `handler/admin/update_center_test.go`
- `handler/admin/settings_maintenance_test.go`
- `docs/analysis/residual-update-center.md`
