# P6: OAuth Subsystem (4 Providers + Route Units)

**S.U.P.E.R**: S (single responsibility) . P (port-first) . R (replaceable) | **Depends on**: P3 + P4 | **Size**: L

## Original TS Reference
- `D:\Code\TokenDance\metapi\src\server\services\oauth\providers.ts` -- provider registry
- `D:\Code\TokenDance\metapi\src\server\services\oauth\service.ts` -- flow orchestrator
- `D:\Code\TokenDance\metapi\src\server\services\oauth\codexProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\claudeProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\geminiCliProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\antigravityProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\sessionStore.ts` -- in-memory session + PKCE
- `D:\Code\TokenDance\metapi\src\server\services\oauth\localCallbackServer.ts` -- loopback HTTP
- `D:\Code\TokenDance\metapi\src\server\services\oauth\refreshSingleflight.ts` -- lazy refresh
- `D:\Code\TokenDance\metapi\src\server\services\oauth\routeUnitService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\quota.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\oauthAccount.ts`

## Go Module Structure
```
service/oauth/
  provider.go            # OAuthProviderDefinition interface
  registry.go            # 4 provider registry
  codex.go               # Codex OAuth implementation
  claude.go              # Claude OAuth implementation
  gemini_cli.go          # Gemini CLI OAuth implementation
  antigravity.go         # Antigravity OAuth implementation
  flow.go                # StartFlow + HandleCallback + manual callback orchestration
  session.go             # In-memory session store + PKCE utilities
  callback_server.go     # Local loopback HTTP callback server
  refresh.go             # Lazy token refresh + singleflight dedup
  connection.go          # Connection management (list/delete/rebind/proxy)
  import.go              # Batch import OAuth connections from native JSON
  quota.go               # Quota snapshot refresh
  account.go             # OAuth account info serialization to extraConfig
  route_unit.go          # OAuth route unit CRUD (strategy execution lives in TokenRouter)
  headers.go             # buildProxyHeaders (proxy path OAuth headers)
```

## Type Definitions

### OAuthProviderId
```go
type OAuthProviderId string

const (
    ProviderCodex      OAuthProviderId = "codex"
    ProviderClaude     OAuthProviderId = "claude"
    ProviderGeminiCli  OAuthProviderId = "gemini-cli"
    ProviderAntigravity OAuthProviderId = "antigravity"
)
```

### ProviderMetadata
```go
type ProviderMetadata struct {
    Provider                   OAuthProviderId // "codex" | "claude" | "gemini-cli" | "antigravity"
    Label                      string          // "Codex" | "Claude" | "Gemini CLI" | "Antigravity"
    Platform                   string          // e.g. "codex", "claude", "gemini-cli", "antigravity"
    Enabled                    bool
    LoginType                  string          // always "oauth"
    RequiresProjectId          bool            // true for gemini-cli only
    SupportsDirectAccountRouting bool
    SupportsCloudValidation    bool
    SupportsNativeProxy        bool
}
```

### ProviderSiteConfig
```go
type ProviderSiteConfig struct {
    Name     string // e.g. "ChatGPT Codex OAuth"
    URL      string // upstream base URL
    Platform string
}
```

### LoopbackConfig
```go
type LoopbackConfig struct {
    Host        string // e.g. "127.0.0.1"
    Port        int    // e.g. 1455 (codex), 54545 (claude), 8085 (gemini-cli), 51121 (antigravity)
    Path        string // e.g. "/auth/callback" (codex), "/callback" (claude)
    RedirectURI string // e.g. "http://localhost:1455/auth/callback"
}
```

### TokenSet (exchange/refresh result)
```go
type TokenSet struct {
    AccessToken    string                 // required
    RefreshToken   string                 // optional
    TokenExpiresAt int64                  // optional, Unix ms
    Email          string                 // optional
    AccountKey     string                 // optional, unique account identity key
    AccountID      string                 // optional, alternative identity
    PlanType       string                 // optional
    ProjectID      string                 // optional, GCP project for gemini-cli/antigravity
    IDToken        string                 // optional, JWT id_token from OAuth response
    ProviderData   map[string]interface{} // optional, provider-specific opaque data (e.g. organization for claude, tokenType/scope for gemini-cli)
}
```

### ProxyHeaderInput
```go
type ProxyHeaderInput struct {
    OAuth struct {
        Provider     string
        AccountKey   string
        AccountID    string
        ProjectID    string
        ProviderData map[string]interface{}
    }
    DownstreamHeaders map[string]interface{} // optional original downstream request headers
}
```

