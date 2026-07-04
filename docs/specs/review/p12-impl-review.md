# P12 Implementation Review: Background Schedulers (Go)

**Review date**: 2026-07-04
**Spec**: `metapi-go/docs/specs/p12-schedulers.md`
**TS reference**: `metapi/src/server/services/`
**Go implementation**: `metapi-go/scheduler/`
**Review scope**: 15 schedulers, cron infrastructure, settings resolution, test coverage

---

## 1. Overall Verdict

| Dimension | Rating | Notes |
|-----------|--------|-------|
| **Structural fidelity** | GOOD | All 15 schedulers exist as separate Go files. Scheduler interface, Registry, cron runner are all present. |
| **Detail fidelity** | PARTIAL | Most schedulers have correct scaffolding (constants, data structures, in-flight guards) but are ~30-40% stub. 7 of 15 schedulers contain `TODO` stubs for actual service calls. |
| **Test coverage** | GOOD | `scheduler_test.go` has ~1740 lines covering registry, cron, settings helpers, and per-scheduler unit tests. 15 test sections. |
| **Edge cases** | PARTIAL | Core edge cases (panic recovery, clamp, idle-stop) are handled. Some spec edge cases not exercised. |

**Bottom line**: The Go scheduler framework is solid -- interface contract, registry with panic isolation, cron runner with 6-field seconds support, settings resolution with DB-over-env precedence, and comprehensive unit tests. The main gap is that 7 schedulers have TODO-stubbed service integration. This is a truthful "framework done, wiring pending" state.

---

## 2. Scheduler Interface and Registry (scheduler.go)

**Compliant**. Matches spec exactly:
- `Name() string`, `Start(ctx context.Context) error`, `Stop() error`
- Registry with `StartAll` (goroutine-per-scheduler, panic-recover), `StopAll` (error-tolerant), `List`

**One note**: The spec says "Timer 使用 `unref()` (Go 等价) 避免阻塞进程退出". In Go, `time.Ticker`/`time.Timer` do not prevent process exit the way Node.js timers do -- there is no `unref()` equivalent needed. This is implicitly satisfied.

---

## 3. Cron Infrastructure (cron.go)

**Partially compliant**. Uses `robfig/cron/v3` with seconds field (`cron.WithSeconds()`).

**CRITICAL ISSUE**: Cron validation discrepancy between TS and Go.

| Aspect | TS (spec) | Go (current) |
|--------|-----------|-------------|
| Cron format | Standard 5-field (`* * * * *`) | 6-field with seconds (`* * * * * *`) |
| Default cron `0 */6 * * *` | Valid 5-field | **INVALID** in 6-field parser |
| Default cron `58 23 * * *` | Valid 5-field | **INVALID** in 6-field parser |
| Default cron `0 6 * * *` | Valid 5-field | **INVALID** in 6-field parser |

The TS reference uses `node-cron` (5-field), and the spec defines all default cron values as 5-field (`0 */6 * * *`, `58 23 * * *`, `0 6 * * *`). The Go implementation uses `cron.WithSeconds()` which requires 6-field with a leading seconds field. This means **every default cron value defined in the spec would fail `ValidateCronExpr()`**, causing all cron-based schedulers to fall back to defaults that are themselves invalid -- a cascade failure.

The test file confirms this: the test cases explicitly test 6-field expressions like `"* * * * * *"` and mark 5-field expressions like `"* * * * *"` as **invalid**.

**Impact**: All cron-based schedulers will encounter this mismatch. The spec's default values are all 5-field. Either the spec must be updated to define 6-field defaults, or the Go cron parser must drop the seconds requirement.

**Recommendation**: Either:
- (A) Add `"0 "` prefix to all default cron constants in Go scheduler files (e.g., `"0 0 */6 * * *"`) -- simpler, backward-compatible for Go, but diverges from TS format.
- (B) Drop `cron.WithSeconds()` and use standard 5-field parser -- matches TS, but loses sub-minute scheduling capability (which the spec does not use anyway). **Recommended**: this matches TS spec exactly.

---

## 4. Per-Scheduler Detailed Review

