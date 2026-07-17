package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

// setupSitesTest creates an in-memory SQLite DB with chi router for sites.
func setupSitesTest(t *testing.T) (*store.DB, chi.Router) {
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

	r := chi.NewRouter()
	RegisterSitesRoutes(r, db.DB)
	return db, r
}

func setupSitesPostgresTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if strings.TrimSpace(dsn) == "" {
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
	RegisterSitesRoutes(r, db.DB)
	return db, r
}

// newSiteFixture inserts a site and returns the decoded response map.
func newSiteFixture(t *testing.T, r chi.Router, name, urlStr string) map[string]any {
	t.Helper()
	body := map[string]any{
		"name": name,
		"url":  urlStr,
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("fixture POST %s returned %d: %s", urlStr, resp.Code, resp.Body.String())
	}
	var m map[string]any
	json.Unmarshal(resp.Body.Bytes(), &m)
	return m
}

// ---- CRUD Tests ----

func TestSites_CreateAndList(t *testing.T) {
	_, r := setupSitesTest(t)

	// Create site
	body := map[string]any{
		"name": "OpenAI API",
		"url":  "https://api.openai.com",
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("create site: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created["name"] != "OpenAI API" {
		t.Errorf("expected name 'OpenAI API', got %v", created["name"])
	}
	if created["platform"] != "openai" {
		t.Errorf("expected platform 'openai', got %v", created["platform"])
	}

	// List sites
	resp = doGet(t, r, "/api/sites")
	if resp.Code != http.StatusOK {
		t.Fatalf("list sites: expected 200, got %d", resp.Code)
	}

	var sites []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &sites); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
}

func TestSites_CreateWithExplicitPlatform(t *testing.T) {
	_, r := setupSitesTest(t)

	body := map[string]any{
		"name":     "DeepSeek V3",
		"url":      "https://api.deepseek.com",
		"platform": "deepseek",
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", resp.Code, resp.Body.String())
	}

	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	if created["platform"] != "deepseek" {
		t.Errorf("expected platform 'deepseek', got %v", created["platform"])
	}
}

