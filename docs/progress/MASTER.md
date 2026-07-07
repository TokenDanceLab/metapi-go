# MASTER.md — MetAPI Go Rewrite

**Task**: MetAPI TypeScript → Go 完整重写 + 生产就绪优化
**Tracking Mode**: LOCAL_ONLY
**Repository**: https://github.com/TokenDanceLab/metapi-go

## Current Status

| Phase | Name | Status |
|:------|:-----|:-------|
| P0-P13 | Original rewrite (14 phases) | ✅ |
| COMPARE | TS vs Go deep comparison | ✅ |
| AUDIT | 16-dimension audit | ✅ |
| FIX | Audit findings remediation | ✅ |
| **HARDEN** | Production hardening (18/23 findings) | ✅ Done |

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
- Release: v0.3.0 → **next: v0.4.0**

## Specs
- Audit: `docs/analysis/hardening-audit.md`
- Task breakdown: `docs/plan/task-breakdown-hardening.md`
- Dependency graph: `docs/plan/dependency-graph-hardening.md`
- Milestones: `docs/plan/milestones-hardening.md`

## Next Steps
1. Wait for CI to pass on latest push (commit 04106fd)
2. Tag v0.4.0
3. Verify CD publishes GHCR package
