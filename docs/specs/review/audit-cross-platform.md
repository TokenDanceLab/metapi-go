# Cross-Platform Audit: metapi-go

**Audited file:** `store/open.go` (and related codebase)
**Date:** 2026-07-04
**Target:** linux/amd64 Docker (per `Dockerfile`: `golang:1.24-alpine` build, `alpine:3.21` runtime, `CGO_ENABLED=0`)

---

## 1. SQLite Driver: Pure Go (no CGO)

**Verdict: PASS -- Fully cross-platform.**

- `go.mod` declares `modernc.org/sqlite v1.38.2` (not `github.com/mattn/go-sqlite3`).
- `store/open.go` line 12-13 explicitly documents: `// Register pure-Go SQLite driver (no CGO).`
- `Dockerfile` line 7 builds with `CGO_ENABLED=0`.
- `modernc.org/sqlite` is a pure-Go SQLite implementation (transpiled C to Go). It compiles and runs identically on Linux, macOS, and Windows with zero CGO dependency.
- The transitive dependency `modernc.org/libc v1.66.3` confirms the pure-Go C runtime shim is in use.
- Postgres driver (`jackc/pgx/v5`) is also pure Go -- no CGO needed.

**No action required.**

---

## 2. OS-Specific Code

**Verdict: PASS with one cosmetic note.**

- Zero Go source files use build tags (`//go:build linux`, `_windows.go`, `_linux.go`, etc.).
- Zero references to `runtime.GOOS` in Go source.
- No `os/exec` usage anywhere -- the proxy layer is purely HTTP-based (`net/http`), so no subprocess spawning or platform-specific process management.

### Finding 2a: Hardcoded Windows User-Agent

`platform/newapi.go` line 416:

```go
"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
```

The application runs in a Linux Docker container but sends a Windows 10 User-Agent string for NewAPI login requests. This is a **fingerprinting inconsistency**, not a functionality bug:
- If the upstream NewAPI server inspects UA for bot detection, a Linux-hosted service claiming to be Windows may raise suspicion.
- If this was carried over from the TypeScript codebase as a deliberate spoof, it is low-risk but worth documenting.

**Severity: Low.** Consider parameterizing or documenting the rationale. No runtime impact on any platform.

---

## 3. File Path Portability

**Verdict: PASS with one dead-code note.**

All path operations use the `path/filepath` standard library, which adapts to the host OS separator:

| Location | Usage | Portable? |
|---|---|---|
| `store/open.go:40` | `filepath.Join(dataDir, "hub.db")` | Yes |
| `store/open.go:55,61` | `filepath.Abs(rel)` / `filepath.Abs(raw)` | Yes |
| `store/open.go:173` | `filepath.Dir(dataDir)` | Yes (but see below) |
| `store/bootstrap.go:56` | `filepath.Dir(sqlitePath)` | Yes |
| `cmd/server/main.go:30` | `filepath.Clean(cfg.DataDir)` | Yes |

The comment on `main.go` line 29 explicitly acknowledges cross-platform path handling:
```go
// Normalize DataDir (E11: trailing slash / Windows backslash)
cfg.DataDir = filepath.Clean(cfg.DataDir)
```

### Finding 3a: Dead function with semantic defect

`store/open.go` lines 168-174:

```go
// EnsureDataDir creates the data directory if it does not exist.
func EnsureDataDir(dataDir string) error {
    if dataDir == "" || dataDir == ":memory:" {
        return nil
    }
    return os.MkdirAll(filepath.Dir(dataDir), 0755)
}
```

This function is **never called** anywhere in the codebase. It also has a semantic bug: it creates the *parent* of `dataDir`, not `dataDir` itself. For `dataDir = "/app/data"`, it creates `/app` but not `/app/data`. The actual directory creation is done correctly in `bootstrap.go:44` (`os.MkdirAll(dataDir, 0755)`).

**Severity: Trivial (dead code).** Remove or fix if ever activated.

### Finding 3b: Windows behavior of filepath.Clean

On a Windows host, `filepath.Clean("./data")` produces `.\data` (backslash separator). This is correct for Windows but the app targets Linux Docker. If a developer runs `make run` on Windows and passes `DATA_DIR` through to a Linux container volume mount, the backslash path would break inside the container. However:

- The Dockerfile hardcodes `ENV DATA_DIR=/app/data` (forward slash).
- `docker-compose.yml` mounts `./data:/app/data` (forward slash).
- Local `make run` on Windows would use the Windows-normalized path, but `os.MkdirAll` handles backslashes correctly on Windows.

**Severity: Low (Docker-only deployment).** No action needed for the primary target.

---

## 4. Signal Handlers

**Verdict: PASS for target platform (Linux/Docker). Conditional Windows limitation.**

`app/app.go` lines 68-69:

```go
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
```

### Linux (Docker target): Full support
- `SIGINT` -- sent by Ctrl+C or `docker stop` (default).
- `SIGTERM` -- sent by `docker stop` (after grace period), `kill`, orchestration shutdown.
- Both are standard on Linux. The graceful shutdown (5s timeout, `Server.Shutdown`, DB close, `OnClose` hooks) works correctly.

### macOS: Full support
- Both signals work identically to Linux.

### Windows (local dev only): Partial support
- **`syscall.SIGINT`** -- works on Windows (maps to Ctrl+C / Ctrl+Break). Graceful shutdown triggers.
- **`syscall.SIGTERM`** -- **not implemented** in Go's Windows runtime. `signal.Notify` for `SIGTERM` on Windows is silently a no-op. The channel never receives SIGTERM.
- Practical impact on Windows: killing the process via Task Manager or `taskkill` sends `SIGKILL`-equivalent (immediate termination, no graceful shutdown). This is acceptable for a dev environment.

**Severity: Low.** Primary target is Linux Docker where both signals work. Windows limitation only affects local `make run` development, where Ctrl+C (SIGINT) covers the normal stop case.

---

## 5. Additional Cross-Platform Notes

| Concern | Status | Notes |
|---|---|---|
| `os.Hostname()` in scheduler | PASS | Portable across all platforms |
| `os.Getpid()` in scheduler | PASS | Portable across all platforms |
| `os.MkdirAll(..., 0755)` | PASS | Windows ignores permission bits but creates dirs correctly |
| `cron` scheduling (`robfig/cron/v3`) | PASS | Pure Go, no platform dependencies |
| `net/http` server | PASS | Fully cross-platform Go standard library |
| File locking (SQLite WAL) | PASS | `modernc.org/sqlite` handles locking internally, no OS-specific file lock APIs |
| Volume mounts (Docker) | PASS | `/app/data` is a standard Linux path |

---

## Summary

| Dimension | Grade | Issues |
|---|---|---|
| SQLite driver | PASS | Pure Go, CGO_ENABLED=0 |
| OS-specific code | PASS | Only cosmetic: hardcoded Windows UA |
| File paths | PASS | `filepath` package used everywhere; dead code noted |
| Signal handlers | PASS | Linux target fully covered; Windows SIGTERM no-op (dev only) |
| Build system | PASS | Multi-stage Docker, `CGO_ENABLED=0`, `GOOS/GOARCH` unset (defaults to build host) |

**Overall: The codebase is well-prepared for linux/amd64 Docker deployment. No blocking cross-platform issues found.** The three low-severity findings (Windows UA string, dead `EnsureDataDir`, Windows SIGTERM no-op) are cosmetic and do not affect the Docker target.
