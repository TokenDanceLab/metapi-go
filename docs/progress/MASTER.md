# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery  
**Mode**: GitHub Issues + Milestones (SDD)  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Latest release**: **[v0.8.15](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.15)** (2026-07-17)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。  
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | [v0.8.16](https://github.com/TokenDanceLab/metapi-go/milestone/25) |
| Program map | `docs/plan/enterprise-program.md` |
| Residual backlog | `docs/analysis/residual-next-candidates.md` |
| Gap matrix | `docs/analysis/original-gap-matrix.md` |
| Architecture | `docs/architecture.md` · design `docs/design/BACKEND.md` |

## Current Status (2026-07-17)

| Track | Status | Notes |
|:------|:-------|:------|
| Rewrite P0–P13 | ✅ | Single-binary Go + embed SPA |
| M-STACK (TS7 / React19 / Vite8) | ✅ | CI frontend green path |
| M-GAP inventory | ✅ | Matrix + backlog; product via residual tags |
| M-BACKEND / UI / SCHEMA / RELIABILITY | ✅ | Design SSOT in `docs/design/` |
| M-FEATURE + competitive learn | ✅ | Gap #38–#56 · learn #110–#121 |
| Enterprise residual **v0.8.15** | ✅ | #298–#300 · tag v0.8.15 |
| Enterprise residual **v0.8.16** | 🔄 | #309 Gemini signature · #310 Responses multi-turn · #311 usage follow-up |

## Active work (v0.8.16)

| Issue | Title | Lane |
|------:|:------|:-----|
| [#309](https://github.com/TokenDanceLab/metapi-go/issues/309) | Gemini tool-history `thought_signature` | protocol |
| [#310](https://github.com/TokenDanceLab/metapi-go/issues/310) | Multi-turn Responses content honesty | protocol |
| [#311](https://github.com/TokenDanceLab/metapi-go/issues/311) | Usage aggregation / non-stream accuracy | observability |

**Closed as duplicate**: #308 → #309. **Superseded PR**: #306 → already merged via #302.

## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.15](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.15) | 24 | Expired-mark guard · cascade isolation · stream usage partial |
| [v0.8.14](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.14) | 23 | Residual inventory · Redis sticky design · admin test honesty |
| [v0.8.13](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.13) | 22 | Gap matrix refresh · sticky eval · route reorder |
| [v0.8.12](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.12) | 21 | Task race · announcement sync · recovery provider · WS residual |
| older | 11–20 | See GitHub Releases / `CHANGELOG.md` |

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
gh issue list --milestone "Enterprise residual v0.8.16" --state open
gh pr list --state open
gh release view v0.8.15
git log --oneline origin/master -10
```

## Next Steps

1. Land **v0.8.16** product PRs for #309 / #310 / #311 (parallel WFs).
2. Keep residuals honest: no fake WS / no Redis sticky product without dedicated ACs.
3. Prefer board hygiene: one Issue per topic; close duplicates immediately.
4. MASTER stays slim — release notes go to `CHANGELOG.md` + GitHub Release only.

## Governance

| Surface | Role |
|:--------|:-----|
| `AGENTS.md` | Agent hard rules (parity, dual dialect, pre-push) |
| GitHub Issues/Milestones | Task SSOT |
| `docs/progress/MASTER.md` | Session resume index (this file) |
| `CHANGELOG.md` | Version narrative |
| Native Claude project memory | Optional short pointers only |

Telemetry / drift: Milestone descriptions + Issue comments (SDD adaptive control).
