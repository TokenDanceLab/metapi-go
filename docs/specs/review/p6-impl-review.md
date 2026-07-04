# P6 OAuth Implementation Review

**Date**: 2026-07-04
**Spec**: `docs/specs/p6-oauth.md`
**Go impl**: `service/oauth/` (16 source + 8 test files)
**TS ref**: `metapi/src/server/services/oauth/`

## Summary

| Dimension | Status | Notes |
|-----------|--------|-------|
| File structure | PASS | All 16 source files per spec, 8/8 test files present |
| Type definitions | PASS | All types match spec exactly |
| Provider interface | PASS | All 4 providers registered, PKCE/client_secret split correct |
| Auth URL construction | PASS | PKCE (codex/claude) vs client_secret (gemini-cli/antigravity) correct |
| Gemini CLI onboarding | PASS | 5-phase flow complete (project select, loadCodeAssist, auto-onboard 15x/2s, onboard 6x/5s, API enablement) |
| Antigravity onboarding | PASS | Simplified onboarding (loadCodeAssist, onboardUser 5x/2s), no API enablement |
| Session store | PASS | PKCE, TTL 10min, pruning, swapable |
| Callback server | PASS | Idempotent start, in-flight dedup, HTML responses |
| Flow orchestration | PARTIAL | StartFlow/HandleCallback/ManualCallback ok; account persistence is a stub |
| Token refresh | PARTIAL | Singleflight dedup correct; cleanup not defer-protected |
| Import | PASS | All validations, batch/continue-on-failure, sub2api rejection, ISO 8601 |
| Route units | PARTIAL | CRUD structure ok; missing route rebuild + rollback |
| Quota | FAIL | Only type definitions; no snapshot refresh logic implemented |
| Tests | PASS | 8/8 test files matching spec test plan, solid coverage |

## Findings

### CRITICAL

**F1: activatePersistedOAuthAccount is a stub (flow.go:329-475)**

The spec requires (HandleCallback step 7):
- (c) Refresh models for account via `refreshModelsForAccount`
- (d) If model refresh fails: full rollback (save previousAccount + previousModelAvailability before mutation, restore on failure)
- (f) Rebuild routes
- (g) On route rebuild fail: rollback account + restore routes

None of these are implemented. The function only does basic upsert (ensure site, find/insert/update account) and returns. The model refresh, rollback, and route rebuild steps are entirely absent. The TS reference (`service.ts`) has this full pipeline.

**F2: Quota snapshot refresh logic not implemented (quota.go:1-37)**

The file contains type definitions only (`OauthQuotaSnapshot`, `OauthQuotaWindows`, `OauthQuotaWindowSnapshot`, `OauthSubscription`). The spec defines these functions that are completely missing:
- `refreshOauthQuotaSnapshot(accountId)` -- probe request with rate limit header parsing
- `refreshOauthConnectionQuotaBatch(accountIds)` -- concurrent batch refresh (concurrency=4)
- `refreshOauthConnectionQuota(accountId)` -- wrapper
- `recordOauthQuotaHeadersSnapshot(accountId, headers)` -- dedup by fingerprint within 30s
- `recordOauthQuotaResetHint(accountId, statusCode, errorText)` -- parse 429 reset hint

### HIGH

**F3: Missing third proxy fallback tier (flow.go:165-173)**

The spec requires three-tier proxy resolution:
```
a) session.proxyUrl (explicit per-flow)
b) session.useSystemProxy ? resolveProxyUrlFromExtraConfig({useSystemProxy: true})
c) resolveOauthProviderProxyUrl(provider) -- provider-specific default
```

The Go implementation only implements (a) and (b). The third tier `resolveOauthProviderProxyUrl(provider)` is never called. This means provider-specific default proxy URLs are not consulted. The TS reference uses `resolveOauthProviderProxyUrl` from `requestProxy.ts`.

### MEDIUM

**F4: Singleflight cleanup not defer-protected (refresh.go:42-50)**

