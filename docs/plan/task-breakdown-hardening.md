# Task Breakdown — MetAPI-Go Production Hardening

**目标**: 消除所有审计发现的 CRITICAL/HIGH/MEDIUM 问题，达到生产就绪状态
**跟踪模式**: LOCAL_ONLY
**版本目标**: v0.4.0

## Overview

- **总阶段**: 5
- **总任务**: 18
- **预估工时**: ~8h（并行化后 ~4h wall clock）

## S.U.P.E.R Design Constraints

- **S (Single Purpose)**: 每个修复只解决一个问题。新增中间件独立文件。
- **U (Unidirectional Flow)**: 数据流 handler→service→store。context 沿调用链单向传递。
- **P (Ports over Implementation)**: `RuntimeExecutor` 接口已定义，只需接入；`APIError` 类型先定义再使用。
- **E (Environment-Agnostic)**: 所有配置从 env 读取。密钥默认值在 config.Validate() 中告警，不改变环境变量名。
- **R (Replaceable Parts)**: http.Client 通过依赖注入替换，不硬编码。

## Testing and Governance Constraints

- **测试默认开**: 所有行为变更必须加测试。新增中间件必须有单元测试。
- **显式豁免**: 纯配置/文档变更可豁免，但需注明最近验证命令。
- **指令面更新**: 改 CI 流程后更新 AGENTS.md 的 "Before pushing" 部分。
- **记忆更新**: 密钥管理、应急操作等新规则写入 memory。

---

## Phase 1: 🔴 Critical Fixes（消灭线上可触发故障）

**Goal**: 修复 4 个 CRITICAL——代理出口无超时、panic 崩进程、弱密钥默认值、流截断
**Prerequisite**: 无
**S.U.P.E.R Focus**: R — RuntimeExecutor 接入是可替换性的经典案例；E — 密钥默认值从环境读取

| # | Task | Priority | Effort | Deps | Lane | S.U.P.E.R | Test | Acceptance |
|:--|:-----|:---------|:-------|:-----|:-----|:----------|:-----|:-----------|
| 1.1 | 代理出口接入 RuntimeExecutor（替换 http.DefaultClient） | P0 | M | — | A | R, U | 加 `TestDispatchUpstream_Timeout`：模拟慢上游，验证超时返回 504 而非永久挂起 | `go test ./handler/proxy/ -run TestDispatch -count=1 -race` 全绿；proxy 路径不再引用 `http.DefaultClient` |
| 1.2 | 消除 6 处 OAuth panic → return error + slog.Error | P0 | S | — | A | E, S | 加 `TestOAuthMissingClientID_ReturnsError`：缺 CLIENT_ID 时返回 500 JSON 而非崩溃 | `grep -r 'panic(' service/oauth/ | wc -l` → 0（除测试） |
| 1.3 | 默认密钥生产告警 + AES 密钥独立 | P0 | S | — | A | E, R | 加 `TestConfigValidate_WarnOnDefaultTokens`：用默认 token 启动时 slog.Warn 输出包含 "UNSAFE" | `AUTH_TOKEN` 未设置时 `config.Validate()` 输出 WARN 级别日志；`ACCOUNT_CREDENTIAL_SECRET` 有独立默认值不再 fallback 到 AUTH_TOKEN |
| 1.4 | SSE 流路径设置 `WriteTimeout: 0`（仅流式 handler） | P0 | S | — | B | E | e2e 测试：模拟 >60s SSE 流确认不断开 | `go test ./e2e/ -run TestLongStream -count=1` 通过 |

### Parallel Lanes

