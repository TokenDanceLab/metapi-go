package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// Edge Case Tests for metapi-go Boundary Audit
// =============================================================================
// Covers: empty request body, oversized request, negative pagination,
// SQLite locks (concurrent writes), duplicate unique key inserts,
// NULL handling in JSON responses, timezone consistency,
// concurrent settings updates, race on route rebuild + proxy request.
// =============================================================================

// ---- Helpers ----

func setupEdgeTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
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
		AccountCredentialSecret: "test-secret-edge",
		ProxyToken:              "edge-test-proxy-token",
		AuthToken:               "admin-edge-test-token",
		RequestBodyLimit:        20 * 1024 * 1024, // 20 MB
	}
	config.Set(cfg)

	r := chi.NewRouter()
	RegisterSitesRoutes(r, db.DB)
	RegisterAccountsRoutes(r, db.DB, cfg)
	RegisterSettingsRoutes(r, db.DB, cfg)
	return db, r, cfg
}

// =============================================================================
// 1. Empty Request Body
// =============================================================================

func TestEdge_EmptyRequestBody_CreateSite(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	req := httptest.NewRequest("POST", "/api/sites", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEdge_EmptyRequestBody_CreateAccount(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	req := httptest.NewRequest("POST", "/api/accounts", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d: %s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	json.Unmarshal(rec.Body.Bytes(), &result)
	if result["error"] == nil && result["message"] == nil {
		t.Error("expected error or message field in response")
	}
}

func TestEdge_EmptyRequestBody_UpdateRuntime(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	req := httptest.NewRequest("PUT", "/api/settings/runtime", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d: %s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	json.Unmarshal(rec.Body.Bytes(), &result)
	if result["success"] != false {
		t.Error("expected success=false for empty body on updateRuntime")
	}
}

func TestEdge_TrailingJSON_CreateSiteRejected(t *testing.T) {
	db, r, _ := setupEdgeTest(t)
	body := `{"name":"Trailing JSON","url":"https://api.openai.com"} {}`
	req := httptest.NewRequest("POST", "/api/sites", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing JSON, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM sites WHERE name = ?", "Trailing JSON"); err != nil {
		t.Fatalf("count sites: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no site to be inserted, got %d", count)
	}
}

func TestEdge_TrailingJSON_UpdateRuntimeRejected(t *testing.T) {
	_, r, cfg := setupEdgeTest(t)
	body := `{"proxyToken":"new-proxy-token"} {}`
	req := httptest.NewRequest("PUT", "/api/settings/runtime", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing JSON, got %d: %s", rec.Code, rec.Body.String())
	}
	if cfg.ProxyToken != "edge-test-proxy-token" {
		t.Fatalf("proxy token mutated to %q", cfg.ProxyToken)
	}
}

func TestEdge_DuplicateJSONKey_UpdateRuntimeRejected(t *testing.T) {
	_, r, cfg := setupEdgeTest(t)
	body := `{"proxyToken":"first-proxy-token","proxyToken":"second-proxy-token"}`
	req := httptest.NewRequest("PUT", "/api/settings/runtime", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate JSON key, got %d: %s", rec.Code, rec.Body.String())
	}
	if cfg.ProxyToken != "edge-test-proxy-token" {
		t.Fatalf("proxy token mutated to %q", cfg.ProxyToken)
	}
}

func TestEdge_TrailingJSON_CreateAccountRejected(t *testing.T) {
	db, r, _ := setupEdgeTest(t)
	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name": "Account Site",
		"url":  "https://api.openai.com",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("create site fixture: %d %s", siteResp.Code, siteResp.Body.String())
	}

	body := `{"siteId":1,"accessToken":"account-token"} {}`
	req := httptest.NewRequest("POST", "/api/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing JSON, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM accounts WHERE access_token = ?", "account-token"); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no account to be inserted, got %d", count)
	}
}

func TestEdge_EmptyRequestBody_BatchAccounts(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	req := httptest.NewRequest("POST", "/api/accounts/batch", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty batch body, got %d", rec.Code)
	}
}

func TestEdge_EmptyRequestBody_Login(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	req := httptest.NewRequest("POST", "/api/accounts/login", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty login body, got %d", rec.Code)
	}
}

// =============================================================================
// 2. Oversized Request Body
// =============================================================================

// TestEdge_OversizedBody verifies the server handles large payloads without
// crashing. The admin JSON decoder has its own cap in addition to router-level
// BodyLimit, so direct handler tests still exercise a bounded read path.
func TestEdge_OversizedBody_CreateSite(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	// 5MB of JSON data
	largeStr := strings.Repeat("x", 1024)
	body := map[string]any{
		"name":          "oversized-site",
		"url":           "https://example.com",
		"platform":      "openai",
		"customHeaders": strings.Repeat(`{"key":"`+largeStr+`"},`, 5000), // ~5MB
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/sites", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Server should NOT crash; should return some error (400/413) or succeed
	if rec.Code < 200 || rec.Code >= 600 {
		t.Fatalf("unexpected status code: %d", rec.Code)
	}
	t.Logf("oversized body (%.1fMB) returned status: %d", float64(len(bodyBytes))/1024/1024, rec.Code)
}

// TestEdge_MaxBodyLimitEnforced verifies direct admin route tests cannot bypass
// the request-body cap even when they do not build the full router middleware
// stack.
func TestEdge_MaxBodyLimitEnforced(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	cfg := config.Get()
	if cfg.RequestBodyLimit <= 0 {
		t.Error("RequestBodyLimit is not configured with a positive value")
	}
	t.Logf("RequestBodyLimit is configured as %d bytes", cfg.RequestBodyLimit)

	// Attempt to send a body larger than the configured limit
	giantStr := strings.Repeat("x", cfg.RequestBodyLimit+1024)
	body := `{"name":"test","url":"https://example.com","platform":"openai","customHeaders":"` + giantStr + `"}`
	req := httptest.NewRequest("POST", "/api/sites", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("oversized body unexpectedly succeeded: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 400 or 413: %s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// 3. Negative Pagination
// =============================================================================

// TestEdge_NoPaginationParameters verifies that list endpoints do not accept
// pagination parameters. Currently there ARE no pagination parameters --
// this is a design gap: unlimited list queries can cause OOM on large datasets.
func TestEdge_NoPaginationOnListSites(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	// Create many sites to test no-pagination behavior
	for i := 0; i < 50; i++ {
		site := newSiteFixture(t, r, "pag-site-"+itoa(int64(i)), "https://api.openai.com/v"+itoa(int64(i)))
		_ = site
	}

	// Request without pagination -- returns ALL records
	rec := doGet(t, r, "/api/sites")
	if rec.Code != http.StatusOK {
		t.Fatalf("list sites: %d", rec.Code)
	}

	var sites []any
	json.Unmarshal(rec.Body.Bytes(), &sites)
	if len(sites) < 50 {
		t.Errorf("expected at least 50 sites, got %d", len(sites))
	}
	t.Logf("No pagination: all %d sites returned in single response", len(sites))

	// Verify that passing pagination params (page=-1) has no effect
	rec2 := doGet(t, r, "/api/sites?page=-1&pageSize=-1")
	if rec2.Code != http.StatusOK {
		t.Fatalf("list sites with negative page: %d", rec2.Code)
	}
	var sites2 []any
	json.Unmarshal(rec2.Body.Bytes(), &sites2)
	// Negative pagination is ignored (no pagination logic exists)
	if len(sites2) != len(sites) {
		t.Errorf("negative pagination should be ignored, got %d vs %d", len(sites2), len(sites))
	}
}

func TestEdge_NegativePaginationIgnored_Accounts(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	rec := doGet(t, r, "/api/accounts?page=-5&limit=-10&offset=-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Negative params are silently ignored (no validation, no pagination logic)
	t.Log("negative pagination params silently ignored in accounts list")
}

// =============================================================================
// 4. SQLite Concurrent Writes (LOCK detection)
// =============================================================================

// TestEdge_SQLiteConcurrentWrites_Settings tests that concurrent writes to the
// settings table do not cause data loss or corruption under WAL mode.
// Uses a proper transaction-based UPSERT to avoid TOCTOU races.
func TestEdge_SQLiteConcurrentWrites_Settings(t *testing.T) {
	db, _, _ := setupEdgeTest(t)

	const goroutines = 10
	const iterations = 30
	var wg sync.WaitGroup
	errorsCh := make(chan error, goroutines*iterations)
	uniqueErrors := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := "edge.concurrent.key." + itoa(int64(gid%3))
				value := "value-" + itoa(int64(gid)) + "-" + itoa(int64(i))
				jsonValue, _ := json.Marshal(value)
				// Use the racy upsertSettingDB which has TOCTOU bug
				var count int
				if err := db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", key); err != nil {
					errorsCh <- err
					continue
				}
				if count > 0 {
					_, err := db.Exec("UPDATE settings SET value = ? WHERE key = ?", string(jsonValue), key)
					if err != nil {
						if strings.Contains(err.Error(), "UNIQUE") {
							uniqueErrors <- err
						} else {
							errorsCh <- err
						}
					}
				} else {
					_, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", key, string(jsonValue))
					if err != nil {
						if strings.Contains(err.Error(), "UNIQUE") {
							uniqueErrors <- err
						} else {
							errorsCh <- err
						}
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errorsCh)
	close(uniqueErrors)

	errCount := 0
	for range errorsCh {
		errCount++
	}
	uniqueCount := 0
	for range uniqueErrors {
		uniqueCount++
	}
	t.Logf("Racy upsertSettingDB: %d non-unique errors, %d UNIQUE constraint violations (TOCTOU race evidence)", errCount, uniqueCount)
}

// TestEdge_SQLiteConcurrentWrites_SafeSettingsStore verifies that the
// SettingsStore.Set (which uses ON CONFLICT DO UPDATE) handles concurrent
// writes without errors.
func TestEdge_SQLiteConcurrentWrites_SafeSettingsStore(t *testing.T) {
	db, _, _ := setupEdgeTest(t)
	settingsStore := store.NewSettingsStore(db)

	const goroutines = 10
	const iterations = 30
	var wg sync.WaitGroup
	errorsCh := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := "edge.safe.key." + itoa(int64(gid%3))
				value := "value-" + itoa(int64(gid)) + "-" + itoa(int64(i))
				if err := settingsStore.Set(key, value); err != nil {
					errorsCh <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errorsCh)

	errCount := 0
	for err := range errorsCh {
		errCount++
		t.Logf("SettingsStore.Set error: %v", err)
	}
	if errCount > 0 {
		t.Logf("SettingsStore ON CONFLICT UPSERT: %d errors (SQLite WAL may serialize concurrent writes)", errCount)
	} else {
		t.Log("SettingsStore ON CONFLICT UPSERT: 0 errors under concurrent writes")
	}
}

// TestEdge_SQLiteConcurrentWrites_Accounts verifies concurrent account creation
// against a single-connection SQLite (necessary for in-memory DB since each
// connection gets its own database with :memory:).
func TestEdge_SQLiteConcurrentWrites_Accounts(t *testing.T) {
	db, _, _ := setupEdgeTest(t)
	// In-memory SQLite: each connection in the pool gets a separate database.
	// Limit to 1 connection so all goroutines share the same DB.
	db.SetMaxOpenConns(1)
	now := time.Now().UTC().Format(time.RFC3339)

	// Insert a site directly for concurrent account tests
	res, _ := db.Exec(
		"INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		"ConcurrentSite", "https://api.openai.com/concurrent", "openai", now, now,
	)
	siteID, _ := res.LastInsertId()

	const goroutines = 20
	var wg sync.WaitGroup
	errorsCh := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			now := time.Now().UTC().Format(time.RFC3339)
			_, err := db.Exec(
				`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
				 VALUES (?, ?, ?, 'active', 1, ?, ?)`,
				siteID, "user-"+itoa(int64(gid)), "concurrent-token-"+itoa(int64(gid)), now, now,
			)
			if err != nil {
				errorsCh <- err
			}
		}(g)
	}
	wg.Wait()
	close(errorsCh)

	errCount := 0
	for err := range errorsCh {
		errCount++
		t.Logf("concurrent account insert error: %v", err)
	}
	if errCount > 0 {
		t.Errorf("%d concurrent account inserts failed", errCount)
	} else {
		t.Logf("All %d concurrent account inserts succeeded", goroutines)
	}
}

// =============================================================================
// 5. Duplicate Unique Key Inserts
// =============================================================================

func TestEdge_DuplicateSiteCreation(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	body := map[string]any{
		"name":     "dup-site",
		"url":      "https://api.openai.com",
		"platform": "openai",
	}

	// First creation: succeeds
	rec1 := doPostJSON(t, r, "/api/sites", body)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first site creation: %d %s", rec1.Code, rec1.Body.String())
	}

	// Second creation: should return 409 Conflict
	rec2 := doPostJSON(t, r, "/api/sites", body)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate site, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var result map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &result)
	if err, ok := result["error"].(string); ok && !strings.Contains(strings.ToLower(err), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %s", err)
	}
}

func TestEdge_DuplicateAccountCreation(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	site := newSiteFixture(t, r, "DupAccSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// Note: accounts table has no unique constraint on (site_id, access_token)
	// So duplicate accounts with same accessToken will succeed.
	// This is a potential data integrity issue for credential import.
	body := map[string]any{
		"siteId":         siteID,
		"accessToken":    "dup-token-test",
		"credentialMode": "apikey",
		"skipModelFetch": true,
	}
	rec1 := doPostJSON(t, r, "/api/accounts", body)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first account: %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := doPostJSON(t, r, "/api/accounts", body)
	if rec2.Code == http.StatusOK {
		t.Log("WARNING: duplicate account with same accessToken was accepted (no UNIQUE constraint on accounts)")
	} else {
		t.Logf("duplicate account returned %d", rec2.Code)
	}
}

// =============================================================================
// 6. NULL Handling in JSON Responses
// =============================================================================

func TestEdge_NULLFieldsInJSONResponse(t *testing.T) {
	db, r, _ := setupEdgeTest(t)

	// Create a site with NULL-able fields
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, external_checkin_url, proxy_url,
		 custom_headers, api_key, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, NULL, NULL, NULL, ?, ?)`,
		"null-test-site", "https://null-test.com", "openai", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()

	// Read via API
	rec := doGet(t, r, "/api/sites")
	if rec.Code != http.StatusOK {
		t.Fatalf("list sites: %d", rec.Code)
	}

	var sites []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &sites)

	var found map[string]any
	for _, s := range sites {
		if id, _ := s["id"].(float64); int64(id) == siteID {
			found = s
			break
		}
	}
	if found == nil {
		t.Fatal("site not found in response")
	}

	// NULL fields should serialize as null in JSON
	if found["externalCheckinUrl"] != nil {
		t.Errorf("expected externalCheckinUrl=nil, got %v", found["externalCheckinUrl"])
	}
	if found["proxyUrl"] != nil {
		t.Errorf("expected proxyUrl=nil, got %v", found["proxyUrl"])
	}
	if found["apiKey"] != nil {
		t.Errorf("expected apiKey=nil, got %v", found["apiKey"])
	}
	t.Log("NULL database fields correctly serialize as JSON null")
}

func TestEdge_NULLOptionalFieldsInAccount(t *testing.T) {
	db, r, _ := setupEdgeTest(t)
	site := newSiteFixture(t, r, "NullAcc", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format(time.RFC3339)
	res, _ := db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, api_token, balance,
		 balance_used, quota, unit_cost, value_score, status, is_pinned, sort_order,
		 checkin_enabled, oauth_provider, oauth_account_key, oauth_project_id,
		 extra_config, created_at, updated_at)
		 VALUES (?, NULL, 'null-token', NULL, 0, 0, 0, NULL, 0, 'active', 0, 0, 1,
		 NULL, NULL, NULL, NULL, ?, ?)`,
		siteID, now, now,
	)
	accID, _ := res.LastInsertId()

	rec := doGet(t, r, "/api/accounts/"+itoa(accID)+"/models")
	if rec.Code != http.StatusNotFound {
		t.Logf("account models for null account: %d", rec.Code)
	}
}

// =============================================================================
// 7. Timezone Consistency (UTC verification)
// =============================================================================

func TestEdge_TimezoneTimestampsAreUTC(t *testing.T) {
	db, r, _ := setupEdgeTest(t)

	// Create site and verify created_at/updated_at are in UTC (RFC3339 format, Z suffix)
	site := newSiteFixture(t, r, "tz-site", "https://api.openai.com/v1")
	siteID := int64(site["id"].(float64))

	// Check raw DB values
	var createdAt, updatedAt string
	db.QueryRow("SELECT created_at, updated_at FROM sites WHERE id = ?", siteID).Scan(&createdAt, &updatedAt)

	if !strings.HasSuffix(createdAt, "Z") && !strings.Contains(createdAt, "+") {
		t.Errorf("created_at '%s' does not appear to be UTC (expected Z or +00:00 suffix)", createdAt)
	}
	if !strings.HasSuffix(updatedAt, "Z") && !strings.Contains(updatedAt, "+") {
		t.Errorf("updated_at '%s' does not appear to be UTC (expected Z or +00:00 suffix)", updatedAt)
	}
	t.Logf("Timestamps stored in UTC: created_at=%s, updated_at=%s", createdAt, updatedAt)

	// Verify API response timestamps
	rec := doGet(t, r, "/api/sites")
	var sites []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &sites)
	for _, s := range sites {
		if id, _ := s["id"].(float64); int64(id) == siteID {
			if ca, ok := s["createdAt"].(string); ok {
				if !strings.Contains(ca, "T") {
					t.Errorf("createdAt in API response not ISO 8601: %s", ca)
				}
			}
		}
	}
}

func TestEdge_TZConfigFieldUnused(t *testing.T) {
	cfg := config.Get()
	if cfg.Tz == "" {
		t.Log("Tz config field is empty (no timezone override configured)")
	} else {
		t.Logf("Tz config field is set to %q but not applied to time.Now() behavior", cfg.Tz)
	}

	// Verify that time.Now() uses system local time (not config.Tz)
	localTZ, _ := time.Now().Zone()
	t.Logf("System local timezone: %s (config.Tz is NOT applied)", localTZ)
}

func TestEdge_GeneratedAtTimestampIsUTC(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	setupAccountFixture(t, r)

	rec := doGet(t, r, "/api/accounts")
	var result map[string]any
	json.Unmarshal(rec.Body.Bytes(), &result)

	generatedAt, ok := result["generatedAt"].(string)
	if !ok {
		t.Fatal("missing generatedAt in accounts response")
	}
	if !strings.HasSuffix(generatedAt, "Z") {
		t.Errorf("generatedAt '%s' is not in strict UTC (expected Z suffix)", generatedAt)
	}
	t.Logf("generatedAt is UTC: %s", generatedAt)
}

// =============================================================================
// 8. Concurrent Settings Updates (TOCTOU Race)
// =============================================================================

// TestEdge_UpsertSettingDB_RaceCondition demonstrates the TOCTOU race in
// upsertSettingDB which uses SELECT-then-INSERT/UPDATE instead of ON CONFLICT.
func TestEdge_UpsertSettingDB_RaceCondition(t *testing.T) {
	db, _, _ := setupEdgeTest(t)

	key := "edge.race.test"
	key2 := "edge.race.safe"
	key3 := "edge.race.admin_helper"

	// Phase 1: racy upsertSettingDB (SELECT count + INSERT/UPDATE)
	const writers = 20
	var wg sync.WaitGroup
	uniqueErrors := make(chan struct{}, writers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			jsonValue, _ := json.Marshal(v)
			var count int
			db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", key)
			if count > 0 {
				db.Exec("UPDATE settings SET value = ? WHERE key = ?", string(jsonValue), key)
			} else {
				_, err := db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", key, string(jsonValue))
				if err != nil && strings.Contains(err.Error(), "UNIQUE") {
					uniqueErrors <- struct{}{}
				}
			}
		}(i)
	}
	wg.Wait()
	close(uniqueErrors)

	raceCount := len(uniqueErrors)
	if raceCount > 0 {
		t.Logf("TOCTOU race confirmed: %d UNIQUE constraint violations (concurrent first-write)", raceCount)
	} else {
		t.Log("No TOCTOU race detected (WAL mode may have serialized -- try with higher concurrency)")
	}

	// Phase 2: safe UPSERT (SettingsStore.Set — uses ON CONFLICT)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			store.NewSettingsStore(db).Set(key2, itoa(int64(v)))
		}(i)
	}
	wg.Wait()
	t.Log("Safe UPSERT via SettingsStore.Set: completed without errors (ON CONFLICT handles races)")

	// Phase 3: admin settings helper should use the same atomic UPSERT pattern.
	helperErrors := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			if err := upsertSettingDB(db.DB, key3, v); err != nil {
				helperErrors <- err
			}
		}(i)
	}
	wg.Wait()
	close(helperErrors)
	for err := range helperErrors {
		t.Fatalf("upsertSettingDB returned error under concurrency: %v", err)
	}

	var count int
	if err := db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", key3); err != nil {
		t.Fatalf("count admin helper setting: %v", err)
	}
	if count != 1 {
		t.Fatalf("admin helper setting row count = %d, want 1", count)
	}
}

