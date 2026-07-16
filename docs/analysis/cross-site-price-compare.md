# Cross-site effective model price comparison (#112)

**Date:** 2026-07-17  
**Backlog:** [#112](https://github.com/TokenDanceLab/metapi-go/issues/112) · competitive learn L3  
**Peers:** all-api-hub model price comparison (server-side, not extension)  
**Code:** `routing/price_compare.go`, `handler/admin/model_price_compare.go`

## Problem

Operators run many relay sites/accounts. Picking a cheaper healthy channel requires
comparing **effective** model prices across sites. all-api-hub does this in a
browser extension; metapi-go already holds multi-site catalogs and proxy cost
signals, so comparison belongs in the **admin API / SPA**, not an extension.

## API

Both paths share one handler:

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/models/price-compare` | Canonical models-namespace path |
| GET | `/api/stats/model-prices` | Alias under stats |

### Query

| Param | Default | Description |
|-------|---------|-------------|
| `model` | _(empty)_ | Model name or substring. Empty → top models by recent successful traffic (or availability). |
| `days` | `30` | Observation window for proxy_logs averages (1–365). |
| `limit` | `50` | Max rows returned (1–200). |
| `topModels` | `12` | When `model` is empty, how many models to expand (1–50). |

### Response shape

```json
{
  "model": "gpt-4o",
  "days": 30,
  "limit": 50,
  "sampleUsage": { "promptTokens": 1000, "completionTokens": 1000, "totalTokens": 2000 },
  "items": [
    {
      "siteId": 1,
      "siteName": "CheapSite",
      "platform": "openai",
      "model": "gpt-4o",
      "accountId": 10,
      "username": "ops",
      "inputPerMillion": 1.0,
      "outputPerMillion": 2.0,
      "source": "billing_details",
      "ratesSource": "billing_details",
      "estimatedCostSample": 0.003,
      "observedSamples": 12,
      "configuredUnitCost": null,
      "missingPrice": false
    }
  ],
  "meta": {
    "count": 1,
    "modelsConsidered": 1,
    "sources": ["billing_details", "observed", "configured", "fallback"],
    "notes": "..."
  }
}
```

## Provenance (no unlabeled invented prices)

`routing.BuildPriceCompareRow` resolves each candidate with explicit `source` /
`ratesSource`:

| Priority | `source` | Signal |
|---------:|----------|--------|
| 1 | `billing_details` | Rates/ratios recovered from `proxy_logs.billing_details` |
| 2 | `observed` | `AVG(estimated_cost)` over successful proxy_logs in the window |
| 3 | `configured` | `accounts.unit_cost` set by the operator |
| 4 | `fallback` | Platform `FallbackTokenCost` + default model_ratio display rates |

Rules:

1. Fallback is **always labeled** (`source=fallback`, `ratesSource=fallback`).
2. `missingPrice=true` when only fallback exists (no billing/observed/configured).
3. UI must treat `missingPrice` as an empty/missing catalog state, not “real” list price.
4. Sample cost uses 1k prompt + 1k completion tokens unless only unit/observed totals apply.

Per-million rates:

- Prefer explicit `inputPerMillion` / `outputPerMillion` from billing breakdown.
- Else derive via `CacheAwarePerMillionRates` from recovered model/completion ratios.
- Else default ratio rates with `ratesSource=fallback` (Claude cache defaults still apply only for cache fields, not base input).

## Data sources (no new tables)

| Source | Table / field |
|--------|----------------|
| Sites / platform | `sites` |
| Accounts / unit cost | `accounts.unit_cost` |
| Model presence | `model_availability` |
| Observed cost | `proxy_logs.estimated_cost` (success only) |
| Billing rates | `proxy_logs.billing_details` JSON |

No remote scrape of vendor marketing pages. No browser extension architecture.

## Admin UI

Thin section on **Models** page: model query + compare table calling
`GET /api/models/price-compare`. Empty/missing-price rows show a muted badge.

Full marketplace pricing hydration (`includePricing`) remains a separate stub/gap.

## Tests

- `routing/price_compare_test.go` — pure aggregation provenance + sort order.
- `handler/admin/model_price_compare_test.go` — SQLite fixtures for billing/observed/configured/fallback and both route aliases.

## Out of scope

- Browser extension product shape
- Automatic re-pricing of upstream vendor contracts
- Full remote `/api/pricing` catalog fetch / marketplace non-stub
- Breaking existing stats APIs
