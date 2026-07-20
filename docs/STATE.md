# STATE.md — MetAPI Go product status

**Last verified**: 2026-07-21

> **现状 SSOT**（产品仓库）。只记当前事实与指针，不写流水账。  
> 运维主机/compose/镜像 pin / PG role LIMIT 以 **server 仓** `projects/metapi/STATE.md` 为准（可能与本 tip 不同步）。  
> 进度开放项 → [`progress/MASTER.md`](progress/MASTER.md)  
> 时间线 → [`log.md`](log.md)  
> 高价值下一步 → [`analysis/high-value-next.md`](analysis/high-value-next.md)  
> 版本叙事 → 根 [`CHANGELOG.md`](../CHANGELOG.md)

## Current

| Fact | Value |
|:-----|:------|
| Latest release tag | **[v0.8.45](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.45)** (2026-07-20) — RE2-safe + UI tip |
| Tip | post-v0.8.45: OAuth refresh + Sub2API due + parity KEYS/#579 + WS C1+C2 + **cloud-ops UI 对齐** |
| Production pin (ops) | server `projects/metapi/STATE.md` — hk3 still **0.8.44 Exited(2)** until authorized pin/up of **0.8.45** + 15min soak; pool/role **1/1**; restart=no |
| Standby us1 pin | compose **0.8.42** + image pulled (#528); cold stack not auto-started |
| Active milestone | **[52 UI-POLISH](https://github.com/TokenDanceLab/metapi-go/milestone/52)** — Wave1 + first-run Wave2 closed; **v0.8.45** tagged |
| Open issues / PRs | board empty (epic #548 closed); optional residual not scheduled as issues |
| Mode | **parity program (docs)** — v0.8.45 released; ops pin/up gated; original-complete plan SSOT [`plan/original-parity-complete-2026-07-20.md`](plan/original-parity-complete-2026-07-20.md) |
| Stack | Go 1.26.5 · React 19 · Vite 8 · dual dialect SQLite/PG |

## Honesty holds (not product yet)

| ID | Status | Note |
|:---|:-------|:-----|
| P0-585 cascade | **partial** | load-proof still required; honesty tests do not flip present |
| P0-555 usage stats | **present-with-residual** | not perfect billing |
| WS-1 Responses WebSocket | **C1+C2 present** · C3 residual | HTTP multi-turn bridge + per-msg quota; C3 Codex upstream wss residual; single-instance honesty |
| STICKY-B Redis sticky | design-only **deferred** | multi-turn/WS requires single instance or LB pin |
| UC-1 update-center deploy | residual → **hide/external** | no invent registry; GHCR/ops deploy |
| OPS-PG-BUDGET | **present product** (v0.8.44 code) | profiles + lease backoff; ops still size role LIMIT |
| OPS-RE2-USERID | **fixed in v0.8.45** | was production Exited(2) on 0.8.44; ops still must pin/up 0.8.45 + soak |
| OPS-OAUTH-REFRESH | **present** (#251 / post-v0.8.45) | OAuth token auto-refresh scheduler (oauth-refresh); TS parity for codex/claude/gemini-cli/antigravity lead times |
| OPS-SUB2API-REFRESH | **present** (post-#246) | Sub2API managed session token refresh via balance.RefreshBalance (extraConfig parsed, due filter with 300s lead window, concurrency=4); residual: no standalone lightweight refresh endpoint |
| UI-REFRESH / UI-POLISH | **released v0.8.45** + **cloud-ops align** | visual family → tokendance-design `styles/cloud-ops`; see [`design/cloud-ops-alignment.md`](design/cloud-ops-alignment.md); residual optional empty-DB shot |
| UI vs 原版功能 | **parity on web surface** | 2026-07-20 inventory: routes/buttons 齐平；体感「没了」= 空库 + pin 落后 tip + 主题换肤 — [`analysis/ui-original-parity-2026-07-20.md`](analysis/ui-original-parity-2026-07-20.md) |

## Next-wave pointer

Prioritized **ours vs original** shortlist: [`analysis/high-value-next.md`](analysis/high-value-next.md)  
**Parity program (ex-Electron)**: [`plan/original-parity-complete-2026-07-20.md`](plan/original-parity-complete-2026-07-20.md)  
UI wave SSOT: [`analysis/ui-ux-refresh.md`](analysis/ui-ux-refresh.md) · **cloud-ops 对齐** [`design/cloud-ops-alignment.md`](design/cloud-ops-alignment.md) · visual harness [`analysis/ui-visual-acceptance.md`](analysis/ui-visual-acceptance.md) · 原版功能对照 [`analysis/ui-original-parity-2026-07-20.md`](analysis/ui-original-parity-2026-07-20.md)  
Full residual inventory: [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md)  
Original parity evidence: [`analysis/original-gap-matrix.md`](analysis/original-gap-matrix.md)  
WS residual: [`analysis/responses-websocket-residual.md`](analysis/responses-websocket-residual.md)

## Entry points

| Need | Path |
|:-----|:-----|
| Doc map | [`README.md`](README.md) |
| Open gates / next | [`progress/MASTER.md`](progress/MASTER.md) |
| High-value next | [`analysis/high-value-next.md`](analysis/high-value-next.md) |
| Formal readiness | [`analysis/formal-readiness.md`](analysis/formal-readiness.md) — Track A 对内正式 / Track B 对外完备 |
| UI/UX refresh | [`analysis/ui-ux-refresh.md`](analysis/ui-ux-refresh.md) |
| Residual inventory | [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) |
| Agent rules | root [`AGENTS.md`](../AGENTS.md) |
| Deploy vars | [`deployment.md`](deployment.md) |
| Ops live host | server `projects/metapi/STATE.md` (not this file) |

## Branch hygiene (repo)

| Fact | Value |
|:-----|:------|
| Default branch | `master` |
| Worktrees | clean master; M52 feature worktrees pruned after merge |
| Stale remote feature heads | deleted after merge-PR |
| Unmerged historical branch | `origin/codex/metapi-regex-crash` (RE2 fix source; reapplied on master tip) |
