# P5: Checkin + Balance Refresh + Notify Services

**S.U.P.E.R**: S (单一职责) · R (可替换) | **依赖**: P3 + P4 | **Size**: M

## 原始 TS 参考
- `<metapi-ts>\src\server\services\checkinService.ts`
- `<metapi-ts>\src\server\services\checkinRewardParser.ts`
- `<metapi-ts>\src\server\services\balanceService.ts`
- `<metapi-ts>\src\server\services\todayIncomeRewardService.ts`
- `<metapi-ts>\src\server\services\notifyService.ts`
- `<metapi-ts>\src\server\services\notificationThrottle.ts`
- `<metapi-ts>\src\server\services\alertService.ts` + `alertRules.ts`
- `<metapi-ts>\src\server\services\dailySummaryService.ts`
- `<metapi-ts>\src\server\services\failureReasonService.ts`

## Go 模块结构
```
service/
  checkin/
    checkin.go           # CheckinAccount + CheckinAll
    reward_parser.go     # 解析签到奖励消息
    failure_reason.go    # 失败原因分类
  balance/
    balance.go           # RefreshBalance + RefreshAllBalances
    today_reward.go      # 今日签到奖励汇总 (todayIncomeSnapshot)
  notify/
    notify.go            # SendNotification (调度器)
    webhook.go           # Webhook 通知通道 (含 WeCom/Feishu bot 检测)
    bark.go              # Bark 通知通道
    serverchan.go        # Server酱 通知通道
    telegram.go          # Telegram Bot 通知通道
    smtp.go              # SMTP 邮件通知通道
    throttle.go          # 通知冷却 (cooldown + state pruning)
  alert/
    alert.go             # AlertService (reportTokenExpired, reportProxyAllFailed)
    rules.go             # AlertRules 匹配引擎 (isTokenExpiredError, isCloudflareChallenge, appendSessionTokenRebindHint)
  daily_summary.go       # 每日汇总 (定时 23:58, collectDailySummaryMetrics + buildDailySummaryNotification)
```

## 功能规格

### Checkin

#### CheckinAccount(accountID, options?)

```
CheckinAccount(accountID, options?):
  1. 加载 account + site (JOIN)
     - 若 account 不存在 → return {success: false, message: 'account not found'}
  2. 站点禁用检查: isSiteDisabled(site.status)
     - 若 disabled → setAccountRuntimeHealth(state: 'disabled', source: 'checkin')
     - 写入 checkin_logs (status: 'skipped', message: 'site disabled')
     - 若 skipEvent=false → 写入 events (type: 'checkin', level: 'info')
     - return {success: true, status: 'skipped', skipped: true, reason: 'site_disabled'}
  3. 获取平台适配器: getAdapter(site.platform)
     - 若不存在 → return {success: false, status: 'failed', message: 'unsupported platform: {platform}'}
  4. 解析 platformUserId 和 proxy:
     - resolvePlatformUserId(extraConfig, username) — 优先从 extraConfig.platformUserId 取，否则从 username 猜
     - resolveProxyUrlFromExtraConfig(extraConfig)
     - 所有 adapter 调用包裹在 withAccountProxyOverride() 中
  5. 首次签到: adapter.checkin(url, accessToken, platformUserId)
  6. 自动重登 (auto-relogin): 若首次失败且 shouldAttemptAutoRelogin() 返回 true:
     - shouldAttemptAutoRelogin 条件: isTokenExpiredError 或 消息含 "new-api-user" 或 "access token"
     - tryAutoRelogin(account, site): 解密密码 → adapter.login() → 更新 DB accessToken/status/updatedAt → 返回新 token
     - 用新 token 重试 adapter.checkin()
  7. 结果分类 (按优先级):
     - isCloudflareChallenge(message) → 标记 cloudflare (后续发 warning 通知)
     - isAlreadyCheckedInMessage(message) → status='success', effectiveSuccess=true, shouldRefreshBalance=true
     - isUnsupportedCheckinMessage(message) → status='skipped', runtimeHealth='degraded'
     - isManualVerificationRequiredMessage(message) → status='skipped', 消息固定为 "站点开启了 Turnstile 校验，需要人工签到"
  8. 奖励解析:
     - parseCheckinRewardAmount(logReward) 或 parseCheckinRewardAmount(result.message)
     - 若 directCheckinSuccess 且 parsedReward <= 0: 用 inferRewardFromBalanceDelta() 推断
       - delta = round6(refreshedBalance - previousBalance)
       - 仅当 delta > 0 时使用推断值
  9. 成功后处理 (effectiveSuccess=true):
     - setAccountRuntimeHealth: 根据状态设 healthy/degraded
     - shouldAdvanceLastCheckinAt: directCheckinSuccess 或 (alreadyCheckedIn && scheduleMode !== 'interval')
     - 若 !storedPlatformUserId && guessedPlatformUserId → 自动持久化到 extraConfig
     - 若 account.status === 'expired' → 自动激活为 'active'
     - 若 shouldRefreshBalance → 调用 refreshBalance(accountId) (静默 catch)
 10. 失败后处理 (effectiveSuccess=false):
     - setAccountRuntimeHealth(state: 'unhealthy', source: 'checkin')
     - 若 isTokenExpiredError → reportTokenExpired() (写 events + 设 status='expired' + 发通知)
     - 若 isCloudflare → sendNotification('Cloudflare challenge', ..., 'warning')
     - 若非 unsupportedCheckin 且非 manualVerification → sendNotification('checkin failed', ..., 'error')
 11. 写入 checkin_logs: 无论 success/skipped/failed 都写入 (status, message, reward, createdAt)
 12. 写入 events: 仅当 skipEvent=false 时写入 (type: 'checkin', title 含 status, level 根据结果)
```

