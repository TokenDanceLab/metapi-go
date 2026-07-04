# P5 Implementation Review: Checkin + Balance + Notify (Go)

**Date**: 2026-07-04  
**Spec**: `docs/specs/p5-checkin-balance-notify.md`  
**TS Reference**: `metapi/src/server/services/checkinService.ts`, `notifyService.ts`  
**Go Source**: `metapi-go/service/{checkin,balance,notify,alert,daily}/`  

---

## Overall Assessment: 70% complete

The Go implementation covers the architectural skeleton and core data flow for all five P5 modules. Message-pattern detection, throttle logic, alert rules, and data model types are faithfully ported from TS. However, **8 non-trivial runtime behaviors are stubbed or broken**, concentrated in proxy application, balance-delta inference, Sub2Api managed sessions, and notification channel edge cases. Test coverage is strong for pure functions (parsers, classifiers, throttle) but absent for integration flows (CheckinAccount, RefreshBalance, full notification dispatch).

---

## Module-by-Module Analysis

### 1. Checkin (`service/checkin/`)

| File | Verdict | Details |
|------|---------|---------|
| `checkin.go` | PARTIAL | Core flow correct. 3 critical stubs and 1 proxy gap. |
| `reward_parser.go` | PASS | Number/string/negative/NaN/comma parsing matches TS exactly. |
| `failure_reason.go` | PASS | 10-priority classifier matches spec. Minor: priority-10 adds "执行成功" variant not in TS. |

**What matches spec**:
- 12 `isAlreadyCheckedInMessage` patterns, 7 `isUnsupportedCheckinMessage` patterns, Turnstile detection -- all identical to TS.
- `shouldAttemptAutoRelogin` (checkin version) calls `alert.IsTokenExpiredError` + "new-api-user" + "access token".
- `CheckinAll` groups by siteId, parallel across sites, serial within site, `skipEvent=true`.
- Site-disabled branch: runtime health, checkin_logs, events, correct return shape.
- `effectiveSuccess` / `directCheckinSuccess` / `shouldAdvanceLastCheckinAt` / `shouldRefreshBalance` logic matches TS.
- platformUserId auto-guess + auto-persist, expired-to-active auto-activation.
- Post-failure: runtimeHealth, token-expired reporting, Cloudflare warning vs error notification gating.

**Critical gaps**:

| # | Issue | Severity | TS line | Go line |
|---|-------|----------|---------|---------|
| C1 | **`tryAutoRelogin` ignores proxy**. TS wraps `adapter.login()` in `withAccountProxyOverride()`. Go extracts `proxyURL` then discards it (`_ = proxyURL`). All auto-relogin login calls go out without proxy. | HIGH | checkinService.ts:106-108 | checkin.go:135-136 |
| C2 | **`inferRewardFromBalanceDelta` is never called**. The function exists in `reward_parser.go` but CheckinAccount stubs it out: `_ = store.Account{Balance: account.Balance}`. Any direct-checkin-success where the reward message yields 0 gets no inferred reward. | HIGH | checkinService.ts:250-256 | checkin.go:330-332 |
| C3 | **Balance refresh after successful checkin is stubbed**. TS calls `refreshBalance(account.id)` (with silent catch). Go writes `_ = refreshedBalanceInfo` with comment "Will be implemented by balance package". This means `inferRewardFromBalanceDelta` also has no post-refresh balance to compare against. | HIGH | checkinService.ts:244-248 | checkin.go:316-319 |

**Reward parsing logic divergence** (checkin.go:323-333):

TS pattern:
```
parsedReward = parseCheckinRewardAmount(logReward) || parseCheckinRewardAmount(result.message)
→ if directCheckinSuccess && parsedReward <= 0: infer from delta
```

Go does a subtly different thing -- it updates `logReward` (= the persisted reward string) when the second parse succeeds, but this conflates the reward-logging concern with the inference trigger. The behavioral outcome is the same (reward field gets the second-parse value) but the inference branch is unreachable because structural stubs (C2 + C3) prevent it.

---

### 2. Balance Refresh (`service/balance/`)

