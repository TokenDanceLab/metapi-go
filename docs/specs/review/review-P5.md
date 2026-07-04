# P5 Spec Cross-Reference Review

**Reviewed**: 2026-07-04 | **Spec**: `docs/specs/p5-checkin-balance-notify.md` | **TS sources**: 8 files (checkinService, checkinRewardParser, balanceService, notifyService, notificationThrottle, alertService, alertRules, dailySummaryService)

---

## Accuracy Issues

### A1. CheckinAccount step 3 -- apikey skip is wrong domain
**Spec claim (lines 45-47):** "检查 checkinEnabled + credentialMode (apikey 模式跳过)"

**TS reality:** `checkinAccount()` (checkinService.ts) does NOT check credential mode or skip apikey accounts. The `isApiKeyConnection()` check lives exclusively in `refreshBalance()` (balanceService.ts:270-278) for balance refresh, not checkin. An apikey account sent through `checkinAccount()` will attempt checkin normally, calling `adapter.checkin()`.

**Impact:** Go implementation following this spec would incorrectly skip apikey accounts during checkin (or miss apikey skip during balance).

### A2. CheckinAll query filter -- no credentialMode condition
**Spec claim (line 53):** "查询所有 checkinEnabled=true 且 credentialMode!=apikey 的账号"

**TS reality (checkinService.ts:325-334):**
```typescript
.where(and(
  eq(schema.accounts.checkinEnabled, true),
  eq(schema.accounts.status, 'active'),
))
```
The actual query filters on `checkinEnabled=true AND status='active'` only. No credential mode filter whatsoever.

**Impact:** Go implementation would filter out legitimate accounts that happen to have apikey credential mode.

### A3. CheckinAll concurrency model -- wrong parallelism
**Spec claim (line 54):** "并发签到 (goroutine pool, concurrency=4)"

**TS reality (checkinService.ts:339-363):** Accounts are grouped by siteId. The groups run in parallel via `Promise.all(promises)` but within each group, accounts are processed **sequentially** (`for (const row of siteRows)`). There is no explicit concurrency pool of 4. The effective parallelism equals the number of distinct sites, with per-site serialization.

**Impact:** Go implementation with a fixed goroutine pool of 4 would change the concurrency semantics -- per-site serialization (which prevents rate-limiting on a single API endpoint) would be lost.

### A4. CheckinAll step 3 -- no notification sent
**Spec claim (line 55):** "汇总结果 → 发送通知"

**TS reality (checkinService.ts:362-363):** `checkinAll()` simply returns the results array. It does NOT send any notification. Individual `checkinAccount()` calls within it use `skipEvent: true`, so per-account events are also suppressed. The feature gap is significant -- the TS code does not produce a batch-checkin summary notification.

**Impact:** Go implementation would send notifications that the TS codebase never sends.

### A5. Balance refresh -- unit_cost and value_score not written
**Spec claim (lines 63-64):** "更新 accounts.balance, .balance_used, .quota, .unit_cost" and "更新 value_score (= balance x unit_cost 估算)"

**TS reality (balanceService.ts:396-403):**
```typescript
const updates: Record<string, unknown> = {
  balance: balanceInfo.balance,
  balanceUsed: balanceInfo.used,
  quota: balanceInfo.quota,
  status: account.status === 'expired' ? 'active' : account.status,
  lastBalanceRefresh: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
};
```
Only `balance`, `balanceUsed`, `quota`, `status`, `lastBalanceRefresh`, `updatedAt` are written. There is NO `unit_cost` field update and NO `value_score` computation anywhere in the TS codebase.

**Impact:** These two fields are entirely fabricated in the spec. If the Go schema includes them, they will never be populated by the service layer.

### A6. RefreshAllBalances step 3 -- no route rebuild
**Spec claim (line 71):** "刷新完成后 → refreshModelsAndRebuildRoutes"

**TS reality (balanceService.ts:426-447):** `refreshAllBalances()` returns `results` array directly. There is no call to any model refresh or route rebuild function.

**Impact:** Go implementation would couple balance refresh to route rebuilding where the TS codebase keeps them separate.

### A7. Throttle key -- three-part signature, not title-only
**Spec claim (line 77):** "检查 throttle (NOTIFY_COOLDOWN_SEC 内不重复发送相同 title)"

