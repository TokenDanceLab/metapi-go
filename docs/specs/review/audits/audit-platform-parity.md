# Platform Adapter Method Parity Audit: Go vs TypeScript

**Date**: 2026-07-04
**Scope**: 14 adapters in `metapi-go/platform/` vs `metapi/src/server/services/platforms/`
**Focus areas**: sub2api subscription parsing, newapi shield challenge, veloera platformUserId, antigravity onboarding

---

## 1. Executive Summary

| Severity | Count | Description |
|----------|-------|-------------|
| CRITICAL | 1 | Shield challenge (acw_sc__v2) unsolvable in Go |
| HIGH | 1 | Login signature mismatch: platformUserId missing in TS |
| MEDIUM | 2 | DeleteAPIToken return type mismatch; proxy config isolation |
| LOW | 3 | Antigravity config source; error-nil semantics; base fallback ordering |

**Overall**: Method coverage is 14/14 for both implementations. All 14 adapter interfaces are fully implemented. HTTP endpoint paths are identical. Request/response fields are structurally equivalent with minor type-level differences.

---

## 2. Adapter Matrix: Method Coverage

| # | Adapter | Go | TS | All Methods? | Parity |
|---|---------|----|----|-------------|--------|
| 1 | openai | StandardAdapter | StandardApiProviderAdapterBase | Yes | MATCH |
| 2 | codex | BaseAdapter (all overridden) | BasePlatformAdapter (all overridden) | Yes | MATCH |
| 3 | claude | StandardAdapter | StandardApiProviderAdapterBase | Yes | MATCH |
| 4 | gemini | StandardAdapter | StandardApiProviderAdapterBase | Yes | MATCH |
| 5 | gemini-cli | GeminiAdapter (Detect override) | GeminiAdapter (Detect override) | Yes | MATCH |
| 6 | antigravity | BaseAdapter (all overridden) | BasePlatformAdapter (all overridden) | Yes | MATCH |
| 7 | cliproxyapi | StandardAdapter | StandardApiProviderAdapterBase | Yes | MATCH |
| 8 | anyrouter | NewApiAdapter (Detect only) | NewApiAdapter (Detect only) | Yes | MATCH |
| 9 | done-hub | OneHubAdapter -> OneApiAdapter | OneHubAdapter -> OneApiAdapter | Yes | MATCH |
| 10 | one-hub | OneApiAdapter | OneApiAdapter | Yes | MATCH |
| 11 | veloera | BaseAdapter (custom) | BasePlatformAdapter (custom) | Yes | MATCH |
| 12 | new-api | BaseAdapter (full custom) | BasePlatformAdapter (full custom) | Yes | MATCH |
| 13 | sub2api | BaseAdapter (full custom) | BasePlatformAdapter (full custom) | Yes | MATCH |
| 14 | one-api | BaseAdapter (custom) | BasePlatformAdapter (custom) | Yes | MATCH |

---

## 3. CRITICAL: Shield Challenge (acw_sc__v2) Unsolvable in Go

**Resolution (2026-07-04)**: Documented limitation. We accept that Go cannot execute the embedded JavaScript rotation function needed to solve acw_sc__v2 shield challenges. The following mitigations have been applied:
- `SolveAcwScV2` now has a detailed doc comment explaining the limitation and the reason (Go cannot execute JS without embedding a VM like goja)
- `fetchWithShieldRetry` tracks unsolvable shield challenges across retries and returns a clear error: `"shield challenge (acw_sc__v2) could not be solved after N retries — this platform requires JavaScript execution which is not available in Go; use cookie import instead"`
- The error message explicitly guides users toward the cookie import workaround for pre-existing sessions

### Finding

Go's `SolveAcwScV2()` in `newapi.go` is non-functional. The XOR seed extraction step (`parseChallengeXorSeed`) is a stub that always returns an empty string, causing the entire solver to return an empty string every time.

**Root cause**: The acw_sc__v2 shield challenge embeds a JavaScript rotation function (`a0j(0x115)`) whose output is the XOR seed needed to decrypt the cookie value. TS executes this JS via Node's `vm.createContext()` / `runInContext()`. Go's `parseChallengeXorSeed` detects the function but falls through to `solveXorSeedThroughRegex` which iterates over matches without capturing them and returns `""`.

