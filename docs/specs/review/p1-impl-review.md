# P1 Implementation Review

**Date**: 2026-07-04
**Reviewer**: Claude Code (cross-reference audit)
**Scope**: Go store layer vs spec (`p1-database.md`) vs authoritative TS schema (`schemaContract.json`, `schema.ts`)
**Reviewed files**: `store/schema.go`, `store/migrate.go`, `store/open.go`, `store/settings.go`, `store/setting_store.go`, `store/bootstrap.go`, `store/switch.go`, all `store/*_test.go`

---

## Summary

The implementation is **high-quality overall**. All 27 tables, 19 FKs (with the critical SET NULL), all UNIQUE constraints, CHECK constraints, and type mappings are correct. The PG path has all 67 non-UNIQUE indexes. The one blocking issue is that **SQLite non-UNIQUE indexes (67 total) are never created**.

---

## Assessment Matrix

| Criterion | Result | Notes |
|:---|:---|:---|
| 27 tables with correct columns | PASS | All 27 tables present with correct column counts and types |
| FK ON DELETE actions (19 total) | PASS | 18 CASCADE + 1 SET NULL all correct |
| Type mappings (bool, real, datetime) | PASS | SQLite: INTEGER 0/1, REAL; PG: BOOLEAN, DOUBLE PRECISION; all datetimes TEXT |
| Timestamps ISO 8601 strings | PASS | All datetime Go fields are `string`/`*string`, DDL uses TEXT |
| CREATE TABLE IF NOT EXISTS | PASS | All DDL uses `IF NOT EXISTS`, idempotency tested |
| pure Go SQLite driver | PASS | `modernc.org/sqlite`, no CGO |
| Text-PK tables (2) | PASS | settings (key) and analytics_projection_checkpoints (projector_key) correct |
| UNIQUE constraints (17) | PASS | All 17 faithfully reproduced as CONSTRAINT ... UNIQUE |
| CHECK constraints (3) | PASS | site_day_usage (8 cols), site_hour_usage (8 cols), model_day_usage (7 cols) |
| PG indexes (67 non-UNIQUE) | PASS | All 67 created via `CREATE INDEX IF NOT EXISTS` |
| **SQLite indexes (67 non-UNIQUE)** | **FAIL** | **0 of 67 created -- see Issue I1** |
| SQLite WAL + foreign_keys PRAGMA | PASS | Both enabled on open |
| PG connection pool (max=20) | PASS | Matches TS behavior |
| SettingsStore KV operations | PASS | Set/Get/GetAll with dialect-aware upsert |
| Runtime database switch | PASS | SwitchDatabase() with rollback-on-failure |
| Go struct `db` tags | PASS | All 27 structs have consistent snake_case tags |

---

## Issues Found

### I1. [HIGH] SQLite non-UNIQUE indexes not created

**File**: `store/migrate.go`, function `AutoMigrate()`, lines 78-81

**The problem**: The migration code has this logic:

```go
// Indexes are created inside CREATE TABLE on SQLite.
// On PostgreSQL we create indexes separately to match the TS pattern.
if dialect == DialectPostgres {
    migrations = append(migrations, buildPostgresIndexes()...)
}
```

The comment "_Indexes are created inside CREATE TABLE on SQLite_" is **factually incorrect**. SQLite's `CREATE TABLE` syntax does **not** support inline non-UNIQUE index definitions (only `CONSTRAINT ... PRIMARY KEY` and `CONSTRAINT ... UNIQUE` create implicit indexes). All 67 plain (non-UNIQUE) indexes that the spec and `schemaContract.json` define are **never created** on SQLite.

**Impact**: On SQLite, the following critical indexes are missing:

- `proxy_logs_created_at_idx` -- every proxy_logs query ordered/filtered by time does a full table scan
- `accounts_site_id_idx` -- account lookups by site do full scans
- `accounts_oauth_identity_idx` -- OAuth identity lookups do full scans
- `checkin_logs_account_created_at_idx` -- checkin history queries degrade
- `route_channels_route_enabled_idx` -- channel selection degrades
- All 63 other plain indexes from the spec

**Fix**: Append SQLite index creation statements analogous to `buildPostgresIndexes()`:

```go
if dialect == DialectSQLite {
    migrations = append(migrations, buildSQLiteIndexes()...)
}
```

The `buildSQLiteIndexes()` function should be identical to `buildPostgresIndexes()` but using `CREATE INDEX IF NOT EXISTS` (SQLite supports this syntax since 3.3.0).

**Severity**: HIGH -- causes full table scans on all common queries in SQLite mode.

---

### I2. [MEDIUM] Spec column-count headings are inaccurate (4 tables)

**File**: `docs/specs/p1-database.md` (not Go code)

