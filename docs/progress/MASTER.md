# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery
**Mode**: GitHub Issues + Milestones (SDD)
**Repo**: https://github.com/TokenDanceLab/metapi-go
**Latest release**: **[v0.8.29](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.29)** (2026-07-18) — remains latest until M40 release gate

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | [Milestone 40 — Enterprise residual client/routing polish v0.8.30](https://github.com/TokenDanceLab/metapi-go/milestone/40) |
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
| Enterprise residual security polish **v0.8.27** | ✅ closed | #407–#410 (PRs #411–#414); tag **v0.8.27** |
| Enterprise residual SSRF/client harden **v0.8.28** | ✅ closed | #416–#418 (PRs #419–#421); tag **v0.8.28** |
| Enterprise residual reliability harden **v0.8.29** | ✅ closed | #423–#426 (PRs #427/#428/#430/#431); tag **v0.8.29** |
| Enterprise residual client/routing polish **v0.8.30** | 🔄 active | Milestone 40 · board **#433–#435** |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| [#433](https://github.com/TokenDanceLab/metapi-go/issues/433) | security P0 | SEC-OAUTH-NOTIFY-REDIR: share RejectCrossOriginRedirect on OAuth HTTP client + Telegram notify client |
| [#434](https://github.com/TokenDanceLab/metapi-go/issues/434) | reliability P1 | REL-SOURCE-MODEL: loadRouteMatch source-model fallback from source route pattern (no empty stub) |
| [#435](https://github.com/TokenDanceLab/metapi-go/issues/435) | docs | Residual honesty post v0.8.29 (M40 board) |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.
**M35–M39 closed**: do not re-list #388–#390, #397–#400, #407–#410, #416–#418, or #423–#426 as active work (landed on master with v0.8.29).
**Latest release**: stays **v0.8.29** until M40 release gate; do not claim v0.8.30 until tag.


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.29](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.29) | 39 | preferred breaker · cooldown timestamp parse · conductor attempt budget · residual honesty |
| [v0.8.28](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.28) | 38 | SEC-REDIR bare clients · SEC-MONITOR logout clear · residual honesty |
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
gh release view v0.8.29
git log --oneline origin/master -10
```

## Next Steps

1. Land M40 board **#433–#435**: SEC-OAUTH-NOTIFY-REDIR (#433 P0) · REL-SOURCE-MODEL (#434 P1) · residual honesty (#435). Latest release stays **v0.8.29** until release gate.
2. Product Milestones only with ACs: full Responses WS Codex; Redis sticky Option B; update-center registry.
3. Optional later: P0-585 production load-proof / empty-filter residual; deeper P0-555 media/lag polish; further dialect context_length enforce.
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