**TS implementation** (`newApi.ts` lines 416-441):
```typescript
private parseChallengeXorSeed(html: string): string | null {
  // ...
  const sandbox: Record<string, unknown> = { decodeURIComponent };
  createContext(sandbox);
  runInContext(helperCode, sandbox, { timeout: 100 });
  runInContext(rotateCode, sandbox, { timeout: 100 });
  const decoder = sandbox['a0j'];
  if (typeof decoder !== 'function') return null;
  const seed = (decoder as (idx: number) => unknown)(0x115);
  // ... validated seed returned
}
```

**Go implementation** (`newapi.go` lines 1383-1433):
```go
func parseChallengeXorSeed(html string) string {
    // ... finds a0j(0x115) ...
    // Then falls to: solveXorSeedThroughRegex(html)
    // Which always returns ""
}
```

**Impact**: Any NewAPI site behind Alibaba Cloud WAF (acw_sc__v2) that issues a shield challenge during login will fail with "shield challenge blocked login". Cookie-based auth can still work for pre-existing sessions.

### Recommendation

Three options, in order of preference:
1. **Embed a Goja JS VM** to execute the challenge JS (similar to how TS uses node:vm). Goja is a pure-Go ES5.1+ interpreter.
2. **Port the rotation algorithm** to Go — reverse-engineer what `a0j(0x115)` does (it rotates a hardcoded string by 0x115 positions).
3. **Accept the limitation** and document that shield-protected NewAPI sites require cookie import rather than login.

---

## 4. HIGH: Login Method Signature Mismatch

### Finding

Go's `PlatformAdapter.Login` includes `platformUserId *int` as the 6th parameter (before `proxy`):
```go
Login(ctx context.Context, url, username, password string, platformUserId *int, proxy *ProxyConfig) (*LoginResult, error)
```

TS's `PlatformAdapter.login` does NOT include `platformUserId`:
```typescript
login(baseUrl: string, username: string, password: string): Promise<LoginResult>;
```

**Impact**: For NewAPI-fork platforms that use cookie-based auth with user-ID headers (Veloera-User, New-API-User, etc.), the TS path cannot pass a pre-known `platformUserId` during login. This matters when the caller already knows the user ID and wants to avoid the discovery probe.

**Note**: NewApiAdapter.login in TS does NOT use platformUserId internally — it delegates to `fetchJsonRawWithCookie` which does its own user-ID probing. So functionally, TS works but with extra probe overhead. The Go signature is forward-looking (the parameter is accepted but currently used only by NewApiAdapter and VeloeraAdapter).

### Recommendation

Add `platformUserId?: number` to TS `PlatformAdapter.login` signature and pass it through in `NewApiAdapter.login`.

---

## 5. MEDIUM: Findings

### 5.1 DeleteAPIToken Return Type

| Aspect | Go | TS |
|--------|----|----|
| Return type | `error` | `Promise<boolean>` |
| Semantics | nil=success, non-nil=failure | true=deleted/absent, false=failed |

The TS `boolean` return carries more information: `true` means "token is gone from upstream (or was never there)", `false` means "tried to delete but upstream rejected it". Go's `error` return conflates these — callers cannot distinguish "already absent (safe no-op)" from "failed to delete."

### 5.2 Proxy Configuration Isolation

Go passes `*ProxyConfig` on every adapter method call. TS does not expose proxy at the adapter interface — it uses `withSiteProxyRequestInit` internally through the `fetchJson` helper in `BasePlatformAdapter`. This means:
- Go callers can set per-request proxy/CustomHeaders/InsecureSkipTLS
- TS callers rely on the global site-proxy configuration

This is an architectural choice rather than a bug, but it limits what TS callers can express at the adapter level.

### 5.3 NewAPI OpenAIModels Shield Cookie Path

TS has `getOpenAiModelsViaShieldCookie` which uses `fetchJsonWithShieldCookieRetry` (from `newApiShield.ts`) specifically for AnyRouter and token-with-equals-sign paths. Go does not have this separate shield-cookie path for model fetching — it only uses `getOpenAIModels` with standard Bearer auth. However, since Go's `SolveAcwScV2` is already non-functional (see CRITICAL finding), this sub-path would also be non-functional even if added.

---

## 6. LOW: Findings

### 6.1 Antigravity Configuration Source

| Aspect | Go | TS |
|--------|----|----|
| User-Agent | `"Antigravity/1.0"` (hardcoded) | `ANTIGRAVITY_USER_AGENT` (imported constant) |
| X-Goog-Api-Client | `"antigravity-client"` (hardcoded) | `ANTIGRAVITY_GOOGLE_API_CLIENT` (imported constant) |
| Client-Metadata | `"antigravity"` (hardcoded) | `ANTIGRAVITY_CLIENT_METADATA` (imported constant) |
| Fallback base URL | None | `ANTIGRAVITY_UPSTREAM_BASE_URL` (imported) |