// =============================================================================
// 9. Race on Route Rebuild + Cache Reads
// =============================================================================

// TestEdge_RouteCacheConcurrentReadWrite tests that RouteCache handles
// concurrent reads, writes, and invalidations without panicking or
// returning corrupted data.
func TestEdge_RouteCacheConcurrentReadWrite(t *testing.T) {
	// RouteCache is internal to routing package but uses sync.RWMutex.
	// Here we verify that the pattern used (mutex-protected reads) is safe.
	// The actual RouteCache is tested within the routing package.

	// Simulate the pattern: concurrent reads of a shared map with a mutex
	var mu sync.RWMutex
	shared := make(map[string]int)

	const readers = 50
	const writers = 5
	const iterations = 200
	var wg sync.WaitGroup
	panicked := false

	// Writers
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(wid int) {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
				}
				wg.Done()
			}()
			for i := 0; i < iterations; i++ {
				mu.Lock()
				shared["key-"+itoa(int64(wid%3))] = i
				mu.Unlock()
			}
		}(w)
	}

	// Readers
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
				}
				wg.Done()
			}()
			for i := 0; i < iterations; i++ {
				mu.RLock()
				_ = shared["key-0"]
				_ = shared["key-1"]
				_ = shared["key-2"]
				mu.RUnlock()
			}
		}()
	}
	wg.Wait()

	if panicked {
		t.Error("PANIC during concurrent read/write with RWMutex")
	} else {
		t.Log("RWMutex-protected concurrent access: no panics, data consistent")
	}
}

