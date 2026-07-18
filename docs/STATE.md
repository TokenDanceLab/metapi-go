# STATE.md — MetAPI Go product status

**Last verified**: 2026-07-18

> **现状 SSOT**（产品仓库）。只记当前事实与指针，不写流水账。  
> 运维主机/compose/镜像 pin 以 **server 仓** `projects/metapi/STATE.md` 为准（生产可能落后本仓 tip）。  
> 进度开放项 → [`progress/MASTER.md`](progress/MASTER.md)  
> 时间线 → [`log.md`](log.md)  
> 版本叙事 → 根 [`CHANGELOG.md`](../CHANGELOG.md)

## Current

| Fact | Value |
|:-----|:------|
| Latest release tag | **[v0.8.39](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.39)** (2026-07-18) |
| Tip | `origin/master` includes post-tag fix **#526** PG pool budget explicit — not yet a new release tag |
| Active milestone | **none** |
| Open issues / PRs | **0 / 0** |
| Mode | **Maintenance** — optional **v0.8.40+** only with dedicated ACs |
| GHCR public badge series | **v0.8.39** (README) |
| Stack | Go 1.26.5 · React 19 · Vite 8 · dual dialect SQLite/PG |

## Honesty holds (not product yet)

| ID | Status | Note |
|:---|:-------|:-----|
| P0-585 cascade | **partial** | load-proof still required; honesty tests do not flip present |
| P0-555 usage stats | **present-with-residual** | not perfect billing |
| WS-1 Responses WebSocket | residual | no invent without AC |
| STICKY-B Redis sticky | design-only | process-local sticky only |
| UC-1 update-center deploy | residual | admin deploy 501 / log-only |

## Entry points

| Need | Path |
|:-----|:-----|
| Doc map | [`README.md`](README.md) |
| Open gates / next | [`progress/MASTER.md`](progress/MASTER.md) |
| Residual inventory | [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) |
| Agent rules | root [`AGENTS.md`](../AGENTS.md) |
| Deploy vars | [`deployment.md`](deployment.md) |
| Ops live host | server `projects/metapi/STATE.md` (not this file) |

## Branch hygiene (repo)

| Fact | Value |
|:-----|:------|
| Default branch | `master` |
| Worktrees | main only (agent fleet worktrees pruned 2026-07-18) |
| Stale remote feature heads | deleted after merge-PR / abandon sweep 2026-07-18 |
