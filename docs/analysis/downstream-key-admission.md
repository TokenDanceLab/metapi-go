# Downstream-key budgets and RPM admission (#116)

**Date:** 2026-07-17  
**Lane:** competitive learn / multi-user soft limits  
**SSOT code:** `auth/key_rpm.go`, `auth/proxy.go`, `auth/downstream.go`, `store/additive.go` (`sc2_005_downstream_rpm_limit`), `handler/admin/downstream_keys.go`  
**Peers:** LiteLLM virtual keys (budget windows, RPM/TPM); New API key rate limits; AxonHub 429 / Retry-After admission

## Goal

Add a **soft, fail-closed** requests-per-minute (RPM) admission gate on managed downstream API keys, complementary to existing lifetime quotas:

| Control | Storage | Semantics | On exceed |
|:--------|:--------|:----------|:----------|
| `max_cost` / `used_cost` | DB | Lifetime spend cap (#41) | 403 `over_cost` |
| `max_requests` / `used_requests` | DB | Lifetime request cap (#41) | 403 `over_requests` |
| **`rpm_limit`** | DB config + **in-memory** sliding window | Soft RPM admission before send | **429** + `Retry-After` |

Global `PROXY_TOKEN` is **not** subject to per-key RPM (no managed key id).

## Schema (additive)

- Column: `downstream_api_keys.rpm_limit INTEGER NULL`
- `NULL` or `0` / omitted / cleared → **unlimited** (legacy behavior)
- Positive integer → max admits per rolling **60s** window
- Migration version: `sc2_005_downstream_rpm_limit` (also present in base CREATE TABLE for fresh installs)

Admin create/update accept camelCase `rpmLimit` with the same clear/set contract as `maxRequests` (`null` / `0` / `""` → unlimited).

## Admission path

```
ProxyAuth
  extract token
  AuthorizeDownstreamToken  (enabled / expiry / max_cost / max_requests)
  KeyRPMLimiter.TryAdmit(keyID, rpm_limit)   // NEW soft gate
       │
       ├─ allow  → consumeManagedKeyRequest (lifetime used_requests++)
       │            → ProxyAuthContext → handlers
       │
       └─ deny   → 429 {"error":"API key has exceeded RPM limit"}
                    Retry-After: <seconds until oldest slot exits window>
                    (does NOT increment used_requests)
```

### Limiter design (`auth.KeyRPMLimiter`)

- Process-local sliding window (default 1 minute)
- Keyed by managed key `id`
- `limit <= 0` → unlimited, no tracking
- Concurrent-safe; exact admission under race (see tests)
- Not Redis-backed — multi-instance deploys do **not** share counters (see learn #119)

## Admin visibility

`GET /api/downstream-keys/{id}/overview` includes:

```json
"rpmAdmission": {
  "windowSeconds": 60,
  "used": 3,
  "limit": 60
}
```

`used` is this process's current window occupancy; `limit` is null when unlimited. Full SPA remaining-budget UX is out of scope for this slice.

## Out of scope

- TPM (tokens-per-minute) — needs pre-dispatch token estimation; deferred
- Budget calendar windows (daily/monthly reset) beyond lifetime maxCost/maxRequests
- Redis-shared rate state (#119)
- Full multi-tenant org/team IAM or Stripe wallets
- Large web UI redesign

## Operator notes

1. Set `rpmLimit` on a managed key via admin API create/update.
2. Clients that receive 429 should honor `Retry-After` (seconds).
3. Pair with `maxRequests` / `maxCost` for hard lifetime budgets; RPM is the soft fairness gate for shared gateways.
4. For multi-replica production, either pin sticky clients to one instance or wait for shared rate-limit state.

## Tests

- `auth/key_rpm_test.go` — allow/deny window, reset, independence, concurrent admission
- `auth/proxy_test.go` — middleware 429 + Retry-After; 429 does not burn `used_requests`; nil limit unlimited
