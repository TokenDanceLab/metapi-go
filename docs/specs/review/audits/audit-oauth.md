# OAuth Flow Security Audit: Go vs TypeScript

**Audit date**: 2026-07-04
**Scope**: `<repo>/service/oauth/` vs `<metapi-ts>/src/server/services/oauth/`
**Method**: Code-review of full flow: PKCE, state, token storage, refresh rotation, clock skew, session TTL, singleflight, callback validation.

---

## Summary

| Dimension | Severity | Go | TS | Parity |
|---|---|---|---|---|
| PKCE generation (SHA256) | OK | Correct | Correct | Identical |
| State entropy (192-bit) | OK | Adequate | Adequate | Identical |
| Token encryption at rest | HIGH | None | None | Identical gap |
| Refresh token rotation | OK | Correct, not atomic | Correct, not atomic | Identical |
| Clock skew tolerance | MEDIUM | None | None | Identical gap |
| Session TTL enforcement | OK | 10 min, prune-on-access | 10 min, prune-on-access | Identical |
| Concurrent refresh (singleflight) | OK | Works, Go bugs | Works | TS cleaner |
| Callback URL validation | OK | Loopback binding | Loopback binding | Identical |

**Verdict**: The two implementations are functionally equivalent. Five findings below: 1 HIGH (tokens plaintext — shared), 2 MEDIUM (no clock skew, hardcoded credentials), 3 LOW (panic paths, SQLite-specific SQL, HTTP client churn).

---

## Finding 1 -- HIGH: No token encryption at rest

**File**: `session.go`, `flow.go`, `refresh.go`, `account.go` (Go); `sessionStore.ts`, `service.ts`, `oauthAccount.ts` (TS)

Tokens are stored as plaintext in the database:

- **Go**: `accounts.access_token` column holds the raw access token. `accounts.extra_config` JSON blob holds `refreshToken`, `idToken`, and provider data in plaintext.
- **TS**: Same pattern, identical risk.

If the database is ever compromised (SQL injection, backup leak, compromised DB credential), all active OAuth tokens for all users are exposed. The attacker can impersonate any user on the upstream provider (Anthropic, OpenAI, Google).

**Recommendation**: Implement application-layer envelope encryption for `refreshToken`, `accessToken`, and `idToken` fields. Use AES-256-GCM with a key derived from a KMS (e.g., HashiCorp Vault, AWS KMS, or a local key file with strict permissions). Encrypt before writing to `extra_config`, decrypt on read. This is a cross-cutting concern -- both Go and TS must be updated together.

**TS parity**: Same gap.

---

## Finding 2 -- MEDIUM: No clock skew tolerance on token expiry

**File**: `claude.go:98-111`, `codex.go:116-129`, `antigravity.go:108-126`, `gemini_cli.go:112-130` (Go); `claudeProvider.ts:45-56` (TS)

All four providers compute `TokenExpiresAt` as:

```go
time.Now().UnixMilli() + int64(expiresIn)*1000
```

This uses the local server clock. If the server clock is ahead of the upstream provider's clock, the computed expiry is optimistic -- the token may actually expire earlier than recorded. If the server clock is behind, tokens are refreshed prematurely.

RFC 6749 recommends subtracting a buffer (typically 30-60 seconds) to account for clock skew and network latency:

```go
time.Now().UnixMilli() + int64(expiresIn)*1000 - 30_000  // 30s buffer
```

**Impact**: Token expiry checks (if any are added in the future) would be incorrect. Currently, neither codebase proactively checks `TokenExpiresAt` before using a token -- they rely on 401 responses from upstream. However, the stored value is displayed in the UI and used by the refresh scheduler (TS: `oauthRefreshScheduler.ts`), so inaccurate expiry can cause premature or delayed refreshes.

**TS parity**: Same gap in `claudeProvider.ts:45-56`, `codexProvider.ts:107`.

---

## Finding 3 -- MEDIUM: Antigravity uses hardcoded placeholder credentials