**选项参数:**
- `skipEvent` (boolean): 为 true 时跳过 events 表写入 (CheckinAll 使用此选项避免 N+1 事件)
- `scheduleMode` ('cron' | 'interval'): interval 模式下 already-checked-in 不推进 lastCheckinAt

**结果状态分类:**

| 检测条件 | status | 行为 |
|---------|--------|------|
| result.success=true 且非 already/unsupported/manual | success | 推进 lastCheckinAt, 刷新余额 |
| alreadyCheckedIn | success | 刷新余额, cron 模式推进 lastCheckinAt, interval 不推进 |
| unsupportedCheckin | skipped | runtimeHealth='degraded', 不刷新余额, 不发失败通知 |
| manualVerificationRequired | skipped | runtimeHealth='degraded', 不刷新余额, 不发失败通知 |
| siteDisabled | skipped | runtimeHealth='disabled', 不刷新余额 |
| 其他失败 | failed | runtimeHealth='unhealthy', 按条件通知/上报 |

**isAlreadyCheckedInMessage 检测模式 (12 种):**
```
English: "already checked in", "already signed", "already sign in"
Chinese: "今日已签到", "今天已签到", "今天已经签到", "今日已经签到", "已经签到", "已签到", "重复签到", "签到达"
```

**isUnsupportedCheckinMessage 检测模式 (6 种):**
```
"invalid url (POST /api/user/checkin)"
"HTTP 404" + "/api/user/checkin"
"checkin endpoint not found"
"check-in is not supported"
"checkin is not supported"
"does not support checkin"
"not support checkin"
```

**isManualVerificationRequiredMessage 检测模式:**
```
"Turnstile token 为空"
"turnstile" + ("token" 或 "校验" 或 "验证")
```

#### CheckinAll(options?)

```
CheckinAll(options?):
  1. 查询: SELECT accounts JOIN sites WHERE checkinEnabled=true AND accounts.status='active'
     (注意: 不过滤 credentialMode — apikey 账号也会签到)
  2. 按 siteId 分组: Map<siteId, rows[]>
  3. 若 options.accountIds 传入 → 仅处理指定 ID (用于 scoped 重试)
  4. 并发模型:
     - 不同 site 组 → 并行 (Promise.all / goroutine 并行)
     - 同一 site 组内 → 串行 (for...of / 顺序执行)
     - 目的: 同一站点的请求串行化, 避免触发频率限制
  5. 每个账号调用 checkinAccount(id, {skipEvent: true, scheduleMode})
     - skipEvent=true: 避免每个账号产生独立的 events 记录
  6. 返回 results[] 数组 (每条含 accountId, username, site, result)
     (注意: 不发送汇总通知, 不调用 refreshModelsAndRebuildRoutes)
```

### Balance Refresh

#### RefreshBalance(accountID)