The cleanup in `RefreshAccessTokenSingleflight` runs after `doRefreshAccessToken`:
```go
result := doRefreshAccessToken(accountID)
ch <- result
refreshInFlightMu.Lock()
delete(refreshInFlight, accountID)
refreshInFlightMu.Unlock()
```
If `doRefreshAccessToken` panics, the map entry leaks permanently. The spec says "guaranteed cleanup even on error." Should use `defer` to protect cleanup after the promise is created.

**F5: Route unit creation missing route rebuild (route_unit.go:150-275)**

`CreateOauthRouteUnit` does not call any route rebuild after successful creation. The spec says:
- "5. Rebuild routes (rebuildRoutesOnly)"
- "6. On rebuild failure: rollback created route unit + retry rebuild"

**F6: Route unit deletion missing route rebuild + rollback (route_unit.go:327-348)**

`DeleteOauthRouteUnit` deletes channels, members, and unit but:
- Does not snapshot members/channels for rollback
- Does not call route rebuild
- Has no rollback-on-rebuild-failure logic

The spec explicitly requires: "Snapshot current members + channels for rollback", "Rebuild routes", "On rebuild failure: full rollback (re-insert unit + members + channels) + retry rebuild."

### LOW

**F7: ResolveRedirectURI not treated as optional in provider struct (provider.go:148)**

The spec marks `ResolveRedirectURI` as optional: `ResolveRedirectURI func(ctx context.Context, input ResolveRedirectURIInput) (string, error) // optional`. In Go, it is declared as a non-optional struct field. None of the 4 providers populate it (it stays nil). This is functionally equivalent to "optional" at runtime (nil function = not implemented), but the struct declaration does not match the optional semantics in the spec. No behavioral impact.

**F8: Route unit update missing token router cache invalidation (route_unit.go:278-324)**

`UpdateOauthRouteUnit` does not invalidate the token router cache. The spec says: "Invalidate token router cache."

**F9: ListOauthConnections missing identity backfill (connection.go:68)**

The spec says: "Ensure OAuth identity backfill (ensureOauthIdentityBackfill)." The Go implementation does not call `BuildOauthIdentityBackfillPatch` or any equivalent backfill routine.

**F10: DeleteOauthConnection missing route rebuild (connection.go:166-185)**

The spec says: "Rebuild routes." The Go implementation only deletes the account.

**F11: UpdateOauthConnectionProxySettings missing model refresh + route rebuild (connection.go:188-235)**

The spec requires:
- "Refresh models for account (allowInactive=true)"
- "Rebuild routes"

Neither is implemented. The function only merges proxy settings into extraConfig.

**F12: Context passed as nil to provider functions (flow.go:103, flow.go:175)**

`StartFlow` calls `def.BuildAuthorizationURL(nil, ...)` and `HandleCallback` calls `def.ExchangeAuthorizationCode(nil, ...)`. While none of the current 4 providers use the context, passing `nil` context is an anti-pattern in Go (should be `context.Background()` at minimum). Not a functional bug today but violates Go conventions.

## Cross-Reference: TypeScript vs Go

| Behavior | TS Reference | Go Implementation | Match |
|----------|-------------|-------------------|-------|
| OAuth account upsert | service.ts: upsertOauthAccount with rollback | flow.go: activatePersistedOAuthAccount (stub) | PARTIAL |
| Model refresh in callback | service.ts: refreshModelsForAccount | not implemented | GAP |
| Route rebuild in callback | service.ts: routeRefreshWorkflow | not implemented | GAP |
| Rollback on model failure | service.ts: revertPersistedOauthAccount | not implemented | GAP |
| Quota probe | quota.ts: refreshOauthQuotaSnapshot | not implemented | GAP |
| Quota batch | quota.ts: refreshOauthConnectionQuotaBatch | not implemented | GAP |
| Provider proxy fallback | requestProxy.ts: resolveOauthProviderProxyUrl | not implemented | GAP |
| Scheduler | oauthRefreshScheduler.ts | not in spec scope | N/A |

