package config

import (
	"encoding/json"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
)

var (
	globalCfg *Config
	cfgMu     sync.RWMutex
)

// Set stores the global config singleton.
func Set(cfg *Config) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	globalCfg = cfg
}

// Get returns the global config singleton.
// Panics if config has not been loaded.
func Get() *Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	if globalCfg == nil {
		panic("config.Get() called before config.Set() — load config first")
	}
	return globalCfg
}

// CodexHeaderDefaults holds Codex-specific HTTP header defaults.
type CodexHeaderDefaults struct {
	UserAgent    string
	BetaFeatures string
}

// RoutingWeights holds the weight factors for the routing algorithm.
type RoutingWeights struct {
	BaseWeightFactor float64
	ValueScoreFactor float64
	CostWeight       float64
	BalanceWeight    float64
	UsageWeight      float64
}

// Config holds ALL configuration fields for the MetAPI server.
// Field names use Go exported (PascalCase) but each maps 1:1 to a TS config field
// as documented in docs/specs/p0-skeleton.md section 3.
type Config struct {
	// Auth (5 fields)
	AuthToken               string
	ProxyToken              string
	DeployHelperToken       string
	AccountCredentialSecret string
	CodexClientId           string

	// OAuth Clients (4 fields)
	ClaudeClientId        string
	ClaudeClientSecret    string
	GeminiCliClientId     string
	GeminiCliClientSecret string

	// Server (7 fields)
	Port       int
	ListenHost string
	DataDir    string
	DbType     string
	DbUrl      string
	DbSsl      bool
	DbSslMode  string
	// PostgreSQL pool budget. Production operators must size MaxOpenConns no
	// higher than the database role CONNECTION LIMIT.
	DbMaxOpenConns       int
	DbMaxIdleConns       int
	DbConnMaxLifetimeSec int
	DbConnMaxIdleTimeSec int
	Tz                   string

	// Cron (5 fields)
	CheckinCron          string
	CheckinScheduleMode  string
	CheckinIntervalHours int
	BalanceRefreshCron   string
	LogCleanupCron       string

	// Log Cleanup (4 fields)
	LogCleanupUsageLogsEnabled   bool
	LogCleanupProgramLogsEnabled bool
	LogCleanupRetentionDays      int
	LogCleanupConfigured         bool

	// Notify: Webhook (2 fields)
	WebhookUrl     string
	WebhookEnabled bool

	// Notify: Bark (2 fields)
	BarkUrl     string
	BarkEnabled bool

	// Notify: ServerChan (2 fields)
	ServerChanKey     string
	ServerChanEnabled bool

	// Notify: Telegram (6 fields)
	TelegramEnabled         bool
	TelegramApiBaseUrl      string
	TelegramBotToken        string
	TelegramChatId          string
	TelegramUseSystemProxy  bool
	TelegramMessageThreadId string

	// Notify: SMTP (8 fields)
	SmtpEnabled bool
	SmtpHost    string
	SmtpPort    int
	SmtpSecure  bool
	SmtpUser    string
	SmtpPass    string
	SmtpFrom    string
	SmtpTo      string

	// Notify: General (2 fields)
	NotifyCooldownSec int
	SystemProxyUrl    string
	// RedisURL enables optional shared admission counters (#118).
	RedisURL string

	// Admin (3 fields)
	AdminIpAllowlist        []string
	AdminCorsAllowedOrigins []string
	TrustedProxyCidrs       []string

	// Proxy: Core (2 fields)
	RequestBodyLimit        int
	RoutingFallbackUnitCost float64
	// ProxyFirstByteTimeoutSec is the operator-facing first-byte / first-token
	// timeout in SECONDS (env PROXY_FIRST_BYTE_TIMEOUT_SEC). Internal dispatch
	// converts to milliseconds via proxy.FirstByteTimeoutMs (sec * 1000).
	// 0 disables observed first-byte timeout.
	ProxyFirstByteTimeoutSec int

	// Proxy: Token Router (2 fields)
	TokenRouterFailureCooldownMaxSec int
	TokenRouterCacheTtlMs            int

	// Proxy: Channel (3 fields)
	ProxyMaxChannelAttempts   int
	ProxyStickySessionEnabled bool
	ProxyStickySessionTtlMs   int

	// Proxy: Session (4 fields)
	ProxySessionChannelConcurrencyLimit int
	ProxySessionChannelQueueWaitMs      int
	ProxySessionChannelLeaseTtlMs       int
	ProxySessionChannelLeaseKeepaliveMs int

	// Proxy: Misc (6 fields)
	CodexUpstreamWebsocketEnabled              bool
	ResponsesCompactFallbackToResponsesEnabled bool
	DisableCrossProtocolFallback               bool
	ProxyEmptyContentFailEnabled               bool
	ProxyErrorKeywords                         []string
	GlobalBlockedBrands                        []string
	GlobalAllowedModels                        []string

	// Proxy: Debug (9 fields)
	ProxyDebugTraceEnabled        bool
	ProxyDebugCaptureHeaders      bool
	ProxyDebugCaptureBodies       bool
	ProxyDebugCaptureStreamChunks bool
	ProxyDebugTargetSessionId     string
	ProxyDebugTargetClientKind    string
	ProxyDebugTargetModel         string
	ProxyDebugRetentionHours      int
	ProxyDebugMaxBodyBytes        int

	// Codex-specific (3 fields)
	CodexResponsesWebsocketBeta string
	CodexHeaderDefaults         CodexHeaderDefaults

	// Model Probe (4 fields)
	ModelAvailabilityProbeEnabled     bool
	ModelAvailabilityProbeIntervalMs  int
	ModelAvailabilityProbeTimeoutMs   int
	ModelAvailabilityProbeConcurrency int

	// Retention (6 fields)
	ProxyLogRetentionDays                       int
	ProxyLogRetentionPruneIntervalMinutes       int
	ProxyFileRetentionDays                      int
	ProxyFileRetentionPruneIntervalMinutes      int
	ProxyVideoTaskRetentionDays                 int
	ProxyVideoTaskRetentionPruneIntervalMinutes int

	// Routing Weights (5 fields)
	RoutingWeights RoutingWeights

	// Payload Rules (2 JSON fields)
	PayloadRules           any
	OpenAiServiceTierRules any
}

