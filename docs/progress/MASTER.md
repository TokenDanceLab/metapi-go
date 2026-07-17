# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery  
**Mode**: GitHub Issues + Milestones (SDD)  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Latest release**: **[v0.8.18](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.18)** (2026-07-17)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。  
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | closed Milestone 27 (v0.8.18) — next residual wave TBD |
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
| Enterprise residual **v0.8.16** | ✅ | #309–#311 · tag v0.8.16 |
| Enterprise residual **v0.8.17** | ✅ | #318–#320 · tag v0.8.17 |
| Enterprise residual **v0.8.18** | ✅ | #327–#329 (PRs #330–#332); tag **v0.8.18** |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| — | — | Board clean after v0.8.18; next residual only with ACs |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.18](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.18) | 27 | models contextLength wire · admin race isolation · residual honesty |
| [v0.8.17](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.17) | 26 | Residual honesty · failed usage aggregates · route contextLength admin |
| [v0.8.16](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.16) | 25 | Gemini thought_signature · Responses multi-turn · failure usage logs |
| [v0.8.15](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.15) | 24 | Expired-mark guard · cascade isolation · stream usage partial |
| older | 11–23 | See GitHub Releases / `CHANGELOG.md` |

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
gh release view v0.8.18
git log --oneline origin/master -10
```

## Next Steps

1. Product Milestones only with ACs: full Responses WS Codex; Redis sticky Option B; update-center registry.
2. Optional residual polish: P0-585 load-proof / site-model breaker; P0-555 policy/media/lag; proxy max-token enforce from contextLength.
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
