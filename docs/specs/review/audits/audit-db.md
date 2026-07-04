# Database Query Efficiency Audit

**Date:** 2026-07-04
**Scope:** `D:/Code/TokenDance/metapi-go`
**Files audited:** `handler/admin/sites.go`, `handler/admin/accounts.go`, `handler/admin/account_tokens.go`, `handler/admin/downstream_keys.go`, `handler/admin/search.go`, `handler/admin/settings_database.go`, `store/schema.go`, `store/migrate.go`, `store/open.go`, `service/site_service.go`, `service/account_service.go`, `scheduler/usage_aggregation.go`, `scheduler/checkin.go`, `scheduler/settings.go`, `scheduler/scheduler.go`

**Severity legend:**
- **CRITICAL** -- Data loss, silent corruption, or guaranteed runtime failure at production scale.
- **HIGH** -- Significant performance degradation or correctness risk under normal load.
- **MEDIUM** -- Suboptimal pattern; will become a problem at scale or under contention.
- **LOW** -- Code style / maintainability issue with negligible runtime impact.

---

## 1. N+1 Query Patterns

### 1.1 `batchSites` -- per-ID SELECT and per-ID DML (HIGH)

**File:** `handler/admin/sites.go`, lines 441--466

Each site ID triggers a `SELECT * FROM sites WHERE id = ?` followed by one `UPDATE` or `DELETE`. For N IDs, this produces 2N individual database round-trips.

```go
for _, rawID := range body.IDs {
    id := int64(rawID)
    var existing store.Site
    err := h.db.Get(&existing, "SELECT * FROM sites WHERE id = ?", id) // N queries
    // ...
    h.db.Exec("DELETE FROM sites WHERE id = ?", id) // + N queries
}
```

**Fix:** Collect all IDs, issue one `SELECT id FROM sites WHERE id IN (?)` to validate existence, then batch the DML with a single `DELETE FROM sites WHERE id IN (?)`.

### 1.2 `batchAccounts` -- same anti-pattern (HIGH)

**File:** `handler/admin/accounts.go`, lines 653--680

Identical N+1: per-ID `SELECT * FROM accounts WHERE id = ?` then per-ID `DELETE`/`UPDATE`.

### 1.3 `batchTokens` -- same anti-pattern (HIGH)

**File:** `handler/admin/account_tokens.go`, lines 211--248

Per-ID: `GetTokenByID`, `GetAccountByID`, then `Exec` for enable/disable/delete. Produces 3N queries minimum.

### 1.4 `batchKeys` -- same anti-pattern (HIGH)

**File:** `handler/admin/downstream_keys.go`, lines 598--622

Per-ID: `SELECT * FROM downstream_api_keys WHERE id = ?` then per-ID `DELETE`/`UPDATE`.

### 1.5 `updateDisabledModels` -- per-model INSERT loop (MEDIUM)

**File:** `handler/admin/sites.go`, lines 559--564

```go
for _, m := range uniqueModels {
    h.db.Exec("INSERT INTO site_disabled_models (...) VALUES (?, ?, ?)", id, m, now)
}
```

**Fix:** Use a bulk INSERT (`INSERT INTO ... VALUES (?,?,?), (?,?,?), ...`) or wrap in a transaction.

### 1.6 `manualModels` -- per-model SELECT + INSERT/UPDATE (MEDIUM)

**File:** `handler/admin/accounts.go`, lines 850--857

```go
for _, m := range models {
    var existingID int64
    err := h.db.Get(&existingID, "SELECT id FROM model_availability WHERE ...") // N queries
    if err == nil {
        h.db.Exec("UPDATE model_availability SET ...")   // +N queries
    } else {
        h.db.Exec("INSERT INTO model_availability ...")   // +N queries
    }
}
```

**Fix:** Load all existing rows with `SELECT id, model_name FROM model_availability WHERE account_id = ? AND model_name IN (?)`, then issue one bulk insert-or-replace.

### 1.7 `validateDownstreamPolicyReferences` -- per-ref DB calls (MEDIUM)

**File:** `handler/admin/downstream_keys.go`, lines 1186--1229

For every `excludedCredentialRef`, the code issues a separate `SELECT ... FROM account_tokens ... INNER JOIN accounts ...` or `SELECT ... FROM accounts ...`. With 100 excluded refs, this is 100 queries.

