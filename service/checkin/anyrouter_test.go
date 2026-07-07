package checkin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func TestCheckinAccount_AnyRouterUsesPlatformAdapter(t *testing.T) {
	selfCalls := 0
	checkinCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self":
			selfCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"id":           42,
					"username":     "anyrouter-user",
					"quota":        1_000_000,
					"used_quota":   250_000,
					"today_income": 50_000,
				},
			})
		case "/api/user/checkin":
			checkinCalls++
			if got := r.Header.Get("New-API-User"); got != "42" {
				t.Fatalf("New-API-User header = %q, want discovered user id 42", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": "checkin success",
				"data":    map[string]any{"reward": 0.1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertAnyRouterSite(t, db, server.URL, now)
	accountID := insertAnyRouterAccount(t, db, siteID, now)

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true, ScheduleMode: "cron"})
	if !result.Success || result.Status != CheckinSuccess {
		t.Fatalf("CheckinAccount result = %+v, want success", result)
	}
	if checkinCalls != 1 {
		t.Fatalf("checkinCalls = %d, want 1", checkinCalls)
	}
	if selfCalls < 2 {
		t.Fatalf("selfCalls = %d, want discovery plus balance refresh", selfCalls)
	}

	var lastCheckinAt *string
	if err := db.Get(&lastCheckinAt, "SELECT last_checkin_at FROM accounts WHERE id = ?", accountID); err != nil {
		t.Fatalf("read last_checkin_at: %v", err)
	}
	if lastCheckinAt == nil || *lastCheckinAt == "" {
		t.Fatal("last_checkin_at was not advanced")
	}

	var logCount int
	if err := db.Get(&logCount, "SELECT COUNT(*) FROM checkin_logs WHERE account_id = ? AND status = ?", accountID, "success"); err != nil {
		t.Fatalf("count checkin logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("success log count = %d, want 1", logCount)
	}

	var balance, used, quota float64
	if err := db.QueryRow("SELECT balance, balance_used, quota FROM accounts WHERE id = ?", accountID).Scan(&balance, &used, &quota); err != nil {
		t.Fatalf("read balance fields: %v", err)
	}
	if balance != 2 || used != 0.5 || quota != 2.5 {
		t.Fatalf("balance fields = (%v,%v,%v), want (2,0.5,2.5)", balance, used, quota)
	}
}

func TestCheckinAccount_UsesSiteCustomHeaders(t *testing.T) {
	selfHeaderSeen := false
	checkinHeaderSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self":
			if r.Header.Get("X-Metapi-Site") == "site-header" {
				selfHeaderSeen = true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"id":         42,
					"username":   "anyrouter-user",
					"quota":      1_000_000,
					"used_quota": 250_000,
				},
			})
		case "/api/user/checkin":
			if r.Header.Get("X-Metapi-Site") == "site-header" {
				checkinHeaderSeen = true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"message": "checkin success",
				"data":    map[string]any{"reward": 0.1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertAnyRouterSiteWithCustomHeaders(t, db, server.URL, `{"X-Metapi-Site":"site-header"}`, now)
	accountID := insertAnyRouterAccount(t, db, siteID, now)

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true, ScheduleMode: "cron"})
	if !result.Success || result.Status != CheckinSuccess {
		t.Fatalf("CheckinAccount result = %+v, want success", result)
	}
	if !selfHeaderSeen {
		t.Fatal("site custom header was not sent to /api/user/self")
	}
	if !checkinHeaderSeen {
		t.Fatal("site custom header was not sent to /api/user/checkin")
	}
}

func TestCheckinAccount_AnyRouterAPIKeyAccountIsSkipped(t *testing.T) {
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertAnyRouterSite(t, db, server.URL, now)
	accountID := insertAnyRouterAPIKeyAccount(t, db, siteID, now)

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true, ScheduleMode: "manual"})
	if !result.Success || result.Status != CheckinSkipped || result.Reason != "checkin_not_supported" {
		t.Fatalf("CheckinAccount result = %+v, want skipped unsupported capability", result)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
	}

	var logCount int
	if err := db.Get(&logCount, "SELECT COUNT(*) FROM checkin_logs WHERE account_id = ? AND status = ? AND message = ?",
		accountID, "skipped", "account credential mode does not support checkin"); err != nil {
		t.Fatalf("count checkin logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("skipped log count = %d, want 1", logCount)
	}
}

