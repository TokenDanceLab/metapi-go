package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/store"
)

func setupDownstreamKeysTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	RegisterDownstreamKeysRoutes(r, db.DB)
	return db, r
}

func setupDownstreamKeysPostgresTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("PG_TEST_DSN"))
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL integration test")
	}

	db, err := store.Open(store.DialectPostgres, dsn, false)
	if err != nil {
		t.Fatalf("failed to open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	RegisterDownstreamKeysRoutes(r, db.DB)
	return db, r
}

func seedDownstreamPolicyRefs(t *testing.T, db *store.DB) (siteID, routeID, accountID, tokenID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	suffix := strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	siteID = insertDownstreamFixtureID(t, db,
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES (?, ?, 'openai', 'active', ?, ?)`,
		"Policy Site "+suffix,
		"https://policy-"+suffix+".example.com",
		now, now,
	)

	routeID = insertDownstreamFixtureID(t, db,
		`INSERT INTO token_routes (model_pattern, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		"gpt-4o-"+suffix,
		true,
		now, now,
	)

	accountID = insertDownstreamFixtureID(t, db,
		`INSERT INTO accounts (site_id, access_token, api_token, status, checkin_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', ?, ?, ?)`,
		siteID,
		"session-token-"+suffix,
		"sk-default-key-"+suffix,
		true,
		now,
		now,
	)

	tokenID = insertDownstreamFixtureID(t, db,
		`INSERT INTO account_tokens (account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'manual-token', ?, 'ready', 'manual', ?, ?, ?, ?)`,
		accountID,
		"sk-token-key-"+suffix,
		true,
		false,
		now,
		now,
	)
	return siteID, routeID, accountID, tokenID
}

func insertDownstreamFixtureID(t *testing.T, db *store.DB, query string, args ...any) int64 {
	t.Helper()
	if db.Dialect == store.DialectPostgres {
		var id int64
		if err := db.QueryRowx(query+" RETURNING id", args...).Scan(&id); err != nil {
			t.Fatalf("insert postgres fixture: %v", err)
		}
		return id
	}

	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("insert sqlite fixture: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("sqlite LastInsertId: %v", err)
	}
	return id
}

func TestDownstreamKeysUpdatePartialPreservesPolicyFields(t *testing.T) {
	db, r := setupDownstreamKeysTest(t)
	siteID, routeID, accountID, tokenID := seedDownstreamPolicyRefs(t, db)
	now := time.Now().UTC().Format(time.RFC3339)

	supportedModels := `["gpt-4o","gpt-4o-mini"]`
	allowedRouteIds := mustJSON(t, []int64{routeID})
	siteWeightMultipliers := mustJSON(t, map[string]float64{itoa(siteID): 1.5})
	excludedSiteIds := mustJSON(t, []int64{siteID})
	excludedCredentialRefs := mustJSON(t, []map[string]any{
		{"kind": "account_token", "siteId": siteID, "accountId": accountID, "tokenId": tokenID},
		{"kind": "default_api_key", "siteId": siteID, "accountId": accountID},
	})

	res, err := db.Exec(
		`INSERT INTO downstream_api_keys
		(name, key, description, group_name, tags, enabled, expires_at, max_cost, used_cost, max_requests, used_requests,
		 supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		 created_at, updated_at)
		VALUES ('client-a', 'sk-client-a', 'desc', 'vip', '["alpha"]', 1, '2099-01-01T00:00:00Z',
		 12.5, 2.25, 99, 3, ?, ?, ?, ?, ?, ?, ?)`,
		supportedModels, allowedRouteIds, siteWeightMultipliers, excludedSiteIds, excludedCredentialRefs, now, now,
	)
	if err != nil {
		t.Fatalf("insert downstream key: %v", err)
	}
	keyID, _ := res.LastInsertId()

	resp := doPutJSON(t, r, "/api/downstream-keys/"+itoa(keyID), map[string]any{"enabled": false})
	if resp.Code != http.StatusOK {
		t.Fatalf("partial update returned %d: %s", resp.Code, resp.Body.String())
	}

	var groupName, expiresAt, storedSupported, storedAllowed, storedSWM, storedExcludedSites, storedExcludedCreds sql.NullString
	var maxCost sql.NullFloat64
	var maxRequests sql.NullInt64
	if err := db.QueryRow(
		`SELECT group_name, expires_at, max_cost, max_requests, supported_models, allowed_route_ids,
		 site_weight_multipliers, excluded_site_ids, excluded_credential_refs
		 FROM downstream_api_keys WHERE id = ?`,
		keyID,
	).Scan(&groupName, &expiresAt, &maxCost, &maxRequests, &storedSupported, &storedAllowed, &storedSWM, &storedExcludedSites, &storedExcludedCreds); err != nil {
		t.Fatalf("select downstream key: %v", err)
	}

	assertNullString(t, "group_name", groupName, "vip")
	assertNullString(t, "expires_at", expiresAt, "2099-01-01T00:00:00Z")
	if !maxCost.Valid || maxCost.Float64 != 12.5 {
		t.Fatalf("max_cost = %#v, want 12.5", maxCost)
	}
	if !maxRequests.Valid || maxRequests.Int64 != 99 {
		t.Fatalf("max_requests = %#v, want 99", maxRequests)
	}
	assertNullString(t, "supported_models", storedSupported, supportedModels)
	assertNullString(t, "allowed_route_ids", storedAllowed, allowedRouteIds)
	assertNullString(t, "site_weight_multipliers", storedSWM, siteWeightMultipliers)
	assertNullString(t, "excluded_site_ids", storedExcludedSites, excludedSiteIds)
	assertNullString(t, "excluded_credential_refs", storedExcludedCreds, excludedCredentialRefs)
}

func TestDownstreamKeysCreateAcceptsValidExcludedCredentialRefs(t *testing.T) {
	db, r := setupDownstreamKeysTest(t)
	siteID, _, accountID, tokenID := seedDownstreamPolicyRefs(t, db)

	resp := doPostJSON(t, r, "/api/downstream-keys", map[string]any{
		"name": "policy-client",
		"key":  "sk-policy-client",
		"excludedCredentialRefs": []map[string]any{
			{"kind": "account_token", "siteId": siteID, "accountId": accountID, "tokenId": tokenID},
			{"kind": "default_api_key", "siteId": siteID, "accountId": accountID},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", resp.Code, resp.Body.String())
	}

	var stored sql.NullString
	if err := db.QueryRow("SELECT excluded_credential_refs FROM downstream_api_keys WHERE key = 'sk-policy-client'").Scan(&stored); err != nil {
		t.Fatalf("select excluded refs: %v", err)
	}
	if !stored.Valid {
		t.Fatal("excluded_credential_refs was not stored")
	}
	var refs []map[string]any
	if err := json.Unmarshal([]byte(stored.String), &refs); err != nil {
		t.Fatalf("invalid excluded refs json: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("stored excluded refs len = %d, want 2: %s", len(refs), stored.String)
	}
}

func TestDownstreamKeys_PostgresCRUDResetBatchAndDelete(t *testing.T) {
	db, r := setupDownstreamKeysPostgresTest(t)
	suffix := "pg-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	key := "sk-dk-" + suffix
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM downstream_api_keys WHERE key = ?", key)
	})

	resp := doPostJSON(t, r, "/api/downstream-keys", map[string]any{
		"name":        "PG Downstream " + suffix,
		"key":         key,
		"description": "pg downstream test",
		"enabled":     true,
		"tags":        []string{"pg", "pg"},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", resp.Code, resp.Body.String())
	}

	var createBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	item, ok := createBody["item"].(map[string]any)
	if !ok {
		t.Fatalf("create response missing item: %#v", createBody)
	}
	keyID := int64(item["id"].(float64))
	if keyID <= 0 {
		t.Fatalf("created id = %d, want positive", keyID)
	}

	renamed := "PG Downstream Updated " + suffix
	resp = doPutJSON(t, r, "/api/downstream-keys/"+itoa(keyID), map[string]any{
		"name":    renamed,
		"enabled": false,
		"maxCost": 12.5,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update returned %d: %s", resp.Code, resp.Body.String())
	}

	resp = doGet(t, r, "/api/downstream-keys/summary?status=disabled&search="+suffix)
	if resp.Code != http.StatusOK {
		t.Fatalf("summary returned %d: %s", resp.Code, resp.Body.String())
	}
	var summary map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	items, ok := summary["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("summary disabled items = %#v, want one item", summary["items"])
	}

	if _, err := db.Exec("UPDATE downstream_api_keys SET used_cost = ?, used_requests = ? WHERE id = ?", 3.25, 7, keyID); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	resp = doPostJSON(t, r, "/api/downstream-keys/"+itoa(keyID)+"/reset-usage", map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("reset usage returned %d: %s", resp.Code, resp.Body.String())
	}
	var usedCost float64
	var usedRequests int64
	if err := db.QueryRowx(db.Rebind("SELECT used_cost, used_requests FROM downstream_api_keys WHERE id = ?"), keyID).Scan(&usedCost, &usedRequests); err != nil {
		t.Fatalf("select reset usage: %v", err)
	}
	if usedCost != 0 || usedRequests != 0 {
		t.Fatalf("usage after reset = cost %.2f requests %d, want zero", usedCost, usedRequests)
	}

	resp = doPostJSON(t, r, "/api/downstream-keys/batch", map[string]any{
		"ids":    []int64{keyID},
		"action": "enable",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("batch enable returned %d: %s", resp.Code, resp.Body.String())
	}
	var enabled bool
	if err := db.QueryRowx(db.Rebind("SELECT enabled FROM downstream_api_keys WHERE id = ?"), keyID).Scan(&enabled); err != nil {
		t.Fatalf("select enabled: %v", err)
	}
	if !enabled {
		t.Fatal("batch enable did not set enabled=true")
	}

	resp = doDelete(t, r, "/api/downstream-keys/"+itoa(keyID))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", resp.Code, resp.Body.String())
	}
	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM downstream_api_keys WHERE id = ?", keyID); err != nil {
		t.Fatalf("count deleted key: %v", err)
	}
	if count != 0 {
		t.Fatalf("deleted key count = %d, want 0", count)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture JSON: %v", err)
	}
	return string(data)
}

func assertNullString(t *testing.T, name string, got sql.NullString, want string) {
	t.Helper()
	if !got.Valid || got.String != want {
		t.Fatalf("%s = %#v, want %q", name, got, want)
	}
}

func TestDownstreamKeysProxyURLCreateUpdateGetAndClear(t *testing.T) {
	_, r := setupDownstreamKeysTest(t)

	// Create with proxyUrl
	resp := doPostJSON(t, r, "/api/downstream-keys", map[string]any{
		"name":     "proxy-client",
		"key":      "sk-proxy-client-1",
		"proxyUrl": "http://key-proxy.example:8080",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create returned %d: %s", resp.Code, resp.Body.String())
	}
	var createBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	item, ok := createBody["item"].(map[string]any)
	if !ok {
		t.Fatalf("create missing item: %#v", createBody)
	}
	if item["proxyUrl"] != "http://key-proxy.example:8080" {
		t.Fatalf("create proxyUrl = %#v, want key proxy", item["proxyUrl"])
	}
	keyID := int64(item["id"].(float64))

	// List/get should include proxyUrl (camelCase via SELECT *)
	resp = doGet(t, r, "/api/downstream-keys")
	if resp.Code != http.StatusOK {
		t.Fatalf("list returned %d: %s", resp.Code, resp.Body.String())
	}
	var listBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	items, _ := listBody["items"].([]any)
	found := false
	for _, raw := range items {
		row, _ := raw.(map[string]any)
		if int64(row["id"].(float64)) == keyID {
			found = true
			if row["proxyUrl"] != "http://key-proxy.example:8080" {
				t.Fatalf("list proxyUrl = %#v", row["proxyUrl"])
			}
		}
	}
	if !found {
		t.Fatal("created key not in list")
	}

	// Partial update without proxyUrl preserves it
	resp = doPutJSON(t, r, "/api/downstream-keys/"+itoa(keyID), map[string]any{
		"name": "proxy-client-renamed",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("partial update returned %d: %s", resp.Code, resp.Body.String())
	}
	var updateBody map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &updateBody); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	item = updateBody["item"].(map[string]any)
	if item["proxyUrl"] != "http://key-proxy.example:8080" {
		t.Fatalf("partial update dropped proxyUrl: %#v", item["proxyUrl"])
	}
	if item["name"] != "proxy-client-renamed" {
		t.Fatalf("name not updated: %#v", item["name"])
	}

	// Clear proxyUrl with empty string → inherit (null)
	resp = doPutJSON(t, r, "/api/downstream-keys/"+itoa(keyID), map[string]any{
		"proxyUrl": "",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("clear proxyUrl returned %d: %s", resp.Code, resp.Body.String())
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &updateBody); err != nil {
		t.Fatalf("unmarshal clear: %v", err)
	}
	item = updateBody["item"].(map[string]any)
	if item["proxyUrl"] != nil {
		t.Fatalf("cleared proxyUrl should be null, got %#v", item["proxyUrl"])
	}

	// Invalid scheme rejected
	resp = doPostJSON(t, r, "/api/downstream-keys", map[string]any{
		"name":     "bad-proxy",
		"key":      "sk-bad-proxy-1",
		"proxyUrl": "ftp://not-supported",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid proxyUrl create returned %d, want 400: %s", resp.Code, resp.Body.String())
	}
}

func TestNormalizeDownstreamProxyURL(t *testing.T) {
	got, errMsg := normalizeDownstreamProxyURL(nil)
	if got != nil || errMsg != "" {
		t.Fatalf("nil => (%v, %q)", got, errMsg)
	}
	empty := "  "
	got, errMsg = normalizeDownstreamProxyURL(&empty)
	if got != nil || errMsg != "" {
		t.Fatalf("empty => (%v, %q)", got, errMsg)
	}
	ok := " http://proxy:1 "
	got, errMsg = normalizeDownstreamProxyURL(&ok)
	if errMsg != "" || got == nil || *got != "http://proxy:1" {
		t.Fatalf("ok => (%v, %q)", got, errMsg)
	}
	bad := "notaurl"
	got, errMsg = normalizeDownstreamProxyURL(&bad)
	if got != nil || errMsg == "" {
		t.Fatalf("bad => (%v, %q)", got, errMsg)
	}
}