**TS reality (notificationThrottle.ts:6-12):**
```typescript
export function createNotificationSignature(title, message, level) {
  return [(level || '').trim().toLowerCase(), (title || '').trim(), (message || '').trim()].join('||');
}
```
The throttle signature is `level||title||message` -- all three components. Two notifications with the same title but different messages or levels will NOT be throttled.

**Impact:** Go implementation throttling by title alone would be too aggressive, suppressing distinct messages that share a title.

### A8. Throttle cooldown -- configurable, not hardcoded 300s
**Spec claim (line 101):** "throttle: 300s 内相同 title 不重复发送"

**TS reality (notifyService.ts:140):**
```typescript
const cooldownMs = Math.max(0, Math.trunc(config.notifyCooldownSec)) * 1000;
```
The cooldown is read from `config.notifyCooldownSec` -- user-configurable, not hardcoded to 300. When cooldown is 0, throttle is entirely bypassed.

**Impact:** Hardcoding 300s removes user configurability and makes the throttle non-disableable.

### A9. SMTP format -- plain text, not HTML
**Spec claim (line 82):** "SMTP: 发送 HTML 邮件"

**TS reality (notifyService.ts:315-321):**
```typescript
transporter.sendMail({
  from: config.smtpFrom,
  to: config.smtpTo,
  subject: `[metapi][${level.toUpperCase()}] ${title}`,
  text: `${resolvedMessage}\n\nLevel: ${level}\n${timeFootnote}`,
})
```
The email is sent as **plain text** via the `text:` property. There is no `html:` property. The SMTP envelope includes a formatted subject but the body is plain text.

**Impact:** Go implementation sending HTML might render differently in email clients. If the Go code wraps content in HTML tags, it could break plain-text expectations from downstream.

### A10. Webhook format -- incomplete; misses WeCom/Feishu detection
**Spec claim (line 79):** "Webhook: POST JSON {title, message, level} 到 WEBHOOK_URL"

**TS reality (notifyService.ts:163-227):** The webhook channel has **three distinct payload formats**:
1. **WeCom bot** (`qyapi.weixin.qq.com`): `{msgtype: 'text', text: {content: ...}}`
2. **Feishu bot** (`open.feishu.cn` / `open.larksuite.com`): `{msg_type: 'text', content: {text: ...}}`
3. **Generic webhook**: `{title, message, level, timestamp, localTime, timeZone}` (6 fields, not 3)

Additionally, WeCom and Feishu responses are parsed for error codes (`errcode`/`code`).

**Impact:** Go implementation sending `{title, message, level}` to WeCom/Feishu webhooks would fail silently because those platforms require their specific schemas.

### A11. Bark URL -- missing query parameters
**Spec claim (line 80):** "Bark: GET BARK_URL/{title}/{message}"

**TS reality (notifyService.ts:230-241):**
```typescript
const url = `${barkBase}/${encodeURIComponent(title)}/${encodeURIComponent(resolvedMessage)}?group=AllApiHub&level=${encodeURIComponent(level)}`;
```
The actual URL appends **`?group=AllApiHub&level={level}`** query parameters. Both title and message are also URI-encoded.

**Impact:** Missing query params lose Bark grouping and level metadata. Missing URI encoding can break on special characters.

### A12. Reward parser edge case -- returns 0, not "unknown reward"
**Spec (Edge Cases line 115):** "reward_parser 容错返回 'unknown reward'"

**TS reality (checkinRewardParser.ts:11-28):** `parseCheckinRewardAmount()` returns a **number** (0) for unparseable input. It never returns a string like "unknown reward". The return type is `number`.

**Impact:** Go implementation returning a string would break downstream consumers that expect a numeric reward field.

### A13. Alert Service rules -- mostly absent from provided TS
**Spec claim (lines 89-92):** "余额低于阈值 → 发送告警 | 签到连续失败 N 次 → 发送告警 | 通道故障率过高 → 发送告警"

**TS reality:** The provided `alertService.ts` only contains two functions: `reportTokenExpired()` and `reportProxyAllFailed()`. There is NO balance-threshold alert, NO consecutive-checkin-failure alert, and NO channel-failure-rate alert in these source files. The `alertRules.ts` file only contains `isCloudflareChallenge()`, `isTokenExpiredError()`, `appendSessionTokenRebindHint()`, and `isEndpointDispatchDeniedMessage()`.

**Impact:** These three alert rules described in the spec have no TypeScript source to reference. Either they exist in files not provided, or they are aspirational features that never existed in the original codebase.

