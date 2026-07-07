# P11 Admin API -- Go Implementation Review

**Review date**: 2026-07-04
**Reviewer**: Claude Code
**Scope**: `<repo>/handler/admin/` vs spec `p11-admin-api.md`
**TS reference**: `<metapi-ts>/src/server/routes/api/`

---

## Executive Summary

The Go implementation covers **all 110 endpoint paths** (84 verified + 13 inferred + 13 OAuth delegation) across 18 handler files. Route registration is complete -- every spec endpoint has a chi router entry. However, the implementation depth varies dramatically:

- **4 files are substantially implemented** (events, site_announcements, downstream_keys, auth_settings)
- **3 files are partially implemented** (stats, settings, token_routes)
- **11 files are almost entirely stubs** (settings_database, settings_backup, settings_notify, settings_maintenance, checkin_routes, update_center, monitor, search, tasks, test, oauth_routes)

**43 of 84 verified endpoints** (51%) are stubs returning hardcoded success responses without any real logic. Only **~12 endpoints** have meaningful business logic.

---

## 1. Endpoint-by-Endpoint Coverage Matrix

### 1.1 Stats (12 endpoints) -- `stats.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/stats/dashboard` | **PARTIAL** | Cache headers always `"miss"`. Returns stub data (siteCount, accountCount, totalTokens, totalCost) instead of full dashboard snapshot. No `refresh` param handling. No `generatedAt` set properly (`nowUTC()` returns empty string). |
| 2 | GET `/api/stats/proxy-logs` | **PARTIAL** | Missing query params: `client`, `from`, `to`. The `client` param is required by TS for filtering by `app:xxx` or `family:xxx`. No client detection fallback chain (dedicated columns -> legacy errorMessage parsing). Missing `usageSource` field. Cost estimation fallback (Veloera vs other platforms) not implemented. `page` formula uses integer division on `offset/limit` which matches TS. |
| 3 | GET `/api/stats/proxy-logs/:id` | **DONE** | Correct 400 for non-numeric id, 404 for not found. `billingDetails` JSON parsed. Response uses `{ "message": "..." }` pattern matching TS spec. |
| 4 | GET `/api/stats/proxy-debug/traces` | **DONE** | Correct `{ items: [...] }` shape. |
| 5 | GET `/api/stats/proxy-debug/traces/:id` | **DONE** | Correct 400/404. Loads related attempts from `proxy_debug_attempts`. |
| 6 | GET `/api/models/marketplace` | **STUB** | Returns empty models/meta. No two-tier cache (15s/90s), no `refresh` logic, no background rebuild. |
| 7 | GET `/api/models/token-candidates` | **STUB** | Returns empty maps. No rate limiting (30 req/min). No globalAllowedModels filter. No marketplace cache reuse. |
| 8 | POST `/api/models/check/:accountId` | **STUB** | Correct error shape `{ success: false, error: "Invalid account id" }` matching TS Pattern C. No model refresh performed. Returns stub `refresh`/`rebuild` objects. |
| 9 | POST `/api/models/probe` | **STUB** | Returns 202 with stub job. No actual probe logic, no `wait` param handling. |
| 10 | GET `/api/stats/site-distribution` | **STUB** | Returns `{ distribution: [] }`. No actual query. |
| 11 | GET `/api/stats/site-trend` | **STUB** | Returns `{ trend: [] }`. No actual query. |
| 12 | GET `/api/stats/model-by-site` | **PARTIAL** | Basic query from `model_day_usage` works, but does not call `runUsageAggregationProjectionPass()` as TS does before querying. |

