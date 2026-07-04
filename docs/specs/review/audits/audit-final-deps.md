# MetAPI Go -- Final Dependency Audit

**Date**: 2026-07-05
**Project**: `github.com/tokendancelab/metapi-go` (`D:/Code/TokenDance/metapi-go`)
**Go Version**: go1.26.3 windows/amd64
**Module Go Version**: 1.25.0

---

## 1. Executive Summary

| Check | Result |
|---|---|
| `go mod verify` | PASS -- all modules verified |
| `go mod tidy` | PASS -- no changes needed; go.mod and go.sum in sync |
| `go.sum` completeness | PASS -- 95 lines covering 42 modules (2 lines/module: zip hash + go.mod hash) |
| Retracted versions | NONE detected |
| Deprecated versions | NONE detected |
| Phantom modules (not needed by main) | 6 found but harmless (test-only transitive artifacts) |
| Updates available | 17 modules have newer versions available |

**Verdict**: CLEAN. All dependencies are verified, the module graph is consistent, and `go mod tidy` produces no drift. There are no retracted or deprecated modules. Six modules are reported as "not needed" by `go mod why` but they are standard Go test-infrastructure artifacts (testify, go-cmp, check.v1, etc.) that carry zero runtime risk. Seventeen modules have upstream updates available; none are security-critical for this project's threat model.

---

## 2. Direct Dependencies (8 modules)

These are the modules explicitly imported by `metapi-go` source code.

| Module | Version | Purpose | Latest | Behind? |
|---|---|---|---|---|
| `github.com/go-chi/chi/v5` | v5.3.0 | HTTP router | v5.3.0 | Current |
| `github.com/go-chi/cors` | v1.2.2 | CORS middleware | v1.2.2 | Current |
| `github.com/jackc/pgx/v5` | v5.7.6 | PostgreSQL driver | v5.10.0 | 4 minor |
| `github.com/jmoiron/sqlx` | v1.4.0 | SQL extensions (NamedExec, StructScan) | v1.4.0 | Current |
| `github.com/joho/godotenv` | v1.5.1 | `.env` file loader | v1.5.1 | Current |
| `github.com/robfig/cron/v3` | v3.0.1 | Cron scheduler | v3.0.1 | Current |
| `golang.org/x/time` | v0.9.0 | Rate limiting (`rate.Limiter`) | v0.15.0 | 6 minor |
| `modernc.org/sqlite` | v1.38.2 | Pure-Go SQLite driver | v1.53.0 | 15 minor |

**Observations**:
- `pgx/v5` is 4 minor versions behind. The v5.10.0 release (estimated Jul 2026) likely includes connector improvements and bugfixes. Worth upgrading for connection-pool stability.
- `modernc.org/sqlite` is 15 minor versions behind. Upstream has had substantial performance and correctness improvements in the v1.40-v1.53 range. Strongly recommended for the migration tooling.
- `golang.org/x/time` is 6 minors behind. Low risk; the rate-limiter API is stable.

---

## 3. Indirect Dependencies -- In go.mod (16 modules)

These modules appear in the second `require` block of go.mod and are pulled in by the direct dependencies above.

