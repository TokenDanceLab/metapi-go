package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service/oauth"
	"github.com/tokendancelab/metapi-go/store"
)

func setupOAuthRoutesTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		store.OverrideDB(nil)
		db.Close()
	})
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store.OverrideDB(db)

	// Isolate session store per test so state cannot leak across cases.
	sessionStore := oauth.NewMemoryOAuthSessionStore()
	oauth.SetSessionStore(sessionStore)
	t.Cleanup(func() {
		oauth.SetSessionStore(oauth.NewMemoryOAuthSessionStore())
	})

	// Clear loopback callback failure state for codex so StartFlow succeeds.
	// StartFlow only fails when Attempted && !Ready; absence is fine.
	config.Set(&config.Config{
		SystemProxyUrl: "",
		// Provider client IDs are required by BuildAuthorizationURL for some providers.
		// Codex/Claude may panic if missing; tests use codex which needs CODEX_CLIENT_ID.
		// Provide a non-empty placeholder so URL construction can proceed.
		CodexClientId:  "test-codex-client",
		ClaudeClientId: "test-claude-client",
	})

	r := chi.NewRouter()
	RegisterOauthRoutes(r, db.DB)
	return db, r
}

func insertOAuthAccount(t *testing.T, db *store.DB, provider string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, use_system_proxy, is_pinned, global_weight, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', 0, 0, 1, 0, ?, ?)`,
		"Codex OAuth Test", "https://chatgpt.com/backend-api", provider, now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("site id: %v", err)
	}

	extra := `{"oauth":{"provider":"` + provider + `","accountId":"acc-1","accountKey":"acc-1","email":"user@example.com","refreshToken":"rt-test"}}`
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, checkin_enabled, status, oauth_provider, oauth_account_key, extra_config, is_pinned, sort_order, balance, balance_used, quota, value_score, created_at, updated_at)
		 VALUES (?, ?, ?, 0, 'active', ?, ?, ?, 0, 0, 0, 0, 0, 0, ?, ?)`,
		siteID, "user@example.com", "at-test", provider, "acc-1", extra, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("account id: %v", err)
	}
	return accountID
}

func TestOAuthStart_IssuesCryptoRandomState(t *testing.T) {
	_, r := setupOAuthRoutesTest(t)

	resp1 := doPostJSON(t, r, "/api/oauth/providers/codex/start", map[string]any{})
	if resp1.Code != http.StatusOK {
		t.Fatalf("start #1 status=%d body=%s", resp1.Code, resp1.Body.String())
	}
	var body1 map[string]any
	if err := json.Unmarshal(resp1.Body.Bytes(), &body1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	state1, _ := body1["state"].(string)
	if state1 == "" || state1 == "stub-state" || state1 == "stub-rebind" {
		t.Fatalf("expected crypto-random state, got %q", state1)
	}
	if len(state1) < 20 {
		t.Fatalf("state too short for 24-byte entropy: %q", state1)
	}
	if body1["authorizationUrl"] == "" {
		t.Fatalf("authorizationUrl should be non-empty: %v", body1)
	}
	if body1["provider"] != "codex" {
		t.Fatalf("provider = %v, want codex", body1["provider"])
	}

	resp2 := doPostJSON(t, r, "/api/oauth/providers/codex/start", map[string]any{})
	if resp2.Code != http.StatusOK {
		t.Fatalf("start #2 status=%d body=%s", resp2.Code, resp2.Body.String())
	}
	var body2 map[string]any
	if err := json.Unmarshal(resp2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	state2, _ := body2["state"].(string)
	if state2 == "" || state2 == state1 {
		t.Fatalf("successive starts must issue distinct states; got %q and %q", state1, state2)
	}
}

func TestOAuthGetSession_ValidatesState(t *testing.T) {
	_, r := setupOAuthRoutesTest(t)

	start := doPostJSON(t, r, "/api/oauth/providers/codex/start", map[string]any{})
	if start.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	var started map[string]any
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	state, _ := started["state"].(string)

	ok := doGet(t, r, "/api/oauth/sessions/"+state)
	if ok.Code != http.StatusOK {
		t.Fatalf("getSession status=%d body=%s", ok.Code, ok.Body.String())
	}
	var session map[string]any
	if err := json.Unmarshal(ok.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session["state"] != state {
		t.Fatalf("state = %v, want %q", session["state"], state)
	}
	if session["status"] != "pending" {
		t.Fatalf("status = %v, want pending", session["status"])
	}
	if session["provider"] != "codex" {
		t.Fatalf("provider = %v, want codex", session["provider"])
	}

	missing := doGet(t, r, "/api/oauth/sessions/not-a-real-state")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing state status=%d body=%s, want 404", missing.Code, missing.Body.String())
	}
	var missingBody map[string]any
	if err := json.Unmarshal(missing.Body.Bytes(), &missingBody); err != nil {
		t.Fatalf("decode missing: %v", err)
	}
	msg, _ := missingBody["message"].(string)
	if !strings.Contains(msg, "oauth session not found") {
		t.Fatalf("message = %q, want session-not-found", msg)
	}
}

func TestOAuthManualCallback_RejectsStateMismatch(t *testing.T) {
	_, r := setupOAuthRoutesTest(t)

	start := doPostJSON(t, r, "/api/oauth/providers/codex/start", map[string]any{})
	if start.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	var started map[string]any
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	state, _ := started["state"].(string)

	// Callback URL carries a different state than the path parameter.
	mismatch := doPostJSON(t, r, "/api/oauth/sessions/"+state+"/manual-callback", map[string]any{
		"callbackUrl": "http://localhost:1455/auth/callback?state=attacker-forged-state&code=xyz",
	})
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status=%d body=%s, want 400", mismatch.Code, mismatch.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(mismatch.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "state mismatch") {
		t.Fatalf("message = %q, want state mismatch", msg)
	}

	// Unknown path state is rejected before callback processing.
	unknown := doPostJSON(t, r, "/api/oauth/sessions/unknown-state/manual-callback", map[string]any{
		"callbackUrl": "http://localhost:1455/auth/callback?state=unknown-state&code=xyz",
	})
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status=%d body=%s, want 404", unknown.Code, unknown.Body.String())
	}
}

func TestOAuthRebind_IssuesCryptoRandomState(t *testing.T) {
	db, r := setupOAuthRoutesTest(t)
	accountID := insertOAuthAccount(t, db, "codex")

	resp := doPostJSON(t, r, fmt.Sprintf("/api/oauth/connections/%d/rebind", accountID), map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("rebind status=%d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	state, _ := body["state"].(string)
	if state == "" || state == "stub-rebind" || state == "stub-state" {
		t.Fatalf("expected crypto-random rebind state, got %q", state)
	}
	if body["provider"] != "codex" {
		t.Fatalf("provider = %v, want codex", body["provider"])
	}
	if body["authorizationUrl"] == "" {
		t.Fatalf("authorizationUrl should be non-empty: %v", body)
	}

	// State must be server-stored and retrievable.
	sessionResp := doGet(t, r, "/api/oauth/sessions/"+state)
	if sessionResp.Code != http.StatusOK {
		t.Fatalf("getSession after rebind status=%d body=%s", sessionResp.Code, sessionResp.Body.String())
	}
}

func TestOAuthStart_UnknownProvider(t *testing.T) {
	_, r := setupOAuthRoutesTest(t)
	resp := doPostJSON(t, r, "/api/oauth/providers/not-a-provider/start", map[string]any{})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", resp.Code, resp.Body.String())
	}
}

func TestOAuthProviders_ListsRealProviders(t *testing.T) {
	_, r := setupOAuthRoutesTest(t)
	resp := doGet(t, r, "/api/oauth/providers")
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	providers, ok := body["providers"].([]any)
	if !ok || len(providers) == 0 {
		t.Fatalf("expected non-empty providers, got %v", body["providers"])
	}
}

// Ensure doGet is available even if sites_test helpers change; keep a local fallback.
func TestOAuthManualCallback_RequiresCallbackURL(t *testing.T) {
	_, r := setupOAuthRoutesTest(t)
	start := doPostJSON(t, r, "/api/oauth/providers/codex/start", map[string]any{})
	if start.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	var started map[string]any
	_ = json.Unmarshal(start.Body.Bytes(), &started)
	state, _ := started["state"].(string)

	resp := doPostJSON(t, r, "/api/oauth/sessions/"+state+"/manual-callback", map[string]any{
		"callbackUrl": "",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

// Silence unused import if httptest is only needed transitively via helpers.
var _ = httptest.NewRecorder
