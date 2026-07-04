# Production Readiness Audit: metapi-go

**Date**: 2026-07-05
**Audited by**: automated production audit
**Scope**: `app/*.go`, `cmd/server/main.go`, `router/*.go`, `Dockerfile`, `config/*.go`, `store/bootstrap.go`, `store/open.go`

## Summary

| # | Check | Status | Severity |
|---|-------|--------|----------|
| 1 | Health check returns proper JSON | OK | -- |
| 2 | Readiness probe (DB connected) | MISSING | High |
| 3 | Docker HEALTHCHECK | OK | -- |
| 4 | Graceful shutdown ordering | BUG | Medium |
| 5 | Signal handling | OK | -- |
| 6 | PID file | MISSING | Low |
| 7 | JSON log format for production | MISSING | Medium |
| 8 | CORS headers | PERMISSIVE | Low |
| 9 | Security headers | MISSING | High |
| 10 | TLS support | DELEGATED | Medium |
| 11 | Maximum body size enforcement | MISSING | High |
| 12 | Request timeout (main server) | MISSING | High |
| 13 | Dead code (app/health.go) | BUG | Low |
| 14 | Double shutdown (main.go) | BUG | Medium |
| 15 | Default credential exposure | WARN | High |

---

## Detailed Findings

### 1. Health Check Endpoint -- OK

**File**: `router/router.go:31-35`, `app/health.go`

The `/health` endpoint is registered before auth middleware and returns:

```json
{"status":"ok"}
```

with `Content-Type: application/json` and HTTP 200. This is correct for liveness probes.

**Issue**: `app/health.go` defines a `Health()` function that is **never called**. The router registers an inline anonymous function instead, making `app/health.go` dead code.

---

### 2. Readiness Probe (DB Connected) -- MISSING [HIGH]

There is no `/ready` or equivalent readiness endpoint that validates database connectivity. The `/health` endpoint returns a static `{"status":"ok"}` regardless of whether the database is reachable.

**Risk**: Kubernetes/Docker will route traffic to the pod before the database is ready, causing 500 errors on any endpoint that queries the database.

**Recommendation**: Add a `/ready` endpoint that pings the database:

```go
r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
    db := store.GetDB()
    if db == nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte(`{"status":"not ready","reason":"no database"}`))
        return
    }
    if err := db.Ping(); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        w.Write([]byte(`{"status":"not ready","reason":"database unreachable"}`))
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ready"}`))
})
```

---

### 3. Docker HEALTHCHECK -- OK

**File**: `Dockerfile:19-20`

```
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:4000/health || exit 1
```

- `curl` is installed in the runtime image (`apk add ... curl`).
- Interval, timeout, start-period, and retries are all reasonable.
- Uses `curl -f` which fails on non-2xx responses.

**Note**: This health check only verifies the HTTP server is responding. It does not verify DB connectivity (see item 2).

---

### 4. Graceful Shutdown Ordering -- BUG [MEDIUM]

**File**: `app/app.go:50-95`, `cmd/server/main.go:88-110`

The shutdown sequence within `App.Start()` is correct:

1. `OnClose()` -- stops background schedulers
2. `Server.Shutdown(ctx)` with 5s timeout -- drains in-flight requests
3. `store.CloseDatabase()` -- closes DB connection pool

**Bug**: In `cmd/server/main.go:102-110`, after `a.Start()` returns (which already performed the full shutdown), main.go calls `a.Shutdown(ctx)` again:

```go
if err := a.Start(); err != nil {
    slog.Error("server exited with error", "error", err)
    os.Exit(1)
}
// Ensure any remaining cleanup
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
_ = a.Shutdown(ctx)
```

`a.Shutdown()` calls `a.OnClose()` again (line 102 of app.go), which means `StopBackgroundServices()` is called **twice**. The second `Server.Shutdown()` call will return `http.ErrServerClosed` but the error is discarded. This is not harmful but indicates a misunderstanding of the control flow.

**Recommendation**: Remove lines 107-110 from main.go since `Start()` already performs the full shutdown. The code after `Start()` is unreachable in normal operation.

---

### 5. Signal Handling -- OK

**File**: `app/app.go:68-77`

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

select {
case err := <-errCh:
    // listen error path
case sig := <-sigCh:
    slog.Info("received signal, shutting down", "signal", sig.String())
}
```

SIGINT and SIGTERM are both handled. The signal channel is buffered (1) to prevent missed signals.

---

### 6. PID File -- MISSING [LOW]

No PID file is written at startup. Process managers (systemd, supervisord) and some container runtimes benefit from PID files for process tracking.

**Recommendation**: Write the PID to a configurable path (e.g., `/var/run/metapi.pid` or `${DATA_DIR}/metapi.pid`) at startup and remove it during shutdown.

---

### 7. JSON Log Format for Production -- MISSING [MEDIUM]

**Evidence**: No `slog.NewJSONHandler` or `slog.SetDefault()` call exists in the codebase.

The application uses Go's `log/slog` package throughout, which defaults to a **text-based** handler (key=value format). For production environments, structured JSON logging is the standard:

- Container orchestrators (Kubernetes, Docker) ingest JSON logs for structured querying.
- Log aggregation systems (ELK, Loki, Datadog) parse JSON natively.
- Text format requires regex parsing at the log collector.

**Recommendation**: Initialize the default logger with a JSON handler at startup:

```go
import "os"

func init() {
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    })
    slog.SetDefault(slog.New(handler))
}
```

---

### 8. CORS Headers -- PERMISSIVE [LOW]

**File**: `router/middleware.go:13-22`

```go
AllowedOrigins:   []string{"*"},
AllowedHeaders:   []string{"*"},
AllowCredentials: false,
```

- Wildcard origins and headers are overly permissive for a proxy/gateway that handles authentication tokens.
- `AllowCredentials: false` is correct when using wildcard origins (browsers reject `*` with credentials).
- Exposed headers only include `Link`.

**Recommendation**: For production, restrict `AllowedOrigins` to known frontend domains, or support configurable origins via `CORS_ORIGINS` environment variable (comma-separated list). The current wildcard is acceptable if the service is intended to be behind a reverse proxy that handles CORS.

---

### 9. Security Headers -- MISSING [HIGH]

**Evidence**: A search for `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Strict-Transport-Security`, and `Referrer-Policy` returned **zero matches** in all Go source files.

No security header middleware is applied. The following headers should be set on all responses:

| Header | Recommended Value |
|--------|-------------------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` (or `SAMEORIGIN` if SPA) |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `X-XSS-Protection` | `0` (deprecated but belt-and-suspenders) |

For services behind a TLS-terminating reverse proxy, `Strict-Transport-Security` may be set at the proxy level, but `X-Content-Type-Options` and `X-Frame-Options` should be set in the application.

**Recommendation**: Add a security headers middleware in the middleware stack after CORS:

```go
func SecurityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
        next.ServeHTTP(w, r)
    })
}
```

---

### 10. TLS Support -- DELEGATED [MEDIUM]

**File**: `app/app.go:52-55`, `docker-compose.prod.yml:10`

The HTTP server uses `ListenAndServe()` (plain HTTP, no TLS):

```go
a.Server = &http.Server{
    Addr:    addr,
    Handler: a.Router,
}
```

The production docker-compose.yml explicitly documents: "Place nginx/Caddy reverse proxy in front of 127.0.0.1:4000". The port is bound to `127.0.0.1` only, confirming TLS termination is delegated to a reverse proxy.

This is a valid architectural choice (TLS termination at the edge), but the application itself has **zero TLS support**: no `TLS_CERT_FILE`/`TLS_KEY_FILE` environment variables, no `TLSConfig` on the server, no `ListenAndServeTLS` code path.

**Recommendation**: Either (a) document this as a hard requirement in the README, or (b) add optional built-in TLS support for simpler deployments:

```go
if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
    err = a.Server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
} else {
    err = a.Server.ListenAndServe()
}
```

---

### 11. Maximum Body Size Enforcement -- MISSING [HIGH]

**File**: `config/defaults.go:18`, `config/config.go:127,422`

`RequestBodyLimit` is defined as `20 * 1024 * 1024` (20 MB) but is **never enforced**. The value is stored in the config struct but no middleware or handler uses `http.MaxBytesReader` to limit request body size.

**Risk**: An attacker can send arbitrarily large request bodies, consuming memory and potentially causing OOM. This is especially critical for proxy endpoints that forward request bodies to upstream services.

**Recommendation**: Add body size limiting middleware:

```go
func BodyLimit(limit int64) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            r.Body = http.MaxBytesReader(w, r.Body, limit)
            next.ServeHTTP(w, r)
        })
    }
}
```

Apply it in the middleware stack: `r.Use(BodyLimit(int64(cfg.RequestBodyLimit)))`.

---

### 12. Request Timeout (Main Server) -- MISSING [HIGH]

**File**: `app/app.go:52-55`

The main HTTP server has **no timeout configuration**:

```go
a.Server = &http.Server{
    Addr:    addr,
    Handler: a.Router,
}
```

Missing fields:
- `ReadTimeout` -- maximum duration for reading the entire request (including body)
- `WriteTimeout` -- maximum duration before timing out writes of the response
- `IdleTimeout` -- maximum amount of time to wait for the next request on keep-alive connections

Without these, a slow client can hold connections indefinitely. The proxy has its own per-upstream-request timeouts (`ProxyFirstByteTimeoutSec`, executor-level timeouts) but the main server connections are unprotected.

**Recommendation**: Set conservative defaults:

```go
a.Server = &http.Server{
    Addr:         addr,
    Handler:      a.Router,
    ReadTimeout:  30 * time.Second,
    WriteTimeout: 60 * time.Second,
    IdleTimeout:  120 * time.Second,
}
```

---

### 13. Dead Code: app/health.go -- BUG [LOW]

**File**: `app/health.go`

The `Health()` function in `app/health.go` is never called. The router registers an anonymous inline handler for `/health` in `router/router.go:31-35`. The two implementations are identical (both return `{"status":"ok"}`), but the exported `Health` function is dead code and the duplicate is confusing.

**Recommendation**: Either delete `app/health.go` or wire the router to use `app.Health` instead of the inline handler. Having the handler in the `app` package is preferable for testability.

---

### 14. Double Shutdown in main.go -- BUG [MEDIUM]

**File**: `cmd/server/main.go:102-110`, `app/app.go:50-95,102-104`

As described in finding 4, `main.go` calls `a.Shutdown()` after `a.Start()` returns, even though `Start()` already handles the full shutdown sequence. Both `Start()` and `Shutdown()` call `a.OnClose()`, so `StopBackgroundServices()` is called twice.

Additionally, in the error path (line 102-104), `os.Exit(1)` is called before the deferred `a.Shutdown(ctx)` at line 110 would execute, but `Start()` already called `OnClose()` before returning the error. This inconsistency is confusing.

**Recommendation**: Remove lines 107-110 from main.go. The `Start()` method already handles the complete lifecycle.

---

### 15. Default Credential Exposure -- WARN [HIGH]

**File**: `config/defaults.go:5-6`, `config/config.go:339-340`

```go
DefaultAuthToken  = "change-me-admin-token"
DefaultProxyToken = "change-me-proxy-sk-token"
```

If the operator forgets to set `AUTH_TOKEN` and `PROXY_TOKEN` environment variables, the server starts with known default credentials. The production docker-compose.yml enforces these with `${AUTH_TOKEN:?...}` (fails if not set), but the standalone `docker-compose.yml` uses hardcoded `change-me` / `sk-change-me`.

The `config.Load()` function warns about insecure defaults for `AccountCredentialSecret` (line 344) but does not warn for `AUTH_TOKEN` or `PROXY_TOKEN`.

**Recommendation**: Add startup warnings when `AUTH_TOKEN` or `PROXY_TOKEN` match their defaults, and consider rejecting startup with critical errors if they are unchanged in production-like environments.

---

## Architecture Notes

### What is done well

1. **Shutdown sequence**: Signals are correctly caught (SIGINT/SIGTERM), background schedulers are stopped first, followed by server graceful shutdown, then database close. (Aside from the double-call bug.)
2. **Database bootstrap**: `EnsureRuntimeDatabase` is idempotent, handles both SQLite and PostgreSQL, applies correct PRAGMAs (WAL, foreign_keys, busy_timeout), and masks DSN passwords in logs.
3. **Config validation**: Startup validates critical config (port, DB type, schedule mode) and exits on fatal errors.
4. **Scheduler panic isolation**: Each scheduler runs in its own goroutine with panic recovery.
5. **Docker multi-stage build**: Separate build and runtime stages, minimal runtime image (Alpine), static binary with CGO disabled.
6. **Production compose**: Binds to 127.0.0.1 only, documents reverse-proxy TLS termination, enforces required env vars.

### What needs immediate attention (P0)

1. **Add ReadTimeout/WriteTimeout/IdleTimeout** to the main HTTP server (finding 12)
2. **Enforce RequestBodyLimit** with `http.MaxBytesReader` (finding 11)
3. **Add security headers middleware** (finding 9)
4. **Add a `/ready` readiness probe** with DB ping (finding 2)
5. **Switch to JSON log format** for production (finding 7)

### What should be addressed soon (P1)

6. **Remove dead code**: `app/health.go` or wire it in (finding 13)
7. **Fix double shutdown**: Remove lines 107-110 from `cmd/server/main.go` (finding 14)
8. **Warn on default credentials**: Add warnings for unchanged AUTH_TOKEN/PROXY_TOKEN (finding 15)
9. **Add optional TLS support**: Built-in `ListenAndServeTLS` with env var config (finding 10)
10. **Add PID file**: Write PID at startup (finding 6)

---

## Files Examined

| File | Purpose |
|------|---------|
| `app/app.go` | Server lifecycle, signal handling, graceful shutdown |
| `app/health.go` | Dead health check handler |
| `app/services.go` | Background scheduler registration/start/stop |
| `cmd/server/main.go` | Entry point, config loading, bootstrap, shutdown |
| `router/router.go` | Chi router setup, middleware stack, route registration |
| `router/middleware.go` | CORS, request logging, RealIP, recoverer middleware |
| `config/config.go` | Config struct and Load() from env vars |
| `config/defaults.go` | Default constants |
| `config/validate.go` | Startup config validation |
| `store/bootstrap.go` | Database initialization and singleton |
| `store/open.go` | Database connection pool, SQLite pragmas, PG pool |
| `Dockerfile` | Multi-stage build, HEALTHCHECK |
| `docker-compose.yml` | Dev compose (hardcoded credentials) |
| `docker-compose.prod.yml` | Production compose (GHCR image, enforced env vars) |
| `proxy/executor.go` | Upstream request dispatch with timeouts |
| `scheduler/scheduler.go` | Scheduler interface and registry |
| `auth/admin.go` | Admin auth middleware (no security headers) |
| `auth/proxy.go` | Proxy auth middleware (no security headers) |
