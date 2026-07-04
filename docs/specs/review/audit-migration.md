# MetAPI Go Migration Audit: SQLite to PostgreSQL Data Correctness

**Date**: 2026-07-04
**Audited files**:
- `D:/Code/TokenDance/metapi-go/cmd/migrate/main.go` (Go, 1191 lines)
- `D:/Code/TokenDance/metapi/src/server/services/databaseMigrationService.ts` (TS reference, 859 lines)
- `D:/Code/TokenDance/metapi/src/server/db/generated/schemaContract.json` (canonical schema contract, 3312 lines)

---

## 1. Type Conversion Accuracy

### 1.1 Boolean (SQLite INTEGER 0/1 to PG BOOLEAN)

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| SQLite read | better-sqlite3 returns JS `number` (0/1) | modernc/sqlite returns `int64` (0/1) via `interface{}` scan | Compatible |
| Conversion logic | `asBoolean`: number `!== 0` | `asBoolean`: `int64 != 0` | Matches |
| PG insert | `buildInsertSql` converts to native `boolean` for sqlite only; for postgres passes JS boolean as-is | `buildInsertPG` passes Go `bool` directly to pgx parameter | Correct via pgx |
| String coercion | handles `"1"/"true"/"yes"/"on"` and `"0"/"false"/"no"/"off"` | Same strings, case-insensitive | Matches |
| NULL fallback | uses `fallback` parameter | Same | Matches |

**Verdict: PASS.** Boolean conversion is correct and consistent.

### 1.2 REAL to DOUBLE PRECISION

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| SQLite read | better-sqlite3 returns JS `number` (IEEE 754 double) | modernc returns `float64` | Compatible |
| Conversion | `asNumber`: `Number(value)`, check `Number.isFinite` | `asNumber`: `float64(val)` for int64, `strconv.ParseFloat` for string | Matches |
| String parsing | `Number("3.14")` yields 3.14 | `strconv.ParseFloat("3.14", 64)` yields 3.14 | Matches |
| Invalid string | `Number("abc")` = NaN, `isFinite` false, returns `fallback` | `ParseFloat` error, returns `fallback` | Matches |
| NULL fallback | `null` (JS) | `nil` (Go interface{}) | Matches via pgx NULL |

**Verdict: PASS.** REAL conversion is correct.

### 1.3 Datetime (TEXT to TEXT)

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| Handling | `asNullableString`: `String(value)` for non-null | `asNullableString`: `fmt.Sprintf("%v", v)` for non-null | Both pass datetime strings through as-is |
| NULL | `null` | `nil` | Matches |

**Verdict: PASS.** Datetime passthrough is correct.

### 1.4 JSON Column Serialization

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| Column identification | Dynamic via `schemaContract.json` `logicalType: "json"` | Hardcoded `jsonColumnSet` map (13 entries) | Go matches contract; fragile if contract changes |
| NULL handling | `null` | `nil` | Matches |
| Already-string value | Return as-is | Return as-is | Matches |
| Non-string value | `JSON.stringify(value)` | `json.Marshal(v)` | **See Issue 1** |
| HTML escaping | JS: no escaping of `<`, `>`, `&` | Go: `json.Marshal` escapes `<` to `<`, `>` to `>`, `&` to `&` | **Divergence** |

**ISSUE 1 [MEDIUM] -- JSON serialization HTML-escaping mismatch.** Go's `json.Marshal` HTML-escapes `<`, `>`, `&` by default, while JavaScript's `JSON.stringify` does not. If any JSON column data contains these characters (e.g., HTML content in `billing_details`, model names with `&` in `model_mapping`), Go and TS produce different serialized output. In practice, for the data types stored in these columns (model configurations, billing metadata, route decisions), these characters are unlikely to appear. However, the difference means that a checksum comparison between Go-migrated and TS-migrated databases could fail due to this encoding difference.

**Recommended fix**: Use `json.NewEncoder(&buf).SetEscapeHTML(false)` instead of `json.Marshal`:

```go
func serializeJSONValue(v interface{}) interface{} {
    if v == nil {
        return nil
    }
    if s, ok := v.(string); ok {
        return s
    }
    var buf bytes.Buffer
    enc := json.NewEncoder(&buf)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(v); err != nil {
        return nil
    }
    // Encode appends a newline; trim it
    return strings.TrimSpace(buf.String())
}
```

Note: `bytes` is already imported in main.go.

### 1.5 Settings Value Serialization

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| Source data | `parseSettingValue`: tries `JSON.parse`, falls back to raw string | Raw SQLite scan value via `interface{}` | Different read path |
| Serialization for PG | `toJsonString`: `JSON.stringify(value ?? null)` | `asNullableString`: `fmt.Sprintf("%v", v)` | **Divergence** |
| Plain string "hello" | Stored as `"hello"` (JSON-encoded with quotes) | Stored as `hello` (raw string) | Different |
| Object `{"foo":"bar"}` | Stored as `{"foo":"bar"}` (re-serialized) | Stored as `{"foo":"bar"}` (via fmt.Sprintf of string) | Same for objects already stored as JSON text |
| Integer 42 | Stored as `42` | Stored as `42` | Same for primitives |
| NULL | Stored as `null` (JSON null as text) | Stored as `nil` (SQL NULL) | **Different** |

**ISSUE 2 [HIGH] -- Settings value serialization divergence.** The TS implementation JSON-encodes all setting values (producing `"hello"` for a plain string), while Go passes values through as their string representation (`hello` without quotes). This means downstream code reading `settings.value` from a Go-migrated database will see different format than from a TS-migrated database.

**Impact**: If application code calls `JSON.parse()` on settings values read from the database, Go-migrated plain-string values will fail to parse (no surrounding quotes), while TS-migrated values will parse correctly. For example:
- TS-migrated: `settings.value = '"hello"'` -> `JSON.parse` -> `"hello"` (works)
- Go-migrated: `settings.value = 'hello'` -> `JSON.parse` -> SyntaxError (fails)

**Recommended fix**: Match the TS behavior by JSON-encoding settings values:

```go
func buildSettings(rows []map[string]interface{}) []insertStmt {
    cols := []string{"key", "value"}
    var stmts []insertStmt
    for _, row := range rows {
        key := asString(v(row, "key"))
        if runtimeDBSettingKeys[key] {
            continue
        }
        // Match TS: toJsonString(value ?? null)
        raw := v(row, "value")
        var serialized string
        if raw == nil {
            serialized = "null"
        } else if s, ok := raw.(string); ok {
            // Try to re-parse and re-serialize to match TS behavior
            // (TS does JSON.parse then JSON.stringify for settings)
            b, err := json.Marshal(s)
            if err == nil {
                serialized = string(b)
            } else {
                serialized = "null"
            }
        } else {
            b, err := json.Marshal(raw)
            if err != nil {
                serialized = "null"
            } else {
                serialized = string(b)
            }
        }
        stmts = append(stmts, insertStmt{
            table: "settings", columns: cols,
            values: []interface{}{key, serialized},
        })
    }
    return stmts
}
```

---

## 2. NULL Handling

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| `asNullableString` | `null` for null/undefined, `String(value)` otherwise | `nil` for nil, `fmt.Sprintf("%v", v)` otherwise | Matches |
| `asNumber` fallback type | `number \| null` | `interface{}` (passes `nil`) | Matches via pgx |
| `asBoolean` | Always returns `boolean`, controlled by fallback param | Always returns `bool`, controlled by fallback param | Matches |
| JSON columns | `null` returned for nil | `nil` returned for nil | Matches |
| Insert params | pg driver maps JS `null` to SQL NULL | pgx maps Go `nil` to SQL NULL | Matches |
| Settings value NULL | `JSON.stringify(null)` = `"null"` (text) | `nil` (SQL NULL) | **Already covered in Issue 2** |

**Verdict: PASS.** NULL handling is correct except for the settings serialization issue already documented.

---

