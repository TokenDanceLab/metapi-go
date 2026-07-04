# SQLite WAL Configuration Audit

**Audit date:** 2026-07-05
**Scope:** `D:/Code/TokenDance/metapi-go/store/open.go`, `store/migrate.go`
**Cross-reference:** `cmd/migrate/main.go`, prior audits: `audit-db.md`, `audit-memory.md`, `audit-shutdown.md`

---

## Summary

| Dimension | Verdict | Severity |
|---|---|---|
| WAL mode enabled | Pass -- `PRAGMA journal_mode=WAL` applied on open | -- |
| `busy_timeout` | Pass -- set to 5000ms | -- |
| `cache_size` | **Fail** -- not set | MEDIUM |
| `synchronous` | **Warn** -- not set (defaults to FULL in WAL mode, which is correct but implicit) | LOW |
| WAL checkpoint logic | **Fail** -- no explicit checkpointing anywhere | HIGH |
| SQLite connection pool limits | **Fail** -- unbounded (defaults to 0 = unlimited) | HIGH |
| Concurrent reader + writer | **Warn** -- WAL enables this, but unbounded pool undermines it | MEDIUM |

---

## 1. WAL Mode -- PASS

**File:** `store/open.go:148`

```go
if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
    return fmt.Errorf("PRAGMA journal_mode=WAL: %w", err)
}
```

WAL mode is explicitly enabled at connection-open time via `PRAGMA journal_mode = WAL`. This is the correct approach. Once set, WAL mode persists on the database file for all subsequent connections.

Additionally, `cmd/migrate/main.go:248` uses `?_journal_mode=WAL` in the DSN query string for the migration tool's read-only SQLite source connection, which is also correct.

**Concurrency note:** WAL mode fundamentally changes SQLite's concurrency model. In WAL mode, readers do NOT block writers and writers do NOT block readers. Multiple readers can read concurrently. Only one writer at a time is allowed (single-writer lock). This means concurrent readers CAN work with one writer -- this is the core benefit of WAL.

**Verdict: PASS.**

---

## 2. PRAGMA: busy_timeout -- PASS

**File:** `store/open.go:154-156`

```go
if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
    slog.Warn("store: failed to set SQLite busy_timeout", "error", err)
}
```

A 5-second busy timeout is set, but failures are only logged (not returned as errors). This means if the driver or SQLite version rejects this pragma, the connection pool will still open, and concurrent writers will immediately get `SQLITE_BUSY` errors instead of waiting.

Since this is a `slog.Warn` rather than a hard error, the pragma is best-effort. For modernc.org/sqlite this pragma works correctly and is important: without it, concurrent write contention on the single-writer lock causes immediate `SQLITE_BUSY` returns instead of retry-with-wait.

**Verdict: PASS (with minor note about warn-only error handling).**

---

## 3. PRAGMA: cache_size -- FAIL (MISSING)

The `applySQLitePragmas()` function does NOT set `PRAGMA cache_size`. SQLite defaults to 2000 pages (-2000 meaning 2000 KiB = ~2 MB). For a database with 27 tables and heavy read/write workloads (proxy logs, usage aggregation, admin queries), this is small.

**Impact:** Every cache miss triggers a disk read. Under concurrent access (multiple readers + writer), the small cache causes excessive page eviction and re-read, increasing I/O wait and reducing throughput.

**Recommendation:** Add a configurable cache size with a reasonable default:

```go
if _, err := db.Exec("PRAGMA cache_size = -64000"); err != nil {
    slog.Warn("store: failed to set SQLite cache_size", "error", err)
}
```

`-64000` = 64 MB. The negative value means KiB (not pages). This is a low-risk change with high upside for read-heavy workloads.

**Verdict: FAIL (MEDIUM).**

---

## 4. PRAGMA: synchronous -- WARN (IMPLICIT DEFAULT)