// ---------------------------------------------------------------------------
// §1  Parse functions — must match TS behavior byte-for-byte
// ---------------------------------------------------------------------------

// §1.1 parseBoolean: "" → fallback; trim+lower → "1"/"true"/"yes"/"on" → true; else false.
func parseBoolean(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "1" || normalized == "true" || normalized == "yes" || normalized == "on"
}

// §1.2 parseNumber: "" → fallback; ParseFloat → NaN/Inf → fallback.
func parseNumber(value string, fallback float64) float64 {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return fallback
	}
	return parsed
}

// §1.3 parseCsvList: "" → []; split by "," → trim each → filter len>0.
func parseCsvList(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if len(trimmed) > 0 {
			result = append(result, trimmed)
		}
	}
	return result
}

// §1.4 parseOptionalSecret: TrimSpace; "" stays "".
func parseOptionalSecret(value string) string {
	return strings.TrimSpace(value)
}

// §1.5 parseJsonValue: "" → nil; Unmarshal failure → nil (no panic).
func parseJsonValue(value string) any {
	if value == "" {
		return nil
	}
	var result any
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}

// §1.6 parseDbType: "" → "sqlite"; trim+lower → "mysql"/"postgres"/"postgresql" → "postgres"; else "sqlite".
func parseDbType(value string) string {
	if value == "" {
		return "sqlite"
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "mysql":
		return "mysql"
	case "postgres", "postgresql":
		return "postgres"
	default:
		return "sqlite"
	}
}

func inferDbType(value string, dbURL string) string {
	if strings.TrimSpace(value) != "" {
		return parseDbType(value)
	}
	normalizedURL := strings.ToLower(strings.TrimSpace(dbURL))
	if strings.HasPrefix(normalizedURL, "postgres://") || strings.HasPrefix(normalizedURL, "postgresql://") {
		return "postgres"
	}
	return "sqlite"
}

func normalizeDbSslMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validDbSslMode(value string) bool {
	switch normalizeDbSslMode(value) {
	case "", "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

// PostgresSSLMode returns the explicit DB_SSLMODE setting, or the legacy
// DB_SSL=true behavior mapped to sslmode=require.
func (c *Config) PostgresSSLMode() string {
	if mode := normalizeDbSslMode(c.DbSslMode); mode != "" {
		return mode
	}
	if c.DbSsl {
		return "require"
	}
	return ""
}

// §1.7 normalizeTokenRouterFailureCooldownMaxSec:
// !finite || <= 0 → (0, false); trunc → clamp[1, ceiling] → (int, true).
func normalizeTokenRouterFailureCooldownMaxSec(value float64) (int, bool) {
	if math.IsInf(value, 0) || math.IsNaN(value) || value <= 0 {
		return 0, false
	}
	truncated := math.Trunc(value)
	clamped := int(math.Max(1, math.Min(float64(TokenRouterFailureCooldownMaxSecCeiling), truncated)))
	return clamped, true
}

// §1.8 parseListenHost: env["HOST"] empty → "0.0.0.0"; trim → empty → "0.0.0.0".
func parseListenHost(env map[string]string) string {
	host := env["HOST"]
	if host == "" {
		return "0.0.0.0"
	}
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return "0.0.0.0"
	}
	return trimmed
}

// ---------------------------------------------------------------------------
// §3  Load — build Config from env map
// ---------------------------------------------------------------------------

// Load reads environment variables from the given map and returns a fully
// populated Config. The caller (main.go) is responsible for loading .env files
// via godotenv before calling Load.
func Load(env map[string]string) *Config {
	// Helper to read from the env map; Go map access returns "" for missing keys.
	get := func(key string) string {
		return env[key]
	}

	// Resolve legacy alias: DEPLOY_HELPER_TOKEN falls back to UPDATE_CENTER_HELPER_TOKEN
	resolveDeployHelperToken := func() string {
		if v := get("DEPLOY_HELPER_TOKEN"); v != "" {
			return v
		}
		return get("UPDATE_CENTER_HELPER_TOKEN")
	}

	// Resolve ACCOUNT_CREDENTIAL_SECRET: env var → AUTH_TOKEN → default
	resolveAccountCredentialSecret := func() string {
		if v := get("ACCOUNT_CREDENTIAL_SECRET"); v != "" {
			return v
		}
		if v := get("AUTH_TOKEN"); v != "" {
			return v
		}
		return DefaultAuthToken
	}

	// Resolve PAYLOAD_RULES from legacy alias
	resolvePayloadRules := func() string {
		if v := get("PAYLOAD_RULES_JSON"); v != "" {
			return v
		}
		return get("PAYLOAD_RULES")
	}

	// Resolve OPENAI_SERVICE_TIER_RULES from legacy alias
	resolveOpenAiServiceTierRules := func() string {
		if v := get("OPENAI_SERVICE_TIER_RULES_JSON"); v != "" {
			return v
		}
		return get("OPENAI_SERVICE_TIER_RULES")
	}

	cfg := &Config{}

	// ---- §3.1 Auth ----
	cfg.AuthToken = firstNonEmpty(get("AUTH_TOKEN"), DefaultAuthToken)
	cfg.ProxyToken = firstNonEmpty(get("PROXY_TOKEN"), DefaultProxyToken)
	cfg.DeployHelperToken = parseOptionalSecret(resolveDeployHelperToken())
	cfg.AccountCredentialSecret = resolveAccountCredentialSecret()
	if cfg.AccountCredentialSecret == DefaultAuthToken {
		slog.Warn("config: AccountCredentialSecret is using the default value — this is insecure for production. Set ACCOUNT_CREDENTIAL_SECRET or AUTH_TOKEN environment variable.")
	}
	cfg.CodexClientId = firstNonEmpty(parseOptionalSecret(get("CODEX_CLIENT_ID")), DefaultCodexClientId)

	// ---- §3.2 OAuth Clients ----
	cfg.ClaudeClientId = firstNonEmpty(parseOptionalSecret(get("CLAUDE_CLIENT_ID")), DefaultClaudeClientId)
	cfg.ClaudeClientSecret = parseOptionalSecret(get("CLAUDE_CLIENT_SECRET"))
	cfg.GeminiCliClientId = firstNonEmpty(parseOptionalSecret(get("GEMINI_CLI_CLIENT_ID")), DefaultGeminiCliClientId)
	cfg.GeminiCliClientSecret = firstNonEmpty(parseOptionalSecret(get("GEMINI_CLI_CLIENT_SECRET")), DefaultGeminiCliClientSecret)

	// ---- §3.3 Server ----
	cfg.Port = int(math.Trunc(parseNumber(get("PORT"), DefaultPort)))
	cfg.ListenHost = parseListenHost(env)
	cfg.DataDir = firstNonEmpty(get("DATA_DIR"), DefaultDataDir)
	cfg.DbUrl = strings.TrimSpace(firstNonEmpty(get("DB_URL"), get("DATABASE_URL")))
	cfg.DbType = inferDbType(get("DB_TYPE"), cfg.DbUrl)
	cfg.DbSsl = parseBoolean(get("DB_SSL"), false)
	cfg.DbSslMode = normalizeDbSslMode(get("DB_SSLMODE"))
	cfg.DbMaxOpenConns = int(math.Trunc(parseNumber(get("DB_MAX_OPEN_CONNS"), 20)))
	cfg.DbMaxIdleConns = int(math.Trunc(parseNumber(get("DB_MAX_IDLE_CONNS"), 5)))
	cfg.DbConnMaxLifetimeSec = int(math.Trunc(parseNumber(get("DB_CONN_MAX_LIFETIME_SEC"), 1800)))
	cfg.DbConnMaxIdleTimeSec = int(math.Trunc(parseNumber(get("DB_CONN_MAX_IDLE_TIME_SEC"), 300)))
	cfg.Tz = get("TZ")

	// ---- §3.4 Cron ----
	cfg.CheckinCron = firstNonEmpty(get("CHECKIN_CRON"), DefaultCheckinCron)
	checkinMode := strings.ToLower(strings.TrimSpace(get("CHECKIN_SCHEDULE_MODE")))
	if checkinMode == "interval" {
		cfg.CheckinScheduleMode = "interval"
	} else {
		cfg.CheckinScheduleMode = "cron"
	}
	cfg.CheckinIntervalHours = clampInt(
		int(math.Trunc(parseNumber(get("CHECKIN_INTERVAL_HOURS"), DefaultCheckinIntervalHours))),
		1, 24,
	)
	cfg.BalanceRefreshCron = firstNonEmpty(get("BALANCE_REFRESH_CRON"), DefaultBalanceRefreshCron)
	cfg.LogCleanupCron = firstNonEmpty(get("LOG_CLEANUP_CRON"), DefaultLogCleanupCron)

	// ---- §3.5 Log Cleanup ----
	cfg.LogCleanupUsageLogsEnabled = parseBoolean(get("LOG_CLEANUP_USAGE_LOGS_ENABLED"), false)
	cfg.LogCleanupProgramLogsEnabled = parseBoolean(get("LOG_CLEANUP_PROGRAM_LOGS_ENABLED"), false)
	cfg.LogCleanupRetentionDays = maxInt(1, int(math.Trunc(parseNumber(get("LOG_CLEANUP_RETENTION_DAYS"), 30))))
	cfg.LogCleanupConfigured = false // set later during runtime settings hydration

	// ---- §3.6 Notify: Webhook ----
	cfg.WebhookUrl = firstNonEmpty(get("WEBHOOK_URL"), "")
	cfg.WebhookEnabled = parseBoolean(get("WEBHOOK_ENABLED"), true)

	// ---- §3.7 Notify: Bark ----
	cfg.BarkUrl = firstNonEmpty(get("BARK_URL"), "")
	cfg.BarkEnabled = parseBoolean(get("BARK_ENABLED"), true)

	// ---- §3.8 Notify: ServerChan ----
	cfg.ServerChanKey = firstNonEmpty(get("SERVERCHAN_KEY"), "")
	cfg.ServerChanEnabled = parseBoolean(get("SERVERCHAN_ENABLED"), true)

	// ---- §3.9 Notify: Telegram ----
	cfg.TelegramEnabled = parseBoolean(get("TELEGRAM_ENABLED"), false)
	cfg.TelegramApiBaseUrl = TelegramApiBaseUrl
	cfg.TelegramBotToken = firstNonEmpty(get("TELEGRAM_BOT_TOKEN"), "")
	cfg.TelegramChatId = firstNonEmpty(get("TELEGRAM_CHAT_ID"), "")
	cfg.TelegramUseSystemProxy = parseBoolean(get("TELEGRAM_USE_SYSTEM_PROXY"), false)
	cfg.TelegramMessageThreadId = strings.TrimSpace(get("TELEGRAM_MESSAGE_THREAD_ID"))

	// ---- §3.10 Notify: SMTP ----
	cfg.SmtpEnabled = parseBoolean(get("SMTP_ENABLED"), false)
	cfg.SmtpHost = firstNonEmpty(get("SMTP_HOST"), "")
	cfg.SmtpPort = atoiOr(get("SMTP_PORT"), DefaultSmtpPort)
	cfg.SmtpSecure = parseBoolean(get("SMTP_SECURE"), false)
	cfg.SmtpUser = firstNonEmpty(get("SMTP_USER"), "")
	cfg.SmtpPass = firstNonEmpty(get("SMTP_PASS"), "")
	cfg.SmtpFrom = firstNonEmpty(get("SMTP_FROM"), "")
	cfg.SmtpTo = firstNonEmpty(get("SMTP_TO"), "")

	// ---- §3.11 Notify: General ----
	cfg.NotifyCooldownSec = maxInt(0, int(math.Trunc(parseNumber(get("NOTIFY_COOLDOWN_SEC"), DefaultNotifyCooldownSec))))
	cfg.SystemProxyUrl = firstNonEmpty(get("SYSTEM_PROXY_URL"), "")
	cfg.RedisURL = firstNonEmpty(get("REDIS_URL"), get("METAPI_REDIS_URL"))

	// ---- §3.12 Admin ----
	cfg.AdminIpAllowlist = parseCsvList(get("ADMIN_IP_ALLOWLIST"))
	cfg.AdminCorsAllowedOrigins = parseCsvList(get("ADMIN_CORS_ALLOWED_ORIGINS"))
	cfg.TrustedProxyCidrs = parseCsvList(get("TRUSTED_PROXY_CIDRS"))

	// ---- §3.13 Proxy: Core ----
	cfg.RequestBodyLimit = DefaultRequestBodyLimit
	cfg.RoutingFallbackUnitCost = math.Max(1e-6, parseNumber(get("ROUTING_FALLBACK_UNIT_COST"), 1))
	// Seconds; internal first-byte observation uses ms = sec * 1000.
	cfg.ProxyFirstByteTimeoutSec = maxInt(0, int(math.Trunc(parseNumber(get("PROXY_FIRST_BYTE_TIMEOUT_SEC"), 0))))

	// ---- §3.14 Proxy: Token Router ----
	tokenRouterParsed := parseNumber(get("TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC"), float64(TokenRouterFailureCooldownMaxSecCeiling))
	if val, ok := normalizeTokenRouterFailureCooldownMaxSec(tokenRouterParsed); ok {
		cfg.TokenRouterFailureCooldownMaxSec = val
	} else {
		cfg.TokenRouterFailureCooldownMaxSec = TokenRouterFailureCooldownMaxSecCeiling
	}
	cfg.TokenRouterCacheTtlMs = maxInt(100, int(math.Trunc(parseNumber(get("TOKEN_ROUTER_CACHE_TTL_MS"), DefaultTokenRouterCacheTtlMs))))

	// ---- §3.15 Proxy: Channel ----
	cfg.ProxyMaxChannelAttempts = maxInt(1, int(math.Trunc(parseNumber(get("PROXY_MAX_CHANNEL_ATTEMPTS"), DefaultProxyMaxChannelAttempts))))
	cfg.ProxyStickySessionEnabled = parseBoolean(get("PROXY_STICKY_SESSION_ENABLED"), true)
	cfg.ProxyStickySessionTtlMs = maxInt(30000, int(math.Trunc(parseNumber(get("PROXY_STICKY_SESSION_TTL_MS"), float64(DefaultProxyStickySessionTtlMs)))))

	// ---- §3.16 Proxy: Session ----
	cfg.ProxySessionChannelConcurrencyLimit = maxInt(0, int(math.Trunc(parseNumber(get("PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT"), DefaultProxySessionChannelConcurrencyLimit))))
	cfg.ProxySessionChannelQueueWaitMs = maxInt(0, int(math.Trunc(parseNumber(get("PROXY_SESSION_CHANNEL_QUEUE_WAIT_MS"), DefaultProxySessionChannelQueueWaitMs))))
	cfg.ProxySessionChannelLeaseTtlMs = maxInt(5000, int(math.Trunc(parseNumber(get("PROXY_SESSION_CHANNEL_LEASE_TTL_MS"), DefaultProxySessionChannelLeaseTtlMs))))
	cfg.ProxySessionChannelLeaseKeepaliveMs = maxInt(1000, int(math.Trunc(parseNumber(get("PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS"), DefaultProxySessionChannelLeaseKeepaliveMs))))

	// ---- §3.17 Proxy: Misc ----
	cfg.CodexUpstreamWebsocketEnabled = parseBoolean(get("CODEX_UPSTREAM_WEBSOCKET_ENABLED"), false)
	cfg.ResponsesCompactFallbackToResponsesEnabled = parseBoolean(get("RESPONSES_COMPACT_FALLBACK_TO_RESPONSES_ENABLED"), false)
	cfg.DisableCrossProtocolFallback = parseBoolean(get("DISABLE_CROSS_PROTOCOL_FALLBACK"), false)
	cfg.ProxyEmptyContentFailEnabled = parseBoolean(get("PROXY_EMPTY_CONTENT_FAIL"), false)
	cfg.ProxyErrorKeywords = parseCsvList(get("PROXY_ERROR_KEYWORDS"))
	cfg.GlobalBlockedBrands = []string{}
	cfg.GlobalAllowedModels = []string{}

	// ---- §3.18 Proxy: Debug ----
	cfg.ProxyDebugTraceEnabled = parseBoolean(get("PROXY_DEBUG_TRACE_ENABLED"), false)
	cfg.ProxyDebugCaptureHeaders = parseBoolean(get("PROXY_DEBUG_CAPTURE_HEADERS"), true)
	cfg.ProxyDebugCaptureBodies = parseBoolean(get("PROXY_DEBUG_CAPTURE_BODIES"), false)
	cfg.ProxyDebugCaptureStreamChunks = parseBoolean(get("PROXY_DEBUG_CAPTURE_STREAM_CHUNKS"), false)
	cfg.ProxyDebugTargetSessionId = strings.TrimSpace(get("PROXY_DEBUG_TARGET_SESSION_ID"))
	cfg.ProxyDebugTargetClientKind = strings.TrimSpace(get("PROXY_DEBUG_TARGET_CLIENT_KIND"))
	cfg.ProxyDebugTargetModel = strings.TrimSpace(get("PROXY_DEBUG_TARGET_MODEL"))
	cfg.ProxyDebugRetentionHours = maxInt(1, int(math.Trunc(parseNumber(get("PROXY_DEBUG_RETENTION_HOURS"), DefaultProxyDebugRetentionHours))))
	cfg.ProxyDebugMaxBodyBytes = maxInt(1024, int(math.Trunc(parseNumber(get("PROXY_DEBUG_MAX_BODY_BYTES"), DefaultProxyDebugMaxBodyBytes))))

	// ---- §3.19 Codex-specific ----
	cfg.CodexResponsesWebsocketBeta = firstNonEmpty(
		parseOptionalSecret(get("CODEX_RESPONSES_WEBSOCKET_BETA")),
		"responses_websockets=2026-02-06",
	)
	cfg.CodexHeaderDefaults = CodexHeaderDefaults{
		UserAgent:    parseOptionalSecret(get("CODEX_HEADER_DEFAULTS_USER_AGENT")),
		BetaFeatures: parseOptionalSecret(get("CODEX_HEADER_DEFAULTS_BETA_FEATURES")),
	}

	// ---- §3.20 Model Probe ----
	cfg.ModelAvailabilityProbeEnabled = parseBoolean(get("MODEL_AVAILABILITY_PROBE_ENABLED"), false)
	cfg.ModelAvailabilityProbeIntervalMs = maxInt(60000, int(math.Trunc(parseNumber(get("MODEL_AVAILABILITY_PROBE_INTERVAL_MS"), float64(DefaultModelAvailabilityProbeIntervalMs)))))
	cfg.ModelAvailabilityProbeTimeoutMs = maxInt(3000, int(math.Trunc(parseNumber(get("MODEL_AVAILABILITY_PROBE_TIMEOUT_MS"), DefaultModelAvailabilityProbeTimeoutMs))))
	cfg.ModelAvailabilityProbeConcurrency = clampInt(
		int(math.Trunc(parseNumber(get("MODEL_AVAILABILITY_PROBE_CONCURRENCY"), DefaultModelAvailabilityProbeConcurrency))),
		1, 16,
	)

	// ---- §3.21 Retention ----
	cfg.ProxyLogRetentionDays = maxInt(0, int(math.Trunc(parseNumber(get("PROXY_LOG_RETENTION_DAYS"), DefaultProxyLogRetentionDays))))
	cfg.ProxyLogRetentionPruneIntervalMinutes = maxInt(1, int(math.Trunc(parseNumber(get("PROXY_LOG_RETENTION_PRUNE_INTERVAL_MINUTES"), float64(DefaultProxyLogRetentionPruneIntervalMinutes)))))
	cfg.ProxyFileRetentionDays = maxInt(0, int(math.Trunc(parseNumber(get("PROXY_FILE_RETENTION_DAYS"), DefaultProxyFileRetentionDays))))
	cfg.ProxyFileRetentionPruneIntervalMinutes = maxInt(1, int(math.Trunc(parseNumber(get("PROXY_FILE_RETENTION_PRUNE_INTERVAL_MINUTES"), float64(DefaultProxyFileRetentionPruneIntervalMinutes)))))
	cfg.ProxyVideoTaskRetentionDays = maxInt(0, int(math.Trunc(parseNumber(get("PROXY_VIDEO_TASK_RETENTION_DAYS"), float64(DefaultProxyVideoTaskRetentionDays)))))
	cfg.ProxyVideoTaskRetentionPruneIntervalMinutes = maxInt(1, int(math.Trunc(parseNumber(get("PROXY_VIDEO_TASK_RETENTION_PRUNE_INTERVAL_MINUTES"), float64(DefaultProxyVideoTaskRetentionPruneIntervalMinutes)))))

	// ---- §3.22 Routing Weights ----
	cfg.RoutingWeights = RoutingWeights{
		BaseWeightFactor: parseNumber(get("BASE_WEIGHT_FACTOR"), 0.5),
		ValueScoreFactor: parseNumber(get("VALUE_SCORE_FACTOR"), 0.5),
		CostWeight:       parseNumber(get("COST_WEIGHT"), 0.4),
		BalanceWeight:    parseNumber(get("BALANCE_WEIGHT"), 0.3),
		UsageWeight:      parseNumber(get("USAGE_WEIGHT"), 0.3),
	}

	// ---- §3.23 Payload Rules + Service Tier ----
	cfg.PayloadRules = parseJsonValue(resolvePayloadRules())
	cfg.OpenAiServiceTierRules = parseJsonValue(resolveOpenAiServiceTierRules())

	return cfg
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// firstNonEmpty returns a if non-empty, otherwise b.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MaxInt returns the larger of a and b. Exported for use by other packages.
func MaxInt(a, b int) int {
	return maxInt(a, b)
}

// ParseJsonValue parses a JSON string into an any value.
// Returns nil on empty input or parse error.
func ParseJsonValue(value string) any {
	if value == "" {
		return nil
	}
	var result any
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return nil
	}
	return result
}

// atoiOr parses s as int, returning fallback on failure.
func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