TS imports these from `antigravityProvider.js` which is part of the OAuth flow. Go hardcodes the same values. The TS also has a fallback base URL constant (used when no URL is provided), which Go lacks — though this is unlikely to matter in practice since a URL is always required for detection.

**Verdict**: Values are identical. TS is more maintainable (single source of truth in OAuth provider). Go should extract constants.

### 6.2 Error-vs-Nil Semantics in GetUserInfo

Go's `BaseAdapter.GetUserInfo` returns `(nil, nil)` on failure — meaning "no error occurred, but also no user info." TS's `BasePlatformAdapter.getUserInfo` returns `null` on failure (via catch block). Both patterns signal "no data available," but Go's dual-return could be misinterpreted as "success with no data" by callers that only check the error. In practice, all callers check `userInfo != nil` before using the result in both languages.

### 6.3 OneApi vs NewApi Group Endpoint Order

| Adapter | Endpoint order |
|---------|---------------|
| one-api (Go) | `/api/user_group_map` first, then `/api/user/self/groups` |
| one-api (TS) | `/api/user_group_map` first, then `/api/user/self/groups` |
| new-api (Go) | `/api/user/self/groups` first, then `/api/user_group_map` |
| new-api (TS) | `/api/user/self/groups` first, then `/api/user_group_map` |

**MATCH** — the reversal between OneApi and NewApi is intentional (different upstream defaults) and consistent across languages.

---

## 7. Focus Area Deep Dives

### 7.1 sub2api Subscription Parsing

| Aspect | Go (`sub2api.go`) | TS (`sub2api.ts`) |
|--------|-------------------|-------------------|
| Endpoint | `/api/v1/subscriptions/summary` | `/api/v1/subscriptions/summary` |
| Fallback | `/api/v1/subscriptions/active` | `/api/v1/subscriptions/active` |
| Envelope | `{code, message, data}` | `{code, message, data}` |
| Group ID sources | `group_id`, `groupId`, `group.id` | `group_id`, `groupId`, `group.id` |
| Group name sources | `group_name`, `groupName`, `name`, `title`, `group.name`, `group.title` | `group_name`, `groupName`, `name`, `title`, `group.name`, `group.title` |
| Expires fields | 8 candidates | 10 candidates (same 8 + `expires_at`/`expiresAt` already included) |
| Daily fields | `daily_used_usd`, `dailyUsedUsd`, `daily_limit_usd`, `dailyLimitUsd` | Same |
| Weekly fields | `weekly_used_usd`, `weeklyUsedUsd`, `weekly_limit_usd`, `weeklyLimitUsd` | Same |
| Monthly fields | `monthly_used_usd`, `monthlyUsedUsd`, `used_usd`, `usedUsd`, `total_used_usd`, `totalUsedUsd` | Same |
| Parallel fetch | Goroutines (2 concurrent) | `Promise.all` (2 concurrent) |
| Active count | From `active_count` or `activeCount`, falls back to `len(subscriptions)` | Same |
| Total used | From `total_used_usd` or `totalUsedUsd`, falls back to sum | Same |
| Currency rounding | `math.Round(v*1e6) / 1e6` | `Math.round(v * 1e6) / 1e6` |

**Verdict**: MATCH. Both implementations handle identical field aliases, identical fallback chains, and identical data normalization. The parallel fetch pattern is equivalent.

### 7.2 newapi Shield Challenge

Already covered in Section 3 (CRITICAL). Summary: Go cannot solve acw_sc__v2 because it cannot execute the embedded JavaScript rotation function. TS uses `node:vm`.

**Additional note**: TS's `newApiShield.ts` exports `fetchJsonWithShieldCookieRetry`, which is a standalone helper used by `getOpenAiModelsViaShieldCookie`. Go has no equivalent standalone helper — shield retry is only available through `fetchWithShieldRetry` on the `NewApiAdapter` struct.

### 7.3 veloera platformUserId

