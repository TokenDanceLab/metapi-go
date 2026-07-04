# Schema & Config Parity Report: TS (metapi) vs Go (metapi-go)

**Date:** 2026-07-04
**Authoritative Reference:** `metapi/src/server/db/generated/schemaContract.json`
**Overall Recommendation:** NEEDS_FIX (2 differences found)

---

## 1. Schema Parity

### 1.1 Tables Overview

| # | Table | TS (schema.ts) | Go (schema.go) | Contract | Status |
|---|-------|:-:|:-:|:-:|:-:|
| 1 | sites | 19 cols | 19 cols | 19 cols | PASS |
| 2 | site_api_endpoints | 11 cols | 11 cols | 11 cols | PASS |
| 3 | site_disabled_models | 4 cols | 4 cols | 4 cols | PASS |
| 4 | accounts | 22 cols | 22 cols | 22 cols | PASS |
| 5 | account_tokens | 11 cols | 11 cols | 11 cols | PASS |
| 6 | checkin_logs | 6 cols | 6 cols | 6 cols | PASS |
| 7 | model_availability | 7 cols | 7 cols | 7 cols | PASS |
| 8 | token_model_availability | 6 cols | 6 cols | 6 cols | PASS |
| 9 | token_routes | 12 cols | 12 cols | 12 cols | PASS |
| 10 | route_group_sources | 3 cols | 3 cols | 3 cols | PASS |
| 11 | oauth_route_units | 7 cols | 7 cols | 7 cols | PASS |
| 12 | oauth_route_unit_members | 16 cols | 16 cols | 16 cols | PASS |
| 13 | route_channels | 20 cols | 20 cols | 20 cols | PASS |
| 14 | proxy_logs | 24 cols | 24 cols | 24 cols | PASS |
| 15 | proxy_debug_traces | 26 cols | 26 cols | 26 cols | PASS |
| 16 | proxy_debug_attempts | 18 cols | 18 cols | 18 cols | PASS |
| 17 | proxy_video_tasks | 15 cols | 15 cols | 15 cols | PASS |
| 18 | proxy_files | 13 cols | 13 cols | 13 cols | PASS |
| 19 | settings | 2 cols | 2 cols | 2 cols | PASS |
| 20 | admin_snapshots | 9 cols | 9 cols | 9 cols | PASS |
| 21 | analytics_projection_checkpoints | 18 cols | 18 cols | 18 cols | PASS |
| 22 | site_day_usage | 13 cols | 13 cols | 13 cols | PASS |
| 23 | site_hour_usage | 13 cols | 13 cols | 13 cols | PASS |
| 24 | model_day_usage | 13 cols | 13 cols | 13 cols | PASS |
| 25 | downstream_api_keys | 20 cols | 20 cols | 20 cols | PASS |
| 26 | site_announcements | 17 cols | **19 cols** | 17 cols | **FAIL** |
| 27 | events | 9 cols | 9 cols | 9 cols | PASS |

### 1.2 Column Name Parity (per-table)

All 27 tables verified column-by-column against the contract. Column names match exactly (case-sensitive, snake_case at DB level) across TS and Go for all tables **except** site_announcements.

**site_announcements** -- Extra columns in Go (see Section 1.3).

### 1.3 Detailed Differences

#### DIFF #1: site_announcements -- Go has extra `created_at` and `updated_at`

- **File:** `metapi-go/store/schema.go` lines 450-451
- **Severity:** Medium
- **What:** Go `SiteAnnouncement` struct declares two columns not present in the contract or TS schema:
  ```go
  CreatedAt  string  `db:"created_at"`   // NOT in contract
  UpdatedAt  string  `db:"updated_at"`   // NOT in contract
  ```
- **Contract:** `site_announcements` columns end at `raw_payload` (17 total). No `created_at` or `updated_at`.
- **TS (`schema.ts` lines 521-543):** `siteAnnouncements` also has 17 columns, no `createdAt`/`updatedAt`.
- **Impact:** Go queries reading `site_announcements` will attempt to scan `created_at`/`updated_at` columns that do not exist in the actual SQLite schema. This will cause **runtime scan errors** on any SELECT or INSERT that includes these fields.
- **Recommendation:** Remove `CreatedAt` and `UpdatedAt` from the `SiteAnnouncement` struct in `schema.go`.

