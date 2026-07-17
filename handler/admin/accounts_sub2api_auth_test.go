package admin

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/store"
)

// ---- Sub2API managed auth merge (#194) ----

func setupSub2ApiAccountFixture(t *testing.T, db *store.DB, r chi.Router, extraConfig string) (int64, int64) {
	t.Helper()
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Sub2API Auth Site",
		"url":      "https://sub2api.example.com",
		"platform": "sub2api",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create sub2api site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	if err := json.Unmarshal(siteResp.Body.Bytes(), &site); err != nil {
		t.Fatalf("decode site: %v", err)
	}
	siteID := int64(site["id"].(float64))
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	res, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, ?, ?, 'active', 1, ?, ?, ?)",
		siteID, "sub2api-user", "session-old", extraConfig, now, now,
	)
	if err != nil {
		t.Fatalf("insert sub2api account: %v", err)
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}
	return siteID, accountID
}

func parseExtraConfigMap(t *testing.T, raw *string) map[string]any {
	t.Helper()
	if raw == nil {
		t.Fatal("extra_config is nil")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*raw), &cfg); err != nil {
		t.Fatalf("decode extra_config: %v body=%s", err, *raw)
	}
	return cfg
}

func TestAccounts_Update_Sub2ApiAuthMergePreserveAndOverwrite(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	seed := `{"credentialMode":"session","proxyUrl":"http://proxy.local","sub2apiAuth":{"refreshToken":"rt_old","tokenExpiresAt":100,"custom":"keep-me"}}`
	_, accountID := setupSub2ApiAccountFixture(t, db, r, seed)

	// Partial top-level refreshToken update should preserve tokenExpiresAt + custom + proxyUrl.
	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"refreshToken": "rt_new",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update refreshToken: %d %s", resp.Code, resp.Body.String())
	}

	var extra *string
	if err := db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extra); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	cfg := parseExtraConfigMap(t, extra)
	if cfg["proxyUrl"] != "http://proxy.local" {
		t.Fatalf("proxyUrl = %#v, want preserved", cfg["proxyUrl"])
	}
	auth, _ := cfg["sub2apiAuth"].(map[string]any)
	if auth == nil {
		t.Fatalf("sub2apiAuth missing: %#v", cfg)
	}
	if auth["refreshToken"] != "rt_new" {
		t.Fatalf("refreshToken = %#v, want rt_new", auth["refreshToken"])
	}
	if auth["tokenExpiresAt"] != float64(100) {
		t.Fatalf("tokenExpiresAt = %#v, want preserved 100", auth["tokenExpiresAt"])
	}
	if auth["custom"] != "keep-me" {
		t.Fatalf("custom = %#v, want keep-me", auth["custom"])
	}

	// Nested partial tokenExpiresAt patch should preserve refreshToken.
	resp = doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"extraConfig": map[string]any{
			"sub2apiAuth": map[string]any{
				"tokenExpiresAt": float64(999),
			},
			"note": "still-here",
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update nested expires: %d %s", resp.Code, resp.Body.String())
	}
	if err := db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extra); err != nil {
		t.Fatalf("read extra_config after nested: %v", err)
	}
	cfg = parseExtraConfigMap(t, extra)
	if cfg["note"] != "still-here" {
		t.Fatalf("note = %#v, want still-here", cfg["note"])
	}
	if cfg["proxyUrl"] != "http://proxy.local" {
		t.Fatalf("proxyUrl lost after nested update: %#v", cfg["proxyUrl"])
	}
	auth, _ = cfg["sub2apiAuth"].(map[string]any)
	if auth["refreshToken"] != "rt_new" {
		t.Fatalf("refreshToken = %#v, want preserved rt_new", auth["refreshToken"])
	}
	if auth["tokenExpiresAt"] != float64(999) {
		t.Fatalf("tokenExpiresAt = %#v, want 999", auth["tokenExpiresAt"])
	}
	if auth["custom"] != "keep-me" {
		t.Fatalf("custom = %#v, want keep-me", auth["custom"])
	}
}