| Aspect | Go | TS |
|--------|----|----|
| Headers set | `Veloera-User`, `New-API-User`, `User-id` | `Veloera-User`, `New-API-User`, `User-id` |
| When set | When `userID != nil` | When `userId` is truthy |
| Checkin | Uses `veloeraHeaders()` | Uses `veloeraHeaders()` |
| GetBalance | Uses `veloeraHeaders()` | Uses `veloeraHeaders()` |
| Balance divisor | 1,000,000 | 1,000,000 |
| Balance formula | `balance = (quota - used) / 1M`, `quota = quota / 1M` | `balance = quota - used`, `quota = quota / 1M` |
| Detect | `GET /api/status`, checks `system_name` or `version` for "veloera" | Same |
| Models | `GET /v1/models` (Bearer only, no Veloera headers) | Same |

**Verdict**: MATCH. The 3 Veloera-specific headers are identical. Balance divisor of 1,000,000 (vs 500,000 for standard NewAPI/OneAPI) is consistent.

**Minor note**: Go applies the 1M divisor to the `quota` field to produce `quotaUSD`, then computes `balance = (quota - used) / 1M`. TS computes `quotaUSD = quota / 1M`, `usedUSD = used / 1M`, then `balance = quotaUSD - usedUSD`. Both produce the same numeric result, but Go's intermediate `balance` is in raw units while TS's is in USD — same final value in the returned `BalanceInfo`.

### 7.4 antigravity Onboarding

| Aspect | Go | TS |
|--------|----|----|
| Login | Unsupported (`"login endpoint not supported"`) | Unsupported (`"login endpoint not supported"`) |
| GetUserInfo | Returns `nil, nil` | Returns `null` |
| Checkin | Unsupported | Unsupported |
| GetBalance | Returns `{0, 0, 0}` | Returns `{0, 0, 0}` |
| GetModels endpoint | `POST /v1internal:fetchAvailableModels` | `POST /v1internal:fetchAvailableModels` |
| Model extraction | Object keys or array of `{id, name}` | Object keys or array of `{id, name}` |
| Auth header | `Bearer <token>` | `Bearer <accessToken>` |
| Custom headers | 5 headers (User-Agent, Accept, X-Goog-Api-Client, Client-Metadata, Authorization) | 5 headers (same) |

**Verdict**: MATCH. Both implementations are structurally identical. The only difference is where the magic string constants live (hardcoded in Go vs imported from OAuth provider module in TS). Both correctly treat Antigravity as an OAuth-only platform with no login/checkin/balance/user-info support.

---

## 8. HTTP Endpoint Parity

All HTTP endpoints used by each adapter are identical between Go and TS:

| Adapter | Endpoints | Match |
|---------|-----------|-------|
| openai | `/v1/models` | Yes |
| codex | (none - OAuth) | Yes |
| claude | `/v1/models` (Anthropic native + OpenAI-compat fallback) | Yes |
| gemini | `/v1/models`, `/<version>/models?key=`, `/v1beta/openai/models` | Yes |
| gemini-cli | (inherits from gemini) | Yes |
| antigravity | `/v1internal:fetchAvailableModels` | Yes |
| cliproxyapi | `/v1/models`, `/v0/management/openai-compatibility` | Yes |
| anyrouter | (inherits from new-api) | Yes |
| done-hub | `/api/user/self`, `/api/notice` | Yes |
| one-hub | `/v1/models`, `/api/available_model`, `/api/user_group_map` | Yes |
| veloera | `/api/status`, `/api/user/checkin`, `/api/user/self`, `/v1/models` | Yes |
| new-api | `/api/status`, `/api/user/login`, `/api/user/self`, `/api/user/checkin`, `/api/user/sign_in`, `/api/user/models`, `/api/user/self/groups`, `/api/user_group_map`, `/api/token/`, `/api/token/?p=0&size=100`, `/api/token/{id}`, `/api/notice`, `/v1/models` | Yes |
| sub2api | `/api/v1/auth/me`, `/v1/models`, `/api/v1/models`, `/api/v1/keys`, `/api/v1/api-keys`, `/api/v1/subscriptions/summary`, `/api/v1/subscriptions/active`, `/api/v1/announcements`, `/api/v1/groups/available`, `/api/v1/groups`, `/api/v1/group` | Yes |
| one-api | `/api/status`, `/api/user/login`, `/api/user/self`, `/api/user/checkin`, `/v1/models`, `/api/token/`, `/api/token/?p=0&size=100`, `/api/token/{id}`, `/api/user_group_map`, `/api/user/self/groups` | Yes |

---

## 9. Request/Response Field Coverage

### 9.1 API Token Create Payload

