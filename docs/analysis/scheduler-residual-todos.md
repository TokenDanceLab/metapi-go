# Residual: scheduler silent TODO no-ops (#246)

**Date**: 2026-07-17  
**Issue**: [#246](https://github.com/TokenDanceLab/metapi-go/issues/246)  
**Lane**: residual honesty (scheduler scaffolding)  
**Scope**: document + strengthen comments only. No invented Sub2API refresh product, no fake sync success.

## Goal

Four background schedulers still carry TODOs that look like product work. Operators reading logs or code should see **honest residual** language: what actually runs, what is a no-op, and multi-instance behavior. This wave does **not** implement the missing product paths.

## Inventory

| Scheduler | File | Interval | What currently runs | Residual / silent gap | Multi-instance |
|-----------|------|----------|---------------------|------------------------|----------------|
| Sub2API refresh | `scheduler/sub2api_refresh.go` | 60s (min 60s) | Lease + SQL scan of active `sub2api` accounts; logs `scanned=N, refreshed=0, failed=0` | Does **not** parse `extraConfig.sub2apiAuth`; does **not** filter due tokens; does **not** call `refreshSub2ApiManagedSessionSingleflight` or any upstream refresh. Concurrency constant unused. | `runWithSchedulerLease` (PG advisory lock / process-local mutex). Only one instance runs the empty pass. |
| Channel recovery | `scheduler/channel_recovery.go` | 30s (min 10s) | Lease + cooling SQL + active candidates via optional `SetActiveChannelIDsProvider` (coordinator) or SQL LIMIT 50 residual + probe | Active path prefers provider (#273); residual SQL only when provider **unset**. Empty provider set does not fall back to residual SQL. Probe real only if model-probe registered. | Lease serializes sweeps; in-flight maps process-local. |
| Site announcement | `scheduler/site_announcement.go` | 15m (min 10s) | Lease + optional `SyncFunc` (wired from admin `SyncSiteAnnouncements` via `SetDefaultSiteAnnouncementSyncFunc` on route register, #272) | When SyncFunc set: real platform adapter + DB writes. When nil: residual scan-only remains for tests/partial boots. | Lease serializes sync/scan. |
| Update center | `scheduler/update_center.go` | 15m (min 10s) | Lease + residual log line only (#283) | **No** remote registry/helper poll, **no** version persistence, **no** deploy/rollback, **no** `updateAvailable` invention. Admin status/check are local stubs (`0.0.0` / `updateAvailable=false` + `residual`); deploy/rollback/SSE are 501 residuals (`residual-update-center.md`). | Lease serializes residual log. No cluster-wide "last checked" state. |

## Per-scheduler detail

### 1. `Sub2APIRefreshScheduler` (`sub2api_refresh.go`)

**Runs today**

1. Start ticker; immediate first `runPass`.
2. Process-local `passInFlight` skip if previous pass still open.
3. Acquire scheduler lease; on miss, skip.
4. Query:

   ```sql
   SELECT a.id, a.extra_config
   FROM accounts a
   INNER JOIN sites s ON a.site_id = s.id
   WHERE LOWER(TRIM(COALESCE(s.platform, ''))) = 'sub2api'
     AND a.status = 'active'
     AND s.status = 'active'
   ```

5. Append every scanned row as a "candidate" without parsing `extra_config`.
6. Log `pass complete (scan-only residual; no tokens refreshed)` with `refreshed=0`, `failed=0`.

**Residual**

| TODO site | Honest status |
|-----------|---------------|
| parse `extraConfig` for `refreshToken` + `tokenExpiresAt` | **Not wired** — `extraConfig` is loaded and discarded |
| wire Sub2API refresh via singleflight | **Not wired** — balance package has `refreshSub2ApiManagedSessionSingleflight` (auto-relogin style), but scheduler never calls it |
| concurrency = 4 | Constant only; no worker pool |

**Do not invent** a full managed-auth refresh product in this residual wave. Related account-merge residual: `docs/analysis/residual-sub2api-auth.md`.

### 2. `ChannelRecoveryScheduler` (`channel_recovery.go`)

**Runs today**

1. Ticker + immediate first sweep; `sweepInFlight` serialization; scheduler lease.
2. `loadCoolingCandidates`: real SQL over `route_channels` with non-null future `cooldown_until`.
3. `loadActiveCandidates` (#273):
   - If `SetActiveChannelIDsProvider` is registered (boot wires `ProxyChannelCoordinator.GetActiveChannelIDs` from `app.ConfigureProxyUpstream`), resolve model names for those IDs via SQL (`enabled` + active account/site; null cooldown preferred, source_model channels still included).
   - If provider is **nil**, residual SQL of enabled channels with null cooldown (`LIMIT 50`).
4. Merge (cooling wins), due-filter via process-local maps, prioritize, cap batch=4.
5. `probeCandidate`: if global model-probe scheduler is registered, call `probeOne`; else debug-skip (no fake success).

**Residual**

| TODO site | Honest status |
|-----------|---------------|
| wire active candidate loading via `proxyChannelCoordinator` | **Wired when proxy upstream boots** (`scheduler.SetActiveChannelIDsProvider` → `coord.GetActiveChannelIDs`, #273). Residual SQL `LIMIT 50` remains only when provider is unset (tests / pre-boot). Process-local leases still mean multi-instance active sets are not shared. |

**Not a pure no-op**: cooling load + optional model probes can perform real work. Remaining honesty gap is multi-instance coordinator state, not missing boot wiring.

### 3. `SiteAnnouncementScheduler` (`site_announcement.go`)

**Runs today**

1. Ticker + immediate first run; `inFlight` + lease.
2. `SELECT id, platform, url FROM sites WHERE status = 'active'`.
3. Count rows; log residual scan complete. No adapter, no DB writes.

**Residual**

| TODO site | Honest status |
|-----------|---------------|
| platform-specific announcement sync | **Not wired** from this scheduler. Prefer reusing admin `syncSiteAnnouncements` rather than inventing a second path. |

Log language is intentionally "residual scan", not "sync success".

### 4. `UpdateCenterScheduler` (`update_center.go`)

**Runs today** (#283)

1. Ticker + immediate first residual pass; `inFlight` + lease.
2. Single info log: residual check, no remote polling, no `updateAvailable` invention.
3. **No** HTTP client, **no** version compare, **no** DB write of last-checked.

**Residual**

| TODO site | Honest status |
|-----------|---------------|
| wire actual update-center polling | **Not wired** — pure log-only no-op for product purposes. Do not add fake poll stubs. |

Related admin residuals (status/check local stubs with residual field; deploy/rollback/SSE 501): `docs/analysis/residual-update-center.md` (#197 / #283).

## Multi-instance notes (shared)

All four use `runWithSchedulerLease` (`scheduler/lease.go`):

| Dialect | Behavior |
|---------|----------|
| PostgreSQL | `pg_try_advisory_lock` per scheduler name hash; non-holder skips the run |
| SQLite / non-PG | Process-local map mutex only (single-process coordination) |

Implications:

1. Duplicate empty passes are suppressed across PG replicas (good for noise).
2. Lease does **not** create product behavior that is missing inside the pass.
3. Channel-recovery probe bookkeeping (`inFlightKeys`, `lastStartedAtByKey`) remains process-local even when the lease is shared — only the lease holder mutates its own maps.
4. No durable job bus / cross-instance task registry is implied (see also `background-tasks-multi-instance-residual.md`).

## Explicit non-goals (#246)

- Implementing full Sub2API scheduled refresh product
- Calling admin announcement sync from the ticker without a deliberate product decision
- Remote update-center helper client / deploy orchestration
- Fake `refreshed>0`, fake announcement inserts, fake `updateAvailable=true`
- Multi-instance durable job coordination

## Verify

```bash
go test ./scheduler -count=1
test -f docs/analysis/scheduler-residual-todos.md
```

## Related docs

- `docs/analysis/residual-sub2api-auth.md` — account-path managed auth merge residual
- `docs/analysis/residual-update-center.md` — admin status/check stubs + deploy/rollback 501 + scheduler log-only (#197 / #283)
- `docs/analysis/background-tasks-multi-instance-residual.md` — process-local admin task registry
- `docs/specs/p12-schedulers.md` — intended TS parity (aspirational vs residual above)

### Update (#272)

Production path: `RegisterSiteAnnouncementsRoutes` installs `SetDefaultSiteAnnouncementSyncFunc` → `admin.SyncSiteAnnouncements`. Residual scan only when `SyncFunc` is nil.

### Update (#273)

Channel recovery active candidates prefer optional `SetActiveChannelIDsProvider` (wired from `ConfigureProxyUpstream` → `ProxyChannelCoordinator.GetActiveChannelIDs`). Nil provider keeps SQL `LIMIT 50` residual fallback. Empty provider set does **not** fall back to residual SQL.

### Update (#283)

Update-center residual honesty hardening: scheduler remains log-only (no remote registry product). Admin status/check expose explicit `residual` and never invent `updateAvailable=true`. Deploy/rollback/SSE stay honest 501. Endpoint table + scheduler behavior documented in `residual-update-center.md`.
