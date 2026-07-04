# Rate Limiting Audit: metapi-go vs metapi (TS)

**Date:** 2026-07-04
**Scope:** Rate limiting enforcement, proxy session concurrency, admin IP allowlist
**Version:** Go rewrite parity audit

---

## 1. Executive Summary

**CRITICAL GAP:** The Go codebase has **zero per-IP rate limiting on /api/ admin endpoints**. The TypeScript codebase has **15 per-endpoint rate limit guards** plus **6 rate-limiter-flexible instances** for OAuth sensitive routes. This is a significant security regression in the Go rewrite.

**Proxy session channel concurrency:** Well-implemented in Go. No issues found.

---

## 2. /api/ Endpoint Rate Limiting

### 2.1 Go (metapi-go): NONE

The only protection on /api/ routes is the `AdminAuth` middleware (`auth/admin.go`), which enforces:
1. IP CIDR allowlist check (config `ADMIN_IP_ALLOWLIST`)
2. Bearer token matching `config.AuthToken`

There is **no request rate counter, no time window, no 429 response for excessive requests**. An authenticated admin IP can hammer any /api/ endpoint at unlimited rate.

Route registration in `router/router.go`:
```go
r.Route("/api", func(r chi.Router) {
    r.Use(auth.AdminAuth(cfg))  // IP allowlist + bearer token ONLY
    // ... all /api/* handlers registered here
})
```

### 2.2 TypeScript (metapi): 15 per-IP guards + 6 rate-limiter-flexible instances

The TS codebase implements per-IP, per-endpoint rate limiting via `createRateLimitGuard` (in `src/server/middleware/requestRateLimit.ts`). Each guard uses an in-memory `Map<key, {count, resetAt}>` keyed by `${bucket}:${clientIP}`.

**Rate limit guards in TS:**

| Bucket | Endpoint | Max | Window |
|---|---|---|---|
| `accounts-login` | Account login | 5 | 60s |
| `accounts-verify-token` | Token verification | 5 | 60s |
| `auth-change` | Admin token change | 3 | 60s |
| `monitor-config-read` | Monitor config read | 30 | 60s |
| `monitor-config-write` | Monitor config write | 10 | 60s |
| `monitor-session` | Monitor session create | 10 | 60s |
| `monitor-proxy` | LDOH proxy | 60 | 60s |
| `oauth-provider-read` | OAuth provider list | 60 | 60s |
| `oauth-start` | OAuth flow start | 20 | 60s |
| `oauth-session-read` | OAuth session read | 120 | 60s |
| `oauth-session-mutate` | OAuth session mutate | 30 | 60s |
| `oauth-connection-read` | OAuth connection read | 60 | 60s |
| `oauth-connection-mutate` | OAuth connection mutate | 20 | 60s |
| `oauth-callback` | OAuth callback | 30 | 60s |
| `models-token-candidates-read` | Models token candidates | 30 | 60s |

**Additional rate-limiter-flexible instances** (20 points/60s each, per-IP via `request.ip`):

| Key Prefix | Used On |
|---|---|
| `oauth-connection-sensitive-quota-batch` | POST quota/refresh-batch |
| `oauth-connection-sensitive-proxy` | PATCH proxy update |
| `oauth-connection-sensitive-import` | POST import |
| `oauth-connection-sensitive-route-unit-create` | POST route-units |
| `oauth-connection-sensitive-route-unit-update` | PATCH route-units/:id |
| `oauth-connection-sensitive-route-unit-delete` | DELETE route-units/:id |

**Total: 21 rate limiting policies covering /api/ endpoints in TS. Zero in Go.**

---

## 3. /v1/ Proxy Endpoint Rate Limiting

### 3.1 Both codebases: NO PER-IP RATE LIMITING (PARITY)

Neither the Go nor the TS codebase applies per-IP rate limiting to /v1/ proxy endpoints. The protection is:

