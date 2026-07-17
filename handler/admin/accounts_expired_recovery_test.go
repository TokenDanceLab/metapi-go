package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

func withAccountModelRefresher(
	t *testing.T,
	fn func(ctx context.Context, db *sqlx.DB, accountID int64, allowInactive bool) map[string]any,
) {
	t.Helper()
	prev := accountModelRefresher
	accountModelRefresher = fn
	t.Cleanup(func() { accountModelRefresher = prev })
}

func TestShouldRecoverExpiredAPIKey(t *testing.T) {
	api := "sk-new"
	old := "sk-old"
	prev := store.Account{
		Status:      "expired",
		AccessToken: old,
		APIToken:    &old,
		ExtraConfig: strPtr(`{"credentialMode":"apikey","runtimeHealth":{"state":"unhealthy","source":"auth","reason":"连接已过期，请更新 API Key"}}`),
	}
	next := prev
	next.AccessToken = api
	next.APIToken = &api

	if !shouldRecoverExpiredAPIKey(prev, next, map[string]any{"accessToken": api, "apiToken": api}) {
		t.Fatal("expected recovery when expired apikey credentials change")
	}
	if shouldRecoverExpiredAPIKey(prev, next, map[string]any{"accessToken": api, "apiToken": api, "status": "disabled"}) {
		t.Fatal("disabled status must not recover")
	}
	if shouldRecoverExpiredAPIKey(prev, next, map[string]any{"username": "x"}) {
		t.Fatal("non-credential update must not recover")
	}
	active := prev
	active.Status = "active"
	if shouldRecoverExpiredAPIKey(active, next, map[string]any{"accessToken": api}) {
		t.Fatal("non-expired account must not recover")
	}
	sessionPrev := prev
	sessionNext := next
	sessionNext.ExtraConfig = strPtr(`{"credentialMode":"session"}`)
	sessionNext.AccessToken = "sess"
	if shouldRecoverExpiredAPIKey(sessionPrev, sessionNext, map[string]any{"accessToken": "sess"}) {
		t.Fatal("session mode must not use apikey recovery path")
	}
}

func TestAccounts_Update_ExpiredAPIKeyRecovery_Success(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	server := newOpenAIModelsServer(t, "sk-recovered", []string{"gpt-4o-mini", "gpt-4o"})
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "ExpiredRecoveryOK",
		"url":      server.URL,
		"platform": "openai",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	_ = json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-old-expired",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	accountID := int64(created["id"].(float64))

	extra := `{"credentialMode":"apikey","runtimeHealth":{"state":"unhealthy","reason":"连接已过期，请更新 API Key","source":"auth"}}`
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		"UPDATE accounts SET status = 'expired', extra_config = ?, updated_at = ? WHERE id = ?",
		extra, now, accountID,
	); err != nil {
		t.Fatalf("seed expired: %v", err)
	}

	// Prefer real adapter path (httptest OpenAI models) — no fake needed for success.
	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"accessToken": "sk-recovered",
		// mirrored apikey path: same value as current stored api token triggers mirror,
		// so omit apiToken to mirror new access token into apiToken.
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if result["status"] != "active" {
		t.Fatalf("status = %v, want active; body=%s", result["status"], resp.Body.String())
	}
	mr, _ := result["modelRefresh"].(map[string]any)
	if mr == nil || mr["success"] != true {
		t.Fatalf("modelRefresh success missing: %#v", result["modelRefresh"])
	}

	var status string
	var extraConfig *string
	if err := db.QueryRow("SELECT status, extra_config FROM accounts WHERE id = ?", accountID).Scan(&status, &extraConfig); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if status != "active" {
		t.Fatalf("db status = %q, want active", status)
	}
	if extraConfig != nil && strings.Contains(*extraConfig, `"source":"auth"`) {
		t.Fatalf("auth runtimeHealth should be cleared, extra_config=%s", *extraConfig)
	}

	var modelCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM model_availability WHERE account_id = ? AND available = 1",
		accountID,
	).Scan(&modelCount); err != nil {
		t.Fatalf("count models: %v", err)
	}
	if modelCount < 2 {
		t.Fatalf("available models = %d, want >= 2", modelCount)
	}
}

