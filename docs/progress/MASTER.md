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
| **M-GAP** | Original metapi gap inventory (docs-only) | 🔄 In progress (Wave 1) |

> **Scope boundary (this round):** M-STACK and M-GAP **do not** implement original-gap product fixes.
> Stack work modernizes frontend tooling only. Gap work is inventory + backlog issues only.
> No product feature implementation ships under either milestone in this program.

## Program map (#3–#11)

| Issue | Track | Title | Wave |
|------:|:------|:------|:-----|
| #3 | S0 / gate | Bootstrap SDD + MASTER status for stack/gap program | Wave 0→1 |
| #4 | S1 | Bump TS 7.0.2 + React 19.2.7 + Vite 8.1.5 (core lock) | Wave 1 |
| #5 | S2 | React 19 test adaptation (~79 react-test-renderer files) | Wave 2 |
| #6 | S3 | Vite 8 plugin / vitepress-mermaid tooling closure | Wave 2 |
| #7 | S4 | CI / Docker / embed regression gate + CHANGELOG | Wave 3 |
| #8 | G1 | Capture original metapi open issues + taxonomy | Wave 1 |
| #9 | G2 | metapi-go capability gap matrix with code evidence | Wave 1 |
| #10 | G3 | Publish [backlog] GitHub issues from gap matrix | Wave 2 |
| #11 | G4 | Gap inventory acceptance (docs-only gate) | Wave 3 |

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

## Enterprise modernization (active)
- **M-UI** design system foundation: `docs/design/DESIGN.md` + `web/styles/tokens.css` (U0 / #12)

## Next Steps
1. Complete Wave 1: S1 core lock + remaining lane PRs (U0/B0/B2/R0)
2. Wave 2: S2/S3 tooling + G3 backlog + U1 components + SC1/R1
3. Wave 3: S4 gate + U2/U3 + feature fleets from F0
4. Continue M-UI: U1 shared components on design tokens (`docs/design/DESIGN.md`)
5. Product gap implementation only after F0 scheduling (except CRITICAL B2/R*)