### 4.1 CheckinScheduler (checkin.go) -- Scheduler #1

| Requirement | Status | Detail |
|-------------|--------|--------|
| Cron + interval dual mode | PASS | `startLocked()` switches based on `mode` |
| Mode switch stops old scheduler first | PASS | `stopLocked()` called before `startLocked()` |
| Cron mode via `checkin_cron` setting | PASS | `resolveCronSetting("checkin_cron", ...)` |
| Interval mode: 60s poll | PASS | `checkinPollMs = 60_000` |
| Interval SQL filters active accounts with active sites | PASS | Query filters `a.status='active' AND s.status<>'disabled'` |
| `checkinIntervalHours` clamped to [1,24] | PASS | `clampInt(..., 1, 24)` applied |
| `intervalAttemptByAccount` dedup | PASS | `attemptByAccount` map used in `filterDue()` |
| Mid-flight checkin suppression | PASS | `attemptMs >= checkinMs && nowMs-attemptMs < intervalMs` |
| First checkin (no `lastCheckinAt`) treated as due | PASS | `hasCheckin` path handles nil/empty timestamps |
| Hot-reload APIs | PASS | `UpdateCheckinSchedule()`, `ResetAttempts()` |
| `lastCheckinAt` parsing via RFC3339 | MINOR ISSUE | TS uses `Date.parse()` (ISO 8601 flexible). Go uses `time.RFC3339` only -- may reject valid ISO formats like `2026-07-04T12:00:00.000Z`. Fallback is correct (treated as never-checked). |

**Gap**: The `UpdateCheckinCron()` and `UpdateBalanceRefreshCron()` functions defined in the spec as separate hot-reload APIs are handled by `UpdateCheckinSchedule(mode="cron", ...)` which is functionally equivalent. The `UpdateBalanceRefreshCron` is on `BalanceScheduler.UpdateCron()`.

### 4.2 BalanceScheduler (balance.go) -- Scheduler #2

| Requirement | Status | Detail |
|-------------|--------|--------|
| Cron from `balance_refresh_cron` setting | PASS | `resolveCronSetting(...)` with env fallback |
| Invalid cron falls back to config default | PASS | `resolveCronSetting` handles this |
| Execute refresh then rebuild (strict order) | PASS | Code calls `RefreshAllBalances()` then `refreshModelsAndRebuildRoutes()` sequentially |
| Runtime hot-reload via `UpdateCron()` | PASS | Stops old cron, creates new |
| **Route rebuild is a stub** | **GAP** | `refreshModelsAndRebuildRoutes()` is `return nil` -- TODO |

### 4.3 DailySummaryScheduler (daily_summary.go) -- Scheduler #3

| Requirement | Status | Detail |
|-------------|--------|--------|
| Default cron `58 23 * * *` | PASS (5-field issue) | Hardcoded constant; will fail 6-field validation |
| Notification with `{bypassThrottle, requireChannel, throwOnFailure}` | PASS | All three options set to `true` |
| Collect metrics then build notification | PASS | Calls `daily.CollectDailySummaryMetrics` then `daily.BuildDailySummaryNotification` |
| Errors logged, not silently ignored | PASS | `err != nil` branch logs error |

### 4.4 LogCleanupScheduler (log_cleanup.go) -- Scheduler #4

| Requirement | Status | Detail |
|-------------|--------|--------|
| Dual table: proxy_logs + events | PASS | Separate DELETE blocks |
| `logCleanupConfigured` gate | PASS | `runJob()` first checks `cfg.LogCleanupConfigured` |
| "both disabled" gate | PASS | Second check: `!usage && !program` => skip |
| Retention days from setting | PASS | `resolvePositiveIntegerSetting` + clamp to [1,3650] |
| Cutoff = now - retentionDays * 24h | PASS | `now.Add(-Duration(retentionDays) * 24 * Hour)` |
| Hot-reload via `UpdateSettings()` | PASS | Stops old cron, updates config, creates new |