**Fix:** Collect all token IDs and account IDs first, issue two bulk `IN (?)` queries, then validate in-memory.

### 1.8 `getAccountModels` -- two separate queries where a JOIN suffices (LOW)

**File:** `handler/admin/accounts.go`, lines 776--780

```go
h.db.Select(&modelRows, "SELECT ... FROM model_availability WHERE account_id = ?", id)
h.db.Select(&disabledRows, "SELECT model_name FROM site_disabled_models WHERE site_id = ?", ...)
```

Two round-trips. Could be folded into one joined query.

---

## 2. Missing Indexes vs. Actual Query Patterns

### 2.1 No index on `accounts(site_id, username)` (MEDIUM)

**Query:** `handler/admin/accounts.go` line 337 (loginAccount):
```sql
SELECT * FROM accounts WHERE site_id = ? AND username = ?
```

**Existing indexes:** `accounts_site_id_idx` (site_id only), `accounts_site_status_idx` (site_id, status).

The `site_id + username` lookup hits only the single-column `site_id` index, then filters by `username` in a residual scan. A composite index `(site_id, username)` would make this a direct index lookup.

### 2.2 No index on `accounts(sort_order)` (LOW)

**Query:** `service/account_service.go` line 279:
```sql
SELECT COALESCE(MAX(sort_order), -1) FROM accounts
```

Full table scan each time. Acceptable for small tables (<10k rows) but will degrade. Consider maintaining a counter or adding an index.

### 2.3 No index on `accounts(oauth_provider, oauth_account_key, oauth_project_id)` is present but no corresponding query (INFO)

Line 1161 in `store/migrate.go` defines `accounts_oauth_identity_idx` on `(oauth_provider, oauth_account_key, oauth_project_id)`. This is an OAuth identity lookup index. No corresponding query was found in the audited handlers, but it likely exists in the OAuth service layer. This is planning-ahead coverage, which is fine.

### 2.4 `model_availability(model_name)` index exists (line 1172) -- good

The `getAvailableModels` and `search` endpoints both filter by `model_name`, so this index is correctly pre-declared.

### 2.5 `proxy_logs` indexes are comprehensive (INFO)

67 indexes created in `migrate.go`. The proxy_logs table has 7 indexes covering common filter+sort patterns (`created_at`, `account_id+created_at`, `status+created_at`, `model_actual+created_at`, `downstream_api_key_id+created_at`, `client_app_id+created_at`, `client_family+created_at`). This is well-designed for time-series query patterns.

---

## 3. Non-Transactional Multi-Row Updates

### 3.1 `batchSites` -- no transaction wrapping (HIGH)

**File:** `handler/admin/sites.go`, lines 441--466

Multiple `DELETE`/`UPDATE` operations are issued individually. If the process crashes mid-loop, some sites are acted upon and others are not, with no atomicity guarantee. Same applies to `batchAccounts`, `batchTokens`, and `batchKeys`.

**Fix:** Wrap batch operations in a single `db.Beginx()` / `tx.Commit()` transaction.

### 3.2 `updateDisabledModels` -- DELETE + INSERTs not atomic (MEDIUM)

**File:** `handler/admin/sites.go`, lines 559--564

```go
h.db.Exec("DELETE FROM site_disabled_models WHERE site_id = ?", id)
for _, m := range uniqueModels {
    h.db.Exec("INSERT INTO site_disabled_models ...")
}
```

If the process dies between DELETE and INSERTs, the site has zero disabled models. Wrap in a transaction.

### 3.3 `manualModels` -- per-model DML not atomic (MEDIUM)

**File:** `handler/admin/accounts.go`, lines 850--857

Per-model UPDATE/INSERT without a transaction. Concurrent requests editing the same account's manual models will race.

### 3.4 `usage_aggregation.applyBatch` -- per-row INSERTs without transaction (CRITICAL)

**File:** `scheduler/usage_aggregation.go`, lines 394--404

```go
for _, d := range deltas {
    dbw.Exec(`INSERT INTO site_day_usage (...) VALUES (...)`, ...)
    dbw.Exec(`INSERT INTO site_hour_usage (...) VALUES (...)`, ...)
}
// Then checkpoint UPDATE separately (lines 407-413)
```

**Problem A (no transaction):** If process crashes partway through, some usage rows are written but the checkpoint is not advanced, leading to **double-counting** on the next projection pass.

