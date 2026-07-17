# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery
**Mode**: GitHub Issues + Milestones (SDD)
**Repo**: https://github.com/TokenDanceLab/metapi-go
**Latest release**: **[v0.8.27](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.27)** (2026-07-18)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | closed Milestone 37 (v0.8.27) — next residual wave TBD |
| Program map | `docs/plan/enterprise-program.md` |
| Residual backlog | `docs/analysis/residual-next-candidates.md` |
| M35 review synthesis | `docs/analysis/enterprise-review-m35.md` (#388; M35 closed) |
| Gap matrix | `docs/analysis/original-gap-matrix.md` |
| Architecture | `docs/architecture.md` · design `docs/design/BACKEND.md` |

## Current Status (2026-07-18)

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
| Enterprise residual **v0.8.23** | ✅ | #366–#368 (PRs #369/#370/#372); tag **v0.8.23** |
| Enterprise residual **v0.8.24** | ✅ | #375–#377 (PRs #378/#379/#380); tag **v0.8.24** |
| Enterprise residual **v0.8.25** | ✅ | #382–#384 (PRs #385/#386/#387); tag **v0.8.25** |
| M35 residual review / follow-ons | ✅ closed | #388 synthesis · #389/#396 endpoint early reject · #390/#395 multi-route regression |
| Enterprise residual polish **v0.8.26** | ✅ closed | #397–#400 (PRs #401–#404/#406); tag **v0.8.26** |
| Enterprise residual security polish **v0.8.27** | ✅ | #407–#410 (PRs #411–#414); tag **v0.8.27** |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| — | — | Board clean after v0.8.27; next residual only with ACs |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.
**M35/M36/M37 closed**: do not re-list #388–#390, #397–#400, or #407–#410 as active work (landed on master with v0.8.27).


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.27](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.27) | 37 | opaque monitor session · OldToken constant-time · Claude max_tokens vs context_length · residual honesty |
| [v0.8.26](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.26) | 36 | IsValidAPIEndpointURL metadata parity · max_tokens vs context_length reject · stream missing-usage warn · residual honesty |
| [v0.8.25](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.25) | 34 | IsValidHTTPURL metadata harden · routes N+1 batch · residual honesty |
| [v0.8.24](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.24) | 33 | routes/search secret redact · site metadata URL guard · residual honesty |
| [v0.8.23](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.23) | 32 | admin account secret redact · RR/stable soft-filter demotion · residual honesty |
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
gh release view v0.8.27
git log --oneline origin/master -10
```

## Next Steps

1. Close Milestone 37: #407 opaque monitor session · #408 OldToken constant-time · #409 Claude max_tokens · #410 residual honesty. Latest release **v0.8.27**.
2. Product Milestones only with ACs: full Responses WS Codex; Redis sticky Option B; update-center registry.
3. Optional later: P0-585 load-proof / site-model breaker; deeper P0-555 media/lag polish; further dialect context_length enforce.
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