**Spec discrepancy**: The spec says retentionDays goes through `normalizeLogCleanupRetentionDays()`. The Go implementation clamps to [1, 3650] which is a reasonable approximation. The TS `normalizeLogCleanupRetentionDays` is a separate function -- Go inline clamp is equivalent for the range but may differ on the exact normalized value for edge inputs. **Minor**.

**Gap**: The spec's log messages `"Log cleanup skipped: legacy fallback mode is active"` and `"Log cleanup skipped: no log target enabled"` are present with the correct text.

### 4.5 ProxyLogRetentionScheduler (log_retention.go) -- Scheduler #14 (Legacy Fallback)

| Requirement | Status | Detail |
|-------------|--------|--------|
| Only active when `logCleanupConfigured=false` | PASS | `Start()` checks this first |
| Uses `proxyLogRetentionDays` + `proxyLogRetentionPruneIntervalMinutes` | PASS | Default 30min, min 1min |
| Calls `DELETE FROM proxy_logs` | PASS | Same table as log_cleanup's usage path |
| retentionDays=0 disables | PASS | Returns early, `enabled: false` behavior |

### 4.6 BackupWebdavScheduler (backup_webdav.go) -- Scheduler #5

| Requirement | Status | Detail |
|-------------|--------|--------|
| Dual switch: `enabled + autoSyncEnabled` | PASS | Both must be true for cron to start |
| `enabled=false` forces `autoSyncEnabled=false` | PASS | In `loadBackupWebdavConfig()` |
| Cron fallback to `0 */6 * * *` | PASS (5-field issue) | Falls back if invalid |
| HTTP Basic Auth | PASS | `req.SetBasicAuth(cfg.Username, cfg.Password)` |
| 15s timeout | PASS | `backupWebdavFetchTimeout = 15 * time.Second` |
| Export version `"2.1"` | PASS | `backupVersion = "2.1"` |
| State stored in `backup_webdav_state_v1` | PASS | `updateState()` writes to settings |
| `reloadBackupWebdavScheduler()` equivalent | PASS | `Reload()` method does stop-and-recreate |
| Export types: all / accounts / preferences | MINOR ISSUE | The `runExport()` builds payload with `type` key overwritten -- the second `if` branch overwrites the first. Should collect types into an array. |
| WebDAV PUT with JSON body | PASS | `http.MethodPut` with `application/json` |

**Bug**: In `runExport()`, the `payload["type"]` assignment is structured to overwrite:
```go
if cfg.ExportType == "all" || cfg.ExportType == "accounts" {
    payload["type"] = "accounts"
}
if cfg.ExportType == "all" || cfg.ExportType == "preferences" {
    payload["type"] = "preferences"  // OVERWRITES previous value
}
```
For `ExportType: "all"`, the result would be `"type": "preferences"` but should export both. The TS reference stores export types in an array, not a string. This needs correction to match spec's export behavior.

### 4.7 SiteAnnouncementScheduler (site_announcement.go) -- Scheduler #6

| Requirement | Status | Detail |
|-------------|--------|--------|
| 15min interval | PASS | `defaultSiteAnnouncementIntervalMs = 15 * 60 * 1000` |
| Immediate first run | PASS | `go s.runSync()` on start |
| In-flight dedup | PASS | `inFlight` boolean guard |
| **Actual sync logic is a stub** | **GAP** | TODO comment: "wire actual platform-specific announcement sync" |

### 4.8 ModelProbeScheduler (model_probe.go) -- Scheduler #7

| Requirement | Status | Detail |
|-------------|--------|--------|
| `modelAvailabilityProbeEnabled` gate | PASS | Returns early with log |
| 60s hard minimum interval | PASS | `maxInt(cfg.IntervalMs, 60_000)` |
| Account-level lease (`probeAccountLeases`) | PASS | `accountLeases` map + TryAcquire/Release |
| **Account-level vs token-level probes** | **GAP (stub)** | Only queries `accounts WHERE status='active'`; does not load `modelAvailability` rows or `tokenModelAvailability` rows per spec |
| **Background task integration** | **GAP (stub)** | TODO: "Wire actual probe execution via background task system" |
| **Per-model probe failure isolation** | **GAP (stub)** | Not implemented; current stub releases all leases immediately |
| **Post-probe route rebuild** | **GAP (stub)** | No route rebuild trigger |