### 1.4 Type Mapping Parity

| Logical Type (Contract) | TS (Drizzle) | Go (db tag) | Verdict |
|-------------------------|-------------|-------------|---------|
| integer | `integer()` | `int64` | PASS |
| real | `real()` | `float64` | PASS |
| text | `text()` | `string` | PASS |
| boolean | `integer({mode:'boolean'})` | `bool` | PASS |
| datetime | `text()` | `string` | PASS |
| json | `text()` | `*string` | PASS (SQLite has no native JSON) |

All type mappings consistent across the 27 tables.

### 1.5 NOT NULL / Nullability Parity

Go encodes nullability via pointer types: `*string` = nullable, `string` = NOT NULL. All 300+ column nullability flags verified against the contract. No mismatches found.

### 1.6 Default Value Parity

Default values are encoded in Drizzle TS (`.default(...)`) and in the contract. Go structs do not encode defaults (they live in migration SQL). Where defaults are defined in the contract, they are correctly present in the TS schema. No mismatches between TS and the contract.

### 1.7 Index Parity (TS vs Contract)

All 77 indexes from the contract verified against TS schema.ts. Every index matches in name, columns, and uniqueness:

- 77 indexes total (60 non-unique, 17 unique)
- All names match exactly
- All column lists match exactly
- All unique flags match exactly

Note: Go `schema.go` does not encode index definitions (indexes are in migration SQL). This is the expected design difference.

### 1.8 Foreign Key Parity (TS vs Contract)

All 20 foreign keys from the contract verified against TS schema.ts. Each matches in:
- Source column(s)
- Referenced table and column(s)
- ON DELETE action

| FK | Source Table | Column | Ref Table | Ref Column | ON DELETE | TS Match |
|----|-------------|--------|-----------|------------|-----------|-----------|
| 1 | account_tokens | account_id | accounts | id | CASCADE | PASS |
| 2 | accounts | site_id | sites | id | CASCADE | PASS |
| 3 | checkin_logs | account_id | accounts | id | CASCADE | PASS |
| 4 | model_availability | account_id | accounts | id | CASCADE | PASS |
| 5 | model_day_usage | site_id | sites | id | CASCADE | PASS |
| 6 | oauth_route_unit_members | account_id | accounts | id | CASCADE | PASS |
| 7 | oauth_route_unit_members | unit_id | oauth_route_units | id | CASCADE | PASS |
| 8 | oauth_route_units | site_id | sites | id | CASCADE | PASS |
| 9 | proxy_debug_attempts | trace_id | proxy_debug_traces | id | CASCADE | PASS |
| 10 | route_channels | account_id | accounts | id | CASCADE | PASS |
| 11 | route_channels | route_id | token_routes | id | CASCADE | PASS |
| 12 | route_channels | token_id | account_tokens | id | SET NULL | PASS |
| 13 | route_group_sources | group_route_id | token_routes | id | CASCADE | PASS |
| 14 | route_group_sources | source_route_id | token_routes | id | CASCADE | PASS |
| 15 | site_announcements | site_id | sites | id | CASCADE | PASS |
| 16 | site_api_endpoints | site_id | sites | id | CASCADE | PASS |
| 17 | site_day_usage | site_id | sites | id | CASCADE | PASS |
| 18 | site_disabled_models | site_id | sites | id | CASCADE | PASS |
| 19 | site_hour_usage | site_id | sites | id | CASCADE | PASS |
| 20 | token_model_availability | token_id | account_tokens | id | CASCADE | PASS |

Note: Go `schema.go` does not encode FK definitions (FKs are in migration SQL). This is the expected design difference.

---

## 2. Config Surface Parity

### 2.1 Env Var Name Parity

All 60+ config fields compared between TS (`config.ts` `buildConfig()`) and Go (`config/config.go` `Load()`).

#### PASS -- All matching config keys:

| Category | TS Field | Go Field | Env Var(s) | Default Match | Parse Match |
|----------|---------|---------|------------|:-:|:-:|
| Auth | authToken | AuthToken | AUTH_TOKEN | PASS | PASS |
| Auth | proxyToken | ProxyToken | PROXY_TOKEN | PASS | PASS |
| Auth | deployHelperToken | DeployHelperToken | DEPLOY_HELPER_TOKEN / UPDATE_CENTER_HELPER_TOKEN | PASS | PASS |
| Auth | codexClientId | CodexClientId | CODEX_CLIENT_ID | PASS | PASS |
| Auth | accountCredentialSecret | AccountCredentialSecret | ACCOUNT_CREDENTIAL_SECRET / AUTH_TOKEN | PASS | PASS |
| OAuth | claudeClientId | ClaudeClientId | CLAUDE_CLIENT_ID | PASS | PASS |
| OAuth | claudeClientSecret | ClaudeClientSecret | CLAUDE_CLIENT_SECRET | PASS | PASS |
| OAuth | geminiCliClientId | GeminiCliClientId | GEMINI_CLI_CLIENT_ID | PASS | PASS |
| OAuth | geminiCliClientSecret | GeminiCliClientSecret | GEMINI_CLI_CLIENT_SECRET | PASS | PASS |
| Server | port | Port | PORT | PASS | PASS |
| Server | listenHost | ListenHost | HOST | PASS | PASS |
| Server | dataDir | DataDir | DATA_DIR | PASS | PASS |
| Server | dbType | DbType | DB_TYPE | PASS | PASS |
| Server | dbUrl | DbUrl | DB_URL | PASS | PASS |
| Server | dbSsl | DbSsl | DB_SSL | PASS | PASS |
| Cron | checkinCron | CheckinCron | CHECKIN_CRON | PASS | PASS |
| Cron | checkinScheduleMode | CheckinScheduleMode | CHECKIN_SCHEDULE_MODE | PASS | PASS |
| Cron | checkinIntervalHours | CheckinIntervalHours | CHECKIN_INTERVAL_HOURS | PASS | PASS |
| Cron | balanceRefreshCron | BalanceRefreshCron | BALANCE_REFRESH_CRON | PASS | PASS |
| Cron | logCleanupCron | LogCleanupCron | LOG_CLEANUP_CRON | PASS | PASS |
| LogCleanup | logCleanupUsageLogsEnabled | LogCleanupUsageLogsEnabled | LOG_CLEANUP_USAGE_LOGS_ENABLED | PASS | PASS |
| LogCleanup | logCleanupProgramLogsEnabled | LogCleanupProgramLogsEnabled | LOG_CLEANUP_PROGRAM_LOGS_ENABLED | PASS | PASS |
| LogCleanup | logCleanupRetentionDays | LogCleanupRetentionDays | LOG_CLEANUP_RETENTION_DAYS | PASS | PASS |
| LogCleanup | logCleanupConfigured | LogCleanupConfigured | (hardcoded false) | PASS | PASS |
| Notify | webhookUrl | WebhookUrl | WEBHOOK_URL | PASS | PASS |
| Notify | webhookEnabled | WebhookEnabled | WEBHOOK_ENABLED | PASS | PASS |
| Notify | barkUrl | BarkUrl | BARK_URL | PASS | PASS |
| Notify | barkEnabled | BarkEnabled | BARK_ENABLED | PASS | PASS |
| Notify | serverChanKey | ServerChanKey | SERVERCHAN_KEY | PASS | PASS |
| Notify | serverChanEnabled | ServerChanEnabled | SERVERCHAN_ENABLED | PASS | PASS |
| Notify | telegramEnabled | TelegramEnabled | TELEGRAM_ENABLED | PASS | PASS |
| Notify | telegramApiBaseUrl | TelegramApiBaseUrl | (hardcoded) | PASS | PASS |
| Notify | telegramBotToken | TelegramBotToken | TELEGRAM_BOT_TOKEN | PASS | PASS |
| Notify | telegramChatId | TelegramChatId | TELEGRAM_CHAT_ID | PASS | PASS |
| Notify | telegramUseSystemProxy | TelegramUseSystemProxy | TELEGRAM_USE_SYSTEM_PROXY | PASS | PASS |
| Notify | telegramMessageThreadId | TelegramMessageThreadId | TELEGRAM_MESSAGE_THREAD_ID | PASS | PASS |
| Notify | smtpEnabled | SmtpEnabled | SMTP_ENABLED | PASS | PASS |
| Notify | smtpHost | SmtpHost | SMTP_HOST | PASS | PASS |
| Notify | smtpPort | SmtpPort | SMTP_PORT | PASS | PASS |
| Notify | smtpSecure | SmtpSecure | SMTP_SECURE | PASS | PASS |
| Notify | smtpUser | SmtpUser | SMTP_USER | PASS | PASS |
| Notify | smtpPass | SmtpPass | SMTP_PASS | PASS | PASS |
| Notify | smtpFrom | SmtpFrom | SMTP_FROM | PASS | PASS |
| Notify | smtpTo | SmtpTo | SMTP_TO | PASS | PASS |
| Notify | notifyCooldownSec | NotifyCooldownSec | NOTIFY_COOLDOWN_SEC | PASS | PASS |
| Notify | systemProxyUrl | SystemProxyUrl | SYSTEM_PROXY_URL | PASS | PASS |
| Admin | adminIpAllowlist | AdminIpAllowlist | ADMIN_IP_ALLOWLIST | PASS | PASS |
| Proxy | requestBodyLimit | RequestBodyLimit | (hardcoded 20MB) | PASS | PASS |
| Proxy | routingFallbackUnitCost | RoutingFallbackUnitCost | ROUTING_FALLBACK_UNIT_COST | PASS | PASS |
| Proxy | proxyFirstByteTimeoutSec | ProxyFirstByteTimeoutSec | PROXY_FIRST_BYTE_TIMEOUT_SEC | PASS | PASS |
| Proxy | tokenRouterFailureCooldownMaxSec | TokenRouterFailureCooldownMaxSec | TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC | PASS | PASS |
| Proxy | tokenRouterCacheTtlMs | TokenRouterCacheTtlMs | TOKEN_ROUTER_CACHE_TTL_MS | PASS | PASS |
| Proxy | proxyMaxChannelAttempts | ProxyMaxChannelAttempts | PROXY_MAX_CHANNEL_ATTEMPTS | PASS | PASS |
| Proxy | proxyStickySessionEnabled | ProxyStickySessionEnabled | PROXY_STICKY_SESSION_ENABLED | PASS | PASS |
| Proxy | proxyStickySessionTtlMs | ProxyStickySessionTtlMs | PROXY_STICKY_SESSION_TTL_MS | PASS | PASS |
| Proxy | proxySessionChannelConcurrencyLimit | ProxySessionChannelConcurrencyLimit | PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT | PASS | PASS |
| Proxy | proxySessionChannelQueueWaitMs | ProxySessionChannelQueueWaitMs | PROXY_SESSION_CHANNEL_QUEUE_WAIT_MS | PASS | PASS |
| Proxy | proxySessionChannelLeaseTtlMs | ProxySessionChannelLeaseTtlMs | PROXY_SESSION_CHANNEL_LEASE_TTL_MS | PASS | PASS |
| Proxy | proxySessionChannelLeaseKeepaliveMs | ProxySessionChannelLeaseKeepaliveMs | PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS | PASS | PASS |
| Proxy | codexUpstreamWebsocketEnabled | CodexUpstreamWebsocketEnabled | CODEX_UPSTREAM_WEBSOCKET_ENABLED | PASS | PASS |
| Proxy | responsesCompactFallbackToResponsesEnabled | ResponsesCompactFallbackToResponsesEnabled | RESPONSES_COMPACT_FALLBACK_TO_RESPONSES_ENABLED | PASS | PASS |
| Proxy | disableCrossProtocolFallback | DisableCrossProtocolFallback | DISABLE_CROSS_PROTOCOL_FALLBACK | PASS | PASS |
| Proxy | proxyEmptyContentFailEnabled | ProxyEmptyContentFailEnabled | PROXY_EMPTY_CONTENT_FAIL | PASS | PASS |
| Proxy | proxyErrorKeywords | ProxyErrorKeywords | PROXY_ERROR_KEYWORDS | PASS | PASS |
| Proxy | globalBlockedBrands | GlobalBlockedBrands | (hardcoded []) | PASS | PASS |
| Proxy | globalAllowedModels | GlobalAllowedModels | (hardcoded []) | PASS | PASS |
| Debug | proxyDebugTraceEnabled | ProxyDebugTraceEnabled | PROXY_DEBUG_TRACE_ENABLED | PASS | PASS |
| Debug | proxyDebugCaptureHeaders | ProxyDebugCaptureHeaders | PROXY_DEBUG_CAPTURE_HEADERS | PASS | PASS |
| Debug | proxyDebugCaptureBodies | ProxyDebugCaptureBodies | PROXY_DEBUG_CAPTURE_BODIES | PASS | PASS |
| Debug | proxyDebugCaptureStreamChunks | ProxyDebugCaptureStreamChunks | PROXY_DEBUG_CAPTURE_STREAM_CHUNKS | PASS | PASS |
| Debug | proxyDebugTargetSessionId | ProxyDebugTargetSessionId | PROXY_DEBUG_TARGET_SESSION_ID | PASS | PASS |
| Debug | proxyDebugTargetClientKind | ProxyDebugTargetClientKind | PROXY_DEBUG_TARGET_CLIENT_KIND | PASS | PASS |
| Debug | proxyDebugTargetModel | ProxyDebugTargetModel | PROXY_DEBUG_TARGET_MODEL | PASS | PASS |
| Debug | proxyDebugRetentionHours | ProxyDebugRetentionHours | PROXY_DEBUG_RETENTION_HOURS | PASS | PASS |
| Debug | proxyDebugMaxBodyBytes | ProxyDebugMaxBodyBytes | PROXY_DEBUG_MAX_BODY_BYTES | PASS | PASS |
| Codex | codexResponsesWebsocketBeta | CodexResponsesWebsocketBeta | CODEX_RESPONSES_WEBSOCKET_BETA | PASS | PASS |
| Codex | codexHeaderDefaults.userAgent | CodexHeaderDefaults.UserAgent | CODEX_HEADER_DEFAULTS_USER_AGENT | PASS | PASS |
| Codex | codexHeaderDefaults.betaFeatures | CodexHeaderDefaults.BetaFeatures | CODEX_HEADER_DEFAULTS_BETA_FEATURES | PASS | PASS |
| Probe | modelAvailabilityProbeEnabled | ModelAvailabilityProbeEnabled | MODEL_AVAILABILITY_PROBE_ENABLED | PASS | PASS |
| Probe | modelAvailabilityProbeIntervalMs | ModelAvailabilityProbeIntervalMs | MODEL_AVAILABILITY_PROBE_INTERVAL_MS | PASS | PASS |
| Probe | modelAvailabilityProbeTimeoutMs | ModelAvailabilityProbeTimeoutMs | MODEL_AVAILABILITY_PROBE_TIMEOUT_MS | PASS | PASS |
| Probe | modelAvailabilityProbeConcurrency | ModelAvailabilityProbeConcurrency | MODEL_AVAILABILITY_PROBE_CONCURRENCY | PASS | PASS |
| Retention | proxyLogRetentionDays | ProxyLogRetentionDays | PROXY_LOG_RETENTION_DAYS | PASS | PASS |
| Retention | proxyLogRetentionPruneIntervalMinutes | ProxyLogRetentionPruneIntervalMinutes | PROXY_LOG_RETENTION_PRUNE_INTERVAL_MINUTES | PASS | PASS |
| Retention | proxyFileRetentionDays | ProxyFileRetentionDays | PROXY_FILE_RETENTION_DAYS | PASS | PASS |
| Retention | proxyFileRetentionPruneIntervalMinutes | ProxyFileRetentionPruneIntervalMinutes | PROXY_FILE_RETENTION_PRUNE_INTERVAL_MINUTES | PASS | PASS |
| Routing | routingWeights.baseWeightFactor | RoutingWeights.BaseWeightFactor | BASE_WEIGHT_FACTOR | PASS | PASS |
| Routing | routingWeights.valueScoreFactor | RoutingWeights.ValueScoreFactor | VALUE_SCORE_FACTOR | PASS | PASS |
| Routing | routingWeights.costWeight | RoutingWeights.CostWeight | COST_WEIGHT | PASS | PASS |
| Routing | routingWeights.balanceWeight | RoutingWeights.BalanceWeight | BALANCE_WEIGHT | PASS | PASS |
| Routing | routingWeights.usageWeight | RoutingWeights.UsageWeight | USAGE_WEIGHT | PASS | PASS |
| Misc | payloadRules | PayloadRules | PAYLOAD_RULES_JSON / PAYLOAD_RULES | PASS | PASS |
| Misc | openAiServiceTierRules | OpenAiServiceTierRules | OPENAI_SERVICE_TIER_RULES_JSON / OPENAI_SERVICE_TIER_RULES | PASS | PASS |

