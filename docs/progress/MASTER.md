# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery
**Mode**: GitHub Issues + Milestones (SDD)
**Repo**: https://github.com/TokenDanceLab/metapi-go
**Latest release**: **[v0.8.22](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.22)** (2026-07-17)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | closed Milestone 31 (v0.8.22) — next residual wave TBD |
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
| Enterprise residual **v0.8.18** | ✅ | #327–#329 · tag v0.8.18 |
| Enterprise residual **v0.8.19** | ✅ | #334–#336 · tag v0.8.19 |
| Enterprise residual **v0.8.20** | ✅ | #345–#346 · tag v0.8.20 |
| Enterprise residual **v0.8.21** | ✅ | #350–#351 (PRs #352/#353); tag **v0.8.21** |
| Enterprise residual **v0.8.22** | ✅ | #355–#359 (PRs #360–#364); tag **v0.8.22** |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| — | — | Board clean after v0.8.22; next residual only with ACs |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.22](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.22) | 31 | admin key redact · custom_headers deny · CheckRedirect · soft-filter priority · residual honesty |
| [v0.8.21](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.21) | 30 | completions include_usage · residual honesty |
| [v0.8.20](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.20) | 29 | chat stream include_usage · residual honesty |
| [v0.8.19](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.19) | 28 | residual honesty · healthPersist race · P0-585 cascade honesty |
| [v0.8.18](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.18) | 27 | models contextLength wire · admin race isolation · residual honesty |
| older | 11–26 | See GitHub Releases / `CHANGELOG.md` |

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
gh release view v0.8.22
git log --oneline origin/master -10
```

## Next Steps

1. Product Milestones only with ACs: full Responses WS Codex; Redis sticky Option B; update-center registry.
2. Optional residual polish: P0-585 load-proof / site-model breaker; P0-555 media/lag; proxy max-token enforce from contextLength.
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
