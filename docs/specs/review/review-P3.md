# P3 Cross-Reference Review: Sites + Accounts + AccountTokens

**Date**: 2026-07-04
**Reviewer**: Automated cross-reference of Go spec against TypeScript source
**Result**: NEEDS_REVISION

---

## Accuracy Issues

### A1. AES Encryption Algorithm: GCM, not CBC
- **Spec claim** (line 110): `accessToken` uses AES-256-**CBC** encryption
- **TS source** (`accountCredentialService.ts:5`): `const ALGORITHM = 'aes-256-gcm';`
- **Impact**: HIGH. Go implementation using CBC would produce incompatible ciphertext. GCM includes an authentication tag (16 bytes); CBC with separate HMAC has an entirely different wire format. The TS encrypted format is `v1:base64url(iv):base64url(authTag):base64url(data)` with IV=12 bytes (GCM nonce), not 16 bytes (CBC IV). Using CBC would break decryption of passwords encrypted by existing TS instances.
- **Recommendation**: Change spec to `aes-256-gcm`. Document the `v1:iv:tag:ciphertext` wire format with base64url encoding.

### A2. What Gets Encrypted: Password, NOT accessToken
- **Spec claim** (line 110): `accessToken` is AES-encrypted at rest
- **TS source**: `encryptAccountPassword` / `decryptAccountPassword` are used exclusively for the `autoRelogin.passwordCipher` field inside `extraConfig` (see `accounts.ts:581`). The `accessToken` column is stored as **plaintext** in the database (see `accounts.ts:599` `accessToken: loginResult.accessToken`).
- **Impact**: HIGH. Implementing accessToken encryption where none exists would (a) break API compatibility with the frontend which expects plaintext accessToken in API responses, (b) add overhead where TS does not, (c) deviate from the existing architecture.
- **Recommendation**: Correct the spec to state: "`autoRelogin.password` (within `extraConfig`) is encrypted with AES-256-GCM using a key derived via SHA-256 from `config.accountCredentialSecret`. The `accessToken` column is stored as plaintext."

### A3. Endpoint Count Mismatches
- **Spec** says Sites has **9 endpoints** but the table lists **10** rows (probe-now is included in the table).
- **Spec** says Accounts has **11 endpoints** but the table lists **12** rows.
- **Spec** says total **31 endpoints** but the actual combined count is **33** (10 + 12 + 11), or **34** if counting the missing SSE probe-stream endpoint.
- **Recommendation**: Correct all three counts to match the finalized endpoint tables.

### A4. Missing SSE Probe-Stream Endpoint
- **Spec**: completely absent from tables, module structure, and test plan
- **TS source** (`sites.ts:922-961`): `GET /api/sites/:id/probe-stream` -- SSE streaming variant of probe-now that emits `probe-start`, `probe-model-checked`, `probe-model-result`, `complete`, and `error` events. Accepts `scope`, `modelName`, `latencyThresholdMs` as query params. Supports `AbortController` for client disconnect propagation.
- **Impact**: MEDIUM. If omitted, the Go version would lack real-time progress feedback that the TS frontend may rely on for model probing UX.
- **Recommendation**: Add this endpoint to the spec (endpoint table, Go module structure as `sites_probe_stream.go`, and test plan).

### A5. `?refresh` Query Param on Wrong Endpoint
- **Spec** (line 51): GET `/api/sites` supports `?refresh`
- **TS source**: `GET /api/sites` has **no** refresh query parameter (`sites.ts:437`). The `?refresh` parameter exists on `GET /api/accounts` (`accounts.ts:485-498`), where it forces a snapshot refresh and sets the `x-accounts-snapshot-cache` response header.
- **Impact**: LOW for sites (Go can safely omit). But an accurate spec prevents wasted implementation effort.
- **Recommendation**: Remove `?refresh` from Sites GET description. Document `?refresh` and snapshot caching on Accounts GET instead.

### A6. AccountCreatePayload: `accessToken` Not Required
- **Spec** Go struct (line 98): `AccessToken string json:"accessToken" validate:"required"`
- **TS Zod schema** (`accountsRoutePayloads.ts:8`): `accessToken: z.string().optional()`
- **Rationale**: When using `accessTokens` (batch API keys) or `credentialMode=apikey` with batch tokens, `accessToken` is allowed to be empty. The TS handler (`accounts.ts:1302-1307`) checks `requestedTokens.length === 0` separately rather than requiring `accessToken` at payload-validation level.
- **Impact**: MEDIUM. `validate:"required"` on `AccessToken` would reject valid create requests that only supply `accessTokens` (batch import).
- **Recommendation**: Remove `validate:"required"` from `AccessToken`. Add `AccessTokens []string json:"accessTokens,omitempty"` field.

---

## Missing Details

### M1. Missing Payload Fields in AccountCreatePayload
The spec's example `AccountCreatePayload` Go struct is missing these fields present in the TS Zod schema:

| Field | TS type | Notes |
|-------|---------|-------|
| `accessTokens` | `string[]` (optional) | Batch API key import; when non-empty, `credentialMode` is forced to `apikey` |
| `platformUserId` | `number` (optional, positive int) | NewAPI/OneAPI user ID for platforms requiring it |

### M2. Batch Actions: Incomplete Enumeration
The spec (AC line 123) says batch endpoints support enable/disable/delete, but the TS supports additional actions:
- **Sites batch** (`sites.ts:754`): `enable`, `disable`, `delete`, **`enableSystemProxy`**, **`disableSystemProxy`**
- **Accounts batch** (`accounts.ts:1595`): `enable`, `disable`, `delete`, **`refreshBalance`**

### M3. Site Update: Missing `postRefreshProbe*` Fields
The TS `PUT /api/sites/:id` handler (`sites.ts:692-698`) supports these fields not mentioned in any spec payload:
- `postRefreshProbeEnabled` (boolean)
- `postRefreshProbeModel` (string, trimmed)
- `postRefreshProbeScope` (`'all'` or `'single'`)
- `postRefreshProbeLatencyThresholdMs` (non-negative integer, ms)

### M4. Account Update: Missing Fields
The TS `AccountUpdatePayload` (`accountsRoutePayloads.ts:19-32`) and handler support these fields not shown in the spec:
- `unitCost` (number | null)
- `extraConfig` (JSON string, object, or null)
- `refreshToken` (string | null) -- sub2api managed OAuth refresh token
- `tokenExpiresAt` (number | string | null) -- sub2api token expiry timestamp
- `proxyUrl` (string | null) -- account-level proxy URL override
- `sortOrder` (non-negative integer)

### M5. AccountTokenCreatePayload: Unlisted Fields
The TS schema (`accountTokensRoutePayloads.ts:3-17`) has these fields beyond basic CRUD:
- `unlimitedQuota` (boolean)
- `remainQuota` (number | string) -- required when unlimitedQuota is false
- `expiredTime` (number | string -- Unix timestamp, epoch seconds)
- `allowIps` (string) -- IP allowlist
- `modelLimitsEnabled` (boolean)
- `modelLimits` (string) -- model-specific limits

These are used when creating tokens upstream (no `token` value provided in the request).

