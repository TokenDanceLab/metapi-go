# P3 Implementation Review: Sites + Accounts + AccountTokens

**Date**: 2026-07-04
**Review scope**: Cross-reference Go implementation against P3 spec and TS source
**Files reviewed**:
- Spec: `D:/Code/TokenDance/metapi-go/docs/specs/p3-sites-accounts.md`
- TS contracts: `siteRoutePayloads.ts`, `accountsRoutePayloads.ts`, `accountTokensRoutePayloads.ts`
- TS routes: `sites.ts`, `accounts.ts`, `accountTokens.ts`
- Go handlers: `sites.go`, `sites_endpoints.go`, `accounts.go`, `account_tokens.go`
- Go payloads: `payloads/sites.go`, `payloads/accounts.go`, `payloads/account_tokens.go`
- Go services: `site_service.go`, `site_detect.go`, `site_endpoint_service.go`, `account_service.go`, `account_credential.go`, `account_token_service.go`

---

## Recommendation: NEEDS_FIX

The implementation covers all 34 required endpoints with correct payload shapes and AES encryption, but contains 4 blocking issues in cache invalidation, route rebuilding, and scope creep that must be resolved before merging.

---

## 1. Endpoint Completeness: PARTIAL (scope creep)

All 34 spec endpoints are registered. **However**, `sites_endpoints.go` registers 4 extra REST endpoints that contradict the spec:

| Extra endpoint | Method | Spec position |
|---|---|---|
| `/api/sites/{id}/api-endpoints` | GET | Not in spec |
| `/api/sites/{id}/api-endpoints` | POST | Not in spec |
| `/api/sites/{id}/api-endpoints/{epId}` | PUT | Not in spec |
| `/api/sites/{id}/api-endpoints/{epId}` | DELETE | Not in spec |

The spec explicitly states (line 519):
> `apiEndpoints` 是 site 的**嵌入字段**, 不是独立 REST 资源

The TS implementation has no such REST endpoints -- apiEndpoints are managed entirely through the `apiEndpoints` array embedded in POST/PUT `/api/sites` payloads and returned in GET `/api/sites` responses.

**Fix**: Remove `registerSiteEndpointRoutes` and `sites_endpoints.go`, or gate them behind a feature flag clearly documented as extensions beyond spec.

---

## 2. Cascade Delete Behavior: BROKEN

### 2.1 Site delete -- missing cache invalidation

**Spec** (line 451): After site delete, call `invalidateSiteProxyCache()` + `invalidateTokenRouterCache()`.

**Go** (`sites.go:398`):
```go
func (h *sitesHandler) deleteSite(w http.ResponseWriter, r *http.Request) {
    idStr := chi.URLParam(r, "id")
    id, _ := strconv.ParseInt(idStr, 10, 64)
    service.DeleteSite(h.db, id)   // error is IGNORED
    writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
```

- **Error ignored**: `service.DeleteSite` error is silently discarded.
- **No cache invalidation**: Neither `invalidateSiteProxyCache()` nor `invalidateTokenRouterCache()` is called.

### 2.2 Account delete -- missing route rebuild

**Spec** (line 456): After account delete, call `rebuildRoutesBestEffort()`.

**Go** (`accounts.go:513`):
```go
func (h *accountsHandler) deleteAccount(w http.ResponseWriter, r *http.Request) {
    idStr := chi.URLParam(r, "id")
    id, _ := strconv.ParseInt(idStr, 10, 64)
    service.DeleteAccount(h.db, id)   // no route rebuild
    writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
```

No `rebuildRoutesBestEffort()` call. The TS equivalent (`accounts.ts:1577-1581`) explicitly calls `await rebuildRoutesBestEffort()`.

### 2.3 Account batch delete -- route rebuild is a dead stub

**Go** (`accounts.go:562`):
```go
case "delete":
    h.db.Exec("DELETE FROM accounts WHERE id = ?", id)
    shouldRebuildRoutes = true
// ...
_ = shouldRebuildRoutes // Stub: P4 route rebuild
```

`shouldRebuildRoutes` is set but discarded via `_ =`. The TS version calls `await rebuildRoutesBestEffort()` after the loop.

**Fix**: Implement `rebuildRoutesBestEffort()` (or at minimum a no-op function with a TODO) and call it in all three locations.

