package service

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// openTestDB opens an in-memory SQLite database for mutation tests.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite :memory:: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		db.Close()
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	return db
}

// createTestSite inserts a site and returns its ID.
func createTestSite(t *testing.T, db *store.DB, name, urlStr, platform string) int64 {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)`,
		name, urlStr, platform, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT site failed: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// createTestAccount inserts an account and returns its ID.
func createTestAccount(t *testing.T, db *store.DB, siteID int64, username *string, accessToken string) int64 {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, err := db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, is_pinned, sort_order,
		 checkin_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', 0, 0, 1, ?, ?)`,
		siteID, username, accessToken, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT account failed: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// ---- Credential Mode Resolution Tests ----

func TestNormalizeCredentialMode_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected AccountCredentialMode
	}{
		{"auto", CredentialModeAuto},
		{"AUTO", CredentialModeAuto},
		{" Auto ", CredentialModeAuto},
		{"session", CredentialModeSession},
		{"SESSION", CredentialModeSession},
		{"apikey", CredentialModeAPIKey},
		{"APIKEY", CredentialModeAPIKey},
		{" ApiKey ", CredentialModeAPIKey},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeCredentialMode(tt.input)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestNormalizeCredentialMode_Invalid(t *testing.T) {
	tests := []string{"", "invalid", "oauth", "password"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			got := NormalizeCredentialMode(input)
			if got != "" {
				t.Errorf("expected empty for %q, got %q", input, got)
			}
		})
	}
}

func TestResolveStoredCredentialMode_ExplicitSession(t *testing.T) {
	ec := `{"credentialMode":"session"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: ""}
	mode := ResolveStoredCredentialMode(account)
	if mode != CredentialModeSession {
		t.Errorf("expected 'session', got %q", mode)
	}
}

func TestResolveStoredCredentialMode_ExplicitAPIKey(t *testing.T) {
	ec := `{"credentialMode":"apikey"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: "sk-abc"}
	mode := ResolveStoredCredentialMode(account)
	if mode != CredentialModeAPIKey {
		t.Errorf("expected 'apikey', got %q", mode)
	}
}

func TestResolveStoredCredentialMode_AutoWithAccessToken(t *testing.T) {
	// auto mode: has accessToken -> session
	ec := `{"credentialMode":"auto"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: "session-token-123"}
	mode := ResolveStoredCredentialMode(account)
	if mode != CredentialModeSession {
		t.Errorf("expected 'session' (auto + has accessToken), got %q", mode)
	}
}

func TestResolveStoredCredentialMode_AutoWithoutAccessToken(t *testing.T) {
	ec := `{"credentialMode":"auto"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: ""}
	mode := ResolveStoredCredentialMode(account)
	if mode != CredentialModeAPIKey {
		t.Errorf("expected 'apikey' (auto + no accessToken), got %q", mode)
	}
}

func TestResolveStoredCredentialMode_NoConfigWithAccessToken(t *testing.T) {
	account := &store.Account{ExtraConfig: nil, AccessToken: "sk-xxx"}
	mode := ResolveStoredCredentialMode(account)
	if mode != CredentialModeSession {
		t.Errorf("expected 'session' (no config + accessToken), got %q", mode)
	}
}

func TestResolveStoredCredentialMode_NoConfigNoAccessToken(t *testing.T) {
	account := &store.Account{ExtraConfig: nil, AccessToken: ""}
	mode := ResolveStoredCredentialMode(account)
	if mode != CredentialModeAPIKey {
		t.Errorf("expected 'apikey' (no config + no accessToken), got %q", mode)
	}
}

func TestResolveRequestedCredentialMode_Valid(t *testing.T) {
	s := "session"
	mode := ResolveRequestedCredentialMode(&s)
	if mode != CredentialModeSession {
		t.Errorf("expected 'session', got %q", mode)
	}
}

func TestResolveRequestedCredentialMode_Nil(t *testing.T) {
	mode := ResolveRequestedCredentialMode(nil)
	if mode != CredentialModeAuto {
		t.Errorf("expected 'auto' for nil, got %q", mode)
	}
}

