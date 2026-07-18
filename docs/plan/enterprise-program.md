# MetAPI-Go Enterprise Modernization Program

> **Status: closed / historical program map** (foundations closed through M46 / v0.8.36). Living residual queue: `docs/analysis/residual-next-candidates.md`. Do not schedule greenfield modernization from this file.

**Goal**: 彻底完成 MetAPI 的现代化升级与企业级改造 — 前端风格统一与 UI/UX、后端理念与结构清晰、schema 兼容原版且可升级、功能全面、边界条件干净。

**Project**: https://github.com/orgs/TokenDanceLab/projects/1  
**Repo**: https://github.com/TokenDanceLab/metapi-go

## Milestone map

| Milestone | URL | Intent |
|:----------|:----|:-------|
| M-STACK | https://github.com/TokenDanceLab/metapi-go/milestone/1 | TS7 + React19 + Vite8 |
| M-GAP | https://github.com/TokenDanceLab/metapi-go/milestone/2 | Original gap inventory (docs → backlog) |
| M-UI | https://github.com/TokenDanceLab/metapi-go/milestone/3 | Design system + visual consistency |
| M-BACKEND | https://github.com/TokenDanceLab/metapi-go/milestone/4 | Architecture clarity + CRITICAL concurrency |
| M-SCHEMA | https://github.com/TokenDanceLab/metapi-go/milestone/5 | Schema parity + additive upgrades |
| M-FEATURE | https://github.com/TokenDanceLab/metapi-go/milestone/6 | Feature completeness from gap matrix |
| M-RELIABILITY | https://github.com/TokenDanceLab/metapi-go/milestone/7 | Boundaries, classification, isolation |

## Issue index

### Stack / Gap (foundation)
| # | ID | Milestone |
|--:|:---|:----------|
| 3 | S0 | STACK |
| 4 | S1 | STACK |
| 5 | S2 | STACK |
| 6 | S3 | STACK |
| 7 | S4 | STACK |
| 8–11 | G1–G4 | GAP |

### UI/UX
| # | ID | Focus |
|--:|:---|:------|
| 12 | U0 | DESIGN.md + tokens |
| 13 | U1 | Shared components |
| 14 | U2 | Pages consistency |
| 15 | U3 | A11y + responsive |

### Backend
| # | ID | Focus |
|--:|:---|:------|
| 16 | B0 | BACKEND.md + architecture truth |
| 17 | B1 | Package boundaries |
| 18 | B2 | CRITICAL concurrency |
| 19 | B3 | Unified errors |

### Schema
| # | ID | Focus |
|--:|:---|:------|
| 20 | SC0 | Parity audit |
| 21 | SC1 | Additive migrations |
| 22 | SC2 | P0 feature columns |

### Feature + Reliability
| # | ID | Focus |
|--:|:---|:------|
| 23 | F0 | Feature roadmap from matrix |
| 24 | R0 | Error classification |
| 25 | R1 | Failure isolation |
| 26 | R2 | Observability + validation |

## Execution waves (enterprise)

```
Wave E0  Governance ready (done): master clean, Project, 7 milestones, issues #3–#26

Wave E1  Parallel (file-disjoint)
  S1 stack bump          web/package*
  G1→G2 gap matrix       docs/analysis/original-gap-*
  U0 design tokens       docs/design + web/styles
  B0 backend docs        docs/architecture + docs/design/BACKEND.md
  B2 concurrency fixes   routing/** proxy/**
  SC0 schema parity      docs/analysis/schema-parity.md

Wave E2  After E1 gates
  S2 tests, S3 tooling
  G3 backlog publish
  U1 components, B1/B3, SC1, R0/R1
  F0 roadmap (needs G2)

Wave E3
  U2 pages + U3 a11y
  SC2 additive columns
  S4 CI/Docker gate
  Feature implementation fleets (from F0/G3)
  R2 observability
  G4 acceptance

Wave E4  Product features from backlog (P0 first)
  rerank, site concurrency, per-key proxy, group rebuild, stats accuracy, ...
```

## Definition of Done (program)

1. **Stack**: TS 7.0.2 + React 19.2.x + Vite 8.1.x; CI frontend + docker embed green  
2. **UI**: DESIGN.md tokens; components/pages consistent; a11y checklist  
3. **Backend**: BACKEND.md truth; no RWMutexStub; lease/context fixed; package docs  
4. **Schema**: parity report; additive migrations; P0 columns with compat defaults  
5. **Features**: P0 gaps implemented or explicitly deferred with issue  
6. **Boundaries**: classification tests; no cascade; validation not silent-200  

## Orchestration rules

- GitHub Issues/Project = SSOT  
- File ownership lanes; no two WFs write same lock/schema file  
- Main session orchestrates only; Workflows execute issues  
- Original product gaps implement only after G2/F0 (except CRITICAL reliability B2/R*)

## Lane teams (mandatory)

See **[lane-charters.md](./lane-charters.md)** — each milestone has a dedicated Workflow fleet and file ownership.

| Lane WF | Active E1 work |
|:--------|:---------------|
| `lane-stack` | S0/S1 |
| `lane-gap` | G1/G2 |
| `lane-ui` | U0 DESIGN.md |
| `lane-backend` | B0 + B2 |
| `lane-schema` | SC0 |
| `lane-reliability` | R0 |
| `lane-feature` | blocked on G2+F0 |
| `lane-gate` | merge MASTER / release |

Detail design SSOT files:

- UI: `docs/design/DESIGN.md`
- Backend: `docs/design/BACKEND.md`
- Schema: `docs/analysis/schema-parity.md` (SC0)
- Program: this file + `lane-charters.md`
