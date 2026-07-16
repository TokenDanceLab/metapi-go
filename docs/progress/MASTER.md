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
| **M-STACK** | Frontend stack modernization (TS7 + React19 + Vite8) | ✅ S0–S3 landed (#35/#68); S4 operational via CI green path |
| **M-GAP** | Original metapi gap inventory (docs-only) | ✅ Closed / accepted (G4 #11) |
| **M-BACKEND** | Backend architecture clarity | ✅ B0–B3 landed |
| **M-UI** | UI/UX design system | ✅ U0–U3 landed (DESIGN/tokens/components/pages/a11y) |
| **M-SCHEMA** | Schema compat + upgrade | ✅ SC0–SC2 landed (parity + additive migrations + P0 columns) |
| **M-RELIABILITY** | Reliability and boundaries | ✅ R0–R2 landed |
| **M-FEATURE** | Feature completeness from gap matrix | ✅ Closed backlog #38–#56 (PRs #74–#94). Competitive learn milestone M-COMPETE open (allapihub/axonhub/new-api/litellm) |

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
| B0–B3 | Landed (architecture docs, package boundaries, concurrency, unified errors) |
| Philosophy SSOT | `docs/design/BACKEND.md` |

### M-FEATURE notes (active)

| Item | Detail |
|------|--------|
| Milestone | https://github.com/TokenDanceLab/metapi-go/milestone/6 · Roadmap `docs/plan/feature-complete-roadmap.md` |
| Shipped infra | Site max concurrency · per-key `proxy_url` · group route rebuild |
| Closed F1+P1 | Full gap backlog #38–#56 (PRs #74–#94) |
| In flight WFs | none (competitive docs #97 + flake fix #96 landed) |
| Next | Schedule M-COMPETE [learn] #110–#121 (TTFT, traces, cost, health, routing strategies…); product only after backlog-only removed |
| Residual CI | `vulncheck` GO-2026-5856 (Go 1.26.4); frontend occasional `EnvironmentTeardownError` flake (tests pass) |

### M-COMPETE notes (active)

| Item | Detail |
|------|--------|
| Milestone | https://github.com/TokenDanceLab/metapi-go/milestone/8 |
| Local clones | operator-only checkouts under competitors/ (not product SSOT) |
| Scope | Docs-first competitive learning; matrix `docs/analysis/competitive/`; `[learn]` **#110–#121** (open) |
| Peers | [all-api-hub](https://github.com/qixing-jk/all-api-hub) · [axonhub](https://github.com/looplj/axonhub) · [new-api](https://github.com/QuantumNous/new-api) · [litellm](https://github.com/BerriAI/litellm) |
| Matrix / backlog | `docs/analysis/competitive/matrix.md` · issues **#110–#121** |
| Inventory | `docs/analysis/competitive/` — [README](../analysis/competitive/README.md) · [sources](../analysis/competitive/sources.md) · [matrix](../analysis/competitive/matrix.md) |
| Status | Matrix L1–L12 published; `[learn]` backlog issues created under M-COMPETE. No product code until scheduled |


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
- **M-UI** complete foundation: `docs/design/DESIGN.md` + tokens + U1–U3
- **M-GAP** inventory closed/accepted: `docs/plan/gap-inventory-acceptance.md` (backlog shells #36–#56; #36/#37 closed as addressed by R0/R1)
- **M-FEATURE shipped P0 slices**: per-site max concurrency, per-key `proxy_url`, custom group route rebuild
- **M-SCHEMA**: additive `schema_migrations` + columns `proxy_url` / `max_concurrency` / `context_length`

## Next Steps
1. Continue **M-FEATURE** remaining P0 backlog: token stats (#42), cache_ratio (#43), key/whitelist correctness (#40/#41/#45/#46), expired-connection UX (#39), cross-protocol failover (#38)
2. Protocol P1: `/v1/rerank` (#48), Gemini thought_signature residual (#47), Responses multi-turn (#50), Codex/OAuth model issues (#49/#55)
3. Watch CI frontend unhandled-rejection flakiness (dashboard observability)
4. Optional: Go stdlib vulncheck GO-2026-5856 via Go 1.26.5 when available