```
RefreshBalance(accountID):
  1. 加载 account + site (JOIN)
     - 若不存在 → return null
  2. 站点禁用检查:
     - 若 disabled → setAccountRuntimeHealth('disabled', source: 'balance')
     - return {balance, used, quota (使用DB现有值), skipped: true, reason: 'site_disabled'}
  3. apikey 连接检查 (isApiKeyConnection):
     - 条件: credentialMode==='apikey' 或 (credentialMode 非显式且 accessToken 为空)
     - 若 apikey → return {balance, used, quota (使用DB现有值), skipped: true, reason: 'proxy_only'}
     (注意: 仅 balance refresh 跳过 apikey; checkin 不跳过)
  4. 获取平台适配器
  5. 解析 platformUserId + proxy (同 checkin)
  6. Sub2Api 托管会话刷新 (前置):
     - 若 isSub2ApiPlatform(site.platform) → 检查 isManagedSub2ApiTokenDue(tokenExpiresAt)
     - 若 due → refreshSub2ApiManagedSessionSingleflight() 刷新 token
     - 失败静默 catch
  7. 首次获取余额: adapter.getBalance(url, accessToken, platformUserId)
  8. 错误处理 (三路分支):
     a) Sub2Api 托管刷新重试:
        - 条件: Sub2Api 平台 + 有 refreshToken + shouldAttemptAutoRelogin(message)
        - 动作: 刷新 Sub2Api token → 重试 getBalance
     b) 自动重登重试 (session 账号):
        - 条件: shouldAttemptAutoRelogin(message) 且非 Sub2Api 路径
        - 动作: tryAutoRelogin → 重试 getBalance
     c) 直接报错: 以上均不适用时抛出
  9. balanceService 的 shouldAttemptAutoRelogin 与 checkin 不同:
     额外匹配: "unauthorized", "forbidden", "not login", "not logged"
     且也检查 isTokenExpiredError + "access token" + "new-api-user"
 10. 今日收入兜底 (todayIncome from logs):
     - 若 balanceInfo.todayIncome 不是有效数字 且 平台支持日志兜底:
       支持平台: new-api / anyrouter / one-api / veloera
     - 调用 fetchTodayIncomeFromLogs():
       请求 /api/log/self?type=1|4&page_size=100&max_pages=6&start_timestamp&end_timestamp
       对每个 item: 若有 quota → income += quota/conversionFactor; 否则从 content 文本解析
       quotaConversionFactor: veloera=1_000_000, 其他=500_000
       PlatformUserId 通过 New-Api-User header 传递
       至少有一个 HTTP 响应正常才返回数值, 否则返回 null
     - 将 fallback 值写入 balanceInfo.todayIncome
 11. 快照更新:
     - updateTodayIncomeSnapshot(): 将 todayIncome 写入 extraConfig.todayIncomeSnapshot
       {day, baseline, latest, updatedAt}
     - baseline: 当天首次刷新时的 income 值（后续刷新时 income 若更低则更新 baseline）
     - Sub2Api 平台还会写入 sub2apiSubscription 摘要
 12. 健康状态保持:
     - 若之前是 checkin 来源的 unsupported degraded 状态 → 保持 degraded (不被 balance 刷新覆盖为 healthy)
     - isUnsupportedCheckinRuntimeHealth() 检测条件:
       state==='degraded' && (source==='checkin' || reason 匹配 unsupported 模式)
 13. 写入 DB (仅以下字段):
     - balance, balanceUsed, quota
     - status (若 account.status==='expired' → 激活为 'active')
     - lastBalanceRefresh, updatedAt
     - extraConfig (若有变更, 含 todayIncomeSnapshot + sub2apiSubscription)
     (注意: 不写入 unit_cost, 不计算 value_score — 这两个字段在 TS 中不存在)
 14. 设置 runtimeHealth: healthy (或保持 degraded)
```

#### RefreshAllBalances()

```
RefreshAllBalances():
  1. 查询所有 status='active' 的账号 (不过滤 credentialMode)
  2. 所有账号并行调用 refreshBalance (无站点分组, 无并发限制)
  3. 每个结果推入 results[] (失败时 push {balance: null})
  4. 返回 results[]
  (注意: 不调用 refreshModelsAndRebuildRoutes)
```

### Today Income Reward (todayIncomeRewardService)

**功能**: 从 extraConfig.todayIncomeSnapshot 追踪每账号每日本收入增量。

**核心数据结构:**
```
TodayIncomeSnapshot = {day, baseline, latest, updatedAt}
```
- `day`: 日期字符串 (local date)
- `baseline`: 当天首次记录的 income（当天内收入更低时更新为更低值）
- `latest`: 最近一次记录的 income
- `updatedAt`: ISO 时间戳

