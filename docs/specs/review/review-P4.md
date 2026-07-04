# P4 Review: Platform Adapters -- Cross-Reference Against TypeScript Source

**Reviewed**: 2026-07-04
**Spec**: `docs/specs/p4-platforms.md`
**TS sources**: `index.ts`, `base.ts`, `newApi.ts`, `oneApi.ts`, `oneHub.ts`, `doneHub.ts`, `sub2api.ts`, `veloera.ts`, `openai.ts`, `claude.ts`, `codex.ts`, `gemini.ts`, `geminiCli.ts`, `antigravity.ts`, `cliproxyapi.ts`, `anyrouter.ts`, `titleHint.ts`, `standardApiProvider.ts`

---

## Accuracy Issues

### A1. OneHub announcements: spec claims "YES inherited" but TS returns empty array

**Spec row**: `| OneHub | ... | YES inherited |` (announcements column)
**TS evidence**: `OneHubAdapter extends OneApiAdapter` (oneHub.ts:3). Neither OneHubAdapter nor OneApiAdapter overrides `getSiteAnnouncements`. The inherited implementation is `BasePlatformAdapter.getSiteAnnouncements()` which returns `[]` (base.ts:188-194). OneApi is correctly marked NO in the matrix. Any child that does not override inherits NO -- not YES.
**Fix**: Change OneHub announcements to NO inherited.

### A2. DoneHub checkin: spec claims "YES" but implementation explicitly unsupported

**Spec row**: `| DoneHub | ... | YES |`
**TS evidence**: `DoneHubAdapter.checkin()` overrides to return `{ success: false, message: 'checkin endpoint not found' }` (doneHub.ts:14-16). The comment on lines 12-13 states: "DoneHub deployments generally do not expose /api/user/checkin. Mark as unsupported."
**Fix**: Change DoneHub checkin to NO no-op (consistent with Codex/Antigravity/Sub2Api pattern).

### A3. Sub2Api login: spec claims "YES override" but implementation always fails

**Spec row**: `| Sub2Api | ... | YES override |`
**TS evidence**: `Sub2ApiAdapter.login()` overrides to return `{ success: false, message: 'Sub2API uses JWT authentication; login is not supported' }` (sub2api.ts:684-689). The class comment on lines 19-21 states: "It does NOT support: login or check-in." The override always returns `success: false` regardless of input.
**Fix**: Change Sub2Api login to NO override (JWT-only).

### A4. GeminiCli detect: spec claims "YES inherited" but it is overridden

**Spec row**: `| GeminiCli | YES inherited |`
**TS evidence**: `GeminiCliAdapter` overrides `detect()` (geminiCli.ts:6-9) with its own URL check for `cloudcode-pa.googleapis.com`. It does not inherit from GeminiAdapter's detect (which checks generativelanguage.googleapis.com / googleapis.com / gemini.google.com).
**Fix**: Change GeminiCli detect to YES override.

### A5. DoneHub announcements: spec says "YES inherited" but it is overridden

**Spec row**: `| DoneHub | ... | YES inherited |` (announcements column)
**TS evidence**: `DoneHubAdapter` overrides `getSiteAnnouncements()` with its own `/api/notice` implementation (doneHub.ts:35-52). It does NOT inherit from OneHub/OneApi/Base.
**Fix**: Change DoneHub announcements to YES override.

### A6. `platformUserId` systematically absent from Go interface

**Spec Go interface** (lines 40-68): None of the 14 methods accept a `platformUserId` parameter.
**TS interface** (base.ts:93-108): Eleven of the 14 methods accept `platformUserId?: number` -- `getUserInfo`, `verifyToken`, `checkin`, `getBalance`, `getModels`, `getApiToken`, `getApiTokens`, `getSiteAnnouncements`, `getUserGroups`, `createApiToken`, `deleteApiToken`.

The `platformUserId` is critical for NewApi-fork platforms (NewApi, OneApi, OneHub, DoneHub, AnyRouter, Veloera) because these platforms use cookie-based authentication that requires a `New-API-User` / `Veloera-User` header with the correct numeric user ID. The TS implements extensive user-ID discovery logic (JWT decoding, Gob field extraction, hardcoded probe list) to find the right ID. Without this parameter, the Go implementation either needs an alternative strategy or must internalize the discovery logic.
**Fix**: Either add `platformUserId` to the Go interface (and all adapter signatures), or document the alternative strategy (e.g., each adapter internally discovers the user ID). If the latter, the NewApi Go adapter must replicate the ~500 lines of user-ID probing logic from newApi.ts.

