# PostgreSQL Dialect Leak Audit

**Date**: 2026-07-05  
**Scope**: `D:/Code/TokenDance/metapi-go/` -- all `.go` files  
**Goal**: Identify SQLite-specific SQL patterns that would fail or behave incorrectly on PostgreSQL.

---

## Summary

| Category | Count | Severity |
|---|---|---|
| `datetime('now')` | 1 | CRITICAL |
| `last_insert_rowid()` | 1 | CRITICAL |
| `LastInsertId()` without RETURNING/fallback (production) | 7 | CRITICAL |
| `LastInsertId()` without fallback (test-only) | Many | LOW |
| `INSERT OR IGNORE` / `INSERT OR REPLACE` | 2 | OK (dialect-switched) |
| PRAGMA statements | 7 | OK (SQLite-only code paths) |
| `AUTOINCREMENT` in DDL | 25 | OK (dialect-switched) |
| `||` for SQL string concatenation | 0 | N/A (not found) |
| `RETURNING` clause | 0 | N/A (not used) |
| Placeholder style leak (`?` in PG path) | 1 | HIGH |

---

## 1. CRITICAL: `datetime('now')` -- SQLite-specific datetime function

### Finding 1.1

- **File**: `service/oauth/refresh.go`
- **Line**: 140
- **Code**:
  ```sql
  UPDATE accounts SET access_token = ?, oauth_provider = ?, oauth_account_key = ?,
   oauth_project_id = ?, extra_config = ?, status = 'active', updated_at = datetime('now')
   WHERE id = ?
  ```
- **Issue**: `datetime('now')` is a SQLite-specific function. PostgreSQL does not have it. This query will fail with `function datetime(unknown) does not exist`.
- **Fix**: Replace `datetime('now')` with `NOW()` or use Go-side `time.Now().UTC().Format(time.RFC3339)` as a bound parameter (consistent with the rest of the codebase).

---

## 2. CRITICAL: `last_insert_rowid()` -- SQLite-specific function

### Finding 2.1

- **File**: `service/site_service.go`
- **Line**: 287
- **Code**:
  ```go
  siteID, err := result.LastInsertId()
  if err != nil {
      var id int64
      tx.Get(&id, "SELECT last_insert_rowid()")
      siteID = id
  }
  ```
- **Issue**: The fallback path uses `last_insert_rowid()` which is SQLite-specific. On PostgreSQL this query will fail with `function last_insert_rowid() does not exist`. When `LastInsertId()` fails on PG (which it will without a `RETURNING` clause), the fallback is also broken.
- **Fix**: Use the same pattern as `service/oauth/flow.go:494-498` and `service/oauth/route_unit.go:244-248` -- fall back to a `SELECT id FROM sites WHERE ... ORDER BY id DESC LIMIT 1` query.

---

## 3. CRITICAL: `result.LastInsertId()` without `RETURNING` clause or PG fallback

On PostgreSQL, `database/sql.Result.LastInsertId()` requires the INSERT statement to include a `RETURNING id` clause. Without it, `LastInsertId()` returns `(0, error)` or `(0, nil)` depending on the driver. The following locations call `LastInsertId()` on INSERTs that do not use `RETURNING` and have no PG fallback:

### Finding 3.1

- **File**: `service/account_service.go`
- **Line**: 301-322
- **Code**:
  ```go
  result, err := db.Exec(
      `INSERT INTO accounts (...) VALUES (?, ?, ...)`,
      ...,
  )
  if err != nil {
      return 0, err
  }
  id, err := result.LastInsertId()
  if err != nil {
      return 0, fmt.Errorf("failed to get inserted account id: %w", err)
  }
  ```
- **Fix**: Add dialect-aware logic -- use `RETURNING id` in the INSERT for PG, or add a fallback `SELECT` like `flow.go:494`.

### Finding 3.2

- **File**: `handler/admin/accounts.go`
- **Line**: 294-305
- **Code**:
  ```go
  result, err := h.db.Exec(
      `INSERT INTO accounts (...) VALUES (?, ?, ...)`,
      ...,
  )
  ...
  return result.LastInsertId()
  ```