### 4.9 ChannelRecoveryScheduler (channel_recovery.go) -- Scheduler #8

| Requirement | Status | Detail |
|-------------|--------|--------|
| 30s sweep interval (min 10s) | PASS | Constants defined, `maxInt(...)` floor applied |
| Dual-source candidates (cooldown + active) | PASS | `loadCoolingCandidates()` + `loadActiveCandidates()` |
| Cooling candidates: cooldown_until > now | PASS | SQL filter present |
| **Provider-directed cooldown skip** | **GAP** | TODO comment: "For simplicity, we don't filter here - the TS does deeper filtering" -- fails spec requirement: skip when `failCount<=0 && consecutiveFailCount<=0 && cooldownLevel<=0` |
| Merged candidates: cooldown > active priority | PASS | `mergeCandidates()` has cooling-first semantics |
| In-flight dedup per `(channelId, modelName)` | PASS | `inFlightKeys` + `lastStartedAtByKey` maps |
| Cooldown recheck 30s, active recheck 5min | PASS | `filterDue()` applies correct windows |
| Priority sort: never-probed first, then earliest | PASS | `prioritize()` with bubble sort |
| MAX_BATCH=4 | PASS | `channelRecoveryMaxBatch = 4` |
| Sweep serialization | PASS | `sweepInFlight` boolean guard |
| Immediate first sweep | PASS | `go s.runSweep()` on start |
| **Actual probe execution** | **GAP (stub)** | TODO: "Wire actual probeRuntimeModel call" |
| **recordProbeSuccess on supported** | **GAP (stub)** | Not wired |
| CONCURRENCY=1, TIMEOUT=12s constants | MISSING | Spec defines `CHANNEL_RECOVERY_PROBE_TIMEOUT_MS=12s` and `PROBE_CONCURRENCY=1` as named constants. Go has no timeout or concurrency constant. |
| `loadActiveCandidates` is simplified | MINOR ISSUE | Uses a direct query instead of `proxyChannelCoordinator.getActiveChannelIds()`. Spec says to get IDs from coordinator first. |

### 4.10 Sub2APIRefreshScheduler (sub2api_refresh.go) -- Scheduler #9

| Requirement | Status | Detail |
|-------------|--------|--------|
| 60s interval (min 60s) | PASS | Both constants = 60_000 |
| Concurrency=4 | PASS | Constant defined (unused -- stub) |
| Pass-in-flight dedup | PASS | `passInFlight` boolean guard |
| Immediate first run | PASS | `go s.runPass()` on start |
| Sub2API platform filter | PASS | SQL: `LOWER(TRIM(COALESCE(s.platform, ''))) = 'sub2api'` |
| Active account + active site filter | PASS | SQL: `a.status = 'active' AND s.status = 'active'` |
| **`isManagedSub2ApiTokenDue()` check** | **GAP (stub)** | TODO: "parse extraConfig and check for refreshToken+tokenExpiresAt" |
| **Singleflight dedup** | **GAP (stub)** | TODO: "Wire actual Sub2API refresh via singleflight" |
| **`Sub2ApiRefreshResult` actual values** | **GAP (stub)** | Result hardcoded to 0 refreshed, 0 failed |

### 4.11 UsageAggregationScheduler (usage_aggregation.go) -- Scheduler #11

This is the most complex scheduler and has the most substantial (though still partial) implementation.

