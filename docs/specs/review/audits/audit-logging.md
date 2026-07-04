# Logging Hygiene Audit: metapi-go

**Date:** 2026-07-04
**Scope:** `router/middleware.go`, `handler/proxy/*.go`, `proxy/*.go`, `service/*.go` (all non-test Go files examined; cross-referenced with all `slog.*` and `log.*` calls across the entire codebase)
**Result:** 16 findings (1 Critical, 5 High, 6 Medium, 4 Low)

---

## Summary

The codebase makes good use of `log/slog` for structured logging throughout most packages. Log levels are generally well-chosen in scheduler and lifecycle code. However, the critical hot-path (proxy request handling) is nearly silent -- failures are silently discarded and latency metrics are thrown away. The oauth service package still uses the legacy stdlib `log.Printf` instead of `slog`. Several panics in cryptographic helpers and config loaders are unguarded in goroutine contexts.

---

## Findings

### Finding 1 [CRITICAL] -- `proxy/surface.go` and `handler/proxy/upstream.go`: proxy failures silently discarded

**Files:** `proxy/surface.go:251,258,312,319,370,376,428,440`, `handler/proxy/upstream.go`
**Category:** missing-error-context

Every failure recording call in the `SurfaceFailureToolkit` discards errors with `_ =`:

```go
// proxy/surface.go:251, 258
_ = ft.Router.RecordFailure(ctx, selected.Channel.ID, routing.SiteRuntimeFailureContext{...}, nil)
_ = ft.LogProxy(ctx, ProxyLogEntry{...})
```

The same pattern repeats at lines 312, 319, 370, 376, 428, 440. If `RecordFailure` or `LogProxy` fail, the error is silently lost. Additionally, `handler/proxy/upstream.go` has no logging at all in `dispatchUpstream` -- no upstream failure is logged, no latency is recorded on success, and `latencyMs` is discarded via `_ = latencyMs` at lines 185 and 212.

**Impact:** Proxy failures in production cannot be diagnosed from logs. The proxy is the primary runtime component and generates zero log entries for its most critical operations.

**Recommendation:**
- Log at minimum a `slog.Warn` when `RecordFailure` or `LogProxy` return errors.
- Add a `slog.Error` in `dispatchUpstream` when all channels are exhausted.
- Log upstream latency at `slog.Debug` level on success paths.

---

### Finding 2 [HIGH] -- `router/middleware.go:28`: `slog.Info` on every request in the hottest path

**File:** `router/middleware.go:28`
**Category:** excessive-logging-hot-path

```go
func RequestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slog.Info("request",  // <-- fires on every single request
            "method", r.Method,
            "path",   r.URL.Path,
            "remote", r.RemoteAddr,
        )
        next.ServeHTTP(w, r)
    })
}
```

This is the hottest code path in the entire application. Every proxy request (chat, completions, embeddings, images, search, etc.) triggers an `slog.Info`. On a production proxy handling thousands of requests per minute, this saturates the log output with noise.

**Impact:** Massive log volume; obscures real errors; I/O contention on log writes under load.

**Recommendation:** Change to `slog.Debug`. If per-request INFO-level logging is desired for audit purposes, use sampling (log every Nth request) or defer to a separate access log mechanism (e.g., the proxy log table or a dedicated access log file).

---

### Finding 3 [HIGH] -- `service/oauth/`: `log.Printf` instead of `slog` -- inconsistent logging API

**Files:** `service/oauth/flow.go:504,507,520,523,615`, `service/oauth/connection.go:198,252,255`, `service/oauth/route_unit.go:270,273,401,404`, `service/oauth/quota.go:408`
**Category:** structured-logging-consistency

11 call sites in the oauth service package use the legacy stdlib `log.Printf` instead of `slog`:

```go
// service/oauth/flow.go:504
log.Printf("[oauth] model refresh failed for account %d, rolling back: %v", accountID, err)

// service/oauth/route_unit.go:270
log.Printf("[oauth] route rebuild failed after creating route unit %d, rolling back: %v", unitID, err)
```

These are important operational events (OAuth model refresh failures, route rebuild rollbacks) logged without structured key=value pairs. They cannot be filtered or queried by structured logging tools.

**Impact:** OAuth lifecycle errors are invisible to structured log aggregation. Manual log parsing required to correlate account IDs or error types.

**Recommendation:** Convert all `log.Printf` in `service/oauth/` to `slog.Error`/`slog.Warn` with structured fields:
```go
slog.Error("oauth: model refresh failed, rolling back",
    "accountID", accountID,
    "error", err)
```

