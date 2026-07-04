package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func setupAccountsTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		db.Close()
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	cfg := &config.Config{
		AccountCredentialSecret: "test-secret-for-accounts",
	}

	r := chi.NewRouter()
	RegisterSitesRoutes(r, db.DB)
	RegisterAccountsRoutes(r, db.DB, cfg)
	return db, r, cfg
}

func setupAccountFixture(t *testing.T, r chi.Router) (int64, int64) {
	t.Helper()
	// Create a site first
	site := newSiteFixture(t, r, "Fixture Site", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// Create an account
	body := map[string]any{
		"siteId":      siteID,
		"accessToken": "sk-fixture-token",
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("fixture POST account: %d %s", resp.Code, resp.Body.String())
	}
	var m map[string]any
	json.Unmarshal(resp.Body.Bytes(), &m)
	accountID := int64(m["id"].(float64))
	return siteID, accountID
}

func setupAccountFixtureWithSite(t *testing.T, db *store.DB, r chi.Router, siteName, url string) (int64, int64) {
	t.Helper()
	site := newSiteFixture(t, r, siteName, url)
	siteID := int64(site["id"].(float64))
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, _ := db.Exec(
		"INSERT INTO accounts (site_id, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'sk-fixture', 'active', 1, ?, ?)",
		siteID, now, now,
	)
	accID, _ := res.LastInsertId()
	return siteID, accID
}

// ---- List Accounts ----

func TestAccounts_List(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	setupAccountFixture(t, r)

	resp := doGet(t, r, "/api/accounts")
	if resp.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", resp.Code, resp.Body.String())
	}

	header := resp.Header().Get("x-accounts-snapshot-cache")
	if header == "" {
		t.Error("expected x-accounts-snapshot-cache header")
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["generatedAt"] == nil {
		t.Error("expected generatedAt in response")
	}
	accounts, _ := result["accounts"].([]any)
	sites, _ := result["sites"].([]any)
	if len(accounts) == 0 {
		t.Error("expected at least 1 account")
	}
	if len(sites) == 0 {
		t.Error("expected at least 1 site in response")
	}
}

// ---- Create Account ----

func TestAccounts_Create(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "CreateSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":      siteID,
		"accessToken": "sk-test-create-token-12345",
		"username":    "testuser",
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create account: %d %s", resp.Code, resp.Body.String())
	}

	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	if created["id"] == nil {
		t.Error("expected id in response")
	}
	if created["tokenType"] != "auto" {
		t.Errorf("expected tokenType='auto', got %v", created["tokenType"])
	}
}

func TestAccounts_Create_InvalidSiteID(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 0, "accessToken": "sk-test"}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestAccounts_Create_NoToken(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "NoTokenSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{"siteId": siteID}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for no token, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["error"] != "请填写 Token" {
		t.Errorf("expected '请填写 Token', got %v", result["error"])
	}
}

func TestAccounts_Create_BatchAPIKeys(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "BatchKeySite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":       siteID,
		"accessTokens": []string{"sk-key-1", "sk-key-2", "sk-key-3"},
		"username":     "batchuser",
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch create: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["batch"] != true {
		t.Error("expected batch=true")
	}
	if n := result["totalCount"].(float64); n != 3 {
		t.Errorf("expected totalCount=3, got %v", n)
	}
	if n := result["createdCount"].(float64); n != 3 {
		t.Errorf("expected createdCount=3, got %v", n)
	}
	items, _ := result["items"].([]any)
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestAccounts_Create_WithCredentialMode(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "CModeSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-cm-test",
		"credentialMode": "apikey",
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create with credentialMode: %d %s", resp.Code, resp.Body.String())
	}

	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	if created["credentialMode"] != "apikey" {
		t.Errorf("expected credentialMode='apikey', got %v", created["credentialMode"])
	}
}

func TestAccounts_Create_SiteNotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 99999, "accessToken": "sk-test"}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Login ----

func TestAccounts_Login(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "LoginSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":   siteID,
		"username": "loginuser",
		"password": "pass123",
	}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("login: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	if result["reusedAccount"] != false {
		t.Errorf("expected reusedAccount=false (first login), got %v", result["reusedAccount"])
	}
}

func TestAccounts_Login_ReusedAccount(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "ReuseSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":   siteID,
		"username": "sameuser",
		"password": "pass123",
	}

	// First login
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	var first map[string]any
	json.Unmarshal(resp.Body.Bytes(), &first)
	if first["reusedAccount"] != false {
		t.Errorf("first login should have reusedAccount=false")
	}

	// Second login (same site+username)
	resp = doPostJSON(t, r, "/api/accounts/login", body)
	var second map[string]any
	json.Unmarshal(resp.Body.Bytes(), &second)
	if second["reusedAccount"] != true {
		t.Errorf("second login should have reusedAccount=true, got %v", second["reusedAccount"])
	}
}

