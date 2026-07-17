# Admin model surfaces (marketplace / token-candidates / model-check)

**Date:** 2026-07-17  
**Issue:** #156  
**SSOT code:** `handler/admin/stats.go`  
**Related:** `docs/analysis/cross-site-price-compare.md`, `docs/specs/p11-admin-api.md`

## Goal

Replace empty stubs for:

| Endpoint | Purpose |
|----------|---------|
| `GET /api/models/marketplace` | Operator model plaza derived from local availability |
| `GET /api/models/token-candidates` | Route-config candidate / missing-token maps |
| `POST /api/models/check/{accountId}` | Per-account availability refresh |

with **DB-derived real data**. No unbounded upstream marketplace scrape.

## Data sources

| Surface | Tables |
|---------|--------|
| Marketplace models | `model_availability`, `token_model_availability`, `account_tokens`, `accounts`, `sites`, exact `token_routes.model_pattern`, optional 7d `proxy_logs` success-rate |
| Token candidates | `token_model_availability` ⨝ `account_tokens` ⨝ `accounts` ⨝ `sites` |
| Models without token | `model_availability` accounts that have **no** enabled token coverage for that model |
| Missing token groups | Accounts with managed tokens but no resolvable `token_group` / group-like name |
| Model check | `platform.GetAdapter(site.Platform).GetModels` → persist `model_availability` → `service.RebuildTokenRoutesFromAvailability` |

Optional filter: settings key `global_allowed_models` (JSON array). Empty / missing = allow all.

## Response contracts

### Marketplace

```json
{
  "models": [{
    "name": "gpt-4o",
    "accountCount": 1,
    "tokenCount": 1,
    "avgLatency": 320,
    "successRate": 50.0,
    "description": null,
    "tags": [],
    "supportedEndpointTypes": ["openai"],
    "pricingSources": [],
    "accounts": [{
      "id": 1,
      "site": "Demo",
      "username": "u",
      "latency": 320,
      "balance": 1.0,
      "tokens": [{ "id": 1, "name": "default", "isDefault": true }]
    }]
  }],
  "meta": {
    "refreshRequested": false,
    "refreshQueued": false,
    "refreshReused": false,
    "refreshRunning": false,
    "refreshJobId": null,
    "includePricing": true,
    "source": "db_availability",
    "pricingStatus": "unavailable",
    "pricingNote": "..."
  }
}
```

Notes:

- `pricingSources` is always empty in this wave.
- When `includePricing=1|true`, `meta.pricingStatus=unavailable` labels the residual (no fake prices).
- `refresh=true` rebuilds from DB only; it does **not** queue a remote catalog job (`refreshQueued=false`).

### Token candidates

```json
{
  "models": {
    "gpt-4o": [{
      "accountId": 1,
      "tokenId": 2,
      "tokenName": "default",
      "isDefault": true,
      "username": "u",
      "siteId": 1,
      "siteName": "Demo"
    }]
  },
  "modelsWithoutToken": {
    "claude-3-5-sonnet": [{
      "accountId": 3,
      "username": "bare",
      "siteId": 1,
      "siteName": "Demo"
    }]
  },
  "modelsMissingTokenGroups": {
    "gpt-4o-mini": [{
      "accountId": 1,
      "username": "u",
      "siteId": 1,
      "siteName": "Demo",
      "missingGroups": [],
      "requiredGroups": [],
      "availableGroups": [],
      "groupCoverageUncertain": true
    }]
  },
  "endpointTypesByModel": {
    "gpt-4o": ["openai"],
    "claude-3-5-sonnet": ["anthropic"]
  }
}
```

`modelsMissingTokenGroups` is conservative: without a remote pricing catalog of **required** enableGroups, we only flag accounts whose managed tokens have no resolvable group label (`groupCoverageUncertain=true`). Full group-vs-catalog diff remains residual.

### Model check

Success:

```json
{
  "success": true,
  "refresh": {
    "id": 1,
    "status": "success",
    "modelCount": 2,
    "models": ["gpt-4o", "gpt-4o-mini"],
    "checkedAt": "2026-07-17T00:00:00Z"
  },
  "rebuild": {
    "success": true,
    "routesConsidered": 1,
    "patternRoutes": 1,
    "groupRoutes": 0,
    "channelsInserted": 0,
    "channelsRemoved": 0,
    "channelsKept": 0
  }
}
```

Failure (never fake success):

```json
{
  "success": false,
  "message": "...",
  "refresh": {
    "id": 1,
    "status": "failed",
    "errorCode": "timeout|unauthorized|empty_models|unsupported_platform|missing_credential|not_found|upstream_error|persist_failed",
    "errorMessage": "..."
  },
  "rebuild": {}
}
```

Invalid path id still uses Pattern C: `{ "success": false, "error": "Invalid account id" }`.

## Residual: pricing refresh job

Full marketplace pricing hydration (`includePricing` two-tier cache, remote `/api/pricing` catalog, background rebuild task, 15s/90s cache keys from p11 spec) is **not** implemented here.

Operators should use:

- `GET /api/models/price-compare` — effective cross-site rates from billing_details / observed / unit_cost / labeled fallback (#112)
- Future job (out of scope for #156): bounded, cached remote pricing catalog with timeout + reuse policy

Forbidden for this residual:

- Unbounded multi-site marketplace scrapes without timeout/cache
- Inventing unlabeled prices in `pricingSources`

## Tests

SQLite fixtures in `handler/admin/stats_test.go`:

- `TestStats_SQLiteMarketplaceFromAvailability`
- `TestStats_SQLiteTokenCandidatesMaps`
- `TestStats_SQLiteModelCheckNoFakeSuccess`

## Out of scope

- `POST /api/models/probe` job queue (still stub)
- Full p11 two-tier marketplace cache + refresh job
- Remote pricing catalog scrape
- 30 req/min dedicated token-candidates limiter (global admin rate limit still applies)
