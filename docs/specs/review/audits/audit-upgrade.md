# MetAPI Go Upgrade Safety Audit: v0.2.0 to v0.3.0

**Date**: 2026-07-05
**Tags compared**: `v0.2.0` vs `v0.3.0`
**Commits**: `e1e435e` (README rewrite + audit round 2), `0f18a1e` (neat-freak cleanup), `f65aed7` (doc.go package name fix)

---

## Executive Summary

**Risk Level: LOW.** This is a documentation, cleanup, and validation-hardening release. There are zero database schema changes, zero config key additions/removals, zero dependency changes, and zero API breaking changes. The only upgrade hazard is the new startup config validation, which may reject previously-tolerated invalid configs.

**Rollback safety: SAFE.** No persistent state is mutated. Downgrading from v0.3.0 to v0.2.0 requires no migration reversal, no data repair, and no config rollback.

---

## 1. DB Migration Compatibility

### 1.1 Schema DDL: Unchanged

| File | Lines | v0.2.0 SHA | v0.3.0 SHA | Diff |
|------|-------|------------|------------|------|
| `store/migrate.go` | 1267 | -- | -- | **IDENTICAL** |
| `store/schema.go` | 464 | -- | -- | **IDENTICAL** |

The `AutoMigrate` function and all 27 table DDL builders, all 67 `CREATE INDEX` statements, type helpers (`btype`, `rtype`, `serialPK`, `textPK`, `isPG`), and the `Migrate` stub are byte-for-byte identical between tags. No new tables, no new columns, no altered constraints, no new indexes.

### 1.2 Idempotency Check

All DDL uses `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS`. Re-running v0.3.0 on a database previously initialized by v0.2.0 is a **no-op** -- the SQL layer silently skips all statements.

### 1.3 Settings Table

```sql
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT
)
```

Unchanged. Both versions read and write the same two-column schema. No settings keys were added, removed, or repurposed.

### 1.4 Go Struct Definitions

All 27 Go struct types in `store/schema.go` are identical. No fields added, removed, renamed, or retyped. The `db:"..."` and `json:"..."` tags are unchanged, guaranteeing that SQL scan and JSON serialization produce the exact same output.

**Verdict: PASS -- zero schema risk.**

---

## 2. Config Key Changes

### 2.1 Config Struct: Unchanged

| File | Lines | v0.2.0 SHA | v0.3.0 SHA | Diff |
|------|-------|------------|------------|------|
| `config/config.go` | 567 | -- | -- | **IDENTICAL** |
| `config/defaults.go` | -- | -- | -- | **IDENTICAL** |

All 100+ config fields, all parse helpers (`parseBoolean`, `parseNumber`, `parseCsvList`, `parseDbType`, etc.), all `Load()` logic, and all default constants are identical. No env var was added, removed, or changed in its parsing behavior.

### 2.2 New: Startup Validation (Critical Finding)

`config/validate.go` is the single most impactful addition in v0.3.0. `cmd/server/main.go` now calls `cfg.Validate()` before the database opens, and **exits with code 1 on critical errors**.

#### Critical (fatal) validations:

| Check | Condition | Impact |
|-------|-----------|--------|
| `PORT` | Must be 1--65535 | A zero or negative PORT value that previously caused a silent bind failure now exits with a clear error. |
| `CHECKIN_SCHEDULE_MODE` | Must be `"cron"` or `"interval"` | A misspelled value (e.g. `"Interval"` with capital I, or `"intervl"`) now exits. v0.2.0 silently defaulted to `"cron"` for unrecognized values. |
| `DB_TYPE` | Must be `"sqlite"` or `"postgres"` | Setting `DB_TYPE=mysql` was previously silently parsed as `"mysql"` (a no-op dialect that behaved like SQLite). Now it is a **fatal startup error**. |

