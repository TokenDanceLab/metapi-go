# File I/O Security Audit -- metapi-go

**Date:** 2026-07-05
**Scope:** Full repository (`D:/Code/TokenDance/metapi-go`)
**Auditor:** Claude Code automated audit
**Files examined:** `store/open.go`, `handler/admin/settings_backup.go`, `cmd/server/main.go`, `store/bootstrap.go`, `scheduler/file_retention.go`, `scheduler/log_cleanup.go`, `scheduler/admin_snapshot.go`, `scheduler/backup_webdav.go`, `router/router.go`, `config/config.go`, `config/defaults.go`, `web/embed.go`, `cmd/migrate/main.go`

---

## 1. Temp File Cleanup

**Verdict:** PASS (no temp files used)

The application does not create temporary files on disk in any code path:

- **Backup export** (`settings_backup.go:94-140`): serializes to JSON in memory, writes directly to HTTP response body.
- **Backup import** (`settings_backup.go:188-235`): reads JSON from HTTP request body into memory, no file staging.
- **WebDAV backup** (`backup_webdav.go:112-172`): marshals payload in memory, sends directly over HTTP PUT. No local file staging.
- **SQLite-to-Postgres migration** (`cmd/migrate/main.go`): reads source SQLite entirely into memory (`readAllTables`), never writes to temp files.
- **Test golden file generation**: uses `os.WriteFile` directly, no temp+rename pattern.
- No calls to `os.CreateTemp`, `ioutil.TempFile`, or `os.TempDir` exist anywhere in the codebase.

Since no temp files are created, there is no temp-file-cleanup risk.

---

## 2. Atomic Writes (Write-Temp + Rename)

**Verdict:** N/A (no file writes in production paths)

The production server code does not write files to disk at all (data is stored in SQLite/Postgres, frontend is compiled-in via `embed.FS`). The only `os.WriteFile` calls are in test code for golden file snapshots:

| Location | Operation | Permission |
|---|---|---|
| `routing/algorithm_test.go:709,744,768` | `os.WriteFile(path, data, 0644)` | OK |
| `routing/selector_compare_test.go:90` | `os.WriteFile(path, data, 0644)` | OK |
| `routing/weights_test.go:137` | `os.WriteFile(path, data, 0644)` | OK |

These are developer-only test utilities and do not run in production. However, they do NOT use the `write-temp + os.Rename` pattern -- a crash during `WriteFile` could leave a truncated golden file. Low risk (dev tooling only).

**Recommendation:** If golden file writing becomes a regular CI step, adopt `os.CreateTemp` + `os.Rename` to prevent truncated goldens.

---

## 3. File Permissions

**Verdict:** PASS with 1 observation

### Directory Creation (all use `0755`)

| Location | Call | Correctness |
|---|---|---|
| `cmd/server/main.go:56` | `os.MkdirAll(cfg.DataDir, 0755)` | OK |
| `store/bootstrap.go:44` | `os.MkdirAll(dataDir, 0755)` | OK |
| `store/bootstrap.go:56` | `os.MkdirAll(filepath.Dir(sqlitePath), 0755)` | OK |
| `store/open.go:173` | `os.MkdirAll(filepath.Dir(dataDir), 0755)` | BUG (see section 6) |

### File Creation

| Location | Call | Permission |
|---|---|---|
| Test golden files | `os.WriteFile(..., 0644)` | OK -- world-readable, group/world not writable |

### Sensitive Data

- No sensitive data is written to disk files. Secrets (`auth_token`, `proxy_token`, OAuth client secrets) exist only in environment variables and the `config.Config` struct in memory.
- The SQLite database file is opened by the driver; permissions depend on the OS `umask`. No explicit `0600` permission is set on the database file.
- `open.go:123-134` enables SQLite WAL mode and `foreign_keys=ON`, which are correct safety settings for the database driver.

**Observation:** The SQLite database file (`hub.db`) contains sensitive data (account tokens, API keys) but inherits default `umask` permissions. On most Linux systems this will be `0644` (world-readable). Consider explicitly setting `0600` on the database file after creation, or documenting that the data directory should be inaccessible to other users.

**Observation:** The `settings_backup` export endpoint (`settings_backup.go:94-140`) streams account tokens and API keys as JSON over HTTP. The response is not encrypted -- this is acceptable when accessing over localhost or TLS, but the admin UI should warn users that backup downloads contain plaintext credentials.

---

## 4. Path Traversal Protection

**Verdict:** PARTIAL PASS with 1 finding

### Data Directory (`cfg.DataDir`)

`main.go:45` applies `filepath.Clean()`:
```go
cfg.DataDir = filepath.Clean(cfg.DataDir)
```

This normalizes redundant separators and `.`/`..` components within the path, but it does NOT prevent the path from resolving outside the intended directory. For example, `DATA_DIR=../../etc` would set `DataDir` to `../../etc` after cleaning -- `filepath.Clean` does not make the path absolute or constrain it. However, since `DataDir` comes from an environment variable set by the operator (not an HTTP request), this is an operational concern, not a remote attack vector.

### SQLite Path Resolution (`store/open.go:37-66`)

`ResolveSQLitePath` handles several formats:
- Empty: defaults to `{DATA_DIR}/hub.db` via `filepath.Join` -- safe.
- `:memory:`: passed through -- safe.
- `file://` prefix: decoded, no further validation.
- `sqlite://` prefix: resolved via `filepath.Abs()` -- makes absolute but does not constrain.
- Plain path: resolved via `filepath.Abs()` -- same.

No containment check is performed (e.g., verifying the resolved path is within the configured `DataDir`). An operator who can set `DB_URL` can point the database anywhere on the filesystem -- this is by design for operational flexibility, but means there is no defense-in-depth against misconfiguration.