func TestAccounts_Update_ExpiredAPIKeyRecovery_FailureKeepsExpired(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	// Models endpoint always 401 for any token.
	server := newOpenAIModelsServer(t, "never-matches", []string{"gpt-4o"})
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "ExpiredRecoveryFail",
		"url":      server.URL,
		"platform": "openai",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	_ = json.Unmarshal(siteResp.Body.Bytes(), &site)
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-old-expired",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	accountID := int64(created["id"].(float64))

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		"UPDATE accounts SET status = 'expired', extra_config = ?, updated_at = ? WHERE id = ?",
		`{"credentialMode":"apikey"}`, now, accountID,
	); err != nil {
		t.Fatalf("seed expired: %v", err)
	}

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"accessToken": "sk-still-bad",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != "expired" {
		t.Fatalf("status = %v, want expired", result["status"])
	}
	msg, _ := result["message"].(string)
	if !strings.Contains(msg, "expired") && !strings.Contains(msg, "模型") {
		t.Fatalf("expected honest failure message, got %q", msg)
	}
	mr, _ := result["modelRefresh"].(map[string]any)
	if mr == nil || mr["success"] == true {
		t.Fatalf("expected failed modelRefresh, got %#v", result["modelRefresh"])
	}

	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", accountID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "expired" {
		t.Fatalf("db status = %q, want expired", status)
	}
}

func TestAccounts_Update_ExpiredAPIKeyRecovery_DisabledSkips(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "ExpiredRecoveryDisabled", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-old",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	accountID := int64(created["id"].(float64))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		"UPDATE accounts SET status = 'expired', extra_config = ?, updated_at = ? WHERE id = ?",
		`{"credentialMode":"apikey"}`, now, accountID,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	called := false
	withAccountModelRefresher(t, func(ctx context.Context, db *sqlx.DB, accountID int64, allowInactive bool) map[string]any {
		called = true
		return map[string]any{"success": true}
	})

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"accessToken": "sk-new",
		"status":      "disabled",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("model refresh must not run when status forced disabled")
	}
	var result map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &result)
	if result["status"] != "disabled" {
		t.Fatalf("status = %v, want disabled", result["status"])
	}
	if _, ok := result["modelRefresh"]; ok {
		t.Fatalf("modelRefresh should be absent, got %#v", result["modelRefresh"])
	}
}

func TestAccounts_Update_ExpiredAPIKeyRecovery_NoCredentialChangeSkips(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "ExpiredRecoveryNoCred", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-same",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	accountID := int64(created["id"].(float64))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		"UPDATE accounts SET status = 'expired', extra_config = ?, updated_at = ? WHERE id = ?",
		`{"credentialMode":"apikey"}`, now, accountID,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	called := false
	withAccountModelRefresher(t, func(ctx context.Context, db *sqlx.DB, accountID int64, allowInactive bool) map[string]any {
		called = true
		return map[string]any{"success": true}
	})

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"username": "only-name",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("model refresh must not run without credential change")
	}
	var status string
	if err := db.QueryRow("SELECT status FROM accounts WHERE id = ?", accountID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "expired" {
		t.Fatalf("status = %q, want expired", status)
	}
}

func TestAccounts_Update_ExpiredAPIKeyRecovery_InjectableRefresh(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	site := newSiteFixture(t, r, "ExpiredRecoveryInject", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	createResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":         siteID,
		"accessToken":    "sk-old",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createResp.Body.Bytes(), &created)
	accountID := int64(created["id"].(float64))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		"UPDATE accounts SET status = 'expired', extra_config = ?, updated_at = ? WHERE id = ?",
		`{"credentialMode":"apikey","runtimeHealth":{"state":"unhealthy","source":"auth","reason":"连接已过期，请更新 API Key"}}`,
		now, accountID,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var sawAllowInactive bool
	withAccountModelRefresher(t, func(ctx context.Context, dbx *sqlx.DB, id int64, allowInactive bool) map[string]any {
		if id != accountID {
			t.Fatalf("accountID = %d, want %d", id, accountID)
		}
		sawAllowInactive = allowInactive
		// Simulate successful model write without upstream.
		_ = persistAccountModelAvailability(dbx, id, []string{"inject-model"}, time.Now().UTC().Format(time.RFC3339))
		return map[string]any{
			"success": true,
			"refresh": map[string]any{"id": id, "status": "success", "modelCount": 1, "models": []string{"inject-model"}},
			"rebuild": map[string]any{"success": true},
		}
	})

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"accessToken": "sk-injected",
		"status":      "active", // deferred until refresh succeeds
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	if !sawAllowInactive {
		t.Fatal("expected allowInactive=true on recovery refresh")
	}
	var result map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &result)
	if result["status"] != "active" {
		t.Fatalf("status = %v, want active", result["status"])
	}

	// Ensure auth health cleared via service helper path.
	var extra *string
	_ = db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extra)
	stored := service.ExtractRuntimeHealth(extra)
	if stored != nil && strings.EqualFold(string(stored.Source), string(service.HealthSourceAuth)) {
		t.Fatalf("auth runtime health still present: %#v", stored)
	}
}

func strPtr(s string) *string { return &s }