- **Fix**: Same as 3.1.

### Finding 3.3

- **File**: `handler/admin/downstream_keys.go`
- **Line**: 213
- **Code**:
  ```go
  id, _ := result.LastInsertId()
  ```
- **Issue**: Error is silently ignored. On PG this returns 0, and the subsequent `SELECT * FROM downstream_api_keys WHERE id = 0` will return nil.
- **Fix**: Use `RETURNING id` or fallback SELECT.

### Finding 3.4

- **File**: `handler/admin/token_routes.go`
- **Line**: 243
- **Code**:
  ```go
  id, _ := result.LastInsertId()
  // For explicit_group, insert source route references
  if routeMode == "explicit_group" && len(body.SourceRouteIds) > 0 {
      for _, srcID := range body.SourceRouteIds {
          h.db.Exec("INSERT INTO route_group_sources (group_route_id, source_route_id) VALUES (?, ?)", id, srcID)
      }
  }
  ```
- **Issue**: On PG, `id` will be 0. Foreign key references to `group_route_id = 0` will either fail or create orphan records.

### Finding 3.5

- **File**: `handler/admin/token_routes.go`
- **Line**: 499
- **Code**:
  ```go
  id, _ := result.LastInsertId()
  created := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", id)
  ```
- **Issue**: Same as 3.3 -- returns nil on PG, route channel appears to not exist.

### Finding 3.6

- **File**: `handler/admin/account_tokens.go`
- **Line**: 171
- **Code**:
  ```go
  tokenID, _ := result.LastInsertId()
  // Set as default if appropriate
  if valueStatus == TokenValueStatusReady && (isDefault || (len(existingTokens) == 0 && enabled)) {
      service.SetDefaultToken(h.db, tokenID)
  }
  ...
  var token store.AccountToken
  h.db.Get(&token, "SELECT * FROM account_tokens WHERE id = ?", tokenID)
  ```
- **Issue**: On PG, `tokenID` will be 0. `SetDefaultToken(0)` will attempt to update a non-existent token, and the subsequent SELECT will return no rows.

### Finding 3.7

- **File**: `service/account_token_service.go`
- **Line**: 326
- **Code**:
  ```go
  id, _ := result.LastInsertId()
  targetID := id
  // Clear other defaults
  db.Exec("UPDATE account_tokens SET is_default = 0, updated_at = ? WHERE account_id = ? AND id != ?", now, accountID, targetID)
  // Update account api_token
  db.Exec("UPDATE accounts SET api_token = ?, updated_at = ? WHERE id = ?", normalized, now, accountID)
  ```
- **Issue**: On PG, `targetID` will be 0. The `WHERE ... AND id != 0` clause will clear ALL other tokens' default flag, including the intended one.

---

## 4. HIGH: Placeholder style leak in PG path

### Finding 4.1

- **File**: `handler/admin/settings_backup.go`
- **Lines**: 260-284
- **Code**:
  ```go
  for col, val := range row {
      columns = append(columns, col)
      placeholders = append(placeholders, "?")    // <-- always "?"
      values = append(values, val)
  }

  switch driverName {
  case "pgx", "postgres":
      query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
          table,
          strings.Join(columns, ", "),
          strings.Join(placeholders, ", "),   // <-- uses "?" for PG
      )
  }
  ```
- **Issue**: The placeholder array always contains `"?"`. On PostgreSQL with the `pgx` driver, placeholders must use `$1, $2, ...` format. This will cause a syntax error on PG.
- **Fix**: Generate numbered placeholders for the PG path (e.g., `$1, $2, ...`).

---

## 5. LOW: Test files using `LastInsertId()` without fallback

These test files use `res.LastInsertId()` on INSERTs without `RETURNING` clauses. They will fail when running against a PG backend. Not critical for production but blocks PG testing.

