# Claude `cache_ratio` pricing fallback

**Date:** 2026-07-17  
**Backlog:** [#43](https://github.com/TokenDanceLab/metapi-go/issues/43) · upstream cita-777/metapi **#496**  
**Code:** `routing/pricing_cost.go`

## Problem

Upstream NewAPI-family `/api/pricing` payloads (notably AnyRouter) often omit
`cache_ratio` / `cache_creation_ratio`. Original MetAPI normalized missing ratios
with `normalizeRatio(undefined, 1)` → **1.0**, so for Claude models:

| Rate | Wrong (fallback 1.0) | Correct (Anthropic) |
|------|----------------------|---------------------|
| `cacheReadPerMillion` | input × 1.0 | input × **0.1** |
| `cacheCreationPerMillion` | input × 1.0 | input × **1.25** |

That inflated estimated cost / billing_details for cache-heavy Claude traffic
and polluted cost signals used by routing weight scoring when catalog costs
are derived from the same ratios.

## Default choice (documented, intentional)

| Model family | Missing `cache_ratio` | Missing `cache_creation_ratio` |
|--------------|----------------------|--------------------------------|
| Claude (`*claude*` in name, case-insensitive) | **0.1** | **1.25** |
| All other models | **1.0** (historical MetAPI) | **1.0** |

Rationale:

1. Anthropic public prompt-cache multipliers relative to input price.
2. Matches upstream issue #496 recommended Scheme A (display/estimate layer).
3. Does **not** invent non-Claude vendor ratios; keeps prior MetAPI 1.0 fallback.
4. Explicit ratios (including **0**) always win over defaults.
5. NaN / negative values are treated as missing.

## API surface

Pure helpers in `routing` (no network, no DB):

- `ResolveCacheRatio` / `ResolveCacheCreationRatio`
- `NormalizePricingRatio` (catalog JSON field normalization)
- `CalculateModelUsageBreakdown` / `CalculateModelUsageCost`
- `BuildPricingOverrideModel` (self-log billing metadata)
- `CacheAwarePerMillionRates` (catalog / routing reference rates)
- `FallbackTokenCost` (last resort when no catalog)

## Tests

`routing/pricing_cost_test.go` covers:

- Claude missing → 0.1 / 1.25 (not equal to input $/M)
- Non-Claude missing → 1.0
- Explicit present / explicit zero / NaN
- Full cache-split cost golden (0.083057) with and without Claude fallback
- Platform fallback token divisor

## Out of scope (this change)

- Full remote pricing catalog fetch / AnyRouter shield cookie path
- Admin UI for per-site ratio overrides (upstream Scheme B)
- Wiring every proxy log write path (helpers are ready for that wave)