**IMPORTANT**: There is a known discrepancy between `Load()` and `Validate()`:
- `parseDbType("mysql")` returns `"mysql"` (allowed by the loader)
- `Validate()` treats `"mysql"` as critical (not in `{"sqlite", "postgres"}`)

If any deployment had `DB_TYPE=mysql`, it would start fine under v0.2.0 but **refuse to start under v0.3.0**. Operators should verify their `DB_TYPE` is a valid value before upgrading.

#### Warning (non-fatal) validations:

| Check | Condition |
|-------|-----------|
| `CHECKIN_CRON` | Must be a valid 6-field cron expression |
| `BALANCE_REFRESH_CRON` | Must be a valid 6-field cron expression |
| `LOG_CLEANUP_CRON` | Must be a valid 6-field cron expression |
| `NOTIFY_COOLDOWN_SEC` | Must be >= 0 |
| `PROXY_FIRST_BYTE_TIMEOUT_SEC` | Must be >= 0 |
| `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` | Must be >= 0 |
| `CHECKIN_INTERVAL_HOURS` | Must be in [1, 24] |
| `BASE_WEIGHT_FACTOR` | Must be >= 0 |
| `VALUE_SCORE_FACTOR` | Must be >= 0 |
| `COST_WEIGHT` | Must be >= 0 |
| `BALANCE_WEIGHT` | Must be >= 0 |
| `USAGE_WEIGHT` | Must be >= 0 |

These produce `WARN` log lines but do not prevent startup. An upgrade may surface previously-hidden config issues in the logs.

**Verdict: PASS -- no config keys changed, but new validation may reject previously-tolerated invalid configs.**

---

## 3. API Breaking Changes

### 3.1 HTTP Endpoints: Unchanged

The `router/router.go` diff is purely cosmetic -- the import alias `proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"` was dropped in favor of the bare import `"github.com/tokendancelab/metapi-go/handler/proxy"`. This is possible because the package's own `package` declaration changed from `package proxy` to `package proxyhandler`. Go imports by directory path, not package name, so **external consumers are unaffected**.

### 3.2 JSON API Surface: Unchanged

The HTTP path structure, request/response shapes, and error formats are unchanged. No routes were added or removed.

### 3.3 Go Public API: No Exported Changes

- `store.AutoMigrate(db *DB) error` -- identical signature
- `store.Migrate(cfg *config.Config) error` -- identical signature
- `config.Get() *Config` -- identical
- `config.Set(cfg *Config)` -- identical
- `config.IsCritical(err error) bool` -- **new** but only used internally in `main.go`

No exported function, type, or constant was removed or changed.

**Verdict: PASS -- zero API breaking changes.**

---

## 4. Runtime Behavioral Changes

### 4.1 Model Swap Optimization (handler/proxy/upstream.go)

| v0.2.0 | v0.3.0 |
|--------|--------|
| `cloneAndSetModel(ctx.Body, upstreamModel)` | `swapModelInJSON(ctx.RawBody, upstreamModel)` |
| Deep map copy + full Marshal | Shallow JSON re-encode via `json.RawMessage` |
| `strings.NewReader(string(b))` | `bytes.NewReader(b)` |

Risk assessment: **LOW.** The `swapModelInJSON` function replaces the `"model"` key in a JSON body. It uses `json.RawMessage` to avoid deep-unmarshalling nested values, then re-encodes. The output is structurally equivalent JSON.

Edge case: If the incoming body is empty, v0.3.0 synthesizes `{"model":"<upstreamModel>"}` whereas v0.2.0 would have panicked on an empty body map access. This is a **bug fix**, not a regression.

### 4.2 Lease TTL Goroutine Pattern (proxy/session.go)

| v0.2.0 | v0.3.0 |
|--------|--------|
| `createTrackedLease`: one-shot timer goroutine, exits after first expiry | `createTrackedLease`: loop-based goroutine, resets timer on each `Touch` via `expiryCh` |
| `touchLease`: spawns a **new** goroutine with a fresh timer on every call | `touchLease`: sends one token to the existing goroutine's `expiryCh` |