### 1.2 Settings (18 endpoints) -- `settings.go` + 4 sub-files

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/settings/runtime` | **DONE** | Returns all 60+ fields. Masked sensitive values (proxyToken, Telegram bot token, SMTP password). Includes `currentAdminIp`, `serverTimeZone`. |
| 2 | PUT `/api/settings/runtime` | **PARTIAL** | **Major gaps**: (a) No `changedLabels: string[]` tracking -- writes a single generic event instead of per-field events. (b) Event type always `"status"` instead of per-field heuristic (`checkinCron`->`"checkin"`, `balanceRefreshCron`->`"balance"`, `proxyToken`->`"proxy"`). (c) **No scheduler restarts**: `updateCheckinSchedule()`, `updateBalanceRefreshCron()`, `updateLogCleanupSettings()` not called. (d) **No self-lockout prevention** for `adminIpAllowlist` -- current request IP not validated against new whitelist. (e) **Missing cross-field validation**: `webhookUrl` not validated when `webhookEnabled` is true; `barkUrl` same issue; `telegramBotToken`/`telegramChatId`/`telegramMessageThreadId` not validated when telegram is enabled. (f) `proxyErrorKeywords` only handles `[]any` and `string` -- TS also accepts newline-separated strings. (g) `payloadRules` not validated via `parsePayloadRulesConfigInput()`. (h) No `globalBlockedBrands`/`globalAllowedModels` change detection for background route rebuild. (i) Response only includes `{ success, message }` -- TS returns full config after update. |
| 3 | GET `/api/settings/brand-list` | **STUB** | Hardcoded brand names instead of querying from DB. |
| 4 | POST `/api/settings/system-proxy/test` | **STUB** | Always returns `reachable: true` with fake latency. No actual HTTP probe to `https://www.gstatic.com/generate_204`. No `finalUrl` or `probeUrl` in response. |
| 5 | GET `/api/settings/database/runtime` | **DONE** | Returns current `active` dialect from runtime config, masks PostgreSQL credentials, and includes `saved` restart-pending overrides. |
| 6 | PUT `/api/settings/database/runtime` | **PARTIAL** | Saves `db_type`, `db_url`, and `db_ssl`; response separates current `active` from restart-pending `saved`. Runtime switch still requires restart. |
| 7 | POST `/api/settings/database/test-connection` | **DONE** | Tests SQLite/PostgreSQL connections and rejects unsupported dialects. Error responses mask credentials. |
| 8 | POST `/api/settings/database/migrate` | **PARTIAL** | Returns 501 with `metapi-migrate` guidance. Admin API migration is still intentionally not wired. |
| 9 | GET `/api/settings/backup/export` | **STUB** | Returns empty backup object. No actual data export. Missing `exportedAt` timestamp. |
| 10 | POST `/api/settings/backup/import` | **STUB** | Does not call `applyImportedSettingToRuntime()` for imported settings. No scheduler restarts, no cache invalidation. |
| 11 | GET `/api/settings/backup/webdav` | **STUB** | Returns hardcoded disabled config. |
| 12 | PUT `/api/settings/backup/webdav` | **STUB** | Does not actually save config. |
| 13 | POST `/api/settings/backup/webdav/export` | **STUB** | Always returns success. No actual WebDAV export. |
| 14 | POST `/api/settings/backup/webdav/import` | **STUB** | Always returns success. No actual WebDAV import. |
| 15 | POST `/api/settings/notify/test` | **STUB** | Returns "成功 0/0". No actual notification dispatch. |
| 16 | POST `/api/settings/maintenance/clear-cache` | **DONE** | Deletes `model_availability`, `route_channels`, `token_routes`. Returns deleted counts. 202 status. Does NOT trigger background `refreshModelsAndRebuildRoutes()` as TS does. |
| 17 | POST `/api/settings/maintenance/clear-usage` | **DONE** | Deletes proxy_logs, resets route_channel stats, resets `accounts.balanceUsed`. Does NOT fire audit event with level `"warning"`. |
| 18 | POST `/api/settings/maintenance/factory-reset` | **STUB** | Returns `{ success: true }` without calling `performFactoryReset()`. |

