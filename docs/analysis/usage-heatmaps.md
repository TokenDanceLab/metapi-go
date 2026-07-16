# Usage heatmaps + slow-request ranking (#121)

**Date:** 2026-07-17  
**Lane:** competitive learn / observability  
**SSOT code:** `handler/admin/stats.go` (`usageHeatmap`, `slowRequests`)  
**Peers:** all-api-hub usage analytics heatmaps / slow-request analysis; New API performance views  
**Matrix:** L12

## Goal

Give operators an admin-facing density view of traffic (hour × site/model) and a ranked list of the slowest proxy requests, without shipping personal chat content or unbounded SQL scans.

## Endpoints

### `GET /api/stats/usage-heatmap`

| Query | Default | Clamp | Notes |
|:------|:--------|:------|:------|
| `days` | `7` | `[1, 31]` | Lookback window from now (UTC). |
| `dimension` | `site` | `site` \| `model` | Anything else falls back to `site`. |

**Response (camelCase):**

```json
{
  "dimension": "site",
  "days": 7,
  "since": "2026-07-10T12:00:00Z",
  "source": "site_hour_usage",
  "cellLimit": 2000,
  "count": 12,
  "cells": [
    {
      "bucket": "2026-07-17T08:00:00Z",
      "key": "3",
      "label": "prod-openai",
      "calls": 42,
      "tokens": 12000,
      "spend": 0.12
    }
  ]
}
```

| Field | Meaning |
|:------|:--------|
| `bucket` | UTC hour start (RFC3339) |
| `key` | site id (string) or model name |
| `label` | site name or model name |
| `calls` / `tokens` / `spend` | density metrics for the cell |
| `source` | `site_hour_usage` when projection hits; otherwise `proxy_logs` |
| `cellLimit` | hard max rows returned (`2000`) |

### `GET /api/stats/slow-requests`

| Query | Default | Clamp | Notes |
|:------|:--------|:------|:------|
| `limit` | `50` | `[1, 200]` | Max ranked rows. |
| `minLatencyMs` | `1000` | `[0, 3600000]` | Minimum `proxy_logs.latency_ms`. |
| `hours` | `24` | `[1, 168]` | Lookback window (max 7 days). |

**Response (camelCase):**

```json
{
  "hours": 24,
  "minLatencyMs": 1000,
  "limit": 50,
  "since": "2026-07-16T12:00:00Z",
  "count": 1,
  "items": [
    {
      "id": 99,
      "model": "gpt-4o",
      "status": "success",
      "latencyMs": 4500,
      "firstByteLatencyMs": 1200,
      "httpStatus": 200,
      "requestId": "req-abc",
      "accountId": 7,
      "siteId": 3,
      "siteName": "prod-openai",
      "createdAt": "2026-07-17T11:22:33Z"
    }
  ]
}
```

## Query bounds (AC)

| Path | Bound | How |
|:-----|:------|:----|
| Heatmap site | `LIMIT 2000` + `bucket_start_utc >= since` | Prefer projected `site_hour_usage` (already hour-bucketed). Fallback live aggregate from `proxy_logs` also uses the same `LIMIT`. |
| Heatmap model | `LIMIT 2000` + `created_at >= since` | Aggregate from `proxy_logs` only (no model_hour table). |
| Slow requests | `LIMIT <= 200` + `created_at >= since` + `latency_ms >= min` | Indexed `proxy_logs_created_at_idx`; ordered by latency DESC. |

No endpoint does a full-table scan without a time predicate and a hard `LIMIT`.

## Privacy

- Never selects request/response bodies, messages, prompts, or billing payload text.
- Heatmap cells are aggregates only.
- Slow-request ranking returns metadata + latency + request id (trace join key), not content.
- Aligns with `docs/analysis/observability-export.md` privacy rules.

## Sources of truth

| Dimension | Preferred source | Fallback |
|:----------|:-----------------|:---------|
| `site` | `site_hour_usage` projected by `scheduler/usage_aggregation.go` | Live `proxy_logs ⋈ accounts ⋈ sites` hour aggregate |
| `model` | Live `proxy_logs` hour × model aggregate | — |

Token totals reuse `effectiveProxyTokensSQL` (prefer `total_tokens`, else prompt+completion; never double-count).

## Dual dialect notes

`created_at` / `bucket_start_utc` are stored as TEXT RFC3339.

- **SQLite hour bucket:** `substr(created_at, 1, 13) || ':00:00Z'`
- **PostgreSQL hour bucket:** `to_char(date_trunc('hour', created_at::timestamptz), ...)`

Both paths go through `rebindAdminQuery` (`?` → `$n` on PG).

## Out of scope

- Real-time clickstream / product analytics
- Exporting personal user chat content into heatmaps
- Frontend chart polish (API-only MVP satisfies AC)
- New projection tables for model×hour (optional later if live aggregate cost grows)

## Tests

- `handler/admin/stats_test.go`
  - site heatmap from `site_hour_usage` fixtures + days filter
  - model heatmap from `proxy_logs` fixtures
  - slow-request ranking / threshold / hours window
  - param clamps (`days`, `limit`, `hours`, `dimension` fallback)
  - no content fields leaked
