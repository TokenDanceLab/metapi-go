# P11 Cross-Reference Review: Spec vs TypeScript Source

**Date**: 2026-07-04
**Reviewer**: Automated cross-reference (spec vs 7 TS files provided)
**Verdict**: NEEDS_REVISION

---

## Accuracy Issues

### A1. Endpoint count inflated in spec headers
- **Stats**: Spec header says "(10 endpoints)" but the endpoint table lists **11** rows (dashboard, proxy-logs/:id, proxy-debug/traces, proxy-debug/traces/:id, marketplace, token-candidates, check/:accountId, probe, site-distribution, site-trend, model-by-site). In reality the TS source has **12** stats endpoints because the main proxy-logs list endpoint `GET /api/stats/proxy-logs` is completely missing from the table.
- **Settings**: Spec says "(14 endpoints)" but the table has **17** distinct route entries (runtime GET+PUT=2, brand-list=1, system-proxy/test=1, database/runtime GET+PUT=2, database/test-connection=1, database/migrate=1, backup/export=1, backup/import=1, backup/webdav GET+PUT=2, backup/webdav/export=1, backup/webdav/import=1, notify/test=1, maintenance/clear-cache=1, maintenance/clear-usage=1, maintenance/factory-reset=1).
- **Token Routes**: Spec says "(13 endpoints)" but the table lists **14** rows and the TS source has **20** distinct routes (14 in the table plus 6 undocumented).
- **Downstream Keys**: Spec says "7 endpoints" but TS has **9** (summary, overview, trend, list, create, update, reset-usage, delete, batch).
- **Checkin routes**: Spec says "(5)" but TS has **4** (trigger all, trigger single, logs, schedule).
- Cumulative endpoint count mismatch: Spec claims "~60+" total; counting TS sources yields at least **75** across the files provided, excluding the 5 unprovided module files (search, tasks, test, monitor, announcements, auth, oauth).

### A2. Missing endpoints from spec table (present in TS source)

**Stats module -- missing from spec table:**
| Method | Path | TS Line | Description |
|--------|------|---------|-------------|
| GET | `/api/stats/proxy-logs` | stats.ts:933 | Main proxy logs list (supports `?view=query\|meta\|full`) |

**Token Routes -- missing from spec table:**
| Method | Path | TS Line | Description |
|--------|------|---------|-------------|
| POST | `/api/routes/:id/channels` | tokens.ts:1302 | Add single channel to route |
| PUT | `/api/channels/batch` | tokens.ts:1364 | Batch update channel priorities |
| PUT | `/api/channels/:channelId` | tokens.ts:1397 | Update single channel |
| DELETE | `/api/channels/:channelId` | tokens.ts:1450 | Delete single channel |
| POST | `/api/routes/decision/by-route/batch` | tokens.ts:1003 | Batch per-route decision |
| POST | `/api/routes/decision/route-wide/batch` | tokens.ts:1030 | Batch route-wide decision |

### A3. Spec uses OLD endpoint path for proxy logs list
The proxy logs detail endpoint is correctly listed as `GET /api/stats/proxy-logs/:id`, but the list endpoint uses the SAME base path `GET /api/stats/proxy-logs` with query parameters to differentiate modes (view=query|meta|full). The spec fails to mention both the list endpoint and the multi-view architecture (3 views served from one route via query param).

### A4. Settings table merges GET/PUT but Database table separates them
The spec uses inconsistent notation: `GET/PUT /api/settings/runtime` (merged) and also `GET/PUT /api/settings/backup/webdav` (merged), but lists `GET/PUT /api/settings/database/runtime` as one row. This masks that runtime and webdav each count as 2 endpoints.

---

## Missing Details