func TestAccounts_Login_InvalidSiteID(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 0, "username": "u", "password": "p"}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestAccounts_Login_EmptyUsername(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 1, "username": "  ", "password": "p"}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestAccounts_Login_EmptyPassword(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 1, "username": "u", "password": ""}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Verify Token ----

func TestAccounts_VerifyToken(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "VerifySite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":      siteID,
		"accessToken": "sk-to-verify",
	}
	resp := doPostJSON(t, r, "/api/accounts/verify-token", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("verify token: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	if result["tokenType"] != "apikey" {
		t.Errorf("expected tokenType='apikey', got %v", result["tokenType"])
	}
}

func TestAccounts_VerifyToken_EmptyToken(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "EmptyTokSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{"siteId": siteID, "accessToken": ""}
	resp := doPostJSON(t, r, "/api/accounts/verify-token", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != false {
		t.Error("expected success=false for empty token")
	}
}

// ---- Rebind Session ----

func TestAccounts_RebindSession(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	siteID, accountID := setupAccountFixtureWithSite(t, db, r, "RebindSite", "https://api.openai.com")

	body := map[string]any{"accessToken": "new-session-token-xyz"}
	url := "/api/accounts/" + itoa(accountID) + "/rebind-session"
	resp := doPostJSON(t, r, url, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("rebind: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	if result["credentialMode"] != "session" {
		t.Errorf("expected credentialMode='session', got %v", result["credentialMode"])
	}

	// Verify accessToken was updated in DB
	var accessToken string
	db.QueryRow("SELECT access_token FROM accounts WHERE id = ?", accountID).Scan(&accessToken)
	if accessToken != "new-session-token-xyz" {
		t.Errorf("expected accessToken='new-session-token-xyz', got %q", accessToken)
	}
	_ = siteID
}

func TestAccounts_RebindSession_NotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"accessToken": "sk-new"}
	resp := doPostJSON(t, r, "/api/accounts/99999/rebind-session", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestAccounts_RebindSession_EmptyToken(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "EmptyRebind", "https://api.openai.com")

	body := map[string]any{"accessToken": ""}
	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/rebind-session", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Update Account ----

func TestAccounts_Update(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateSite", "https://api.openai.com")

	body := map[string]any{
		"username": "updatedname",
		"status":   "disabled",
	}
	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["Username"] != "updatedname" {
		t.Errorf("expected username='updatedname', got %v", result["Username"])
	}
	if result["Status"] != "disabled" {
		t.Errorf("expected status='disabled', got %v", result["Status"])
	}
}

func TestAccounts_Update_NotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"username": "ghost"}
	resp := doPutJSON(t, r, "/api/accounts/99999", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestAccounts_Update_SortOrderNormalized(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "SortSite", "https://api.openai.com")

	body := map[string]any{"sortOrder": -5}
	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 (normalized to 0), got %d: %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	// SortOrder is stored as int64 in Go struct, JSON key is "SortOrder"
	if so, ok := result["SortOrder"].(float64); ok && so != 0 {
		t.Errorf("expected sortOrder normalized to 0, got %v", so)
	}
}

// ---- Delete Account ----

func TestAccounts_Delete(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "DelSite", "https://api.openai.com")

	resp := doDelete(t, r, "/api/accounts/"+itoa(accountID))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete: %d", resp.Code)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", accountID).Scan(&count)
	if count != 0 {
		t.Errorf("expected account deleted, got %d", count)
	}
}

// ---- Batch Accounts ----

