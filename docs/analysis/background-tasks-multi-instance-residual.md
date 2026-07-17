# Residual: multi-instance admin background task registry (#236)

**Date**: 2026-07-17  
**Issue**: [#236](https://github.com/TokenDanceLab/metapi-go/issues/236)  
**Lane**: residual admin task honesty

## Scope (this wave)

Honest documentation + code comments for the existing process-local admin task
registry. **No** Redis/DB job store, **no** cross-instance coordination, **no**
API shape change.

## What exists

| Surface | Implementation |
|---------|----------------|
| `StartBackgroundTask` | In-process map + mutex in `handler/admin/tasks.go` |
| `GET /api/tasks` | Lists tasks from **this process only** |
| `GET /api/tasks/{id}` | Looks up `id` in **this process only** |
| Dedupe keys | Process-local (`backgroundDedupeIDs`) |
| TTL / cleanup | Process-local expiry + periodic sweep |

Callers that queue real work through this registry include (non-exhaustive):

- `POST /api/settings/maintenance/clear-cache` (rebuild routes after local cache wipe)
- account runtime health refresh (`wait=false`)
- account-token sync / announcement sync style admin jobs

Returned `jobId` / `taskId` are the same random hex id from the local registry.

## Honest residual

1. **Process-local in-memory only.** `StartBackgroundTask` and `/api/tasks*` do
   not share state across replicas. Restart loses pending/running/finished tasks
   still within TTL.
2. **`jobId` / `taskId` are not cluster-wide.** A client that starts a job on
   instance A and polls instance B gets **404 task not found** (or never sees
   the job in list).
3. **List/get only see this process.** Operators cannot use the task API as a
   fleet-wide job board.
4. **Dedupe is per process.** Concurrent identical dedupe keys on two instances
   both run; there is no shared lock.
5. **Out of scope for #236:** durable multi-instance job coordination (shared
   stream/queue, DB table, K8s Job API). Do not invent those here.

## Operator options

| Option | When | Notes |
|--------|------|-------|
| Sticky load balancer to one admin instance | Multi-replica + UI polling tasks | Keep start + poll on the same process |
| Single admin replica for write/async ops | Small deployments | Simplest operational model |
| Accept degradation | Non-sticky LB | Poll may 404; treat job ids as best-effort hints, rely on durable side effects (DB rows) for truth |
| Future shared job store | Only if product requires true multi-instance task visibility | Separate design; not this residual |

Durable side effects of jobs (DB deletes, route rebuilds writing shared tables)
remain multi-instance-safe when they hit shared storage. Only the **task
registry / progress polling** surface is process-local.

## Related residual

Settings clear-cache multi-instance note (local cache invalidation only):

- Code: `handler/admin/settings_maintenance.go` — `clearCache` comment:
  *“Multi-instance residual: only this process's in-memory caches are cleared;
  peer instances retain their own caches until TTL/local invalidation.”*
- Response message already tells operators multi-instance needs per-process
  invalidation.
- Site proxy / accounts snapshot caches: `docs/analysis/site-proxy-cache-invalidation.md`
  (process-local residual).
- Health refresh residual also states process-local task registry:
  `docs/analysis/residual-health-refresh.md`.
- Optional shared-count notes (RPM only today; not a job bus): `docs/analysis/redis-shared-state.md`.

## Tests

```bash
go test ./handler/admin -count=1 -run Task
```

Coverage intent: `StartBackgroundTask` creates a process-local task gettable by
id; list sees it; unknown id is not found. No fake distributed claims.
