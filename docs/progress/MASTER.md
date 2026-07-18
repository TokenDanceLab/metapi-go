# MASTER.md — MetAPI Go

**Task**: MetAPI TypeScript → Go rewrite + enterprise residual delivery
**Mode**: GitHub Issues + Milestones (SDD)
**Repo**: https://github.com/TokenDanceLab/metapi-go
**Latest release**: **[v0.8.35](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.35)** (2026-07-18)

> 本文件是**轻量导航索引**，不是变更日志。细节进 Issue / PR / CHANGELOG。
> 文档地图：[`docs/README.md`](../README.md)

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Active milestone | [Milestone 46](https://github.com/TokenDanceLab/metapi-go/milestone/46) open until **v0.8.36** release gate |
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
| Enterprise residual client/routing polish **v0.8.30** | ✅ closed | #433–#435 (PRs #436–#438); tag **v0.8.30** |
| Enterprise residual security polish **v0.8.31** | ✅ closed | #440–#443 (PRs #444–#447); tag **v0.8.31** |
| Enterprise residual security/product polish **v0.8.32** | ✅ closed | #449–#451 (PRs #452–#454); tag **v0.8.32** |
| Enterprise UI/schema/product residual polish **v0.8.33** | ✅ closed | #456–#459 (PRs #460–#462 / #464); tag **v0.8.33** |
| Enterprise UI schema-product residual polish **v0.8.34** | ✅ closed | #466–#469 (PRs #470–#473); tag **v0.8.34** |
| Enterprise UI/reliability residual polish **v0.8.35** | ✅ closed | #475–#478 (PRs #479–#482); tag **v0.8.35** |
| Enterprise security/UI residual polish **v0.8.36** | 🔄 product landed / release pending | Milestone 46 · product #484–#486 (PRs #489/#490/#488) on master; docs #487 this PR; **tag pending** |

## Active work

| Issue | Track | Title |
|------:|:------|:------|
| — | — | Product board empty after this PR closes #487; next is **v0.8.36** release gate |

**Board hygiene**: one Issue per topic; never leave conflict markers in squash merges.
**M35–M45 closed**: do not re-list #388–#390, #397–#400, #407–#410, #416–#418, #423–#426, #433–#435, #440–#443, #449–#451, #456–#459, #466–#469, or #475–#478 as active work (landed on master with v0.8.35).
**M46 product landed on master**: #484 SEC-MONITOR-TOKEN-CLEAR (PR #489 / 1f3a674) · #485 UI-CSS-RESIDUAL (PR #490 / a2432da) · #486 REL-P0555-STREAM-TESTS (PR #488 / b84a9ea). Docs honesty is #487 (this PR).
**Milestone 46**: remains open until release gate / **v0.8.36** tag — do not claim v0.8.36 released.
**Latest release**: stays **v0.8.35** until the M46 release gate.


## Residual releases (pointer only)

| Tag | Milestone | Highlights |
|:----|:----------|:-----------|
| [v0.8.35](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.35) | 45 | DownstreamKeys maxRpm/maxTpm UI · P0-585 empty-filter honesty tests · login token debt · residual honesty |
| [v0.8.34](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.34) | 44 | DownstreamKeys proxyUrl UI · TokenRoutes contextLength UI · CSS token debt · residual honesty |
| [v0.8.33](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.33) | 43 | stat-icon design tokens · Sites maxConcurrency UI · Gemini maxOutputTokens · residual honesty |
| [v0.8.32](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.32) | 42 | system-proxy/test targetUrl guard · Responses max_output_tokens vs context_length · residual honesty |
| [v0.8.31](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.31) | 41 | ProxyAware/SiteProxy CheckRedirect · key mutate redact · residual honesty |
| [v0.8.30](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.30) | 40 | OAuth/Telegram CheckRedirect · source-model fallback · residual honesty |
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
gh release view v0.8.33
git log --oneline origin/master -10
```

## Next Steps

1. After this docs PR closes #487: M46 product board is empty on master; run **v0.8.36** release gate (tag + milestone close). Latest release remains **v0.8.35** until then.
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
