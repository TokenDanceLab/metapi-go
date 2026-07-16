# MASTER.md — MetAPI Go Rewrite

**Task**: MetAPI TypeScript → Go 完整重写 + 生产就绪优化 + stack/gap 双轨计划
**Tracking Mode**: GitHub Project + Milestones (SDD)
**Repository**: https://github.com/TokenDanceLab/metapi-go

## Tracking

| Item | URL |
|:-----|:----|
| Project | https://github.com/orgs/TokenDanceLab/projects/1 |
| Milestone M-STACK | https://github.com/TokenDanceLab/metapi-go/milestone/1 |
| Milestone M-GAP | https://github.com/TokenDanceLab/metapi-go/milestone/2 |
| Plan | `docs/plan/milestones-stack-gap.md` |

## Current Status

| Phase | Name | Status |
|:------|:-----|:-------|
| P0-P13 | Original rewrite (14 phases) | ✅ |
| COMPARE | TS vs Go deep comparison | ✅ |
| AUDIT | 16-dimension audit | ✅ |
| FIX | Audit findings remediation | ✅ |
| **HARDEN** | Production hardening (18/23 findings) | ✅ Done |
| **M-STACK** | Frontend stack modernization (TS7 + React19 + Vite8) | 🔄 In progress (Wave 1) |
| **M-GAP** | Original metapi gap inventory (docs-only) | ✅ Closed / accepted (G4 #11) |
| **M-BACKEND** | Backend architecture clarity | 🔄 In progress (B0 docs #16) |
| **M-UI** | UI/UX design system | 🔄 In progress (U0) |
| **M-SCHEMA** | Schema compat + upgrade | 🔄 SC0 done |
| **M-RELIABILITY** | Reliability and boundaries | 🔄 R0 in flight |
| **M-FEATURE** | Feature completeness from gap matrix | 🔄 Scheduled (F0 roadmap; not G4) |

> **Program map**: `docs/plan/enterprise-program.md` + `docs/plan/lane-charters.md`  
> **Scope**: product gap implementation only after F0; CRITICAL reliability (B2/R*) may ship earlier.

## Program map (#3–#11 stack/gap)

| Issue | Track | Title | Wave |
|------:|:------|:------|:-----|
| #3 | S0 / gate | Bootstrap SDD + MASTER status for stack/gap program | Wave 0→1 |
| #4 | S1 | Bump TS 7.0.2 + React 19.2.7 + Vite 8.1.5 (core lock) | Wave 1 |
| #5 | S2 | React 19 test adaptation (~79 react-test-renderer files) | Wave 2 |
| #6 | S3 | Vite 8 plugin / vitepress-mermaid tooling closure | Wave 2 |
| #7 | S4 | CI / Docker / embed regression gate + CHANGELOG | Wave 3 |
| #8 | G1 | Capture original metapi open issues + taxonomy | Wave 1 ✅ |
| #9 | G2 | metapi-go capability gap matrix with code evidence | Wave 1 ✅ |
| #10 | G3 | Publish [backlog] GitHub issues from gap matrix | Wave 2 ✅ |
| #11 | G4 | Gap inventory acceptance (docs-only gate) | Wave 3 ✅ |

### M-GAP notes (closed / accepted)

| Item | Detail |
|------|--------|
| Milestone | https://github.com/TokenDanceLab/metapi-go/milestone/2 |
| Acceptance | `docs/plan/gap-inventory-acceptance.md` (G4 #11) |
| Sources | `docs/analysis/original-gap-sources.md` |
| Taxonomy | `docs/analysis/original-gap-taxonomy.md` |
| Matrix | `docs/analysis/original-gap-matrix.md` (mandatory set complete) |
| Backlog | `docs/plan/original-gap-backlog.md` · issues **#36–#56** |
| Product fixes | **Not** this gate — schedule via **M-FEATURE** / individual PRs (`docs/plan/feature-complete-roadmap.md`) |

### M-BACKEND notes

| Item | Detail |
|------|--------|
| Milestone | https://github.com/TokenDanceLab/metapi-go/milestone/4 |
| B0 (#16) | `docs/architecture.md` truth + `docs/design/BACKEND.md` principles (docs only) |
| Next | B1 package boundaries · B2 CRITICAL concurrency · B3 unified errors |
| Philosophy SSOT | `docs/design/BACKEND.md` |

## Hardening Results

**Completed 2026-07-05. 4 commits pushed. All 30 packages test green.**

### Phase 1: Critical Fixes ✅
| Fix | Detail |
|-----|--------|
| C1 | `http.DefaultClient` → RuntimeExecutor (90s fallback) |
| C2 | 6 OAuth panics → return errors |
| C3 | Default token warnings in config.Validate() + AES key standalone |
| C4 | SSE streaming WriteTimeout disabled via SetWriteDeadline |

### Phase 2: Security & Reliability ✅
| Fix | Detail |
|-----|--------|
| H1 | `!=` → subtle.ConstantTimeCompare (admin + proxy auth) |
| H2 | CI: errcheck/staticcheck/ineffassign enabled, lint gates, -race in tests, standalone vet |
| H3 | DB pool: ConnMaxLifetime(5min) + ConnMaxIdleTime(2min) |
| H4 | 13 log.Printf → slog.Warn/Error |
| H5 | RequestID middleware (chi middleware.RequestID) |
| H7 | usage_aggregation: no longer re-panics (prevents server crash) |

### Phase 3: Observability & Tests ✅
| Fix | Detail |
|-----|--------|
| M1 | 7 zero-coverage packages → all ≥50% (chat 89.9%, images 100%, profiles 85.2%) |
| M3 | /metrics Prometheus endpoint (zero-dependency text format) |
| M4 | handler/shared/errors.go: APIError type + WriteError/WriteErrorDetail |
| M5 | SecurityHeaders: X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP |
| M8 | /debug/vars behind admin auth |

### Phase 4: Polish ✅
| Fix | Detail |
|-----|--------|
| E2E | 3 new hardening tests (concurrent proxy, auth timing, rate limit) |
| AGENTS.md | Pre-push checklist now includes -race |

### Deferred (not blocking v0.4.0)
- context.Background → request context in OAuth (needs broader signature refactor)
- chatFormatsCore.go file split
- PGO in Docker build
- CORS per-route group refinement
- Responses WebSocket (known STUB, matches TS parity gap doc)

## Coverage Highlights

| Package | Before | After |
|---------|--------|-------|
| transform/openai/chat | 0% | **89.9%** |
| transform/openai/completions | 0% | **50.0%** |
| transform/openai/embeddings | 0% | **100%** |
| transform/openai/images | 0% | **100%** |
| transform/openai/responses | 0% | **100%** |
| proxy/profiles | 0% | **85.2%** |
| service/adapter | 0% | covered |
| e2e tests | 4 files | **5 files** (+3 hardening tests) |

## CI/CD Status
- CI workflow includes lint, vet, vulncheck, frontend, test-sqlite `-race`, test-pg, build, and Docker image build plus `/health` and `/ready` smoke gates.
- CD: ✅ (ghcr.io/tokendancelab/metapi-go:latest)
- Release: v0.3.0 → **next: v0.4.0** (stack modernization may land in a later patch/minor after S4 gate)

## Specs
- Audit: `docs/analysis/hardening-audit.md`
- Schema parity (SC0 #20): `docs/analysis/schema-parity.md`
- Task breakdown: `docs/plan/task-breakdown-hardening.md`
- Dependency graph: `docs/plan/dependency-graph-hardening.md`
- Milestones (hardening): `docs/plan/milestones-hardening.md`
- Milestones (stack + gap): `docs/plan/milestones-stack-gap.md`
- Gap inventory acceptance (G4 #11): `docs/plan/gap-inventory-acceptance.md`
- Feature-complete roadmap (F0 #23): `docs/plan/feature-complete-roadmap.md`

## Enterprise modernization (active)
- **M-UI** design system foundation: `docs/design/DESIGN.md` + `web/styles/tokens.css` (U0 / #12)
- **M-GAP** inventory closed/accepted: `docs/plan/gap-inventory-acceptance.md` (backlog shells #36–#56)

## Next Steps
1. Complete remaining stack/UI/backend/reliability lane PRs (S*/U*/B*/R*/SC*)
2. Wave 3 stack: S4 CI/Docker/embed regression gate + CHANGELOG
3. Claim product gaps from backlog **#36–#56** via **M-FEATURE** / individual PRs (not M-GAP)
4. Continue M-UI design-token adoption on shared components and pages
5. CRITICAL reliability (B2/R*) may ship earlier than full M-FEATURE when required
