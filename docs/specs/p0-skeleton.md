# P0: Go 项目骨架 + 全量 Config + Chi Router + Docker

**S.U.P.E.R**: E (环境无关) · R (可替换) | **依赖**: 无 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\config.ts` -- 全量 config (172 行, 88+ 字段)
- `D:\Code\TokenDance\metapi\src\server\index.ts` -- 启动流程 (309 行)
- `D:\Code\TokenDance\metapi\src\server\desktop.ts` -- 公开路由白名单
- `D:\Code\TokenDance\metapi\.env.example` -- env var 清单 (31 行)
- `D:\Code\TokenDance\metapi\Dockerfile.slim` -- TS 构建/部署参考

本 spec 将 Chi router 架构作为 **设计翻译目标**（TS 使用 Fastify）。路由顺序、中间件选择是 Go 生态的等价翻译，非一字一句照搬。

## Go 目录结构

```
cmd/server/main.go          # main: 完整启动流程 (见下方完整步骤)
config/
  config.go                  # Config struct (88+ fields) + Load(env map) + 8 parse functions
  defaults.go                # 所有默认值常量
store/
  bootstrap.go               # ensureRuntimeDatabaseReady
  migrate.go                 # 11 schema 迁移函数 (stub)
  settings.go                # settings 表读写 + runtime hydration
  switch.go                  # switchRuntimeDatabase (含回滚)
router/
  router.go                  # chi.NewRouter() 组装 + 路由注册 + SPA fallback
  middleware.go              # request logger, recovery, RealIP, CORS, auth hook
app/
  app.go                     # App{Config,Store,Router,Server} + Start/Shutdown
  health.go                  # GET /health (设计新增, 非 TS 移植)
  services.go                # 后台服务生命周期 (start/stop stubs)
Dockerfile                   # 多阶段: go build + frontend build -> alpine
docker-compose.yml           # 开发用, 挂载 web/dist + data/
Makefile                     # build test lint docker-build run
```

## 功能规格

---

### 1. Parse 函数规范 (config/config.go)

所有 parse 函数必须与 TS 行为逐字节一致。Go 签名使用 `string`（Go 中零值 `""` 等价于 TS `undefined`）。

#### 1.1 `parseBoolean(value string, fallback bool) bool`
```
"" → fallback
trim + toLower → "1" | "true" | "yes" | "on" → true
其他 → false
```

#### 1.2 `parseNumber(value string, fallback float64) float64`
```
"" → fallback
strconv.ParseFloat → math.IsInf || math.IsNaN → fallback
返回原值 (不 trunc, 由调用方 trunc)
```

#### 1.3 `parseCsvList(value string) []string`
```
"" → []
strings.Split(value, ",") → trim each → filter len>0
```

#### 1.4 `parseOptionalSecret(value string) string`
```
strings.TrimSpace(value)  // "" 保持 ""
```

#### 1.5 `parseJsonValue(value string) any`
```
"" → nil
json.Unmarshal 失败 → nil (不 panic)
```

#### 1.6 `parseDbType(value string) string`
```
"" → "sqlite"
trim + toLower
  "mysql" → "mysql"
  "postgres" | "postgresql" → "postgres"
  其他 → "sqlite"
