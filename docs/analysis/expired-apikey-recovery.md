# Expired API-key account recovery (#205)

**Date**: 2026-07-17  
**Issue**: [#205](https://github.com/TokenDanceLab/metapi-go/issues/205)  
**Spec**: `docs/specs/p3-sites-accounts.md` § Account Expired API Key Recovery (594-602)

## Problem

`PUT /api/accounts/{id}` still had a `TODO(P4)` for the TS parity path: when an
operator updates credentials on an account that is already `status=expired` and
credential-mode `apikey`, the backend should validate the new key via model
refresh (`allowInactive=true`) and only then flip the account back to `active`.

Without this, operators could write a new API key while the account stayed
`expired` forever, or a naive status flip could claim `active` without proving
the key works.

## Detection (needs recovery)

All of the following must hold on `updateAccount`:

| Condition | Source |
|-----------|--------|
| Previous status is `expired` | stored `accounts.status` |
| Next credential mode is `apikey` | `ResolveStoredCredentialMode(nextAccount)` after applying token/extraConfig projection |
| Credential changed | `accessToken` and/or `apiToken` differs from previous values |
| Next status is not forced `disabled` | request `status` (if present) |

If the request also sends `status=active`, that activation is deferred
(`preserveExpiredStatus`) until model refresh succeeds.

## Behavior

1. Persist the field updates first (tokens / extraConfig / non-status fields).
2. Call shared `refreshAccountModels(..., allowInactive=true)`.
3. **Success**:
   - set `status=active`
   - best-effort clear `extraConfig.runtimeHealth` when source is `auth`
   - response includes `modelRefresh` success payload
4. **Failure**:
   - force `status=expired` (never false-active)
   - response still 200 with updated credentials
   - honest `message` + `modelRefresh` failure payload (`errorCode` / `errorMessage`)

Disabled accounts remain rejected by the shared refresher regardless of
`allowInactive`.

## Shared helper

`handler/admin/model_refresh.go` owns:

- `refreshAccountModels(ctx, db, accountID, allowInactive)`
- `persistAccountModelAvailability`
- token / platform-user resolution and error classification

Call sites:

| Caller | `allowInactive` |
|--------|-----------------|
| `POST /api/models/check/{accountId}` (`stats.modelCheck`) | `true` (previous stats behavior only blocked `disabled`) |
| `PUT /api/accounts/{id}` recovery path | `true` |

Tests inject via package var `accountModelRefresher`.

## Residuals / honest limits

1. Recovery is **update-path only** — batch enable / health refresh / create do not auto-reactivate expired API keys.
2. Runtime-health clear is **auth-source only**; balance/checkin health entries are left alone.
3. Route rebuild after model refresh is best-effort (same as model-check); rebuild failure does not undo a successful model write or reactivation.
4. OAuth session recovery remains on the OAuth rebind / workflow hooks path, not this API-key branch.

## Tests

```bash
go test ./handler/admin -count=1 -run 'Account|Expired|Recovery|ModelRefresh'
```

Coverage:

- success: expired apikey + new token → `active`, auth runtimeHealth cleared, models persisted
- failure: expired apikey + bad token → stays `expired`, honest message, no false active
- skip: forced `disabled` status does not enter recovery
- skip: non-credential update on expired account does not refresh
- unit: `shouldRecoverExpiredAPIKey` / `credentialFieldsChanged` detection edges