| Layer | Go | TS | Parity |
|---|---|---|---|
| Auth middleware | `ProxyAuth` (Bearer/x-api-key/x-goog-api-key/?key=) | `proxyAuthMiddleware` (same extraction logic) | MATCH |
| Managed key auth | `AuthorizeDownstreamToken` → downstream_api_keys DB | `authorizeDownstreamToken` → same DB | MATCH |
| Key hard cap | `max_requests` / `max_cost` (lifetime counters) | Same | MATCH |
| Per-IP throttle | **NONE** | **NONE** | MATCH |
| Per-key throttle | **NONE** | **NONE** | MATCH |

### 3.2 Managed key limits (both codebases)

The `downstream_api_keys` table has `max_requests` (hard lifetime cap) and `max_cost` fields. These are absolute limits, not time-windowed rate limits. A key with `max_requests=1000` can make all 1000 requests in the first second.

When exceeded, the response is **403 Forbidden** (not 429 Rate Limited). There is no `retry-after` header.

---

## 4. Admin IP Allowlist

Both codebases implement identical logic:

| Feature | Go (`auth/admin.go`) | TS (`middleware/auth.ts`) | Parity |
|---|---|---|---|
| Exact IP match | Yes | Yes | MATCH |
| CIDR match (IPv4 only) | Yes | Yes | MATCH |
| Empty allowlist = allow all | Yes | Yes | MATCH |
| `::ffff:` normalization | Yes | Yes | MATCH |
| `::1` → `127.0.0.1` | Yes | Yes | MATCH |
| X-Forwarded-For handling | Yes (first segment) | Yes (first segment) | MATCH |
| Config source | `ADMIN_IP_ALLOWLIST` CSV | `config.adminIpAllowlist` | MATCH |

The IP allowlist is the **only** rate-limiting-adjacent protection on Go /api/ endpoints. It is an access control mechanism, not a rate limiter.

---

## 5. Burst Allowance

**Neither codebase implements burst allowance.**

- TS `createRateLimitGuard`: Simple count-reset window. Once `count >= max`, all requests in that window are rejected. No burst > max is ever allowed.
- TS `RateLimiterMemory`: The `rate-limiter-flexible` library supports `blockDuration` after exhausting points, but the code uses default behavior (strict limit, no burst).
- Go: No rate limiter exists at all.

---

## 6. Proxy Session Channel Concurrency (Go only)

### 6.1 Implementation: `proxy/session.go`

The `ProxyChannelCoordinator` implements a lease-based concurrency control for session-scoped channels:

| Feature | Implementation | Assessment |
|---|---|---|
| Concurrency limit per channel | `SessionChannelConcurrencyLimit` (configurable, default 0=disabled) | CORRECT |
| Queue with timeout | `SessionChannelQueueWaitMs` (configurable, default 0=no wait) | CORRECT |
| Lease TTL + keepalive | `LeaseTtlMs` + background keepalive goroutine | CORRECT |
| Stale binding cleanup | Timer-based expiry in `GetStickyChannelID` | CORRECT |
| Cancelled waiter pruning | `pruneCancelledWaitersLocked` before drain | CORRECT |
| Thread safety | `sync.Mutex` on coordinator and per-channel state | CORRECT |
| Goroutine leak prevention | `doneCh` channel closes on Release, both timers stop | CORRECT |

### 6.2 Code quality observations

**Well-handled:**
- `AcquireChannelLease` returns a noop lease when concurrency limit is 0 or channelID <= 0
- Queue drain is re-entrant safe (called under `state.mu`)
- `pruneAndMaybeDelete` double-checks with re-lock to avoid TOCTOU
- Both expiry timer and keepalive timer are properly cleaned up on `Release()`

**Minor concern (non-blocking):**
- `touchLease` spawns a new goroutine per call. If keepalive interval is very short (e.g., 100ms), this could accumulate. The keepalive goroutine in `createTrackedLease` uses a ticker-based approach which is correct. The `touchLease` goroutine is self-terminating.

---

## 7. Retry Policy and 429 Handling

| Scenario | Go (`proxy/retry_policy.go`) | TS | Parity |
|---|---|---|---|
| 429 = always retryable | Yes | Yes | MATCH |
| 429 + rate_limit text → abort same-site | Yes (`ShouldAbortSameSiteEndpointFallback`) | Yes | MATCH |
| Usage limit cooldown on 429 | Yes (5min default in `router.go`) | Yes | MATCH |
| OAuth quota reset hint from 429 body | Yes (`RecordOauthQuotaResetHint`) | Yes | MATCH |