```

#### 1.7 `normalizeTokenRouterFailureCooldownMaxSec(value float64) (int, bool)`
```
!finite || <= 0 → (0, false)
trunc → clamp[1, 30*24*60*60] → (int, true)
```

#### 1.8 `parseListenHost(env map[string]string) string`
```
env["HOST"] 为空 → "0.0.0.0"
trim → 为空 → "0.0.0.0"
```

---

### 2. 默认值常量 (config/defaults.go)

```go
const (
    DefaultRequestBodyLimit       = 20 * 1024 * 1024  // 20 MB
    DefaultCodexClientId          = "CODEX_CLIENT_ID_PLACEHOLDER"
    DefaultClaudeClientId         = "CLAUDE_CLIENT_ID_PLACEHOLDER"
    DefaultGeminiCliClientId      = "GEMINI_CLI_CLIENT_ID_PLACEHOLDER"
    DefaultGeminiCliClientSecret  = "GEMINI_CLI_CLIENT_SECRET_PLACEHOLDER"
    TokenRouterFailureCooldownMaxSecCeiling = 30 * 24 * 60 * 60  // 30 days
)
```

---

### 3. Config 完整映射 (config/config.go)

`config.Load(env map[string]string)` 读取环境变量 → 返回填充好的 `Config` 结构体。优先级: env > 默认值（无 `.env` 文件自动加载层 -- `cmd/server/main.go` 负责用 `godotenv` 先加载再传入）。

所有 env var 名称与 TS 版 **完全一致** (不添加前缀)。以下是全量字段映射表。Go 类型列是目标 Go 类型；每个字段的构造逻辑必须与 TS `buildConfig()` 逐行一致。

#### 3.1 Auth (5 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 1 | `AUTH_TOKEN` | `AuthToken` | `string` | `env["AUTH_TOKEN"]` or `"change-me-admin-token"` |
| 2 | `PROXY_TOKEN` | `ProxyToken` | `string` | `env["PROXY_TOKEN"]` or `"change-me-proxy-sk-token"` |
| 3 | `DEPLOY_HELPER_TOKEN` (legacy: `UPDATE_CENTER_HELPER_TOKEN`) | `DeployHelperToken` | `string` | `parseOptionalSecret(env["DEPLOY_HELPER_TOKEN"] or env["UPDATE_CENTER_HELPER_TOKEN"])` |
| 4 | `ACCOUNT_CREDENTIAL_SECRET` | `AccountCredentialSecret` | `string` | `env["ACCOUNT_CREDENTIAL_SECRET"]` or `env["AUTH_TOKEN"]` or `"change-me-admin-token"` |
| 5 | `CODEX_CLIENT_ID` | `CodexClientId` | `string` | `parseOptionalSecret(env["CODEX_CLIENT_ID"])` or `DefaultCodexClientId` |

#### 3.2 OAuth Clients (4 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 6 | `CLAUDE_CLIENT_ID` | `ClaudeClientId` | `string` | `parseOptionalSecret(...)` or `DefaultClaudeClientId` |
| 7 | `CLAUDE_CLIENT_SECRET` | `ClaudeClientSecret` | `string` | `parseOptionalSecret(...)` (空字符串 fallback) |
| 8 | `GEMINI_CLI_CLIENT_ID` | `GeminiCliClientId` | `string` | `parseOptionalSecret(...)` or `DefaultGeminiCliClientId` |
| 9 | `GEMINI_CLI_CLIENT_SECRET` | `GeminiCliClientSecret` | `string` | `parseOptionalSecret(...)` or `DefaultGeminiCliClientSecret` |

#### 3.3 Server (7 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 10 | `PORT` | `Port` | `int` | `math.Trunc(parseNumber(env["PORT"], 4000))` |
| 11 | `HOST` | `ListenHost` | `string` | `parseListenHost(env)` |
| 12 | `DATA_DIR` | `DataDir` | `string` | `env["DATA_DIR"]` or `"./data"` |
| 13 | `DB_TYPE` | `DbType` | `string` | `parseDbType(env["DB_TYPE"])` |
| 14 | `DB_URL` | `DbUrl` | `string` | `strings.TrimSpace(env["DB_URL"])` |
| 15 | `DB_SSL` | `DbSsl` | `bool` | `parseBoolean(env["DB_SSL"], false)` |
| 16 | `TZ` | `Tz` | `string` | `env["TZ"]` (无默认; 容器镜像中 tzdata 负责) |

#### 3.4 Cron (5 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 17 | `CHECKIN_CRON` | `CheckinCron` | `string` | `env["CHECKIN_CRON"]` or `"0 8 * * *"` |
| 18 | `CHECKIN_SCHEDULE_MODE` | `CheckinScheduleMode` | `string` | `"interval"` if trim+toLower == "interval", else `"cron"` |
| 19 | `CHECKIN_INTERVAL_HOURS` | `CheckinIntervalHours` | `int` | `clamp[1,24](trunc(parseNumber(env["CHECKIN_INTERVAL_HOURS"], 6)))` |
| 20 | `BALANCE_REFRESH_CRON` | `BalanceRefreshCron` | `string` | `env["BALANCE_REFRESH_CRON"]` or `"0 * * * *"` |
| 21 | `LOG_CLEANUP_CRON` | `LogCleanupCron` | `string` | `env["LOG_CLEANUP_CRON"]` or `"0 6 * * *"` |

#### 3.5 Log Cleanup (4 fields, 1 derived)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 22 | `LOG_CLEANUP_USAGE_LOGS_ENABLED` | `LogCleanupUsageLogsEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 23 | `LOG_CLEANUP_PROGRAM_LOGS_ENABLED` | `LogCleanupProgramLogsEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 24 | `LOG_CLEANUP_RETENTION_DAYS` | `LogCleanupRetentionDays` | `int` | `max(1, trunc(parseNumber(env[...], 30)))` |
| 25 | (derived) | `LogCleanupConfigured` | `bool` | 由 runtime settings 阶段设置 (见启动流程步骤 12) |

#### 3.6 Notify: Webhook (2 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 26 | `WEBHOOK_URL` | `WebhookUrl` | `string` | `env["WEBHOOK_URL"]` or `""` |
| 27 | `WEBHOOK_ENABLED` | `WebhookEnabled` | `bool` | `parseBoolean(env["WEBHOOK_ENABLED"], true)` |

#### 3.7 Notify: Bark (2 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 28 | `BARK_URL` | `BarkUrl` | `string` | `env["BARK_URL"]` or `""` |
| 29 | `BARK_ENABLED` | `BarkEnabled` | `bool` | `parseBoolean(env["BARK_ENABLED"], true)` |

#### 3.8 Notify: ServerChan (2 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 30 | `SERVERCHAN_KEY` | `ServerChanKey` | `string` | `env["SERVERCHAN_KEY"]` or `""` |
| 31 | `SERVERCHAN_ENABLED` | `ServerChanEnabled` | `bool` | `parseBoolean(env["SERVERCHAN_ENABLED"], true)` |

#### 3.9 Notify: Telegram (6 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 32 | `TELEGRAM_ENABLED` | `TelegramEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 33 | (hardcoded) | `TelegramApiBaseUrl` | `string` | `"https://api.telegram.org"` (无 env var) |
| 34 | `TELEGRAM_BOT_TOKEN` | `TelegramBotToken` | `string` | `env[...]` or `""` |
| 35 | `TELEGRAM_CHAT_ID` | `TelegramChatId` | `string` | `env[...]` or `""` |
| 36 | `TELEGRAM_USE_SYSTEM_PROXY` | `TelegramUseSystemProxy` | `bool` | `parseBoolean(env[...], false)` |
| 37 | `TELEGRAM_MESSAGE_THREAD_ID` | `TelegramMessageThreadId` | `string` | `strings.TrimSpace(env[...])` |