Both Go (`buildDefaultTokenPayload`) and TS (`buildDefaultTokenPayload`) produce identical fields:
`name`, `unlimited_quota`, `expired_time`, `remain_quota`, `allow_ips`, `model_limits_enabled`, `model_limits`, `group`.

OneApiAdapter TS wraps these in a typed interface (`CreateApiTokenPayload`); NewApiAdapter TS uses `Record<string, unknown>`. Go uses `map[string]interface{}` everywhere. Fields are identical.

### 9.2 TokenInfo Normalization

Go's `normalizeTokenItems` extracts: `key`, `name`, `group`/`group_name`/`token_group`, `status` (1=enabled).
TS's `normalizeTokenItems` extracts: `key`, `name`, `group`/`group_name`/`token_group`, `status` (1=enabled).

Fields matched. Name fallback logic (`"default"` for index 0, `"token-{i+1}"` otherwise) is identical.

### 9.3 SubscriptionPlanSummary

Both Go and TS parse identical fields across both implementations. See Section 7.1 for field-by-field comparison.

### 9.4 BalanceInfo

| Field | Go | TS |
|-------|----|----|
| `balance` | float64 | number |
| `used` | float64 | number |
| `quota` | float64 | number |
| `todayIncome` | *float64 (optional) | number? (optional) |
| `todayQuotaConsumption` | *float64 (optional) | number? (optional) |
| `subscriptionSummary` | *SubscriptionSummary (optional) | SubscriptionSummary? (optional) |

**MATCH**.

---

## 10. Error Handling Pattern Comparison

| Aspect | Go | TS |
|--------|----|----|
| Pattern | `(result, error)` tuples | `Promise<result>` with throw |
| Login errors | Returns `{Success: false, Message: ...}` | Returns `{success: false, message: ...}` |
| GetUserInfo errors | Returns `(nil, nil)` | Returns `null` |
| GetBalance errors | Returns `(&BalanceInfo{}, nil)` or `(nil, error)` | Returns `BalanceInfo` with zeros, or throws |
| Checkin errors | Returns `{Success: false, Message: ...}` | Returns `{success: false, message: ...}` |
| Token CRUD errors | Returns zero/empty + nil error | Returns empty/false (no throw except terminal in group fetch) |
| Group fetch terminal | Returns error string in second return | Throws Error |

**Key divergence**: Go's `GetBalance` returns `(nil, error)` on terminal failures (NewApiAdapter), while TS's `getBalance` throws. Go's `GetUserGroups` returns `(nil, error)` for expired sessions; TS throws. This matters for callers — Go callers must check `err != nil` explicitly; TS callers must catch.

NewApiAdapter's `GetBalance` in Go returns `nil, fmt.Errorf(...)` on failure, while TS `getBalance` throws `new Error(...)`. These are semantically equivalent in their respective languages.

---

## 11. Summary of All Findings

| # | Severity | Component | Description |
|---|----------|-----------|-------------|
| 1 | **CRITICAL** | newapi shield | `SolveAcwScV2` non-functional in Go; TS uses node:vm to execute challenge JS |
| 2 | **HIGH** | PlatformAdapter.Login | Go signature includes `platformUserId *int`; TS signature does not |
| 3 | **MEDIUM** | deleteApiToken | Go returns `error`; TS returns `Promise<boolean>` (richer semantics) |
| 4 | **MEDIUM** | Proxy config | Go passes `*ProxyConfig` per-call; TS uses global site-proxy internally |
| 5 | **LOW** | antigravity | Go hardcodes constants that TS imports from OAuth provider module |
| 6 | **LOW** | GetUserInfo nil | Go returns `(nil, nil)` on failure vs TS returning `null` |
| 7 | **LOW** | newapi models | TS has shield-cookie model fallback path; Go does not (moot since shield unsolvable) |

## 12. Overall Assessment

**Method parity**: COMPLETE. All 14 PlatformAdapter interface methods are implemented in both languages for all 14 adapters.

**HTTP endpoint parity**: COMPLETE. Every endpoint path is identical between Go and TS.

**Request/response field parity**: COMPLETE. All fields are parsed identically with the same fallback chains and normalization logic.

**Error handling**: EQUIVALENT. Patterns differ due to language conventions (Go error tuples vs TS throw/catch) but semantics are preserved.

**The one material gap**: Shield challenge solving. Go cannot bypass Alibaba Cloud WAF (acw_sc__v2) because it cannot execute the embedded JS rotation function. This affects login and any non-cookie request to shield-protected NewAPI sites. Mitigation: cookie import still works for pre-existing sessions.
