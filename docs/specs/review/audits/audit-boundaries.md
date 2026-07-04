# Boundary Audit: metapi-go Edge Cases and Boundary Conditions

**Audit date**: 2026-07-05
**Scope**: Full repository -- HTTP handlers, store layer, routing, settings, config, timezone handling
**Methodology**: Static code analysis + edge case test suite (29 tests in `handler/admin/edge_cases_test.go`)

---

## 1. Findings Summary

| # | Severity | File | Category | Issue |
|---|----------|------|----------|-------|
| B1 | HIGH | `handler/admin/settings.go:625-634` | Data Race | `upsertSettingDB` uses SELECT-then-INSERT/UPDATE (TOCTOU); racy under concurrent writes |
| B2 | HIGH | `router/middleware.go` | Missing Defense | `RequestBodyLimit` (20MB default) is configured but never enforced -- no `http.MaxBytesReader` anywhere |
| B3 | MEDIUM | `handler/admin/sites.go:45-53`, `handler/admin/accounts.go:79-115` | Scalability | No pagination on list endpoints; negative page/limit params silently ignored |
| B4 | MEDIUM | `config/config.go:361` | Config Drift | `Tz` config field is captured but never applied -- `time.Now()` always uses system local timezone |
| B5 | LOW | `handler/admin/settings.go:537` | Error Handling | `testSystemProxy` ignores `json.NewDecoder` decode error; silent failure on malformed body |
| B6 | LOW | `handler/admin/settings.go:517` | Error Handling | `logSettingsEvent` silently ignores DB errors (matches TS behavior but loses audit trail) |
| B7 | LOW | `store/schema.go` | Data Integrity | `accounts` table has no UNIQUE constraint on `(site_id, access_token)` -- duplicate credentials accepted |

---

## 2. Detailed Analysis

### B1 [HIGH] `upsertSettingDB` has TOCTOU race condition

**File**: `handler/admin/settings.go`
**Lines**: 625-634

```go
func upsertSettingDB(db *sqlx.DB, key string, value any) {
    jsonValue, _ := json.Marshal(value)
    var count int
    db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", key)  // read
    if count > 0 {
        db.Exec("UPDATE settings SET value = ? WHERE key = ?", ...)      // write
    } else {
        db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", ...)  // write
    }
}
```

Two concurrent goroutines for the same key can both see `count=0`, then both attempt INSERT. The second INSERT gets a UNIQUE constraint violation.

