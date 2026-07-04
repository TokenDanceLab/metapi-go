# P6 Cross-Reference Review: Go Spec vs TypeScript Source

**Spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p6-oauth.md`
**Reviewed TS files**: `providers.ts`, `service.ts`, `codexProvider.ts`, `claudeProvider.ts`, `geminiCliProvider.ts`, `antigravityProvider.ts`, `sessionStore.ts`, `localCallbackServer.ts`, `refreshSingleflight.ts`, `routeUnitService.ts`

---

## Accuracy Issues

### A1. Provider Interface: `BuildAuthorizationURL` missing `redirectUri` and `projectId` parameters

**Spec says** (line 49):
```go
BuildAuthorizationURL func(state, codeVerifier string) (string, error)
```

**TS reality** (`providers.ts` lines 59-64):
```ts
buildAuthorizationUrl(input: { state: string; redirectUri: string; codeVerifier: string; projectId?: string }): Promise<string>;
```

The TS interface passes `redirectUri` and optional `projectId` to the provider. The Go spec omits both. The `redirectUri` is critical because in the flow (service.ts line 725-730), the caller passes `session.redirectUri` from the session. The `projectId` is used by Gemini CLI to pin to a specific GCP project.

### A2. Provider Interface: `ExchangeAuthorizationCode` missing `state`, `redirectUri`, `projectId`

**Spec says** (line 50):
```go
ExchangeAuthorizationCode func(ctx, code, codeVerifier string, proxy *ProxyConfig) (*TokenSet, error)
```

**TS reality** (`providers.ts` lines 69-75):
```ts
exchangeAuthorizationCode(input: {
  code: string; state: string; redirectUri: string; codeVerifier: string;
  projectId?: string; proxyUrl?: string | null;
}): Promise<OAuthProviderExchangeResult>;
```

Missing from spec: `state` (validated by some providers), `redirectUri` (required by OAuth spec for code exchange), `projectId` (used by Gemini CLI for project selection during exchange). Also, the spec declares `proxy *ProxyConfig` with an unspecified struct; the TS passes `proxyUrl?: string | null` as a simple string.

### A3. Provider Interface: `RefreshAccessToken` missing `oauth` context

**Spec says** (line 51):
```go
RefreshAccessToken func(ctx, refreshToken string, proxy *ProxyConfig) (*TokenSet, error)
```

**TS reality** (`providers.ts` lines 76-83):
```ts
refreshAccessToken(input: {
  refreshToken: string;
  oauth?: { projectId?: string; providerData?: Record<string, unknown> };
  proxyUrl?: string | null;
}): Promise<OAuthProviderRefreshResult>;
```

Missing: the `oauth` sub-object with `projectId` and `providerData`. Both Gemini CLI (line 470-472) and Antigravity (line 296) use `oauth?.projectId` during refresh to retain the project binding. The spec omits this entirely.

### A4. Provider Interface: `BuildProxyHeaders` has entirely wrong signature

**Spec says** (line 52):
```go
BuildProxyHeaders func(ctx, accessToken string) (http.Header, error)
```

**TS reality** (`providers.ts` lines 84):
```ts
buildProxyHeaders?(input: OAuthProviderProxyHeaderInput): Record<string, string>;
```

Where `OAuthProviderProxyHeaderInput` is (`providers.ts` lines 33-44):
```ts
{
  oauth: { provider: string; accountKey?: string; accountId?: string; projectId?: string; providerData?: Record<string, unknown> };
  downstreamHeaders?: Record<string, unknown>;
}
```

The TS passes the full OAuth identity object (not just accessToken) plus optional downstream headers. The actual Codex implementation (codexProvider.ts lines 236-248) uses `accountId`/`accountKey` and `downstreamHeaders` -- it never uses the raw accessToken. The spec's signature would break all buildProxyHeaders implementations.

Note also: TS return type is `Record<string, string>` (never errors), not `(http.Header, error)`.

### A5. Provider Interface: Missing `resolveRedirectUri` method

**TS reality** (`providers.ts` lines 65-67):
```ts
resolveRedirectUri?(input: { requestOrigin?: string }): string;
```

The TS interface declares an optional `resolveRedirectUri` hook. The spec omits it entirely. While optional, the Go interface should reserve space for it.

### A6. Provider Interface: Missing `OAuthProviderMetadata` struct definition

The spec references `ProviderMetadata` but never defines its fields. TS defines (`providers.ts` lines 8-18):
```ts
{
  provider: OAuthProviderId;    // "codex" | "claude" | "gemini-cli" | "antigravity"
  label: string;                // "Codex" | "Claude" | "Gemini CLI" | "Antigravity"
  platform: string;
  enabled: boolean;
  loginType: 'oauth';
  requiresProjectId: boolean;   // true for gemini-cli only
  supportsDirectAccountRouting: boolean;
  supportsCloudValidation: boolean;
  supportsNativeProxy: boolean;
}
```

These fields are consumed by `listOauthProviders()` (service.ts line 681-689) to build the provider list response. The Go spec must define this struct.

### A7. Provider Interface: `ProviderSiteConfig` struct undefined

The spec references `ProviderSiteConfig` (line 47) but never defines it. TS defines (`providers.ts` lines 48-52):
```ts
site: { name: string; url: string; platform: string; }
```

### A8. Provider Interface: `LoopbackConfig` struct undefined

The spec references `LoopbackConfig` (line 48) but never defines it. TS defines (`providers.ts` lines 53-58):
```ts
loopback: { host: string; port: number; path: string; redirectUri: string; }
```

### A9. `StartFlow`: passes `codeChallenge` to BuildAuthorizationURL, but TS passes `codeVerifier`

**Spec says** (step 5, line 61): "调用 provider.BuildAuthorizationURL(state, codeChallenge)"

**TS reality** (service.ts line 725-730): `buildAuthorizationUrl({ state, redirectUri, codeVerifier })` -- passes the raw `codeVerifier`, not the `codeChallenge`. The `codeChallenge` (SHA256 digest) is computed inside each provider's `buildAuthorizationUrl` via `createPkceChallenge(codeVerifier)`. The spec has the wrong argument.

### A10. `StartFlow`: missing `rebindAccountId`, `projectId`, `proxyUrl`, `useSystemProxy` parameters

**Spec says** (line 58): `StartFlow(provider, requestOrigin)`

**TS reality** (service.ts lines 697-704): `startOauthProviderFlow({ provider, rebindAccountId?, projectId?, proxyUrl?, useSystemProxy?, requestOrigin? })`

The missing parameters are load-bearing: `rebindAccountId` enables the rebind flow; `projectId` is forwarded to providers that need project scoping; `proxyUrl`/`useSystemProxy` control proxy during the callback exchange.

### A11. `StartFlow`: step 2 "确保 loopback callback server 已启动" is misleading

**Spec says** (line 60): "确保 loopback callback server 已启动"

**TS reality** (service.ts lines 710-713): The TS checks the callback server state but does NOT start it. It throws if `attempted && !ready`:
```ts
if (callbackServerState.attempted && !callbackServerState.ready) {
  throw new Error(`${input.provider} oauth callback listener is unavailable: ...`);
}
```

The callback server is started independently via `startOAuthLoopbackCallbackServer()` (localCallbackServer.ts). The spec implies StartFlow ensures the server is running, which could mislead the Go implementer into starting it inside the flow.

### A12. `HandleCallback`: missing `error` parameter for OAuth error callbacks

**Spec says** (line 67): `HandleCallback(provider, code, state)`

**TS reality** (service.ts lines 750-755): `handleOauthCallback({ provider, state, code?, error? })`

When the OAuth provider redirects with an error parameter (user denied, etc.), TS handles it via `input.error` (lines 765-768: marks session as error and throws). The Go spec has no mechanism to handle OAuth error callbacks.

### A13. `HandleCallback`: missing provider-mismatch validation

**TS reality** (service.ts line 757): `if (!session || session.provider !== input.provider)`. The session must match the provider being called back. Spec doesn't mention this guard.

### A14. `HandleCallback`: missing manual callback URL parsing flow

**TS reality** (service.ts lines 817-841): `submitOauthManualCallback` parses a user-pasted callback URL, extracts state/code/error, and delegates to `handleOauthCallback`. This is a critical UX path for non-local environments. The spec mentions SSH tunnel hints but doesn't model the manual callback submission at all.

### A15. Token Refresh: spec says "标记账号 status=expired" on failure -- TS does NOT do this

**Spec says** (line 87): "如果刷新失败 → 标记账号 status=expired"

**TS reality** (service.ts lines 1210-1273): On refresh failure, the TS function throws without any status update. There is NO `status='expired'` assignment anywhere in the refresh path. The error propagates to the caller (singleflight wrapper in refreshSingleflight.ts). The spec describes behavior that doesn't exist in the reference implementation.

---

## Missing Details

### M1. Gemini CLI does NOT use PKCE; uses `client_secret` instead

Gemini CLI OAuth (`geminiCliProvider.ts` lines 429-440) uses Google OAuth with `client_secret`. It does NOT include `code_challenge` or `code_challenge_method` in the authorization URL. The exchange sends `client_secret` (line 447). The spec's PKCE-centric flow assumes all providers use PKCE, but Gemini CLI uses the confidential-client flow. The Go implementation needs a way for providers to opt out of PKCE or supply `client_secret` independently.

### M2. Antigravity does NOT use PKCE either

Similarly, `antigravityProvider.ts` (lines 253-263) has no `code_challenge` or `code_challenge_method` in its authorization URL. It also uses `client_secret` (line 269). Both Google-based providers use the same confidential-client pattern.

### M3. Gemini CLI multi-phase onboarding flow is entirely undocumented

After token exchange, Gemini CLI runs `ensureGeminiProjectAndOnboard` (geminiCliProvider.ts lines 388-404) which:
1. Calls `fetchGcpProjects` to list user's GCP projects (line 398)
2. Picks the first project (or uses explicitly provided projectId)
3. Calls `performGeminiCliSetup` which does:
   - `loadCodeAssist` API call (line 298)
   - Up to 15 retries of `onboardUser` polling at 2s intervals if no project (lines 314-338)
   - Another `onboardUser` phase with up to 6 retries at 5s intervals (lines 346-380)
   - Free user project detection (lines 119-128)
   - Project ID mismatch resolution (lines 367-370)
4. Calls `checkCloudAIAPIEnabled` to enable the Cloud AI Companion API (lines 242-287)

This is a complex multi-minute operation with retry logic, polling, and fallback paths. The spec describes Gemini CLI as just another PKCE provider with no special flow.

### M4. Antigravity onboarding flow is entirely undocumented

Similar to Gemini CLI but simpler (`antigravityProvider.ts` lines 182-228):
- `loadCodeAssist` API call to discover project
- Up to 5 retries of `onboardUser` polling at 2s intervals
- Different API client metadata (`ANTIGRAVITY` ideType vs Gemini's `IDE_UNSPECIFIED`)

### M5. `ExchangeAuthorizationCode` return type (`TokenSet`) fields are unspecified

The spec uses `*TokenSet` without defining it. TS `OAuthProviderExchangeResult` has 10 fields:
```ts
{ accessToken: string; refreshToken?: string; tokenExpiresAt?: number; email?: string;
  accountKey?: string; accountId?: string; planType?: string; projectId?: string;
  idToken?: string; providerData?: Record<string, unknown> }