### A7. Go `VerifyToken` uses `CredentialMode` -- TS uses `platformUserId`

**Spec Go interface**: `VerifyToken(ctx, url, accessToken, mode CredentialMode, proxy) (*VerifyTokenResult, error)`
**TS interface**: `verifyToken(baseUrl, token, platformUserId?): Promise<TokenVerifyResult>`

These are fundamentally different signatures. In TS, `verifyToken` in `NewApiAdapter` (newApi.ts:990-1056) uses `platformUserId` for cookie-based fallback and `New-Api-User` header injection. The TS BasePlatformAdapter.verifyToken (base.ts:118-138) tries session token first, then API key -- no `CredentialMode` concept exists. The Go spec introduces a `CredentialMode` concept that does not exist in TS.
**Fix**: Document the rationale for `CredentialMode` vs `platformUserId`, or align with TS.

### A8. Go `CreateAPIToken` takes `name string` -- TS takes `options: CreateApiTokenOptions`

**Spec Go interface**: `CreateAPIToken(ctx, url, accessToken, name string, proxy) (*APIToken, error)`
**TS interface**: `createApiToken(baseUrl, accessToken, platformUserId?, options?: CreateApiTokenOptions): Promise<boolean>`

TS `CreateApiTokenOptions` includes: `name`, `group`, `unlimitedQuota`, `remainQuota`, `expiredTime`, `allowIps`, `modelLimitsEnabled`, `modelLimits` (base.ts:82-91). The Go spec reduces this to just `name`, discarding 7 parameters. NewApi's `buildDefaultTokenPayload` (newApi.ts:376-395) uses all of these to construct the POST body.
**Fix**: Either expand Go signature to accept the full options struct, or document that the Go implementation will use hardcoded defaults for the omitted fields.

### A9. Go `DeleteAPIToken` takes `tokenID string` -- TS takes `tokenKey string`

**Spec Go interface**: `DeleteAPIToken(ctx, url, accessToken, tokenID string, proxy) error`
**TS interface**: `deleteApiToken(baseUrl, accessToken, tokenKey, platformUserId?): Promise<boolean>`

In TS, `tokenKey` is the actual API key string (e.g., `sk-xxx...`), NOT a numeric database ID. The implementation first lists all tokens, finds the one matching the key string, extracts the numeric ID, then sends DELETE with that ID (newApi.ts:1366-1427; oneApi.ts:196-245; sub2api.ts:856-893). The Go parameter name `tokenID` is misleading -- it should be `tokenKey` to match the actual lookup semantics.
**Fix**: Rename `tokenID` to `tokenKey` in the Go interface.

### A10. Go `GetModels` returns `[]ModelInfo` -- TS returns `string[]`

**Spec Go interface**: `GetModels(...) ([]ModelInfo, error)`
**TS interface**: `getModels(...): Promise<string[]>`

TS returns plain strings (model ID names like `gpt-4`, `claude-3-opus-20240229`). If Go returns `ModelInfo` structs, this is a richer type that needs justification -- what fields does `ModelInfo` carry beyond the model ID string?
**Fix**: Either align with TS (`[]string`) or define the `ModelInfo` struct and justify the enrichment.

---

## Missing Details

### M1. Veloera balance conversion factor differs: 1,000,000 vs 500,000

**TS evidence**: Veloera divides by `1000000` (veloera.ts:49-50). All other NewApi-fork adapters (NewApi, OneApi, OneHub, DoneHub) divide by `500000`. Sub2Api uses `500000` for USD-to-quota conversion (sub2api.ts:680).
**Impact**: Using the wrong factor would produce incorrect balance values for Veloera sites -- off by a factor of 2.
**Recommendation**: Document this in the Veloera adapter spec or add a "conversion factor" column to the capability matrix.

### M2. DoneHub balance calculation is `quota + used`, not `quota - used`

**TS evidence**: DoneHub treats `quota` as remaining balance (not total allocation), so it computes total as `quotaRemaining + used` (doneHub.ts:25-27). OneApi computes `balance = quota - used` (oneApi.ts:76), treating quota as total allocation. NewApi treats quota as remaining and adds used for total (newApi.ts:350-355).
**Impact**: Three different balance semantics across the NewApi fork family. The Go implementation must handle each variant separately.
**Recommendation**: Add a "Balance Semantics" section documenting the three patterns:
- **NewApi/DoneHub**: `quota` field = remaining, `total = remaining + used`
- **OneApi/OneHub**: `quota` field = total, `balance = total - used`
- **Veloera**: Same as OneApi but with 1M divisor instead of 500K