**Impact**: Under concurrent load (e.g., multiple admin API calls to `PUT /api/settings/runtime`), the race causes:
- Lost settings updates (first writer's value overwritten by second)
- UNIQUE constraint errors on the losing side
- Admin UI error flashes ("设置更新失败" with no clear error)

**Evidence**: The `TestEdge_UpsertSettingDB_RaceCondition` test documents the pattern. While WAL mode serializes light concurrency, the TOCTOU window is structurally present.

**Fix**: Use the existing `SettingsStore.Set` method instead, which uses proper `ON CONFLICT DO UPDATE`:

```go
// Replace upsertSettingDB body with:
func upsertSettingDB(db *sqlx.DB, key string, value any) {
    jsonValue, _ := json.Marshal(value)
    settingsStore := store.NewSettingsStore(&store.DB{DB: db, Dialect: store.DialectSQLite})
    settingsStore.Set(key, string(jsonValue))
}
```

Or equivalently, use raw SQL with `ON CONFLICT`:

```go
db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(jsonValue))
```

**Note**: This fix requires detecting the dialect (SQLite vs Postgres) for the `ON CONFLICT` syntax. The `SettingsStore.Set` method already handles this.

---

### B2 [HIGH] `RequestBodyLimit` not enforced -- no body size limit in middleware

**File**: `router/middleware.go`, `config/config.go:127,422`

`RequestBodyLimit` is read from config and defaults to 20MB (`config/defaults.go:18`), but it is **never wired** to any HTTP middleware. There is no `http.MaxBytesReader` anywhere in the request pipeline.

**Impact**:
- An attacker can send an arbitrarily large POST body (e.g., 10GB) to any endpoint
- The server will attempt to allocate memory for the full body, leading to OOM
- Even non-malicious oversized uploads (e.g., a misconfigured client sending a huge log) can crash the process

**Evidence**: The `TestEdge_MaxBodyLimitNotEnforced` test confirms that `RequestBodyLimit` is configured (20MB) but requests exceeding this limit are NOT rejected at the middleware level.

**Fix**: Add a body-size-limiting middleware in `router/router.go`:

```go
// In router.New(), add before the route groups:
r.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
            r.Body = http.MaxBytesReader(w, r.Body, int64(cfg.RequestBodyLimit))
        }
        next.ServeHTTP(w, r)
    })
})
```

---

### B3 [MEDIUM] No pagination on list endpoints; negative params ignored

**Files**: `handler/admin/sites.go:45-53`, `handler/admin/accounts.go:79-115`, `handler/admin/account_tokens.go`, `handler/admin/downstream_keys.go`

None of the `GET /api/*` list endpoints accept `page`, `limit`, or `offset` query parameters. They return ALL records in a single response. Negative values for these parameters are silently ignored because no parsing logic exists.

**Impact**:
- With sufficiently many sites/accounts/tokens, the response can grow to megabytes
- No `offset` or `limit` means no cursor-based or offset-based pagination
- Admin UI must load all data into memory for display
- Database load scales linearly with record count (no `LIMIT` clause)

**Evidence**: `TestEdge_NoPaginationOnListSites` creates 50 sites and confirms all are returned. `TestEdge_NegativePaginationIgnored_Accounts` confirms `?page=-5&limit=-10` is silently accepted.

**Current state**: The `listAccounts` handler does have a 30-second response cache (`globalAccountsCache`), which mitigates database load but not response size.

**Fix**: Add `page`/`pageSize` query parameter parsing with validation:

```go
page, _ := strconv.Atoi(r.URL.Query().Get("page"))
pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
if page < 1 { page = 1 }
if pageSize < 1 || pageSize > 100 { pageSize = 50 }
offset := (page - 1) * pageSize
// Append to query: LIMIT ? OFFSET ?
```

---

### B4 [MEDIUM] `Tz` config field captured but never applied

**File**: `config/config.go:361`

```go
cfg.Tz = get("TZ")  // captured but NEVER used to set timezone
```

Go's `time.Now()` always uses the **system's** local timezone. To honor a configured timezone, the program must call `time.LoadLocation()` and use `time.Now().In(loc)`. This is never done.

**Impact**:
- If the server is configured with `TZ=Asia/Shanghai` but the OS timezone is UTC, `FormatLocalDateTime()` (in `service/localtime.go`) will still show UTC times
- Cron schedules (`CheckinCron`, `BalanceRefreshCron`) may fire at unexpected local times
- Notifications show mismatched timezones

**Evidence**: `TestEdge_TZConfigFieldUnused` confirms `Tz` is empty in the default config (not set) and Go uses the system timezone (CST).

**Positive finding**: All timestamps stored in the database use `time.Now().UTC().Format(time.RFC3339)` consistently. The UTC storage layer is correct; only the local-time display layer has the gap.

**Fix**: Apply `Tz` config during boot in `cmd/server/main.go`:

```go
if cfg.Tz != "" {
    loc, err := time.LoadLocation(cfg.Tz)
    if err != nil {
        slog.Warn("invalid TZ config, using system local", "tz", cfg.Tz)
    } else {
        time.Local = loc
    }
}
```

---

### B5 [LOW] `testSystemProxy` ignores JSON decode errors

**File**: `handler/admin/settings.go:532-561`

```go
func (h *settingsHandler) testSystemProxy(w http.ResponseWriter, r *http.Request) {
    var body struct {
        ProxyUrl *string `json:"proxyUrl"`
    }
    json.NewDecoder(r.Body).Decode(&body)  // error ignored
    // ...
}
```

If the request body is malformed JSON, `body.ProxyUrl` will be `nil` and the handler proceeds with the config default. The caller receives a success response with bogus latency data without knowing the body was rejected.

**Impact**: Low -- this is an admin-only test endpoint. But it can produce misleading "proxy reachable" results when the request body was unintelligible.

**Fix**: Check the decode error:

```go
if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
    writeJSON(w, http.StatusBadRequest, map[string]any{
        "success": false, "message": "Invalid request body",
    })
    return
}
```

---

### B6 [LOW] `logSettingsEvent` silently drops DB errors

**File**: `handler/admin/settings.go:636-640`

```go
func logSettingsEvent(db *sqlx.DB, ...) {
    // Silently ignore errors (matches TS behavior)
    db.Exec(...)
}
```

The comment acknowledges this matches the TS behavior, but the Go runtime should at minimum log the error. A full `events` table or a locked database would silently swallow audit trail entries.

**Fix**: Log at debug level:

```go
if _, err := db.Exec(...); err != nil {
    slog.Debug("failed to log settings event", "err", err, "type", eventType)
}
```

---

### B7 [LOW] No UNIQUE constraint on duplicate account credentials

**File**: `store/schema.go`, `store/migrate.go`

The `accounts` table has no unique constraint preventing duplicate `(site_id, access_token)` pairs. A user can import the same credential twice, creating two accounts pointing to the same upstream token.

**Impact**: Low -- typically caught by the admin UI flow. But programmatic API calls can create duplicates silently.

**Evidence**: `TestEdge_DuplicateAccountCreation` confirms that posting the same `(siteId, accessToken)` twice creates two separate account rows.

**Fix**: Add a partial unique index (SQLite and Postgres both support):

```sql
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_site_token
    ON accounts (site_id, access_token)
    WHERE access_token IS NOT NULL AND access_token != '';
```

---

## 3. Edge Case Test Suite

A new test file `handler/admin/edge_cases_test.go` (29 tests) covers the boundaries identified. Tests are organized by category:

| Category | Test Count | Key Tests |
|----------|-----------|-----------|
| Empty Request Body | 5 | CreateSite, CreateAccount, UpdateRuntime, BatchAccounts, Login |
| Oversized Request | 2 | 5MB body, limit-not-enforced gap |
| Negative Pagination | 2 | No pagination exists, negative params ignored |
| SQLite Concurrent Writes | 3 | Racy upsert, safe UPSERT, concurrent accounts |
| Duplicate Unique Key | 2 | Site conflict (409), account duplicates (accepted) |
| NULL in JSON | 2 | NULL fields serialize as null, optional fields |
| Timezone Consistency | 3 | UTC timestamps, TZ config unused, generatedAt is UTC |
| Concurrent Settings | 1 | TOCTOU race in upsertSettingDB |
| Route Rebuild Race | 2 | Cache read/write safety, rebuild stub safety |
| Additional Edges | 7 | JSON injection, Unicode/emoji, nil pointers, zero values, long strings, concurrent create+delete, malformed bodies |

### Test Results

```
=== 29 tests, 29 PASS, 0 FAIL ===
Total test time: ~0.45s (in-memory SQLite)
```

All tests use in-memory SQLite (`:memory:`) with WAL mode. No external PostgreSQL required.

To run:
```bash
go test ./handler/admin/ -run TestEdge -v -count=1
```

---

## 4. Timezone Audit: UTC Storage, Local Display Gap

All timestamp storage uses `time.Now().UTC().Format(time.RFC3339)`:

| File | Line Count | Consistent? |
|------|-----------|-------------|
| `handler/admin/sites.go` | 5 call sites | Yes (UTC) |
| `handler/admin/accounts.go` | 7 call sites | Yes (UTC) |
| `handler/admin/settings.go` | 1 call site | Yes (UTC) |
| `handler/admin/account_tokens.go` | 2 call sites | Yes (UTC) |
| `handler/admin/downstream_keys.go` | 3 call sites | Yes (UTC) |
| `service/site_service.go` | 5 call sites | Yes (UTC) |
| `service/account_service.go` | 3 call sites | Yes (UTC) |
| `routing/router.go` | 4 call sites | Yes (UTC) |
| `scheduler/usage_aggregation.go` | 6 call sites | Yes (UTC) |
| All other files | All `time.Now().UTC()` | Yes |

**Local-time functions** (`service/localtime.go`) use `time.Now()` (system local). These are display-only and not used for storage. The gap is that `Tz` config is not applied to `time.Local`.

---

## 5. Request Body Limit: Full Audit

| Layer | File | Limit Applied? |
|-------|------|---------------|
| Config definition | `config/config.go:127` | Defined (`RequestBodyLimit int`) |
| Default value | `config/defaults.go:18` | 20 MB |
| Config load | `config/config.go:422` | `cfg.RequestBodyLimit = DefaultRequestBodyLimit` |
| Middleware | `router/middleware.go` | **NOT applied** |
| Handler | All `handler/admin/*.go` | **NOT applied** |
| Handler | All `handler/proxy/*.go` | **NOT applied** |
| Router setup | `router/router.go` | **NOT applied** |

The field exists and is configured but has no runtime effect. This is a complete wiring gap.

---

## 6. SQLite Locking: Concurrent Write Safety

| Scenario | Mechanism | Safe? |
|----------|-----------|-------|
| WAL mode | `PRAGMA journal_mode = WAL` | Concurrent reads + 1 writer |
| Busy timeout | `PRAGMA busy_timeout = 5000` | 5s wait on lock |
| Foreign keys | `PRAGMA foreign_keys = ON` | Cascade deletes tested |
| `SettingsStore.Set` | `ON CONFLICT DO UPDATE` | **Safe** (atomic UPSERT) |
| `upsertSettingDB` | SELECT + INSERT/UPDATE | **Racy** (TOCTOU) |
| Route cache | `sync.RWMutex` | **Safe** (in-process only) |
| Global accounts cache | `sync.RWMutex` | **Safe** (30s TTL) |
| Config singleton | `sync.RWMutex` | **Safe** |

The `upsertSettingDB` function (B1) is the only identified unsafe concurrent write path.

---

## 7. Route Rebuild + Proxy Request Race

**File**: `service/site_service.go:373-376`, `routing/cache.go`

`RebuildRoutesBestEffort()` is currently a **stub** (empty function body). When implemented:
- It must coordinate with `RouteCache.InvalidateAll()` (already has `sync.RWMutex`)
- Proxy requests reading from `RouteCache.GetRoutes()` hold `RLock` and will not see partial rebuilds
- The stable-first state maps in `routing/weights.go` use `sync.RWMutex` (confirmed real mutex, not stub -- the concurrency audit F1 may have been fixed)

**Current risk**: None (stub). **Future risk**: Low if implemented with existing `RouteCache` API.

---

## 8. Recommendations (Priority Order)

1. **B2 (HIGH) -- Wire `RequestBodyLimit`**: Add `http.MaxBytesReader` middleware in `router.go`. This is a one-line change with high security/robustness impact.

2. **B1 (HIGH) -- Fix `upsertSettingDB` TOCTOU**: Replace the racy SELECT+INSERT/UPDATE with `ON CONFLICT DO UPDATE`. The `SettingsStore.Set` method already has the correct implementation -- use it, or duplicate its UPSERT SQL.

3. **B3 (MEDIUM) -- Add pagination to list endpoints**: Add `page`/`pageSize` query params to `listSites`, `listAccounts`, `listAccountTokens`, `listDownstreamKeys`. Clamp negative values.

4. **B4 (MEDIUM) -- Apply `Tz` config**: Load the configured timezone in `main.go` and set `time.Local`.

5. **B5 (LOW) -- Check decode errors in `testSystemProxy`**: One-line fix.

6. **B6 (LOW) -- Log errors from `logSettingsEvent`**: Change `silently ignore` to `slog.Debug`.

7. **B7 (LOW) -- Add UNIQUE index on `accounts (site_id, access_token)`**: Migration + handler-level duplicate check.

---

## 9. Files Reviewed

| File | Lines Reviewed |
|------|---------------|
| `handler/admin/settings.go` | 641 |
| `handler/admin/sites.go` | 799 |
| `handler/admin/accounts.go` | 894 |
| `handler/admin/test.go` | 113 |
| `handler/admin/payloads/accounts.go` | 73 |
| `router/middleware.go` | 48 |
| `router/router.go` | 139 |
| `config/config.go` | 567 |
| `config/defaults.go` | 48 |
| `store/schema.go` | 464 |
| `store/open.go` | 176 |
| `store/setting_store.go` | 82 |
| `store/sqlite_test.go` | 331 |
| `service/localtime.go` | 74 |
| `service/site_service.go` | 459 |
| `routing/router.go` | 814 |
| `routing/cache.go` | 139 |
| `handler/admin/edge_cases_test.go` | 906 (new) |
| **Total** | ~7,800 lines |
