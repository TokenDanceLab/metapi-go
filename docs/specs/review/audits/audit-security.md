# MetAPI-Go Security Audit Report

**Audit date:** 2026-07-04
**Scope:** `<repo>` - full Go source tree
**Methodology:** Static analysis of auth middleware, handler/admin/*.go, service/*.go, config/*.go, and router setup
**Final verdict:** NEEDS_FIX (3 CRITICAL, 4 HIGH, 4 MEDIUM, 3 LOW)

---

## Findings

### CRITICAL

#### C1 - Hardcoded default authentication tokens (defaults.go:7-8)
**File:** `config/defaults.go`
**Lines:** 7-8
**Description:** Two hardcoded default tokens serve as fallback when environment variables `AUTH_TOKEN` and `PROXY_TOKEN` are not set:
```go
DefaultAuthToken  = "change-me-admin-token"
DefaultProxyToken = "change-me-proxy-sk-token"
```
These values are well-known and appear in the open-source upstream (NewAPI/OneAPI). If the operator neglects to set the corresponding environment variables, the admin API and proxy endpoints become trivially exploitable by anyone who reads the source.
**Remediation:** Remove all hardcoded token defaults. Require `AUTH_TOKEN` and `PROXY_TOKEN` environment variables at startup, or generate a random token at first launch and persist it to the database.

#### C2 - Admin auth token leaked via Set-Cookie (monitor.go:63)
**File:** `handler/admin/monitor.go`
**Line:** 63
**Description:** The `POST /api/monitor/session` endpoint sets an HttpOnly cookie containing the raw admin Bearer token:
```go
w.Header().Set("Set-Cookie",
    monitorAuthCookie+"="+h.cfg.AuthToken+"; Path=/; HttpOnly; SameSite=Lax; Max-Age=7200")
```
While HttpOnly prevents JavaScript access, the raw admin token is present in the Set-Cookie response header (visible in server responses via browser devtools, proxy logs, and TLS termination points). Any attacker who obtains this cookie value gains full admin API access. The auth token should NEVER appear in a cookie value -- the cookie should carry a separate session token or signed JWT.
**Remediation:** Generate a separate session token for monitor auth (random UUID or JWT). Never reuse the global admin bearer token as a cookie value.

#### C3 - Weak encryption key derivation with hardcoded fallback (account_credential.go:27)
**File:** `service/account_credential.go`
**Lines:** 21-31
**Description:** Account password encryption uses AES-256-GCM, but the key is derived via a single SHA-256 of a secret that has a hardcoded fallback:
```go
if secret == "" {
    secret = "change-me-admin-token" // fallback matches TS config defaults
}
h := sha256.Sum256([]byte(secret))
```
If neither `ACCOUNT_CREDENTIAL_SECRET` nor `AUTH_TOKEN` are configured (or if the operator leaves `AUTH_TOKEN` at the default), the AES key becomes a publicly known SHA-256 hash of `"change-me-admin-token"`. Any encrypted password in the database can be decrypted by anyone with source code access. Additionally, key derivation uses zero salt, zero iterations -- a single raw SHA-256. This is inadequate for password-based key derivation.
**Remediation:** Require `ACCOUNT_CREDENTIAL_SECRET` at startup (refuse to start without it). Use PBKDF2, scrypt, or Argon2id for key derivation with a random salt stored alongside the ciphertext. Use HKDF to derive independent keys for encryption vs authentication.

---

### HIGH

#### H1 - Non-constant-time token comparison (admin.go:59)
**File:** `auth/admin.go`
**Line:** 59
**Description:** Admin Bearer token comparison uses standard string equality:
```go
if token != cfg.AuthToken {
```
Go's `!=` on strings short-circuits on the first differing byte, creating a timing side channel. An attacker can brute-force the token character by character by measuring response time differences. The token length is also leaked (if the input is shorter, the comparison is faster).
**Remediation:** Use `crypto/subtle.ConstantTimeCompare` or `subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AuthToken))`.

#### H2 - Hardcoded real OAuth client IDs (defaults.go:9-10)
**File:** `config/defaults.go`
**Lines:** 9-10
**Description:** Two OAuth client IDs are embedded in source:
```go
DefaultCodexClientId  = "CODEX_CLIENT_ID_PLACEHOLDER"
DefaultClaudeClientId = "CLAUDE_CLIENT_ID_PLACEHOLDER"
```
These appear to be real/production client identifiers (not placeholders). While client IDs are considered "public" in the OAuth 2.0 model, their exposure in open-source code means they could be used for phishing or impersonation campaigns. More critically, they may be tied to rate limits or usage quotas that could be abused if widely known.
**Remediation:** Store OAuth client identifiers as environment-variable-only configuration. Do not embed them in open-source source code.

#### H3 - Overly broad public route bypass (admin.go:77-78)
**File:** `auth/admin.go`
**Lines:** 72-80
**Description:** The public route bypass whitelist includes a full wildcard for OAuth callbacks:
```go
func isPublicAPIRoute(urlPath string) bool {
    if urlPath == "/api/desktop/health" { return true }
    if strings.HasPrefix(urlPath, "/api/oauth/callback/") { return true }
    return false
}
```
This means `/api/oauth/callback/ANYTHING` bypasses all admin auth (token check AND IP allowlist). Currently the callback handler returns only static HTML, but if any future handler is mounted under this path or if the callback handler evolves to accept query parameters that trigger side effects, this becomes a CSRF/SSRF vector. The pattern should only whitelist known legitimate callback path prefixes.
**Remediation:** Narrow the whitelist to known provider names only (e.g., `callback/google`, `callback/github`). Do not use a wildcard.

#### H4 - AES-GCM without key separation (account_credential.go:21-31)
**File:** `service/account_credential.go`
**Lines:** 21-31
**Description:** The same 32-byte SHA-256 hash is used directly as the AES-256 key. While AES-GCM itself is secure, the key derivation has multiple problems:
- No salt: identical passwords across deployments produce identical keys.
- No key separation: if `ACCOUNT_CREDENTIAL_SECRET` is reused elsewhere, key compromise in one place compromises all encrypted passwords.
- No key rotation versioning beyond the `v1` prefix: the "v1" tag in the ciphertext format is cosmetic; the actual key cannot be rotated because old ciphertexts must remain decryptable with the old key, but there is no key ID in the format to distinguish which key was used.
**Remediation:** Use HKDF to derive AES key from `ACCOUNT_CREDENTIAL_SECRET` with a context string and per-encryption random salt. Embed a key ID in the ciphertext format (`v1:<key_id>:iv:tag:ciphertext`) to enable key rotation.

---

### MEDIUM

#### M1 - Missing CSRF protection on state-changing admin endpoints
**Files:** `handler/admin/settings.go`, `handler/admin/accounts.go`, `handler/admin/sites.go`, `handler/admin/downstream_keys.go`
**Description:** All admin state-changing endpoints (POST/PUT/DELETE on settings, accounts, sites, downstream keys) lack CSRF protection. There is no CSRF token requirement, no `Origin`/`Referer` header validation, and no `SameSite` cookie enforcement. An attacker who can trick an authenticated admin into clicking a link could perform state changes (e.g., create a new admin downstream key, change proxy settings). The bearer token auth model (vs cookie-based) partially mitigates this since browsers don't auto-attach `Authorization` headers, but CORS middleware may allow cross-origin requests.
**Remediation:** Add `SameSite=Strict` on any auth cookies. Validate `Origin`/`Referer` headers on state-changing endpoints. Consider requiring a custom header (`X-Requested-With`) for JSON API calls.

#### M2 - No rate limiting on admin authentication
**File:** `auth/admin.go`
**Description:** The admin auth middleware has no rate limiting. An attacker can attempt unlimited bearer token brute-force attempts against the `/api/*` endpoints. Combined with H1 (non-constant-time comparison), this enables practical token exfiltration via timing attacks.
**Remediation:** Implement IP-based rate limiting (exponential backoff) on failed admin auth attempts. Log and alert on repeated failures. Use `crypto/subtle.ConstantTimeCompare` to eliminate the timing channel.

#### M3 - Monitor session auth bypasses IP allowlist (monitor.go:60-64)
**File:** `handler/admin/monitor.go`
**Lines:** 60-64
**Description:** The `POST /api/monitor/session` endpoint is protected by `AdminAuth` (which checks IP allowlist + bearer token). However, it creates a cookie with the raw admin token (`monitor.go:63`). If an attacker who IS authorized (correct IP + bearer token) sets this cookie, and then someone else on a different IP accesses the LDOH proxy endpoint (`/monitor-proxy/ldoh/*`) -- if that endpoint validates the cookie instead of the bearer token -- the IP allowlist is effectively bypassed for that downstream user. Currently the LDOH proxy is a stub, but the architectural pattern is dangerous.
**Remediation:** Do not reuse the admin bearer token as a cookie value. Generate a separate, time-limited session token. The session token should be bound to the client IP that created it.

#### M4 - Unauthenticated /health endpoint (router.go:31-35)
**File:** `router/router.go`
**Lines:** 31-35
**Description:** The `/health` endpoint returns `{"status":"ok"}` with no authentication. While standard for container orchestration (Kubernetes liveness probes), this endpoint also returns unconditionally regardless of whether the database is connected or services are healthy (`app/health.go` was not found -- there may be no actual health check logic). An always-200 health endpoint creates a false sense of security in monitoring.
**Remediation:** Either implement a real health check (verify DB connectivity, service health) or document that this is a liveness-only endpoint. Consider adding a separate `/ready` endpoint for readiness probes.

---

### LOW

#### L1 - Debug trace endpoints could expose sensitive data
**File:** `handler/admin/stats.go`
**Lines:** 236-264
**Description:** The `/api/stats/proxy-debug/traces` and `/api/stats/proxy-debug/traces/{id}` endpoints return debug trace data including request/response details. If debug capture is enabled with `PROXY_DEBUG_CAPTURE_BODIES=true`, these endpoints could expose user prompts, API responses, and potentially API keys. While admin-auth protected, any admin compromise exposes historical user data.
**Remediation:** Consider an additional confirmation step before enabling body capture. Add a data-retention warning in the settings UI.

#### L2 - Error messages may leak internal details
**Files:** Multiple handler files
**Description:** Several endpoints return raw Go error messages to the client, e.g.:
```go
writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
```
Database errors (`err.Error()`) may include table names, column names, query fragments, and connection details. This aids attackers in reconnaissance.
**Remediation:** Log the raw error server-side; return generic messages to clients ("Internal server error", "Account not found") without DB internals.

#### L3 - No JSON body size limits on admin endpoints
**Files:** All handler/admin/*.go POST/PUT handlers
**Description:** Most admin endpoints use `json.NewDecoder(r.Body).Decode(&body)` without any `http.MaxBytesReader` or `io.LimitReader` wrapper. An attacker could send extremely large JSON payloads to exhaust server memory. The `RequestBodyLimit` config (20MB default) appears to apply only to proxy routes, not admin routes. Some handlers (settings.go) read the entire body via `io.ReadAll` before parsing, compounding the issue.
**Remediation:** Apply `http.MaxBytesReader` to all request bodies, or use middleware to enforce a server-wide request body limit.

---

## Observations (not findings)

1. **SQL injection:** The codebase consistently uses parameterized queries (`?` placeholders) via `sqlx`. Dynamic SQL fragments (column names in UPDATE SET clauses) are constructed only from fixed lookup maps where user input is validated against a whitelist before key lookup. No raw string concatenation with user input was found for SQL values. **No SQL injection vulnerabilities found.**

2. **Path traversal:** No file-serving handlers were found that accept user-controlled file paths. File handlers (`proxy/files.go`) parse `fileId` from URL path but use it only as a database lookup key, not a filesystem path. **No path traversal vulnerabilities found.**

3. **Missing auth checks:** The router setup (`router/router.go:39-81`) applies `auth.AdminAuth(cfg)` middleware to the entire `/api` route group, covering all admin CRUD routes. Public bypasses are limited to `/api/desktop/health` and `/api/oauth/callback/*`. Proxy routes under `/v1` use `auth.ProxyAuth(cfg)`. **No unprotected /api endpoints found** (beyond the two documented public paths).

4. **AES-256-GCM:** The actual crypto primitives used (AES-256-GCM with random 12-byte IV from `crypto/rand`) are correct and secure. The issues are in key derivation (C3, H4), not in the cipher itself. GCM is the recommended authenticated encryption mode.

5. **No hardcoded secrets in business logic (other than defaults.go):** Beyond the `config/defaults.go` constants, no secrets (passwords, keys, tokens) were found embedded in Go source files. Configuration is loaded from environment variables.

---

## Summary

| Severity | Count | Verdict |
|----------|-------|---------|
| CRITICAL | 3     | C1 (default tokens), C2 (auth token in cookie), C3 (weak key derivation) |
| HIGH     | 4     | H1 (timing attack), H2 (OAuth client IDs), H3 (broad bypass), H4 (key rotation) |
| MEDIUM   | 4     | M1 (CSRF), M2 (rate limiting), M3 (IP bypass), M4 (/health) |
| LOW      | 3     | L1 (debug traces), L2 (error leaks), L3 (body limits) |

**Overall verdict: NEEDS_FIX**

The three CRITICAL findings must be addressed before production deployment. The CRITICAL issues center on authentication token management and encryption key derivation -- both are foundational to the security of the entire system. The HIGH findings (especially H1 timing attack and H2 exposed OAuth client IDs) should be fixed in the same remediation cycle.