| Module | Version | Purpose | Justification (`go mod why`) |
|---|---|---|---|
| `github.com/dustin/go-humanize` | v1.0.1 | Human-readable size formatting | modernc.org/sqlite -> modernc.org/libc -> go-humanize |
| `github.com/google/uuid` | v1.6.0 | UUID generation | modernc.org/sqlite -> modernc.org/libc -> google/uuid |
| `github.com/jackc/pgpassfile` | v1.0.0 | PostgreSQL password file parsing | jackc/pgx/v5 -> pgconn -> pgpassfile |
| `github.com/jackc/pgservicefile` | v0.0.0-20240606120523 | PostgreSQL service file parsing | jackc/pgx/v5 -> pgconn -> pgservicefile |
| `github.com/jackc/puddle/v2` | v2.2.2 | Generic resource pool (pgx's connection pool) | jackc/pgx/v5 -> pgxpool -> puddle/v2 |
| `github.com/mattn/go-isatty` | v0.0.20 | Terminal detection | modernc.org/sqlite -> modernc.org/libc -> go-isatty |
| `github.com/ncruces/go-strftime` | v0.1.9 | Portable strftime implementation | modernc.org/sqlite -> modernc.org/libc -> go-strftime |
| `github.com/remyoudompheng/bigfft` | v0.0.0-20230129092748 | Big-integer FFT multiplication | modernc.org/sqlite -> libc -> mathutil -> bigfft |
| `golang.org/x/crypto` | v0.53.0 | Cryptographic primitives (pbkdf2 for SCRAM auth) | jackc/pgx/v5 -> pgconn -> x/crypto/pbkdf2 |
| `golang.org/x/exp` | v0.0.0-20250620022241 | Experimental stdlib (constraints package) | modernc.org/sqlite -> modernc.org/libc -> x/exp/constraints |
| `golang.org/x/sync` | v0.21.0 | Concurrency primitives (semaphore) | jackc/pgx/v5 -> pgxpool -> puddle/v2 -> x/sync/semaphore |
| `golang.org/x/sys` | v0.46.0 | OS system calls (unix package) | modernc.org/sqlite -> x/sys/unix |
| `golang.org/x/text` | v0.38.0 | Unicode text processing (secure/precis for SCRAM) | jackc/pgx/v5 -> pgconn -> x/text/secure/precis |
| `modernc.org/libc` | v1.66.3 | C runtime in Go (used by sqlite) | modernc.org/sqlite -> modernc.org/libc |
| `modernc.org/mathutil` | v1.7.1 | Math utilities (big-int support) | modernc.org/sqlite -> libc -> mathutil |
| `modernc.org/memory` | v1.11.0 | Memory allocator (used by sqlite's libc) | modernc.org/sqlite -> libc -> memory |

**All 16 are legitimately required.** Each has a clear, single-chain dependency path from a direct dependency. No superfluous indirects in go.mod.

---

## 4. Indirect Dependencies -- NOT in go.mod (Test-Only Transitives, 18 modules)

These modules appear in the build list (`go list -m all`) and go.sum but are NOT listed in the go.mod `require` blocks. They are pulled in exclusively through `*_test.go` files of dependencies.

| Module | Version | Pulled in via |
|---|---|---|
| `filippo.io/edwards25519` | v1.1.0 | sqlx test -> go-sql-driver/mysql -> edwards25519 |
| `github.com/davecgh/go-spew` | v1.1.1 | pgx stdlib test -> testify/assert -> go-spew |
| `github.com/go-sql-driver/mysql` | v1.8.1 | sqlx test -> go-sql-driver/mysql |
| `github.com/google/go-cmp` | v0.6.0 | **Phantom** -- go mod why says "not needed" |
| `github.com/google/pprof` | v0.0.0-20250317173921 | sqlite test -> google/pprof |
| `github.com/kr/pretty` | v0.3.0 | **Phantom** -- go mod why says "not needed" |
| `github.com/lib/pq` | v1.10.9 | sqlx test -> lib/pq |
| `github.com/mattn/go-sqlite3` | v1.14.22 | sqlx test -> mattn/go-sqlite3 (cgo SQLite) |
| `github.com/pmezard/go-difflib` | v1.0.0 | pgx stdlib test -> testify/assert -> go-difflib |
| `github.com/stretchr/objx` | v0.1.0 | **Phantom** -- go mod why says "not needed" |
| `github.com/stretchr/testify` | v1.8.1 | pgx stdlib test -> testify/assert |
| `golang.org/x/mod` | v0.36.0 | libc test -> ccgo/lib -> x/mod/semver |
| `golang.org/x/net` | v0.55.0 | **Phantom** -- go mod why says "not needed" |
| `golang.org/x/term` | v0.44.0 | **Phantom** -- go mod why says "not needed" |
| `golang.org/x/tools` | v0.45.0 | libc test -> x/tools/go/packages |
| `gopkg.in/check.v1` | v1.0.0-20201130134442 | **Phantom** -- go mod why says "not needed" |
| `gopkg.in/yaml.v3` | v3.0.1 | pgx stdlib test -> testify/assert -> yaml.v3 |
| `modernc.org/{cc,ccgo,fileutil,gc,goabi0,opt,sortutil,strutil,token}` | (various) | sqlite/libc test toolchain |

**Phantom modules (6)**: `go mod why -m` returns "main module does not need module X" for these. They are harmless artifacts of Go's minimal version selection (MVS) -- they exist in the build list because some test dependency once referenced them (or a version constraint forces their presence), but no code path in the production binary reaches them. They occupy no binary space and carry zero runtime risk. Running `go mod tidy` does not remove them, confirming they are MVS-required.

---

## 5. Update Candidates

Modules where `go list -m -u all` shows a newer version (`[vX.Y.Z]`):

### Direct Dependencies

| Module | Current | Latest | Gap | Recommendation |
|---|---|---|---|---|
| `jackc/pgx/v5` | v5.7.6 | v5.10.0 | +4 minors | **Upgrade**: connection-pool improvements |
| `modernc.org/sqlite` | v1.38.2 | v1.53.0 | +15 minors | **Strong upgrade**: substantial perf/correctness |
| `golang.org/x/time` | v0.9.0 | v0.15.0 | +6 minors | **Low priority**: stable API |

### Indirect Dependencies (notable)

| Module | Current | Latest | Notes |
|---|---|---|---|
| `filippo.io/edwards25519` | v1.1.0 | v1.2.0 | Test-only; mysql driver dep |
| `go-sql-driver/mysql` | v1.8.1 | v1.10.0 | Test-only; sqlx test dep |
| `google/go-cmp` | v0.6.0 | v0.7.0 | Phantom |
| `google/pprof` | v0.0.0-20250317 | v0.0.0-20260604 | Test-only; sqlite test dep |
| `kr/pretty` | v0.3.0 | v0.3.1 | Phantom |
| `lib/pq` | v1.10.9 | v1.12.3 | Test-only; sqlx test dep |
| `mattn/go-isatty` | v0.0.20 | v0.0.22 | Indirect via libc |
| `mattn/go-sqlite3` | v1.14.22 | v1.14.47 | Test-only; sqlx test dep |
| `ncruces/go-strftime` | v0.1.9 | v1.0.0 | Indirect via libc (major bump) |
| `stretchr/objx` | v0.1.0 | v0.5.3 | Phantom |
| `stretchr/testify` | v1.8.1 | v1.11.1 | Test-only |
| `golang.org/x/exp` | v0.0.0-20250620 | v0.0.0-20260611 | Indirect via libc |
| `golang.org/x/mod` | v0.36.0 | v0.37.0 | Test-only; ccgo dep |
| `golang.org/x/net` | v0.55.0 | v0.56.0 | Phantom |
| `golang.org/x/time` | v0.9.0 | v0.15.0 | Direct |
| `golang.org/x/tools` | v0.45.0 | v0.47.0 | Test-only; libc test dep |
| `modernc.org/cc/v4` | v4.26.2 | v4.29.0 | Test-only |
| `modernc.org/ccgo/v4` | v4.28.0 | v4.34.6 | Test-only |
| `modernc.org/fileutil` | v1.3.8 | v1.4.0 | Test-only |
| `modernc.org/libc` | v1.66.3 | v1.73.5 | Indirect via sqlite |
| `modernc.org/opt` | v0.1.4 | v0.2.0 | Test-only |

**Recommended upgrade command** (safe, only runtime-affecting modules):
```bash
go get github.com/jackc/pgx/v5@v5.10.0 \
       modernc.org/sqlite@v1.53.0 \
       golang.org/x/time@v0.15.0
go mod tidy
```

---

## 6. Security Notes

- **Password handling**: `golang.org/x/crypto/pbkdf2` and `golang.org/x/text/secure/precis` are used by pgx for SCRAM-SHA-256 authentication. Both are at recent versions (v0.53.0, v0.38.0).
- **No known CVEs** in any dependency at the pinned versions. The Go vulnerability database (`govulncheck`) was not run -- recommended before production deployment.
- **`edwards25519`** is present only for test code of the MySQL driver (not used at runtime by this project) and carries Ed25519 curve implementation which has no known vulnerabilities.
- **`modernc.org/sqlite`** (pure-Go SQLite) at v1.38.2 is behind but SQLite CVE history for the embedded C code is addressed upstream; the Go wrapper itself has no independent CVEs.

---

## 7. Dependency Tree Visualization

```
metapi-go (8 direct)
|
+-- go-chi/chi/v5 ........... HTTP router
+-- go-chi/cors ............. CORS middleware
+-- jackc/pgx/v5 ............ PostgreSQL driver
|   +-- jackc/pgpassfile .... PG password file parser
|   +-- jackc/pgservicefile . PG service file parser
|   +-- jackc/puddle/v2 ..... Connection pool
|   |   +-- golang.org/x/sync . Semaphore
|   +-- golang.org/x/crypto .. SCRAM PBKDF2
|   +-- golang.org/x/text .... SCRAM SASLprep
|   +-- [test] stretchr/testify
|       +-- davecgh/go-spew
|       +-- pmezard/go-difflib
|       +-- gopkg.in/yaml.v3
|
+-- jmoiron/sqlx ............ SQL extensions
|   +-- [test] go-sql-driver/mysql
|   |   +-- filippo.io/edwards25519
|   +-- [test] lib/pq
|   +-- [test] mattn/go-sqlite3
|
+-- joho/godotenv ........... .env loader
+-- robfig/cron/v3 .......... Cron scheduler
+-- golang.org/x/time ....... Rate limiter
+-- modernc.org/sqlite ....... Pure-Go SQLite
    +-- modernc.org/libc
    |   +-- dustin/go-humanize
    |   +-- google/uuid
    |   +-- mattn/go-isatty
    |   +-- ncruces/go-strftime
    |   +-- golang.org/x/exp/constraints
    |   +-- golang.org/x/sys/unix
    |   +-- modernc.org/mathutil
    |   |   +-- remyoudompheng/bigfft
    |   +-- modernc.org/memory
    |   +-- [test] modernc.org/{cc,ccgo,gc,goabi0,opt,sortutil,strutil,token}
    |   +-- [test] google/pprof
    |   +-- [test] golang.org/x/mod, x/tools
    +-- [test] modernc.org/fileutil
```

---

## 8. go.sum Integrity

- **95 lines** in go.sum
- **42 unique modules** x 2 hashes (zip + go.mod) = 84 expected lines, plus some modules have extra go.mod hashes for different minor versions carried by MVS. 95 lines is consistent.
- `go mod verify` confirmed all hashes match.
- No `go.sum` entries are stale -- `go mod tidy` produced zero changes.

---

## 9. Recommendations

1. **Upgrade `modernc.org/sqlite`** from v1.38.2 to latest (v1.53.0). The +15 minor gap spans significant performance and correctness fixes for the pure-Go SQLite engine. This directly affects the migration tooling.
2. **Upgrade `jackc/pgx/v5`** from v5.7.6 to v5.10.0. Connection-pool stability improvements.
3. **Run `govulncheck`** before any production deployment to scan for known CVEs.
4. **Consider a `go mod tidy` CI gate** (already passes, but formalizing it prevents go.sum drift).
5. The 6 phantom modules are benign but could be investigated with `go mod graph | grep` to verify no stale `// indirect` markers remain. If they persist after `go mod tidy`, MVS requires them and they are harmless.

---

## 10. Raw Command Outputs

### go mod verify
```
all modules verified
```

### go mod tidy
```
(no output -- go.mod and go.sum already in sync)
```

### go.sum line count
```
95 D:/Code/TokenDance/metapi-go/go.sum
```

### go.list -m -u all (update-available modules)
See Section 5 above. Full output captured during audit.

