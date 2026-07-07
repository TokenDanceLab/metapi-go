# Test Coverage Audit Report

**Generated**: 2026-07-04
**Project**: metapi-go (<repo>)
**Overall coverage**: 40.8% of statements

---

## 1. Summary

| Metric | Value |
|--------|-------|
| Total packages | 36 |
| Packages with tests | 18 |
| Packages at 0% coverage | 13 |
| Packages <50% coverage | 15 |
| Packages >=80% coverage | 2 (auth: 90.2%, handler/proxy: 78.5%) |
| Overall statement coverage | 40.8% |

The project has a severe coverage gap: over one-third of packages (13/36) have zero test coverage, including entire handler/admin surface, the config system, the app bootstrap layer, and the router. Of the packages that do have tests, 15 fall below the 50% threshold.

---

## 2. Packages Below 50% Coverage (Critical)

| Package | Coverage | Risk |
|---------|----------|------|
| `handler/admin` | **27.4%** | HIGH - All stats, settings, OAuth routes, downstream keys, token routes untested |
| `service/oauth` | **24.9%** | HIGH - Token refresh, quota, connection lifecycle untested |
| `service/balance` | **22.8%** | HIGH - Core balance refresh logic untested |
| `service/checkin` | **33.7%** | HIGH - Core checkin workflow untested |
| `service/notify` | **19.9%** | MEDIUM - All notification senders (bark, telegram, smtp, webhook) untested |
| `service/daily` | **18.4%** | MEDIUM - Daily summary collection untested |
| `service` | **37.9%** | MEDIUM - account_health, site_service, proxy_util all at 0% |
| `store` | **39.0%** | HIGH - DB bootstrap, settings loading, DB switching all untested |
| `scheduler` | **43.7%** | MEDIUM - Many sheduler run functions at 0% |
| `routing` | **48.4%** | HIGH - TokenRouter, ChannelSelector, RouteDecisionService all at 0% |
| `transform/anthropic/messages` | **43.7%** | MEDIUM - Stream bridge, cache control, content conversion branches untested |
| `transform/shared` | **31.7%** | MEDIUM - Claude request parsing, responses format parsing at 0% |

### Packages at 0% Coverage

| Package | Notes |
|---------|-------|
| `app` | Bootstrap, health check, bg services lifecycle |
| `cmd/migrate` | SQLite-to-PG migration tool (all helper functions) |
| `cmd/server` | Entry point |
| `config` | All config loading, parsing, validation |
| `proxy/profiles` | Client detection profiles (claude_code, codex, gemini_cli) |
| `router` | HTTP router setup, middleware |
| `service/adapter` | Adapter registry |
| `transform/openai/*` | chat, completions, embeddings, images, responses transformers |
| `handler/admin/payloads` | No test files at all |
| `proxy/types` | No test files at all |
| `web` | No test files at all |

---

## 3. Focus Area Deep Dives

### 3.1 handler/admin/stats.go (0% coverage)

**All 13 route handlers have zero test coverage.** This is the entire stats/monitoring dashboard API surface.

Functions:
- `RegisterStatsRoutes` - 0%
- `dashboard` - 0% -- main dashboard endpoint, queries sites + accounts + proxy_logs
- `proxyLogs` - 0% -- complex multi-view query with filters (status, search, siteId), pagination, LEFT JOINs
- `proxyLogDetail` - 0% -- single log detail with billing JSON parsing
- `debugTraces` - 0%
- `debugTraceDetail` - 0% -- loads related attempts
- `siteDistribution` - 0% -- STUB, returns empty array
- `siteTrend` - 0% -- STUB, returns empty array
- `modelBySite` - 0% -- queries model_day_usage table
- `marketplace` - 0% -- STUB
- `tokenCandidates` - 0% -- STUB
- `modelCheck` - 0% -- STUB
- `modelProbe` - 0% -- STUB

**Untested error paths**: Invalid proxy log ID parsing, missing proxy log row, billing detail JSON parse failure, invalid account ID in modelCheck, empty view parameter fallback.

**Missing test scenarios**: each handler needs at minimum: (a) valid request returns 200, (b) invalid ID returns 400/404, (c) filter/pagination combinations, (d) view parameter switching (query/meta/full).

### 3.2 handler/admin/settings*.go (0% coverage)

Five files, no tests whatsoever:

| File | Functions | Coverage |
|------|-----------|----------|
| `settings.go` | RegisterSettingsRoutes, getRuntime, updateRuntime, brandList, testSystemProxy + 9 helpers | 0% |
| `settings_database.go` | RegisterDatabaseRoutes, getRuntime, saveRuntime, testConnection, migrate + 2 helpers | 0% |
| `settings_backup.go` | RegisterBackupRoutes, exportBackup, importBackup, getWebdavConfig, saveWebdavConfig, exportToWebdav, importFromWebdav | 0% |
| `settings_notify.go` | RegisterNotifyRoutes, testNotify | 0% |
| `settings_maintenance.go` | RegisterMaintenanceRoutes, clearCache, clearUsage, factoryReset | 0% |

