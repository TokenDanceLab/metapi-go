package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/robfig/cron/v3"
)

// Validate checks Config fields and returns all validation errors.
// Callers should treat these as a single report — log warnings for
// non-fatal issues and exit on critical ones before binding the port.
func (c *Config) Validate() []error {
	var errs []error

	// --- Critical: Port must be in [1, 65535] ---
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, &configError{
			field:    "port",
			value:    fmt.Sprintf("%d", c.Port),
			msg:      "must be in [1, 65535]",
			critical: true,
		})
	}

	// --- Critical: CheckinScheduleMode must be "cron" or "interval" ---
	mode := strings.TrimSpace(strings.ToLower(c.CheckinScheduleMode))
	if mode != "cron" && mode != "interval" {
		errs = append(errs, &configError{
			field:    "checkin_schedule_mode",
			value:    c.CheckinScheduleMode,
			msg:      "must be 'cron' or 'interval'",
			critical: true,
		})
	}

	// --- Critical: DBType must be "sqlite" or "postgres" ---
	dbType := strings.TrimSpace(strings.ToLower(c.DbType))
	if dbType != "sqlite" && dbType != "postgres" {
		errs = append(errs, &configError{
			field:    "db_type",
			value:    c.DbType,
			msg:      "must be 'sqlite' or 'postgres'",
			critical: true,
		})
	}
	if !validDbSslMode(c.DbSslMode) {
		errs = append(errs, &configError{
			field:    "db_sslmode",
			value:    c.DbSslMode,
			msg:      "must be one of disable, allow, prefer, require, verify-ca, or verify-full",
			critical: true,
		})
	}
	if c.DbMaxOpenConns < 1 {
		errs = append(errs, &configError{
			field:    "db_max_open_conns",
			value:    fmt.Sprintf("%d", c.DbMaxOpenConns),
			msg:      "must be >= 1",
			critical: true,
		})
	}
	if c.DbMaxIdleConns < 0 || c.DbMaxIdleConns > c.DbMaxOpenConns {
		errs = append(errs, &configError{
			field:    "db_max_idle_conns",
			value:    fmt.Sprintf("%d", c.DbMaxIdleConns),
			msg:      "must be between 0 and db_max_open_conns",
			critical: true,
		})
	}
	if c.DbConnMaxLifetimeSec < 0 {
		errs = append(errs, &configError{
			field:    "db_conn_max_lifetime_sec",
			value:    fmt.Sprintf("%d", c.DbConnMaxLifetimeSec),
			msg:      "must be >= 0",
			critical: true,
		})
	}
	if c.DbConnMaxIdleTimeSec < 0 {
		errs = append(errs, &configError{
			field:    "db_conn_max_idle_time_sec",
			value:    fmt.Sprintf("%d", c.DbConnMaxIdleTimeSec),
			msg:      "must be >= 0",
			critical: true,
		})
	}

	// --- Warning: Cron expressions must be parseable ---
	if !validateCronExpr(c.CheckinCron) {
		errs = append(errs, &configError{
			field:    "checkin_cron",
			value:    c.CheckinCron,
			msg:      "invalid cron expression",
			critical: false,
		})
	}
	if !validateCronExpr(c.BalanceRefreshCron) {
		errs = append(errs, &configError{
			field:    "balance_refresh_cron",
			value:    c.BalanceRefreshCron,
			msg:      "invalid cron expression",
			critical: false,
		})
	}
	if !validateCronExpr(c.LogCleanupCron) {
		errs = append(errs, &configError{
			field:    "log_cleanup_cron",
			value:    c.LogCleanupCron,
			msg:      "invalid cron expression",
			critical: false,
		})
	}

	// --- Warning: NotifyCooldownSec >= 0 ---
	if c.NotifyCooldownSec < 0 {
		errs = append(errs, &configError{
			field:    "notify_cooldown_sec",
			value:    fmt.Sprintf("%d", c.NotifyCooldownSec),
			msg:      "must be >= 0",
			critical: false,
		})
	}

	// --- Warning: ProxyFirstByteTimeoutSec >= 0 ---
	if c.ProxyFirstByteTimeoutSec < 0 {
		errs = append(errs, &configError{
			field:    "proxy_first_byte_timeout_sec",
			value:    fmt.Sprintf("%d", c.ProxyFirstByteTimeoutSec),
			msg:      "must be >= 0",
			critical: false,
		})
	}

	// --- Warning: TokenRouterFailureCooldownMaxSec >= 0 ---
	if c.TokenRouterFailureCooldownMaxSec < 0 {
		errs = append(errs, &configError{
			field:    "token_router_failure_cooldown_max_sec",
			value:    fmt.Sprintf("%d", c.TokenRouterFailureCooldownMaxSec),
			msg:      "must be >= 0",
			critical: false,
		})
	}

	// --- Critical: Admin CORS origins must be exact http(s) origins ---
	for _, origin := range c.AdminCorsAllowedOrigins {
		if !validateAdminCorsOrigin(origin) {
			errs = append(errs, &configError{
				field:    "admin_cors_allowed_origins",
				value:    origin,
				msg:      "must be exact http(s) origins without wildcards, paths, query strings, or fragments",
				critical: true,
			})
		}
	}

	// --- Critical: Trusted proxy CIDRs must be parseable IP prefixes ---
	for _, cidr := range c.TrustedProxyCidrs {
		if _, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err != nil {
			errs = append(errs, &configError{
				field:    "trusted_proxy_cidrs",
				value:    cidr,
				msg:      "must be valid IP CIDR prefixes",
				critical: true,
			})
		}
	}

	// --- Warning: CheckinIntervalHours in [1, 24] ---
	if c.CheckinIntervalHours < 1 || c.CheckinIntervalHours > 24 {
		errs = append(errs, &configError{
			field:    "checkin_interval_hours",
			value:    fmt.Sprintf("%d", c.CheckinIntervalHours),
			msg:      "must be in [1, 24]",
			critical: false,
		})
	}

	// --- Warning: RoutingWeights all >= 0 ---
	rw := c.RoutingWeights
	if rw.BaseWeightFactor < 0 {
		errs = append(errs, &configError{
			field:    "base_weight_factor",
			value:    fmt.Sprintf("%f", rw.BaseWeightFactor),
			msg:      "must be >= 0",
			critical: false,
		})
	}
	if rw.ValueScoreFactor < 0 {
		errs = append(errs, &configError{
			field:    "value_score_factor",
			value:    fmt.Sprintf("%f", rw.ValueScoreFactor),
			msg:      "must be >= 0",
			critical: false,
		})
	}
	if rw.CostWeight < 0 {
		errs = append(errs, &configError{
			field:    "cost_weight",
			value:    fmt.Sprintf("%f", rw.CostWeight),
			msg:      "must be >= 0",
			critical: false,
		})
	}
	if rw.BalanceWeight < 0 {
		errs = append(errs, &configError{
			field:    "balance_weight",
			value:    fmt.Sprintf("%f", rw.BalanceWeight),
			msg:      "must be >= 0",
			critical: false,
		})
	}
	if rw.UsageWeight < 0 {
		errs = append(errs, &configError{
			field:    "usage_weight",
			value:    fmt.Sprintf("%f", rw.UsageWeight),
			msg:      "must be >= 0",
			critical: false,
		})
	}

	// --- Critical: Default AUTH_TOKEN / PROXY_TOKEN ---
	if c.AuthToken == DefaultAuthToken {
		errs = append(errs, &configError{
			field:    "auth_token",
			value:    "(default)",
			msg:      "UNSAFE: using default admin token — set AUTH_TOKEN",
			critical: true,
		})
	}
	if c.ProxyToken == DefaultProxyToken {
		errs = append(errs, &configError{
			field:    "proxy_token",
			value:    "(default)",
			msg:      "UNSAFE: using default proxy token — set PROXY_TOKEN",
			critical: true,
		})
	}

	// --- Warning: account_credential_secret fallback ---
	if c.AccountCredentialSecret == "" {
		errs = append(errs, &configError{
			field:    "account_credential_secret",
			value:    "(empty)",
			msg:      "UNSAFE: account credential encryption key not set — stored credentials are NOT encrypted",
			critical: false,
		})
	}

	// --- Warning: OAuth client IDs are placeholders ---
	if c.ClaudeClientId == "" || c.ClaudeClientId == DefaultClaudeClientId {
		errs = append(errs, &configError{
			field:    "claude_client_id",
			value:    "(placeholder)",
			msg:      "Claude OAuth login will fail — set CLAUDE_CLIENT_ID",
			critical: false,
		})
	}
	if c.CodexClientId == "" || c.CodexClientId == DefaultCodexClientId {
		errs = append(errs, &configError{
			field:    "codex_client_id",
			value:    "(placeholder)",
			msg:      "Codex OAuth login will fail — set CODEX_CLIENT_ID",
			critical: false,
		})
	}
	if c.GeminiCliClientId == "" || c.GeminiCliClientId == DefaultGeminiCliClientId {
		errs = append(errs, &configError{
			field:    "gemini_cli_client_id",
			value:    "(placeholder)",
			msg:      "Gemini CLI OAuth login will fail — set GEMINI_CLI_CLIENT_ID",
			critical: false,
		})
	}

	return errs
}

// configError implements the error interface and carries metadata for
// callers that need to distinguish critical from non-fatal issues.
type configError struct {
	field    string
	value    string
	msg      string
	critical bool
}

func (e *configError) Error() string {
	severity := "warning"
	if e.critical {
		severity = "critical"
	}
	return fmt.Sprintf("config %s: %s=%s — %s", severity, e.field, e.value, e.msg)
}

// IsCritical returns true if the error represents a fatal config issue.
func IsCritical(err error) bool {
	if ce, ok := err.(*configError); ok {
		return ce.critical
	}
	return false
}

// validateCronExpr checks if a cron expression is parseable. Auto-normalizes
// 5-field expressions (minute hour dom month dow) to 6-field (second ...)
// for compatibility with cron.WithSeconds(), matching the scheduler behavior.
func validateCronExpr(expr string) bool {
	if strings.TrimSpace(expr) == "" {
		return false
	}
	fields := strings.Fields(expr)
	if len(fields) == 5 {
		expr = "0 " + expr
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err == nil
}

func validateAdminCorsOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" || strings.Contains(origin, "*") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Path == "" || parsed.Path == "/"
}
