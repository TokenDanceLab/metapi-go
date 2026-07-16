# Competitive matrix: metapi-go vs peers

> Snapshot: 2026-07-17 · Milestone [M-COMPETE](https://github.com/TokenDanceLab/metapi-go/milestone/8)

## Positioning

| Product | Shape | Primary user | Core job |
|---------|-------|--------------|----------|
| **metapi-go** | Go single-binary meta-gateway + embedded SPA | Relay **operator** | Aggregate many NewAPI-family sites / OAuth accounts into one key with routing, failover, admin |
| **all-api-hub** | Browser extension (WXT/React) | End-user with many site accounts | Balances, check-in, keys, price compare, export to tools — **no proxy loop** |
| **axonhub** | Go gateway (Gin/Ent/gqlgen) + React | App developers / teams | Any SDK → many **providers**, LB, cost, tracing, multi-user |
| **new-api** | Go gateway (OneAPI lineage) + React | Public/private **relay operators** with end-user billing | Channel distribution, multi-tenant billing, 40+ provider adapters |
| **litellm** | Python SDK + FastAPI proxy + Admin UI | Platform / ML enablement teams | 100+ providers, virtual keys, budgets, guardrails, observability ecosystem |

## Capability heat (qualitative)

| Dimension | metapi-go | all-api-hub | axonhub | new-api | litellm |
|-----------|-----------|------------|---------|---------|---------|
| Site/account portfolio ops | **Strong** | Strong (client) | Weak | Medium | Weak |
| Provider-channel catalog | Medium (via sites) | n/a | **Strong** | **Strong** | **Strong** |
| Protocol transforms | Strong (OpenAI/Claude/Gemini/Responses) | n/a | Strong | Strong | Strong |
| Failover / first-byte | Strong (post #38) | n/a | Strong | Medium | Strong |
| Multi-tenant billing | Downstream keys + quotas | n/a | Medium | **Strong** | **Strong** (virtual keys) |
| Observability exports | Prometheus basic | Local analytics | Tracing-oriented | Medium | **Strong** (Langfuse/OTEL/…) |
| Check-in / balance | **Strong** | **Strong** | Weak | Medium | Weak |
| Client export adapters | Weak | **Strong** | Medium | Medium | Medium |

## What metapi-go already does better

- **Meta-aggregation of NewAPI-family sites** + OAuth accounts as first-class inventory (sites/accounts/tokens/check-in).
- **Single binary + go:embed SPA** operational simplicity vs Python multi-process (LiteLLM) or heavier codegen stacks.
- **Operator control plane** aligned with Chinese relay ecosystems (platform adapters, rebuild routes, group channels).
- Recent gap pack closed reliability/protocol holes (#38–#56).

## Ranked learnings (deduped)

| Pri | Theme | Peers | Suggested direction |
|-----|-------|-------|---------------------|
| P0 | Observability integration ecosystem | axonhub, litellm, newapi | metapi has zero-dep /metrics counters only; LiteLLM ships Prometheus integration + compose scrape stack. Operators runni |
| P0 | Latency / TTFT-aware channel selection | axonhub, litellm | LiteLLM's latency-based routing uses streaming time-to-first-token to prefer healthier deployments; metapi already obser |
| P0 | Per-request cost attribution headers + spend logs | axonhub, litellm | LiteLLM attaches x-litellm-response-cost and async-writes spend logs; metapi has pricing_cost estimation and proxy logs  |
| P0 | Virtual keys with fine-grained access control | axonhub, litellm | metapi downstream keys already have max_cost/max_requests/model allowlists; LiteLLM adds teams/orgs/soft budgets/TPM-RPM |
| P0 | Cross-site effective price comparison UI | allapihub | Users choose cheaper sites; All API Hub normalizes site billing ratios against a LiteLLM price table. MetAPI routes by c |
| P0 | Health-check driven deployment cooldown | axonhub, litellm | LiteLLM pairs allowed_fails/cooldown with health endpoints and health_state_cache; metapi has Fibonacci cooldown + runti |
| P0 | Add per-request trace IDs with end-to-end correlation across retries/failovers | axonhub | Current proxy_logs have no trace correlation. When a request goes through 3 retries across different channels, there is  |
| P0 | Session/trace sticky channel selection for cache-friendly multi-turn | axonhub | AxonHub's TraceAware strategy (+1000 score) keeps the same conversation on the same upstream channel, improving provider |
| P1 | Add per-channel concurrency admission control | axonhub | AxonHub RateLimitAware strategy cools channels on 429 Retry-After (-10000) and tracks RPM/TPM/MaxConcurrent sliding wind |
| P1 | Multiple routing strategies | litellm | LiteLLM exposes explicit strategies (simple-shuffle, least-busy, usage-based, latency, cost, tag, adaptive). metapi has  |
| P1 | Redis-backed distributed state | litellm | LiteLLM DualCache (memory+Redis) backs rate limits, cooldowns, key cache, sticky affinity. metapi deliberately has no Re |
| P1 | Usage heatmaps and latency analytics | allapihub | Ops need which models/tokens are slow/expensive over time; All API Hub has local usage history sync + heatmaps/latency r |

## Do not copy

- Turning metapi-go into a **browser extension** (all-api-hub lane).
- Full multi-tenant SaaS billing clone of new-api/litellm unless product strategy changes.
- Provider-by-provider direct adapters that abandon site-meta model without a dual-mode design.

## Sources

See `sources.md`. Analyses produced by parallel fable/sonnet agents on local clones.
