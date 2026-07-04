# Multi-Instance Safety Audit: 2 MetAPI instances, 1 PostgreSQL

**Date**: 2026-07-05
**Auditor**: Automated audit of `D:/Code/TokenDance/metapi-go`
**Scope**: What breaks when 2 MetAPI instances share 1 PostgreSQL instance. Five dimensions: scheduler lease, sticky session affinity, cache invalidation, settings hot-reload, backup export concurrency.

---

## 1. Scheduler Lease Mechanism

### Verdict: SAFE (DB-backed CAS lease)

**File**: `scheduler/usage_aggregation.go`

The usage aggregation scheduler uses a proper database-backed lease with compare-and-swap semantics.

**Mechanism**:
- Lease is stored in the `analytics_projection_checkpoints` table (columns: `lease_owner`, `lease_token`, `lease_expires_at`)
- `tryAcquireLease()` (line 242) performs a conditional UPDATE:
  ```sql
  UPDATE analytics_projection_checkpoints
  SET lease_owner = ?, lease_token = ?, lease_expires_at = ?, updated_at = ?
  WHERE projector_key = ?
    AND (lease_expires_at IS NULL OR lease_expires_at <= ?)
  ```
- Only the instance whose `rowsAffected > 0` proceeds. The loser returns `nil` lease and skips the pass.
- `releaseLease()` (line 280) clears the lease using token matching: `WHERE ... AND lease_token = ?` -- prevents accidental release by a different instance.
- Lease duration is 10 minutes (600,000ms). If an instance crashes, the lease expires naturally and the surviving instance picks up after expiration.
- Owner identity is `hostname:PID` (line 228-234), which provides clear attribution in the DB row.

**What happens with 2 instances**: Only ONE instance holds the lease at any time. The other skips its pass and reads the checkpoint watermark only. This is correct and safe. No duplicate aggregation.

**Gaps / Notes**:
- The 10-minute lease is long. If the lease-holding instance dies, aggregation is paused for up to 10 minutes (until expiry). Acceptable for analytics; not a correctness issue.
- This is the ONLY scheduler with a DB-backed lease. Other schedulers (checkin, log_cleanup, model_probe, file_retention, sub2api_refresh, etc.) use only `store.GetDB()` but have NO multi-instance coordination. If two instances run these, duplicate work occurs (see Appendix A).

---

## 2. Sticky Session Affinity

### Verdict: BREAKS -- in-memory only, no cross-instance sharing

**Files**: `proxy/session.go`, `proxy/channel_selection.go`, `proxy/surface.go`

Sticky session bindings are stored in `ProxyChannelCoordinator.stickyBindings`, which is a Go `map[string]StickyEntry` (session.go:99). There is NO database backing, NO shared cache, NO distributed coordination.

**Mechanism**:
- `BindStickyChannel()` (session.go:179) writes to the local map.
- `GetStickyChannelID()` (session.go:159) reads from the local map, checking TTL.
- Sticky key is built from clientKind + sessionId + model + path + downstreamAPIKeyID.

**What happens with 2 instances**:

| Scenario | Instance A | Instance B | Result |
|---|---|---|---|
| Client sends request to A, gets channel 42, sticky binding created on A | `stickyBindings["key"] = {42, ...}` | (nothing) | OK |
| Client retries same session, load balancer routes to B | (no binding) | `GetStickyChannelID("key")` returns 0 | Sticky preference LOST. B falls back to normal channel selection, picks a different channel. |
| Client retries, routes back to A | Binding still valid on A | (nothing) | Sticky works again -- but only if LB happens to route back to A |

**Impact**:
- **No data corruption** -- sticky binding loss is a behavioral degradation, not a correctness bug.
- **Weakened reliability on retries**: A client that had a successful channel on A may get a different channel on B, potentially hitting a less reliable account. This undermines the purpose of sticky sessions (channel affinity for stable request flows).
- **No error will be thrown** -- the system silently loses the affinity.
- Sticky sessions are enabled by default (`ProxyStickySessionEnabled = true` in config).

**Required fix for multi-instance**:
- Option 1: Make sticky bindings DB-backed (new table `sticky_session_bindings` with TTL column).
- Option 2: Use an external shared cache (Redis).
- Option 3: Configure load balancer for session affinity (cookie-based or IP-hash) so requests always hit the same instance -- this is the lowest-effort mitigation but couples deployment to LB behavior.
- Option 4: Accept the degradation and document it as a known limitation for multi-instance deployments.