#### 3.10 Notify: SMTP (8 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 38 | `SMTP_ENABLED` | `SmtpEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 39 | `SMTP_HOST` | `SmtpHost` | `string` | `env[...]` or `""` |
| 40 | `SMTP_PORT` | `SmtpPort` | `int` | `strconv.Atoi` on `env[...]` or `587` |
| 41 | `SMTP_SECURE` | `SmtpSecure` | `bool` | `parseBoolean(env[...], false)` |
| 42 | `SMTP_USER` | `SmtpUser` | `string` | `env[...]` or `""` |
| 43 | `SMTP_PASS` | `SmtpPass` | `string` | `env[...]` or `""` |
| 44 | `SMTP_FROM` | `SmtpFrom` | `string` | `env[...]` or `""` |
| 45 | `SMTP_TO` | `SmtpTo` | `string` | `env[...]` or `""` |

#### 3.11 Notify: General (2 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 46 | `NOTIFY_COOLDOWN_SEC` | `NotifyCooldownSec` | `int` | `max(0, trunc(parseNumber(env[...], 300)))` |
| 47 | `SYSTEM_PROXY_URL` | `SystemProxyUrl` | `string` | `env["SYSTEM_PROXY_URL"]` or `""` |

#### 3.12 Admin (1 field)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 48 | `ADMIN_IP_ALLOWLIST` | `AdminIpAllowlist` | `[]string` | `parseCsvList(env["ADMIN_IP_ALLOWLIST"])` |

#### 3.13 Proxy: Core (3 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 49 | (hardcoded) | `RequestBodyLimit` | `int` | `DefaultRequestBodyLimit` (20MB, 无 env var) |
| 50 | `ROUTING_FALLBACK_UNIT_COST` | `RoutingFallbackUnitCost` | `float64` | `max(1e-6, parseNumber(env[...], 1))` |
| 51 | `PROXY_FIRST_BYTE_TIMEOUT_SEC` | `ProxyFirstByteTimeoutSec` | `int` | `max(0, trunc(parseNumber(env[...], 0)))` |

#### 3.14 Proxy: Token Router (2 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 52 | `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` | `TokenRouterFailureCooldownMaxSec` | `int` | `normalizeTokenRouterFailureCooldownMaxSec(parseNumber(...))` else `TokenRouterFailureCooldownMaxSecCeiling` |
| 53 | `TOKEN_ROUTER_CACHE_TTL_MS` | `TokenRouterCacheTtlMs` | `int` | `max(100, trunc(parseNumber(env[...], 1500)))` |

#### 3.15 Proxy: Channel (3 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 54 | `PROXY_MAX_CHANNEL_ATTEMPTS` | `ProxyMaxChannelAttempts` | `int` | `max(1, trunc(parseNumber(env[...], 3)))` |
| 55 | `PROXY_STICKY_SESSION_ENABLED` | `ProxyStickySessionEnabled` | `bool` | `parseBoolean(env[...], true)` |
| 56 | `PROXY_STICKY_SESSION_TTL_MS` | `ProxyStickySessionTtlMs` | `int` | `max(30000, trunc(parseNumber(env[...], 30*60*1000)))` |

#### 3.16 Proxy: Session (4 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 57 | `PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT` | `ProxySessionChannelConcurrencyLimit` | `int` | `max(0, trunc(parseNumber(env[...], 2)))` |
| 58 | `PROXY_SESSION_CHANNEL_QUEUE_WAIT_MS` | `ProxySessionChannelQueueWaitMs` | `int` | `max(0, trunc(parseNumber(env[...], 1500)))` |
| 59 | `PROXY_SESSION_CHANNEL_LEASE_TTL_MS` | `ProxySessionChannelLeaseTtlMs` | `int` | `max(5000, trunc(parseNumber(env[...], 90000)))` |
| 60 | `PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS` | `ProxySessionChannelLeaseKeepaliveMs` | `int` | `max(1000, trunc(parseNumber(env[...], 15000)))` |