**Critical: `updateRuntime` (settings.go:121)** -- This is the single most dangerous untested function. It handles 60+ config fields with type coercion (toFloat64, toBool), validation (token prefix "sk-", cron format implied, range checks), and DB persistence via upsertSettingDB. A single type coercion bug or validation bypass can corrupt runtime config. There is zero coverage of any mutation path.

**`getRuntime` (settings.go:30)** -- Exposes all runtime config to admin UI, including masked secrets. No coverage of the masking logic or the 50+ field mappings.

**`testSystemProxy` (settings.go:533)** -- STUB that always returns `{"reachable": true, "ok": true}` regardless of input. Zero integration testing.

### 3.3 proxy/endpoint_flow.go (77.3% package, 94.8% on ExecuteEndpointFlow)

This file is actually well-tested. Coverage on `ExecuteEndpointFlow` is 94.8%. All helper functions (BuildUpstreamURL, SummarizeUpstreamError, WithUpstreamPath, runHook) are at 100%.

**Remaining gaps** in the `proxy` package:
- `NewDefaultProxyConductor` - 0%
- `PreviewSelectedChannel` - 0%
- `Execute` (conductor) - 0%
- `NewRuntimeExecutor`, `Dispatch` - 0%
- `handleStreamUpstream`, `handleNonStreamUpstream` - 0%
- `Touch` (session) - 0%
- `touchLease` - 0%

### 3.4 service/oauth/*.go (24.9% package coverage)

The OAuth subsystem has test files for session, flow, import, quota, route_unit, gemini_cli, provider, and refresh -- but coverage is still only 24.9%. Many critical code paths are untested.

**Fully untested critical functions:**

In `flow.go`:
- `activatePersistedOAuthAccount` - 0% -- Core account activation after OAuth callback (DB writes, model sync)
- `captureAccountSnapshot` - 0%
- `revertPersistedOauthAccount` - 0%
- `ensureOAuthProviderSite` - 0%
- `findExistingOAuthAccount` - 0%
- `resolveOauthProviderProxyUrl` - 0%

In `refresh.go`:
- `RefreshAccessTokenSingleflight` - 0% -- Critical token refresh with singleflight dedup
- `doRefreshAccessToken` - 0% -- Actual refresh logic

In `connection.go`:
- `ListOauthConnections` - 0% -- Main connection listing with complex multi-table queries
- `DeleteOauthConnection` - 0%
- `UpdateOauthConnectionProxySettings` - 0%
- `StartOauthRebindFlow` - 0%
- `ensureOauthIdentityBackfill` - 0%

In `quota.go`:
- `RefreshOauthQuotaSnapshot` - 0%
- `RefreshOauthConnectionQuotaBatch` - 0%
- `RefreshOauthConnectionQuota` - 0%
- All `build*QuotaSnapshot` variants - 0%

In `callback_server.go`:
- `StartLoopbackCallbackServer` - 0%
- `StartLoopbackCallbackServers` - 0%
- `StopLoopbackCallbackServers` - 0%
- `handleCallbackRequest` - 0%

**Per-provider token exchange (all 0%):**
- `claude.go`: `postClaudeToken`, `exchangeClaudeAuthorizationCode`, `refreshClaudeAccessToken`, `buildClaudeProxyHeaders`
- `codex.go`: `exchangeCodexToken`, `exchangeCodexAuthorizationCode`, `refreshCodexAccessToken`, `buildCodexProxyHeaders`, `parseJWTClaims`
- `gemini_cli.go`: `postGeminiToken`, `exchangeGeminiCliAuthorizationCode`, `refreshGeminiCliAccessToken`, `buildGeminiCliProxyHeaders`, `callGeminiCliInternalAPI`, `performGeminiCliSetup`
- `antigravity.go`: `postAntigravityToken`, `exchangeAntigravityAuthorizationCode`, `refreshAntigravityAccessToken`, `buildAntigravityProxyHeaders`

**Untested error paths in OAuth:**
- DB not initialized (store.GetDB returns nil)
- Missing OAuth info from account
- Expired/missing refresh token
- Provider definition not found
- Network errors during token exchange
- Rate limit responses from providers
- Duplicate connection detection

---

## 4. Top 10 Most Critical Untested Functions