### 1.3 Token Routes + Channels (20 endpoints) -- `token_routes.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/routes/lite` | **DONE** | Returns correct fields (id, modelPattern, displayName, displayIcon, routeMode, routingStrategy, enabled). Missing `sourceRouteIds`. Order by id ASC. |
| 2 | GET `/api/routes/summary` | **PARTIAL** | Returns correct shape with channelCount, enabledChannelCount. Missing: `siteNames` always empty `[]string{}` - should be populated from route channels. `sourceRouteIds` always empty `[]int64{}` - should be loaded from `route_group_sources`. **No rate limiting** (spec requires 60 req/min with 429 + retry-after). `decisionSnapshot` parsed correctly. |
| 3 | GET `/api/routes` | **PARTIAL** | Includes channels with account/site join. **No rate limiting** (spec requires 60 req/min). Missing `decisionRefreshedAt` field. `sourceRouteIds` not populated. |
| 4 | GET `/api/routes/:id/channels` | **DONE** | Returns channels with username, site_name. Returns 404 if route not found (but returns `{success: false, message}` instead of checking route existence first -- it returns empty channels array for missing routes). |
| 5 | POST `/api/routes/:id/cooldown/clear` | **DONE** | Clears cooldown_until, consecutive_fail_count, cooldown_level. Doesn't check if route exists (always clears, even for non-existent routes). |
| 6 | POST `/api/routes/:id/channels/batch` | **PARTIAL** | Deduplication by `(accountId, tokenId)` implemented but **missing sourceModel in dedup key** -- spec requires dedup by `(accountId, tokenId, sourceModel)`. Doesn't set `manualOverride: true` (sets `manual_override = 1` correctly). Doesn't block explicit_group routes. Missing `siteNames` in response. |
| 7 | POST `/api/routes/:id/channels` | **PARTIAL** | Deduplication check same issue as batch -- missing sourceModel. Sets `manualOverride: false` (sets `manual_override = 0`). Missing: token-account ownership validation, model support validation for exact-model routes, decision snapshot invalidation, cache invalidation. |
| 8 | GET `/api/routes/decision` | **STUB** | Returns empty decision. |
| 9 | POST `/api/routes/decision/batch` | **STUB** | Returns empty decisions. |
| 10 | POST `/api/routes/decision/by-route/batch` | **STUB** | Returns empty decisions. |
| 11 | POST `/api/routes/decision/route-wide/batch` | **STUB** | Returns empty decisions. |
| 12 | POST `/api/routes/decision/refresh` | **STUB** | Returns 202 with stub job. |
| 13 | POST `/api/routes` (create) | **PARTIAL** | **Missing**: (a) explicit_group validation: sourceRouteIds must be non-empty, all must exist, cannot self-reference, source routes must be exact-model, cannot be explicit_group themselves. (b) `syncExplicitGroupSourceRouteStrategies()` not called. (c) Auto-populate channels via `populateRouteChannelsByModelPattern()` for pattern routes. (d) `modelMapping` field always set to `nil` (hardcoded) instead of marshaling from body. (e) `sourceRouteIds` stored in `route_group_sources` but not validated. |
| 14 | PUT `/api/routes/:id` (update) | **PARTIAL** | **Missing**: (a) Cannot switch routeMode validation not enforced. (b) On modelPattern change: no automatic channel rebuild. (c) No decision snapshot clearing on behavior change. (d) No cache invalidation. (e) `sourceRouteIds` replacement doesn't validate references. |
| 15 | DELETE `/api/routes/:id` | **PARTIAL** | Cleans up route_group_sources and route_channels. Missing: clearing dependent explicit_group snapshots, cache invalidation. |
| 16 | POST `/api/routes/batch` | **PARTIAL** | No max 500 ID limit enforcement. No decision snapshot clearing for affected routes. No dependent group snapshot clearing. |
| 17 | POST `/api/routes/rebuild` | **STUB** | Returns 202 with stub. No body parsing (`refreshModels`, `wait`). |
| 18 | PUT `/api/channels/batch` | **PARTIAL** | Sets manual_override=1. Missing: channel ID existence validation (should return 404 for missing), decision clearing for affected routes. Response `channels` always `[]`. |
| 19 | PUT `/api/channels/:channelId` | **PARTIAL** | Missing: token ownership validation, model support validation, 404 if channel doesn't exist, parent route existence check. |
| 20 | DELETE `/api/channels/:channelId` | **PARTIAL** | Missing: decision clearing for parent route. Returns success even for non-existent channel IDs. |