**核心函数:**

```
getTodayIncomeDelta(extraConfig, day):
  - 从 extraConfig.todayIncomeSnapshot 解析 snapshot
  - 若 snapshot.day !== day → return 0
  - delta = snapshot.latest - snapshot.baseline
  - 若 delta <= 0 → return 0; 否则 return delta

getTodayIncomeValue(extraConfig, day):
  - 返回 snapshot.latest (当天最近收入快照)

updateTodayIncomeSnapshot(extraConfig, todayIncome):
  - 若 todayIncome 非正数 → 保持原 extraConfig 不变
  - 计算 day = formatLocalDate(now)
  - 若有同一天已存在的 snapshot:
    baseline = min(existing.baseline, todayIncome)
    latest = todayIncome
  - 若无同天 snapshot:
    baseline = todayIncome, latest = todayIncome
  - 将 snapshot 写入 extraConfig.todayIncomeSnapshot

estimateRewardWithTodayIncomeFallback(input):
  - input: {day, successCount, parsedRewardCount, rewardSum, extraConfig}
  - 若 successCount <= 0 → return rewardSum
  - 若 parsedRewardCount < successCount (有签到的奖励未解析出来) → 回退到 income delta
  - 若 rewardSum > 0 且 parsedRewardCount >= successCount → 直接用 rewardSum
  - 兜底: max(rewardSum, getTodayIncomeValue(extraConfig, day))
```

### Notification (5 通道)

#### SendNotification(title, message, level, options?)

```
SendNotification(title, message, level='info', options?):
  选项:
    - bypassThrottle?: boolean       // 跳过冷却检查
    - requireChannel?: boolean       // 无通道时抛错
    - throwOnFailure?: boolean       // 全失败时抛错

  1. 构建时间脚注 (timeFootnote):
     Local Time: {formatLocalDateTime(now)} ({timeZone})
     UTC Time: {now.toISOString()}

  2. 冷却检查 (throttle):
     - cooldownMs = max(0, trunc(config.notifyCooldownSec)) * 1000
     - 若 bypassThrottle=true 或 cooldownMs <= 0 → 跳过冷却
     - 先 prune: 清理超过 max(cooldownMs*6, 600_000) ms 的陈旧条目
     - 签名: createNotificationSignature(title, message, level) = "level||title||message"
       注意: 三个字段都用 || 连接, 不是仅 title
     - evaluateNotificationThrottle(state, signature, nowMs, cooldownMs):
       - 若未见签名 → 记录并发送
       - 若距上次发送 < cooldownMs → suppressedCount++, 返回 shouldSend=false
       - 若距上次发送 >= cooldownMs 且 suppressedCount > 0 → 发送且消息追加合并提示
     - 若被抑制: return {throttled: true, attempted: 0, succeeded: 0, failed: 0, failedChannels: []}

  3. 合并提示:
     当 suppressedCount > 0 时, resolvedMessage 追加:
     "\n\n[通知合并] 冷静期内已合并 {suppressedCount} 条重复告警"

  4. 无通道: 若 tasks.length === 0
     - requireChannel||throwOnFailure → 抛错 "未启用任何通知渠道..."
     - 否则 return {throttled: false, attempted: 0, ...}

  5. 并行发送: Promise.all 所有通道
     - 每个通道独立 try-catch, 失败不阻塞其他
     - 返回 {attempted, succeeded, failed, failedChannels}

  6. throwOnFailure: 若全部失败 → 抛出第一个失败的错误
```

#### Webhook 通道 (3 种格式自动检测)

```
若 config.webhookEnabled && config.webhookUrl:
  检测 URL:
    1. WeCom 企业微信机器人: hostname=qyapi.weixin.qq.com && path 含 /cgi-bin/webhook/send
       → Payload: {"msgtype":"text","text":{"content": buildWeComText(...)}}
       → 文本: [metapi][LEVEL] title\n\nmessage\n\ntimeFootnote
       → 最大长度: 1900 字符 (超出截断 + "...(truncated)")
       → 响应解析: errcode !== 0 → 抛错
    2. Feishu 飞书机器人: hostname=open.feishu.cn|open.larksuite.com && path 含 /open-apis/bot/v2/hook/
       → Payload: {"msg_type":"text","content":{"text": buildFeishuText(...)}}
       → 文本: [metapi][LEVEL] title\n\nmessage\n\ntimeFootnote
       → 最大长度: 3900 字符 (超出截断)
       → 响应解析: code !== 0 → 抛错
    3. 通用 Webhook (默认):
       → Payload: {"title","message","level","timestamp"(ISO),"localTime","timeZone"}
       → 共 6 个字段
```

