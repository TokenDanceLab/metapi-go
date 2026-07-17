# Residual: OAuth state + rebind CSRF tokens (#196)

**Date**: 2026-07-17  
**Issue**: [#196](https://github.com/TokenDanceLab/metapi-go/issues/196)  
**Lane**: residual OAuth security

## Scope landed

Admin OAuth start/rebind no longer returns fixed `stub-state` / `stub-rebind` strings.

| Endpoint | Behavior |
|----------|----------|
| `POST /api/oauth/providers/{provider}/start` | `service/oauth.StartFlow` creates a cryptographically random base64url state (24 bytes) + PKCE verifier, stored in the in-memory session store with **10-minute TTL** |
| `POST /api/oauth/connections/{accountId}/rebind` | `service/oauth.StartOauthRebindFlow` → same `StartFlow` path with `RebindAccountID` |
| `GET /api/oauth/sessions/{state}` | Looks up server-stored session; **404** when missing/expired |
| `POST /api/oauth/sessions/{state}/manual-callback` | Rejects unknown state (**404**); rejects callback URL whose `state` query ≠ path state (**400 state mismatch**) |

State generation and TTL live in `service/oauth/session.go` (`MemoryOAuthSessionStore`, `sessionTTL = 10m`). Validation for manual callback lives in `service/oauth.SubmitManualCallback`.

## Residuals / honest limits

1. **In-memory session store only** — state is process-local. Multi-instance / restart loses pending flows. No Redis/shared store yet (see `docs/analysis/redis-shared-state.md` if/when multi-node is required).
2. **Provider matrix still incomplete for production credentials** — Antigravity still uses placeholder client credentials in `service/oauth/antigravity.go`; Gemini CLI / Claude / Codex require env client IDs/secrets. State CSRF is fixed independently of provider credential completeness.
3. **Admin loopback callback HTML** (`GET /api/oauth/callback/{provider}`) remains a browser close helper; real code exchange runs via provider loopback servers + `HandleCallback` / manual-callback, not this HTML route.
4. **Other OAuth admin stubs** (proxy update, delete connection, quota refresh, import, route-units mutations) are out of scope for #196 and may still be stubs.
5. **Token encryption at rest** remains an open shared gap (see `docs/specs/review/audits/audit-oauth.md`).

## Tests

`handler/admin/oauth_routes_test.go`:

- start issues non-stub, distinct crypto-random states
- getSession validates known vs unknown state
- manual-callback rejects state mismatch and unknown state
- rebind issues non-stub random state bound to an OAuth account
- unknown provider → 404

Verify:

```bash
go test ./handler/admin ./service/oauth -count=1 -run OAuth
```