#### 3.17 Proxy: Misc (6 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 61 | `CODEX_UPSTREAM_WEBSOCKET_ENABLED` | `CodexUpstreamWebsocketEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 62 | `RESPONSES_COMPACT_FALLBACK_TO_RESPONSES_ENABLED` | `ResponsesCompactFallbackToResponsesEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 63 | `DISABLE_CROSS_PROTOCOL_FALLBACK` | `DisableCrossProtocolFallback` | `bool` | `parseBoolean(env[...], false)` |
| 64 | `PROXY_EMPTY_CONTENT_FAIL` | `ProxyEmptyContentFailEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 65 | `PROXY_ERROR_KEYWORDS` | `ProxyErrorKeywords` | `[]string` | `parseCsvList(env["PROXY_ERROR_KEYWORDS"])` |
| 66 | (hardcoded) | `GlobalBlockedBrands` | `[]string` | `[]string{}` (写死空) |
| 67 | (hardcoded) | `GlobalAllowedModels` | `[]string` | `[]string{}` (写死空) |

#### 3.18 Proxy: Debug (9 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 68 | `PROXY_DEBUG_TRACE_ENABLED` | `ProxyDebugTraceEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 69 | `PROXY_DEBUG_CAPTURE_HEADERS` | `ProxyDebugCaptureHeaders` | `bool` | `parseBoolean(env[...], true)` |
| 70 | `PROXY_DEBUG_CAPTURE_BODIES` | `ProxyDebugCaptureBodies` | `bool` | `parseBoolean(env[...], false)` |
| 71 | `PROXY_DEBUG_CAPTURE_STREAM_CHUNKS` | `ProxyDebugCaptureStreamChunks` | `bool` | `parseBoolean(env[...], false)` |
| 72 | `PROXY_DEBUG_TARGET_SESSION_ID` | `ProxyDebugTargetSessionId` | `string` | `strings.TrimSpace(env[...])` |
| 73 | `PROXY_DEBUG_TARGET_CLIENT_KIND` | `ProxyDebugTargetClientKind` | `string` | `strings.TrimSpace(env[...])` |
| 74 | `PROXY_DEBUG_TARGET_MODEL` | `ProxyDebugTargetModel` | `string` | `strings.TrimSpace(env[...])` |
| 75 | `PROXY_DEBUG_RETENTION_HOURS` | `ProxyDebugRetentionHours` | `int` | `max(1, trunc(parseNumber(env[...], 24)))` |
| 76 | `PROXY_DEBUG_MAX_BODY_BYTES` | `ProxyDebugMaxBodyBytes` | `int` | `max(1024, trunc(parseNumber(env[...], 262144)))` |

#### 3.19 Codex-specific (3 fields, 1 nested)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 77 | `CODEX_RESPONSES_WEBSOCKET_BETA` | `CodexResponsesWebsocketBeta` | `string` | `parseOptionalSecret(env[...])` or `"responses_websockets=2026-02-06"` |
| 78 | `CODEX_HEADER_DEFAULTS_USER_AGENT` | `CodexHeaderDefaults.UserAgent` | `string` | `parseOptionalSecret(env[...])` |
| 79 | `CODEX_HEADER_DEFAULTS_BETA_FEATURES` | `CodexHeaderDefaults.BetaFeatures` | `string` | `parseOptionalSecret(env[...])` |

#### 3.20 Model Probe (4 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 80 | `MODEL_AVAILABILITY_PROBE_ENABLED` | `ModelAvailabilityProbeEnabled` | `bool` | `parseBoolean(env[...], false)` |
| 81 | `MODEL_AVAILABILITY_PROBE_INTERVAL_MS` | `ModelAvailabilityProbeIntervalMs` | `int` | `max(60000, trunc(parseNumber(env[...], 30*60*1000)))` |
| 82 | `MODEL_AVAILABILITY_PROBE_TIMEOUT_MS` | `ModelAvailabilityProbeTimeoutMs` | `int` | `max(3000, trunc(parseNumber(env[...], 15000)))` |
| 83 | `MODEL_AVAILABILITY_PROBE_CONCURRENCY` | `ModelAvailabilityProbeConcurrency` | `int` | `clamp[1,16](trunc(parseNumber(env[...], 1)))` |

#### 3.21 Retention (4 fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 84 | `PROXY_LOG_RETENTION_DAYS` | `ProxyLogRetentionDays` | `int` | `max(0, trunc(parseNumber(env[...], 30)))` |
| 85 | `PROXY_LOG_RETENTION_PRUNE_INTERVAL_MINUTES` | `ProxyLogRetentionPruneIntervalMinutes` | `int` | `max(1, trunc(parseNumber(env[...], 30)))` |
| 86 | `PROXY_FILE_RETENTION_DAYS` | `ProxyFileRetentionDays` | `int` | `max(0, trunc(parseNumber(env[...], 30)))` |
| 87 | `PROXY_FILE_RETENTION_PRUNE_INTERVAL_MINUTES` | `ProxyFileRetentionPruneIntervalMinutes` | `int` | `max(1, trunc(parseNumber(env[...], 60)))` |

#### 3.22 Routing Weights (5 nested fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 88 | `BASE_WEIGHT_FACTOR` | `RoutingWeights.BaseWeightFactor` | `float64` | `parseNumber(env[...], 0.5)` |
| 89 | `VALUE_SCORE_FACTOR` | `RoutingWeights.ValueScoreFactor` | `float64` | `parseNumber(env[...], 0.5)` |
| 90 | `COST_WEIGHT` | `RoutingWeights.CostWeight` | `float64` | `parseNumber(env[...], 0.4)` |
| 91 | `BALANCE_WEIGHT` | `RoutingWeights.BalanceWeight` | `float64` | `parseNumber(env[...], 0.3)` |
| 92 | `USAGE_WEIGHT` | `RoutingWeights.UsageWeight` | `float64` | `parseNumber(env[...], 0.3)` |

#### 3.23 Payload Rules + Service Tier (2 JSON fields)