func TestResolveRequestedCredentialMode_Invalid(t *testing.T) {
	s := "invalid-mode"
	mode := ResolveRequestedCredentialMode(&s)
	if mode != CredentialModeAuto {
		t.Errorf("expected 'auto' for invalid input, got %q", mode)
	}
}

// ---- IsAPIKeyConnection Tests ----

func TestIsAPIKeyConnection_ExplicitTrue(t *testing.T) {
	ec := `{"credentialMode":"apikey"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: ""}
	if !IsAPIKeyConnection(account) {
		t.Error("expected API key connection")
	}
}

func TestIsAPIKeyConnection_ExplicitFalse(t *testing.T) {
	ec := `{"credentialMode":"session"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: "sk-xxx"}
	if IsAPIKeyConnection(account) {
		t.Error("expected NOT API key connection")
	}
}

func TestIsAPIKeyConnection_ImplicitByEmptyAccessToken(t *testing.T) {
	account := &store.Account{ExtraConfig: nil, AccessToken: ""}
	if !IsAPIKeyConnection(account) {
		t.Error("expected API key connection (implicit, empty accessToken)")
	}
}

func TestIsAPIKeyConnection_HasAccessToken(t *testing.T) {
	account := &store.Account{ExtraConfig: nil, AccessToken: "sk-real-token"}
	if IsAPIKeyConnection(account) {
		t.Error("expected NOT API key connection (has accessToken)")
	}
}

// ---- BuildCapabilities Tests ----

func TestBuildCapabilities_Session(t *testing.T) {
	ec := `{"credentialMode":"session"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: "session-token"}
	caps := BuildCapabilitiesForAccount(account)
	if !caps.CanCheckin {
		t.Error("session account should be able to checkin")
	}
	if !caps.CanRefreshBalance {
		t.Error("session account should be able to refresh balance")
	}
	if caps.ProxyOnly {
		t.Error("session account should NOT be proxyOnly")
	}
}

func TestBuildCapabilities_SessionNoToken(t *testing.T) {
	ec := `{"credentialMode":"session"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: ""}
	caps := BuildCapabilitiesForAccount(account)
	if caps.CanCheckin {
		t.Error("session without token should NOT be able to checkin")
	}
	if caps.CanRefreshBalance {
		t.Error("session without token should NOT be able to refresh balance")
	}
	if !caps.ProxyOnly {
		t.Error("session without token should be proxyOnly")
	}
}

func TestBuildCapabilities_APIKey(t *testing.T) {
	ec := `{"credentialMode":"apikey"}`
	account := &store.Account{ExtraConfig: &ec, AccessToken: "sk-xxx"}
	caps := BuildCapabilitiesForAccount(account)
	if caps.CanCheckin {
		t.Error("apikey account should NOT be able to checkin")
	}
	if caps.CanRefreshBalance {
		t.Error("apikey account should NOT be able to refresh balance")
	}
	if !caps.ProxyOnly {
		t.Error("apikey account should be proxyOnly")
	}
}

func TestBuildCapabilitiesFromCredentialMode(t *testing.T) {
	tests := []struct {
		mode      AccountCredentialMode
		hasToken  bool
		canCheck  bool
		canBal    bool
		proxyOnly bool
	}{
		{CredentialModeSession, true, true, true, false},
		{CredentialModeSession, false, false, false, true},
		{CredentialModeAPIKey, true, false, false, true},
		{CredentialModeAPIKey, false, false, false, true},
		{CredentialModeAuto, true, true, true, false},
		{CredentialModeAuto, false, false, false, true},
	}

	for _, tt := range tests {
		name := string(tt.mode) + "_token=" + boolStr(tt.hasToken)
		t.Run(name, func(t *testing.T) {
			caps := BuildCapabilitiesFromCredentialMode(tt.mode, tt.hasToken)
			if caps.CanCheckin != tt.canCheck {
				t.Errorf("CanCheckin: expected %v, got %v", tt.canCheck, caps.CanCheckin)
			}
			if caps.CanRefreshBalance != tt.canBal {
				t.Errorf("CanRefreshBalance: expected %v, got %v", tt.canBal, caps.CanRefreshBalance)
			}
			if caps.ProxyOnly != tt.proxyOnly {
				t.Errorf("ProxyOnly: expected %v, got %v", tt.proxyOnly, caps.ProxyOnly)
			}
		})
	}
}