### M3. Adapter registration order affects sequential detection

**TS evidence**: `adapters` array in index.ts (lines 19-35) has a specific order: OpenAi, Codex, Claude, Gemini, GeminiCli, Antigravity, CliProxyApi, AnyRouter, DoneHub, OneHub, Veloera, NewApi, Sub2Api, OneApi. The comment on line 20 states "Specific forks before generic adapters for better auto-detection." NewApi is near the end (before Sub2Api and OneApi). OneApi is dead last.
**Impact**: The Go `registry.go` and `detect.go` must match this registration order for detect behavior to be identical.
**Recommendation**: Document the registration order explicitly in the spec and the rationale (forks first, generic parents last).

### M4. NewApi and OneApi detect are HTTP probes, not URL checks

**Spec matrix says**: NewApi and OneApi detect columns are labeled "YES URL".
**TS evidence**: `NewApiAdapter.detect()` does `GET /api/status` and checks `system_name` (newApi.ts:10-16). `OneApiAdapter.detect()` does `GET /api/status` and checks that `system_name` is absent (oneApi.ts:43-49). These are HTTP probes, not URL pattern matches. Veloera also probes `/api/status` (veloera.ts:7-15). CliProxyApi probes `/v0/management/openai-compatibility` (cliproxyapi.ts:25-50).
**Recommendation**: Distinguish "HTTP probe" from "URL match" in the matrix annotations.

### M5. Title-first detection set is narrower than title detection capability

**TS evidence**: `detectPlatformByTitle` (titleHint.ts:17-30) can detect 7 platforms: `anyrouter`, `done-hub`, `one-hub`, `veloera`, `sub2api`, `new-api`, `one-api`. But `titleFirstPlatforms` set (index.ts:46-52) only includes 5: `anyrouter`, `done-hub`, `one-hub`, `veloera`, `sub2api`. The remaining two (`new-api`, `one-api`) intentionally fall through to step 3 (sequential probe) because their HTTP probes are needed to distinguish them from each other.
**Recommendation**: Document this nuance in the detection flow description -- the full set of title-detectable platforms is 7, but only 5 short-circuit before sequential probing.

### M6. NewApi's cookie-based fallback and shield challenge not documented

**TS evidence**: NewApiAdapter (newApi.ts) contains ~1300 lines including: JWT user ID decoding (line 39-50), Gob binary field extraction (line 116-172), cookie-based session fallback (line 771-804), shield challenge solving (`solveAcwScV2`, line 443-468), and multi-header user ID injection (`New-API-User`, `Veloera-User`, `voapi-user`, `User-id`, `X-User-Id`, `Rix-Api-User`, `neo-api-user` at lines 62-72).
**Recommendation**: Add a "NewApi Shield Challenge" subsection documenting the `acw_sc__v2` cookie solving flow and the cookie-based retry logic. This is the most complex adapter and its intricacies warrant explicit documentation.

### M7. Sub2Api URL resolution logic is complex and undocumented

**TS evidence**: Sub2Api has `resolveManagementBaseUrl()` (sub2api.ts:467-495) that strips API path suffixes like `/models`, `/v1`, `/antigravity/v1beta`, etc. from the base URL. `resolveModelEndpoints()` (sub2api.ts:446-465) tries up to 5 different model endpoint patterns. Group resolution has `listGroups()` with 5 fallback endpoints and `inferGroupsFromKeys()` as a secondary strategy.
**Recommendation**: Document the Sub2Api URL resolution heuristics in the spec.

### M8. OneApi `deleteApiToken` has double-DELETE fallback

**TS evidence**: `OneApiAdapter.deleteApiToken()` tries `DELETE /api/token/${id}` first, then `DELETE /api/token/${id}/` with trailing slash as fallback (oneApi.ts:228-244). This is unique to OneApi and not mentioned.
**Recommendation**: Note this as a OneApi-specific implementation detail.

### M9. NewApi `parseBalance` treats `quota` field as remaining balance

**TS evidence**: `NewApiAdapter.parseBalance()` (newApi.ts:349-355) uses `data.quota` as remaining balance and `data.used_quota` as usage, computing total as `quota + used`. This differs from OneApi which computes `balance = quota - used` (oneApi.ts:72-76), treating quota as total allocation. This is distinct from DoneHub's approach (M2 above).
**Impact**: The Go NewApi adapter's `GetBalance` must replicate this specific parse logic, not a generic one-size-fits-all calculation.