### 1.4 Downstream API Keys (9 endpoints) -- `downstream_keys.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/downstream-keys/summary` | **PARTIAL** | Missing: `tags` and `tagMatch` filters (spec requires comma/pipe separated, max 20, each max 32 chars). Missing: `rangeUsage` in each item (totalRequests, successRequests, failedRequests, successRate, totalTokens, totalCost). Missing: `hasProxyLogDownstreamApiKeyIdColumn()` gating -- should return null usage if column missing. `keyMasked` added. Group filtering (`__ungrouped__`, specific group) works. |
| 2 | GET `/api/downstream-keys/:id/overview` | **PARTIAL** | Usage always `{ last24h: null, last7d: null, all: null }` -- no actual usage queries. Missing column-readiness gating. |
| 3 | GET `/api/downstream-keys/:id/trend` | **PARTIAL** | Empty buckets always. Missing `timeZone` from query param. Missing column-readiness gating. |
| 4 | GET `/api/downstream-keys` | **DONE** | Lists all keys with keyMasked. |
| 5 | POST `/api/downstream-keys` | **PARTIAL** | **Missing validations**: (a) `allowedRouteIds` existence check in `token_routes`. (b) `siteWeightMultipliers` key existence in `sites`. (c) `excludedSiteIds` existence in `sites`. (d) `excludedCredentialRefs` policy reference validation (account_token kind, default_api_key kind). `siteWeightMultipliers` hardcoded to `'{}'` in INSERT. `excludedCredentialRefs` hardcoded to `'[]'` in INSERT. Tags, supportedModels, excludedSites properly serialized. Duplicate key detection works (both pre-check and unique constraint). |
| 6 | PUT `/api/downstream-keys/:id` | **PARTIAL** | Only updates `name`, `enabled`, `groupName`. **Missing**: `description`, `tags`, `expiresAt`, `maxCost`, `maxRequests`, `supportedModels`, `allowedRouteIds`, `siteWeightMultipliers`, `excludedSiteIds`, `excludedCredentialRefs`. No policy reference validation on update. No duplicate key conflict check if `key` changed. Missing fields should retain existing values (only partially done). |
| 7 | POST `/api/downstream-keys/:id/reset-usage` | **DONE** | Sets used_cost=0, used_requests=0. Returns updated item. |
| 8 | DELETE `/api/downstream-keys/:id` | **DONE** | Returns `{ success: true }`. |
| 9 | POST `/api/downstream-keys/batch` | **PARTIAL** | Handles enable/disable/delete/resetUsage/updateMetadata. **Missing**: max 500 ids enforcement. `tagOperation` not handled. `updateMetadata` only handles groupName set/clear -- missing tag append. No `groupOperation: "keep"` support (default behavior from spec). |

### 1.5 Events (5 endpoints) -- `events.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/events` | **DONE** | Correct limit (1-500, default 30), offset, type filter, read filter. Ordered by created_at DESC. Returns array directly (not wrapped in `{ items }`) -- matches TS? **BUG**: TS uses `data` and `total` in its response. Go returns raw array. The TS `/api/events` implementation in events.ts line 24 returns just the rows array. This matches TS. |
| 2 | GET `/api/events/count` | **DONE** | Returns `{ count: number }`. |
| 3 | POST `/api/events/{id}/read` | **DONE** | Marks one event read. Gracefully handles invalid id. |
| 4 | POST `/api/events/read-all` | **DONE** | Marks all unread. |
| 5 | DELETE `/api/events` | **DONE** | Deletes all. |

### 1.6 Checkin (4 endpoints) -- `checkin_routes.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | POST `/api/checkin/trigger` | **STUB** | Returns 202 with stub job. No dedupeKey. No actual checkin execution. |
| 2 | POST `/api/checkin/trigger/:id` | **STUB** | Returns `success: false, message: "checkin not yet implemented"`. |
| 3 | GET `/api/checkin/logs` | **DONE** | Joins accounts, sites. Supports accountId filter, limit, offset. Missing `failureReason` via `classifyFailureReason()`. |
| 4 | PUT `/api/checkin/schedule` | **STUB** | Does not persist to settings. Does not update scheduler. Error responses use `{ error: "..." }` matching TS Pattern B. |