| File | Verdict | Details |
|------|---------|---------|
| `balance.go` | PARTIAL | Core flow correct. 3 stubs, 1 broken parser. |
| `today_reward.go` (at `service/today_reward.go`) | PASS | Snapshot update, delta/value extraction, estimateReward fallback all match TS. |

**What matches spec**:
- Site-disabled short-circuit (returns DB values + skipped).
- apikey connection skip (`proxy_only`).
- `shouldAttemptAutoReloginBalance` with 4 extra patterns beyond checkin version ("unauthorized", "forbidden", "not login", "not logged").
- Three-way error handling structure: Sub2Api retry → auto-relogin → error.
- `todayIncomeSnapshot` update via `UpdateTodayIncomeSnapshot` (baseline=min, latest=now).
- `isUnsupportedCheckinRuntimeHealth` preservation during balance refresh.
- DB update only writes balance/balanceUsed/quota/status/lastBalanceRefresh/updatedAt/extraConfig -- no unit_cost, no value_score.
- `RefreshAllBalances` parallelizes all accounts, no site grouping, no `refreshModelsAndRebuildRoutes`.

**Critical gaps**:

| # | Issue | Severity | TS line | Go line |
|---|-------|----------|---------|---------|
| B1 | **Sub2Api managed session pre-refresh is a stub**. TS checks `isManagedSub2ApiTokenDue(tokenExpiresAt)` and calls `refreshSub2ApiManagedSessionSingleflight()`. Go only logs: `slog.Info("Sub2Api token due for refresh, but singleflight not implemented", ...)`. | HIGH | balanceService.ts (step 6) | balance.go:338-343 |
| B2 | **Sub2Api managed retry on first failure is a stub**. TS retries with refreshed Sub2Api token before falling through to auto-relogin. Go checks the condition but comments "Would call refreshSub2ApiManagedSessionSingleflight here -- for now, fall through to auto-relogin". | HIGH | balanceService.ts (step 8a) | balance.go:374-377 |
| B3 | **`parseIncomeFromContent` is broken**. The function body scans for digit/sign/dot positions but then discards them and always returns 0. The comment says "simplified - use regex in real implementation". All content-based income parsing from logs is non-functional. | HIGH | balanceService.ts (step 10 content parsing) | balance.go:226-237 |

**Minor issues**:

| # | Issue | Severity |
|---|-------|----------|
| B4 | `fetchTodayIncomeFromLogs` does not apply account-level proxy. TS would route through `withAccountProxyOverride`. | MEDIUM |
| B5 | `quotaConversionFactor` for veloera uses `1_000_000` (TS: `1_000_000`). The factor is so large that a Go `int` literal separator needs no change -- fine. | OK |

---

### 3. Today Income Reward (`service/today_reward.go`)

PASS. All four functions match TS:

- `GetTodayIncomeDelta` -- snapshot.day check, delta = latest - baseline, zero-guard.
- `GetTodayIncomeValue` -- returns snapshot.latest.
- `UpdateTodayIncomeSnapshot` -- non-positive guard, same-day baseline=min(existing.baseline, income), new-day baseline=income.
- `EstimateRewardWithTodayIncomeFallback` -- successCount gate, parsedRewardCount < successCount trigger, max(rewardSum, incomeValue) fallback.

No issues found.

---

### 4. Notification (`service/notify/`)

| File | Verdict | Details |
|------|---------|---------|
| `notify.go` | PARTIAL | Dispatch structure correct. 1 bug in throwOnFailure. |
| `throttle.go` | PASS | Signature format, cooldown, suppression merge, prune all match TS. |
| `webhook.go` | BUG | Generic webhook payload has empty fields. |
| `bark.go` | PASS | URI-encode + group/level params correct. |
| `serverchan.go` | PASS | Form-encoded POST correct. |
| `telegram.go` | PARTIAL | System proxy support is stubbed. |
| `smtp.go` | PARTIAL | Plain SMTP only; TLS/STARTTLS not handled. |