func TestCheckinAccount_LegacyMirroredAPIKeyAccountIsSkipped(t *testing.T) {
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertAnyRouterSite(t, db, server.URL, now)
	accountID := insertAnyRouterAPIKeyAccount(t, db, siteID, now)
	if _, err := db.Exec("UPDATE accounts SET extra_config = NULL WHERE id = ?", accountID); err != nil {
		t.Fatalf("clear credentialMode: %v", err)
	}

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true, ScheduleMode: "manual"})
	if !result.Success || result.Status != CheckinSkipped || result.Reason != "checkin_not_supported" {
		t.Fatalf("CheckinAccount result = %+v, want skipped unsupported capability", result)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
	}
}

func TestCheckinAccount_DisabledAnyRouterAccountIsSkippedWithoutUpstream(t *testing.T) {
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteID := insertAnyRouterSite(t, db, server.URL, now)
	accountID := insertAnyRouterAccount(t, db, siteID, now)
	if _, err := db.Exec("UPDATE accounts SET status = 'DISABLED' WHERE id = ?", accountID); err != nil {
		t.Fatalf("disable account: %v", err)
	}

	result := CheckinAccount(&config.Config{}, db.DB, accountID, &CheckinOptions{SkipEvent: true, ScheduleMode: "manual"})
	if !result.Success || result.Status != CheckinSkipped || result.Reason != "account_disabled" {
		t.Fatalf("CheckinAccount result = %+v, want skipped account_disabled", result)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
	}

	var logCount int
	if err := db.Get(&logCount, "SELECT COUNT(*) FROM checkin_logs WHERE account_id = ? AND status = ? AND message = ?",
		accountID, "skipped", "account disabled"); err != nil {
		t.Fatalf("count checkin logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("skipped log count = %d, want 1", logCount)
	}

	var extraConfig *string
	if err := db.Get(&extraConfig, "SELECT extra_config FROM accounts WHERE id = ?", accountID); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	if extraConfig == nil {
		t.Fatal("extra_config is nil; want runtimeHealth")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*extraConfig), &cfg); err != nil {
		t.Fatalf("unmarshal extra_config: %v", err)
	}
	health, ok := cfg["runtimeHealth"].(map[string]any)
	if !ok || health["state"] != "disabled" || health["source"] != "checkin" {
		t.Fatalf("runtimeHealth = %#v, want disabled/checkin", cfg["runtimeHealth"])
	}
}

func insertAnyRouterSite(t *testing.T, db *store.DB, url, now string) int64 {
	t.Helper()
	result, err := db.Exec(
		"INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)",
		"AnyRouter test", url, "anyrouter", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	return id
}

func insertAnyRouterSiteWithCustomHeaders(t *testing.T, db *store.DB, url, customHeaders, now string) int64 {
	t.Helper()
	result, err := db.Exec(
		"INSERT INTO sites (name, url, platform, custom_headers, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?)",
		"AnyRouter headers test", url, "anyrouter", customHeaders, now, now,
	)
	if err != nil {
		t.Fatalf("insert site with custom headers: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	return id
}

func insertAnyRouterAPIKeyAccount(t *testing.T, db *store.DB, siteID int64, now string) int64 {
	t.Helper()
	extraConfig := `{"credentialMode":"apikey"}`
	result, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, extra_config, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?)",
		siteID, nil, "anyrouter-api-key", "anyrouter-api-key", true, extraConfig, now, now,
	)
	if err != nil {
		t.Fatalf("insert api-key account: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("api-key account LastInsertId: %v", err)
	}
	return id
}

func insertAnyRouterAccount(t *testing.T, db *store.DB, siteID int64, now string) int64 {
	t.Helper()
	result, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?, ?)",
		siteID, "anyrouter-user", "session-token", true, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}
	return id
}
