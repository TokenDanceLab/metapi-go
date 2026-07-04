# PostgreSQL Connection Settings Audit: metapi-go

**Date**: 2026-07-05
**Auditor**: Automated audit of `D:/Code/TokenDance/metapi-go/store/open.go` and `D:/Code/TokenDance/metapi-go/store/schema.go`
**Scope**: PostgreSQL connection pool, sslmode, statement timeout, idle connection lifecycle

---

## Summary

The PostgreSQL connection layer has four material deficiencies: (1) sslmode is a hardcoded boolean incapable of expressing `verify-full`, (2) connection pool lifetime limits (`ConnMaxLifetime`, `ConnMaxIdleTime`) are absent, (3) no statement timeout is configured, and (4) pool sizing is hardcoded with zero operator configurability. These issues were previously identified in `audit-memory.md` (2026-07-05) and `audit-resilience.md` (2026-07-05) and remain unfixed as of this audit.

---

## Finding 1: sslmode is a bare boolean -- no `verify-ca` or `verify-full` support

**Severity**: HIGH
**File**: `store/open.go`, lines 93-113
**Config source**: `config/config.go` line 360: `cfg.DbSsl = parseBoolean(get("DB_SSL"), false)`

### Current behavior

```go
func Open(dialect string, dsn string, sslMode bool) (*DB, error) {
    // ...
    case DialectPostgres:
        driverName = "pgx"
        connStr = dsn
        if sslMode {
            if strings.Contains(connStr, "?") {
                connStr += "&sslmode=require"
            } else {
                connStr += "?sslmode=require"
            }
        }
```

The function signature accepts `sslMode bool`. When `true`, it unconditionally appends `sslmode=require`. When `false`, it appends nothing.

### Problems

1. **No `verify-full`**: `sslmode=require` performs TLS but does NOT verify the server certificate. This is the TLS equivalent of `curl --insecure` -- it encrypts the wire but provides no protection against MITM attacks. Production PostgreSQL connections should use `sslmode=verify-full`.

2. **No `verify-ca`**: Some environments chain to a private CA and need `verify-ca` without hostname verification. There is no way to express this.

3. **No `disable` override**: If the user sets `DB_SSL=false` but the DSN already contains `?sslmode=require` (e.g., from a connection-string template), the SSL mode from the DSN persists silently. The boolean default of `false` means "do nothing to the DSN" -- it does NOT mean "force disable."

4. **DSN already contains sslmode**: The CI configuration at `.github/workflows/ci.yml:47` uses `DATABASE_URL: postgres://postgres:test@localhost:5432/metapi_test?sslmode=disable`. If `DB_SSL=true` were set simultaneously, the result would be `postgres://...?sslmode=disable&sslmode=require` -- pgx behavior with duplicate query parameters is implementation-defined and may be silently wrong.

5. **Incorrect query-string concatenation**: When `sslMode=true` and the DSN already contains query parameters (via `?`), it appends `&sslmode=require`. But if the DSN uses the `postgres://` URI format and already contains an sslmode parameter, this code blindly appends a second one instead of updating it.

### Recommendation

Replace the boolean `sslMode` parameter with a string `sslMode` (accepting `disable`, `require`, `verify-ca`, `verify-full`). Update `config.go` to read `DB_SSLMODE` as a string. Parse the DSN to update the sslmode query parameter rather than appending blindly.

```go
// Proposed
func Open(dialect string, dsn string, sslMode string) (*DB, error) {
    // ...
    case DialectPostgres:
        driverName = "pgx"
        connStr = setDSNParameter(dsn, "sslmode", sslMode)
```

---

## Finding 2: No connection pool lifetime limits

**Severity**: HIGH
**File**: `store/open.go`, lines 169-173
**Previously reported**: `audit-memory.md` section 2, `audit-resilience.md` section 1

### Current behavior

```go
func configurePostgresPool(db *DB) error {
    db.SetMaxOpenConns(20)
    db.SetMaxIdleConns(5)
    return nil
}
```

Only `MaxOpenConns` and `MaxIdleConns` are configured. Neither `ConnMaxLifetime` nor `ConnMaxIdleTime` is set.

### Problems

1. **No `ConnMaxLifetime`**: Connections live forever. Go's `database/sql` default is `0` (no limit). In long-running deployments behind a load balancer, NAT gateway, or cloud-proxy with idle-timeout enforcement, connections silently break. The next query that borrows a dead connection fails with a network error (e.g., `EOF`, `connection reset by peer`). `database/sql` retries the query on a fresh connection, but the failed attempt causes a latency spike and a wasted round-trip. Over time, every idle connection in the pool is dead, and the first query batch after an idle period sees spurious errors.

