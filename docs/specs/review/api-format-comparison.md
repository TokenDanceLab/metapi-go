# API Response Format Comparison: TypeScript vs Go

**Date**: 2026-07-04
**Scope**: All admin API endpoints across sites, accounts, token-routes, stats, and models
**Severity legend**: BLOCKING (breaks frontend) / HIGH (visible to user) / LOW (cosmetic)

---

## Root Cause Analysis

Two architectural patterns in the Go codebase cause the majority of mismatches:

### Pattern A: `queryRows`/`MapScan` produces snake_case keys

`handler/admin/search.go:queryRows` uses `rows.MapScan(row)` which returns a `map[string]any` where keys are the SQLite column names: `model_pattern`, `display_name`, `decision_refreshed_at`, etc. The TS codebase uses Drizzle ORM, which normalizes column names to camelCase (`modelPattern`, `displayName`, `decisionRefreshedAt`). Every endpoint that calls `queryRows` and serializes the result directly returns snake_case JSON keys that the frontend cannot consume.

### Pattern B: Struct serialization without JSON tags produces PascalCase

`store/schema.go` defines all structs with only `db` tags (no `json` tags). `json.NewEncoder(w).Encode(structValue)` serializes Go field names as-is: `SiteID`, `AccessToken`, `IsPinned`. The TS frontend expects camelCase: `siteId`, `accessToken`, `isPinned`.

### Pattern C: Sites service correctly uses `siteToMap` (camelCase)

`service/site_service.go:siteToMap` manually maps every field to camelCase, and `ListSites` uses it. This is the one place where field naming is correct -- all other handlers need similar treatment.

---

## Section 1: Sites Endpoints

### 1.1 GET /api/sites

| Aspect | TS (camelCase) | Go | Severity |
|--------|----------------|-----|----------|
| site field names | `proxyUrl`, `useSystemProxy`, `customHeaders`, `externalCheckinUrl`, `isPinned`, `sortOrder`, `globalWeight`, `postRefreshProbeEnabled`, `postRefreshProbeModel`, `postRefreshProbeScope`, `postRefreshProbeLatencyThresholdMs`, `createdAt`, `updatedAt` | Correct (via `siteToMap`) | **OK** |
| `apiEndpoints` array | Present on each site | Present (via `siteToMap`) | **OK** |
| `totalBalance` | Present (number, rounded) | Present | **OK** |
| `subscriptionSummary` | Full aggregate object | Stub: always `null` | **LOW** |
| `totalBalance` per site | Aggregated from all accounts | Aggregated | **OK** |

**Verdict**: Largely correct. subscriptionSummary is a stub.

---

### 1.2 POST /api/sites (create)

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | `{ ...site, apiEndpoints, initializationPresetId? }` | `LoadSiteWithEndpoints` → `siteToMap` output | **OK** |
| Error response (validation) | `{ error: "..." }` | `{ "error": "..." }` | **OK** |
| Error response (409 conflict) | `{ error: "..." }` | `{ "error": "..." }` | **OK** |
| Response includes `initializationPresetId` | Yes, when detected | No -- `createSite` in handler does not return it | **HIGH** |

**Verdict**: Missing `initializationPresetId` in create response.

---

### 1.3 PUT /api/sites/:id (update)

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | `LoadSiteWithEndpoints(id)` via `siteToMap` | `LoadSiteWithEndpoints(id)` via `siteToMap` | **OK** |
| Error response | `{ error: "..." }` | `{ "error": "..." }` | **OK** |

**Verdict**: OK.

---

### 1.4 DELETE /api/sites/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | `{ success: true }` | `{ "success": true }` | **OK** |

**Verdict**: OK.

---

### 1.5 POST /api/sites/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success shape | `{ success: true, successIds, failedItems }` | `{ "success": true, "successIds", "failedItems" }` | **OK** |
| Error response (validation) | `{ message: "..." }` | `{ "error": "..." }` | **BLOCKING** |

**Verdict**: Go batchSites uses key `"error"` for validation failures; TS uses `"message"`. Frontend error display code expecting `message` will show nothing.

---

