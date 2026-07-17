# P4 Account Verify (#183)

Last updated: 2026-07-17

## Scope

Wire `POST /api/accounts` create path (`createSingleAccount`) to platform adapters so credentials are verified before the account row is written.

## Behavior

### Credential modes

| Requested mode | Verify path | Fail closed when | Explicit skip |
|---|---|---|---|
| `auto` / `session` | `platform.VerifyToken(site.url, token, platformUserId, proxy)` | error, nil result, or `tokenType=unknown` | not available |
| `apikey` | `adapter.GetModels(...)` | error or zero models | `skipModelFetch=true` accepts without upstream call |
| batch `accessTokens` | each token uses `apikey` path | per-item failure; all-fail => HTTP 400 | `skipModelFetch=true` preserved |

### Success enrichment (best effort)

- `session`: fill username from `UserInfo` when request omitted it; copy discovered `apiToken` when missing.
- `apikey`: store secret on `api_token`, clear `access_token` (proxy-only connection), report `modelCount`.
- `credentialMode` stored as resolved type (`session` / `apikey`), not request `auto`.
- `skipModelFetch=true` stored in `extraConfig` for residual/init consumers.

### Failure response

HTTP **400**:

```json
{
  "success": false,
  "requiresVerification": true,
  "message": "Token 验证失败，请先点击“验证 Token”，验证成功后再绑定账号"
}
```

Non-apikey failures also pass through `alert.AppendSessionTokenRebindHint` for invalid access-token phrasing.

## Residual / unknown verify platforms

- Platforms without a registered adapter: create fails with `platform not supported: <name>` (no silent create).
- Platforms whose `VerifyToken` always returns `unknown` (weak defaults / no usable session or models endpoints) cannot create unless the UI uses `credentialMode=apikey` + `skipModelFetch=true` for proxy-only import.
- **Standard adapters (openai/claude/gemini/cliproxyapi)**: `BaseAdapter.VerifyToken` calls methods on the embedded `*BaseAdapter` receiver, so `GetModels` overrides on the outer adapter are not used. Auto/session create via `VerifyToken` therefore often returns `unknown`. Prefer explicit `credentialMode=apikey` (handler calls `adapter.GetModels` through the interface) or `skipModelFetch=true` for proxy-only import. Fixing VerifyToken dispatch is adapter-layer work outside #183.
- Background account init / model refresh after create remains out of scope for #183 (TS queues `account-init`; Go still returns `queued=false`).

## Tests

```bash
go test ./handler/admin -count=1 -run 'Account'
```

Coverage includes:

- successful OpenAI-style apikey create via `/v1/models`
- reject unknown/invalid token (no row written)
- session username enrichment
- apikey `skipModelFetch` no upstream call
- invalid access-token rebind hint