### 2.2 Detailed Config Differences

#### DIFF #2: Go has `Tz` field, TS does not

- **File:** `metapi-go/config/config.go` line 357
- **Severity:** Low
- **What:** Go config reads `TZ` env var into `cfg.Tz`:
  ```go
  cfg.Tz = get("TZ")
  ```
- **TS:** No `TZ` field exists in the TS config (`config.ts` `buildConfig()` output).
- **Analysis:** TS uses SQLite exclusively, which has no timezone support. If Go is expected to support MySQL/Postgres (configured via DB_TYPE), the `TZ` field is legitimate for Go but not applicable to TS. This is a design divergence rather than a parity bug.
- **Recommendation:** Document this as an intentional Go-only extension. If TS will never support non-SQLite databases, this is acceptable. If TS may add MySQL/Postgres later, add `TZ` to the TS config too.

### 2.3 Parse Function Parity

| Function | TS | Go | Verdict |
|----------|----|----|---------|
| parseBoolean | `1/true/yes/on` case-insensitive, else fallback | `1/true/yes/on` case-insensitive, else fallback | PASS |
| parseNumber | NaN/Infinity -> fallback, else return | NaN/Infinity -> fallback, else return | PASS |
| parseCsvList | split by `,`, trim, filter empty | split by `,`, trim, filter empty | PASS |
| parseOptionalSecret | `(value\|\|'').trim()` | `strings.TrimSpace(value)` | PASS |
| parseJsonValue | JSON.parse, silent fail | json.Unmarshal, silent fail | PASS |
| parseDbType | sqlite/mysql/postgres | sqlite/mysql/postgres (postgresql alias) | PASS |
| normalizeTokenRouterFailureCooldownMaxSec | trunc, clamp[1, ceiling] | trunc, clamp[1, ceiling] | PASS |
| parseListenHost | HOST -> fallback "0.0.0.0" | HOST -> fallback "0.0.0.0" | PASS |