**File**: `antigravity.go:19-20`

```go
antigravityClientID     = "ANTIGRAVITY_CLIENT_ID_PLACEHOLDER"
antigravityClientSecret = "ANTIGRAVITY_CLIENT_SECRET_PLACEHOLDER"
```

These are literal placeholder strings, never loaded from `config.Get()`. Every other provider (`claude.go`, `codex.go`, `gemini_cli.go`) reads credentials from config:

```go
func requireClaudeClientID() string {
    id := strings.TrimSpace(config.Get().ClaudeClientId)
    ...
}
```

**Impact**: Antigravity OAuth flows will fail with a Google OAuth error because the client ID is invalid. This provider is effectively non-functional in the Go port.

**TS parity**: Need to verify the TS `antigravityProvider.ts` -- from the Go code alone, this is a Go-specific bug.

---

## Finding 4 -- LOW: `panic` calls in request-handler paths

**File**: `session.go:179`, `account.go:225`, `account.go:298`, `claude.go:61`, `codex.go:61`, `gemini_cli.go:77-80`

Multiple functions call `panic` on error conditions that can occur during normal request handling:

1. `randomBase64URL` (session.go:179): panics if `crypto/rand.Read` fails. While rare on Linux (getrandom syscall), it can happen under entropy starvation in containers/VMs. A `panic` here crashes the entire Go process.

2. `BuildOauthInfo` and `BuildOauthInfoFromAccount` (account.go:225, 298): panic if no provider can be determined. These are called from `activatePersistedOAuthAccount` (flow.go:384), which is in the callback handling path. A malformed `extraConfig` could trigger this.

3. `requireClaudeClientID`, `requireCodexClientID`, `requireGeminiCliOAuthConfig`: panic if config is missing. These are initialization-time checks in `init()` functions, which is acceptable (fail-fast on startup). But if config is reloaded dynamically, the panic can occur mid-request.

**Recommendation**: Convert `randomBase64URL` to return `(string, error)`. Convert `BuildOauthInfo` to return errors instead of panicking. For config functions, the `init()` panic is acceptable but add a log message first.

**TS parity**: TS throws `Error` objects, which are caught by the try/catch in `handleOauthCallback` (service.ts:806-814). Go panics are NOT caught by any recovery middleware visible in this codebase.

---

## Finding 5 -- LOW: SQLite-specific SQL in refresh.go

**File**: `refresh.go:138`

```go
_, err = db.Exec(
    `UPDATE accounts SET ... updated_at = datetime('now') WHERE id = ?`,
    ...
)
```

`datetime('now')` is SQLite syntax. PostgreSQL requires `NOW()` or `CURRENT_TIMESTAMP`. Other parts of the codebase consistently use `time.Now().Format(time.RFC3339)` with parameterized queries:

```go
// flow.go:430
now := time.Now().Format(time.RFC3339)
db.Exec("UPDATE accounts SET ... updated_at = ? WHERE id = ?", ..., now, accountID)
```

**Impact**: Token refresh persistence will fail on PostgreSQL backends. The singleflight code will return an error, and the caller will retry the refresh, creating a tight retry loop with the upstream provider.

**TS parity**: Not applicable (TS uses Drizzle ORM which generates dialect-appropriate SQL).

---

## Finding 6 -- LOW: `doHTTP` creates new HTTP client per request

**File**: `codex.go:311-323`

```go
func doHTTP(req *http.Request, proxyURL *string, client *http.Client) (*http.Response, error) {
    if client == nil {
        client = &http.Client{Timeout: 30 * time.Second}
    }
    if proxyURL != nil && *proxyURL != "" {
        proxy, err := url.Parse(*proxyURL)
        if err == nil {
            client.Transport = &http.Transport{
                Proxy: http.ProxyURL(proxy),
            }
        }
    }
    return client.Do(req)
}
```

A fresh `http.Client` (and `http.Transport`) is created on every call. This means:
- No connection pooling -- every request opens a new TCP+TLS connection.
- No HTTP/2 multiplexing.
- No idle connection reuse.