---

## Missing Details

### M1. Cloudflare challenge detection and notification
The spec does not mention Cloudflare challenge detection. TS code (checkinService.ts:194, 297-303; alertRules.ts:1-5) detects Cloudflare responses, sends a `warning`-level notification, and labels the event as "checkin failed (cloudflare challenge)".

### M2. Already-checked-in detection (12 patterns)
The spec does not document `isAlreadyCheckedInMessage()` (checkinService.ts:28-46), which detects 12 patterns across English ("already checked in", "already signed") and Chinese ("今日已签到", "已经签到", "重复签到", etc.). An already-checked-in result is treated as **success** (not failure) and triggers balance refresh.

### M3. Unsupported checkin endpoint detection
The spec does not document `isUnsupportedCheckinMessage()` (checkinService.ts:48-60), which detects 6 patterns (including "invalid url (POST /api/user/checkin)" and HTTP 404 on checkin endpoint). Unsupported checkin is treated as **skipped** (not failed), with runtime health set to 'degraded'.

### M4. Turnstile manual verification detection
The spec does not document `isManualVerificationRequiredMessage()` (checkinService.ts:62-69), which detects Turnstile token verification requirements. This is also treated as **skipped** with a specific "站点开启了 Turnstile 校验，需要人工签到" message.

### M5. Auto-relogin flow (both checkin and balance)
The spec mentions auto-relogin in AC line 98 but gives zero implementation detail. TS code has:
- `shouldAttemptAutoRelogin()` in checkinService (lines 71-79): checks isTokenExpiredError + "new-api-user" + "access token"
- `tryAutoRelogin()` in checkinService (lines 95-121): decrypts password, calls `adapter.login()`, updates accessToken + status in DB, returns new token
- BalanceService has its own `shouldAttemptAutoRelogin()` (lines 36-49) with additional patterns: "unauthorized", "forbidden", "not login", "not logged"
- BalanceService has its own `tryAutoRelogin()` (lines 211-237)

### M6. Balance delta reward inference
Not documented. When direct checkin succeeds but parsed reward is <= 0, TS code (checkinService.ts:251-256) computes `inferRewardFromBalanceDelta(previousBalance, refreshedBalance)` -- the difference between pre-checkin balance and post-refresh balance, rounded to 6 decimal places.

### M7. Sub2Api managed session refresh
BalanceService (lines 287-301) handles `isSub2ApiPlatform()` accounts with managed auth: checks `isManagedSub2ApiTokenDue()`, calls `refreshSub2ApiManagedSessionSingleflight()` proactively before getting balance, and retries on failure. Entirely absent from spec.

### M8. Today income from log fallback
BalanceService (lines 68-209) implements `fetchTodayIncomeFromLogs()` which paginates through `/api/log/self` (log types 1 and 4, page_size=100, max 6 pages per type) to compute today's income when the adapter doesn't return `todayIncome` directly. Uses platform-specific quota conversion factors (500K default, 1M for Veloera). Not mentioned in spec.

### M9. WeCom/Feishu bot webhook detection and formatting
NotifyService detects WeCom (`qyapi.weixin.qq.com` + `/cgi-bin/webhook/send`) and Feishu (`open.feishu.cn`/`open.larksuite.com` + `/open-apis/bot/v2/hook/`) URLs and formats payloads accordingly. Each has its own text builder with platform-specific max lengths (WeCom: 1900, Feishu: 3900).

### M10. Notification merging display within throttle window
When throttle suppresses N duplicate notifications, the next allowed send appends `[通知合并] 冷静期内已合并 N 条重复告警` to the message (notifyService.ts:156-158). The spec doesn't mention this.

### M11. PlatformUserId resolution
Both checkin and balance resolve `platformUserId` from extraConfig (via `resolvePlatformUserId()`), with auto-guessing from username (`guessPlatformUserIdFromUsername()`) and auto-persistence to extraConfig on success. Not documented.

### M12. Proxy override support
Every adapter call is wrapped in `withAccountProxyOverride()` using `resolveProxyUrlFromExtraConfig()`. The spec mentions "proxy" in the adapter call signature but gives no detail on how proxy configuration is resolved per-account.

### M13. Runtime health tracking
`setAccountRuntimeHealth()` is called throughout with states: `healthy`, `degraded`, `unhealthy`, `disabled`, and sources: `checkin`, `balance`, `auth`. Health state flows across operations -- e.g., unsupported-checkin 'degraded' state is **preserved** during balance refresh (`isUnsupportedCheckinRuntimeHealth`, balanceService.ts:56-66, 394-421).