### 2.4 Site status side effects -- no cache invalidation

**Spec**: After status change, invalidate site caches.

**Go** (`sites.go:384-388`): `ApplySiteStatusSideEffects` is called but no `invalidateSiteCaches()` follows. The TS `applySiteStatusSideEffects` in `sites.ts` always calls `invalidateSiteCaches()` after any update (line 730: `invalidateSiteCaches()` is outside `applySiteStatusSideEffects` but always executed after it).

---

## 3. AES Encryption: PASS

### 3.1 Algorithm

| Aspect | Spec | Go | Status |
|---|---|---|---|
| Algorithm | AES-256-GCM | `crypto/aes` + `cipher.NewGCM` | PASS |
| Key derivation | SHA-256(`AccountCredentialSecret`) | `sha256.Sum256([]byte(secret))` | PASS |
| Key fallback | `AuthToken` if `AccountCredentialSecret` empty | `cfg.AuthToken` if empty | PASS |
| Ultimate fallback | `"change-me-admin-token"` | `"change-me-admin-token"` | PASS |
| IV | 12 random bytes | `make([]byte, 12)` + `rand.Reader` | PASS |
| Auth tag | 16 bytes | GCM auto-appended, 16 bytes extracted | PASS |
| Ciphertext format | `v1:base64url(iv):base64url(tag):base64url(ciphertext)` | Exact match | PASS |
| Encoding | base64url (no padding) | `base64.RawURLEncoding` | PASS |
| Decrypt failure | Silent return null/error | Returns empty string on any failure | PASS |

### 3.2 Encryption target

**Spec (line 363)**: "`autoRelogin.password` (stored in `extraConfig.autoRelogin.passwordCipher`), NOT `accessToken`."

**Go**: `EncryptPassword` is called in `accounts.go:295` during login and the ciphertext stored under `extraConfig.autoRelogin.passwordCipher`. `accessToken` is stored as plaintext. Correct.

### 3.3 Credential mode resolution

`ResolveStoredCredentialMode` (Go) matches TS spec:
1. Read `extraConfig.credentialMode` -> if explicit AND != "auto", return it
2. If `accessToken` non-empty -> return "session"
3. Otherwise -> return "apikey"

`ResolveRequestedCredentialMode` (Go) matches TS spec:
1. Read `credentialMode` from input, normalize
2. If valid -> return it; else return "auto"

---

## 4. Payload Field Accuracy: MINOR ISSUES

### 4.1 SiteCreatePayload -- initializationPresetId validation missing

**Spec (line 112-114)**: If `initializationPresetId` is provided, it must match the detected platform. TS validates this explicitly.

**Go**: The `InitializationPresetID` field exists in the payload struct but is never read or validated in `createSite`. If a user provides a mismatched preset, the Go implementation silently ignores it rather than rejecting with error 400.

### 4.2 AccountCreatePayload -- tokenExpiresAt unused

**Spec**: `tokenExpiresAt` (epoch seconds, number or numeric string) is documented in `AccountCreatePayload`.

**Go**: The field exists in the payload struct but is never used in `createSingleAccount`. It should be stored in `extraConfig` for session-mode accounts.

### 4.3 AccountUpdatePayload -- extraConfig merge semantics differ

**Spec**: Merges new extraConfig with existing extraConfig.

**Go** (`accounts.go:468-473`):
```go
if body.ExtraConfig != nil {
    b, err := json.Marshal(body.ExtraConfig)
    if err == nil {
        s := string(b)
        updates["extraConfig"] = s
    }
}
```
This overwrites the entire `extraConfig` with the request body value. The TS version uses `mergeAccountExtraConfig` which preserves existing keys not mentioned in the update.

### 4.4 Error response key inconsistency

Batch endpoints use `"message"` as the error key (e.g., `accounts.go:522: "message": "Invalid action"`), while non-batch endpoints use `"error"` (e.g., `sites.go:63: "error": "Invalid name."`). TS consistently uses `"error"` for structure validation and `"message"` for business logic errors. The Go implementation blurs this distinction for batch payloads.

---

## 5. Business Logic Gaps

### 5.1 Sub2API managed auth not implemented

**Spec (lines 528-542)**: When platform is `sub2api`, rebind-session and account update must handle `extraConfig.sub2apiAuth` with `refreshToken` and `tokenExpiresAt`.

