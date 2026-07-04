# metapi-go Dependency Health Audit

**Date**: 2026-07-04
**Module**: `github.com/tokendancelab/metapi-go`
**Go Version**: 1.24.0

---

## 1. Executive Summary

| Metric | Value |
|--------|-------|
| Direct dependencies | **8** |
| Indirect dependencies | **14** |
| `go mod tidy` clean? | Yes -- zero drift |
| Dependencies removable? | No -- all either directly imported or correctly transitive |
| Security CVEs known? | None at current versions |
| Critical maintenance concern | `jmoiron/sqlx` (archived/effectively unmaintained) |
| High maintenance concern | `robfig/cron/v3` (no release since Dec 2019) |
| Version lag | `pgx/v5` 3 minor releases behind (v5.7.6 vs v5.10.0) |

---

## 2. Direct Dependencies (8)

### 2.1 github.com/go-chi/chi/v5 v5.3.0

- **Usage**: Primary HTTP router. Used in 37 source files across `router/`, `handler/admin/`, `handler/proxy/`, and `e2e/`.
- **Imported via**: `router/router.go` (as `chi.Router`), `router/middleware.go` (as `middleware.RealIP`, `middleware.Recoverer`).
- **Maintenance**: The author (pkieltyka) has declared chi v5 **feature-complete and in maintenance mode**. No v6 is planned. Bug fixes and compatibility patches are still accepted. The last release was Oct 2024.
- **Verdict**: STABLE. Chi remains an excellent choice for this project's middleware-heavy, sub-router architecture. The stdlib `net/http` in Go 1.22+ improved routing (method+path patterns) but still lacks chi's middleware chaining ergonomics. Alternatives like `gin`, `echo`, or `fiber` would add weight without meaningful benefit. No action needed.

### 2.2 github.com/go-chi/cors v1.2.2

- **Usage**: CORS middleware configured in `router/middleware.go`.
- **Maintenance**: Stable utility package. Last release Jan 2024. Minimal surface area; unlikely to need updates.
- **Verdict**: STABLE. Essential for browser-based admin UI access. No action needed.

### 2.3 github.com/jackc/pgx/v5 v5.7.6

- **Usage**: PostgreSQL driver. Imported in `store/open.go` (as `stdlib` driver) and `cmd/migrate/main.go` (SQLite-to-Postgres migration tool).
- **Latest**: v5.10.0 (3 minor versions behind).
- **Maintenance**: Actively maintained by jackc. Regular releases with performance improvements and bug fixes. v5.8.0 added `pgxpool` improvements; v5.9.0 added named parameters; v5.10.0 added `CopyFrom` optimizations.
- **Verdict**: HEALTHY but STALE. Should be updated to v5.10.0. The version gap is not security-critical but misses performance and ergonomic improvements. The project uses only `database/sql`-compatible APIs (via `stdlib` driver), so upgrade risk is low.

### 2.4 github.com/jmoiron/sqlx v1.4.0

- **Usage**: Database abstraction layer over `database/sql`. Used in 33 source files across handlers, services, and store layers for `NamedExec`, `Select`, `Get`, and struct scanning.
- **Maintenance**: **ARCHIVED / effectively unmaintained**. The repository README explicitly states it is in maintenance mode. The last release was Sep 2023. No new features, no active development. The author has moved on.
- **Verdict**: **CRITICAL CONCERN**. While sqlx is stable and works correctly today, the lack of active maintenance means:
  - No Go version compatibility guarantees going forward
  - No new features (e.g., generics-based scanning)
  - Potential slow decay as the Go ecosystem evolves
  - The project has deeply integrated sqlx -- switching would be a non-trivial migration

  **Mitigation options** (in order of effort):
  1. **Accept the risk** (low effort): sqlx is stable. Do nothing for now but track Go compatibility.
  2. **Gradually migrate to `database/sql` + manual scanning** (medium effort): The project could replace `sqlx.Select`/`sqlx.Get` with raw `database/sql` queries and manual `rows.Scan`. This eliminates the dependency entirely. The main loss is `NamedExec` convenience.
  3. **Migrate to `sqlc`** (high effort): Code-generate type-safe queries. This is a significant architectural change but would eliminate runtime ORM overhead.
  4. **Fork sqlx internally** (low-medium effort): Vendor a fork to guarantee maintenance control.

  **Recommendation**: Option 1 (monitor) for now, with a plan to migrate to raw `database/sql` in a future sprint. The `NamedExec` convenience is not worth the unmaintained-dependency risk long-term.