### 1. `handler/admin/settings.go:updateRuntime` (0%)
**Risk**: CRITICAL
**Why**: Handles 60+ runtime config fields with type coercion, validation, and DB persistence. Any regression can silently corrupt all runtime configuration. This is the admin UI's primary settings mutation endpoint.
**Suggested test**: Table-driven test covering each field group (checkin, balance, notify, debug, routing, proxy) with valid/invalid inputs, type coercion edge cases (float vs int for JSON numbers), and validation rejection paths.

### 2. `service/oauth/refresh.go:RefreshAccessTokenSingleflight` (0%)
**Risk**: CRITICAL
**Why**: Singleflight token refresh is the core of OAuth reliability. A bug here can cause cascading auth failures across all OAuth-managed accounts, deadlocks under concurrent access, or stale tokens not being refreshed.
**Suggested test**: Concurrent refresh for same account (verify singleflight), refresh on missing account, refresh with expired token, panic recovery in defer, concurrent refresh for different accounts.

### 3. `service/oauth/flow.go:activatePersistedOAuthAccount` (0%)
**Risk**: CRITICAL
**Why**: Activated after every successful OAuth callback. Performs DB writes (account creation/update), model sync, and route rebuilds. A failure here means a successful OAuth login produces no usable account.
**Suggested test**: New account creation path, existing account rebind path, DB write failure rollback, model sync failure handling, duplicate detection.

### 4. `handler/admin/stats.go:proxyLogs` (0%)
**Risk**: HIGH
**Why**: Complex multi-view endpoint with LEFT JOINs, conditional WHERE clauses, status/search/siteId filters, pagination, and separate query/meta/full view modes. SQL injection risk if filters are not properly parameterized (they use `?` placeholders, which is correct, but untested).
**Suggested test**: All three view modes, filter combinations, empty results, pagination boundary, search with special characters.

### 5. `service/balance/balance.go:RefreshBalance` (0%)
**Risk**: HIGH
**Why**: Core balance refresh with auto-relogin and income log fallback logic. Called by scheduler and admin manual refresh. A failure here means all balance data is stale.
**Suggested test**: Normal refresh, auto-relogin triggered, income log fallback, disabled site skip, balance parsing edge cases.

### 6. `service/checkin/checkin.go:CheckinAccount` (0%)
**Risk**: HIGH
**Why**: Core checkin workflow with auto-relogin, already-checked-in detection, manual verification required detection. Called by scheduler and admin manual trigger.
**Suggested test**: Successful checkin, already-checked-in detection, unsupported checkin detection, auto-relogin path, manual verification required.

### 7. `handler/admin/settings.go:getRuntime` (0%)
**Risk**: HIGH
**Why**: Exposes 50+ config fields to admin UI including masked secrets. Incorrect masking can leak tokens/keys. Incorrect field mapping can show wrong values in admin UI.
**Suggested test**: Verify all fields present, verify masked fields are actually masked (not plaintext), verify field count matches expectations.

### 8. `service/oauth/connection.go:ListOauthConnections` (0%)
**Risk**: HIGH
**Why**: Main OAuth connection listing with multi-table JOINs, pagination, quota snapshot attachment, route participation resolution. Called by admin UI connection list page.
**Suggested test**: Pagination, empty results, connections with/without quota, connections with route participation, error on missing DB.

### 9. `routing/router.go:SelectChannel` (0%)
**Risk**: HIGH
**Why**: This is the central routing decision function -- every proxy request flows through it. SelectChannel/SelectNextChannel/SelectPreferredChannel are all at 0%. The routing package has complex weight calculation, cooldown logic, and stable-first selection that must work correctly under load.
**Suggested test**: Basic channel selection, next channel on failure, preferred channel with sticky session, cooldown bypass, model not found, zero eligible channels.

### 10. `handler/admin/downstream_keys.go:createKey` (0%)
**Risk**: HIGH
**Why**: Creates downstream API keys with complex policy validation (supported models, allowed route IDs, site weight multipliers, excluded credential refs, expires-at normalization). The normalize functions (20+ helpers) are all at 0% coverage.
**Suggested test**: Valid key creation, policy validation errors, expires-at parsing, site weight normalization, duplicate key name, empty required fields.

---

## 5. Untested Error Paths (Cross-Cutting)

### 5.1 Database Unavailability
- Nearly every handler and service function calls `store.GetDB()` -- none test the nil-return path.
- Example: `service/oauth/refresh.go:doRefreshAccessToken` returns `"database not initialized"` but this error path has zero test coverage across all of `service/oauth`.

### 5.2 Type Coercion in Settings Handlers
- `toFloat64` (settings.go:584) converts JSON numbers via type switch -- `json.Number` conversion error is silently ignored (`n, _ := val.Float64()`).
- `toBool` (settings.go:602) accepts strings "1"/"true"/"yes"/"on" -- unexpected string values default to false with no error.
- These coercion functions are used in `updateRuntime` for every numeric/bool field -- untested coercion bugs become silent config corruption.