**Go**: Neither `rebindSession` nor `updateAccount` handle sub2api managed auth. The `RefreshToken` and `TokenExpiresAt` fields in the payload structs are not used.

### 5.2 Expired API key recovery not implemented

**Spec (lines 594-602)**: When updating an expired API key account, automatically trigger model refresh with `allowInactive: true` and `reactivateAfterSuccessfulModelRefresh: true`.

**Go**: No detection of expired status or recovery logic. The TS `accounts.ts:1550-1556` has complex logic for this.

### 5.3 Accounts GET snapshot caching not implemented

**Spec (lines 510-516)**: `GET /api/accounts` uses `getAccountsSnapshot` with TTL-based in-memory cache. `?refresh=true` forces refresh.

**Go**: The `?refresh` param is accepted but ignored. No caching -- data is always fresh. The `x-accounts-snapshot-cache` header is hardcoded to `"miss"`. No TTL or cache store.

### 5.4 Token groups fetched from wrong source

**Spec (lines 100-101)**: `GET /api/account-tokens/groups/:accountId` returns token groups.

**Go** (`service/account_token_service.go:377-391`): Fetches `DISTINCT COALESCE(token_group, 'default')` from the local `account_tokens` table.

**TS** (`accountTokens.ts:939-944`): Calls `adapter.getUserGroups()` on the upstream site.

These produce different results: Go returns what is already in the local DB; TS returns what is available upstream. This matters for platforms where token groups are managed server-side.

### 5.5 AccountToken upstream-first delete not implemented

**Spec (lines 484-492)**: Token delete must try upstream deletion first; if upstream delete fails, abort local delete.

**Go** (`account_tokens.go:449-478`): Local-only delete. The upstream-first logic is skipped entirely (P4 stub).

### 5.6 AccountToken sync -- P4 stub with no fallback

**Spec (lines 670-678)**: Sync calls `adapter.getApiTokens()` with 15s timeout, falls back to `adapter.getApiToken()`, then `convergeAccountMutation`.

**Go** (`account_tokens.go:483-510`): Hardcoded stub returning `"status": "skipped", "reason": "no_upstream_tokens"`. No upstream calls.

---

## 6. Stub Inventory (P4-dependent)

The following endpoints are correctly identified as P4 stubs but are non-functional:

| Endpoint | Go status | Expected P4 phase |
|---|---|---|
| `POST /api/sites/:id/probe-now` | Returns empty results | P4 adapter |
| `GET /api/sites/:id/probe-stream` | SSE headers set but no real probe | P4 adapter |
| `POST /api/accounts/login` | Simulated session token | P4 adapter.login |
| `POST /api/accounts/verify-token` | Hardcoded success | P4 adapter.verifyToken |
| `POST /api/accounts/:id/balance` | Returns existing balance | P4 balance refresh |
| `POST /api/accounts/health/refresh` | Returns empty summary | P4 health check |
| `POST /api/account-tokens` (upstream) | Returns 502 | P4 adapter.createApiToken |
| `POST /api/account-tokens/sync/:accountId` | Returns skipped | P4 adapter.getApiTokens |
| `POST /api/account-tokens/sync-all` | Returns queued (no-op) | P4 adapter sync |
| `POST /api/accounts/:id/models/manual` | No route rebuild | P4 route rebuild |

These are acceptable for the current P3 phase. The spec test plan (line 746-756) acknowledges these as P4 adapter dependencies.

---

## 7. Site Status Side Effects: PASS (with caches caveat)

`ApplySiteStatusSideEffects` (`site_service.go:354-384`) correctly implements:

- **Disable**: Sets all site accounts to `disabled`, creates event `{type:"status", title:"站点已禁用", level:"warning"}`
- **Enable**: Restores only previously-disabled accounts to `active`, creates event `{type:"status", title:"站点已启用", level:"info"}`

This matches TS `applySiteStatusSideEffects` in `sites.ts:383-427`. The only missing piece is cache invalidation after the side effects (covered in section 2.4).

---

## 8. Token Value Status System: PASS

The `TokenValueStatusReady` / `TokenValueStatusMaskedPending` handling is correct:

- `IsMaskedTokenValue()`: Checks for `*` or `*` (bullet) characters
- Masked tokens: `enabled=false`, `isDefault=false` enforced in create and update
- `SetDefaultToken` blocked for masked tokens
- GET `/api/account-tokens/:id/value` returns 409 for masked
- Token update: providing plaintext switches status back to `ready`
- `ResolveAccountTokenValueStatus()` delegates to both `value_status` column and runtime check of token content

---

## 9. Batch Operations: PASS (with route rebuild caveat)

All batch action sets match spec:

| Resource | Spec actions | Go actions | Status |
|---|---|---|---|
| Sites | enable, disable, delete, enableSystemProxy, disableSystemProxy | identical | PASS |
| Accounts | enable, disable, delete, refreshBalance | identical | PASS |
| AccountTokens | enable, disable, delete | identical | PASS |

Response format `{success, successIds, failedItems}` matches spec. The only issue is the route rebuild stub for account batch delete (section 2.3).

---

## 10. API Endpoints Embedded Management: PARTIAL

### Correct behavior:
- `normalizeAPIEndpointsInput()` validates URLs, deduplicates, normalizes sort order
- `UpsertSiteAPIEndpoints()` implements full-replace semantics in a transaction
- `LoadSiteWithEndpoints()` attaches endpoints (sorted by sortOrder, id)
- Duplicate URL detection returns user-friendly error

### Incorrect: Extra REST endpoints
The 4 extra CRUD endpoints in `sites_endpoints.go` contradict the "embedded, not separate REST resource" design. These should be removed or explicitly documented as deviation from spec.

---

## 11. Platform Detection: PASS (enhanced)

The spec says "stub implementation" but the Go code includes a comprehensive heuristic detection covering 20+ platforms (openai, anthropic, gemini, deepseek, moonshot, dashscope, baichuan, zhipu, minimax, stepfun, bytedance, siliconflow, modelscope, mistral, cohere, together, fireworks, groq, perplexity, xai, huggingface, azure, github-copilot, claude, new-api). This exceeds the spec requirement and is production-ready.

---

## 12. URL Normalization: PASS

`CanonicalizeSiteURL()`, `NormalizeSiteAPIEndpointBaseUrl()`, `IsValidProxyURL()`, `IsValidHTTPURL()` all correctly strip query params, fragments, and trailing slashes; validate http/https/socks schemes.

---

## Summary of Required Fixes

### BLOCKING (must fix before merge):

| # | Issue | File | Fix |
|---|---|---|---|
| 1 | Extra api-endpoints REST endpoints | `sites_endpoints.go` | Remove or gate behind feature flag |
| 2 | No cache invalidation after site delete | `sites.go:395-400` | Call `InvalidateSiteProxyCache()` + `InvalidateTokenRouterCache()` |
| 3 | No route rebuild after account delete | `accounts.go:513` | Call `rebuildRoutesBestEffort()` |
| 4 | Account batch delete route rebuild stubbed | `accounts.go:571` | Call `rebuildRoutesBestEffort()` instead of `_ =` |

### HIGH (should fix before P4):

| # | Issue | File | Fix |
|---|---|---|---|
| 5 | Sub2API managed auth not handled | `accounts.go:374-422, 428-506` | Implement `sub2apiAuth` merge in rebind and update |
| 6 | No snapshot caching for GET accounts | `accounts.go:44-64` | Implement TTL cache with `?refresh` support |
| 7 | Token groups from local DB not upstream | `account_token_service.go:377-391` | Call adapter.getUserGroups() or document deviation |
| 8 | initPresetId validation missing | `sites.go:127-131` | Validate against detected platform |
| 9 | deleteSite ignores error | `sites.go:395-400` | Handle error, return 500 on failure |
| 10 | No cache invalidation after site status change | `sites.go:385-390` | Call invalidate caches after ApplySiteStatusSideEffects |

### MEDIUM:

| # | Issue | File | Fix |
|---|---|---|---|
| 11 | ExtraConfig overwrite vs merge | `accounts.go:467-474` | Use MergeExtraConfig instead of direct assignment |
| 12 | TokenExpiresAt unused in account create | `accounts.go:202-246` | Store in extraConfig for session accounts |
| 13 | Error response key inconsistency (message vs error) | Multiple | Standardize: use "error" for validation, "message" for business |
