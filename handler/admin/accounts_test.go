package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/service/alert"
	"github.com/tokendancelab/metapi-go/store"
)

func setupAccountsTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
	t.Helper()
	globalAccountsCache = &accountsSnapshotCache{ttl: 30 * time.Second}

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

func setupAccountsPostgresTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
	t.Helper()
	globalAccountsCache = &accountsSnapshotCache{ttl: 30 * time.Second}

	db, r := setupSitesPostgresTest(t)
	cfg := &config.Config{
		AccountCredentialSecret: "test-secret-for-accounts",
	}
	RegisterAccountsRoutes(r, db.DB, cfg)
	return db, r, cfg
}

func setupAccountFixture(t *testing.T, r chi.Router) (int64, int64) {
	t.Helper()
	// Create a site first
	site := newSiteFixture(t, r, "Fixture Site", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// Create an account (skip upstream verify for generic fixtures).
	body := map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-fixture-token",
		"credentialMode": "apikey",
		"skipModelFetch": true,
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

func newOpenAIModelsServer(t *testing.T, expectedToken string, models []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if expectedToken != "" && r.Header.Get("Authorization") != "Bearer "+expectedToken {
			http.Error(w, `{"error":"bad token"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		items := make([]map[string]any, 0, len(models))
		for _, m := range models {
			items = append(items, map[string]any{"id": m})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	}))
	t.Cleanup(server.Close)
	return server
}

func newSessionVerifyServer(t *testing.T, expectedToken, username string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self":
			if r.Header.Get("Authorization") != "Bearer "+expectedToken {
				http.Error(w, `{"success":false,"message":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"id":         7,
					"username":   username,
					"quota":      1_000_000,
					"used_quota": 0,
				},
			})
		case "/api/user/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    []any{},
			})
		case "/v1/models":
			http.Error(w, `{"error":"not an apikey"}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
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

func insertAnyRouterBalanceSite(t *testing.T, db *store.DB, siteURL string, now string) int64 {
	t.Helper()
	siteRes, err := db.Exec(
		"INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, 'anyrouter', 'active', ?, ?)",
		"AnyRouter balance", siteURL, now, now,
	)
	if err != nil {
		t.Fatalf("insert anyrouter site: %v", err)
	}
	siteID, err := siteRes.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	return siteID
}

func setupAnyRouterBalanceAccount(t *testing.T, db *store.DB, siteURL string) (int64, int64) {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	siteID := insertAnyRouterBalanceSite(t, db, siteURL, now)
	accRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, ?, ?, 'active', 1, ?, ?)",
		siteID, "anyrouter-user", "session-token", now, now,
	)
	if err != nil {
		t.Fatalf("insert anyrouter account: %v", err)
	}
	accountID, err := accRes.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}
	return siteID, accountID
}

func setupAnyRouterAPIKeyAccount(t *testing.T, db *store.DB, siteURL string) (int64, int64) {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	siteID := insertAnyRouterBalanceSite(t, db, siteURL, now)
	accRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', 0, ?, ?, ?)",
		siteID, nil, "anyrouter-api-key-token", "anyrouter-api-key-token", `{"credentialMode":"apikey"}`, now, now,
	)
	if err != nil {
		t.Fatalf("insert anyrouter api-key account: %v", err)
	}
	accountID, err := accRes.LastInsertId()
	if err != nil {
		t.Fatalf("api-key account LastInsertId: %v", err)
	}
	return siteID, accountID
}

func newAnyRouterBalanceServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/user/self" {
			http.NotFound(w, r)
			return
		}
		(*calls)++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":           42,
				"username":     "anyrouter-user",
				"quota":        1_000_000,
				"used_quota":   250_000,
				"today_income": 50_000,
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func newAnyRouterLoginServer(t *testing.T, calls *int, loginOK bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/user/login" {
			http.NotFound(w, r)
			return
		}
		(*calls)++
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode login body: %v", err)
		}
		if body["username"] == "" || body["password"] == "" {
			t.Fatalf("login body missing credentials: %#v", body)
		}
		if !loginOK {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"message": "invalid credentials",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":      true,
			"access_token": "anyrouter-session-token-from-upstream",
		})
	}))
	t.Cleanup(server.Close)
	return server
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

func TestAccounts_UpdateClearsSnapshotCache(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixture(t, r)

	first := doGet(t, r, "/api/accounts")
	if first.Code != http.StatusOK {
		t.Fatalf("first list accounts: %d %s", first.Code, first.Body.String())
	}
	second := doGet(t, r, "/api/accounts")
	if second.Header().Get("x-accounts-snapshot-cache") != "hit" {
		t.Fatalf("second list should hit cache, header=%q", second.Header().Get("x-accounts-snapshot-cache"))
	}

	update := doPutJSON(t, r, "/api/accounts/"+strconv.FormatInt(accountID, 10), map[string]any{
		"status": "disabled",
	})
	if update.Code != http.StatusOK {
		t.Fatalf("update account: %d %s", update.Code, update.Body.String())
	}

	after := doGet(t, r, "/api/accounts")
	if after.Code != http.StatusOK {
		t.Fatalf("after list accounts: %d %s", after.Code, after.Body.String())
	}
	if after.Header().Get("x-accounts-snapshot-cache") != "miss" {
		t.Fatalf("after update list should miss cache, header=%q", after.Header().Get("x-accounts-snapshot-cache"))
	}
	var result map[string]any
	json.Unmarshal(after.Body.Bytes(), &result)
	accounts, _ := result["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts length = %d, want 1", len(accounts))
	}
	account, _ := accounts[0].(map[string]any)
	if account["status"] != "disabled" {
		t.Fatalf("cached account status = %v, want disabled", account["status"])
	}
}

// ---- Create Account ----

func TestAccounts_Create(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	server := newOpenAIModelsServer(t, "sk-test-create-token-12345", []string{"gpt-4o-mini"})
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "CreateSite",
		"url":      server.URL,
		"platform": "openai",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-test-create-token-12345",
		"username":       "testuser",
		"credentialMode": "apikey",
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
	if created["tokenType"] != "apikey" {
		t.Errorf("expected tokenType='apikey', got %v", created["tokenType"])
	}
	if created["apiTokenFound"] != true {
		t.Errorf("expected apiTokenFound=true, got %v", created["apiTokenFound"])
	}
	if created["modelCount"] != float64(1) {
		t.Errorf("expected modelCount=1, got %v", created["modelCount"])
	}
}

func TestAccounts_Create_RejectsUnknownToken(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Force VerifyToken unknown: no usable session/user and no models.
		if r.URL.Path == "/api/user/self" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "nope"})
			return
		}
		if r.URL.Path == "/v1/models" {
			http.Error(w, `{"error":"bad token"}`, http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Reject Unknown",
		"url":      upstream.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	resp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":      siteID,
		"accessToken": "invalid-token",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != false {
		t.Fatalf("success = %v, want false", result["success"])
	}
	if result["requiresVerification"] != true {
		t.Fatalf("requiresVerification = %v, want true", result["requiresVerification"])
	}
	msg, _ := result["message"].(string)
	if !strings.Contains(msg, "Token 验证失败") {
		t.Fatalf("message = %q, want verify failure", msg)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE site_id = ?", siteID).Scan(&count); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("created %d accounts, want 0", count)
	}
}

func TestAccounts_Create_EnrichesUsernameFromUserInfo(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	server := newSessionVerifyServer(t, "session-token-xyz", "detected-user")
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Session Enrich",
		"url":      server.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	resp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":      siteID,
		"accessToken": "session-token-xyz",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create account: %d %s", resp.Code, resp.Body.String())
	}
	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	if created["tokenType"] != "session" {
		t.Fatalf("tokenType = %v, want session", created["tokenType"])
	}
	if created["usernameDetected"] != true {
		t.Fatalf("usernameDetected = %v, want true", created["usernameDetected"])
	}
	if created["username"] != "detected-user" {
		t.Fatalf("username = %v, want detected-user", created["username"])
	}

	var username string
	if err := db.QueryRow("SELECT username FROM accounts WHERE id = ?", int64(created["id"].(float64))).Scan(&username); err != nil {
		t.Fatalf("read username: %v", err)
	}
	if username != "detected-user" {
		t.Fatalf("stored username = %q, want detected-user", username)
	}
}

func TestAccounts_Create_APIKeySkipModelFetch(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Skip Model Fetch",
		"url":      server.URL,
		"platform": "openai",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	resp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-skip-verify",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create account: %d %s", resp.Code, resp.Body.String())
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
	}
	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	if created["tokenType"] != "apikey" {
		t.Fatalf("tokenType = %v, want apikey", created["tokenType"])
	}

	var accessToken string
	var apiToken *string
	if err := db.QueryRow("SELECT access_token, api_token FROM accounts WHERE id = ?", int64(created["id"].(float64))).Scan(&accessToken, &apiToken); err != nil {
		t.Fatalf("read tokens: %v", err)
	}
	if accessToken != "" {
		t.Fatalf("access_token = %q, want empty for apikey", accessToken)
	}
	if apiToken == nil || *apiToken != "sk-skip-verify" {
		t.Fatalf("api_token = %v, want sk-skip-verify", apiToken)
	}
}

func TestAccounts_Create_InvalidAccessTokenFailClosed(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	// NewAPI-style adapters treat HTTP failures as "unknown" rather than
	// propagating the body message; create must still fail closed.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"success":false,"message":"无权进行此操作，access token 无效"}`, http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Fail Closed",
		"url":      upstream.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	resp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":      siteID,
		"accessToken": "bad-session",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["requiresVerification"] != true {
		t.Fatalf("requiresVerification = %v, want true", result["requiresVerification"])
	}
	msg, _ := result["message"].(string)
	if !strings.Contains(msg, "Token 验证失败") && !strings.Contains(msg, "重新绑定账号") {
		t.Fatalf("message = %q, want verify failure or rebind hint", msg)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE site_id = ?", siteID).Scan(&count); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("created %d accounts, want 0", count)
	}
}

func TestAccounts_Create_AppendsRebindHintOnVerifyError(t *testing.T) {
	// Directly exercise the response path used when VerifyToken returns a
	// concrete invalid-access-token error (TS parity).
	msg := alert.AppendSessionTokenRebindHint("无权进行此操作，access token 无效")
	if !strings.Contains(msg, "重新绑定账号") {
		t.Fatalf("message = %q, want rebind hint", msg)
	}
}

func TestAccounts_Postgres_CreateUpdateManualModelsAndBatch(t *testing.T) {
	db, r, _ := setupAccountsPostgresTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	siteURL := "https://pg-accounts-" + suffix + ".example.com"
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", siteURL)
	})

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "PG Accounts " + suffix,
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

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "pg-account-token",
		"username":       "pg-user-" + suffix,
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("postgres create account: expected 200, got %d: %s", createResp.Code, createResp.Body.String())
	}
	var account map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &account); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	accountID := int64(account["id"].(float64))

	updateResp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"status": "disabled",
	})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("postgres update account: expected 200, got %d: %s", updateResp.Code, updateResp.Body.String())
	}

	modelResp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/models/manual", map[string]any{
		"models": []string{"gpt-4o", " gpt-4o ", "o3"},
	})
	if modelResp.Code != http.StatusOK {
		t.Fatalf("postgres manual models: expected 200, got %d: %s", modelResp.Code, modelResp.Body.String())
	}

	batchResp := doPostJSON(t, r, "/api/accounts/batch", map[string]any{
		"action": "enable",
		"ids":    []int64{accountID},
	})
	if batchResp.Code != http.StatusOK {
		t.Fatalf("postgres batch enable: expected 200, got %d: %s", batchResp.Code, batchResp.Body.String())
	}
}

