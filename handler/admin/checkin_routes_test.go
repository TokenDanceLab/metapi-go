package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func setupCheckinRoutesTest(t *testing.T) (*store.DB, chi.Router, *config.Config) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}

	cfg := &config.Config{
		CheckinCron:          config.DefaultCheckinCron,
		CheckinScheduleMode:  "cron",
		CheckinIntervalHours: config.DefaultCheckinIntervalHours,
	}
	r := chi.NewRouter()
	RegisterCheckinRoutes(r, db.DB, cfg)
	return db, r, cfg
}

func TestCheckinTriggerOneRunsAnyRouterCheckin(t *testing.T) {
	db, r, _ := setupCheckinRoutesTest(t)
	upstream, calls := newCheckinAnyRouterServer(t)
	siteID := insertCheckinRouteSite(t, db, upstream.URL)
	accountID := insertCheckinRouteAccount(t, db, siteID, true)

	resp := doPostJSON(t, r, "/api/checkin/trigger/"+itoa(accountID), nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("trigger one: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result["success"] != true {
		t.Fatalf("success = %v, response=%v", result["success"], result)
	}
	if result["status"] != "success" {
		t.Fatalf("status = %v, want success", result["status"])
	}
	if *calls != 1 {
		t.Fatalf("checkin calls = %d, want 1", *calls)
	}

	var logCount int
	if err := db.Get(&logCount, "SELECT COUNT(*) FROM checkin_logs WHERE account_id = ? AND status = ?", accountID, "success"); err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("success log count = %d, want 1", logCount)
	}
}

func TestCheckinTriggerAllSkipsDisabledCheckinAccounts(t *testing.T) {
	db, r, _ := setupCheckinRoutesTest(t)
	upstream, calls := newCheckinAnyRouterServer(t)
	siteID := insertCheckinRouteSite(t, db, upstream.URL)
	insertCheckinRouteAccount(t, db, siteID, true)
	insertCheckinRouteAccount(t, db, siteID, false)

	resp := doPostJSON(t, r, "/api/checkin/trigger", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("trigger all: %d %s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	summary, ok := result["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %v", result)
	}
	if summary["total"] != float64(1) || summary["success"] != float64(1) || summary["failed"] != float64(0) {
		t.Fatalf("summary = %#v, want one successful eligible account", summary)
	}
	if *calls != 1 {
		t.Fatalf("checkin calls = %d, want 1", *calls)
	}
}

func TestCheckinTriggerOneNotFound(t *testing.T) {
	_, r, _ := setupCheckinRoutesTest(t)

	resp := doPostJSON(t, r, "/api/checkin/trigger/99999", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", resp.Code, resp.Body.String())
	}
}

func TestCheckinUpdateSchedulePersistsSettings(t *testing.T) {
	db, r, cfg := setupCheckinRoutesTest(t)

	resp := doPutJSON(t, r, "/api/checkin/schedule", map[string]any{
		"mode":          "interval",
		"cron":          "15 9 * * *",
		"intervalHours": 6,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update schedule: %d %s", resp.Code, resp.Body.String())
	}

	if cfg.CheckinScheduleMode != "interval" {
		t.Fatalf("cfg mode = %q, want interval", cfg.CheckinScheduleMode)
	}
	if cfg.CheckinCron != "15 9 * * *" {
		t.Fatalf("cfg cron = %q, want updated cron", cfg.CheckinCron)
	}
	if cfg.CheckinIntervalHours != 6 {
		t.Fatalf("cfg interval = %d, want 6", cfg.CheckinIntervalHours)
	}

	settings := map[string]string{}
	rows, err := db.Query("SELECT key, value FROM settings WHERE key IN (?, ?, ?)",
		"checkin_schedule_mode", "checkin_cron", "checkin_interval_hours")
	if err != nil {
		t.Fatalf("query settings: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatalf("scan setting: %v", err)
		}
		settings[key] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("settings rows: %v", err)
	}
	if settings["checkin_schedule_mode"] != `"interval"` {
		t.Fatalf("stored mode = %q, want JSON string interval", settings["checkin_schedule_mode"])
	}
	if settings["checkin_cron"] != `"15 9 * * *"` {
		t.Fatalf("stored cron = %q, want JSON string cron", settings["checkin_cron"])
	}
	if settings["checkin_interval_hours"] != `6` {
		t.Fatalf("stored interval = %q, want JSON number 6", settings["checkin_interval_hours"])
	}
}

func TestCheckinUpdateScheduleRejectsInvalidMode(t *testing.T) {
	_, r, _ := setupCheckinRoutesTest(t)

	resp := doPutJSON(t, r, "/api/checkin/schedule", map[string]any{"mode": "daily"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

func TestCheckinUpdateScheduleRejectsInvalidInterval(t *testing.T) {
	_, r, _ := setupCheckinRoutesTest(t)

	resp := doPutJSON(t, r, "/api/checkin/schedule", map[string]any{
		"mode":          "interval",
		"intervalHours": 0,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

func TestCheckinUpdateScheduleRejectsInvalidCron(t *testing.T) {
	_, r, _ := setupCheckinRoutesTest(t)

	resp := doPutJSON(t, r, "/api/checkin/schedule", map[string]any{
		"mode": "cron",
		"cron": "bad cron",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

func newCheckinAnyRouterServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	checkinCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"username":"anyrouter-user"}}`))
		case "/api/user/checkin":
			checkinCalls++
			if r.Header.Get("Authorization") != "Bearer session-token" {
				http.Error(w, `{"success":false,"message":"bad token"}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"message":"checkin success","data":{"reward":"5"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &checkinCalls
}

func insertCheckinRouteSite(t *testing.T, db *store.DB, siteURL string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	siteRes, err := db.Exec(
		"INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, 'anyrouter', 'active', ?, ?)",
		"AnyRouter Checkin", siteURL, now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := siteRes.LastInsertId()
	if err != nil {
		t.Fatalf("site id: %v", err)
	}
	return siteID
}

func insertCheckinRouteAccount(t *testing.T, db *store.DB, siteID int64, checkinEnabled bool) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	accountRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?, ?)",
		siteID, "anyrouter-user", "session-token", checkinEnabled, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := accountRes.LastInsertId()
	if err != nil {
		t.Fatalf("account id: %v", err)
	}
	return accountID
}
