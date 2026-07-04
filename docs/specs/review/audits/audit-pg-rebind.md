# PG Rebind Audit: scheduler/ `?` Placeholder Safety

**Date:** 2026-07-05
**Scope:** `D:/Code/TokenDance/metapi-go/scheduler/` -- 7 target files + 2 extras
**Verdict:** PASS -- zero issues found.

---

## 1. Summary

Every raw SQL query containing `?` placeholders in the audited files passes through `sqlx` (either `*sqlx.DB` or `*sqlx.Tx`). The PostgreSQL driver binder is globally registered in `store/open.go`, so all `?` are auto-rebound to `$1`, `$2`, ... on PG. No file imports or uses `database/sql` directly.

---

## 2. Safety Mechanism

`store/open.go:122-123`:
```go
if driverName == "pgx" {
    sqlx.BindDriver("pgx", sqlx.DOLLAR)
}
```

This registers a global rebind function. Every `sqlx` method (`Query`, `Exec`, `QueryRow` -- on both `*sqlx.DB` and `*sqlx.Tx`) calls this binder when the driver is `"pgx"`, transforming all `?` tokens into `$1`, `$2`, ... before sending to the wire.

`store.DB` (defined in `store/open.go:26-29`) embeds `*sqlx.DB`:
```go
type DB struct {
    *sqlx.DB
    Dialect string
}
```

Therefore, `dbw.Query()`, `dbw.Exec()`, `dbw.QueryRow()` are all `sqlx` methods and participate in rebinding.

---

## 3. Per-File Inventory

### 3.1 channel_recovery.go

| Line | Query | Method | Safe? |
|------|-------|--------|-------|
| 158-168 | `SELECT ... WHERE rc.cooldown_until > ?` | `dbw.Query` (sqlx) | SAFE |
| 194-203 | `SELECT ... WHERE rc.cooldown_until IS NULL LIMIT 50` | `dbw.Query` (sqlx) | SAFE (no `?`) |

### 3.2 admin_snapshot.go

| Line | Query | Method | Safe? |
|------|-------|--------|-------|
| 170 | `DELETE FROM admin_snapshots WHERE expires_at < ?` | `dbw.Exec` (sqlx) | SAFE |

### 3.3 file_retention.go

| Line | Query | Method | Safe? |
|------|-------|--------|-------|
| 103 | `DELETE FROM proxy_files WHERE created_at < ? AND deleted_at IS NULL` | `dbw.Exec` (sqlx) | SAFE |

### 3.4 log_retention.go

| Line | Query | Method | Safe? |
|------|-------|--------|-------|
| 109 | `DELETE FROM proxy_logs WHERE created_at < ?` | `dbw.Exec` (sqlx) | SAFE |

### 3.5 usage_aggregation.go (heaviest file)

| Line(s) | Query | Method | Safe? |
|---------|-------|--------|-------|
| 261-264 | `INSERT OR IGNORE ... VALUES (?, 'UTC', 0, ?, ?)` (SQLite branch) | `dbw.Exec` (sqlx) | SAFE -- SQLite dialect, no rebind needed |
| 255-258 | `INSERT INTO ... VALUES ($1, 'UTC', 0, $2, $3)` (PG branch) | `dbw.Exec` (sqlx) | SAFE -- uses `$N` directly |
| 272-277 | `UPDATE ... SET lease_owner = ?, lease_token = ?, lease_expires_at = ?, updated_at = ? WHERE projector_key = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)` | `dbw.Exec` (sqlx) | SAFE |
| 297-302 | `UPDATE ... SET ... last_error = ?, updated_at = ? WHERE projector_key = ? AND lease_token = ?` | `dbw.Exec` (sqlx) | SAFE |
| 307-312 | `SELECT ... WHERE projector_key = ?` | `dbw.QueryRow` (sqlx) | SAFE |
| 332-340 | `SELECT ... WHERE pl.id > ? ... LIMIT ?` | `dbw.Query` (sqlx) | SAFE |
| 412-414 | `INSERT INTO site_day_usage ... VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)` | `tx.Exec` (sqlx.Tx) | SAFE |
| 417-419 | `INSERT INTO site_hour_usage ... VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)` | `tx.Exec` (sqlx.Tx) | SAFE |
| 426-429 | `UPDATE analytics_projection_checkpoints SET last_proxy_log_id = ?, ... WHERE projector_key = ?` | `tx.Exec` (sqlx.Tx) | SAFE |
| 447-451 | `SELECT id, created_at FROM proxy_logs WHERE id >= ?` | `dbw.QueryRow` (sqlx) | SAFE |
| 455-457 | `UPDATE ... SET recompute_from_id = NULL, ... WHERE projector_key = ?` | `dbw.Exec` (sqlx) | SAFE |
| 482-484 | `DELETE FROM site_day_usage WHERE local_day >= ?` etc. | `dbw.Exec` (sqlx) | SAFE |
| 491-493 | `UPDATE ... SET last_proxy_log_id = ?, ... WHERE projector_key = ?` | `dbw.Exec` (sqlx) | SAFE |
| 515-517 | `UPDATE ... SET recompute_from_id = ?, recompute_requested_at = ?, updated_at = ? WHERE projector_key = ?` | `dbw.Exec` (sqlx) | SAFE |