| # | Env Var | Go 字段 | Go 类型 | 默认值/构造逻辑 |
|---|---------|---------|---------|---------------|
| 93 | `PAYLOAD_RULES_JSON` (legacy: `PAYLOAD_RULES`) | `PayloadRules` | `any` | `parseJsonValue(env["PAYLOAD_RULES_JSON"] or env["PAYLOAD_RULES"])` -- P4 阶段 `normalizePayloadRulesConfig` 处理 |
| 94 | `OPENAI_SERVICE_TIER_RULES_JSON` (legacy: `OPENAI_SERVICE_TIER_RULES`) | `OpenAiServiceTierRules` | `any` | `parseJsonValue(env["OPENAI_SERVICE_TIER_RULES_JSON"] or env["OPENAI_SERVICE_TIER_RULES"])` |

**总计 94 个配置入口** (含嵌套字段)。

---

### 4. 完整启动流程 (cmd/server/main.go)

TS 版的启动流程共 17 步。Go 版必须以相同顺序实现。标记为 `[stub]` 的步骤在 P0 阶段只需空函数体 + TODO 注释。

```
func main() {
    // 0. 加载 .env (godotenv.Load, 失败不报错)
    env := godotenvRead()

    // 1. 加载 config
    cfg := config.Load(env)

    // 2. Bootstrap: ensureRuntimeDatabaseReady
    //    [stub] 创建 dataDir, 打开初始 SQLite/MySQL/PG 连接
    store.EnsureRuntimeDatabaseReady(cfg.DbType, cfg.DbUrl, cfg.DbSsl)

    // 3. 从 settings 表读取存储的 DB 配置
    //    读取 settings 表 → toSettingsMap → extractSavedRuntimeDatabaseConfig
    settingsMap := store.LoadSettings()
    savedDbConfig := store.ExtractSavedRuntimeDatabaseConfig(settingsMap)

    // 4. 如果 settings 中的 DB 配置与当前不同, 尝试切换
    //    [stub] switchRuntimeDatabase; 失败回滚到原始配置
    if savedDbConfig != nil && differs from current {
        store.SwitchRuntimeDatabase(savedDbConfig.Dialect, savedDbConfig.DbUrl, savedDbConfig.Ssl)
        // 失败恢复: 如果部分切换成功, 回滚到原始 config
    }

    // 5. 兼容性 schema 迁移 (顺序重要, 每个 [stub])
    store.EnsureSiteCompatibilityColumns()
    store.EnsureRouteGroupingCompatibilityColumns()
    store.EnsureProxyFileCompatibilityColumns()
    store.EnsureProxyLogStreamTimingColumns()
    store.EnsureProxyLogClientColumns()
    store.EnsureProxyLogDownstreamApiKeyIdColumn()

    // 6. 重新读取 settings (迁移后可能新增行)
    finalSettingsMap := store.LoadSettings()

    // 7. 从 settings 表 apply runtime overrides 到 config
    //    [stub] 遍历 finalSettingsMap, 覆盖 cfg 中对应字段
    store.ApplyRuntimeSettings(cfg, finalSettingsMap)

    // 8. 判断是否有显式 log_cleanup_* settings
    //    如果有任意 log_cleanup_* key 在 settings 表中, cfg.LogCleanupConfigured = true
    cfg.LogCleanupConfigured = store.HasExplicitLogCleanupSettings(finalSettingsMap)

    // 9. 如果没有显式 log cleanup 配置且 proxy log retention > 0
    //    自动启用 usage log cleanup: usage=true, program=false,
    //    retention = normalizeLogCleanupRetentionDays(proxyLogRetentionDays)
    if !cfg.LogCleanupConfigured && cfg.ProxyLogRetentionDays > 0 {
        cfg.LogCleanupUsageLogsEnabled = true
        cfg.LogCleanupProgramLogsEnabled = false
        cfg.LogCleanupRetentionDays = normalizeLogCleanupRetentionDays(cfg.ProxyLogRetentionDays)
    }

    // 10. 剩余迁移 [stub]
    store.EnsureProxyLogBillingDetailsColumn()
    store.RepairStoredCreatedAtValues()
    store.MigrateSiteApiKeysToAccounts()
    store.EnsureDefaultSitesSeeded()
    store.EnsureOauthIdentityBackfill()
    //    [stub] routeRefreshWorkflow.RebuildRoutesOnly()

    // 11. 确保 OAuth provider sites 存在 [stub]
    store.EnsureOauthProviderSitesExist()

    // --- 以上全部包裹在 try/catch: 失败只 warn, 不阻止启动 ---

    // 12. 创建 HTTP router
    r := chi.NewRouter()

    // 13. 注册中间件 (Chi middleware 是 Fastify 行为的 GO 等价翻译)
    r.Use(middleware.RequestLogger)        // slog 结构化, 等价 TS `logger: true`
    r.Use(middleware.Recoverer)            // panic recovery
    r.Use(middleware.RealIP)              // 等价 TS `trustProxy: true`
    r.Use(middleware.CORS)                // 等价 TS `@fastify/cors` 默认行为 (见下方 CORS 详述)

    // 14. 注册 /health (设计新增, TS 无此路由)
    r.Get("/health", app.Health)

    // 15. 注册路由组
    //     /api/* 路由 (不包括公开路由) 需要 AuthMiddleware (Bearer admin token + IP allowlist)
    //     公开 /api 路由: /api/desktop/health, /api/oauth/callback/*
    r.Route("/api", func(r chi.Router) {
        r.Use(auth.AuthMiddleware)  // 内部跳过 isPublicApiRoute
        // P3-P11 注册具体路由 (当前 stub)
    })
    //     /v1/* 代理路由 -- [stub] P10, 自带内部 auth
    r.Route("/v1", func(r chi.Router) {
        // P10 注册代理路由 (自带 auth, 不在 Chi 层面加中间件)
    })

    // 16. SPA 静态文件 fallback (见下方 SPA 详述)
    if webDir exists {
        r.Handle("/assets/*", staticWithCacheControl(...))  // immutable
        r.NotFound(spaFallbackHandler)                       // SPA + API 404 JSON
    }

    // 17. 后台服务启动 [stub]
    scheduler.Start()                         // checkin scheduler
    backup.ReloadWebdavScheduler()            // [stub]
    siteAnnouncement.StartPolling()           // [stub]
    modelProbe.StartScheduler()               // [stub]
    channelRecovery.StartScheduler()          // [stub]
    sub2api.StartScheduler()                  // [stub]
    updateCenter.StartPolling()               // [stub]
    usageAggregation.StartScheduler()         // [stub]
    adminSnapshot.StartScheduler()            // [stub]
    oauth.StartLoopbackCallbackServers()      // [stub] -- 包裹在 try/catch, 失败只 warn

    // 18. proxy log retention legacy fallback
    //     [stub] setLegacyProxyLogRetentionFallbackEnabled(!cfg.LogCleanupConfigured)
    // 19. proxy file retention service [stub]
    proxyFileRetention.Start()

    // 20. 注册 onClose hook (注册所有 cleanup 回调)
    app.RegisterOnClose(func() {
        // stopSiteAnnouncementPolling()
        // stopUpdateCenterPolling()
        // stopProxyFileRetentionService()
        // stopProxyLogRetentionService()
        // stopModelAvailabilityProbeScheduler()
        // stopChannelRecoveryProbeScheduler()
        // stopUsageAggregationProjectorScheduler()   -- async, 等待完成
        // stopAdminSnapshotWarmScheduler()           -- async, 等待完成
        // stopSub2ApiManagedRefreshScheduler()       -- async, 等待完成
        // stopOAuthLoopbackCallbackServers()         -- async, 等待完成
    })

    // 21. Listen
    srv := &http.Server{Addr: cfg.ListenHost + ":" + strconv.Itoa(cfg.Port), Handler: r}
    go srv.ListenAndServe()
    printStartupSummary(cfg)

    // 22. 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    // 23. Graceful shutdown
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    app.OnClose()   // 顺序执行所有 cleanup
    srv.Shutdown(ctx)
    store.Close()
}
```