---

### Finding 4 [HIGH] -- `service/account_credential.go` and `service/oauth/session.go`: panics in cryptographic helpers

**Files:** `service/account_credential.go:40,45,50`, `service/oauth/session.go:179`
**Category:** panic-in-goroutine

```go
// service/account_credential.go:40
func EncryptAccountPassword(cfg *config.Config, password string) string {
    // ...
    if _, err := io.ReadFull(rand.Reader, iv); err != nil {
        panic("crypto/rand failed: " + err.Error())  // panics on read failure
    }
    // ...
    panic("aes.NewCipher failed: " + err.Error())  // line 45
    // ...
    panic("cipher.NewGCM failed: " + err.Error())  // line 50
}
```

While crypto/rand failure is extremely unlikely, if `EncryptAccountPassword` or `DecryptAccountPassword` is called from a goroutine (e.g., during parallel checkin or balance refresh), the panic will terminate the entire process.

**Impact:** Process crash if called from an un-recovered goroutine context. The `EncryptAccountPassword` path is reached through `EncryptPassword` in `account_service.go`, which is called from admin handler account creation -- but theoretically could be called from any parallel account mutation.

**Recommendation:** Return an error instead of panicking:
```go
func EncryptAccountPassword(cfg *config.Config, password string) (string, error) {
    // ...
    return "", fmt.Errorf("crypto/rand: %w", err)
}
```
Update callers to propagate the error. Alternatively, wrap with recover in any goroutine that calls these functions.

---

### Finding 5 [HIGH] -- `service/oauth/[codex|claude|gemini_cli].go`: configuration validation via panic

**Files:** `service/oauth/codex.go:61`, `service/oauth/claude.go:61`, `service/oauth/gemini_cli.go:77,81`, `service/oauth/account.go:225,298`
**Category:** panic-in-goroutine

```go
// service/oauth/codex.go:61
panic("CODEX_CLIENT_ID is not configured")
// service/oauth/claude.go:61
panic("CLAUDE_CLIENT_ID is not configured")
// service/oauth/gemini_cli.go:77
panic("GEMINI_CLI_CLIENT_ID is not configured")
```

These panics happen in `NewCodexProvider()`, `NewClaudeProvider()`, and `NewGeminiCLIProvider()`. If any of these constructors are invoked from within a handler goroutine (e.g., during OAuth flow processing), the panic crashes the process.

**Impact:** Process crash on misconfiguration, rather than a clean error response.

**Recommendation:** Return `(nil, error)` from these constructors and let the caller decide how to handle missing configuration (log and return HTTP 500, or skip the provider).

---

### Finding 6 [HIGH] -- `cmd/server/main.go`: bootstrap hard failures logged as WARN

**File:** `cmd/server/main.go:36,42,47,52,60`
**Category:** inappropriate-log-level

```go
slog.Warn("startup bootstrap panicked", "panic", r)     // line 36
slog.Warn("failed to create data directory", ...)        // line 42
slog.Warn("bootstrap: ensureRuntimeDatabase failed", ...) // line 47
slog.Warn("settings: failed to load runtime settings", ...) // line 52
slog.Warn("migration failed", ...)                        // line 60
```

These are all startup-hardening failures. If the data directory cannot be created or the database cannot be bootstrapped, the server is effectively dead in the water. Logging these as `Warn` understates their severity.

**Impact:** Operators scanning for ERROR-level messages will miss these critical startup failures. Alerting systems keyed on ERROR level will not fire.

**Recommendation:** Change to `slog.Error` for lines 42, 47, 52, 60. Line 36 (panic recovery during bootstrap) should also be `slog.Error` since the bootstrap is the initialization path and a panic there means the server cannot start.

---

### Finding 7 [MEDIUM] -- `proxy/conductor.go:132`: `OnTerminalFailure` error discarded

**File:** `proxy/conductor.go:132`
**Category:** missing-error-context

```go
if input.OnTerminalFailure != nil {
    _ = input.OnTerminalFailure(ctx, selected, struct{...}{...})  // error discarded
}
```

When a terminal failure occurs, the `OnTerminalFailure` callback (which may send alerts or record metrics) can fail silently. No log entry is produced.

**Impact:** Terminal failure alerts may not fire if the callback fails. No visibility into why.

**Recommendation:** Log the error from `OnTerminalFailure` at `slog.Warn` level.

---

### Finding 8 [MEDIUM] -- `service/notify/notify.go:157`: string concatenation in slog.Error

