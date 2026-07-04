package oauth

import (
	"strings"
	"testing"
)

// ---- mapImportedOauthProvider Tests ----

func TestMapImportedOauthProvider_DirectNames(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"codex", "codex"},
		{"claude", "claude"},
		{"gemini-cli", "gemini-cli"},
		{"antigravity", "antigravity"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := mapImportedOauthProvider(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestMapImportedOauthProvider_Aliases(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"openai", "codex"},
		{"OpenAI", "codex"},
		{"OPENAI", "codex"},
		{"anthropic", "claude"},
		{"Anthropic", "claude"},
		{"gemini", "gemini-cli"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := mapImportedOauthProvider(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q for %q, got %q", tc.expected, tc.input, result)
			}
		})
	}
}

func TestMapImportedOauthProvider_Unknown(t *testing.T) {
	result := mapImportedOauthProvider("unknown")
	if result != "" {
		t.Errorf("expected empty for unknown provider, got %q", result)
	}
}

func TestMapImportedOauthProvider_Empty(t *testing.T) {
	result := mapImportedOauthProvider("")
	if result != "" {
		t.Errorf("expected empty for empty input, got %q", result)
	}
}

// ---- parseImportedOauthExpiry Tests ----

func TestParseImportedOauthExpiry_Nil(t *testing.T) {
	result := parseImportedOauthExpiry(nil)
	if result != 0 {
		t.Errorf("expected 0 for nil, got %d", result)
	}
}

func TestParseImportedOauthExpiry_NumericFloat(t *testing.T) {
	result := parseImportedOauthExpiry(float64(1700000000000))
	if result != 1700000000000 {
		t.Errorf("expected 1700000000000, got %d", result)
	}
}

func TestParseImportedOauthExpiry_NumericString(t *testing.T) {
	result := parseImportedOauthExpiry("1700000000000")
	if result != 1700000000000 {
		t.Errorf("expected 1700000000000, got %d", result)
	}
}

func TestParseImportedOauthExpiry_ISO8601_RFC3339(t *testing.T) {
	result := parseImportedOauthExpiry("2026-07-04T12:00:00Z")
	if result <= 0 {
		t.Error("expected positive timestamp for ISO 8601 date")
	}
}

func TestParseImportedOauthExpiry_ISO8601_WithMillis(t *testing.T) {
	result := parseImportedOauthExpiry("2026-07-04T12:00:00.000Z")
	if result <= 0 {
		t.Error("expected positive timestamp for ISO 8601 with millis")
	}
}

func TestParseImportedOauthExpiry_EmptyString(t *testing.T) {
	result := parseImportedOauthExpiry("")
	if result != 0 {
		t.Errorf("expected 0 for empty, got %d", result)
	}
}

func TestParseImportedOauthExpiry_Whitespace(t *testing.T) {
	result := parseImportedOauthExpiry("   ")
	if result != 0 {
		t.Errorf("expected 0 for whitespace, got %d", result)
	}
}

func TestParseImportedOauthExpiry_InvalidString(t *testing.T) {
	result := parseImportedOauthExpiry("not-a-number")
	if result != 0 {
		t.Errorf("expected 0 for invalid string, got %d", result)
	}
}

func TestParseImportedOauthExpiry_ZeroFloat(t *testing.T) {
	result := parseImportedOauthExpiry(float64(0))
	if result != 0 {
		t.Errorf("expected 0 for zero float, got %d", result)
	}
}

func TestParseImportedOauthExpiry_NegativeFloat(t *testing.T) {
	result := parseImportedOauthExpiry(float64(-1))
	if result != 0 {
		t.Errorf("expected 0 for negative float, got %d", result)
	}
}

func TestParseImportedOauthExpiry_RFC1123(t *testing.T) {
	result := parseImportedOauthExpiry("Sat, 04 Jul 2026 12:00:00 GMT")
	if result <= 0 {
		t.Error("expected positive timestamp for RFC1123 date")
	}
}

// ---- decodeJWTClaims Tests ----

func TestDecodeJWTClaims_ValidToken(t *testing.T) {
	// Base64url-encoded header + payload + signature.
	// Payload: {"sub":"123","email":"test@example.com"}
	// Using a minimal JWT-like structure.
	claims := decodeJWTClaims("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMiLCJlbWFpbCI6InRlc3RAZXhhbXBsZS5jb20ifQ.signature")
	if claims == nil {
		t.Error("expected non-nil claims from valid JWT")
	}
}

