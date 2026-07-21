# High-value next candidates (ours vs original)

**Date**: 2026-07-21  
**Scope**: planning inventory — **no product code in this file**.  
**Mode**: **parity program** after v0.8.45; plan SSOT [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md).  
**Ops pin**: may lag (read server `projects/metapi/STATE.md`).

> **两套问题，不要混：**  
> - **Ours** = residual / ops / engineering（[`residual-next-candidates.md`](./residual-next-candidates.md) + [`../STATE.md`](../STATE.md)）  
> - **Original** = 上游 parity（[`original-gap-matrix.md`](./original-gap-matrix.md)；sources 2026-07-16 snapshot + 2026-07-20 reverify）

## How to read

| Surface | Role |
|:--------|:-----|
| This file | **Next-wave shortlist** |
| [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md) | **Parity program plan** (ex-Electron) |
| [`residual-next-candidates.md`](./residual-next-candidates.md) | Full honesty inventory |
| [`original-gap-matrix.md`](./original-gap-matrix.md) | Evidence table (may lag code) |
| [`../progress/MASTER.md`](../progress/MASTER.md) | Open gates + schedule |

---

## A. Ours — residual / ops / engineering

| Rank | ID | Title | Status | Next | Risk if skip |
|-----:|:---|:------|:-------|:-----|:-------------|
| 1 | **WS-1** | Responses WebSocket Codex | **C1+C2+C3 present** | Full TS parity shipped (C3 Codex upstream wss flagged); sticky single-instance honesty | multi-instance pin only |
| 2 | **#547** | Per-downstream-key weight | **present** | shipped key_weight + selector + UI | — |
| 3 | **#584** | Site header override priority | **present** | shipped override flag + ApplyCustomHeadersWithOptions + Sites UI | — |
| 4 | **#579** | Multi-credential on one key | **present** | allow-list sites/credentials shipped | — |
| 5 | **#514** | Multi-tier ctx routing | **present** | estimate + tightest-fit among same-model routes | residual: tokenizer accuracy |
| 6 | **P0-585** | Channel cascade | partial | HTTP e2e present; live procedure #557 + `scripts/p0585_cascade_probe.py` | Silent cascade claim |
| 7 | **P0-555** | Billing accuracy | present-with-residual | media detail fold + orphan/missing-usage observability; multi-instance lag residual | not perfect billing |
| 8 | **UC-1** | Update-center deploy | **hide/external present** | UI ops note + 501 residual | — |
| 9 | **STICKY-B** | Redis sticky | design-only | **Deferred** — single-instance / LB pin honesty | Multi-instance multi-turn |
| 10 | **OPS-PIN** | Prod 0.8.45 | ops | Authorized pin/up + soak | Users on Exited 0.8.44 |
| 11 | **REL-RE2** | RE2 user-id | **present** v0.8.45 | Ops pin | Was Exited(2) |
| 12 | **OAUTH-REFRESH** | OAuth token scheduler | **present** #251 | — | — |
| 13 | **SUB2API-REFRESH** | Managed token due window | **present** | — | Was always-true due |

### Explicit non-goals (without reopening)

- Electron desktop  
- Fake WS frames / Hijack-silent-close  
- STICKY-B unless multi-instance product reopen  
- Invent UC registry client  
- Flip P0-585 present from unit tests alone  
- Claim perfect billing  

---

## B. Original matrix leftovers (reverified)

| Upstream# | Title | Our status | Next |
| ---: | --- | --- | --- |
| **585** | Channel cascade | partial | Prod e2e |
| **555** | Token stats | present-with-residual | Media / multi-instance |
| **579** | Multi-key binding | **present** | allow-list bind |
| **547** | Per-key weight | **present** | — |
| **584** | Header priority | **present** | — |
| **514** | Multi-tier ctx | **present** | — |
| **534** | Bulk account import | **present** (matrix stale if still missing) | Docs only |
| **520** | context_length | **present-with-residual** | Dialects residual only |
| **577** | AnyRouter check-in | partial | Live runtime |
| **571** | Codex OAuth gpt-5.5 | unknown-needs-runtime | Live probe only (static allowlist+tests present) |

Out-of-product: Electron · MySQL · k3s · noise issues.

---

## C. Recommended sequencing (authoritative)

1. Parity core **shipped**: KEYS · WS C1–C3 · #514 · UC-1 · C4 docs.  
2. **P0-585** production e2e only (partial until then).  
3. **P0-555** remains present-with-residual (media fold present; multi-instance lag).  
4. Ops pin **0.8.45** when authorized + soak.  
5. Optional: #571/#577 runtime probes; empty-DB UI shots.

---

## D. Doc ownership

| Question | Read |
|:---------|:-----|
| Production now? | [`../STATE.md`](../STATE.md) + server metapi STATE |
| Parity program? | [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md) |
| Open gates? | [`../progress/MASTER.md`](../progress/MASTER.md) |
| Residual ours? | [`residual-next-candidates.md`](./residual-next-candidates.md) |
| Matrix evidence? | [`original-gap-matrix.md`](./original-gap-matrix.md) |
| WS residual? | [`responses-websocket-residual.md`](./responses-websocket-residual.md) |
| 正式可用? | [`formal-readiness.md`](./formal-readiness.md) |
| UI 对照? | [`ui-original-parity-2026-07-20.md`](./ui-original-parity-2026-07-20.md) |
