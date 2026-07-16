# Upstream Error Classification (R0 / #24)

**Date:** 2026-07-16  
**Lane:** reliability  
**SSOT code:** `platform/error_classification.go`  
**Mark path consumers:** `service/alert.IsTokenExpiredError` → balance/checkin `ReportTokenExpired`; proxy `SurfaceFailureToolkit` (wired via `ShouldMarkAccountExpired`)

## Purpose

Prevent non-auth upstream failures (billing, model capability, validation, rate-limit) from flipping `accounts.status` to `expired`. Auto-relogin and UI failure-reason labels may still use broader heuristics; **only `ClassExpired` may mark the account expired**.

## Signal → Class → Action

| Signal (status / message) | Class | Action |
|:--------------------------|:------|:-------|
| `jwt expired`, `token expired`, `access token expired` | **expired** | Mark `accounts.status=expired`; event + notify + runtime health auth-unhealthy; allow auto-relogin |
| `invalid access token` / `access token is invalid` / `access token无效` | **expired** | Same as above |
| `令牌/访问令牌` + `过期/无效` | **expired** | Same as above |
| HTTP **401** with **empty** body | **expired** | Same as above (legacy bare-401 policy; residual risk below) |
| `HTTP 401 Unauthorized` / clear 401 + unauthorized | **expired** | Same as above |
| HTTP 401/403 with auth wording but **no** expiry/invalid-credential phrase | **auth** | Do **not** mark expired; auto-relogin callers may still try via broader local heuristics |
| `未登录且未提供 access token` (NewAPI probe) | **auth** | Do **not** mark expired |
| `invalid_argument` / `invalid_request_error` / `input token limit` / `context length` / `max_tokens` / dispatch denied | **validation** | Do **not** mark; do not treat as credential loss |
| `model … is not supported` / `unsupported model` / `不支持…模型` / `model_not_found` | **model** | Do **not** mark; channel/model failover only |
| `billing` / `payment method` / `insufficient_quota` / `quota exceeded` / `余额不足` / `额度不足` | **billing** | Do **not** mark; surface as quota/billing health if needed |
| HTTP 408/409/425/429/5xx, `rate limit`, `timeout`, `bad gateway`, Cloudflare challenge | **transient** | Do **not** mark; retry / cooldown / breaker |
| Opaque 401 body without auth/billing/model/validation signal | **unknown** | Do **not** mark expired |
| Empty message + non-401 status | **unknown** | No mark |

### Action legend

| Action | Meaning |
|:-------|:--------|
| **Mark expired** | `ReportTokenExpired` → `UPDATE accounts SET status='expired'` + event + notification + `HealthSourceAuth` |
| **Auto-relogin** | Balance/checkin may re-login when local `shouldAttemptAutoRelogin*` is true (broader than mark) |
| **Retry / failover** | Proxy `ShouldRetryProxyRequest` / channel penalty — independent of account status |
| **No account status write** | Classification only; health may still go unhealthy from the calling job |

## Call-site matrix

| Call site | Uses classifier? | Marks expired? |
|:----------|:-----------------|:---------------|
| `service/alert.IsTokenExpiredError` | Yes (delegates to `platform`) | Indirect — callers decide |
| `service/balance.handleBalanceError` | Via alert | Yes if expired class |
| `service/checkin` post-failure | Via alert | Yes if expired class |
| `service/checkin.ClassifyFailureReason` | Via alert (display) | No (UI only) |
| `proxy.SurfaceFailureToolkit.HandleUpstreamFailure` | `platform.ShouldMarkAccountExpired` | Yes when hook set |
| `platform.resolveGroupFetchErrorMessage` | Session UX string only | No |
| `platform.isCookieSessionFailureMessage` | Cookie/session retry heuristic | No |
| `auth/downstream` managed-key `ExpiresAt` | Separate concept | No (`downstream_api_keys`, not accounts) |

## Residual risks

1. **Bare HTTP 401 + empty body** still classifies as **expired** (legacy parity with TS / auto-relogin). Any upstream that returns 401 with an empty body for non-auth reasons can still mark the account. Mitigation later: require a second signal or consecutive failures.
2. **Substring heuristics remain**. Novel billing/quota wording without `billing`/`quota`/`payment`/`余额` may fall through to unknown (safe: no mark) or, if paired with 401 + `unauthorized`, to expired (unsafe). Expand exclusion lists when new providers appear.
3. **Balance auto-relogin is broader than mark** (`unauthorized`/`forbidden`/`not login`). Relogin can run without marking; marking still requires **expired** class.
4. **Checkin failure_reason priority** can show Turnstile/Cloudflare over token_expired in UI while the post-failure mark path uses the raw message classifier independently — after R0 they stay consistent on non-auth exclusions, but priority demotion is display-only.
5. **Intermittent false positives flap status**: successful balance/checkin still revive `expired → active`. Prefer fixing classification over removing revive.
6. **`ReportTokenExpired` DB `Exec` error is still unchecked** (pre-existing) — mark may silently fail while event/notify run.
7. **Downstream API key `expires_at`** and sticky-session TTL cleanup are unrelated clocks; do not conflate with account token expiry.

## Tests

- `platform/error_classification_test.go` — matrix + non-auth never-mark table + positive auth signals
- `service/alert` tests continue to cover historical `IsTokenExpiredError` surface via thin delegate
- `proxy` failure toolkit tests cover mark-hook invocation only on expired class

## Non-goals (R0)

- Schema / UI changes
- Replacing all string matching with typed upstream errors
- Changing OAuth refresh policy or circuit-breaker penalties beyond classification guards