#### Bark 通道

```
若 config.barkEnabled && config.barkUrl:
  URL: {barkBase}/{encodeURIComponent(title)}/{encodeURIComponent(resolvedMessage)}
       ?group=AllApiHub&level={encodeURIComponent(level)}
  注意: 必须 URI-encode title 和 message, 必须带 group 和 level 查询参数
  Method: GET
```

#### Server酱 通道

```
若 config.serverChanEnabled && config.serverChanKey:
  URL: https://sctapi.ftqq.com/{serverChanKey}.send
  Method: POST, Content-Type: application/x-www-form-urlencoded
  Body: title={title}&desp={resolvedMessage}\n\nLevel: {level}\n{timeFootnote}
```

#### Telegram 通道

```
若 config.telegramEnabled && config.telegramBotToken && config.telegramChatId:
  API Base URL: config.telegramApiBaseUrl 或 "https://api.telegram.org"
  URL: {baseUrl}/bot{botToken}/sendMessage
  Method: POST, Content-Type: application/json
  代理: 若 config.telegramUseSystemProxy → 通过 config.systemProxyUrl 发送
  Body:
    - chat_id: config.telegramChatId
    - message_thread_id: 仅在 Number.isFinite(threadId) && threadId > 0 时包含
    - text: buildTelegramText() — 最大 3900 字符 (超出截断)
    - disable_web_page_preview: true
  文本格式: [metapi][LEVEL] title\n\nmessage\n\nLevel: level\n{timeFootnote}
  响应解析: payload.ok === false → 抛错
```

#### SMTP 通道

```
若 config.smtpEnabled && smtpHost && smtpPort>0 && smtpFrom && smtpTo:
  发送纯文本邮件 (不是 HTML):
    from: config.smtpFrom
    to: config.smtpTo
    subject: [metapi][LEVEL] title
    text: {resolvedMessage}\n\nLevel: {level}\n{timeFootnote}

  连接池缓存 (getSmtpTransporter):
    - 指纹: smtpHost|smtpPort|smtpSecure?1:0|smtpUser|smtpPass|smtpFrom|smtpTo
    - 配置变更时自动重建 transporter (否则复用缓存)
    - auth: smtpUser ? {user, pass} : undefined
    - secure: config.smtpSecure
```

### Notification Throttle

**核心概念:**
- `NotificationThrottleState = {lastSentAtMs, suppressedCount}`
- 全局 `Map<string, NotificationThrottleState>` (内存)
- 签名: `createNotificationSignature(title, message, level)` = `"level||title||message"` (三个字段, 不是仅 title)

**evaluateNotificationThrottle(state, signature, nowMs, cooldownMs):**
```
若 cooldownMs <= 0 → {shouldSend: true, mergedCount: 0}
若未见签名 → 记录 {lastSentAtMs: nowMs, suppressedCount: 0} → {shouldSend: true, mergedCount: 0}
若距上次 < cooldownMs → suppressedCount++ → {shouldSend: false, mergedCount: 0}
若距上次 >= cooldownMs → 返回 mergedCount=suppressedCount, 重置 suppressedCount=0 → {shouldSend: true, mergedCount}
```

**pruneNotificationThrottleState(state, nowMs, staleMs):**
```
清理所有 nowMs - lastSentAtMs > staleMs 的条目
staleMs = max(cooldownMs * 6, 600_000) ms  (最少 10 分钟)
```

**可配置冷却:**
- cooldown 来源: `config.notifyCooldownSec`
- 设为 0 → throttle 完全旁路 (所有消息立即发送)
- 默认值由配置决定, 不是硬编码 300

### Alert Service

**仅包含以下两个上报函数 (TS 中没有余额阈值/连续失败/通道故障率告警):**

```
reportTokenExpired({accountId, username, siteName, detail}):
  1. 构建 detail: detail 若存在则 appendSessionTokenRebindHint(detail)
  2. 写入 events: type='token', title='Token 已失效', level='error', relatedType='account'
  3. 更新 accounts: status='expired', updatedAt=now
  4. setAccountRuntimeHealth: state='unhealthy', source='auth'
  5. sendNotification('Token 已失效', message, 'error')

reportProxyAllFailed({model, reason}):
  1. 写入 events: type='proxy', title='代理全部失败', level='error', relatedType='route'
  2. sendNotification('代理全部失败', message, 'error')
```

