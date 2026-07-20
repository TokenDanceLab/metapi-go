# Token usage statistics accuracy (FE-STATS / #42 / upstream #555)

**Date**: 2026-07-17  
**Scope**: single-instance correctness of usage aggregation + admin stats reads  
**Out of scope**: multi-instance Redis coordination, cache_ratio pricing (#43), web redesign

## Summary of fixes

1. **Effective token accounting** (proxy_logs → aggregates / admin reads)
   - Prefer `total_tokens` when `> 0`.
   - Otherwise fall back to `prompt_tokens + completion_tokens`.
   - Never sum prompt+completion *on top of* total_tokens (avoids double count).
   - Applied in:
     - `scheduler/usage_aggregation.go` (`effectiveTokenCount`)
     - `handler/admin/stats.go` (`effectiveProxyTokensSQL` for dashboard / proxy-log meta)

2. **Spend resolution on projection**
   - Explicit `estimated_cost > 0` wins.
   - Otherwise fallback: Veloera → `tokens/1e6`, other platforms → `tokens/5e5` (model always `/5e5`).
   - Aligns site_day / site_hour / model_day spend with the documented TS rules.

3. **Projection batch correctness**
   - Aggregate deltas by day/hour/model key inside a batch before upsert.
   - Orphan proxy logs (no site join) still advance the watermark so they are not re-fetched forever, but do not inflate site/model aggregates.
   - `RequestRecompute` keeps the earliest pending `recompute_from_id` (min) so rewind windows do not shrink.

4. **Stats endpoints no longer stub empty charts**
   - `/api/stats/site-distribution` reads account balances + `site_day_usage` spend windowed by `days`.
   - `/api/stats/site-trend` returns `{date, sites:{[name]:{spend,calls}}}` from `site_day_usage`.
   - `/api/stats/model-by-site` filters by `days` (was ignored) and optional `siteId`.
   - Dashboard summary now exposes `proxy24h.totalTokens` with effective token math.

## Residual multi-instance limitations

| Area | Single-instance | Multi-instance (shared PostgreSQL, no Redis) |
|------|-----------------|-----------------------------------------------|
| Usage aggregation projector | Correct incremental watermark + DB lease | **SAFE**: DB CAS lease in `analytics_projection_checkpoints` prevents double projection while the lease holder is alive. Dead lease holder pauses projection up to ~10 minutes until expiry. |
| Sticky sessions / route cache / other schedulers | N/A for token totals | Still **not** multi-instance safe (in-memory sticky bindings, local route cache invalidation, non-leased schedulers). See `docs/specs/review/audits/audit-multi-instance.md`. |
| Admin stats live `SUM(proxy_logs)` paths | Correct | Safe (read-only SQL). |
| Projected tables lag | Up to one projection interval (5s) | Same, plus lease wait if another instance holds the lease. |

**Documented decision**: Redis / distributed sticky / shared cache invalidation remain out of scope for #42. Operators running multiple MetAPI instances against one database should treat usage aggregation as lease-serialized (correct but delayed on crash) and treat sticky routing as best-effort until a shared store exists.


### Orphan proxy_logs (no site join) — observability

`ProjectionPassResult.OrphanLogs` counts rows skipped for site/model buckets while still advancing the watermark. Operators can correlate log lines:

```text
usage-aggregation: orphan proxy_logs skipped site buckets orphan_logs=N processed_logs=M watermark_id=W
```

This does **not** invent a synthetic site bucket; global dashboard `SUM(proxy_logs)` paths still see those rows when reading raw logs. Perfect multi-instance billing is **not** claimed.


### Stream include_usage without tokens

When OpenAI chat/completions stream path injects `stream_options.include_usage` but the SSE ends without usable token counts, we log a warn and increment `metapi_stream_missing_usage_total`. Tokens are **never invented**.

## Tests

- `scheduler/usage_aggregation_test.go`
  - existing incremental accumulation
  - partial token field fallback
  - orphan watermark without double count
  - unit tests for effective tokens + spend fallbacks
- `handler/admin/stats_test.go`
  - proxy-log meta effective tokens (no double count)
  - dashboard `proxy24h` tokens
  - model-by-site `days` filter
  - site-trend projection shape
