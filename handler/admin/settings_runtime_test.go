package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSettingsRuntimeUpdateCheckinSchedulePersistsSettings(t *testing.T) {
	db, r, cfg := setupEdgeTest(t)

	resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"checkinScheduleMode":  "interval",
		"checkinCron":          "15 9 * * *",
		"checkinIntervalHours": 6,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update runtime: %d %s", resp.Code, resp.Body.String())
	}
	if cfg.CheckinScheduleMode != "interval" || cfg.CheckinCron != "15 9 * * *" || cfg.CheckinIntervalHours != 6 {
		t.Fatalf("cfg schedule = (%q, %q, %d), want (interval, 15 9 * * *, 6)",
			cfg.CheckinScheduleMode, cfg.CheckinCron, cfg.CheckinIntervalHours)
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

func TestSettingsRuntimeUpdateCheckinScheduleRejectsInvalidCron(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"checkinCron": "bad cron",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

func TestSettingsRuntimeUpdateCheckinScheduleRejectsFractionalInterval(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"checkinIntervalHours": 1.5,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

func TestSettingsRuntimeGlobalAllowedModelsPersistsAndNormalizes(t *testing.T) {
	db, r, cfg := setupEdgeTest(t)

	resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"globalAllowedModels": []any{"gpt-4o", " claude-3.7-sonnet ", "gpt-4o", "", "gemini-pro"},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("update runtime: %d %s", resp.Code, resp.Body.String())
	}

	want := []string{"gpt-4o", "claude-3.7-sonnet", "gemini-pro"}
	if len(cfg.GlobalAllowedModels) != len(want) {
		t.Fatalf("cfg.GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
	}
	for i := range want {
		if cfg.GlobalAllowedModels[i] != want[i] {
			t.Fatalf("cfg.GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
		}
	}

	var stored string
	if err := db.Get(&stored, "SELECT value FROM settings WHERE key = ?", "global_allowed_models"); err != nil {
		t.Fatalf("query stored whitelist: %v", err)
	}
	if stored != `["gpt-4o","claude-3.7-sonnet","gemini-pro"]` {
		t.Fatalf("stored whitelist = %q, want normalized JSON array", stored)
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	models, ok := body["globalAllowedModels"].([]any)
	if !ok {
		t.Fatalf("response globalAllowedModels type = %T, want []any", body["globalAllowedModels"])
	}
	if len(models) != len(want) {
		t.Fatalf("response globalAllowedModels = %#v, want %#v", models, want)
	}
}

func TestSettingsRuntimeGlobalAllowedModelsExplicitEmptyClears(t *testing.T) {
	db, r, cfg := setupEdgeTest(t)

	if resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"globalAllowedModels": []any{"keep-me", "also-keep"},
	}); resp.Code != http.StatusOK {
		t.Fatalf("seed whitelist: %d %s", resp.Code, resp.Body.String())
	}

	resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"globalAllowedModels": []any{},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("clear whitelist: %d %s", resp.Code, resp.Body.String())
	}
	if len(cfg.GlobalAllowedModels) != 0 {
		t.Fatalf("cfg.GlobalAllowedModels = %#v, want empty after explicit clear", cfg.GlobalAllowedModels)
	}
	var stored string
	if err := db.Get(&stored, "SELECT value FROM settings WHERE key = ?", "global_allowed_models"); err != nil {
		t.Fatalf("query stored whitelist: %v", err)
	}
	if stored != `[]` {
		t.Fatalf("stored whitelist = %q, want []", stored)
	}
}

func TestSettingsRuntimeGlobalAllowedModelsRejectsNullAndNonArray(t *testing.T) {
	db, r, cfg := setupEdgeTest(t)
	cfg.GlobalAllowedModels = []string{"must-survive"}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`,
		"global_allowed_models", `["must-survive"]`); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	for _, body := range []map[string]any{
		{"globalAllowedModels": nil},
		{"globalAllowedModels": "gpt-4o"},
		{"globalAllowedModels": map[string]any{"model": "gpt-4o"}},
		{"globalAllowedModels": []any{"ok", 1}},
	} {
		resp := doPutJSON(t, r, "/api/settings/runtime", body)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("body=%v status=%d, want 400; resp=%s", body, resp.Code, resp.Body.String())
		}
	}

	if len(cfg.GlobalAllowedModels) != 1 || cfg.GlobalAllowedModels[0] != "must-survive" {
		t.Fatalf("cfg.GlobalAllowedModels clobbered to %#v", cfg.GlobalAllowedModels)
	}
	var stored string
	if err := db.Get(&stored, "SELECT value FROM settings WHERE key = ?", "global_allowed_models"); err != nil {
		t.Fatalf("query stored whitelist: %v", err)
	}
	if stored != `["must-survive"]` {
		t.Fatalf("stored whitelist wiped to %q", stored)
	}
}

func TestSettingsRuntimePartialUpdateDoesNotClobberWhitelist(t *testing.T) {
	db, r, cfg := setupEdgeTest(t)
	cfg.GlobalAllowedModels = []string{"alpha", "beta"}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`,
		"global_allowed_models", `["alpha","beta"]`); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	resp := doPutJSON(t, r, "/api/settings/runtime", map[string]any{
		"proxyDebugTraceEnabled": true,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s", resp.Code, resp.Body.String())
	}
	if len(cfg.GlobalAllowedModels) != 2 || cfg.GlobalAllowedModels[0] != "alpha" || cfg.GlobalAllowedModels[1] != "beta" {
		t.Fatalf("whitelist clobbered by partial update: %#v", cfg.GlobalAllowedModels)
	}
	var stored string
	if err := db.Get(&stored, "SELECT value FROM settings WHERE key = ?", "global_allowed_models"); err != nil {
		t.Fatalf("query stored whitelist: %v", err)
	}
	if stored != `["alpha","beta"]` {
		t.Fatalf("stored whitelist changed by partial update: %q", stored)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/runtime", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET runtime: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	models, ok := got["globalAllowedModels"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("GET globalAllowedModels = %#v", got["globalAllowedModels"])
	}
}