```

The Go struct must capture all of these -- especially `accountKey`, `accountId`, `projectId`, and `providerData` which are essential for account dedup and proxy header construction.

### M6. Session store fields are incomplete

**Spec says** (line 63): `{id, provider, codeVerifier, createdAt, ...}`

**TS reality** (`sessionStore.ts` lines 5-21):
```ts
{ provider, state, status, codeVerifier, redirectUri, createdAt, updatedAt, expiresAt,
  accountId?, siteId?, error?, rebindAccountId?, projectId?, proxyUrl?, useSystemProxy? }
```

Missing from spec: `status` (pending/success/error), `redirectUri`, `updatedAt`, `expiresAt`, `rebindAccountId`, `projectId`, `proxyUrl`, `useSystemProxy`. The `redirectUri` and `projectId` are needed for the exchange step. The `proxyUrl`/`useSystemProxy` are needed for proxy resolution during callback.

### M7. `HandleCallback` proxy resolution logic is oversimplified

**Spec says** (line 69): "解析代理配置"

**TS reality** (service.ts lines 776-780) has a three-tier resolution:
```ts
const resolvedProxyUrl = session.proxyUrl
  ? session.proxyUrl                                          // explicit per-flow proxy
  : session.useSystemProxy
    ? resolveProxyUrlFromExtraConfig({ useSystemProxy: true })  // global system proxy
    : await resolveOauthProviderProxyUrl(input.provider);       // provider default
