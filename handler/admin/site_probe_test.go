package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/store"
)

func insertSiteProbeFixtures(t *testing.T, db *store.DB, siteURL string) (siteID, accountID, channelID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "ProbeWireSite", siteURL, "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ = res.LastInsertId()

	apiTok := "sk-probe-wire-token"
	res, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'active', 0, ?, ?)`, siteID, "probe-user", "session", apiTok, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ = res.LastInsertId()

	if _, err := db.Exec(`INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at)
		VALUES (?, ?, 1, 0, ?)`, accountID, "gpt-4o-mini", now); err != nil {
		t.Fatalf("insert model availability: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at)
		VALUES (?, ?, 1, 0, ?)`, accountID, "gpt-4o", now); err != nil {
		t.Fatalf("insert model availability 2: %v", err)
	}

	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES (?, ?, 'standard', 'weighted', 1, ?, ?)`, "gpt-*", "Probe Route", now, now)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled)
		VALUES (?, ?, ?, 10, 10, 1)`, routeID, accountID, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	channelID, _ = res.LastInsertId()
	return siteID, accountID, channelID
}

func setupSitesProbeHandler(t *testing.T) (*store.DB, *sitesHandler, chi.Router) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	h := &sitesHandler{db: db.DB}
	r := chi.NewRouter()
	// Register using the same handler instance so tests can inject transport.
	r.Get("/api/sites", h.listSites)
	r.Post("/api/sites/{id}/probe-now", h.probeNow)
	r.Get("/api/sites/{id}/probe-stream", h.probeStream)
	r.Get("/api/sites/{id}/available-models", h.getAvailableModels)
	r.Get("/api/sites/{id}/disabled-models", h.getDisabledModels)
	return db, h, r
}

func TestSites_ProbeNow_RealResultsWithFakeTransport(t *testing.T) {
	db, h, r := setupSitesProbeHandler(t)
	siteID, accountID, _ := insertSiteProbeFixtures(t, db, "https://upstream.probe.test")

	var hits atomic.Int32
	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits.Add(1)
		body := `{"id":"chatcmpl-probe","choices":[{"message":{"role":"assistant","content":"pong"}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/sites/"+itoa(siteID)+"/probe-now", map[string]any{"scope": "all"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("success=%v body=%s", out["success"], resp.Body.String())
	}
	total := int(out["totalModels"].(float64))
	if total < 1 {
		t.Fatalf("expected non-empty totalModels, got %v emptyReason=%v", out["totalModels"], out["emptyReason"])
	}
	if int(out["available"].(float64)) < 1 {
		t.Fatalf("expected available > 0, got %v results=%v", out["available"], out["results"])
	}
	if hits.Load() < 1 {
		t.Fatalf("expected fake transport hits, got %d", hits.Load())
	}

	// model_availability should be updated for the probed account/model.
	var available int
	if err := db.QueryRow(`SELECT available FROM model_availability WHERE account_id = ? AND model_name = 'gpt-4o-mini'`, accountID).Scan(&available); err != nil {
		t.Fatalf("load availability: %v", err)
	}
	if available != 1 {
		t.Fatalf("expected available=1 after successful probe, got %d", available)
	}
}

func TestSites_ProbeNow_EmptyReasonWhenNoAccounts(t *testing.T) {
	db, _, r := setupSitesProbeHandler(t)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "EmptySite", "https://empty.probe.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()

	resp := doPostJSON(t, r, "/api/sites/"+itoa(siteID)+"/probe-now", map[string]any{"scope": "all"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	json.Unmarshal(resp.Body.Bytes(), &out)
	if int(out["totalModels"].(float64)) != 0 {
		t.Fatalf("expected 0 models, got %v", out["totalModels"])
	}
	if out["emptyReason"] == nil || out["emptyReason"] == "" {
		t.Fatalf("expected emptyReason, got %v", out["emptyReason"])
	}
}

func TestSites_ProbeStream_EmitsProgressEvents(t *testing.T) {
	db, h, r := setupSitesProbeHandler(t)
	siteID, _, _ := insertSiteProbeFixtures(t, db, "https://upstream.stream.test")

	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"id":"chatcmpl-stream","choices":[{"message":{"role":"assistant","content":"ok"}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	req := httptest.NewRequest("GET", "/api/sites/"+itoa(siteID)+"/probe-stream?scope=all", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: start") && !strings.Contains(body, "event: probe-start") {
		t.Fatalf("expected start event, body=%s", body)
	}
	if !strings.Contains(body, "event: model") && !strings.Contains(body, "event: probe-model-result") {
		t.Fatalf("expected model progress event, body=%s", body)
	}
	if !strings.Contains(body, "event: complete") {
		t.Fatalf("expected complete event, body=%s", body)
	}
	// Must not be the old instant-zero complete-only stub.
	if strings.Count(body, "event:") < 3 {
		t.Fatalf("expected multi-event stream, body=%s", body)
	}
}

func TestSites_ProbeNow_UnsupportedDisablesModel(t *testing.T) {
	db, h, r := setupSitesProbeHandler(t)
	siteID, _, _ := insertSiteProbeFixtures(t, db, "https://upstream.fail.test")

	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"no such model"}`)),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/sites/"+itoa(siteID)+"/probe-now", map[string]any{
		"scope":     "single",
		"modelName": "gpt-4o-mini",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	json.Unmarshal(resp.Body.Bytes(), &out)
	if int(out["unsupported"].(float64)) < 1 {
		t.Fatalf("expected unsupported >= 1, got %v body=%s", out["unsupported"], resp.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM site_disabled_models WHERE site_id = ? AND model_name = 'gpt-4o-mini'`, siteID).Scan(&count); err != nil {
		t.Fatalf("count disabled: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected model auto-disabled, count=%d", count)
	}
}
