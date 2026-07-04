# API Endpoint Compatibility Audit (Uncovered Endpoints)

**Date**: 2026-07-04
**Scope**: All /api/* and /v1/* endpoints NOT covered by `api-format-comparison.md`
**Severity**: BLOCKING (breaks frontend) / HIGH (noticeable) / LOW (cosmetic) / STUB (not implemented)

This document supplements `api-format-comparison.md` which already covers: Sites, Accounts, Token Routes, Stats dashboard/proxy-logs/proxy-debug/models-marketplace/token-candidates, and error key inconsistencies.

---

## Section 1: Account Tokens

TS source: `metapi/src/server/routes/api/accountTokens.ts`
Go source: `metapi-go/handler/admin/account_tokens.go`
Contracts: `metapi/src/server/contracts/accountTokensRoutePayloads.ts`

### 1.1 GET /api/account-tokens

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query param: `accountId` | Optional filter | Supported | **OK** |
| Response field names | camelCase (Drizzle) | **snake_case** (MapScan) | **BLOCKING** |
| `valueStatus` field | `ready` / `masked_pending` / `empty` | Correct from DB | **OK** |
| Token value masking | `maskToken()` scrubs raw value | Present | **OK** |

### 1.2 POST /api/account-tokens

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request body fields | `accountId`, `name`, `tokenGroup`, `accessToken`, `apiToken`, `expiresAt`, `autoRefresh` | Same (via payload parsing) | **OK** |
| Success response | Token object (camelCase) | **snake_case** (MapScan) | **BLOCKING** |
| `value` field masking | Token value masked (never returned raw) | Masked | **OK** |
| Error: validation | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

### 1.3 POST /api/account-tokens/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request shape | `{ tokens: [...] }` | Same | **OK** |
| Success response | `{ success: true, items: [...] }` | Same structure | **OK** |
| Item field names | camelCase | **snake_case** | **BLOCKING** |
| Error: validation | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

### 1.4 PUT /api/account-tokens/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | Token object (camelCase) | **snake_case** (MapScan) | **BLOCKING** |
| Error: 404 | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

### 1.5 POST /api/account-tokens/:id/default

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 1.6 GET /api/account-tokens/:id/value

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ value: maskedValue }` | `{ "value": maskedValue }` | **OK** |
| 404 handling | `{ success: false, message: "..." }` | `{ "error": "..." }` | **HIGH** |

### 1.7 DELETE /api/account-tokens/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 1.8 POST /api/account-tokens/sync/:accountId

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | Detailed sync result object (camelCase) | Stub/partial | **HIGH** |
| Error: 404 account | `{ success: false, message: "..." }` | `{ "error": "..." }` | **HIGH** |

### 1.9 POST /api/account-tokens/sync-all

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response: queued | `{ success: true, queued, reused, jobId, status, message }` | Stub | **HIGH** |
| Response: wait | `{ success: true, ...results }` | Not implemented | **STUB** |

### 1.10 GET /api/account-tokens/groups/:accountId

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ groups: [...], tokens: [...] }` | Stub/partial | **HIGH** |
| Field names | camelCase | **snake_case** | **BLOCKING** |

### 1.11 GET /api/account-tokens/account/:accountId/default

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ defaultTokenId: number|null }` (camelCase) | Stub | **HIGH** |
| 404 handling | `{ success: false, message: "..." }` | `{ "error": "..." }` | **HIGH** |

**Account Tokens Verdict**: **BLOCKING** -- snake_case field names across all list/create/update responses. Error key `"error"` vs `"message"` on several endpoints.

---

## Section 2: Events

TS source: `metapi/src/server/routes/api/events.ts`
Go source: `metapi-go/handler/admin/events.go`

### 2.1 GET /api/events

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query params: `limit`, `offset`, `type`, `read` | Supported | Supported | **OK** |
| Response field names | camelCase (Drizzle) | **snake_case** (`created_at`, `related_type`, `related_id`) -- raw MapScan | **BLOCKING** |
| Response type | Direct array of events | Direct array of events | **OK** |

### 2.2 GET /api/events/count

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ count: number }` | `{ "count": number }` | **OK** |

### 2.3 POST /api/events/:id/read

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 2.4 POST /api/events/read-all

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 2.5 DELETE /api/events

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

**Events Verdict**: **BLOCKING** -- snake_case field names in GET /api/events. All mutation endpoints are OK.

---

## Section 3: Search

TS source: `metapi/src/server/routes/api/search.ts`
Go source: `metapi-go/handler/admin/search.go`

### 3.1 POST /api/search

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request body | `{ query: string, limit?: number }` | Same | **OK** |
| Response shape | `{ accounts, accountTokens, sites, checkinLogs, proxyLogs, models }` | Same | **OK** |
| Account field names | camelCase (Drizzle) | camelCase (via `mapKeysToCamel`) | **OK** |
| `account.segment` field | `"session"` or `"apikey"` | **Missing** -- not computed | **HIGH** |
| Account `site` sub-object | `{ id, name, platform, url, ... }` | Flat: just `siteName`, `sitePlatform` strings | **HIGH** |
| `accountTokens` shape | `{ ...token, account: {id, username, segment}, site: {...} }` | Flat: token with `accountUsername`, `siteName` | **HIGH** |
| Models shape | `{ name, accountCount, tokenCount, siteCount }` | `{ modelName, tokenCount, accountCount, siteCount }` (snake_case key `modelName` despite mapKeysToCamel -- "model_name" -> "modelName" is correct) | **OK** |
| `checkinLogs` column names | camelCase (Drizzle) | camelCase (via mapKeysToCamel) | **OK** |
| `proxyLogs` column names | camelCase (Drizzle) | camelCase (via mapKeysToCamel) | **OK** |

**Search Verdict**: **HIGH** -- `queryRows` in search.go correctly uses `mapKeysToCamel` (unlike events.go which does not). However, structural differences exist: missing `segment` on accounts, flat instead of nested `site`/`account` sub-objects.

---

## Section 4: Settings (Runtime, Brand, Database, Backup, Notify, Maintenance)

TS source: `metapi/src/server/routes/api/settings.ts`
Go sources: `handler/admin/settings.go`, `settings_database.go`, `settings_backup.go`, `settings_notify.go`, `settings_maintenance.go`

### 4.1 GET /api/settings/runtime

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | All runtime config keys from DB settings | Keys from Go `config.Config` | **HIGH** |
| Field names | camelCase | **PascalCase** in some nested objects (Go struct serialization) | **HIGH** |
| `payloadRules` field | Parsed rules config | **Missing** | **HIGH** |
| `adminIpAllowlist` field | Present | **Missing** | **HIGH** |
| `proxyToken` field | Masked | **Missing** | **HIGH** |
| `systemProxyUrl` field | Present | **Missing** | **HIGH** |
| `proxyErrorKeywords`, `proxyEmptyContentFailEnabled` | Present | **Missing** | **HIGH** |
| `globalBlockedBrands`, `globalAllowedModels` | Present | **Missing** | **HIGH** |

### 4.2 PUT /api/settings/runtime

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request body | `RuntimeSettingsBody` (20+ fields) | Partial support | **HIGH** |
| Validation | Extensive per-field | Minimal | **HIGH** |
| Success response | `{ success: true, message: "..." }` | `{ "success": true, "message": "..." }` | **OK** |
| Persistence | Writes to `settings` table | Updates `config.Config` in memory | **HIGH** (no DB persistence) |

### 4.3 GET /api/settings/brand-list

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ brands: string[] }` | Stub: `{ "brands": [] }` | **LOW** (stub) |

### 4.4 POST /api/settings/system-proxy/test

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ proxyUrl: string }` | Same | **OK** |
| Success response | `{ success: true, latencyMs, proxyIp? }` | Not implemented | **STUB** |
| Error response | `{ success: false, message: "..." }` | Stub | **STUB** |

### 4.5 GET /api/settings/database/runtime

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response fields | `{ dialect, connectionString (masked), ssl }` | Same (manual) | **OK** |

### 4.6 PUT /api/settings/database/runtime

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ dialect, connectionString, ssl }` | Same | **OK** |
| Success response | `{ success: true, message: "..." }` | Same | **OK** |

### 4.7 POST /api/settings/database/test-connection

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true, message, dialect, latencyMs? }` or `{ success: false, message }` | Stub/partial | **STUB** |

### 4.8 POST /api/settings/database/migrate

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response shape | `{ success: true, ...stats }` | Stub | **STUB** |

### 4.9 GET /api/settings/backup/export

TS route: GET /api/settings/backup/export?type=
Go route: GET /api/settings/backup/export

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query param `type` | `"database"` / `"settings"` | Supported | **OK** |
| Response | Binary file download or JSON | Stub | **STUB** |

### 4.10 POST /api/settings/backup/import

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | Multipart file upload | Stub | **STUB** |

### 4.11 GET /api/settings/backup/webdav

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ enabled, fileUrl, username, maskedPassword?, ... }` | Stub | **STUB** |

### 4.12 PUT /api/settings/backup/webdav

Parsing, validation, save -- Go stub.

### 4.13 POST /api/settings/backup/webdav/export

Go stub.

### 4.14 POST /api/settings/backup/webdav/import

Go stub.

### 4.15 POST /api/settings/notify/test

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` (stub) | **OK** |

### 4.16 POST /api/settings/maintenance/clear-cache

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 4.17 POST /api/settings/maintenance/clear-usage

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true, ...stats }` | `{ "success": true }` | **OK** |

### 4.18 POST /api/settings/maintenance/factory-reset

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true, message: "..." }` | Stub | **STUB** |

**Settings Verdict**: **HIGH** -- Runtime settings response missing many fields (`payloadRules`, `adminIpAllowlist`, `proxyToken`, etc.). Most backup/system-proxy/migrate endpoints are stubs. Runtime PUT writes to in-memory config only, not DB.

---

## Section 5: Auth Settings

TS source: `metapi/src/server/routes/api/auth.ts`
Go source: `metapi-go/handler/admin/auth_settings.go`

### 5.1 POST /api/settings/auth/change

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ oldToken, newToken }` | Same | **OK** |
| Success | `{ success: true, message: "Token 已更新" }` | Same | **OK** |
| Error: wrong old token | `{ success: false, message: "..." }` (403) | `{ "success": false, "message": "..." }` | **OK** |
| Error: validation | `{ success: false, message: "..." }` (400) | `{ "success": false, "message": "..." }` | **OK** |

### 5.2 GET /api/settings/auth/info

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ masked: "xxxx****xxxx" }` | Same | **OK** |

**Auth Settings Verdict**: **OK** -- Correctly implemented, consistent error keys.

---

## Section 6: Checkin

TS source: `metapi/src/server/routes/api/checkin.ts`
Go source: `metapi-go/handler/admin/checkin_routes.go`

### 6.1 POST /api/checkin/trigger

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response: queued | `{ success: true, queued, reused, jobId, status, message }` (202) | Stub | **STUB** |

### 6.2 POST /api/checkin/trigger/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Checkin result for single account | Stub | **STUB** |

### 6.3 GET /api/checkin/logs

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query params | `limit`, `offset`, `accountId` | Supported | **OK** |
| Field names | camelCase | snake_case (raw MapScan) | **BLOCKING** |

### 6.4 PUT /api/checkin/schedule

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ mode, cron?, intervalHours? }` | Same | **OK** |
| Success | `{ success: true }` | `{ "success": true }` | **OK** |

**Checkin Verdict**: **BLOCKING** -- snake_case in GET /api/checkin/logs. trigger endpoints are stubs.

---

## Section 7: Downstream API Keys

TS source: `metapi/src/server/routes/api/downstreamApiKeys.ts`
Go source: `metapi-go/handler/admin/downstream_keys.go`

### 7.1 GET /api/downstream-keys/summary

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query params | `range`, `status`, `search`, `group`, `tags`, `tagMatch` | Partially supported | **HIGH** |
| Response field names | camelCase | **snake_case** (raw MapScan) | **BLOCKING** |

### 7.2 GET /api/downstream-keys/:id/overview

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response field names | camelCase | **snake_case** | **BLOCKING** |
| `policy` field | Parsed JSON object | Parsed JSON object | **OK** |

### 7.3 GET /api/downstream-keys/:id/trend

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query params | `range`, `timeZone` | Supported | **OK** |
| Response `buckets` | Array of trend data points | snake_case fields | **BLOCKING** |

### 7.4 GET /api/downstream-keys

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Array of downstream key objects (camelCase) | **snake_case** | **BLOCKING** |

### 7.5 POST /api/downstream-keys

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | Created key object (camelCase) | **snake_case** | **BLOCKING** |
| Validation error | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |

### 7.6 PUT /api/downstream-keys/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Success response | Updated key object (camelCase) | **snake_case** | **BLOCKING** |

### 7.7 POST /api/downstream-keys/:id/reset-usage

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 7.8 DELETE /api/downstream-keys/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 7.9 POST /api/downstream-keys/batch

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Actions | enable, disable, delete | Same | **OK** |
| Success | `{ success: true, successIds, failedItems }` | Same | **OK** |
| Validation error | `{ message: "..." }` | `{ "error": "..." }` | **BLOCKING** |

**Downstream Keys Verdict**: **BLOCKING** -- All read endpoint field names are snake_case. Batch validation uses `"error"` instead of `"message"`.

---

## Section 8: Monitor

TS source: `metapi/src/server/routes/api/monitor.ts`
Go source: `metapi-go/handler/admin/monitor.go`

### 8.1 GET /api/monitor/config

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ ldohCookieConfigured, ldohCookieMasked }` | Same | **OK** |

### 8.2 PUT /api/monitor/config

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ ldohCookie }` | Same | **OK** |
| Success | `{ success: true, message, ldohCookieConfigured, ldohCookieMasked }` | Same | **OK** |
| Error | `{ success: false, message: "..." }` (400) | Same | **OK** |

### 8.3 POST /api/monitor/session

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` + Set-Cookie header | Same | **OK** |

### 8.4 ALL /monitor-proxy/ldoh/*

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Proxy behavior | Full reverse-proxy with cookie rewrites, HTML text rewriting | Stub | **STUB** |
| Auth check | `meta_monitor_auth` cookie check | Stub | **STUB** |

**Monitor Verdict**: Config endpoints are OK. LDOH proxy is a stub.

---

## Section 9: OAuth

TS source: `metapi/src/server/routes/api/oauth.ts`
Go source: `metapi-go/handler/admin/oauth_routes.go`

### 9.1 GET /api/oauth/providers

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | List of provider descriptors | Stub/partial | **HIGH** |

### 9.2 POST /api/oauth/providers/:provider/start

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ siteId? }` | Same | **OK** |
| Response | `{ sessionId, authUrl }` | Stub | **STUB** |

### 9.3 GET /api/oauth/sessions/:state

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Session status object | Stub | **STUB** |

### 9.4 POST /api/oauth/sessions/:state/manual-callback

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` or error | Stub | **STUB** |

### 9.5 GET /api/oauth/connections

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Array of connection objects (camelCase) | **snake_case** (raw MapScan) | **BLOCKING** |

### 9.6 POST /api/oauth/connections/:accountId/rebind

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Connection result object | Stub | **STUB** |

### 9.7 PATCH /api/oauth/connections/:accountId/proxy

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Updated connection | Stub | **STUB** |

### 9.8 DELETE /api/oauth/connections/:accountId

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 9.9 POST /api/oauth/connections/:accountId/quota/refresh

Stub.

### 9.10 POST /api/oauth/connections/quota/refresh-batch

Stub.

### 9.11 POST /api/oauth/import

Stub.

### 9.12 POST /api/oauth/route-units

Stub.

### 9.13 PATCH /api/oauth/route-units/:routeUnitId

Stub.

### 9.14 DELETE /api/oauth/route-units/:routeUnitId

OK.

### 9.15 GET /api/oauth/callback/:provider

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | OAuth callback handler (redirect) | Stub | **STUB** |

**OAuth Verdict**: **BLOCKING** for connections list (snake_case). All other endpoints are stubs.

---

## Section 10: Site Announcements

TS source: `metapi/src/server/routes/api/siteAnnouncements.ts`
Go source: `metapi-go/handler/admin/site_announcements.go`

### 10.1 GET /api/site-announcements

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query params | `limit`, `offset`, `read` | Supported | **OK** |
| Response field names | camelCase | **snake_case** (raw MapScan) | **BLOCKING** |

### 10.2 POST /api/site-announcements/:id/read

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ success: true }` | `{ "success": true }` | **OK** |

### 10.3 POST /api/site-announcements/read-all

OK.

### 10.4 DELETE /api/site-announcements

OK.

### 10.5 POST /api/site-announcements/sync

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ siteId? }` | Same | **OK** |
| Response | Sync result | Stub | **STUB** |

**Site Announcements Verdict**: **BLOCKING** -- snake_case in GET response.

---

## Section 11: Stats (Uncovered Endpoints)

### 11.1 GET /api/stats/site-distribution

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ items: [{ siteName, siteId, totalCost, totalTokens, ... }] }` | Stub: `{ "items": [] }` | **STUB** |

### 11.2 GET /api/stats/site-trend

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ buckets: [{ date, cost, tokens, siteBreakdown }] }` | Stub: `{ "buckets": [] }` | **STUB** |

### 11.3 GET /api/stats/model-by-site

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ items: [{ siteName, models: [...], ... }] }` | Stub: `{ "items": [] }` | **STUB** |

### 11.4 POST /api/models/check/:accountId

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | None (uses URL param) | Same | **OK** |
| Response | Model check result (available/unavailable models) | Stub | **STUB** |

### 11.5 POST /api/models/probe

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Request | `{ model, accountId? }` | Same | **OK** |
| Response | Probe result with latency, error | Stub | **STUB** |

**Stats Verdict**: All 5 uncovered endpoints are **STUB** implementations.

---

## Section 12: Tasks

TS source: `metapi/src/server/routes/api/tasks.ts`
Go source: `metapi-go/handler/admin/tasks.go`

### 12.1 GET /api/tasks

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Query param `limit` | Supported | Supported | **OK** |
| Response shape | `{ tasks: [...] }` | `{ "tasks": [...] }` | **OK** |
| Task field names | camelCase | **snake_case** / PascalCase (struct serialization) | **BLOCKING** |

### 12.2 GET /api/tasks/:id

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Found response | `{ success: true, task: {...} }` | `{ "success": true, "task": {...} }` | **OK** |
| Not found | `{ success: false, message: "task not found" }` (404) | `{ "success": false, "message": "task not found" }` | **OK** |
| Task field names | camelCase | **snake_case** | **BLOCKING** |

**Tasks Verdict**: **BLOCKING** -- snake_case task field names.

---

## Section 13: Test Endpoints

TS source: `metapi/src/server/routes/api/test.ts`
Go source: `metapi-go/handler/admin/test.go`

All 10 test endpoints are **STUB** implementations in Go:

| Endpoint | TS Status | Go Status |
|----------|-----------|-----------|
| POST /api/test/proxy | Fully implemented | Stub `{ success: true, message: "not yet implemented" }` |
| POST /api/test/proxy/stream | Fully implemented (SSE) | Stub |
| POST /api/test/proxy/jobs | Async job queue | Stub (returns fake jobId) |
| GET /api/test/proxy/jobs/:jobId | Job status with results | Stub (always 404) |
| DELETE /api/test/proxy/jobs/:jobId | Cancel running job | `{ success: true }` stub |
| POST /api/test/chat | Full chat test with routing | Stub |
| POST /api/test/chat/stream | Streaming chat test | Stub |
| POST /api/test/chat/jobs | Async chat job | Stub |
| GET /api/test/chat/jobs/:jobId | Chat job status | Stub |
| DELETE /api/test/chat/jobs/:jobId | Cancel chat job | `{ success: true }` stub |

**Test Verdict**: All **STUB** -- No real implementation. Error response format differs: stubs use `{ "error": { "message": "...", "type": "..." } }` while TS uses `{ error: "..." }`.

---

## Section 14: Update Center

TS source: `metapi/src/server/routes/api/updateCenter.ts`
Go source: `metapi-go/handler/admin/update_center.go`

### 14.1 GET /api/update-center/status

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | `{ currentVersion, latestVersion, updateAvailable, ... }` | Stub/partial | **STUB** |

### 14.2 POST /api/update-center/check

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Response | Check result | Stub | **STUB** |

### 14.3 PUT /api/update-center/config

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Go-only route (not in TS) | -- | New feature | **INFO** |

### 14.4 POST /api/update-center/deploy

TS route present. Go stub.

### 14.5 POST /api/update-center/rollback

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| TS route present | Yes | Stub | **STUB** |

### 14.6 GET /api/update-center/tasks/:id/stream

TS route present. Go stub.

**Update Center Verdict**: All **STUB**.

---

## Section 15: Proxy Routes (/v1/* and non-/v1/*)

TS proxy sources: `metapi/src/server/routes/proxy/*.ts`
Go proxy sources: `metapi-go/handler/proxy/*.go`

### 15.1 POST /v1/chat/completions + POST /chat/completions

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route exists | Yes (both /v1 and non-/v1) | Yes (both paths) | **OK** |
| Request body | OpenAI Chat Completion format | Same format accepted | **OK** |
| Streaming response | SSE / NDJSON | SSE copying | **OK** |
| Error format | OpenAI `{ error: { message, type } }` | Same format via `writeJSONError` | **OK** |

### 15.2 POST /v1/messages + POST /messages

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route exists | Yes (both /v1 and non-/v1) | Yes (both paths) | **OK** |
| Request body | Anthropic Messages format | Same | **OK** |
| Stream support | Yes (SSE) | Partially implemented | **HIGH** |

### 15.3 POST /v1/messages/count_tokens + POST /messages/count_tokens

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route exists | Yes (both paths) | Yes (both paths) | **OK** |

### 15.4 POST /v1/completions + POST /completions

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route exists | Yes (both paths) | Yes (both paths) | **OK** |

### 15.5 POST /v1/embeddings

OK -- both implemented.

### 15.6 POST /v1/images/generations

OK -- both implemented.

### 15.7 POST /v1/images/edits

OK -- both implemented.

### 15.8 POST /v1/images/variations

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| Route exists | Yes | Yes | **OK** |
| Implementation | Full multipart handling | Stub (returns error) | **STUB** |

### 15.9 GET /v1/models

OK -- both implemented. Response format matches OpenAI `{ data: [...], object: "list" }`.

### 15.10 POST /v1/responses + GET /v1/responses + POST /v1/responses/compact
### Non-/v1: POST /responses, POST /responses/*, GET /responses, GET /responses/*

| Aspect | TS | Go | Severity |
|--------|-----|-----|----------|
| /v1/responses POST | Full implementation including WS upgrade | HTTP only (no WS) | **HIGH** |
| /v1/responses GET | 426 Upgrade Required | 426 status | **OK** |
| /v1/responses/compact POST | Full implementation | Implemented | **OK** |
| /responses POST | Alias | Implemented | **OK** |
| /responses/* POST | Wildcard alias | Implemented | **OK** |
| /responses GET | 426 | 426 | **OK** |
| /responses/* GET | 426 | 426 | **OK** |

**Responses Verdict**: **HIGH** -- WebSocket endpoint (/v1/responses with WS upgrade) is missing in Go.

### 15.11 POST /v1/search

OK -- both implemented.

### 15.12 POST /v1/videos + GET /v1/videos/:id + DELETE /v1/videos/:id

OK -- both implemented.

### 15.13 Files Routes

| TS Route | Go Route | Status |
|----------|----------|--------|
| POST /v1/files | POST /v1/files | **OK** |
| GET /v1/files | GET /v1/files | **OK** |
| GET /v1/files/:fileId | GET /v1/files/{fileId} | **OK** |
| GET /v1/files/:fileId/content | GET /v1/files/{fileId}/content | **OK** |
| DELETE /v1/files/:fileId | DELETE /v1/files/{fileId} | **OK** |

### 15.14 Gemini Routes

| TS Route | Go Route | Status |
|----------|----------|--------|
| POST /v1beta/models/* | POST /v1beta/models/* | **OK** |
| GET /v1beta/models | GET /v1beta/models | **OK** |
| POST /gemini/:version/models/* | POST /gemini/{geminiApiVersion}/models/* | **OK** |
| GET /gemini/:version/models | GET /gemini/{geminiApiVersion}/models | **OK** |
| POST /v1internal::generateContent | POST /v1internal::generateContent | OK (Go-only: CLI bridge) |
| POST /v1internal::streamGenerateContent | POST /v1internal::streamGenerateContent | OK (Go-only) |
| POST /v1internal::countTokens | POST /v1internal::countTokens | OK (Go-only) |

### 15.15 Missing Proxy Routes

| TS Route | Go Status | Severity |
|----------|-----------|----------|
| GET /v1/files (input files list) | Exists | **OK** |
| POST /v1/input_files | **Missing** -- no Go route maps to `/v1/input_files` | **HIGH** |

**Proxy Routes Verdict**: **HIGH** -- Missing `/v1/input_files` endpoint. WebSocket upgrade for /v1/responses missing. Images/variations is stub.

---

## Section 16: Cross-Cutting Issues

### 16.1 `mapKeysToCamel` Adoption

Only `search.go` uses `mapKeysToCamel` in its `queryRows` helper. All other handlers that use raw `rows.MapScan` (events.go, checkin_routes.go, downstream_keys.go, tasks.go, site_announcements.go, oauth_routes.go) return snake_case keys -- **BLOCKING**.

### 16.2 Error Key Consistency (Expanded)

From the uncovered endpoints:

| Endpoint Family | TS Error Key | Go Error Key | Severity |
|-----------------|-------------|-------------|----------|
| AccountTokens create/update | `{ success: false, message: "..." }` | Mixed | **HIGH** |
| AccountTokens value/sync not-found | `{ success: false, message: "..." }` | `{ "error": "..." }` | **HIGH** |
| DownstreamKeys batch validation | `{ message: "..." }` | `{ "error": "..." }` | **BLOCKING** |
| Auth change validation | `{ success: false, message: "..." }` | `{ "success": false, "message": "..." }` | **OK** |
| Checkin logs | `{ message: "..." }` | `{ "error": "..." }` | **HIGH** |
| Test endpoints (stubs) | `{ error: "..." }` | `{ "error": { "message": "...", "type": "..." } }` | **LOW** (all stubs) |

---

## Section 17: Route Existence Matrix

Endpoints present in TS but missing in Go:

| TS Route | Method | Status |
|----------|--------|--------|
| POST /v1/input_files | POST | **MISSING** |

Endpoints present in Go but missing in TS (new Go features):

| Go Route | Method | Note |
|----------|--------|------|
| PUT /api/update-center/config | PUT | New feature |
| POST /v1internal::generateContent | POST | Gemini CLI bridge (Go-only) |
| POST /v1internal::streamGenerateContent | POST | Gemini CLI bridge (Go-only) |
| POST /v1internal::countTokens | POST | Gemini CLI bridge (Go-only) |

---

## Section 18: Summary of Findings

### Severity Counts (Uncovered Endpoints Only)

| Severity | Count | Description |
|----------|-------|-------------|
| **BLOCKING** | 12 | snake_case field names: account-tokens list/create/update, events list, checkin logs, downstream-keys (all reads), connections list, site-announcements list, tasks; error key mismatch on batch |
| **HIGH** | 18 | Missing response fields (runtime settings, search segment/nesting), missing /v1/input_files, missing WS upgrade, stub bodies for critical endpoints |
| **LOW** | 1 | brand-list stub |
| **STUB** | 35+ | Entire test suite (10), most OAuth (12), most settings backup/webdav/migrate (6), update-center (6), monitor proxy, stats site-* (3), stats model-check/probe (2), checkin trigger |
| **OK** | ~20 | Events mutations, auth settings, monitor config, search structure, proxy POST routes |

### Root Cause Analysis

1. **Inconsistent use of `mapKeysToCamel`**: Only `search.go` and `stats.go` (via shared `queryRows`) convert snake_case DB columns to camelCase JSON. All other handlers serialize raw DB column names -- **12+ endpoints return wrong field names**.

2. **Struct serialization without json tags**: `store/schema.go` structs have only `db` tags. Any endpoint that serializes a struct directly produces PascalCase keys.

3. **Widespread stubs**: Settings backup, OAuth, test endpoints, update center, stats analytics are all stubs. These are expected per the migration plan but worth documenting.

### Recommended Fix Priority

**P0 -- Unblock frontend immediately:**
1. Add `mapKeysToCamel` to all handlers that use raw `rows.MapScan`: events.go, account_tokens.go, downstream_keys.go, oauth_routes.go, checkin_routes.go, site_announcements.go, tasks.go
2. Add `json` tags to all `store/schema.go` structs
3. Fix error key: downstream-keys batch from `"error"` to `"message"`

**P1 -- Fill missing response fields:**
1. Settings runtime: add `payloadRules`, `adminIpAllowlist`, `proxyToken`, `systemProxyUrl`, `proxyErrorKeywords`, `globalBlockedBrands`, `globalAllowedModels`
2. Search: add `segment` to accounts, nest `site` sub-object
3. Accounts create: return full account fields (`checkinEnabled`, `extraConfig`, `isPinned`, `sortOrder`, `balance`, etc.)

**P2 -- Proxy completeness:**
1. Add `POST /v1/input_files` route
2. Implement images/variations multipart support
3. WebSocket upgrade for /v1/responses

**P3 -- Stub implementation:**
1. Test endpoints (chat + proxy tester)
2. OAuth full flow
3. Backup webdav/migrate
4. Update center