## Test Coverage Assessment

All 8 test files from the spec test plan are present with solid coverage:

| Test file | Lines | Coverage summary |
|-----------|-------|-----------------|
| provider_test.go | 445 | Registry, 4 providers, auth URL PKCE/no-PKCE, site config, interface compliance |
| session_test.go | 522 | PKCE verifier/challenge, TTL 10min, expiry pruning, markSuccess/markError, swapable store |
| flow_test.go | 536 | StartFlow, HandleCallback, ManualCallback, SSH tunnels, error paths, ListOauthProviders |
| refresh_test.go | 291 | Singleflight dedup, cleanup, coalesce helpers, mergeProviderData |
| route_unit_test.go | 342 | Strategy normalization, uniquePositiveIDs, CRUD validations |
| import_test.go | 507 | Provider aliases, ISO 8601 expiry, sub2api rejection, JWT decoding, all fields |
| gemini_cli_test.go | 556 | Project extraction, tier selection, free-user detection, expiry parsing, metadata |
| quota_test.go | 300 | Snapshot types, JSON round-trip, window normalization, status values |

Note: Tests for the missing quota refresh logic (probe request, header parsing, window normalization, reset hints) and the missing account persistence logic (upsert with rollback, model refresh, route rebuild) are NOT present -- because the corresponding production logic does not exist yet.

## Acceptance Criteria Map

| Criterion | Status |
|-----------|--------|
| 4 provider OAuth flows complete: start -> callback -> persist | PARTIAL -- flow stops at basic upsert; no model refresh or route rebuild |
| Codex + Claude use PKCE S256; Gemini CLI + Antigravity use client_secret | PASS |
| buildAuthorizationUrl receives raw codeVerifier | PASS |
| Gemini CLI multi-phase onboarding | PASS |
| Antigravity project discovery | PASS |
| Loopback callback server idempotent start | PASS |
| Session 10 minute TTL, auto-pruned | PASS |
| HandleCallback: provider mismatch, OAuth error callbacks | PASS |
| Manual callback parses user-pasted URL, validates state | PASS |
| Token refresh singleflight dedup | PASS |
| Token refresh cleanup in finally() | PARTIAL -- cleanup exists but not defer-protected |
| OAuth account dedup by provider+accountKey+projectId | PASS |
| Create default active, update default disabled | PASS |
| Full rollback on model refresh failure | FAIL -- not implemented |
| Route unit: >=2 accounts, same site/provider, unique constraint | PASS |
| Route unit: enabled field, listEnabled only returns enabled | PASS |
| Route unit strategy in TokenRouter (P7) | PASS |
| Quota snapshot: codex probe, batch refresh | FAIL -- not implemented |
| Import: <=100 items, reject sub2api, ISO 8601, batch continue | PASS |
| SSH tunnel instructions for non-loopback origin | PASS |

## Verdict

The Go implementation achieves **functional correctness for the OAuth flow skeleton** but has **two critical gaps** that prevent production readiness:

1. **Account persistence pipeline is incomplete** (F1): The callback handler performs basic DB upsert but is missing model refresh, route rebuild, and the full rollback protocol required by the spec. This means successfully adding an OAuth account does not populate models or rebuild routes, making the account unusable at runtime.

2. **Quota snapshot refresh is unimplemented** (F2): Only type definitions exist. The codex probe request, rate-limit header parsing, batch refresh with concurrency control, and deduplication logic are all missing.

Additionally, proxy URL resolution is missing one fallback tier (F3), singleflight cleanup is not panic-safe (F4), and route unit operations are missing route rebuild calls (F5, F6).

The provider implementations (codex.go, claude.go, gemini_cli.go, antigravity.go), session store, callback server, flow orchestration, and import logic are well-done and closely match both the spec and the TS reference. Test coverage across all 8 test files is thorough.
