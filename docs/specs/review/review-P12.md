# P12 Schedulers -- Cross-Reference Review

**Review date**: 2026-07-04
**Spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p12-schedulers.md`
**TS sources reviewed**: 6 of 14 referenced files

| # | TS File | Source Lines | Scheduler(s) Covered |
|---|---------|-------------|---------------------|
| 1 | `checkinScheduler.ts` | 1-335 | Checkin, Balance, Daily Summary, Log Cleanup |
| 2 | `usageAggregationService.ts` | 1-896 | Usage Aggregation |
| 3 | `modelAvailabilityProbeService.ts` | 1-448 | Model Probe |
| 4 | `channelRecoveryProbeService.ts` | 1-300 | Channel Recovery |
| 5 | `sub2apiRefreshScheduler.ts` | 1-157 | Sub2API Refresh |
| 6 | `logCleanupService.ts` | 1-130 | Log Cleanup (core logic) |

**Not reviewed** (TS files not provided): backupService.ts, siteAnnouncementPollingService.ts, updateCenterPollingService.ts, adminSnapshotWarmService.ts, proxyFileRetentionService.ts, proxyLogRetentionService.ts, dailySummaryService.ts, oauth/localCallbackServer.ts

---

## Accuracy Issues

### A1. Checkin cron default is not hardcoded `0 8 * * *` in TS

**Spec claim** (row 56): "cron: `CHECKIN_CRON` (默认 `0 8 * * *`)"

**TS reality**: The TS reads `config.checkinCron` (from environment) and also resolves from DB setting `checkin_cron`. Neither the env var default nor the DB query has a hardcoded `0 8 * * *` anywhere in the scheduler file. The default lives in `config.ts`, not in the scheduler itself. The spec should clarify that the default is an env-var default, not a scheduler-code-level constant.

**Severity**: Low -- likely correct in practice if `config.ts` sets it, but the spec is imprecise about where the default originates.

### A2. Channel Recovery scope: probes active channels too, not just cooldown/fault

**Spec claim** (row 63): "重新探测冷却/故障通道" (re-probe cooldown/faulty channels)

**TS reality**: `channelRecoveryProbeService.ts` probes two candidate pools:
- `loadCoolingProbeCandidates()` -- channels with active `cooldownUntil > now`
- `loadActiveProbeCandidates()` -- channels returned by `proxyChannelCoordinator.getActiveChannelIds()`

The active-channel probe path (`CHANNEL_RECOVERY_ACTIVE_RECHECK_MS = 5 * 60_000`, i.e. 5 minutes) is a full health-check sweep on active channels, not just recovery. This is a significant scope difference: the scheduler is a **dual-purpose health monitor**, not just a recovery mechanism.

**Severity**: **High** -- Go implementation that omits active-channel probing will miss a key operational feature. The spec table row should read "重新探测冷却通道 + 活跃通道健康检查".

### A3. Model Probe default interval is config-driven, not a scheduler constant

**Spec claim** (row 62): "`MODEL_AVAILABILITY_PROBE_INTERVAL_MS` (默认 30 min)"

**TS reality**: `startModelAvailabilityProbeScheduler(intervalMs = config.modelAvailabilityProbeIntervalMs)` reads from config, then enforces `Math.max(60_000, ...)` as the *minimum*. The "default 30 min" is a config-level default, not a scheduler-level constant. The scheduler's own floor is 60 seconds. The spec is misleading about where the guard lives.

**Severity**: Low -- functional outcome is the same, but a Go implementer reading only the spec might assume the scheduler itself defaults to 30 min and skip the minimum guard.

### A4. Usage Aggregation: spec omits recompute path entirely

**Spec claim** (lines 86-95): 5-step flow: acquire lease, read checkpoint, fetch proxy_logs, aggregate, upsert + update checkpoint.

**TS reality**: There is a **sixth major step** before step 2: `applyPendingRecompute()`. When `checkpoint.recomputeFromId > 0`, the TS:
1. Locates the affected row's day boundary
2. **Deletes** all existing aggregates from `siteDayUsage`, `siteHourUsage`, `modelDayUsage` for that day forward
3. Resets `lastProxyLogId` to `restartFromId - 1`
4. Continues normal projection from the reset point

This is triggered by `requestUsageAggregatesRecompute()` (line 857-873). The spec's 5-step flow is missing this critical "clear + re-project" phase that runs before normal incremental processing.

**Severity**: **High** -- the recompute mechanism is a core data-correctness feature. A Go implementation that follows only the spec's 5-step flow will silently never support recompute.

---

## Missing Details

### D1. Checkin interval mode: `intervalAttemptByAccount` guard

**Spec says**: "查询所有 checkinEnabled=true 的账号 → 筛选 lastCheckinAt 超过 INTERVAL_HOURS → CheckinAll()"

**TS additionally**: Maintains an in-memory `Map<number, number>` (`intervalAttemptByAccount`) recording the last attempt timestamp per account. The `selectDueIntervalCheckinAccountIds()` function (lines 78-102) uses this to **prevent re-queueing an account that was already attempted within the same interval**, even if the DB `lastCheckinAt` hasn't been updated yet (e.g., mid-flight checkin). This prevents duplicate checkins within a single interval window.

**Go implementer impact**: Without this guard, an account could be submitted for checkin multiple times in rapid succession if the DB write is slow.

### D2. Checkin interval mode: additional status filters

**Spec says**: Only mentions `checkinEnabled=true`.

**TS additionally**: Filters by `accounts.status === 'active'` AND `sites.status !== 'disabled'` (lines 113-114). Inactive accounts and accounts on disabled sites are excluded regardless of `checkinEnabled`.

### D3. Checkin interval mode: `checkinIntervalHours` clamped to [1, 24]

**TS**: Line 242 -- `config.checkinIntervalHours = Math.min(24, Math.max(1, activeCheckinIntervalHours))`. The spec mentions the setting key but not the valid range enforcement.

### D4. Model Probe: token-level availability

**Spec says**: "探测已配置模型的可用性"

**TS additionally**: Probes **two** availability tables:
- `modelAvailability` -- account-level model availability (rows with `isManual !== true`)
- `tokenModelAvailability` -- token-level model availability (for accounts with usable tokens, `valueStatus === 'READY'`)

The spec treats these as one concept, but the code path, DB table, and update logic differ. The Go spec should distinguish these.

### D5. Model Probe: `modelAvailabilityProbeEnabled` flag

**TS** (line 418): `if (!config.modelAvailabilityProbeEnabled) return { enabled: false, intervalMs: 0 }`. The scheduler gates on an enable flag. Not mentioned in spec.

### D6. Model Probe: account-level lease (`probeAccountLeases`)

**TS** (lines 68, 207-216): In-memory `Set<number>` prevents concurrent probe execution on the same account. If a probe is already running for account X, a new request yields a `'skipped'` result with message "model availability probe already running for account". Not in spec.

### D7. Model Probe: background task integration

**TS** (lines 386-414): `queueModelAvailabilityProbeTask()` wraps execution in `startBackgroundTask()` with dedup key, notifications, and success/failure messages. The scheduler does not call `executeModelAvailabilityProbe()` directly -- it goes through the background task system for observability. Not in spec.

### D8. Channel Recovery: batch cap

**TS** (line 25): `CHANNEL_RECOVERY_MAX_BATCH = 4` -- maximum probes per sweep. The spec says "concurrency 1" but does not mention the 4-candidate hard cap.

### D9. Channel Recovery: dual recheck windows

**TS** (lines 26-27, 44-48):
- Cooldown candidates: `CHANNEL_RECOVERY_COOLDOWN_RECHECK_MS = 30_000` (30s)
- Active candidates: `CHANNEL_RECOVERY_ACTIVE_RECHECK_MS = 5 * 60_000` (5 min)

These two windows create different re-probe cadences for the two candidate types. The spec only mentions a single 30s sweep interval with no differentiation.

### D10. Channel Recovery: `isProviderDirectedCooldown()` skip

**TS** (lines 80-86, 124-125): Channels where `cooldownUntil` is set but `failCount`, `consecutiveFailCount`, and `cooldownLevel` are all zero are treated as "provider-directed cooldowns" and **skipped entirely** from recovery probing. This is a deliberate design choice to avoid probing channels that the upstream provider itself marked for cooldown. Not in spec.

### D11. Channel Recovery: `tokenRouter.recordProbeSuccess()` on recovery

**TS** (lines 219-224): On successful probe (`status === 'supported'`), the scheduler calls `tokenRouter.recordProbeSuccess(channelId, latencyMs, modelName)` to record the successful recovery and potentially update routing state. Not in spec.

### D12. Channel Recovery: `proxyChannelCoordinator.getActiveChannelIds()` integration

**TS** (line 241): The active-channel probe path queries the `proxyChannelCoordinator` for the list of currently-active channel IDs. The spec does not mention this dependency at all.

### D13. Sub2API Refresh: singleflight dedup

**TS** (line 8): Imports `refreshSub2ApiManagedSessionSingleflight` -- uses a singleflight pattern so that concurrent calls to refresh the same account are coalesced into one. Not in spec.

### D14. Sub2API Refresh: immediate first pass

**TS** (line 134): `void runScheduledSub2ApiRefreshPass()` is called immediately on scheduler start, before the interval timer begins. The spec says "60 sec" but does not mention the initial eager pass.

### D15. Sub2API Refresh: pass-in-flight dedup

**TS** (lines 113-128): `sub2ApiRefreshPassInFlight` variable prevents overlapping passes. If a pass is already running when the interval fires, the new invocation awaits the existing promise. Not in spec.

### D16. Usage Aggregation: batch size constants

**TS**: `PROJECTION_BATCH_SIZE = 1_000`, `PROJECTION_MAX_BATCHES_PER_PASS = 120`. Max 120,000 rows per pass. Not in spec.

### D17. Usage Aggregation: MySQL vs SQLite upsert branching

**TS**: Every upsert function (`upsertSiteDayUsage`, `upsertSiteHourUsage`, `upsertModelDayUsage`, `writeProjectionCheckpoint`) branches on `runtimeDbDialect === 'mysql'` to use `onDuplicateKeyUpdate` vs `onConflictDoUpdate`. The spec has no mention of dialect-specific SQL.

### D18. Usage Aggregation: snapshot cache invalidation

**TS** (line 713, 783): `clearAnalyticsSnapshots()` clears cached snapshots (`site-stats`, `dashboard-summary`, `dashboard-insights`) after each projection batch and recompute. Not in spec.

### D19. Usage Aggregation: cost calculation fallbacks

**TS** (lines 160-195): Three spend-resolution functions with platform-specific rates:
- `resolveSummarySpend`: Veloera platform uses `tokens / 1_000_000`, others `tokens / 500_000`
- `resolveSiteSpend`: Uses `fallbackTokenCost(tokens, platform)` from `modelPricingService`
- `resolveModelSpend`: Fixed `tokens / 500_000`

Spec says only "按 (site, model, day/hour) 聚合" with no mention of spend calculation logic.

### D20. Usage Aggregation: `projectionInFlight` dedup

**TS** (lines 844-855): `runUsageAggregationProjectionPass()` returns existing promise if one is in flight. The internal 5s interval fires via `startUsageAggregationProjectorScheduler()` but overlapping calls are coalesced. Not in spec.

### D21. Usage Aggregation: lease ownership format

**TS** (lines 207-210): Lease owner is `hostname:PID` (e.g., `myhost:12345`). Spec only says "10 min lease" with no ownership identification detail.

### D22. Log Cleanup: distinguishes usage logs from program logs

**TS**: `cleanupUsageLogs()` deletes from `proxyLogs` table; `cleanupProgramLogs()` deletes from `events` table. The spec only says "清理过期日志" without distinguishing the two log types or their target tables.

### D23. Log Cleanup: `logCleanupConfigured` gating

**TS** (checkinScheduler.ts line 194): Before running cleanup, checks `config.logCleanupConfigured`. If false (legacy fallback mode is active), the task skips with a log message. The spec mentions "仅当设置显式配置时" but does not detail the gating mechanism.

### D24. Checkin: runtime hot-reload functions

**TS** (lines 266-323): Exported functions `updateCheckinCron()`, `updateCheckinSchedule()`, `updateBalanceRefreshCron()`, `updateLogCleanupSettings()` allow runtime changes to cron expressions, schedule mode, and cleanup settings without restart. The spec mentions "可通过 settings 表动态更新" in AC but does not list these hot-reload APIs.

### D25. Balance Refresh: route rebuild after balance refresh

**TS** (checkinScheduler.ts lines 161-171): Balance refresh task calls `refreshAllBalances()` then `routeRefreshWorkflow.refreshModelsAndRebuildRoutes()`. The spec says "刷新所有账号余额 + 重建路由" which is accurate, but the two-step sequence (balances first, then routes) is an ordering constraint not explicit in the spec.

### D26. Daily Summary: notification options

**TS** (checkinScheduler.ts lines 174-190): Daily summary notification uses `{ bypassThrottle: true, requireChannel: true, throwOnFailure: true }`. These options ensure the daily summary always sends, requires a configured notification channel, and throws on failure (so the cron task sees the error). Not in spec.

---

## Edge Cases Not Covered

### E1. Checkin interval: zero/negative hours

**TS behavior**: `checkinIntervalHours` is clamped to `[1, 24]` (line 242). `Math.max(1, ...)` ensures minimum 1 hour. `selectDueIntervalCheckinAccountIds()` also does `Math.max(1, intervalHours)`. The spec does not specify valid range or clamping.

### E2. Checkin dual-mode runtime switch

**TS behavior**: `updateCheckinSchedule()` calls `stopCheckinSchedule()` (which clears both cron task and interval timer), then `startCheckinSchedule()` which starts the new mode. The spec describes the two modes but does not address the transition from one mode to the other at runtime.

### E3. Checkin: empty CHECKIN_CRON

**Spec edge case**: "CHECKIN_CRON 为空 → 使用默认值"

**TS behavior**: `resolveCronSetting()` uses `cron.validate(value)` as the guard. An empty string would not pass `cron.validate()`, so it would fall back to the config default. This aligns with the spec edge case, but the validation is via cron library, not an explicit empty-string check.

### E4. Model Probe: disabled scheduler

**TS behavior**: When `modelAvailabilityProbeEnabled === false`, `startModelAvailabilityProbeScheduler()` returns `{ enabled: false, intervalMs: 0 }` without starting a timer. The spec does not mention this flag.

### E5. Model Probe: per-model probe failure

**TS behavior**: If an individual model probe throws, it is caught (lines 316-329), recorded as `inconclusive`, and the loop continues. The account result is marked `'failed'` only if **any** probe in that account failed. The spec does not address partial failure semantics.

### E6. Channel Recovery: cooldown vs active priority merge

**TS behavior**: `mergeRecoveryProbeCandidates()` (lines 172-181) deduplicates by channelId. If both cooldown and active candidates exist for the same channel, the cooldown candidate takes priority (`existing.source === 'active' && candidate.source === 'cooldown'`). The spec does not describe this merging or prioritization.

### E7. Channel Recovery: sweep-in-flight serialization

**TS behavior**: `runChannelRecoveryProbeSweep()` (lines 233-267) awaits the previous sweep if one is in flight (`recoveryProbeSweepInFlight`). If a sweep takes longer than 30s, subsequent sweeps queue behind it rather than overlapping. Not in spec.

### E8. Sub2API Refresh: pass-in-flight serialization

**TS behavior**: Same pattern as channel recovery -- `sub2ApiRefreshPassInFlight` prevents concurrent passes. Not in spec.

### E9. Usage Aggregation: lease release on error

**TS behavior**: `tryAcquireProjectionLease()` returns null if acquisition fails (already held). In the error path (lines 838-841), `releaseProjectionLease(lease, { error })` is called in the `catch` block before re-throwing. The spec mentions "lease 获取失败 → 跳过本轮" but not the error-release pattern.

### E10. Usage Aggregation: empty batch early exit

**TS behavior**: The projection loop (lines 818-830) breaks when `rows.length <= 0` or `rows.length < PROJECTION_BATCH_SIZE`. The latter check prevents an unnecessary extra fetch when the last batch was partial. Not in spec.

### E11. Usage Aggregation: recompute with no affected row

**TS behavior**: `applyPendingRecompute()` (lines 735-746) handles the case where `recomputeFromId` points to a log that no longer exists -- it clears the recompute flags without error. Not in spec.

### E12. Usage Aggregation: recompute with unresolvable day boundary

**TS behavior**: Lines 751-753 throw `'Failed to resolve recompute boundary for usage aggregates'` if `toLocalDayKeyFromStoredUtc()` or `toLocalDayStartUtcFromStoredUtc()` return null. Not in spec.

### E13. Log Cleanup: null cutoff

**TS behavior**: `getLogCleanupCutoffUtc()` can return null. Both `cleanupUsageLogs()` and `cleanupProgramLogs()` handle null cutoff by returning `deleted: 0` without executing a DELETE. Not in spec.

### E14. BALANCE_REFRESH_CRON = "manual" or invalid

**Spec edge case**: "BALANCE_REFRESH_CRON 设为 'manual' 或非法值 → 禁用调度, 仅手动触发"

**TS behavior**: `resolveCronSetting()` uses `cron.validate()`. If the setting value fails validation, it falls back to `config.balanceRefreshCron` (the env default). The TS does **not** have a special `'manual'` handling -- it would just fall through to the default cron. The spec edge case is **not implemented** in the TS as described; "manual" would be treated as invalid and would fall back to the default, not disable the scheduler.

---

## Incorrect Details

### I1. Model Probe interval floor is 60s, not "no floor"

**Spec**: Implies `model_availability_probe_interval_ms` can be any value (row 62).

**TS fact**: `Math.max(60_000, Math.trunc(intervalMs || 0))` enforces a hard minimum of 60 seconds. Setting the interval to 5s in config would still result in 60s actual.

### I2. Channel Recovery probe scope is incomplete

**Spec row 63**: "重新探测冷却/故障通道 (timeout 12s, concurrency 1)"

**TS fact**: Also probes active channels via `proxyChannelCoordinator.getActiveChannelIds()` with a 5-minute recheck window. The spec description covers only half of the scheduler's function.

### I3. BALANCE_REFRESH_CRON edge case does not match TS behavior

**Spec edge case** (line 119): "BALANCE_REFRESH_CRON 设为 'manual' 或非法值 → 禁用调度, 仅手动触发"

**TS fact**: There is no `'manual'` special case. Invalid cron values fall back to the default from config, not to disabled. A Go implementation that implements the spec's `'manual'` behavior would diverge from the TS behavior.

### I4. Usage Aggregation flow is oversimplified

**Spec** (lines 86-95): Describes a 5-step linear flow with no recompute, no batch limits, no snapshot invalidation, no dialect branching.

**TS fact**: The projection pass involves:
1. Try acquire lease
2. Read checkpoint (with recompute check)
3. **If recompute pending: locate day boundary, delete existing aggregates, reset watermark**
4. Loop up to 120 batches of 1000 rows each
5. Per batch: delta aggregation with spend calculation → upsert 3 tables → update checkpoint → clear snapshots
6. Release lease

The spec's 5-step summary omits steps 2-3 entirely and collapses steps 4-5.

### I5. Scheduler count mismatch between spec body and AC

**Spec body**: Lists 15 schedulers (rows 54-70: #1 through #15).

**Spec AC** (line 98): "[ ] 15 个 scheduler 全部实现并在 app.Start 时注册"

**TS files provided for review**: 6 out of 14 referenced TS files. I cannot verify the total count, but the spec references 14 files for 15 schedulers, suggesting the mapping is correct. No issue here, just noting the review scope is partial.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 4 |
| Missing Details | 26 |
| Edge Cases Not Covered | 14 |
| Incorrect Details | 5 |
| **Total findings** | **49** |

**Verdict: NEEDS_REVISION**

### Critical items to fix before Go implementation proceeds:

1. **I2 / A2 -- Channel Recovery scope**: The spec says "cooldown/fault channels only" but the TS also probes active channels. The Go spec row (#8) and description must include the active-channel health-check path with its 5-minute recheck window.

2. **A4 / I4 -- Usage Aggregation recompute**: The spec's 5-step flow omits the recompute clearing phase. This is a whole feature branch that the Go implementation would miss if following the spec alone. The flow must be expanded to include `applyPendingRecompute()` logic.

3. **I3 -- BALANCE_REFRESH_CRON "manual" edge case**: The spec describes behavior that is **not present in the TS code**. If the Go implementation follows the spec literally, it will add a `'manual'` sentinel value that the TS never had -- a behavioral divergence. Either align the spec to TS (fallback to default on invalid) or explicitly note this as a Go improvement.

4. **A3 -- Model Probe minimum interval**: The 60s floor enforced by TS must be documented in the spec so Go implements the same guard.

5. **D8/D9 -- Channel Recovery batch cap and dual recheck windows**: The recovery scheduler's operational parameters (MAX_BATCH=4, cooldown recheck 30s, active recheck 5min) are critical to its behavior and must be in the spec.

6. **D4 -- Model Probe token-level availability**: The two-table probe architecture (`modelAvailability` vs `tokenModelAvailability`) needs explicit documentation in the spec.

### Recommended additions to spec:

- Document all in-memory state guards: `intervalAttemptByAccount`, `probeAccountLeases`, `recoveryProbeInFlightKeys`, `recoveryProbeLastStartedAtByKey`, `projectionInFlight`, `sub2ApiRefreshPassInFlight`, `recoveryProbeSweepInFlight`
- Document the hot-reload APIs for checkin/balance/log-cleanup cron changes
- Document the snapshot cache invalidation in usage aggregation
- Document the cost calculation fallbacks (summarySpend, siteSpend, modelSpend)
- Document the MySQL/SQLite dialect branching requirement for all upsert operations
- Document the background task integration for model probe
- Document the singleflight pattern for Sub2API refresh
- Document the notification options for daily summary