| Lane | Tasks | Effort | Merge Risk | Key Files |
|:-----|:------|:-------|:-----------|:----------|
| A | 1.1, 1.2, 1.3 | M+S+S | 🟢 Low（不同文件） | upstream.go, oauth/*.go, config/defaults.go |
| B | 1.4 | S | 🟢 Low | app/app.go, handler/proxy/upstream.go |

---

## Phase 2: 🟡 Security & Reliability（安全/可靠性加固）

**Goal**: 常量时间比较、CI 加固、DB 连接池、slog 统一、Request ID、context 传递、re-panic 修复
**Prerequisite**: Phase 1
**S.U.P.E.R Focus**: E — 安全配置环境化；U — context 单向传递

| # | Task | Priority | Effort | Deps | Lane | S.U.P.E.R | Test | Acceptance |
|:--|:-----|:---------|:-------|:-----|:-----|:----------|:-----|:-----------|
| 2.1 | admin/proxy token 比较改用 `subtle.ConstantTimeCompare` | P1 | S | — | A | E | 加 benchmark 对比 `!=` vs `ConstantTimeCompare` | auth 包中 `!=` 替换为 `subtle.ConstantTimeCompare` |
| 2.2 | CI 修复：启用 linter + 加 `-race` + lint 不再 continue-on-error | P1 | M | — | A | — | CI 绿需 lint pass + `-race` pass | `.golangci.yml` 启用 errcheck/staticcheck；`ci.yml` lint job 去掉 `continue-on-error`，test job 加 `-race`；新增 vet standalone step |
| 2.3 | DB 连接池补齐 `ConnMaxLifetime`(5min) + `ConnMaxIdleTime`(2min) | P1 | S | — | A | E | 已有 PG 集成测试覆盖连接 | `store/open.go` PG 分支有完整连接池参数 |
| 2.4 | 13 处 `log.Printf` → `slog.Error/Warn` | P1 | S | — | A | S | 无需新测试（纯日志变更） | `grep -r 'log\.Printf\|log\.Println\|log\.Print(' service/ | wc -l` → 0 |
| 2.5 | Request ID 中间件 + RequestLogger 加 request_id 字段 | P1 | S | — | B | S, U | 加 `TestRequestID_Propagates`：验证 X-Request-Id header 和 context 传递 | curl 响应头含 `X-Request-Id`；slog 日志含 `request_id` 字段 |
| 2.6 | OAuth 路径 `context.Background()` → 请求 context | P1 | M | — | B | U, E | 已有 OAuth 测试覆盖 | `grep -r 'context.Background()' service/oauth/ | wc -l` → 0（除测试和 init） |
| 2.7 | usage_aggregation re-panic 修复（不重新 panic，只 log） | P1 | S | — | B | S | 加 `TestUsageAggregation_PanicRecovery`：模拟 goroutine panic 不崩进程 | scheduler goroutine 内的 recover 记录错误后正常退出，不 `panic(r)` |

### Parallel Lanes

| Lane | Tasks | Effort | Merge Risk | Key Files |
|:-----|:------|:-------|:-----------|:----------|
| A | 2.1, 2.2, 2.3, 2.4 | S+M+S+S | 🟡 Med（auth + ci + store 不同文件但都在核心层） | auth/admin.go, auth/proxy.go, .golangci.yml, ci.yml, store/open.go |
| B | 2.5, 2.6, 2.7 | S+M+S | 🟢 Low | router/middleware.go, router/request_id.go, service/oauth/*.go, scheduler/usage_aggregation.go |

---

## Phase 3: 🟠 Observability & Test Coverage（可观测性 + 测试补全）

**Goal**: 补齐零覆盖包测试、提升 admin 覆盖率、Prometheus 端点、安全头、错误模式
**Prerequisite**: Phase 2
**S.U.P.E.R Focus**: P — 错误响应接口先定义；R — 中间件可替换

| # | Task | Priority | Effort | Deps | Lane | S.U.P.E.R | Test | Acceptance |
|:--|:-----|:---------|:-------|:-----|:-----|:----------|:-----|:-----------|
| 3.1 | 8 个零覆盖包补齐基础测试 | P1 | L | — | A | S, P | 每个包加 *_test.go | `go test ./router/ ./transform/openai/... ./proxy/profiles/ ./service/adapter/ -cover` 全部 ≥40% |
| 3.2 | handler/admin 覆盖率 26.3% → 40%+ | P1 | L | — | A | S | httptest + SQLite :memory: 全链路 | `go test ./handler/admin/ -cover` ≥40% |
| 3.3 | `/metrics` Prometheus 端点（零依赖 text format） | P2 | M | — | B | P, E | 加 `TestMetrics_Format`：验证 text/plain Prometheus 格式 | `curl :4000/metrics` 返回 metapi_* 指标 |
| 3.4 | 安全响应头中间件 | P2 | S | — | B | E, R | 加 `TestSecurityHeaders`：验证 5 个安全头 | curl 响应含 X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP, HSTS |
| 3.5 | 结构化错误响应 `APIError` 类型 + `writeError()` helper | P2 | M | — | B | P, S | 加 `TestAPIError_JSON`：验证 error → JSON 序列化 | handler 中 `raw error.Error()` 全部替换为 `writeError(w, code, msg)` |

### Parallel Lanes

| Lane | Tasks | Effort | Merge Risk | Key Files |
|:-----|:------|:-------|:-----------|:----------|
| A | 3.1, 3.2 | L+L | 🟡 Med（文件多） | router/*_test.go, transform/openai/**/*_test.go, handler/admin/*_test.go |
| B | 3.3, 3.4, 3.5 | M+S+M | 🟢 Low | app/metrics.go, router/security.go, handler/shared/errors.go |

---

## Phase 4: 🟢 Polish（代码整洁 + 安全收尾）

**Goal**: TODO 清零、CORS 锁定、expvar 加固、文件拆分
**Prerequisite**: Phase 3
**S.U.P.E.R Focus**: S — 拆分单体文件

| # | Task | Priority | Effort | Deps | Lane | S.U.P.E.R | Test | Acceptance |
|:--|:-----|:---------|:-------|:-----|:-----|:----------|:-----|:-----------|
| 4.1 | 18 个 TODO/FIXME 清理（删已完成 + 标注 STUB + 保留真 TODO） | P2 | S | — | A | S | 纯注释变更 | `grep -r 'TODO\|FIXME' --include='*.go' . | grep -v '_test'` 仅余真待办项 ≤6 个 |
| 4.2 | CORS 锁定：`/api/*` 用受限 origin，proxy 用通配符 | P2 | S | — | A | E | 已有 CORS 中间件测试 | admin 路由组独立 CORS 中间件；proxy 路由组保持现有 |
| 4.3 | `/debug/vars` 移入 auth 保护（或 build tag 可选） | P2 | S | — | A | E | 无需新测试 | `curl :4000/debug/vars` 返回 401（无 token 时） |
| 4.4 | `chatFormatsCore.go` 拆分（openai/anthropic/gemini 各自 format 文件） | P2 | M | — | B | S | 已有 transform 测试 | `go build ./... && go test ./transform/... -count=1` 全绿；文件从 1667 行拆至 ≤600 行/文件 |
| 4.5 | Go 1.25 PGO 在 Docker 构建中启用（条件：default.pgo 存在） | P2 | S | — | B | — | 纯构建变更 | Dockerfile 添加 `COPY default.pgo* .` + `-pgo=auto` |

### Parallel Lanes

| Lane | Tasks | Effort | Merge Risk | Key Files |
|:-----|:------|:-------|:-----------|:----------|
| A | 4.1, 4.2, 4.3 | S+S+S | 🟢 Low | handler/admin/*.go, router/middleware.go, router/router.go |
| B | 4.4, 4.5 | M+S | 🟢 Low（不同目录） | transform/shared/*.go, Dockerfile |

---

## Phase 5: 🚀 Release

**Goal**: 全量验证 + tag v0.4.0
**Prerequisite**: Phase 1-4 全部完成

| # | Task | Priority | Effort | Deps | Acceptance |
|:--|:-----|:---------|:-------|:-----|:-----------|
| 5.1 | 全量验证：`go build ./... && go vet ./... && go test ./... -count=1 -race` | P0 | S | P4 | 全部通过 |
| 5.2 | 验证 CI/CD：push → GitHub Actions 全绿 | P0 | S | 5.1 | CI 3 job 全部通过（lint + test-sqlite + test-pg + build） |
| 5.3 | 更新 AGENTS.md（添加 `-race` 到 pre-push checklist） | P2 | S | 5.2 | AGENTS.md "Before pushing" 包含 `-race` |
| 5.4 | Tag v0.4.0 + CD 发布 GHCR | P1 | S | 5.2 | `gh release create v0.4.0 --generate-notes`；ghcr.io 有新镜像 |
