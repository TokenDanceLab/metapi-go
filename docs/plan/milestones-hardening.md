# Milestones — Production Hardening

> **Status: closed / superseded** (original v0.4.0 target; product is **v0.8.39+**). Do not treat M1–M5 as open production board. 现状: `docs/STATE.md` · 开放门禁: `docs/progress/MASTER.md`. Residual train: `docs/analysis/residual-next-candidates.md` + `CHANGELOG.md`.

| # | Milestone | After | Criteria | Status |
|:--|:----------|:------|:---------|:-------|
| M1 | **Critical Fixes Done** | Phase 1 | `http.DefaultClient` 零引用；`panic(` 在 `service/oauth/` 零出现；默认密钥生产告警；SSE >60s 不断 | Pending |
| M2 | **Security Baseline** | Phase 2 | ConstantTimeCompare 用于 token 比较；CI lint 不跳过 + `-race` 开启；slog 统一；Request ID 传播 | Pending |
| M3 | **Observability Ready** | Phase 3 | 8 零覆盖包 ≥40%；admin ≥40%；`/metrics` 端点在线；安全响应头就绪；APIError 模式规范 | Pending |
| M4 | **Code Polish** | Phase 4 | TODO ≤6 个；CORS 区分 admin/proxy；debug/vars 受保护；chatFormatsCore 拆分完成；PGO 条件启用 | Pending |
| M5 | **Ship v0.4.0** | Phase 5 | `go build + vet + test -race` 全绿；CI 3 job pass；GHCR 新镜像就绪 | Pending |

## 版本路线

| 版本 | Tag | 内容 |
|------|-----|------|
| v0.1.0 | 2026-07-03 | 初始 Go 重写，P0-P13 全部完成 |
| v0.2.0 | 2026-07-04 | 审计修复（限流/优雅关机/RWMutex/DB 事务等） |
| v0.3.0 | 2026-07-05 | PG 兼容修复 + 隐私清理 + CD 打通 |
| **v0.4.0** | **target** | **生产就绪：23 项审计修复 + 测试覆盖率提升 + 可观测性** |