### 1.7 Update Center (6 endpoints) -- `update_center.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/update-center/status` | **STUB** | Returns hardcoded version 0.0.0. |
| 2 | POST `/api/update-center/check` | **STUB** | Same as status. |
| 3 | PUT `/api/update-center/config` | **STUB** | Does not persist config. Returns echo of input. |
| 4 | POST `/api/update-center/deploy` | **STUB** | Validates targetTag/targetVersion required. Does not check config prerequisites. No deploy guard. No background task spawn. |
| 5 | POST `/api/update-center/rollback` | **STUB** | Validates targetRevision required. No actual rollback. |
| 6 | GET `/api/update-center/tasks/:id/stream` | **STUB** | Sets SSE headers correctly (`text/event-stream`, `Cache-Control: no-cache, no-transform`, `Connection: keep-alive`). Does NOT use `reply.hijack()` (chi doesn't have Fastify's hijack). Sends `done` event immediately. No `setInterval` polling, no client disconnect cleanup. |

### 1.8 Monitor (5 endpoints) -- `monitor.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/monitor/config` | **PARTIAL** | Reads LDOH cookie from settings. **No rate limiting** (spec requires 30 req/min). |
| 2 | PUT `/api/monitor/config` | **STUB** | Does not actually save cookie. No validation of `ld_auth_session=` prefix or length >= 24. **No rate limiting** (spec requires 10 req/min). |
| 3 | POST `/api/monitor/session` | **DONE** | Sets HttpOnly cookie correctly (Path=/, SameSite=Lax, Max-Age=7200). **No rate limiting** (spec requires 10 req/min). |
| 4-5 | ALL `/monitor-proxy/ldoh*` | **STUB** | Returns 400 error. No reverse proxy, no URL rewriting, no cookie auth check. **No rate limiting** (spec requires 60 req/min). |

### 1.9 Site Announcements (5 endpoints) -- `site_announcements.go`

| # | Endpoint | Status | Issues |
|---|----------|--------|--------|
| 1 | GET `/api/site-announcements` | **DONE** | Correctly handles siteId, platform, read, status filters. Status filtering logic (active/expired/dismissed) matches TS spec exactly. Pagination applied after content filters. Date fields are raw from DB (not localized to server timezone as TS does). |
| 2 | POST `/api/site-announcements/{id}/read` | **DONE** | Sets read_at. |
| 3 | POST `/api/site-announcements/read-all` | **DONE** | Sets read_at on all. |
| 4 | DELETE `/api/site-announcements` | **DONE** | Deletes all. |
| 5 | POST `/api/site-announcements/sync` | **STUB** | Returns stub taskId. No actual sync. |

### 1.10 Unverified Modules

| Module | File | Status |
|--------|------|--------|
| Search (1 endpoint) | `search.go` | **DONE** -- Searches across 6 tables (sites, accounts, accountTokens, checkinLogs, proxyLogs, models). Correct per-category limiting. |
| Tasks (2 endpoints) | `tasks.go` | **STUB** -- In-memory task registry not implemented. |
| Test (8 endpoints) | `test.go` | **STUB** -- All 8 endpoints return stub responses. Route registration correct. |
| Auth Settings (2 endpoints) | `auth_settings.go` | **DONE** -- Token view with masking, token change with old-token verification, persistence to settings table, event logging. |
| OAuth (13 endpoints) | `oauth_routes.go` | **STUB** -- All 13 endpoints return stub responses. Callback provides basic window.close() HTML. Connections endpoint queries DB for accounts with oauth_provider. |

---

## 2. Critical Cross-Cutting Gaps

### 2.1 Rate Limiting -- NOT IMPLEMENTED

The spec requires rate limiting on 7 endpoints. The Go implementation has zero rate limiting anywhere:

| Endpoint | Spec Limit | Status |
|----------|-----------|--------|
| GET `/api/models/token-candidates` | 30 req/min | Missing |
| GET `/api/routes/summary` | 60 req/min | Missing |
| GET `/api/routes` | 60 req/min | Missing |
| GET `/api/monitor/config` | 30 req/min | Missing |
| PUT `/api/monitor/config` | 10 req/min | Missing |
| POST `/api/monitor/session` | 10 req/min | Missing |
| ALL `/monitor-proxy/ldoh*` | 60 req/min | Missing |

The TS implementation uses `rate-limiter-flexible` (in-memory). Go should use `golang.org/x/time/rate` or equivalent at the chi middleware level.