func TestAccounts_Postgres_CreateAnyRouterAPIKeyWithoutUsernameReturnsID(t *testing.T) {
	db, r, _ := setupAccountsPostgresTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	siteURL := "https://pg-anyrouter-" + suffix + ".example.com"
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", siteURL)
	})

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "PG AnyRouter " + suffix,
		"url":      siteURL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("postgres create AnyRouter site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	if err := json.Unmarshal(siteResp.Body.Bytes(), &site); err != nil {
		t.Fatalf("unmarshal site: %v", err)
	}
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "pg-anyrouter-api-key-" + suffix,
		"credentialMode": "apikey",
		"checkinEnabled": true,
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("postgres create AnyRouter API-key account: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	accountID, ok := created["id"].(float64)
	if !ok || accountID <= 0 {
		t.Fatalf("create response missing positive id: %#v", created["id"])
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", int64(accountID)); err != nil {
		t.Fatalf("read created account: %v", err)
	}
	if account.Username != nil {
		t.Fatalf("username = %q, want NULL", *account.Username)
	}
	if account.CheckinEnabled {
		t.Fatal("API-key account should default checkin_enabled=false")
	}
	if account.APIToken == nil || *account.APIToken != "pg-anyrouter-api-key-"+suffix {
		t.Fatalf("api_token = %v, want mirrored accessToken", account.APIToken)
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
		"siteId":         siteID,
		"accessTokens":   []string{"batch-key-1", "batch-key-2", "batch-key-3"},
		"username":       "batchuser",
		"credentialMode": "apikey",
		"skipModelFetch": true,
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

func TestAccounts_Create_BatchAPIKeysTrimsAndFiltersBlankTokens(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "BatchTrimKeySite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":         siteID,
		"accessTokens":   []string{" batch-key-1 ", "   ", "\t", "batch-key-2"},
		"username":       "batchuser",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch create: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if n := result["totalCount"].(float64); n != 2 {
		t.Errorf("expected totalCount=2 after filtering blanks, got %v", n)
	}
	if n := result["createdCount"].(float64); n != 2 {
		t.Errorf("expected createdCount=2, got %v", n)
	}

	var tokens []string
	// API-key create stores the secret on api_token and clears access_token.
	if err := db.Select(&tokens, "SELECT api_token FROM accounts WHERE site_id = ? ORDER BY id", siteID); err != nil {
		t.Fatalf("read created tokens: %v", err)
	}
	if len(tokens) != 2 || tokens[0] != "batch-key-1" || tokens[1] != "batch-key-2" {
		t.Fatalf("tokens = %#v, want trimmed non-blank tokens", tokens)
	}
}

func TestAccounts_Create_BatchAPIKeysRejectsAllBlankTokens(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "BatchBlankKeySite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":       siteID,
		"accessTokens": []string{" ", "\t"},
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank batch tokens, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestAccounts_Create_WithCredentialMode(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "CModeSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":         siteID,
		"accessToken":    "anyrouter-api-token-test",
		"credentialMode": "apikey",
		"skipModelFetch": true,
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
	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", int64(created["id"].(float64))); err != nil {
		t.Fatalf("read created account: %v", err)
	}
	if account.CheckinEnabled {
		t.Fatal("API-key account should default checkin_enabled=false")
	}
	if account.APIToken == nil || *account.APIToken != "anyrouter-api-token-test" {
		t.Fatalf("API-key account api_token = %v, want accessToken value", account.APIToken)
	}
}

func TestAccounts_Create_APIKeyCannotEnableCheckin(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "CreateAPIKeyCheckin", "https://anyrouter.example.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":         siteID,
		"accessToken":    "anyrouter-api-token-create-checkin",
		"credentialMode": "apikey",
		"checkinEnabled": true,
		"skipModelFetch": true,
	}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create api-key account: %d %s", resp.Code, resp.Body.String())
	}
	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)

	var checkinEnabled bool
	if err := db.QueryRow("SELECT checkin_enabled FROM accounts WHERE id = ?", int64(created["id"].(float64))).Scan(&checkinEnabled); err != nil {
		t.Fatalf("read checkin_enabled: %v", err)
	}
	if checkinEnabled {
		t.Fatal("API-key account should force checkin_enabled=false even when requested true")
	}
}