### Migration Tool (`cmd/migrate/main.go:369-400`)

`normalizeSQLitePath` strips prefixes and guards against network URLs but does NOT contain path traversal. A user can pass `--from sqlite://../../../etc/passwd` and the tool will attempt to open that path as a SQLite database.

### Backup Handler Table Names (`settings_backup.go:144-153`)

The `queryTableAsJSON` function validates table names against a hardcoded whitelist (`isKnownTable` checks against `allTables`). This prevents SQL injection via table names. The `importTableRows` function similarly validates with `isKnownTable`. Both are correct whitelist approaches.

**Finding (Medium):** Column names in `importTableRows` are derived from JSON input keys:
```go
// settings_backup.go:263-267
for col, val := range row {
    columns = append(columns, col)
    ...
}
```
This passes JSON object keys directly into SQL column references. While `INSERT OR IGNORE INTO %s (col1, col2) VALUES (?, ?)` would cause a syntax error on invalid column names (not data leakage), a malicious admin (or an attacker who has compromised the admin token) could probe for column existence through error messages. Since this endpoint is behind admin auth, risk is low.

---

## 5. Symlink Handling

**Verdict:** PASS (no symlink surface)

- No symlink-related functions are used anywhere in the codebase: no `os.Readlink`, `os.Lstat`, `os.Symlink`, or `filepath.EvalSymlinks`.
- The frontend is compiled-in via `embed.FS` -- immune to symlink attacks.
- The SQLite database path is resolved through `filepath.Abs()` which does NOT follow or resolve symlinks. If an attacker with filesystem access replaced `hub.db` with a symlink to a sensitive file, the application would open it as SQLite and potentially corrupt it. This is a general risk for any process that opens files by path, but the attacker would need filesystem-level access to place the symlink in the first place.
- The `http.FileServer` used for SPA assets (`router.go:108-139`) operates on an `embed.FS` (not the live filesystem), so symlink traversal in the SPA directory is not possible.

**No action required** -- the current architecture has minimal symlink surface area.

---

## 6. Data Directory Auto-Creation

**Verdict:** BUG FOUND (1 defect, low severity due to limited call sites)

### Defect: `store/open.go:169-174` -- EnsureDataDir creates the wrong directory

```go
func EnsureDataDir(dataDir string) error {
    if dataDir == "" || dataDir == ":memory:" {
        return nil
    }
    return os.MkdirAll(filepath.Dir(dataDir), 0755)  // BUG: should be filepath.Dir(dataDir) → dataDir
}
```

`filepath.Dir("./data")` returns `"."`, not `"./data"`. This means `EnsureDataDir("./data")` would create the current working directory (which already exists) rather than `./data/`. The function name and documentation claim it creates the data directory, but it creates the parent directory instead.

**Impact:** This function is exported from the `store` package but is **never called** anywhere in the production code (verified via grep). The actual data directory creation is handled correctly by:
- `main.go:56`: `os.MkdirAll(cfg.DataDir, 0755)` -- correct
- `bootstrap.go:44`: `os.MkdirAll(dataDir, 0755)` -- correct
- `bootstrap.go:56`: `os.MkdirAll(filepath.Dir(sqlitePath), 0755)` -- correct (creates parent of db file)

The function also lacks defense against `:memory:` for the `filepath.Dir` call -- if called with `:memory:`, `filepath.Dir(":memory:")` returns `"."`, which would create `"."` (no-op, but semantically wrong).

**Recommendation:** Fix the function:
```go
func EnsureDataDir(dataDir string) error {
    if dataDir == "" || dataDir == ":memory:" {
        return nil
    }
    return os.MkdirAll(dataDir, 0755)
}
```

### Duplicate DataDir creation

`main.go:56` creates `cfg.DataDir` before calling `store.EnsureRuntimeDatabase(cfg)`, which then calls `os.MkdirAll(dataDir, 0755)` again in `bootstrap.go:44`. This is harmless (idempotent) but indicates a slight lack of coordination between the bootstrap and main functions.

---

## Summary Table

| Category | Status | Issues |
|---|---|---|
| Temp file cleanup | PASS | No temp files created. |
| Atomic writes | N/A | No production file writes; test goldens use non-atomic `os.WriteFile`. |
| File permissions | PASS (1 observation) | `0755` dirs and `0644` files are correct. SQLite DB file inherits `umask`; consider explicit `0600`. |
| Path traversal | PARTIAL PASS (1 medium) | `DataDir` uses `filepath.Clean` but not full containment. Column names from JSON input in import flow (admin auth mitigates). |
| Symlink handling | PASS | No symlink surface; `embed.FS` and DB-only architecture minimize risk. |
| Data dir auto-creation | BUG (1 defect) | `EnsureDataDir` creates parent dir, not target dir. Dead code (never called). Duplicate creation in main+bootstrap (harmless). |

---

## Recommendations

1. **[Low] Fix `EnsureDataDir`** (`store/open.go:173`): Replace `filepath.Dir(dataDir)` with `dataDir`.
2. **[Low] SQLite DB permissions**: Document or enforce `0600` on `hub.db` to prevent world-readable token leakage.
3. **[Low] Adopt atomic writes for test goldens**: Use `os.CreateTemp` + `os.Rename` in test golden file generation to prevent truncated files from partial writes.
4. **[Informational] Backup export plaintext**: The `/api/settings/backup/export` endpoint returns account tokens in plaintext JSON. Document this in the admin UI as a security consideration.
5. **[Informational] `DataDir` containment**: Consider adding an explicit containment check (verify resolved path is under an expected root) for defense-in-depth, though current attack surface is limited to operator-set environment variables.
