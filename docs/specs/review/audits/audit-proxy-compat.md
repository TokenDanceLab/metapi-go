# Reverse Proxy Compatibility Audit: metapi-go

**Date:** 2026-07-05
**Scope:** Reverse proxy header handling, IP extraction, scheme/protocol awareness, trusted proxy configuration
**Files audited:**
- `<repo>/router/middleware.go`
- `<repo>/app/app.go`
- `<repo>/router/router.go`
- `<repo>/auth/admin.go`
- `<repo>/handler/admin/settings.go` (lines 698-710)
- `<repo>/config/config.go`
- `<repo>/docs/deployment.md`
- `<repo>/service/oauth/flow.go`
- `<repo>/service/oauth/callback_server.go`
- `<go-module-cache>/github.com/go-chi/chi/v5@v5.3.0/middleware/realip.go`
- `<go-module-cache>/github.com/go-chi/chi/v5@v5.3.0/middleware/client_ip.go`

**Final verdict:** NEEDS_FIX (2 CRITICAL, 1 MEDIUM, 2 LOW)

---

## 1. Current State Summary

### Reverse proxy layer (nginx, from deployment.md)

The documented nginx config sends three proxy headers:

```nginx
proxy_set_header Host $host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
```

### Application layer (metapi-go)

The application processes these headers as follows:

| Header | Handled by | Location | Method |
|---|---|---|---|
| `X-Real-IP` | chi `middleware.RealIP` (deprecated) | `router/middleware.go:39-41` | Sets `r.RemoteAddr` |
| `X-Forwarded-For` | chi `middleware.RealIP` (deprecated) | `router/middleware.go:39-41` | Sets `r.RemoteAddr` (first comma segment) |
| `X-Forwarded-For` | `auth/admin.go:extractClientIP()` | `auth/admin.go:94-116` | Re-parses XFF manually after RealIP |
| `X-Forwarded-For` | `handler/admin/settings.go:extractClientIP()` | `handler/admin/settings.go:698-710` | Separate, simpler re-parsing |
| `X-Forwarded-Proto` | **NOT HANDLED** | -- | -- |
| `X-Forwarded-Host` | **NOT HANDLED** | -- | -- |

---

## 2. Findings

### CRITICAL

#### C1 -- Deprecated `middleware.RealIP` trusts ALL proxies unconditionally (router/middleware.go:39-41)

The middleware stack uses chi's deprecated `middleware.RealIP`:

```go
// router/middleware.go:37-41
// RealIP reads X-Forwarded-For / X-Real-IP headers.
// Equivalent to TS Fastify `trustProxy: true`.
func RealIP(next http.Handler) http.Handler {
    return middleware.RealIP(next)
}
```

**Why this is critical:**

chi/v5.3.0 explicitly deprecates `middleware.RealIP` with a security advisory referencing three GitHub Security Advisories:

> Deprecated: RealIP is vulnerable to IP spoofing -- it mutates r.RemoteAddr
> to the leftmost X-Forwarded-For value, or to True-Client-IP / X-Real-IP
> whether or not your infrastructure actually sets them. See
> GHSA-3fxj-6jh8-hvhx, GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf.

The fundamental problem: `RealIP` has no concept of trusted vs. untrusted proxies. It takes the first (leftmost) X-Forwarded-For value unconditionally. If an attacker can reach the application directly (bypassing nginx), they can inject any IP they want via the X-Forwarded-For header. This IP then becomes:

1. **The effective IP for admin rate limiting** (`AdminRateLimit` -- per-IP token bucket)
2. **The effective IP for admin IP allowlist checks** (`AdminAuth` -- `AdminIpAllowlist` CIDR matching)
3. **The value logged as `remote`** in request logs (`RequestLogger`)

**Compounding severity:** The `AdminRateLimit` middleware and `AdminIpAllowlist` both depend entirely on the client IP for their security guarantees. If IP spoofing is possible, both protections are trivially bypassed. An attacker can:
- Cycle forged IPs to evade per-IP rate limits entirely
- Forge an allowlisted IP to bypass the admin IP allowlist (if `AdminIpAllowlist` is non-empty)

**The TS equivalent** (`trustProxy: true` in Fastify) also trusts all proxies by default in single-proxy setups. However, Fastify's approach is a deliberate design trade-off for simplicity when the app listens on localhost only. The Go code currently inherits this "trust all" posture but does so using a library function that its own maintainers have deprecated as a known security vulnerability.