### M6. AccountToken Value Retrieval Flow
The spec lists `GET /api/account-tokens/:id/value` as "get token value (decrypt)" but the TS implementation (`accountTokens.ts:873-908`) shows:
- Tokens stored as **plaintext** in DB (not encrypted; the spec's "decrypt" label is misleading)
- Returns `{ token, tokenMasked }` where `token` is the raw value (Bearer prefix stripped for NewAPI) and `tokenMasked` is a display-safe mask
- Masked-pending tokens return HTTP 409
- ApiKey connections are rejected with HTTP 400

### M7. AccountToken Value Status System
The TS has a multi-state value status not documented in the spec:
- **`masked-pending`**: Token synced from upstream with a masked value; auto-disabled, cannot be set as default
- **`ready`**: Full plaintext token available; can be enabled and set as default

### M8. API Endpoints as Embedded Sub-Resource
The TS sites handler embeds `apiEndpoints` as a field within site create/update payloads. The Go module structure lists `sites_endpoints.go` as a separate file, but the spec endpoint table has no dedicated REST endpoints for API endpoints CRUD. The TS does **not** expose standalone `GET/POST/PUT/DELETE /api/sites/:id/api-endpoints` routes. The spec should clarify this is an embedded field, not a separate REST resource.

### M9. Token Delete: Upstream-First Strategy
The TS `deleteAccountTokenById` (`accountTokens.ts:645-691`) attempts to delete the token **upstream** (on the target platform via `adapter.deleteApiToken`) **before** deleting locally. On upstream delete failure, the local delete is **aborted**. Masked-pending tokens and disabled-site accounts skip the upstream delete.

### M10. Account GET: Snapshot Caching Architecture
The TS `GET /api/accounts` (`accounts.ts:485-498`) returns cached snapshots via `getAccountsSnapshot` with `forceRefresh`. Response includes `generatedAt` and `x-accounts-snapshot-cache` header. The response wraps `{ generatedAt, accounts, sites }` -- sites are included alongside accounts for frontend name resolution.

---

## Edge Cases Not Covered

### E1. Empty accessToken: Spec Overstates Validation
- **Spec** (edge cases): `accessToken` is empty string -> reject
- **TS reality**: `accessToken` is optional in the Zod schema. Rejection occurs when **no usable token was provided at all** (`requestedTokens.length === 0`), not specifically `accessToken === ""`. An account with `accessToken=""` but `accessTokens=["sk-abc"]` is valid.
- **Recommendation**: Refine edge case to: "No usable token provided (both accessToken empty and accessTokens empty) -> reject."

### E2. API Key Connection: Restricted Operations
API key connections (`isApiKeyConnection`) have many restricted operations not enumerated:
- Cannot create upstream account tokens (POST without a token value)
- Cannot manage/sync account tokens (batch, sync, groups, value retrieval)
- Skipped in runtime health refresh (proxyOnly capability)

### E3. Masked Pending Token Lifecycle
- Always stored with `enabled: false` and `isDefault: false`
- Batch enable/disable operations skip masked-pending tokens with specific error
- `repairDefaultToken` is called when the current default becomes unusable

### E4. Site Delete: DB-Level Cascade, Not Application-Level
The TS `DELETE /api/sites/:id` (`sites.ts:736-741`) only executes `db.delete(schema.sites).where(...)` and relies on **database-level** `ON DELETE CASCADE` foreign keys. The spec should clarify the Go implementation must replicate FK cascade constraints from P1.

### E5. Account Delete: Cascade and Route Rebuild
The TS `DELETE /api/accounts/:id` deletes the row (relying on DB CASCADE for accountTokens, modelAvailability, etc.) and calls `rebuildRoutesBestEffort()`.

### E6. Site Disable/Enable Side Effects
When a site changes status (update or batch), the TS (`sites.ts:389-427`):
- **Disable**: All associated accounts -> `status: 'disabled'`. Events row created with title "Site disabled" and level warning.
- **Enable**: Only previously-disabled accounts -> `active`. Events row with title "Site enabled" and level info.

### E7. Account Expired API Key Recovery
The TS account update handler (`accounts.ts:1549-1566`) has recovery logic for expired API key accounts: model refresh with `allowInactive: true` + `reactivateAfterSuccessfulModelRefresh`.

### E8. Sub2API Managed Auth
Rebind-session and account update have sub2api-specific `refreshToken`/`tokenExpiresAt` logic in `extraConfig.sub2apiAuth`. The spec should document this platform-specific extra config structure.

### E9. Token Create: Dual Path (Local vs Upstream)
`POST /api/account-tokens` has two code paths:
1. **Local path** (token provided): validates, inserts, sets default, refreshes coverage
2. **Upstream path** (no token): calls `adapter.createApiToken()`, then `executeAccountTokenSync`

Payload requirements and error handling differ substantially between paths.

### E10. Account Login: Existing Account Reuse
The TS `POST /api/accounts/login` checks for existing account with same `(siteId, username)`. If found, updates it rather than creating duplicate. Response includes `reusedAccount: true`.

---

## Incorrect Details

### I1. Algorithm: CBC vs GCM (duplicates A1)
Must be corrected from `AES-256-CBC` to `AES-256-GCM`.

### I2. Token `expiredTime` / `tokenExpiresAt` Type
- **Spec**: `TokenExpiresAt string` (string only)
- **TS**: `z.union([z.number(), z.string()]).optional()` -- accepts both Unix timestamp (number) and ISO date strings
- Handler `parseExpiredTime` normalizes both into epoch seconds

### I3. Error Message Language: Not All Chinese
- **Spec** (AC line 118): "Payload validation errors include Chinese (matching Zod)"
- **TS reality**: Zod-level errors are in **English** (e.g., "Invalid siteId. Expected positive number."). Chinese messages exist only at the handler-level business-logic validation (e.g., "account ID invalid", "token not found").
- **Recommendation**: Clarify handler-level vs structural payload validation message language.

### I4. Account Create Workflow Order: Oversimplified
- **Spec**: "verifyToken -> getBalance -> getModels -> write accounts + model_availability + trigger route rebuild"
- **TS reality**: The `createManualAccount` flow is:
  1. verifyToken (returns userInfo, balance, apiToken, tokenType)
  2. Build account values + write row in transaction (with model_availability)
  3. `convergeAccountMutation` post-creation: ensureDefaultToken, syncTokensFromUpstream, refreshBalance, refreshModels, rebuildRoutes (with `continueOnError: true`)
- The `convergeAccountMutation` post-creation step is missing from the spec.

### I5. Config Key Casing
- **Spec**: `config.AccountCredentialSecret` (PascalCase)
- **TS**: `config.accountCredentialSecret` (camelCase)
- Go exported config fields would be PascalCase, so this may be intentional. Spec should note the mapping between TS camelCase and Go PascalCase config keys.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 6 |
| Missing Details | 10 |
| Edge Cases Not Covered | 10 |
| Incorrect Details | 5 |
| **Total Findings** | **31** |

**Verdict**: **NEEDS_REVISION**

The most critical issues are:
1. **AES-256-GCM vs CBC** (A1/I1) -- algorithm mismatch would produce incompatible ciphertext
2. **Password vs accessToken encryption scope** (A2) -- implementing accessToken encryption where TS has none would break API compatibility
3. **Missing SSE probe-stream endpoint** (A4) -- real-time progress feature may be required by frontend
4. **`accessToken` not required** (A6) -- would reject valid batch API key imports

The spec has strong structural coverage of the endpoint inventory but contains several factual errors about the encryption scheme and payload validation rules that must be corrected before implementation begins. The batch action enumeration, payload field completeness, and edge case documentation also need significant expansion.
