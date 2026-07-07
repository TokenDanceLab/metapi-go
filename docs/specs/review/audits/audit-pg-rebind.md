# PostgreSQL Placeholder Rebind Audit

**Date:** 2026-07-06
**Scope:** `store`, admin sites/accounts/account-tokens/downstream-keys/token-routes/events/site-announcements/stats paths, OAuth account persistence, OAuth route units, usage aggregation projection, check-in runtime-health persistence, related service helpers, and known follow-up areas
**Verdict:** PARTIAL PASS

## Summary

PostgreSQL is supported at the database bootstrap layer, but not every business path was safe. The issue was not SQL injection. Queries still used bound parameters, but several `sqlx` calls sent `?` placeholders to PostgreSQL instead of rebinding them to `$1`, `$2`, and so on.

The fixed slices are the admin sites, accounts, account-token, downstream-key, token-route, events, site-announcement, stats proxy-log, OAuth account-persistence, OAuth route-unit, usage-aggregation projection, and check-in runtime-health persistence paths.

Sites:

- `POST /api/sites`
- `PUT /api/sites/{id}`
- `DELETE /api/sites/{id}`
- `POST /api/sites/batch`
- `GET/PUT /api/sites/{id}/disabled-models`
- `GET /api/sites/{id}/available-models`

Accounts:

- `GET /api/accounts`
- `POST /api/accounts`
- `POST /api/accounts/login`
- `POST /api/accounts/verify-token`
- `POST /api/accounts/{id}/rebind-session`
- `PUT /api/accounts/{id}`
- `DELETE /api/accounts/{id}`
- `POST /api/accounts/batch`
- `POST /api/accounts/{id}/balance`
- `GET /api/accounts/{id}/models`
- `POST /api/accounts/{id}/models/manual`

Account tokens:

- `GET /api/account-tokens`
- `POST /api/account-tokens`
- `POST /api/account-tokens/batch`
- `PUT /api/account-tokens/{id}`
- `POST /api/account-tokens/{id}/default`
- `GET /api/account-tokens/{id}/value`
- `DELETE /api/account-tokens/{id}`
- `POST /api/account-tokens/sync/{accountId}`
- `GET /api/account-tokens/groups/{accountId}`
- `GET /api/account-tokens/account/{accountId}/default`

Downstream keys:

- `GET /api/downstream-keys/summary?status=...&search=...`
- `POST /api/downstream-keys`
- `PUT /api/downstream-keys/{id}`
- `POST /api/downstream-keys/{id}/reset-usage`
- `DELETE /api/downstream-keys/{id}`
- `POST /api/downstream-keys/batch`

Token routes:

- `GET /api/routes/lite`
- `GET /api/routes/summary`
- `GET /api/routes`
- `POST /api/routes`
- `PUT /api/routes/{id}`
- `DELETE /api/routes/{id}`
- `POST /api/routes/batch`
- `GET /api/routes/{id}/channels`
- `POST /api/routes/{id}/channels`
- `POST /api/routes/{id}/channels/batch`
- `POST /api/routes/{id}/cooldown/clear`
- `PUT /api/channels/batch`
- `PUT /api/channels/{channelId}`
- `DELETE /api/channels/{channelId}`

Events:

- `GET /api/events`
- `GET /api/events/count`
- `POST /api/events/{id}/read`
- `POST /api/events/read-all`
- `DELETE /api/events`

Site announcements:

- `GET /api/site-announcements`
- `POST /api/site-announcements/{id}/read`
- `POST /api/site-announcements/read-all`
- `DELETE /api/site-announcements`

Stats:

- `GET /api/stats/proxy-logs?view=full&search=...`
- `GET /api/stats/proxy-logs?view=query&search=...`
- `GET /api/stats/proxy-logs?view=meta&search=...`
- Same path with `siteId` filters

OAuth account persistence:

- `activatePersistedOAuthAccount` create-account path
- `activatePersistedOAuthAccount` update-existing-account path
- `ensureOAuthProviderSite` create-site path
- `revertPersistedOauthAccount` rollback transaction path

OAuth route units:

- `CreateOauthRouteUnit`
- `UpdateOauthRouteUnit`
- `DeleteOauthRouteUnit`
- `ListOauthRouteUnitsByAccountIDs`
- `ListEnabledOauthRouteUnitsWithMembers`

Usage aggregation:

- `UsageAggregationScheduler.RunProjectionPass`
- `fetchBatch` proxy-log reads
- `applyBatch` transaction writes for `site_day_usage`
- `applyBatch` transaction writes for `site_hour_usage`
- `applyBatch` transaction writes for `model_day_usage`
- `analytics_projection_checkpoints` watermark update

Check-in runtime health:

- `CheckinAccount` disabled-site branch
- `SetAccountRuntimeHealth`
- `checkin_logs` writes from check-in execution
- `CheckinAll` account selection

The store wrapper now also covers the common `sqlx` helper methods:

- `Get`
- `Select`
- `Queryx`
- `QueryRowx`

## What Changed

`store.DB` already rebound `Exec`, `Query`, and `QueryRow`. It now explicitly rebounds `Get`, `Select`, `Queryx`, and `QueryRowx` before delegating to the embedded `*sqlx.DB`.

`handler/admin/sites.go` still receives a bare `*sqlx.DB` through the current router signature, so every parameterized sites query now calls `h.db.Rebind(query)` before `Get`, `Select`, or `Exec`.

`service/site_service.go` now calls `db.Rebind(query)` or `tx.Rebind(query)` for the sites CRUD and API endpoint transaction path. This covers PostgreSQL transaction failures during site create/update with `apiEndpoints`.

`handler/admin/accounts.go` and `service/account_service.go` now rebind parameterized queries on the account control-plane path. SQLite-style boolean literals in account login, manual model updates, and event creation were replaced with bound boolean parameters where they target boolean columns.

`handler/admin/account_tokens.go` and `service/account_token_service.go` now rebind token CRUD, default-token repair, groups, token value, and relation queries. Boolean writes for `enabled` and `is_default` now use bound booleans instead of SQLite-style `0`/`1` literals.

`handler/admin/downstream_keys.go` now rebinds downstream-key duplicate checks, create/update/reset/delete, and batch updates. PostgreSQL create uses `RETURNING id` through the shared admin insert-id helper, while SQLite keeps `LastInsertId()`. Boolean filters and batch enable/disable writes use bound booleans instead of integer literals.

`handler/admin/token_routes.go` now rebinds route CRUD, route-channel CRUD, batch route updates, batch channel inserts, cooldown clearing, and explicit group source queries. Boolean writes for `token_routes.enabled`, `route_channels.enabled`, and `route_channels.manual_override` now use bound booleans. PostgreSQL inserts that need the new row id use `RETURNING id`; SQLite keeps `LastInsertId()`.

`handler/admin/events.go` now rebinds filtered list, unread count, mark-read, and mark-all-read SQL. The `events.read` boolean column uses bound booleans instead of SQLite-style `0`/`1` literals.

`handler/admin/site_announcements.go` now rebinds filtered list and mark-read SQL. The delete-all path has no parameters and remains dialect-neutral.

`handler/admin/stats.go` now rebinds the filtered proxy-log count and summary queries. The item query already used the shared admin query helper; before this change, PostgreSQL could return matching items while reporting `total=0` and an empty summary for filtered requests.

`service/oauth/flow.go` now uses bound booleans for OAuth-managed `accounts` and provider `sites` rows. PostgreSQL inserts that need a new `sites.id` or `accounts.id` use `RETURNING id`; SQLite keeps `LastInsertId()`. The rollback transaction now rebinds `*sqlx.Tx` statements before execution.

`service/oauth/route_unit.go` now avoids duplicate-column nested scans on the account lookup path. Route-unit creation uses a dialect-aware insert-id helper: PostgreSQL uses `RETURNING id`, while SQLite keeps `LastInsertId()`. Transaction inserts call `tx.Rebind`, and the enabled-unit query uses a bound boolean instead of a database-specific literal.

`scheduler/usage_aggregation.go` now keeps projection writes inside a `sqlx` transaction and calls `tx.Rebind` for every parameterized statement. The projection uses additive upserts for `site_day_usage`, `site_hour_usage`, and `model_day_usage`, so later batches accumulate totals instead of being dropped by `DO NOTHING`. Hour buckets now derive from `proxy_logs.created_at`, and model usage uses `proxy_logs.model_actual` with an `unknown` fallback.

`service/account_health.go` now rebinds the `accounts.extra_config` read and update statements used by runtime-health persistence. `service/checkin/checkin.go` now rebinds check-in writes, account updates, and `CheckinAll` selection through the active driver. Core check-in persistence errors are no longer ignored: failures to write runtime health, account updates, or `checkin_logs` are returned as failed check-in results, while non-critical event-write failures are logged and do not change the check-in outcome.

## Verification

Run without PostgreSQL:

```bash
go test ./handler/admin ./service ./store -count=1
```

Run with a PostgreSQL test database:

```bash
PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestSites_Postgres_CreateUpdateAndDisabledModels -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./store \
  -run TestPostgresSQLXHelpersRebindPlaceholders -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestAccounts_Postgres_CreateUpdateManualModelsAndBatch -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestTokens_Postgres_CreateListUpdateDefaultValueAndDelete -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestDownstreamKeys_PostgresCRUDResetBatchAndDelete -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestTokenRoutes_Postgres_CreateUpdateChannelAndDelete -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestEventsAndAnnouncements_PostgresLifecycle -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin \
  -run TestStats_PostgresProxyLogsFilteredTotals -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./service/oauth \
  -run TestActivatePersistedOAuthAccount_PostgresCreate -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./service/oauth \
  -run TestOauthRouteUnit_PostgresLifecycle -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./scheduler \
  -run TestUsageAggregationProjection_PostgresAccumulatesIncrementalLogs -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./service/checkin \
  -run TestCheckinAccount_PostgresDisabledSitePersistsRuntimeHealthAndLog -count=1

PG_TEST_DSN="$POSTGRES_TEST_DSN" go test ./handler/admin ./store ./service/oauth ./scheduler ./service/checkin \
  -run 'TestSites_Postgres_CreateUpdateAndDisabledModels|TestAccounts_Postgres_CreateUpdateManualModelsAndBatch|TestTokens_Postgres_CreateListUpdateDefaultValueAndDelete|TestDownstreamKeys_PostgresCRUDResetBatchAndDelete|TestTokenRoutes_Postgres_CreateUpdateChannelAndDelete|TestEventsAndAnnouncements_PostgresLifecycle|TestStats_PostgresProxyLogsFilteredTotals|TestPostgresSQLXHelpersRebindPlaceholders|TestActivatePersistedOAuthAccount_PostgresCreate|TestOauthRouteUnit_PostgresLifecycle|TestUsageAggregationProjection_PostgresAccumulatesIncrementalLogs|TestCheckinAccount_PostgresDisabledSitePersistsRuntimeHealthAndLog' \
  -count=1
```

Before the fixes:

- `POST /api/sites` returned `500 {"error":"Create site failed"}`.
- `POST /api/accounts` returned `400 {"message":"site not found","success":false}` even after the site was created.
- `POST /api/account-tokens` returned `500 {"message":"Token creation failed","success":false}` with a PostgreSQL syntax error near `,`.
- `POST /api/downstream-keys` and downstream-key update/reset/delete paths could fail on PostgreSQL because the handler received a bare `*sqlx.DB` and sent `?` placeholders without rebinding.
- `POST /api/routes` returned `500 {"message":"创建路由失败","success":false}`.
- `POST /api/routes/{id}/channels` returned `200` with a `null` row body after the first placeholder fix, because pgx does not support `LastInsertId()` and the nullable-field fallback could miss the inserted row.
- `GET /api/events?type=...&read=false` returned `500 {"error":"Failed to load events"}` with a PostgreSQL syntax error near `AND`.
- `GET /api/stats/proxy-logs?search=...` could return matching `items` but report `total=0` and an empty summary, because only the item query was rebound.
- OAuth account persistence failed while creating the provider site because `use_system_proxy` and `is_pinned` were written as integer literals into PostgreSQL boolean columns.
- OAuth route-unit creation depended on `LastInsertId()` and transaction statements with `?` placeholders, which pgx does not support.
- Usage aggregation used a raw `database/sql` transaction path, skipped transaction Exec errors, and used `DO NOTHING` for aggregate rows. PostgreSQL rejected the un-rebound `?` placeholders, and repeated projections for the same site/day/hour could fail to add later usage.
- Check-in on a disabled site returned a skipped result but failed to persist `extra_config.runtimeHealth` and `checkin_logs` on PostgreSQL, because runtime-health and log writes used un-rebound `?` placeholders and ignored write errors.

The PostgreSQL downstream-key, route, events, site-announcement, stats, OAuth account-persistence, OAuth route-unit, usage-aggregation, and check-in runtime-health tests pass after the rebind, boolean-literal, `RETURNING id`, additive-upsert, and persistence-error handling changes.

## Remaining Risk

This is not a whole-repository PostgreSQL compatibility pass yet. Several areas still pass bare `*sqlx.DB` or use `*sqlx.Tx` directly:

- Other admin handlers that still accept bare `*sqlx.DB`.
- Scheduler entrypoints that unwrap `store.DB` before calling service packages.
- OAuth service paths outside account persistence and route units, including connection helpers.
- Some account and account-token insert paths still use race-prone `LastInsertId()` fallbacks instead of `RETURNING id`.

The next fix should move production code away from bare `*sqlx.DB` signatures. Preferred options:

1. Route handlers and services accept `*store.DB` where they need dialect-aware helpers.
2. Introduce a small project DB interface for `Get`, `Select`, `Queryx`, `Exec`, and transaction helpers.
3. Add a static regression test that flags new production `*sqlx.DB` or `*sqlx.Tx` signatures outside `store` and tests unless each parameterized query explicitly calls `Rebind`.

## External Guidance Checked

The follow-up direction matches common Go data-access guidance:

- `sqlx` exposes `Rebind`, but it does not make bare `database/sql` or every direct `sqlx` call dialect-safe by itself: https://pkg.go.dev/github.com/jmoiron/sqlx
- Go's database documentation recommends parameterized queries and avoiding SQL string assembly from untrusted input: https://go.dev/doc/database/sql-injection
- Repository and unit-of-work patterns keep transaction and database details out of handlers and service logic:
  - https://rednafi.com/go/repo-txn-uow/
  - https://threedots.tech/post/repository-pattern-in-go/
  - https://threedots.tech/post/database-transactions-in-go/
