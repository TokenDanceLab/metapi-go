# Task Breakdown — MetAPI Go Rewrite

> **Target**: 完整 Go 重写 MetAPI 后端，功能/稳定性/边界条件 ≥ TS 版，性能 >> TS 版
> **约束**: API 响应格式、DB schema、env var 命名 100% 兼容。前端 React SPA 不做任何改动。
> **架构**: chi router + sqlx + go:embed 单二进制。S.U.P.E.R 合规: tokenRouter 拆分, 循环依赖消除, 接口契约化。

---

## P0: Go 项目骨架 + 全量 config + chi router + graceful shutdown + Docker

**Priority**: P0 | **Size**: M (~1 day) | **Lane**: Solo (no deps)
**S.U.P.E.R Drivers**: E (环境无关), R (可替换部件)

### 功能规格

```
metapi-go/
├── cmd/server/main.go          # 入口: config加载 → DB打开 → migration → 注册路由 → listen
├── config/config.go             # ~100 env vars, 等同 TS config.ts
├── config/defaults.go           # 默认值常量
├── router/router.go             # chi router 组装: middleware → admin API → proxy API → SPA fallback
├── router/middleware.go         # 请求日志, CORS, recovery, rate limit 骨架
├── app/app.go                   # App struct: 生命周期管理 (Start/Shutdown)
├── app/health.go                # /health 端点
├── Dockerfile                   # 多阶段: go build + frontend build → scratch 单二进制 ~15MB
├── docker-compose.yml           # 开发/测试 compose, 挂载 web/dist + data/
├── .goreleaser.yml              # (可选) 发布自动化
├── Makefile                     # build / test / lint / docker-build / run
└── go.mod                       # module github.com/tokendancelab/metapi-go
```

### Config 全量映射 (TS config.ts → Go config.go)

config 优先级: **环境变量 > .env 文件 > 代码默认值**