```

Each tier has different semantics. The Go spec must model this three-tier fallback.

### M8. `upsertOauthAccount` logic is significantly more complex than described

**Spec says** (lines 71-75): upsert by provider+accountKey+projectId, encrypt tokens, credentialMode="session"

**TS reality** (`service.ts` lines 585-679):
- Existing account detection via `findExistingOauthAccount` with complex multi-key lookup (accountKey first, then email for non-codex providers)
- Rebind mode: look up by `rebindAccountId`
- Existing account update: preserves status unless `persistedStatus` is explicitly set; sets status to `'disabled'` by default on update (line 643) but `'active'` on create (line 667)
- `buildOauthInfo` + `buildStoredOauthState` for structured extraConfig serialization
- Proxy settings merged into extraConfig (proxyUrl, useSystemProxy)
- Sort order allocation for new accounts

The spec oversimplifies this. The asymmetry between create (active) and update (disabled) status is particularly important.

### M9. `activatePersistedOauthAccount` rollback logic is complex

**Spec says** (line 77): "失败时 full rollback (删除已创建的账号)"

**TS reality** (`service.ts` lines 506-514): The rollback `revertPersistedOauthAccount` handles two cases:
1. **Newly created account**: delete it
2. **Updated existing account**: restore all previous fields (siteId, username, accessToken, apiToken, checkinEnabled, status, oauthProvider, oauthAccountKey, oauthProjectId, extraConfig, updatedAt) AND restore previous modelAvailability rows

The spec's "删除已创建的账号" only covers case 1. The update-rollback (case 2) is entirely undocumented.

### M10. Connection management operations are not detailed

The module structure lists `connection.go` for "连接管理 (list/delete/rebind/proxy)" but the spec body details none of:
- `listOauthConnections` (service.ts lines 843-971): 200+ lines with route unit participation, model discovery status, quota snapshots, proxy settings
- `deleteOauthConnection` (lines 973-987)
- `updateOauthConnectionProxySettings` (lines 1106-1150)
- `startOauthRebindFlow` (lines 1152-1179)

### M11. Import flow edge cases beyond sub2api rejection

**Spec says**: "从 native JSON 导入 OAuth 连接 (≤100 条, 拒绝 sub2api envelope)"

**TS reality** (`service.ts` lines 194-350) handles many more details:
- Provider name normalization: maps `openai` → `codex`, `anthropic` → `claude`, `gemini` → `gemini-cli`
- JWT id_token decoding to extract account_id, plan_type, email from `https://api.openai.com/auth` claims
- Multiple credential source fields: `access_token`, `session_token`, `refresh_token`, `id_token`
- Multiple account key sources: `chatgpt_account_id`, `account_key`, `account_id`, or JWT claims
- Expiry parsing: supports numeric Unix timestamps AND ISO 8601 date strings via `Date.parse`
- Field fallbacks: `cloudaicompanionProject` as alternative project ID source
- Disabled flag: `payload.disabled === true` maps to `persistedStatus: 'disabled'`
- Batch mode: when `items` array is provided, continues on individual item failures