**关键设计说明**：

- 步骤 2-11 包裹在一个 try/catch 块中。任何失败只 `log.Warn`，不阻止启动。
- `switchRuntimeDatabase` 有回滚逻辑：如果 DB dialect/URL/SSL 在切换过程中改变了但切换本身失败，需要恢复到原始配置调用 `switchRuntimeDatabase` 回滚。
- `ApplyRuntimeSettings` (步骤 7) 从 settings 表读取配置覆盖 env 值。这是两阶段配置加载：env vars 提供初始值，DB settings 表提供运行时覆盖。
- `isPublicApiRoute` 白名单: URL == `/api/desktop/health` 或前缀匹配 `/api/oauth/callback/`。这些路由跳过 admin auth 中间件。

---

### 5. CORS 配置 (router/middleware.go)

TS 使用 `@fastify/cors` **无参数注册**。Fastify 的默认行为: 对于同源请求（无 `Origin` header）不发出 CORS header；对于带 `Origin` 的跨域请求，默认 **不** 自动返回 `Access-Control-Allow-Origin: *` -- 需要显式配置。

Go 实现策略: 使用 `go-chi/cors` 中间件，配置为 **生产安全** 的 CORS 策略。

```go
r.Use(cors.Handler(cors.Options{
    // 严格默认: 仅允许同源 + 显式配置的 origin
    // 实际生产: 从 cfg 读取 allowed origins
    AllowedOrigins:   []string{"*"},  // 匹配 TS 意图 (需显式配置)
    AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
    AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
    ExposedHeaders:   []string{"Link"},
    AllowCredentials: false,
    MaxAge:           300,
}))
```

注意: TS 的 `@fastify/cors` 无参数注册的默认行为与 `AllowedOrigins: ["*"]` 不完全相同，但两者的实际效果（在大多数 MetAPI 使用场景中）一致。这是有意的设计选择，不是 bug。

---

### 6. SPA 静态文件服务 (router/router.go)

```go
webDir := filepath.Join(cfg.DataDir, "..", "web", "dist")  // or embed.FS
if dirExists(webDir) {
    // /assets/*  → 1 年 immutable 缓存
    r.Handle("/assets/*", staticFileServer(webDir, map[string]string{
        "/assets/": "public, max-age=31536000, immutable",
    }))

    // SPA fallback: 非 /api/ 非 /v1/ → index.html; API → 404 JSON
    r.NotFound(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(404)
            w.Write([]byte(`{"error":"Not found"}`))
            return
        }
        // 返回 index.html (SPA)
        http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
    })
}
```