| Requirement | Status | Detail |
|-------------|--------|--------|
| 5s interval | PASS | `usageProjectionIntervalMs = 5_000` |
| In-flight dedup | PASS | `projectionInFlight` guard |
| Lease: owner=`hostname:PID`, token=UUID | PASS | `buildLeaseOwner()` + `generateLeaseToken()` |
| Lease: 10min | PASS | `usageProjectionLeaseMs = 600_000` |
| Lease: conditional UPDATE | PASS | `lease_expires_at IS NULL OR lease_expires_at <= ?` |
| Lease acquisition failure = skip (no error) | PASS | Returns nil lease, caller returns `{ProcessedLogs: 0}` |
| Lease release with error recording | PASS | `releaseLease()` accepts optional error for `last_error` |
| Recompute phase | PASS | `applyRecompute()`: finds affectedRow, determines day boundary, deletes aggregates, resets checkpoint |
| Recompute: row not found clears recompute | PASS | Handles scan error as "row deleted" |
| Recompute: unresolvable day = throw | PASS | Returns error: "failed to resolve recompute boundary" |
| BATCH_SIZE=1000, MAX_BATCHES=120 | PASS | Constants defined |
| Batch loop with empty-batch early exit | PASS | Two breaks: empty batch + partial last page |
| Checkpoint `last_proxy_log_id` update | PASS | After each batch |
| **MySQL/SQLite dialect branching** | **GAP** | SQL uses `INSERT` without `ON DUPLICATE KEY UPDATE` (MySQL) or `ON CONFLICT` (SQLite). The upsert logic is not dialect-aware. |
| **3-table upsert** | **GAP** | `site_day_usage` and `site_hour_usage` use plain `INSERT INTO ... VALUES` -- no ON CONFLICT / ON DUPLICATE KEY UPDATE clause. Will fail on duplicate keys. |
| **`model_day_usage` upsert** | **GAP** | No INSERT for model_day_usage at all |
| **clearAnalyticsSnapshots()** | **GAP** | Not called after batch projection or recompute |
| **Spend resolution (3 types)** | **GAP** | `resolveSummarySpend` / `resolveSiteSpend` / `resolveModelSpend` not implemented |
| **localTimeService for day/hour resolution** | **GAP** | Uses `time.Now().UTC()` directly instead of localTimeService per spec |
| **Recompute: find first log of the day** | **GAP** | `applyRecompute()` uses `affectedID - 1` as restart point, but spec says to find the first log of that day (`WHERE createdAt >= dayStartUtc, ORDER BY id ASC`) |
| Recompute: panic recovery with lease release | PASS | `defer/recover` in `runPass()` releases lease |
| POST-condition: error path releases lease | PASS | Inner defer catches `passErr` and releases |

**Critical gap in usage_aggregation.go**: The upsert SQL is plain INSERT, meaning this scheduler will fail SQL constraint violations on the second pass. This is a showstopper for the usage aggregation feature.

### 4.12 AdminSnapshotScheduler (admin_snapshot.go) -- Scheduler #12

| Requirement | Status | Detail |
|-------------|--------|--------|
| 20s interval | PASS | `adminSnapshotWarmIntervalMs = 20_000` |
| Usage aggregation pass runs first | PASS | `s.aggregation.RunProjectionPass()` |
| 4 parallel target warm | PASS | Goroutines with WaitGroup for 4 targets |
| `Promise.allSettled` equivalent | PASS | Individual goroutine errors don't propagate (WaitGroup only waits) |
| Prune every 6 passes | PASS | `passCount % adminSnapshotPruneEvery == 0` |
| In-flight dedup | PASS | `inFlight` boolean guard |
| Immediate first run | PASS | `go s.runWarm()` on start |
| **Actual snapshot warm logic** | **GAP (stub)** | `warmTarget()` only logs; does not regenerate snapshots |
| **`forceRefresh=true`** | **GAP (stub)** | Not passed to warm logic |

### 4.13 ProxyFileRetentionScheduler (file_retention.go) -- Scheduler #13

| Requirement | Status | Detail |
|-------------|--------|--------|
| Interval: config default 60min, min 1min | PASS | `defaultProxyFileRetentionIntervalMin = 60`, `maxInt(..., 1)` |
| retentionDays=0 disables | PASS | Returns early in `Start()` |
| Immediate first run | PASS | `go s.runCleanup()` on start |
| DELETE proxy_files WHERE created_at < cutoff | PASS | SQL with `deleted_at IS NULL` added (extra safety) |

### 4.14 UpdateCenterScheduler (update_center.go) -- Scheduler #10

