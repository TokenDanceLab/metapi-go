# Integration Test Gap Audit

**Date**: 2026-07-05
**Scope**: metapi-go integration / end-to-end test coverage
**Method**: Full scan of 67 `*_test.go` files, 1 `e2e/*_test.go` file, source code cross-referenced for uncovered critical paths.

---

## Coverage Summary

| Layer | Test Files | Coverage Level |
|-------|-----------|---------------|
| E2E (proxy pipeline) | 1 (`e2e/e2e_test.go`) | Good -- 13 tests covering chat, streaming, retry, sticky, auth |
| Admin CRUD (handler) | 3 (`accounts_test.go`, `sites_test.go`, `edge_cases_test.go`) | Good -- full CRUD, batch, login, balance, models |
| Service (business logic) | 4+ (oauth, checkin, account, site) | Good -- unit-level coverage for all providers |
| Platform adapters | 15+ | Good -- each platform has dedicated tests |
| Proxy (channel, session, retry) | 7 | Good -- channel selection, sticky, retry policy tested |
| Routing | 8+ | Good -- cooldown, weights, matcher, selectors |
| Store (migration, schema) | 5 | Moderate -- idempotent migration, table structure; PG conditional |

---

## Critical Gaps -- NO Automated Test

### Gap 1: Site-Create-to-Proxy Full Business Flow (CRITICAL)

**Journey**: `POST /api/sites` -> `POST /api/accounts` -> `POST /api/accounts/checkin` -> `POST /chat/completions` (proxy)

**Current state**: Each step tested in isolation. Sites CRUD in `handler/admin/sites_test.go`, accounts CRUD in `handler/admin/accounts_test.go`, checkin in `service/checkin/checkin_test.go`, proxy in `e2e/e2e_test.go`. No single test spans all four stages.

**Risk**: A regression in site creation could break proxy routing silently. Account token model changes could desync between admin API and proxy handler.

**What to test**:
1. Create site via admin API, create account with API key via admin API, invoke checkin, then send a chat completions request through the proxy and verify the account's token reaches the upstream.
2. Verify that a disabled site still has accounts but the proxy skips all of them.
3. Verify that a site with `status=disabled` does not appear in route channels after rebuild.

### Gap 2: OAuth Authorization Code Flow End-to-End (CRITICAL)

**Journey**: `POST /api/oauth/start` -> browser callback -> `POST /api/oauth/callback` -> token stored in `account_tokens` -> proxy uses OAuth token for upstream.

**Current state**: `service/oauth/flow_test.go` tests `StartFlow`, `HandleCallback`, `SubmitManualCallback`, session management at the service layer. `service/oauth/import_test.go` tests import parsing. No test drives the full round-trip through the HTTP API layer. No test verifies the token exchange actually produces a usable credential that the proxy can consume.

**Risk**: Each OAuth provider (Codex, Claude, Gemini CLI, Antigravity) has unique callback URL formats and token exchange logic. Without end-to-end testing, a change to the callback URI or session store can break the whole flow while unit tests still pass.

**What to test**:
1. POST `/api/oauth/start` with `provider=codex` -> verify `authorizationURL` and `state` returned.
2. POST `/api/oauth/callback` with valid `state` and mock `code` -> verify `access_token` stored in `account_tokens`.
3. Query accounts list -> verify new account has `oauthProvider=codex`, correct `oauthAccountKey`.
4. Route rebuild -> verify route channel exists for the new OAuth account.
5. Send chat completions request -> verify proxy picks the OAuth channel and the access token reaches upstream.
6. Repeat for all 4 providers.

### Gap 3: Backup -> Factory Reset -> Import -> Verify Roundtrip (HIGH)

**Journey**: `GET /api/settings/backup/export` -> save response -> `POST /api/settings/maintenance/factory-reset` -> `POST /api/settings/backup/import` -> re-export and compare.

**Current state**: `handler/admin/settings_backup.go` implements export, import, WebDAV stubs. `handler/admin/settings_maintenance.go` implements factory reset. Zero tests exist for any backup/maintenance endpoint.

**Risk**: Backup and restore are **the** disaster recovery path. A bug here is catastrophic -- a bad export that drops columns, a broken import that skips rows silently, or a factory reset that leaves FK orphans would all cause permanent data loss.

**What to test**:
1. Seed database with known data (sites, accounts, tokens, settings, route channels).
2. Export full backup via `GET /api/settings/backup/export?type=all`.
3. Factory reset via `POST /api/settings/maintenance/factory-reset {"confirm":true}`.
4. Verify all 27 tables are empty.
5. Import the saved export via `POST /api/settings/backup/import`.
6. Re-export and verify tables match the original export (row counts, key fields).
7. Verify accounts export (`?type=accounts`) skips usage/proxy_log tables but includes all credential tables.
8. Verify preferences export (`?type=preferences`) returns only the `settings` table.