### 1.6 POST /api/sites/detect

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success (detected) | Detection result object | Detection result object | **OK** |
| Success (not detected) | `{ error: "Could not detect platform" }` (HTTP 200) | `{ "error": "Could not detect platform" }` (HTTP 200) | **OK** |

**Verdict**: OK.

---

### 1.7 GET/PUT /api/sites/:id/disabled-models

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ siteId, models: string[] }` | `{ "siteId", "models": string[] }` | **OK** |

**Verdict**: OK.

---

### 1.8 GET /api/sites/:id/available-models

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ siteId, models }` | `{ "siteId", "models" }` | **OK** |

**Verdict**: OK.

---

### 1.9 POST /api/sites/:id/probe-now

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Full probe result object with `success`, `totalModels`, `available`, `unavailable`, `results[]`, `error` on failure | Stub: `{ success: true, totalModels: 0, available: 0, unavailable: 0, results: [] }` | **HIGH** (stub, expected) |

**Verdict**: Stub -- probe implementation pending.

---

## Section 2: Accounts Endpoints

### 2.1 GET /api/accounts

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Top-level shape | `{ generatedAt, accounts, sites }` | `{ "generatedAt", "accounts", "sites" }` | **OK** |
| Account fields | camelCase: `siteId`, `accessToken`, `apiToken`, `isPinned`, `sortOrder`, `checkinEnabled`, `extraConfig`, `oauthProvider`, `createdAt`, `updatedAt` | **snake_case**: `site_id`, `access_token`, `api_token`, `is_pinned`, `sort_order`, `checkin_enabled`, `extra_config`, `oauth_provider`, `created_at`, `updated_at` | **BLOCKING** |
| Site fields | camelCase | **PascalCase**: `ID`, `Name`, `SiteID`, etc. (Go struct without json tags) | **BLOCKING** |

**Root cause**: `ListAccountsWithSites` (service/account_service.go:373) uses `rows.MapScan` which returns raw SQL column names. The `sites` array uses `h.db.Select(&sites, ...)` into `[]store.Site` structs, which serialize with PascalCase field names because no `json` tags are defined.

**Verdict**: **BLOCKING** -- Two different naming conventions (`snake_case` for accounts, `PascalCase` for sites) in the same response. Frontend cannot read either.

---

### 2.2 POST /api/accounts (create)

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Single-token success shape | `{ ...accountFields, tokenType, credentialMode, capabilities, modelCount, apiTokenFound, usernameDetected, queued, jobId?, message? }` | Explicit flat map: `{ id, siteId, username, accessToken, status, tokenType, credentialMode, capabilities, modelCount, apiTokenFound, usernameDetected, queued }` | **OK** (same data, slightly different shape) |
| Missing fields | -- | Go missing: `checkinEnabled`, `extraConfig`, `isPinned`, `sortOrder`, `balance`, `apiToken`, `createdAt`, `updatedAt`, `jobId`, `message` | **HIGH** |
| Error response (validation) | `{ success: false, message: "..." }` | `{ success: false, error: "..." }` for some cases; `{ success: false, message: "..." }` for others | **HIGH** |
| Batch error response | `{ success: false, batch, totalCount, createdCount, failedCount, message, items }` | Same structure, message field OK | **OK** |

**Verdict**: Account create returns fewer fields than TS. Error key inconsistency (`error` vs `message`).

---

### 2.3 POST /api/accounts/login

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success `account` field | Full account row object | **`null`** | **BLOCKING** |
| Success shape | `{ success: true, account, apiTokenFound, tokenCount, reusedAccount }` | `{ success: true, account: null, apiTokenFound: false, tokenCount: 0, reusedAccount }` | **BLOCKING** |
| Error response | `{ success: false, message: "...", shieldBlocked? }` | `{ success: false, error: "..." }` (payload validation); `{ success: false, message: "..." }` (site not found) | **HIGH** |

**Verdict**: **BLOCKING** -- Login returns `null` account, frontend will crash trying to access `account.id` etc.

---