---

## Edge Cases Not Covered

### E1. NewApi user-ID discovery via Gob binary decoding

**TS evidence**: `extractGobFieldInts()` (newApi.ts:137-172) parses Go's `encoding/gob` binary format from session cookie payloads to extract numeric user IDs. This is a highly specific heuristic. Go would need to either replicate Gob parsing (using the `encoding/gob` stdlib) or use a different approach.
**Recommendation**: Decide whether Go will replicate Gob-decoding or use an alternative strategy.

### E2. NewApi hardcoded user-ID probe list

**TS evidence**: `buildUserIdProbeCandidates()` (newApi.ts:233-249) appends a hardcoded list: `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20, 50, 100, 8899, 11494]`. These are common default admin/root user IDs on NewApi deployments. The Go implementation must either hardcode the same list or make it configurable.
**Recommendation**: Document this probe strategy and consider making it configurable per deployment.

### E3. NewApi multi-header user-ID injection

**TS evidence**: `userIdHeaders()` (newApi.ts:60-73) sets 7 different headers simultaneously: `New-API-User`, `Veloera-User`, `voapi-user`, `User-id`, `X-User-Id`, `Rix-Api-User`, `neo-api-user`. Each targets a different NewApi fork variant (Veloera, VoAPI, RixAPI, NeoAPI, generic NewAPI).
**Recommendation**: Document the header strategy and which fork variant uses which specific header.

### E4. Sub2Api group inference from API key endpoints

**TS evidence**: `inferGroupsFromKeys()` (sub2api.ts:402-427) lists API keys via `/api/v1/keys` or `/api/v1/api-keys` and extracts `group_id` from token payloads as a fallback when direct group listing fails. This is a secondary strategy not mentioned in the spec.
**Recommendation**: Document the full fallback chain: direct groups -> keys-based inference -> ['default'].

### E5. Sub2Api model endpoint resolution with antigravity prefix

**TS evidence**: `resolveModelEndpoints()` (sub2api.ts:446-465) includes `/antigravity/v1beta/models` and `/antigravity/v1/models` as fallback endpoints, and strips `models/` prefixes from model names (line 441: `.replace(/^models\//i, '')`).
**Recommendation**: Document the antigravity-compatible model endpoint patterns.

### E6. Sub2Api `login()` and `checkin()` both return `success: false` -- not errors

**TS evidence**: Sub2Api's login returns `{ success: false, message: '...' }` (sub2api.ts:684-689) and checkin returns `{ success: false, message: '...' }` (sub2api.ts:706-711). They do not throw errors. Callers must check `result.success` to distinguish "unsupported" from "failed."
**Recommendation**: Define the contract for "unsupported" methods -- should Go use `(nil, error)` or `(&Result{Success: false}, nil)`?

### E7. AnyRouter Cloudflare challenge handling

**Spec mentions** (line 138): "AnyRouter Cloudflare challenge -> use __fixtures__ challenge handling logic"
**TS evidence**: `AnyRouterAdapter` extends `NewApiAdapter` (anyrouter.ts:3) and has zero custom challenge logic. It inherits NewApi's full `solveAcwScV2` machinery. The `__fixtures__` reference appears to be a Go testing concept with no TS equivalent.
**Recommendation**: Clarify whether the Go implementation reuses NewApi's shield challenge logic for AnyRouter (as TS does), or if `__fixtures__` is an alternative strategy for test data.

### E8. Claude adapter's `/anthropic` suffix stripping

**TS evidence**: `resolveOpenAiCompatibleBaseUrl()` (claude.ts:5-10) strips `/anthropic` suffix from base URL for OpenAI-compatible fallback model discovery. This enables Claude endpoints that proxy through OpenAI-compatible gateways (e.g., `https://gateway.example.com/anthropic` -> try `https://gateway.example.com/v1/models`).
**Recommendation**: Document this URL resolution behavior in the Claude adapter spec.

### E9. Gemini adapter's three-path model discovery

**TS evidence**: `GeminiAdapter.getModels()` (gemini.ts:52-81) tries three paths:
1. OpenAI-compat `/v1beta/openai/models` if URL contains `/openai/`
2. Native Gemini `v1beta/models?key=...` endpoint
3. OpenAI-compat again if native was empty and URL does not already contain `/openai/`