**Recommendation**: Use a package-level shared `http.Client` with `http.DefaultTransport` as the base, configuring proxy per-request via `http.Transport.Proxy` which is already a function. The proxy function can read from a context value.

**TS parity**: TS uses `undici` fetch with `withExplicitProxyRequestInit`, which benefits from undici's built-in connection pooling.

---

## Dimension-by-dimension analysis

### 1. PKCE code_verifier/code_challenge generation

**Go**: `session.go:164-172`

- `CreatePKCEVerifier`: 48 bytes from `crypto/rand` (384 bits), base64url-encoded -> ~64 chars. RFC 7636 compliant (min 43 chars).
- `CreatePKCEChallenge`: `sha256.Sum256([]byte(codeVerifier))` then `base64.RawURLEncoding.EncodeToString`. Correct S256.
- Used by: Claude (`claude.go:75`) and Codex (`codex.go:75-76`). Gemini-CLI and Antigravity use Google OAuth with `client_secret` (no PKCE needed), which is correct.

**TS**: `sessionStore.ts:43-54`

- Same: `randomBytes(48)` -> identical entropy. `webcrypto.subtle.digest('SHA-256', ...)` -> identical S256.
- Async (`createPkceChallenge` returns `Promise<string>`), correctly awaited in providers.

**Verdict**: Both correct. Go's `randomBase64URL` has the panic issue (Finding 4).

### 2. State parameter entropy and validation

**Go**: `session.go:64`

- State: `randomBase64URL(24)` -> 24 bytes from `crypto/rand` = 192 bits entropy. Exceeds OAuth 2.0 best practices (128+ bits recommended).
- Validation in `HandleCallback` (flow.go:144-148):
  - Session lookup by state
  - Provider match check (`session.Provider != input.Provider`) -- prevents cross-provider state confusion.
  - `SubmitManualCallback` (flow.go:233) additionally checks `parsed.State != input.State`.
- No constant-time comparison -- negligible risk given 192-bit random value.

**TS**: `sessionStore.ts:76`

- Same: `randomBytes(24)` -> identical entropy. Same validation logic.

**Verdict**: Both adequate. Identical behavior.

### 3. Token storage encryption

Covered in Finding 1. No encryption in either implementation.

### 4. Refresh token rotation handling

**Go**: `refresh.go:61-155`

- `doRefreshAccessToken` calls provider's `RefreshAccessToken`, gets back a `TokenSet` with potentially a new `refreshToken`.
- Merge uses `coalesceStr(refreshed.RefreshToken, oauth.RefreshToken)` -- new token wins if present.
- Stored in `extraConfig.oauth.refreshToken` via `BuildOauthInfoFromAccount` -> `MergeAccountExtraConfig`.
- All four providers return `RefreshToken` in their refresh responses.
- **Non-atomic**: If the upstream refresh succeeds but the DB write fails, the old refresh token remains in the DB while the provider may have already invalidated it.

**TS**: `service.ts:1210-1272`

- Same pattern: `refreshed.refreshToken || oauth.refreshToken`. Same non-atomic concern.

**Verdict**: Both correct in the success path. Same atomicity gap (acceptable for this use case since the singleflight wrapper prevents concurrent refreshes).

### 5. Expiry clock skew tolerance

Covered in Finding 2. Neither implementation has it.

### 6. Session TTL enforcement

**Go**: `session.go:31,48-54`

- `sessionTTL = 10 * time.Minute`
- `pruneExpiredSessions` called on every `Create` and `Get`.
- TOCTOU gap: between `GetSession` (which prunes) and callback completion, the session could expire. Window is effectively zero for any practical attack since both operations are in-memory and sequential.

**TS**: `sessionStore.ts:37,59-64`

- `SESSION_TTL_MS = 10 * 60 * 1000` -> identical. Same prune-on-access pattern. Same TOCTOU gap.

**Verdict**: Both adequate for short-lived OAuth flows. Identical.