| Requirement | Status | Detail |
|-------------|--------|--------|
| 15min interval | PASS | Constant defined |
| In-flight dedup | PASS | `inFlight` boolean guard |
| **Actual polling logic** | **GAP (stub)** | TODO: "wire actual update center polling logic" |

### 4.15 OAuthLoopbackScheduler (oauth_loopback.go) -- Scheduler #15

| Requirement | Status | Detail |
|-------------|--------|--------|
| Non-cron, persistent HTTP listener | PASS | Uses `net.Listen` + `http.Server` |
| Multiple provider ports | PASS | 3 providers: Claude (9844), Codex (9845), Gemini CLI (9846) |
| Graceful shutdown with timeout | PASS | 2s context timeout on shutdown |
| **OAuth callback handling** | PARTIAL | Generic handler returns success HTML. TS version delegates to `oauthService` for token exchange -- not wired. |

---

## 5. Settings Resolution (settings.go)

| Function | Status | Detail |
|----------|--------|--------|
| `resolveCronSetting` | PASS | DB read -> JSON unmarshal -> `ValidateCronExpr` -> fallback |
| `resolveBooleanSetting` | PASS | DB read -> JSON unmarshal -> fallback |
| `resolvePositiveIntegerSetting` | PASS | Checks `IsInf`, `IsNaN`, `value >= 1` |
| `resolveJsonSetting` | PASS | Generic JSON resolver |
| `resolveCheckinScheduleMode` | PASS | Specialized for mode string |

All match TS semantics. `resolvePositiveIntegerSetting` correctly uses `math.Trunc()` for non-integer values.

**Note**: The `resolveCronSetting` fallback chain is: DB setting -> validate -> fallback. This matches spec: "如果 DB 中的值不合法, 回退到 config 默认值. 不存在 'manual' 特殊标记".

---

## 6. Test Coverage Assessment

The test file `scheduler_test.go` covers:

| Section | Tests | Status |
|---------|-------|--------|
| Registry | Register, List, StartAll, StopAll, PanicRecovery, ErrorTolerance | Full |
| Cron | ValidateCronExpr, ParseCronExpr, AddJob, RemoveJob, StartStop, PanicRecovery | Full |
| Helpers | clampInt, maxInt, maxInt64, stringsTrimLower, formatErr, toISOTime, formatTimeToSQL | Full |
| Checkin | Constructor, UpdateSchedule (all branches), filterDue (7 subcases), ResetAttempts, Stop, countResults | Good |
| Balance | Constructor, UpdateCron (valid/invalid), Stop | Good |
| ChannelRecovery | Constructor, mergeCandidates (4 subcases), filterDue (6 subcases), prioritize (4 subcases) | Good |
| ModelProbe | Constructor, AccountLease acquire/release/reset (3 subcases), Stop | Good |
| LogCleanup | Constructor, UpdateSettings (4 subcases with clamp) | Good |
| DailySummary | Constructor, Stop | Minimal |
| UsageAggregation | Constructor, buildLeaseOwner, generateLeaseToken, Stop, ProjectionPassResult | Minimal |
| AdminSnapshot | Constructor with usage dep, Stop | Minimal |
| BackupWebdav | isValidHTTPURL, validateBackupWebdavConfig (7 subcases), Constructor | Good |
| OAuthLoopback | intToStr, Constructor, Stop-before-Start | Good |
| ProxyFileRetention | Constructor, Stop | Minimal |
| ProxyLogRetention | Constructor, disabled-by-logcleanup, disabled-by-zero-days, Stop | Good |
| Sub2APIRefresh | Constructor, Stop, Result struct | Minimal |
| UpdateCenter | Constructor, Stop | Minimal |
| SiteAnnouncement | Constructor, Stop | Minimal |
| Interface compliance | All 15 schedulers implement Scheduler, no duplicate names | Full |

**Test gaps** (per spec Test Plan):
- No `balance_test.go` for order-of-operations
- No `usage_aggregation_test.go` for dialect SQL, spend calculation, lease concurrency
- No `admin_snapshot_test.go` for usage pass execution, 4-target warm, prune cycle
- No `model_probe_test.go` for account+token dual layer, per-model failure isolation
- No `sub2api_refresh_test.go` for singleflight, pass-in-flight dedup
- No `lease_test.go` for lease acquire/timeout/concurrent/error-release scenarios