The spec headings state incorrect column counts for 4 tables, though the actual column listings are correct:

| Table | Spec heading | Actual columns (spec listing) | Go struct fields | Correct count |
|:---|:---|:---|:---|:---|
| proxy_logs | 列 (23) | 24 listed | 24 | 24 |
| proxy_debug_traces | 列 (24) | 26 listed | 26 | 26 |
| proxy_files | 列 (12) | 13 listed | 13 | 13 |
| proxy_video_tasks | 列 (14) | 15 listed | 15 | 15 |
| site_announcements | 列 (18) | 19 listed | 19 | 19 |
| site_day_usage | 列 (12) | 13 listed | 13 | 13 |
| site_hour_usage | 列 (12) | 13 listed | 13 | 13 |

The Go implementation matches the **actual column listings** in the spec, not the headings. The spec headings should be corrected for documentation accuracy.

**Severity**: MEDIUM -- The Go code is correct; the spec has documentation errors. No code change needed but spec should be fixed to avoid confusion.

---

### I3. [MEDIUM] Index count discrepancy: spec says 65, actual is 67

**File**: `docs/specs/p1-database.md`, line 983

The spec states "共 65 个 plain 索引 (不含 UNIQUE 约束索引)." However, the actual number of non-UNIQUE indexes in `schemaContract.json` is **67** (84 total index entries minus 17 UNIQUE = 67 non-UNIQUE). The Go code (`buildPostgresIndexes()`) correctly creates all 67.

Tables with more plain indexes than spec counted:
- The discrepancy appears to be a simple counting error in the spec. The Go code faithfully follows `schemaContract.json` which is the authoritative source.

**Severity**: MEDIUM -- Code is correct; spec count is wrong.

---

### I4. [LOW] `setting_store.go` Get() uses `?` placeholder unconditionally

**File**: `store/setting_store.go`, line 23

```go
err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
```

While `pgx/v5` stdlib does support both `?` and `$1` styles (via the simple protocol in `database/sql` mode), this is inconsistent with the `Set()` method which explicitly switches between `?` (SQLite) and `$1/$2` (PG). For consistency and to avoid potential issues with pgx configuration changes, the `Get()` method should use dialect-aware placeholders like `Set()` does.

**Severity**: LOW -- Works in practice with pgx/v5 but is a consistency/maintenance concern.

---

### I5. [LOW] No `busy_timeout` verification in SQLite test

**File**: `store/open.go`, lines 154-156

The `busy_timeout` PRAGMA is applied but its failure is only logged as a warning. The SQLite test `TestSQLiteOpenMemory` verifies `journal_mode` and `foreign_keys` but not `busy_timeout`. Consider adding a `PRAGMA busy_timeout` readback assertion.

**Severity**: LOW -- Not a correctness issue; the PRAGMA is best-effort.

---

### I6. [LOW] Directory structure diverges from spec

**File**: N/A (directory layout)

The spec (p1-database.md lines 26-40) proposes:

```
store/
  schema.go
  open.go
  dialect.go        ← spec names this, implementation uses settings.go
  migrate.go
  sqlite/
    driver.go       ← not created; merged into open.go
    migrate.go      ← not created; merged into migrate.go
  postgres/
    driver.go       ← not created; merged into open.go
    migrate.go      ← not created; merged into migrate.go
  db.go             ← not created; DB type is in open.go
  setting_store.go
```

The actual structure is flat (no `sqlite/` or `postgres/` subdirectories):
```
store/
  schema.go         (27 structs)
  open.go           (Open, DB type, ResolveSQLitePath, PRAGMAs, pool config)
  migrate.go        (all 27 DDL builders + PG indexes + Migrate stub)
  settings.go       (runtime settings hydration, not in spec layout -- replaces dialect.go)
  setting_store.go  (SettingsStore KV)
  bootstrap.go      (EnsureRuntimeDatabase, CloseDatabase, GetDB)
  switch.go         (SwitchDatabase with rollback)
```

This is acceptable given the current codebase size, but if the migrate.go file grows significantly (it is already 1289 lines), splitting into dialect-specific files would improve maintainability.

**Severity**: LOW -- Does not affect correctness; spec vs implementation divergence should be reconciled.

---

## Verification: Cross-Reference Details

### All 19 FKs verified

