# Pluggable routing strategies (#115)

## Operator-selectable values (`token_routes.routing_strategy`)

| Value | Behavior | Default |
|-------|----------|---------|
| `weighted` | Existing cost/balance/usage weighted random (default) | **yes** |
| `round_robin` | Deterministic rotation among eligible channels | |
| `stable_first` | Prefer recently healthy sites with observation rotation | |
| `least_busy` | Lowest load pressure (lease ratio when available, else historical volume) | |
| `lowest_latency` / `latency` | Lowest site runtime first-byte/total EMA (#113) | |
| `lowest_cost` / `cost` | Lowest `EffectiveUnitCost` (observed → configured → catalog → fallback) | |

Unknown values normalize to `weighted`.

## Invariants

- Sticky sessions / exclude lists / eligibility filters run **before** strategy ranking.
- Recent-failure soft filters apply to least_busy / lowest_latency / lowest_cost (same as weighted).
- Strategies are per-route (`token_routes.routing_strategy`), not global-only.

## Residual

- Multi-instance load snapshots still process-local (see #118 Redis cooldown).
- No ML bandit / LAR1 copy.
