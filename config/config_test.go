package config

import (
	"strings"
	"testing"
)

func TestLoadParsesRedisURL(t *testing.T) {
	cfg := Load(map[string]string{
		"REDIS_URL": "redis://:secret@127.0.0.1:6379/1",
	})
	if cfg.RedisURL != "redis://:secret@127.0.0.1:6379/1" {
		t.Fatalf("RedisURL = %q", cfg.RedisURL)
	}
}

func TestLoadRedisURLEmptyByDefault(t *testing.T) {
	cfg := Load(map[string]string{})
	if cfg.RedisURL != "" {
		t.Fatalf("RedisURL default = %q, want empty", cfg.RedisURL)
	}
}

func TestLoadParsesAdminCorsAllowedOrigins(t *testing.T) {
	cfg := Load(map[string]string{
		"ADMIN_CORS_ALLOWED_ORIGINS": " https://admin.example.com,https://ops.example.com ,, ",
	})

	want := []string{"https://admin.example.com", "https://ops.example.com"}
	if len(cfg.AdminCorsAllowedOrigins) != len(want) {
		t.Fatalf("AdminCorsAllowedOrigins length = %d, want %d: %#v", len(cfg.AdminCorsAllowedOrigins), len(want), cfg.AdminCorsAllowedOrigins)
	}
	for i := range want {
		if cfg.AdminCorsAllowedOrigins[i] != want[i] {
			t.Fatalf("AdminCorsAllowedOrigins[%d] = %q, want %q", i, cfg.AdminCorsAllowedOrigins[i], want[i])
		}
	}
}

func TestLoadParsesTrustedProxyCidrs(t *testing.T) {
	cfg := Load(map[string]string{
		"TRUSTED_PROXY_CIDRS": " 127.0.0.1/32,10.0.0.0/8 ,, ",
	})

	want := []string{"127.0.0.1/32", "10.0.0.0/8"}
	if len(cfg.TrustedProxyCidrs) != len(want) {
		t.Fatalf("TrustedProxyCidrs length = %d, want %d: %#v", len(cfg.TrustedProxyCidrs), len(want), cfg.TrustedProxyCidrs)
	}
	for i := range want {
		if cfg.TrustedProxyCidrs[i] != want[i] {
			t.Fatalf("TrustedProxyCidrs[%d] = %q, want %q", i, cfg.TrustedProxyCidrs[i], want[i])
		}
	}
}

func TestLoadInfersPostgresFromDatabaseURLAlias(t *testing.T) {
	cfg := Load(map[string]string{
		"DATABASE_URL": "postgres://user:pass@example.com:5432/metapi?sslmode=require",
	})

	if cfg.DbType != "postgres" {
		t.Fatalf("DbType = %q, want postgres", cfg.DbType)
	}
	if cfg.DbUrl != "postgres://user:pass@example.com:5432/metapi?sslmode=require" {
		t.Fatalf("DbUrl = %q, want DATABASE_URL value", cfg.DbUrl)
	}
}

func TestLoadPrefersDBURLOverDatabaseURLAlias(t *testing.T) {
	cfg := Load(map[string]string{
		"DB_URL":       "sqlite://local.db",
		"DATABASE_URL": "postgres://user:pass@example.com:5432/metapi",
	})

	if cfg.DbType != "sqlite" {
		t.Fatalf("DbType = %q, want sqlite", cfg.DbType)
	}
	if cfg.DbUrl != "sqlite://local.db" {
		t.Fatalf("DbUrl = %q, want DB_URL value", cfg.DbUrl)
	}
}

func TestLoadParsesPostgresSSLMode(t *testing.T) {
	cfg := Load(map[string]string{
		"DB_SSL":     "true",
		"DB_SSLMODE": "verify-full",
	})

	if cfg.DbSslMode != "verify-full" {
		t.Fatalf("DbSslMode = %q, want verify-full", cfg.DbSslMode)
	}
	if got := cfg.PostgresSSLMode(); got != "verify-full" {
		t.Fatalf("PostgresSSLMode() = %q, want verify-full", got)
	}
}

func TestPostgresSSLModePreservesLegacyDBSSL(t *testing.T) {
	cfg := Load(map[string]string{
		"DB_SSL": "true",
	})

	if got := cfg.PostgresSSLMode(); got != "require" {
		t.Fatalf("PostgresSSLMode() = %q, want require", got)
	}
}