func TestAccounts_Create_SiteNotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 99999, "accessToken": "missing-site-token"}
	resp := doPostJSON(t, r, "/api/accounts", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Login ----

func TestAccounts_Login(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	loginCalls := 0
	server := newAnyRouterLoginServer(t, &loginCalls, true)
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Login AnyRouter",
		"url":      server.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
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
	if loginCalls != 1 {
		t.Fatalf("loginCalls = %d, want 1", loginCalls)
	}
	var token string
	if err := db.QueryRow("SELECT access_token FROM accounts WHERE site_id = ? AND username = ?", siteID, "loginuser").Scan(&token); err != nil {
		t.Fatalf("read login account: %v", err)
	}
	if token != "anyrouter-session-token-from-upstream" {
		t.Fatalf("access_token = %q, want upstream token", token)
	}
}

func TestAccounts_Login_ReusedAccount(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	loginCalls := 0
	server := newAnyRouterLoginServer(t, &loginCalls, true)
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Reuse AnyRouter",
		"url":      server.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
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
	if loginCalls != 2 {
		t.Fatalf("loginCalls = %d, want 2", loginCalls)
	}
}

func TestAccounts_Login_InvalidCredentialsDoNotCreateAccount(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	loginCalls := 0
	server := newAnyRouterLoginServer(t, &loginCalls, false)
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Invalid Login AnyRouter",
		"url":      server.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	resp := doPostJSON(t, r, "/api/accounts/login", map[string]any{
		"siteId":   siteID,
		"username": "baduser",
		"password": "badpass",
	})
	if resp.Code == http.StatusOK {
		t.Fatalf("login failure must not return HTTP 200, body=%s", resp.Body.String())
	}
	if resp.Code < 400 {
		t.Fatalf("login failure response status = %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["error"] == nil || result["error"] == "" {
		t.Fatalf("expected error field, got %#v", result)
	}
	if loginCalls != 1 {
		t.Fatalf("loginCalls = %d, want 1", loginCalls)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM accounts WHERE site_id = ? AND username = ?", siteID, "baduser").Scan(&count); err != nil {
		t.Fatalf("count bad login accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("bad login created %d account rows, want 0", count)
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer anyrouter-valid-api-token" {
			http.Error(w, `{"error":"bad token"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"claude-sonnet-4"}]}`))
	}))
	t.Cleanup(upstream.Close)

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Verify AnyRouter",
		"url":      upstream.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"siteId":      siteID,
		"accessToken": "anyrouter-valid-api-token",
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
	if result["modelCount"] != float64(2) {
		t.Errorf("expected modelCount=2, got %v", result["modelCount"])
	}
}

