# Middleware Ordering Audit: metapi-go

**Date:** 2026-07-05
**Scope:** Router middleware chain in `D:/Code/TokenDance/metapi-go/router/router.go`
**Methodology:** Static analysis of middleware implementations + industry best-practice research
**Final verdict:** NEEDS_FIX (1 CRITICAL ordering issue, 2 informational observations)

---

## 1. Current Middleware Chain (as-deployed)

### Global (applied to ALL routes)

| Order | Middleware | What it does | Cost |
|---|---|---|---|
| 1 | `RealIP` | Sets `r.RemoteAddr` from X-Forwarded-For / X-Real-IP | Trivial (header read) |
| 2 | `CORS()` | Sets CORS headers, handles OPTIONS preflight | Trivial (header write) |
| 3 | `RequestLogger` | `slog.Info("request", method, path, remote)` at request start | Low (one log line, no body) |
| 4 | `Recoverer` | Catches panics, returns 500 | Zero unless panic occurs |
| 5 | `BodyLimit` | Wraps `r.Body` with `http.MaxBytesReader` (lazy -- enforced at read time) | Trivial (reader wrap) |

### `/api/*` subrouter

| Order | Middleware | What it does | Cost |
|---|---|---|---|
| 6 | `AdminAuth` | IP allowlist (CIDR matching), Bearer token comparison | Moderate (CIDR parse + string ops) |
| 7 | `AdminRateLimit` | Per-IP token bucket (100 req/s, burst 200) | Low (mutex + rate.Allow) |
| 8 | `OAuthRateLimit` | Per-IP token bucket for `/api/oauth/*` only (10 req/s, burst 20) | Low (early-return for non-oauth paths) |

### `/v1/*` and non-`/v1` proxy subrouters

| Order | Middleware | What it does | Cost |
|---|---|---|---|
| 6 | `ProxyAuth` | Bearer/x-api-key/x-goog-api-key/?key= extraction + DB key lookup | High (DB query) |

---

## 2. Findings

### CRITICAL

#### C1 -- RateLimit positioned AFTER Auth on `/api/*` (router.go:41-45)

**Current order (incorrect):**
```
AdminAuth → AdminRateLimit → OAuthRateLimit
```

**Required order:**
```
AdminRateLimit → OAuthRateLimit → AdminAuth
```

**Why this matters:**

`AdminAuth` is the expensive middleware. On every request it:
1. Re-parses X-Forwarded-For headers with `extractClientIP()` (despite `RealIP` already having set `r.RemoteAddr`)
2. Iterates the IP allowlist and performs `netip.ParseAddr` + `netip.ParsePrefix` CIDR matching (lines 230-265 of `auth/admin.go`)
3. Extracts and compares the Bearer token via `strings.Replace` + string comparison (line 58-59)

`AdminRateLimit` is cheap: a mutex lock, map lookup, and `rate.Allow()` call.

By placing `AdminAuth` before `AdminRateLimit`, **every request -- including floods of unauthenticated garbage -- passes through the full auth pipeline before rate limiting can reject it**. An attacker sending 10,000 invalid requests per second will force the server to execute 10,000 IP-allowlist iterations and 10,000 token comparisons per second before the rate limiter gets a chance to say "enough."

This defeats the purpose of rate limiting as a DoS protection mechanism.

**Industry consensus:**

| Source | Guidance |
|---|---|
| OWASP | "Rate limiting should be applied early in the middleware stack, before expensive operations like authentication" |
| Cloudflare API Shield | Rate limiting executes before authentication in the request pipeline |
| AWS API Gateway | Throttling is applied before request authorization |
| go-chi community | "Put cheap, broad protection first. Global rate limiting should sit BEFORE authentication to block abusive traffic early" |
| cc-relay (Go reference project) | Middleware order: MaxBodyBytes → Auth → Handler; rate limiting is a form of body/resource protection |

**The two-tier pattern (recommended for future enhancement):**

A common production pattern uses TWO rate-limit layers:
1. **Global IP-based rate limit BEFORE auth** -- protects against volumetric attacks on ALL endpoints including auth itself
2. **Per-user rate limit AFTER auth** -- enforces per-user quotas once identity is known

The current `AdminRateLimit` is IP-based (not user-based), so it belongs in tier 1 (before auth). If per-user rate limiting is added later, it would go after auth.

**Fix (router.go lines 40-45):**

```go
// Current (WRONG):
r.Route("/api", func(r chi.Router) {
    r.Use(auth.AdminAuth(cfg))
    r.Use(auth.AdminRateLimit(100, 200))
    r.Use(auth.OAuthRateLimit(10, 20))

// Fixed (CORRECT):
r.Route("/api", func(r chi.Router) {
    r.Use(auth.AdminRateLimit(100, 200))
    r.Use(auth.OAuthRateLimit(10, 20))
    r.Use(auth.AdminAuth(cfg))
```

**Impact of fix:**
- Unauthenticated flood attacks are rejected at the rate limiter (cheap, 1-2 microsecond mutex+Allow) instead of passing through the full auth pipeline (CIDR parsing, token extraction, string ops)
- Authenticated users who stay within rate limits see identical behavior
- No functional change to how auth works -- auth still rejects invalid tokens, just after rate limiting confirms the requester hasn't exceeded their quota