### 2.2 Dashboard Cache Headers -- Always "miss"

`stats.go:46-47` always sets cache headers to `"miss"`. The TS implementation integrates with snapshot services (`getDashboardSummarySnapshot`, `getDashboardInsightsSnapshot`) that track `hit`/`miss`/`stale` state. The Go implementation must either integrate with equivalent caching or at minimum implement TTL-based cache with the three-state header enum.

### 2.3 Settings PUT Side Effects -- Missing Chain

The TS `PUT /api/settings/runtime` fires an extensive side-effect chain:
1. **changedLabels tracking** -- per-field change detection, recorded in event
2. **Event type heuristic** -- single-field changes get specific event types (`checkinCron`->`"checkin"`, `balanceRefreshCron`->`"balance"`, `proxyToken`->`"proxy"`)
3. **Scheduler updates** -- `updateCheckinSchedule()`, `updateBalanceRefreshCron()`, `updateLogCleanupSettings()`
4. **Service lifecycle** -- `stopProxyLogRetentionService()`, `startModelAvailabilityProbeScheduler()`/`stopModelAvailabilityProbeScheduler()`
5. **Cache invalidation** -- `invalidateSiteProxyCache()`
6. **Background route rebuild** -- when `globalBlockedBrands` or `globalAllowedModels` change
7. **Self-lockout prevention** -- current request IP must be in new `adminIpAllowlist`

The Go implementation does NONE of these. It saves to DB and writes a single generic event.

### 2.4 Missing Validation Chains

| Validation | Spec Requirement | Go Status |
|-----------|-----------------|-----------|
| `proxyToken` format | starts with `sk-`, >=6 chars | Done |
| `systemProxyUrl` | validated via `normalizeSiteProxyUrl()` | Missing |
| `checkinCron` | valid cron expression | Missing (string only, not parsed) |
| `checkinScheduleMode` | `"cron"` or `"interval"` literal | Done |
| `checkinIntervalHours` | integer 1-24 | Done |
| `balanceRefreshCron` | valid cron expression | Missing |
| `logCleanupCron` | valid cron expression | Missing |
| `logCleanupRetentionDays` | integer >= 1, normalized | Done (>= 1 check, no normalization function) |
| `webhookUrl` | non-empty + valid URL if webhookEnabled is true | Missing cross-field validation |
| `barkUrl` | non-empty + valid URL if barkEnabled is true | Missing cross-field validation |
| `telegramEnabled` chain | if true: botToken required (contains `:`), chatId required, messageThreadId matches `/^[1-9]\d*$/`, apiBaseUrl valid URL | Missing cross-field validation |
| `smtpPort` | positive integer | Not validated |
| `notifyCooldownSec` | >= 0 | Done |
| `adminIpAllowlist` | each entry valid IP/CIDR + self-lockout prevention | Missing both |
| `routingFallbackUnitCost` | > 0, clamped to min 1e-6 | Done |
| `proxyFirstByteTimeoutSec` | >= 0 integer | Done |
| `tokenRouterFailureCooldownMaxSec` | > 0 | Done |
| `proxyErrorKeywords` | string, string[], OR newline-separated | Missing newline parsing |
| `proxyDebugRetentionHours` | >= 1 | Done |
| `proxyDebugMaxBodyBytes` | >= 1024 | Done |
| `routingWeights` sub-fields | all non-negative | Not validated |
| `payloadRules` | `parsePayloadRulesConfigInput()` | Missing |
| `globalBlockedBrands` | array of strings + trigger route rebuild on change | Missing trigger |
| `globalAllowedModels` | array of strings + trigger route rebuild on change | Missing trigger |

### 2.5 Backup Import -- Missing `applyImportedSettingToRuntime()`

Both `POST /api/settings/backup/import` and `POST /api/settings/backup/webdav/import` must call `applyImportedSettingToRuntime()` for every imported setting key. This function handles 60+ setting keys with individual validation, scheduler restarts, cache invalidation, and background rebuilds. The Go stubs return success without any processing.

### 2.6 SSE Streaming -- Incomplete