### M14. Site disabled handling
Both checkin and balance check `isSiteDisabled(site.status)` and set runtime health to 'disabled' with source-specific reasons. Checkin writes a `skipped` log entry and an `events` record. Balance returns existing values with `skipped: true, reason: 'site_disabled'`.

### M15. skipEvent option and scheduleMode
`checkinAccount()` accepts `{skipEvent, scheduleMode}` options. `skipEvent` suppresses events insertion (used by CheckinAll to avoid N+1 events). `scheduleMode: 'interval'` changes behavior: for already-checked-in accounts, `lastCheckinAt` is NOT advanced in interval mode (checkinService.ts:203).

### M16. Token expiry reporting flow
When checkin/balance fail with token expiry, `reportTokenExpired()` (alertService.ts:8-46) is called, which: (a) inserts an event with type='token', (b) sets account status to 'expired', (c) sets runtime health to 'unhealthy', (d) sends notification. Not documented in spec.

### M17. Configurable Telegram API base URL and proxy
Telegram channel supports `config.telegramApiBaseUrl` (custom API endpoint), `config.telegramUseSystemProxy` (route through system proxy), and `disable_web_page_preview: true`. `message_thread_id` is only included when `Number.isFinite(threadId) && threadId > 0`. None of these details are in the spec.

### M18. SMTP fingerprint caching
The SMTP transporter is cached via `getSmtpTransporter()` (notifyService.ts:45-64) using a fingerprint hash of all SMTP config parameters. Recreated only when config changes. Not mentioned.

### M19. Notification options (requireChannel, throwOnFailure, bypassThrottle)
`sendNotification()` accepts `SendNotificationOptions` with three flags that alter behavior: bypass throttle, error when no channels configured, throw on total failure. Not in spec.

### M20. checkin_logs always written (success and failure)
The spec implies checkin_logs is only written on success (step 5), but TS code ALWAYS writes a checkin_logs row (checkinService.ts:260-266) regardless of outcome -- it records the status (success/skipped/failed), message, and reward.

### M21. Auto-activation of expired accounts
Both checkin and balance auto-set account status from 'expired' to 'active' on success (checkinService.ts:232-235, balanceService.ts:400).

### M22. spec references TS files not provided
The spec's "原始 TS 参考" section lists `todayIncomeRewardService.ts` and `failureReasonService.ts`, which were not among the 8 files provided for review. These modules are referenced by the reviewed code but their internals could not be verified.

---

## Edge Cases Not Covered

### E1. Cloudflare during balance refresh
Spec mentions "签到接口超时" as an edge case but not Cloudflare challenge, which has dedicated detection and notification logic.

### E2. Already-checked-in as success
Spec treats checkin as binary success/fail. TS treats "already checked in" as a form of success that still triggers balance refresh but may skip lastCheckinAt advancement (scheduleMode-dependent).

### E3. Unsupported checkin treated as skipped (not error)
Spec edge case says "平台返回非标准奖励消息 → reward_parser 容错", but the more important edge case is that an unsupported checkin endpoint is not an error -- it's a `skipped` status with `degraded` health, and the degradation persists across balance refreshes.

### E4. Turnstile/manual verification as skipped
Manual verification (Turnstile token missing) is detected and treated as skipped, not failed. Not mentioned.

### E5. Token expired during balance refresh triggering different retry paths
BalanceService has three error-handling branches: (a) managed Sub2Api refresh, (b) auto-relogin for session-based accounts, (c) direct error if neither applies. The spec only mentions a single retry path.

### E6. Notification throttle state pruning
`pruneNotificationThrottleState()` cleans stale entries older than `max(cooldown*6, 600000ms)`. Without pruning, the throttle map would grow unbounded.

### E7. Notification throttle merging
When a throttled notification is eventually sent, it reports how many were suppressed. The spec doesn't cover this.

### E8. interval scheduleMode's effect on lastCheckinAt
When `scheduleMode === 'interval'`, an already-checked-in result does NOT advance `lastCheckinAt`. This prevents the interval scheduler from racing ahead of the daily cron scheduler.

### E9. WeCom/Feishu webhook error response parsing
Both platforms return typed error responses that the code parses and throws on non-zero codes. Not mentioned.