---

## 7. Detailed Gaps by Severity

### BLOCKERS (would prevent correct operation)

1. **Cron format mismatch (5-field vs 6-field)**. All default cron constants are 5-field. Go parser requires 6-field. Every cron-based scheduler will either fail to schedule or fall back to invalid defaults. See Section 3.

2. **Usage aggregation upsert SQL is plain INSERT**. The `INSERT INTO site_day_usage VALUES (...)` / `INSERT INTO site_hour_usage VALUES (...)` in `applyBatch()` will fail on duplicate keys. No `ON CONFLICT`/`ON DUPLICATE KEY UPDATE` clause. The scheduler cannot accumulate usage data over multiple passes.

3. **Usage aggregation missing `model_day_usage` upsert**. The spec requires 3 tables. Only 2 are inserted.

### HIGH (behavioral correctness)

4. **Backup WebDAV `payload["type"]` overwritten**. The `runExport()` function overwrites the `type` field when ExportType is "all", ending up with `"preferences"` only. Should use an array.

5. **Channel recovery missing provider-directed cooldown skip**. The code has a TODO comment acknowledging this gap. Spec requires skipping channels where `cooldownUntil` is set but `failCount <= 0 && consecutiveFailCount <= 0 && cooldownLevel <= 0`.

6. **Channel recovery missing constants** for `PROBE_TIMEOUT_MS=12_000` and `PROBE_CONCURRENCY=1`.

7. **Usage aggregation missing `clearAnalyticsSnapshots()` calls**. Required after each batch and after recompute to invalidate cached snapshots (site-stats, dashboard-summary, dashboard-insights).

8. **Usage aggregation missing MySQL/SQLite dialect branching**. Spec requires `runtimeDbDialect === 'mysql'` detection for correct upsert syntax.

9. **Usage aggregation uses UTC directly instead of localTimeService**. Per spec: "解析 affectedRow 的 localDay + dayStartUtc (通过 localTimeService)".

10. **Usage aggregation recompute uses `affectedID - 1` instead of finding first log of the day**. Spec: "找到该天第一条 proxyLog (WHERE createdAt >= dayStartUtc, ORDER BY id ASC)".

### MEDIUM (missing wiring / stub)

11. **7 scheduler stubs**: UsageAggregation (partial), ModelProbe, ChannelRecovery, Sub2API Refresh, AdminSnapshot, UpdateCenter, SiteAnnouncement. Service calls are TODO-stubbed.

12. **Balance route rebuild is a stub** (`refreshModelsAndRebuildRoutes` returns nil).

13. **Checkin `lastCheckinAt` timestamp parsing is RFC3339-only**. TS uses `Date.parse()` which handles more formats.

14. **Channel recovery `loadActiveCandidates()` does a direct query** instead of going through `proxyChannelCoordinator.getActiveChannelIds()` as specified.

15. **Model probe missing background task integration** (`startBackgroundTask` with dedupe key, notifyOnFailure).

16. **Model probe missing token-level queries** (`tokenModelAvailability` table).

17. **Usage aggregation missing spend resolution functions** (`resolveSummarySpend`, `resolveSiteSpend`, `resolveModelSpend`).

18. **Usage aggregation missing `RequestRecompute` function exposed for external callers**. The `RequestRecompute` method exists but does not implement the `min(existingFromId, fromLogId)` logic from the spec (it overwrites rather than taking the min).

### LOW (minor spec divergence)

19. **Usage aggregation recompute: non-existent recomputeFromId behavior**. Spec: merge with existing (take min). Go: directly overwrites -- could expand recompute range.

20. **Log cleanup retentionDays clamp**: Go uses [1, 3650]; TS `normalizeLogCleanupRetentionDays` may have different normalization logic. Acceptable approximation.

21. **`checkinPollMs` type**: declared as `int64` but `time.NewTicker` takes `time.Duration(int64)` -- this is fine but unusual.

---

## 8. Edge Case Coverage