**Proof:** From chi source (`realip.go:39-53`):
```go
func realIP(r *http.Request) string {
    var ip string
    if tcip := r.Header.Get(trueClientIP); tcip != "" {
        ip = tcip
    } else if xrip := r.Header.Get(xRealIP); xrip != "" {
        ip = xrip
    } else if xff := r.Header.Get(xForwardedFor); xff != "" {
        ip, _, _ = strings.Cut(xff, ",")
    }
    if ip == "" || net.ParseIP(ip) == nil {
        return ""
    }
    return ip
}
```

No CIDR check. No trusted proxy list. Any value in any of these headers wins.

**Fix:**

Replace `middleware.RealIP` with chi's newer `ClientIPFromXFF` or `ClientIPFromHeader`. The correct choice depends on the deployment model:

**Option A: Single trusted proxy on localhost (most common for this project)**

```go
// router/middleware.go
// RealIP extracts the client IP from X-Forwarded-For, trusting only
// the rightmost entry added by nginx on localhost.
// Equivalent to TS Fastify `trustProxy: true` but with trusted-proxy awareness.
func RealIP(next http.Handler) http.Handler {
    return middleware.ClientIPFromXFFTrustedProxies(1)(next)
}
```

This trusts exactly 1 proxy hop (the local nginx). The rightmost XFF entry (added by the last/only proxy) is discarded as trusted; the one before it is the client. If XFF has only one entry, no IP is set (fail-closed -- client connected directly, which should not happen behind nginx).

To read the client IP downstream, use `middleware.GetClientIP(r.Context())` instead of `r.RemoteAddr`.

**Option B: Multiple trusted proxies with known CIDRs (robust)**

```go
func RealIP(next http.Handler) http.Handler {
    return middleware.ClientIPFromXFF(
        "10.0.0.0/8",     // internal network
        "172.16.0.0/12",  // Docker
        "192.168.0.0/16", // internal network
        "127.0.0.0/8",    // localhost
    )(next)
}
```

This walks XFF right-to-left, skipping any IP in the trusted CIDRs, and returns the first untrusted IP as the client.

**Option C: X-Real-IP only (simplest, if nginx unconditionally overwrites it)**

```go
func RealIP(next http.Handler) http.Handler {
    return middleware.ClientIPFromHeader("X-Real-IP")(next)
}
```

`ClientIPFromHeader` takes the LAST value of a multi-valued header (fail-closed against appending proxies). This is safe because nginx's `proxy_set_header X-Real-IP $remote_addr` replaces (not appends) the value.

**Recommendation:** Option A (`ClientIPFromXFFTrustedProxies(1)`) is the best fit -- it matches the current single-reverse-proxy deployment model, is simple to configure, and is fail-closed (if someone bypasses nginx, no client IP is set rather than trusting a forged one).

Additionally, replace all direct `r.RemoteAddr` references with `middleware.GetClientIP(r.Context())`.

---

#### C2 -- X-Forwarded-Proto is NOT handled (no scheme awareness)

The nginx config sends `X-Forwarded-Proto $scheme`, but the application has zero code that reads this header.

**Impact:**

1. **No HTTPS-aware URL generation.** If any handler generates an absolute URL in a response body or `Location` header, it will use `http://` even when the client connected via HTTPS. Currently no such URLs are generated (OAuth uses loopback `http://localhost:*` and the SPA is embedded), but this is a latent defect that will silently break when such features are added.

2. **No redirect hardening.** There is no middleware to enforce HTTPS redirects. A client could theoretically make plaintext HTTP requests to the app if the proxy's TLS enforcement is misconfigured.

3. **CORS `Origin` checking.** The current CORS config allows `*` origins, so scheme mismatch doesn't matter. If origin restrictions are tightened in the future, `http://` vs `https://` in the Origin header could cause legitimate requests to be rejected.

**TS comparison:** The TS Fastify codebase also has no explicit X-Forwarded-Proto handling. This is parity, but it is still a gap.

**Fix:**

Add a middleware that reads `X-Forwarded-Proto` and sets `r.URL.Scheme`:

```go
// ForwardedProto reads X-Forwarded-Proto and updates r.URL.Scheme.
// Required for correct scheme-aware URL generation behind TLS-terminating proxies.
func ForwardedProto(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
            r.URL.Scheme = proto
        }
        next.ServeHTTP(w, r)
    })
}
```

Register it early in the middleware chain:

```go
r.Use(RealIP)
r.Use(ForwardedProto)  // after RealIP, before CORS/Logger
r.Use(CORS())
```