## 3. Auto-Increment ID Preservation

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| ID included in INSERT | Yes, as first column with `asNumber(id, 0)` | Yes, as first column with `asNumber(id, 0)` | Matches |
| Sequence sync | `setval(pg_get_serial_sequence(t, 'id'), COALESCE(MAX(id), 1), TRUE)` | Same exact SQL | Matches |
| Tables synced | 17 tables (all except settings) | 17 tables (all except settings) | Matches |
| Empty table handling | `COALESCE(MAX(id), 1)` resets to 1 | Same | Matches |
| `pg_get_serial_sequence` null handling | Error swallowed (no check) | Warning printed, continues | Go is more robust |

**Verdict: PASS.** ID preservation is correct and both implementations are consistent. Go's logging of sequence sync failures is a minor improvement over TS's silent error swallowing.

---

## 4. Foreign Key Constraint Satisfaction

### 4.1 FK Relationships (from schemaContract.json)

The schema contract defines 20 FK relationships. The 18 migrated tables participate in these:

| Child Table | FK Column | Parent Table | On Delete |
|-------------|-----------|--------------|-----------|
| account_tokens | account_id | accounts | CASCADE |
| accounts | site_id | sites | CASCADE |
| checkin_logs | account_id | accounts | CASCADE |
| model_availability | account_id | accounts | CASCADE |
| route_channels | account_id | accounts | CASCADE |
| route_channels | route_id | token_routes | CASCADE |
| route_channels | token_id | account_tokens | SET NULL |
| route_group_sources | group_route_id | token_routes | CASCADE |
| route_group_sources | source_route_id | token_routes | CASCADE |
| site_announcements | site_id | sites | CASCADE |
| site_api_endpoints | site_id | sites | CASCADE |
| site_disabled_models | site_id | sites | CASCADE |
| token_model_availability | token_id | account_tokens | CASCADE |

### 4.2 Insert Order Analysis

Go insert order (`buildStatements`):
1. sites (no FK deps)
2. site_api_endpoints (FK -> sites) OK
3. site_disabled_models (FK -> sites) OK
4. site_announcements (FK -> sites) OK
5. accounts (FK -> sites) OK
6. account_tokens (FK -> accounts) OK
7. checkin_logs (FK -> accounts) OK
8. model_availability (FK -> accounts) OK
9. token_model_availability (FK -> account_tokens) OK
10. token_routes (no FK deps)
11. route_channels (FK -> accounts, token_routes, account_tokens) OK (all parents precede)
12. route_group_sources (FK -> token_routes) OK
13. proxy_logs (no declared FK)
14. proxy_video_tasks (no declared FK)
15. proxy_files (no declared FK)
16. downstream_api_keys (no declared FK)
17. events (no declared FK)
18. settings (no declared FK)

**Verdict: PASS.** The insert order satisfies all FK constraints. All child tables are inserted after their parent tables. Note that `proxy_logs`, `proxy_video_tasks`, `proxy_files`, `downstream_api_keys`, and `events` have no FK constraints defined in the schema contract.

### 4.3 Delete Order Analysis

Go clear order (`clearOrder`):
```
route_channels -> route_group_sources -> token_model_availability -> model_availability -> checkin_logs -> proxy_logs -> proxy_video_tasks -> proxy_files -> account_tokens -> accounts -> site_announcements -> site_disabled_models -> site_api_endpoints -> token_routes -> sites -> downstream_api_keys -> events -> settings
```

This is children-before-parents: all FK children are deleted before their parents. `route_channels` (references `accounts`, `token_routes`, `account_tokens`) is first. `accounts` comes after all its children. `sites` comes after all site-dependents.

**Verdict: PASS.** FK-safe delete order is correct.

---

## 5. Batch Size and Transaction Boundaries

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| Transaction scope | `begin -> clear -> insert all -> sync seq -> commit` | `Begin -> clear -> insert all -> sync seq -> Commit` | Matches |
| Rollback on error | `catch { rollback; throw }` | `defer tx.Rollback()` (no-op after commit) | Matches |
| Insert granularity | Row-by-row `for...await execute` | Row-by-row `for...tx.Exec` | Matches |
| Batch size flag | N/A | `--batch-size` flag parsed but **never used** | **Issue 3** |

**ISSUE 3 [LOW] -- Dead `--batch-size` flag.** The `--batch-size` flag is defined (line 47) and passed to `runMigration` (line 1183) but never referenced in the insert loop (lines 321-331). All rows are always inserted one at a time. This is a dead code/configuration issue.

