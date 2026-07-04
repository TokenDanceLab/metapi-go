package oauth

import (
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

func init() {
	// Ensure config singleton is set for provider tests that read client IDs.
	// Override defaults to avoid panics in require*ClientID() functions.
	config.Set(&config.Config{
		CodexClientId:         "test-codex-client-id",
		ClaudeClientId:        "test-claude-client-id",
		GeminiCliClientId:     "test-gemini-client-id",
		GeminiCliClientSecret: "test-gemini-client-secret",
	})
}

// ---- Registry Tests ----

func TestRegistry_AllFourProvidersRegistered(t *testing.T) {
	providers := []OAuthProviderId{ProviderCodex, ProviderClaude, ProviderGeminiCli, ProviderAntigravity}
	for _, pid := range providers {
		def := GetProviderDefinition(string(pid))
		if def == nil {
			t.Errorf("provider %q not registered", pid)
		}
	}
}

func TestRegistry_GetProviderDefinition_Missing(t *testing.T) {
	def := GetProviderDefinition("nonexistent")
	if def != nil {
		t.Error("expected nil for nonexistent provider")
	}
}

func TestRegistry_IsRegisteredProvider(t *testing.T) {
	if !IsRegisteredProvider("codex") {
		t.Error("codex should be registered")
	}
	if !IsRegisteredProvider("claude") {
		t.Error("claude should be registered")
	}
	if !IsRegisteredProvider("gemini-cli") {
		t.Error("gemini-cli should be registered")
	}
	if !IsRegisteredProvider("antigravity") {
		t.Error("antigravity should be registered")
	}
	if IsRegisteredProvider("nonexistent") {
		t.Error("nonexistent should not be registered")
	}
}

func TestRegistry_ListProviderDefinitions(t *testing.T) {
	defs := ListProviderDefinitions()
	if len(defs) != 4 {
		t.Errorf("expected 4 providers, got %d", len(defs))
	}
	seen := make(map[OAuthProviderId]bool)
	for _, def := range defs {
		seen[def.Metadata.Provider] = true
	}
	for _, pid := range []OAuthProviderId{ProviderCodex, ProviderClaude, ProviderGeminiCli, ProviderAntigravity} {
		if !seen[pid] {
			t.Errorf("provider %q missing from list", pid)
		}
	}
}

func TestRegistry_ListProviderDefinitions_ReturnsCopy(t *testing.T) {
	defs1 := ListProviderDefinitions()
	defs2 := ListProviderDefinitions()
	if len(defs1) > 0 {
		defs1[0] = nil
	}
	if len(defs2) > 0 && defs2[0] == nil {
		t.Error("ListProviderDefinitions should return a copy, not a reference")
	}
}

// ---- Metadata Tests ----

func TestProviderMetadata_Codex(t *testing.T) {
	def := GetProviderDefinition(string(ProviderCodex))
	if def == nil {
		t.Fatal("codex provider not found")
	}
	meta := def.Metadata
	if meta.Provider != ProviderCodex {
		t.Errorf("expected Provider %q, got %q", ProviderCodex, meta.Provider)
	}
	if meta.Label != "Codex" {
		t.Errorf("expected Label 'Codex', got %q", meta.Label)
	}
	if meta.Platform != "codex" {
		t.Errorf("expected Platform 'codex', got %q", meta.Platform)
	}
	if !meta.Enabled {
		t.Error("codex should be enabled")
	}
	if meta.LoginType != "oauth" {
		t.Errorf("expected LoginType 'oauth', got %q", meta.LoginType)
	}
	if meta.RequiresProjectId {
		t.Error("codex should not require project ID")
	}
	if !meta.SupportsDirectAccountRouting {
		t.Error("codex should support direct account routing")
	}
}

func TestProviderMetadata_Claude(t *testing.T) {
	def := GetProviderDefinition(string(ProviderClaude))
	if def == nil {
		t.Fatal("claude provider not found")
	}
	meta := def.Metadata
	if meta.Provider != ProviderClaude {
		t.Errorf("expected Provider %q, got %q", ProviderClaude, meta.Provider)
	}
	if meta.Label != "Claude" {
		t.Errorf("expected Label 'Claude', got %q", meta.Label)
	}
	if meta.RequiresProjectId {
		t.Error("claude should not require project ID")
	}
	if def.Site.Platform != "claude" {
		t.Errorf("expected site platform 'claude', got %q", def.Site.Platform)
	}
}

func TestProviderMetadata_GeminiCli(t *testing.T) {
	def := GetProviderDefinition(string(ProviderGeminiCli))
	if def == nil {
		t.Fatal("gemini-cli provider not found")
	}
	meta := def.Metadata
	if meta.Provider != ProviderGeminiCli {
		t.Errorf("expected Provider %q, got %q", ProviderGeminiCli, meta.Provider)
	}
	if meta.Label != "Gemini CLI" {
		t.Errorf("expected Label 'Gemini CLI', got %q", meta.Label)
	}
	if !meta.RequiresProjectId {
		t.Error("gemini-cli should require project ID")
	}
}

func TestProviderMetadata_Antigravity(t *testing.T) {
	def := GetProviderDefinition(string(ProviderAntigravity))
	if def == nil {
		t.Fatal("antigravity provider not found")
	}
	meta := def.Metadata
	if meta.Provider != ProviderAntigravity {
		t.Errorf("expected Provider %q, got %q", ProviderAntigravity, meta.Provider)
	}
	if meta.Label != "Antigravity" {
		t.Errorf("expected Label 'Antigravity', got %q", meta.Label)
	}
	if meta.RequiresProjectId {
		t.Error("antigravity should not require project ID")
	}
}

// ---- Loopback Config Tests ----

func TestLoopbackConfig_Codex(t *testing.T) {
	def := GetProviderDefinition(string(ProviderCodex))
	if def.Loopback.Port != 1455 {
		t.Errorf("expected port 1455, got %d", def.Loopback.Port)
	}
	if def.Loopback.Path != "/auth/callback" {
		t.Errorf("expected path '/auth/callback', got %q", def.Loopback.Path)
	}
}

func TestLoopbackConfig_Claude(t *testing.T) {
	def := GetProviderDefinition(string(ProviderClaude))
	if def.Loopback.Port != 54545 {
		t.Errorf("expected port 54545, got %d", def.Loopback.Port)
	}
	if def.Loopback.Path != "/callback" {
		t.Errorf("expected path '/callback', got %q", def.Loopback.Path)
	}
}

func TestLoopbackConfig_GeminiCli(t *testing.T) {
	def := GetProviderDefinition(string(ProviderGeminiCli))
	if def.Loopback.Port != 8085 {
		t.Errorf("expected port 8085, got %d", def.Loopback.Port)
	}
	if def.Loopback.Path != "/oauth2callback" {
		t.Errorf("expected path '/oauth2callback', got %q", def.Loopback.Path)
	}
}

func TestLoopbackConfig_Antigravity(t *testing.T) {
	def := GetProviderDefinition(string(ProviderAntigravity))
	if def.Loopback.Port != 51121 {
		t.Errorf("expected port 51121, got %d", def.Loopback.Port)
	}
	if def.Loopback.Path != "/oauth-callback" {
		t.Errorf("expected path '/oauth-callback', got %q", def.Loopback.Path)
	}
}

// ---- Auth URL Construction Tests ----

func TestBuildAuthorizationURL_Codex_UsesPKCE(t *testing.T) {
	def := GetProviderDefinition(string(ProviderCodex))
	urlStr, err := def.BuildAuthorizationURL(nil, BuildAuthURLInput{
		State:        "test-state",
		RedirectURI:  "http://localhost:1455/auth/callback",
		CodeVerifier: "test-verifier",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urlStr == "" {
		t.Fatal("expected non-empty URL")
	}
	if !strings.Contains(urlStr, "code_challenge_method=S256") {
		t.Error("codex should use PKCE S256")
	}
	if !strings.Contains(urlStr, "code_challenge=") {
		t.Error("codex should include code_challenge")
	}
	if strings.Contains(urlStr, "code_verifier=") {
		t.Error("codex should NOT include raw code_verifier in URL")
	}
	if !strings.Contains(urlStr, "prompt=login") {
		t.Error("codex should include prompt=login")
	}
	if !strings.Contains(urlStr, "id_token_add_organizations=true") {
		t.Error("codex should include id_token_add_organizations=true")
	}
	if !strings.Contains(urlStr, "codex_cli_simplified_flow=true") {
		t.Error("codex should include codex_cli_simplified_flow=true")
	}
	if !strings.Contains(urlStr, "response_type=code") {
		t.Error("codex should use response_type=code")
	}
	if !strings.Contains(urlStr, "scope=openid+email+profile+offline_access") {
		t.Error("codex should have correct scopes")
	}
	if !strings.Contains(urlStr, "client_id=test-codex-client-id") {
		t.Error("codex should include client_id")
	}
}

func TestBuildAuthorizationURL_Claude_UsesPKCE(t *testing.T) {
	def := GetProviderDefinition(string(ProviderClaude))
	urlStr, err := def.BuildAuthorizationURL(nil, BuildAuthURLInput{
		State:        "test-state",
		RedirectURI:  "http://localhost:54545/callback",
		CodeVerifier: "test-verifier",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(urlStr, "code_challenge_method=S256") {
		t.Error("claude should use PKCE S256")
	}
	if !strings.Contains(urlStr, "code_challenge=") {
		t.Error("claude should include code_challenge")
	}
	if !strings.Contains(urlStr, "code=true") {
		t.Error("claude should include code=true param")
	}
	if !strings.Contains(urlStr, "response_type=code") {
		t.Error("claude should use response_type=code")
	}
}

func TestBuildAuthorizationURL_GeminiCli_NoPKCE(t *testing.T) {
	def := GetProviderDefinition(string(ProviderGeminiCli))
	urlStr, err := def.BuildAuthorizationURL(nil, BuildAuthURLInput{
		State:        "test-state",
		RedirectURI:  "http://localhost:8085/oauth2callback",
		CodeVerifier: "test-verifier",
		ProjectID:    "my-project",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(urlStr, "code_challenge") {
		t.Error("gemini-cli should NOT use PKCE challenge")
	}
	if strings.Contains(urlStr, "code_challenge_method") {
		t.Error("gemini-cli should NOT include code_challenge_method")
	}
	if !strings.Contains(urlStr, "access_type=offline") {
		t.Error("gemini-cli should use access_type=offline")
	}
	if !strings.Contains(urlStr, "prompt=consent") {
		t.Error("gemini-cli should use prompt=consent")
	}
	if !strings.Contains(urlStr, "response_type=code") {
		t.Error("gemini-cli should use response_type=code")
	}
	// Scope values are URL-encoded by url.Values.Encode().
	if !strings.Contains(urlStr, "cloud-platform") {
		t.Error("gemini-cli should have cloud-platform scope (url-encoded)")
	}
}

func TestBuildAuthorizationURL_Antigravity_NoPKCE(t *testing.T) {
	def := GetProviderDefinition(string(ProviderAntigravity))
	urlStr, err := def.BuildAuthorizationURL(nil, BuildAuthURLInput{
		State:        "test-state",
		RedirectURI:  "http://localhost:51121/oauth-callback",
		CodeVerifier: "test-verifier",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(urlStr, "code_challenge") {
		t.Error("antigravity should NOT use PKCE challenge")
	}
	if !strings.Contains(urlStr, "access_type=offline") {
		t.Error("antigravity should use access_type=offline")
	}
	if !strings.Contains(urlStr, "prompt=consent") {
		t.Error("antigravity should use prompt=consent")
	}
	// Scope values are URL-encoded by url.Values.Encode().
	if !strings.Contains(urlStr, "cclog") {
		t.Error("antigravity should have cclog scope (url-encoded)")
	}
	if !strings.Contains(urlStr, "experimentsandconfigs") {
		t.Error("antigravity should have experimentsandconfigs scope (url-encoded)")
	}
}

func TestBuildAuthorizationURL_Codex_StateInURL(t *testing.T) {
	def := GetProviderDefinition(string(ProviderCodex))
	urlStr, err := def.BuildAuthorizationURL(nil, BuildAuthURLInput{
		State:        "my-custom-state-abc",
		RedirectURI:  "http://localhost:1455/auth/callback",
		CodeVerifier: "verifier",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(urlStr, "state=my-custom-state-abc") {
		t.Error("codex URL should contain the state parameter")
	}
}

// ---- Provider Site Config Tests ----

func TestProviderSiteConfig_Codex(t *testing.T) {
	def := GetProviderDefinition(string(ProviderCodex))
	if def.Site.Name != "ChatGPT Codex OAuth" {
		t.Errorf("expected site name, got %q", def.Site.Name)
	}
	if def.Site.URL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("expected site URL, got %q", def.Site.URL)
	}
}

func TestProviderSiteConfig_Claude(t *testing.T) {
	def := GetProviderDefinition(string(ProviderClaude))
	if def.Site.Name != "Anthropic Claude OAuth" {
		t.Errorf("expected site name, got %q", def.Site.Name)
	}
	if def.Site.URL != "https://api.anthropic.com" {
		t.Errorf("expected site URL, got %q", def.Site.URL)
	}
}

// ---- Interface Compliance Tests ----

func TestInterfaceCompliance_AllProvidersHaveRequiredFunctions(t *testing.T) {
	providers := []OAuthProviderId{ProviderCodex, ProviderClaude, ProviderGeminiCli, ProviderAntigravity}
	for _, pid := range providers {
		def := GetProviderDefinition(string(pid))
		if def.BuildAuthorizationURL == nil {
			t.Errorf("%s missing BuildAuthorizationURL", pid)
		}
		if def.ExchangeAuthorizationCode == nil {
			t.Errorf("%s missing ExchangeAuthorizationCode", pid)
		}
		if def.RefreshAccessToken == nil {
			t.Errorf("%s missing RefreshAccessToken", pid)
		}
	}
}

func TestInterfaceCompliance_BuildProxyHeaders_Optional(t *testing.T) {
	for _, pid := range []OAuthProviderId{ProviderCodex, ProviderClaude, ProviderGeminiCli, ProviderAntigravity} {
		def := GetProviderDefinition(string(pid))
		if def.BuildProxyHeaders == nil {
			t.Errorf("%s should implement BuildProxyHeaders", pid)
		}
	}
}

// ---- TokenSet and Type Tests ----

func TestTokenSet_ZeroValue(t *testing.T) {
	ts := TokenSet{}
	if ts.AccessToken != "" {
		t.Error("zero AccessToken should be empty")
	}
	if ts.RefreshToken != "" {
		t.Error("zero RefreshToken should be empty")
	}
	if ts.TokenExpiresAt != 0 {
		t.Error("zero TokenExpiresAt should be 0")
	}
}

func TestSessionStatus_Constants(t *testing.T) {
	if SessionPending != "pending" {
		t.Errorf("SessionPending = %q, want 'pending'", SessionPending)
	}
	if SessionSuccess != "success" {
		t.Errorf("SessionSuccess = %q, want 'success'", SessionSuccess)
	}
	if SessionError != "error" {
		t.Errorf("SessionError = %q, want 'error'", SessionError)
	}
}

func TestProviderID_Constants(t *testing.T) {
	if ProviderCodex != "codex" {
		t.Errorf("ProviderCodex = %q, want 'codex'", ProviderCodex)
	}
	if ProviderClaude != "claude" {
		t.Errorf("ProviderClaude = %q, want 'claude'", ProviderClaude)
	}
	if ProviderGeminiCli != "gemini-cli" {
		t.Errorf("ProviderGeminiCli = %q, want 'gemini-cli'", ProviderGeminiCli)
	}
	if ProviderAntigravity != "antigravity" {
		t.Errorf("ProviderAntigravity = %q, want 'antigravity'", ProviderAntigravity)
	}
}
