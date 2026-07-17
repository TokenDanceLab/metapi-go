package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/store"
)

func TestRedactAccountSecretsHelper(t *testing.T) {
	t.Parallel()
	secret := "sk-account-list-secret-abcdef"
	api := "sk-api-token-secret-xyz"
	extra := `{"credentialMode":"session","sub2apiAuth":{"refreshToken":"rt-super-secret","tokenExpiresAt":123},"autoRelogin":{"passwordCipher":"cipher-secret"},"proxyUrl":"http://127.0.0.1:7890"}`
	row := map[string]any{
		"id":          int64(1),
		"accessToken": secret,
		"apiToken":    api,
		"extraConfig": extra,
		"username":    "u1",
	}
	redactAccountSecrets(row)
	if row["accessToken"] == secret {
		t.Fatalf("accessToken not redacted: %#v", row["accessToken"])
	}
	if row["apiToken"] == api {
		t.Fatalf("apiToken not redacted: %#v", row["apiToken"])
	}
	if !strings.Contains(row["accessToken"].(string), "***") && !strings.Contains(row["accessToken"].(string), "****") {
		t.Fatalf("accessToken mask unexpected: %#v", row["accessToken"])
	}
	ec, _ := row["extraConfig"].(string)
	if strings.Contains(ec, "rt-super-secret") || strings.Contains(ec, "cipher-secret") {
		t.Fatalf("extraConfig leaked secret: %s", ec)
	}
	if !strings.Contains(ec, "proxyUrl") || !strings.Contains(ec, "credentialMode") {
		t.Fatalf("extraConfig lost non-secret fields: %s", ec)
	}
	redactAccountSecrets(nil) // must not panic
}

func TestRedactAccountTokenSecretsHelper(t *testing.T) {
	t.Parallel()
	secret := "sk-token-list-secret-123456"
	row := map[string]any{
		"id":    int64(9),
		"token": secret,
		"name":  "t1",
		"site":  map[string]any{"platform": "newapi"},
	}
	redactAccountTokenSecrets(row)
	if _, ok := row["token"]; ok {
		t.Fatalf("token still present: %#v", row["token"])
	}
	masked, _ := row["tokenMasked"].(string)
	if masked == "" || masked == secret {
		t.Fatalf("tokenMasked = %#v", masked)
	}
}

func TestAccountsListRedactsPlaintextSecrets(t *testing.T) {
	secretAccess := "sk-list-access-token-secret-aaa"
	secretAPI := "sk-list-api-token-secret-bbb"
	db, r, _ := setupAccountsTest(t)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES (?, ?, 'newapi', 'active', ?, ?)`,
		"Secret List Site", "https://secret-list.example.com", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	extra := `{"credentialMode":"session","sub2apiAuth":{"refreshToken":"rt-list-secret-should-not-leak"}}`
	_, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'active', 1, ?, ?, ?)`,
		siteID, "secret-user", secretAccess, secretAPI, extra, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Force miss so we exercise the redaction path, not a stale empty cache.
	globalAccountsCache.clear()
	resp := doGet(t, r, "/api/accounts?refresh=1")
	if resp.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, secretAccess) {
		t.Fatalf("list leaked accessToken secret")
	}
	if strings.Contains(body, secretAPI) {
		t.Fatalf("list leaked apiToken secret")
	}
	if strings.Contains(body, "rt-list-secret-should-not-leak") {
		t.Fatalf("list leaked refreshToken secret")
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	accounts, _ := result["accounts"].([]any)
	if len(accounts) == 0 {
		t.Fatal("expected accounts")
	}
	found := false
	for _, raw := range accounts {
		acc, _ := raw.(map[string]any)
		if acc["username"] != "secret-user" {
			continue
		}
		found = true
		at, _ := acc["accessToken"].(string)
		if at == "" || at == secretAccess {
			t.Fatalf("accessToken not masked: %#v", at)
		}
		api, _ := acc["apiToken"].(string)
		if api == "" || api == secretAPI {
			t.Fatalf("apiToken not masked: %#v", api)
		}
	}
	if !found {
		t.Fatal("secret-user missing from list")
	}
}