**Problem B (no upsert):** The tables have `UNIQUE (local_day, site_id)` and `UNIQUE (bucket_start_utc, site_id)` constraints. Plain `INSERT` will fail with a UNIQUE violation on the second pass for the same (day, site) or (hour, site). This means: **usage aggregation will break entirely after the first successful pass for any real workload.** The query needs `INSERT ... ON CONFLICT ... DO UPDATE SET ...` (PostgreSQL) or `INSERT OR REPLACE` (SQLite).

**Problem C (wrong hour bucket):** Line 369:
```go
hour := time.Now().UTC().Format("2006-01-02 15:04:05") // full timestamp
```
This writes the *current wall-clock time* as the hour bucket, not the *log's creation time*. Every projection row gets a unique timestamp, defeating the purpose of hourly buckets. Should be derived from the log's `created_at`, truncated to the hour boundary.

**Problem D (hardcoded model):** Line 375:
```go
model: "unknown",
```
The `model_day_usage` table never receives real model names. The `model_actual` column from `proxy_logs` is not selected in `fetchBatch` (line 322--325).

### 3.5 `usage_aggregation.applyRecompute` -- DELETEs not transactional (HIGH)

**File:** `scheduler/usage_aggregation.go`, lines 466--477

Three `DELETE FROM ...` statements followed by a checkpoint `UPDATE` -- all outside a transaction. Partial deletion means aggregate data becomes inconsistent.

---

## 4. SELECT * Without Column Lists

This is the single most pervasive issue in the codebase. Nearly every query in `handler/admin/` and `service/` uses `SELECT *`.

**Why it matters:** `SELECT *` breaks when columns are added/removed, defeats covering indexes, transfers unnecessary data over the wire, and prevents the query planner from using index-only scans.

### 4.1 `handler/admin/sites.go`

| Line | Query |
|------|-------|
| 230 | `SELECT * FROM sites WHERE id = ?` |
| 444 | `SELECT * FROM sites WHERE id = ?` |
| 513 | `SELECT * FROM sites WHERE id = ?` |
| 542 | `SELECT * FROM sites WHERE id = ?` |
| 583 | `SELECT * FROM sites WHERE id = ?` |

### 4.2 `handler/admin/accounts.go`

| Line | Query |
|------|-------|
| 101 | `SELECT * FROM sites ORDER BY sort_order, id` |
| 131 | `SELECT * FROM sites WHERE id = ?` |
| 232 | `SELECT * FROM accounts WHERE id = ?` |
| 324 | `SELECT * FROM sites WHERE id = ?` |
| 337 | `SELECT * FROM accounts WHERE site_id = ? AND username = ?` |
| 380 | `SELECT * FROM accounts WHERE site_id = ? AND username = ?` |
| 429 | `SELECT * FROM sites WHERE id = ?` |
| 493 | `SELECT * FROM accounts WHERE id = ?` |
| 607 | `SELECT * FROM accounts WHERE id = ?` |
| 663 | `SELECT * FROM accounts WHERE id = ?` |
| 739 | `SELECT * FROM accounts WHERE id = ?` |
| 844 | `SELECT * FROM accounts WHERE id = ?` |

### 4.3 `handler/admin/account_tokens.go`

| Line | Query |
|------|-------|
| 122 | `SELECT * FROM account_tokens WHERE account_id = ?` |
| 179 | `SELECT * FROM account_tokens WHERE id = ?` |
| 434 | `SELECT * FROM accounts WHERE id = ?` |
| 435 | `SELECT * FROM sites WHERE id = ?` |
| 586 | `SELECT * FROM accounts WHERE id = ?` |
| 603 | `SELECT * FROM sites WHERE id = ?` |

### 4.4 `handler/admin/downstream_keys.go`

| Line | Query |
|------|-------|
| 47 | `SELECT * FROM downstream_api_keys` (summary endpoint, no LIMIT) |
| 99 | `SELECT * FROM downstream_api_keys ORDER BY id DESC` (no LIMIT) |
| 214 | `SELECT * FROM downstream_api_keys WHERE id = ?` |
| 236, 466, 491, 520, 550, 599 | Same pattern |

### 4.5 `handler/admin/search.go`

| Line | Query |
|------|-------|
| 71 | `SELECT * FROM sites WHERE ...` |
| 103 | `SELECT * FROM proxy_logs WHERE ...` |