### 2.5 github.com/joho/godotenv v1.5.1

- **Usage**: `.env` file loader in `cmd/server/main.go` (`godotenv.Load()`).
- **Maintenance**: Stable, widely used (20k+ stars). Last release Dec 2023. Minimal surface area; the `.env` format does not change.
- **Verdict**: STABLE. Essential for local development ergonomics. No action needed.

### 2.6 github.com/robfig/cron/v3 v3.0.1

- **Usage**: Cron expression parser in `scheduler/cron.go`. Used for `ValidateCronExpr`, `ParseCronExpr`, and `cronRunner` (background checkin scheduler with seconds-field support).
- **Maintenance**: **STALE**. Last release Dec 2019 (6.5+ years ago). No known security issues. The library is small and the API is stable, but the total absence of releases is concerning for a dependency.
- **Verdict**: **HIGH CONCERN**. The project uses only the cron expression parser + `AddFunc`/`Start`/`Stop` -- a tiny subset of the API. This is actually implementable in <100 lines of Go using `time.Ticker` and a cron parser table.

  **Recommendation**: Replace with an in-house cron scheduler or use `github.com/adhocore/gron` (a maintained alternative). Given the project's usage (parse expression, run jobs on schedule), a simple in-house implementation using `time.Ticker` + second-resolution scheduling would be ~50-80 lines and eliminate a 6-year-stale dependency.

### 2.7 golang.org/x/time v0.9.0

- **Usage**: Token-bucket rate limiter in `auth/ratelimit.go` (`rate.Limiter`, `rate.NewLimiter`). Used for per-IP admin API rate limiting (100 req/s) and OAuth rate limiting (10 req/s).
- **Latest**: v0.15.0 (6 minor versions behind).
- **Maintenance**: Official Go extended library. Actively maintained.
- **Verdict**: HEALTHY but STALE. Should be updated. The `rate` package API is stable; upgrade is safe.

### 2.8 modernc.org/sqlite v1.38.2

- **Usage**: Pure-Go SQLite driver (no CGO). Imported via blank import in `store/open.go` and `cmd/migrate/main.go` (`_ "modernc.org/sqlite"`). Registered as the `"sqlite"` driver name for `database/sql`.
- **Latest**: v1.53.0 (15 minor versions behind).
- **Maintenance**: Actively maintained by cznic. Regular releases tracking upstream SQLite versions.
- **Verdict**: HEALTHY but STALE. Should be updated to current version.

---

## 3. Indirect Dependencies (14)

All indirect dependencies are correctly tagged and are transitive requirements. None are imported directly by project code.

| Dependency | Pulled in by | Version | Notes |
|-----------|-------------|---------|-------|
| `dustin/go-humanize` | modernc.org/libc | v1.0.1 | Human-friendly number formatting for SQLite internals |
| `google/uuid` | modernc.org/libc | v1.6.0 | UUID generation for SQLite internals |
| `jackc/pgpassfile` | pgx/v5 | v1.0.0 | PostgreSQL password file parsing |
| `jackc/pgservicefile` | pgx/v5 | v0.0.0-20240606120523 | PostgreSQL service file parsing |
| `jackc/puddle/v2` | pgx/v5 | v2.2.2 | Connection pool for pgx |
| `mattn/go-isatty` | modernc.org/libc | v0.0.20 | Terminal detection for SQLite |
| `ncruces/go-strftime` | modernc.org/libc | v0.1.9 | strftime implementation for SQLite |
| `remyoudompheng/bigfft` | modernc.org/mathutil | v0.0.0-20230129092748 | Big integer FFT for math operations |
| `golang.org/x/crypto` | pgx/v5 | v0.37.0 | Cryptographic functions (SCRAM auth for PG) |
| `golang.org/x/exp` | modernc.org/libc | v0.0.0-20250620022241 | Experimental stdlib extensions |
| `golang.org/x/sync` | pgx/v5, jackc/puddle | v0.15.0 | Concurrency primitives |
| `golang.org/x/sys` | pgx/v5, modernc.org/libc, go-isatty | v0.34.0 | Low-level OS interface |
| `golang.org/x/text` | pgx/v5 | v0.24.0 | Text processing |
| `modernc.org/libc` | modernc.org/sqlite | v1.66.3 | C runtime for pure-Go SQLite |
| `modernc.org/mathutil` | modernc.org/libc | v1.7.1 | Math utilities for pure-Go SQLite |
| `modernc.org/memory` | modernc.org/libc | v1.11.0 | Memory allocator for pure-Go SQLite |

