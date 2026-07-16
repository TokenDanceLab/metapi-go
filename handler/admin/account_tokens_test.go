package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func setupTokensTest(t *testing.T) (*store.DB, chi.Router) {
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
		AccountCredentialSecret: "token-test-secret",
	}

	r := chi.NewRouter()
	RegisterSitesRoutes(r, db.DB)
	RegisterAccountsRoutes(r, db.DB, cfg)
	RegisterAccountTokensRoutes(r, db.DB)
	return db, r
}

func setupTokensPostgresTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	db, r, _ := setupAccountsPostgresTest(t)
	RegisterAccountTokensRoutes(r, db.DB)
	return db, r
}

// tokenFixture creates a site + account with accessToken and returns IDs.
func tokenFixture(t *testing.T, db *store.DB, r chi.Router) (int64, int64) {
	t.Helper()

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Create site
	site := newSiteFixture(t, r, "TokenSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// Create account with accessToken (session mode, not apikey connection)
	extraConfig := `{"credentialMode":"session"}`
	username := "tokuser"
	res, err := db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, is_pinned, sort_order,
		 checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, ?, 'sk-session-token', 'active', 0, 0, 1, ?, ?, ?)`,
		siteID, &username, &extraConfig, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT account: %v", err)
	}
	accountID, _ := res.LastInsertId()
	return siteID, accountID
}

// createTokenFixture inserts a token directly and returns its ID.
func createTokenFixture(t *testing.T, db *store.DB, accountID int64, name, tokenVal, group string, enabled, isDefault bool) int64 {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	var tokenGroup *string
	if group != "" {
		tokenGroup = &group
	}
	valueStatus := "ready"
	if IsMaskedTokenValue(tokenVal) {
		valueStatus = "masked_pending"
	}
	res, err := db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, token_group, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'manual', ?, ?, ?, ?)`,
		accountID, name, tokenVal, tokenGroup, valueStatus, enabled, isDefault, now, now,
	)
	if err != nil {
		t.Fatalf("INSERT token: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

// ---- List Tokens ----

func TestTokens_List(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	createTokenFixture(t, db, accountID, "default", "sk-abc123", "default", true, true)

	resp := doGet(t, r, "/api/account-tokens?accountId="+itoa(accountID))
	if resp.Code != http.StatusOK {
		t.Fatalf("list tokens: %d %s", resp.Code, resp.Body.String())
	}

	var tokens []map[string]any
	json.Unmarshal(resp.Body.Bytes(), &tokens)
	if len(tokens) == 0 {
		t.Error("expected at least 1 token in list")
	}
	// Token should have tokenMasked, not raw token
	if len(tokens) > 0 {
		tok := tokens[0]
		if tok["tokenMasked"] == nil || tok["tokenMasked"] == "" {
			t.Error("expected tokenMasked in response")
		}
		if tok["token"] != nil && tok["token"] != "" {
			t.Error("plain token should not be exposed in list")
		}
	}
}

func TestTokens_List_NoTokens(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	resp := doGet(t, r, "/api/account-tokens?accountId="+itoa(accountID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var tokens []map[string]any
	json.Unmarshal(resp.Body.Bytes(), &tokens)
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

func TestTokens_List_AllWithoutFilter(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	createTokenFixture(t, db, accountID, "t1", "sk-t1", "", true, true)

	resp := doGet(t, r, "/api/account-tokens")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var tokens []map[string]any
	json.Unmarshal(resp.Body.Bytes(), &tokens)
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}
}

// ---- Create Token (Local Path) ----

func TestTokens_Create_Local(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	body := map[string]any{
		"accountId": accountID,
		"token":     "sk-new-local-token-12345",
	}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create local token: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	tok, _ := result["token"].(map[string]any)
	if tok == nil {
		t.Fatal("expected token object in response")
	}
	if tok["valueStatus"] != "ready" {
		t.Errorf("expected valueStatus='ready', got %v", tok["valueStatus"])
	}
}

func TestTokens_CreateLocal_DefaultFailureCleansInsertedToken(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	if _, err := db.Exec(`CREATE TRIGGER fail_create_default_api_token
		BEFORE UPDATE OF api_token ON accounts
		BEGIN
			SELECT RAISE(ABORT, 'forced create default failure');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	resp := doPostJSON(t, r, "/api/account-tokens", map[string]any{
		"accountId": accountID,
		"token":     "plain-local-token",
	})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on failed default setup, got %d: %s", resp.Code, resp.Body.String())
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE account_id = ?", accountID).Scan(&count); err != nil {
		t.Fatalf("count account_tokens: %v", err)
	}
	if count != 0 {
		t.Fatalf("account token rows = %d, want cleanup to leave none", count)
	}
}

func TestTokens_Postgres_CreateListUpdateDefaultValueAndDelete(t *testing.T) {
	db, r := setupTokensPostgresTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	siteURL := "https://pg-tokens-" + suffix + ".example.com"
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", siteURL)
	})

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "PG Tokens " + suffix,
		"url":      siteURL,
		"platform": "openai",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("postgres create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	if err := json.Unmarshal(siteResp.Body.Bytes(), &site); err != nil {
		t.Fatalf("unmarshal site: %v", err)
	}
	siteID := int64(site["id"].(float64))

	accountResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":      siteID,
		"accessToken": "pg-session-token-" + suffix,
		"username":    "pg-token-user-" + suffix,
	})
	if accountResp.Code != http.StatusOK {
		t.Fatalf("postgres create account: %d %s", accountResp.Code, accountResp.Body.String())
	}
	var account map[string]any
	if err := json.Unmarshal(accountResp.Body.Bytes(), &account); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	accountID := int64(account["id"].(float64))

	createResp := doPostJSON(t, r, "/api/account-tokens", map[string]any{
		"accountId": accountID,
		"token":     "pg-token-" + suffix,
		"name":      "pg-token",
		"group":     "pg-group",
		"isDefault": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("postgres create token: expected 200, got %d: %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	tokenMap, ok := created["token"].(map[string]any)
	if !ok {
		t.Fatalf("missing token object: %#v", created)
	}
	tokenID := int64(tokenMap["id"].(float64))

	listResp := doGet(t, r, "/api/account-tokens?accountId="+itoa(accountID))
	if listResp.Code != http.StatusOK {
		t.Fatalf("postgres list tokens: expected 200, got %d: %s", listResp.Code, listResp.Body.String())
	}
	var tokens []map[string]any
	if err := json.Unmarshal(listResp.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("unmarshal token list: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}

	updateResp := doPutJSON(t, r, "/api/account-tokens/"+itoa(tokenID), map[string]any{
		"name":    "pg-token-updated",
		"enabled": true,
	})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("postgres update token: expected 200, got %d: %s", updateResp.Code, updateResp.Body.String())
	}

	defaultResp := doPostJSON(t, r, "/api/account-tokens/"+itoa(tokenID)+"/default", nil)
	if defaultResp.Code != http.StatusOK {
		t.Fatalf("postgres set default: expected 200, got %d: %s", defaultResp.Code, defaultResp.Body.String())
	}

	valueResp := doGet(t, r, "/api/account-tokens/"+itoa(tokenID)+"/value")
	if valueResp.Code != http.StatusOK {
		t.Fatalf("postgres get value: expected 200, got %d: %s", valueResp.Code, valueResp.Body.String())
	}

	deleteResp := doDelete(t, r, "/api/account-tokens/"+itoa(tokenID))
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("postgres delete token: expected 200, got %d: %s", deleteResp.Code, deleteResp.Body.String())
	}
}

func TestTokens_Create_Local_Masked(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	body := map[string]any{
		"accountId": accountID,
		"token":     "sk-abc***123", // masked token
	}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create masked token: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	tok, _ := result["token"].(map[string]any)
	if tok["valueStatus"] != "masked_pending" {
		t.Errorf("expected valueStatus='masked_pending', got %v", tok["valueStatus"])
	}
	if tok["enabled"] != false {
		t.Errorf("expected enabled=false for masked token, got %v", tok["enabled"])
	}
	if tok["isDefault"] != false {
		t.Errorf("expected isDefault=false for masked token, got %v", tok["isDefault"])
	}
}

func TestTokens_Create_Local_AutoName(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	// First token should be named "default"
	body := map[string]any{"accountId": accountID, "token": "sk-t1"}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	var r1 map[string]any
	json.Unmarshal(resp.Body.Bytes(), &r1)
	t1, _ := r1["token"].(map[string]any)
	if t1["name"] != "default" {
		t.Errorf("expected first token named 'default', got %v", t1["name"])
	}

	// Second token should be named "token-2"
	body2 := map[string]any{"accountId": accountID, "token": "sk-t2"}
	resp = doPostJSON(t, r, "/api/account-tokens", body2)
	var r2 map[string]any
	json.Unmarshal(resp.Body.Bytes(), &r2)
	t2, _ := r2["token"].(map[string]any)
	if t2["name"] != "token-2" {
		t.Errorf("expected second token named 'token-2', got %v", t2["name"])
	}
}

func TestTokens_Create_Local_ExplicitName(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	body := map[string]any{"accountId": accountID, "token": "sk-t1", "name": "my-custom-name"}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	tok, _ := result["token"].(map[string]any)
	if tok["name"] != "my-custom-name" {
		t.Errorf("expected 'my-custom-name', got %v", tok["name"])
	}
}

func TestTokens_Create_Local_WithGroup(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	body := map[string]any{"accountId": accountID, "token": "sk-t1", "group": "premium"}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	tok, _ := result["token"].(map[string]any)
	if tok["tokenGroup"] != "premium" {
		t.Errorf("expected group='premium', got %v", tok["tokenGroup"])
	}
}

func TestTokens_Create_InvalidAccountID(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"accountId": 0, "token": "sk-test"}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestTokens_Create_AccountNotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"accountId": 99999, "token": "sk-test"}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestTokens_Create_Upstream_APIKeyConnection(t *testing.T) {
	db, r := setupTokensTest(t)
	site := newSiteFixture(t, r, "AKCSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// API key connection: credentialMode=apikey, empty accessToken
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	extraConfig := `{"credentialMode":"apikey"}`
	res, _ := db.Exec(
		`INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, '', 'active', 1, ?, ?, ?)`,
		siteID, &extraConfig, now, now,
	)
	accountID, _ := res.LastInsertId()

	// Upstream create (no token value)
	body := map[string]any{"accountId": accountID}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for apikey connection, got %d", resp.Code)
	}
}

func TestTokens_Create_Upstream_DisabledSite(t *testing.T) {
	db, r := setupTokensTest(t)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Create disabled site
	res, _ := db.Exec(
		"INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES ('DisabledSite', 'https://api.openai.com', 'openai', 'disabled', ?, ?)",
		now, now,
	)
	siteID, _ := res.LastInsertId()

	// Account with access token
	extraConfig := `{"credentialMode":"session"}`
	res, _ = db.Exec(
		`INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, 'sk-tok', 'active', 1, ?, ?, ?)`,
		siteID, &extraConfig, now, now,
	)
	accountID, _ := res.LastInsertId()

	// Upstream create (no token value) should fail because site is disabled
	body := map[string]any{"accountId": accountID}
	resp := doPostJSON(t, r, "/api/account-tokens", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disabled site, got %d %s", resp.Code, resp.Body.String())
	}
}

// ---- Batch Tokens ----

func TestTokens_Batch_Enable(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-t1", "", false, false)

	body := map[string]any{"ids": []int{int(tokID)}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch enable: %d %s", resp.Code, resp.Body.String())
	}

	// Verify enabled
	var enabled bool
	db.QueryRow("SELECT enabled FROM account_tokens WHERE id = ?", tokID).Scan(&enabled)
	if !enabled {
		t.Error("expected enabled=true after batch enable")
	}
}

func TestTokens_Batch_Disable(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-t1", "", true, true)

	body := map[string]any{"ids": []int{int(tokID)}, "action": "disable"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch disable: %d %s", resp.Code, resp.Body.String())
	}

	var enabled bool
	db.QueryRow("SELECT enabled FROM account_tokens WHERE id = ?", tokID).Scan(&enabled)
	if enabled {
		t.Error("expected enabled=false after batch disable")
	}
}

func TestTokens_Batch_Delete(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-t1", "", true, false)

	body := map[string]any{"ids": []int{int(tokID)}, "action": "delete"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch delete: %d %s", resp.Code, resp.Body.String())
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE id = ?", tokID).Scan(&count)
	if count != 0 {
		t.Errorf("expected token deleted, got %d", count)
	}
}

func TestTokens_Batch_MaskedPending_Enable(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "masked-t", "sk-abc***123", "", false, false)

	body := map[string]any{"ids": []int{int(tokID)}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	fails, _ := result["failedItems"].([]any)
	if len(fails) != 1 {
		t.Errorf("expected 1 failure for masked-pending token, got %d", len(fails))
	}
}

func TestTokens_Batch_EmptyIDs(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"ids": []int{}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestTokens_Batch_InvalidAction(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"ids": []int{1}, "action": "reload"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestTokens_Batch_NestedAPIKeyConnection(t *testing.T) {
	db, r := setupTokensTest(t)
	site := newSiteFixture(t, r, "BatchAPIConn", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	extraConfig := `{"credentialMode":"apikey"}`
	res, _ := db.Exec(
		"INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, '', 'active', 1, ?, ?, ?)",
		siteID, &extraConfig, now, now,
	)
	accountID, _ := res.LastInsertId()
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-t1", "", true, false)

	body := map[string]any{"ids": []int{int(tokID)}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/account-tokens/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	fails, _ := result["failedItems"].([]any)
	if len(fails) != 1 {
		t.Errorf("expected 1 failure for API key connection batch, got %d", len(fails))
	}
}

// ---- Update Token ----

func TestTokens_Update(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "old-name", "sk-old", "", true, false)

	body := map[string]any{"name": "new-name"}
	url := "/api/account-tokens/" + itoa(tokID)
	resp := doPutJSON(t, r, url, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	tok, _ := result["token"].(map[string]any)
	if tok["name"] != "new-name" {
		t.Errorf("expected name='new-name', got %v", tok["name"])
	}
}

func TestTokens_Update_MaskedToken(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-clear", "", true, true)

	// Update with masked value
	body := map[string]any{"token": "sk-xxx***yyy"}
	resp := doPutJSON(t, r, "/api/account-tokens/"+itoa(tokID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update masked: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	tok, _ := result["token"].(map[string]any)
	vs, _ := tok["value_status"].(string)
	if vs != TokenValueStatusMaskedPending {
		t.Logf("value_status after masked update: %q", vs)
	}
	// After masked update, token should be disabled and not default
	var enabled, isDefault bool
	var valueStatus string
	db.QueryRow("SELECT enabled, is_default, value_status FROM account_tokens WHERE id = ?", tokID).Scan(&enabled, &isDefault, &valueStatus)
	if enabled {
		t.Error("expected enabled=false after masked update")
	}
	if isDefault {
		t.Error("expected isDefault=false after masked update")
	}
}

func TestTokens_Update_NotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"name": "ghost"}
	resp := doPutJSON(t, r, "/api/account-tokens/99999", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestTokens_Update_EmptyToken(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-old", "", true, false)

	body := map[string]any{"token": ""}
	resp := doPutJSON(t, r, "/api/account-tokens/"+itoa(tokID), body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Set Default ----

func TestTokens_SetDefault(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-t1", "", true, false)

	resp := doPostJSON(t, r, "/api/account-tokens/"+itoa(tokID)+"/default", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("set default: %d %s", resp.Code, resp.Body.String())
	}

	var isDefault bool
	db.QueryRow("SELECT is_default FROM account_tokens WHERE id = ?", tokID).Scan(&isDefault)
	if !isDefault {
		t.Error("expected isDefault=true after set default")
	}
}

func TestTokens_SetDefault_MaskedPending(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "masked", "sk-abc***123", "", false, false)

	resp := doPostJSON(t, r, "/api/account-tokens/"+itoa(tokID)+"/default", nil)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for masked default, got %d", resp.Code)
	}
}

func TestTokens_SetDefault_NotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	resp := doPostJSON(t, r, "/api/account-tokens/99999/default", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

// ---- Get Token Value ----

func TestTokens_GetValue(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "t1", "sk-clear-token-value", "", true, true)

	resp := doGet(t, r, "/api/account-tokens/"+itoa(tokID)+"/value")
	if resp.Code != http.StatusOK {
		t.Fatalf("get value: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	if result["tokenMasked"] == nil || result["tokenMasked"] == "" {
		t.Error("expected tokenMasked in response")
	}
}

func TestTokens_GetValue_Masked(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "masked", "sk-abc***123", "", false, false)

	resp := doGet(t, r, "/api/account-tokens/"+itoa(tokID)+"/value")
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for masked value, got %d", resp.Code)
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	errMsg, _ := result["error"].(string)
	if !strings.Contains(errMsg, "脱敏") {
		t.Errorf("expected masked token error message, got %#v", result)
	}
}

func TestTokens_GetValue_APIKeyConnection(t *testing.T) {
	db, r := setupTokensTest(t)
	site := newSiteFixture(t, r, "ValueAKC", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	ec := `{"credentialMode":"apikey"}`
	res, _ := db.Exec(
		"INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, '', 'active', 1, ?, ?, ?)",
		siteID, &ec, now, now,
	)
	accID, _ := res.LastInsertId()
	tokID := createTokenFixture(t, db, accID, "t1", "sk-t1", "", true, false)

	resp := doGet(t, r, "/api/account-tokens/"+itoa(tokID)+"/value")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for apikey connection, got %d", resp.Code)
	}
}

// ---- Delete Token ----

func TestTokens_Delete(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "todel", "sk-del", "", true, false)

	resp := doDelete(t, r, "/api/account-tokens/"+itoa(tokID))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.Code, resp.Body.String())
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE id = ?", tokID).Scan(&count)
	if count != 0 {
		t.Errorf("expected token deleted, got %d", count)
	}
}

func TestTokens_Delete_DefaultRepair(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	createTokenFixture(t, db, accountID, "default-tok", "sk-default", "", true, true)
	tok2 := createTokenFixture(t, db, accountID, "backup", "sk-backup", "", true, false)

	// Delete the default token - repairDefaultToken should pick tok2
	resp := doDelete(t, r, "/api/account-tokens/"+itoa(defaultID(db, accountID)))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete default: %d", resp.Code)
	}

	// Verify tok2 is now default
	var isDef bool
	db.QueryRow("SELECT is_default FROM account_tokens WHERE id = ?", tok2).Scan(&isDef)
	if !isDef {
		t.Error("expected backup token to become default after default deletion")
	}
}

func TestTokens_Delete_NotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	resp := doDelete(t, r, "/api/account-tokens/99999")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

// ---- Sync Account ----

func TestTokens_SyncAccount(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)

	resp := doPostJSON(t, r, "/api/account-tokens/sync/"+itoa(accountID), nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	if result["status"] != "skipped" {
		t.Errorf("expected status='skipped' (stub), got %v", result["status"])
	}
}

func TestTokens_SyncAccount_NotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	resp := doPostJSON(t, r, "/api/account-tokens/sync/99999", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

// ---- Sync All ----

func TestTokens_SyncAll_Sync(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"wait": true}
	resp := doPostJSON(t, r, "/api/account-tokens/sync-all", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("sync-all sync: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
}

func TestTokens_SyncAll_Background(t *testing.T) {
	_, r := setupTokensTest(t)
	body := map[string]any{"wait": false}
	resp := doPostJSON(t, r, "/api/account-tokens/sync-all", body)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("sync-all bg: expected 202, got %d", resp.Code)
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["queued"] != true {
		t.Error("expected queued=true")
	}
}

// ---- Get Groups ----

func TestTokens_GetGroups(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	createTokenFixture(t, db, accountID, "t1", "sk-t1", "group-a", true, false)
	createTokenFixture(t, db, accountID, "t2", "sk-t2", "group-b", true, false)

	resp := doGet(t, r, "/api/account-tokens/groups/"+itoa(accountID))
	if resp.Code != http.StatusOK {
		t.Fatalf("get groups: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	groups, _ := result["groups"].([]any)
	// Should have at least "group-a" and "group-b"
	if len(groups) < 2 {
		t.Errorf("expected at least 2 groups, got %d", len(groups))
	}
}

func TestTokens_GetGroups_NotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	resp := doGet(t, r, "/api/account-tokens/groups/99999")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestTokens_GetGroups_APIKeyConnection(t *testing.T) {
	db, r := setupTokensTest(t)
	site := newSiteFixture(t, r, "GroupAKC", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	ec := `{"credentialMode":"apikey"}`
	res, _ := db.Exec(
		"INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, '', 'active', 1, ?, ?, ?)",
		siteID, &ec, now, now,
	)
	accID, _ := res.LastInsertId()

	resp := doGet(t, r, "/api/account-tokens/groups/"+itoa(accID))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for apikey connection, got %d", resp.Code)
	}
}

// ---- Get Account Default ----

func TestTokens_GetAccountDefault(t *testing.T) {
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	createTokenFixture(t, db, accountID, "default", "sk-default-token", "", true, true)

	resp := doGet(t, r, "/api/account-tokens/account/"+itoa(accountID)+"/default")
	if resp.Code != http.StatusOK {
		t.Fatalf("get default: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	tok, _ := result["token"].(map[string]any)
	if tok == nil {
		t.Fatal("expected token in response")
	}
	if tok["tokenMasked"] == nil || tok["tokenMasked"] == "" {
		t.Error("expected tokenMasked in default token response")
	}
}

func TestTokens_GetAccountDefault_APIKeyConnection(t *testing.T) {
	db, r := setupTokensTest(t)
	site := newSiteFixture(t, r, "DefAKC", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	ec := `{"credentialMode":"apikey"}`
	res, _ := db.Exec(
		"INSERT INTO accounts (site_id, access_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, '', 'active', 1, ?, ?, ?)",
		siteID, &ec, now, now,
	)
	accID, _ := res.LastInsertId()

	resp := doGet(t, r, "/api/account-tokens/account/"+itoa(accID)+"/default")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["token"] != nil {
		t.Error("expected token=nil for apikey connection")
	}
}

func TestTokens_GetAccountDefault_NotFound(t *testing.T) {
	_, r := setupTokensTest(t)
	resp := doGet(t, r, "/api/account-tokens/account/99999/default")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["token"] != nil {
		t.Error("expected token=nil for non-existent account")
	}
}

// ---- Token Value Status Tests ----

func TestIsMaskedTokenValue_True(t *testing.T) {
	tests := []string{
		"sk-abc***123",
		"sk-***xyz",
		"sk-proj-•••-key",
		"***hidden***",
	}
	for _, val := range tests {
		if !IsMaskedTokenValue(val) {
			t.Errorf("expected masked for %q", val)
		}
	}
}

func TestIsMaskedTokenValue_False(t *testing.T) {
	tests := []string{
		"sk-proj-real-token-12345",
		"plain-token-no-stars",
		"",
		"   ",
	}
	for _, val := range tests {
		if IsMaskedTokenValue(val) {
			t.Errorf("expected NOT masked for %q", val)
		}
	}
}

// ---- Helpers ----

func defaultID(db *store.DB, accountID int64) int64 {
	var id int64
	db.QueryRow("SELECT id FROM account_tokens WHERE account_id = ? AND is_default = 1", accountID).Scan(&id)
	return id
}