### Alert Rules

```
isCloudflareChallenge(message?):
  检测: "cloudflare" || "cf challenge" || "challenge required" (不区分大小写)

isTokenExpiredError({status?, message?}):
  排除: isEndpointDispatchDeniedMessage (dispatch denied 不代表 token 过期)
  排除: "未登录且未提供 access token" (NewAPI 某操作缺少 session 上下文, 不代表 token 过期)
  401 检测: status===401 或 消息含 HTTP 401
  模式匹配:
    "jwt expired"
    "token expired"
    (token 或 令牌 或 访问令牌) + (invalid 或 无效 或 expired 或 过期)
    "invalid access token"
    "access token is invalid"

isEndpointDispatchDeniedMessage(message?):
  检测: /does\s+not\s+allow\s+\/v1\/[a-z0-9/_:-]+\s+dispatch/i 或 "dispatch denied"

appendSessionTokenRebindHint(message?):
  若消息提示 invalid access token → 追加 "，请在中转站重新生成系统访问令牌后重新绑定账号"
  已含提示的 → 不重复追加
```

### FailureReasonService

**classifyFailureReason({message?, status?, httpStatus?}) 返回 FailureReason {code, category, title, actionHint, detailHint}:**

| 优先级 | 检测条件 | code | category |
|--------|---------|------|----------|
| 1 | status='skipped' + 含 "site disabled" | site_disabled | site |
| 2 | 含 "checkin endpoint not found" / "not support checkin" / "does not support checkin" 等 | checkin_not_supported | site |
| 3 | 含 "turnstile" + ("校验"/"token"/"验证"/"manual") | manual_turnstile_required | verification |
| 4 | 含 "cloudflare tunnel error" / "error 1033" / "unable to resolve it" | cloudflare_tunnel_unavailable | network |
| 5 | isCloudflareChallenge(message) | cloudflare_challenge | verification |
| 6 | isTokenExpiredError({status, message}) | token_expired | auth |
| 7 | 含 "already checked in" / "already signed" / "今天已经签到" / "今日已签到" | already_checked_in | state |
| 8 | 含 "timeout" / "timed out" / "etimedout" / "请求超时" | network_timeout | network |
| 9 | httpStatus >= 500 或 含 "http 5" / "upstream" / "internal server error" | upstream_error | site |
| 10 | 默认 | unknown_error | unknown |

### Daily Summary

```
collectDailySummaryMetrics(now):
  时间范围: getLocalDayRangeUtc(now) → {localDay, startUtc, endUtc}
  数据聚合:
    - 账号: 所有 active 站点的账号 → totalAccounts / activeAccounts / lowBalanceAccounts(balance<1)
    - 签到: 当天 checkin_logs (JOIN active sites) → checkinTotal/success/skipped/failed
    - 奖励: 按 accountId 聚合, parseCheckinRewardAmount(reward/message), 汇总 rewardSum
    - 代理: 当天 proxy_logs (JOIN active sites) → proxyTotal/success/failed/totalTokens
    - spend: proxy_logs.estimatedCost 求和
    - todayReward: 每个账号调用 estimateRewardWithTodayIncomeFallback() (含 todayIncomeSnapshot 兜底)
  返回: DailySummaryMetrics {localDay, generatedAtLocal, timeZone, totalAccounts, activeAccounts, lowBalanceAccounts,
         checkinTotal, checkinSuccess, checkinSkipped, checkinFailed, proxyTotal, proxySuccess, proxyFailed,
         proxyTotalTokens, todaySpend, todayReward}

buildDailySummaryNotification(metrics):
  title: "每日总结 {localDay}"
  message: 包含日期/生成时间/账号概览/签到统计/代理统计/费用统计(支出+奖励+净值)
  净值 = round6(todayReward - todaySpend)
```

## Acceptance Criteria

