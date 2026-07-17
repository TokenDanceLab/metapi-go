# Token count coverage residual notes (FE-TOKEN / #44 / upstream #491)

**Date**: 2026-07-17  
**Scope**: production proxy path usage extraction + proxy_logs persistence  
**Related**: `token-stats-accuracy.md` (aggregation correctness), issue #42 (stats reads)

## What this wave fixed

1. **Production non-stream success** (`handler/proxy/upstream.go`)
   - Parses upstream JSON usage (OpenAI / Anthropic / Gemini / nested Responses).
   - Passes real `UsageSummary` into `DetectProxyFailure`.
   - Inserts `proxy_logs` with prompt/completion/total tokens, latency, channel/account/route ids, status `success`.

2. **Production stream success**
   - Incremental SSE analyzer extracts end-of-stream usage (chat completion chunk usage, Anthropic `message_delta`, Responses `response.completed`).
   - Persists `proxy_logs` after stream end with best-effort usage (zeros + `usage_source=unknown` when absent).

3. **Wiring**
   - `UpstreamConfig.LogProxy` injected from `app/proxy_upstream.go` via `InsertProxyLog` → `store.GetDB()`.
   - Stub path still returns zero usage (demo/tests only; not production when upstream is configured).

## Residual gaps (not in this issue)

| Gap | Notes |
| --- | --- |
| `usage_source` / `upstream_path` columns | Present on `proxy.ProxyLogEntry` but **not** in `proxy_logs` DDL yet; values are available in-process only. |
| Failure-path proxy_logs | HTTP/network failures still record router health but do not always write `proxy_logs` from `dispatchSelectedUpstream` (surface toolkit path has LogProxy on failure; production dispatch is success-focused for #44). |
| Pricing / `estimated_cost` | Still 0 on write; cache_ratio pricing is #43. |
| Multipart / non-chat surfaces | Share the same dispatch path, so usage is counted when upstream returns a parseable usage object; media endpoints that omit usage remain zero. |
| Aggregation lag | Projected stats still depend on the usage aggregation scheduler reading newly inserted rows. |
| Stream/partial under-count audit | Follow-up in `docs/analysis/stream-usage-token-audit.md` (#300 / #555 partial): Anthropic cache inclusion, SSE cache merge, Gemini thoughts, client-disconnect retention. |

## Tests

- `handler/proxy/usage_test.go` — OpenAI / Anthropic / Gemini / SSE merge
- `handler/proxy/upstream_test.go` — non-stream + stream success write non-zero tokens via LogProxy
- `handler/proxy/sse_parser` incremental analyzer usage extraction