The `applySQLitePragmas()` function does NOT set `PRAGMA synchronous`. In WAL mode, SQLite defaults to `synchronous=FULL`, which is the safest setting -- every transaction commit is flushed to disk before returning. This is correct and desirable.

However, the implicit default is fragile: if the database file was previously created with a different journal mode (and thus a different `synchronous` default), or if the default changes in a future SQLite version, the behavior could silently degrade to `synchronous=NORMAL` (less durable) or `synchronous=OFF` (unsafe).

**Recommendation:** Explicitly set `PRAGMA synchronous = FULL` in `applySQLitePragmas()` for defense-in-depth. This is a one-liner with zero risk:

```go
if _, err := db.Exec("PRAGMA synchronous = FULL"); err != nil {
    slog.Warn("store: failed to set SQLite synchronous=FULL", "error", err)
}
```

**Verdict: WARN (LOW).**

---

## 5. WAL Checkpoint Logic -- FAIL (MISSING)

**No explicit WAL checkpoint is triggered anywhere in the codebase.**

Verified via grep for `wal_checkpoint`, `PRAGMA wal_checkpoint`, `checkpoint`, and related terms across the entire `metapi-go` tree. The only match was the existing `audit-shutdown.md` which already noted: "The WAL file is not checkpointed on exit."

### Why this matters

WAL mode writes changes to a separate `-wal` file. Without checkpointing:

1. **WAL file grows unboundedly.** Every write appends to the WAL. The `-wal` file can grow to gigabytes on a busy server, consuming disk I/O and space.
2. **Startup time increases.** On restart, SQLite must replay the entire WAL to reconstruct the database state. A large WAL file can cause multi-second startup delays.
3. **No graceful shutdown cleanup.** The `audit-shutdown.md` finding C1 (DB never closed) compounds this: even the implicit checkpoint on `sql.DB.Close()` cannot run because `CloseDatabase()` is never called in the shutdown sequence. Even if C1 were fixed, `Close()` performs checkpoint only if `SQLITE_FCNTL_PERSIST_WAL` is not set; the behavior depends on driver defaults.
4. **Backup/copy inconsistency risk.** The `docs/deployment.md` line 129 says "Simple file copy (while server is running -- WAL mode safe)" -- this is only true if the WAL has been recently checkpointed. Copying `hub.db` without the corresponding `hub.db-wal` file loses all uncheckpointed transactions.

### Recommendation

Add two checkpoint mechanisms:

**A. Periodic automatic checkpoint (scheduler, 5-minute interval):**

```go
// In a scheduler goroutine:
_, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
```

`TRUNCATE` mode checkpoints all frames and truncates the WAL file to zero bytes. This prevents unbounded growth.

**B. Shutdown-time checkpoint (in CloseDatabase or shutdown hook):**

```go
_, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
```

This ensures clean shutdown with no WAL replay on next start.

**Verdict: FAIL (HIGH).**

---

## 6. Connection Pool Configuration for SQLite -- FAIL (MISSING)

**File:** `store/open.go:162-165`

```go
func configurePostgresPool(db *DB) error {
    db.SetMaxOpenConns(20)
    db.SetMaxIdleConns(5)
    return nil
}
```

PostgreSQL gets explicit pool tuning (20 max open, 5 max idle). **SQLite gets no pool configuration at all.** This means `db.SetMaxOpenConns(0)` (the default), which means **unlimited open connections**.

### Impact

- SQLite is single-writer. Every write acquires an exclusive write lock on the database. With unlimited connections, N concurrent writes will create N goroutines contending for the same lock. The `busy_timeout=5000` pragma mitigates this by having callers wait (up to 5s) rather than failing immediately, but:
  - Each waiting goroutine blocks a connection from the pool.
  - Under high concurrency (HTTP handlers + multiple schedulers), dozens of connections can be simultaneously blocked waiting for the write lock.
  - This wastes memory (each connection has its own page cache) and creates lock contention overhead.