| # | Table | Column | References | ON DELETE | Go (PG inline) | Go (SQLite FK clause) |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | accounts | site_id | sites(id) | CASCADE | PASS | PASS |
| 2 | account_tokens | account_id | accounts(id) | CASCADE | PASS | PASS |
| 3 | checkin_logs | account_id | accounts(id) | CASCADE | PASS | PASS |
| 4 | model_availability | account_id | accounts(id) | CASCADE | PASS | PASS |
| 5 | model_day_usage | site_id | sites(id) | CASCADE | PASS | PASS |
| 6 | oauth_route_units | site_id | sites(id) | CASCADE | PASS | PASS |
| 7 | oauth_route_unit_members | unit_id | oauth_route_units(id) | CASCADE | PASS | PASS |
| 8 | oauth_route_unit_members | account_id | accounts(id) | CASCADE | PASS | PASS |
| 9 | route_channels | route_id | token_routes(id) | CASCADE | PASS | PASS |
| 10 | route_channels | account_id | accounts(id) | CASCADE | PASS | PASS |
| 11 | route_channels | token_id | account_tokens(id) | **SET NULL** | PASS | PASS |
| 12 | route_group_sources | group_route_id | token_routes(id) | CASCADE | PASS | PASS |
| 13 | route_group_sources | source_route_id | token_routes(id) | CASCADE | PASS | PASS |
| 14 | site_announcements | site_id | sites(id) | CASCADE | PASS | PASS |
| 15 | site_api_endpoints | site_id | sites(id) | CASCADE | PASS | PASS |
| 16 | site_day_usage | site_id | sites(id) | CASCADE | PASS | PASS |
| 17 | site_disabled_models | site_id | sites(id) | CASCADE | PASS | PASS |
| 18 | site_hour_usage | site_id | sites(id) | CASCADE | PASS | PASS |
| 19 | token_model_availability | token_id | account_tokens(id) | CASCADE | PASS | PASS |
| 20 | proxy_debug_attempts | trace_id | proxy_debug_traces(id) | CASCADE | PASS | PASS |

Key verification: `route_channels.token_id` uses **ON DELETE SET NULL** in both PG (inline REFERENCES with SET NULL) and SQLite (FOREIGN KEY clause with SET NULL). The `route_channels.oauth_route_unit_id` has **no FK constraint** in both dialects, matching the TS schema exactly.

### SET NULL behavior tested

`TestSQLiteFKSetNull` (sqlite_test.go:237-294) verifies:
1. Insert site, account, account_token, token_route
2. Insert route_channel with token_id
3. Delete account_token
4. Assert route_channel survives with token_id=NULL

This is a critical test that validates the non-CASCADE FK behavior. PASS.

### PG type verification tests

- `TestPostgresDoublePrecision`: verifies `global_weight` is `double precision` not `real` -- PASS
- `TestPostgresBooleanType`: verifies `is_pinned` is `boolean` not `integer` -- PASS
- `TestPostgresDatetimeIsText`: verifies `created_at` is `text` not `timestamp` -- PASS

---

## Test Coverage Assessment

| Test file | What it covers | Coverage quality |
|:---|:---|:---|
| `schema_test.go` | 27 struct field counts, db tags, camelCase, text PK verification | GOOD |
| `migrate_test.go` | idempotency (double run), data persistence, UNIQUE enforcement, table structure | GOOD |
| `sqlite_test.go` | :memory: open, all 27 tables, CRUD, timestamps, FK CASCADE, FK SET NULL, CHECK constraints | GOOD (missing index verification) |
| `postgres_test.go` | connection, SSL, all 27 tables, CRUD, DOUBLE PRECISION, BOOLEAN, TEXT datetime, sample indexes, pool config, placeholders, concurrent access | GOOD |
| `setting_store_test.go` | Set/Get roundtrip, non-existent keys, overwrite, GetAll, GetAll empty, JSON values, NULL values, large values | GOOD |
| `dialect_test.go` | ISO 8601 format, btype/rtype/serialPK/textPK helpers, isPG, path resolution, dialect constants | GOOD |

**Missing test**: No test verifies that all 67 non-UNIQUE indexes exist in SQLite after migration. The existing `TestAutoMigrateIndexesExist` only checks total index count (>0), which passes because UNIQUE constraints create implicit indexes.

---

## Recommendation

**NEEDS_FIX** -- The implementation is close to passing but has one blocking defect:

**Fix I1 before merge**: Add `buildSQLiteIndexes()` function and call it from `AutoMigrate()` for the SQLite dialect. This is a straightforward addition: the function can be nearly identical to `buildPostgresIndexes()` since SQLite supports `CREATE INDEX IF NOT EXISTS`.

After fixing I1, all acceptance criteria from the spec are met. The implementation faithfully reproduces the TS schema with correct dialect-specific type mappings, FK semantics, and constraints.

**Minor follow-ups** (not blocking):
- Fix spec column-count headings (I2) and index count (I3)
- Add dialect-aware placeholder to `SettingsStore.Get()` (I4)
- Add `busy_timeout` verification to `TestSQLiteOpenMemory` (I5)
- Consider splitting `migrate.go` into dialect-specific files when it grows (I6)
