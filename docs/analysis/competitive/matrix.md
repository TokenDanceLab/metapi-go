# Competitive Capability Matrix (metapi-go vs peers)

> Snapshot date: 2026-07-17  
> Milestone: [M-COMPETE](https://github.com/TokenDanceLab/metapi-go/milestone/8)  
> Sources: `docs/analysis/competitive/sources.md`  
> Status enum is **planning only** — not an implementation commitment.

```
████████████████████████████████████████████████████████████████
█  DOCS-ONLY competitive learning.                             █
█  No product code ships from this matrix until scheduled.     █
█  Issues created from this doc are [learn] backlog shells.    █
████████████████████████████████████████████████████████████████
```

## Peer positioning (one line each)

| Product | User | Core job | Shape |
| --- | --- | --- | --- |
| **metapi-go** | Operator of many AI **relay sites** | Aggregate sites/accounts/tokens, route + transform, check-in/balance/notify | Self-hosted Go gateway + embedded admin SPA |
| **all-api-hub** | End-user with many relay **accounts in browser** | Balances, check-ins, price compare, credential export, site sniffing | Browser extension (WXT/React), not a proxy server |
| **AxonHub** | App/platform developer teams | Any SDK → any model, tracing, RBAC, cost | Go gateway + GraphQL admin + embedded SPA |
| **New API** | Relay operators / multi-tenant gateway admins | Channel admin, user billing, provider adapters, dashboard | Go (Gin/GORM) + React themes; One API lineage |
| **LiteLLM** | Platform/ML teams + app developers | Unified OpenAI-format access to 100+ providers, virtual keys, budgets | Python SDK + FastAPI proxy + admin UI |

## Dimension matrix

Legend for **metapi-go status**: `strong` / `partial` / `weak` / `n/a` (wrong product shape).

| Dimension | metapi-go | all-api-hub | AxonHub | New API | LiteLLM | metapi status | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Product positioning | Relay meta-aggregator | Browser asset manager | Dev platform gateway | Multi-tenant LLM gateway | SDK + gateway | strong (own niche) | Peers overlap partially; metapi unique on multi-site relay ops |
| Architecture | Go chi/sqlx, embed SPA, SQLite+PG | Extension MV3/MV2 | Go Gin/Ent/gqlgen/FX | Go Gin/GORM, SQLite/MySQL/PG | Python FastAPI + Next UI | strong simplicity | Avoid Ent/FX/GraphQL copy unless justified |
| Protocol surface | OpenAI/Anthropic/Gemini/Responses (+ files/search/images) | N/A (client) | Broad multi-modal + rerank paths | Broad OpenAI-compat + Realtime paths | Very broad (chat/responses/embed/image/audio/batch/rerank/…) | partial | Learn select surfaces (cost/headers, TTFT), not full multi-modal sprawl |
| Routing & reliability | Weighted + Fibonacci cooldown + sticky + site concurrency | Client retries only | Composite LB, health, admission | Channel weights, multi-token, affinity concepts | Many named strategies + health poll + Redis cooldown | partial | Strong cooldown story; weaker proactive health + strategy catalog |
| Auth & multi-tenant | Admin token + downstream keys + OAuth providers | Local vault / site sessions | Projects/users/RBAC | Full users/roles/billing | Virtual keys, teams, budgets, RPM/TPM | partial | Keep operator-first; learn key budgets/limits carefully |
| Ops UX | Sites/accounts/tokens/routes, check-in, balance, notify | Best-in-class multi-account dashboard + export | GraphQL admin + tracing UI | Rich channel/user admin | Admin UI + key mgmt | strong ops automation | Learn price compare / test tools / export adapters as **server UX**, not extension |
| Observability | proxy_logs + basic Prometheus + admin stats | Heatmaps / slow requests (client) | Request tracing timelines | Spend/logs/admin analytics | 30+ integrations, cost DB, OTEL-ish exports | weak→partial | Highest-value learn cluster |
| Cost / pricing | Billing details fields; cache_ratio work in M-FEATURE | Cross-site price compare | Per-request cost tracking | Token cost estimation / pricing UI | model_prices DB + spend logs + response headers | partial | Price catalog + attribution headers are portable ideas |
| Horizontal scale | Single process primary; Redis noted as future | N/A | Multi-user server | Redis + multi-DB options | Redis dual-cache cooldown/rate limits | weak | Optional Redis is a P1 learn, not mandatory |
| What **not** to copy | — | Become an extension | Ent+gqlgen+full RBAC SaaS | Public multi-tenant wallet/Stripe first | 100+ direct providers, MCP/A2A, enterprise SSO as core | n/a | Product-shape boundaries in sources.md |

## MetAPI strengths (keep)

Evidence-backed advantages versus peers (do not dilute):

1. **Relay-site first-class model** — platforms/adapters for NewAPI-family and related sites (`platform/`), not only direct vendor APIs.
2. **Ops automation** — check-in, balance refresh, multi-channel notify, OAuth account import (Codex/Claude/Gemini/…).
3. **Operator routing realism** — channel leases, sticky sessions, Fibonacci cooldown, site max concurrency, failover isolation work from M-RELIABILITY/M-FEATURE.
4. **Protocol transform depth for coding clients** — OpenAI ↔ Anthropic ↔ Gemini ↔ Responses paths under `transform/`.
5. **Single-binary deploy** — no Node in production image; SPA embedded.

## Ranked learn backlog (deduplicated)

Priority guide:

- **P0** — operator correctness / debugability / cost truth that peers prove valuable and metapi lacks or is weak on.
- **P1** — reliability/routing/key-control improvements with clear peer patterns.
- **P2** — UX polish, optional scale, nice-to-have protocol/ops helpers.

| # | Priority | Learn title | Peers | Why it matters for metapi-go | Primary evidence themes |
| ---: | --- | --- | --- | --- | --- |
| L1 | P0 | End-to-end request trace IDs across retries/failovers | AxonHub, LiteLLM | Multi-channel retries produce uncorrelated `proxy_logs` rows; debugging production failovers is hard without a trace/session id spanning attempts | AxonHub tracing docs/pipeline; LiteLLM request correlation + callbacks |
| L2 | P0 | Per-request cost attribution (token types + optional response headers) | AxonHub, LiteLLM, New API | Operators need cache/reasoning/input/output breakdown and spend truth, not only aggregate stats | LiteLLM cost calculator + spend logs; AxonHub cost tracking; New API pricing visibility |
| L3 | P0 | Cross-site effective model price comparison (admin UX) | all-api-hub | MetAPI operators already hold multi-site catalogs; price comparison is a natural server-side dashboard job without becoming an extension | all-api-hub model price comparison feature family |
| L4 | P0 | TTFT / first-token latency signals in channel selection | AxonHub, LiteLLM | Cost-only or static weights miss slow-but-alive channels; peers score latency/TTFT | AxonHub FTTL scoring; LiteLLM latency-based routing |
| L5 | P1 | Proactive channel/model health probing (background) | AxonHub, LiteLLM | Reactive Fibonacci cooldown waits for user traffic to fail; peers probe ahead of traffic | LiteLLM health_check polling; AxonHub channel health |
| L6 | P1 | Pluggable routing strategies (least-busy / latency / cost) | LiteLLM, AxonHub | Single strategy limits operator control; peers expose named strategies | LiteLLM `RoutingStrategy`; AxonHub load-balance guides |
| L7 | P1 | Downstream-key budgets + RPM/TPM admission | LiteLLM, New API, AxonHub | Sharing a gateway among tools/users needs time-window budgets and soft admission before send | LiteLLM virtual keys; New API key rate limits; AxonHub 429/RPM concepts |
| L8 | P1 | Richer Prometheus histograms + optional OTEL/Langfuse export | LiteLLM, AxonHub | Basic counters are insufficient for SLO dashboards and external APM | LiteLLM integrations/*; AxonHub observability surface |
| L9 | P1 | Optional Redis-backed cooldown / rate-limit state | LiteLLM, New API | Multi-instance deploy currently splits cooldown/rate state | LiteLLM Redis dual cache; New API Redis usage |
| L10 | P1 | In-admin API test harness (forced channel / model matrix) | all-api-hub | Operators need one-click verify of models/keys without external tools | all-api-hub multi-dimensional API verification |
| L11 | P2 | Client/tool export adapters (Cherry Studio / CCR / etc.) | all-api-hub | Credential export reduces setup friction; implement as **server export**, not browser scrape | all-api-hub export integrations docs |
| L12 | P2 | Usage heatmaps + slow-request ranking in stats UI | all-api-hub, New API | Complements proxy_logs with operator-facing latency analytics | all-api-hub analytics; New API performance views |

### Deferred / do-not-schedule from this matrix

| Idea | Peers | Reason |
| --- | --- | --- |
| Browser extension product | all-api-hub | Wrong shape; metapi is server |
| Full multi-tenant wallet / Stripe / public top-up | New API, LiteLLM enterprise | Strategy change, not a learn slice |
| Ent ORM + gqlgen GraphQL rewrite | AxonHub | Build complexity without product win |
| 100+ direct provider SDKs as primary model | LiteLLM | Conflicts with site/account/token abstraction |
| Guardrails/MCP/A2A/video storage first | LiteLLM, AxonHub | Out of core relay mission for now |
| Privacy-invasive telemetry of prompts/keys | any | Forbidden; only aggregate opt-in metrics if ever |

## Suggested issue shells (for `gh issue create`)

Titles below are canonical `[learn]` names. Bodies live in created GitHub issues (AC / Evidence / Out of scope).

1. `[learn][P0] End-to-end request trace IDs across retries/failovers`
2. `[learn][P0] Per-request cost attribution + token-type breakdown`
3. `[learn][P0] Cross-site effective model price comparison (admin)`
4. `[learn][P0] TTFT/first-token latency signals for channel selection`
5. `[learn][P1] Background channel health probing`
6. `[learn][P1] Pluggable routing strategies (least-busy/latency/cost)`
7. `[learn][P1] Downstream-key budgets and RPM/TPM admission`
8. `[learn][P1] Richer Prometheus + optional OTEL/Langfuse export`
9. `[learn][P1] Optional Redis-backed cooldown/rate-limit state`
10. `[learn][P1] In-admin API test harness (forced channel)`
11. `[learn][P2] Client/tool credential export adapters`
12. `[learn][P2] Usage heatmaps and slow-request ranking`

## How to promote a learn item to implementation

1. Issue remains `status:backlog-only` until a program milestone schedules it (usually M-FEATURE or a dedicated reliability wave).
2. Implementation PR must add its own design notes under `docs/analysis/` or `docs/design/` and tests; this matrix is not a design SSOT.
3. Prefer progressive delivery: schema/headers first, then UI, then strategy changes that affect routing correctness.
