# Pluggable routing strategies (#115)

**Date:** 2026-07-17  
**Lane:** competitive learn / routing  
**SSOT code:** `routing/ports.go` (`RouteRoutingStrategy`), `routing/strategies.go`, `routing/selector.go`  
**Peers:** LiteLLM `RoutingStrategy` catalog; AxonHub load-balance strategies

## Goal

Expose **named, operator-selectable** channel selection strategies beyond the historical hard-coded weighted blend, without breaking sticky sessions, exclude lists, cooldown, or Fibonacci failure isolation.

## Strategy catalog

| Name | Constant | Selection rule | Priority (P) | Random? | Default? |
|:-----|:---------|:---------------|:-------------|:--------|:---------|
| `weighted` | `StrategyWeighted` | Multi-signal contribution (weight, cost, balance, usage, health, load) then weighted random | Hard layer gate | Yes | **Yes** |
| `round_robin` | `StrategyRoundRobin` | Least-recently-selected among healthy candidates | Ignored | No | No |
| `stable_first` | `StrategyStableFirst` | Primary/observation site pools + site rotation | Site order (soft) | No | No |
| `least_busy` | `StrategyLeastBusy` | Argmin concurrency load score | Hard layer gate | No | No |
| `lowest_latency` | `StrategyLowestLatency` | Argmin TTFT/first-byte EMA, else latency EMA | Hard layer gate | No | No |
| `lowest_cost` | `StrategyLowestCost` | Argmin effective unit cost | Hard layer gate | No | No |

### Normalization

`NormalizeRouteRoutingStrategy` accepts:

- Canonical snake_case: `weighted`, `round_robin`, `stable_first`, `least_busy`, `lowest_latency`, `lowest_cost`
- Hyphen aliases: `least-busy`, `lowest-latency`, `lowest-cost`, `round-robin`, `stable-first`
- Short aliases: `latency` → `lowest_latency`, `cost` → `lowest_cost`
- Unknown / empty → **`weighted`** (documented default; preserves historical routes)

Storage field: `token_routes.routing_strategy` (per-route). There is no separate global env switch in this wave; unset routes stay weighted via DB default `'weighted'`.

## Score signals (new pure strategies)

### `least_busy`

Uses `ChannelLoadSnapshot` (same lease/concurrency telemetry already feeding weighted load multipliers):

```
score = active/limit + 2 * waiting/limit + (1 if saturated else 0)
```

Channels without session-scoped concurrency telemetry score `0` (treated idle). Lower score wins; ties break by ascending `channel.id`.

### `lowest_latency`

Reads in-memory site runtime health (#113 TTFT work):

1. Model-scoped `FirstByteEMAMs` if present
2. Model-scoped `LatencyEMAMs`
3. Site-global first-byte / latency EMA
4. Unknown → `+Inf` (sorted last so observed channels win)

### `lowest_cost`

Uses existing `EffectiveUnitCost` provenance chain:

`observed (totalCost/successCount)` → `account.unitCost` → pricing catalog → fallback unit cost.

## Shared invariants (must not break)

These apply to **all** strategies, including the new pure ones:

1. **Exclude lists** — `SelectNextChannel` / failover still remove already-tried channel IDs before strategy scoring.
2. **Sticky / preferred** — `SelectPreferredChannel` still eligibility-checks the preferred ID first; strategy only affects free selection when preferred is unavailable.
3. **Cooldown / breaker / recent-failure soft filter** — eligibility and breaker filtering run **before** strategy pick; named strategies never re-admit cooled channels.
4. **Priority layers** — `weighted`, `least_busy`, `lowest_latency`, `lowest_cost` only compete inside the lowest available priority layer (same hard-P gate as historical weighted).
5. **OAuth route units** — member selection inside a unit remains unit-local; unit presence is still eligibility-gated.

## Residual multi-instance limits

| State | Scope today | Multi-instance impact |
|:------|:------------|:----------------------|
| Runtime latency EMA / TTFT | In-process memory (+ optional settings persist) | Each replica scores from its own samples; cross-replica sharing is future work (#118) |
| Channel load snapshots | In-process leases | Least-busy is **per-process**; load is not cluster-global |
| Stable-first rotation keys | In-process maps | Rotation not shared across replicas |
| Channel cooldown / failCount | DB | Shared across instances |
| Route strategy setting | DB `routing_strategy` | Shared |

Operators running multiple replicas should treat pure latency/busy strategies as **best-effort local signals**, not cluster-perfect LB. Weighted remains the safest default when signals are sparse.

## Operator guidance

| Goal | Recommended strategy |
|:-----|:---------------------|
| Balanced multi-signal production default | `weighted` |
| Drain load from saturated session-scoped accounts | `least_busy` |
| Prefer snappy TTFT after warm traffic | `lowest_latency` |
| Strict cheapest-first (catalog/observed cost) | `lowest_cost` |
| Even exploration / testing | `round_robin` |
| Prefer proven healthy sites | `stable_first` |

## Out of scope (this issue)

- ML / bandit routers
- Copying LiteLLM LAR1 verbatim
- Frontend redesign of strategy labels (admin can already set `routingStrategy` string)
- Shared multi-instance load/latency store (#118)

## Tests

- `routing/strategies_test.go` — normalization, pure-strategy invariants, deterministic tie-break
- Existing weighted / RR / stable_first golden + algorithm tests remain authoritative for legacy strategies