Also normalizes model names by stripping `models/` prefix (line 6: `.replace(/^models\//i, '')`).
**Recommendation**: Document the three-path strategy and the `stripModelPrefix` normalization.

---

## Incorrect Details

### I1. OneHub announcements marked "YES inherited" -- should be NO

(See Accuracy Issue A1. OneHub inherits empty `[]` from BasePlatformAdapter via OneApiAdapter.)

### I2. DoneHub checkin marked "YES" -- should be NO no-op

(See Accuracy Issue A2. DoneHub explicitly overrides to return "checkin endpoint not found".)

### I3. Sub2Api login marked "YES override" -- should be NO override (JWT-only)

(See Accuracy Issue A3. Override always returns `success: false`.)

### I4. GeminiCli detect marked "YES inherited" -- should be YES override

(See Accuracy Issue A4. GeminiCli overrides detect with its own URL check for `cloudcode-pa.googleapis.com`.)

### I5. DoneHub announcements marked "YES inherited" -- should be YES override

(See Accuracy Issue A5. DoneHub overrides `getSiteAnnouncements` with its own `/api/notice` implementation.)

### I6. NewApi and OneApi detect marked "YES URL" -- should be YES HTTP probe

Both use `GET /api/status` with response content checks, not URL pattern matching. Veloera and CliProxyApi also use HTTP probes, not URL matching. The distinction between NewApi and OneApi detect is in the response payload (NewApi requires `system_name` present; OneApi requires it absent), not in URL patterns.

### I7. Detection flow step 3 description inaccurate -- not always HTTP GET

**Spec** (line 95): "By registration order, call adapter.Detect(url) (HTTP GET probe characteristics)"
**Issue**: About half the adapters (OpenAI, Codex, AnyRouter, DoneHub, OneHub, Antigravity, GeminiCli) use pure URL keyword checks, not HTTP probes. The parenthetical "(HTTP GET probe characteristics)" is an overgeneralization.

### I8. Go `DeleteAPIToken` parameter named `tokenID` -- should be `tokenKey`

(See Accuracy Issue A9. The parameter is the API key string (e.g., `sk-xxx...`), not the numeric database ID. The adapter internally resolves key string to numeric ID.)

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 10 |
| Missing Details | 9 |
| Edge Cases Not Covered | 9 |
| Incorrect Details | 8 |
| **Total findings** | **36** |

**Verdict**: **NEEDS_REVISION**

The spec captures the high-level architecture correctly: the 14-platform layout, the inheritance chains (OneApi->OneHub->DoneHub, NewApi->AnyRouter, Gemini->GeminiCli, StandardApiProvider->OpenAI/Claude/Gemini/CliProxyApi), and the detection flow pipeline are all directionally correct. The SiteProxy design and acceptance criteria are reasonable.

However, the capability matrix has 5 concrete errors (OneHub announcements, DoneHub checkin, Sub2Api login, GeminiCli detect, DoneHub announcements), and the Go interface systematically diverges from the TS interface in ways that need either explicit justification or correction. Specifically:

1. **`platformUserId` omission** (largest design gap): Eleven of 14 TS interface methods accept `platformUserId`. The Go spec removes it entirely. The team must decide whether Go will handle user-ID discovery internally (each adapter) or accept it as a parameter. If internal, the NewApi adapter's ~500 lines of user-ID probing logic must be replicated.

2. **`CreateAPIToken` signature simplification**: Dropping `CreateApiTokenOptions` (7 fields: name, group, unlimitedQuota, remainQuota, expiredTime, allowIps, modelLimitsEnabled, modelLimits) down to `name string` discards important configuration that downstream callers use.

3. **Capability matrix errors**: OneHub and DoneHub rows contain 3 definite errors (A1, A2, A3) plus 2 mislabelings (A4, A5) of inherited-vs-overridden. These matter because the matrix drives which Go adapters need what methods.

4. **Three different balance semantics** across the NewApi fork family (NewApi/DoneHub, OneApi/OneHub, Veloera) are not documented.

Recommended actions before Go implementation:
- Fix the 8 incorrect details in the capability matrix and detection flow description
- Decide the `platformUserId` strategy and update the Go interface accordingly
- Add the missing detail sections: balance semantics table, Sub2Api URL resolution, NewApi shield challenge, adapter registration order
- Expand the edge cases list with the 9 findings above, particularly Gob decoding, user-ID probing, and multi-header injection