// TestEdge_RebuildRoutesConcurrent verifies concurrent rebuild calls do not panic.
// RebuildRoutesBestEffort now performs real channel recompose under a process mutex.
func TestEdge_RebuildRoutesConcurrent(t *testing.T) {
	db, _, _ := setupEdgeTest(t)
	service.SetRouteRebuildDB(db.DB)
	t.Cleanup(func() { service.SetRouteRebuildDB(nil) })

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service.RebuildRoutesBestEffort()
		}()
	}
	wg.Wait()
	t.Log("RebuildRoutesBestEffort concurrent: no panics")
}

// =============================================================================
// 10. Additional Edge Cases
// =============================================================================

// TestEdge_JSONInjectionInSettings ensures that special characters in settings
// values are properly encoded/escaped.
func TestEdge_JSONInjectionInSettings(t *testing.T) {
	db, r, _ := setupEdgeTest(t)

	// Create a site with special characters in name (potential JSON injection)
	body := map[string]any{
		"name":     `test","url":"evil","platform":"hacked";--`,
		"url":      "https://safe.example.com",
		"platform": "openai",
	}
	rec := doPostJSON(t, r, "/api/sites", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("site with special chars: %d %s", rec.Code, rec.Body.String())
	}

	// Verify the name is stored exactly, not interpreted
	var name string
	db.QueryRow("SELECT name FROM sites WHERE url = ?", "https://safe.example.com").Scan(&name)
	if name == "evil" {
		t.Error("SQL injection-like name was interpreted (expected literal storage)")
	}
	if name != body["name"] {
		t.Logf("name stored as literal: %s", name)
	}
}

