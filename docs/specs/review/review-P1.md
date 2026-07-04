# P1 Cross-Reference Review: Go Spec vs TypeScript Source

**Spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p1-database.md`
**TS Sources**: `schema.ts`, `index.ts`, `migrate.ts`, `runtimeSchemaBootstrap.ts`, `schemaContract.json`
**Date**: 2026-07-04

---

## Accuracy Issues

### A1. `real` type mapping claims `DOUBLE PRECISION` for PG -- ambiguous naming
**Spec line**: type mapping table, row 3
The spec maps `real` to `DOUBLE PRECISION`. In PostgreSQL, `REAL` (unqualified) means `float4` (4 bytes), while `DOUBLE PRECISION` means `float8` (8 bytes). SQLite's `real` is always 8-byte IEEE 754. Therefore `DOUBLE PRECISION` is technically correct, but the spec should explicitly warn that bare `REAL` in PG is NOT equivalent -- the Go migration DDL must use `DOUBLE PRECISION` (or `FLOAT8`), never bare `REAL`.

### A2. `route_channels` FK constraint summary is incomplete
**Spec line**: table row 13 -- "FK to token_routes + accounts + account_tokens"
TS source (`schema.ts` lines 213-216):
- `route_id` REFERENCES `token_routes(id)` ON DELETE **CASCADE**
- `account_id` REFERENCES `accounts(id)` ON DELETE **CASCADE**
- `token_id` REFERENCES `account_tokens(id)` ON DELETE **SET NULL**

The spec omits the ON DELETE actions. The `token_id` FK uses `SET NULL` (not `CASCADE`), which matters because deleting an `account_token` should nullify the channel's token_id rather than delete the channel itself. A Go migration that uses CASCADE for all three would break production behavior.

### A3. `site_disabled_models` FK constraint not documented
**Spec line**: table row 3 -- "UNIQUE(site_id, model_name)"
TS source (`schema.ts` line 49):
```ts
siteId: integer('site_id').notNull().references(() => sites.id, { onDelete: 'cascade' }),
```
The spec only lists the UNIQUE constraint. The foreign key to `sites(id)` with ON DELETE CASCADE is missing from the constraint column entirely.

### A4. `proxy_debug_attempts` FK to `proxy_debug_traces` omitted
**Spec line**: table row 16 -- "UNIQUE(trace_id, attempt_index)"
TS source (`schema.ts` line 313):
```ts
traceId: integer('trace_id').notNull().references(() => proxyDebugTraces.id, { onDelete: 'cascade' }),
```
The FK to `proxy_debug_traces(id)` ON DELETE CASCADE is present in TS but missing from the spec's constraint summary.

### A5. `settings` and `analytics_projection_checkpoints` PK type is `text`, not `SERIAL`
**Spec line**: table rows 19 and 21 -- "PK=key (text)" / "PK=projector_key (text)"
This is stated correctly. However, the type mapping table says `integer` (autoIncrement PK) maps to `SERIAL PRIMARY KEY` in PG, which could mislead Go developers when they encounter these two tables. The spec should explicitly note that these two tables do NOT use auto-increment integer PKs; they use text primary keys.

### A6. `oauth_route_unit_id` column has no FK constraint (correctly absent, but column not mentioned)
**Spec line**: table row 13 mentions `route_channels` FKs
TS source (`schema.ts` line 217): `oauthRouteUnitId: integer('oauth_route_unit_id')` -- no `.references()` call, no FK constraint. The spec does not claim an FK for this column, which is correct, but the column itself is not mentioned at all in the constraint column, potentially causing it to be overlooked during schema generation.

---

## Missing Details

### M1. Complete column definitions omitted for all 27 tables
**Spec declares**: "精简映射 (完整 27 表细节见 TS 原始 schema.ts)"

The spec table only lists table name, Go struct name, and 1-2 key constraints per table. Go implementers must read both `schema.ts` and `schemaContract.json` to build all columns. Critical omissions per table (column counts):

| Table | Columns in TS | Columns mentioned in spec |
|---|---|---|
| `sites` | 19 columns | 0 |
| `site_api_endpoints` | 11 columns | 0 |
| `site_disabled_models` | 4 columns | 0 |
| `accounts` | 22 columns | 0 |
| `account_tokens` | 11 columns | 0 |
| `checkin_logs` | 6 columns | 0 |
| `model_availability` | 7 columns | 0 |
| `token_model_availability` | 6 columns | 0 |
| `token_routes` | 12 columns | 0 |
| `route_group_sources` | 3 columns | 0 |
| `oauth_route_units` | 8 columns | 0 |
| `oauth_route_unit_members` | 16 columns | 0 |
| `route_channels` | 20 columns | 0 |
| `proxy_logs` | 23 columns | 0 |
| `proxy_debug_traces` | 24 columns | 0 |
| `proxy_debug_attempts` | 18 columns | 0 |
| `proxy_video_tasks` | 14 columns | 0 |
| `proxy_files` | 12 columns | 0 |
| `settings` | 2 columns | 0 |
| `admin_snapshots` | 9 columns | 0 |
| `analytics_projection_checkpoints` | 17 columns | 0 |
| `site_day_usage` | 12 columns | 0 |
| `site_hour_usage` | 12 columns | 0 |
| `model_day_usage` | 13 columns | 0 |
| `downstream_api_keys` | 20 columns | 0 |
| `site_announcements` | 18 columns | 0 |
| `events` | 9 columns | 0 |

The `schemaContract.json` (84KB) is the authoritative DDL source per spec line 9, but the spec itself does not reproduce or summarize column definitions. This forces Go developers to cross-reference TS continuously.

### M2. Index definitions not enumerated
The TS source defines 60+ indexes across all tables. The spec mentions only:
- "7 composite indexes on created_at" for `proxy_logs` (a summary, not a list)
- PK/FK/UNIQUE constraints in the spec table

Missing index examples (not exhaustive):
- `sites`: `sites_status_idx` on (status)
- `accounts`: `accounts_site_id_idx`, `accounts_status_idx`, `accounts_site_status_idx`, `accounts_oauth_provider_idx`, `accounts_oauth_identity_idx`
- `account_tokens`: `account_tokens_account_id_idx`, `account_tokens_account_enabled_idx`, `account_tokens_enabled_idx`
- `route_channels`: 6 indexes (route_id, account_id, token_id, oauth_route_unit_id, route+enabled, route+token)
- `proxy_debug_traces`: 4 indexes on (created_at, session+created_at, model+created_at, final_status+created_at)
- `proxy_debug_attempts`: 2 indexes
- `events`: 3 indexes on (read+created_at, type+created_at, created_at)
- `checkin_logs`: 3 indexes
- `model_availability`: 3 indexes
- `token_model_availability`: 4 indexes
- `token_routes`: 2 indexes (model_pattern, enabled)
- `oauth_route_units`: 2 indexes
- `oauth_route_unit_members`: 4 indexes (beyond the 2 UNIQUEs listed)
- `proxy_video_tasks`: 3 indexes
- `proxy_files`: 2 indexes (beyond the UNIQUE listed)
- `admin_snapshots`: 3 indexes
- `analytics_projection_checkpoints`: 2 indexes
- `site_day_usage`: 2 indexes (beyond the UNIQUE listed)
- `site_hour_usage`: 2 indexes (beyond the UNIQUE listed)
- `model_day_usage`: 3 indexes (beyond the UNIQUE listed)
- `downstream_api_keys`: 4 indexes (beyond the UNIQUE listed)
- `site_announcements`: 3 indexes (beyond the UNIQUE listed)
- `site_api_endpoints`: 3 indexes
- `site_disabled_models`: 1 index

### M3. CHECK constraints not fully documented
TS source defines CHECK constraints on three usage tables:
- `site_day_usage`: CHECK on 8 columns >= 0 (`total_calls`, `success_calls`, `failed_calls`, `total_tokens`, `total_summary_spend`, `total_site_spend`, `total_latency_ms`, `latency_count`)
- `site_hour_usage`: CHECK on 8 columns >= 0 (same columns as site_day_usage)
- `model_day_usage`: CHECK on 7 columns >= 0 (`total_calls`, `success_calls`, `failed_calls`, `total_tokens`, `total_spend`, `total_latency_ms`, `latency_count`)

The spec table mentions "CHECK >= 0" for these (rows 22-24) but does not enumerate which columns are covered. PG migrations must replicate these CHECK constraints with exact column lists.

### M4. Default values for PG datetime columns not specified
The TS Drizzle uses `sql\`(datetime('now'))\`` as a default expression, which is SQLite-specific. The spec says `TEXT DEFAULT ...` with an ellipsis -- no concrete PG default expression is given. Options:
- **Go-side**: Generate ISO 8601 before INSERT (no DB default needed, but loses the "default at DB level" property)
- **DB-side**: Complex expression like `to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS".000Z"')`
- **Default NULL + Go-side fill**: Always populate in application code