---

## 3. Cache Invalidation

### Verdict: BREAKS -- per-instance in-memory cache with no cross-instance invalidation

**Files**: `routing/cache.go`, `routing/router.go`, `handler/admin/settings_maintenance.go`

### 3.1 RouteCache is per-instance memory

`RouteCache` (`routing/cache.go`) stores:
- `routes` -- the full enabled routes list (TTL-based freshness)
- `matchCache` -- per-route match results (TTL-based)
- `stableFirst*` globals -- state for the StableFirst routing strategy

**All of these are Go struct fields with `sync.RWMutex`. No external store.**

### 3.2 Write path: PatchCachedChannel only updates local cache

When `RecordSuccess()` (router.go:456) or `RecordFailure()` (router.go:608) is called on Instance A:
1. A writes to DB (e.g., `UpdateChannelSuccessFields` writes to `route_channels`)
2. A patches its own local cache via `PatchCachedChannel()` -- modifies the in-memory channel object

**Instance B never sees the cache patch.** Instance B still has the stale channel state in its cache until:
- Its own TTL expires and it reloads from DB, OR
- It receives a request that triggers its own `RecordSuccess`/`RecordFailure`

### 3.3 InvalidateAll is local only

`InvalidateAll()` (cache.go:115) clears local maps. `InvalidateRouteScopedCache()` clears a single route locally. Neither signals other instances.

### 3.4 Maintenance endpoints do NOT invalidate cache

`clear-cache` in `settings_maintenance.go` (line 26):
```go
h.db.Exec("DELETE FROM model_availability")
h.db.Exec("DELETE FROM route_channels")
h.db.Exec("DELETE FROM token_routes")
```
This DELETEs rows from PostgreSQL but does NOT call `InvalidateAll()` on any instance's `RouteCache`. The in-memory cache on ALL instances (including the one receiving the API call) remains populated with now-deleted data. This is a bug even in single-instance mode.

Similarly, `factory-reset` (line 73) DELETEs all 27 tables but never invalidates the cache.

### 3.5 Split-brain scenarios

| Operation | Instance A | Instance B | DB State | Result |
|---|---|---|---|---|
| A records channel success on ch-42 | Patches cache: ch-42.successCount=100, DB updated | Cache: ch-42.successCount=50 (stale) | ch-42.successCount=100 | B selects channels based on stale load data |
| A records channel failure on ch-42 | Patches cache: ch-42.cooldownUntil=now+5min, DB updated | Cache: ch-42.cooldownUntil=nil (stale) | ch-42.cooldownUntil=future | B may select a cooling-down channel |
| A calls clear-cache | (no invalidation) | DB rows deleted | route_channels empty | BOTH instances still serve routes from stale in-memory cache |
| A modifies route_channels in DB | Patches local cache only | Reads stale cache (TTL not yet expired) | Modified | B uses stale routing decisions |
| TTL expires on B | Cache evicted | Reloads fresh from DB | (latest) | B catches up -- latency equals cache TTL |

**Impact severity**: MODERATE-HIGH. Routing decisions on Instance B are based on stale data. Failed channels may be retried, successful channels may be under-utilized. The cache TTL (`TokenRouterCacheTtlMs`, default from env) determines the staleness window. A short TTL mitigates but increases DB load.

**Required fix for multi-instance**:
- Minimum fix: Add `cache.InvalidateAll()` after every DB-write admin endpoint (clear-cache, factory-reset, any route/site/account mutation).
- Multi-instance fix: Implement a cache invalidation channel (Redis pub/sub, periodic DB polling with `updated_at` checks, or a dedicated `cache_invalidation` table with version columns that each instance polls).

---

## 4. Settings Hot-Reload Conflicts

### Verdict: BREAKS -- settings loaded once at startup, no reload mechanism

**Files**: `store/settings.go`, `config/config.go`

### 4.1 Settings are loaded ONCE at startup

`LoadRuntimeSettings()` (settings.go:14) reads all rows from the `settings` table and calls `ApplyRuntimeSettings()` to mutate the global `Config` object. This happens once during bootstrap. After that, there is NO periodic reload, NO watch mechanism, NO invalidation trigger.

### 4.2 Admin API writes to DB but does not reload config