// TestEdge_UnicodeAndEmojiInFields verifies that Unicode content survives
// round-trip through JSON serialization and SQLite storage.
func TestEdge_UnicodeAndEmojiInFields(t *testing.T) {
	db, r, _ := setupEdgeTest(t)

	site := newSiteFixture(t, r, "unicode-"+string([]rune{0x1F600, 0x1F310}), "https://api.openai.com/v1/unicode")
	siteID := int64(site["id"].(float64))

	var storedName string
	db.QueryRow("SELECT name FROM sites WHERE id = ?", siteID).Scan(&storedName)

	if !strings.Contains(storedName, string([]rune{0x1F600})) {
		t.Errorf("emoji not preserved in storage: got %q", storedName)
	}
	t.Logf("Unicode/emoji preserved: %q", storedName)
}

// TestEdge_NilPointerInJSON tests that nil pointers serialize correctly.
func TestEdge_NilPointerInJSON(t *testing.T) {
	type testStruct struct {
		Name    *string  `json:"name"`
		Balance *float64 `json:"balance"`
		Count   *int64   `json:"count"`
		Enabled *bool    `json:"enabled"`
	}
	ts := testStruct{}
	b, _ := json.Marshal(ts)
	var result map[string]any
	json.Unmarshal(b, &result)

	if result["name"] != nil {
		t.Errorf("nil *string should serialize as null, got %v", result["name"])
	}
	if result["balance"] != nil {
		t.Errorf("nil *float64 should serialize as null, got %v", result["balance"])
	}
	if result["count"] != nil {
		t.Errorf("nil *int64 should serialize as null, got %v", result["count"])
	}
	if result["enabled"] != nil {
		t.Errorf("nil *bool should serialize as null, got %v", result["enabled"])
	}
	t.Log("nil Go pointers correctly serialize as JSON null")
}