### SessionRecord
```go
type SessionStatus string

const (
    SessionPending SessionStatus = "pending"
    SessionSuccess SessionStatus = "success"
    SessionError   SessionStatus = "error"
)

type SessionRecord struct {
    Provider        string         // provider ID
    State           string         // random base64url, 24 bytes -- used as lookup key
    Status          SessionStatus  // pending | success | error
    CodeVerifier    string         // PKCE code_verifier (raw, 48 random bytes base64url)
    RedirectURI     string         // the redirect_uri used for this flow
    CreatedAt       time.Time
    UpdatedAt       time.Time
    ExpiresAt       time.Time      // createdAt + 10 min
    AccountID       int64          // set on markSuccess
    SiteID          int64          // set on markSuccess
    Error           string         // set on markError
    RebindAccountID int64          // optional, if this is a rebind flow
    ProjectID       string         // optional, for gemini-cli project scoping
    ProxyURL        string         // optional, explicit per-flow proxy
    UseSystemProxy  bool           // optional, whether to use system proxy
}
```

## Functional Spec

### Provider Interface
```go
type OAuthProviderDefinition struct {
    Metadata ProviderMetadata
    Site     ProviderSiteConfig
    Loopback LoopbackConfig

    // BuildAuthorizationURL constructs the OAuth authorization URL.
    // Input: state (session state), redirectUri (loopback redirect), codeVerifier (raw PKCE verifier), optional projectId
    // The provider computes code_challenge internally from codeVerifier (for PKCE providers) or omits PKCE (for client_secret providers).
    // Returns the full URL the user should be redirected to.
    BuildAuthorizationURL func(ctx context.Context, input BuildAuthURLInput) (string, error)

    // ResolveRedirectURI is optional. If provided, it allows the provider to customize the callback URI
    // based on the incoming request origin (e.g., for non-loopback environments). If not implemented,
    // the loopback redirectUri is used as-is.
    ResolveRedirectURI func(ctx context.Context, input ResolveRedirectURIInput) (string, error) // optional

    // ExchangeAuthorizationCode exchanges the OAuth authorization code for tokens.
    // Input: code, state, redirectUri, codeVerifier (for PKCE), optional projectId, optional proxyUrl
    // Returns TokenSet with accessToken, refreshToken, and identity fields.
    ExchangeAuthorizationCode func(ctx context.Context, input ExchangeCodeInput) (*TokenSet, error)

    // RefreshAccessToken refreshes an expired access token using the refresh token.
    // Input: refreshToken, oauth context (projectId, providerData from previously stored state), optional proxyUrl
    // Returns TokenSet with new tokens.
    RefreshAccessToken func(ctx context.Context, input RefreshTokenInput) (*TokenSet, error)

    // BuildProxyHeaders constructs provider-specific proxy headers for upstream requests.
    // Input: full OAuth identity + optional downstream headers.
    // Returns headers map (never errors). Optional -- if not implemented, no extra headers are added.
    BuildProxyHeaders func(ctx context.Context, input ProxyHeaderInput) map[string]string // optional
}

type BuildAuthURLInput struct {
    State        string
    RedirectURI  string
    CodeVerifier string
    ProjectID    string // optional, for gemini-cli
}

type ResolveRedirectURIInput struct {
    RequestOrigin string // optional, the Origin header from the incoming request
}

type ExchangeCodeInput struct {
    Code         string
    State        string
    RedirectURI  string
    CodeVerifier string
    ProjectID    string  // optional
    ProxyURL     *string // optional, nullable
}

type RefreshTokenInput struct {
    RefreshToken string
    OAuth        *RefreshOAuthContext // optional
    ProxyURL     *string              // optional, nullable
}

type RefreshOAuthContext struct {
    ProjectID    string
    ProviderData map[string]interface{}
}
```

### Provider Capability Model

Not all providers use PKCE. Two distinct OAuth patterns exist:

| Provider | PKCE | client_secret | Post-exchange onboarding | requiresProjectId |
|----------|------|---------------|--------------------------|-------------------|
| Codex    | Yes (S256) | No | No | No |
| Claude   | Yes (S256) | No | No | No |
| Gemini CLI | **No** | Yes (Google OAuth) | **Yes** (loadCodeAssist + onboardUser polling + API enablement) | Yes |
| Antigravity | **No** | Yes (Google OAuth) | **Yes** (loadCodeAssist + onboardUser polling, simpler than Gemini CLI) | No |

The Go implementation must support both PKCE and client_secret flows. Each provider is responsible for its own authorization URL construction and code exchange. The flow orchestrator should NOT assume all providers use PKCE -- the raw codeVerifier is passed to BuildAuthorizationURL, and each provider decides whether to compute a code_challenge from it.

### Provider-Specific Details

