# Residual: update-center honesty (admin + scheduler)

**Date**: 2026-07-17  
**Issues**: [#197](https://github.com/TokenDanceLab/metapi-go/issues/197), [#283](https://github.com/TokenDanceLab/metapi-go/issues/283)  
**Lane**: residual honesty (admin ops + scheduler scaffolding)  
**Scope**: honest 501 / local stubs only. **No remote registry product.**  
**User decision 2026-07-20 (UC-1)**: **hide/external** — Settings UI is an ops note + GHCR/Releases links; no in-app deploy controls.

## Goal

Stop success-theater for operations that are not implemented in-process, and keep status/check from inventing `updateAvailable=true` without a real helper/registry client.

## Current endpoint table (#283)

| Method | Endpoint | Status | Behavior | Invents? |
|--------|----------|--------|----------|----------|
| `GET` | `/api/update-center/status` | **200** | Local stub: `mode=external`, versions `0.0.0`, `updateAvailable=false`, `lastCheckedAt=null`, plus `residual` string | **No** remote poll, **no** fake available update |
| `POST` | `/api/update-center/check` | **200** | Same payload as status (local-only; no remote registry check) | **No** remote poll, **no** fake available update |
| `PUT` | `/api/update-center/config` | **200** | Echo request body for UI round-trip; `residual` notes echo-only (not a durable product config store) | **No** deploy side effects |
| `POST` | `/api/update-center/deploy` | **400** / **501** | Validates `targetTag` (or `targetVersion` alias); then honest residual | **No** `stub-deploy` task id |
| `POST` | `/api/update-center/rollback` | **400** / **501** | Validates `targetRevision`; then honest residual | **No** `stub-rollback` task id |
| `GET` | `/api/update-center/tasks/:id/stream` | **501** | No in-process update-center task registry / SSE log stream | **No** fake SSE `done/stub` |

Related (same residual wave historically, implemented work — not residual):

| Method | Endpoint | Status | Behavior |
|--------|----------|--------|----------|
| `POST` | `/api/settings/maintenance/clear-cache` | **202** | Real DB wipe + process-local cache invalidation + real background rebuild `jobId` (never `stub-clear-cache`) |

## Previous success-theater → current honesty (#197)

| Endpoint | Previous | New |
|----------|----------|-----|
| `POST /api/update-center/deploy` | `202` + fake `task.id: "stub-deploy"` | **501** residual (`success:false`, no task id) after validation |
| `POST /api/update-center/rollback` | `202` + fake `task.id: "stub-rollback"` | **501** residual after validation |
| `GET /api/update-center/tasks/:id/stream` | SSE with immediate fake `done/stub` | **501** residual (no fake SSE) |
| `POST /api/settings/maintenance/clear-cache` | deleted rows + `jobId: "stub-clear-cache"` | delete rows + **real** in-process invalidation + **real** background task id |

## Status / check payload (local stub)

```json
{
  "currentVersion": "0.0.0",
  "latestVersion": "0.0.0",
  "updateAvailable": false,
  "lastCheckedAt": null,
  "residual": "external deploy only; no remote registry/helper polling or in-app version discovery",
  "mode": "external"
}
```

Rules:

- Never invent `updateAvailable=true` without a real remote registry/helper client.
- Never invent a non-null `lastCheckedAt` from a fake poll.
- `0.0.0` is an explicit local placeholder, not a discovered latest release.

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

## Scheduler: log-only residual (#246 / #283)

`scheduler/update_center.go` (`UpdateCenterScheduler`):

| Aspect | Honest status |
|--------|---------------|
| Interval | 15m (min 10s) |
| What runs | Ticker + process-local in-flight guard + `runWithSchedulerLease` + **one info log line** |
| What does **not** run | Remote registry/helper HTTP, version compare, `lastCheckedAt` persistence, deploy/rollback, SSE task completion |
| Product effect | **None** — residual scaffolding only |
| Multi-instance | Lease serializes the residual log; no cluster-wide "last checked" state |

Log language is intentionally residual (`no remote polling; no version discovery; no updateAvailable invention`), not "update check success".

Do **not** add fake polling stubs that invent `updateAvailable=true`. Wire real helper/registry client only when product path exists.

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

## Multi-instance residual

| Surface | Shared across instances? | Notes |
|---------|--------------------------|-------|
| DB row deletes (availability / routes / channels / usage) | **Yes** (shared DB) | All instances see empty tables |
| `routing` in-process cache | **No** | Only the receiving process invalidates |
| accounts snapshot cache | **No** | Only the receiving process clears |
| background task registry (`jobId`) | **No** | Process-local; peer `/api/tasks/:id` will 404 |
| update-center status/check | **N/A** | Local stub; no shared last-checked state |
| remote deploy/rollback / task SSE | **N/A** | Residual 501 everywhere until helper is wired |
| update-center scheduler | Lease only | Serializes residual log; no product state |

Operators running multi-instance should either:

1. call clear-cache on each instance (or restart pods), and/or  
2. rely on route-cache TTL + account-snapshot TTL for natural expiry.

## Explicit non-goals (#283)

- Remote update-center helper client / registry polling product
- Inventing `updateAvailable=true` or fake `lastCheckedAt`
- Fake deploy/rollback task ids or SSE completion
- Durable update-center config store from `PUT /config`
- Multi-instance durable deploy job bus

## Tests

```bash
go test ./handler/admin ./scheduler -count=1 -run 'Update|update'
```

Coverage:

- status/check local stub: `updateAvailable=false`, residual field present, no invented available update
- deploy/rollback validation `400`
- deploy/rollback/stream honest `501` without stub task ids
- clear-cache (related) wipes seeded `token_routes`, clears accounts + route caches, returns real `jobId`, task reaches `succeeded`
- scheduler construct/stop residual ticker

## Files

- `handler/admin/update_center.go`
- `handler/admin/update_center_test.go`
- `handler/admin/settings_maintenance.go`
- `handler/admin/settings_maintenance_test.go`
- `scheduler/update_center.go`
- `docs/analysis/residual-update-center.md`
- `docs/analysis/scheduler-residual-todos.md` (update-center inventory row)