Risk assessment: **LOW.** This is a correctness fix. In v0.2.0, calling `touchLease` spawned orphan goroutines (the old timer goroutines were never stopped). In v0.3.0, the lease has exactly one timer goroutine for its lifetime, and `Touch` resets it. The API contract (`Touch()` resets the TTL) is preserved.

### 4.3 Package Renames and doc.go Additions

Multiple packages gained `doc.go` files (`auth`, `platform`, `proxy`, `routing`, `scheduler`, `service`, `transform`, `config`, `store`, `e2e`). The `handler/proxy` package's internal name changed from `proxy` to `proxyhandler`. These have **zero runtime impact** -- they affect only `go doc` output and IDE tooltips.

### 4.4 Unused Imports Cleaned Up

`handler/proxy/upstream.go` removed:
```go
var _ = fmt.Sprintf
var _ = json.Marshal
var _ = context.Background
```
These were compile-time guards for unused imports that are no longer needed after the `swapModelInJSON` refactor. No behavioral change.

**Verdict: PASS -- behavioral changes are correctness fixes or no-ops.**

---

## 5. Dependency Changes

`go.mod` is identical between v0.2.0 and v0.3.0. No new dependencies, no version bumps.

```
$ git diff v0.2.0..v0.3.0 -- go.mod
(empty)
```

**Verdict: PASS -- zero dependency changes.**

---

## 6. Rollback Procedure

Rolling back from v0.3.0 to v0.2.0:

1. Stop v0.3.0 process
2. Deploy v0.2.0 binary
3. Start v0.2.0 process

**No additional steps needed.** There are:
- No DB migrations to reverse
- No config values to reset
- No settings table rows to delete
- No file format changes

The v0.2.0 binary will start successfully against any database that was used by v0.3.0, because the schema is identical.

---

## 7. Upgrade Checklist

| Item | Status | Action |
|------|--------|--------|
| DB schema migration | None needed | -- |
| Config key audit | No changes | -- |
| `DB_TYPE` value check | **Verify** | Ensure `DB_TYPE` is `sqlite` or `postgres`, not `mysql` |
| `CHECKIN_SCHEDULE_MODE` check | **Verify** | Ensure value is exactly `cron` or `interval` (lowercase) |
| `PORT` check | **Verify** | Ensure port is in valid range |
| Cron expression validity | Review warnings | Check `CHECKIN_CRON`, `BALANCE_REFRESH_CRON`, `LOG_CLEANUP_CRON` are valid 6-field expressions |
| Negative value check | Review warnings | Check routing weights and cooldown values are >= 0 |
| Dependency update | Not needed | -- |
| Binary replacement | Standard | Replace binary, restart |
| Smoke test | Standard | Verify proxy endpoint responds, admin UI loads |
| Rollback dry-run | Standard | Keep v0.2.0 binary available |

---

## 8. Summary

| Dimension | Finding | Severity |
|-----------|---------|----------|
| DB migrations | Zero changes -- `migrate.go` and `schema.go` identical | None |
| Config keys | Zero changes -- `config.go` and `defaults.go` identical | None |
| Config validation | New startup validation may reject invalid `DB_TYPE`/`CHECKIN_SCHEDULE_MODE`/`PORT` | **Medium** (for operators with invalid config) |
| API endpoints | Zero changes | None |
| Dependencies | Zero changes -- `go.mod` identical | None |
| Settings table | Zero changes | None |
| Runtime behavior | Minor optimizations and goroutine leak fixes | Low |
| Rollback | Trivial -- no state mutation | None |

**Overall: Safe to upgrade.** The only pre-upgrade action is verifying that `DB_TYPE`, `CHECKIN_SCHEDULE_MODE`, and `PORT` pass the new `Validate()` checks.