These are **channel-level retry policies**, not client-facing rate limiting. They handle what happens when upstream responds 429.

---

## 8. Gap Summary

| Item | Go | TS | Severity |
|---|---|---|---|
| /api/ per-IP rate limiting | **MISSING** (21 guard instances) | Present | CRITICAL |
| /v1/ per-IP rate limiting | **MISSING** | **MISSING** | PARITY (both missing) |
| /v1/ per-key rate limiting | **MISSING** | **MISSING** | PARITY (both missing) |
| Burst allowance | **MISSING** | **MISSING** | PARITY (both missing) |
| Admin IP allowlist | Present | Present | MATCH |
| Managed key absolute caps | Present | Present | MATCH |
| Proxy channel concurrency limit | Present | N/A (Go-only feature) | GO-ONLY |
| Rate-limiter-flexible OAuth guards | **MISSING** (6 instances) | Present | CRITICAL |

---

## 9. Recommendations

1. **P0 (CRITICAL):** Implement per-IP rate limiting middleware for all /api/ routes in Go matching the TS `createRateLimitGuard` pattern. At minimum, the 15 endpoint-specific guards must be ported.

2. **P0 (CRITICAL):** Implement the 6 `rate-limiter-flexible` equivalents for OAuth sensitive operations (quota batch refresh, proxy update, import, route unit CRUD).

3. **P1 (HIGH):** Consider adding per-key time-windowed rate limiting on /v1/ proxy endpoints (e.g., X requests per minute per downstream key). Neither codebase has this, but it is a valuable addition.

4. **P2 (MEDIUM):** Consider adding burst allowance to rate limiters (e.g., allow burst of 2x the sustained rate for short periods).

5. **P3 (LOW):** Consider switching `touchLease` to a ticker-based approach to reduce goroutine churn, or add a minimum interval guard.

---

## 10. Files Referenced

| File | Role |
|---|---|
| `D:/Code/TokenDance/metapi-go/router/router.go` | Go route registration (no rate limit middleware) |
| `D:/Code/TokenDance/metapi-go/auth/admin.go` | Go admin auth (IP allowlist only) |
| `D:/Code/TokenDance/metapi-go/auth/proxy.go` | Go proxy auth (key auth, no rate limit) |
| `D:/Code/TokenDance/metapi-go/auth/downstream.go` | Go managed key auth (lifetime caps only) |
| `D:/Code/TokenDance/metapi-go/proxy/session.go` | Go channel concurrency (well-implemented) |
| `D:/Code/TokenDance/metapi-go/proxy/retry_policy.go` | Go retry policy (429 handling correct) |
| `D:/Code/TokenDance/metapi-go/handler/admin/monitor.go` | Go monitor routes (rate limit comment but not enforced) |
| `D:/Code/TokenDance/metapi-go/handler/admin/oauth_routes.go` | Go OAuth routes (no rate limit middleware) |
| `D:/Code/TokenDance/metapi/src/server/middleware/requestRateLimit.ts` | TS rate limit guard implementation |
| `D:/Code/TokenDance/metapi/src/server/routes/api/oauth.ts` | TS OAuth routes (15 guards + 6 rate-limiter instances) |
| `D:/Code/TokenDance/metapi/src/server/routes/api/accounts.ts` | TS account routes (2 guards) |
| `D:/Code/TokenDance/metapi/src/server/routes/api/auth.ts` | TS auth routes (1 guard) |
| `D:/Code/TokenDance/metapi/src/server/routes/api/monitor.ts` | TS monitor routes (4 guards) |
| `D:/Code/TokenDance/metapi/src/server/routes/api/stats.ts` | TS stats routes (1 guard) |
| `D:/Code/TokenDance/metapi/src/server/middleware/auth.ts` | TS auth middleware (IP allowlist + proxy auth) |
| `D:/Code/TokenDance/metapi/src/server/routes/proxy/router.ts` | TS proxy routes (no per-IP rate limiting) |
