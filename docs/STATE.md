# STATE.md — MetAPI Go product status

**Last verified**: 2026-07-19

> **现状 SSOT**（产品仓库）。只记当前事实与指针，不写流水账。  
> 运维主机/compose/镜像 pin / PG role LIMIT 以 **server 仓** `projects/metapi/STATE.md` 为准（可能与本 tip 不同步）。  
> 进度开放项 → [`progress/MASTER.md`](progress/MASTER.md)  
> 时间线 → [`log.md`](log.md)  
> 高价值下一步 → [`analysis/high-value-next.md`](analysis/high-value-next.md)  
> 版本叙事 → 根 [`CHANGELOG.md`](../CHANGELOG.md)

## Current

| Fact | Value |
|:-----|:------|
| Latest release tag | **[v0.8.44](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.44)** (2026-07-19) |
| Tip | `origin/master` M52 Wave1 merged (unreleased; ops pin v0.8.44) |
| Production pin (ops) | server `projects/metapi/STATE.md` (hk3 **0.8.44** healthy; pool/role **1/1**; restart=no) |
| Standby us1 pin | compose **0.8.42** + image pulled (#528); cold stack not auto-started |
| Active milestone | **[52 UI-POLISH](https://github.com/TokenDanceLab/metapi-go/milestone/52)** open — Wave1 #543–#546 **closed**; residual #553/#554 first-run + release decision |
| Open issues / PRs | M52: epic #548 · gate #547 · first-run #553/#554; M51 closed |
| Mode | **UI-POLISH** (Track A ops stable on 0.8.44) |
| Stack | Go 1.26.5 · React 19 · Vite 8 · dual dialect SQLite/PG |

## Honesty holds (not product yet)

| ID | Status | Note |
|:---|:-------|:-----|
| P0-585 cascade | **partial** | load-proof still required; honesty tests do not flip present |
| P0-555 usage stats | **present-with-residual** | not perfect billing |
| WS-1 Responses WebSocket | residual | no invent without AC |
| STICKY-B Redis sticky | design-only | process-local sticky only |
| UC-1 update-center deploy | residual | admin deploy 501 / log-only |
| OPS-PG-BUDGET | **present product** (v0.8.44 code) | profiles + lease backoff; ops still size role LIMIT |
| UI-REFRESH | **delivered unreleased** | M51 closed; M52 Wave1 landed (Traffic chart well · real page shots · hex soft pass · axe smoke); residual first-run Dashboard/Sites (#553/#554) · UI patch release decision |

## Next-wave pointer

Prioritized **ours vs original** shortlist: [`analysis/high-value-next.md`](analysis/high-value-next.md)  
UI wave SSOT: [`analysis/ui-ux-refresh.md`](analysis/ui-ux-refresh.md) · visual harness [`analysis/ui-visual-acceptance.md`](analysis/ui-visual-acceptance.md)  
Full residual inventory: [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md)  
Original parity evidence (historical matrix): [`analysis/original-gap-matrix.md`](analysis/original-gap-matrix.md)

## Entry points

| Need | Path |
|:-----|:-----|
| Doc map | [`README.md`](README.md) |
| Open gates / next | [`progress/MASTER.md`](progress/MASTER.md) |
| High-value next | [`analysis/high-value-next.md`](analysis/high-value-next.md) |
| Formal readiness | [`analysis/formal-readiness.md`](analysis/formal-readiness.md) — Track A 对内正式 / Track B 对外完备 |
| UI/UX refresh | [`analysis/ui-ux-refresh.md`](analysis/ui-ux-refresh.md) — M51 Phase 1 in tree |
| Residual inventory | [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) |
| Agent rules | root [`AGENTS.md`](../AGENTS.md) |
| Deploy vars | [`deployment.md`](deployment.md) |
| Ops live host | server `projects/metapi/STATE.md` (not this file) |

## Branch hygiene (repo)

| Fact | Value |
|:-----|:------|
| Default branch | `master` |
| Worktrees | clean master; prior M51 lanes pruned after merge |
| Stale remote feature heads | deleted after merge-PR / abandon sweep 2026-07-18 |
