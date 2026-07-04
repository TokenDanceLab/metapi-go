# P12: Background Schedulers (15 Schedulers)

**S.U.P.E.R**: S (单一职责) · E (环境无关) | **依赖**: P5 + P6 + P7 + P8 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\checkinScheduler.ts` — 签到/余额/每日汇总/日志清理调度器
- `D:\Code\TokenDance\metapi\src\server\services\backupService.ts` — WebDAV 备份调度 + 导入/导出
- `D:\Code\TokenDance\metapi\src\server\services\siteAnnouncementPollingService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\modelAvailabilityProbeService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\channelRecoveryProbeService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\sub2apiRefreshScheduler.ts`
- `D:\Code\TokenDance\metapi\src\server\services\updateCenterPollingService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\usageAggregationService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\adminSnapshotWarmService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\proxyFileRetentionService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\proxyLogRetentionService.ts` (legacy fallback)
- `D:\Code\TokenDance\metapi\src\server\services\logCleanupService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\dailySummaryService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\localCallbackServer.ts` — OAuth loopback

## Go 模块结构
```
scheduler/
  scheduler.go           # 通用 scheduler 接口 + 注册表
  cron.go                 # robfig/cron 调度器封装
  checkin.go              # CheckinScheduler: 签到 (cron + interval 双模式)
  balance.go              # BalanceScheduler: 余额刷新 + 路由重建
  daily_summary.go        # DailySummaryScheduler: 每日汇总 (23:58)
  log_cleanup.go          # LogCleanupScheduler: 日志清理 (usage + program 双表)
  backup_webdav.go        # WebDAV 备份自动同步 (默认每6小时)
  site_announcement.go    # 站点公告轮询 (15 min)
  model_probe.go          # 模型可用性探测 (account + token 双层)
  channel_recovery.go     # 通道恢复探测 (cooldown + active 双源, 30s sweep)
  sub2api_refresh.go      # Sub2API 托管认证刷新 (60s, concurrency 4, singleflight)
  update_center.go        # Update Center 版本轮询 (15 min)
  usage_aggregation.go    # 用量聚合投影 (5s + 10min lease + recompute)
  admin_snapshot.go       # Admin 快照预热 (20s, 每6次prune)
  file_retention.go       # 代理文件保留清理 (默认60min)
  log_retention.go        # 代理日志保留清理 (legacy fallback, 默认30min)
  oauth_loopback.go       # OAuth Loopback 回调监听器