func TestAccounts_Update_Sub2ApiAuthTopLevelWinsOverNested(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	seed := `{"credentialMode":"session","sub2apiAuth":{"refreshToken":"rt_old","tokenExpiresAt":100}}`
	_, accountID := setupSub2ApiAccountFixture(t, db, r, seed)

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"refreshToken":   "rt_top",
		"tokenExpiresAt": int64(777),
		"extraConfig": map[string]any{
			"sub2apiAuth": map[string]any{
				"refreshToken":   "rt_nested",
				"tokenExpiresAt": float64(555),
			},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	var extra *string
	if err := db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extra); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	cfg := parseExtraConfigMap(t, extra)
	auth, _ := cfg["sub2apiAuth"].(map[string]any)
	if auth["refreshToken"] != "rt_top" {
		t.Fatalf("refreshToken = %#v, want top-level rt_top", auth["refreshToken"])
	}
	if auth["tokenExpiresAt"] != float64(777) {
		t.Fatalf("tokenExpiresAt = %#v, want top-level 777", auth["tokenExpiresAt"])
	}
}

func TestAccounts_RebindSession_Sub2ApiAuthMerge(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	seed := `{"credentialMode":"session","proxyUrl":"http://proxy.local","sub2apiAuth":{"refreshToken":"rt_old","tokenExpiresAt":100,"custom":"keep-me"}}`
	_, accountID := setupSub2ApiAccountFixture(t, db, r, seed)

	resp := doPostJSON(t, r, "/api/accounts/"+itoa(accountID)+"/rebind-session", map[string]any{
		"accessToken":    "session-new",
		"refreshToken":   "rt_rebind",
		"tokenExpiresAt": int64(12345),
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("rebind: %d %s", resp.Code, resp.Body.String())
	}

	var accessToken string
	var extra *string
	if err := db.QueryRow("SELECT access_token, extra_config FROM accounts WHERE id = ?", accountID).Scan(&accessToken, &extra); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if accessToken != "session-new" {
		t.Fatalf("access_token = %q, want session-new", accessToken)
	}
	cfg := parseExtraConfigMap(t, extra)
	if cfg["proxyUrl"] != "http://proxy.local" {
		t.Fatalf("proxyUrl = %#v, want preserved", cfg["proxyUrl"])
	}
	if cfg["credentialMode"] != "session" {
		t.Fatalf("credentialMode = %#v, want session", cfg["credentialMode"])
	}
	auth, _ := cfg["sub2apiAuth"].(map[string]any)
	if auth["refreshToken"] != "rt_rebind" {
		t.Fatalf("refreshToken = %#v, want rt_rebind", auth["refreshToken"])
	}
	if auth["tokenExpiresAt"] != float64(12345) {
		t.Fatalf("tokenExpiresAt = %#v, want 12345", auth["tokenExpiresAt"])
	}
	if auth["custom"] != "keep-me" {
		t.Fatalf("custom = %#v, want keep-me", auth["custom"])
	}
}

func TestAccounts_Update_NonSub2ApiIgnoresManagedAuthFields(t *testing.T) {
	db, r, _ := setupAccountsTest(t)
	_, accountID := setupAccountFixtureWithSite(t, db, r, "NonSub2", "https://api.openai.com")
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := db.Exec(
		"UPDATE accounts SET extra_config = ?, updated_at = ? WHERE id = ?",
		`{"credentialMode":"session","proxyUrl":"http://proxy.local"}`, now, accountID,
	); err != nil {
		t.Fatalf("seed extra_config: %v", err)
	}

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"refreshToken":   "rt_should_ignore",
		"tokenExpiresAt": int64(999),
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update non-sub2api: %d %s", resp.Code, resp.Body.String())
	}
	var extra *string
	if err := db.QueryRow("SELECT extra_config FROM accounts WHERE id = ?", accountID).Scan(&extra); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	cfg := parseExtraConfigMap(t, extra)
	if _, ok := cfg["sub2apiAuth"]; ok {
		t.Fatalf("non-sub2api update should not write sub2apiAuth: %#v", cfg)
	}
	if cfg["proxyUrl"] != "http://proxy.local" {
		t.Fatalf("proxyUrl = %#v, want preserved", cfg["proxyUrl"])
	}
}