### M12. Route unit strategy execution logic is not in routeUnitService.ts

The spec maps route unit strategies (round_robin, stick_until_unavailable) and cooldown management to `routeUnitService.ts`. However, TS `routeUnitService.ts` only handles CRUD operations (create, update, delete, list, query by account IDs). The actual strategy execution -- member selection, failure counting, cooldown management -- lives in the TokenRouter (not reviewed here). The spec should clarify this separation.

### M13. Route unit member uniqueness constraint

TS enforces that an account can only belong to one route unit (`oauthRouteUnitMembers` has a unique constraint on `accountId`). The `createOauthRouteUnit` function checks for existing memberships (lines 267-273) and handles the DB-level unique constraint violation (lines 58-70). The spec doesn't mention this constraint.

### M14. Route unit enabled/disabled state

TS `oauthRouteUnits` has an `enabled` boolean field. `listEnabledOauthRouteUnitsWithMembers` only returns enabled units. The spec doesn't mention route unit enable/disable.

### M15. Import batch `continueOnItemFailure` behavior

When importing via `items` array (batch mode), TS continues processing remaining items after individual failures (service.ts line 1035: `continueOnItemFailure = Array.isArray(input.items)`). When importing a single `data` object, any failure aborts. The spec doesn't describe this behavioral difference.