### 3.6 balance.go

No raw SQL queries in this file. Delegates to `balance.RefreshAllBalances(cfg, db)` where `db` is `*sqlx.DB` from `getSqlxDB()`. The helper `getSqlxDB()` (helpers.go:27-33) extracts `*sqlx.DB` from `*store.DB`.

### 3.7 checkin.go

| Line | Query | Method | Safe? |
|------|-------|--------|-------|
| 181-188 | `SELECT a.id, a.last_checkin_at FROM accounts a ... WHERE ...` | `dbw.Query` (sqlx) | SAFE (no `?` in this query) |

Delegates checkin execution to `checkin.CheckinAll(cfg, db, dueIDs, mode)` where `db` is `*sqlx.DB`.

---

## 4. Additional Files Checked (Beyond Requested Scope)

### log_cleanup.go

| Line | Query | Method | Safe? |
|------|-------|--------|-------|
| 113 | `DELETE FROM proxy_logs WHERE created_at < ?` | `dbw.Exec` (sqlx) | SAFE |
| 122 | `DELETE FROM events WHERE created_at < ?` | `dbw.Exec` (sqlx) | SAFE |

### sub2api_refresh.go

Contains `dbw.Query()` with no `?` placeholders. SAFE.

### backup_webdav.go

No SQL queries. SAFE.

---

## 5. Notable Pattern: Dialect-Branched Query in usage_aggregation.go

`tryAcquireLease()` (lines 252-265) is the only place where PG and SQLite queries diverge:

```go
switch dbw.Dialect {
case store.DialectPostgres:
    ensureQuery = `INSERT INTO ... VALUES ($1, 'UTC', 0, $2, $3) ON CONFLICT ...`
default: // sqlite
    ensureQuery = `INSERT OR IGNORE INTO ... VALUES (?, 'UTC', 0, ?, ?)`
}
```

This is correct because:
- PG branch uses `$N` directly, so sqlx does not rebind it (sqlx only rebinds `?`).
- SQLite branch uses `?` natively; no rebinder is registered for the SQLite driver.

However, this introduces a **maintenance hazard**: if someone adds `?` to the PG branch query without switching to `$N`, it would still work (because sqlx rebinds), creating inconsistency. Recommend either:
1. Stick to `?` uniformly (rely on sqlx rebind for both) -- simpler, single source of truth.
2. Or always use `$N` in the PG branch for ALL PG-branch queries (not just this one) for explicit clarity.

Currently, this is the ONLY PG-branch query in the file; all other queries use `?` and rely on sqlx rebind. Consistency would favor option 1.

---

## 6. Verification Commands Used

```bash
# Confirm no database/sql import in any scheduler file
grep -r "database/sql" D:/Code/TokenDance/metapi-go/scheduler/

# List every ? in SQL queries across the scheduler directory
grep -rn '\?' D:/Code/TokenDance/metapi-go/scheduler/*.go | grep -E '(Exec|Query|QueryRow)'
```

---

## 7. Conclusion

**Zero violations.** All `?` placeholder usage in the scheduler package is protected by `sqlx.BindDriver("pgx", sqlx.DOLLAR)` registered globally in `store/open.go`. No raw `database/sql` connections exist. The audit finds no risk of `?` being sent to PostgreSQL unrebound.