Cache 规则:
- `/assets/*` → `Cache-Control: public, max-age=31536000, immutable`
- `index.html` (在 SPA fallback 中返回) → `Cache-Control: no-cache`

---

### 7. Graceful Shutdown (app/app.go)

```go
type App struct {
    Config     *config.Config
    Store      *store.Store
    Router     chi.Router
    Server     *http.Server
    onCloseFns []func()  // FIFO 顺序执行
}

func (a *App) RegisterOnClose(fn func()) {
    a.onCloseFns = append(a.onCloseFns, fn)
}

func (a *App) OnClose() {
    for _, fn := range a.onCloseFns {
        fn()
    }
}

// 关闭顺序 (与 TS onClose hook 一致):
// 1. stopSiteAnnouncementPolling()
// 2. stopUpdateCenterPolling()
// 3. stopProxyFileRetentionService()
// 4. stopProxyLogRetentionService()
// 5. stopModelAvailabilityProbeScheduler()
// 6. stopChannelRecoveryProbeScheduler()
// 7. stopUsageAggregationProjectorScheduler()   -- 阻塞等待完成
// 8. stopAdminSnapshotWarmScheduler()           -- 阻塞等待完成
// 9. stopSub2ApiManagedRefreshScheduler()       -- 阻塞等待完成
// 10. stopOAuthLoopbackCallbackServers()        -- 阻塞等待完成
// 11. srv.Shutdown(ctx)                          -- HTTP graceful
// 12. store.Close()                              -- DB 关闭
```

### 8. Dockerfile

```dockerfile
# Stage 1: build frontend (复用现有 React SPA)
FROM node:25-alpine AS web
WORKDIR /web
# Native deps: esbuild sharp (better-sqlite3 不需要 -- Go 不使用)
RUN apk add --no-cache python3 make g++
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
RUN npm rebuild esbuild sharp --no-audit --no-fund
COPY web/ ./
RUN npm run build:web

# Stage 2: build Go
FROM golang:1.24-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi ./cmd/server

# Stage 3: runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
ENV DATA_DIR=/app/data
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=web /web/dist /app/web/dist
COPY --from=go /app/metapi /app/metapi
EXPOSE 4000
CMD ["/app/metapi"]
```

注意:
- Go 版不需要 `better-sqlite3`，所以 npm rebuild 只需要 `esbuild sharp`。
- `ENV DATA_DIR=/app/data` 必须在 CMD 之前设置，确保容器内默认数据目录正确。
- Go 版直接运行二进制，无需 `node dist/server/db/migrate.js` 预步骤 -- 迁移已内嵌在 `main()` 中。

---

## Acceptance Criteria
- [ ] `go build ./cmd/server` 通过, 无编译错误
- [ ] `./metapi` 启动 → `curl :4000/health` → `{"status":"ok"}` (注意: /health 是 Go 版设计新增, TS 无此路由)
- [ ] 所有 env var 解析正确: parseBoolean ("1"/"true"/"yes"/"on" → true, 其他 → false); parseNumber (NaN/Inf → fallback); parseCsvList (trim + filter empty); parseJsonValue (parse error → nil); parseDbType (postgresql → postgres; 未知 → sqlite)
- [ ] 无必填项 -- TS 所有字段都有默认值或优雅降级。非法值 (DB_TYPE=postgres 但 DB_URL 为空) 在启动时报结构化错误并退出。
- [ ] SIGTERM 后 5s 内优雅退出 (所有 onClose 回调执行完毕, DB 关闭)
- [ ] Docker 镜像 <20MB (Go 二进制 ~8MB, /app/web/dist 是最大变量)
- [ ] `make build` `make test` `make lint` `make docker-build` `make run` 全部可用 (Makefile 定义清晰目标)
- [ ] slog 日志格式: `{"time":"...","level":"INFO","msg":"listening","port":4000}` (这是 Go 版设计选择, 不是 TS pino 格式的移植)
- [ ] `DB_TYPE=mysql` 或非 sqlite 方言 → `DB_URL` 格式验证 (mysql: `mysql://...`; postgres: `postgres://...` 或 `postgresql://...`)

## Test Plan
| 文件 | 测试内容 |
|------|----------|
| `config/config_test.go` | 8 个 parse 函数全覆盖; 每个字段默认值; 每个字段边界值 (clamp, trunc, NaN, Inf, 负数, 大数); `parseJsonValue` 非法 JSON; `parseDbType` 所有已知/未知值; `normalizeTokenRouterFailureCooldownMaxSec` 无效输入返回 nil; TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC 超过 30 天 cap |
| `config/config_integration_test.go` | `Load()` 全字段 golden test: 给定已知 env map, 验证输出 Config 每个字段值精确匹配 TS `buildConfig` |
| `app/app_test.go` | graceful shutdown 时序; 双重 SIGTERM 行为; ListenAndServe 失败 |
| `router/router_test.go` | 路由注册顺序; SPA fallback: `/api/xxx` → 404 JSON, `/other` → index.html; /health → 200; `isPublicApiRoute` 白名单 |

## Edge Cases

### E1. `.env` 文件不存在
TS: `import 'dotenv/config'` 静默跳过。Go: `godotenv.Load()` 返回 `os.ErrNotExist` → 忽略, 只使用系统环境变量。