func TestSites_Postgres_CreateUpdateAndDisabledModels(t *testing.T) {
	db, r := setupSitesPostgresTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	siteURL := "https://pg-sites-" + suffix + ".example.com"
	endpointURL := siteURL + "/v1"
	updatedEndpointURL := siteURL + "/api"
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", siteURL)
	})

	resp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "PG Sites " + suffix,
		"url":      siteURL,
		"platform": "openai",
		"apiEndpoints": []map[string]any{
			{"url": endpointURL, "enabled": true, "sortOrder": 0},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("postgres create site: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	siteID, ok := created["id"].(float64)
	if !ok || siteID <= 0 {
		t.Fatalf("create response missing numeric id: %#v", created["id"])
	}
	pathID := itoa(int64(siteID))

	resp = doPutJSON(t, r, "/api/sites/"+pathID, map[string]any{
		"name": "PG Sites Updated " + suffix,
		"apiEndpoints": []map[string]any{
			{"url": updatedEndpointURL, "enabled": true, "sortOrder": 0},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("postgres update site: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	resp = doPutJSON(t, r, "/api/sites/"+pathID+"/disabled-models", map[string]any{
		"models": []string{"gpt-4o", "gpt-4o", " o3 "},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("postgres update disabled models: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var disabled map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &disabled); err != nil {
		t.Fatalf("unmarshal disabled models: %v", err)
	}
	models, ok := disabled["models"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("expected deduplicated disabled models, got %#v", disabled["models"])
	}
}

func TestSites_Create_Duplicate(t *testing.T) {
	_, r := setupSitesTest(t)
	newSiteFixture(t, r, "Site1", "https://api.openai.com")

	body := map[string]any{
		"name": "Site2",
		"url":  "https://api.openai.com",
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d: %s", resp.Code, resp.Body.String())
	}
	var err map[string]string
	json.Unmarshal(resp.Body.Bytes(), &err)
	if !strings.Contains(err["error"], "already exists") {
		t.Errorf("expected 'already exists' in error, got %q", err["error"])
	}
}

func TestSites_Create_EmptyName(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{"name": "  ", "url": "https://api.openai.com"}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestSites_Create_EmptyURL(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{"name": "Test", "url": ""}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestSites_CannotDetectPlatform(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{"name": "Unknown", "url": "https://completely-unknown-llm.example.com"}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for undetectable platform, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestSites_CreateDetectsAnyRouter(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{
		"name": "AnyRouter",
		"url":  "https://anyrouter.example.com",
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	if created["platform"] != "anyrouter" {
		t.Fatalf("platform = %v, want anyrouter", created["platform"])
	}
}

func TestSites_CreateWithAPIEndpoints(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{
		"name": "Multi Endpoint",
		"url":  "https://api.openai.com",
		"apiEndpoints": []map[string]any{
			{"url": "https://api.openai.com/v1", "enabled": true, "sortOrder": 0},
			{"url": "https://api2.openai.com/v1", "enabled": false, "sortOrder": 1},
		},
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	eps, ok := created["apiEndpoints"].([]any)
	if !ok {
		t.Fatal("expected apiEndpoints array in response")
	}
	if len(eps) != 2 {
		t.Errorf("expected 2 apiEndpoints, got %d", len(eps))
	}
}

func TestSites_CreateDuplicateAPIEndpointURL(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{
		"name": "Dup EP",
		"url":  "https://api.openai.com",
		"apiEndpoints": []map[string]any{
			{"url": "https://api.openai.com/v1", "enabled": true},
			{"url": "https://api.openai.com/v1", "enabled": true},
		},
	}
	resp := doPostJSON(t, r, "/api/sites", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate endpoint, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestSites_Update(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "Original", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{
		"name":   "Updated Name",
		"status": "disabled",
	}
	url := "/api/sites/" + itoa(siteID)
	resp := doPutJSON(t, r, url, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var updated map[string]any
	json.Unmarshal(resp.Body.Bytes(), &updated)
	if updated["name"] != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %v", updated["name"])
	}
	if updated["status"] != "disabled" {
		t.Errorf("expected status 'disabled', got %v", updated["status"])
	}
}

func TestSites_Update_NotFound(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{"name": "Ghost"}
	resp := doPutJSON(t, r, "/api/sites/99999", body)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestSites_Update_APIEndpointsFullReplace(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "EPSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// First, add endpoints
	body := map[string]any{
		"apiEndpoints": []map[string]any{
			{"url": "https://ep1.openai.com/v1", "enabled": true, "sortOrder": 0},
		},
	}
	resp := doPutJSON(t, r, "/api/sites/"+itoa(siteID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("update ep: %d %s", resp.Code, resp.Body.String())
	}

	// Then replace with different endpoints
	body2 := map[string]any{
		"apiEndpoints": []map[string]any{
			{"url": "https://ep2.openai.com/v1", "enabled": false, "sortOrder": 0},
		},
	}
	resp = doPutJSON(t, r, "/api/sites/"+itoa(siteID), body2)
	if resp.Code != http.StatusOK {
		t.Fatalf("replace ep: %d %s", resp.Code, resp.Body.String())
	}

	var updated map[string]any
	json.Unmarshal(resp.Body.Bytes(), &updated)
	eps, _ := updated["apiEndpoints"].([]any)
	if len(eps) != 1 {
		t.Errorf("expected 1 endpoint after full replace, got %d", len(eps))
	}
}

func TestSites_Update_InvalidStatus(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "StatusTest", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{"status": "invalid-status"}
	resp := doPutJSON(t, r, "/api/sites/"+itoa(siteID), body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestSites_Delete(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "ToDelete", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	resp := doDelete(t, r, "/api/sites/"+itoa(siteID))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	// Verify it's gone from list
	resp = doGet(t, r, "/api/sites")
	var sites []map[string]any
	json.Unmarshal(resp.Body.Bytes(), &sites)
	if len(sites) != 0 {
		t.Errorf("expected 0 sites after delete, got %d", len(sites))
	}
}

func TestSites_DeleteCascade(t *testing.T) {
	db, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "CascadeTest", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// Create accounts under this site
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	db.Exec("INSERT INTO accounts (site_id, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'sk1', 'active', 1, ?, ?)", siteID, now, now)
	db.Exec("INSERT INTO accounts (site_id, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'sk2', 'active', 1, ?, ?)", siteID, now, now)

	// Delete site
	resp := doDelete(t, r, "/api/sites/"+itoa(siteID))
	if resp.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", resp.Code)
	}

	// Verify accounts are cascade-deleted
	var count int
	db.QueryRow("SELECT COUNT(*) FROM accounts WHERE site_id = ?", siteID).Scan(&count)
	if count != 0 {
		t.Errorf("expected accounts to be cascade-deleted, got %d", count)
	}
}

// ---- Detect Tests ----

func TestSites_Detect_OpenAI(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]string{"url": "https://api.openai.com/v1"}
	resp := doPostJSON(t, r, "/api/sites/detect", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["platform"] != "openai" {
		t.Errorf("expected 'openai', got %v", result["platform"])
	}
}

func TestSites_Detect_AnyRouter(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]string{"url": "https://anyrouter.example.com"}
	resp := doPostJSON(t, r, "/api/sites/detect", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["platform"] != "anyrouter" {
		t.Fatalf("platform = %v, want anyrouter", result["platform"])
	}
}

func TestSites_Detect_Unknown(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]string{"url": "https://unknown.example.com"}
	resp := doPostJSON(t, r, "/api/sites/detect", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["error"] != "Could not detect platform" {
		t.Errorf("expected 'Could not detect platform' error, got %v", result)
	}
}

func TestSites_Detect_EmptyURL(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]string{"url": ""}
	resp := doPostJSON(t, r, "/api/sites/detect", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

// ---- Disabled Models Tests ----

func TestSites_DisabledModels_CRUD(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "DMSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	// Initially empty
	resp := doGet(t, r, "/api/sites/"+itoa(siteID)+"/disabled-models")
	if resp.Code != http.StatusOK {
		t.Fatalf("get disabled models: %d", resp.Code)
	}
	var getResp map[string]any
	json.Unmarshal(resp.Body.Bytes(), &getResp)
	models, _ := getResp["models"].([]any)
	if len(models) != 0 {
		t.Errorf("expected 0 disabled models initially, got %d", len(models))
	}

	// Set disabled models (full replace)
	putBody := map[string]any{"models": []string{"gpt-4", "gpt-3.5-turbo"}}
	resp = doPutJSON(t, r, "/api/sites/"+itoa(siteID)+"/disabled-models", putBody)
	if resp.Code != http.StatusOK {
		t.Fatalf("put disabled models: %d %s", resp.Code, resp.Body.String())
	}
	var putResp map[string]any
	json.Unmarshal(resp.Body.Bytes(), &putResp)
	models, _ = putResp["models"].([]any)
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}

	// Replace with new set
	putBody2 := map[string]any{"models": []string{"claude-3-opus"}}
	resp = doPutJSON(t, r, "/api/sites/"+itoa(siteID)+"/disabled-models", putBody2)
	if resp.Code != http.StatusOK {
		t.Fatalf("replace disabled models: %d", resp.Code)
	}
	json.Unmarshal(resp.Body.Bytes(), &putResp)
	models, _ = putResp["models"].([]any)
	if len(models) != 1 {
		t.Errorf("expected 1 model after replace, got %d", len(models))
	}

	// Verify get after replace
	resp = doGet(t, r, "/api/sites/"+itoa(siteID)+"/disabled-models")
	json.Unmarshal(resp.Body.Bytes(), &getResp)
	models, _ = getResp["models"].([]any)
	if len(models) != 1 {
		t.Errorf("expected 1 model after replace on get, got %d", len(models))
	}
}

func TestSites_DisabledModels_Deduplication(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "DedupSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{"models": []string{"gpt-4", "gpt-4", "  gpt-4  "}}
	resp := doPutJSON(t, r, "/api/sites/"+itoa(siteID)+"/disabled-models", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("put: %d", resp.Code)
	}
	var putResp map[string]any
	json.Unmarshal(resp.Body.Bytes(), &putResp)
	models, _ := putResp["models"].([]any)
	if len(models) != 1 {
		t.Errorf("expected 1 model after deduplication, got %d", len(models))
	}
}

func TestSites_DisabledModels_NotFound(t *testing.T) {
	_, r := setupSitesTest(t)
	resp := doGet(t, r, "/api/sites/99999/disabled-models")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

// ---- Available Models Tests ----

func TestSites_AvailableModels_Empty(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "AMSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	resp := doGet(t, r, "/api/sites/"+itoa(siteID)+"/available-models")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	models, _ := result["models"].([]any)
	if len(models) != 0 {
		t.Errorf("expected 0 available models, got %d", len(models))
	}
}

func TestSites_AvailableModels_WithData(t *testing.T) {
	db, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "RichSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Create an account
	res, _ := db.Exec("INSERT INTO accounts (site_id, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'sk1', 'active', 1, ?, ?)", siteID, now, now)
	accountID, _ := res.LastInsertId()

	// Add model availability
	db.Exec("INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at) VALUES (?, ?, 1, 0, ?)", accountID, "gpt-4", now)
	db.Exec("INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at) VALUES (?, ?, 1, 0, ?)", accountID, "gpt-3.5-turbo", now)
	db.Exec("INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at) VALUES (?, ?, 0, 0, ?)", accountID, "unavailable-model", now)

	resp := doGet(t, r, "/api/sites/"+itoa(siteID)+"/available-models")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	models, _ := result["models"].([]any)
	if len(models) != 2 {
		t.Errorf("expected 2 available models (excluding unavailable), got %d", len(models))
	}
}

// ---- Probe Tests ----

func TestSites_ProbeNow(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "ProbeSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	body := map[string]any{"scope": "all"}
	resp := doPostJSON(t, r, "/api/sites/"+itoa(siteID)+"/probe-now", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

func TestSites_ProbeNow_InvalidID(t *testing.T) {
	_, r := setupSitesTest(t)
	resp := doPostJSON(t, r, "/api/sites/not-a-number/probe-now", map[string]any{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestSites_ProbeStream(t *testing.T) {
	_, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "SSESite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	req := httptest.NewRequest("GET", "/api/sites/"+itoa(siteID)+"/probe-stream?scope=all", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", contentType)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "probe-start") {
		t.Error("expected probe-start event")
	}
	if !strings.Contains(body, "complete") {
		t.Error("expected complete event")
	}
}

// ---- Batch Tests ----

func TestSites_Batch_Enable(t *testing.T) {
	_, r := setupSitesTest(t)
	s1 := newSiteFixture(t, r, "Batch1", "https://api.openai.com")
	s2 := newSiteFixture(t, r, "Batch2", "https://api.deepseek.com")

	body := map[string]any{
		"ids":    []int{int(s1["id"].(float64)), int(s2["id"].(float64))},
		"action": "disable",
	}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch disable: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	if result["success"] != true {
		t.Error("expected success=true")
	}
	ids, _ := result["successIds"].([]any)
	if len(ids) != 2 {
		t.Errorf("expected 2 successIds, got %d", len(ids))
	}
}

func TestSites_Batch_Delete(t *testing.T) {
	_, r := setupSitesTest(t)
	s1 := newSiteFixture(t, r, "Del1", "https://api.openai.com")
	s2 := newSiteFixture(t, r, "Del2", "https://api.deepseek.com")

	body := map[string]any{
		"ids":    []int{int(s1["id"].(float64)), int(s2["id"].(float64))},
		"action": "delete",
	}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch delete: %d %s", resp.Code, resp.Body.String())
	}

	// Verify they're gone
	resp = doGet(t, r, "/api/sites")
	var sites []map[string]any
	json.Unmarshal(resp.Body.Bytes(), &sites)
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestSites_Batch_EnableSystemProxy(t *testing.T) {
	_, r := setupSitesTest(t)
	s1 := newSiteFixture(t, r, "Proxy1", "https://api.openai.com")

	body := map[string]any{
		"ids":    []int{int(s1["id"].(float64))},
		"action": "enableSystemProxy",
	}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("enableSystemProxy: %d %s", resp.Code, resp.Body.String())
	}
}

func TestSites_Batch_DisableSystemProxy(t *testing.T) {
	_, r := setupSitesTest(t)
	s1 := newSiteFixture(t, r, "Proxy2", "https://api.openai.com")

	body := map[string]any{
		"ids":    []int{int(s1["id"].(float64))},
		"action": "disableSystemProxy",
	}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("disableSystemProxy: %d %s", resp.Code, resp.Body.String())
	}
}

func TestSites_Batch_EmptyIDs(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{"ids": []int{}, "action": "enable"}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestSites_Batch_InvalidAction(t *testing.T) {
	_, r := setupSitesTest(t)
	body := map[string]any{"ids": []int{1}, "action": "invalidAction"}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
}

func TestSites_Batch_PartialFailure(t *testing.T) {
	_, r := setupSitesTest(t)
	s1 := newSiteFixture(t, r, "PartialOK", "https://api.openai.com")

	body := map[string]any{
		"ids":    []int{int(s1["id"].(float64)), 99999},
		"action": "enable",
	}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch partial: %d %s", resp.Code, resp.Body.String())
	}

	var result map[string]any
	json.Unmarshal(resp.Body.Bytes(), &result)
	ids, _ := result["successIds"].([]any)
	fails, _ := result["failedItems"].([]any)
	if len(ids) != 1 {
		t.Errorf("expected 1 success, got %d", len(ids))
	}
	if len(fails) != 1 {
		t.Errorf("expected 1 failure, got %d", len(fails))
	}
}

// ---- Probe Stream Events Test ----

func TestSites_ProbeStream_InvalidID(t *testing.T) {
	_, r := setupSitesTest(t)
	req := httptest.NewRequest("GET", "/api/sites/not-a-number/probe-stream", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// ---- Status Side Effects Tests ----

func TestSites_StatusSideEffects_Disable(t *testing.T) {
	db, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "SideEffectSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	db.Exec("INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'acc1', 'sk1', 'active', 1, ?, ?)", siteID, now, now)

	// Disable site via update
	body := map[string]any{"status": "disabled"}
	resp := doPutJSON(t, r, "/api/sites/"+itoa(siteID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("disable site: %d %s", resp.Code, resp.Body.String())
	}

	// Verify account is disabled
	var accStatus string
	db.QueryRow("SELECT status FROM accounts WHERE site_id = ?", siteID).Scan(&accStatus)
	if accStatus != "disabled" {
		t.Errorf("expected account status 'disabled', got %q", accStatus)
	}

	// Verify event was created
	var eventCount int
	db.QueryRow("SELECT COUNT(*) FROM events WHERE related_id = ? AND related_type = 'site' AND level = 'warning'", siteID).Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("expected 1 warning event, got %d", eventCount)
	}
}

func TestSites_StatusSideEffects_Enable(t *testing.T) {
	db, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "EnableSite", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	db.Exec("INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'acc1', 'sk1', 'disabled', 1, ?, ?)", siteID, now, now)

	// Enable site via update
	body := map[string]any{"status": "active"}
	resp := doPutJSON(t, r, "/api/sites/"+itoa(siteID), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("enable site: %d %s", resp.Code, resp.Body.String())
	}

	// Verify account is re-enabled (it was previously disabled)
	var accStatus string
	db.QueryRow("SELECT status FROM accounts WHERE site_id = ?", siteID).Scan(&accStatus)
	if accStatus != "active" {
		t.Errorf("expected account status 'active' after site enable, got %q", accStatus)
	}

	// Verify info event was created
	var eventCount int
	db.QueryRow("SELECT COUNT(*) FROM events WHERE related_id = ? AND related_type = 'site' AND level = 'info'", siteID).Scan(&eventCount)
	if eventCount != 1 {
		t.Errorf("expected 1 info event, got %d", eventCount)
	}
}

// ---- Batch Status Side Effects ----

func TestSites_BatchDisable_WithStatusSideEffects(t *testing.T) {
	db, r := setupSitesTest(t)
	site := newSiteFixture(t, r, "BatchDisable", "https://api.openai.com")
	siteID := int64(site["id"].(float64))

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	db.Exec("INSERT INTO accounts (site_id, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, 'sk1', 'active', 1, ?, ?)", siteID, now, now)

	body := map[string]any{"ids": []int{int(siteID)}, "action": "disable"}
	resp := doPostJSON(t, r, "/api/sites/batch", body)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch disable: %d", resp.Code)
	}

	var accStatus string
	db.QueryRow("SELECT status FROM accounts WHERE site_id = ?", siteID).Scan(&accStatus)
	if accStatus != "disabled" {
		t.Errorf("expected 'disabled', got %q", accStatus)
	}
}

// ---- HTTP Helpers ----

func doGet(t *testing.T, r chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doDelete(t *testing.T, r chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("DELETE", path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doPostJSON(t *testing.T, r chi.Router, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest("POST", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func doPutJSON(t *testing.T, r chi.Router, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest("PUT", path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func itoa(i int64) string {
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}

func TestSites_MaxConcurrencyRoundTrip(t *testing.T) {
	_, r := setupSitesTest(t)

	// Create with maxConcurrency
	resp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":           "Conc Site",
		"url":            "https://conc.example.com",
		"platform":       "openai",
		"maxConcurrency": 3,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if got := int64(created["maxConcurrency"].(float64)); got != 3 {
		t.Fatalf("create maxConcurrency=%v, want 3", created["maxConcurrency"])
	}
	id := int64(created["id"].(float64))

	// List should include field
	resp = doGet(t, r, "/api/sites")
	if resp.Code != http.StatusOK {
		t.Fatalf("list: %d", resp.Code)
	}
	var sites []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &sites); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(sites) != 1 || int64(sites[0]["maxConcurrency"].(float64)) != 3 {
		t.Fatalf("list maxConcurrency missing/wrong: %+v", sites)
	}

	// Update to unlimited (0)
	resp = doPutJSON(t, r, "/api/sites/"+strconv.FormatInt(id, 10), map[string]any{
		"maxConcurrency": 0,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	if got := int64(updated["maxConcurrency"].(float64)); got != 0 {
		t.Fatalf("update maxConcurrency=%v, want 0", updated["maxConcurrency"])
	}

	// Negative rejected
	resp = doPutJSON(t, r, "/api/sites/"+strconv.FormatInt(id, 10), map[string]any{
		"maxConcurrency": -1,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("negative maxConcurrency: expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestSites_MutationsInvalidateAccountsSnapshotAndRouteCache(t *testing.T) {
	_, r := setupSitesTest(t)

	// Warm package-level accounts snapshot cache (cleared via registered hook).
	globalAccountsCache = &accountsSnapshotCache{ttl: 30 * time.Second}
	globalAccountsCache.set([]byte(`{"accounts":[],"sites":[]}`))
	if !globalAccountsCache.isValid() {
		t.Fatal("accounts snapshot should be warm")
	}

	rc := routing.NewRouteCache(5_000)
	rc.SetRoutes([]store.TokenRoute{{ID: 7, ModelPattern: "gpt-*"}})
	routing.SetGlobalCache(rc)
	t.Cleanup(func() { routing.SetGlobalCache(nil) })
	if rc.GetRoutes() == nil {
		t.Fatal("route cache should be warm")
	}

	site := newSiteFixture(t, r, "CacheSite", "https://api.openai.com/v1")
	id := int64(site["id"].(float64))

	// Create path must clear both caches.
	if globalAccountsCache.isValid() {
		t.Fatal("accounts snapshot still warm after site create")
	}
	if rc.GetRoutes() != nil {
		t.Fatal("route cache still warm after site create")
	}

	// Re-warm and update (non-status field) - must still invalidate.
	globalAccountsCache.set([]byte(`{"accounts":[],"sites":[]}`))
	rc.SetRoutes([]store.TokenRoute{{ID: 8, ModelPattern: "claude-*"}})
	resp := doPutJSON(t, r, "/api/sites/"+strconv.FormatInt(id, 10), map[string]any{
		"name": "CacheSite-renamed",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.Code, resp.Body.String())
	}
	if globalAccountsCache.isValid() {
		t.Fatal("accounts snapshot still warm after site update")
	}
	if rc.GetRoutes() != nil {
		t.Fatal("route cache still warm after site update")
	}

	// Re-warm and delete.
	globalAccountsCache.set([]byte(`{"accounts":[],"sites":[]}`))
	rc.SetRoutes([]store.TokenRoute{{ID: 9, ModelPattern: "gemini-*"}})
	del := doDelete(t, r, "/api/sites/"+strconv.FormatInt(id, 10))
	if del.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
	if globalAccountsCache.isValid() {
		t.Fatal("accounts snapshot still warm after site delete")
	}
	if rc.GetRoutes() != nil {
		t.Fatal("route cache still warm after site delete")
	}
}