`GET /api/update-center/tasks/:id/stream` sets correct SSE headers but:
- Does not use `reply.hijack()` (Fastify-specific; Go chi equivalent is `http.Hijacker` interface)
- Does not poll task status with `setInterval` (TS polls every 25ms)
- Does not handle client disconnect cleanup
- Does not check if task is already complete (TS sends existing logs then `done`)
- SSE event framing is present (`sseWrite` helper) but not shown in file

### 2.7 LDOH Reverse Proxy -- Not Implemented

The TS implementation is a full reverse proxy with:
- Cookie-based auth gating (`meta_monitor_auth` cookie == `config.authToken`)
- URL rewriting for HTML/JS/CSS/JSON content
- Location header rewriting for redirects
- Binary content pass-through
- `redirect: "manual"` for 3xx handling

Go stub returns 400 error.

### 2.8 Channel Deduplication -- Missing Source Model

`POST /api/routes/:id/channels/batch` and `POST /api/routes/:id/channels` deduplicate by `(accountId, tokenId)` only. The spec requires dedup by `(accountId, tokenId, sourceModel)`. The `sourceModel` field is INSERTed but not included in the dedup check.

### 2.9 Route Channel Population -- Missing

TS `POST /api/routes` (create) auto-populates channels for pattern routes via `populateRouteChannelsByModelPattern()` which merges from two sources:
1. Route channels from matching exact-model routes (higher priority)
2. Raw `token_model_availability` entries matching the pattern

Go create route does not populate any channels.

### 2.10 Decision Engine Integration -- Missing

All 5 route decision endpoints return empty stubs. The TS decision engine (`tokenRouter`) computes per-model/per-route decisions with channel selection, cost scoring, balance weighting, cooldown awareness, and snapshot persistence. None of this is wired.

---

## 3. Error Response Pattern Audit

The spec documents 4 distinct error response patterns:

| Pattern | Used By | Go Compliance |
|---------|---------|---------------|
| A: `{ success: false, message: "..." }` | Most endpoints (400/404/500) | **Mostly compliant** -- settings, downstream-keys, tokens use this pattern. |
| B: `{ error: "..." }` | PUT /api/checkin/schedule | **Compliant** -- checkin_routes.go line 91 uses `{ error: "..." }`. |
| C: `{ success: false, error: "..." }` | POST /api/models/check/:accountId | **Compliant** -- stats.go line 350. |
| D: `{ message: "..." }` | GET /api/stats/proxy-logs/:id | **Compliant** -- stats.go line 205, 216. |

**Specific mismatches found:**
- `events.go:57` uses `{ "error": "..." }` for DB query errors (should be Pattern A with 500)
- `events.go:66` uses `{ "error": "..." }` for MapScan errors (should be Pattern A with 500)
- `site_announcements.go:59` uses `{ "error": "..." }` for DB errors (should be Pattern A)
- `token_routes.go:343` returns 404 body `{ success: false, message: "路由不存在" }` for invalid route ID parsing (should check if route actually exists vs parsing error)

---

## 4. Structural Issues

### 4.1 `nowUTC()` Returns Empty String

`stats.go:394-397`: The `nowUTC()` function returns an empty string `""` with comment "resolved at compile time". This means `generatedAt` in the dashboard response is always empty. Use `time.Now().UTC().Format(time.RFC3339)`.

### 4.2 `strOrNull` Used Without nil Check

`token_routes.go:183`: `strOrNull(body.DisplayIcon)` is called even when `body.DisplayIcon` is nil, which returns nil correctly. OK.

### 4.3 `coalesceInt` Duplicate

`checkin_routes.go:107` defines `coalesceInt`. `update_center.go` also uses `coalesceStr` (defined at line 100+ of checkin_routes.go). Both are package-level but in different files -- they work due to same package but are scattered. Consider consolidating into a shared helper file.

### 4.4 `queryRows` Defined in `search.go`, Used Across Files

`queryRows` (line 129 of search.go) and `normalizeSlice` (line 147 of search.go) are used by stats.go, settings_database.go, downstream_keys.go, token_routes.go, checkin_routes.go, oauth_routes.go, site_announcements.go. They are in `search.go` which loads last alphabetically -- this works in Go (order-independent in same package) but is an organizational smell.

