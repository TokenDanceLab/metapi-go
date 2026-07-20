# Residual: Sub2API Managed Auth (#194)

Last updated: 2026-07-20 (item 2 wired, not incomplete)

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
2. **Scheduler filter wired (post #246).** `scheduler/sub2api_refresh.go` now parses `extraConfig.sub2apiAuth` via `isSub2APIRefreshCandidate` and calls `balance.RefreshBalance` (→ `refreshSub2ApiManagedSessionSingleflight`) for due tokens only. See `docs/analysis/scheduler-residual-todos.md` for the updated status.
3. **Create-path managed auth.** `POST /api/accounts` stores top-level `tokenExpiresAt` as a flat extraConfig key for session mode, but does not yet write nested `extraConfig.sub2apiAuth` for sub2api imports.
4. **Due window (wired).** `IsManagedSub2ApiTokenDue` now checks `expiresAt - now <= 300s` lead window. Only tokens expiring within ~5 minutes (or already expired) trigger a refresh. Item closed.
5. **Subscription summary aggregation** from managed auth remains a stub in site service.

## Verify

```bash
go test ./handler/admin -count=1 -run Account
go test ./service -count=1 -run 'Sub2Api|Managed'
```
