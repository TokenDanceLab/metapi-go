package oauth

import (
	"testing"
	"time"
)

// ---- PKCE Utilities ----

func TestCreatePKCEVerifier_Length(t *testing.T) {
	v, err := CreatePKCEVerifier()
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if len(v) == 0 {
		t.Fatal("expected non-empty verifier")
	}
	// 48 bytes = 64 base64url chars (no padding)
	if len(v) != 64 {
		t.Errorf("expected 64 char base64url verifier (48 bytes), got %d chars", len(v))
	}
}

func TestCreatePKCEVerifier_IsRandom(t *testing.T) {
	v1, err := CreatePKCEVerifier()
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	v2, err := CreatePKCEVerifier()
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if v1 == v2 {
		t.Error("successive calls should produce different verifiers")
	}
}

func TestCreatePKCEChallenge_Deterministic(t *testing.T) {
	const verifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	c1 := CreatePKCEChallenge(verifier)
	c2 := CreatePKCEChallenge(verifier)
	if c1 != c2 {
		t.Errorf("same input should produce same challenge, got %q vs %q", c1, c2)
	}
}

func TestCreatePKCEChallenge_DifferentInputs(t *testing.T) {
	c1 := CreatePKCEChallenge("verifier1")
	c2 := CreatePKCEChallenge("verifier2")
	if c1 == c2 {
		t.Error("different inputs should produce different challenges")
	}
}

func TestCreatePKCEChallenge_NonEmpty(t *testing.T) {
	c := CreatePKCEChallenge("test")
	if c == "" {
		t.Error("challenge should be non-empty")
	}
}

func TestCreatePKCEChallenge_EmptyInput(t *testing.T) {
	c := CreatePKCEChallenge("")
	if c == "" {
		t.Error("challenge of empty input should be non-empty (SHA256 of empty)")
	}
}

// ---- MemoryOAuthSessionStore: Create ----

func TestMemorySessionStore_Create(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/auth/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if rec == nil {
		t.Fatal("expected non-nil session record")
	}
	if rec.Status != SessionPending {
		t.Errorf("expected status pending, got %q", rec.Status)
	}
	if rec.Provider != "codex" {
		t.Errorf("expected provider 'codex', got %q", rec.Provider)
	}
	if rec.State == "" {
		t.Error("state should be non-empty")
	}
	if rec.CodeVerifier == "" {
		t.Error("codeVerifier should be non-empty")
	}
	if rec.RedirectURI != "http://localhost:1455/auth/callback" {
		t.Errorf("expected redirectUri, got %q", rec.RedirectURI)
	}
}

func TestMemorySessionStore_Create_DifferentStates(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	s1 , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	s2 , err := store.Create(CreateSessionInput{Provider: "claude", RedirectURI: "http://localhost:54545/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if s1.State == s2.State {
		t.Error("sessions should have different states")
	}
}

func TestMemorySessionStore_Create_StateAndVerifierLength(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	// 24 bytes = 32 base64url chars
	if len(rec.State) != 32 {
		t.Errorf("state should be 32 base64url chars (24 bytes), got %d", len(rec.State))
	}
	// 48 bytes = 64 base64url chars
	if len(rec.CodeVerifier) != 64 {
		t.Errorf("codeVerifier should be 64 base64url chars (48 bytes), got %d", len(rec.CodeVerifier))
	}
}

func TestMemorySessionStore_Create_SetsExpiry(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	expectedExpiry := rec.CreatedAt.Add(10 * time.Minute)
	diff := rec.ExpiresAt.Sub(expectedExpiry)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expiry should be ~10 minutes from createdAt, got diff %v", diff)
	}
}

func TestMemorySessionStore_Create_StoresAllFields(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	input := CreateSessionInput{
		Provider:        "gemini-cli",
		RedirectURI:     "http://localhost:8085/oauth2callback",
		RebindAccountID: 42,
		ProjectID:       "test-project",
		ProxyURL:        "http://proxy:8080",
		UseSystemProxy:  true,
	}
	rec , err := store.Create(input)
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if rec.RebindAccountID != 42 {
		t.Errorf("expected RebindAccountID 42, got %d", rec.RebindAccountID)
	}
	if rec.ProjectID != "test-project" {
		t.Errorf("expected ProjectID 'test-project', got %q", rec.ProjectID)
	}
	if rec.ProxyURL != "http://proxy:8080" {
		t.Errorf("expected ProxyURL, got %q", rec.ProxyURL)
	}
	if !rec.UseSystemProxy {
		t.Error("expected UseSystemProxy true")
	}
}

// ---- MemoryOAuthSessionStore: Get ----

