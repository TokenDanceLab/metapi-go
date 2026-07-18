# High-value next candidates (ours vs original)

**Date**: 2026-07-18  
**Scope**: planning inventory only — **no product code**.  
**Mode**: maintenance after **v0.8.42** (prod hk3 pin `0.8.42`).

> **两套问题，不要混：**  
> - **Ours** = metapi-go 自有 residual / 运维 / 工程质量（权威：[`residual-next-candidates.md`](./residual-next-candidates.md) + [`../STATE.md`](../STATE.md)）  
> - **Original** = 上游 [cita-777/metapi](https://github.com/cita-777/metapi) issue/PR 对等（权威：[`original-gap-matrix.md`](./original-gap-matrix.md) + [`original-gap-sources.md`](./original-gap-sources.md)；sources 为 **2026-07-16 快照**）

## How to read

| Surface | Role |
|:--------|:-----|
| This file | **Next-wave shortlist** for humans / agents |
| [`residual-next-candidates.md`](./residual-next-candidates.md) | Full honesty inventory (ours) |
| [`original-gap-matrix.md`](./original-gap-matrix.md) | Upstream parity evidence table |
| [`../progress/MASTER.md`](../progress/MASTER.md) | Open gates only (empty unless ACs open a board) |
| GitHub Issues | Task SSOT **when** a wave is scheduled |

Do **not** invent WS-1 / STICKY-B / UC-1 product without dedicated ACs.

---

## A. Ours — metapi-go residual / ops / engineering

Ranked by leverage × risk of claiming “done” incorrectly.

| Rank | ID | Title | Status | Why high value | Suggested AC shape | Risk if we skip |
|-----:|:---|:------|:-------|:---------------|:-------------------|:----------------|
| 1 | **P0-585** | Channel failure cascade poison | **partial** | Production trust: one bad channel must not sink siblings; honesty tests ≠ load-proof | Multi-channel load/chaos proof AC; define whether empty-filter full-set fallback stays intentional | Silent cascade under load; false “present” claims |
| 2 | **P0-555** | Token usage / billing accuracy | **present-with-residual** | Ops + customer trust; stream/partial/media zeros | Provider-ignored flag; media zero policy; multi-instance lag bound | Wrong cost dashboards |
| 3 | **OPS-PG-BUDGET** | Azure PG role/pool budget | **present ops** (role limit 15; app pool 5/2 on hk3) | Shared Azure B1ms fleet; scheduler lease storms | Document pool vs role LIMIT; monitor `too many connections` | Startup thrash / lease failures |
| 4 | **OPS-US1-PIN** | Standby us1 image pin lag | residual ops | DR cold standby should not pin 0.6.x forever | Sync compose pin to `0.8.42` on us1 after smoke | Failover to stale binary |
| 5 | **OPS-OAUTH-CFG** | OAuth client placeholders | residual config | Warnings only until real client IDs | Provide real `CLAUDE_`/`CODEX_`/`GEMINI_CLI_` IDs if product needs OAuth login | OAuth login paths fail (expected) |
| 6 | **WS-1** | Responses WebSocket Codex | residual (501/426) | Codex desktop/interop | Protocol AC + multi-instance sticky interaction | High protocol cost; do not stub |
| 7 | **STICKY-B** | Redis sticky map | design-only | Multi-instance session affinity | Only if LB pin unavailable and sticky is product-critical | Hot-path Redis; fail-open required |
| 8 | **UC-1** | Update-center remote deploy | residual (501) | Admin “update available” honesty | Real registry client + rollback AC | Fake updateAvailable is forbidden |
| 9 | **TEST-1** | Admin stream/job harness | residual 501 | Operator DX | Optional; sync harness already present | Low |
| 10 | **REL-CRON-5F** | Config 5-field cron validate | **present** (v0.8.42) | Spurious startup warnings | Done | — |
| 11 | **REL-MIG-REQID-IDX** | proxy_logs request_id index upgrade | **present** (v0.8.41) | v0.6.5→new boots | Done | — |
| 12 | **REL-PG-POOL** | Explicit PG pool env | **present** (#526 / v0.8.40) | Shared DB budget | Done | — |

### Ours — explicit non-goals (without ACs)

- Flip P0-585 to present from unit tests alone  
- Claim perfect billing (P0-555)  
- Fake WS frames / Hijack-silent-close  
- Cluster sticky while bindings are process-local  
- Invent update-center success without registry  

---

## B. Original — cita-777/metapi parity leftovers

Sources snapshot: **2026-07-16** (115 open issues + 6 product PRs inventoried).  
Mandatory high-value set (29 numbers) was **fully inventoried**; most are **present** in metapi-go. Below = **still useful residual relative to upstream intent** (not “we ignored them”).

### B1. Mandatory high-value still not fully present

| Upstream# | Title | Our status (matrix / later residual) | Suggested next | Notes |
| ---: | --- | --- | --- | --- |
| **585** | One channel failure cascades | **partial** (P0-585) | Same as Ours #1 load-proof | Core reliability |
| **555** | Token stats inaccurate | **present-with-residual** (P0-555) | Same as Ours #2 | Billing honesty |
| **579** | Downstream key multi-key / multi-site | **partial** | Product AC: multi-credential binding vs keep route exclusions only | We have exclusions/allowed routes |
| **547** | Per-key weight | **partial** | AC: scalar weight on downstream key vs site multipliers only | Channel weight present |
| **584** | Site custom header override priority | **partial** | AC: opt-in override policy flag | Headers apply via Set; no opt-in product flag |
| **577** | AnyRouter check-in / models | **partial** | Live AnyRouter runtime proof | Adapter inherits NewAPI paths |
| **571** | Codex OAuth gpt-5.5 | **unknown-needs-runtime** | Runtime OAuth + model probe AC | Static code does not hard-block |

### B2. Matrix staleness (do not trust old partial blindly)

| Upstream# | Matrix said | Current product truth (prefer residual + code) |
| ---: | --- | --- |
| **520** | partial (no context_length) | **largely present** as CTX-520 (admin + OpenAI/Claude/Responses/Gemini enforce + UI) — further dialects only with AC |
| **590 / 594 / 591 / 578 / 496 / 538 / …** | present in matrix | Still present; use matrix as evidence, not open work |

### B3. Additional high-value original rows still backlog-shaped

| Upstream# | Title | Matrix status | Priority band |
| ---: | --- | --- | --- |
| 534 | Bulk account import | missing | P4 UX |
| 514 | Multi-tier ctx sizes → switch channels | missing | P2 routing |
| 292 | Auto priority orchestration strategy | missing | P2 routing |
| 572 | Passthrough endpoint order | partial | P2 |
| 565 | Token refresh renames default key | unknown-needs-runtime | P0 if repro |
| 515 | Global model whitelist resets to `[]` | unknown-needs-runtime | P0 if repro |
| 531 | Anthropic→OpenAI skill-call anomaly | partial | P1 runtime |
| 504 | `previous_response_id` | partial | P1 |
| 491 / 405 / 387 / 359 | Token/quota/failover UI consistency | partial | P0–P1 |

### B4. Explicit out-of-product (do not schedule as metapi-go work)

| Upstream# | Why |
| ---: | --- |
| 592, 574, 553, 552, 459 | Noise / maintenance questions |
| 575 MySQL upsert | We are SQLite/PG only |
| 595 k3s existingSecret | Deploy chart scope, not Go product core |

---

## C. Recommended sequencing (if / when ACs appear)

1. **Reliability pack**: P0-585 load-proof (+ keep honesty labels)  
2. **Billing honesty pack**: P0-555 residual slices with measurable ACs  
3. **Ops hygiene**: us1 pin sync; OAuth only if product needs login  
4. **Upstream parity pick-list**: #579 / #547 / #584 only with product ACs  
5. **Big protocol**: WS-1 / STICKY-B / UC-1 — separate milestones only  

Default without ACs: **stay in maintenance** (bugfix + deploy safety).

---

## D. Doc ownership map

| Question | Read |
|:---------|:-----|
| What is production right now? | [`../STATE.md`](../STATE.md) + server `projects/metapi/STATE.md` |
| What is open on the board? | [`../progress/MASTER.md`](../progress/MASTER.md) |
| What is still residual (ours)? | [`residual-next-candidates.md`](./residual-next-candidates.md) |
| What did we learn from original issues? | [`original-gap-sources.md`](./original-gap-sources.md) + [`original-gap-matrix.md`](./original-gap-matrix.md) |
| What should we do next? | **This file** |
| What shipped? | root `CHANGELOG.md` + GitHub Releases |

## Refresh policy

- Re-run original `gh issue list` when starting a parity wave (sources snapshot ages).  
- Prefer **merge/update** this shortlist over new parallel “roadmap” docs.  
- Absolute dates only. No invent product from residual design docs.
