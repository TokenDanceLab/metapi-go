# P11: Admin API -- Stats/Settings/Backup/DownstreamKeys/Events/Tokens/Monitor/Announcements/UpdateCenter/Checkin

**S.U.P.E.R**: S (单一职责) | **依赖**: P3-P8 | **Size**: XL

## 原始 TS 参考 (9 verified files + unverified modules)

Verified against TS source:
- `D:\Code\TokenDance\metapi\src\server\routes\api\stats.ts` -- 12 endpoints (2035 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\settings.ts` -- 18 endpoints (2069 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\tokens.ts` -- 20 endpoints (1509 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\downstreamApiKeys.ts` -- 9 endpoints (775 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\events.ts` -- 5 endpoints (61 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\checkin.ts` -- 4 endpoints (178 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\updateCenter.ts` -- 6 endpoints (324 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\monitor.ts` -- 5 endpoints (244 lines)
- `D:\Code\TokenDance\metapi\src\server\routes\api\siteAnnouncements.ts` -- 5 endpoints (197 lines)

Not verified (files not provided; implement from inferred requirements):
- `search.ts` (1 endpoint)
- `tasks.ts` (2 endpoints)
- `test.ts` (8 endpoints)
- `auth.ts` (2 endpoints)
- OAuth routes (13 endpoints, covered by P6 spec -- P11 delegates to P6)

**Total verified endpoints: 84** (plus 13 inferred + 13 OAuth delegation).

## Go 模块结构

```
handler/admin/
  stats.go                  # 统计端点 (dashboard/proxy-logs/proxy-debug/probe/marketplace/token-candidates)
  settings.go               # 设置端点 (runtime r/w + brand-list + system-proxy/test)
  settings_database.go      # 数据库迁移/测试连接/运行时配置
  settings_backup.go         # 备份导出/导入/WebDAV
  settings_notify.go        # 通知测试
  settings_maintenance.go   # 清缓存/清用量/工厂重置
  downstream_keys.go        # Downstream API key 管理 (CRUD + 用量 + 趋势 + 批量)
  events.go                 # 事件日志 (程序日志 CRUD)
  search.go                 # 搜索
  tasks.go                  # 后台任务状态
  test.go                   # 测试代理/chat 端点
  monitor.go                # 监控配置 + LDOH 代理
  site_announcements.go     # 站点公告
  auth_settings.go          # Auth 设置 (查看/修改 token)
  checkin_routes.go         # 签到路由 (手动触发/日志查询/调度设置)
  token_routes.go           # Token Routes + Channels CRUD (20 endpoints)
  update_center.go          # Update Center (版本检查/部署/回滚/SSE 流)
  oauth_routes.go           # OAuth 端点 (委托 P6 services)
```

---

## Endpoint Inventory

### Stats (12 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/stats/dashboard` | `refresh` (bool), `view` (summary\|insights\|full) | Dashboard snapshot. Defaults to `view=full` merging summary+insights. Sets `x-dashboard-summary-cache` and `x-dashboard-insights-cache` headers (values: `hit`/`miss`/`stale`). |
| GET | `/api/stats/proxy-logs` | `view` (query\|meta\|full), `limit` (1-100, default 50), `offset` (>=0, default 0), `status` (all\|success\|failed), `search` (string), `client` (app:xxx\|family:xxx), `siteId` (int>0), `from` (ISO datetime), `to` (ISO datetime) | Proxy logs list. **Three views served from one route via `?view=` query param**: `query` returns paginated items+total+page+pageSize; `meta` returns clientOptions+summary+sites; `full` (default) merges both. |
| GET | `/api/stats/proxy-logs/:id` | -- | Single proxy log detail. Returns 400 for non-numeric id, 404 if not found. Includes `billingDetails` (parsed JSON). |
| GET | `/api/stats/proxy-debug/traces` | `limit` (1-100, default 50) | Debug traces list. Returns `{ items: [...] }`. |
| GET | `/api/stats/proxy-debug/traces/:id` | -- | Debug trace detail. Returns 400 for non-numeric id, 404 if not found. |
| GET | `/api/models/marketplace` | `refresh` (bool), `includePricing` (bool) | Model marketplace. Two-tier cache: 15s base, 90s pricing. Cache keys: `"base"` and `"pricing"`. `refresh=true` clears cache and starts background rebuild task. Returns `{ models: [...], meta: { refreshRequested, refreshQueued, refreshReused, refreshRunning, refreshJobId, includePricing, cacheHit? } }`. |
| GET | `/api/models/token-candidates` | -- | Token candidate analysis. **Rate limited: 30 req/min**. Returns `{ models, modelsWithoutToken, modelsMissingTokenGroups, endpointTypesByModel }`. Applies `globalAllowedModels` whitelist filter. |
| POST | `/api/models/check/:accountId` | -- | Refresh models for single account + rebuild routes. Body: none. Returns `{ success: boolean, error?, refresh, rebuild }`. 400 for NaN accountId: `{ success: false, error: "Invalid account id" }`. |
| POST | `/api/models/probe` | -- | Model availability probe. Body: `{ accountId?: number, wait?: boolean }`. If `wait=true`: synchronous probe with completion waiting. If `wait=false` or omitted: async with 202 status. Returns `{ success, queued?, reused?, jobId, status, ...result }`. |
| GET | `/api/stats/site-distribution` | `days` (int, default 7), `refresh` (bool) | Site distribution (per-site aggregate). Returns `{ distribution: [...] }`. |
| GET | `/api/stats/site-trend` | `days` (int, default 7), `refresh` (bool) | Site trend (daily spend/calls by site). Returns `{ trend: [...] }`. |
| GET | `/api/stats/model-by-site` | `siteId` (optional int), `days` (int, default 7, min 1) | Model stats by site from `modelDayUsage` table. Runs `runUsageAggregationProjectionPass()` before query. Returns `{ models: [{ model, calls, spend, tokens }] }` sorted by calls desc. |

### Settings (18 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/settings/runtime` | -- | Full runtime config (60+ fields). Sensitive values masked (proxyToken, Telegram bot token, serverChan key, SMTP password). Includes `currentAdminIp` and `serverTimeZone`. |
| PUT | `/api/settings/runtime` | -- | Update runtime config. Body: partial object of 60+ possible fields. See **Runtime Settings Body Schema** below. Returns `{ success: true, message: "...", ...fullConfig }`. On change: persists to DB via `upsertSetting()`, fires `appendSettingsEvent()` with `changedLabels[]`. |
| GET | `/api/settings/brand-list` | -- | All known brand names. Returns `{ brands: string[] }`. |
| POST | `/api/settings/system-proxy/test` | -- | Test system proxy connectivity against `https://www.gstatic.com/generate_204` (15s timeout). Body: `{ proxyUrl?: string }` (falls back to `config.systemProxyUrl`). Returns `{ success: true, proxyUrl, reachable, ok, statusCode, latencyMs, probeUrl, finalUrl }` or 400/502 with error message. |
| GET | `/api/settings/database/runtime` | -- | Current and saved database config with masked connection strings. Returns `{ success: true, active: {...}, saved: {...}, restartRequired: boolean }`. |
| PUT | `/api/settings/database/runtime` | -- | Save database runtime config (takes effect after restart). Body: `{ dialect, connectionString, ssl?, overwrite? }`. Saved to `db_type`, `db_url`, `db_ssl` settings. Returns `{ success: true, message: "...", ...state }`. |
| POST | `/api/settings/database/test-connection` | -- | Test database connection. Body: `{ dialect, connectionString, ssl? }`. Returns `{ success: true, message: "...", ...result }`. |
| POST | `/api/settings/database/migrate` | -- | Cross-dialect data migration (SQLite -> PG). Body: same as test-connection. Returns `{ success: true, message: "...", dialect, rows: { sites, accounts, accountTokens, tokenRoutes, routeChannels, settings } }`. Fires audit event. |
| GET | `/api/settings/backup/export` | `type` (all\|accounts\|preferences, default all) | Export backup as JSON response. 400 for invalid type. |
| POST | `/api/settings/backup/import` | -- | Import backup. Body: `{ data: {...} }`. Calls `applyImportedSettingToRuntime()` for each imported setting key (60+ keys with individual validation + scheduler restarts). Reloads WebDAV scheduler if `backup_webdav_config_v1` is among imported settings. |
| GET | `/api/settings/backup/webdav` | -- | Get WebDAV backup config. Returns masked credentials. |
| PUT | `/api/settings/backup/webdav` | -- | Save WebDAV config. Body: `{ enabled?, fileUrl?, username?, password?, clearPassword?, exportType?, autoSyncEnabled?, autoSyncCron? }`. `clearPassword: true` explicitly clears the stored password. |
| POST | `/api/settings/backup/webdav/export` | -- | Export backup to WebDAV. Body: `{ type?: "all"\|"accounts"\|"preferences" }`. |
| POST | `/api/settings/backup/webdav/import` | -- | Import backup from WebDAV. Calls `applyImportedSettingToRuntime()` for each imported setting. Reloads WebDAV scheduler if config was imported. |
| POST | `/api/settings/notify/test` | -- | Send test notification via all enabled channels. Options: `{ bypassThrottle: true, requireChannel: true, throwOnFailure: true }`. Returns `{ success: true, message: "测试通知已发送（成功 N/M）" }`. |
| POST | `/api/settings/maintenance/clear-cache` | -- | **Destructive**: Deletes ALL rows from `modelAvailability`, `routeChannels`, AND `tokenRoutes`. Then triggers background `refreshModelsAndRebuildRoutes`. Returns 202 with `{ success, queued, reused, jobId, message, deletedModelAvailability, deletedRouteChannels, deletedTokenRoutes }`. |
| POST | `/api/settings/maintenance/clear-usage` | -- | Deletes ALL proxy logs. Resets ALL route channel stats to 0 (successCount, failCount, totalLatencyMs, totalCost, lastUsedAt, lastSelectedAt, lastFailAt, consecutiveFailCount, cooldownLevel, cooldownUntil). Resets ALL account `balanceUsed` to 0. Fires audit event (level: warning). Returns `{ success, message, deletedProxyLogs }`. |
| POST | `/api/settings/maintenance/factory-reset` | -- | Complete database wipe via `performFactoryReset()`. Returns `{ success: true }` or 500. |

### Token Routes + Channels (20 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/routes/lite` | -- | Lightweight route list for selectors. Returns `[{ id, modelPattern, displayName, displayIcon, routeMode, sourceRouteIds, routingStrategy, enabled }]`. |
| GET | `/api/routes/summary` | -- | Route summary for first-screen rendering. **Rate limited: 60 req/min**. Returns `[{ id, modelPattern, displayName, displayIcon, routeMode, sourceRouteIds, modelMapping, routingStrategy, enabled, channelCount, enabledChannelCount, siteNames, decisionSnapshot, decisionRefreshedAt }]`. Rate limit exceeded: 429 with `retry-after` header, body `{ success: false, message: "请求过于频繁，请稍后再试" }`. |
| GET | `/api/routes` | -- | Full route list with channels. **Rate limited: 60 req/min**. Returns `[{ ...route, decisionSnapshot, decisionRefreshedAt, channels: [...] }]`. |
| GET | `/api/routes/:id/channels` | -- | Channels for a single route. Returns 404 if route not found. |
| POST | `/api/routes/:id/cooldown/clear` | -- | Clear cooldown for route. Returns 404 if route not found. |
| POST | `/api/routes/:id/channels/batch` | -- | Batch add channels to route. Body: `{ channels: [{ accountId, tokenId?, sourceModel? }] }`. Deduplicates by `(accountId, tokenId, sourceModel)`. Sets `manualOverride: true`. Returns `{ success, created, skipped, errors }`. Cannot use on explicit_group routes. |
| POST | `/api/routes/:id/channels` | -- | Add single channel. Body: `{ accountId, tokenId?, sourceModel?, priority?, weight? }`. Validates token belongs to account, validates model support for exact-model routes, deduplicates (returns 400 if duplicate). Sets `manualOverride: false` by default. Clears decision snapshot + invalidates caches on success. |
| GET | `/api/routes/decision` | `model` (required) | Route decision explanation for a model. Returns `{ success: true, decision: {...} }`. 400 if model missing. |
| POST | `/api/routes/decision/batch` | -- | Batch route decisions by model list. Body: `{ models: string[], refreshPricingCatalog?: boolean, persistSnapshots?: boolean }`. Returns `{ success, decisions: { [model]: decision } }`. Max 500 models. |
| POST | `/api/routes/decision/by-route/batch` | -- | Batch per-route decisions. Body: `{ items: [{ routeId, model }], refreshPricingCatalog?, persistSnapshots? }`. Returns `{ success, decisions: { [routeId]: { [model]: decision } } }`. Max 500 items. |
| POST | `/api/routes/decision/route-wide/batch` | -- | Batch route-wide decisions. Body: `{ routeIds: number[], refreshPricingCatalog?, persistSnapshots? }`. Returns `{ success, decisions: { [routeId]: decision } }`. Max 500 route ids. |
| POST | `/api/routes/decision/refresh` | -- | Trigger background refresh of all route decision snapshots. Returns 202 with `{ success, queued, reused, jobId, status, message }`. |
| POST | `/api/routes` | -- | Create route. Body: `{ modelPattern, routeMode?, displayName?, displayIcon?, sourceRouteIds?, routingStrategy?, modelMapping?, enabled? }`. See **Route create/update body schema**. For explicit_group: `modelPattern` is set from `displayName`, requires sourceRouteIds validation. For pattern: auto-populates channels via `populateRouteChannelsByModelPattern()`. |
| PUT | `/api/routes/:id` | -- | Update route. Body: partial object. 404 if not found. Cannot switch routeMode (400). On modelPattern change for pattern routes: rebuilds automatic channels. On behavior change: clears decision snapshots. Invalidates token router cache. |
| DELETE | `/api/routes/:id` | -- | Delete route. Clears dependent explicit_group snapshots. Invalidates cache. Returns `{ success: true }`. |
| POST | `/api/routes/batch` | -- | Batch enable/disable routes. Body: `{ action: "enable"\|"disable", ids: number[] }`. Max 500 ids. Clears decisions + dependent group snapshots for affected routes. Returns `{ success, updatedCount }`. |
| POST | `/api/routes/rebuild` | -- | Rebuild routes/channels. Body: `{ refreshModels?: boolean, wait?: boolean }`. If `refreshModels=false`: rebuilds routes only (sync). If `wait=true`: synchronous model refresh + rebuild. Default: async background task, returns 202. |
| PUT | `/api/channels/batch` | -- | Batch update channel priorities. Body: `{ updates: [{ id, priority }] }`. Validates all channel IDs exist (404 on missing). Sets `manualOverride: true`. Clears decisions for affected routes. Returns `{ success, channels }`. |
| PUT | `/api/channels/:channelId` | -- | Update single channel. Body: `{ tokenId?, sourceModel?, priority?, weight?, enabled? }`. Validates token ownership + model support. Sets `manualOverride: true`. Returns updated channel. 404 if channel or route missing. |
| DELETE | `/api/channels/:channelId` | -- | Delete single channel. Clears decision for parent route. Returns `{ success: true }`. |

### Downstream API Keys (9 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/downstream-keys/summary` | `range` (24h\|7d\|all, default 24h), `status` (all\|enabled\|disabled), `search` (max 80 chars), `group` (max 64 chars, or `__ungrouped__`), `tags` (comma/pipe separated, max 20, each max 32 chars), `tagMatch` (any\|all, default any) | Summary with usage data. Checks `hasProxyLogDownstreamApiKeyIdColumn()` before querying usage (gracefully returns null usage if column missing). Returns `{ success, range, status, search, group, tags, tagMatch, items: [{ ...key, rangeUsage: { totalRequests, successRequests, failedRequests, successRate, totalTokens, totalCost } }] }`. |
| GET | `/api/downstream-keys/:id/overview` | -- | Single key detail with 3-range usage (last24h, last7d, all). Column-ready gating. Returns `{ success, item, usage: { last24h, last7d, all } }`. 404 if not found. |
| GET | `/api/downstream-keys/:id/trend` | `range` (24h\|7d\|all), `timeZone` (IANA string) | Trend buckets for a key. Column-ready gating. Returns `{ success, range, item: { id, name }, bucketSeconds, timeZone, buckets }`. 404 if key not found. |
| GET | `/api/downstream-keys` | -- | List all keys (full detail via `listDownstreamApiKeys()`). Returns `{ success, items }`. |
| POST | `/api/downstream-keys` | -- | Create key. Body validated via Zod. Fields: `name` (required), `key` (required, must start with `sk-` and >=6 chars), `description`, `groupName`, `tags`, `enabled`, `expiresAt`, `maxCost`, `maxRequests`, `supportedModels`, `allowedRouteIds`, `siteWeightMultipliers`, `excludedSiteIds`, `excludedCredentialRefs`. Validates policy references (routes, sites, tokens must exist). Uses `usedCost`=0, `usedRequests`=0. Returns 409 on unique key conflict, 400 on validation failure, 500 on other errors. Returns `{ success, item }`. |
| PUT | `/api/downstream-keys/:id` | -- | Update key. Same validation as create. Only updates fields present in body; missing fields retain existing values. 404 if not found, 409 on key conflict. Returns `{ success, item }`. |
| POST | `/api/downstream-keys/:id/reset-usage` | -- | Reset usedCost and usedRequests to 0. Returns `{ success, item }`. |
| DELETE | `/api/downstream-keys/:id` | -- | Delete key. Returns `{ success: true }`. |
| POST | `/api/downstream-keys/batch` | -- | Batch operations. Body: `{ ids: number[], action: "enable"\|"disable"\|"delete"\|"resetUsage"\|"updateMetadata", groupName?, groupOperation?, tags?, tagOperation? }`. Max 500 ids. `updateMetadata` action: `groupOperation` can be `keep`\|`set`\|`clear`, `tagOperation` can be `keep`\|`append`. Returns `{ success, successIds, failedItems }`. |

### Events (5 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/events` | `limit` (1-500, default 30), `offset` (>=0, default 0), `type` (optional filter), `read` (true\|false, optional bool filter) | List events ordered by createdAt desc. |
| GET | `/api/events/count` | -- | Unread event count. Returns `{ count: number }`. |
| POST | `/api/events/:id/read` | -- | Mark single event as read. Returns `{ success: true }`. |
| POST | `/api/events/read-all` | -- | Mark all unread events as read. Returns `{ success: true }`. |
| DELETE | `/api/events` | -- | Delete ALL events. Returns `{ success: true }`. |

### Checkin (4 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| POST | `/api/checkin/trigger` | -- | Trigger checkin for all accounts. Runs via background task (dedupeKey: `checkin-all`). Returns 202 with `{ success, queued, reused, jobId, status, message }`. |
| POST | `/api/checkin/trigger/:id` | -- | Trigger checkin for single account (synchronous). Returns result object from `checkinAccount()`. |
| GET | `/api/checkin/logs` | `limit` (default 50), `offset` (default 0), `accountId` (optional filter) | Checkin logs with account+site join. Adds `failureReason` via `classifyFailureReason()`. |
| PUT | `/api/checkin/schedule` | -- | Update checkin schedule. Body: `{ mode?: "cron"\|"interval", cron?: string, intervalHours?: number }`. Persists to settings. Error responses use `{ error: "..." }` format (no `success` field). |

### Update Center (6 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/update-center/status` | -- | Current update center status (cached). |
| POST | `/api/update-center/check` | -- | Refresh version check cache. Returns `status` object. |
| PUT | `/api/update-center/config` | -- | Save update center config. Body: `{ enabled?, helperBaseUrl?, namespace?, releaseName?, chartRef?, imageRepository?, defaultDeploySource? }`. Returns `{ success, config }`. |
| POST | `/api/update-center/deploy` | -- | Deploy update. Body: `{ source?: "docker-hub-tag"\|"github-release", targetTag, targetDigest?, targetVersion? }`. Requires config validation (enabled, helperBaseUrl, namespace, releaseName, chartRef, imageRepository, DEPLOY_HELPER_TOKEN). Runs deploy guard check. Spawns background task (dedupeKey: `update-center-deploy`). Returns 202 with `{ success, reused, task }`. 409 if deploy blocked. |
| POST | `/api/update-center/rollback` | -- | Rollback update. Body: `{ targetRevision }`. Requires config validation. Spawns background task. Returns 202 with `{ success, reused, task }`. |
| GET | `/api/update-center/tasks/:id/stream` | -- | **SSE stream** for task logs. Content-Type: `text/event-stream; charset=utf-8`. Headers: `Cache-Control: no-cache, no-transform`, `Connection: keep-alive`. Events: `log` (with entry data), `done` (with status). Uses `reply.hijack()` on the raw response. Polls task status every 25ms. Cleans up on client disconnect. |

### Monitor (5 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/monitor/config` | -- | Get LDOH cookie config. **Rate limited: 30 req/min**. Returns `{ ldohCookieConfigured: boolean, ldohCookieMasked: string }`. |
| PUT | `/api/monitor/config` | -- | Save LDOH cookie. **Rate limited: 10 req/min**. Body: `{ ldohCookie: string }`. Validates cookie starts with `ld_auth_session=` and >=24 chars. Returns `{ success, message, ldohCookieConfigured, ldohCookieMasked }`. |
| POST | `/api/monitor/session` | -- | Create monitor proxy session. **Rate limited: 10 req/min**. Sets `meta_monitor_auth` HttpOnly cookie (SameSite=Lax, Max-Age=7200) with `config.authToken`. Returns `{ success: true }`. |
| ALL | `/monitor-proxy/ldoh` | -- | **Rate limited: 60 req/min**. LDOH reverse proxy. Requires `meta_monitor_auth` cookie = `config.authToken` (401 otherwise). Requires LDOH cookie configured (400 otherwise). Rewrites: HTML/JS/CSS/JSON content (URLs rewritten from `https://ldoh.105117.xyz/` to `/monitor-proxy/ldoh/`), Location headers. Passes through binary content as buffer. `redirect: "manual"` to handle 3xx. |
| ALL | `/monitor-proxy/ldoh/*` | -- | Same as above, wildcard path forwarded to LDOH. |

### Site Announcements (5 endpoints)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| GET | `/api/site-announcements` | `limit` (1-500, default 50), `offset` (>=0, default 0), `siteId` (optional int filter), `platform` (optional string filter), `read` (true\|false), `status` (active\|expired\|dismissed) | List announcements. Filters: read (by readAt presence), status (active=not dismissed + not expired, expired=not dismissed + endAt < now, dismissed=has dismissedAt). Date fields localized to server timezone. Applies offset/limit AFTER read+status filters. |
| POST | `/api/site-announcements/:id/read` | -- | Mark one announcement as read (sets readAt to now). Returns `{ success: true }`. |
| POST | `/api/site-announcements/read-all` | -- | Mark ALL announcements as read. Returns `{ success: true }`. |
| DELETE | `/api/site-announcements` | -- | Delete ALL announcements. Returns `{ success: true }`. |
| POST | `/api/site-announcements/sync` | -- | Trigger site announcement sync. Body: `{ siteId?: number }`. Runs background task (dedupeKey: `site-announcements:{siteId}` or `site-announcements:all`). No success/failure notifications. Returns `{ success, queued, reused, taskId }`. |

### Unverified Modules

These files were listed in the original spec but NOT provided for review. Implement from inferred API shape; mark as TBD in implementation plan.

| Module | Claimed Endpoints | Notes |
|--------|-------------------|-------|
| `search.ts` | 1 | Admin search endpoint |
| `tasks.ts` | 2 | Background task status listing/viewing |
| `test.ts` | 8 | Proxy test + chat test endpoints |
| `auth.ts` | 2 | Auth settings (view token, change token) |
| OAuth routes | 13 | **Delegated to P6 spec**. P11 only wires routes; P6 provides implementation. |

---

## Response Schemas

### Error Response Patterns (3 patterns used in TS)

**Pattern A -- `{ success: false, message: "..." }`** (most endpoints):
Used by settings, downstream-keys, tokens, stats probe, etc. for validation errors (400), not-found (404), and operational errors (500).

**Pattern B -- `{ error: "..." }`** (checkin schedule):
Only `PUT /api/checkin/schedule` uses `{ error: err.message }` without `success` field on failure.

**Pattern C -- `{ success: false, error: "..." }`** (model check):
Only `POST /api/models/check/:accountId` uses `error` instead of `message` for the invalid account ID case.

**Pattern D -- `{ message: "..." }`** (proxy log detail):
`GET /api/stats/proxy-logs/:id` uses `{ message: "..." }` for 400 (invalid id) and 404 (not found) without `success` field.

### Dashboard Response

```
GET /api/stats/dashboard?view=full
Response headers:
  x-dashboard-summary-cache: hit|miss|stale
  x-dashboard-insights-cache: hit|miss|stale

Body:
{
  generatedAt: string (ISO datetime),
  // summary fields (from getDashboardSummarySnapshot)
  // insights fields (from getDashboardInsightsSnapshot)
  // When view=summary: only summary fields
  // When view=insights: only insights fields
}
```

### Proxy Logs List Response

```
GET /api/stats/proxy-logs?view=full
Body (view=query):
{
  items: [{
    id, createdAt, modelRequested, modelActual, status, latencyMs,
    totalTokens, estimatedCost, isStream, firstByteLatencyMs,
    clientFamily, clientAppId, clientAppName, clientConfidence,
    usageSource: "upstream"|"self-log"|"unknown"|null,
    username, siteId, siteName, siteUrl,
    downstreamKeyId, downstreamKeyName, downstreamKeyGroupName, downstreamKeyTags,
    // plus all raw proxy_logs columns
  }],
  total: number,
  page: number,  // Math.floor(offset/limit)+1
  pageSize: number
}

Body (view=meta):
{
  clientOptions: [{ value: string, label: string }],
  summary: {
    totalCount, successCount, failedCount,
    totalCost (rounded to 6 decimal places),
    totalTokensAll
  },
  sites: [{ id, name, status }]
}

Body (view=full): merged query + meta (items, total, page, pageSize, clientOptions, summary, sites)
```

### Marketplace Response

```
GET /api/models/marketplace
Body:
{
  models: [{
    name, accountCount, tokenCount, avgLatency (ms, rounded),
    successRate (percent, null if no data),
    description, tags, supportedEndpointTypes,
    pricingSources: [{ siteId, siteName, accountId, username, ownerBy, enableGroups, groupPricing }],
    accounts: [{ id, site, username, latency, unitCost, balance, tokens: [{ id, name, isDefault }] }]
  }],
  meta: {
    refreshRequested, refreshQueued, refreshReused,
    refreshRunning, refreshJobId, includePricing,
    cacheHit?: boolean  // only present on cache hit
  }
}
```

### Token Candidates Response

```
GET /api/models/token-candidates
Body:
{
  models: {
    [modelName]: [{
      accountId, tokenId, tokenName, isDefault, username, siteId, siteName
    }]
  },
  modelsWithoutToken: {
    [modelName]: [{ accountId, username, siteId, siteName }]
  },
  modelsMissingTokenGroups: {
    [modelName]: [{
      accountId, username, siteId, siteName,
      missingGroups, requiredGroups, availableGroups,
      groupCoverageUncertain?: boolean
    }]
  },
  endpointTypesByModel: { [modelName]: string[] }
}
```

### Runtime Settings Response

```
GET /api/settings/runtime
Body (60+ fields):
{
  // Checkin
  checkinCron, checkinScheduleMode, checkinIntervalHours,
  // Balance
  balanceRefreshCron,
  // Log cleanup
  logCleanupCron, logCleanupUsageLogsEnabled, logCleanupProgramLogsEnabled,
  logCleanupRetentionDays,
  // Model probe
  modelAvailabilityProbeEnabled,
  // Codex
  codexUpstreamWebsocketEnabled,
  // Responses
  responsesCompactFallbackToResponsesEnabled,
  // Cross-protocol
  disableCrossProtocolFallback,
  // Proxy session
  proxySessionChannelConcurrencyLimit, proxySessionChannelQueueWaitMs,
  // Debug trace
  proxyDebugTraceEnabled, proxyDebugCaptureHeaders, proxyDebugCaptureBodies,
  proxyDebugCaptureStreamChunks, proxyDebugTargetSessionId,
  proxyDebugTargetClientKind, proxyDebugTargetModel,
  proxyDebugRetentionHours, proxyDebugMaxBodyBytes,
  // Routing
  routingFallbackUnitCost, proxyFirstByteTimeoutSec,
  tokenRouterFailureCooldownMaxSec,
  routingWeights: { baseWeightFactor, valueScoreFactor, costWeight, balanceWeight, usageWeight },
  // Notify: Webhook
  webhookUrl, webhookEnabled,
  // Notify: Bark
  barkUrl, barkEnabled,
  // Notify: ServerChan
  serverChanEnabled, serverChanKeyMasked,
  // Notify: Telegram
  telegramEnabled, telegramApiBaseUrl, telegramBotTokenMasked,
  telegramChatId, telegramUseSystemProxy, telegramMessageThreadId,
  // Notify: SMTP
  smtpEnabled, smtpHost, smtpPort, smtpSecure, smtpUser,
  smtpPassMasked, smtpFrom, smtpTo,
  // Notify: cooldown
  notifyCooldownSec,
  // Admin
  adminIpAllowlist, currentAdminIp, serverTimeZone,
  // System
  systemProxyUrl, proxyTokenMasked,
  // Proxy
  payloadRules, proxyErrorKeywords, proxyEmptyContentFailEnabled,
  // Global filters
  globalBlockedBrands, globalAllowedModels
}
```

### Token Routes Summary Response

```
GET /api/routes/summary
Body: [{
  id, modelPattern, displayName, displayIcon,
  routeMode, sourceRouteIds, modelMapping,
  routingStrategy, enabled,
  channelCount, enabledChannelCount, siteNames,
  decisionSnapshot,     // parsed from stored JSON
  decisionRefreshedAt   // ISO datetime or null
}]
```

### Downstream Keys Summary Response

```
GET /api/downstream-keys/summary
Body:
{
  success: true,
  range, status, search, group, tags, tagMatch,
  items: [{
    // all key fields from toDownstreamApiKeyPolicyView()
    id, name, keyMasked, description, groupName, tags,
    enabled, expiresAt, maxCost, usedCost, maxRequests, usedRequests,
    supportedModels, allowedRouteIds, siteWeightMultipliers,
    excludedSiteIds, excludedCredentialRefs,
    createdAt, updatedAt,
    rangeUsage: {
      totalRequests, successRequests, failedRequests,
      successRate (percent, 1 decimal, null if 0 requests),
      totalTokens,
      totalCost (rounded to 6 decimal places)
    }
  }]
}
```

### Site Announcements Row Schema

```
{
  id, siteId, platform, title, content, url, severity,
  firstSeenAt (localized datetime string or null),
  lastSeenAt (localized datetime string or null),
  readAt (localized datetime string or null),
  dismissedAt (localized datetime string or null),
  // ... other announcement fields
}
// read filter: hasDateTimeValue(readAt) === true means "read"
// status filter:
//   "active"   = !dismissedAt && (endsAt == null || endsAt >= now)
//   "expired"  = !dismissedAt && (endsAt != null && endsAt < now)
//   "dismissed" = hasDateTimeValue(dismissedAt) === true
```

---

## Request Body Schemas

### PUT /api/settings/runtime

All fields optional. Only present fields are updated; absent fields retain current value.

**Validation rules by field:**

| Field | Type | Validation |
|-------|------|------------|
| `proxyToken` | string | Must start with `sk-`, length >= 6 |
| `systemProxyUrl` | string | Must be valid http/https/socks proxy URL (validated by `normalizeSiteProxyUrl`) |
| `checkinCron` | string | Must be valid cron expression (`cron.validate()`) |
| `checkinScheduleMode` | `"cron"`\|`"interval"` | Literal check |
| `checkinIntervalHours` | number | Integer 1-24 |
| `balanceRefreshCron` | string | Valid cron expression |
| `logCleanupCron` | string | Valid cron expression |
| `logCleanupRetentionDays` | number | Integer >= 1, normalized by `normalizeLogCleanupRetentionDays()` |
| `webhookUrl` | string | If webhookEnabled is true (or becomes true), must be non-empty AND valid http/https URL |
| `barkUrl` | string | If barkEnabled is true (or becomes true), must be non-empty AND valid http/https URL |
| `telegramEnabled` | boolean | If true (or becomes true): botToken required (non-empty), must contain `:`; chatId required (non-empty); messageThreadId must match `/^[1-9]\d*$/` if non-empty; apiBaseUrl must be valid http/https if non-empty |
| `telegramBotToken` | string | Must contain `:` if telegram is enabled |
| `telegramChatId` | string | Non-empty if telegram enabled |
| `telegramApiBaseUrl` | string | Valid http/https URL (defaults to `https://api.telegram.org`) |
| `telegramMessageThreadId` | string | Must match `/^[1-9]\d*$/` if non-empty |
| `smtpPort` | number | Positive integer |
| `notifyCooldownSec` | number | >= 0 |
| `adminIpAllowlist` | string[] or comma-separated string | Each entry must be valid IP or CIDR. Current request IP must be in new whitelist (self-lockout prevention). |
| `proxySessionChannelConcurrencyLimit` | number | >= 0 integer |
| `proxySessionChannelQueueWaitMs` | number | >= 0 integer |
| `proxyDebugRetentionHours` | number | >= 1 integer |
| `proxyDebugMaxBodyBytes` | number | >= 1024 integer |
| `proxyErrorKeywords` | string\|string[] | Newline/comma-separated or array of strings |
| `routingFallbackUnitCost` | number | > 0, clamped to minimum 1e-6 |
| `proxyFirstByteTimeoutSec` | number | >= 0 integer |
| `tokenRouterFailureCooldownMaxSec` | number | > 0 |
| `routingWeights` | object | `{ baseWeightFactor?, valueScoreFactor?, costWeight?, balanceWeight?, usageWeight? }` -- all non-negative numbers |
| `globalBlockedBrands` | string[] | Array of strings. If changed: triggers background route rebuild. |
| `globalAllowedModels` | string[] | Array of strings. If changed: triggers background route rebuild. |
| `payloadRules` | object | Validated by `parsePayloadRulesConfigInput()`. Returns `{ success: false, message }` on invalid. |
| Boolean flags | boolean | `modelAvailabilityProbeEnabled`, `codexUpstreamWebsocketEnabled`, `responsesCompactFallbackToResponsesEnabled`, `disableCrossProtocolFallback`, `proxyDebugTraceEnabled`, `proxyDebugCaptureHeaders`, `proxyDebugCaptureBodies`, `proxyDebugCaptureStreamChunks`, `proxyEmptyContentFailEnabled`, `webhookEnabled`, `barkEnabled`, `serverChanEnabled`, `telegramEnabled`, `telegramUseSystemProxy`, `smtpEnabled`, `smtpSecure` |

**Side effects on PUT:**
- `changedLabels: string[]` built from every field that actually changed
- Persisted to DB via `upsertSetting(key, value)` for each changed field
- `appendSettingsEvent({ type, title, message })` fired with all changed labels joined by commas
- Event `type` heuristic: single-field `checkinCron` change -> `"checkin"`, `balanceRefreshCron` -> `"balance"`, `proxyToken` -> `"proxy"`, otherwise -> `"status"`
- Scheduler updates triggered: `updateCheckinSchedule()`, `updateBalanceRefreshCron()`, `updateLogCleanupSettings()`
- `stopProxyLogRetentionService()` called on log cleanup settings change
- `startModelAvailabilityProbeScheduler()` / `stopModelAvailabilityProbeScheduler()` on probe toggle
- `invalidateSiteProxyCache()` on systemProxyUrl change
- Background route rebuild on `globalBlockedBrands` or `globalAllowedModels` change

### POST /api/routes (Create) / PUT /api/routes/:id (Update)

| Field | Type | Create Required | Notes |
|-------|------|-----------------|-------|
| `modelPattern` | string | Yes (for pattern mode) | Model matching pattern. `explicit_group` mode sets from `displayName`. |
| `routeMode` | string | No (defaults to `"pattern"`) | `"pattern"` or `"explicit_group"`. Cannot switch between modes on update. |
| `displayName` | string | Yes (for explicit_group) | Display name; in explicit_group mode, becomes the `modelPattern` |
| `displayIcon` | string | No | |
| `sourceRouteIds` | number[] | Yes (for explicit_group) | Source route IDs for explicit group |
| `routingStrategy` | string | No | Defaults to default strategy |
| `modelMapping` | any | No | |
| `enabled` | boolean | No | Defaults to `true` |

**explicit_group validation rules:**
- `sourceRouteIds` must be non-empty
- All source route IDs must exist in `tokenRoutes`
- Cannot reference the group itself as a source
- Source routes must be exact-model routes (`isExactModelPattern()` returns true -- no `*` or `?`)
- Source routes must NOT themselves be explicit_group routes
- On create: `syncExplicitGroupSourceRouteStrategies()` propagates strategy to source routes that share the default strategy or the target strategy
- On create/update: clears dependent group decision snapshots for source routes
- Creating/updating a group clears the group's own decision snapshot

**Channel population for pattern routes:**
- On create: `populateRouteChannelsByModelPattern()` pulls from TWO sources:
  1. Existing route channels from matching exact-model routes (higher priority)
  2. Raw token_model_availability entries matching the pattern
- On update with modelPattern change: `rebuildAutomaticRouteChannelsByModelPattern()` removes non-manual-override channels and repopulates
- Deduplication: `(accountId, tokenId, sourceModel)` tuple uniqueness enforced

### POST/PUT /api/downstream-keys

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `name` | string | Yes | Cannot be empty |
| `key` | string | Yes | Must start with `sk-`, length >= 6 |
| `description` | string | No | |
| `groupName` | string | No | |
| `tags` | string[] | No | |
| `enabled` | boolean | No | Default true |
| `expiresAt` | string | No | |
| `maxCost` | number | No | |
| `maxRequests` | number | No | |
| `supportedModels` | string[] | No | |
| `allowedRouteIds` | number[] | No | Validated: all must exist in tokenRoutes |
| `siteWeightMultipliers` | Record<number, number> | No | Keys validated: all siteIds must exist |
| `excludedSiteIds` | number[] | No | Validated: all must exist |
| `excludedCredentialRefs` | object[] | No | Validated: account_token refs must match account+site; default_api_key refs must match site and have apiToken |

**Policy reference validation:**
- `allowedRouteIds`: checks existence in `tokenRoutes`
- `siteWeightMultipliers` keys + `excludedSiteIds`: checks existence in `sites`
- `excludedCredentialRefs` of kind `account_token`: validates token exists, belongs to correct account+site
- `excludedCredentialRefs` of kind `default_api_key`: validates account exists, site matches, account has non-empty `apiToken`

### POST /api/settings/backup/webdav/export

```
Body: { type?: "all" | "accounts" | "preferences" }
// Default: no type (service handles undefined)
```

### POST /api/checkin/schedule

```
Body: {
  mode?: "cron" | "interval",   // default "cron"
  cron?: string,                 // cron expression
  intervalHours?: number         // interval in hours
}
```

---

## Cross-Cutting Behaviors

### Cache Headers

Two endpoints set custom cache status headers consumed by the frontend:
- `GET /api/stats/dashboard`: sets `x-dashboard-summary-cache` and `x-dashboard-insights-cache` with values `hit`, `miss`, or `stale`
- These come from the snapshot services; the Go implementation must replicate the cache status enum

### Rate Limiting

| Endpoint | Limit | Window | Error Response |
|----------|-------|--------|----------------|
| `GET /api/models/token-candidates` | 30 req/min | 60s | -- (preHandler blocks with 429) |
| `GET /api/routes/summary` | 60 req/min | 60s | 429 + `retry-after` header + `{ success: false, message: "请求过于频繁，请稍后再试" }` |
| `GET /api/routes` | 60 req/min | 60s | Same as above |
| `GET /api/monitor/config` | 30 req/min | 60s | preHandler rate limit |
| `PUT /api/monitor/config` | 10 req/min | 60s | preHandler rate limit |
| `POST /api/monitor/session` | 10 req/min | 60s | preHandler rate limit |
| ALL `/monitor-proxy/ldoh*` | 60 req/min | 60s | preHandler rate limit |

### Column-Readiness Gating

`hasProxyLogDownstreamApiKeyIdColumn()` is checked before querying `proxyLogs.downstreamApiKeyId` in:
- `GET /api/downstream-keys/summary` -- usage data returns empty if column missing
- `GET /api/downstream-keys/:id/overview` -- returns `{ last24h: null, last7d: null, all: null }`
- `GET /api/downstream-keys/:id/trend` -- returns empty trend buckets

This is a schema migration compatibility check; Go must implement equivalent.

### Client Detection for Proxy Logs

`resolveProxyLogClientMeta()` (stats.ts line 313) has a two-tier fallback:
1. **Primary**: reads dedicated `clientFamily`, `clientAppId`, `clientAppName`, `clientConfidence` columns from proxy_logs
2. **Fallback**: parses `errorMessage` for legacy `usageSource` metadata via `parseProxyLogMessageMeta()` -- only used when ALL primary columns are null/empty

### Marketplace Cache

- Two-tier TTL: base data = 15 seconds, pricing data = 90 seconds
- Cache keys: `"base"` and `"pricing"`
- `refresh=true` clears ALL cache and starts background model refresh + route rebuild
- Cache hit: response includes `meta.cacheHit: true`
- Token candidates endpoint reuses marketplace cache for `endpointTypesByModel` (tries pricing cache first, falls back to base cache)

### Proxy Cost Estimation Fallback

`proxyCostSqlExpression()` implements a fallback cost formula:
- When `estimatedCost` is null: Veloera platform uses `totalTokens / 1,000,000`, other platforms use `totalTokens / 500,000`
- Applied at SQL level via `COALESCE(estimatedCost, CASE ...)`

### Settings Audit Trail

`appendSettingsEvent()` creates entries in the `events` table:
- Type: `"checkin"` | `"balance"` | `"proxy"` | `"status"` | `"token"`
- `relatedType: "settings"`
- `level`: `"info"` (default) | `"warning"` | `"error"`
- Caught silently (try/catch with empty catch) -- event write failures do NOT fail the request

### Backup Import Side Effects

Both `POST /api/settings/backup/import` and `POST /api/settings/backup/webdav/import` call `applyImportedSettingToRuntime()` for EVERY imported setting key. This function (settings.ts lines 335-712) handles 60+ setting keys individually, with:
- Type + value validation per key
- Scheduler restarts (checkin, balance, log cleanup, model probe)
- Cache invalidation (site proxy)
- Background route rebuilds (brand filter, model whitelist changes)
- Setting keys that are silently skipped: `post_refresh_probe_enabled`, `post_refresh_probe_model`, `post_refresh_probe_scope`

**Critical**: The `performFactoryReset()` function is called directly via `performFactoryReset()` service; its exact behavior needs to be verified in the factoryResetService source (not provided in this review scope).

### Clear Cache Operation Scope

`POST /api/settings/maintenance/clear-cache` is extremely destructive:
- Deletes ALL `modelAvailability` rows
- Deletes ALL `routeChannels` rows
- Deletes ALL `tokenRoutes` rows
- Then triggers background `refreshModelsAndRebuildRoutes()` to rebuild everything from scratch

This is NOT just a cache clear -- it wipes the entire routing infrastructure.

### Clear Usage Operation Scope

`POST /api/settings/maintenance/clear-usage`:
- Deletes ALL `proxyLogs` rows
- Resets ALL `routeChannels` stats: successCount=0, failCount=0, totalLatencyMs=0, totalCost=0, lastUsedAt=null, lastSelectedAt=null, lastFailAt=null, consecutiveFailCount=0, cooldownLevel=0, cooldownUntil=null
- Resets ALL `accounts.balanceUsed` to 0
- Fires audit event with level `"warning"`

### SSE Streaming (Update Center)

`GET /api/update-center/tasks/:id/stream` uses Server-Sent Events:
- Content-Type: `text/event-stream; charset=utf-8`
- Headers: `Cache-Control: no-cache, no-transform`, `Connection: keep-alive`
- Uses `reply.hijack()` on the Fastify raw response
- Event framing: `event: {type}\ndata: {JSON}\n\n`
- Event types: `log` (task log entries), `done` (completion with status)
- Polls task status every 25ms via `setInterval`
- Cleans up interval + subscription on client disconnect (`request.raw.on('close')`)
- If task is already complete at stream start, sends existing logs then `done` immediately

### Site Availability Computation

`buildSiteAvailabilitySummaries()` (stats.ts line 436):
- 24 hourly buckets aligned to local hour boundaries (e.g., 14:00-14:59)
- Only `status === "success"` counts as success
- Latency averaged only over entries with valid numeric `latencyMs` >= 0
- Availability percent rounded to 1 decimal place (e.g., 98.5)
- Average latency rounded to integer ms

### Token Candidate Complexity

`GET /api/models/token-candidates`:
- **models**: token-model-availability entries grouped by modelName (dedup by tokenId)
- **modelsWithoutToken**: accounts with model_availability but no token coverage (only for accounts requiring managed tokens via `requiresManagedAccountTokens()`)
- **modelsMissingTokenGroups**: accounts where token groups don't cover required enableGroups from pricing catalog. Includes `groupCoverageUncertain` flag when some tokens have unresolvable group labels.
- Token group resolution: explicit `tokenGroup` > token name (unless name is "default"/"默认"/pattern like "token-N") > null
- **endpointTypesByModel**: reused from marketplace cache (pricing cache first, base cache fallback)
- **Global model whitelist**: `config.globalAllowedModels` filters ALL three result maps. If whitelist is empty, all models pass through (backward compatible).

### Channel Population from Dual Sources

`populateRouteChannelsByModelPattern()` merges from two sources:
1. **Route channels from exact-model routes**: channels belonging to exact-match routes whose `modelPattern` matches the target pattern. These carry existing priority, weight, and `manualOverride` status.
2. **Raw availability entries**: token_model_availability rows matching the pattern, with default priority=0, weight=10, enabled=true, manualOverride=false.

Route channel candidates are placed FIRST in the merged list; then availability candidates. Within the merged list, deduplication by `(accountId, tokenId, sourceModel)` happens -- first occurrence wins, giving route channels priority over raw availability.

### Explicit Group Strategy Sync

When creating/updating explicit_group routes:
- `syncExplicitGroupSourceRouteStrategies()` propagates the group's strategy to source routes
- Only affects source routes that: (a) use the DEFAULT strategy, (b) already use the TARGET strategy, or (c) were using the group's PREVIOUS strategy (on update)
- Skips source routes referenced by OTHER groups (to avoid overwriting another group's configuration)
- Skips source routes that are themselves explicit_group routes or non-exact-model routes

### Deduplication in Channel Operations

- `POST /api/routes/:id/channels/batch`: silently skips duplicate `(accountId, tokenId, sourceModel)` tuples (counted as `skipped`)
- `POST /api/routes/:id/channels`: returns 400 `"该来源模型的通道已存在"` on duplicate
- Both operations check: `tokenId` belongs to `accountId`; for exact-model routes, token supports the model

---

## Acceptance Criteria
- [ ] All 84 verified endpoints implemented with correct paths, methods, and query params
- [ ] Stats dashboard response includes `x-dashboard-summary-cache` / `x-dashboard-insights-cache` headers
- [ ] Proxy logs list supports 3-view architecture (`?view=query|meta|full`) from single route
- [ ] Proxy logs: client detection fallback chain (dedicated columns -> legacy errorMessage parsing)
- [ ] Proxy logs: cost estimation fallback (Veloera vs other platforms)
- [ ] Marketplace: two-tier cache (15s base / 90s pricing) with `refresh=true` clearing
- [ ] Token candidates: 30 req/min rate limit; globalAllowedModels filter; marketplace cache reuse
- [ ] Settings runtime PUT: 60+ fields with individual validation; changedLabels audit trail; scheduler restarts
- [ ] Settings runtime PUT: self-lockout prevention for admin IP whitelist
- [ ] Backup import: `applyImportedSettingToRuntime()` for all imported settings (60+ keys, individual validation + side effects)
- [ ] Database migration: cross-dialect data transfer with audit event
- [ ] Clear-cache: deletes ALL modelAvailability + routeChannels + tokenRoutes, then rebuilds
- [ ] Clear-usage: deletes ALL proxy logs, resets ALL route channel stats, resets ALL account balanceUsed
- [ ] Downstream key: column-readiness gating for `downstreamApiKeyId` in usage queries
- [ ] Downstream key: policy reference validation (routes, sites, tokens, default_api_keys)
- [ ] Downstream key batch: 5 actions (enable/disable/delete/resetUsage/updateMetadata) with per-item error handling
- [ ] Token routes: explicit_group validation (source routes exist, are exact-model, not self-referencing, not nested groups)
- [ ] Token routes: strategy sync propagation for explicit groups
- [ ] Token routes: dual-source channel population (route channels + raw availability)
- [ ] Token routes: channel deduplication logic
- [ ] Token routes: 60 req/min rate limit on /routes/summary and /routes
- [ ] Update Center: SSE stream task logs with proper event framing and cleanup
- [ ] Monitor: LDOH reverse proxy with URL rewriting and cookie-based auth
- [ ] Site announcements: read/status filtering with timezone-localized dates
- [ ] Error responses match TS patterns per endpoint (4 distinct error response shapes)
- [ ] All JSON response shapes match TS frontend compatibility

## Test Plan
Per-submodule handler test files, mock underlying service layer. Core coverage targets:
- Stats dashboard: verify cache header behavior for all 3 views (summary/insights/full)
- Proxy logs: verify 3-view architecture, parameter parsing edge cases, client fallback chain
- Marketplace: cache TTL behavior, refresh lifecycle, pricing toggle
- Token candidates: rate limit enforcement, whitelist filtering, token group resolution
- Settings runtime: full field validation matrix, changedLabels audit trail, self-lockout prevention
- Backup roundtrip: export -> import preserves all data, settings side effects fire correctly
- Clear-cache: verify all 3 table deletions + rebuild trigger
- Clear-usage: verify proxy logs deletion + route channel reset + balance reset
- Downstream keys: CRUD + summary/overview/trend + batch operations + column-readiness
- Token routes: CRUD + explicit_group validation + channel population + strategy sync
- Checkin: trigger (all + single), logs, schedule update
- Update Center: SSE stream framing and cleanup
- Monitor: LDOH proxy URL rewriting and auth gating

## Key Dependencies
- `service/backup/` -- Backend service (export/import/WebDAV)
- `service/database_migration/` -- Cross-dialect migration
- `service/factory_reset/` -- Factory reset
- `service/proxy_log_store/` -- Log query + field selection
- `service/downstream_key/` -- Key management + policy views
- `service/update_center/` -- Version check/deploy/rollback
- `service/background_task/` -- Task lifecycle + dedup + SSE subscriptions
- `service/route_refresh_workflow/` -- Model refresh + route rebuild
- `service/token_router/` -- Route decision engine
- `service/route_decision_snapshot_store/` -- Decision snapshot persistence
- `service/route_cooldown/` -- Cooldown management
- `service/checkin/` -- Checkin execution
- `service/notify/` -- Notification dispatch
- `service/site_announcement/` -- Announcement sync
- `service/dashboard_snapshot/` -- Dashboard data aggregation
- `service/site_stats_snapshot/` -- Site stats aggregation
- `service/model_availability_probe/` -- Model probe scheduling
- `service/model_pricing/` -- Pricing catalog fetch
- `service/usage_aggregation/` -- Day-level usage aggregation
- `contracts/*RoutePayloads.ts` -- All Zod validation schemas (must be replicated in Go)