When an admin changes a setting via the UI (which writes to the `settings` table via `SettingsStore.Set()`), the config in the writing instance's memory is NOT automatically updated. The other instance's config is also NOT updated.

### 4.3 Global config singleton

`config.Get()` (config.go:26) returns a global `*Config` guarded by `sync.RWMutex`. But `ApplyRuntimeSettings()` mutates fields of this shared struct directly -- there's no copy-on-write or versioning.

### 4.4 Concrete conflict scenarios

| Scenario | Instance A | Instance B | Result |
|---|---|---|---|
| Admin sets `webhook_enabled` = false via API on A | DB updated. A's config still has old value until restart | B's config has old value | Both continue sending webhooks until restart |
| Admin sets `proxy_max_channel_attempts` = 1 on A | DB updated. A's config unchanged (still 3) | B's config unchanged | Routing uses stale attempt limits until restart |
| Admin changes `auth_token` on A | DB updated. A's in-memory token unchanged | B's token unchanged | Authentication uses old token until restart |
| Both A and B modify same setting simultaneously | Last-write-wins in DB | Last-write-wins in DB | DB is consistent. Both instances' configs are stale. |

**Impact**: HIGH for security-sensitive settings (auth_token, proxy_token). MODERATE for operational settings (webhook, proxy limits). LOW for cosmetic settings.

**Root cause**: There is no `config.Reload()` function. The design assumes settings are static after bootstrap, but the admin API allows runtime changes.

**Required fix for multi-instance**:
- Add a `config.Reload()` function that re-reads from the settings table and re-applies.
- Either: (a) Add an API endpoint `POST /api/settings/reload` that admins call after changes, or (b) Add a periodic reload goroutine (e.g., every 30s), or (c) After every `SettingsStore.Set()` call, trigger `config.Reload()` on the local instance.
- For cross-instance: Poll the settings table for a `updated_at` timestamp change, or use a NOTIFY channel on PostgreSQL.

---

## 5. Backup Export Concurrent Safety

### Verdict: PARTIALLY BREAKS -- no snapshot isolation, concurrent modifications cause inconsistent exports

**File**: `handler/admin/settings_backup.go`

### 5.1 Export path: no transaction wrapper

`exportBackup()` (line 94) iterates through `allTables` (27 tables) and does individual `SELECT * FROM table` queries. There is NO `BEGIN TRANSACTION` or `SET TRANSACTION ISOLATION LEVEL SERIALIZABLE`. Each table read is a separate query.

**What happens when export runs concurrently with writes**:
- Instance A: runs exportBackup. Reads `sites`, then `accounts`, then `account_tokens`...
- Instance B: concurrently INSERTs a new account. This creates a row in `accounts` and `account_tokens`.
- Result: Export includes the new `accounts` row but NOT the child `account_tokens` row (because `accounts` was read before the INSERT happened). FK-dependent data is inconsistent in the export.
- On SQLite with WAL: each `SELECT` is internally consistent, but cross-table consistency is NOT guaranteed.
- On PostgreSQL with default READ COMMITTED: same problem.

### 5.2 Import path: INSERT OR IGNORE per table

`importTableRows()` (line 239) uses `INSERT OR IGNORE` within per-table transactions. Two instances importing simultaneously:
- Both try to insert the same row. One succeeds, the other ignores (no error).
- If the exports came from different states, you get a merge (union) of both exports -- possibly inconsistent.

### 5.3 factory-reset is atomic per instance but destructive across instances

`factoryReset()` (line 73) wraps all DELETEs in a single transaction. It is atomic: either all tables are cleared or none. However, if Instance A is doing factory-reset while Instance B is serving proxy traffic:
- On PostgreSQL with READ COMMITTED: B may read partially-deleted state during the transaction window.
- On PostgreSQL with REPEATABLE READ or SERIALIZABLE: B's reads are blocked until A commits (or B sees pre-transaction state).
- Current code sets no isolation level explicitly, so PostgreSQL defaults (READ COMMITTED) apply.

### 5.4 WebDAV stubs

`exportToWebdav()` and `importFromWebdav()` are stubs (return hardcoded success). No real concurrency concerns yet, but the stubs will need the same fixes when implemented.

### Summary table