| 分类 | 环境变量 | 类型 | 默认值 | Go struct field |
|------|----------|------|--------|-----------------|
| Auth | `AUTH_TOKEN` | string | `change-me-admin-token` | `AuthToken` |
| | `PROXY_TOKEN` | string | `change-me-proxy-sk-token` | `ProxyToken` |
| | `DEPLOY_HELPER_TOKEN` | string | `""` | `DeployHelperToken` |
| | `ACCOUNT_CREDENTIAL_SECRET` | string | fallback→AUTH_TOKEN | `AccountCredentialSecret` |
| OAuth | `CODEX_CLIENT_ID` | string | `CODEX_CLIENT_ID_PLACEHOLDER` | `CodexClientID` |
| | `CLAUDE_CLIENT_ID` | string | `9d1c250a-...` | `ClaudeClientID` |
| | `CLAUDE_CLIENT_SECRET` | string | `""` | `ClaudeClientSecret` |
| | `GEMINI_CLI_CLIENT_ID` | string | placeholder | `GeminiCliClientID` |
| | `GEMINI_CLI_CLIENT_SECRET` | string | placeholder | `GeminiCliClientSecret` |
| Network | `PORT` | int | `4000` | `Port` |
| | `HOST` | string | `0.0.0.0` | `ListenHost` |
| | `SYSTEM_PROXY_URL` | string | `""` | `SystemProxyURL` |
| | `DATA_DIR` | string | `./data` | `DataDir` |
| DB | `DB_TYPE` | string | `sqlite` | `DBType` (sqlite/postgres) |
| | `DB_URL` | string | `""` | `DBURL` |
| | `DB_SSL` | bool | `false` | `DBSSL` |
| Cron | `CHECKIN_CRON` | string | `0 8 * * *` | `CheckinCron` |
| | `CHECKIN_SCHEDULE_MODE` | string | `cron` | `CheckinScheduleMode` (cron/interval) |
| | `CHECKIN_INTERVAL_HOURS` | int | `6` (1-24 clamp) | `CheckinIntervalHours` |
| | `BALANCE_REFRESH_CRON` | string | `0 * * * *` | `BalanceRefreshCron` |
| | `LOG_CLEANUP_CRON` | string | `0 6 * * *` | `LogCleanupCron` |
| Cleanup | `LOG_CLEANUP_USAGE_LOGS_ENABLED` | bool | `false` | `LogCleanupUsageLogsEnabled` |
| | `LOG_CLEANUP_PROGRAM_LOGS_ENABLED` | bool | `false` | `LogCleanupProgramLogsEnabled` |
| | `LOG_CLEANUP_RETENTION_DAYS` | int | `30` | `LogCleanupRetentionDays` |
| Notify | `WEBHOOK_URL` | string | `""` | `WebhookURL` |
| | `WEBHOOK_ENABLED` | bool | `true` | `WebhookEnabled` |
| | `BARK_URL` | string | `""` | `BarkURL` |
| | `BARK_ENABLED` | bool | `true` | `BarkEnabled` |
| | `SERVERCHAN_ENABLED` | bool | `true` | `ServerchanEnabled` |
| | `SERVERCHAN_KEY` | string | `""` | `ServerchanKey` |
| | `TELEGRAM_ENABLED` | bool | `false` | `TelegramEnabled` |
| | `TELEGRAM_BOT_TOKEN` | string | `""` | `TelegramBotToken` |
| | `TELEGRAM_CHAT_ID` | string | `""` | `TelegramChatID` |
| | `TELEGRAM_USE_SYSTEM_PROXY` | bool | `false` | `TelegramUseSystemProxy` |
| | `TELEGRAM_MESSAGE_THREAD_ID` | string | `""` | `TelegramMessageThreadID` |
| | `SMTP_ENABLED` | bool | `false` | `SMTPEnabled` |
| | `SMTP_HOST` | string | `""` | `SMTPHost` |
| | `SMTP_PORT` | int | `587` | `SMTPPort` |
| | `SMTP_SECURE` | bool | `false` | `SMTPSecure` |
| | `SMTP_USER` | string | `""` | `SMTPUser` |
| | `SMTP_PASS` | string | `""` | `SMTPPass` |
| | `SMTP_FROM` | string | `""` | `SMTPFrom` |
| | `SMTP_TO` | string | `""` | `SMTPTo` |
| | `NOTIFY_COOLDOWN_SEC` | int | `300` | `NotifyCooldownSec` |
| Security | `ADMIN_IP_ALLOWLIST` | csv | `""` | `AdminIPAllowlist` |
| Proxy Engine | `REQUEST_BODY_LIMIT` | const | `20MB` | (constant) |
| | `ROUTING_FALLBACK_UNIT_COST` | float | `1` | `RoutingFallbackUnitCost` |
| | `PROXY_FIRST_BYTE_TIMEOUT_SEC` | int | `0` | `ProxyFirstByteTimeoutSec` |
| | `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` | int | `2592000` (30d) | `TokenRouterFailureCooldownMaxSec` |
| | `TOKEN_ROUTER_CACHE_TTL_MS` | int | `1500` | `TokenRouterCacheTTLMs` |
| | `PROXY_MAX_CHANNEL_ATTEMPTS` | int | `3` | `ProxyMaxChannelAttempts` |
| | `PROXY_STICKY_SESSION_ENABLED` | bool | `true` | `ProxyStickySessionEnabled` |
| | `PROXY_STICKY_SESSION_TTL_MS` | int | `1800000` (30min) | `ProxyStickySessionTTLMs` |
| | `PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT` | int | `2` | `ProxySessionChannelConcurrencyLimit` |
| | `PROXY_SESSION_CHANNEL_QUEUE_WAIT_MS` | int | `1500` | `ProxySessionChannelQueueWaitMs` |
| | `PROXY_SESSION_CHANNEL_LEASE_TTL_MS` | int | `90000` | `ProxySessionChannelLeaseTTLMs` |
| | `PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS` | int | `15000` | `ProxySessionChannelLeaseKeepaliveMs` |
| | `CODEX_UPSTREAM_WEBSOCKET_ENABLED` | bool | `false` | `CodexUpstreamWebsocketEnabled` |
| | `RESPONSES_COMPACT_FALLBACK` | bool | `false` | `ResponsesCompactFallbackEnabled` |
| | `DISABLE_CROSS_PROTOCOL_FALLBACK` | bool | `false` | `DisableCrossProtocolFallback` |
| Debug | `PROXY_DEBUG_TRACE_ENABLED` | bool | `false` | `ProxyDebugTraceEnabled` |
| | `PROXY_DEBUG_CAPTURE_HEADERS` | bool | `true` | `ProxyDebugCaptureHeaders` |
| | `PROXY_DEBUG_CAPTURE_BODIES` | bool | `false` | `ProxyDebugCaptureBodies` |
| | `PROXY_DEBUG_CAPTURE_STREAM_CHUNKS` | bool | `false` | `ProxyDebugCaptureStreamChunks` |
| | `PROXY_DEBUG_TARGET_SESSION_ID` | string | `""` | `ProxyDebugTargetSessionID` |
| | `PROXY_DEBUG_TARGET_CLIENT_KIND` | string | `""` | `ProxyDebugTargetClientKind` |
| | `PROXY_DEBUG_TARGET_MODEL` | string | `""` | `ProxyDebugTargetModel` |
| | `PROXY_DEBUG_RETENTION_HOURS` | int | `24` | `ProxyDebugRetentionHours` |
| | `PROXY_DEBUG_MAX_BODY_BYTES` | int | `262144` | `ProxyDebugMaxBodyBytes` |
| Probe | `MODEL_AVAILABILITY_PROBE_ENABLED` | bool | `false` | `ModelAvailabilityProbeEnabled` |
| | `MODEL_AVAILABILITY_PROBE_INTERVAL_MS` | int | `1800000` | `ModelAvailabilityProbeIntervalMs` |
| | `MODEL_AVAILABILITY_PROBE_TIMEOUT_MS` | int | `15000` | `ModelAvailabilityProbeTimeoutMs` |
| | `MODEL_AVAILABILITY_PROBE_CONCURRENCY` | int | `1` | `ModelAvailabilityProbeConcurrency` |
| Retention | `PROXY_LOG_RETENTION_DAYS` | int | `30` | `ProxyLogRetentionDays` |
| | `PROXY_LOG_RETENTION_PRUNE_INTERVAL_MIN` | int | `30` | `ProxyLogRetentionPruneIntervalMin` |
| | `PROXY_FILE_RETENTION_DAYS` | int | `30` | `ProxyFileRetentionDays` |
| | `PROXY_FILE_RETENTION_PRUNE_INTERVAL_MIN` | int | `60` | `ProxyFileRetentionPruneIntervalMin` |
| | `PROXY_ERROR_KEYWORDS` | csv | `""` | `ProxyErrorKeywords` |
| | `PROXY_EMPTY_CONTENT_FAIL` | bool | `false` | `ProxyEmptyContentFailEnabled` |
| Routing Weights | `BASE_WEIGHT_FACTOR` | float | `0.5` | `BaseWeightFactor` |
| | `VALUE_SCORE_FACTOR` | float | `0.5` | `ValueScoreFactor` |
| | `COST_WEIGHT` | float | `0.4` | `CostWeight` |
| | `BALANCE_WEIGHT` | float | `0.3` | `BalanceWeight` |
| | `USAGE_WEIGHT` | float | `0.3` | `UsageWeight` |
| Misc | `OPENAI_SERVICE_TIER_RULES` | JSON | `""` | `OpenAIServiceTierRules` |
| | `PAYLOAD_RULES` | JSON | `""` | `PayloadRules` |
| | `GLOBAL_BLOCKED_BRANDS` | JSON array | `[]` | `GlobalBlockedBrands` |
| | `GLOBAL_ALLOWED_MODELS` | JSON array | `[]` | `GlobalAllowedModels` |
| | `CODEX_RESPONSES_WEBSOCKET_BETA` | string | `responses_websockets=2026-02-06` | `CodexResponsesWebsocketBeta` |
| | `CODEX_HEADER_DEFAULTS_USER_AGENT` | string | `""` | `CodexHeaderDefaultsUserAgent` |
| | `CODEX_HEADER_DEFAULTS_BETA_FEATURES` | string | `""` | `CodexHeaderDefaultsBetaFeatures` |