The preferred approach is not specified, yet it fundamentally affects whether `created_at` columns have a DB-level DEFAULT or rely entirely on application code.

### M5. MySQL dialect deliberately omitted but not stated as such
TS supports 3 dialects: SQLite, MySQL, PG. The Go module structure (`store/sqlite/`, `store/postgres/`) and all text in the spec only cover SQLite + PG. The spec never explicitly states "MySQL support is dropped from the Go port." This should be a conscious, documented decision with rationale.

### M6. `token_routes` and `events` constraint columns misleadingly blank
**Spec line**: table rows 9 and 27 -- both show "`—`"
- `token_routes` TS source defines 2 indexes: `modelPatternIdx` on (model_pattern) and `enabledIdx` on (enabled)
- `events` TS source defines 3 indexes: `readCreatedIdx`, `typeCreatedIdx`, `createdAtIdx`

The "`—`" is accurate if interpreted strictly as "no UNIQUE or FK constraints," but plain indexes are still constraints that matter for query performance. The blank entry is misleading.

### M7. `downstream_api_keys` late-added columns not mentioned
The TS source defines 20 columns. The initial migration in `ensureDownstreamApiKeySchema()` (index.ts lines 561-609) covers only 16 columns. Two columns were added later:
- `excluded_site_ids` (JSON array<number>)
- `excluded_credential_refs` (JSON array<DownstreamExcludedCredentialRef>)