// ---- ExtraConfig Helpers ----

func TestMergeExtraConfig_NewKey(t *testing.T) {
	existing := `{"a":1}`
	result := MergeExtraConfig(&existing, map[string]any{"b": 2})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	config := ParseExtraConfig(result)
	if config["a"].(float64) != 1 {
		t.Errorf("expected a=1, got %v", config["a"])
	}
	if config["b"].(float64) != 2 {
		t.Errorf("expected b=2, got %v", config["b"])
	}
}

func TestMergeExtraConfig_Overwrite(t *testing.T) {
	existing := `{"a":1}`
	result := MergeExtraConfig(&existing, map[string]any{"a": 99})
	config := ParseExtraConfig(result)
	if config["a"].(float64) != 99 {
		t.Errorf("expected a=99, got %v", config["a"])
	}
}

func TestMergeExtraConfig_DeleteKey(t *testing.T) {
	existing := `{"a":1,"b":2}`
	result := MergeExtraConfig(&existing, map[string]any{"a": nil})
	config := ParseExtraConfig(result)
	if _, ok := config["a"]; ok {
		t.Error("key 'a' should have been deleted")
	}
	if config["b"].(float64) != 2 {
		t.Errorf("expected b=2, got %v", config["b"])
	}
}

func TestMergeExtraConfig_EmptyExisting(t *testing.T) {
	result := MergeExtraConfig(nil, map[string]any{"new": "value"})
	config := ParseExtraConfig(result)
	if config["new"] != "value" {
		t.Errorf("expected new=value, got %v", config["new"])
	}
}

func TestMarshalExtraConfig_Roundtrip(t *testing.T) {
	original := map[string]any{
		"credentialMode": "session",
		"platformUserId": float64(42),
		"proxyUrl":       "http://proxy:8080",
	}
	encoded := MarshalExtraConfig(original)
	if encoded == nil {
		t.Fatal("expected non-nil")
	}
	decoded := ParseExtraConfig(encoded)
	if decoded["credentialMode"] != "session" {
		t.Errorf("credentialMode mismatch")
	}
	if decoded["platformUserId"].(float64) != 42 {
		t.Errorf("platformUserId mismatch")
	}
}

func TestGetProxyURLFromExtraConfig(t *testing.T) {
	ec := `{"proxyUrl":"http://proxy.example.com:3128"}`
	got := GetProxyURLFromExtraConfig(&ec)
	if got != "http://proxy.example.com:3128" {
		t.Errorf("expected proxy URL, got %q", got)
	}
}

func TestGetProxyURLFromExtraConfig_Absent(t *testing.T) {
	ec := `{"credentialMode":"session"}`
	got := GetProxyURLFromExtraConfig(&ec)
	if got != "" {
		t.Errorf("expected empty for absent proxyUrl, got %q", got)
	}
}

// ---- Account CRUD Integration Tests ----