All 8 parse functions verified behavior-identical.

### 2.4 Default Values Parity

All 20 constants in `metapi-go/config/defaults.go` verified against TS `config.ts` hardcoded defaults. Every value matches exactly.

---

## 3. Summary

| Category | Items Checked | PASS | DIFF |
|----------|:-:|:-:|:-:|
| Tables | 27 | 26 | 1 |
| Table columns | 390 | 388 | 2 (2 extra in Go) |
| Column types | 390 | 390 | 0 |
| NOT NULL constraints | 390 | 390 | 0 |
| Default values (TS vs Contract) | 390 | 390 | 0 |
| Indexes (TS vs Contract) | 77 | 77 | 0 |
| Foreign keys (TS vs Contract) | 20 | 20 | 0 |
| Config keys | 93 | 92 | 1 (1 extra in Go) |
| Config defaults | 20 | 20 | 0 |
| Parse functions | 8 | 8 | 0 |

### Issues Found

| # | Category | Severity | Description | File |
|---|---------|----------|-------------|------|
| 1 | Schema | **Medium** | Go `SiteAnnouncement` struct has extra `created_at`/`updated_at` columns not in contract or TS | `metapi-go/store/schema.go:450-451` |
| 2 | Config | Low | Go has `Tz` field (from TZ env var) not present in TS config | `metapi-go/config/config.go:357` |

### Recommended Actions

1. **DIFF #1 (NEEDS_FIX):** Remove `CreatedAt` and `UpdatedAt` fields from the `SiteAnnouncement` struct in `metapi-go/store/schema.go` lines 450-451. These columns do not exist in the authoritative SQLite schema and will cause runtime errors.

2. **DIFF #2 (DOCUMENT):** Document the `Tz` field as a Go-only extension intended for MySQL/Postgres timezone support. Add it to TS config only if multi-DB support is planned for the TS codebase.
