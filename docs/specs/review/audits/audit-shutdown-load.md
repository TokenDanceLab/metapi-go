# Audit: Graceful Shutdown Under Streaming Load

**Date:** 2026-07-05
**Auditor:** E2E Test Suite
**Test File:** `e2e/e2e_shutdown_test.go`
**Status:** PASSED

---

## 1. Test Summary

Two end-to-end tests validate the graceful shutdown behavior of the MetAPI Go proxy server when it receives a shutdown signal while streaming proxy requests are in-flight.

### Test 1: `TestShutdownUnderStreamingLoad`

**Scenario:** Start server, send 10 concurrent streaming requests to `/v1/chat/completions`, then trigger shutdown (simulated SIGTERM) while streams are still in-flight.

**Assertions:**

| # | Assertion | Result |
|---|-----------|--------|
| 1 | All 10 in-flight streaming requests return HTTP 200 | PASS |
| 2 | Each client receives 12 SSE data chunks (10 content + finish_reason + [DONE]) | PASS |
| 3 | Each client receives the `[DONE]` SSE marker | PASS |
| 4 | Upstream mock completes all 10 requests | PASS |
| 5 | `store.CloseDatabase()` succeeds with no error | PASS |
| 6 | `store.GetDB()` returns nil after close (singleton reset) | PASS |
| 7 | Database can be re-opened after clean close (idempotency) | PASS |

### Test 2: `TestShutdownRejectsNewConnections`

**Scenario:** Start a real HTTP server, verify it accepts connections, shut it down, then verify new TCP connections are rejected.

**Assertions:**

| # | Assertion | Result |
|---|-----------|--------|
| 1 | `/health` returns 200 before shutdown | PASS |
| 2 | `http.Get()` after shutdown returns connection-refused error | PASS |
| 3 | `store.GetDB()` returns nil after `CloseDatabase()` | PASS |

---

## 2. Architecture

The test uses a layered architecture to simulate the production shutdown path:

```
Client (10 goroutines)
  |
  v
chi Router (/v1/chat/completions)
  |- ProxyAuth middleware (validates global proxy token)
  |- HandleChatCompletions
       |
       v
  dispatchUpstream (proxy/upstream.go)
       |- SelectProxyChannelForAttempt -> mockRouter (static channel)
       |- http.DefaultClient.Do -> mockUpstream (httptest.Server)
       |- handleStreamUpstream (SSE passthrough)
```

**Shutdown trigger:** `http.Server.Shutdown(ctx)` is called after requests are confirmed in-flight, mirroring the `app.App.Start()` path triggered by SIGTERM in production (`cmd/server/main.go` lines 104-108).

**Database:** File-backed SQLite in a temp directory (`MaxOpenConns=1`) to avoid the `:memory:` connection-pool isolation issue where each new pool connection gets its own in-memory database.

---

## 3. Key Findings

### 3.1 What Works Correctly

1. **In-flight request completion:** `http.Server.Shutdown()` correctly waits for active connections to drain. All 10 streaming requests (each ~1.5s duration) completed fully after the shutdown signal.

2. **SSE stream integrity:** Each client received the full SSE stream including content chunks, `finish_reason: "stop"`, and the `[DONE]` marker. No truncated streams.

3. **Database cleanup:** `store.CloseDatabase()` resets the singleton (`GetDB()` returns nil) and the database can be re-opened, confirming clean resource disposal.

4. **Connection rejection:** After `http.Server.Shutdown()`, the TCP listener closes, and new connection attempts receive `connection refused`.

5. **Concurrent proxy routing:** The mock router with `staticChannel` correctly serves all 10 concurrent requests through the same channel without locking issues.

### 3.2 Issues Identified

#### ISSUE 1 (Pre-existing, not caused by this test): `downstream_api_keys` table query returns 500 on DB error

**Location:** `auth/downstream.go` lines 80-89

**Description:** When `getManagedKeyByToken()` encounters a DB error (e.g., table not found, connection issue), `AuthorizeDownstreamToken()` returns a 500 "Internal server error" instead of falling through to the global proxy token check.

**Impact:** If the `downstream_api_keys` table is missing or the DB connection fails, ALL proxy requests fail with 500, even those using the valid global proxy token. This is a resilience regression from the TypeScript implementation.

**Severity:** Medium. The TypeScript version degrades gracefully (table missing -> fall through to global token). The Go version returns 500, creating a hard dependency on the managed keys table.

**Reproduction (discovered during test development):** Using SQLite `:memory:` with default connection pool settings. Each new pool connection opens a separate in-memory database; AutoMigrate creates tables on connection A, but the ProxyAuth middleware on connection B sees an empty database. The `downstream_api_keys` table query fails, and the 500 is returned before the global token check on line 148.

