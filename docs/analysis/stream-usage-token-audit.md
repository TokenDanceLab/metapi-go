# Stream / partial usage token extraction audit (#300 / upstream #555 partial)

**Date**: 2026-07-17  
**Scope**: how `proxy_logs.prompt_tokens` / `completion_tokens` / `total_tokens` are populated on stream, non-stream, partial, and error paths  
**Related**: `token-count-coverage.md` (prior production wiring), `token-stats-accuracy.md` (aggregation math)

## Population map

| Path | Where usage is extracted | Written to `proxy_logs`? | Notes |
| --- | --- | --- | --- |
| Non-stream 2xx | `ParseUsageFromBody` on full JSON body in `dispatchSelectedUpstream` | Yes via `writeSuccessProxyLog` | OpenAI / Anthropic / Gemini / nested Responses + `message.usage` |
| Stream 2xx | Incremental SSE analyzer (`sse_parser.go`) → `mergeUsagePreferLater` | Yes via `writeSuccessProxyLog` | Later usage-bearing events win; Anthropic `message_start` + `message_delta` merge |
| Stream mid-client disconnect | Analyzer keeps events already pushed; write-fail / context cancel returns `Result().Usage` when Found | Yes (success row with best-effort partial) | Does **not** invent tokens if no usage event yet |
| HTTP/network non-2xx / content failure | `recordUpstreamFailure` only | **No** (success-focused hot path) | Residual: failure rows still optional |
| Stub / missing upstream config | Zeros in stub JSON | No production path when upstream configured | Demo/tests only |

`usage_source` / `upstream_path` remain on `proxy.ProxyLogEntry` in-process; they are still **not** DDL columns on `proxy_logs` (unchanged residual).

## Clear under-count bugs fixed in this PR

1. **Anthropic cache tokens excluded from prompt/total** (matches upstream #555 report: “only input/output, not cache”)  
   - `cache_read_input_tokens` / `cache_creation_input_tokens` are exclusive of Anthropic `input_tokens`.  
   - Fix: expand `PromptTokens = input + cache_read + cache_creation`, recompute `TotalTokens` when sum exceeds reported total.  
   - OpenAI `prompt_tokens_details.cached_tokens` / Responses `input_tokens_details.cached_tokens` stay **subsets** (recorded for billing cache fields, not added again).

2. **SSE merge dropped cache fields**  
   - `mergeUsagePreferLater` previously only retained prompt/completion/total.  
   - Fix: also retain `CacheReadTokens` / `CacheCreationTokens` / `ReasoningTokens` across partial stream events (e.g. cache on `message_start`, output on `message_delta`).

3. **Gemini `thoughtsTokenCount` missing when `totalTokenCount` omitted**  
   - When total is present (usual case), thoughts are already inside total — leave completion as candidates only.  
   - When total is omitted, fold thoughts into completion before recomputing total.

4. **Client disconnect / write failure wiped already-extracted stream usage**  
   - Context cancel returned empty even if earlier SSE events had usage.  
   - Write path pushed to analyzer only **after** a successful write, so a disconnect on the final usage chunk lost tokens.  
   - Fix: `analyzer.Push` before write; on cancel/write-fail return `Result().Usage` when Found. Still zero/unknown when upstream never emitted usage.

## Already correct (no code change)

- Non-stream OpenAI / Gemini / nested Responses parsing for core fields  
- Stream final OpenAI chat.completion.chunk usage + Anthropic output-only `message_delta` merge of prompt/completion  
- Aggregation `effectiveTokenCount` (prefer total, else prompt+completion; no double-count) — see `token-stats-accuracy.md`  
- Policy: **do not invent usage** when upstream omitted token fields (`Found=false`, zeros, `usage_source=unknown`)

## Residual / unknown-needs-runtime

| Item | Why residual |
| --- | --- |
| Failure-path `proxy_logs` rows | Hot path still success-focused; failures update router health without always inserting logs. Needs product decision + schema status semantics. |
| `usage_source` / `upstream_path` DDL | In-process only until schema migration. |
| Upstream omits usage entirely (some media / tools / mid-stream kill before usage event) | Correctly stays 0; cannot recover without inventing or vendor-specific counters. |
| OpenAI `stream_options.include_usage` not forced | **Addressed for chat/completions stream (#345)**: `applyUpstreamStreamIncludeUsage` injects `include_usage=true` on OpenAI-compatible chat stream upstream bodies; skips codex/sub2api and non-chat paths. Still residual if provider ignores the flag or never emits usage. |
| Transformed multi-protocol SSE (Claude↔OpenAI etc.) | Passthrough path extracts from upstream wire format; if a transformer rewrites usage keys, needs runtime fixture against real adapters. |
| Multi-instance projection lag / lease | Aggregation correctness is separate; see `token-stats-accuracy.md`. |
| Cache fields not first-class `proxy_logs` columns | Cache lives in `billing_details` JSON + in-process `ParsedUsage`; stats `SUM(total_tokens)` now includes Anthropic cache via expanded prompt/total. |

## Tests

- `handler/proxy/usage_test.go` — Anthropic cache expansion, Gemini thoughts ± total, OpenAI cached/reasoning subset, SSE merge retains cache, no-invent  
- `handler/proxy/upstream_test.go` — client disconnect preserves usage; context cancel does not invent  
- Existing non-stream / stream success proxy_log persistence tests still apply  

```bash
go test ./handler/proxy ./scheduler -count=1
```