| Edge Case (from spec) | Status |
|-----------------------|--------|
| Scheduler panic -> recover, not affecting others | PASS (Registry + cronRunner both recover) |
| CHECKIN_CRON empty -> cron.validate fails -> fallback | PASS |
| checkinIntervalHours out of [1,24] -> clamped | PASS |
| intervalAttemptByAccount dedup for mid-flight | PASS |
| Checkin mode switch: stop old, start new | PASS |
| Usage lease acquisition failure -> skip, no error | PASS |
| Usage recompute: log deleted -> clear recompute | PASS |
| Usage recompute: unresolvable day -> throw error | PASS |
| Usage: error catch -> release lease with error | PASS |
| BALANCE_REFRESH_CRON invalid -> fallback | PASS |
| Model probe: enabled=false -> returns early | PASS |
| Model probe: account lease occupied -> skip | PASS (acquire fails) |
| Channel recovery: cooldown > active priority | PASS |
| Channel recovery: sweep serialization | PASS |
| Channel recovery: provider-directed cooldown skip | FAIL (TODO) |
| Sub2API: pass exceeds 60s -> share promise | PASS (passInFlight dedup) |
| Log cleanup: logCleanupConfigured=false -> skip | PASS |
| Log cleanup: cutoff null -> skip (retentionDays <= 0) | PASS (implicit via clamp) |
| Backup WebDAV: enabled=false -> no cron | PASS |
| Backup WebDAV: invalid cron -> fallback | PASS |
| Proxy file/log: retentionDays=0 -> disabled | PASS |
| Admin snapshot: in-flight dedup | PASS |

---

## 9. Summary Assessment

| Count | Category |
|-------|----------|
| 3 | BLOCKERS |
| 7 | HIGH |
| 8 | MEDIUM |
| 3 | LOW |
| 21 | Total gaps |

### Files requiring changes

| File | Severity | Issues |
|------|----------|--------|
| `cron.go` | BLOCKER | 5-field vs 6-field mismatch; all default cron values affected |
| `usage_aggregation.go` | BLOCKER + HIGH | INSERT without upsert, missing model_day_usage, missing clearAnalyticsSnapshots, missing dialect branching, missing spend resolution, localTimeService not used, recompute boundary off-by-design |
| `backup_webdav.go` | HIGH | `payload["type"]` overwritten |
| `channel_recovery.go` | HIGH + MEDIUM | Missing provider cooldown skip, missing TIMEOUT/CONCURRENCY constants, TODO stubs |
| `model_probe.go` | MEDIUM | TODO stubs for actual probe logic, missing token-level queries |
| `sub2api_refresh.go` | MEDIUM | TODO stubs for singleflight and ttl check |
| `balance.go` | MEDIUM | Route rebuild stub |
| `site_announcement.go` | MEDIUM | TODO stub |
| `update_center.go` | MEDIUM | TODO stub |
| `admin_snapshot.go` | MEDIUM | TODO stubs for warm logic |
| `checkin.go` | LOW | RFC3339-only time parsing |

### What is solid

- Scheduler interface and registry with panic isolation
- Cron runner with seconds support (aside from format mismatch)
- Settings resolution (DB-over-env with validation)
- All 15 scheduler files exist with correct structure, constants, and lifecycle management
- Comprehensive unit tests for registry, cron, helpers, and per-scheduler constructor/stop/update paths
- In-flight dedup guards on all interval-based schedulers
- Correct stop-before-start pattern on config reload
- Correct lease-based multi-instance coordination for usage aggregation
- Correct recompute phase design (structure matches spec, individual details need fixing)

### Recommendation

The Go scheduler module is at a solid **scaffold-complete, wiring-incomplete** state. The next pass should:

1. Fix the cron format mismatch (BLOCKER)
2. Fix usage aggregation upsert SQL + dialect branching (BLOCKER)
3. Fix backup WebDAV export payload (HIGH)
4. Wire channel recovery provider cooldown skip (HIGH)
5. Wire remaining 7 TODO-stubbed service calls (MEDIUM)
6. Write the missing test files per spec Test Plan
