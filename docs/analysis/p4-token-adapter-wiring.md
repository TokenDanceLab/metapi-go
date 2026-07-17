# P4 Token Adapter Wiring (#182)

Last updated: 2026-07-17

## Scope

Wire admin account token create / delete / sync stubs to platform adapters:

- `handler/admin/account_tokens.go`
- `service/account_token_service.go` thin helpers
- SQLite tests with httptest fake NewAPI upstream

## Flows

### Create (upstream path)

When `POST /api/account-tokens` has no local `token` value:

1. Reject API-key connections / disabled sites / missing access token
2. Build `platform.CreateAPITokenOptions` from payload
3. `platform.GetAdapter(site.Platform).CreateAPIToken(...)`
4. Refresh local rows via `GetAPITokens` (+ `GetAPIToken` fallback) then `service.SyncTokensFromUpstream`
5. Return synced token summary

### Delete (upstream-first)

For single and batch delete:

1. Skip remote delete when token is `masked_pending` / masked value (key residual unknown)
2. Skip remote delete when site disabled, access token empty, or adapter missing
3. Otherwise call `adapter.DeleteAPIToken(url, accessToken, tokenKey, platformUserId, proxy)`
4. On remote failure: abort local delete
5. On success / skip: local `DeleteTokenByID` + default repair

Residual note: masked tokens cannot be matched remotely because only redacted key material is stored.

### Sync account / sync-all

`POST /api/account-tokens/sync/{accountId}` and `sync-all`:

1. Skip reasons: `site_disabled`, `apikey_connection`, `no_access_token`, `unsupported_platform`, `no_upstream_tokens`
2. Otherwise: `FetchUpstreamAPITokens` → `SyncTokensFromUpstream`
3. Real counts for created/updated/total
4. `sync-all wait=true` runs in process (batch size 3)
5. `sync-all wait=false` uses `StartBackgroundTask` with dedupe key `sync-all-account-tokens`

## Helpers

- `service.PlatformAPITokensToUpstream`
- `service.FetchUpstreamAPITokens`
- `service.ResolvePlatformUserIDPtr`
- `service.BuildPlatformProxyConfig` (existing)

## Verify

```bash
go test ./handler/admin ./service -count=1 -run Token
```