func TestAccounts_VerifyToken_EmptyToken(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "EmptyTokSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{"siteId": siteID, "accessToken": ""}
	resp := doPostJSON(t, r, "/api/accounts/verify-token", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["error"] == nil || result["error"] == "" {
		t.Errorf("expected error field for empty token, got %#v", result)
	}
	if errMsg, _ := result["error"].(string); !strings.Contains(errMsg, "Token") {
		t.Fatalf("error = %v, want Token empty message", result["error"])
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

func TestAccounts_RebindSession_DBWriteFailureReturnsError(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "FailRebind", "https://api.openai.com")

	if _, err := db.Exec(`CREATE TRIGGER fail_rebind_update
		BEFORE UPDATE OF access_token ON accounts
		BEGIN
			SELECT RAISE(ABORT, 'forced rebind failure');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/rebind-session", map[string]any{
		"accessToken": "new-session-token",
	})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on failed rebind write, got %d: %s", resp.Code, resp.Body.String())
	}

	var accessToken string
	if err := db.QueryRow("SELECT access_token FROM accounts WHERE id = ?", accountID).Scan(&accessToken); err != nil {
		t.Fatalf("read access_token: %v", err)
	}
	if accessToken == "new-session-token" {
		t.Fatal("access_token changed despite failed rebind write")
	}
}

// ---- Update Account ----

func TestAccounts_List_ExpiredStatusNotHealthy(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "ExpiredHealthSite", "https://api.openai.com")

	// Stale healthy runtimeHealth should not win over expired status.
	extra := `{"credentialMode":"session","runtimeHealth":{"state":"healthy","reason":"余额刷新成功","source":"balance"}}`
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := db.Exec(
		"UPDATE accounts SET status = 'expired', extra_config = ?, updated_at = ? WHERE id = ?",
		extra, now, accountID,
	); err != nil {
		t.Fatalf("seed expired account: %v", err)
	}

	resp := doGet(t, r, "/api/accounts?refresh=1")
	if resp.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	accounts, _ := result["accounts"].([]any)
	if len(accounts) == 0 {
		t.Fatal("expected accounts in list")
	}
	var found map[string]any
	for _, raw := range accounts {
		acc, _ := raw.(map[string]any)
		if int64(acc["id"].(float64)) == accountID {
			found = acc
			break
		}
	}
	if found == nil {
		t.Fatalf("account %d not found in list", accountID)
	}
	if found["status"] != "expired" {
		t.Fatalf("status = %v, want expired", found["status"])
	}
	health, _ := found["runtimeHealth"].(map[string]any)
	if health == nil {
		t.Fatalf("runtimeHealth missing: %#v", found)
	}
	if health["state"] != "unhealthy" {
		t.Fatalf("runtimeHealth.state = %v, want unhealthy", health["state"])
	}
	if health["source"] != "auth" {
		t.Fatalf("runtimeHealth.source = %v, want auth", health["source"])
	}
	if reason, _ := health["reason"].(string); reason == "" || strings.Contains(reason, "余额刷新成功") {
		t.Fatalf("runtimeHealth.reason = %q, want expired auth reason", reason)
	}
}

func TestAccounts_Update_ExpiredStatusReturnsUnhealthyRuntimeHealth(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateExpiredHealth", "https://api.openai.com")

	// Leave a stale healthy runtimeHealth in extraConfig.
	extra := `{"credentialMode":"session","runtimeHealth":{"state":"healthy","reason":"余额刷新成功","source":"balance"}}`
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := db.Exec(
		"UPDATE accounts SET extra_config = ?, updated_at = ? WHERE id = ?",
		extra, now, accountID,
	); err != nil {
		t.Fatalf("seed healthy runtimeHealth: %v", err)
	}

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"status": "expired",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update expired: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if result["status"] != "expired" {
		t.Fatalf("status = %v, want expired", result["status"])
	}
	health, _ := result["runtimeHealth"].(map[string]any)
	if health == nil {
		t.Fatalf("runtimeHealth missing on detail response: %#v", result)
	}
	if health["state"] != "unhealthy" {
		t.Fatalf("runtimeHealth.state = %v, want unhealthy", health["state"])
	}
	if health["source"] != "auth" {
		t.Fatalf("runtimeHealth.source = %v, want auth", health["source"])
	}
}

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
	if result["username"] != "updatedname" {
		t.Errorf("expected username='updatedname', got %v", result["username"])
	}
	if result["status"] != "disabled" {
		t.Errorf("expected status='disabled', got %v", result["status"])
	}
}

func TestAccounts_UpdateRejectsInvalidStatus(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateInvalidStatus", "https://api.openai.com")

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"status": "paused",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}

	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", accountID).Scan(&status); err != nil {
		t.Fatalf("read account status: %v", err)
	}
	if status != "active" {
		t.Fatalf("status = %q, want unchanged active", status)
	}
}

func TestAccounts_UpdateRejectsNonPositiveUnitCost(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateInvalidUnitCost", "https://api.openai.com")

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"unitCost": -1,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}

	var unitCost *float64
	if err := db.QueryRow("SELECT unit_cost FROM accounts WHERE id = ?", accountID).Scan(&unitCost); err != nil {
		t.Fatalf("read unit_cost: %v", err)
	}
	if unitCost != nil {
		t.Fatalf("unit_cost = %v, want unchanged NULL", *unitCost)
	}
}

func TestAccounts_UpdateAcceptsPositiveUnitCost(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateValidUnitCost", "https://api.openai.com")

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"unitCost": 0.25,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}

	var unitCost *float64
	if err := db.QueryRow("SELECT unit_cost FROM accounts WHERE id = ?", accountID).Scan(&unitCost); err != nil {
		t.Fatalf("read unit_cost: %v", err)
	}
	if unitCost == nil || *unitCost != 0.25 {
		t.Fatalf("unit_cost = %v, want 0.25", unitCost)
	}
}

func TestAccounts_UpdateAPIKeyMirrorsAccessTokenWhenAPITokenUnchanged(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "UpdateAPIKeyMirror", "https://anyrouter.example.com")
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "old-anyrouter-api-key",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create apikey account: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created account: %v", err)
	}
	accountID := int64(created["id"].(float64))

	updateResp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"accessToken": "new-anyrouter-api-key",
		"apiToken":    "old-anyrouter-api-key",
	})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update apikey account: %d %s", updateResp.Code, updateResp.Body.String())
	}

	var accessToken string
	var apiToken *string
	if err := db.QueryRow("SELECT access_token, api_token FROM accounts WHERE id = ?", accountID).Scan(&accessToken, &apiToken); err != nil {
		t.Fatalf("read updated tokens: %v", err)
	}
	if accessToken != "new-anyrouter-api-key" {
		t.Fatalf("access_token = %q, want new token", accessToken)
	}
	if apiToken == nil || *apiToken != "new-anyrouter-api-key" {
		t.Fatalf("api_token = %v, want mirrored new token", apiToken)
	}
}

func TestAccounts_UpdateAPIKeyPreservesExplicitDifferentAPIToken(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "UpdateAPIKeyExplicit", "https://anyrouter.example.com")
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "old-anyrouter-api-key",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create apikey account: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created account: %v", err)
	}
	accountID := int64(created["id"].(float64))

	updateResp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"accessToken": "new-anyrouter-api-key",
		"apiToken":    "explicit-api-token",
	})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update apikey account: %d %s", updateResp.Code, updateResp.Body.String())
	}

	var accessToken string
	var apiToken *string
	if err := db.QueryRow("SELECT access_token, api_token FROM accounts WHERE id = ?", accountID).Scan(&accessToken, &apiToken); err != nil {
		t.Fatalf("read updated tokens: %v", err)
	}
	if accessToken != "new-anyrouter-api-key" {
		t.Fatalf("access_token = %q, want new token", accessToken)
	}
	if apiToken == nil || *apiToken != "explicit-api-token" {
		t.Fatalf("api_token = %v, want explicit token", apiToken)
	}
}

func TestAccounts_UpdateAPIKeyCannotEnableCheckin(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "UpdateAPIKeyCheckin", "https://anyrouter.example.com")
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "anyrouter-api-key-checkin",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create apikey account: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created account: %v", err)
	}
	accountID := int64(created["id"].(float64))

	updateResp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"checkinEnabled": true,
	})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update apikey account: %d %s", updateResp.Code, updateResp.Body.String())
	}

	var checkinEnabled bool
	if err := db.QueryRow("SELECT checkin_enabled FROM accounts WHERE id = ?", accountID).Scan(&checkinEnabled); err != nil {
		t.Fatalf("read checkin_enabled: %v", err)
	}
	if checkinEnabled {
		t.Fatal("API-key account should remain checkin_enabled=false")
	}
}

func TestAccounts_UpdateSessionToAPIKeyForcesCheckinDisabled(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateSessionToAPIKey", "https://anyrouter.example.com")

	var before bool
	if err := db.QueryRow("SELECT checkin_enabled FROM accounts WHERE id = ?", accountID).Scan(&before); err != nil {
		t.Fatalf("read initial checkin_enabled: %v", err)
	}
	if !before {
		t.Fatal("fixture should start with checkin_enabled=true")
	}

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"extraConfig": map[string]any{
			"credentialMode": "apikey",
		},
		"checkinEnabled": true,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update session account to apikey: %d %s", resp.Code, resp.Body.String())
	}

	var checkinEnabled bool
	var extraConfig string
	if err := db.QueryRow("SELECT checkin_enabled, extra_config FROM accounts WHERE id = ?", accountID).Scan(&checkinEnabled, &extraConfig); err != nil {
		t.Fatalf("read updated account: %v", err)
	}
	if checkinEnabled {
		t.Fatal("same-request session-to-apikey update should force checkin_enabled=false")
	}
	if !strings.Contains(extraConfig, `"credentialMode":"apikey"`) {
		t.Fatalf("extra_config = %q, want apikey credential mode", extraConfig)
	}
}

func TestAccounts_UpdateSessionToAPIKeyWithoutCheckinFieldDisablesExistingCheckin(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "UpdateSessionToAPIKeyImplicit", "https://anyrouter.example.com")

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"extraConfig": map[string]any{
			"credentialMode": "apikey",
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update session account to apikey: %d %s", resp.Code, resp.Body.String())
	}

	var checkinEnabled bool
	if err := db.QueryRow("SELECT checkin_enabled FROM accounts WHERE id = ?", accountID).Scan(&checkinEnabled); err != nil {
		t.Fatalf("read checkin_enabled: %v", err)
	}
	if checkinEnabled {
		t.Fatal("session-to-apikey update should clear existing checkin_enabled even without checkinEnabled payload")
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
	if _, err := db.Exec("UPDATE accounts SET status = 'disabled' WHERE id = ?", acc1); err != nil {
		t.Fatalf("disable fixture account: %v", err)
	}

	body := map[string]any{"ids": []int{int(acc1)}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch enable: %d %s", resp.Code, resp.Body.String())
	}
	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", acc1).Scan(&status); err != nil {
		t.Fatalf("read account status: %v", err)
	}
	if status != "active" {
		t.Fatalf("status = %q, want active", status)
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
	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", acc1).Scan(&status); err != nil {
		t.Fatalf("read account status: %v", err)
	}
	if status != "disabled" {
		t.Fatalf("status = %q, want disabled", status)
	}
}

func TestAccounts_BatchDisableInvalidatesRouteCache(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, acc1 := setupAccountFixtureWithSite(t, db, r, "BatchCacheSite", "https://api.openai.com")
	cache := routing.NewRouteCache(60_000)
	cache.SetRoutes([]store.TokenRoute{{ID: 1, ModelPattern: "gpt-*", RouteMode: "direct", RoutingStrategy: "weighted", Enabled: true}})
	routing.SetGlobalCache(cache)
	t.Cleanup(func() { routing.SetGlobalCache(nil) })
	if cached := cache.GetRoutes(); len(cached) != 1 {
		t.Fatalf("precondition cached routes = %d, want 1", len(cached))
	}

	body := map[string]any{"ids": []int{int(acc1)}, "action": "disable"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch disable: %d %s", resp.Code, resp.Body.String())
	}
	if cached := cache.GetRoutes(); cached != nil {
		t.Fatalf("route cache still populated after batch disable: %#v", cached)
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
	calls := 0
	server := newAnyRouterBalanceServer(t, &calls)
	_, acc1 := setupAnyRouterBalanceAccount(t, db, server.URL)

	body := map[string]any{"ids": []int{int(acc1)}, "action": "refreshBalance"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch refreshBalance: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result["successIds"].([]any)) != 1 {
		t.Fatalf("successIds = %#v, want one refreshed account", result["successIds"])
	}
	if len(result["failedItems"].([]any)) != 0 {
		t.Fatalf("failedItems = %#v, want empty", result["failedItems"])
	}
	if len(result["skippedItems"].([]any)) != 0 {
		t.Fatalf("skippedItems = %#v, want empty", result["skippedItems"])
	}
	if calls < 2 {
		t.Fatalf("upstream calls = %d, want discovery plus balance fetch", calls)
	}

	var balance, used, quota float64
	if err := db.QueryRow("SELECT balance, balance_used, quota FROM accounts WHERE id = ?", acc1).Scan(&balance, &used, &quota); err != nil {
		t.Fatalf("read balance fields: %v", err)
	}
	if balance != 2 || used != 0.5 || quota != 2.5 {
		t.Fatalf("balance fields = (%v,%v,%v), want (2,0.5,2.5)", balance, used, quota)
	}
}

func TestAccounts_BatchRefreshBalanceMissingIDFails(t *testing.T) {
	_, r, _ := setupAccountsTest(t)

	body := map[string]any{"ids": []int{99999}, "action": "refreshBalance"}
	resp := doPostJSON(t, r, "/api/accounts/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch refreshBalance missing account: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result["successIds"].([]any)) != 0 {
		t.Fatalf("successIds = %#v, want empty", result["successIds"])
	}
	if len(result["failedItems"].([]any)) != 1 {
		t.Fatalf("failedItems = %#v, want one missing account", result["failedItems"])
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
	db, r, _ := setupAccountsTest(t)
	calls := 0
	server := newAnyRouterBalanceServer(t, &calls)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	siteID := insertAnyRouterBalanceSite(t, db, server.URL, now)

	// Session-capable account (balance probe path).
	accRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, ?, ?, 'active', 1, ?, ?)",
		siteID, "anyrouter-user", "session-token", now, now,
	)
	if err != nil {
		t.Fatalf("insert session account: %v", err)
	}
	accountID, err := accRes.LastInsertId()
	if err != nil {
		t.Fatalf("session account LastInsertId: %v", err)
	}

	// proxy-only apikey on same site should be skipped honestly
	apiKeyRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', 0, ?, ?, ?)",
		siteID, nil, "anyrouter-api-key-token", "anyrouter-api-key-token", `{"credentialMode":"apikey"}`, now, now,
	)
	if err != nil {
		t.Fatalf("insert apikey account: %v", err)
	}
	apiKeyAccountID, err := apiKeyRes.LastInsertId()
	if err != nil {
		t.Fatalf("apikey account LastInsertId: %v", err)
	}

	body := map[string]any{"wait": true}
	resp := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("health refresh sync: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["success"] != true {
		t.Error("expected success=true")
	}
	summary, _ := result["summary"].(map[string]any)
	if summary == nil {
		t.Fatalf("summary missing: %#v", result)
	}
	if summary["total"] != float64(2) {
		t.Fatalf("summary.total = %v, want 2", summary["total"])
	}
	if summary["success"] != float64(1) {
		t.Fatalf("summary.success = %v, want 1", summary["success"])
	}
	if summary["skipped"] != float64(1) {
		t.Fatalf("summary.skipped = %v, want 1", summary["skipped"])
	}
	if summary["healthy"] != float64(1) {
		t.Fatalf("summary.healthy = %v, want 1", summary["healthy"])
	}
	if calls < 2 {
		t.Fatalf("upstream calls = %d, want discovery plus balance fetch", calls)
	}

	results, _ := result["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	byID := map[int64]map[string]any{}
	for _, raw := range results {
		item, _ := raw.(map[string]any)
		byID[int64(item["accountId"].(float64))] = item
	}
	sessionItem := byID[accountID]
	if sessionItem == nil || sessionItem["status"] != "success" {
		t.Fatalf("session item = %#v, want success", sessionItem)
	}
	if sessionItem["state"] != "healthy" {
		t.Fatalf("session state = %v, want healthy", sessionItem["state"])
	}
	apiKeyItem := byID[apiKeyAccountID]
	if apiKeyItem == nil || apiKeyItem["status"] != "skipped" {
		t.Fatalf("apikey item = %#v, want skipped", apiKeyItem)
	}
	if apiKeyItem["reason"] != "proxy_only" {
		t.Fatalf("apikey reason = %v, want proxy_only", apiKeyItem["reason"])
	}

	// Balance + runtimeHealth should be persisted for the session account.
	var balance float64
	var extraConfig *string
	if err := db.QueryRow("SELECT balance, extra_config FROM accounts WHERE id = ?", accountID).Scan(&balance, &extraConfig); err != nil {
		t.Fatalf("read refreshed account: %v", err)
	}
	if balance != 2 {
		t.Fatalf("balance = %v, want 2", balance)
	}
	if extraConfig == nil || !strings.Contains(*extraConfig, `"state":"healthy"`) {
		t.Fatalf("extra_config = %v, want healthy runtimeHealth", extraConfig)
	}
}

func TestAccounts_HealthRefresh_SingleAccountID(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	calls := 0
	server := newAnyRouterBalanceServer(t, &calls)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	siteID := insertAnyRouterBalanceSite(t, db, server.URL, now)

	accRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, ?, ?, 'active', 1, ?, ?)",
		siteID, "anyrouter-user", "session-token", now, now,
	)
	if err != nil {
		t.Fatalf("insert session account: %v", err)
	}
	accountID, err := accRes.LastInsertId()
	if err != nil {
		t.Fatalf("session account LastInsertId: %v", err)
	}
	// Second account on same site is intentionally not targeted by accountId.
	if _, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', 0, ?, ?, ?)",
		siteID, nil, "anyrouter-api-key-token", "anyrouter-api-key-token", `{"credentialMode":"apikey"}`, now, now,
	); err != nil {
		t.Fatalf("insert other apikey account: %v", err)
	}

	body := map[string]any{"wait": true, "accountId": accountID}
	resp := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("health refresh single: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	summary, _ := result["summary"].(map[string]any)
	if summary["total"] != float64(1) {
		t.Fatalf("summary.total = %v, want 1", summary["total"])
	}
	if summary["success"] != float64(1) {
		t.Fatalf("summary.success = %v, want 1", summary["success"])
	}
	results, _ := result["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	item, _ := results[0].(map[string]any)
	if int64(item["accountId"].(float64)) != accountID {
		t.Fatalf("accountId = %v, want %d", item["accountId"], accountID)
	}
	if item["status"] != "success" {
		t.Fatalf("status = %v, want success", item["status"])
	}
	if calls < 2 {
		t.Fatalf("upstream calls = %d, want discovery plus balance fetch", calls)
	}
}

func TestAccounts_HealthRefresh_SingleAccountNotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doPostJSON(t, r, "/api/accounts/health/refresh", map[string]any{
		"wait":      true,
		"accountId": 99999,
	})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAccounts_HealthRefresh_Background(t *testing.T) {
	resetBackgroundTasksForTests()
	t.Cleanup(resetBackgroundTasksForTests)

	db, r, _ := setupAccountsTest(t)
	calls := 0
	server := newAnyRouterBalanceServer(t, &calls)
	_, accountID := setupAnyRouterBalanceAccount(t, db, server.URL)

	body := map[string]any{"wait": false, "accountId": accountID}
	resp := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("health refresh bg: expected 202, got %d body=%s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["queued"] != true {
		t.Error("expected queued=true")
	}
	jobID, _ := result["jobId"].(string)
	if jobID == "" || jobID == "stub-health-refresh" {
		t.Fatalf("jobId = %q, want real in-process task id", jobID)
	}
	if result["taskId"] != jobID {
		t.Fatalf("taskId = %v, want same as jobId %q", result["taskId"], jobID)
	}

	// Wait for background runner to finish and persist runtime health.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task := getBackgroundTask(jobID)
		if task != nil && (task.Status == BackgroundTaskSucceeded || task.Status == BackgroundTaskFailed) {
			if task.Status != BackgroundTaskSucceeded {
				t.Fatalf("background task status = %s error=%v", task.Status, task.Error)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	task := getBackgroundTask(jobID)
	if task == nil || task.Status != BackgroundTaskSucceeded {
		t.Fatalf("background task did not succeed: %#v", task)
	}

	var balance float64
	var extraConfig *string
	if err := db.QueryRow("SELECT balance, extra_config FROM accounts WHERE id = ?", accountID).Scan(&balance, &extraConfig); err != nil {
		t.Fatalf("read refreshed account: %v", err)
	}
	if balance != 2 {
		t.Fatalf("balance = %v, want 2 after async health refresh", balance)
	}
	if extraConfig == nil || !strings.Contains(*extraConfig, `"state":"healthy"`) {
		t.Fatalf("extra_config = %v, want healthy runtimeHealth after async refresh", extraConfig)
	}
	if calls < 2 {
		t.Fatalf("upstream calls = %d, want discovery plus balance fetch", calls)
	}
}

func TestAccounts_HealthRefresh_BackgroundDedupe(t *testing.T) {
	resetBackgroundTasksForTests()
	t.Cleanup(resetBackgroundTasksForTests)

	db, r, _ := setupAccountsTest(t)
	// Slow-ish upstream is unnecessary; empty batch still exercises registry.
	// Use a session account so the runner has real work if it runs twice.
	calls := 0
	server := newAnyRouterBalanceServer(t, &calls)
	_, accountID := setupAnyRouterBalanceAccount(t, db, server.URL)

	body := map[string]any{"wait": false, "accountId": accountID}
	first := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first bg refresh: %d %s", first.Code, first.Body.String())
	}
	var firstResult map[string]any
	json.Unmarshal(first.Body.Bytes(), &firstResult)
	jobID, _ := firstResult["jobId"].(string)
	if jobID == "" {
		t.Fatal("first jobId empty")
	}

	second := doPostJSON(t, r, "/api/accounts/health/refresh", body)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second bg refresh: %d %s", second.Code, second.Body.String())
	}
	var secondResult map[string]any
	json.Unmarshal(second.Body.Bytes(), &secondResult)
	if secondResult["reused"] != true {
		t.Fatalf("reused = %v, want true for concurrent dedupe", secondResult["reused"])
	}
	if secondResult["jobId"] != jobID {
		t.Fatalf("second jobId = %v, want %q", secondResult["jobId"], jobID)
	}
}

// ---- Refresh Balance ----

func TestAccounts_RefreshBalance(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	calls := 0
	server := newAnyRouterBalanceServer(t, &calls)
	_, accountID := setupAnyRouterBalanceAccount(t, db, server.URL)

	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/balance", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("refresh balance: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["balance"] != float64(2) || result["balanceUsed"] != float64(0.5) || result["quota"] != float64(2.5) {
		t.Fatalf("response balance fields = %#v", result)
	}
	if result["skipped"] != false {
		t.Fatalf("skipped = %v, want false", result["skipped"])
	}
	if calls < 2 {
		t.Fatalf("upstream calls = %d, want discovery plus balance fetch", calls)
	}

	var balance, used, quota float64
	if err := db.QueryRow("SELECT balance, balance_used, quota FROM accounts WHERE id = ?", accountID).Scan(&balance, &used, &quota); err != nil {
		t.Fatalf("read balance fields: %v", err)
	}
	if balance != 2 || used != 0.5 || quota != 2.5 {
		t.Fatalf("balance fields = (%v,%v,%v), want (2,0.5,2.5)", balance, used, quota)
	}
	var lastBalanceRefresh *string
	if err := db.QueryRow("SELECT last_balance_refresh FROM accounts WHERE id = ?", accountID).Scan(&lastBalanceRefresh); err != nil {
		t.Fatalf("read last_balance_refresh: %v", err)
	}
	if lastBalanceRefresh == nil || *lastBalanceRefresh == "" {
		t.Fatal("last_balance_refresh was not recorded")
	}
}

func TestAccounts_RefreshBalance_NotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	resp := doPostJSON(t, r, "/api/accounts/99999/balance", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestAccounts_RefreshBalanceAPIKeySkippedWithoutUpstream(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	_, accountID := setupAnyRouterAPIKeyAccount(t, db, server.URL)

	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/balance", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("refresh api-key balance: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["skipped"] != true || result["reason"] != "proxy_only" {
		t.Fatalf("result = %#v, want proxy_only skip", result)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
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

func TestAccounts_ManualModels_DBWriteFailureRollsBack(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "FailManualModels", "https://api.openai.com")

	if _, err := db.Exec(`CREATE TRIGGER fail_manual_model_insert
		BEFORE INSERT ON model_availability
		BEGIN
			SELECT RAISE(ABORT, 'forced manual model failure');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/models/manual", map[string]any{
		"models": []string{"gpt-4o"},
	})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on failed manual model write, got %d: %s", resp.Code, resp.Body.String())
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM model_availability WHERE account_id = ?", accountID).Scan(&count); err != nil {
		t.Fatalf("count model_availability: %v", err)
	}
	if count != 0 {
		t.Fatalf("model rows = %d, want rollback to leave none", count)
	}
}