**What matches spec**:
- `createNotificationSignature` format: `"level||title||message"` (three fields, not just title).
- Throttle: cooldown guard, prune with `max(cooldownMs*6, 600_000)`, suppression count tracking, merge message on re-send.
- Cooldown=0 bypass path.
- 5-channel parallel dispatch with per-channel error isolation.
- WeCom/Feishu URL auto-detection via hostname + path matching.
- WeCom: `errcode !== 0` → error; non-JSON response → error.
- Feishu: `code !== 0` → error; non-JSON response → error.
- Bark: `encodeURIComponent(title)/encodeURIComponent(message)?group=AllApiHub&level=`.
- ServerChan: `application/x-www-form-urlencoded` POST.
- Telegram: `message_thread_id` only when positive integer, custom API base URL, `disable_web_page_preview: true`.
- SMTP: fingerprint string format matches TS (7 fields pipe-joined).

**Critical gaps**:

| # | Issue | Severity | TS line | Go line |
|---|-------|----------|---------|---------|
| N1 | **Generic webhook payload has empty timestamp/localTime/timeZone**. TS fills `now.toISOString()`, `formatLocalDateTime(now)`, `getResolvedTimeZone()`. Go sends `"timestamp": "", "localTime": "", "timeZone": ""`. Downstream receivers get no time data. | HIGH | notifyService.ts:186-196 | webhook.go:41-48 |

**Significant gaps**:

| # | Issue | Severity |
|---|-------|----------|
| N2 | **Telegram system proxy is stubbed**. When `TelegramUseSystemProxy && SystemProxyUrl` is set, Go creates a bare `&http.Client{}` with comment "Proxy would be configured on transport". TS uses `withExplicitProxyRequestInit()` which sets the proxy agent on the fetch request init. | MEDIUM |
| N3 | **SMTP does not handle `smtpSecure` (TLS/STARTTLS)**. TS uses nodemailer with `secure: config.smtpSecure`. Go uses `net/smtp.SendMail` which is plain SMTP. For TLS (port 465) or STARTTLS (port 587), this will fail silently or send in cleartext. | MEDIUM |
| N4 | **`throwOnFailure` does not return the error**. TS throws the first failure error. Go checks `succeeded == 0 && len(failedResults) > 0` but only calls `slog.Error(...)` -- the caller never receives the error. | MEDIUM |
| N5 | **SMTP fingerprint caching is decorative**. `cachedFingerprint` is stored but never compared on subsequent calls. Every `Send()` call creates a new SMTP connection. TS's nodemailer transporter caching is not replicated. | LOW |

**Minor issues**:

| # | Issue | Severity |
|---|-------|----------|
| N6 | `NotifyCooldownSec` is cast directly with `int64(cfg.NotifyCooldownSec)` -- if the config value is a float (e.g. 300.7), Go truncates toward zero (int64 of float64). TS uses `Math.trunc()` first. For integer configs this is a no-op; for fractional, TS would trunc to 300, Go to 300 (Go's int64 conversion truncates toward zero for floats). Behaviorally equivalent for positive floats. | OK |
| N7 | Negative cooldown is guarded with `if cooldownMs < 0 { cooldownMs = 0 }` -- matches TS `Math.max(0, ...)`. | OK |
| N8 | No-channel case: Go only logs error but doesn't throw it even when options demand it. TS throws: `throw new Error('未启用任何通知渠道...')`. | LOW |

---

### 5. Alert Service (`service/alert/`)

| File | Verdict | Details |
|------|---------|---------|
| `alert.go` | PASS | Both report functions match TS exactly. |
| `rules.go` | PASS | All pattern matchers correct with exclusions. |

**What matches spec**:
- `reportTokenExpired`: CreateEvent → UPDATE accounts status='expired' → SetAccountRuntimeHealth(unhealthy/auth) → SendNotification. Detail has `appendSessionTokenRebindHint` applied.
- `reportProxyAllFailed`: CreateEvent → SendNotification. No balance threshold alert, no consecutive-failure alert, no channel-failure-rate alert (correct -- TS doesn't have these).
- `isCloudflareChallenge`: "cloudflare", "cf challenge", "challenge required" (case-insensitive).
- `isTokenExpiredError`: JWT expired, token expired, token/令牌/访问令牌 + invalid/无效/expired/过期, status 401, plus two exclusions (dispatch denied, "未登录且未提供 access token").
- `isEndpointDispatchDeniedMessage`: regex `does\s+not\s+allow\s+/v1/[a-z0-9/_:-]+\s+dispatch` + plain "dispatch denied".
- `appendSessionTokenRebindHint`: targets invalid access token patterns only, skips if hint already present, appends Chinese rebind message.

**No issues found.**

---

### 6. Daily Summary (`service/daily/`)

| File | Verdict | Details |
|------|---------|---------|
| `daily_summary.go` | PARTIAL | Metrics collection has proxy stubs. Notification builder passes. |

**What matches spec**:
- `CollectDailySummaryMetrics`: account aggregation (total/active/lowBalance), checkin log aggregation (success/skipped/failed + reward parsing + account-level rollup), `estimateRewardWithTodayIncomeFallback` per-account, `BuildDailySummaryNotification` with all spec fields.
- `lowBalanceAccounts` thresholds at `balance < 1`.
- Net value = `round6(todayReward - todaySpend)`.
- formatTokens with comma grouping.

**Gap**:

| # | Issue | Severity |
|---|-------|----------|
| D1 | **Proxy log section is entirely stubbed**. `_ = db` with comment "Simplified — in full implementation, query proxy_logs table". All six proxy metrics (total, success, failed, totalTokens, todaySpend) are always 0. | MEDIUM |
| D2 | `SendDailySummary` exists but has no cron trigger wiring in `app/services.go` -- needs to be called by the scheduler to satisfy the 23:58 daily trigger acceptance criterion. | MEDIUM |

---

### 7. Failure Reason Service (`service/checkin/failure_reason.go`)

PASS with one note.

- All 10 priority levels match spec (site_disabled → checkin_not_supported → manual_turnstile → cloudflare_tunnel → cloudflare_challenge → token_expired → already_checked_in → network_timeout → upstream_error → unknown_error).
- Priority ordering tests confirm correctness (priority 1 beats 8, 2 beats 5, 3 beats 6, 6 beats 8, 8 beats 10).
- `ClassifyFailureInput` accepts `Message`, `Status`, `HTTPStatus` -- matches spec.
- Priority 9 `upstream_error` checks `httpStatus >= 500 || text contains 'http 5'/'upstream'/'internal server error'`.
- Priority 10 has a divergence: when `status == "success"`, it returns "执行成功" title. TS's `classifyFailureReason` doesn't have this path (it's a logging classification, not a success classifier). This is a non-harmful extension.

---

## Cross-Cutting Issues

| # | Issue | Impact |
|---|-------|--------|
| X1 | **7 integration-level stubs prevent end-to-end correctness**: Sub2Api pre-refresh (B1), Sub2Api retry (B2), balance refresh from checkin (C3), delta inference (C2), proxy on auto-relogin (C1), content-based income parsing (B3), Telegram proxy (N2), SMTP TLS (N3), proxy logs in daily summary (D1). | Blocks production readiness |
| X2 | **Generic webhook empty fields (N1)** is a spec violation, not a stub -- it produces incorrect runtime behavior even if fully wired. | Data loss for generic webhook consumers |
| X3 | **`throwOnFailure` semantic mismatch (N4)**: TS callers can catch the thrown error; Go callers see a silent log. Any caller checking the DispatchResult for failure must also check the error return, which doesn't exist. | API contract break |
| X4 | **Test coverage for integration flows is absent**. Spec test plan lists 8 test files covering CheckinAccount flow, CheckinAll grouping, auto-relogin, RefreshBalance flow, etc. Current tests cover pure functions only (parsers, matchers, throttle, classifier, formatters). Integration tests that drive `CheckinAccount`/`CheckinAll`/`RefreshBalance`/`SendNotification` through mock adapters are missing. | Cannot verify spec compliance without integration tests |

---

## Test Coverage Matrix

| Spec Test Plan File | Go Test File | Status |
|---------------------|--------------|--------|
| `checkin_test.go` (success/already/skipped/failed + CheckinAll + auto-relogin + 12 patterns + 6 unsupported + Turnstile + reward inference + skipEvent + scheduleMode) | `service/checkin/checkin_test.go` | **PARTIAL** -- pure-function tests only (message detection, status constants, result structs). No integration flow tests. |
| `reward_parser_test.go` | `service/checkin/reward_parser_test.go` | **PASS** -- Comprehensive: numbers, strings, commas, text, negatives, NaN, non-string/non-number. |
| `failure_reason_test.go` | `service/checkin/failure_reason_test.go` | **PASS** -- All 10 categories, priority ordering, edge cases. |
| `balance_test.go` (apikey skip + Sub2Api + todayIncome + three-way retry + snapshot + degraded preservation + auto-activation) | `service/balance/balance_test.go` | **PARTIAL** -- Pure-function tests only (shouldAttemptAutoReloginBalance, supportsTodayIncomeLogFallback, quota factor, helpers). No RefreshBalance flow tests. |
| `today_reward_test.go` | `service/today_reward_test.go` | **PASS** -- Exists (not reviewed line-by-line in this report, but function signatures in production code match spec). |
| `notify_test.go` (5 channels + throttle + merge + prune + cooldown=0 + WeCom/Feishu + generic 6-field) | `service/notify/notify_test.go` + `throttle_test.go` | **PARTIAL** -- Throttle: PASS (all scenarios). Notify: channel detection, text formatting. No integration dispatch test. Generic webhook 6-field test exists but only checks payload length, not content -- would miss N1 bug. |
| `alert_test.go` | `service/alert/alert_test.go` | **PASS** -- All matchers, exclusions, hints, params. |
| `daily_summary_test.go` | `service/daily/daily_summary_test.go` | **PASS** -- Notification builder, formatTokens, net value, zero-handling. No metrics collection test (cannot test without DB). |

---

## Acceptance Criteria Tracker

| # | Criterion | Status |
|---|-----------|--------|
| AC1 | Checkin success → checkin_logs (status='success') + lastCheckinAt update | **PARTIAL** -- logic present, no integration test |
| AC2 | Checkin failed → checkin_logs (status='failed') + runtimeHealth='unhealthy' | PASS |
| AC3 | Checkin skipped (site disabled/unsupported/manual) → checkin_logs (status='skipped') + correct health | PASS |
| AC4 | 12 种 "已签到" 模式 → success + balance refresh + cron/interval lastCheckinAt difference | PASS |
| AC5 | apikey checkin still executes; balance refresh skips (proxy_only) | PASS |
| AC6 | auto-relogin on token expired (checkin + balance each have own logic) | **PARTIAL** -- checkin: PASS logic, proxy missing. balance: PASS logic with extra patterns |
| AC7 | balanceService shouldAttemptAutoRelogin extra patterns | PASS |
| AC8 | Balance refresh updates only balance/balanceUsed/quota/status/lastBalanceRefresh/updatedAt | PASS |
| AC9 | todayIncome log fallback for 4 platforms | **PARTIAL** -- structure correct, content parsing broken (B3) |
| AC10 | todayIncomeSnapshot written to extraConfig | PASS |
| AC11 | Sub2Api managed session pre-refresh + retry | **FAIL** -- both stubbed (B1, B2) |
| AC12 | 5 notification channels independent | **PARTIAL** -- 3 of 5 have gaps (webhook N1, telegram N2, SMTP N3) |
| AC13 | throttle signature = "level\|\|title\|\|message", cooldown configurable, 0=bypass | PASS |
| AC14 | cooldown merge message: "[通知合并] 冷静期内已合并 N 条" | PASS |
| AC15 | throttle prune at max(cooldown*6, 600s) | PASS |
| AC16 | channel failure doesn't block other channels | PASS |
| AC17 | Webhook auto-detects WeCom/Feishu with correct format | PASS (but generic empty fields N1) |
| AC18 | Bark URL with group/level params, URI-encode | PASS |
| AC19 | Telegram message_thread_id (positive only), custom API base, system proxy | **PARTIAL** -- proxy stubbed (N2) |
| AC20 | SMTP plain text, fingerprint cache, config-change rebuild | **PARTIAL** -- TLS missing (N3), cache non-functional |
| AC21 | Daily summary at 23:58 + notification | **PARTIAL** -- builder passes, proxy metrics stubbed, cron wiring missing |
| AC22 | reportTokenExpired: events + status='expired' + runtimeHealth + notification | PASS |
| AC23 | appendSessionTokenRebindHint for invalid access token | PASS |
| AC24 | CheckinAll site-group serial, cross-site parallel, skipEvent=true | PASS |
| AC25 | CheckinAll no summary notification, returns results[] only | PASS |
| AC26 | RefreshAllBalances no refreshModelsAndRebuildRoutes | PASS |
| AC27 | Site disabled: checkin returns skipped, balance returns existing values | PASS |
| AC28 | Account expired auto-activation on success | PASS |
| AC29 | Checkin unsupported degraded preserved during balance refresh | PASS |
| AC30 | platformUserId auto-guess + persist to extraConfig | PASS |
| AC31 | All adapter calls through withAccountProxyOverride | **FAIL** -- auto-relogin login bypasses proxy (C1) |

---

## Priority-Ranked Action Items

### P0 (blocks production correctness)

1. **[C1]** Apply proxy in `tryAutoRelogin` -- wrap `adp.Login()` call (checkin.go:138)
2. **[N1]** Fill generic webhook timestamp/localTime/timeZone fields (webhook.go:41-48)
3. **[B3]** Fix `parseIncomeFromContent` to actually extract numbers from log content text (balance.go:226-237)
4. **[C2 + C3]** Wire `InferRewardFromBalanceDelta` and post-checkin `RefreshBalance` call
5. **[B1 + B2]** Implement Sub2Api managed session pre-refresh and retry (or defer to a future milestone if Sub2Api is not yet needed)

### P1 (significant gaps)

6. **[N4]** Make `throwOnFailure` return the error instead of just logging
7. **[N2]** Implement Telegram system proxy via HTTP transport proxy configuration
8. **[N3]** Add SMTP TLS/STARTTLS support (secure flag)
9. **[D1]** Implement proxy_logs query section in daily summary metrics collection

### P2 (integration test coverage)

10. Write `CheckinAccount` integration tests (mock adapter, verify DB writes, verify notification calls)
11. Write `RefreshBalance` integration tests (apikey skip, todayIncome fallback, three-way error retry)
12. Add generic webhook payload field content assertions to `notify_test.go`

### P3 (polish)

13. Wire daily summary cron trigger in application scheduler
14. Implement functional SMTP transporter caching (fingerprint comparison + reuse)
15. Consider removing or documenting `ClassifyFailureReason` priority-10 "执行成功" special case as intentional extension

---

## Files Reviewed

| Path | Lines | Status |
|------|-------|--------|
| `service/checkin/checkin.go` | 501 | Core logic: 3 stubs |
| `service/checkin/reward_parser.go` | 86 | PASS |
| `service/checkin/failure_reason.go` | 174 | PASS (minor divergence noted) |
| `service/balance/balance.go` | 527 | Core logic: 3 stubs |
| `service/today_reward.go` | 142 | PASS |
| `service/notify/notify.go` | 217 | 1 behavior gap |
| `service/notify/throttle.go` | 95 | PASS |
| `service/notify/webhook.go` | 130 | 1 bug (empty fields) |
| `service/notify/bark.go` | 41 | PASS |
| `service/notify/serverchan.go` | 40 | PASS |
| `service/notify/telegram.go` | 87 | 1 stub |
| `service/notify/smtp.go` | 55 | 2 gaps |
| `service/alert/alert.go` | 94 | PASS |
| `service/alert/rules.go` | 100 | PASS |
| `service/daily/daily_summary.go` | 204 | 1 stub |
| `service/checkin/checkin_test.go` | 329 | PASS (pure functions) |
| `service/checkin/reward_parser_test.go` | 129 | PASS |
| `service/checkin/failure_reason_test.go` | 284 | PASS |
| `service/balance/balance_test.go` | 334 | PASS (pure functions) |
| `service/notify/throttle_test.go` | 384 | PASS |
| `service/notify/notify_test.go` | 250 | PASS (channel detection + formatting) |
| `service/alert/alert_test.go` | 329 | PASS |
| `service/daily/daily_summary_test.go` | 222 | PASS |
| `service/today_reward_test.go` | -- | Present, not line-reviewed |
