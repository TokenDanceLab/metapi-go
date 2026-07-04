package oauth

import (
	"testing"
)

// ---- extractGeminiProjectID Tests ----

func TestExtractGeminiProjectID_String(t *testing.T) {
	id := extractGeminiProjectID("my-project")
	if id != "my-project" {
		t.Errorf("expected 'my-project', got %q", id)
	}
}

func TestExtractGeminiProjectID_MapWithID(t *testing.T) {
	id := extractGeminiProjectID(map[string]interface{}{
		"id": "gcp-project-123",
	})
	if id != "gcp-project-123" {
		t.Errorf("expected 'gcp-project-123', got %q", id)
	}
}

func TestExtractGeminiProjectID_MapWithoutID(t *testing.T) {
	id := extractGeminiProjectID(map[string]interface{}{
		"name": "some-project",
	})
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestExtractGeminiProjectID_Nil(t *testing.T) {
	id := extractGeminiProjectID(nil)
	if id != "" {
		t.Errorf("expected empty for nil, got %q", id)
	}
}

func TestExtractGeminiProjectID_Integer(t *testing.T) {
	id := extractGeminiProjectID(42)
	if id != "" {
		t.Errorf("expected empty for integer, got %q", id)
	}
}

func TestExtractGeminiProjectID_WhitespaceString(t *testing.T) {
	id := extractGeminiProjectID("   ")
	if id != "" {
		t.Errorf("expected empty for whitespace, got %q", id)
	}
}

// ---- extractAntigravityProjectID Tests ----

// Verifies it delegates to extractGeminiProjectID.
func TestExtractAntigravityProjectID_String(t *testing.T) {
	id := extractAntigravityProjectID("antigravity-project")
	if id != "antigravity-project" {
		t.Errorf("expected 'antigravity-project', got %q", id)
	}
}

func TestExtractAntigravityProjectID_MapWithID(t *testing.T) {
	id := extractAntigravityProjectID(map[string]interface{}{
		"id": "ag-project",
	})
	if id != "ag-project" {
		t.Errorf("expected 'ag-project', got %q", id)
	}
}

func TestExtractAntigravityProjectID_Nil(t *testing.T) {
	id := extractAntigravityProjectID(nil)
	if id != "" {
		t.Errorf("expected empty for nil, got %q", id)
	}
}

// ---- extractGeminiDefaultTierID Tests ----

func TestExtractGeminiDefaultTierID_WithDefaultTier(t *testing.T) {
	tiers := []interface{}{
		map[string]interface{}{"id": "tier-1", "isDefault": false},
		map[string]interface{}{"id": "tier-2", "isDefault": true},
		map[string]interface{}{"id": "tier-3", "isDefault": false},
	}
	payload := &geminiLoadCodeAssistPayload{
		AllowedTiers: tiers,
	}
	result := extractGeminiDefaultTierID(payload)
	if result != "tier-2" {
		t.Errorf("expected 'tier-2', got %q", result)
	}
}

func TestExtractGeminiDefaultTierID_NoDefault_FallbackLegacy(t *testing.T) {
	tiers := []interface{}{
		map[string]interface{}{"id": "tier-1", "isDefault": false},
	}
	payload := &geminiLoadCodeAssistPayload{
		AllowedTiers: tiers,
	}
	result := extractGeminiDefaultTierID(payload)
	if result != "legacy-tier" {
		t.Errorf("expected 'legacy-tier', got %q", result)
	}
}

func TestExtractGeminiDefaultTierID_EmptyTiers(t *testing.T) {
	payload := &geminiLoadCodeAssistPayload{
		AllowedTiers: []interface{}{},
	}
	result := extractGeminiDefaultTierID(payload)
	if result != "legacy-tier" {
		t.Errorf("expected 'legacy-tier', got %q", result)
	}
}

func TestExtractGeminiDefaultTierID_NilTiers(t *testing.T) {
	payload := &geminiLoadCodeAssistPayload{}
	result := extractGeminiDefaultTierID(payload)
	if result != "legacy-tier" {
		t.Errorf("expected 'legacy-tier', got %q", result)
	}
}

func TestExtractGeminiDefaultTierID_FirstDefaultWins(t *testing.T) {
	tiers := []interface{}{
		map[string]interface{}{"id": "first-default", "isDefault": true},
		map[string]interface{}{"id": "second-default", "isDefault": true},
	}
	payload := &geminiLoadCodeAssistPayload{
		AllowedTiers: tiers,
	}
	result := extractGeminiDefaultTierID(payload)
	if result != "first-default" {
		t.Errorf("expected 'first-default', got %q", result)
	}
}

func TestExtractGeminiDefaultTierID_TierWithoutID(t *testing.T) {
	tiers := []interface{}{
		map[string]interface{}{"isDefault": true},
	}
	payload := &geminiLoadCodeAssistPayload{
		AllowedTiers: tiers,
	}
	result := extractGeminiDefaultTierID(payload)
	if result != "legacy-tier" {
		t.Errorf("expected 'legacy-tier' when default tier has no id, got %q", result)
	}
}

// ---- extractAntigravityDefaultTierID Tests ----

func TestExtractAntigravityDefaultTierID_WithDefault(t *testing.T) {
	tiers := []interface{}{
		map[string]interface{}{"id": "ag-free", "isDefault": true},
	}
	payload := &antigravityLoadCodeAssistPayload{
		AllowedTiers: tiers,
	}
	result := extractAntigravityDefaultTierID(payload)
	if result != "ag-free" {
		t.Errorf("expected 'ag-free', got %q", result)
	}
}

func TestExtractAntigravityDefaultTierID_NoDefault(t *testing.T) {
	payload := &antigravityLoadCodeAssistPayload{
		AllowedTiers: []interface{}{},
	}
	result := extractAntigravityDefaultTierID(payload)
	if result != "legacy-tier" {
		t.Errorf("expected 'legacy-tier', got %q", result)
	}
}

// ---- isGeminiFreeUserProject Tests ----

func TestIsGeminiFreeUserProject_FreePrefix(t *testing.T) {
	if !isGeminiFreeUserProject("gen-lang-client-abc123", "paid-tier") {
		t.Error("gen-lang-client- prefixed project should be free")
	}
}

func TestIsGeminiFreeUserProject_FreeTier(t *testing.T) {
	if !isGeminiFreeUserProject("my-project", "FREE") {
		t.Error("FREE tier should be free user")
	}
}

func TestIsGeminiFreeUserProject_LegacyTier(t *testing.T) {
	if !isGeminiFreeUserProject("my-project", "LEGACY") {
		t.Error("LEGACY tier should be free user")
	}
}

func TestIsGeminiFreeUserProject_CaseInsensitive(t *testing.T) {
	if !isGeminiFreeUserProject("my-project", "free") {
		t.Error("'free' (lowercase) should be free user")
	}
	if !isGeminiFreeUserProject("gen-lang-client-abc", "paid") {
		t.Error("gen-lang-client prefix (case-sensitive) should be free user")
	}
}

func TestIsGeminiFreeUserProject_Paid(t *testing.T) {
	if isGeminiFreeUserProject("my-paid-project", "PAID") {
		t.Error("PAID project + paid tier should not be free user")
	}
}

// ---- isSameGeminiProjectID Tests ----

func TestIsSameGeminiProjectID_Same(t *testing.T) {
	if !isSameGeminiProjectID("my-project", "my-project") {
		t.Error("same project IDs should match")
	}
}

func TestIsSameGeminiProjectID_CaseInsensitive(t *testing.T) {
	if !isSameGeminiProjectID("My-Project", "my-project") {
		t.Error("project ID comparison should be case-insensitive")
	}
}

func TestIsSameGeminiProjectID_Different(t *testing.T) {
	if isSameGeminiProjectID("project-a", "project-b") {
		t.Error("different project IDs should not match")
	}
}

func TestIsSameGeminiProjectID_Whitespace(t *testing.T) {
	if !isSameGeminiProjectID("  my-project  ", "my-project") {
		t.Error("whitespace should be trimmed")
	}
}

// ---- parseInt64Safe Tests ----

func TestParseInt64Safe_ValidNumber(t *testing.T) {
	n, err := parseInt64Safe("12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 12345 {
		t.Errorf("expected 12345, got %d", n)
	}
}

func TestParseInt64Safe_Zero(t *testing.T) {
	n, err := parseInt64Safe("0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestParseInt64Safe_NegativeSign(t *testing.T) {
	_, err := parseInt64Safe("-123")
	if err == nil {
		t.Error("expected error for negative number")
	}
}

func TestParseInt64Safe_NonDigit(t *testing.T) {
	_, err := parseInt64Safe("123abc")
	if err == nil {
		t.Error("expected error for non-digit characters")
	}
}

func TestParseInt64Safe_Empty(t *testing.T) {
	n, err := parseInt64Safe("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 for empty, got %d", n)
	}
}

// ---- parseInt64 (claude) Tests ----

func TestParseInt64_Valid(t *testing.T) {
	n, err := parseInt64("42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

func TestParseInt64_Invalid(t *testing.T) {
	_, err := parseInt64("abc")
	if err == nil {
		t.Error("expected error for non-numeric")
	}
}

// ---- buildGeminiCliMetadata Tests ----

func TestBuildGeminiCliMetadata(t *testing.T) {
	meta := buildGeminiCliMetadata()
	if meta.IDEType != "IDE_UNSPECIFIED" {
		t.Errorf("expected IDEType 'IDE_UNSPECIFIED', got %q", meta.IDEType)
	}
	if meta.Platform != "PLATFORM_UNSPECIFIED" {
		t.Errorf("expected Platform 'PLATFORM_UNSPECIFIED', got %q", meta.Platform)
	}
	if meta.PluginType != "GEMINI" {
		t.Errorf("expected PluginType 'GEMINI', got %q", meta.PluginType)
	}
}

// ---- buildAntigravityMetadata Tests ----

func TestBuildAntigravityMetadata(t *testing.T) {
	meta := buildAntigravityMetadata()
	if meta.IDEType != "ANTIGRAVITY" {
		t.Errorf("expected IDEType 'ANTIGRAVITY', got %q", meta.IDEType)
	}
	if meta.Platform != "PLATFORM_UNSPECIFIED" {
		t.Errorf("expected Platform 'PLATFORM_UNSPECIFIED', got %q", meta.Platform)
	}
	if meta.PluginType != "GEMINI" {
		t.Errorf("expected PluginType 'GEMINI', got %q", meta.PluginType)
	}
}

// ---- Gemini CLI Constant Tests ----

func TestGeminiCliConstants(t *testing.T) {
	if geminiCliAutoOnboardPollIntervalMs != 2000 {
		t.Errorf("expected auto-onboard poll 2000ms, got %d", geminiCliAutoOnboardPollIntervalMs)
	}
	if geminiCliAutoOnboardMaxAttempts != 15 {
		t.Errorf("expected auto-onboard max attempts 15, got %d", geminiCliAutoOnboardMaxAttempts)
	}
	if geminiCliOnboardPollIntervalMs != 5000 {
		t.Errorf("expected onboard poll 5000ms, got %d", geminiCliOnboardPollIntervalMs)
	}
	if geminiCliOnboardMaxAttempts != 6 {
		t.Errorf("expected onboard max attempts 6, got %d", geminiCliOnboardMaxAttempts)
	}
	if geminiCliRequiredService != "cloudaicompanion.googleapis.com" {
		t.Errorf("expected required service, got %q", geminiCliRequiredService)
	}
}

// ---- Antigravity Constant Tests ----

func TestAntigravityConstants(t *testing.T) {
	if antigravityOnboardPollIntervalMs != 2000 {
		t.Errorf("expected onboard poll 2000ms, got %d", antigravityOnboardPollIntervalMs)
	}
	if antigravityOnboardMaxAttempts != 5 {
		t.Errorf("expected onboard max attempts 5, got %d", antigravityOnboardMaxAttempts)
	}
	if antigravityClientID != "ANTIGRAVITY_CLIENT_ID_PLACEHOLDER" {
		t.Errorf("expected placeholder client ID, got %q", antigravityClientID)
	}
	if antigravityClientSecret != "ANTIGRAVITY_CLIENT_SECRET_PLACEHOLDER" {
		t.Errorf("expected placeholder client secret, got %q", antigravityClientSecret)
	}
}

// ---- Scope Tests ----

func TestGeminiCliScopes(t *testing.T) {
	if len(geminiCliScopes) != 3 {
		t.Errorf("expected 3 scopes for gemini-cli, got %d", len(geminiCliScopes))
	}

	hasCloudPlatform := false
	hasUserinfoEmail := false
	hasUserinfoProfile := false
	for _, s := range geminiCliScopes {
		if s == "https://www.googleapis.com/auth/cloud-platform" {
			hasCloudPlatform = true
		}
		if s == "https://www.googleapis.com/auth/userinfo.email" {
			hasUserinfoEmail = true
		}
		if s == "https://www.googleapis.com/auth/userinfo.profile" {
			hasUserinfoProfile = true
		}
	}
	if !hasCloudPlatform {
		t.Error("gemini-cli should have cloud-platform scope")
	}
	if !hasUserinfoEmail {
		t.Error("gemini-cli should have userinfo.email scope")
	}
	if !hasUserinfoProfile {
		t.Error("gemini-cli should have userinfo.profile scope")
	}
}

func TestAntigravityScopes(t *testing.T) {
	if len(antigravityScopes) != 5 {
		t.Errorf("expected 5 scopes for antigravity, got %d", len(antigravityScopes))
	}

	hasCCLog := false
	hasExpsAndConfigs := false
	for _, s := range antigravityScopes {
		if s == "https://www.googleapis.com/auth/cclog" {
			hasCCLog = true
		}
		if s == "https://www.googleapis.com/auth/experimentsandconfigs" {
			hasExpsAndConfigs = true
		}
	}
	if !hasCCLog {
		t.Error("antigravity should have cclog scope")
	}
	if !hasExpsAndConfigs {
		t.Error("antigravity should have experimentsandconfigs scope")
	}
}

// ---- parseExpiresIn Tests ----

func TestParseExpiresIn_Float64(t *testing.T) {
	v, ok := parseExpiresIn(float64(3600))
	if !ok {
		t.Error("expected ok for float64")
	}
	if v != 3600 {
		t.Errorf("expected 3600, got %d", v)
	}
}

func TestParseExpiresIn_String(t *testing.T) {
	v, ok := parseExpiresIn("7200")
	if !ok {
		t.Error("expected ok for string")
	}
	if v != 7200 {
		t.Errorf("expected 7200, got %d", v)
	}
}

func TestParseExpiresIn_ZeroFloat(t *testing.T) {
	_, ok := parseExpiresIn(float64(0))
	if ok {
		t.Error("expected not ok for zero")
	}
}

func TestParseExpiresIn_Nil(t *testing.T) {
	_, ok := parseExpiresIn(nil)
	if ok {
		t.Error("expected not ok for nil")
	}
}

func TestParseExpiresIn_InvalidString(t *testing.T) {
	_, ok := parseExpiresIn("not-a-number")
	if ok {
		t.Error("expected not ok for invalid string")
	}
}

// ---- parseGeminiExpiresAt Tests ----

func TestParseGeminiExpiresAt_Float64(t *testing.T) {
	payload := &geminiOAuthTokenPayload{
		ExpiresIn: float64(3600),
	}
	result := parseGeminiExpiresAt(payload)
	if result <= 0 {
		t.Error("expected positive timestamp")
	}
}

func TestParseGeminiExpiresAt_String(t *testing.T) {
	payload := &geminiOAuthTokenPayload{
		ExpiresIn: "1800",
	}
	result := parseGeminiExpiresAt(payload)
	if result <= 0 {
		t.Error("expected positive timestamp from string")
	}
}

func TestParseGeminiExpiresAt_ExpiryField(t *testing.T) {
	payload := &geminiOAuthTokenPayload{
		Expiry: "2026-08-01T12:00:00Z",
	}
	result := parseGeminiExpiresAt(payload)
	if result <= 0 {
		t.Error("expected positive timestamp from expiry field")
	}
}

func TestParseGeminiExpiresAt_NoExpiryInfo(t *testing.T) {
	payload := &geminiOAuthTokenPayload{}
	result := parseGeminiExpiresAt(payload)
	if result != 0 {
		t.Errorf("expected 0 with no expiry info, got %d", result)
	}
}

// ---- parseClaudeExpiresAt Tests ----

func TestParseClaudeExpiresAt_Float64(t *testing.T) {
	result := parseClaudeExpiresAt(float64(3600))
	if result <= 0 {
		t.Error("expected positive timestamp")
	}
}

func TestParseClaudeExpiresAt_String(t *testing.T) {
	result := parseClaudeExpiresAt("7200")
	if result <= 0 {
		t.Error("expected positive timestamp from string")
	}
}

func TestParseClaudeExpiresAt_Zero(t *testing.T) {
	result := parseClaudeExpiresAt(float64(0))
	if result != 0 {
		t.Errorf("expected 0 for zero input, got %d", result)
	}
}

// ---- parseAntigravityExpiresAt Tests ----

func TestParseAntigravityExpiresAt_Float64(t *testing.T) {
	payload := &antigravityOAuthTokenPayload{
		ExpiresIn: float64(3600),
	}
	result := parseAntigravityExpiresAt(payload)
	if result <= 0 {
		t.Error("expected positive timestamp")
	}
}

func TestParseAntigravityExpiresAt_ExpiryField(t *testing.T) {
	payload := &antigravityOAuthTokenPayload{
		Expiry: "2026-08-01T12:00:00Z",
	}
	result := parseAntigravityExpiresAt(payload)
	if result <= 0 {
		t.Error("expected positive timestamp from expiry field")
	}
}
