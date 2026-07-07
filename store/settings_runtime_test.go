package store

import (
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

func TestApplyRuntimeSettingsAppliesCheckinSchedule(t *testing.T) {
	cfg := &config.Config{
		CheckinCron:          config.DefaultCheckinCron,
		CheckinScheduleMode:  "cron",
		CheckinIntervalHours: config.DefaultCheckinIntervalHours,
	}

	ApplyRuntimeSettings(cfg, map[string]string{
		"checkin_cron":           `"15 9 * * *"`,
		"checkin_schedule_mode":  `"interval"`,
		"checkin_interval_hours": `6`,
	})

	if cfg.CheckinCron != "15 9 * * *" {
		t.Fatalf("CheckinCron = %q, want updated cron", cfg.CheckinCron)
	}
	if cfg.CheckinScheduleMode != "interval" {
		t.Fatalf("CheckinScheduleMode = %q, want interval", cfg.CheckinScheduleMode)
	}
	if cfg.CheckinIntervalHours != 6 {
		t.Fatalf("CheckinIntervalHours = %d, want 6", cfg.CheckinIntervalHours)
	}
}

func TestApplyRuntimeSettingsIgnoresInvalidCheckinInterval(t *testing.T) {
	cfg := &config.Config{
		CheckinCron:          config.DefaultCheckinCron,
		CheckinScheduleMode:  "cron",
		CheckinIntervalHours: config.DefaultCheckinIntervalHours,
	}

	ApplyRuntimeSettings(cfg, map[string]string{
		"checkin_interval_hours": `48`,
	})

	if cfg.CheckinIntervalHours != config.DefaultCheckinIntervalHours {
		t.Fatalf("CheckinIntervalHours = %d, want fallback %d", cfg.CheckinIntervalHours, config.DefaultCheckinIntervalHours)
	}
}