**Recommended fix**: Either remove the flag or implement batching by grouping consecutive INSERTs for the same table into multi-row VALUES clauses:

```go
// Group inserts by table, then batch
grouped := make(map[string][]insertStmt)
for _, stmt := range inserts {
    grouped[stmt.table] = append(grouped[stmt.table], stmt)
}
for table, stmts := range grouped {
    for i := 0; i < len(stmts); i += batchSize {
        end := i + batchSize
        if end > len(stmts) {
            end = len(stmts)
        }
        batch := stmts[i:end]
        // Build multi-row INSERT
        if err := executeBatch(tx, table, batch); err != nil {
            return err
        }
    }
}
```

---

## 6. Table Order (FK Dependencies)

Both implementations use identical logical ordering. The Go version correctly follows the parent-before-child insertion pattern that the TS version establishes.

**Verdict: PASS** for FK ordering. However, see Issue 4 below regarding column count mismatches.

---

## 7. Column Count Comparison Against Schema Contract

Comparing both implementations against `schemaContract.json` (the canonical schema):

### 7.1 Go version vs Schema Contract

| Table | Contract columns | Go columns | Match |
|-------|-----------------|------------|-------|
| sites | 19 | 15 (subset) | Go intentionally excludes `post_refresh_probe_*` columns (not in snapshot) |
| site_api_endpoints | 11 | 11 | Exact |
| site_announcements | 17 | 19 | **ISSUE 4** |
| site_disabled_models | 4 | 4 | Exact |
| accounts | 21 | 19 | Go excludes `oauth_provider`, `oauth_account_key`, `oauth_project_id` |
| account_tokens | 11 | 11 | Exact |
| checkin_logs | 6 | 6 | Exact |
| model_availability | 7 | 7 | Exact |
| token_model_availability | 6 | 6 | Exact |
| token_routes | 12 | 12 | Exact |
| route_channels | 20 | 20 | Exact |
| route_group_sources | 3 | 3 | Exact |
| proxy_logs | 24 | 24 | Exact |
| proxy_video_tasks | 15 | 15 | Exact |
| proxy_files | 13 | 13 | Exact |
| downstream_api_keys | 20 | 20 | Exact |
| events | 9 | 9 | Exact |
| settings | 2 | 2 | Exact |

### 7.2 TS version vs Schema Contract (for reference)

| Table | Contract columns | TS columns | Missing in TS |
|-------|-----------------|------------|---------------|
| model_availability | 7 | 6 | `is_manual` |
| downstream_api_keys | 20 | 18 | `group_name`, `tags`, `excluded_site_ids`, `excluded_credential_refs` |
| route_channels | 20 | 19 | `oauth_route_unit_id` |
| proxy_logs | 24 | 18 | `is_stream`, `first_byte_latency_ms`, `client_family`, `client_app_id`, `client_app_name`, `client_confidence`, `billing_details` (TS has this) -- actually missing: `is_stream`, `first_byte_latency_ms`, `client_family`, `client_app_id`, `client_app_name`, `client_confidence` | Wait, let me recheck... TS proxy_logs has `downstream_api_key_id` and `billing_details`. Missing: `is_stream`, `first_byte_latency_ms`, `client_family`, `client_app_id`, `client_app_name`, `client_confidence`. That's 18 vs 24. |

**ISSUE 4 [LOW-MEDIUM] -- `site_announcements` has extra columns in Go not in contract.** The Go version inserts `created_at` and `updated_at` for `site_announcements`, but the schema contract does not define these columns. The Go `ensureTargetSchema` DDL creates them, so the INSERT will succeed, but this represents schema drift from the contract. The TS version correctly omits them (matching the contract). If the contract is later updated to add these columns, the field order must match Go's DDL to avoid migration issues.

Note that Go intentionally excludes `oauth_provider`, `oauth_account_key`, `oauth_project_id` from `accounts`, and `post_refresh_probe_*` from `sites` -- these are schema contract columns not present in the migration snapshot. This is intentional since migration only snapshots the 18 application tables.