### Gap 4: Migration SQLite <-> PostgreSQL Roundtrip (HIGH)

**Journey**: SQLite DB -> dump all tables -> import into PostgreSQL -> verify schema compatibility and data integrity.

**Current state**: `store/dialect_test.go` tests dialect differences. `store/migrate_test.go` tests AutoMigrate idempotency. `store/postgres_test.go` tests PG Open/AutoMigrate but is conditional on `PG_TEST_DSN` env var. No test exports data from SQLite and imports into PG (or vice versa). No test verifies that the schema (27 tables, all indexes, FK constraints) is identical between the two backends.

**Risk**: Schema DDL uses GORM-compatible type mappings but subtle differences exist: SQLite uses `INTEGER PRIMARY KEY AUTOINCREMENT`, PG uses `SERIAL`/`BIGSERIAL`. TEXT PK tables behave differently. FK cascade behavior differs. Without a roundtrip test, a migration from SQLite to PG could silently drop data or fail at runtime.

**What to test**:
1. Spin up both SQLite (`:memory:`) and PostgreSQL (test container or `PG_TEST_DSN`).
2. Run AutoMigrate on both, diff the resulting schemas (all 27 tables, columns, types, indexes, FKs).
3. Seed SQLite with a representative dataset (sites, accounts, tokens, route channels, settings, usage data, model availability).
4. Dump all tables from SQLite, import into PostgreSQL using the backup import format.
5. Verify row counts match for all 27 tables.
6. Verify specific data integrity: account tokens decrypt correctly, settings key-value pairs survive, FK relationships intact, timestamps preserve UTC.
7. Reverse: seed PG, export, import into SQLite.

### Gap 5: Rate Limiter Actually Blocks Requests (MEDIUM)

**Journey**: Fire N+1 requests at the admin API, verify the N+1th gets 429.

**Current state**: `auth/ratelimit.go` implements per-IP token-bucket rate limiter via `golang.org/x/time/rate`. `AdminRateLimit(100, 200)` and `OAuthRateLimit(10, 20)` are wired in `router/router.go`. No test exists. The `TestRateLimit` in `e2e/e2e_test.go` tests **channel concurrency** (5 concurrent requests), not the **rate limiter**.

**Risk**: If the rate limiter is misconfigured (e.g., burst > rate, or rate=0), it silently allows all traffic. If `extractClientIP` gets the wrong IP from headers, the rate limit is per-forwarded-IP rather than per-real-client. The cleanup goroutine could leak if never tested.

**What to test**:
1. Spin up the router with `AdminRateLimit(2, 3)`. Fire 4 requests in rapid succession. Verify the 4th returns 429 with `Retry-After: 1` header.
2. Verify different IPs get independent rate limits: IP A exhausted, IP B still allowed.
3. Verify OAuth endpoints use `OAuthRateLimit` cap independent from `AdminRateLimit`: `/api/oauth/start` blocked after 10 req/s but `/api/sites` still available.
4. Verify non-OAuth `/api/*` paths are NOT subject to OAuthRateLimit (only AdminRateLimit).
5. Long-running: wait 6+ minutes, verify idle IP entries are cleaned up (memory not leaked).

---

## What IS Covered (for reference)