```

## Scheduler 接口
```go
type Scheduler interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
}
```

所有 scheduler 在 `app.Start` 时注册, 在 `app.Shutdown` 时优雅停止. 单个 scheduler panic 不影响其他 scheduler (每个 scheduler 内部 recover).

---

## 完整调度器清单

| # | Scheduler | 频率 | 配置项 | 依赖服务 | 说明 |
|---|-----------|------|--------|----------|------|
| 1 | **Checkin** | cron: 由 `CHECKIN_CRON` env/DB 决定, 或 interval: 每 60s 轮询 | `checkin_cron`, `checkin_schedule_mode`, `checkin_interval_hours` | checkinService | 批量执行到期账号签到 (cron + interval 双模式) |
| 2 | **Balance Refresh** | cron: 由 `BALANCE_REFRESH_CRON` env/DB 决定 (默认每小时) | `balance_refresh_cron` | balanceService, routeRefreshWorkflow | 刷新余额 → 重建路由 (严格顺序) |
| 3 | **Daily Summary** | `58 23 * * *` (硬编码常量, 可通过 DB 覆盖) | `daily_summary_cron` | dailySummaryService, notifyService | 每日 23:58 收集指标 → 发送通知 (bypassThrottle + requireChannel + throwOnFailure) |
| 4 | **Log Cleanup** | cron: 由 `LOG_CLEANUP_CRON` env/DB 决定 (默认 `0 6 * * *`) | `log_cleanup_cron`, `log_cleanup_*` | logCleanupService | 清理 usage 日志(proxyLogs表) + program 日志(events表), 仅当 `logCleanupConfigured=true` 时执行 |
| 5 | **Backup WebDAV** | cron: DB 中 `autoSyncCron` (默认 `0 */6 * * *`, 每6小时) | `backup_webdav_config_v1` 中的 `autoSyncCron` | backupService | 自动导出备份到 WebDAV (需 enabled + autoSyncEnabled 同时为 true) |
| 6 | **Site Announcements** | 15 min | — | siteAnnouncementService | 轮询所有站点公告 |
| 7 | **Model Probe** | 由 `modelAvailabilityProbeIntervalMs` config 决定 (最小 60s, 默认 30min) | `model_availability_probe_*` | modelAvailabilityProbeService, backgroundTaskService | 探测 account 级 + token 级模型可用性, 通过后台任务系统执行 |
| 8 | **Channel Recovery** | 30s sweep (最小间隔 10s) | — | channelRecoveryProbeService, proxyChannelCoordinator, tokenRouter | 双源: 冷却通道恢复探测(30s recheck) + 活跃通道健康检查(5min recheck), 每 sweep 最多4个候选 |
| 9 | **Sub2API Refresh** | 60s (最小 60s) | — | sub2apiRefreshScheduler | 刷新 sub2api 托管认证/订阅 (concurrency 4, singleflight 去重, 首次立即执行) |
| 10 | **Update Center** | 15 min | — | updateCenterPollingService | 检查新版本 |
| 11 | **Usage Aggregation** | 5s 内部触发, 但通过 lease + in-flight 去重防止重叠 | — | usageAggregationService | 增量投影 proxy_logs→site/model day/hour 用量表, 支持 recompute |
| 12 | **Admin Snapshot** | 20s | — | adminSnapshotWarmService | 预热 4 个 Dashboard/admin 快照 (含 usage projection pass), 每 6 次 prune 过期快照 |
| 13 | **Proxy File Retention** | 由 `PROXY_FILE_RETENTION_PRUNE_INTERVAL_MIN` config 决定 (默认 60min, 最小 1min) | `proxy_file_retention_*` | proxyFileRetentionService | 清理过期代理文件 |
| 14 | **Proxy Log Retention** | 由 `PROXY_LOG_RETENTION_PRUNE_INTERVAL_MIN` config 决定 (默认 30min, 最小 1min) | `proxy_log_retention_*` | proxyLogRetentionService | 清理过期代理日志 (仅当无显式 log_cleanup 设置时作为 fallback 启用) |
| 15 | **OAuth Loopback** | — | — | oauthService | 本地 HTTP 监听器 (非 cron, 常驻) |

---

## 通用模式: 设置解析与 Cron 验证

所有 DB 设置通过 `resolveCronSetting()` / `resolveJsonSetting()` / `resolveBooleanSetting()` / `resolvePositiveIntegerSetting()` 解析, 优先级: DB settings 表 > config (env) 默认值.

**Cron 验证**: 使用 cron 库的 `validate()` 函数验证表达式合法性. 如果 DB 中的值不合法, 回退到 config 默认值. **不存在** `"manual"` 特殊标记——无效的 cron 值始终回退到默认 cron, 不会禁用调度器.

```go
func resolveCronSetting(settingKey string, fallback string) string {
    // 从 DB settings 表读取 → JSON.parse → cron.validate()
    // 有效 → 返回该值; 无效/不存在 → 返回 fallback
}
```

**PositiveInteger 验证**: 值必须是有限数字且 >= 1.

---

## Scheduler 1: Checkin 双模式

### 模式切换

`checkin_schedule_mode` 支持 `"cron"` 和 `"interval"` 两种模式. 切换时先执行 `stopCheckinSchedule()` (清除 cron task + interval timer), 再启动新模式.

### Mode "cron"

- 使用 `checkin_cron` 设置值 (从 DB 或 env config 解析)
- 到时间 → 调用 `CheckinAll({scheduleMode: 'cron'})`
- 日志输出成功/失败数量

### Mode "interval"

- 每 60s (`CHECKIN_INTERVAL_POLL_MS = 60_000`) 执行一次检查
- 查询 SQL:
  ```sql
  SELECT accounts.*, sites.*
  FROM accounts
  INNER JOIN sites ON accounts.siteId = sites.id
  ```
- 在应用层筛选:
  1. `accounts.checkinEnabled === true`
  2. `accounts.status === 'active'`
  3. `sites.status !== 'disabled'`
- 然后调用 `selectDueIntervalCheckinAccountIds()` 进一步筛选:
  - `checkinIntervalHours` 被 clamped 到 `[1, 24]`
  - 对每个候选账号, 检查 `lastCheckinAt` 是否距今超过 `intervalHours` 小时
  - **关键:** 使用内存中的 `intervalAttemptByAccount: Map<accountId, timestamp>` 防止重复提交. 如果账号上一次尝试的时间戳 >= `lastCheckinAt` 且距今不到 `intervalHours`, 则跳过——即使 DB 的 `lastCheckinAt` 还未更新 (mid-flight checkin)
  - 首次签到 (无 `lastCheckinAt`) 的账号: 如果最近 `intervalHours` 内已被尝试过, 跳过
- 对筛选出的 `dueAccountIds` 调用 `CheckinAll({accountIds, scheduleMode: 'interval'})`
- 每次尝试后更新 `intervalAttemptByAccount` 的时间戳

### `checkinIntervalHours` 范围

Clamped 到 `[1, 24]`. `Math.min(24, Math.max(1, value))`. 在 `selectDueIntervalCheckinAccountIds()` 中也做了 `Math.max(1, intervalHours)` 防御.

### 运行时热重载 API

以下函数允许运行时更改调度配置, 无需重启:

| 函数 | 作用 |
|------|------|
| `updateCheckinCron(cronExpr)` | 切换到 cron 模式, 更新 cron 表达式 |
| `updateCheckinSchedule({mode, cronExpr?, intervalHours?})` | 切换模式, 更新参数 (需验证 cron 合法性 + interval [1,24] 范围) |
| `updateBalanceRefreshCron(cronExpr)` | 更新余额刷新 cron (停止旧 task, 创建新 task) |
| `updateLogCleanupSettings({cronExpr?, usageLogsEnabled?, programLogsEnabled?, retentionDays?})` | 更新日志清理设置 |

---

## Scheduler 2: Balance Refresh

- Cron 表达式: `BALANCE_REFRESH_CRON` (从 env config 读取默认值, 可被 DB `balance_refresh_cron` 覆盖)
- 默认值不是 scheduler 硬编码的——来自 `config.balanceRefreshCron` (env default)
- 执行流程 (严格顺序):
  1. `refreshAllBalances()` — 刷新所有账号余额
  2. `routeRefreshWorkflow.refreshModelsAndRebuildRoutes()` — 重建路由
- **Edge case**: 如果 DB 中的 `balance_refresh_cron` 值无效 (cron.validate 失败), 回退到 config 默认值. **不存在** `"manual"` 特殊值来禁用调度器.

---

## Scheduler 3: Daily Summary

- 默认 cron: `"58 23 * * *"` (硬编码常量, 可通过 DB `daily_summary_cron` 覆盖)
- 执行流程:
  1. `collectDailySummaryMetrics()` — 收集指标
  2. `buildDailySummaryNotification(metrics)` — 构建通知 (title + message)
  3. `sendNotification(title, message, 'info', { bypassThrottle: true, requireChannel: true, throwOnFailure: true })`
- 通知选项含义:
  - `bypassThrottle: true` — 绕过限流, 确保每日汇总总是发送
  - `requireChannel: true` — 要求已配置通知渠道
  - `throwOnFailure: true` — 失败时抛异常, 让 cron task 感知到错误

---

## Scheduler 4: Log Cleanup

### 双类型日志清理

| 类型 | 目标表 | 函数 |
|------|--------|------|
| Usage 日志 | `proxyLogs` | `cleanupUsageLogs(retentionDays)` |
| Program 日志 | `events` | `cleanupProgramLogs(retentionDays)` |

### 执行门控

1. `config.logCleanupConfigured === false` → 跳过, 日志 `"Log cleanup skipped: legacy fallback mode is active"`
2. 如果两种日志类型都未启用 (`usageLogsEnabled=false && programLogsEnabled=false`) → 跳过, 日志 `"Log cleanup skipped: no log target enabled"`

### 保留天数计算

- `retentionDays` 通过 `normalizeLogCleanupRetentionDays()` 规范化
- `cutoffUtc = now - retentionDays * 24h`, 格式化为 UTC SQL datetime
- 如果 `cutoffUtc` 为 null (例如 retentionDays 无效), 返回 `deleted: 0` 不执行 DELETE

### 清理 SQL

```sql
DELETE FROM proxyLogs WHERE createdAt < cutoffUtc;   -- cleanupUsageLogs
DELETE FROM events    WHERE createdAt < cutoffUtc;   -- cleanupProgramLogs
```

### 设置项

| DB Setting Key | 类型 | 说明 |
|----------------|------|------|
| `log_cleanup_cron` | string (cron) | 清理 cron, 默认 `0 6 * * *` (来自 `LOG_CLEANUP_CRON` env 或常量) |
| `log_cleanup_usage_logs_enabled` | boolean | 是否清理 usage 日志 |
| `log_cleanup_program_logs_enabled` | boolean | 是否清理 program 日志 |
| `log_cleanup_retention_days` | number (>=1) | 保留天数 |

### Proxy Log Retention (Legacy Fallback)

`proxyLogRetentionService` 是一个独立于 `log_cleanup` 配置的备用清理路径:

- 当无显式 `log_cleanup` 配置时 (`logCleanupConfigured=false`), 此 fallback 路径启用
- 使用 `config.proxyLogRetentionDays` + `config.proxyLogRetentionPruneIntervalMinutes` (默认 30min)
- 调用 `cleanupUsageLogs()` 清理 `proxyLogs` 表
- 通过 `setLegacyProxyLogRetentionFallbackEnabled(enabled)` 控制启用/禁用

---

## Scheduler 5: Backup WebDAV

- 配置存储在 DB `backup_webdav_config_v1` setting (JSON)
- 配置结构:
  ```json
  {
    "enabled": true,
    "fileUrl": "https://dav.example.com/backup.json",
    "username": "user",
    "password": "secret",
    "exportType": "all",
    "autoSyncEnabled": true,
    "autoSyncCron": "0 */6 * * *"
  }
  ```
- 默认 `autoSyncCron`: `"0 */6 * * *"` (每 6 小时)
- 启动条件: `enabled === true` AND `autoSyncEnabled === true` AND 配置通过 `validateBackupWebdavConfig()` 校验
- 如果 `enabled` 被设为 false, `autoSyncEnabled` 自动设为 false
- WebDAV 请求: HTTP PUT (导出) / GET (导入), Basic Auth, 超时 15s (`BACKUP_WEBDAV_FETCH_TIMEOUT_MS = 15_000`)
- `reloadBackupWebdavScheduler()`: 停止旧 cron task → 读取配置 → 创建新 cron task
- 导出类型: `all` | `accounts` | `preferences`
- 导出数据版本: `"2.1"`
- 运行时状态 (同步时间、错误) 存储在 DB `backup_webdav_state_v1` setting
- Cron task 执行 `exportBackupToWebdav(config.exportType)`, 错误通过 console.warn 记录

---

## Scheduler 7: Model Probe

### 双层探测架构

**Account 级** (`modelAvailability` 表):
- 查询每个 active 账号的所有非手动 (`isManual !== true`) model availability 行
- 按 `checkedAt` ASC 排序 (最久未检查的优先)

**Token 级** (`tokenModelAvailability` 表):
- 查询每个 active 账号的可用 token (`accountTokens.enabled=true, valueStatus='READY'`)
- 对每个符合条件的 token, 获取其 `tokenModelAvailability` 行
- Token 值必须有效 (`isUsableAccountToken()` + token 非空)
- 按 `checkedAt` ASC 排序

### 执行模式

- 启用门控: `config.modelAvailabilityProbeEnabled === false` → scheduler 返回 `{enabled: false, intervalMs: 0}`, 不启动 timer
- 间隔: `config.modelAvailabilityProbeIntervalMs`, **硬性最小值 `Math.max(60_000, intervalMs)`** (60 秒 floor), 默认 30min (来自 config 文件, 非 scheduler 常量)
- 并发: `config.modelAvailabilityProbeConcurrency` (worker 数量)

### 账号级并发控制

- 内存中的 `probeAccountLeases: Set<accountId>` 防止同一账号被同时探测
- 如果账号 X 的探测已在运行, 新请求返回 `skipped` 状态, 消息: `"model availability probe already running for account"`

### 后台任务集成

Scheduler 不直接调用 `executeModelAvailabilityProbe()`, 而是通过 `queueModelAvailabilityProbeTask()`:

```
queueModelAvailabilityProbeTask() → startBackgroundTask({
    type: 'model-probe',
    dedupeKey: 'model-availability-probe-all' (或 'model-availability-probe-{accountId}'),
    notifyOnFailure: true,
    successMessage: ...,
    failureMessage: ...,
}) → executeModelAvailabilityProbe({accountId?, rebuildRoutes: true})
```

Dedupe key 防止相同范围的探测任务重复排队.

### 探测流程 (per account)

```
1. loadActiveProbeAccountContext(accountId) → 验证 account + site 均为 active
2. tryAcquireProbeAccountLease(accountId) → 如果失败 → skipped
3. loadProbeTargetsForAccount(context) → 返回 account 级 + token 级 targets
4. mapWithConcurrency(targets, concurrency, per-target):
   a. probeSingleTarget(target) → probeRuntimeModel({site, account, modelName, tokenValue?, timeoutMs})
   b. updateProbeRow(target, status, latencyMs):
      - inconclusive/skipped → 不更新
      - supported/unsupported → UPDATE modelAvailability 或 tokenModelAvailability
      - 返回 {touched, availabilityChanged}