The Go spec should note whether these columns must exist (check production DB state) and whether the initial migration creates them.

### M8. `schemaContract.json` logical types not covered by spec's type mapping
The `schemaContract.json` uses these `logicalType` values: `integer`, `text`, `real`, `boolean`, `datetime`, `json`.
The spec's type mapping covers: `integer`, `text`, `real`, `integer(mode:'boolean')`.
Missing explicit mappings for: `datetime` (stored as TEXT) and `json` (stored as TEXT).

While both are TEXT in storage, the Go code needs to know marshalling semantics:
- `datetime` fields: Go `string` with ISO 8601 format -- spec covers this in the date convention section
- `json` fields: Go `string` (raw JSON) or `json.RawMessage` -- spec does not specify

### M9. SQLite WAL mode and `foreign_keys` PRAGMA not mentioned
TS `index.ts` (lines 1356-1357) enables at connection open:
```ts
sqlite.pragma('journal_mode = WAL');
sqlite.pragma('foreign_keys = ON');
```
The Go spec does not mention either pragma. Without WAL mode, concurrent reads block on writes (SQLite default is DELETE journal mode, which serializes all access). Without `foreign_keys = ON`, FK constraints are silently ignored by SQLite (they are OFF by default).

### M10. `site_api_endpoints` cooldown index not mentioned
The `site_api_endpoints` table has a `cooldown_until` column and a composite index `siteCooldownIdx` on (site_id, cooldown_until). This index is critical for the cooldown-aware endpoint selection logic in the proxy path. The spec table only documents the UNIQUE(site_id, url) constraint, omitting this performance-critical index.

