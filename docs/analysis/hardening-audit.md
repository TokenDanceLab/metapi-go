# Production Hardening — 风险审计

**审计日期**: 2026-07-05 | **审计源**: 综合审计（自分析 + 2x Explore Agent + 3x grok-search 2026 Go 最佳实践）

## 审计方法

| 维度 | Agent | 发现 |
|------|-------|------|
| 功能对齐 | Compare Agent | ~95% 对齐，19 路由组/11 Proxy/15 scheduler/27 表一致。2 遗漏：WS stub、acw_sc__v2 |
| 代码质量 | Audit Agent | http.DefaultClient 死代码、弱密钥默认值、WriteTimeout 截流、CI 残废 |
| 自分析 | 主会话 | 6 panic、13 log.Printf、8 零覆盖包、context 泄露、DB 池缺失 |
| 最佳实践 | grok-search ×3 | RequestID 中间件、结构化错误、PGO、安全头、-race |

## 发现汇总

### 🔴 CRITICAL（线上可触发故障）

| # | 发现 | 位置 | 影响 |
|---|------|------|------|
| C1 | 代理出口走 `http.DefaultClient`（零超时） | `handler/proxy/upstream.go:96` | 上游卡住→goroutine 永久泄漏；`RuntimeExecutor` 已实现但未接入 |
| C2 | 6 处 OAuth panic 在请求路径中 | `service/oauth/*.go` | 一个 OAuth 配置错误崩掉整个进程 |
| C3 | 默认密钥 `change-me-*` 无生产告警 | `config/defaults.go:7-8` | 忘记设 AUTH_TOKEN → admin API 全裸 |
| C4 | `WriteTimeout: 60s` 截断长 LLM 流 | `app/app.go:56` | 超过 60s 的推理/流式响应连接被硬断 |

### 🟡 HIGH（安全/可靠性）

| # | 发现 | 位置 |
|---|------|------|
| H1 | admin token 比较 `!=` 非 constant-time | `auth/admin.go:59` |
| H2 | CI lint 禁用 errcheck/staticcheck/ineffassign + continue-on-error + 无 `-race` | `.golangci.yml` + `ci.yml` |
| H3 | DB 连接池缺 `ConnMaxLifetime` / `ConnMaxIdleTime` | `store/open.go:202` |
| H4 | 13 处 `log.Printf` 残留 → 应为 slog | `service/oauth/*.go` |
| H5 | 12+ 处 `context.Background()` 在 OAuth 路径 | `service/oauth/*.go` |
| H6 | 无 Request ID 中间件（无法追踪请求链路） | `router/router.go` |
| H7 | usage_aggregation goroutine re-panic 可崩进程 | `scheduler/usage_aggregation.go:141` |
| H8 | 默认 AES 加密密钥 = `SHA-256("change-me-admin-token")` | `service/account_credential.go:28` |

### 🟠 MEDIUM

| # | 发现 |
|---|------|
| M1 | 8 包零测试覆盖（router, 5×openai/transform, proxy/profiles, service/adapter） |
| M2 | handler/admin 覆盖率仅 26.3%（最大 handler 包） |
| M3 | 无 `/metrics` Prometheus 端点 |
| M4 | 结构化错误响应模式缺失（裸 `error.Error()` 返回客户端） |
| M5 | 安全响应头缺失（X-Content-Type-Options, X-Frame-Options, CSP） |
| M6 | 18 个 TODO/FIXME 待清理 |
| M7 | CORS `AllowedOrigins: ["*"]` 应用于全局含 /api/* |
| M8 | `/debug/vars` expvar 无认证暴露 cmdline+memstats |
| M9 | SSE 流缓冲全响应体在内存中（`bytes.Buffer`） |

### 🟢 LOW

| # | 发现 |
|---|------|
| L1 | `chatFormatsCore.go` 1667 行单体文件 |
| L2 | Go 1.25 PGO 未启用 |
| L3 | Responses WebSocket 为 STUB（TS 有完整实现） |
| L4 | newApiShield acw_sc__v2 求解器不可用（Go 无 JS 引擎） |

## 合并优先级

所有 CRITICAL + HIGH 必须在 tag v0.4.0 前修复。MEDIUM 尽量修，至少 M1（零覆盖包）必做。LOW 可以 backlog。

**修复顺序**：C1（代理出口）→ C3（密钥默认值）→ C2（消除 panic）→ C4（WriteTimeout）→ H1-H8 → M1-M9 → L1-L4
