package config

// Default constants for MetAPI Go configuration.
// These mirror the original TypeScript runtime defaults.

const (
	DefaultAuthToken             = "change-me-admin-token"
	DefaultProxyToken            = "change-me-proxy-sk-token"
	DefaultCodexClientId         = "CODEX_CLIENT_ID_PLACEHOLDER"
	DefaultClaudeClientId        = "CLAUDE_CLIENT_ID_PLACEHOLDER"
	DefaultGeminiCliClientId     = "GEMINI_CLI_CLIENT_ID_PLACEHOLDER"
	DefaultGeminiCliClientSecret = "GEMINI_CLI_CLIENT_SECRET_PLACEHOLDER"

	DefaultPort    = 4000
	DefaultDataDir = "./data"
	DefaultDbType  = "sqlite"

	DefaultRequestBodyLimit = 20 * 1024 * 1024 // 20 MB

	TokenRouterFailureCooldownMaxSecCeiling = 30 * 24 * 60 * 60 // 30 days

	DefaultCheckinCron          = "0 8 * * *"
	DefaultCheckinIntervalHours = 6
	DefaultBalanceRefreshCron   = "0 * * * *"
	DefaultLogCleanupCron       = "0 6 * * *"

	DefaultNotifyCooldownSec = 300
	DefaultSmtpPort          = 587

	DefaultTokenRouterCacheTtlMs               = 1500
	DefaultProxyMaxChannelAttempts             = 3
	DefaultProxyStickySessionTtlMs             = 30 * 60 * 1000 // 30 minutes
	DefaultProxySessionChannelConcurrencyLimit = 2
	DefaultProxySessionChannelQueueWaitMs      = 1500
	DefaultProxySessionChannelLeaseTtlMs       = 90000
	DefaultProxySessionChannelLeaseKeepaliveMs = 15000

	DefaultModelAvailabilityProbeIntervalMs  = 30 * 60 * 1000 // 30 minutes
	DefaultModelAvailabilityProbeTimeoutMs   = 15000
	DefaultModelAvailabilityProbeConcurrency = 1

	DefaultProxyLogRetentionDays                  = 30
	DefaultProxyLogRetentionPruneIntervalMinutes  = 30
	DefaultProxyFileRetentionDays                 = 30
	DefaultProxyFileRetentionPruneIntervalMinutes = 60
	// Video task mappings: default 0 = retention disabled (no silent mass delete).
	DefaultProxyVideoTaskRetentionDays                 = 0
	DefaultProxyVideoTaskRetentionPruneIntervalMinutes = 60

	DefaultProxyDebugRetentionHours = 24
	DefaultProxyDebugMaxBodyBytes   = 262144

	TelegramApiBaseUrl = "https://api.telegram.org"
)