**Recommended Fix:**

```go
// auth/downstream.go lines 79-89
managed, err := getManagedKeyByToken(normalized)
if err != nil {
    slog.Warn("downstream auth: failed to query managed key, falling back to global", "error", err)
    // DO NOT return 500. Fall through to global token check below.
    managed = nil
}
```

This matches the TypeScript resilience pattern where the DB query failure does not block the global token fallback.

#### ISSUE 2 (Pre-existing, not caused by this test): SQLite `:memory:` connection pool isolation

**Location:** `store/open.go` lines 93-143

**Description:** SQLite `:memory:` databases are per-connection. When `database/sql` opens multiple connections from the pool, each gets its own empty database. The AutoMigrate creates tables on the first connection, but concurrent queries on other connections see an empty database.

**Impact:** Any concurrent proxy request (which triggers `ProxyAuth` -> `getManagedKeyByToken`) on a new pool connection will fail because the `downstream_api_keys` table doesn't exist on that connection's in-memory database.

**Severity:** Low for production (production uses file-backed SQLite or PostgreSQL). Medium for test reliability.

**Recommended Fix:** Add `db.SetMaxOpenConns(1)` for SQLite databases in `store.Open()`:

```go
case DialectSQLite:
    db.SetMaxOpenConns(1) // SQLite :memory: databases are per-connection
    if err := applySQLitePragmas(db); err != nil {
        db.Close()
        return nil, fmt.Errorf(...)
    }
```

---

## 4. Production Shutdown Path Analysis

The production shutdown sequence in `app/app.go:Start()` is:

1. **Signal received** (SIGINT/SIGTERM) via `signal.Notify`
2. **`a.OnClose()`** executes registered cleanup functions (FIFO), including `app.StopBackgroundServices()`
3. **`a.Server.Shutdown(ctx)`** with 5-second timeout:
   - Closes idle keep-alive connections
   - Waits for active connections to finish (respecting `ReadTimeout`/`WriteTimeout`)
   - Returns when all connections are drained or timeout expires
4. **`store.CloseDatabase()`** closes the DB pool and resets the singleton

### Verification

The E2E test validates steps 3 and 4. All in-flight streaming requests completed within the shutdown window (no timeout needed - requests completed in ~1.5s, well within the 5s default).

### Edge Cases

| Edge Case | Expected Behavior | Verification Status |
|-----------|-------------------|---------------------|
| Streaming request exceeds shutdown timeout | Connection forcefully closed; client receives truncated stream | NOT TESTED (requires >5s stream) |
| DB close fails | Error logged at WARN level; graceful shutdown proceeds | NOT TESTED |
| Multiple consecutive SIGTERMs | Idempotent; second signal after shutdown is no-op | NOT TESTED |
| Shutdown with no active connections | Immediate clean shutdown | Verified (empty server) |

---

## 5. Test Coverage

```
e2e/e2e_shutdown_test.go:
  TestShutdownUnderStreamingLoad         - PASS (3.31s)
  TestShutdownRejectsNewConnections      - PASS (0.03s)
```

The test file is 575 lines and covers:
- Concurrent streaming request dispatch (10 goroutines)
- SSE stream passthrough with chunk verification
- `http.Server.Shutdown()` graceful drain behavior
- `store.CloseDatabase()` singleton reset
- Database re-open after close (idempotency)
- TCP listener closure after shutdown

---

## 6. Recommendations

1. **P0 (Critical):** Fix ISSUE 1 — `AuthorizeDownstreamToken` should fall through to global token check on DB error, not return 500. This is a resilience regression from TS.

2. **P1 (Important):** Fix ISSUE 2 — Add `MaxOpenConns(1)` for SQLite in `store.Open()` to prevent `:memory:` pool isolation. Affects test reliability.

3. **P2 (Nice-to-have):** Add a test for the "streaming request exceeds shutdown timeout" edge case. Requires a mock upstream with configurable delay >5s.

4. **P2 (Nice-to-have):** Add a test for graceful shutdown with active non-streaming requests.

5. **P2 (Nice-to-have):** Add a test for `CloseDatabase()` failure handling.

---

## 7. Files Modified/Created

| File | Action | Purpose |
|------|--------|---------|
| `e2e/e2e_shutdown_test.go` | Created | Shutdown-under-load E2E tests |
| `e2e/e2e_backup_test.go` | Fixed | Removed duplicate `doPostJSON_Map`, added missing `doGet` helper |
| `docs/specs/review/audits/audit-shutdown-load.md` | Created | This audit report |
