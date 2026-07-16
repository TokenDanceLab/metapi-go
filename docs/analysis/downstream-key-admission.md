# Downstream-key RPM/TPM admission (#116)

## Fields (`downstream_api_keys`)

| Column | JSON | Meaning |
|--------|------|---------|
| `max_rpm` | `maxRpm` | Optional requests-per-minute soft limit (NULL = unlimited) |
| `max_tpm` | `maxTpm` | Optional tokens-per-minute soft limit (NULL = unlimited) |

Additive migration: `sc2_005_downstream_key_rate_limits`.

## Runtime gate

- Process-local sliding 60s window in `auth.KeyAdmissionLimiter`.
- Enforced in `ProxyAuth` after managed-key quota reservation.
- Over limit → **429** with `Retry-After` and clear error text (`over_rpm` / `over_tpm`).
- Existing lifetime `max_cost` / `max_requests` quotas unchanged.

## Admin visibility

List/summary rows include `windowUsedRpm` / `windowUsedTpm` from the local window.

## Residual

- ~~Multi-instance shared admission needs Redis (#118).~~ Landed: set `REDIS_URL` for shared fixed-window RPM/TPM counters (`docs/analysis/redis-shared-state.md`). Default remains process-local; Redis errors fail-open to local.
- TPM currently reserved at 0 tokens on auth (request-count RPM is primary); token estimate hook can feed later.
