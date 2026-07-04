package platform

import (
	"context"
	"testing"
	"time"
)

// TestAdapterInterfaceCompliance verifies PlatformName + Detect for all adapters.
// Full method compliance is verified in per-adapter tests.
func TestAdapterInterfaceCompliance(t *testing.T) {
	adapters := ListAdapters()
	if len(adapters) < 14 {
		t.Fatalf("expected at least 14 adapters in registry, got %d", len(adapters))
	}

	for _, a := range adapters {
		name := a.PlatformName()
		if name == "" {
			t.Error("adapter has empty PlatformName()")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		// Detect: keyword-based adapters are instant; HTTP-probe adapters fail quickly
		// on unreachable addresses.
		a.Detect(ctx, "http://127.0.0.1:1")

		cancel()
	}
}

// TestFullMethodCompliance calls every method on a small set of representative adapters.
func TestFullMethodCompliance(t *testing.T) {
	// Only test adapters whose methods are non-blocking (no discoverUserID loops).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	unreachable := "http://127.0.0.1:1"

	testMethodCompliance(t, ctx, &OpenAiAdapter{StandardAdapter: NewStandardAdapter("openai")}, unreachable)
	testMethodCompliance(t, ctx, &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}, unreachable)
	testMethodCompliance(t, ctx, &ClaudeAdapter{StandardAdapter: NewStandardAdapter("claude")}, unreachable)
	testMethodCompliance(t, ctx, &AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")}, unreachable)
	testMethodCompliance(t, ctx, &Sub2ApiAdapter{BaseAdapter: NewBaseAdapter("sub2api")}, unreachable)
	testMethodCompliance(t, ctx, &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}, unreachable)
}

func testMethodCompliance(t *testing.T, ctx context.Context, a PlatformAdapter, url string) {
	t.Helper()
	a.Detect(ctx, url)
	a.Login(ctx, url, "user", "pass", nil, nil)
	a.GetUserInfo(ctx, url, "token", nil, nil)
	a.VerifyToken(ctx, url, "token", nil, nil)
	a.Checkin(ctx, url, "token", nil, nil)
	a.GetBalance(ctx, url, "token", nil, nil)
	a.GetModels(ctx, url, "token", nil, nil)
	a.GetAPIToken(ctx, url, "token", nil, nil)
	a.GetAPITokens(ctx, url, "token", nil, nil)
	a.CreateAPIToken(ctx, url, "token", nil, nil, nil)
	a.DeleteAPIToken(ctx, url, "token", "key", nil, nil)
	a.GetSiteAnnouncements(ctx, url, "token", nil, nil)
	a.GetUserGroups(ctx, url, "token", nil, nil)
}

// TestRequiredPlatformNames verifies that all 14 canonical platform names are present.
func TestRequiredPlatformNames(t *testing.T) {
	required := map[string]bool{
		"openai": true, "claude": true, "codex": true, "gemini": true,
		"gemini-cli": true, "antigravity": true, "cliproxyapi": true,
		"anyrouter": true, "done-hub": true, "one-hub": true,
		"veloera": true, "new-api": true, "sub2api": true, "one-api": true,
	}

	seen := make(map[string]bool)
	for _, a := range ListAdapters() {
		seen[a.PlatformName()] = true
	}

	for name := range required {
		if !seen[name] {
			t.Errorf("required platform %q not found in registry", name)
		}
	}
}

// TestTypeDefinitions exercises the exported types.
func TestTypeDefinitions(t *testing.T) {
	// CheckinResult
	cr := CheckinResult{Success: true, Message: "ok", Reward: "10 USD"}
	if !cr.Success {
		t.Error("CheckinResult.Success should be true")
	}

	// BalanceInfo
	bi := BalanceInfo{Balance: 1.5, Used: 0.5, Quota: 2.0}
	if bi.Balance != 1.5 {
		t.Error("BalanceInfo.Balance mismatch")
	}

	// LoginResult
	lr := LoginResult{Success: true, AccessToken: "tok", Username: "u"}
	if lr.AccessToken != "tok" {
		t.Error("LoginResult.AccessToken mismatch")
	}

	// TokenVerifyResult
	tvr := TokenVerifyResult{TokenType: "session"}
	if tvr.TokenType != "session" {
		t.Error("TokenVerifyResult.TokenType mismatch")
	}

	// ApiTokenInfo
	ati := ApiTokenInfo{Name: "n", Key: "k", Enabled: true, TokenGroup: "g"}
	if ati.Key != "k" {
		t.Error("ApiTokenInfo.Key mismatch")
	}

	// SubscriptionPlanSummary
	sps := SubscriptionPlanSummary{ID: intPtr(1), GroupName: "default"}
	if *sps.ID != 1 {
		t.Error("SubscriptionPlanSummary.ID mismatch")
	}

	// SubscriptionSummary
	ss := SubscriptionSummary{ActiveCount: 3, TotalUsedUsd: 10.0}
	if ss.ActiveCount != 3 {
		t.Error("SubscriptionSummary.ActiveCount mismatch")
	}

	// SiteAnnouncement
	sa := SiteAnnouncement{SourceKey: "k", Title: "t", Content: "c", Level: "info"}
	if sa.Level != "info" {
		t.Error("SiteAnnouncement.Level mismatch")
	}

	// CreateAPITokenOptions
	opts := CreateAPITokenOptions{
		Name: "metapi", UnlimitedQuota: true, ExpiredTime: -1,
	}
	if opts.ExpiredTime != -1 {
		t.Error("CreateAPITokenOptions.ExpiredTime mismatch")
	}

	// ProxyConfig
	pc := ProxyConfig{ProxyURL: "socks5://proxy:1080", UseSystemProxy: true}
	if pc.ProxyURL != "socks5://proxy:1080" {
		t.Error("ProxyConfig.ProxyURL mismatch")
	}

	// CredentialMode
	if CredentialAuto != 0 || CredentialSession != 1 || CredentialAPIKey != 2 {
		t.Error("CredentialMode values mismatch")
	}
}

func intPtr(i int) *int { return &i }