func TestCreateAndGetAccount(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Test Site", "https://api.openai.com", "openai")

	username := "testuser"
	accountData := map[string]any{
		"siteId":          siteID,
		"username":        &username,
		"accessToken":     "sk-test-token-123",
		"apiToken":        nil,
		"balance":         0.0,
		"balanceUsed":     0.0,
		"quota":           0.0,
		"unitCost":        nil,
		"valueScore":      0.0,
		"status":          "active",
		"isPinned":        false,
		"sortOrder":       int64(0),
		"checkinEnabled":  true,
		"lastCheckinAt":   nil,
		"lastBalanceRefresh": nil,
		"oauthProvider":   nil,
		"oauthAccountKey": nil,
		"oauthProjectID":  nil,
		"extraConfig":     nil,
	}

	id, err := InsertAccount(db.DB, accountData)
	if err != nil {
		t.Fatalf("InsertAccount failed: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}

	// Fetch by ID
	account, err := GetAccountByID(db.DB, id)
	if err != nil {
		t.Fatalf("GetAccountByID failed: %v", err)
	}
	if account.SiteID != siteID {
		t.Errorf("expected siteID=%d, got %d", siteID, account.SiteID)
	}
	if account.Status != "active" {
		t.Errorf("expected status=active, got %q", account.Status)
	}
}

func TestUpdateAccountFields(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Test Site", "https://api.openai.com", "openai")
	accountID := createTestAccount(t, db, siteID, strPtr("testuser"), "sk-original")

	updates := map[string]any{
		"username": "updateduser",
		"status":   "disabled",
	}
	if err := UpdateAccountFields(db.DB, accountID, updates); err != nil {
		t.Fatalf("UpdateAccountFields failed: %v", err)
	}

	account, err := GetAccountByID(db.DB, accountID)
	if err != nil {
		t.Fatalf("GetAccountByID after update failed: %v", err)
	}
	if account.Username == nil || *account.Username != "updateduser" {
		t.Errorf("expected username='updateduser', got %v", account.Username)
	}
	if account.Status != "disabled" {
		t.Errorf("expected status='disabled', got %q", account.Status)
	}
}

func TestDeleteAccount(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Test Site", "https://api.openai.com", "openai")
	accountID := createTestAccount(t, db, siteID, strPtr("testuser"), "sk-to-delete")

	if err := DeleteAccount(db.DB, accountID); err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}

	_, err := GetAccountByID(db.DB, accountID)
	if err == nil {
		t.Error("expected error fetching deleted account")
	}
}

func TestGetAccountWithSiteByID(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Test Site", "https://api.openai.com", "openai")
	accountID := createTestAccount(t, db, siteID, strPtr("testuser"), "sk-with-site")

	row, err := GetAccountWithSiteByID(db.DB, accountID)
	if err != nil {
		t.Fatalf("GetAccountWithSiteByID failed: %v", err)
	}
	if row.Site.Name != "Test Site" {
		t.Errorf("expected site name 'Test Site', got %q", row.Site.Name)
	}
	if row.Site.Platform != "openai" {
		t.Errorf("expected platform 'openai', got %q", row.Site.Platform)
	}
	if row.Account.ID != accountID {
		t.Errorf("expected account ID %d, got %d", accountID, row.Account.ID)
	}
}

func TestListAccountsWithSites(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Site A", "https://api.openai.com", "openai")
	createTestAccount(t, db, siteID, strPtr("user1"), "sk-u1")
	createTestAccount(t, db, siteID, strPtr("user2"), "sk-u2")

	accounts, err := ListAccountsWithSites(db.DB)
	if err != nil {
		t.Fatalf("ListAccountsWithSites failed: %v", err)
	}
	if len(accounts) < 2 {
		t.Errorf("expected at least 2 accounts, got %d", len(accounts))
	}
}

func TestGetNextAccountSortOrder(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Sort Test", "https://api.openai.com", "openai")

	// No accounts yet
	order, err := GetNextAccountSortOrder(db.DB)
	if err != nil {
		t.Fatalf("GetNextAccountSortOrder failed: %v", err)
	}
	if order != 0 {
		t.Errorf("expected sortOrder=0 for empty table, got %d", order)
	}

	createTestAccount(t, db, siteID, strPtr("u1"), "sk1")
	order, err = GetNextAccountSortOrder(db.DB)
	if err != nil {
		t.Fatalf("GetNextAccountSortOrder failed: %v", err)
	}
	if order != 1 {
		t.Errorf("expected sortOrder=1 after one account, got %d", order)
	}
}

// ---- Event Tests ----

func TestCreateEvent(t *testing.T) {
	db := openTestDB(t)
	siteID := createTestSite(t, db, "Event Site", "https://api.openai.com", "openai")

	err := CreateEvent(db.DB, "status", "Test Event", "Test message", "info", siteID, "site")
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM events WHERE type = 'status' AND title = 'Test Event'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

// ---- HasSessionToken Tests ----

func TestHasSessionToken_True(t *testing.T) {
	if !HasSessionToken("sk-real-token") {
		t.Error("expected true for non-empty token")
	}
}

func TestHasSessionToken_False(t *testing.T) {
	if HasSessionToken("") {
		t.Error("expected false for empty string")
	}
	if HasSessionToken("   ") {
		t.Error("expected false for whitespace-only string")
	}
}

// ---- Helpers ----

func strPtr(s string) *string {
	return &s
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