func TestValidateRejectsInvalidPostgresSSLMode(t *testing.T) {
	cfg := Load(map[string]string{
		"AUTH_TOKEN":                "admin-token",
		"PROXY_TOKEN":               "proxy-token",
		"ACCOUNT_CREDENTIAL_SECRET": "credential-secret",
		"CLAUDE_CLIENT_ID":          "claude-client",
		"CODEX_CLIENT_ID":           "codex-client",
		"GEMINI_CLI_CLIENT_ID":      "gemini-client",
		"DB_SSLMODE":                "verify-maybe",
	})

	var found bool
	for _, err := range cfg.Validate() {
		if strings.Contains(err.Error(), "db_sslmode") && IsCritical(err) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Validate did not return critical db_sslmode error")
	}
}

func TestValidateRejectsDefaultTokensAsCritical(t *testing.T) {
	cfg := Load(map[string]string{
		"ACCOUNT_CREDENTIAL_SECRET": "credential-secret",
		"CLAUDE_CLIENT_ID":          "claude-client",
		"CODEX_CLIENT_ID":           "codex-client",
		"GEMINI_CLI_CLIENT_ID":      "gemini-client",
	})

	for _, field := range []string{"auth_token", "proxy_token"} {
		t.Run(field, func(t *testing.T) {
			var found bool
			for _, err := range cfg.Validate() {
				if strings.Contains(err.Error(), field) && IsCritical(err) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Validate did not return critical %s error for default token", field)
			}
		})
	}
}

func TestValidateAcceptsExplicitNonDefaultTokens(t *testing.T) {
	cfg := Load(map[string]string{
		"AUTH_TOKEN":                "admin-token",
		"PROXY_TOKEN":               "proxy-token",
		"ACCOUNT_CREDENTIAL_SECRET": "credential-secret",
		"CLAUDE_CLIENT_ID":          "claude-client",
		"CODEX_CLIENT_ID":           "codex-client",
		"GEMINI_CLI_CLIENT_ID":      "gemini-client",
	})

	for _, err := range cfg.Validate() {
		if strings.Contains(err.Error(), "auth_token") || strings.Contains(err.Error(), "proxy_token") {
			t.Fatalf("Validate returned token error for explicit non-default tokens: %v", err)
		}
	}
}

func TestValidateRejectsUnsafeAdminCorsOrigins(t *testing.T) {
	for _, origin := range []string{"*", "https://*.example.com", "https://admin.example.com/path", "javascript:alert(1)"} {
		t.Run(origin, func(t *testing.T) {
			cfg := Load(map[string]string{
				"AUTH_TOKEN":                 "admin-token",
				"PROXY_TOKEN":                "proxy-token",
				"ACCOUNT_CREDENTIAL_SECRET":  "credential-secret",
				"CLAUDE_CLIENT_ID":           "claude-client",
				"CODEX_CLIENT_ID":            "codex-client",
				"GEMINI_CLI_CLIENT_ID":       "gemini-client",
				"ADMIN_CORS_ALLOWED_ORIGINS": origin,
			})

			var found bool
			for _, err := range cfg.Validate() {
				if strings.Contains(err.Error(), "admin_cors_allowed_origins") && IsCritical(err) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Validate did not return critical admin CORS error for %q", origin)
			}
		})
	}
}

func TestValidateAcceptsExactAdminCorsOrigins(t *testing.T) {
	cfg := Load(map[string]string{
		"AUTH_TOKEN":                 "admin-token",
		"PROXY_TOKEN":                "proxy-token",
		"ACCOUNT_CREDENTIAL_SECRET":  "credential-secret",
		"CLAUDE_CLIENT_ID":           "claude-client",
		"CODEX_CLIENT_ID":            "codex-client",
		"GEMINI_CLI_CLIENT_ID":       "gemini-client",
		"ADMIN_CORS_ALLOWED_ORIGINS": "https://admin.example.com,http://localhost:5173",
	})

	for _, err := range cfg.Validate() {
		if strings.Contains(err.Error(), "admin_cors_allowed_origins") {
			t.Fatalf("Validate returned admin CORS error for exact origins: %v", err)
		}
	}
}

func TestValidateRejectsInvalidTrustedProxyCidrs(t *testing.T) {
	cfg := Load(map[string]string{
		"AUTH_TOKEN":                "admin-token",
		"PROXY_TOKEN":               "proxy-token",
		"ACCOUNT_CREDENTIAL_SECRET": "credential-secret",
		"CLAUDE_CLIENT_ID":          "claude-client",
		"CODEX_CLIENT_ID":           "codex-client",
		"GEMINI_CLI_CLIENT_ID":      "gemini-client",
		"TRUSTED_PROXY_CIDRS":       "127.0.0.1/32,not-a-cidr",
	})

	var found bool
	for _, err := range cfg.Validate() {
		if strings.Contains(err.Error(), "trusted_proxy_cidrs") && IsCritical(err) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Validate did not return critical trusted proxy CIDR error")
	}
}