| File | Lines |
|---|---|
| `service/account_mutation_test.go` | 37, 54 |
| `auth/downstream_test.go` | 132, 601, 647, 677, 708, 738, 768, 799 |
| `auth/proxy_test.go` | 310 |
| `handler/admin/accounts_test.go` | 68 |
| `handler/admin/account_tokens_test.go` | 61, 85, 289, 308, 317, 432, 614, 775, 820 |
| `handler/admin/edge_cases_test.go` | 363, 473, 523 |
| `handler/admin/sites_test.go` | 470 |
| `store/sqlite_test.go` | 207, 217, 248, 255, 262, 269, 277, 305 |

---

## 6. ALREADY HANDLED (no action needed)

These patterns are correctly dialect-switched and pose no risk:

### 6.1 `INSERT OR IGNORE` -> `ON CONFLICT DO NOTHING`

| File | Lines | Notes |
|---|---|---|
| `scheduler/usage_aggregation.go` | 255-264 | PG uses `ON CONFLICT (projector_key) DO NOTHING`, SQLite uses `INSERT OR IGNORE` |
| `handler/admin/settings_backup.go` | 270-284 | Same pattern, driver-aware switch |

### 6.2 `AUTOINCREMENT` -> `SERIAL` in DDL

- **File**: `store/migrate.go`
- **Lines**: 110-115 (`serialPK` helper), and all 25 table DDL builders
- **Note**: Each builder produces `SERIAL PRIMARY KEY` for PG and `INTEGER PRIMARY KEY AUTOINCREMENT` for SQLite. Column types are also dialect-correct (`BOOLEAN`/`INTEGER`, `DOUBLE PRECISION`/`REAL`).

### 6.3 PRAGMA statements

- **File**: `store/open.go` lines 154-164
- **Note**: `applySQLitePragmas()` is only called for the `DialectSQLite` case in `Open()`. PG code path never executes these.

### 6.4 `LastInsertId()` with PG fallback

These locations already have correct PG fallback logic:

| File | Lines | Pattern |
|---|---|---|
| `service/oauth/flow.go` | 493-498 | Falls back to `SELECT id FROM accounts WHERE ... ORDER BY id DESC LIMIT 1` |
| `service/oauth/flow.go` | 658-663 | Falls back to `SELECT id FROM sites WHERE platform = ? AND url = ? LIMIT 1` |
| `service/oauth/route_unit.go` | 244-249 | Falls back to `SELECT id FROM oauth_route_units WHERE site_id = ? AND name = ? ORDER BY id DESC LIMIT 1` |

---

## 7. NOT FOUND (no action needed)

- **`INSERT OR REPLACE`**: Not used anywhere in the codebase.
- **`INSERT OR ABORT`**: Not used anywhere in the codebase.
- **`RETURNING` clause**: Not used anywhere (inserts that need the ID rely on `LastInsertId()` or fallback SELECTs).
- **`||` for SQL string concatenation**: Not found in any SQL string. All `||` occurrences in `.go` files are Go logical OR operators.
- **`strftime()`**: Not used anywhere in the codebase.
- **`sqlx.Rebind`**: Not used anywhere. The codebase uses manual dialect switching instead.

---

## 8. Recommended Fix Priority

1. **Fix `datetime('now')`** (Finding 1.1) -- single-line fix, blocks all OAuth token refresh on PG.
2. **Fix `last_insert_rowid()` fallback** (Finding 2.1) -- blocks site creation on PG.
3. **Fix `LastInsertId()` callers** (Findings 3.1-3.7) -- blocks account/token/route/channel creation on PG.
4. **Fix placeholder leak** (Finding 4.1) -- blocks settings backup import on PG.
5. **Fix test files** (Section 5) -- needed to run the test suite against PG.

For items 2-3 and the test files, the established pattern from `service/oauth/flow.go` and `service/oauth/route_unit.go` should be applied consistently:
```go
result, err := db.Exec(`INSERT INTO table (...) VALUES (...)`, ...)
if err != nil {
    return 0, err
}
id, err := result.LastInsertId()
if err != nil {
    // Fallback for Postgres which doesn't support LastInsertId
    // without RETURNING clause.
    _ = db.Get(&id, "SELECT id FROM table WHERE unique_cols ORDER BY id DESC LIMIT 1", ...)
}
```