### 4.6 `service/site_service.go`

| Line | Query |
|------|-------|
| 155 | `SELECT * FROM sites WHERE id = ?` |
| 200 | `SELECT * FROM sites ORDER BY sort_order, id` |
| 216 | `SELECT site_id, balance, extra_config FROM accounts` -- **correct, explicit columns** |

### 4.7 `service/account_service.go`

| Line | Query |
|------|-------|
| 181 | `SELECT * FROM accounts WHERE id = ?` |

**Bulk-fix approach:** Define a `.Columns()` method or named column constants per struct, and replace all `SELECT *` with explicit column lists. A linter rule (e.g., `golangci-lint` with `sqlclosecheck` or custom SQL linting) should enforce this going forward.

---

## 5. Missing LIMIT on Unbounded Queries

### 5.1 `listSites` -- no LIMIT (HIGH)

**File:** `handler/admin/sites.go`, line 45
```go
sites, err := service.ListSites(h.db)
```
Which calls (`service/site_service.go` line 200):
```sql
SELECT * FROM sites ORDER BY sort_order, id
```
No pagination, no LIMIT. Returns every site row + all account aggregates + all API endpoints.

### 5.2 `listAccounts` -- no LIMIT (HIGH)

**File:** `handler/admin/accounts.go`, line 93
```go
accounts, err := service.ListAccountsWithSites(h.db)
```
Full accounts JOIN sites with no pagination.

Plus line 101:
```sql
SELECT * FROM sites ORDER BY sort_order, id
```
A second full table scan of sites within the same handler.

### 5.3 `downstream_keys.summary` -- no LIMIT on base query (HIGH)

**File:** `handler/admin/downstream_keys.go`, line 47
```sql
SELECT * FROM downstream_api_keys
```
Builds WHERE clauses but never adds LIMIT. Pagination is applied *after* fetching all rows (lines 70--83).

### 5.4 `downstream_keys.listKeys` -- no LIMIT (MEDIUM)

**File:** `handler/admin/downstream_keys.go`, line 99
```sql
SELECT * FROM downstream_api_keys ORDER BY id DESC
```

### 5.5 `checkin` scheduler -- full table scan for active accounts (not read but likely)

The `runIntervalPass` in `scheduler/checkin.go` presumably queries all active accounts. If it also lacks a LIMIT clause, repeated full scans every 60 seconds could be costly.

---

## 6. Prepared Statement Reuse

### 6.1 Zero use of prepared statements (MEDIUM)

The entire codebase constructs inline SQL strings passed to `db.Query()`, `db.Queryx()`, `db.Exec()`, `db.Get()`, and `db.Select()`. `sqlx` does NOT implicitly cache or reuse parsed statements -- each call re-parses the SQL.

Notable high-frequency call sites:
- `usage_aggregation.fetchBatch` (every 5 seconds, up to 120 times per pass)
- `usage_aggregation.tryAcquireLease` (every 5 seconds)
- `usage_aggregation.applyBatch` per-row INSERTs (every 5 seconds, per-row)
- `usage_aggregation.readCheckpoint` (every 5 seconds)
- Handler queries on every HTTP request

**Fix:** For the high-frequency scheduler queries, use `db.Preparex()` at startup and reuse the prepared statement. For handler-level queries, `sqlx` can benefit from `db.Preparex()` wrapped in a sync.Pool or simple map cache. PostgreSQL benefits more from this than SQLite.

---

## 7. Connection Pool Exhaustion Risk

### 7.1 Scheduler goroutine accumulation (MEDIUM)

**File:** `scheduler/usage_aggregation.go`, lines 52--65

```go
go s.runPass()  // immediate first run
go func() {
    for {
        select {
        case <-s.ticker.C:
            go s.runPass()  // new goroutine every 5s
        // ...
        }
    }
}()
```

The `projectionInFlight` mutex prevents concurrent execution, but each tick spawns a new goroutine that blocks on the mutex. If `runPass` takes >5 seconds (e.g., large recompute), goroutines accumulate. Use a non-blocking select with `default` to skip ticks when already in flight, rather than spawning goroutines:

```go
case <-s.ticker.C:
    select {
    case s.sem <- struct{}{}:
        go func() { s.runPass(); <-s.sem }()
    default:
        // skip this tick
    }
```