func TestDecodeJWTClaims_EmptyToken(t *testing.T) {
	claims := decodeJWTClaims("")
	if claims != nil {
		t.Error("expected nil for empty token")
	}
}

func TestDecodeJWTClaims_InvalidFormat(t *testing.T) {
	claims := decodeJWTClaims("not-a-jwt")
	if claims != nil {
		t.Error("expected nil for invalid format")
	}
}

func TestDecodeJWTClaims_TooFewParts(t *testing.T) {
	claims := decodeJWTClaims("one.two")
	if claims != nil {
		t.Error("expected nil for two-part token")
	}
}

func TestDecodeJWTClaims_TooManyParts(t *testing.T) {
	claims := decodeJWTClaims("one.two.three.four")
	if claims != nil {
		t.Error("expected nil for four-part token")
	}
}

// ---- resolveImportedNativeOauthIdentity Tests ----

func TestResolveImportedNativeOauthIdentity_ValidCodex(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "codex",
		"access_token":  "at-123",
		"refresh_token": "rt-456",
		"email":         "user@example.com",
		"account_key":   "acc-key-1",
		"expired":       float64(1700000000000),
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.provider != "codex" {
		t.Errorf("expected provider 'codex', got %q", identity.provider)
	}
	if identity.exchange.AccessToken != "at-123" {
		t.Errorf("expected accessToken 'at-123', got %q", identity.exchange.AccessToken)
	}
	if identity.exchange.RefreshToken != "rt-456" {
		t.Errorf("expected refreshToken 'rt-456', got %q", identity.exchange.RefreshToken)
	}
	if identity.exchange.TokenExpiresAt != 1700000000000 {
		t.Errorf("expected TokenExpiresAt, got %d", identity.exchange.TokenExpiresAt)
	}
	if identity.disabled {
		t.Error("expected disabled=false")
	}
}

func TestResolveImportedNativeOauthIdentity_ValidClaude(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "claude",
		"access_token":  "claude-at",
		"refresh_token": "claude-rt",
		"email":         "claude@example.com",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.provider != "claude" {
		t.Errorf("expected provider 'claude', got %q", identity.provider)
	}
}

func TestResolveImportedNativeOauthIdentity_OpenAIAlias(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "openai",
		"access_token":  "at-123",
		"refresh_token": "rt-456",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.provider != "codex" {
		t.Errorf("expected provider 'codex' for 'openai', got %q", identity.provider)
	}
}

func TestResolveImportedNativeOauthIdentity_AnthropicAlias(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "anthropic",
		"access_token":  "at-123",
		"refresh_token": "rt-456",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.provider != "claude" {
		t.Errorf("expected provider 'claude' for 'anthropic', got %q", identity.provider)
	}
}

func TestResolveImportedNativeOauthIdentity_GeminiAlias(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "gemini",
		"access_token":  "at-123",
		"refresh_token": "rt-456",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.provider != "gemini-cli" {
		t.Errorf("expected provider 'gemini-cli' for 'gemini', got %q", identity.provider)
	}
}

func TestResolveImportedNativeOauthIdentity_Disabled(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "codex",
		"access_token":  "at-123",
		"refresh_token": "rt-456",
		"disabled":      true,
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !identity.disabled {
		t.Error("expected disabled=true")
	}
}

func TestResolveImportedNativeOauthIdentity_SessionTokenFallback(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "codex",
		"session_token": "st-123",
		"refresh_token": "rt-456",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.exchange.AccessToken != "st-123" {
		t.Errorf("expected session_token as accessToken, got %q", identity.exchange.AccessToken)
	}
}

func TestResolveImportedNativeOauthIdentity_AccessTokenPreferred(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "codex",
		"access_token":  "at-123",
		"session_token": "st-456",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.exchange.AccessToken != "at-123" {
		t.Errorf("expected access_token to take precedence, got %q", identity.exchange.AccessToken)
	}
}

func TestResolveImportedNativeOauthIdentity_MissingAccessToken(t *testing.T) {
	payload := map[string]interface{}{
		"type":          "codex",
		"refresh_token": "rt-456",
	}

	_, err := resolveImportedNativeOauthIdentity(payload)
	if err == nil {
		t.Fatal("expected error for missing access token")
	}
	if !strings.Contains(err.Error(), "access_token") {
		t.Errorf("expected access_token error, got %q", err.Error())
	}
}