### M11. `model_availability.is_manual` and `token_model_availability` late additions
The TS `migrate.ts` `VERIFIED_SCHEMA_MARKERS` array shows that `model_availability.is_manual` was added in migration 0009 and `account_tokens.value_status` was added in migration 0012. The spec does not distinguish between "initial schema" columns and "later-added" columns. For Go DDL that creates tables from scratch, this distinction is irrelevant, but it matters for pre-migration compatibility checks on existing Drizzle DBs.

---

## Edge Cases Not Covered

### E1. Compatibility with Drizzle-journaled production DBs
The spec says "CREATE TABLE IF NOT EXISTS (idempotent)" and "不做 Drizzle 式的 journal/自修复 recovery loop." However, production DBs (us1, hk2) were created through Drizzle's incremental migration system. Key concerns:

1. **Migration journal table**: Production DBs contain `__drizzle_migrations` table. The Go app will see it exists and ignore it.
2. **Partial migrations**: If a Drizzle migration only partially applied (e.g. added a column but not the next migration's index), `CREATE TABLE IF NOT EXISTS` won't fix the missing index. The TS `backfillMissingRecordedMigrations` and `recoverMigrationSequence` handle this.
3. **Column drift**: If a column was added later via `ALTER TABLE`, `CREATE TABLE IF NOT EXISTS` won't add it. The TS `ensure*Schema()` functions handle column-level drift.

**Recommendation**: Add a pre-migration compatibility check that verifies all expected columns/indexes exist on existing DBs, or document that the Go port requires a fresh migration (export/import cycle).

### E2. PG schema upgrade path not specified
The TS `runtimeSchemaBootstrap.ts` has a sophisticated diff-based upgrade system:
- Reads `schemaContract.json` as the desired state
- Introspects the live PG schema via `information_schema`
- Builds a compatible baseline from matching columns/indexes/uniques/FKs
- Generates incremental DDL (ALTER, ADD COLUMN, CREATE INDEX)

The Go spec says `CREATE TABLE IF NOT EXISTS` which works for green-field PG but not for PG DBs that already have tables from a previous run. If a Go developer adds a new column and redeploys, the column won't be created.

### E3. `sites_platform_url_unique` index conflict with legacy duplicates
TS `migrate.ts` has `deduplicateLegacySitesForUniqueIndex()` (lines 582-635) which:
- Finds duplicate (platform, url) pairs in sites
- Merges duplicates: rebinds accounts, merges disabled_models, rewrites site_weight_multipliers, deletes duplicate site rows
- Runs inside a transaction with a retry budget of 64 attempts

The Go spec has no equivalent. Production DBs may have legacy duplicates from before the unique index was added. A naive `CREATE UNIQUE INDEX` will fail with a duplicate key error on existing DBs.

### E4. Concurrent `Open()` -- singleton/lock protection
**Spec Edge Cases**: "并发 Open → 单例/锁保护"
TS does NOT have explicit singleton protection. In TS, `activeDb = initDb()` runs once at module load (synchronous), and `switchRuntimeDatabase()` handles transitions. Multiple concurrent `Open()` calls are not a concern in the TS architecture because `initDb()` is called exactly once during import. The Go spec adds this requirement without specifying the implementation pattern (sync.Once, mutex, or package-level init).

### E5. PG connection failure error message format
**Spec Edge Cases**: "PG 连接失败 → 清晰报错 (含 host/dbname), 不要 panic trace"
TS throws a generic `Error('DB_URL is required when DB_TYPE=postgres')` but does not parse host/dbname from the connection string for detailed error messages. The spec adds a stricter requirement than TS. pgx's connection error already includes host/port/dbname -- the Go code simply needs to ensure this error is surfaced cleanly without a raw panic stack trace.

### E6. SQLite file path with spaces/Chinese characters
**Spec Edge Cases**: "SQLite 文件路径含空格/中文 → 正确处理"
TS `resolveSqlitePath()` uses `decodeURIComponent()` for `file://` URIs and `resolve()` for raw paths. modernc.org/sqlite uses Go's `os.Open` internally, which handles Unicode paths correctly on Windows. This should work but requires explicit test coverage.

### E7. Large production DB file handling
Production `hub.db` files on us1/hk2 may be large (potentially GB-scale with proxy_logs). The spec does not address:
- Connection pool sizing for SQLite (only one writer, WAL readers)
- Busy timeout configuration
- PRAGMA cache_size or mmap_size for large files
- Vacuum/optimize strategy for the legacy `__drizzle_migrations` table

### E8. `DB_URL` is `:memory:` for SQLite -- PG equivalent
**Spec Edge Cases**: "DB_URL=:memory: → 内存 SQLite (测试用)"
The spec should also define the PG equivalent for testing. In TS, tests use SQLite in-memory or temp files. For PG tests, the spec references pg testcontainers. There is no "in-memory PG" equivalent -- tests must use a real PG instance or testcontainers. The distinction between SQLite `:memory:` (zero setup) and PG testcontainers (Docker dependency) affects CI pipeline design.

---

## Incorrect Details

### I1. `proxy_logs` index count description is imprecise
**Spec line**: table row 14 -- "7 composite indexes on created_at"
TS source (`schema.ts` lines 267-275) defines 7 indexes, but only **6** are composite (pairing another column with created_at):
1. `createdAtIdx` -- single column on (created_at) -- NOT composite
2. `accountCreatedIdx` -- composite on (account_id, created_at)
3. `statusCreatedIdx` -- composite on (status, created_at)
4. `modelActualCreatedIdx` -- composite on (model_actual, created_at)
5. `downstreamKeyCreatedIdx` -- composite on (downstream_api_key_id, created_at)
6. `clientAppCreatedIdx` -- composite on (client_app_id, created_at)
7. `clientFamilyCreatedIdx` -- composite on (client_family, created_at)

The description should say "6 composite indexes on (column, created_at) + 1 single-column index on created_at."

---

## Summary

| Category | Count |
|---|---|
| Accuracy Issues (FK omissions, type ambiguity) | 6 |
| Missing Details (columns, indexes, defaults, pragmas, MySQL) | 11 |
| Edge Cases Not Covered (Drizzle compat, PG upgrade, dedup, concurrency) | 8 |
| Incorrect Details (imprecise index count) | 1 |
| **Total findings** | **26** |

### Verdict: **NEEDS_REVISION**

The spec correctly captures: the 27-table inventory, the high-level constraint patterns, the dialect split (SQLite+PG), and the date-as-ISO-8601-strings convention. However, it is a **skeleton spec** that cannot be implemented without continuous reference to TS source files.

**Blocking gaps:**
1. No column definitions for any table -- a Go developer cannot write a single `CREATE TABLE` statement from the spec alone.
2. No index definitions -- only UNIQUE constraints are listed; 60+ plain indexes omitted.
3. No FK ON DELETE actions -- CASCADE vs SET NULL matters for data integrity.
4. Production DB compatibility unaddressed -- existing Drizzle-journaled DBs with partial migration state.
5. No PG incremental upgrade path -- `CREATE TABLE IF NOT EXISTS` does not handle schema evolution.

**Recommended fixes before implementation:**
1. Add a full column appendix per table, or formally declare `schemaContract.json` as the canonical input with documented mechanical translation rules to Go structs and DDL.
2. Add a complete index inventory per table (beyond just UNIQUE constraints).
3. Specify FK ON DELETE actions explicitly for every FK.
4. Specify PG default expressions for datetime columns (Go-side vs DB-side fill).
5. Add a pre-migration compatibility section covering existing Drizzle DBs (column drift, `__drizzle_migrations` table, legacy deduplication).
6. Add WAL mode and foreign_keys PRAGMA to the SQLite initialization requirements.
7. Define a PG incremental upgrade strategy (ALTER-safe idempotency for evolving schemas).