**File:** `service/notify/notify.go:157`
**Category:** structured-logging-consistency

```go
slog.Error("SendNotification: " + err.Error())
```

This concatenates the error message into the log message string rather than using a structured key=value pair. Compare with the correct pattern at line 208:

```go
slog.Error("SendNotification: all channels failed", "first_error", firstErr)
```

**Impact:** Minor -- the error still appears in log output, but it is not queryable as a structured field.

**Recommendation:** Change to `slog.Error("SendNotification: no channels configured", "error", err)`.

---

### Finding 9 [MEDIUM] -- `handler/proxy/surface.go:155`: unused `slog.Info` import workaround

**File:** `handler/proxy/surface.go:155`
**Category:** structured-logging-consistency

```go
var _ = slog.Info
```

This blank-identifier reference exists solely to prevent the `slog` import from being removed by `goimports`. It signals that `slog` was planned to be used but the code was never written.

**Impact:** Low (code smell). Indicates unfinished logging instrumentation in the handler/proxy package.

**Recommendation:** Remove the line once actual `slog` calls are added to the `handler/proxy` package (see Finding 1).

---

### Finding 10 [MEDIUM] -- `scheduler/usage_aggregation.go:141`: deliberate re-panic after lease release

**File:** `scheduler/usage_aggregation.go:141`
**Category:** panic-in-goroutine

```go
defer func() {
    if r := recover(); r != nil {
        s.releaseLease(dbw, lease, fmt.Errorf("panic: %v", r))
        panic(r)  // re-panics after cleanup
    }
}()
```

The intent is correct (release the lease then propagate the panic), but this means a panic in the usage aggregation projection pass will crash the scheduler goroutine. The scheduler's own panic recovery (`scheduler/scheduler.go:40`) wraps goroutine start, but the goroutine itself may not be recoverable.

**Impact:** A panic in usage aggregation kills the scheduler goroutine. The scheduler will attempt to restart on the next tick, but the lease may be lost.

**Recommendation:** Replace `panic(r)` with `slog.Error("usage-aggregation: projection panicked", "panic", r)` and return gracefully. The scheduler's cron tick will retry on the next interval.

---

### Finding 11 [MEDIUM] -- `config/config.go:29`: `config.Get()` panics

**File:** `config/config.go:29`
**Category:** panic-in-goroutine

```go
func Get() *Config {
    cfgMu.RLock()
    defer cfgMu.RUnlock()
    if globalCfg == nil {
        panic("config.Get() called before config.Set() -- load config first")
    }
    return globalCfg
}
```

This is called extensively (e.g., from `proxy/failure_judge.go:31`, `proxy/session.go` throughout). If any code path reaches `config.Get()` before startup completes, the process crashes. While architecturally this only happens at startup, the panic is unnecessarily harsh for what is effectively a precondition check.

**Impact:** Process crash if config access order is disrupted by any code change.

**Recommendation:** Consider a `MustGet()` convention (keep the panic) and add a `TryGet() (*Config, error)` variant for non-startup paths. Alternatively, document clearly that `Get()` panics and audit all callers.

---

### Finding 12 [MEDIUM] -- `store/bootstrap.go:63`: DSN logged at INFO level

**File:** `store/bootstrap.go:63`
**Category:** sensitive-data

```go
slog.Info("bootstrap: opening PostgreSQL database", "dsn", maskDSN(dsn))
```

The DSN is masked via `maskDSN()`, which presumably redacts the password portion. If `maskDSN` has a bug or edge case, the raw DSN (including password) could be logged.

**Impact:** Low risk with proper masking, but credential leakage is high-impact if masking fails.

**Recommendation:** Verify `maskDSN` is thoroughly tested with edge cases (URL-encoded passwords, special characters). Consider logging only the host and database name, not the DSN at all.

---

### Finding 13 [LOW] -- Inconsistent use of WARN vs ERROR for database-not-initialized conditions

**Files:** Multiple files in `auth/downstream.go:234,270`, `store/settings.go:17`, `store/migrate.go:1252`
**Category:** inappropriate-log-level

```go
slog.Warn("consumeManagedKeyRequest: database not initialized, skipping")
slog.Warn("RecordManagedKeyCostUsage: database not initialized, skipping")
slog.Warn("settings: database not initialized, skipping runtime settings load")
```