---

### MEDIUM

#### M1 -- Duplicate `extractClientIP` with inconsistent robustness (auth/admin.go vs handler/admin/settings.go)

Two different `extractClientIP` implementations exist:

**auth/admin.go:94-116** (robust):
```go
func extractClientIP(r *http.Request) string {
    for _, xff := range r.Header.Values("X-Forwarded-For") {
        xff = strings.TrimSpace(xff)
        if xff != "" {
            if idx := strings.IndexByte(xff, ','); idx >= 0 {
                xff = xff[:idx]
            }
            ip := strings.TrimSpace(xff)
            if ip != "" {
                return normalizeIP(ip)
            }
        }
    }
    return normalizeIP(stripPort(r.RemoteAddr))
}
```

This handles: multi-header XFF (Header.Values), empty entries, comma-split, IPv4-mapped IPv6 normalization, IPv6 bracket port stripping.

**handler/admin/settings.go:698-710** (simpler, less robust):
```go
func extractClientIP(r *http.Request) string {
    xff := r.Header.Get("X-Forwarded-For")
    if xff != "" {
        parts := strings.Split(xff, ",")
        return strings.TrimSpace(parts[0])
    }
    addr := r.RemoteAddr
    if idx := strings.LastIndex(addr, ":"); idx > 0 {
        return addr[:idx]
    }
    return addr
}
```

This uses `Header.Get` (only first header value), no IP normalization, port stripping uses `LastIndex(":")` which is incorrect for IPv6 (e.g., `[::1]:1234` would become `[::1]` then `[` if stripPort is called).

**Impact:** The settings.go version is used in the `POST /api/settings/notify/test` handler. If a client behind a proxy with multi-header XFF calls this endpoint, the wrong IP could be extracted. Normalization differences could cause IP allowlist mismatches.

**Fix:** Consolidate to a single `extractClientIP` function in a shared location (e.g., `router/middleware.go` or a new `util/ip.go`). The settings.go version should delegate to `middleware.GetClientIP(r.Context())` once the RealIP migration (C1) is complete.

---

### LOW

#### L1 -- `r.RemoteAddr` used directly in request logging instead of GetClientIP

`router/middleware.go:31` logs `r.RemoteAddr` directly:

```go
slog.Info("request",
    "method", r.Method,
    "path", r.URL.Path,
    "remote", r.RemoteAddr,
)
```

After C1 migration to `ClientIPFromXFF`, the client IP will be stored in the request context, not in `r.RemoteAddr`. The logger should use `middleware.GetClientIP(r.Context())` for the correct IP.

**Fix (after C1 migration):**
```go
slog.Info("request",
    "method", r.Method,
    "path", r.URL.Path,
    "remote", middleware.GetClientIP(r.Context()),
)
```

#### L2 -- No `X-Forwarded-Host` handling

The nginx config sets `proxy_set_header Host $host;`, so the Go `r.Host` already reflects the correct host. However, if the deployment uses a non-standard setup where `Host` is rewritten, the `X-Forwarded-Host` header would be needed. This is a very low-risk gap given the documented deployment pattern always sets Host.

No fix required unless multi-proxy or CDN deployments are planned.

---

## 3. Impact Assessment by Security Surface

| Security Surface | Depends on IP | Risk if IP spoofed |
|---|---|---|
| `AdminRateLimit` (100 req/s per IP) | Yes -- `extractClientIP(r)` | DoS protection defeated; attacker can flood admin API |
| `OAuthRateLimit` (10 req/s per IP) | Yes -- `extractClientIP(r)` | DoS protection defeated; attacker can flood OAuth endpoints |
| `AdminAuth` IP allowlist | Yes -- `extractClientIP(r)` | Allowlist bypassed; attacker can access admin API from any IP |
| `RequestLogger` | Yes -- `r.RemoteAddr` | Audit trail poisoned with forged IPs |
| Proxy auth (`ProxyAuth`) | No -- uses Bearer/api-key | Not affected |

**Conclusion:** All three IP-dependent security mechanisms are vulnerable to spoofing because the deprecated `middleware.RealIP` trusts all proxy headers unconditionally.

---

## 4. Recommended Middleware Chain (After Fixes)