### 2.4 POST /api/accounts/verify-token

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Session success | `{ success: true, tokenType: "session", userInfo, balance, apiToken }` | Stub: always returns apikey result | **HIGH** (stub) |
| APIKey success | `{ success: true, tokenType: "apikey", modelCount, models[] }` | `{ success: true, tokenType: "apikey", modelCount: 0, models: [] }` | **HIGH** (stub) |
| Error response | Complex: `{ success: false, needsUserId?, invalidUserId?, shieldBlocked?, message }` | `{ success: false, error: "..." }` (payload) or `{ success: false, message: "..." }` | **HIGH** |

**Verdict**: Stub implementation. Error keys mismatch.

---

### 2.5 POST /api/accounts/:id/rebind-session

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success `account` field | Full account object | **Missing** -- Go returns no `account` field | **BLOCKING** |
| Success shape | `{ success: true, account, tokenType, credentialMode, capabilities, apiTokenFound }` | `{ success: true, tokenType: "session", credentialMode: "session", capabilities, apiTokenFound: false }` | **BLOCKING** |
| Error response | `{ success: false, message: "..." }` | `{ success: false, error: "..." }` for payload; `{ success: false, message: "..." }` for not-found | **HIGH** |

**Verdict**: **BLOCKING** -- Missing `account` field in response.

---

### 2.6 PUT /api/accounts/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response type | Full account object (Drizzle → camelCase) | `store.Account` struct serialized directly | **BLOCKING** |
| JSON field names | `id`, `siteId`, `username`, `accessToken`, `apiToken`, `balance`, `status`, `isPinned`, `sortOrder`, `checkinEnabled`, `extraConfig`, `createdAt`, `updatedAt` | `ID`, `SiteID`, `Username`, `AccessToken`, `APIToken`, `Balance`, `Status`, `IsPinned`, `SortOrder`, `CheckinEnabled`, `ExtraConfig`, `CreatedAt`, `UpdatedAt` (PascalCase) | **BLOCKING** |
| Error response (not found) | `{ message: "account not found" }` | `{ "message": "account not found" }` | **OK** |
| Error response (validation) | `{ message: "..." }` | `{ "error": "..." }` | **HIGH** |

**Root cause**: `updateAccount` does `json.NewEncoder(w).Encode(updated)` where `updated` is `store.Account` with only `db` tags (no `json` tags). Go serializes by struct field name.

**Verdict**: **BLOCKING** -- Every field name is wrong.

---

### 2.7 DELETE /api/accounts/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

**Verdict**: OK.

---

### 2.8 POST /api/accounts/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success shape | `{ success: true, successIds, failedItems }` | `{ "success": true, "successIds", "failedItems" }` | **OK** |
| Error response | `{ message: "..." }` | `{ "error": "..." }` | **BLOCKING** |

**Verdict**: Batch validation error uses `"error"` instead of `"message"`.

---