- [ ] 签到成功 → checkin_logs 写入 (status='success') + lastCheckinAt 更新
- [ ] 签到失败 → checkin_logs 写入 (status='failed') + runtimeHealth='unhealthy'
- [ ] 签到跳过 (site disabled / unsupported / manual verification) → checkin_logs 写入 (status='skipped') + runtimeHealth 相应状态
- [ ] "今日已签到" 检测 (12 种模式) → 视为 success, 触发余额刷新, 但 cron/interval 模式对 lastCheckinAt 行为不同
- [ ] apikey 模式账号 → 签到照常执行, 仅余额刷新跳过 (返回 reason: 'proxy_only')
- [ ] credentialMode=session 且 token 过期 → 触发 auto-relogin 重试 (checkin 和 balance 各有独立逻辑)
- [ ] balanceService shouldAttemptAutoRelogin 额外匹配 "unauthorized"/"forbidden"/"not login"/"not logged"
- [ ] 余额刷新 → 仅更新 balance, balanceUsed, quota, status, lastBalanceRefresh, updatedAt (不写入 unit_cost/value_score)
- [ ] 余额刷新 todayIncome 缺失 → fetchTodayIncomeFromLogs 兜底 (4 平台支持: new-api/anyrouter/one-api/veloera)
- [ ] todayIncomeSnapshot 写入 extraConfig (baseline/latest 追踪)
- [ ] Sub2Api 托管会话 → 前置刷新 + 失败重试
- [ ] 所有 5 个通知通道独立工作 (webhook/bark/serverchan/telegram/smtp)
- [ ] throttle 签名: "level||title||message" (三个字段), cooldown 可配置 (config.notifyCooldownSec), 0=旁路
- [ ] 冷却期内合并通知: 发送时追加 "[通知合并] 冷静期内已合并 N 条重复告警"
- [ ] throttle state 自动清理陈旧条目 (max(cooldown*6, 600s))
- [ ] 通知通道失败不阻塞其他通道 (Promise.all + try-catch)
- [ ] Webhook 自动检测 WeCom/Feishu bot 并使用对应格式; 通用 webhook 含 6 字段
- [ ] Bark URL 含 ?group=AllApiHub&level={level} 参数, title/message URI-encode
- [ ] Telegram 支持 message_thread_id (仅正数), 自定义 API base URL, 系统代理
- [ ] SMTP 纯文本邮件 (非 HTML), 连接池指纹缓存, 配置变更自动重建
- [ ] Daily summary 在 23:58 自动生成 + 发送通知
- [ ] reportTokenExpired: 写 events + 更新 account status='expired' + 设置 runtimeHealth + 发送通知
- [ ] appendSessionTokenRebindHint: invalid access token 错误自动追加绑定提示
- [ ] CheckinAll 按 site 分组串行, cross-site 并行, 使用 skipEvent=true 避免 N+1 events
- [ ] CheckinAll 不发送汇总通知, 仅返回 results[]
- [ ] RefreshAllBalances 不调用 refreshModelsAndRebuildRoutes
- [ ] 站点禁用时 checkin 返回 skipped + balance 返回现有值
- [ ] account status='expired' 在成功 checkin/balance 后自动激活为 'active'
- [ ] checkin 的 unsupported degraded 状态在 balance 刷新后保持 (不被覆盖)
- [ ] platformUserId 从 username 自动猜测并在成功后持久化到 extraConfig
- [ ] 所有 adapter 调用通过 withAccountProxyOverride 包裹

## Test Plan
| 文件 | 内容 |
|------|------|
| `service/checkin/checkin_test.go` | 签到流程 (success/already/skipped/failed) + CheckinAll 分组并发 + auto-relogin + 12 种已签到模式 + 6 种 unsupported 模式 + Turnstile 检测 + reward 推断 + skipEvent + scheduleMode |
| `service/checkin/reward_parser_test.go` | 各种奖励格式解析 (数字/含逗号/含文本/负值/非数字→0) |
| `service/checkin/failure_reason_test.go` | 10 种失败原因分类 |
| `service/balance/balance_test.go` | 余额刷新 + apikey 跳过 + Sub2Api 托管刷新 + todayIncome 日志兜底 + 三路错误重试 + todayIncomeSnapshot 更新 + unsupported degraded 保持 + account auto-activation |
| `service/balance/today_reward_test.go` | todayIncomeSnapshot baseline/latest 追踪 + delta 计算 + estimateRewardWithTodayIncomeFallback |
| `service/notify/notify_test.go` | 5 通道发送 + throttle (level||title||message 签名) + 合并提示 + prune + cooldown=0 旁路 + WeCom/Feishu 格式检测 + 通用 webhook 6 字段 |
| `service/alert/alert_test.go` | reportTokenExpired + reportProxyAllFailed + isTokenExpiredError + isCloudflareChallenge + appendSessionTokenRebindHint + dispatch denied 排除 |
| `service/daily_summary_test.go` | 指标聚合 + reward fallback + 通知构建 |