### 7.2 PostgreSQL pool size = 20, SQLite unbounded (MEDIUM)

**File:** `store/open.go`, line 163

```go
db.SetMaxOpenConns(20)
db.SetMaxIdleConns(5)
```

PostgreSQL pool is capped at 20. SQLite has no explicit pool configuration. Under load (concurrent batch operations + scheduler + HTTP handlers), 20 PG connections may be insufficient if `applyBatch` holds a connection for each per-row INSERT. For SQLite with WAL mode, concurrent writers will contend on the single-writer lock, and excessive open connections provide no benefit.

**Recommendation:** For SQLite, set `SetMaxOpenConns(1)` to serialize all writes through WAL (which already serializes writers). For PostgreSQL, audit peak concurrent DB operations and size accordingly.

### 7.3 `usage_aggregation.applyBatch` -- per-row Exec without connection reuse (HIGH)

**File:** `scheduler/usage_aggregation.go`, lines 394--404

Each `dbw.Exec()` call acquires a connection from the pool, executes one INSERT, and returns it. For a batch of 1000 rows with 2 INSERTs each = 2000 pool acquire/release cycles per 5s tick. This is highly inefficient.

**Fix:** Batch all INSERTs into a single multi-row statement, or use a prepared statement within a transaction, or use `dbw.NamedExec` with a slice.

---

## 8. Additional Critical Findings

### 8.1 Usage aggregation INSERT vs UNIQUE constraint (CRITICAL)

As detailed in section 3.4 Problem B, the `applyBatch` function uses plain `INSERT` into tables with `UNIQUE` constraints. The second projection pass for the same (day, site_id) pair will fail, aborting the entire batch. **Usage aggregation is functionally broken for any real workload.**

### 8.2 Hour bucket uses wall-clock time instead of log timestamp (CRITICAL)

As detailed in section 3.4 Problem C, `bucket_start_utc` is set to `time.Now()` instead of the log's `created_at`, truncated to the hour. Every row in `site_hour_usage` gets a unique timestamp, making hourly aggregation meaningless.

### 8.3 Model name hardcoded to "unknown" (HIGH)

As detailed in section 3.4 Problem D, `model_day_usage.model` is always "unknown", rendering the model-level usage table useless.

### 8.4 No shutdown cleanup for scheduler goroutines

**File:** `scheduler/usage_aggregation.go`, line 58

The `go s.runPass()` goroutine is spawned on each tick but there is no mechanism to cancel in-flight passes during shutdown. Combined with the lease timeout of 10 minutes, a crashed node's lease blocks other instances for the full lease duration.

---

## 9. Summary

| Category | CRITICAL | HIGH | MEDIUM | LOW |
|----------|----------|------|--------|-----|
| N+1 Queries | 0 | 3 | 4 | 1 |
| Missing Indexes | 0 | 0 | 1 | 1 |
| Non-Transactional DML | 1 | 3 | 3 | 0 |
| SELECT * | 0 | 0 | 0 | 30+ |
| Missing LIMIT | 0 | 3 | 1 | 0 |
| Prepared Statement Reuse | 0 | 0 | 1 | 0 |
| Connection Pool Risk | 1 | 1 | 2 | 0 |

**Top priorities (must-fix before production):**

1. **CRITICAL -- `usage_aggregation.applyBatch`**: Replace plain INSERT with upsert (INSERT ... ON CONFLICT), fix hour bucket derivation from log timestamp, fix model name extraction, and wrap all DML in a transaction.
2. **HIGH -- All batch handlers**: Wrap multi-row mutations in transactions and batch the queries (use `IN (?)` for reads, multi-row DML for writes).
3. **HIGH -- All list endpoints**: Add pagination (LIMIT/OFFSET or keyset pagination) to `listSites`, `listAccounts`, `listKeys`, and `summary`.
4. **MEDIUM -- `applyBatch` per-row INSERTs**: Replace with bulk INSERT using multi-row VALUES syntax.
5. **MEDIUM -- Prepared statements**: Add `Preparex` for high-frequency scheduler queries.

**Quick wins (low effort, high impact):**

- Add `db.SetMaxOpenConns(1)` for SQLite in `open.go`.
- Fix hour bucket derivation (1-line change: use log `created_at` truncated to hour).
- Add LIMIT clauses to the 4 unbounded list queries.
- Replace SELECT * in the top 10 most-called queries with explicit column lists.