#### Codex
- Auth URL: `https://auth.openai.com/oauth/authorize`
- Token URL: `https://auth.openai.com/oauth/token`
- Loopback: port 1455, path `/auth/callback`
- Uses PKCE with code_challenge_method=S256
- Non-standard auth params: `prompt=login`, `id_token_add_organizations=true`, `codex_cli_simplified_flow=true`
- Exchange validates JWT id_token, extracts `chatgpt_account_id` from `https://api.openai.com/auth` claims -- throws if missing
- Token exchange form: `grant_type=authorization_code`, `client_id`, `code`, `redirect_uri`, `code_verifier`
- Refresh form: `grant_type=refresh_token`, `client_id`, `refresh_token`, `scope=openid profile email`
- accountId = accountKey = chatgpt_account_id from JWT
- buildProxyHeaders: sets `Originator` header (from downstream or defaults to `codex_cli_rs`), sets `Chatgpt-Account-Id` if accountId exists
- Config: requires `CODEX_CLIENT_ID` env var, throws on missing

#### Claude
- Auth URL: `https://claude.ai/oauth/authorize`
- Token URL: `https://api.anthropic.com/v1/oauth/token`
- Loopback: port 54545, path `/callback`
- Uses PKCE with code_challenge_method=S256
- Auth params additionally include: `code=true`
- Token exchange sends JSON body (not URL-encoded form), includes `state` in the body
- Exchange response parses `account.uuid` as accountId, `account.email_address` as email, `organization.uuid`/`organization.name` as providerData
- accountKey = account.uuid || account.email_address
- Refresh body: `grant_type=refresh_token`, `client_id`, `refresh_token`
- buildProxyHeaders: sets `anthropic-version: 2023-06-01`
- Config: requires `CLAUDE_CLIENT_ID` env var, throws on missing

#### Gemini CLI
- Auth URL: `https://accounts.google.com/o/oauth2/v2/auth`
- Token URL: `https://oauth2.googleapis.com/token`
- Loopback: port 8085, path `/oauth2callback`
- Does NOT use PKCE. Uses Google OAuth confidential-client flow with `client_secret`
- Scopes: `cloud-platform`, `userinfo.email`, `userinfo.profile`
- Auth params: `response_type=code`, `access_type=offline`, `prompt=consent` -- no code_challenge
- Exchange sends URL-encoded form: `code`, `client_id`, `client_secret`, `redirect_uri`, `grant_type=authorization_code`
- After exchange, performs multi-phase onboarding (see Gemini CLI Onboarding below)
- Refresh sends: `client_id`, `client_secret`, `grant_type=refresh_token`, `refresh_token`
- On refresh, oauth.projectId is used if available; otherwise re-runs project discovery
- accountId = accountKey = email
- buildProxyHeaders: `User-Agent` and `X-Goog-Api-Client` static headers
- Config: requires `GEMINI_CLI_CLIENT_ID` and `GEMINI_CLI_CLIENT_SECRET` env vars, throws on missing

