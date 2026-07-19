# High-value next candidates (ours vs original)

**Date**: 2026-07-19  
**Scope**: planning inventory only — **no product code**.  
**Mode**: maintenance after **v0.8.43** / M50; **#531 pool budget** in v0.8.44 (prod pin see STATE / ops STATE).

> **两套问题，不要混：**  
> - **Ours** = metapi-go 自有 residual / 运维 / 工程质量（权威：[`residual-next-candidates.md`](./residual-next-candidates.md) + [`../STATE.md`](../STATE.md)）  
> - **Original** = 上游 [cita-777/metapi](https://github.com/cita-777/metapi) issue/PR 对等（权威：[`original-gap-matrix.md`](./original-gap-matrix.md) + [`original-gap-sources.md`](./original-gap-sources.md)；sources 为 **2026-07-16 快照**）

## How to read

| Surface | Role |
|:--------|:-----|
| This file | **Next-wave shortlist** for humans / agents |
| [`residual-next-candidates.md`](./residual-next-candidates.md) | Full honesty inventory (ours) |
| [`original-gap-matrix.md`](./original-gap-matrix.md) | Upstream parity evidence table |
| [`../progress/MASTER.md`](../progress/MASTER.md) | Open gates only |
| GitHub Issues | Task SSOT when a wave is scheduled |

Do **not** invent WS-1 / STICKY-B / UC-1 product without dedicated ACs.

---

## A. Ours — metapi-go residual / ops / engineering

| Rank | ID | Title | Status | Why high value | Suggested AC shape | Risk if we skip |
|-----:|:---|:------|:-------|:---------------|:-------------------|:----------------|
| 1 | **P0-585** | Channel failure cascade poison | **partial** (M50 unit load-proof #527) | Production trust | **Production e2e** multi-channel storm only | Silent cascade under live load |
| 2 | **P0-555** | Token usage / billing accuracy | **present-with-residual** (+ Gemini SSE #530) | Ops trust | Media zeros / multi-instance lag ACs | Wrong cost dashboards |
| 3 | **OPS-PG-BUDGET** | Azure PG role/pool budget | **present product** (#531 / v0.8.44 code; ops still size role LIMIT) | Shared Azure B1ms | Keep pool ≤ role LIMIT; `DB_PROFILE=shared-tiny` on hk3 | Connect / lease failures |
| 4 | **OPS-US1-PIN** | Standby us1 image pin | **present ops** (#528 pin 0.8.42+) | DR cold standby | Optional smoke start if authorized | Failover to stale binary |
| 5 | **OPS-OAUTH-CFG** | OAuth client placeholders | residual config | Warnings until real IDs | Real client IDs if product needs OAuth | OAuth login fails (expected) |
| 6 | **WS-1** | Responses WebSocket Codex | residual (501/426) | Codex interop | Protocol AC + multi-instance sticky | High protocol cost |
| 7 | **STICKY-B** | Redis sticky map | design-only | Multi-instance affinity | Only if LB pin unavailable | Hot-path Redis |
| 8 | **UC-1** | Update-center remote deploy | residual (501) | Admin honesty | Real registry client | Fake updateAvailable forbidden |
| 9 | **REL-CRON-5F** | Config 5-field cron validate | **present** (v0.8.42) | Startup warnings | Done | — |
| 10 | **REL-MIG-REQID-IDX** | proxy_logs request_id index | **present** (v0.8.41) | Upgrade path | Done | — |
| 11 | **REL-PG-POOL** | Explicit PG pool env | **present** (v0.8.40) | Shared DB budget | Done | — |
| 12 | **REL-P0585-LOADPROOF** | Multi-channel storm honesty | **present unit** (#527 / v0.8.43) | Load-proof honesty | Production e2e still residual | — |
| 13 | **REL-P0555-SLICE** | Gemini SSE usage honesty | **present** (#530 / v0.8.43) | Stream billing honesty | Keep present-with-residual | — |
| 14 | **REL-PG-POOL-PROFILE** | DB_PROFILE + 53300 lease backoff | **present** (#531 / v0.8.44) | Shared tiny + large dedicated both work | Ops pin profile/role LIMIT | — |
| 15 | **UI-REFRESH** | Admin UI visual language | **delivered unreleased** ([ui-ux-refresh.md](./ui-ux-refresh.md)) | FOUC/shell/forms/a11y/EmptyState/focus-trap landed | Optional UI patch release (v0.8.45+) + live shots | Unreleased aesthetic debt until pin |
| 16 | **UI-PARITY-FEEL** | 「原版按钮没了」体感 | **docs-only present** ([ui-original-parity-2026-07-20.md](./ui-original-parity-2026-07-20.md)) | 静态对照：路由/按钮齐平 | 发版 tip + 重录空库 shot；可选 VIS-1/NAV-1 | 继续被误读为功能回归 |

### Ours — explicit non-goals (without ACs)

- Flip P0-585 to present from unit tests alone  
- Claim perfect billing (P0-555)  
- Fake WS frames / Hijack-silent-close  
- Cluster sticky while bindings are process-local  
- Invent update-center success without registry  

---

## B. Original — cita-777/metapi parity leftovers

Sources snapshot: **2026-07-16**. Mandatory high-value set inventoried; most **present**. Still useful residual:

| Upstream# | Title | Our status | Suggested next |
| ---: | --- | --- | --- |
| **585** | Channel cascade | **partial** | Production e2e only |
| **555** | Token stats | **present-with-residual** | Media / multi-instance ACs |
| **579** | Multi-key / multi-site key | **partial** | Product AC |
| **547** | Per-key weight | **partial** | Product AC |
| **584** | Header override priority | **partial** | Product AC |
| **577** | AnyRouter check-in | **partial** | Live runtime |
| **571** | Codex OAuth gpt-5.5 | **unknown-needs-runtime** | Runtime probe |

Matrix staleness: **#520 context_length** → product later shipped **CTX-520**.

Out-of-product: #592/#574/#553/#552/#459 noise; #575 MySQL; #595 k3s chart.

---

## C. Recommended sequencing

1. Stay **maintenance** unless production e2e AC for P0-585.  
2. Ops: keep pool ≤ Azure role LIMIT (read server STATE).  
3. Original parity #579/#547/#584 only with product ACs.  
4. WS-1 / STICKY-B / UC-1 — separate milestones only.
5. **UI-REFRESH** — tip 已含 first-run；优先 **UI patch release** + 空库 shot 重录，别再当「功能缺失」开 issue。

---

## D. Doc ownership map

| Question | Read |
|:---------|:-----|
| Production now? | [`../STATE.md`](../STATE.md) + server `projects/metapi/STATE.md` |
| Open board? | [`../progress/MASTER.md`](../progress/MASTER.md) |
| Residual (ours)? | [`residual-next-candidates.md`](./residual-next-candidates.md) |
| Original parity? | [`original-gap-matrix.md`](./original-gap-matrix.md) |
| Next? | **This file** |
| 正式可用？ | [`formal-readiness.md`](./formal-readiness.md) |
| UI 重构？ | [`ui-ux-refresh.md`](./ui-ux-refresh.md) |
| 原版功能/按钮对照？ | [`ui-original-parity-2026-07-20.md`](./ui-original-parity-2026-07-20.md) |
| Shipped? | `CHANGELOG.md` + Releases |