### 2.9 POST /api/accounts/health/refresh

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ success: true, summary?, results? }` or `{ success: true, queued, reused, jobId, status, message }` | Stub: simple summary or queued | **LOW** (stub) |

**Verdict**: Stub, shape is close enough.

---

### 2.10 POST /api/accounts/:id/balance

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ balance, balanceUsed, ... }` | `{ "balance", "balanceUsed", "quota" }` (snake_map because it's a map literal, keys are camelCase) | **OK** |

**Verdict**: OK (manually constructed map with camelCase keys).

---

### 2.11 GET /api/accounts/:id/models

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ siteId, siteName, models: [{ name, latencyMs, disabled, isManual }], totalCount, disabledCount }` | Same structure with camelCase keys | **OK** |

**Verdict**: OK (manually constructed).

---

### 2.12 POST /api/accounts/:id/models/manual

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

**Verdict**: OK.

---

## Section 3: Token Routes Endpoints

### 3.1 GET /api/routes/lite

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route field names | `id`, `modelPattern`, `displayName`, `displayIcon`, `routeMode`, `sourceRouteIds`, `routingStrategy`, `enabled` | **snake_case**: `id`, `model_pattern`, `display_name`, `display_icon`, `route_mode`, `routing_strategy`, `enabled` | **BLOCKING** |
| `sourceRouteIds` field | Present (array of numbers) | **Missing** | **BLOCKING** |

**Root cause**: `listLite` calls `queryRows(h.db, "SELECT ... FROM token_routes")` which returns snake_case. No `sourceRouteIds` join.

**Verdict**: **BLOCKING** -- All field names wrong, missing `sourceRouteIds`.

---

### 3.2 GET /api/routes/summary

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Field names | All camelCase | Manually mapped to camelCase: `modelPattern`, `displayName`, `displayIcon`, `routeMode`, `sourceRouteIds`, `modelMapping`, `routingStrategy`, `enabled`, `channelCount`, `enabledChannelCount`, `siteNames`, `decisionSnapshot`, `decisionRefreshedAt` | **OK** |
| `siteNames` | Array of strings from channel join | **Always `[]`** (stub, not computed) | **HIGH** |
| `decisionSnapshot` | Parsed JSON object | Parsed JSON object | **OK** |

**Verdict**: Field names correct. `siteNames` is stubbed to empty array.

---

### 3.3 GET /api/routes

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route field names | camelCase (`modelPattern`, `displayName`, `routingStrategy`, `enabled`, `decisionRefreshedAt`, etc.) | **snake_case**: `model_pattern`, `display_name`, `routing_strategy`, `enabled`, `decision_refreshed_at`, etc. | **BLOCKING** |
| `decisionSnapshot` | Parsed JSON object | Parsed JSON object (manual) | **OK** |
| `decisionRefreshedAt` | camelCase string/JSON key | snake_case `decision_refreshed_at` from raw DB row (overridden by `decisionSnapshot` which is manual) | **BLOCKING** |
| `channels` array | Nested objects with `account`, `site`, `token`, `routeUnit` sub-objects | Flat rows with just `username`, `site_name` added. **No `account` sub-object, no `site` sub-object, no `token` sub-object, no `routeUnit` sub-object** | **BLOCKING** |
| Channel field names within `channels[]` | camelCase: `accountId`, `tokenId`, `sourceModel`, `priority`, `weight`, `enabled`, `manualOverride` | **snake_case**: `account_id`, `token_id`, `source_model`, `priority`, `weight`, `enabled`, `manual_override` | **BLOCKING** |

**Root cause**: `listRoutes` sets `item := route` (raw MapScan row with snake_case keys), then adds `item["channels"]` and `item["decisionSnapshot"]` with camelCase. The result is a mix of snake_case (route fields) and camelCase (channels/decisionSnapshot keys) -- inconsistent.

**Verdict**: **BLOCKING** -- Mix of snake_case and camelCase in single object.

---

### 3.4 POST /api/routes (create)

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | Route object (camelCase, with `sourceRouteIds`) | Route object (**snake_case** from `queryRow`, no `sourceRouteIds`) | **BLOCKING** |
| Error response | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

**Verdict**: **BLOCKING** -- Created route returned with snake_case.

---

### 3.5 PUT /api/routes/:id (update)

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | Route object (camelCase, with `sourceRouteIds`) | Route object (**snake_case** from `queryRow`, no `sourceRouteIds`) | **BLOCKING** |
| Error response | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

**Verdict**: **BLOCKING**.

---

### 3.6 DELETE /api/routes/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

**Verdict**: OK.

---

### 3.7 POST /api/routes/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true, updatedCount }` | `{ "success": true, "updatedCount" }` | **OK** |

**Verdict**: OK.

---

### 3.8 GET /api/routes/:id/channels

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Channel field names | camelCase (Drizzle) | **snake_case** (raw MapScan) | **BLOCKING** |
| Nested objects | Contains `account`, `site`, `token`, `routeUnit` sub-objects | Flat rows with `username`, `site_name` | **BLOCKING** |
| Error response | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

**Verdict**: **BLOCKING**.

---

### 3.9 POST /api/routes/:id/channels (add channel)

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Created channel | camelCase fields | **snake_case** fields from `queryRow` | **BLOCKING** |
| Error duplicate | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

**Verdict**: **BLOCKING**.

---

### 3.10 POST /api/routes/:id/channels/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true, created, skipped, errors }` | Same | **OK** |

**Verdict**: OK.

---

### 3.11 PUT /api/channels/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| `channels` array | Array of updated channel objects (camelCase) | **Empty array `[]`** | **BLOCKING** |

**Verdict**: **BLOCKING** -- Returns empty channels array instead of updated channels.

---