func TestResolveImportedNativeOauthIdentity_Sub2APIEnvelope_Type(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type": "sub2api-data",
	})
	if err == nil {
		t.Fatal("expected error for sub2api-data envelope")
	}
	if !strings.Contains(err.Error(), "sub2api") {
		t.Errorf("expected sub2api rejection, got %q", err.Error())
	}
}

func TestResolveImportedNativeOauthIdentity_Sub2APIEnvelope_Bundle(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type": "sub2api-bundle",
	})
	if err == nil {
		t.Fatal("expected error for sub2api-bundle envelope")
	}
}

func TestResolveImportedNativeOauthIdentity_Sub2APIEnvelope_Accounts(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type":     "codex",
		"accounts": []interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for accounts field")
	}
}

func TestResolveImportedNativeOauthIdentity_Sub2APIEnvelope_Proxies(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type":    "codex",
		"proxies": []interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for proxies field")
	}
}

func TestResolveImportedNativeOauthIdentity_Sub2APIEnvelope_Version(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type":    "codex",
		"version": "1.0",
	})
	if err == nil {
		t.Fatal("expected error for version field")
	}
}

func TestResolveImportedNativeOauthIdentity_Sub2APIEnvelope_ExportedAt(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type":        "codex",
		"exported_at": "2026-07-04",
	})
	if err == nil {
		t.Fatal("expected error for exported_at field")
	}
}

func TestResolveImportedNativeOauthIdentity_UnsupportedType(t *testing.T) {
	_, err := resolveImportedNativeOauthIdentity(map[string]interface{}{
		"type": "unsupported",
	})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestResolveImportedNativeOauthIdentity_ISO8601Expiry(t *testing.T) {
	payload := map[string]interface{}{
		"type":         "codex",
		"access_token": "at-123",
		"expired":      "2026-08-01T00:00:00Z",
	}

	identity, err := resolveImportedNativeOauthIdentity(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.exchange.TokenExpiresAt <= 0 {
		t.Error("expected positive TokenExpiresAt from ISO 8601 date")
	}
}

func TestResolveImportedNativeOauthIdentity_NoTypeField(t *testing.T) {
	payload := map[string]interface{}{
		"access_token": "at-123",
	}

	_, err := resolveImportedNativeOauthIdentity(payload)
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

// ---- resolveImportedOauthIdentityFields Tests ----

func TestResolveImportedOauthIdentityFields_EmptyPayload(t *testing.T) {
	result := resolveImportedOauthIdentityFields("codex", map[string]interface{}{})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.AccountKey != "" {
		t.Errorf("expected empty AccountKey, got %q", result.AccountKey)
	}
}

func TestResolveImportedOauthIdentityFields_WithAccountKey(t *testing.T) {
	payload := map[string]interface{}{
		"account_key": "my-account-key",
		"email":       "test@example.com",
	}
	result := resolveImportedOauthIdentityFields("codex", payload)
	if result.AccountKey != "my-account-key" {
		t.Errorf("expected AccountKey 'my-account-key', got %q", result.AccountKey)
	}
}

func TestResolveImportedOauthIdentityFields_ChatGPTAccountID(t *testing.T) {
	payload := map[string]interface{}{
		"chatgpt_account_id": "chatgpt-id-123",
	}
	result := resolveImportedOauthIdentityFields("codex", payload)
	if result.AccountKey != "chatgpt-id-123" {
		t.Errorf("expected AccountKey from chatgpt_account_id, got %q", result.AccountKey)
	}
}

// ---- ImportResult Type Tests ----

func TestImportResult_Defaults(t *testing.T) {
	r := ImportResult{}
	if r.Success {
		t.Error("zero ImportResult should not be successful")
	}
	if r.Imported != 0 {
		t.Errorf("Imported should be 0, got %d", r.Imported)
	}
	if r.Failed != 0 {
		t.Errorf("Failed should be 0, got %d", r.Failed)
	}
	if len(r.Items) != 0 {
		t.Errorf("Items should be empty, got %d items", len(r.Items))
	}
}

func TestImportItem_Statuses(t *testing.T) {
	item := ImportItem{Status: "imported"}
	if item.Status != "imported" {
		t.Errorf("expected 'imported', got %q", item.Status)
	}
	item.Status = "failed"
	if item.Status != "failed" {
		t.Errorf("expected 'failed', got %q", item.Status)
	}
}

// ---- Batch Size Constant Tests ----

func TestMaxOAuthImportBatchSize(t *testing.T) {
	if maxOAuthImportBatchSize != 100 {
		t.Errorf("expected maxOAuthImportBatchSize 100, got %d", maxOAuthImportBatchSize)
	}
}
