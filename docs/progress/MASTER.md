# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery  
**Mode**: GitHub Issues + Milestones (SDD)  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Latest release**: **[v0.8.17](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.17)** (2026-07-17)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。  
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | [Milestone 27 — Enterprise residual v0.8.18](https://github.com/TokenDanceLab/metapi-go/milestone/27) |
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
| Enterprise residual **v0.8.16** | ✅ | #309–#311 · tag v0.8.16 |
| Enterprise residual **v0.8.17** | ✅ | #318–#320 · tag **v0.8.17** |
| Enterprise residual **v0.8.18** | 🔄 | Milestone 27 · #327–#329 |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| [#327](https://github.com/TokenDanceLab/metapi-go/issues/327) | product | `/v1/models` context_length from token_routes.context_length (#520 residual) |
| [#328](https://github.com/TokenDanceLab/metapi-go/issues/328) | test | fix handler/admin race flake RefreshBalance_NotFound under -race suite |
| [#329](https://github.com/TokenDanceLab/metapi-go/issues/329) | docs | residual honesty refresh post v0.8.17 |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.17](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.17) | 26 | Residual honesty · failed usage aggregates · route contextLength |
| [v0.8.16](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.16) | 25 | Gemini thought_signature wire · Responses multi-turn · failure usage logs |
| [v0.8.15](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.15) | 24 | Expired-mark guard · cascade isolation · stream usage partial |
| [v0.8.14](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.14) | 23 | Residual inventory · Redis sticky design · admin test honesty |
| [v0.8.13](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.13) | 22 | Gap matrix refresh · sticky eval · route reorder |
| older | 11–21 | See GitHub Releases / `CHANGELOG.md` |

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
gh issue list --milestone "Enterprise residual v0.8.18" --state open
gh pr list --state open
gh release view v0.8.17
git log --oneline origin/master -10
```

## Next Steps

1. Land **v0.8.18** Milestone 27: #327 models context_length wire · #328 admin race flake · #329 residual honesty refresh.
2. Product Milestones only with ACs after that: full Responses WS Codex; Redis sticky Option B; update-center registry.
3. Keep MASTER slim; docs map at `docs/README.md`.


## Governance

| Surface | Role |
|:--------|:-----|
| `AGENTS.md` | Agent hard rules (parity, dual dialect, pre-push) |
| GitHub Issues/Milestones | Task SSOT |
| `docs/progress/MASTER.md` | Session resume index (this file) |
| `CHANGELOG.md` | Version narrative |
| Native Claude project memory | Optional short pointers only |

Telemetry / drift: Milestone descriptions + Issue comments (SDD adaptive control).
