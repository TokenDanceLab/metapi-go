package service

import (
	"encoding/json"
	"testing"
)

func healthBoolPtr(v bool) *bool { return &v }

func TestBuildRuntimeHealthForAccount_ExpiredOverridesHealthyStored(t *testing.T) {
	extra := `{"credentialMode":"apikey","runtimeHealth":{"state":"healthy","reason":"模型探测成功","source":"model-discovery","checkedAt":"2026-04-01T10:00:00.000Z"}}`
	health := BuildRuntimeHealthForAccount(RuntimeHealthInput{
		AccountStatus:       "expired",
		SiteStatus:          "active",
		ExtraConfig:         &extra,
		SessionCapable:      healthBoolPtr(false),
		HasDiscoveredModels: true,
	})
	if health.State != HealthUnhealthy {
		t.Fatalf("state = %q, want unhealthy", health.State)
	}
	if health.Source != HealthSourceAuth {
		t.Fatalf("source = %q, want auth", health.Source)
	}
	if health.Reason != "连接已过期，请更新 API Key" {
		t.Fatalf("reason = %q, want API Key expiry message", health.Reason)
	}
}

func TestBuildRuntimeHealthForAccount_ExpiredSessionMessage(t *testing.T) {
	extra := `{"credentialMode":"session","runtimeHealth":{"state":"healthy","reason":"余额刷新成功","source":"balance"}}`
	health := BuildRuntimeHealthForAccount(RuntimeHealthInput{
		AccountStatus:  "expired",
		SiteStatus:     "active",
		ExtraConfig:    &extra,
		SessionCapable: healthBoolPtr(true),
	})
	if health.State != HealthUnhealthy {
		t.Fatalf("state = %q, want unhealthy", health.State)
	}
	if health.Reason != "访问令牌已过期" {
		t.Fatalf("reason = %q, want session expiry message", health.Reason)
	}
	if health.Source != HealthSourceAuth {
		t.Fatalf("source = %q, want auth", health.Source)
	}
}

func TestBuildRuntimeHealthForAccount_ExpiredOauthMessage(t *testing.T) {
	extra := `{"oauth":{"provider":"codex"},"runtimeHealth":{"state":"healthy","reason":"ok","source":"balance"}}`
	provider := "codex"
	health := BuildRuntimeHealthForAccount(RuntimeHealthInput{
		AccountStatus: "expired",
		SiteStatus:    "active",
		ExtraConfig:   &extra,
		OAuthProvider: &provider,
	})
	if health.State != HealthUnhealthy {
		t.Fatalf("state = %q, want unhealthy", health.State)
	}
	if health.Reason != "连接凭证已过期，请更新凭证" {
		t.Fatalf("reason = %q", health.Reason)
	}
}

func TestBuildRuntimeHealthForAccount_DisabledPrecedence(t *testing.T) {
	health := BuildRuntimeHealthForAccount(RuntimeHealthInput{
		AccountStatus: "expired",
		SiteStatus:    "disabled",
	})
	if health.State != HealthDisabled {
		t.Fatalf("state = %q, want disabled when site disabled", health.State)
	}
}

func TestBuildRuntimeHealthForAccount_UsesStoredHealthyWhenActive(t *testing.T) {
	extra := `{"runtimeHealth":{"state":"healthy","reason":"余额刷新成功","source":"balance","checkedAt":"2026-02-25T12:00:00.000Z"}}`
	health := BuildRuntimeHealthForAccount(RuntimeHealthInput{
		AccountStatus: "active",
		SiteStatus:    "active",
		ExtraConfig:   &extra,
	})
	if health.State != HealthHealthy {
		t.Fatalf("state = %q, want healthy", health.State)
	}
	if health.Reason != "余额刷新成功" {
		t.Fatalf("reason = %q", health.Reason)
	}
}

func TestExtractRuntimeHealth_RoundTrip(t *testing.T) {
	raw := map[string]any{
		"runtimeHealth": map[string]any{
			"state":  "healthy",
			"reason": "ok",
			"source": "balance",
		},
	}
	b, _ := json.Marshal(raw)
	s := string(b)
	got := ExtractRuntimeHealth(&s)
	if got == nil || got.State != HealthHealthy {
		t.Fatalf("ExtractRuntimeHealth = %#v", got)
	}
}