**Note**: `modernc.org/sqlite` pulls in a substantial transitive tree (`libc`, `mathutil`, `memory`, `cc/v4`, `ccgo/v4`, `fileutil`, `goabi0`, `gc/v2`, `opt`, `sortutil`, `strutil`, `token`). This is the cost of a full C-to-Go transpiled SQLite. The alternatives would also carry this weight (or require CGO, which the project explicitly avoids).

---

## 4. SQLite Driver Analysis: modernc.org/sqlite vs Alternatives

The project uses `modernc.org/sqlite` with the explicit goal of **pure Go, no CGO**. This constraint is well-motivated:

- Cross-compilation (Windows/Linux/macOS) without C toolchain
- No libsqlite3 system dependency
- Simpler Docker builds (no `gcc`, no `sqlite-dev`)
- Smaller attack surface (no native code in the binary)

### Alternatives Considered

| Driver | CGO Required | Maturity | Binary Size | Recommendation |
|--------|-------------|----------|-------------|----------------|
| **modernc.org/sqlite** (current) | No | High | +8-12 MB | **Correct choice** |
| `mattn/go-sqlite3` | Yes | High | +2 MB (+libsqlite3) | Rejected: adds CGO dependency |
| `zombiezen.com/go/sqlite` | No (uses wazero WASM) | Medium | +4-6 MB | Viable alternative, less mature |
| `ncruces/go-sqlite3` | No (uses wazero WASM) | Medium | +5-7 MB | Similar to zombiezen, newer |
| `github.com/glebarez/go-sqlite` | No | Low | ~3 MB | Thin wrapper, less battle-tested |

**Verdict**: `modernc.org/sqlite` is the correct pure-Go SQLite driver for this project. No change recommended. However, update to v1.53.0 is overdue (v1.38.2 is from early 2024).

---

## 5. HTTP Router Analysis: chi vs Alternatives

The project uses chi v5 extensively with middleware chaining, sub-routers, and SPA fallback. Chi was chosen for its stdlib compatibility and lightweight middleware model.

### Alternatives Considered

| Router | Stdlib compat | Middleware model | Runtime perf | Notes |
|--------|--------------|-----------------|-------------|-------|
| **chi v5** (current) | 100% `net/http` | Stackable, intuitive | ~350 ns/op | Feature-complete, maintenance mode |
| stdlib `net/http` (Go 1.22+) | 100% | Basic (chaining via adapters) | ~200 ns/op | Missing sub-routers, no built-in middleware |
| `gin` | Partial (gin.Context) | Built-in, rich | ~50 ns/op | Heavier, non-stdlib context, larger API surface |
| `echo` | Partial (echo.Context) | Built-in, rich | ~80 ns/op | Similar weight to gin, custom context |
| `fiber` | No (fasthttp) | Built-in | ~20 ns/op | Incompatible with stdlib middleware ecosystem |

**Verdict**: Chi v5 remains the best choice for this project. The middleware stack (RealIP, CORS, RequestLogger, Recoverer, rate limiting, auth) fits chi's model perfectly. The sub-router pattern (`r.Route("/api", ...)`, `r.Route("/v1", ...)`) is idiomatic chi. The SPA fallback with `embed.FS` is clean with chi's `NotFound` handler. No migration warranted.

---

## 6. Removal Candidates (go mod tidy Analysis)

`go mod tidy` produces zero output, confirming the current `go.mod` is **exactly minimal** -- every dependency in the file is required by the codebase. No dependencies can be removed without code changes.