2. **No `ConnMaxIdleTime`**: Idle connections never expire (default `0` = unlimited). Under bursty traffic, 5 idle connections (`MaxIdleConns`) remain open indefinitely, consuming PostgreSQL server resources (one backend process per connection). On a database server with connection limits, this wastes slots. On cloud PostgreSQL (e.g., Supabase, Neon, RDS), idle connections count toward the connection limit and may incur costs.

3. **No operator configuration**: These values cannot be tuned via environment variables or config. A high-traffic gateway deployment may need `MaxOpenConns=50` or `100`. A low-traffic edge deployment may want `MaxOpenConns=5` and `MaxIdleConns=1`. Neither profile is possible today.

### Recommendation

Add `ConnMaxLifetime` and `ConnMaxIdleTime` with sensible defaults, and expose pool sizing via environment variables:

```go
func configurePostgresPool(db *DB, cfg *config.Config) error {
    maxOpen := cfg.DBMaxOpenConns    // default 20
    maxIdle := cfg.DBMaxIdleConns    // default 5
    maxLifetime := cfg.DBConnMaxLifetime  // default 30m
    maxIdleTime := cfg.DBConnMaxIdleTime  // default 5m

    db.SetMaxOpenConns(maxOpen)
    db.SetMaxIdleConns(maxIdle)
    db.SetConnMaxLifetime(maxLifetime)
    db.SetConnMaxIdleTime(maxIdleTime)
    return nil
}
```

The defaults `ConnMaxLifetime=30m` and `ConnMaxIdleTime=5m` mirror pgx pool defaults and are suitable for production behind typical cloud load balancers (which often enforce 10-60 minute idle timeouts).

---

## Finding 3: No statement timeout configured

**Severity**: MEDIUM
**File**: `store/open.go`, lines 93-150 (no statement_timeout anywhere)

### Current behavior

Neither `open.go` nor any other file in the codebase sets a PostgreSQL `statement_timeout`. The pgx driver does not set one by default. PostgreSQL's default `statement_timeout` is `0` (no limit).

### Problems

1. **Runaway query risk**: A query that accidentally scans a large table without an index, or a query blocked by a lock, will run indefinitely. It occupies one of the `MaxOpenConns` pool slots and holds a PostgreSQL backend process. If all 20 slots are consumed by hung queries, the entire application is deadlocked -- no new queries can be executed.

2. **No application-level guard**: The application has no `context.WithTimeout` wrapping database calls. While the pgx driver supports context cancellation, the codebase's query functions do not pass deadline contexts.

3. **Production risk**: In production, a single unindexed query (e.g., a naive `SELECT * FROM proxy_logs WHERE created_at < ?` without a covering index) could take minutes on a large table. Without statement_timeout, this query runs to completion, consuming I/O and CPU on the database server and blocking the application.

### Recommendation

Two complementary approaches:

**(a) Set a server-side default via DSN options:**

```go
// Append pgx runtime params for statement_timeout
if !strings.Contains(connStr, "options=") {
    connStr += "?options=-c%20statement_timeout%3D30s"
}
```

Or use pgx's `AfterConnect` hook to execute `SET statement_timeout = '30s'` on each new connection.