func TestAccounts_Batch_Enable(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, acc1 := setupAccountFixtureWithSite(t, db, r, "BatchEnSite", "https://api.openai.com")

	body := map[string]any{"ids": []int{int(acc1)}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch enable: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAccounts_Batch_Disable(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, acc1 := setupAccountFixtureWithSite(t, db, r, "BatchDisSite", "https://api.openai.com")

	body := map[string]any{"ids": []int{int(acc1)}, "action": "disable"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch disable: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAccounts_Batch_Delete(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, acc1 := setupAccountFixtureWithSite(t, db, r, "BatchDelSite", "https://api.openai.com")

	body := map[string]any{"ids": []int{int(acc1)}, "action": "delete"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch delete: %d %s", resp.Code, resp.Body.String())
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = ?", acc1).Scan(&count)
	if count != 0 {
		t.Errorf("expected account deleted after batch, got %d", count)
	}
}

func TestAccounts_Batch_RefreshBalance(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, acc1 := setupAccountFixtureWithSite(t, db, r, "BatchBalSite", "https://api.openai.com")

	body := map[string]any{"ids": []int{int(acc1)}, "action": "refreshBalance"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch refreshBalance: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAccounts_Batch_EmptyIDs(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"ids": []int{}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestAccounts_Batch_InvalidAction(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"ids": []int{1}, "action": "reload"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestAccounts_Batch_PartialFailure(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, acc1 := setupAccountFixtureWithSite(t, db, r, "PartialAcc", "https://api.openai.com")

	body := map[string]any{"ids": []int{int(acc1), 99999}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("%d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if len(result["successIds"].([]any)) != 1 {
		t.Error("expected 1 success")
	}
	if len(result["failedItems"].([]any)) != 1 {
		t.Error("expected 1 failure")
	}
}

// ---- Health Refresh ----

func TestAccounts_HealthRefresh_Sync(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"wait": true}
	resp := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("health refresh sync: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
}

func TestAccounts_HealthRefresh_Background(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"wait": false}
	resp := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("health refresh bg: expected 202, got %d", resp.Code)
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["queued"] != true {
		t.Error("expected queued=true")
	}
	if result["jobId"] == nil {
		t.Error("expected jobId")
	}
}

// ---- Refresh Balance ----

func TestAccounts_RefreshBalance(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "BalSite", "https://api.openai.com")

	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/balance", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("refresh balance: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["balance"] == nil {
		t.Error("expected balance field")
	}
}

func TestAccounts_RefreshBalance_NotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doPostJSON(t, r, "/api/accounts/99999/balance", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

// ---- Account Models ----

func TestAccounts_GetModels(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "ModelSite", "https://api.openai.com")

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	db.Exec("INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at) VALUES (?, 'gpt-4', 1, 0, ?)", accountID, now)
	db.Exec("INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at) VALUES (?, 'gpt-3.5', 1, 0, ?)", accountID, now)

	resp := doGet(t, r, "/api/accounts/"+itoa(accountID)+"/models")
	if resp.Code != http.StatusOK {
		t.Fatalf("get models: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	models, _ := result["models"].([]any)
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if n := result["totalCount"].(float64); n != 2 {
		t.Errorf("expected totalCount=2, got %v", n)
	}
}

func TestAccounts_GetModels_NotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doGet(t, r, "/api/accounts/99999/models")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestAccounts_GetModels_InvalidID(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doGet(t, r, "/api/accounts/not-a-number/models")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Manual Models ----

func TestAccounts_ManualModels(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "MMSite", "https://api.openai.com")

	body := map[string]any{"models": []string{"gpt-4-turbo", "gpt-4o"}}
	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/models/manual", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("manual models: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}

	// Verify models exist
	var count int
	db.QueryRow("SELECT COUNT(*) FROM model_availability WHERE account_id = ? AND is_manual = 1", accountID).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 manual models, got %d", count)
	}
}

func TestAccounts_ManualModels_Empty(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "EmptyMM", "https://api.openai.com")

	body := map[string]any{"models": []string{}}
	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/models/manual", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestAccounts_ManualModels_Deduplication(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "DedupMM", "https://api.openai.com")

	body := map[string]any{"models": []string{"gpt-4", "gpt-4", "  gpt-4  "}}
	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/models/manual", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("dedup manual models: %d", resp.Code)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM model_availability WHERE account_id = ? AND is_manual = 1", accountID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 model after dedup, got %d", count)
	}
}

// ---- Login Integration Tests ----

func TestAccounts_Login_SiteNotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 99999, "username": "u", "password": "p"}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != false {
		t.Error("expected success=false for missing site")
	}
}

func TestAccounts_Login_EncryptsPassword(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "EncryptSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":   siteID,
		"username": "encuser",
		"password": "my-secret-pw",
	}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("login: %d %s", resp.Code, resp.Body.String())
	}

	// Fetch the account and verify extraConfig has autoRelogin with passwordCipher
	var extraConfig *string
	db.QueryRow("SELECT extra_config FROM accounts WHERE username = 'encuser' AND site_id = ?", siteID).Scan(&extraConfig)
	if extraConfig == nil || !strings.Contains(*extraConfig, "passwordCipher") {
		t.Error("expected passwordCipher in extraConfig")
	}
	if !strings.Contains(*extraConfig, "autoRelogin") {
		t.Error("expected autoRelogin in extraConfig")
	}
	if !strings.Contains(*extraConfig, "v1:") {
		t.Error("expected v1: prefix in cipher (AES-256-GCM)")
	}
}

// ---- Verify Token Integration Tests ----

func TestAccounts_VerifyToken_SiteNotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 99999, "accessToken": "sk-test"}
	resp := doPostJSON(t, r, "/api/accounts/verify-token", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != false {
		t.Error("expected success=false for missing site")
	}
	if !strings.Contains(result["message"].(string), "site not found") {
		t.Errorf("expected 'site not found', got %v", result["message"])
	}
}

// ---- Extra Config Tests ----

func TestAccounts_Update_ProxyURL(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "ProxySite", "https://api.openai.com")

	body := map[string]any{"proxyUrl": "http://my-proxy:8080"}
	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update proxy: %d %s", resp.Code, resp.Body.String())
	}

	var extraConfig *string
	db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extraConfig)
	if extraConfig == nil || !strings.Contains(*extraConfig, "proxyUrl") {
		t.Error("expected proxyUrl in extraConfig")
	}
}
