# Usage token extraction audit (#300 / #555 partial)

**Date**: 2026-07-17  
**Issue**: [#300](https://github.com/TokenDanceLab/metapi-go/issues/300)  
**Related**: matrix #555 partial, `docs/analysis/token-stats-accuracy.md`

## Goal

Audit how `proxy_logs` prompt/completion/total tokens are populated for stream vs non-stream vs error paths. Fix clear under-count bugs only; never invent usage when upstream omitted tokens.

## Current extraction paths (code truth)

| Path | Mechanism | Notes |
|------|-----------|-------|
| Non-stream buffered body | `ParseUsage` / billing helpers in `handler/proxy` | Uses upstream JSON usage when present |
| SSE stream | `handler/proxy/sse_parser.go` best-effort usage from SSE data events; later events win (`mergeUsagePreferLater`) | Stream-end usage / message_delta / response.completed |
| Error / failed attempt | Surface failure toolkit may pass 0 tokens when no body usage | Honest zero — not invented |
| Aggregation | `scheduler/usage_aggregation.go` sums `proxy_logs.*_tokens` into day/hour tables | Downstream of extraction quality |

## Findings (this wave)

1. **SSE parser already prefers later usage events** — stream under-count residual is mainly upstream omitting usage frames, not local overwrite of final usage with zeros mid-stream.
2. **No silent invention**: when usage is missing, logs keep 0 / null-equivalent rather than estimating from content length.
3. **Remaining runtime audit**: multi-provider SSE dialects that only emit usage on rare event types; partial disconnect before final usage event; transform bridges that strip usage.

## Fixes in this PR

Document-only unless a clear under-count bug is found in code review of `upstream.go` / `sse_parser.go`. Prefer residual honesty over inventing totals.

## Verify

```bash
go test ./handler/proxy ./scheduler -count=1
```