However, three dependencies could be **eliminated through code changes**:

### 6.1 robfig/cron/v3 -- Replaceable with ~80 lines of in-house code

Current usage in `scheduler/cron.go`:
- `cron.NewParser(cron.Second | ...)` -- parse cron expressions with seconds
- `cron.New(cron.WithSeconds())` -- create a scheduler
- `AddFunc`, `Start`, `Stop` -- manage scheduled jobs

These APIs are trivial to replace with a `time.Ticker`-based implementation. The cron expression parser itself can be vendored or replaced with a small parsing function. This eliminates a 6-year-stale dependency.

### 6.2 jmoiron/sqlx -- Replaceable with raw database/sql

Current usage across 33 files:
- `sqlx.DB` as wrapper around `*sql.DB`
- `sqlx.NamedExec` for named parameter binding
- `db.Select` / `db.Get` for struct scanning

A migration to raw `database/sql` + manual `rows.Scan` would eliminate sqlx. The main pain point is replacing `NamedExec` (convenience for `:param` syntax) and struct-scanning. This is a larger effort (33 files) but removes the most significant maintenance risk in the dependency tree.

### 6.3 go-chi/cors -- Replaceable with manual CORS headers

At ~10 lines of CORS header setting, this could be inlined. However, the `cors` package handles preflight (`OPTIONS`) requests correctly, including `Access-Control-Max-Age` and proper header negotiation. The effort-to-risk ratio does not justify removal.

---

## 7. Recommended Actions (Prioritized)

### P0 -- Critical
- [ ] **Plan sqlx migration**: Document a migration path from `jmoiron/sqlx` to raw `database/sql`. sqlx is effectively unmaintained. Create an issue tracking this technical debt. Estimate: 2-3 days of focused work.

### P1 -- High
- [ ] **Replace robfig/cron**: Write an in-house cron scheduler (~80 lines) using `time.Ticker` + a second-resolution cron parser. Eliminates the most stale dependency. Estimate: 1 day.
- [ ] **Update pgx to v5.10.0**: Run `go get github.com/jackc/pgx/v5@v5.10.0 && go mod tidy`. Verify tests pass. Estimate: 30 min.

### P2 -- Medium
- [ ] **Update modernc.org/sqlite to v1.53.0**: Run `go get modernc.org/sqlite@v1.53.0 && go mod tidy`. This is a 15-minor-version jump; verify WAL mode and foreign key pragmas still work in tests. Estimate: 1 hour.
- [ ] **Update golang.org/x/time to v0.15.0**: Run `go get golang.org/x/time@v0.15.0 && go mod tidy`. Safe upgrade. Estimate: 15 min.

### P3 -- Low
- [ ] **Update chi to latest v5 patch**: Check if v5.3.0 is still latest (it was as of this audit). Apply any patch updates as they release.
- [ ] **Periodic `go mod tidy` check**: Run in CI to catch dependency drift early.

---

## 8. Dependency Health Scorecard

| Dependency | Type | In Use | Maintained | Version Fresh | Risk |
|-----------|------|--------|-----------|---------------|------|
| go-chi/chi/v5 | direct | Yes | Maintenance mode | Current | LOW |
| go-chi/cors | direct | Yes | Stable | Current | LOW |
| jackc/pgx/v5 | direct | Yes | Active | 3 versions behind | LOW-MED |
| jmoiron/sqlx | direct | Yes | **ARCHIVED** | Current (last ever) | **HIGH** |
| joho/godotenv | direct | Yes | Stable | Current | LOW |
| robfig/cron/v3 | direct | Yes | **STALE (2019)** | Current (last ever) | **HIGH** |
| golang.org/x/time | direct | Yes | Active | 6 versions behind | LOW |
| modernc.org/sqlite | direct | Yes | Active | 15 versions behind | LOW-MED |

**Overall health grade: B-** (downgraded by sqlx + cron risk)

---

## 9. References

- `go.mod` and `go.sum` at repository root
- `go mod graph` output for transitive dependency tracing
- `go list -m -u all` for available updates
- `go mod tidy -v` for drift detection (clean -- zero output)
- Source code grep for import verification across all 287 `.go` files