### M1. No response schemas documented
The spec's acceptance criteria say "所有端点 JSON 响应格式与 TS 版一致 (前端兼容)" but **zero** response shapes are documented. Critical response types the Go implementation must match include:
- `GET /api/stats/dashboard` -- complex merged object from `getDashboardSummarySnapshot()` + `getDashboardInsightsSnapshot()` with `generatedAt`, cache headers, and dozens of nested fields
- `GET /api/stats/proxy-logs` -- paginated list with `items`, `total`, `page`, `pageSize`, `clientOptions`, `summary`, `sites`
- `GET /api/models/marketplace` -- large `models[]` array with `accounts[]`, `pricingSources[]`, `tags[]`, `supportedEndpointTypes[]`, plus `meta` envelope
- `GET /api/models/token-candidates` -- `models`, `modelsWithoutToken`, `modelsMissingTokenGroups`, `endpointTypesByModel`
- `GET /api/settings/runtime` -- 60+ flat config fields (checkin, proxy, debug, notify, routing, etc.)
- `GET /api/routes/summary` -- array with `channelCount`, `enabledChannelCount`, `siteNames`, `decisionSnapshot`
- `GET /api/downstream-keys/summary` -- `items[]` with nested `rangeUsage` object per key

### M2. No query parameter documentation
Many endpoints have rich query parameters undocumented in the spec:
- `/api/stats/proxy-logs`: `?view=query|meta|full`, `?limit`, `?offset`, `?status=all|success|failed`, `?search`, `?client=app:xxx|family:xxx`, `?siteId`, `?from`, `?to`
- `/api/stats/dashboard`: `?refresh=true|false`, `?view=summary|insights|full`
- `/api/models/marketplace`: `?refresh=true|false`, `?includePricing=true|false`
- `/api/models/probe`: `?accountId`, `?wait=true|false`
- `/api/stats/model-by-site`: `?siteId`, `?days`
- `/api/downstream-keys/summary`: `?range=24h|7d|all`, `?status=enabled|disabled|all`, `?search`, `?group`, `?tags`, `?tagMatch=any|all`
- `/api/downstream-keys/:id/trend`: `?range`, `?timeZone`
- `/api/events`: `?limit`, `?offset`, `?type`, `?read=true|false`
- `/api/routes/decision`: `?model=`

### M3. No request body schemas documented
The spec says nothing about request body shapes for PUT/POST endpoints. Critical bodies:
- `PUT /api/settings/runtime`: 60+ possible fields with precise validation rules (e.g., proxyToken must start with `sk-`, Telegram Bot Token must contain `:`, Cron expressions validated by `cron.validate()`)
- `POST /api/routes` / `PUT /api/routes/:id`: `modelPattern`, `routeMode`, `displayName`, `sourceRouteIds`, `routingStrategy`, `modelMapping`, `displayIcon`, `enabled`
- `POST /api/downstream-keys` / `PUT /api/downstream-keys/:id`: `name`, `key`, `description`, `groupName`, `tags`, `enabled`, `expiresAt`, `maxCost`, `maxRequests`, `supportedModels`, `allowedRouteIds`, `siteWeightMultipliers`, `excludedSiteIds`, `excludedCredentialRefs`
- `POST /api/routes/rebuild`: `refreshModels`, `wait`

### M4. No error response format documented
The spec says nothing about error response shapes. TS consistently returns `{ success: false, message: "..." }` but also uses `{ error: "..." }` in checkin schedule and `{ message: "..." }` in 400/404 responses. The `success` boolean field is inconsistent across the codebase but the Go implementation must match each endpoint's specific pattern.

### M5. No cache-header behavior documented
Multiple endpoints set custom response headers that the frontend depends on:
- `/api/stats/dashboard`: `x-dashboard-summary-cache`, `x-dashboard-insights-cache`
- These carry `hit|miss|stale` cache status values used by the frontend

### M6. No rate-limiting behavior documented
Several endpoints have rate limits that must be preserved:
- `/api/models/token-candidates`: 30 req/min (stats.ts line 85-89)
- `/api/routes/summary`: 60 req/min (tokens.ts line 53-54)
- `/api/routes`: 60 req/min (tokens.ts line 54)
- Returns `429` with `retry-after` header and `{ success: false, message: "请求过于频繁，请稍后再试" }`

