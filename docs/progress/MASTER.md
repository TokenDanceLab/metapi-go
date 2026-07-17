# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery  
**Mode**: GitHub Issues + Milestones (SDD)  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Latest release**: **[v0.8.16](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.16)** (2026-07-17)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。  
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | next residual wave after v0.8.16 (open when needed) |
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
| Enterprise residual **v0.8.16** | ✅ | #309–#311 (PRs #313/#314/#315); tag **v0.8.16** |

## Active work

v0.8.16 product Issues **closed**. Next wave: residual-next-candidates partials (#585 load-proof residual, #555 aggregation deeper, multi-key #579) — open Issues only when starting work.

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.16](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.16) | 25 | Gemini thought_signature wire · Responses multi-turn · failure usage logs |
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

1. Tag **v0.8.16** (this release) — Milestone 25 closed.
2. Next residual wave only with clear ACs: #585 load residual, #555 aggregation, optional #579 multi-key.
3. Product Milestones only with ACs: full Responses WS Codex; Redis sticky Option B; update-center registry.
4. Keep MASTER slim; docs map at `docs/README.md`.


## Governance

| Surface | Role |
|:--------|:-----|
| `AGENTS.md` | Agent hard rules (parity, dual dialect, pre-push) |
| GitHub Issues/Milestones | Task SSOT |
| `docs/progress/MASTER.md` | Session resume index (this file) |
| `CHANGELOG.md` | Version narrative |
| Native Claude project memory | Optional short pointers only |

Telemetry / drift: Milestone descriptions + Issue comments (SDD adaptive control).
