# Token Groups from Upstream (#206)

Last updated: 2026-07-17

## Problem

`GET /api/account-tokens/groups/{accountId}` previously returned only local
`SELECT DISTINCT COALESCE(token_group, 'default')` from `account_tokens`.

TS `accountTokens.ts` calls `adapter.getUserGroups()` so the UI can offer
server-managed groups that may not yet exist as local token rows.

## Design

### Handler (`handler/admin/account_tokens.go`)

`getGroups` still:

1. Loads account + site
2. Rejects API-key connections (`IsAPIKeyConnection`)

Then builds proxy config via `BuildPlatformProxyConfig(h.cfg, ...)` (cfg may be
nil — already supported by `RegisterAccountTokensRoutesWithConfig`) and calls:

```text
service.GetTokenGroups(ctx, db, accountID, adapter, siteURL, accessToken, platformUserID, proxy)
```

### Service (`service/account_token_service.go`)

| Helper | Role |
|--------|------|
| `NormalizeTokenGroups` | trim + dedupe; empty → `["default"]` |
| `GetLocalTokenGroups` | local DISTINCT + normalize |
| `GetTokenGroups` | upstream-first with local fallback |

Upstream path is used only when:

- `adapter != nil`
- `accessToken` is non-empty after trim

Fallback to local when:

- adapter is nil
- access token missing
- `GetUserGroups` returns error
- upstream returns only blank strings / empty slice

Upstream errors are **not** surfaced as HTTP 5xx for groups: local rows remain a
usable picker source. Only local DB failures return 500.

## Platform coverage

`platform.PlatformAdapter.GetUserGroups` already exists:

| Platform | Behavior |
|----------|----------|
| NewAPI / AnyRouter | `/api/user/self/groups` then `/api/user_group_map` (+ cookie fallback) |
| OneAPI | `/api/user_group_map` then `/api/user/self/groups` |
| OneHub / DoneHub | `/api/user_group_map` then OneAPI |
| Sub2API | key/group endpoints + key inference; else `default` |
| Base / OpenAI-style | `["default"]` |

## Tests

```bash
go test ./service ./handler/admin -count=1 -run 'Group|Token'
```

- Service: fake `stubTokenAdapter` covers prefer-upstream, error/empty/nil
  adapter fallback, missing access token skip, default when empty.
- Handler: httptest NewAPI fake serves group endpoints; unsupported platform
  falls back to local DISTINCT groups. Existing API-key reject test retained.

## Residual

- Groups are not persisted into local rows by this endpoint (read-only picker).
- Masked / apikey connections remain out of scope for group listing.