**(b) Add context deadlines at the application layer:**

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
rows, err := db.QueryContext(ctx, query, args...)
```

Approach (a) is the stronger guarantee -- it protects even queries issued without context deadlines.

---

## Finding 4: `sqlx.BindDriver` is a global side-effect called repeatedly

**Severity**: LOW
**File**: `store/open.go`, lines 122-124

### Current behavior

```go
if driverName == "pgx" {
    sqlx.BindDriver("pgx", sqlx.DOLLAR)
}
```

This is called every time `Open()` is invoked for PostgreSQL (including rollbacks in `switch.go`). `sqlx.BindDriver` mutates a global `sync.Map` in the `sqlx` package. While technically idempotent (same `DOLLAR` value each call), it is:
- Not needed after the first call in the process lifetime.
- A global side-effect buried inside what looks like a constructor.
- Potentially confusing if other parts of the codebase register different bind styles for the same driver name.

### Recommendation

Move the `sqlx.BindDriver` call to an `init()` function or call it once in `EnsureRuntimeDatabase` before the first `Open()`:

```go
func init() {
    sqlx.BindDriver("pgx", sqlx.DOLLAR)
}
```

---

## Finding 5: No connection health validation on pool borrow

**Severity**: LOW
**File**: Not present

### Current behavior

After initial `db.Ping()` in `Open()`, individual connections in the pool are never validated before being handed out for queries. `database/sql` does not validate connections on borrow by default.

pgx/v5 supports automatic connection health checking via `ConnConfig.AfterConnect` and by setting pool-level health check configuration, but none of these are configured.

### Problem

If the PostgreSQL server restarts (e.g., for a minor version upgrade, or a crash-recovery cycle), the 5 idle connections in the pool all hold dead TCP sockets. The next query that borrows one will fail, `database/sql` will retry on a new connection, but the first attempt wastes time. In bursty traffic scenarios where all 20 connections are dead simultaneously (PG restart while app was idle), the first 20 queries all fail and retry, causing a latency spike.

### Recommendation

Pass a connection validator to pgx or configure a shorter `ConnMaxLifetime` (as recommended in Finding 2) to ensure connections are rotated before they can accumulate too much stale state.

---

## Finding 6: No connection pool metrics or observability

**Severity**: LOW
**File**: Not present

### Current behavior

The pool statistics (`db.Stats()`) are never read or exported. There is no way to monitor:
- `OpenConnections` -- how many connections are actually open
- `InUse` -- how many are currently executing queries
- `Idle` -- how many are idle in the pool
- `WaitCount` / `WaitDuration` -- how many callers have waited for a connection (indicates pool exhaustion)
- `MaxIdleClosed` / `MaxLifetimeClosed` -- how many connections were closed due to limits

Without these metrics, operators cannot tune pool sizes and cannot detect pool saturation before it causes production issues.

### Recommendation

Expose `db.Stats()` via a `/health/db` endpoint or log it periodically.

---

## Issue Matrix

| # | Issue | Severity | File | Line(s) | Previously Reported |
|---|---|---|---|---|---|
| 1 | sslmode boolean -- no verify-full | HIGH | store/open.go, config/config.go | 93-113, 360 | No |
| 2 | No ConnMaxLifetime / ConnMaxIdleTime | HIGH | store/open.go | 169-173 | audit-memory.md, audit-resilience.md |
| 3 | No statement_timeout | MEDIUM | store/open.go | -- | No |
| 4 | Repeated sqlx.BindDriver side-effect | LOW | store/open.go | 122-124 | No |
| 5 | No connection health validation | LOW | -- | -- | Partial (audit-resilience.md mentions no Ping before query) |
| 6 | No pool metrics | LOW | -- | -- | No |

## Remediation Priority

1. **IMMEDIATE (P0)**: Add `ConnMaxLifetime` and `ConnMaxIdleTime` to `configurePostgresPool` (Finding 2). This is the most impactful single change -- it prevents silent connection breakage in production after PG restarts or network events. Estimated fix: 5 lines of code, no breaking change.

2. **HIGH (P1)**: Replace `sslMode bool` with `sslMode string` supporting `disable/require/verify-ca/verify-full` (Finding 1). This is a breaking change to the `Open()` function signature and requires updates to all call sites (`store/open.go`, `store/switch.go`, `store/bootstrap.go`, `config/config.go`, and all test files). Estimated fix: 10-15 call sites.

3. **MODERATE (P2)**: Add `statement_timeout` connection configuration (Finding 3). Estimate: 3-5 lines in `configurePostgresPool` or DSN assembly.

4. **NICE-TO-HAVE (P3)**: Move `sqlx.BindDriver` to `init()` (Finding 4), expose pool metrics (Finding 6).

## Call-Site Impact Analysis

`store.Open()` has the following call sites. Changes to its signature affect:

| Caller | File | Notes |
|---|---|---|
| `EnsureRuntimeDatabase` | store/bootstrap.go:69 | Production entry point |
| `SwitchDatabase` | store/switch.go:45 | Runtime DB switch |
| `rollbackSwitch` | store/switch.go:85 | Rollback path |
| Tests (13 sites) | Various `*_test.go` | All use `DialectSQLite, ":memory:", false` |

---

## References

- [pgx/v5 connection pool documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool)
- [Go database/sql DB.Stats](https://pkg.go.dev/database/sql#DB.Stats)
- [PostgreSQL sslmode documentation](https://www.postgresql.org/docs/current/libpq-ssl.html)
- [PostgreSQL statement_timeout](https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-STATEMENT-TIMEOUT)
- Prior audits: `audit-memory.md` (Finding 2), `audit-resilience.md` (Finding 1, Pool)
