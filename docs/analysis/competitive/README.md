# Competitive Analysis Index (M-COMPETE)

Docs-first competitive learning for **metapi-go** against four peers:

| Peer | Upstream |
| --- | --- |
| all-api-hub | https://github.com/qixing-jk/all-api-hub |
| AxonHub | https://github.com/looplj/axonhub |
| New API | https://github.com/QuantumNous/new-api |
| LiteLLM | https://github.com/BerriAI/litellm |

## Documents

| File | Purpose |
| --- | --- |
| [sources.md](./sources.md) | Peer set, method, product-shape boundaries, citation rules |
| [matrix.md](./matrix.md) | Capability matrix, strengths, ranked learn backlog (L1–L12) |

Related program docs:

- `docs/progress/MASTER.md` — M-COMPETE status notes
- `docs/plan/feature-complete-roadmap.md` — where product work is scheduled after learn inventory
- `docs/analysis/original-gap-*` — original MetAPI gap inventory (different program; already closed)

## How to use

1. **Read `sources.md` first** so product-shape boundaries are clear (especially: all-api-hub is a browser client, not a gateway).
2. Use **`matrix.md`** to pick learn items. Prefer P0 before P1/P2.
3. GitHub issues created from this inventory are labeled:
   - `program:competitive-learn`
   - `type:learn`
   - `status:backlog-only`
   - `spec-driven`
   - `priority:P0` | `priority:P1` | `priority:P2`
4. Issues sit under milestone **M-COMPETE Competitive learning (allapihub/axonhub/peers)** until a later implementation milestone claims them.
5. **Do not implement from this folder alone.** A learn issue must be explicitly scheduled (new PR with design + tests) before product code changes.

## Rules

- Docs only inside this directory for the competitive inventory itself.
- Prefer public GitHub / docs URLs as evidence. Do not commit operator-local absolute checkout paths as product truth.
- Do not copy peer branding, licenses, or proprietary assets.
- Do not invent peer features without code/docs evidence.

## Refresh cadence

- Re-run peer read-only reviews when a peer ships a major routing/observability release, or when metapi-go plans a reliability/observability wave.
- Update `sources.md` snapshot date and append a short “delta” section rather than rewriting history silently.