5. 汇总结果:
   - 任一 per-model probe 抛出异常 → caught, 标记为 inconclusive
   - 如果整个 account 有任意 failure → account status = 'failed'
   - 否则 → account status = 'success'
   - 如果任何 probe 出现 availabilityChanged → shouldRebuildRoutes = true
6. releaseProbeAccountLease(accountId)
```

### 探测后路由重建

如果 `shouldRebuildRoutes === true` (且 `rebuildRoutes !== false`), 调用 `routeRefreshWorkflow.rebuildRoutesOnly()` 重建路由.

### 结果摘要

```go
type ModelAvailabilityProbeExecutionResult struct {
    Results []AccountResult  // per-account
    Summary struct {
        TotalAccounts, Success, Failed, Skipped int
        Scanned, Supported, Unsported, Inconclusive, SkippedModels int
        UpdatedRows int
        RebuiltRoutes bool
    }
}
```

---

## Scheduler 8: Channel Recovery

### 双源候选池

**冷却通道候选** (`loadCoolingProbeCandidates()`):
- 查询 `routeChannels` WHERE `enabled=true`, `cooldownUntil IS NOT NULL`, `cooldownUntil > now`
- JOIN `accounts` (status=active) + `sites` (status=active) + `tokenRoutes` + `accountTokens` (LEFT)
- **Provider-directed cooldown 跳过**: 如果 `cooldownUntil` 已设置但 `failCount <= 0 && consecutiveFailCount <= 0 && cooldownLevel <= 0`, 视为上游 provider 标记的冷却——**跳过, 不探测**
- 必须有可解析的 `modelName` 和 `tokenValue` 才算有效候选

**活跃通道候选** (`loadActiveProbeCandidates()`):
- 从 `proxyChannelCoordinator.getActiveChannelIds()` 获取当前活跃通道 ID 列表
- 查询这些 ID 对应的 `routeChannels` (同样 JOIN accounts/sites/tokenRoutes/accountTokens)
- 必须有可解析的 `modelName` 和 `tokenValue`

### 常量

| 常量 | 值 | 说明 |
|------|-----|------|
| `CHANNEL_RECOVERY_SWEEP_INTERVAL_MS` | 30_000 (30s) | Sweep 主间隔 (最小 10s) |
| `CHANNEL_RECOVERY_PROBE_TIMEOUT_MS` | 12_000 (12s) | 单次探测超时 |
| `CHANNEL_RECOVERY_PROBE_CONCURRENCY` | 1 | 探测并发数 |
| `CHANNEL_RECOVERY_MAX_BATCH` | 4 | 每 sweep 最多探测候选数 |
| `CHANNEL_RECOVERY_COOLDOWN_RECHECK_MS` | 30_000 (30s) | 冷却候选重新探测窗口 |
| `CHANNEL_RECOVERY_ACTIVE_RECHECK_MS` | 5 * 60_000 (5min) | 活跃候选重新探测窗口 |

### 候选合并与优先级

`mergeRecoveryProbeCandidates()`:
- 按 `channelId` 去重
- 如果同一 channel 同时出现在冷却和活跃候选池中, **冷却候选优先** (`cooldown > active`)
- 这确保正在冷却的通道不会被活跃通道的更长 recheck 窗口覆盖

### 探测门控

`shouldProbeCandidate(candidate)`:
- 检查 `recoveryProbeInFlightKeys` — 如果该 `(channelId, modelName)` 组合正在探测中, 跳过
- 检查 `recoveryProbeLastStartedAtByKey` — 距上次探测不到 recheck 窗口时间, 跳过
  - 冷却候选: 30s recheck 窗口
  - 活跃候选: 5min recheck 窗口

### 优先级排序

`compareRecoveryProbeCandidatePriority()`:
1. 从未探测过的候选优先 (lastStartedAt == null 排最前)
2. 最早探测的优先 (升序)
3. channelId 升序作为 tiebreaker

### 探测执行

`runRecoveryProbeCandidate()`:
1. 加入 `recoveryProbeInFlightKeys`, 记录 `recoveryProbeLastStartedAtByKey`
2. 调用 `probeRuntimeModel({site, account, modelName, tokenValue, timeoutMs: 12_000})`
3. **如果 `result.status === 'supported'`**: 调用 `tokenRouter.recordProbeSuccess(channelId, latencyMs, modelName)` 记录恢复成功
4. 最后从 `recoveryProbeInFlightKeys` 移除

### Sweep 串行化

`runChannelRecoveryProbeSweep()`:
- `recoveryProbeSweepInFlight: Promise<void> | null` — 如果上一个 sweep 仍在运行, 等待其完成
- 先发起 `loadCoolingProbeCandidates()` 和 `loadActiveProbeCandidates()` 并行加载
- 合并 → 过滤 → 排序 → 截取前 `MAX_BATCH(=4)` → 并发探测
- `finally` 中清除 `recoveryProbeSweepInFlight`

### 调度器启动

`startChannelRecoveryProbeScheduler()`:
- 间隔: `Math.max(10_000, intervalMs)` (最小 10 秒)
- 启动后**立即执行一次首次 sweep**
- Timer 使用 `unref()` 避免阻塞进程退出

### 内存状态

| 变量 | 类型 | 说明 |
|------|------|------|
| `recoveryProbeInFlightKeys` | `Set<string>` | 正在探测的 `(channelId, modelName)` key |
| `recoveryProbeLastStartedAtByKey` | `Map<string, number>` | 每个 key 上次探测开始时间戳 |
| `recoveryProbeSweepInFlight` | `Promise<void> \| null` | 当前 sweep 的 promise |

---

## Scheduler 9: Sub2API Refresh

### 目标筛选

只处理 Sub2API 平台的 active 账号:
- `site.platform.toLowerCase() === 'sub2api'`
- `account.status === 'active'` AND `site.status === 'active'`
- 账号的 `extraConfig` 中有 `sub2apiAuth` 且包含 `refreshToken` + `tokenExpiresAt`
- `isManagedSub2ApiTokenDue(tokenExpiresAt, nowMs)` 为 true (token 过期或即将过期)

### 执行参数

| 参数 | 值 | 说明 |
|------|-----|------|
| 间隔 | 60_000ms (60s, 最小 60s) | `SUB2API_REFRESH_SCHEDULER_INTERVAL_MS` |
| 并发 | 4 | `SUB2API_REFRESH_SCHEDULER_CONCURRENCY` |

### Singleflight 去重

每个候选调用 `refreshSub2ApiManagedSessionSingleflight()`:
- 如果多个并发 pass 同时刷新同一账号, 只执行一次实际刷新, 其他调用共享结果
- 防止对同一账号的重复刷新请求

### Pass 串行化

- `sub2ApiRefreshPassInFlight: Promise<void> | null` — 如果上一 pass 仍在运行, 新触发直接 await 同一 promise
- Pass 完成后在 `finally` 中清除该变量

### 立即首次执行

`startSub2ApiManagedRefreshScheduler()` 启动时立即调用 `void runScheduledSub2ApiRefreshPass()`.

### 结果

```go
type Sub2ApiRefreshResult struct {
    Scanned             int      // 扫描的候选账号总数
    Refreshed           int      // 成功刷新数
    Failed              int      // 失败数
    Skipped             int      // 跳过数 (token 未到期等)
    RefreshedAccountIds []int
    FailedAccountIds    []int
}
```

---

## Scheduler 11: Usage Aggregation

### 核心流程

每 5s 触发一次 (`PROJECTION_INTERVAL_MS = 5_000`), 但通过 lease + in-flight 去重确保不会重叠执行.

```
每 5 秒触发:
  1. in-flight 去重: 如果已有 projection 在运行, 返回同一个 promise
  2. 尝试获取租约 (10 min lease, owner=hostname:PID, token=randomUUID)
     - 租约获取: UPDATE ... WHERE leaseExpiresAt IS NULL OR leaseExpiresAt <= now
     - 如果 changes=0 (被其他实例持有) → 跳过本轮, 返回 {processedLogs: 0}
  3. 读取 checkpoint (含 recompute 检查)
  4. [RECOMPUTE PHASE] 如果 checkpoint.recomputeFromId > 0:
     a. 查找 affectedRow (proxyLogs WHERE id >= recomputeFromId, ORDER BY id ASC)
     b. 如果 affectedRow 不存在 (log 已被删除): 清除 recompute 标记, 直接进入正常投影
     c. 解析 affectedRow 的 localDay + dayStartUtc (通过 localTimeService)
        - 如果无法解析 → throw "Failed to resolve recompute boundary for usage aggregates"
     d. 找到该天第一条 proxyLog (WHERE createdAt >= dayStartUtc, ORDER BY id ASC)
     e. 在事务中:
        - DELETE FROM siteDayUsage WHERE localDay >= affectedDay
        - DELETE FROM siteHourUsage WHERE bucketStartUtc >= dayStartUtc
        - DELETE FROM modelDayUsage WHERE localDay >= affectedDay
        - 重置 lastProxyLogId = restartFromId - 1
        - 清除所有 recompute 字段
     f. clearAnalyticsSnapshots()
  5. [NORMAL PROJECTION] 循环最多 120 batches:
     a. fetchProjectionBatch(lastProxyLogId, BATCH_SIZE=1000)
        - 从 proxyLogs JOIN accounts JOIN sites 读取
        - WHERE proxyLogs.id > lastProxyLogId ORDER BY id ASC LIMIT 1000
     b. 如果 rows.length <= 0 → 退出循环
     c. buildProjectionBatchDelta(rows) → 构建 3 种 delta:
        - siteDayDelta (key: localDay:siteId)
        - siteHourDelta (key: bucketStartUtc:siteId)
        - modelDayDelta (key: localDay:siteId:model)
     d. 在事务中:
        - upsertSiteDayUsage(tx, deltaRows)
        - upsertSiteHourUsage(tx, deltaRows)
        - upsertModelDayUsage(tx, deltaRows)
        - writeProjectionCheckpoint(tx, nextCheckpoint)
        - 每个 upsert 都是 ON DUPLICATE KEY UPDATE ... + VALUES (累加)
     e. clearAnalyticsSnapshots() 清除 'site-stats', 'dashboard-summary', 'dashboard-insights'
     f. 如果 rows.length < BATCH_SIZE (最后一页不完整) → 退出循环 (避免额外空查询)
  6. releaseProjectionLease(lease) — 正常路径
  7. 如果发生异常: releaseProjectionLease(lease, {error}) — catch 中先释放再 rethrow