#### Antigravity
- Auth URL: `https://accounts.google.com/o/oauth2/v2/auth`
- Token URL: `https://oauth2.googleapis.com/token`
- Loopback: port 51121, path `/oauth-callback`
- Does NOT use PKCE. Uses client_secret (same Google pattern as Gemini CLI)
- Scopes: `cloud-platform`, `userinfo.email`, `userinfo.profile`, `cclog`, `experimentsandconfigs`
- Exchange sends: `code`, `client_id` (placeholder), `client_secret` (placeholder), `redirect_uri`, `grant_type=authorization_code`
- After exchange: fetches user email, then runs project discovery (see Antigravity Onboarding below)
- client_id/client_secret are hardcoded placeholders (not config-driven like Gemini CLI)
- ideType in internal API client metadata: `ANTIGRAVITY` (vs Gemini CLI's `IDE_UNSPECIFIED`)
- On refresh, oauth.projectId is used if available; otherwise re-discovers project
- buildProxyHeaders: `User-Agent`, `X-Goog-Api-Client`, `Client-Metadata` static headers

### Gemini CLI Onboarding (multi-phase, post-exchange)

After token exchange, `ensureGeminiProjectAndOnboard` runs:

1. **Project selection**: If user provided an explicit projectId, use it. Otherwise, call `fetchGcpProjects` -- if the list is empty, throw "no Google Cloud projects available for this account".
2. **loadCodeAssist**: Call internal API `v1internal:loadCodeAssist` with project and metadata. Extract `defaultTierId` from `allowedTiers` (find tier with `isDefault: true`, fallback to `"legacy-tier"`). Extract `cloudaicompanionProject` ID.
3. **Auto-onboard** (if no project found): Poll `v1internal:onboardUser` up to 15 times at 2s intervals. On `done: true`, extract project from response.
4. **Phase 2 onboard**: Poll `v1internal:onboardUser` with the resolved project up to 6 times at 5s intervals.
5. **Project mismatch resolution**: If an explicit projectId was requested and the onboard response returns a different project:
   - If the requested project is a free user project (starts with `gen-lang-client-`), use the server's response
   - Otherwise, keep the explicitly requested project
6. **API enablement** (`checkCloudAIAPIEnabled`): Check if `cloudaicompanion.googleapis.com` is enabled. If not, call `:enable` endpoint. Handle 400 "already enabled" as success.
7. **On refresh**: Re-run onboarding only if no oauth.projectId is stored; otherwise reuse stored projectId.

### Antigravity Onboarding (post-exchange)

Simpler than Gemini CLI:

1. Call `loadCodeAssist` with metadata (ideType: `ANTIGRAVITY`)
2. If `cloudaicompanionProject` found in response, use it
3. Otherwise, poll `onboardUser` up to 5 times at 2s intervals
4. Returns discovered projectId (or undefined if not found)

Note: Antigravity does NOT run `checkCloudAIAPIEnabled` and does NOT do a second onboard phase.

### OAuth Flow

```
StartFlow(provider, rebindAccountId?, projectId?, proxyUrl?, useSystemProxy?, requestOrigin?):
  1. Validate provider is registered (getOAuthProviderDefinition returns non-nil)
  2. Check callback server state: if attempted && !ready, throw error with callback listener unavailable message.
     NOTE: StartFlow does NOT start the callback server. The server is started independently via
     startOAuthLoopbackCallbackServer(). StartFlow only checks if it is available.
  3. Create session via session store:
     - Random state (24 bytes base64url)
     - Random code_verifier (48 bytes base64url)
     - Redirect URI from provider's loopback config
     - Store rebindAccountId, projectId, proxyUrl, useSystemProxy in session
     - TTL: 10 minutes from createdAt
     - Session status: "pending"
  4. Call provider.BuildAuthorizationURL({
       state: session.state,
       redirectUri: session.redirectUri,
       codeVerifier: session.codeVerifier,   // raw codeVerifier, NOT codeChallenge
       projectId: session.projectId,         // for gemini-cli project pinning
     })
  5. Return {provider, state, authorizationUrl, instructions}
     - instructions include redirectUri, callbackPort, callbackPath, manualCallbackDelayMs(15000)
     - If requestOrigin is not loopback (not localhost/127.0.0.1/::1), include SSH tunnel commands:
       ssh -L {port}:127.0.0.1:{port} root@{host} -p 22
       ssh -i <path_to_your_key> -L {port}:127.0.0.1:{port} root@{host} -p 22

getOauthSessionStatus(state):
  1. Look up session by state
  2. If not found, return null
  3. Return {provider, state, status, accountId, siteId, error, createdAt, updatedAt}

HandleCallback(provider, state, code?, error?):
  1. Look up session by state
  2. Validate: session exists && session.provider == input.provider
     - If mismatch or missing: throw "oauth session not found or provider mismatch"
  3. If input.error is non-empty: mark session as error, throw input.error
     - This handles OAuth error callbacks (user denied consent, etc.)
  4. Validate code is non-empty; if empty: mark session error, throw
  5. Resolve proxy URL with three-tier fallback:
     a. session.proxyUrl (explicit per-flow proxy) -- highest priority
     b. session.useSystemProxy ? resolveProxyUrlFromExtraConfig({useSystemProxy: true}) : nil
     c. resolveOauthProviderProxyUrl(provider) -- provider-specific default
  6. Call provider.ExchangeAuthorizationCode({
       code, state, redirectUri, codeVerifier, projectId, proxyUrl
     })
  7. activatePersistedOauthAccount:
     a. Upsert OAuth account (see upsertOauthAccount below)
     b. If not created (existing account updated), save previousAccount + previousModelAvailability for rollback
     c. Refresh models for account via refreshModelsForAccount (allowInactive for existing updates)
     d. If model refresh fails: full rollback (see revertPersistedOauthAccount below)
     e. If existing and activateExistingAfterRefresh: set status to "active"
     f. Rebuild routes
     g. On route rebuild fail: rollback account + restore routes
  8. On success: mark session as success (status=success, accountId, siteId)
  9. On any error: mark session as error with error message, throw

submitOauthManualCallback(state, callbackUrl):
  1. Look up session by state; throw if not found
  2. Parse callbackUrl (user-pasted full redirect URL):
     - Extract state, code, error, error_description from query params
     - Validate: state non-empty && (code non-empty || error non-empty)
     - Parse error_description to append to error: "{error}: {error_description}"
  3. Validate parsed.state == input.state; throw "oauth callback state mismatch"
  4. Delegate to handleOauthCallback(provider, state, code, error)
  5. Return {success: true}
  This is the manual callback flow for non-local environments (SSH tunnel scenario).
```

### upsertOauthAccount

```
upsertOauthAccount(definition, exchange, rebindAccountId?, proxyUrl?, useSystemProxy?, persistedStatus?):
  1. Ensure provider site exists (ensureOauthProviderSite)
  2. Find existing account via findExistingOauthAccount:
     a. If rebindAccountId: look up directly by account ID
     b. Otherwise, lookup by provider + accountKey + projectId:
        - Match oauthProvider + oauthAccountKey + (oauthProjectId exact OR NULL/empty)
     c. Fallback (non-codex providers): lookup by provider + username (email) if no accountKey
     d. Returns nil if no existing account found
  3. Build username: email || accountKey || "{provider}-user"
  4. Build oauth info via buildOauthInfo (merges existing extraConfig.oauth with exchange data)
  5. Build extraConfig: mergeAccountExtraConfig with credentialMode="session", proxy settings, oauth stored state
  6. If existing account FOUND (update):
     - Update fields: siteId, username, accessToken, apiToken=null, checkinEnabled=false
     - Status: persistedStatus ?? "disabled"  (default disabled on update)
     - oauthProvider, oauthAccountKey, oauthProjectId, extraConfig, updatedAt
     - Returns: {account, site, created=false, previousAccount=existing}
  7. If NO existing account (create):
     - Insert with: siteId, username, accessToken, apiToken=null, checkinEnabled=false
     - Status: persistedStatus ?? "active"  (default active on create)
     - oauthProvider, oauthAccountKey, oauthProjectId, extraConfig
     - isPinned=false, sortOrder = max(sortOrder) + 1
     - Returns: {account, site, created=true, previousAccount=nil}
```

### revertPersistedOauthAccount (rollback)

```
revertPersistedOauthAccount(accountId, created, previousAccount, previousModelAvailability):
  1. If created=true (new account): delete account by ID
  2. If created=false (updated existing): restore ALL previous fields in a transaction:
     - siteId, username, accessToken, apiToken, checkinEnabled, status
     - oauthProvider, oauthAccountKey, oauthProjectId
     - extraConfig, updatedAt
     - Delete current modelAvailability for accountId
     - Re-insert previousModelAvailability rows
```

### Token Refresh (Lazy Singleflight)

```
RefreshAccessToken(accountID):
  1. Singleflight: check map[accountID] -> if promise exists, return it (dedup concurrent refreshes)
  2. If no existing promise, create new promise:
     a. Load account from DB
     b. Parse oauth info from extraConfig; require refreshToken (throw if missing)
     c. Get provider definition
     d. Call provider.RefreshAccessToken({
          refreshToken: oauth.refreshToken,
          oauth: {projectId, providerData},  // pass stored OAuth context
          proxyUrl: resolveOauthAccountProxyUrl(siteId, extraConfig),
        })
     e. Build nextOauth: merge refreshed fields with existing stored state (refreshed fields take precedence)
     f. Update account in DB:
        - accessToken = refreshed.accessToken
        - oauthProvider, oauthAccountKey, oauthProjectId
        - extraConfig (merged with credentialMode="session", new oauth stored state)
        - status = "active"  (set to active on successful refresh)
        - updatedAt
     g. Return {accountId, accessToken, accountKey, extraConfig}
  3. Singleflight cleanup: promise.finally() deletes map entry -- guaranteed cleanup even on error
     NOTE: On refresh failure, no account status change occurs. The error propagates to caller.
     The account is NOT marked "expired". The singleflight entry is cleaned up so the next
     attempt gets a fresh try.
```

### Connection Management

```
listOauthConnections(limit?, offset?):
  - Default limit=50, max 200, min 1. Default offset=0.
  - Ensure OAuth identity backfill (ensureOauthIdentityBackfill)
  - Query accounts WHERE oauthProvider IS NOT NULL, ordered by id DESC, paginated
  - For each account:
    - Parse oauth info
    - Query modelAvailability (available=true) to build modelsPreview (first 10)
    - Query route unit participation via listOauthRouteUnitsByAccountIds
    - Query route channel counts grouped by accountId and oauthRouteUnitId
    - Status: "abnormal" if modelDiscoveryStatus=abnormal OR account.status!=active OR site.status!=active; else "healthy"
    - Include: proxyUrl, useSystemProxy from extraConfig
    - Include: quota snapshot via buildQuotaSnapshotFromOauthInfo
    - Include: routeParticipation (kind=route_unit, id, name, strategy, memberCount)
  - Returns {items[], total, limit, offset}

deleteOauthConnection(accountId):
  - Validate account exists and is OAuth-managed
  - Delete account by ID
  - Rebuild routes
  - Returns {success: true}

updateOauthConnectionProxySettings(accountId, proxyUrl?, useSystemProxy?):
  - Validate account exists and is OAuth-managed
  - Merge proxyUrl and/or useSystemProxy into extraConfig
  - Refresh models for account (allowInactive=true)
  - Rebuild routes
  - Returns {success, accountId, proxyUrl, useSystemProxy, refreshedRoutes, modelRefresh{status, errorMessage}}

startOauthRebindFlow(accountId, requestOrigin?, proxyUrl?, useSystemProxy?):
  - Load account, validate OAuth-managed
  - Delegate to startOauthProviderFlow with:
    - provider = oauth.provider
    - rebindAccountId = accountId
    - projectId = oauth.projectId
    - proxyUrl from options, fallback to account's stored proxyUrl
    - useSystemProxy from options, fallback to account's stored useSystemProxy
    - requestOrigin from options
```

### Import OAuth Connections from Native JSON

```
importOauthConnectionsFromNativeJson(data?, items?, proxyUrl?, useSystemProxy?):
  1. Normalize input: if items[] is non-empty, use items; else wrap single data into array
  2. continueOnItemFailure = isArray(input.items) (batch mode: continue on individual failures)
  3. Validate: 0 < len(items) <= 100 (MAX_OAUTH_IMPORT_BATCH_SIZE)
  4. For each payload:
     a. Validate: must be a record (plain object), not sub2api envelope
        - Reject if type is "sub2api-data" or "sub2api-bundle"
        - Reject if payload has "accounts" or "proxies" or "version" or "exported_at" keys
     b. Determine provider:
        - If type field present, normalize: "openai"->"codex", "anthropic"->"claude", "gemini"->"gemini-cli"
        - Throw if unsupported
     c. Extract credentials:
        - accessToken: access_token || session_token (required)
        - refreshToken: refresh_token
        - tokenExpiresAt: parseImportedOauthExpiry(expired) -- supports numeric Unix timestamp AND ISO 8601 date strings
        - idToken: id_token
        - email: email || JWT claims
        - accountKey: chatgpt_account_id || account_key || account_id || JWT claims || email
        - planType: plan_type || JWT claims
        - projectId: project_id || cloudaicompanionProject
        - providerData: from provider_data field
     d. disabled = payload.disabled === true -> persistedStatus = "disabled"
     e. activatePersistedOauthAccount with the resolved exchange
     f. On success: record as imported
     g. On failure:
        - If batch mode (continueOnItemFailure=true): record as failed, continue to next item
        - If single mode: throw immediately
  5. Returns {success, imported, skipped(=0), failed, items[]}
```

### Quota Snapshot Refresh

```
refreshOauthQuotaSnapshot(accountId):
  - Only implemented for Codex provider
  - For non-codex: build "unsupported" snapshot, persist
  - For codex:
    a. Send POST /responses probe request to Codex upstream with model "gpt-5.4"
    b. Parse x-codex-primary-* and x-codex-secondary-* rate limit headers
    c. Normalize headers into 5h/7d windows based on window_minutes
    d. If 429 with usage_limit_reached: parse resets_at or resets_in_seconds as reset hint
    e. If headers present: build "reverse_engineered" snapshot with used/limit/remaining/resetAt
    f. If probe fails: build "error" snapshot
    g. Persist snapshot to account extraConfig.oauth.quota

refreshOauthConnectionQuotaBatch(accountIds):
  - Deduplicate account IDs, run concurrently with max 4 workers
  - Per account: call refreshOauthConnectionQuota, catch errors individually
  - Returns {success, refreshed, failed, items[{accountId, success, quota?, error?}]}

refreshOauthConnectionQuota(accountId):
  - Wraps refreshOauthQuotaSnapshot
  - Returns {success: true, quota}

recordOauthQuotaHeadersSnapshot(accountId, headers):
  - Only for codex provider
  - Deduplicate by fingerprint within 30s window
  - Also deduplicate in-progress (pending) snapshots by (accountId + fingerprint) key
  - Build snapshot from headers, persist

recordOauthQuotaResetHint(accountId, statusCode, errorText):
  - Only for codex, only on 429
  - Parse 429 error body for usage_limit_reached reset hint
  - Persist as lastLimitResetAt
```

### buildOauthProviderHeaders / buildProxyHeaders

```
buildOauthProviderHeaders(account, extraConfig, downstreamHeaders):
  1. Parse oauth info from account/extraConfig
  2. If no oauth info: return empty map
  3. Get provider definition
  4. If provider has buildProxyHeaders:
     Call with {oauth: {provider, accountKey, accountId, projectId, providerData}, downstreamHeaders}
  5. Return headers map (never errors, optional method)
```

### OAuth Route Units (CRUD Only)

**IMPORTANT**: Route unit strategy execution (member selection, failover, cooldown counting) lives in TokenRouter (P7), NOT in this module. This module provides CRUD operations on route units and membership queries consumed by TokenRouter.

```
createOauthRouteUnit(accountIds[], name, strategy):
  1. Validate: at least 2 accounts, name non-empty, strategy is round_robin or stick_until_unavailable
  2. Load all accounts, validate: all exist, all same siteId, all same oauthProvider, all OAuth accounts
  3. Check uniqueness: no account in accountIds already belongs to a route unit
     - Query oauthRouteUnitMembers WHERE accountId IN accountIds
     - If any found: throw "oauth route unit accounts already grouped"
  4. In transaction:
     a. Insert oauthRouteUnits row (siteId, provider, name, strategy, enabled=true)
     b. Insert oauthRouteUnitMembers rows (unitId, accountId, sortOrder=index)
  5. Rebuild routes (rebuildRoutesOnly)
  6. On rebuild failure: rollback created route unit + retry rebuild
  7. Returns {success, routeUnit{id, siteId, provider, name, strategy, enabled, memberCount}}

updateOauthRouteUnit(routeUnitId, name?, strategy?):
  1. Validate route unit exists
  2. Update name and/or strategy, set updatedAt
  3. Invalidate token router cache
  4. Return updated routeUnit with memberCount

deleteOauthRouteUnit(routeUnitId):
  1. Validate route unit exists
  2. Snapshot current members + channels for rollback
  3. Delete channels -> members -> unit
  4. Rebuild routes
  5. On rebuild failure: full rollback (re-insert unit + members + channels) + retry rebuild
  6. Returns {success}

listEnabledOauthRouteUnitsWithMembers():
  - Returns only enabled units with their members (account + site info)
  - Members sorted by sortOrder, then id

listOauthRouteUnitsByAccountIds(accountIds):
  - Returns map[accountId -> {kind: "route_unit", id, name, strategy, memberCount}]

RouteUnitMember uniqueness: an account can only belong to ONE route unit at a time.
The DB unique constraint is enforced on oauth_route_unit_members.account_id (unique).
Handles SQLITE_CONSTRAINT, ER_DUP_ENTRY, and PG 23505 error codes for the constraint violation.
```

### Session Store

```
Session Store:
  - In-memory implementation (MemoryOAuthSessionStore) using Map<state, SessionRecord>
  - TTL: 10 minutes (SESSION_TTL_MS = 10 * 60 * 1000)
  - Expired session pruning: called on every create() and get() call (pruneExpiredSessions)
    Iterates all sessions, deletes those where expiresAt <= now
  - create(provider, redirectUri, rebindAccountId?, projectId?, proxyUrl?, useSystemProxy?):
    - Generates random 24-byte state (base64url)
    - Generates random 48-byte PKCE code_verifier (base64url)
    - Sets status="pending", createdAt=now, expiresAt=now+TTL
  - get(state): returns session or nil
  - markSuccess(state, {accountId, siteId}): sets status="success", clears error
  - markError(state, error): sets status="error", error=trimmed || "OAuth failed"
  - Interface is swappable via setOauthSessionStore() for testability

PKCE Utilities:
  - createPkceVerifier(): randomBytes(48) base64url
  - createPkceChallenge(codeVerifier): SHA256(codeVerifier) -> base64url
    Used internally by PKCE providers (codex, claude). NOT used by google-based providers (gemini-cli, antigravity).
```

### Loopback Callback Server

```
OAuthLoopbackCallbackServerState:
  {provider, attempted, ready, host, port, path, origin, redirectUri, error?}

getOAuthLoopbackCallbackServerState(provider):
  - Returns copy of stored state, or default state from provider definition

startOAuthLoopbackCallbackServer(provider, host?):
  1. Validate provider exists in registry
  2. If server already exists: return current state (idempotent)
  3. If start promise already in flight: return existing promise (prevents duplicate listeners)
  4. Create HTTP server listening on provider.loopback.host:provider.loopback.port
  5. On startup error: set state {attempted:true, ready:false, error}, reject promise
  6. On success: set state {attempted:true, ready:true}, resolve promise
  7. .finally(): clean up startPromises entry

handleCallbackRequest(provider, request, response):
  - Validate provider exists (404)
  - Validate GET method (405)
  - Validate path matches provider.loopback.path (404)
  - Parse state, code, error from query params
  - Call handleOauthCallback({provider, state, code, error})
  - On success: respond 200 HTML with "succeeded" message (auto-closes window)
  - On error: respond 500 HTML with "failed" message

stopOAuthLoopbackCallbackServers():
  - Close all active servers
  - Clear all maps (servers, states, startPromises)
  - Reset states to defaults
```

### OAuth Account Info Serialization (oauthAccount.go)

```
extraConfig.oauth structure:
  {
    provider: string         // identity (also stored as accounts.oauthProvider column)
    accountId: string        // identity (also oauthAccountKey column)
    accountKey: string       // identity
    projectId: string        // identity (also oauthProjectId column)
    email: string            // runtime
    planType: string         // runtime
    tokenExpiresAt: number   // runtime
    refreshToken: string     // runtime (encrypted at rest)
    idToken: string          // runtime (JWT for codex)
    providerData: object     // runtime (opaque provider state)
    quota: OauthQuotaSnapshot // runtime
    modelDiscoveryStatus: "healthy" | "abnormal"
    lastModelSyncAt: string
    lastModelSyncError: string
    lastDiscoveredModels: string[]
  }

Dual-source identity reads:
  - getOauthInfoFromAccount() reads from both column fields (oauthProvider, oauthAccountKey, oauthProjectId)
    AND legacy extraConfig.oauth fields, preferring column fields
  - buildOauthIdentityBackfillPatch() produces patches to backfill columns from extraConfig.oauth

StoredOauthState = OauthInfo minus {provider, accountId, accountKey, projectId}
  - The identity fields are stored as columns; runtime fields go into extraConfig.oauth
```

## Acceptance Criteria
- [ ] 4 provider OAuth flows complete: start -> callback -> persist
- [ ] Codex + Claude use PKCE S256; Gemini CLI + Antigravity use client_secret (no PKCE)
- [ ] buildAuthorizationUrl receives raw codeVerifier (NOT codeChallenge); each provider computes challenge internally if needed
- [ ] Gemini CLI multi-phase onboarding: loadCodeAssist + auto-onboard (15x/2s) + onboard (6x/5s) + API enablement
- [ ] Antigravity project discovery: loadCodeAssist + onboardUser polling (5x/2s)
- [ ] Loopback callback server on configured port, idempotent start (existing server returned, in-flight promise reused)
- [ ] Session 10 minute TTL, auto-pruned on create/get
- [ ] HandleCallback: provider mismatch guard, OAuth error callbacks handled (error param)
- [ ] Manual callback (submitOauthManualCallback) parses user-pasted URL, validates state
- [ ] Token refresh singleflight dedup, cleanup in finally() (always), NO status change on failure
- [ ] OAuth account dedup by provider+accountKey+projectId, fallback email for non-codex
- [ ] Create default status="active", update default status="disabled"
- [ ] Full rollback on model refresh failure: revert account (new=delete, existing=restore all fields + modelAvailability)
- [ ] Route unit: at least 2 accounts, same site/provider, one unit per account (unique constraint)
- [ ] Route unit: enabled field, listEnabledOauthRouteUnitsWithMembers only returns enabled
- [ ] Route unit strategy execution is in TokenRouter (P7), NOT in this module
- [ ] Quota snapshot: codex probe with rate limit headers, batch refresh (concurrency=4)
- [ ] Import: <=100 items, reject sub2api envelope, provider name normalization, ISO 8601 expiry, batch continue-on-failure
- [ ] SSH tunnel instructions generated when requestOrigin is non-loopback

## Test Plan
| File | Content |
|------|---------|
| `service/oauth/provider_test.go` | Interface compliance, registry, all 4 provider auth URL construction |
| `service/oauth/flow_test.go` | Full flow mock: startFlow + handleCallback + manual callback + error paths |
| `service/oauth/session_test.go` | PKCE generation, TTL expiry, pruning, markSuccess/markError |
| `service/oauth/refresh_test.go` | Singleflight dedup, cleanup in finally, no status change on failure |
| `service/oauth/route_unit_test.go` | CRUD, uniqueness constraint, enabled filter, member ordering |
| `service/oauth/import_test.go` | Native JSON import, sub2api rejection, ISO 8601 expiry, batch continue |
| `service/oauth/gemini_cli_test.go` | Multi-phase onboarding, project mismatch resolution, API enablement |
| `service/oauth/quota_test.go` | Probe request, header parsing, window normalization, reset hint |

## Edge Cases
- Loopback port occupied: startup fails, state.attempted=true, state.ready=false, state.error set
- Callback server startup error event: caught, server removed, state set to failed
- Duplicate callback server start: returns existing server state (idempotent)
- Callback server start while previous start in flight: returns existing promise
- OAuth callback code already used: depends on upstream provider rejecting exchange
- OAuth callback with error param: session marked error, exception thrown with error text
- Session expired (10 min): pruned on next create/get, lookup returns nil
- Provider mismatch in callback: session provider != input.provider -> throw
- Refresh token expired: upstream returns error, singleflight entry cleaned in finally, NO status change
- Concurrent refresh of same account: singleflight merges into one upstream call
- Gemini CLI: no GCP projects available -> throw specific error
- Gemini CLI: explicit projectId mismatches onboard response -> free user project uses server response, others keep explicit
- Gemini CLI: Cloud AI API already enabled (400 response) -> treated as success
- Import JSON with sub2api envelope: rejected with validation error, NOT silently skipped
- Import single item failure: aborts whole import (throws)
- Import batch item failure: continues remaining items (records as failed)
- Import expired as ISO 8601 date string: parsed via Date.parse equivalent
- SSH tunnel scenario: instructions include port forwarding commands; manual callback flow for callback URL submission
- Callback HTML response: 200 + success page on ok, 500 + failure page on error
- Codex quota probe timeout (10s): captured as error snapshot
- Codex quota header dedup within 30s window: returns stored snapshot, no re-probe
- Codex quota header dedup for in-flight same fingerprint: returns stored snapshot, no duplicate persist
- Route unit member uniqueness violation: caught at DB level, throws "accounts already grouped"
- Route unit delete rollback: full restore of unit + members + channels on route rebuild failure
- Provider config missing (client_id/client_secret): provider throws on construction, not at flow time
