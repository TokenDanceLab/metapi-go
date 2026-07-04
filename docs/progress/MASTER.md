# MASTER.md — MetAPI Go Rewrite

**Task**: MetAPI TypeScript → Go 完整重写
**Tracking Mode**: LOCAL_ONLY
**Repository**: https://github.com/TokenDanceLab/metapi-go

## Current Status

| Phase | Name | Status |
|:------|:-----|:-------|
| P0 | Project skeleton + config + Docker | ✅ |
| P1 | DB schema (27 tables) + migration | ✅ |
| P2 | Auth middleware | ✅ |
| P3 | Sites + Accounts + AccountTokens CRUD | ✅ |
| P4 | Platform adapters (14 platforms) | ✅ |
| P5 | Checkin + Balance + Notify | ✅ |
| P6 | OAuth subsystem (4 providers) | ✅ |
| P7 | TokenRouter | ✅ |
| P8 | ProxyCore orchestration | ✅ |
| P9 | Protocol transformers | ✅ |
| P10 | Proxy routes (/v1/*) | ✅ |
| P11 | Admin API (~144 endpoints) | ✅ |
| P12 | Background schedulers (15) | ✅ |
| P13 | Frontend embed + CI/CD + docs | ✅ |
| **COMPARE** | TS vs Go deep comparison | ✅ |
| **AUDIT** | 16-dimension audit | ✅ |
| **FIX** | Audit findings remediation | ✅ |

## CI/CD Status
- CI: ✅ (build + test-sqlite + test-pg + lint)
- CD: ✅ (ghcr.io/tokendancelab/metapi-go:latest)
- Release: v0.3.0

## Deep Comparison Results
- **Routing**: ALL MATCH (9 algorithm steps verified)
- **Schema**: 27/27 tables, 20/20 FKs, 77/77 indexes MATCH
- **Config**: ~100 env vars MATCH
- **API format**: camelCase normalization applied
- **E2E tests**: 13/13 pass

## Audit Findings Summary (16 agents)
See `docs/plan/audit-fix-plan.md` for full prioritized fix plan.

| Severity | Count | Key Issues |
|:---------|:-----|:-----------|
| CRITICAL | 10 | No rate limiting, RWMutexStub no-op, proxy failures silenced, DB never closed, backup/factory-reset stubs, SSE byte passthrough, usage aggregation no transaction |
| HIGH | 12 | Error leak, AES key fallback, platform shield, OAuth coverage 24.9%, streaming gaps |
| MEDIUM | 15 | Log levels, alloc efficiency, password validation |
| LOW | 10 | Style, docs, minor optimizations |

## Next Steps
1. ~~Write fix plan~~ (done)
2. ~~Execute CRITICAL fixes first~~ (done)
3. ~~Verify locally (build + vet + test + e2e)~~ (done)
4. ~~Push + verify CI/CD green~~ (done)
5. ~~Tag v0.2.0~~ (done, now v0.3.0)
6. Monitor production metrics, observability tuning
