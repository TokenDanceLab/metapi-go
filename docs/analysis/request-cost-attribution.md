# Per-request cost attribution + token-type breakdown

**Date:** 2026-07-17  
**Backlog:** [#111](https://github.com/TokenDanceLab/metapi-go/issues/111) · competitive matrix L2  
**Peers:** LiteLLM spend logs + cost headers · AxonHub real-time cost · New API pricing visibility  
**Code:** `routing/pricing_cost.go`, `handler/proxy/usage.go`, `handler/proxy/cost.go`, `handler/proxy/upstream.go`

## Goal

Persist a truthful per-request cost estimate and token-type breakdown on every
successful proxy attempt:

| Surface | Field / header | Notes |
|---------|----------------|-------|
| `proxy_logs.estimated_cost` | float | Used by usage aggregation spend |
| `proxy_logs.billing_details` | JSON (`ProxyBillingDetails`) | input/output/cache split + ratios + provenance |
| Response headers (non-stream) | `X-Metapi-Cost`, `X-Metapi-Cost-Source`, token headers | Best-effort; stream commits headers at first byte |

## Token-type extraction

`ParsedUsage` now carries optional:

- `CacheReadTokens` / `CacheCreationTokens`
- `ReasoningTokens` (operator-only; not billed separately)
- `PromptTokensIncludeCache`

Sources:

| Protocol | Cache read | Cache creation | Reasoning |
|----------|------------|----------------|-----------|
| Anthropic messages | `cache_read_input_tokens` | `cache_creation_input_tokens` / `cache_creation.*` | — |
| OpenAI chat/completions | `prompt_tokens_details.cached_tokens` | — | `completion_tokens_details.reasoning_tokens` |
| OpenAI Responses | `input_tokens_details.cached_tokens` | — | `output_tokens_details.reasoning_tokens` |
| Gemini | `cachedContentTokenCount` | — | `thoughtsTokenCount` |

Missing fields stay **0** (never invented).

## Cost policy

`routing.EstimateRequestCost` chooses:

1. **`pricing_model`** — when a `PricingModel` is supplied (catalog / override).  
   Claude missing `cache_ratio` / `cache_creation_ratio` → **0.1 / 1.25**  
   (see `docs/analysis/cache-ratio-pricing.md`). Explicit 0 is preserved.
2. **`fallback`** — no pricing model. `FallbackTokenCost(total, platform)`  
   (veloera `/1e6`, others `/5e5`). Billing details still record token breakdown;  
   **ratios are zeroed** (not silent 1.0) so operators can tell “unknown pricing”
   from “priced at input rate”.
3. **`zero`** — empty usage; cost 0 with a zero shell `billing_details`.

`billing_details.costSource` mirrors the provenance string.

## Wire path

```
upstream success
  → ParseUsageFromBody / ParseUsageFromSSEEvents
  → buildRequestCostAttribution (model currently nil → fallback)
  → writeSuccessProxyLog { EstimatedCost, BillingDetails }
  → RecordSuccess(..., cost, ...)
  → (non-stream only) X-Metapi-* headers before body write
```

Stream responses still persist cost on `proxy_logs`; headers are not rewritten
after the SSE status line is committed.

## Headers

| Header | Example |
|--------|---------|
| `X-Metapi-Cost` | `0.003` |
| `X-Metapi-Cost-Source` | `fallback` / `pricing_model` / `zero` |
| `X-Metapi-Prompt-Tokens` | `1000` |
| `X-Metapi-Completion-Tokens` | `500` |
| `X-Metapi-Total-Tokens` | `1500` |
| `X-Metapi-Cache-Read-Tokens` | present when > 0 |
| `X-Metapi-Cache-Creation-Tokens` | present when > 0 |
| `X-Metapi-Reasoning-Tokens` | present when > 0 |

## Tests

- `routing/pricing_cost_test.go` — `EstimateRequestCost` pricing / fallback / zero
- `handler/proxy/usage_test.go` — Anthropic cache + OpenAI cached/reasoning + partial
- `handler/proxy/cost_test.go` — attribution, headers, proxy log persistence

## Out of scope

- Full remote pricing catalog fetch into the hot path (model pointer is ready)
- Multi-tenant virtual key wallet / Stripe
- Web redesign of ProxyLogs UI (already displays `billingDetails` when present)
- Stream response trailer headers
