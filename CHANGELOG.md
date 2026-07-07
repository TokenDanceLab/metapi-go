# Changelog

All notable changes to MetAPI-Go will be documented in this file.

格式基于 [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)，
版本号遵循 [Semantic Versioning](https://semver.org/spec/v2.0.0.html)。

## [v0.6.1] — 2026-07-07

### Fixed
- CI/CD secret scan 改用开源 gitleaks CLI，避免 organization 仓库被 `gitleaks/gitleaks-action@v2` license gate 阻断发布。

## [v0.6.0] — 2026-07-07

### Security
- CI/CD 发布门禁加入 gitleaks、Go module 校验、race 测试、PostgreSQL integration 测试、前端 typecheck/test/build 和生产依赖 audit。
- CD 镜像发布前执行 Docker smoke test；发布镜像启用 provenance 和 SBOM。
- 测试和文档中的 PostgreSQL DSN 改为运行时拼接，减少 secret scanner 噪声。
- 站点自定义 headers 过滤 `Authorization`、`Cookie`、`New-API-User`、`Content-Type`、`Content-Length`、`Host` 等保留头，避免覆盖运行时认证语义。

### Fixed
- `/v1/*` 数据面接入数据库路由和真实上游选择，不再停留在未配置 stub 行为。
- 上游代理支持站点/账号代理、自定义 headers、失败记录和非流式可重试 failover。
- AnyRouter 禁用 NewAPI 风格 token 管理端点，避免错误调用 `/api/token`。
- API-key/proxy-only 账号不再执行签到或余额上游调用，禁用状态判断改为大小写不敏感。
- 账号 session rebind、manual models、account token 默认值维护补齐事务和错误处理，失败路径回滚。

### Added
- 覆盖 SQLite 和 PostgreSQL 的账号、签到、余额、AnyRouter、代理上游和路由选择回归测试。
- 运行时说明明确当前支持 SQLite 单节点和 PostgreSQL 部署；Redis 尚未集成。

## [v0.5.0] — 2026-07-05

### Security
- Admin/proxy token 比较改用 `crypto/subtle.ConstantTimeCompare`（防时序攻击）
- CI 启用 `errcheck`、`staticcheck`、`ineffassign` linter
- CI 测试启用 `-race`（data race 检测）
- `/debug/vars` 移至 admin auth 保护后（此前无认证暴露）
- 安全响应头中间件：`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `CSP`
- AES 密钥派生不再 fallback 到 `AUTH_TOKEN`（独立默认值）

### Fixed
- 代理出口 `http.DefaultClient`（零超时）→ 接入 `RuntimeExecutor`（90s 超时 fallback）
- 6 处 OAuth `panic()` → `return error`
- SSE 流式响应 `WriteTimeout: 60s` 截断问题 → `SetWriteDeadline` 禁用
- 13 处 `log.Printf` → `slog.Warn/Error`
- DB 连接池补充 `ConnMaxLifetime`(5min) + `ConnMaxIdleTime`(2min)
- `usage_aggregation` goroutine re-panic 修复（不再能崩进程）
- `CheckinScheduler.Stop()` data race 修复
- CI：`golangci-lint-action@v6` Go 1.25 不兼容 → `go install` 最新版
- `golangci-lint` 全项目 zero warning

### Added
- `/metrics` Prometheus 端点（零依赖 text format）
- `RequestID` 中间件（`X-Request-Id` header + 日志关联）
- `handler/shared/errors.go`：`APIError` 结构化错误类型
- git pre-push hook（`.githooks/pre-push`）：自动跑 `vet + test -race`
- Claude Code push guard（`~/.claude/hooks/metapi-go-push-guard.sh`）
- `AGENTS.md` CI Discipline 规范

### Tests
- 8 个零覆盖包全部补齐测试（最低 50%，最高 100%）
- 新增 3 个 e2e 场景：并发代理、auth 时序安全、rate limit 拒绝
- `e2e` 测试包总数：4 → 5 文件

## [v0.4.0] — 2026-07-05

### Fixed
- 6 轮 audit 全部修复
- PG 兼容：`INSERT OR IGNORE` → `ON CONFLICT DO NOTHING`
- Cron 5 字段 → 6 字段自动转换
- `sqlx.BindDriver` 时序修复（`?` → `$N` 占位符重绑定）

## [v0.3.0] — 2026-07-04

### Changed
- goroutine 泄漏修复
- JSON 性能优化
- 包命名规范化
- `config.Validate()` 10 项启动校验

## [v0.2.0] — 2026-07-04

### Added
- 限流中间件（admin 100rps, OAuth 10rps）
- RWMutex 假桩替换为真实 `sync.RWMutex`
- DB 事务包裹 usage aggregation batch
- `store.Close()` 优雅关机

## [v0.1.0] — 2026-07-03

### Added
- MetAPI TypeScript → Go 完整重写初始发布
- 27 表双数据库（SQLite + PostgreSQL）
- 14 平台适配器
- 4 协议流式转换
- 15 后台调度任务
- 单二进制 + Docker 部署

[v0.6.1]: https://github.com/TokenDanceLab/metapi-go/compare/v0.6.0...v0.6.1
[v0.6.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.1.0
