package oauth

import (
	"strings"
	"testing"
)

// ---- StartFlow Tests ----

func TestStartFlow_ValidProvider(t *testing.T) {
	// Reset callback state so StartFlow doesn't find a failed server.
	callbackMu.Lock()
	delete(callbackStates, "codex")
	callbackMu.Unlock()

	result, err := StartFlow(StartFlowInput{
		Provider:    "codex",
		RequestOrigin: "http://localhost:8080",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Provider != "codex" {
		t.Errorf("expected provider 'codex', got %q", result.Provider)
	}
	if result.State == "" {
		t.Error("state should be non-empty")
	}
	if result.AuthorizationURL == "" {
		t.Error("authorizationURL should be non-empty")
	}
	if result.Instructions == nil {
		t.Fatal("instructions should be non-nil")
	}
	if result.Instructions.ManualCallbackDelayMs != 15000 {
		t.Errorf("expected ManualCallbackDelayMs 15000, got %d", result.Instructions.ManualCallbackDelayMs)
	}
	if result.Instructions.RedirectURI == "" {
		t.Error("instructions should include redirectURI")
	}
	if result.Instructions.CallbackPort <= 0 {
		t.Errorf("instructions should have valid callbackPort, got %d", result.Instructions.CallbackPort)
	}
}

func TestStartFlow_InvalidProvider(t *testing.T) {
	_, err := StartFlow(StartFlowInput{Provider: "invalid-provider"})
	if err == nil {
		t.Fatal("expected error for invalid provider")
	}
	if !strings.Contains(err.Error(), "unsupported oauth provider") {
		t.Errorf("expected unsupported provider error, got %q", err.Error())
	}
}

func TestStartFlow_CheckCallbackServerState_Unavailable(t *testing.T) {
	// Simulate a failed callback server state.
	callbackMu.Lock()
	callbackStates["codex"] = &LoopbackCallbackServerState{
		Provider:  "codex",
		Attempted: true,
		Ready:     false,
		Error:     "port already in use",
		Port:      1455,
	}
	callbackMu.Unlock()

	_, err := StartFlow(StartFlowInput{Provider: "codex"})
	if err == nil {
		t.Fatal("expected error when callback server is unavailable")
	}
	if !strings.Contains(err.Error(), "callback listener is unavailable") {
		t.Errorf("expected callback listener unavailable error, got %q", err.Error())
	}

	// Clean up.
	callbackMu.Lock()
	delete(callbackStates, "codex")
	callbackMu.Unlock()
}

func TestStartFlow_AllFourProviders(t *testing.T) {
	providers := []string{"codex", "claude", "gemini-cli", "antigravity"}
	for _, provider := range providers {
		// Reset callback state.
		callbackMu.Lock()
		delete(callbackStates, provider)
		callbackMu.Unlock()

		t.Run(provider, func(t *testing.T) {
			result, err := StartFlow(StartFlowInput{
				Provider:    provider,
				RequestOrigin: "http://localhost:8080",
			})
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", provider, err)
			}
			if result.Provider != provider {
				t.Errorf("expected provider %q, got %q", provider, result.Provider)
			}
		})
	}
}

func TestStartFlow_StoresSessionFields(t *testing.T) {
	callbackMu.Lock()
	delete(callbackStates, "codex")
	callbackMu.Unlock()

	result, err := StartFlow(StartFlowInput{
		Provider:    "gemini-cli",
		RebindAccountID: 99,
		ProjectID:       "my-gcp-project",
		ProxyURL:        "http://proxy:3128",
		UseSystemProxy:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	session := GetSession(result.State)
	if session.RebindAccountID != 99 {
		t.Errorf("expected RebindAccountID 99, got %d", session.RebindAccountID)
	}
	if session.ProjectID != "my-gcp-project" {
		t.Errorf("expected ProjectID 'my-gcp-project', got %q", session.ProjectID)
	}
	if session.ProxyURL != "http://proxy:3128" {
		t.Errorf("expected ProxyURL, got %q", session.ProxyURL)
	}
	if !session.UseSystemProxy {
		t.Error("expected UseSystemProxy true")
	}
}

// ---- GetSessionStatus Tests ----

func TestGetSessionStatus_ExistingSession(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)
	callbackMu.Lock()
	delete(callbackStates, "codex")
	callbackMu.Unlock()

	result, err := StartFlow(StartFlowInput{Provider: "codex"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	status := GetSessionStatus(result.State)
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Status != SessionPending {
		t.Errorf("expected pending, got %q", status.Status)
	}
	if status.Provider != "codex" {
		t.Errorf("expected provider 'codex', got %q", status.Provider)
	}
	if status.State != result.State {
		t.Errorf("state mismatch: %q vs %q", status.State, result.State)
	}
}

func TestGetSessionStatus_MissingSession(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	status := GetSessionStatus("nonexistent")
	if status != nil {
		t.Error("expected nil for missing session")
	}
}

// ---- SSH Tunnel Instruction Tests ----

func TestBuildLoopbackInstructions_NoSSHForLoopback(t *testing.T) {
	def := GetProviderDefinition("codex")
	instructions := buildLoopbackInstructions(def, "http://localhost:8080")
	if instructions.SSHTunnelCommand != "" {
		t.Error("should not include SSH command for loopback origin")
	}
	if instructions.SSHTunnelKeyCommand != "" {
		t.Error("should not include SSH key command for loopback origin")
	}
}

func TestBuildLoopbackInstructions_NoSSHFor127(t *testing.T) {
	def := GetProviderDefinition("codex")
	instructions := buildLoopbackInstructions(def, "http://127.0.0.1:8080")
	if instructions.SSHTunnelCommand != "" {
		t.Error("should not include SSH command for 127.0.0.1 origin")
	}
}

func TestBuildLoopbackInstructions_SSHForRemote(t *testing.T) {
	def := GetProviderDefinition("codex")
	instructions := buildLoopbackInstructions(def, "https://myserver.example.com")
	if instructions.SSHTunnelCommand == "" {
		t.Error("should include SSH command for remote origin")
	}
	if !strings.Contains(instructions.SSHTunnelCommand, "ssh -L 1455:127.0.0.1:1455") {
		t.Errorf("SSH command should have correct port forwarding, got %q", instructions.SSHTunnelCommand)
	}
	if instructions.SSHTunnelKeyCommand == "" {
		t.Error("should include SSH key command for remote origin")
	}
	if !strings.Contains(instructions.SSHTunnelKeyCommand, "ssh -i <path_to_your_key>") {
		t.Error("SSH key command should include key path placeholder")
	}
}

func TestResolveSSHTunnelHost_Empty(t *testing.T) {
	host := resolveSSHTunnelHost("")
	if host != "" {
		t.Errorf("expected empty for empty origin, got %q", host)
	}
}

func TestResolveSSHTunnelHost_Localhost(t *testing.T) {
	host := resolveSSHTunnelHost("http://localhost:8080")
	if host != "" {
		t.Errorf("expected empty for localhost, got %q", host)
	}
}

func TestResolveSSHTunnelHost_Remote(t *testing.T) {
	host := resolveSSHTunnelHost("https://my-server.example.com:443")
	if host != "my-server.example.com" {
		t.Errorf("expected 'my-server.example.com', got %q", host)
	}
}

// ---- isLoopbackHost Tests ----

func TestIsLoopbackHost_Localhost(t *testing.T) {
	if !isLoopbackHost("localhost") {
		t.Error("localhost should be loopback")
	}
}

func TestIsLoopbackHost_127001(t *testing.T) {
	if !isLoopbackHost("127.0.0.1") {
		t.Error("127.0.0.1 should be loopback")
	}
}

func TestIsLoopbackHost_IPv6Loopback(t *testing.T) {
	if !isLoopbackHost("::1") {
		t.Error("::1 should be loopback")
	}
}

func TestIsLoopbackHost_Remote(t *testing.T) {
	if isLoopbackHost("example.com") {
		t.Error("example.com should not be loopback")
	}
}

// ---- Manual Callback Parsing Tests ----

func TestParseManualCallbackURL_Valid(t *testing.T) {
	parsed, err := parseManualCallbackURL("http://localhost:1455/auth/callback?state=abc&code=xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.State != "abc" {
		t.Errorf("expected state 'abc', got %q", parsed.State)
	}
	if parsed.Code != "xyz" {
		t.Errorf("expected code 'xyz', got %q", parsed.Code)
	}
	if parsed.Error != "" {
		t.Errorf("expected no error, got %q", parsed.Error)
	}
}

func TestParseManualCallbackURL_ErrorWithDescription(t *testing.T) {
	parsed, err := parseManualCallbackURL("http://localhost:1455/auth/callback?state=abc&error=access_denied&error_description=user%20denied")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.State != "abc" {
		t.Errorf("expected state 'abc', got %q", parsed.State)
	}
	if parsed.Error != "access_denied: user denied" {
		t.Errorf("expected combined error, got %q", parsed.Error)
	}
}

func TestParseManualCallbackURL_ErrorNoDescription(t *testing.T) {
	parsed, err := parseManualCallbackURL("http://localhost:1455/auth/callback?state=abc&error=access_denied")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Error != "access_denied" {
		t.Errorf("expected 'access_denied', got %q", parsed.Error)
	}
}

func TestParseManualCallbackURL_Invalid(t *testing.T) {
	_, err := parseManualCallbackURL("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestParseManualCallbackURL_MissingState(t *testing.T) {
	_, err := parseManualCallbackURL("http://localhost:1455/auth/callback?code=xyz")
	if err == nil {
		t.Fatal("expected error for missing state")
	}
}

func TestParseManualCallbackURL_MissingCodeAndError(t *testing.T) {
	_, err := parseManualCallbackURL("http://localhost:1455/auth/callback?state=abc")
	if err == nil {
		t.Fatal("expected error when both code and error are missing")
	}
}

// ---- HandleCallback Error Path Tests ----

func TestHandleCallback_SessionNotFound(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	_, err := HandleCallback(CallbackInput{
		Provider: "codex",
		State:    "nonexistent",
		Code:     "test-code",
	})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "oauth session not found") {
		t.Errorf("expected 'oauth session not found', got %q", err.Error())
	}
}

func TestHandleCallback_ProviderMismatch(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	session, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	_, err = HandleCallback(CallbackInput{
		Provider: "claude",
		State:    session.State,
		Code:     "test-code",
	})
	if err == nil {
		t.Fatal("expected error for provider mismatch")
	}
	if !strings.Contains(err.Error(), "provider mismatch") {
		t.Errorf("expected 'provider mismatch', got %q", err.Error())
	}
}

func TestHandleCallback_ErrorParam(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	session, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	_, err = HandleCallback(CallbackInput{
		Provider: "codex",
		State:    session.State,
		Error:    "access_denied",
	})
	if err == nil {
		t.Fatal("expected error for OAuth error callback")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("expected 'access_denied', got %q", err.Error())
	}

	// Session should be marked as error.
	got := GetSession(session.State)
	if got.Status != SessionError {
		t.Errorf("expected session status 'error', got %q", got.Status)
	}
}

func TestHandleCallback_MissingCode(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	session, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	_, err = HandleCallback(CallbackInput{
		Provider: "codex",
		State:    session.State,
		Code:     "",
	})
	if err == nil {
		t.Fatal("expected error for missing code")
	}
	if !strings.Contains(err.Error(), "missing oauth code") {
		t.Errorf("expected 'missing oauth code', got %q", err.Error())
	}
}

func TestHandleCallback_MissingCode_Whitespace(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	session, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	_, err = HandleCallback(CallbackInput{
		Provider: "codex",
		State:    session.State,
		Code:     "   ",
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only code")
	}
}

// ---- SubmitManualCallback Tests ----

func TestSubmitManualCallback_SessionNotFound(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	err := SubmitManualCallback(ManualCallbackInput{
		State:       "nonexistent",
		CallbackURL: "http://localhost:1455/auth/callback?state=nonexistent&code=xyz",
	})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "oauth session not found") {
		t.Errorf("expected 'oauth session not found', got %q", err.Error())
	}
}

func TestSubmitManualCallback_StateMismatch(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	session, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	err = SubmitManualCallback(ManualCallbackInput{
		State:       session.State,
		CallbackURL: "http://localhost:1455/auth/callback?state=wrong-state&code=xyz",
	})
	if err == nil {
		t.Fatal("expected error for state mismatch")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("expected 'state mismatch', got %q", err.Error())
	}
}

func TestSubmitManualCallback_InvalidURL(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	session, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	err = SubmitManualCallback(ManualCallbackInput{
		State:       session.State,
		CallbackURL: "",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// ---- FlowInstructions Tests ----

func TestFlowInstructions_CallbackPorts(t *testing.T) {
	tests := []struct {
		provider string
		port     int
	}{
		{"codex", 1455},
		{"claude", 54545},
		{"gemini-cli", 8085},
		{"antigravity", 51121},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			def := GetProviderDefinition(tc.provider)
			instructions := buildLoopbackInstructions(def, "http://localhost:8080")
			if instructions.CallbackPort != tc.port {
				t.Errorf("expected port %d, got %d", tc.port, instructions.CallbackPort)
			}
		})
	}
}

// ---- ListOauthProviders Tests ----

func TestListOauthProviders_ReturnsAllFour(t *testing.T) {
	providers := ListOauthProviders()
	if len(providers) != 4 {
		t.Errorf("expected 4 providers, got %d", len(providers))
	}
}

func TestListOauthProviders_CodexMetadata(t *testing.T) {
	providers := ListOauthProviders()
	for _, p := range providers {
		if p.Provider == ProviderCodex {
			if p.Label != "Codex" {
				t.Errorf("expected Label 'Codex', got %q", p.Label)
			}
			return
		}
	}
	t.Error("codex not found in providers list")
}
