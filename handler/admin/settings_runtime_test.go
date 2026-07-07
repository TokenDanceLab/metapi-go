package admin

import (
	"net/http"
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
