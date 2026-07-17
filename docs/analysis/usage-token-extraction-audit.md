# Usage token extraction accuracy follow-up (#311 / #555 residual)

**Date**: 2026-07-17  
**Scope**: non-stream error paths + aggregation under-count residuals after #300 disconnect partial  
**Related**: stream-usage-token-audit.md (#300), token-count-coverage.md, token-stats-accuracy.md

## Prioritized under-count gaps

| Priority | Gap | Evidence | Status in this PR |
| --- | --- | --- | --- |
| P0 | Production hot path HTTP/network/content failures never wrote proxy_logs | recordUpstreamFailure only; writeSuccessProxyLog success-only; Surface toolkit already wrote status=failed | **Fixed** via writeFailureProxyLog |
| P0 | Non-stream error bodies that still include usage dropped tokens | 429/5xx JSON with usage parsed nowhere into logs | **Fixed**: ParseUsageFromBody on non-2xx + content-detect failures |
| P1 | Content-detected failures (keyword / empty-content) discarded already-parsed usage | Detected after ParseUsageFromBody then only RecordFailure | **Fixed**: failed row keeps prompt/completion/total |
| P1 | Multi-attempt retries under-counted failed attempts in logs | Success row only after failover; failed attempts invisible to stats | **Fixed**: each terminal channel attempt that records failure also logs |
| P2 | Aggregation ignores orphan proxy_logs without site join | applyBatch skips siteID==nil (by design) | Residual (correct for site buckets; watermark still advances) |
| P2 | usage_source / upstream_path not DDL columns | In-process only | Residual (schema) |
| P2 | OpenAI stream without final usage chunk | Requires stream_options.include_usage policy | Residual (policy) |
| P3 | Pure network / timeout failures with no usage | Correctly zeros; no invent | Documented; failed row still written for call counts |

Aggregation effectiveTokenCount already prefers total_tokens else prompt+completion without double-count (token-stats-accuracy.md). No change required there for this wave.

## Chosen measurable fix (this PR)

**Persist proxy_logs on production non-stream (and terminal stream non-2xx / network) failure paths, retaining any parseable upstream usage.**

Measurable before/after:

- Before: terminal 429 body with usage.total_tokens=11 -> 0 proxy_logs rows, tokens under-counted forever.
- After: same request -> 1 status=failed row with prompt/total = 11, projected by usage aggregation into failed_calls + total_tokens.

Status string uses `failed` to match proxy.SurfaceFailureToolkit and admin stats (status <> success).

Does **not** invent tokens when upstream omitted usage (usage_source=unknown, zeros).

## Tests

```bash
go test ./handler/proxy ./scheduler -count=1 -run Usage
```

- TestNonStreamHTTPErrorPersistsUsageTokensToFailedProxyLog
- TestNonStreamContentFailurePersistsParsedUsageToFailedProxyLog
- TestDispatchUpstream_RetryThenSuccessKeepsSameRequestIDInProxyLog (failed+success rows, shared request_id)
- TestTruncateErrTextBoundsLength
- TestUsageAggregationProjectsFailedStatusTokens (#319: `status=failed` → failed_calls + total_tokens)
- Existing stream disconnect / Anthropic cache / aggregation effective-token tests remain green

## Residual honesty

Perfect billing accuracy is **not** claimed. Remaining gaps: schema metadata columns, stream_options policy, media endpoints that never emit usage, multi-instance projection lag, orphan logs without site join.

## Aggregation of failed `proxy_logs` (#319)

**Verdict: present-with-residual** (no product code gap in `scheduler/usage_aggregation.go`).

`applyBatch` is status-agnostic for tokens: every projected row adds `effectiveTokenCount(...)` to day/hour/model `total_tokens`. Call classification is exact-string only:

| `proxy_logs.status` | `success_calls` | `failed_calls` | `total_tokens` |
|---------------------|-----------------|----------------|----------------|
| `"success"` | +1 | +0 | +tokens |
| `"failed"` (#311 `writeFailureProxyLog`) | +0 | +1 | +tokens |
| other / nil (incl. legacy `"error"`) | +0 | +1 | +tokens |

Zero/unknown usage stays 0 (`effectiveTokenCount` does not invent; #311 cost only when `usage.Found`). This is **not** perfect billing — residuals above still apply (stream_options policy, media endpoints, multi-instance lag, orphans without site join).

Regression: `TestUsageAggregationProjectsFailedStatusTokens` (success + `status=failed` → `failed_calls=1`, `total_tokens=T1+T2`). Lifecycle projection seeds also use `"failed"` for the non-success row.