### M16. `resolveRedirectUri` is not in spec's Provider Interface

While `resolveRedirectUri` is optional in TS, it exists in the interface. If the Go spec's interface doesn't have it, any future provider that uses it cannot be expressed. Add it as an optional method.

---

## Edge Cases Not Covered

### E1. Gemini CLI: Project mismatch between explicit request and onboard response

TS `performGeminiCliSetup` (lines 367-370) handles the case where an explicit `requestedProjectId` doesn't match the project returned by the onboard response. If the user is a free tier user (`projectId.startsWith('gen-lang-client-')`), the server's response is used; otherwise, the explicitly requested project is kept. This subtle priority logic is not in the spec.

### E2. Gemini CLI: No GCP projects available

TS `ensureGeminiProjectAndOnboard` (lines 398-404): if `fetchGcpProjects` returns an empty list, it throws "no Google Cloud projects available for this account". The spec doesn't cover this failure mode.

### E3. Gemini CLI: Cloud AI API enablement

TS `checkCloudAIAPIEnabled` (lines 242-287) checks if `cloudaicompanion.googleapis.com` is enabled and enables it if not. It handles a 400 response with "already enabled" as success. The spec doesn't document this API enablement step.

### E4. Codex-specific: `id_token_add_organizations` and `codex_cli_simplified_flow` params

Codex authorization URL includes non-standard OAuth parameters (`prompt: 'login'`, `id_token_add_organizations: 'true'`, `codex_cli_simplified_flow: 'true'` -- codexProvider.ts lines 74-76). These are critical for getting the right token response. The spec doesn't note these provider-specific parameters.

### E5. Codex: `requireCodexClientId` throws if not configured

TS Codex provider fails fast with "CODEX_CLIENT_ID is not configured" (codexProvider.ts lines 17-22). Similarly, Claude throws for missing CLAUDE_CLIENT_ID, and Gemini CLI throws for missing CLIENT_ID/CLIENT_SECRET. The Go spec should model these provider config validation failures.

### E6. Claude: `state` is sent in exchange body

Claude's `exchangeAuthorizationCode` sends `state` to the token endpoint (claudeProvider.ts lines 137-144). Most OAuth providers don't include state in the exchange. The Go spec must preserve this provider-specific behavior.

### E7. Session creation: prune expired sessions on every create/get

TS `MemoryOAuthSessionStore` calls `pruneExpiredSessions()` on every `create` and `get` call (sessionStore.ts lines 75, 99). This is a cleanup mechanism to prevent memory leaks. The spec doesn't mention session pruning.

### E8. Loopback callback server: port-in-use deduplication

TS `startOAuthLoopbackCallbackServer` (localCallbackServer.ts lines 144-149):
- If server already exists for provider, returns existing state
- If a start promise is in flight for the provider, returns the existing promise

This prevents duplicate listeners. The spec doesn't describe this idempotency guarantee.

### E9. Loopback callback server: startup error handling

TS (localCallbackServer.ts lines 178-183) listens for `error` events during server startup, removes the listener on success, and sets a failed state. The spec doesn't describe the error-state tracking.

### E10. Manual callback: state mismatch between session and parsed URL

TS `submitOauthManualCallback` (service.ts line 829) validates `parsed.state !== input.state` and throws if they don't match. The spec doesn't model manual callback at all.

### E11. Callback HTML response: success page always returns 200, error returns 500 but page renders

TS `handleCallbackRequest` (localCallbackServer.ts lines 115-125): On success it returns 200 HTML with "succeeded" message; on catch it returns 500 HTML with "failed" message. The spec doesn't describe the HTTP response semantics of the callback server.

### E12. Singleflight: cleanup in `.finally()` not on error path

TS `refreshSingleflight.ts` line 11: `refreshInFlight.delete(accountId)` runs in `.finally()`, ensuring the map entry is always removed even on error. This prevents stuck map entries. The spec should note that cleanup must be unconditional.

### E13. Refresh on failure: error propagates, no status change

As noted in A15, on refresh failure the error propagates and the singleflight entry is cleaned up. No account status change occurs. The spec's "标记账号 status=expired" does not exist in TS.

