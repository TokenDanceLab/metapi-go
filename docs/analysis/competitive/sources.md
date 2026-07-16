# Competitive Learning Sources (M-COMPETE)

> Snapshot date: 2026-07-17  
> Milestone: [M-COMPETE Competitive learning](https://github.com/TokenDanceLab/metapi-go/milestone/8)  
> Scope: docs-first competitive learning only. No product code in this inventory.

## Method

1. Parallel read-only analyses of four peer products (two models each where available).
2. Cross-check claims against metapi-go package map (`docs/architecture.md`, `AGENTS.md`, handlers/services).
3. Deduplicate learnings into a capability matrix + ranked `[learn]` backlog (max 12).
4. Prefer public GitHub URLs as citations. Local competitor checkouts are operator-only references and must not appear in product docs as absolute paths.

## Peer set

| Peer | Role | Default upstream | Notes |
| --- | --- | --- | --- |
| **all-api-hub** | Browser extension / multi-account AI relay **asset manager** | [qixing-jk/all-api-hub](https://github.com/qixing-jk/all-api-hub) | Client to NewAPI-family sites. **Not** a server gateway. Docs: [all-api-hub.qixing1217.top](https://all-api-hub.qixing1217.top/en/get-started.html) |
| **AxonHub** | All-in-one AI gateway / platform | [looplj/axonhub](https://github.com/looplj/axonhub) | Any SDK → any provider transform, GraphQL admin, tracing, load balance |
| **New API** | LLM gateway + AI asset management (One API lineage) | [QuantumNous/new-api](https://github.com/QuantumNous/new-api) | Multi-tenant billing, 40+ provider channels, admin dashboard |
| **LiteLLM** | Python AI Gateway + SDK | [BerriAI/litellm](https://github.com/BerriAI/litellm) | 100+ providers, virtual keys, budgets, router strategies, observability integrations |

## MetAPI-Go baseline (this repo)

| Item | Evidence |
| --- | --- |
| Product | Self-hosted **relay-site meta-aggregator**: sites/accounts/tokens → routes/channels → multi-protocol proxy |
| Stack | Go single binary + `go:embed` React SPA; SQLite + PostgreSQL via sqlx |
| Proxy | `proxy/` conductor, channel selection, sticky session, retry/failure judge |
| Routing | `routing/` TokenRouter, weights, Fibonacci cooldown, site breaker |
| Protocols | OpenAI chat/completions/responses/embeddings/images, Anthropic messages, Gemini, files/search surfaces under `handler/proxy/` |
| Ops | Check-in, balance, notify, OAuth (Codex/Claude/Gemini/…), downstream keys |
| Architecture SSOT | `docs/architecture.md`, `docs/design/BACKEND.md` |
| Prior gap inventory | `docs/analysis/original-gap-*`, closed M-FEATURE backlog #38–#56 |

## Collection artifacts

| Artifact | Purpose |
| --- | --- |
| `docs/analysis/competitive/matrix.md` | Capability matrix + ranked learn backlog |
| `docs/analysis/competitive/README.md` | Index + usage rules for `[learn]` issues |
| GitHub milestone 8 | Tracking container for backlog-only learn issues |
| Labels | `program:competitive-learn`, `type:learn`, `status:backlog-only`, `spec-driven`, `priority:P0|P1|P2` |

## Explicit product-shape boundaries

- **Do not** turn metapi-go into a browser extension (all-api-hub shape).
- **Do not** re-home metapi-go as a multi-tenant public SaaS billing platform (new-api / LiteLLM enterprise shape) unless product strategy changes.
- **Do not** add 100+ direct provider adapters only because LiteLLM has them; metapi-go’s first-class entity is the **relay site / account / token**, not every upstream vendor SDK.
- Learn items must be **actionable inside metapi-go’s operator/self-host product**, with evidence from peers and a metapi path that would change.

## Non-goals for this milestone

- No product code, schema, or behavior changes outside `docs/**`.
- No copying of peer licenses, branding, or proprietary assets.
- No live traffic benchmarks; matrix uses static code/docs evidence only.