| Area | Test File(s) | What's Tested |
|------|------------|--------------|
| Proxy E2E | `e2e/e2e_test.go` | Chat, retry, sticky session, managed keys, model matching, model-not-allowed, error propagation, SSE streaming, non-streaming, missing model, SSE headers |
| Admin Sites | `handler/admin/sites_test.go` | CRUD, duplicate detection, list, update, delete |
| Admin Accounts | `handler/admin/accounts_test.go` | CRUD, batch, login with password encrypt, rebind, verify token, health refresh, balance, models, manual models |
| Admin Edge Cases | `handler/admin/edge_cases_test.go` | Empty body, oversized body, negative pagination, SQLite concurrent writes, duplicate inserts, NULL handling, timezone, TOCTOU race, JSON injection, Unicode, settings update |
| OAuth Service | `service/oauth/flow_test.go` | StartFlow all 4 providers, callback, session, manual callback, SSH instructions |
| OAuth Import | `service/oauth/import_test.go` | Provider mapping, expiry parsing, JWT claims, identity resolution, Sub2API rejection |
| OAuth Provider | `service/oauth/provider_test.go` | Provider definitions |
| OAuth Refresh | `service/oauth/refresh_test.go` | Token refresh logic |
| OAuth Session | `service/oauth/session_test.go` | Session store |
| OAuth Gemini CLI | `service/oauth/gemini_cli_test.go` | Gemini CLI OAuth specifics |
| Checkin | `service/checkin/checkin_test.go` | Message pattern matching (already signed, turnstile, auto-relogin, site disabled) |
| Checkin Reward | `service/checkin/reward_parser_test.go` | Reward parsing |
| Checkin Failure | `service/checkin/failure_reason_test.go` | Failure reason classification |
| Account Mutation | `service/account_mutation_test.go` | Credential mode, capabilities, CRUD, extra config |
| Account Credential | `service/account_credential_test.go` | Credential encryption |
| Site Service | `service/site_service_test.go` | Platform detection (30+ platforms), URL canonicalization, validation |
| Store Migration | `store/migrate_test.go` | AutoMigrate idempotent, with data, indexes, table structure, text PK tables |
| Store Schema | `store/schema_test.go` | Schema tests |
| Store PG | `store/postgres_test.go` | PG open, AutoMigrate (conditional on env) |
| Store Dialect | `store/dialect_test.go` | Dialect detection |
| Platform (all) | `platform/*_test.go` | Per-platform adapter tests |
| Proxy Retry | `proxy/retry_policy_test.go` | Retry/shouldRetry/abort policies including rate-limit text |
| Proxy Session | `proxy/session_test.go` | Sticky session, concurrency, lease |
| Proxy Profiles | `proxy/profiles_test.go` | Client detection (Claude Code, Codex, Gemini CLI, generic) |
| Proxy Channel | `proxy/channel_selection_test.go` | Channel selection |
| Proxy Surface | `proxy/surface_test.go` | Surface/endpoint tests |
| Proxy Endpoint | `proxy/endpoint_flow_test.go` | Endpoint flow including rate-limit abort |
| Routing | `routing/*_test.go` | Fibonacci backoff, cooldown, round-robin, matcher, weights, algorithm, cache, runtime health |
| Transform | `transform/*/roundtrip_test.go` | Request/response transformation roundtrips |
| Scheduler | `scheduler/scheduler_test.go` | Scheduled task tests |
| Notify | `service/notify/*_test.go` | Notification throttling and sending |
| Alert | `service/alert/alert_test.go` | Alert service |
| Daily Summary | `service/daily/daily_summary_test.go` | Daily summary generation |
| Balance | `service/balance/balance_test.go` | Balance tests |

---

## Recommended Integration Test Implementation Order

1. **P0 - Rate Limiter** (lowest effort, high value): Add `auth/ratelimit_test.go` with the 5 test cases above. This is a pure unit/integration test with no external dependencies.
2. **P0 - OAuth E2E** (medium effort, high value): Add `e2e/oauth_flow_e2e_test.go` that drives the full OAuth lifecycle through the HTTP router for all 4 providers.
3. **P1 - Backup/Factory-Reset/Import** (medium effort, high value): Add `handler/admin/backup_maintenance_test.go` covering the export-import roundtrip.
4. **P1 - Site-to-Proxy** (medium effort, high value): Extend `e2e/e2e_test.go` with an integration test that creates a site+account through admin API, then proxies a request through it.
5. **P2 - SQLite<->PG Migration** (high effort, high value): Add `store/migration_roundtrip_test.go` requiring a PG test container or `PG_TEST_DSN`.

---

## Appendix: Files Examined

- `<repo>/e2e/e2e_test.go` (1200 lines, 13 tests)
- `<repo>/service/oauth/flow_test.go` (554 lines)
- `<repo>/service/oauth/import_test.go` (507 lines)
- `<repo>/service/checkin/checkin_test.go` (329 lines)
- `<repo>/handler/admin/accounts_test.go` (794 lines)
- `<repo>/handler/admin/edge_cases_test.go` (907 lines)
- `<repo>/handler/admin/settings_backup.go` (338 lines, zero tests)
- `<repo>/handler/admin/settings_maintenance.go` (155 lines, zero tests)
- `<repo>/auth/ratelimit.go` (120 lines, zero tests)
- `<repo>/store/migrate_test.go` (247 lines)
- `<repo>/store/postgres_test.go` (conditional, env-dependent)
- `<repo>/store/sqlite_test.go` (no test functions with standard naming)
- `<repo>/service/site_service_test.go` (501 lines)
- `<repo>/service/account_mutation_test.go` (561 lines)
- `<repo>/routing/cooldown_test.go` (471 lines)
- `<repo>/proxy/profiles_test.go` (501 lines)
- 67 total `*_test.go` files across the repository