### 3.12 PUT /api/channels/:channelId

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Updated channel | camelCase fields (Drizzle) | **snake_case** fields (MapScan from `queryRow`) | **BLOCKING** |

**Verdict**: **BLOCKING**.

---

### 3.13 DELETE /api/channels/:channelId

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

**Verdict**: OK.

---

### 3.14 POST /api/routes/rebuild

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Queued response | `{ success: true, queued, reused, jobId, status, message }` | `{ "success": true, "queued", "reused", "jobId" (stub), "status", "message" }` | **OK** |
| Sync response | `{ success: true, ...rebuildStats }` | Not implemented (always returns queued) | **HIGH** (stub) |

**Verdict**: Stub. Only async path exists.

---

## Section 4: Stats Endpoints

### 4.1 GET /api/stats/dashboard

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Top-level shape | `{ generatedAt, siteCount, accountCount, totalTokens?, totalCost?, ...manyMore }` | `{ generatedAt, siteCount, accountCount, totalTokens?, totalCost? }` | **HIGH** (stub, missing many fields) |
| `generatedAt` value | Valid ISO 8601 datetime | **Empty string** (`nowUTC()` returns `""`) | **HIGH** |
| `view=summary` | Summary payload fields | Only `siteCount`, `accountCount` | **HIGH** (stub) |
| `view=insights` | Insights payload fields | Only `totalTokens`, `totalCost` | **HIGH** (stub) |

**Verdict**: Missing most dashboard fields. `generatedAt` is empty string.

---

### 4.2 GET /api/stats/proxy-logs

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| `items[]` field names | camelCase: `modelRequested`, `modelActual`, `status`, `httpStatus`, `isStream`, `firstByteLatencyMs`, `latencyMs`, `promptTokens`, `completionTokens`, `totalTokens`, `estimatedCost`, `billingDetails`, `errorMessage`, `clientFamily`, `clientAppId`, `clientAppName`, `clientConfidence`, `usageSource`, `username`, `siteId`, `siteName`, `siteUrl`, `downstreamKeyId`, `downstreamKeyName`, `downstreamKeyGroupName`, `downstreamKeyTags` | **snake_case**: `model_requested`, `model_actual`, `status`, `http_status`, `is_stream`, `first_byte_latency_ms`, `latency_ms`, `prompt_tokens`, `completion_tokens`, `total_tokens`, `estimated_cost`, `billing_details`, `error_message`, etc. | **BLOCKING** |
| Top-level shape | `{ items, total, page, pageSize, clientOptions?, summary?, sites? }` | Same structure | **OK** |
| `billingDetails` in detail view | Parsed JSON object | Parsed JSON object (manual) | **OK** |

**Verdict**: **BLOCKING** -- All item field names are snake_case.

---

### 4.3 GET /api/stats/proxy-logs/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Field names | camelCase (manually mapped via `mapProxyLogRow`) | **snake_case** (raw MapScan) | **BLOCKING** |
| `billingDetails` | Parsed JSON object | Parsed JSON object (manual) | **OK** |
| Client fields | `clientFamily`, `clientAppId`, `clientAppName`, `clientConfidence` | **Missing** (no wrapping logic applied) | **BLOCKING** |
| `username`, `siteName` | Present | Present (joined columns) | **OK** |

**Verdict**: **BLOCKING** -- Field names wrong, client metadata missing.

---

### 4.4 GET /api/stats/proxy-debug/traces

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Field names | camelCase (Drizzle) | **snake_case** (MapScan) | **BLOCKING** |
| Response shape | `{ items: [...] }` | `{ "items": [...] }` | **OK** |

**Verdict**: **BLOCKING** -- All field names are snake_case.

---

### 4.5 GET /api/stats/proxy-debug/traces/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Field names | camelCase + `attempts[]` sub-array | **snake_case** + `attempts[]` sub-array | **BLOCKING** |

**Verdict**: **BLOCKING**.

---

