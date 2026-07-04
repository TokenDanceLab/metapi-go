package scheduler

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// ---- Setting resolution utilities ----

// resolveCronSetting reads a cron expression from the DB settings table,
// validates it, and returns the fallback if invalid or missing.
// Mirrors TS resolveCronSetting().
func resolveCronSetting(settingKey string, fallback string) string {
	db := store.GetDB()
	if db == nil {
		return fallback
	}

	settingsStore := store.NewSettingsStore(db)
	raw, err := settingsStore.Get(settingKey)
	if err != nil || raw == "" {
		return fallback
	}

	var value string
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fallback
	}

	if !ValidateCronExpr(value) {
		return fallback
	}

	return value
}

// resolveBooleanSetting reads a boolean from the DB settings table.
// Falls back to the provided default if missing or invalid.
func resolveBooleanSetting(settingKey string, fallback bool) bool {
	db := store.GetDB()
	if db == nil {
		return fallback
	}

	settingsStore := store.NewSettingsStore(db)
	raw, err := settingsStore.Get(settingKey)
	if err != nil || raw == "" {
		return fallback
	}

	var value bool
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fallback
	}

	return value
}

// resolvePositiveIntegerSetting reads a positive integer (>=1) from DB settings.
func resolvePositiveIntegerSetting(settingKey string, fallback int) int {
	db := store.GetDB()
	if db == nil {
		return fallback
	}

	settingsStore := store.NewSettingsStore(db)
	raw, err := settingsStore.Get(settingKey)
	if err != nil || raw == "" {
		return fallback
	}

	var value float64
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fallback
	}

	if math.IsInf(value, 0) || math.IsNaN(value) || value < 1 {
		return fallback
	}

	return int(math.Trunc(value))
}

// resolveJsonSetting reads a JSON value from DB settings into the target type.
func resolveJsonSetting(settingKey string, target any) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	settingsStore := store.NewSettingsStore(db)
	raw, err := settingsStore.Get(settingKey)
	if err != nil || raw == "" {
		return fmt.Errorf("setting %s not found", settingKey)
	}

	return json.Unmarshal([]byte(raw), target)
}

// ---- Common helpers ----

// clampInt clamps v to the range [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// toISOTime returns the current time as an ISO 8601 string.
func toISOTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// resolveCheckinScheduleMode reads the checkin schedule mode from config and DB.
// Returns "cron" or "interval".
func resolveCheckinScheduleMode(cfg *config.Config) string {
	db := store.GetDB()
	if db == nil {
		return cfg.CheckinScheduleMode
	}

	settingsStore := store.NewSettingsStore(db)
	raw, err := settingsStore.Get("checkin_schedule_mode")
	if err != nil || raw == "" {
		return cfg.CheckinScheduleMode
	}

	var value string
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return cfg.CheckinScheduleMode
	}

	value = strings.TrimSpace(strings.ToLower(value))
	if value == "interval" {
		return "interval"
	}
	return "cron"
}