### M7. No SSE streaming endpoint documented
`GET /api/update-center/tasks/:id/stream` uses SSE (Server-Sent Events) with `text/event-stream` content type, `hijack()` on the reply, and custom `event:`/`data:` framing. This requires special handler treatment in Go that the spec does not mention.

### M8. Explicit group route validation rules undocumented
The TS code has complex validation for `routeMode: "explicit_group"`:
- Must have at least one source route
- Source routes must exist
- Source routes cannot reference the group itself
- Source routes must be exact-model routes (not wildcard)
- Source routes must not themselves be explicit groups
- Strategy sync propagates to source routes that share the same or default strategy
- Creating/updating a group clears dependent group snapshots

### M9. Token candidate endpoint complexity undocumented
`GET /api/models/token-candidates` has extensive logic for:
- `modelsWithoutToken` (accounts with model availability but no tokens)
- `modelsMissingTokenGroups` (accounts where token groups don't cover required enableGroups from pricing catalog)
- `groupCoverageUncertain` flag
- Global model whitelist filtering (`config.globalAllowedModels`)
- Marketplace cache reuse for endpoint types
This is one of the most complex read endpoints but receives zero documentation.

### M10. Backup import applies settings to runtime
Both `/api/settings/backup/import` and `/api/settings/backup/webdav/import` call `applyImportedSettingToRuntime()` for each imported setting key. This function (settings.ts lines 335-712) handles 60+ setting keys with individual validation and side effects (e.g., restarting schedulers, invalidating caches). The spec does not mention this critical side-effect behavior.

---

## Edge Cases Not Covered

### E1. Proxy logs: `downstreamApiKeyId` column may not exist
`hasProxyLogDownstreamApiKeyIdColumn()` is checked before querying usage data (downstreamApiKeys.ts lines 297, 384, 439). If the column doesn't exist, usage data returns null gracefully. The spec does not mention this column-readiness check.

### E2. Proxy logs: client detection fallback chain
`resolveProxyLogClientMeta()` (stats.ts line 313) has a two-tier fallback: first checks dedicated `clientFamily`/`clientAppId`/`clientAppName` columns, then falls back to parsing `errorMessage` for legacy `usageSource` metadata. The spec does not cover this.

### E3. Marketplace cache: two-tier TTL
The marketplace endpoint uses a 15-second cache for base data and 90-second cache for pricing data (stats.ts line 83-84). Cache keys are `"base"` and `"pricing"`. Refresh clears all cache. The spec does not mention caching behavior.

### E4. Proxy cost estimation fallback formula
`proxyCostSqlExpression()` (stats.ts line 126-137) implements a fallback cost formula: Veloera uses `totalTokens / 1,000,000`, other platforms use `totalTokens / 500,000`. Only applied when `estimatedCost` is null. The spec does not document this business rule.

### E5. Settings: `changedLabels` event tracking
The `PUT /api/settings/runtime` handler builds a `changedLabels: string[]` array that feeds into `appendSettingsEvent()` for the audit log. Only specific fields trigger events. The spec does not mention this audit trail mechanism.

### E6. Downstream keys: deduplication on create/update
The downstream key create and update endpoints check for duplicate `(accountId, tokenId, sourceModel)` tuples before inserting channels (tokens.ts lines 1332-1342). Batch adds skip duplicates silently. The spec does not mention dedup behavior.

### E7. Token routes: dual source (token_model_availability + model_availability)
`populateRouteChannelsByModelPattern()` (tokens.ts line 387) populates channels from TWO sources: existing route channels from matching exact routes AND raw availability entries. Both are merged with route channels taking priority. This dual-source behavior is undocumented.

### E8. Site availability: 24-hour rolling window
`buildSiteAvailabilitySummaries()` (stats.ts line 436) computes hourly buckets for the last 24 hours aligned to local hour boundaries. Only `success` status counts as success. Latency is averaged only over entries with valid numeric latencyMs. The spec mentions "site distribution" and "site trend" but not this detailed availability computation.

### E9. Factory reset side effects
`POST /api/settings/maintenance/factory-reset` calls `performFactoryReset()` which presumably wipes the entire database. The spec says "清空所有数据" but does not specify whether this includes settings, whether the process restarts, or whether there's a confirmation mechanism.

### E10. Clear cache deletes 3 table types
`POST /api/settings/maintenance/clear-cache` deletes ALL rows from `modelAvailability`, `routeChannels`, AND `tokenRoutes`, then triggers a background rebuild. This is an extremely destructive operation. The spec just says "清除缓存" -- the fact that it deletes all routes and channels (not just cache) is a significant detail.

### E11. Clear usage resets route channel stats + account balances
`POST /api/settings/maintenance/clear-usage` not only deletes all proxy logs but also resets all route channel stats (successCount, failCount, totalLatencyMs, totalCost, lastUsedAt, cooldownLevel, etc.) AND all account `balanceUsed` to 0. The spec says "清除用量统计" which understates the scope.

---

## Incorrect Details

### I1. Checkin endpoint count
Spec says checkin has "5" endpoints in the "Other" section. TS source (checkin.ts) has exactly **4** endpoints:
1. `POST /api/checkin/trigger`
2. `POST /api/checkin/trigger/:id`
3. `GET /api/checkin/logs`
4. `PUT /api/checkin/schedule`

### I2. Token Routes endpoint count
Spec claims "13 endpoints" for Token Routes + Channels. The actual TS source (tokens.ts) has **20** endpoints. The spec table itself lists 14 rows, already exceeding the stated count.

### I3. Spec references 29 TS files but only 7 provided for review
The spec header says "29 个文件" as TS reference, but the review was scoped to only 7 files. The following files listed in the spec were NOT provided and cannot be verified:
- `search.ts` (1 endpoint claimed)
- `tasks.ts` (2 endpoints claimed)
- `test.ts` (8 endpoints claimed)
- `monitor.ts` (2 endpoints claimed)
- `siteAnnouncements.ts` (4 endpoints claimed)
- `auth.ts` (2 endpoints claimed)
- `*RoutePayloads.ts` (Zod schemas for all routes)
- ~30 service files

### I4. Settings module lists `oauth_routes.go` but P6 OAuth endpoints are separate
The Go module structure includes `oauth_routes.go` under admin, but OAuth is a separate spec (P6). The P11 spec should note this delegation rather than counting OAuth as part of admin.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues (endpoint counts, missing endpoints, inconsistent notation) | 4 |
| Missing Details (schemas, params, bodies, errors, caches, SSE, rate limits) | 10 |
| Edge Cases Not Covered (column readiness, fallbacks, caching, dedup, dual-source, audit trail) | 11 |
| Incorrect Details (wrong counts, unverifiable references) | 4 |
| **Total Findings** | **29** |

**Verdict**: **NEEDS_REVISION**

The spec is a reasonable structural outline but is unreliable for implementation as-is. Three critical gaps must be addressed before Go implementation begins:

1. **Document all response shapes** -- at minimum for the 12 most complex endpoints (dashboard, proxy-logs list, marketplace, token-candidates, settings runtime, routes summary, downstream-keys summary, downstream-keys overview, routes/channels list, decision, model-by-site, events).
2. **Complete the endpoint table** -- add the 7+ missing endpoints found in TS source (main proxy-logs list, 4 channel CRUD endpoints, 2 decision variant endpoints).
3. **Document cross-cutting behaviors** -- cache headers, rate limits, audit event generation, column-readiness gating, and the side effects of clear-cache / clear-usage / factory-reset / backup-import operations.

Additionally, the 22 unprovided TS files and ~30 service files listed in the spec preamble should be reviewed in a follow-up pass before the spec can be considered complete.