### 4.5 `getQueryInt`, `clampInt`, `maxInt` Defined in `search.go`

Also used across the package. Should be in a dedicated `helpers.go`.

---

## 5. Test Coverage

No test files found under `handler/admin/` for P11 endpoints. The existing test files in that directory (`sites_test.go`, `accounts_test.go`, `account_tokens_test.go`) cover P3-P5 endpoints. No tests exist for stats, settings, tokens, downstream_keys, events, checkin, update_center, monitor, site_announcements, search, tasks, test, oauth, or auth_settings.

---

## 6. Completeness Summary

| Module | Endpoints | Fully Done | Partial | Stub |
|--------|-----------|------------|---------|------|
| stats.go | 12 | 3 | 3 | 6 |
| settings.go | 4 | 2 | 1 | 1 |
| settings_database.go | 4 | 0 | 0 | 4 |
| settings_backup.go | 6 | 0 | 0 | 6 |
| settings_notify.go | 1 | 0 | 0 | 1 |
| settings_maintenance.go | 3 | 2 | 0 | 1 |
| downstream_keys.go | 9 | 3 | 6 | 0 |
| events.go | 5 | 5 | 0 | 0 |
| token_routes.go | 20 | 4 | 10 | 6 |
| checkin_routes.go | 4 | 1 | 0 | 3 |
| update_center.go | 6 | 0 | 0 | 6 |
| monitor.go | 5 | 1 | 1 | 3 |
| site_announcements.go | 5 | 4 | 0 | 1 |
| search.go | 1 | 1 | 0 | 0 |
| tasks.go | 2 | 0 | 0 | 2 |
| test.go | 8 | 0 | 0 | 8 |
| auth_settings.go | 2 | 2 | 0 | 0 |
| oauth_routes.go | 13 | 0 | 0 | 13 |
| **TOTAL** | **110** | **28** | **21** | **61** |

**56% of all endpoints are stubs. Only 25% are fully implemented.**

---

## 7. Priority-Graded Recommendations

### P0 -- Blocking for Production Use

1. **Rate limiting** on all 7 spec-mandated endpoints
2. **Settings PUT side-effect chain** (scheduler restarts, cache invalidation, self-lockout prevention)
3. **Settings PUT cross-field validation** (telegram, webhook, bark conditional validation)
4. **LDOH reverse proxy** implementation (cookie auth, URL rewriting, binary passthrough)

### P1 -- High Impact

5. **Dashboard cache headers** -- implement snapshot caching to return `hit`/`stale` instead of always `miss`
6. **Route channel deduplication** -- include `sourceModel` in dedup key
7. **Route create channel auto-population** -- `populateRouteChannelsByModelPattern()` equivalent
8. **Explicit group validation** -- source route existence, exact-model check, non-self-referencing
9. **Backup import side effects** -- `applyImportedSettingToRuntime()` equivalent
10. **Downstream keys policy reference validation** -- routes, sites, tokens existence checks
11. **Downstream keys update** -- handle all 14 fields, not just 3

### P2 -- Important for Feature Parity

12. **Marketplace with two-tier cache** (15s/90s) and refresh lifecycle
13. **Token candidates** with rate limiting, globalAllowedModels filter, marketplace cache reuse
14. **Route decision engine** wiring for all 5 decision endpoints
15. **Update center SSE streaming** with proper hijack, polling, cleanup
16. **Model probe** with async/sync modes
17. **Site distribution/trend** actual queries
18. **Database migration** cross-dialect data transfer
19. **Backup WebDAV** export/import with real WebDAV operations
20. **Notification test** with actual dispatch

### P3 -- Refinement

21. Settings PUT response should return full config (not just success/message)
22. Merge `nowUTC()` stub with real `time.Now().UTC()`
23. Consolidate helpers (`queryRows`, `getQueryInt`, `clampInt`, `maxInt`, `normalizeSlice`, `coalesceStr`, `coalesceInt`) into `helpers.go`
24. Add per-field event type heuristic in settings PUT
25. Add `model_mapping` serialization in route create
26. Populate `siteNames` in routes summary from route channels
27. Populate `sourceRouteIds` from `route_group_sources` table
28. Add `failureReason` classification to checkin logs
