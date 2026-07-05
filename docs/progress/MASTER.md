# MASTER.md — MetAPI Go Rewrite

**Task**: MetAPI TypeScript → Go 完整重写 + 生产就绪优化
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
| **HARDEN** | Production hardening (23 findings) | ⏳ Planning |

## Hardening Status
**Task**: 消除综合审计全部 CRITICAL/HIGH/MEDIUM 问题（23 项发现）
**Target**: v0.4.0
**Specs**: 
- 审计: `docs/analysis/hardening-audit.md`
- 任务分解: `docs/plan/task-breakdown-hardening.md`
- 依赖图: `docs/plan/dependency-graph-hardening.md`
- 里程碑: `docs/plan/milestones-hardening.md`

| Phase | Name | Status |
|:------|:-----|:-------|
| H1 | Critical Fixes (4 tasks) | Pending |
| H2 | Security & Reliability (7 tasks) | Pending |
| H3 | Observability & Tests (5 tasks) | Pending |
| H4 | Polish (5 tasks) | Pending |
| H5 | Release (4 tasks) | Pending |

## CI/CD Status
- CI: ✅ (build + test-sqlite + test-pg + lint)
- CD: ✅ (ghcr.io/tokendancelab/metapi-go:latest)
- Release: v0.3.0

## Next Steps
1. 用户确认 H1-H5 执行计划
2. 按 Phase 1→2→3→4→5 顺序执行
3. 全量验证 → CI 全绿 → tag v0.4.0