- modernc.org/sqlite (the pure-Go driver used here) does not benefit from multiple connections for concurrency. WAL mode already provides concurrent read + write via a single connection; additional connections only help if the Go `database/sql` pool interleaves queries on the same underlying connection, which it does not.
- In JMOIRON/sqlx with WAL mode, a single connection can handle concurrent readers and one writer efficiently. Setting `SetMaxOpenConns(1)` forces all queries through a single connection, which:
  - Eliminates connection pool overhead.
  - Ensures writes are naturally serialized (no `SQLITE_BUSY` retries needed).
  - Matches the physical reality of SQLite (single-writer lock).

### Existing prior findings

Both `audit-db.md` (section 7.2) and `audit-memory.md` (section 3) independently identified this gap and recommended `db.SetMaxOpenConns(1)` for SQLite. This has not been addressed.

The test file `handler/admin/edge_cases_test.go:355` already uses `db.SetMaxOpenConns(1)` as a workaround for `:memory:` databases (where each connection is a separate database), confirming the developers are aware of the issue for in-memory DBs but have not applied the same fix for file-backed databases.

### Recommendation

Add SQLite-specific pool configuration in `Open()`:

```go
case DialectSQLite:
    if err := applySQLitePragmas(db); err != nil {
        db.Close()
        return nil, fmt.Errorf("store: failed to apply SQLite pragmas: %w", err)
    }
    // SQLite is single-writer; serialize all access through one connection.
    db.SetMaxOpenConns(1)
    // Still allow some idle connections for burst reads between writes.
    // Actually with MaxOpenConns=1, this is effectively 1.
    db.SetMaxIdleConns(1)
```

Note: `MaxIdleConns` with value 1 and `MaxOpenConns` with value 1 means the pool keeps exactly one connection alive. This is correct for SQLite WAL mode.

**Verdict: FAIL (HIGH).** (Already flagged as HIGH in `audit-db.md` section 7.2 and MEDIUM in `audit-memory.md` section 3.)

---

## 7. DSN-Level vs PRAGMA-Level WAL -- COMPARISON

The migration tool (`cmd/migrate/main.go:248`) uses DSN-level WAL:

```go
srcDB, err := sql.Open("sqlite", sourcePath+"?_journal_mode=WAL")
```

The store (`store/open.go:148`) uses PRAGMA-level WAL:

```go
db.Exec("PRAGMA journal_mode = WAL")
```

Both approaches work. The DSN approach is cleaner because WAL is enabled before any statement executes. The PRAGMA approach is also fine because `journal_mode=WAL` is persistent (survives across connections on the same file). Since the store applies it at connection-open time and WAL persists on the file, subsequent connections automatically use WAL mode without needing the PRAGMA.

However, there is a subtlety: if the DSN contains `?_journal_mode=WAL` AND the code also runs `PRAGMA journal_mode=WAL`, there is a redundant second call that returns the already-set mode. This is harmless.

For **consistency**, it would be cleaner to either:
- Include `?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1` in the DSN and remove `applySQLitePragmas()` entirely, OR
- Keep the current PRAGMA approach and add the missing pragmas (`cache_size`, `synchronous`, `wal_autocheckpoint`).

**Verdict: No action needed. Both mechanisms are valid.**

---

## 8. Concurrent Readers + One Writer -- ANALYSIS

### Does WAL mode support concurrent readers with one writer?

**Yes.** This is the defining feature of WAL mode. In rollback-journal mode (the SQLite default), readers block writers and writers block readers. In WAL mode:

- **Readers** read from a consistent snapshot of the database at the start of their transaction. They never block on a writer.
- **One writer** at a time can append to the WAL file. Writers never block readers.
- **Multiple readers** can read concurrently from the same snapshot.

The implementation uses modernc.org/sqlite (pure Go, no CGO), which correctly implements WAL semantics.

### What undermines this?