### 5.3 JSON Parsing in Stats
- `proxyLogDetail` (stats.go:222) parses billingDetails JSON with silent error swallowing.
- `loadSavedDatabaseConfig` (settings_database.go:121) parses JSON settings values with silent error swallowing.

### 5.4 STUB Endpoints Return Fake Success
- `testSystemProxy` always returns `{"reachable": true, "ok": true}` regardless of actual connectivity.
- `testConnection` (database) skips actual connection test if dialect is non-empty.
- `migrate` (database) returns success with zero rows.
- `modelProbe`, `modelCheck`, `marketplace`, `tokenCandidates` all return stub data.

### 5.5 OAuth Provider Error Paths
- All four providers (claude, codex, gemini_cli, antigravity) have 0% coverage on token exchange (`exchange*`, `post*Token`, `refresh*AccessToken`).
- Error paths from providers (rate limiting, invalid_grant, expired_token) are completely untested.
- The `build*ProxyHeaders` functions are untested -- malformed headers can break upstream requests silently.

---

## 6. Missing Integration Tests for Non-Proxy Handlers

The `handler/admin` package has tests that achieve 27.4% coverage, but these tests only cover `account_tokens.go`, `accounts.go`, and `sites.go`. The following handler files have **zero integration test coverage**:

| File | Routes | Impact |
|------|--------|--------|
| `stats.go` | 13 routes | Stats dashboard, proxy logs, debug traces, model marketplace |
| `settings.go` | 4 routes | Runtime settings CRUD, brand list, proxy test |
| `settings_database.go` | 4 routes | Database config CRUD, connection test, migration |
| `settings_backup.go` | 6 routes | Backup export/import, WebDAV config |
| `settings_notify.go` | 1 route | Notification test |
| `settings_maintenance.go` | 3 routes | Cache clear, usage clear, factory reset |
| `auth_settings.go` | 2 routes | Admin auth token management |
| `checkin_routes.go` | 4 routes | Checkin trigger, logs, schedule |
| `downstream_keys.go` | 10+ routes | Downstream API key CRUD and analytics |
| `events.go` | 5 routes | Event listing, read/unread management |
| `monitor.go` | 4 routes | LDOH monitor config and proxy |
| `oauth_routes.go` | 17 routes | OAuth provider listing, flow, connections, route units |
| `search.go` | 1 route | Global search |
| `tasks.go` | 2 routes | Async task listing |
| `test.go` | 10 routes | Proxy/chat test endpoints |
| `token_routes.go` | 15+ routes | Token route CRUD, channel management, route decision |
| `update_center.go` | 6 routes | Update checking, deploy, rollback |

**None of these 89+ admin API routes have integration tests.**

The existing tests in `handler/admin` appear to use `httptest` with an in-memory SQLite database, which is a good pattern that could be extended to all other handler files.

---

## 7. Recommendations

### Immediate (P0)
1. Add tests for `handler/admin/settings.go:updateRuntime` -- the highest-risk untested function.
2. Add tests for `service/oauth/refresh.go:RefreshAccessTokenSingleflight` -- singleflight correctness is critical.
3. Add tests for `handler/admin/stats.go:dashboard` and `proxyLogs` -- these are the most-accessed admin endpoints.
4. Fix STUB endpoints to either return proper errors or implement the actual logic.

### Short-term (P1)
5. Add integration tests for `handler/admin/settings_*.go` (database, backup, notify, maintenance).
6. Add tests for `service/oauth/flow.go:activatePersistedOAuthAccount` and `HandleCallback`.
7. Add tests for `service/balance/balance.go:RefreshBalance` and `service/checkin/checkin.go:CheckinAccount`.
8. Add tests for `handler/admin/downstream_keys.go` CRUD operations.
9. Add tests for `handler/admin/token_routes.go` route and channel management.

### Medium-term (P2)
10. Add tests for OAuth provider token exchange (claude, codex, gemini_cli, antigravity) -- mock the HTTP layer.
11. Add tests for `routing/router.go` channel selection logic.
12. Add tests for the `app` and `config` packages (bootstrap lifecycle).
13. Add tests for `scheduler` run functions (balance, checkin, daily_summary, backup_webdav).

### Test Infrastructure
- The project already uses `httptest` + SQLite for handler tests (see `handler/admin/*_test.go`). Extend this pattern.
- For OAuth provider tests, add an `httptest.Server` mock that simulates provider token endpoints.
- For scheduler tests, inject a fake clock to control timing.
- Target: raise overall coverage from 40.8% to at least 65% within 2 weeks.

---

## 8. Coverage Data (Raw)

Full function-level coverage data is available at:
- Coverage profile: `<repo>/coverage.out`
- Per-function report generated via `go tool cover -func=coverage.out` (1,708 functions analyzed)