```

### Recompute 触发

```go
requestUsageAggregatesRecompute(fromLogId = 1):
  1. 读取当前 checkpoint
  2. 如果有现有 recomputeFromId, 取 min(existingFromId, fromLogId) (不扩大范围)
  3. 在事务中更新 checkpoint 的 recompute 字段:
     - recomputeFromId = nextFromId
     - recomputeRequestedAt = now
  4. 下一次 projection pass 会在步骤 4 自动执行 recompute
```

### 聚合维度与字段

**siteDayUsage**: `(localDay, siteId)` unique key
- totalCalls, successCalls, failedCalls
- totalTokens
- totalSummarySpend, totalSiteSpend
- totalLatencyMs, latencyCount

**siteHourUsage**: `(bucketStartUtc, siteId)` unique key
- 与 siteDayUsage 相同字段

**modelDayUsage**: `(localDay, siteId, model)` unique key
- totalCalls, successCalls, failedCalls
- totalTokens, totalSpend
- totalLatencyMs, latencyCount

### 成本计算 (Spend Resolution)

三种不同的 spend 计算方式, 同时应用到对应的聚合表:

| 函数 | 适用表 | 逻辑 |
|------|--------|------|
| `resolveSummarySpend` | siteDay + siteHour | 有 explicit cost>0 → 用 explicit; 否则 Veloera 平台: `tokens/1M`, 其他: `tokens/500K` |
| `resolveSiteSpend` | siteDay + siteHour | 有 explicit cost>0 → 用 explicit; 否则 `fallbackTokenCost(tokens, platform)` |
| `resolveModelSpend` | modelDay | 有 explicit cost>0 → 用 explicit; 否则 `tokens/500K` |

### 租约格式

- `leaseOwner`: `"{hostname}:{pid}"` (例如 `myhost:12345`)
- `leaseToken`: `randomUUID()`
- `leaseExpiresAt`: `now + 10min` ISO string
- 获取: conditional UPDATE (仅当 leaseExpired 或 null)
- 释放: UPDATE ... SET leaseOwner=null, leaseToken=null, leaseExpiresAt=null, lastError=? WHERE leaseToken=?

### 批量参数

| 参数 | 值 | 说明 |
|------|-----|------|
| `PROJECTION_BATCH_SIZE` | 1,000 | 每批读取行数 |
| `PROJECTION_MAX_BATCHES_PER_PASS` | 120 | 每 pass 最多批次数 (120,000 行上限) |
| `PROJECTION_INTERVAL_MS` | 5,000 | 间隔 5s |
| `PROJECTION_LEASE_MS` | 600,000 | 租约 10min |

### MySQL / SQLite 方言分支

所有 4 个 upsert 操作 (+ checkpoint write) 必须按方言分支:

- **MySQL**: `INSERT INTO ... VALUES ... ON DUPLICATE KEY UPDATE col = col + VALUES(col)`
- **SQLite**: `INSERT INTO ... VALUES ... ON CONFLICT(target) DO UPDATE SET col = col + excluded.col`

Go 实现需使用相同的方言检测 (`runtimeDbDialect === 'mysql'`) 来构建正确的 SQL.

### 快照缓存失效

`clearAnalyticsSnapshots()` 在每次 batch 和 recompute 后清除以下快照的缓存:
- `site-stats`
- `dashboard-summary`
- `dashboard-insights`

这确保下一次读取这些快照时会用最新数据重新计算.

### 内存状态

| 变量 | 类型 | 说明 |
|------|------|------|
| `projectionInFlight` | `Promise<ProjectionPassResult> \| null` | 去重: 如果已有投影 pass 在运行, 返回同一 promise |

---

## Scheduler 12: Admin Snapshot Warm

### 执行流程

每 20s (`ADMIN_SNAPSHOT_WARM_INTERVAL_MS = 20_000`):
1. 先调用 `runUsageAggregationProjectionPass()` — 确保用量数据是最新的
2. 并行预热 4 个快照目标 (forceRefresh=true):
   - `dashboard-summary`
   - `accounts-snapshot`
   - `site-stats` (days=7)
   - `dashboard-insights`
3. 使用 `Promise.allSettled` — 单个快照失败不影响其他
4. `completedWarmPassCount += 1`
5. 每 6 次 pass (`ADMIN_SNAPSHOT_PRUNE_EVERY_PASSES = 6`, 即每 2 分钟) — 调用 `deleteExpiredAdminSnapshots()`

### In-flight 去重

`adminSnapshotWarmInFlight: Promise<void> | null` — 如果已有 warm 在运行, 返回同一 promise.

### 立即首次执行

`startAdminSnapshotWarmScheduler()` 启动时立即调用 `void warmAdminSnapshotsOnce()`.

---

## Scheduler 13: Proxy File Retention

- 间隔: `config.proxyFileRetentionPruneIntervalMinutes` (默认 60min, 最小 1min)
- 保留天数: `config.proxyFileRetentionDays`
- 如果 retentionDays <= 0 → `cutoffUtc = null` → `enabled: false, deleted: 0`
- 调用 `purgeExpiredProxyFiles(cutoffUtc)` 执行清理
- 启动时立即执行首次清理

---

## Scheduler 10 & 6 & Others

### Update Center (Scheduler 10)
- 15min 间隔轮询更新中心 API

### Site Announcements (Scheduler 6)
- 15min 间隔轮询所有站点公告

### OAuth Loopback (Scheduler 15)
- 本地 HTTP 监听器, 非 cron, 常驻服务
- 处理 OAuth 回调

---

## Acceptance Criteria
- [ ] 15 个 scheduler 全部实现并在 app.Start 时注册
- [ ] Checkin 支持 cron + interval 双模式, 含 `intervalAttemptByAccount` 去重
- [ ] Checkin interval 模式下筛选 status=active AND site.status!=disabled
- [ ] Checkin `checkinIntervalHours` clamped 到 [1, 24]
- [ ] Checkin 热重载 API (updateCheckinCron/updateCheckinSchedule/updateBalanceRefreshCron/updateLogCleanupSettings)
- [ ] Balance refresh: `refreshAllBalances()` 必须在 `rebuildRoutes()` 之前
- [ ] Daily summary notification 使用 `{bypassThrottle, requireChannel, throwOnFailure}`
- [ ] Log cleanup 区分 `proxyLogs` (usage) 和 `events` (program) 两种类型
- [ ] Log cleanup 由 `logCleanupConfigured` 门控, 未配置时跳过
- [ ] Log cleanup `getLogCleanupCutoffUtc()` 返回 null 时安全跳过
- [ ] Proxy log retention legacy fallback 路径独立于 `log_cleanup` 配置
- [ ] Backup WebDAV 需 `enabled + autoSyncEnabled` 双开关
- [ ] Backup WebDAV cron 解析失败时回退到默认值 `0 */6 * * *`
- [ ] Model probe: account 级 + token 级双层探测
- [ ] Model probe: `probeAccountLeases` 防止同一账号并发探测
- [ ] Model probe: 60s 硬性最小间隔 (`Math.max(60_000, intervalMs)`)
- [ ] Model probe: 通过 `startBackgroundTask` 后台任务系统执行 (含 dedup key)
- [ ] Model probe: per-model 探测失败不影响其他 model (标记为 inconclusive)
- [ ] Model probe: availabilityChanged 时触发 `rebuildRoutesOnly()`
- [ ] Channel recovery: 双源候选 (冷却通道 + 活跃通道)
- [ ] Channel recovery: `isProviderDirectedCooldown()` 跳过 provider 级冷却
- [ ] Channel recovery: 冷却候选 30s recheck, 活跃候选 5min recheck
- [ ] Channel recovery: MAX_BATCH=4, PROBE_CONCURRENCY=1, TIMEOUT=12s
- [ ] Channel recovery: `recoveryProbeSweepInFlight` 串行化 + 首次立即执行
- [ ] Channel recovery: 成功探测后调用 `tokenRouter.recordProbeSuccess()`
- [ ] Channel recovery: 间隔 floor `Math.max(10_000, intervalMs)`
- [ ] Sub2API refresh: singleflight 去重 + pass-in-flight 串行化
- [ ] Sub2API refresh: concurrency=4, 首次立即执行
- [ ] Usage aggregation: lease owner 格式 `hostname:PID`, token=UUID
- [ ] Usage aggregation: recompute 机制 (clear + re-project from day boundary)
- [ ] Usage aggregation: 3 种 spend 计算 (summary/site/model)
- [ ] Usage aggregation: MySQL/SQLite 方言分支 (所有 upsert + checkpoint write)
- [ ] Usage aggregation: 每 batch 后 `clearAnalyticsSnapshots()`
- [ ] Usage aggregation: 租约错误时在 catch 中先释放再 rethrow
- [ ] Usage aggregation: 空 batch 早退 + 末页不完整早退
- [ ] Admin snapshot: 每 6 次 pass prune + 首次立即执行
- [ ] Proxy file/log retention: retentionDays=0 时 enabled=false
- [ ] 所有 cron 表达式通过 `cron.validate()` 验证, 无效值回退到默认值 (不存在 "manual" 特殊值)
- [ ] 所有 scheduler 在 app.Shutdown 时优雅停止 (等待 in-flight promise 完成)
- [ ] 单个 scheduler 启动失败不阻塞主服务
- [ ] Timer 使用 `unref()` (Go 等价) 避免阻塞进程退出

## Test Plan
| 文件 | 内容 |
|------|------|
| `scheduler/checkin_test.go` | cron + interval 切换, intervalAttemptByAccount 去重, 状态筛选, intervalHours 边界 |
| `scheduler/balance_test.go` | 余额刷新 → 路由重建顺序, 无效 cron 回退 |
| `scheduler/daily_summary_test.go` | 通知选项传递 |
| `scheduler/log_cleanup_test.go` | logCleanupConfigured 门控, null cutoff, 双表清理 |
| `scheduler/backup_webdav_test.go` | enabled+autoSyncEnabled 双开关, cron 回退 |
| `scheduler/model_probe_test.go` | account+token 双层, accountLease 去重, per-model 失败, rebuildRoutes 触发 |
| `scheduler/channel_recovery_test.go` | 双源候选, provider cooldown 跳过, maxBatch, 串行化 |
| `scheduler/sub2api_refresh_test.go` | singleflight, pass-in-flight, 首次立即执行 |
| `scheduler/usage_aggregation_test.go` | recompute, 方言 SQL, spend 计算, lease 并发, 快照失效 |
| `scheduler/admin_snapshot_test.go` | usage pass 执行, 4 目标预热, 每6次 prune |
| `scheduler/lease_test.go` | 租约获取/超时/并发/错误释放 |
| `scheduler/registry_test.go` | 注册/启动/停止/panic 隔离 |

## Edge Cases
- 调度器 panic → recover, 不影响其他调度器
- CHECKIN_CRON 为空字符串 → `cron.validate('')` 失败, 回退到 config 默认值
- Checkin interval: `checkinIntervalHours` 超出 [1,24] 范围 → clamped
- Checkin interval: 同一账号在 interval 窗口内被重复轮询 → `intervalAttemptByAccount` 去重阻止
- Checkin 模式切换: stop 清除 cron+interval, 再 start 新模式
- Usage aggregation lease 获取失败 → 跳过本轮 (返回 processedLogs=0, 不报错)
- Usage aggregation: recompute 引用的 log 已不存在 → 清除 recompute 标记, 继续正常投影
- Usage aggregation: recompute affectedRow 的 dayStartUtc 无法解析 → throw error
- Usage aggregation: 租约释放时带上 error (catch 块) → 记录到 checkpoint.lastError
- BALANCE_REFRESH_CRON 无效 → `cron.validate()` 失败, 回退到 `config.balanceRefreshCron` (不存在 "manual" 禁用)
- Model probe: `modelAvailabilityProbeEnabled=false` → scheduler 返回 `{enabled:false}`, 不启动 timer
- Model probe: 单个 model probe 抛出异常 → caught, 标记为 inconclusive, 继续探测其他 models
- Model probe: account 级 lease 已被占用 → 返回 skipped 状态
- Channel recovery: cooldown 和 active 候选有重叠 → cooldown 优先
- Channel recovery: sweep 耗时超过 30s → 后续 sweep 排队等待
- Channel recovery: `cooldownUntil` 存在但 failCount/consecutiveFailCount/cooldownLevel 均为 0 → 跳过 (provider-directed)
- Sub2API refresh: pass 耗时超过 60s → 后续 pass 共享同一 promise
- Log cleanup: `logCleanupConfigured=false` → 整个 task 跳过 (legacy fallback 路径)
- Log cleanup: `getLogCleanupCutoffUtc()` 返回 null → 返回 deleted=0, 不执行 DELETE
- Log cleanup: 两种日志均未启用 → 跳过并记录日志
- Backup WebDAV: `enabled=false` 或 `autoSyncEnabled=false` → 不启动 cron
- Backup WebDAV: cron 表达式无效 → 回退到 `0 */6 * * *`
- Proxy file/log retention: retentionDays=0 → enabled=false, 不执行清理
- Admin snapshot: in-flight 去重 → 同一时间只有一个 warm pass 运行