**Key finding**: The TS `buildStatements` in `databaseMigrationService.ts` is **outdated** relative to the schema contract -- it is missing columns that the contract defines and that Go correctly handles (`is_manual` in model_availability, `oauth_route_unit_id` in route_channels, `group_name`/`tags` in downstream_api_keys, and 6 columns in proxy_logs). The Go version is the more complete implementation.

---

## 8. Checksum Verification

**ISSUE 5 [MEDIUM] -- Checksum verification is structurally unreliable.** The `--verify` mode (`verifyChecksums`, lines 1061-1087) compares row counts and SHA256 hashes between the in-memory SQLite snapshot and the re-read PostgreSQL data. This comparison is fragile:

1. **Type representation mismatch**: SQLite boolean values are scanned as `int64` (0/1) via `interface{}`, while pgx returns native Go `bool` (true/false). `fmt.Sprintf("%v", int64(1))` = `"1"`, but `fmt.Sprintf("%v", true)` = `"true"`. These will never match, causing false verification failures.

2. **JSONB representation**: PostgreSQL may return JSONB columns as `[]byte` or parsed types via pgx, while the in-memory snapshot stores them as Go strings from `json.Marshal`. The `fmt.Sprintf("%v", ...)` representations will differ.

3. **Numeric precision**: SQLite REAL (float64) and PG DOUBLE PRECISION (float64) should match, but `fmt.Sprintf("%v", ...)` may produce different decimal representations (e.g., `1.0` vs `1`).

4. **Setting values**: The different serialization approach (Issue 2) guarantees settings checksums will not match.

**Impact**: The `--verify` flag will likely report false failures even for correct migrations. It should not be relied upon for production validation.

**Recommended fix**: Instead of raw `fmt.Sprintf("%v", ...)`, implement type-normalized hashing:
```go
func normalizeForHash(v interface{}) string {
    switch val := v.(type) {
    case bool:
        if val { return "1" } else { return "0" }
    case int64:
        return strconv.FormatInt(val, 10)
    case float64:
        return strconv.FormatFloat(val, 'G', -1, 64)
    case []byte:
        return string(val)
    case nil:
        return "<NULL>"
    default:
        return fmt.Sprintf("%v", v)
    }
}
```
Apply this normalization in both `hashRows` and `hashPGTable`.

---

## 9. Dry-Run Mode

**Verdict: PASS (Go improvement).**

| Aspect | TS | Go |
|--------|----|-----|
| Dry-run capability | Not implemented | `--dry-run` flag |
| Behavior | N/A | Reads SQLite, builds all statements, prints per-table counts, exits without connecting to PG |
| Validation | N/A | Validates source/target URL syntax, checks SQLite is readable |

This is a useful Go addition with no TS equivalent. Implementation is correct.

---

## 10. Error Recovery on Partial Failure

| Aspect | TS | Go | Verdict |
|--------|----|----|---------|
| Transaction boundary | `client.begin()` ... `client.commit()` | `tx, _ := tgtDB.Begin()` ... `tx.Commit()` | Matches |
| Rollback | `catch { client.rollback(); throw }` | `defer tx.Rollback()` (safe no-op after Commit) | Matches |
| Atomicity guarantee | Full: all-or-nothing within transaction | Full: all-or-nothing within transaction | Matches |
| Schema creation outside TX | `ensureRuntimeDatabaseSchema` runs before `begin()` | `ensureTargetSchema` runs before `Begin()` | Matches |
| Empty tables on failure | Yes (schema survives, data does not) | Yes (same) | Matches |
| Partial insert recovery | Impossible (single TX rolls back entirely) | Impossible (same) | Matches |

**Verdict: PASS.** Both implementations use single-transaction atomicity correctly.

### Additional Go robustness

Go adds one resilience feature TS lacks: the `readAllTables` function gracefully handles missing tables by setting `snapshot[table] = nil` and continuing (line 443). This allows partial migration when some tables haven't been created yet in SQLite. The subsequent `build*` functions iterate over nil slices harmlessly (empty loops).

---

## 11. Additional Findings

### 11.1 `ensureTargetSchema` vs `ensureRuntimeDatabaseSchema`