Some scheduler files use `slog.Error` for the same condition:
```go
slog.Error("daily-summary: database not available")       // scheduler/daily_summary.go:53
slog.Error("checkin: database not available")              // scheduler/checkin.go:165
slog.Error("balance: database not available")              // scheduler/balance.go:84
```

The level choice is inconsistent. A database-not-initialized condition during normal operation is a real error, but some files use WARN while others use ERROR.

**Impact:** Operators cannot reliably filter on a single log level to catch this condition.

**Recommendation:** Standardize: use `slog.Error` for database-unavailable conditions (they prevent core operations from completing). Reserve `slog.Warn` for conditions where the operation can proceed with degraded functionality.

---

### Finding 14 [LOW] -- Proxy handlers have zero logging

**Files:** `handler/proxy/chat.go`, `handler/proxy/messages.go`, `handler/proxy/completions.go`, `handler/proxy/responses.go`, `handler/proxy/embeddings.go`, `handler/proxy/search.go`, `handler/proxy/images.go`, `handler/proxy/videos.go`, `handler/proxy/gemini.go`, `handler/proxy/models.go`, `handler/proxy/files.go`
**Category:** missing-error-context

None of the proxy handler files contain any `slog` calls (other than the `var _ = slog.Info` workaround in `surface.go`). Every handler simply calls `dispatchUpstream` and writes an error response to the client. Server-side, there is zero visibility into which handler failed, with what error, for which model, at what time.

**Impact:** Operational blind spot. When the proxy returns errors to clients, there is no server-side log trail to correlate with client reports.

**Recommendation:** Add a deferred `slog.Debug` or `slog.Warn` (on error) in `PrepareCtx` or at the top of each handler. The proxy log table (`ProxyLogEntry`) is the primary structured audit trail, but having in-process logs for real-time observability is essential for operations.

---

### Finding 15 [LOW] -- `slog.Debug` usage is near-zero

**Category:** inappropriate-log-level

Only two `slog.Debug` calls exist in the entire codebase:
```go
scheduler/channel_recovery.go:316: slog.Debug("channel-recovery: probing candidate", ...)
scheduler/admin_snapshot.go:160:   slog.Debug("admin-snapshot: warming target", ...)
```

There are zero `slog.Debug` calls in the proxy/handler/service layers. This means adjusting log level at runtime provides no additional detail beyond what is already emitted at INFO.

**Impact:** Operators cannot increase log verbosity at runtime. Debugging production issues requires code changes to add temporary log lines.

**Recommendation:** Add `slog.Debug` calls at key decision points: model resolution, channel selection decisions, retry decisions, upstream URL construction. The `slog` package supports runtime level changes via `slog.SetLogLoggerLevel`, making this an easy win.

---

### Finding 16 [LOW] -- `app/services.go` lifecycle logging missing scheduler counts

**File:** `app/services.go:70`
**Category:** structured-logging-consistency

```go
slog.Info("all background schedulers registered",
    "active", activeCount,     // <-- this is good
)
```

This log line already includes an `active` count. It would be more useful with a list of scheduler names that failed to start (line 47 logs failures individually, but there is no aggregate summary).

**Impact:** Minor. The data is available but requires scanning multiple log lines.

**Recommendation:** Add a `failed` count or a `failed_names` list to the "all background schedulers registered" log line.

---

## Positive Observations

1. **Consistent structured logging pattern** -- The vast majority of `slog` calls use key=value pairs (`"error", err`, `"accountID", id`), which is the correct Go idiom.

2. **Appropriate log levels in scheduler code** -- Schedulers consistently use `Info` for lifecycle, `Error` for failures, `Warn` for config issues. This is well-done.

3. **Good startup/shutdown logging** -- `app/app.go` has clean, readable lifecycle messages covering listen, signal handling, and graceful shutdown.

4. **No sensitive data directly logged** -- A thorough search found no instances of tokens, API keys, or passwords appearing in log messages. The `VideoTask` struct has a `TokenValue` field but it is never logged.

5. **Panic recovery in scheduler framework** -- `scheduler/scheduler.go:36-43` wraps goroutine starts in recover, catching panics and logging them at ERROR level. This prevents one misbehaving scheduler from crashing the process.

6. **Well-structured proxy log entries** -- The `ProxyLogEntry` struct in `proxy/surface.go:106-133` is comprehensive and well-typed, providing a solid foundation for structured audit logging (even if the logging itself is currently silent -- see Finding 1).

7. **No `log.Fatal` usage** -- No calls to `log.Fatal` or `os.Exit` exist in the application code (only in the migrate command). This is excellent Go hygiene.
