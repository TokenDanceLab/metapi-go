package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsageHeatmap_EmptyOK(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	_ = db
	req := httptest.NewRequest(http.MethodGet, "/api/stats/usage-heatmap?days=7&dimension=site", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSlowRequests_EmptyOK(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)
	// minimal site/account/log for ranking path
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		"hs", "https://h.test", "openai", "active", now, now)
	if err != nil {
		t.Fatal(err)
	}
	siteID, _ := res.LastInsertId()
	res, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		siteID, "u", "tok", "active", now, now)
	if err != nil {
		t.Fatal(err)
	}
	accountID, _ := res.LastInsertId()
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, latency_ms, created_at)
		VALUES (?,?,?,?,?,?)`, accountID, "gpt-4o", "gpt-4o", "success", 5000, now)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/stats/slow-requests?limit=10&minLatencyMs=1000&hours=24", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true {
		t.Fatalf("body=%s", rec.Body.String())
	}
	items, _ := body["items"].([]any)
	if len(items) < 1 {
		t.Fatalf("expected slow item, body=%s", rec.Body.String())
	}
}