### E10. Error in one channel not blocking others
Spec says "失败不阻塞其他通道" but the actual mechanism (`Promise.all` with individual try-catch) means all channels fire in parallel and individual failures are collected. None of this detail is in the spec.

### E11. Veloera-specific quota conversion factor
`resolveQuotaConversionFactor()` returns 1,000,000 for Veloera vs 500,000 for other platforms. This materially affects today-income-from-logs calculations.

### E12. Sub2Api token expiry detection and proactive refresh
BalanceService proactively checks if a managed Sub2Api token is due for refresh before calling getBalance, plus retries on failure. The spec mentions "token 过期" only for session-based accounts with auto-relogin.

### E13. PlatformUserId auto-guess and persistence
When `checkinAccount` succeeds and a guessed platformUserId differs from the stored one, it auto-persists to extraConfig. Not mentioned.

### E14. Zero cooldown bypasses throttle entirely
When `config.notifyCooldownSec` is 0, the throttle is completely bypassed (cooldownMs=0, evaluateNotificationThrottle returns true immediately). The spec implies throttle is always active at 300s.

### E15. SMTP reconstitution on config change
If any SMTP config field changes, the cached transporter is discarded and a new one created. Without this, credentials changes would be silently ignored.

### E16. Generic webhook payload includes timestamp/timeZone/localTime
Beyond the spec's `{title, message, level}`, the generic webhook payload carries `timestamp` (ISO), `localTime` (formatted), and `timeZone` (resolved). These are valuable for downstream consumers.

---

## Incorrect Details

| ID | Spec Claim | TS Reality | Severity |
|---|---|---|---|
| I1 | CheckinAccount skips apikey-mode accounts | apikey skip only exists in balanceService; checkin never checks credentialMode | HIGH |
| I2 | CheckinAll filters credentialMode!=apikey | Query is checkinEnabled=true AND status=active only | HIGH |
| I3 | CheckinAll uses goroutine pool, concurrency=4 | Groups by site, sequential per site, Promise.all across sites | MEDIUM |
| I4 | CheckinAll sends notification after summary | Returns results array only; no notification sent | MEDIUM |
| I5 | Balance refresh writes unit_cost and value_score | Neither field is computed or written in TS code | HIGH |
| I6 | RefreshAllBalances calls refreshModelsAndRebuildRoutes | No such call exists; just returns results | MEDIUM |
| I7 | Throttle by "相同 title" (title only) | Throttle signature is level+title+message | HIGH |
| I8 | Throttle hardcoded at 300s (NOTIFY_COOLDOWN_SEC) | Cooldown is config.notifyCooldownSec, configurable, can be 0 | MEDIUM |
| I9 | SMTP sends HTML email | Sends plain text via `text:` property | MEDIUM |
| I10 | Reward parser returns "unknown reward" string | Returns numeric 0 | MEDIUM |
| I11 | Webhook sends {title, message, level} | Three formats: WeCom bot, Feishu bot, generic (6 fields) | HIGH |
| I12 | Bark URL is BARK_URL/{title}/{message} | Also includes ?group=AllApiHub&level={level} query params | LOW |
| I13 | Alert rules: balance-threshold, consecutive-failure, channel-failure-rate | Not present in provided alertService.ts or alertRules.ts | HIGH |

---

## Summary

| Category | Count |
|---|---|
| Accuracy Issues | 13 |
| Missing Details | 22 |
| Edge Cases Not Covered | 16 |
| Incorrect Details | 13 |

**Total findings**: 64

**Verdict**: **NEEDS_REVISION**

The spec has 13 factual inaccuracies (7 rated HIGH severity), 22 missing implementation details critical for faithful Go porting, and 16 edge cases that the TS codebase handles but the spec omits. The most critical gaps are:

1. The apikey-mode skip logic is misplaced into checkin when it belongs in balance refresh.
2. The throttle signature (level/title/message vs title-only) and configurable cooldown are wrong -- the Go impl would over-throttle or use wrong keys.
3. The webhook channel description is drastically oversimplified; WeCom/Feishu detection is entirely missing.
4. `unit_cost` and `value_score` database writes are fabricated -- they do not exist in the TS source.
5. Three alert rules described in the spec have no corresponding TypeScript implementation in the provided source files.

**Recommendation**: Revise the spec to align with TS source truth before implementing `service/notify/webhook.go` and `service/balance/balance.go`, as these two modules have the highest density of discrepancies.