func TestAccountsUpdateResponseRedactsSecrets(t *testing.T) {
	secretAccess := "sk-update-access-token-secret"
	secretAPI := "sk-update-api-token-secret"
	db, r, _ := setupAccountsTest(t)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES (?, ?, 'newapi', 'active', ?, ?)`,
		"Secret Update Site", "https://secret-update.example.com", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'active', 1, ?, ?)`,
		siteID, "upd-user", secretAccess, secretAPI, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ := res.LastInsertId()

	resp := doPutJSON(t, r, "/api/accounts/"+itoa(accountID), map[string]any{
		"status": "active",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: %d %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), secretAccess) || strings.Contains(resp.Body.String(), secretAPI) {
		t.Fatalf("update response leaked secret: %s", resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["accessToken"] == secretAccess {
		t.Fatalf("update accessToken plaintext: %#v", body["accessToken"])
	}
	if body["apiToken"] == secretAPI {
		t.Fatalf("update apiToken plaintext: %#v", body["apiToken"])
	}
}

func TestAccountTokenUpdateResponseOmitsPlaintextToken(t *testing.T) {
	secret := "sk-update-token-value-secret-zzz"
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "upd", secret, "", true, false)

	resp := doPutJSON(t, r, "/api/account-tokens/"+itoa(tokID), map[string]any{
		"name": "upd-renamed",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update token: %d %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), secret) {
		t.Fatalf("update token response leaked plaintext secret")
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tok, _ := result["token"].(map[string]any)
	if tok == nil {
		t.Fatalf("missing token object: %#v", result)
	}
	if v, ok := tok["token"]; ok && v != nil && v != "" {
		t.Fatalf("token field present: %#v", v)
	}
	if tok["tokenMasked"] == nil || tok["tokenMasked"] == "" {
		t.Fatalf("tokenMasked missing: %#v", tok)
	}
	if tok["name"] != "upd-renamed" {
		t.Fatalf("name = %#v", tok["name"])
	}
}

func TestAccountTokenValueExportKeepsFullSecret(t *testing.T) {
	// Intentional export-secret contract: GET /api/account-tokens/:id/value returns full token (#367).
	secret := "sk-export-value-token-secret-keep"
	db, r := setupTokensTest(t)
	_, accountID := tokenFixture(t, db, r)
	tokID := createTokenFixture(t, db, accountID, "export-me", secret, "", true, true)

	resp := doGet(t, r, "/api/account-tokens/"+itoa(tokID)+"/value")
	if resp.Code != http.StatusOK {
		t.Fatalf("get value: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["token"] != secret {
		t.Fatalf("export /value must return full token, got %#v", result["token"])
	}
	if result["tokenMasked"] == nil || result["tokenMasked"] == "" {
		t.Fatalf("tokenMasked missing on export path")
	}
}

func TestRouteChannelsListRedactsAccountSecrets(t *testing.T) {
	secretAccess := "sk-route-access-secret-1111"
	secretAPI := "sk-route-api-secret-2222"
	db, r := setupTokenRoutesTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES ('Route Secret Site', 'https://route-secret.example.com', 'newapi', 'active', ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, created_at, updated_at)
		 VALUES (?, 'route-user', ?, ?, 'active', 0, ?, ?)`,
		siteID, secretAccess, secretAPI, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ := res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'rt', 'sk-route-token-only', 'ready', 'manual', 1, 1, ?, ?)`,
		accountID, now, now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokenID, _ := res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO token_routes (model_pattern, enabled, sort_order, created_at, updated_at)
		 VALUES ('secret-model', 1, 0, ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override)
		 VALUES (?, ?, ?, 'secret-model', 1, 1, 1, 0)`,
		routeID, accountID, tokenID,
	)
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	// List routes embeds channels.
	resp := doGet(t, r, "/api/routes")
	if resp.Code != http.StatusOK {
		t.Fatalf("list routes: %d %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), secretAccess) || strings.Contains(resp.Body.String(), secretAPI) {
		t.Fatalf("routes list leaked account secret")
	}

	// Explicit channels endpoint.
	resp = doGet(t, r, "/api/routes/"+itoa(routeID)+"/channels")
	if resp.Code != http.StatusOK {
		t.Fatalf("get channels: %d %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), secretAccess) || strings.Contains(resp.Body.String(), secretAPI) {
		t.Fatalf("channels list leaked account secret")
	}
	var channels []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &channels); err != nil {
		t.Fatalf("unmarshal channels: %v body=%s", err, resp.Body.String())
	}
	if len(channels) == 0 {
		t.Fatal("expected channel")
	}
	acc, _ := channels[0]["account"].(map[string]any)
	if acc == nil {
		t.Fatalf("missing account: %#v", channels[0])
	}
	if acc["accessToken"] == secretAccess {
		t.Fatalf("channel accessToken plaintext")
	}
	if acc["apiToken"] == secretAPI {
		t.Fatalf("channel apiToken plaintext")
	}
	// Presence preserved via non-empty mask for FE heuristics.
	if s, _ := acc["accessToken"].(string); s == "" {
		t.Fatalf("accessToken mask empty; presence should be preserved")
	}
}

func TestSearchRedactsAccountAndTokenSecrets(t *testing.T) {
	secretAccess := "sk-search-access-secret-xyz"
	secretToken := "sk-search-token-secret-xyz"
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	r := chi.NewRouter()
	RegisterSearchRoutes(r, db.DB)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES ('Search Secret Site', 'https://search-secret.example.com', 'newapi', 'active', ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	res, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		 VALUES (?, 'search-secret-user', ?, 'active', 0, ?, ?)`,
		siteID, secretAccess, now, now,
	)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	accountID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'search-token', ?, 'ready', 'manual', 1, 1, ?, ?)`,
		accountID, secretToken, now, now,
	)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	resp := doPostJSON(t, r, "/api/search", map[string]any{"query": "search-secret", "limit": 20})
	if resp.Code != http.StatusOK {
		t.Fatalf("search: %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, secretAccess) {
		t.Fatalf("search leaked accessToken")
	}
	if strings.Contains(body, secretToken) {
		t.Fatalf("search leaked account token")
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tokens, _ := result["accountTokens"].([]any)
	if len(tokens) == 0 {
		t.Fatal("expected accountTokens hit")
	}
	tok := tokens[0].(map[string]any)
	if _, ok := tok["token"]; ok {
		t.Fatalf("search token field present: %#v", tok["token"])
	}
	if tok["tokenMasked"] == nil || tok["tokenMasked"] == "" {
		t.Fatalf("search missing tokenMasked: %#v", tok)
	}
}

func TestDownstreamKeyExportKeepsFullSecret(t *testing.T) {
	// Intentional export-secret contract: GET /api/downstream-keys/:id/export returns full key (#367).
	secret := "sk-export-credential-secret-keep"
	_, r := setupDownstreamKeysTest(t)
	create := doPostJSON(t, r, "/api/downstream-keys", map[string]any{
		"name": "export-client",
		"key":  secret,
	})
	if create.Code != http.StatusOK {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	item := created["item"].(map[string]any)
	keyID := int64(item["id"].(float64))

	resp := doGet(t, r, "/api/downstream-keys/"+itoa(keyID)+"/export")
	if resp.Code != http.StatusOK {
		t.Fatalf("export: %d %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), secret) {
		t.Fatalf("export must include full secret for operator copy")
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	profiles, _ := body["profiles"].([]any)
	if len(profiles) == 0 {
		t.Fatal("expected export profiles")
	}
}

func TestOAuthConnectionsFallbackRedactsSecrets(t *testing.T) {
	// Force the sql fallback list path by not wiring oauth.ListOauthConnections success
	// through a global store that returns items — use handler DB only and break service by
	// leaving store.DB nil / empty oauth provider query via direct handler registration.
	secretAccess := "sk-oauth-conn-access-secret"
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		store.OverrideDB(nil)
		db.Close()
	})
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Ensure service list path fails so handler uses queryRows fallback.
	store.OverrideDB(nil)

	r := chi.NewRouter()
	RegisterOauthRoutes(r, db.DB)

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES ('OAuth Secret Site', 'https://oauth-secret.example.com', 'newapi', 'active', ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	siteID, _ := res.LastInsertId()
	extra := `{"oauth":{"provider":"codex","refreshToken":"rt-oauth-secret-leak"}}`
	_, err = db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, oauth_provider, extra_config, created_at, updated_at)
		 VALUES (?, 'oauth-user', ?, 'active', 0, 'codex', ?, ?, ?)`,
		siteID, secretAccess, extra, now, now,
	)
	if err != nil {
		t.Fatalf("account: %v", err)
	}

	resp := doGet(t, r, "/api/oauth/connections")
	if resp.Code != http.StatusOK {
		t.Fatalf("connections: %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, secretAccess) {
		t.Fatalf("oauth connections leaked accessToken")
	}
	if strings.Contains(body, "rt-oauth-secret-leak") {
		t.Fatalf("oauth connections leaked refreshToken")
	}
}

// silence unused import guards for packages only used in this file's local setups.
var (
	_ = chi.NewRouter
	_ = store.DialectSQLite
)