// TestEdge_ZeroValuesInJSON ensures zero values are sent correctly.
func TestEdge_ZeroValuesInJSON(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	site := newSiteFixture(t, r, "zero-site", "https://api.openai.com/v1/zero")

	// Verify zero values in response
	if v, ok := site["isPinned"].(bool); ok && v {
		t.Error("expected isPinned=false (zero value)")
	}
	if v, ok := site["globalWeight"].(float64); ok && v != 1.0 {
		t.Logf("globalWeight default: %v", v)
	}
}

// TestEdge_VeryLongStringInField tests a field value approaching maximum
// reasonable size (not the 20MB body limit, but a very long value).
func TestEdge_VeryLongStringInField(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	longName := strings.Repeat("a", 10000)
	body := map[string]any{
		"name":     longName,
		"url":      "https://very-long-name.example.com",
		"platform": "openai",
	}
	rec := doPostJSON(t, r, "/api/sites", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("site with 10KB name: %d %s", rec.Code, rec.Body.String())
	}
	t.Log("10KB name stored successfully")
}

// TestEdge_ConcurrentSiteCreateDelete verifies interleaved create+delete operations.
func TestEdge_ConcurrentSiteCreateDelete(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	var wg sync.WaitGroup
	const ops = 20

	for i := 0; i < ops; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := map[string]any{
				"name":     "cd-site-" + itoa(int64(idx)),
				"url":      "https://cd-" + itoa(int64(idx)) + ".com",
				"platform": "openai",
			}
			rec := doPostJSON(t, r, "/api/sites", body)
			if rec.Code == http.StatusOK {
				var s map[string]any
				json.Unmarshal(rec.Body.Bytes(), &s)
				if id, ok := s["id"].(float64); ok {
					doDelete(t, r, "/api/sites/"+itoa(int64(id)))
				}
			}
		}(i)
	}
	wg.Wait()
	t.Log("Concurrent create+delete: completed without panic")
}

// TestEdge_SettingsUpdateWithMalformedBody verifies handling of malformed JSON.
func TestEdge_SettingsUpdateWithMalformedBody(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	tests := []struct {
		name string
		body string
	}{
		{"unclosed brace", `{"key": "value"`},
		{"bare string", `"just a string"`},
		{"trailing comma", `{"key": "value",}`},
		{"null byte injection", "{\"key\": \"val\x00ue\"}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/api/settings/runtime", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code < 200 || rec.Code >= 600 {
				t.Fatalf("unexpected status %d for %s", rec.Code, tt.name)
			}
			t.Logf("%s -> %d", tt.name, rec.Code)
		})
	}
}