| Operation | Transaction? | Cross-instance safe? | Notes |
|---|---|---|---|
| `exportBackup` | No | No | 27 sequential reads, no snapshot |
| `importBackup` | Per-table only | Partial | INSERT OR IGNORE; cross-table FK consistency not enforced |
| `factoryReset` | Yes (single tx) | No for concurrent readers | Readers see mid-reset state on READ COMMITTED |
| `clear-cache` | No | Yes (simple DELETE) | But does not invalidate in-memory cache (bug) |
| `clear-usage` | No | Yes (simple DELETE+UPDATE) | OK |
| `exportToWebdav` | N/A (stub) | N/A | Stub |

**Required fix for multi-instance**:
- Wrap `exportBackup` in a `BEGIN READ ONLY` or `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ` to get a consistent snapshot across all 27 tables.
- Add an export lock (via settings table flag) to prevent concurrent exports if that is desired (basically, a mutex).
- Consider using `pg_dump` for PostgreSQL deployments instead of per-table SELECT loops -- it handles snapshot consistency natively.

---

## Summary: What Breaks?

| Dimension | Status | Severity | Will it cause data corruption? |
|---|---|---|---|
| Scheduler lease (usage aggregation) | SAFE | Low | No. DB-backed CAS lease ensures exactly-one execution. |
| Sticky session affinity | BROKEN | Medium | No data corruption. Silently loses session affinity on cross-instance retries. |
| Cache invalidation | BROKEN | High | No corruption, but both instances serve stale routing data. `clear-cache` API is a no-op for cache. |
| Settings hot-reload | BROKEN | High | No corruption. Runtime settings changes require full restart to take effect on any instance. |
| Backup export consistency | BROKEN | Medium | Export snapshots are cross-table inconsistent during concurrent writes. Import can produce merged state. |

### Other schedulers (Appendix A -- not in scope but noted)

The following schedulers have NO multi-instance coordination (no lease, no locking):

| Scheduler | File | Multi-instance behavior |
|---|---|---|
| checkin | `scheduler/checkin.go` | Both instances run checkin for all accounts. Duplicate API calls, balance check collisions. |
| channel_recovery | `scheduler/channel_recovery.go` | Both instances attempt to recover channels. Duplicate DB writes (last-write-wins). |
| log_cleanup | `scheduler/log_cleanup.go` | Both instances DELETE old logs. Potentially conflicting DELETEs (one succeeds, other does 0 rows). |
| model_probe | `scheduler/model_probe.go` | Both instances probe model availability. Duplicate upstream calls. |
| file_retention | `scheduler/file_retention.go` | Both instances prune old files. Both try to delete the same files. |
| sub2api_refresh | `scheduler/sub2api_refresh.go` | Both instances refresh subscriptions. Duplicate upstream API calls. |
| log_retention | `scheduler/log_retention.go` | Same as log_cleanup pattern. |
| site_announcement | `scheduler/site_announcement.go` | Both instances check and update announcements. |
| backup_webdav | `scheduler/backup_webdav.go` | Both instances trigger WebDAV exports. Duplicate operations. |
| admin_snapshot | `scheduler/admin_snapshot.go` | Both instances create snapshots. Duplicate DB writes. |

These are all at least wasteful; some (checkin, model_probe, sub2api_refresh) also generate duplicate upstream API calls that could trigger rate limits.

---

## Recommended Mitigation Priority

1. **P0 -- Cache invalidation on admin writes**: Add `router.InvalidateAllCaches()` after every admin mutation that changes routes/sites/accounts/channels. This fixes single-instance correctness first.

2. **P0 -- Settings reload after admin writes**: Add `config.Reload()` and call it after every settings mutation. This fixes single-instance correctness.

3. **P1 -- All schedulers need DB-backed leases**: Extend the `usage_aggregation` lease pattern (or a simpler `analytics_projection_checkpoints`-like row) to all other schedulers. Without this, multi-instance deployment generates duplicate work and duplicate upstream API calls.

4. **P1 -- Backup export with snapshot isolation**: Wrap `exportBackup` in a `REPEATABLE READ` transaction.

5. **P2 -- Cache invalidation bus for multi-instance**: Either Redis pub/sub, PostgreSQL LISTEN/NOTIFY, or periodic polling of an `updated_at` column for cross-instance cache coherence.

6. **P2 -- Sticky session affinity**: Either DB-backed sticky bindings or document that sticky sessions require load balancer session affinity in multi-instance deployments.

7. **P3 -- `factory-reset` should drain traffic first**: Add a maintenance mode flag in the settings table that proxy handlers check before serving requests, set during destructive operations.