```
GLOBAL (applies to all routes):
  1. RequestID           ← traceability (from audit-middleware.md I2)
  2. RealIP              ← ClientIPFromXFFTrustedProxies(1) — secure IP extraction (FIXED)
  3. ForwardedProto      ← X-Forwarded-Proto → r.URL.Scheme (NEW)
  4. CORS                ← preflight handling (KEEP)
  5. RequestLogger       ← audit log (use GetClientIP) (FIXED)
  6. Recoverer           ← panic safety net (KEEP)
  7. BodyLimit           ← body size cap (KEEP)

/api/* subrouter:
  8. AdminRateLimit      ← IP-based DoS protection (MOVED before auth per audit-middleware.md C1)
  9. OAuthRateLimit      ← OAuth-specific strict cap
 10. AdminAuth           ← IP allowlist + token (use GetClientIP)

/v1/* subrouter:
 11. ProxyAuth           ← managed key auth (KEEP)
```

---

## 5. Remediation Checklist

- [ ] **C1**: Replace `middleware.RealIP` with `middleware.ClientIPFromXFFTrustedProxies(1)` in `router/middleware.go`
- [ ] **C1**: Update `auth/admin.go:extractClientIP()` to use `middleware.GetClientIP(r.Context())` instead of manually parsing XFF
- [ ] **C1**: Update `handler/admin/settings.go:extractClientIP()` to use `middleware.GetClientIP(r.Context())` instead of its own XFF parsing
- [ ] **C1**: Update `router/middleware.go:RequestLogger` to log `middleware.GetClientIP(r.Context())` instead of `r.RemoteAddr`
- [ ] **C1**: Add unit test: XFF spoofing by direct client connection (no trusted proxy) should result in no client IP
- [ ] **C1**: Add unit test: legitimate XFF from trusted proxy should extract correct client IP
- [ ] **C2**: Add `ForwardedProto` middleware to `router/middleware.go`
- [ ] **C2**: Register `ForwardedProto` in the global middleware chain after `RealIP` in `router/router.go`
- [ ] **M1**: Remove the duplicate `extractClientIP` in `handler/admin/settings.go`; consolidate to context-based approach
- [ ] **L1**: Update `RequestLogger` to use `GetClientIP` from context
- [ ] **Docs**: Update `docs/deployment.md` to document the trusted proxy assumption (exactly 1 proxy hop)

---

## 6. Verification Commands

After fixes are applied, verify with:

```bash
# Test 1: Spoofing blocked (direct connection with forged XFF)
curl -H "X-Forwarded-For: 10.0.0.99" http://localhost:4000/health
# Expected: health check passes but the IP logged should be the actual
# connection IP, not 10.0.0.99. Verify in server logs.

# Test 2: Legitimate proxy chain (simulate nginx)
curl -H "X-Forwarded-For: 203.0.113.5" http://localhost:4000/api/sites
# Expected: 401 (no auth), but client IP should be 203.0.113.5.
# Verify in server logs and that rate limit counters use 203.0.113.5.

# Test 3: X-Forwarded-Proto awareness
curl -H "X-Forwarded-Proto: https" http://localhost:4000/api/sites
# Expected: r.URL.Scheme should be "https" in handler context.
```

---

## 7. Files Referenced

| File | Role |
|---|---|
| `<repo>/router/middleware.go` | Custom middleware: RealIP (uses deprecated chi.RealIP), CORS, RequestLogger |
| `<repo>/router/router.go` | Middleware chain registration |
| `<repo>/auth/admin.go` | AdminAuth with IP extraction and allowlist |
| `<repo>/handler/admin/settings.go` | Duplicate extractClientIP in notify test handler |
| `<repo>/config/config.go` | No trusted proxy CIDR config fields |
| `<repo>/app/app.go` | HTTP server startup (no proxy awareness) |
| `<repo>/docs/deployment.md` | Nginx config sending proxy headers |
| `<go-module-cache>/github.com/go-chi/chi/v5@v5.3.0/middleware/realip.go` | Deprecated RealIP -- trusts all, no CIDR filter |
| `<go-module-cache>/github.com/go-chi/chi/v5@v5.3.0/middleware/client_ip.go` | Replacement APIs: ClientIPFromXFF, ClientIPFromXFFTrustedProxies, ClientIPFromHeader |

---

## 8. Related Audits

| Audit | Relevance |
|---|---|
| `audit-middleware.md` (2026-07-05) | C1: RateLimit must be before Auth. Shares dependency on correct IP extraction. |
| `audit-security.md` (2026-07-04) | H1: Non-constant-time token comparison. IP spoofing makes token comparison irrelevant if allowlist is bypassable. |
| `audit-ratelimit.md` (2026-07-04) | IP-based rate limiting depends on correct IP extraction from trusted proxy headers. |