// ---- Login Integration Tests ----

func TestAccounts_Login_SiteNotFound(t *testing.T) {
	_, r, _ := setupAccountsTest(t)
	body := map[string]any{"siteId": 99999, "username": "u", "password": "p"}
	resp := doPostJSON(t, r, "/api/accounts/login", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	errField, _ := result["error"].(string)
	msgField, _ := result["message"].(string)
	if errField == "" && msgField == "" {
		t.Errorf("expected error field for missing site, got %#v", result)
	}
	combined := errField + msgField
	if !strings.Contains(combined, "site not found") {
		t.Errorf("expected 'site not found', got %#v", result)
	}
}

func TestAccounts_Login_EncryptsPassword(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	loginCalls := 0
	server := newAnyRouterLoginServer(t, &loginCalls, true)
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Encrypt AnyRouter",
		"url":      server.URL,
		"platform": "anyrouter",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	if err := json.Unmarshal(siteResp.Body.Bytes(), &site); err != nil {
		t.Fatalf("decode site: %v", err)
	}
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
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if result["success"] != true {
		t.Fatalf("login success = %v body=%s", result["success"], resp.Body.String())
	}
	if loginCalls != 1 {
		t.Fatalf("loginCalls = %d, want 1", loginCalls)
	}

	// Fetch the account and verify extraConfig has autoRelogin with passwordCipher
	var extraConfig *string
	if err := db.QueryRow("SELECT extra_config FROM accounts WHERE username = 'encuser' AND site_id = ?", siteID).Scan(&extraConfig); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	if extraConfig == nil || !strings.Contains(*extraConfig, "passwordCipher") {
		t.Fatalf("expected passwordCipher in extraConfig, got %v", extraConfig)
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
	body := map[string]any{"siteId": 99999, "accessToken": "missing-site-token"}
	resp := doPostJSON(t, r, "/api/accounts/verify-token", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	errMsg, _ := result["error"].(string)
	if !strings.Contains(errMsg, "site not found") {
		t.Errorf("expected 'site not found', got %#v", result)
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

func TestAccounts_Update_ExtraConfigAndProxyURLMergeTogether(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "ProxyMergeSite", "https://api.openai.com")

	body := map[string]any{
		"extraConfig": map[string]any{
			"useSystemProxy": true,
			"platformUserId": float64(42),
		},
		"proxyUrl": "http://my-proxy:8080",
	}
	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update extraConfig+proxyUrl: %d %s", resp.Code, resp.Body.String())
	}

	var extraConfig *string
	if err := db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extraConfig); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	if extraConfig == nil {
		t.Fatal("extra_config is nil")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*extraConfig), &cfg); err != nil {
		t.Fatalf("decode extra_config: %v", err)
	}
	if cfg["proxyUrl"] != "http://my-proxy:8080" {
		t.Fatalf("proxyUrl = %#v, want merged proxyUrl", cfg["proxyUrl"])
	}
	if cfg["useSystemProxy"] != true {
		t.Fatalf("useSystemProxy = %#v, want true", cfg["useSystemProxy"])
	}
	if cfg["platformUserId"] != float64(42) {
		t.Fatalf("platformUserId = %#v, want 42", cfg["platformUserId"])
	}
}