The Go `ensureTargetSchema` (lines 1025-1057) embeds a minimal DDL for all 18 tables, while TS delegates to `ensureRuntimeDatabaseSchema` (from `runtimeSchemaBootstrap.ts`). The Go DDL is a good faith copy but may drift from the runtime schema bootstrap used by the main server binary.

- Go DDL uses `BIGSERIAL` for id columns and `BIGINT` for FK columns.
- The schema contract does not specify PG column types.
- If the actual runtime `store.Migrate()` uses different types (e.g., `INTEGER` instead of `BIGINT`), there could be type mismatches.

**Risk**: Low. The comment on line 1027-1028 acknowledges this: "The full schema is handled by store.Migrate() in the server binary. Here we only need the tables to exist for data insertion." As long as `store.Migrate()` runs before the migration tool writes data, the schemas will be consistent.

### 11.2 `downstream_api_key_id` column name handling in proxy_logs

TS line 599 handles both camelCase (`downstreamApiKeyId`) and snake_case (`downstream_api_key_id`) column access: `(row as any).downstreamApiKeyId ?? (row as any).downstream_api_key_id`. 

Go line 849 only accesses `"downstream_api_key_id"`. This is correct if the SQLite schema uses snake_case column names (which it does, per the schema contract). However, if migrating from a Drizzle-managed SQLite that uses camelCase, the Go version would silently get zero values.

**Risk**: Low in practice since the SQLite schema consistently uses snake_case.

---

## 12. Summary of Issues

| ID | Severity | Category | Description |
|----|----------|----------|-------------|
| **1** | **HIGH** | Settings serialization | Go uses `fmt.Sprintf("%v")` while TS uses `JSON.stringify`. Plain string values differ (`hello` vs `"hello"`). NULL settings differ (SQL NULL vs `"null"` text). Downstream `JSON.parse()` on Go-migrated plain strings will fail. |
| **2** | MEDIUM | JSON HTML escaping | Go `json.Marshal` escapes `<`, `>`, `&` to Unicode; JS `JSON.stringify` does not. Causes output mismatch if these characters appear in JSON data. |
| **3** | MEDIUM | Checksum fragility | `--verify` hash comparison uses `fmt.Sprintf("%v")` on mixed-type scans (SQLite int64 vs PG bool). Will produce false mismatches even for correct migrations. |
| **4** | LOW-MEDIUM | Schema drift | Go adds `created_at`/`updated_at` to `site_announcements` not in contract. Self-consistent within Go but diverges from contract and TS. |
| **5** | LOW | Dead code | `--batch-size` flag is accepted but never used. All inserts are row-by-row regardless of flag value. |

## 13. Summary of Go Improvements over TS

| Improvement | Description |
|-------------|-------------|
| Complete schema coverage | Go includes all columns from `schemaContract.json` (e.g., `is_manual`, `oauth_route_unit_id`, `group_name`/`tags`, proxy_logs client tracking). TS `buildStatements` is missing these. |
| Dry-run mode | `--dry-run` validates and reports without writing data. TS has no equivalent. |
| Checksum verification | `--verify` provides post-migration validation (though implementation needs fixing per Issue 3). |
| Graceful missing table handling | `readAllTables` skips tables that don't exist in SQLite. TS would crash on missing tables. |
| Sequence sync error logging | Go logs warnings for failed `setval` calls; TS silently swallows errors. |
| Progress reporting | `--progress` shows per-100-row progress. TS has no equivalent. |
| Password masking | `maskPassword` in summary output. TS has `maskConnectionString` but only uses it in summary. |

## 14. Overall Assessment

The Go migration tool is a faithful port of the TS `databaseMigrationService.ts` with several improvements (dry-run, verification, progress, better schema contract coverage). However, two correctness issues must be addressed before production use:

1. **HIGH: Fix settings value serialization** (Issue 1) to match TS behavior and prevent downstream JSON.parse failures.
2. **MEDIUM: Fix checksum verification normalization** (Issue 3) to produce reliable verification results.

The JSON HTML escaping (Issue 2) is unlikely to cause problems in practice but should be fixed for strict behavioral equivalence. The dead `--batch-size` flag (Issue 5) is a polish item.