### E2. 已知 env var 的边界值
- `PORT` 为负数 / 非数字 / NaN / Inf → 使用默认值 4000
- `PORT=0` → 使用 0 (Go 绑定随机端口; TS `Math.trunc(0)` = 0)。如果 `PORT=0` 不是期望行为, 在配置层 clamp 为 `max(1, ...)`。
- `CHECKIN_INTERVAL_HOURS` 超出 [1, 24] → clamp
- `CHECKIN_SCHEDULE_MODE` 非 `interval` → 回退 `cron`
- `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` 超过 30 天 → cap 到 `TokenRouterFailureCooldownMaxSecCeiling`; 无效 → 使用 ceiling
- `NOTIFY_COOLDOWN_SEC` 负数 → clamp 到 0
- `ROUTING_FALLBACK_UNIT_COST` < 1e-6 → clamp 到 1e-6
- `MODEL_AVAILABILITY_PROBE_CONCURRENCY` 超出 [1, 16] → clamp
- 所有毫秒级时间配置有最小下限 (见字段表)

### E3. 非法/未知 config 值
- `DB_TYPE=mongodb` 或其他未知值 → `parseDbType` 回退 `sqlite`
- `DB_TYPE=postgres` 但 `DB_URL` 为空 → 在 `switchRuntimeDatabase`/`ensureRuntimeDatabaseReady` 阶段报错退出
- `PAYLOAD_RULES_JSON` 或 `OPENAI_SERVICE_TIER_RULES_JSON` 含非法 JSON → `parseJsonValue` 返回 `nil`, 不 panic

### E4. Port 冲突
`ListenAndServe` 返回 `EADDRINUSE` → `log.Error` + `os.Exit(1)`。**不在** `go srv.ListenAndServe()` goroutine 中静默丢弃错误 -- 需要显式传回主 goroutine 或使用 channel。

### E5. DATA_DIR 处理
- TS 使用 `env.DATA_DIR || './data'` 不做规范化
- Go: `os.MkdirAll(cfg.DataDir, 0755)` 在 `ensureRuntimeDatabaseReady` 之前调用
- 相对路径 (如 `./data`) 相对于进程工作目录

### E6. Settings 表读取失败
TS 包裹整个 settings-load block 在 try/catch 中，失败只 `console.warn`。Go: 任一步骤（读取 settings 表、迁移）失败 → `log.Warn` + 继续启动。`switchRuntimeDatabase` 失败 → 回滚到原始配置 + `log.Warn` + 继续启动。

### E7. Settings 表为空的首次启动
新建数据库 settings 表为空 → `toSettingsMap` 返回空 map → 所有运行时覆盖跳过 → 纯用 env var 启动。

### E8. 双重 SIGTERM
第一个 SIGTERM → 开始 graceful shutdown (5s timeout)。第二个 SIGTERM → Go 默认行为 `os.Exit` 立即退出。P0 不做特殊处理；P12 可考虑优雅重置 `signal.Notify` channel。

### E9. 后台服务启动失败
TS `startOAuthLoopbackCallbackServers` 包裹在 try/catch → 失败只 warn 不阻止启动。其他后台服务（scheduler, polling, probe）未包裹 try/catch -- 假设不会失败或 panic。Go: 每个后台服务启动包裹在 goroutine + recover 中；失败只 `log.Warn`。

### E10. Empty string vs undefined
Go 中 `map[string]string` 不存在 key 时返回 `""`。需要在 parse 函数中区分 "key not present" 和 "key set to empty string"。TS `parseOptionalSecret` 返回 `""` for undefined, 与 `""` for empty string 行为一致 -- 不需要区分。Go 统一处理即可。

### E11. `DATA_DIR` 尾随斜杠或 Windows 反斜杠
Go: `filepath.Clean(cfg.DataDir)` 在启动时规范化一次。

### E12. DB 迁移半失败时的幂等性
每个 `ensureCompatibilityColumns` 函数必须是幂等的（使用 `IF NOT EXISTS` / `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 或等效检查）。启动时按序执行所有迁移不会因已有列而失败。

---

## 设计新增说明 (非 TS 移植)

以下选择是 Go 版的设计决策，不是 TS 行为的直接移植：

| 项目 | Go 版 | TS 版 | 理由 |
|------|-------|-------|------|
| HTTP framework | `chi` router + `net/http` | Fastify | Go 生态最轻量惯用选择 |
| `/health` endpoint | `GET /health` → `{"status":"ok"}` | 无 (SPA fallback 返回 `index.html`) | K8s/docker healthcheck 标准需求 |
| CORS middleware | `go-chi/cors` 显式配置 | `@fastify/cors` 无参数 | 生产安全可控 |
| Build tooling | Makefile | npm scripts | Go 生态标准 (`make build test lint`) |
| Log format | `log/slog` structured JSON | pino (Fastify 内置) | Go 标准库, 格式等效 |
| Docker base | `alpine:3.21` (~7MB) | `node:25-alpine` (~130MB) | Go 静态二进制, 无需 Node runtime |
| Docker image target | <20MB | ~200MB+ | Go 静态编译优势 |
| Data dir in container | `ENV DATA_DIR=/app/data` | `ENV DATA_DIR=/app/data` | 一致 |
| Config loading | env map (caller loads `.env`) | `dotenv/config` side-effect import | Go 显式优于隐式 |