---

### INFORMATIONAL

#### I1 -- BodyLimit position: current ordering (Logger → BodyLimit) is CORRECT

**Current order:**
```
RequestLogger → Recoverer → BodyLimit
```

**Question asked:** Should BodyLimit be before or after Logger?

**Answer:** Logger should remain BEFORE BodyLimit. The current ordering is optimal.

**Rationale:**

`BodyLimit` uses `http.MaxBytesReader` which is **lazy** -- it wraps the request body but does not read or reject anything in the middleware itself. The limit is only enforced when the handler (or a downstream middleware) actually reads `r.Body`. This means:

- BodyLimit at position 5 does NOT reject oversized requests in the middleware layer -- it only instruments the body reader. The actual rejection happens in the handler when it tries to `io.ReadAll()` or `json.Decode()` the oversized body.
- If BodyLimit were moved before Logger, the Logger would still run before the body is read and the limit enforced. There is zero resource-protection benefit to moving BodyLimit before Logger.
- Keeping Logger before BodyLimit ensures that every request (including eventually-oversized ones) generates an audit log entry with method, path, and remote IP. This is critical for incident response: if someone is sending 1GB payloads as an attack, you want log evidence of it.

**Note on the custom `RequestLogger`:** The current implementation only logs at request START (`slog.Info` before `next.ServeHTTP`). It does not wrap the `ResponseWriter` to capture the response status code. Consider upgrading to chi's built-in `middleware.Logger` (which captures status, bytes written, and duration) for richer observability. This is a separate concern from ordering.

#### I2 -- Missing `RequestID` middleware

Chi's canonical middleware stack (per chi documentation and community examples) includes `middleware.RequestID` as the first middleware to inject a unique request ID into the context. This enables end-to-end request tracing across log lines.

```go
// Recommended addition:
r.Use(middleware.RequestID)  // ← should be FIRST
r.Use(RealIP)
r.Use(CORS())
r.Use(RequestLogger)
...
```

The `RequestLogger` should also be updated to log the request ID:
```go
func RequestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slog.Info("request",
            "request_id", middleware.GetReqID(r.Context()),  // ← add
            "method", r.Method,
            "path", r.URL.Path,
            "remote", r.RemoteAddr,
        )
        next.ServeHTTP(w, r)
    })
}
```

Severity: Low. Not a security issue, but important for production observability.

---

## 3. Verdict Summary

| Issue | Severity | Current | Required | Impact |
|---|---|---|---|---|
| RateLimit after Auth | **CRITICAL** | Auth → RateLimit | RateLimit → Auth | Auth vulnerable to DoS floods |
| BodyLimit after Logger | **OK** | Logger → BodyLimit | Logger → BodyLimit (keep) | Current is correct |
| Missing RequestID | INFO | Not present | Add `middleware.RequestID` first | Observability gap |

---

## 4. Ideal Middleware Chain (Recommended)

```
GLOBAL (applies to all routes):
  1. RequestID        ← traceability (NEW)
  2. RealIP           ← trust proxy (KEEP)
  3. CORS             ← preflight handling (KEEP)
  4. RequestLogger    ← audit log (KEEP)
  5. Recoverer        ← panic safety net (KEEP)
  6. BodyLimit        ← body size cap (KEEP)

/api/* subrouter:
  7. AdminRateLimit   ← IP-based DoS protection (MOVE BEFORE AUTH)
  8. OAuthRateLimit   ← OAuth-specific strict cap (MOVE BEFORE AUTH)
  9. AdminAuth        ← IP allowlist + token (MOVE AFTER RATE LIMIT)

/v1/* subrouter:
 10. ProxyAuth        ← managed key auth (KEEP -- no rate limit, parity with TS)

Non-/v1 proxy subrouter:
 11. ProxyAuth        ← managed key auth (KEEP)
```

---

## 5. Files Referenced

| File | Role |
|---|---|
| `D:/Code/TokenDance/metapi-go/router/router.go` | Route registration and middleware chain (PRIMARY) |
| `D:/Code/TokenDance/metapi-go/router/middleware.go` | Custom middleware implementations (RealIP, CORS, Logger, Recoverer, BodyLimit) |
| `D:/Code/TokenDance/metapi-go/auth/admin.go` | AdminAuth middleware (IP allowlist + Bearer token) |
| `D:/Code/TokenDance/metapi-go/auth/ratelimit.go` | AdminRateLimit + OAuthRateLimit (per-IP token bucket) |
| `D:/Code/TokenDance/metapi-go/auth/proxy.go` | ProxyAuth middleware (managed key lookup) |

---

## 6. Related Audits

| Audit | Relevance |
|---|---|
| `audit-ratelimit.md` (2026-07-04) | Prior audit noting zero rate limiting; rate limiters have since been added but in wrong order |
| `audit-security.md` (2026-07-04) | Covers H1 (non-constant-time token comparison in AdminAuth) -- magnified by current ordering where Auth processes every request before rate limiting |