### Acceptence Criteria
- [ ] `go build ./cmd/server` 产出可执行二进制
- [ ] `./metapi` 启动后 `curl /health` 返回 `{"status":"ok"}`
- [ ] 所有 ~100 个 env var 可通过 `METAPI_` 前缀设置 (或保持原名以兼容)
- [ ] `--help` 打印所有配置项说明
- [ ] SIGTERM/SIGINT graceful shutdown (5s timeout), 关闭 HTTP server + DB pool
- [ ] `Dockerfile` 多阶段构建产出 <20MB 镜像
- [ ] `make build` / `make test` / `make docker-build` 可用
- [ ] logger 结构化日志 (slog), 日志输出到 stdout (12-factor)
- [ ] chi router 注册顺序: middleware stack → /health → /api/* (admin) → /v1/* (proxy) → /* (SPA fallback)

### Test Requirements
- `config/config_test.go`: 每个 env var 的 parse 逻辑测试 (bool/int/float/csv/JSON)
- `config/config_test.go`: 默认值测试
- `app/app_test.go`: graceful shutdown 测试
- `router/router_test.go`: 路由注册顺序验证

### Documentation
- `README.md`: 项目简介, 快速开始, 构建说明
- `docs/deployment.md`: Docker 部署指南 (替代 TS 版 Dockerfile.slim 说明)