### 7. Concurrent refresh dedup (singleflight)

**Go**: `refresh.go:31-59`

- Manual singleflight with `map[int64]*refreshPromise` + channels.
- Buffered channel (`make(chan refreshResult, 1)`) -- non-blocking write.
- `defer` cleanup handles panics.
- **Issue**: SQLite-specific SQL (Finding 5).

**TS**: `refreshSingleflight.ts`

- `Map<number, Promise>` -- idiomatically cleaner.
- `Promise.finally()` ensures cleanup regardless of resolution/rejection.

**Verdict**: Both functional. TS implementation is cleaner. Go has the SQLite-specific SQL bug.

### 8. Callback URL validation (open redirect prevention)

**Go**: `callback_server.go:260-297`

- Binds to `127.0.0.1` (loopback) per provider config -- only local callbacks possible.
- Validates: GET method, exact path match.
- Extracts `state` and `code` from query params, passes to `HandleCallback`.
- `HandleCallback` validates state via session lookup.
- `SubmitManualCallback` parses URL, validates state match, extracts code.

**TS**: `localCallbackServer.ts:89-126`

- Same pattern, same validation logic.
- Same loopback binding.
- Same manual callback path.

**Verdict**: Both rely on loopback binding as primary defense. No redirect_uri whitelist validation needed since the server only accepts local connections. Acceptable for loopback-based OAuth flows.

---

## Go-Only Additional Observations

1. **`prompt` parameter included in all Codex auth URLs** (codex.go:77): `params.Set("prompt", "login")` -- forces re-login even if session cookie exists. This is a UX choice, not a security issue, but worth noting it differs from the TS codexProvider.ts which also includes `prompt: 'login'` (line 74). Actually both include it -- identical.

2. **No context propagation in HTTP calls**: `doHTTP` accepts a `context.Context` nowhere. The `http.Client.Do` uses `http.NewRequest` without a context, meaning: (a) no request timeout beyond the 30s client timeout, (b) no cancellation propagation if the caller's context is cancelled. The TS code uses `undici` fetch which has its own timeout but also doesn't wire through AbortSignal from context.

3. **`init()` registration pattern**: All providers register themselves in `init()` functions. This is correct Go -- providers are registered at program startup before any requests are served. TS uses module-level evaluation, equivalent.

---

## TS-Only Observations

1. **`CODEX_CALLBACK_PATH = '/api/oauth/callback/codex'`** (codexProvider.ts:12): This constant exists but appears unused in the code scanned. If there is a server-side (non-loopback) callback endpoint using this path elsewhere in the TS codebase, it would need redirect_uri validation against a whitelist to prevent open redirect attacks.

2. **`inferCodexOfficialOriginator`** (codexProvider.ts:238): The TS Codex provider uses `inferCodexOfficialOriginator(downstreamHeaders)` to determine the Originator header. The Go version (codex.go:247) only checks `getHeaderValue(input.DownstreamHeaders, "originator")`. This might be a missing feature in the Go port -- the `inferCodexOfficialOriginator` function may use additional heuristics.

---

## Recommended Action Items (priority order)

| Priority | Item | Owner |
|---|---|---|
| P0 | Implement token encryption at rest (both Go + TS) | Shared infra |
| P1 | Add 30s clock skew buffer to all `TokenExpiresAt` computations (both) | Go + TS |
| P1 | Fix Antigravity hardcoded credentials -- load from config (Go) | Go |
| P2 | Convert `randomBase64URL` to return error instead of panic (Go) | Go |
| P2 | Convert `BuildOauthInfo` to return error instead of panic (Go) | Go |
| P2 | Fix `datetime('now')` -> parameterized `NOW()`/`time.Now()` in refresh.go (Go) | Go |
| P2 | Share a single `http.Client` across requests, use per-request proxy via context (Go) | Go |
| P3 | Verify `inferCodexOfficialOriginator` parity between Go and TS | Go |
| P3 | Add cancellation context propagation to OAuth HTTP calls (both) | Both |