### E14. Import: token expiry from Date.parse supports full ISO 8601

TS `parseImportedOauthExpiry` (service.ts lines 263-283) supports numeric epoch timestamps AND ISO 8601 date strings (via `Date.parse`). The spec only mentions "时间戳格式" without distinguishing these two formats.

---

## Incorrect Details

### I1. Spec says Gemini CLI uses PKCE -- it does NOT

The spec's flow description (lines 57-78) is PKCE-centric: generates code_verifier + code_challenge, passes code_challenge to authorization URL. Gemini CLI (`geminiCliProvider.ts` lines 429-440) uses Google's confidential-client OAuth flow: no PKCE challenge, includes `access_type: 'offline'` and `prompt: 'consent'`, and exchanges with `client_secret`. The Go spec must model a provider capability flag for "uses PKCE" vs "uses client_secret".

### I2. Spec says Antigravity uses PKCE -- it does NOT

Same as Gemini CLI. Antigravity (`antigravityProvider.ts` lines 253-263) also uses `client_secret` without PKCE.

### I3. Spec says `BuildAuthorizationURL` receives `codeChallenge` -- it receives `codeVerifier`

See A9. The spec says step 5 passes the SHA256 digest (`codeChallenge`). TS passes the raw `codeVerifier` and each provider computes the challenge internally.

### I4. Spec says `RefreshAccessToken` sets status=expired on failure -- TS does NOT

See A15. This behavior simply does not exist in the reference implementation.

### I5. Spec says "OAuth Route Units" section describes routing logic -- it's in TokenRouter

See M12. The spec's route unit description (lines 91-96) suggests round_robin and stick_until_unavailable with cooldown live in this subsystem. In TS, `routeUnitService.ts` is purely CRUD. The execution logic is in TokenRouter.

### I6. Spec provider lookup uses separate ID field -- TS uses metadata.provider

Spec struct has `ID string` and a separate `Metadata`. In TS, `provider` is a field inside `metadata` and the `PROVIDER_BY_ID` map (providers.ts line 94) indexes by `metadata.provider`. Having a separate `ID` field could diverge from `metadata.provider`.

### I7. Module structure lists `account.go` but spec doesn't describe it

The module structure includes `account.go` (line 34) for "OAuth 账号信息序列化到 extraConfig" but there is no functional description of this module in the spec. The TS counterpart is `oauthAccount.ts` / `codexAccount.ts` which handle structured serialization of OAuth identity into `extraConfig.oauth`.

### I8. Module structure lists `headers.go` but function signature already wrong in Provider Interface

`headers.go` (line 36) is described as "buildProxyHeaders (代理路径用的 OAuth header)" but given that `BuildProxyHeaders` has the wrong signature in the Provider Interface (A4), this module's design is inherently flawed.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 15 |
| Missing Details | 16 |
| Edge Cases Not Covered | 14 |
| Incorrect Details | 8 |

**Verdict**: **BLOCKED**

**Rationale**: The Provider Interface has **four fundamental signature errors** (A1-A4) that would make the Go implementation incompatible with the actual OAuth provider behavior. Two of four providers (Gemini CLI, Antigravity) are **wrongly assumed to use PKCE** (I1, I2) when they use confidential-client OAuth with `client_secret` instead. The Gemini CLI and Antigravity multi-phase onboarding flows (M3, M4) are entirely undocumented despite being the most complex part of those providers. The spec needs a fundamental revision of the Provider Interface before any implementation can proceed.

**Key fixes required before re-submission**:
1. Re-design Provider Interface to match all TS parameters and return types
2. Add a provider capability model: PKCE vs client_secret, onboarding required vs not, project-scoped vs not
3. Document Gemini CLI multi-phase project onboarding (loadCodeAssist + onboardUser polling + API enablement)
4. Document Antigravity project discovery and onboarding
5. Remove the incorrect "status=expired on refresh failure" claim
6. Expand session store fields to include redirectUri, projectId, proxyUrl, useSystemProxy
7. Add `submitOauthManualCallback` flow for non-local callback submission
8. Add connection management operation details (list/delete/rebind/proxy)
9. Clarify that route unit strategy execution is in TokenRouter, not routeUnitService
10. Define all struct types: ProviderMetadata, ProviderSiteConfig, LoopbackConfig, TokenSet, Session