## Edge Cases

### 签到相关
- 平台返回非标准奖励消息 → parseCheckinRewardAmount 返回数字 0 (不是字符串 "unknown reward")
- 签到返回值是字符串 → parseCheckinRewardAmount 去掉逗号后用正则提取第一个数字; 非数字返回 0; 负数返回 0
- 直接签到成功但 reward 解析为 0 → 用 inferRewardFromBalanceDelta (post-refresh balance - pre-checkin balance) 推断
- 签到接口超时 → 标记 failed, 设置 runtimeHealth='unhealthy', 不阻塞同组其他账号
- 签到返回 "今日已签到" → 视为 success, 仍触发余额刷新, 但 interval 模式不推进 lastCheckinAt
- 站点不支持签到 → status='skipped', runtimeHealth='degraded', 不刷新余额, 不发失败通知
- Turnstile 验证 → status='skipped', runtimeHealth='degraded', 消息固定 "站点开启了 Turnstile 校验，需要人工签到"
- Cloudflare challenge → 发 warning 通知, 不发 error 通知 (避免告警风暴)
- auto-relogin 前提: getAutoReloginConfig 非空 + 密码可解密 + adapter.login 成功
- auto-relogin 成功: 更新 DB accessToken/status/updatedAt, 返回新 token
- scheduledMode='interval' + already-checked-in: 不推进 lastCheckinAt
- PlatformUserId: 初次签到成功时从 username 猜测并自动持久化到 extraConfig

### 余额刷新相关
- apikey 账号 → 返回 DB 现有值 + skipped:true, reason:'proxy_only' (不调 adapter)
- 站点禁用 → 返回 DB 现有值 + skipped:true, reason:'site_disabled'
- Sub2Api 托管 token 到期 → 前置刷新 (不等待失败), 若前置失败仍尝试原 token
- 余额刷新首次失败 → 三路重试: Sub2Api 托管 / auto-relogin (含 6 种模式) / 直接报错
- getBalance 返回的 todayIncome 非数字 → 日志兜底 (仅 4 平台), 兜底失败静默忽略
- 日志兜底 page_size=100, 最多 6 页/type, type=1 和 type=4 分别遍历
- 日志兜底至少一个 HTTP 响应正常才返回数值, 否则返回 null
- todayIncomeSnapshot 同天多次刷新: baseline=min(existing.baseline, income), latest=income
- account status='expired' 在成功刷新后自动激活为 'active'
- checkin 来源的 unsupported degraded 健康状态在 balance 刷新后保持

### 通知相关
- throttle 签名三个字段 "level||title||message" — 同 title 不同 message 不会合并
- cooldown=0 → 完全跳过 throttle 检查
- 冷却期内相同签名 → suppressedCount 递增, 最后一条发送时报告合并数
- throttle map 自动清理陈旧条目 (最少 10 分钟不活动)
- WeCom webhook errcode !== 0 → 抛错; 响应非 JSON → 抛错
- Feishu webhook code !== 0 → 抛错; 响应非 JSON → 抛错
- Bark URL 特殊字符需 URI-encode
- Telegram thread_id 为空/非正数 → 不传 message_thread_id
- Telegram 文本超过 3900 字符 → 截断 + "...(truncated)"
- SMTP 连接失败 → 返回错误, 不 panic, 通道独立重试
- SMTP 配置变更 → 自动丢弃旧 transporter, 新建
- 所有 5 个通道均未启用 → 根据 options 决定静默返回或抛错
- throwOnFailure=true + 全失败 → 抛出首个失败的错误

### 告警相关
- 仅两个告警上报函数: reportTokenExpired 和 reportProxyAllFailed
- 无余额阈值告警, 无连续签到失败 N 次告警, 无通道故障率告警 (TS 中不存在)
- isTokenExpiredError 排除 dispatch denied 场景 (不代表 token 过期)
- isTokenExpiredError 排除 "未登录且未提供 access token" (NewAPI 特定场景)
- appendSessionTokenRebindHint 仅对 invalid access token 消息追加, 已含通用提示不重复
- reportTokenExpired 将 account status 设为 'expired' 并设置 runtimeHealth='unhealthy'/source='auth'