1. **Unbounded connection pool** (finding 6 above): Without `SetMaxOpenConns(1)`, concurrent writers create multiple connections all contending for the single-writer lock. The `busy_timeout=5000` means they wait instead of failing immediately, but the contention overhead remains.
2. **No WAL checkpoint** (finding 5 above): The WAL file grows without bound, meaning reads must scan an ever-growing WAL file to maintain their snapshot, degrading read performance over time.
3. **No pool lifetime limits** (from `audit-memory.md` section 3): `SetConnMaxLifetime` and `SetConnMaxIdleTime` are not set for either SQLite or PostgreSQL. For SQLite with `MaxOpenConns=1`, this is a non-issue because there is only one connection, but the risk is that if `MaxOpenConns` is ever increased, connections could live forever with stale state.

### Recommended configuration for production WAL concurrency

```go
case DialectSQLite:
    if err := applySQLitePragmas(db); err != nil {
        db.Close()
        return nil, fmt.Errorf(...)
    }
    db.SetMaxOpenConns(1)                     // serialize via single connection
    db.SetMaxIdleConns(1)                     // keep it alive
    db.SetConnMaxLifetime(0)                  // never expire (single connection, no benefit)
    db.SetConnMaxIdleTime(0)                  // never expire idle (single connection)
```

**Verdict: WARN (MEDIUM).** WAL mode inherently supports the concurrency model. The configuration gaps (no pool limits, no checkpoint) undermine it.

---

## 9. Complete PRAGMA Checklist

| Pragma | Set? | Value | Recommended |
|---|---|---|---|
| `journal_mode` | Yes | `WAL` | `WAL` |
| `foreign_keys` | Yes | `ON` | `ON` |
| `busy_timeout` | Yes | `5000` | `5000` |
| `cache_size` | **No** | (default: -2000) | `-64000` (64 MB) |
| `synchronous` | **No** | (WAL default: FULL) | `FULL` (explicit) |
| `wal_autocheckpoint` | **No** | (default: 1000) | `1000` or custom |
| `temp_store` | **No** | (default: 0 = FILE) | `MEMORY` |

---

## 10. Recommendations

### High priority (should fix before production deployment)

1. **Add `db.SetMaxOpenConns(1)` for SQLite** in `store/open.go:applySQLitePragmas()` or after its call. This prevents connection pool contention on the single-writer lock. (Also flagged in `audit-db.md` and `audit-memory.md`.)

2. **Add WAL checkpoint logic.** As a minimum:
   - Add a periodic checkpoint scheduler (every 5 minutes, `PRAGMA wal_checkpoint(TRUNCATE)`).
   - Add a shutdown-time checkpoint in `store.CloseDatabase()`, called from the shutdown hook (which must be added -- see `audit-shutdown.md` finding C1).
   - Both are required: periodic prevents unbounded growth; shutdown ensures clean restart.

### Medium priority

3. **Add `PRAGMA cache_size = -64000`** for 64 MB page cache. This is a one-line change with zero risk. For deployments with large proxy_logs tables, this significantly reduces disk I/O.

### Low priority

4. **Add `PRAGMA synchronous = FULL`** explicitly (defense-in-depth; already the WAL default).
5. **Add `PRAGMA temp_store = MEMORY`** to keep temporary tables and indices in memory.

---

## 11. Cross-Reference with Prior Audits

| Prior Finding | This Audit | Status |
|---|---|---|
| `audit-db.md` 7.2: SQLite unbounded pool | Finding 6: Confirmed, still unaddressed | Open |
| `audit-memory.md` 3: No pool limits, no `SetMaxOpenConns(1)` | Finding 6: Confirmed, still unaddressed | Open |
| `audit-shutdown.md` C1: DB never closed, WAL not checkpointed | Finding 5: Confirmed, no checkpoint logic anywhere | Open |
| `audit-shutdown.md` C1: DB close missing from shutdown | Finding 5b: Blocks shutdown-time checkpoint | Open |

All four prior findings related to WAL/connection pool are still unresolved.
