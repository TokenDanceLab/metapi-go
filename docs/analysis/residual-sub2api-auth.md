# Residual: Sub2API Managed Auth (#194)

Last updated: 2026-07-17

## Scope

Merge `extraConfig.sub2apiAuth` managed auth fields (`refreshToken`, `tokenExpiresAt`) on account update and rebind-session when the site platform is `sub2api`.

## Behavior landed

| Path | Behavior |
|------|----------|
| `PUT /api/accounts/:id` | When platform is `sub2api`, merge top-level `refreshToken` / `tokenExpiresAt` and nested `extraConfig.sub2apiAuth` into stored `extraConfig.sub2apiAuth`. Valid new values overwrite; missing/invalid values preserve existing keys. Unrelated `extraConfig` keys are preserved via `MergeExtraConfig`. Nested `sub2apiAuth` is stripped from the generic extraConfig merge so partial nested patches cannot wholesale-clobber auth. |
| `POST /api/accounts/:id/rebind-session` | Same managed-auth merge for `sub2api` platforms while always setting `credentialMode=session`. Unrelated extraConfig keys remain intact. |
| Non-sub2api platforms | Top-level refresh/expiry fields are ignored for managed auth (no invented platform behavior). |

Helpers:

- `service.NormalizeManagedRefreshToken` — non-empty trimmed string
- `service.NormalizeManagedTokenExpiresAt` — positive epoch seconds (number or numeric string)
- `service.MergeSub2ApiAuth` / `service.BuildMergedSub2ApiAuth` — preserve/overwrite merge

## Residual / not in this PR

1. **No dedicated Sub2API refresh endpoint in Go yet.** Balance refresh currently falls back through `refreshSub2ApiManagedSessionSingleflight` → auto-relogin style path when managed auth is due. A true `/api/v1/auth/refresh` (or platform-specific) adapter call is still residual.
2. **Scheduler filter incomplete.** `scheduler/sub2api_refresh.go` still has a TODO to parse `extraConfig` and only refresh candidates with `refreshToken` + due `tokenExpiresAt`.
3. **Create-path managed auth.** `POST /api/accounts` stores top-level `tokenExpiresAt` as a flat extraConfig key for session mode, but does not yet write nested `extraConfig.sub2apiAuth` for sub2api imports.
4. **Due window.** `IsManagedSub2ApiTokenDue` currently returns true whenever `tokenExpiresAt` is present; a real expiry-window policy remains residual.
5. **Subscription summary aggregation** from managed auth remains a stub in site service.

## Verify

```bash
go test ./handler/admin -count=1 -run Account
go test ./service -count=1 -run 'Sub2Api|Managed'
```