### 4.6 GET /api/models/marketplace

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ models: [{ name, accountCount, tokenCount, avgLatency, successRate, description, tags, supportedEndpointTypes, pricingSources, accounts }], meta: { refreshRequested, refreshQueued, refreshReused, refreshRunning, refreshJobId, includePricing, cacheHit? } }` | Stub: `{ models: [], meta: { refreshRequested: false, refreshQueued: false, refreshReused: false, refreshRunning: false, refreshJobId: null, includePricing: false } }` | **HIGH** (stub) |

**Verdict**: Stub. Returns empty models.

---

### 4.7 GET /api/models/token-candidates

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ models, modelsWithoutToken, modelsMissingTokenGroups, endpointTypesByModel }` | Stub: all empty maps | **HIGH** (stub) |

**Verdict**: Stub.

---

## Section 5: Error Response Format Inconsistency Table

Go uses three different error key patterns inconsistently. The TS codebase expects specific keys per endpoint:

| Endpoint Family | TS Error Key | Go Error Key | Impact |
|-----------------|-------------|-------------|--------|
| Sites POST/PUT/DELETE | `{ error: "..." }` | `{ "error": "..." }` | OK |
| Sites batch (POST) | `{ message: "..." }` | `{ "error": "..." }` | BLOCKING |
| Sites detect (POST) | `{ error: "..." }` | `{ "error": "..." }` | OK |
| Accounts create (POST) | `{ success: false, message: "..." }` | `{ success: false, error: "..." }` (some) / `{ success: false, message: "..." }` (others) | HIGH |
| Accounts login (POST) | `{ success: false, message: "..." }` | `{ success: false, error: "..." }` (payload) | HIGH |
| Accounts verify-token (POST) | `{ success: false, message: "..." }` | `{ success: false, error: "..." }` (payload) | HIGH |
| Accounts update (PUT) | `{ message: "..." }` | `{ "error": "..." }` (some) / `{ "message": "..." }` (others) | HIGH |
| Accounts batch (POST) | `{ message: "..." }` | `{ "error": "..." }` | BLOCKING |
| Routes CRUD | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | OK |
| Stats/proxy-logs | `{ message: "..." }` | `{ "message": "..." }` | OK |

---

## Summary: Severity Counts

| Severity | Count | Description |
|----------|-------|-------------|
| **BLOCKING** | 17 | Field name mismatches (snake_case/PascalCase vs camelCase), missing required fields, inconsistent error keys |
| **HIGH** | 12 | Missing optional fields, stub implementations, error key inconsistencies |
| **LOW** | 3 | Cosmetic (empty string placeholder, stub values) |

---

## Recommended Fix Priority

### Priority 0: Fix queryRows to return camelCase (unblocks 10+ endpoints)

Two approaches:
- **Option A**: Add a `mapKeysToCamelCase` transform after `MapScan` in `queryRows`. Apply to all callers (most endpoints).
- **Option B**: Rewrite each handler to use `sqlx.StructScan` with structs that have `json` tags, then build response maps manually.

Option A is faster but may mask the need for per-endpoint structural fixes (see Priority 1). Option B is the correct long-term approach and should be done endpoint-by-endpoint.

### Priority 1: Fix struct JSON serialization (PascalCase)

Add `json:"camelCase"` tags to every field in `store/schema.go`. Example:
```go
type Account struct {
    ID          int64  `db:"id" json:"id"`
    SiteID      int64  `db:"site_id" json:"siteId"`
    AccessToken string `db:"access_token" json:"accessToken"`
    // ... all other fields
}
```

This fixes endpoint handlers that serialize structs directly (PUT /api/accounts/:id, GET /api/accounts sites array).

### Priority 2: Fix individual structural deficiencies

1. GET /api/accounts: `account` field must be camelCase (fix MapScan + add structured nesting)
2. POST /api/accounts/login: return actual account object, not null
3. POST /api/accounts/:id/rebind-session: return `account` field
4. GET /api/routes: channels must have nested `account`, `site`, `token`, `routeUnit` sub-objects
5. PUT /api/channels/batch: return updated channels array, not empty
6. GET /api/routes/lite: add `sourceRouteIds` field
7. GET /api/stats/dashboard: return all payload fields from TS, fix `nowUTC()`

### Priority 3: Normalize error keys

Standardize on the TS-per-endpoint error key:
- Sites: always `error`
- Accounts: always `success: false` + `message`
- Routes: always `success: false` + `message`
