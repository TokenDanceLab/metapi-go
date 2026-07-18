# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery  
**Mode**: GitHub Issues + Milestones (SDD)  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Latest release**: **[v0.8.37](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.37)** (2026-07-18)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。  
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | none (board clean; next residual only with ACs) |
| Program map | `docs/plan/enterprise-program.md` (historical / closed foundations) |
| Residual backlog | `docs/analysis/residual-next-candidates.md` |
| M35 review synthesis | `docs/analysis/enterprise-review-m35.md` (#388; historical) |
| Gap matrix | `docs/analysis/original-gap-matrix.md` |
| Architecture | `docs/architecture.md` · design `docs/design/BACKEND.md` |

## Current Status (2026-07-18)

| Track | Status | Notes |
|:------|:-------|:------|
| Rewrite P0–P13 | ✅ | Single-binary Go + embed SPA |
| Program foundations (STACK / GAP / BACKEND / UI / SCHEMA / RELIABILITY / FEATURE) | ✅ | Closed; residual polish only |
| Enterprise residual train **v0.8.18–v0.8.36** (M27–M46) | ✅ closed | Latest **v0.8.36** / M46; full narrative → `CHANGELOG.md` + Releases |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| — | — | Board clean (no open residual product board) |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.
**M35–M47 closed** with v0.8.37. Latest release **v0.8.37** (published after this gate).

## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.37](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.37) | 47 | README stack truth · TPM admission estimate · P0-585 credential usage honesty · residual honesty |
| [v0.8.36](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.36) | 46 | monitor cookie clear · CSS residual · P0-555 stream usage honesty · residual honesty |
| [v0.8.35](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.35) | 45 | DownstreamKeys maxRpm/maxTpm UI · P0-585 empty-filter honesty · login token debt |
| older | 11–44 | See GitHub Releases / `CHANGELOG.md` |

## Architecture entry points

| Concern | SSOT |
|:--------|:-----|
| Package map / request path | `docs/architecture.md` |
| Backend philosophy / boundaries | `docs/design/BACKEND.md` |
| UI design system | `docs/design/DESIGN.md` |
| Engineering rules | root `AGENTS.md` |
| Public API surface | `docs/api.md` |
| Deploy | `docs/deployment.md` |
| Residual honesty (what is NOT product yet) | `docs/analysis/residual-next-candidates.md` |

## Quick status commands

```bash
gh issue list --state open --limit 20
gh pr list --state open
gh release view v0.8.36
git log --oneline origin/master -10
```

## Next Steps

1. Board clean after **v0.8.37**. Optional residual **v0.8.38+** only with dedicated ACs.
2. Do **not** invent WS-1 / STICKY-B Redis / UC-1 product without dedicated ACs.
3. Optional later: P0-585 production load-proof; deeper P0-555 media/lag polish; further dialect context_length only if a new dialect needs ACs.


## Governance

| Surface | Role |
|:--------|:-----|
| `AGENTS.md` | Agent hard rules (parity, dual dialect, pre-push) |
| GitHub Issues/Milestones | Task SSOT |
| `docs/progress/MASTER.md` | Session resume index (this file) |
| `CHANGELOG.md` | Version narrative |
| Native Claude project memory | Optional short pointers only |

Telemetry / drift: Milestone descriptions + Issue comments (SDD adaptive control).