func TestMemorySessionStore_Get_ExistingSession(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	got := store.Get(rec.State)
	if got == nil {
		t.Fatal("should find created session")
	}
	if got.State != rec.State {
		t.Errorf("expected state %q, got %q", rec.State, got.State)
	}
}

func TestMemorySessionStore_Get_MissingSession(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	got := store.Get("nonexistent-state")
	if got != nil {
		t.Error("should return nil for missing session")
	}
}

func TestMemorySessionStore_Get_ReturnsSameInstance(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	got := store.Get(rec.State)
	// Change via get, verify it's the same pointer
	got.Error = "modified"
	gotAgain := store.Get(rec.State)
	if gotAgain.Error != "modified" {
		t.Error("Get should return reference to same session")
	}
}

// ---- MemoryOAuthSessionStore: Expiry and Pruning ----

func TestMemorySessionStore_ExpiredSession_ReturnsNil(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	// Manually expire the session
	store.mu.Lock()
	rec.ExpiresAt = time.Now().Add(-time.Second)
	store.mu.Unlock()

	got := store.Get(rec.State)
	if got != nil {
		t.Error("expired session should return nil")
	}
}

func TestMemorySessionStore_Pruning_RemovesExpiredOnly(t *testing.T) {
	store := NewMemoryOAuthSessionStore()

	// Create one expired session
	expired , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	store.mu.Lock()
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	store.mu.Unlock()

	// Create a valid session
	valid , err := store.Create(CreateSessionInput{Provider: "claude", RedirectURI: "http://localhost:54545/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	// Get the valid session (triggers pruning)
	got := store.Get(valid.State)
	if got == nil {
		t.Fatal("valid session should exist")
	}

	// Expired should be gone
	gotExpired := store.Get(expired.State)
	if gotExpired != nil {
		t.Error("expired session should have been pruned")
	}
}

func TestMemorySessionStore_Pruning_OnCreate(t *testing.T) {
	store := NewMemoryOAuthSessionStore()

	// Create an expired session
	expired , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	store.mu.Lock()
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	store.mu.Unlock()

	// Create a new session (triggers pruning)
	store.Create(CreateSessionInput{Provider: "claude", RedirectURI: "http://localhost:54545/callback"})

	// Expired should be gone
	gotExpired := store.Get(expired.State)
	if gotExpired != nil {
		t.Error("expired session should have been pruned on create")
	}
}

func TestMemorySessionStore_TTL_IsTenMinutes(t *testing.T) {
	if sessionTTL != 10*time.Minute {
		t.Errorf("expected sessionTTL = 10 minutes, got %v", sessionTTL)
	}
}

// ---- MemoryOAuthSessionStore: MarkSuccess ----

func TestMemorySessionStore_MarkSuccess(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	got := store.MarkSuccess(rec.State, 100, 200)
	if got == nil {
		t.Fatal("MarkSuccess should return updated session")
	}
	if got.Status != SessionSuccess {
		t.Errorf("expected status 'success', got %q", got.Status)
	}
	if got.AccountID != 100 {
		t.Errorf("expected AccountID 100, got %d", got.AccountID)
	}
	if got.SiteID != 200 {
		t.Errorf("expected SiteID 200, got %d", got.SiteID)
	}
	if got.Error != "" {
		t.Errorf("error should be cleared on success, got %q", got.Error)
	}
}

func TestMemorySessionStore_MarkSuccess_MissingSession(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	result := store.MarkSuccess("nonexistent", 1, 2)
	if result != nil {
		t.Error("MarkSuccess on missing state should return nil")
	}
}

func TestMemorySessionStore_MarkSuccess_ClearsPreviousError(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	store.MarkError(rec.State, "some error")
	store.MarkSuccess(rec.State, 1, 1)

	got := store.Get(rec.State)
	if got.Status != SessionSuccess {
		t.Errorf("expected success after markSuccess, got %q", got.Status)
	}
	if got.Error != "" {
		t.Errorf("error should be empty after success, got %q", got.Error)
	}
}

// ---- MemoryOAuthSessionStore: MarkError ----

func TestMemorySessionStore_MarkError(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	got := store.MarkError(rec.State, "authorization denied")
	if got == nil {
		t.Fatal("MarkError should return updated session")
	}
	if got.Status != SessionError {
		t.Errorf("expected status 'error', got %q", got.Status)
	}
	if got.Error != "authorization denied" {
		t.Errorf("expected error 'authorization denied', got %q", got.Error)
	}
}

func TestMemorySessionStore_MarkError_EmptyFallsBackToDefault(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	got := store.MarkError(rec.State, "")
	if got.Error != "OAuth failed" {
		t.Errorf("expected default error 'OAuth failed', got %q", got.Error)
	}
}

func TestMemorySessionStore_MarkError_WhitespaceFallsBackToDefault(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	rec , err := store.Create(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}

	got := store.MarkError(rec.State, "   ")
	if got.Error != "OAuth failed" {
		t.Errorf("expected default error 'OAuth failed' for whitespace, got %q", got.Error)
	}
}

func TestMemorySessionStore_MarkError_MissingSession(t *testing.T) {
	store := NewMemoryOAuthSessionStore()
	result := store.MarkError("nonexistent", "error")
	if result != nil {
		t.Error("MarkError on missing state should return nil")
	}
}

// ---- Global Session Store ----

func TestGlobalSessionStore_CreateAndGet(t *testing.T) {
	// Reset to a known store for this test.
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	rec, err := CreateSession(CreateSessionInput{
		Provider:    "codex",
		RedirectURI: "http://localhost:1455/callback",
	})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	got := GetSession(rec.State)
	if got == nil {
		t.Fatal("should find session via global store")
	}
	if got.State != rec.State {
		t.Error("state mismatch via global store")
	}
}

func TestGlobalSessionStore_MarkSuccess(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	rec, err := CreateSession(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	result := MarkSessionSuccess(rec.State, 10, 20)
	if result == nil {
		t.Fatal("MarkSessionSuccess should return session")
	}
	if result.Status != SessionSuccess {
		t.Errorf("expected success, got %q", result.Status)
	}
}

func TestGlobalSessionStore_MarkError(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	rec, err := CreateSession(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	result := MarkSessionError(rec.State, "test error")
	if result == nil {
		t.Fatal("MarkSessionError should return session")
	}
	if result.Status != SessionError {
		t.Errorf("expected error status, got %q", result.Status)
	}
}

func TestGlobalSessionStore_GetSession_ReturnsNil(t *testing.T) {
	custom := NewMemoryOAuthSessionStore()
	SetSessionStore(custom)

	got := GetSession("nonexistent")
	if got != nil {
		t.Error("should return nil for missing state")
	}
}

func TestSetSessionStore_Swappable(t *testing.T) {
	store1 := NewMemoryOAuthSessionStore()
	store2 := NewMemoryOAuthSessionStore()

	SetSessionStore(store1)
	rec, err := CreateSession(CreateSessionInput{Provider: "codex", RedirectURI: "http://localhost:1455/callback"})
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	state := rec.State

	// Switch to store2
	SetSessionStore(store2)

	// store2 should not have the session from store1
	got := GetSession(state)
	if got != nil {
		t.Error("after switching stores, old sessions should not be found")
	}
}

// ---- Helpers ----

func TestTrimOr_NonEmpty(t *testing.T) {
	result := trimOr("hello", "fallback")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTrimOr_Empty(t *testing.T) {
	result := trimOr("", "fallback")
	if result != "fallback" {
		t.Errorf("expected 'fallback', got %q", result)
	}
}

func TestTrimOr_Whitespace(t *testing.T) {
	result := trimOr("   ", "fallback")
	if result != "fallback" {
		t.Errorf("expected 'fallback' for whitespace, got %q", result)
	}
}

func TestTrimString_NoWhitespace(t *testing.T) {
	result := trimString("hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTrimString_LeadingWhitespace(t *testing.T) {
	result := trimString("  hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTrimString_TrailingWhitespace(t *testing.T) {
	result := trimString("hello  ")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTrimString_BothSides(t *testing.T) {
	result := trimString("  hello world  ")
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestTrimString_OnlyWhitespace(t *testing.T) {
	result := trimString("   ")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestTrimString_Empty(t *testing.T) {
	result := trimString("")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestAsNonEmptyString_Nil(t *testing.T) {
	result := asNonEmptyString(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestAsNonEmptyString_String(t *testing.T) {
	result := asNonEmptyString("hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestAsNonEmptyString_WhitespaceString(t *testing.T) {
	result := asNonEmptyString("   ")
	if result != "" {
		t.Errorf("expected empty for whitespace, got %q", result)
	}
}

func TestAsNonEmptyString_NonString(t *testing.T) {
	result := asNonEmptyString(42)
	if result != "" {
		t.Errorf("expected empty for non-string, got %q", result)
	}
}

func TestRandomBase64URL_Length(t *testing.T) {
	result , err := randomBase64URL(24)
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if len(result) != 32 {
		t.Errorf("24 bytes should encode to 32 base64url chars, got %d", len(result))
	}
}

func TestRandomBase64URL_Randomness(t *testing.T) {
	r1 , err := randomBase64URL(48)
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	r2 , err := randomBase64URL(48)
	if err != nil {
		t.Fatal("unexpected error: " + err.Error())
	}
	if r1 == r2 {
		t.Error("successive calls should produce different values")
	}
}
