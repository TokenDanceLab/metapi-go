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

func TestApplyRuntimeSettingsGlobalAllowedModelsJSONArray(t *testing.T) {
	cfg := &config.Config{GlobalAllowedModels: []string{"stale"}}
	ApplyRuntimeSettings(cfg, map[string]string{
		"global_allowed_models": `["gpt-4o"," claude-3.7-sonnet ","gpt-4o"]`,
	})
	want := []string{"gpt-4o", "claude-3.7-sonnet"}
	if len(cfg.GlobalAllowedModels) != len(want) {
		t.Fatalf("GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
	}
	for i := range want {
		if cfg.GlobalAllowedModels[i] != want[i] {
			t.Fatalf("GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
		}
	}
}

func TestApplyRuntimeSettingsGlobalAllowedModelsDoubleEncodedExact(t *testing.T) {
	cfg := &config.Config{GlobalAllowedModels: []string{"stale"}}
	// Value as stored after JSON.stringify(JSON.stringify([...]))
	ApplyRuntimeSettings(cfg, map[string]string{
		"global_allowed_models": `"[\"model-alpha\",\" model-beta \",\"model-gamma\"]"`,
	})
	want := []string{"model-alpha", "model-beta", "model-gamma"}
	if len(cfg.GlobalAllowedModels) != len(want) {
		t.Fatalf("GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
	}
	for i := range want {
		if cfg.GlobalAllowedModels[i] != want[i] {
			t.Fatalf("GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
		}
	}
}

func TestApplyRuntimeSettingsGlobalAllowedModelsExplicitEmpty(t *testing.T) {
	cfg := &config.Config{GlobalAllowedModels: []string{"stale"}}
	ApplyRuntimeSettings(cfg, map[string]string{
		"global_allowed_models": `[]`,
	})
	if cfg.GlobalAllowedModels == nil || len(cfg.GlobalAllowedModels) != 0 {
		t.Fatalf("GlobalAllowedModels = %#v, want empty non-nil slice", cfg.GlobalAllowedModels)
	}
}

func TestApplyRuntimeSettingsGlobalAllowedModelsInvalidDoesNotWipe(t *testing.T) {
	cfg := &config.Config{GlobalAllowedModels: []string{"keep-me", "also"}}
	ApplyRuntimeSettings(cfg, map[string]string{
		"global_allowed_models": `{"oops":true}`,
	})
	if len(cfg.GlobalAllowedModels) != 2 || cfg.GlobalAllowedModels[0] != "keep-me" {
		t.Fatalf("invalid value wiped allowlist: %#v", cfg.GlobalAllowedModels)
	}
}

func TestApplyRuntimeSettingsGlobalAllowedModelsCommaSeparatedLegacy(t *testing.T) {
	cfg := &config.Config{GlobalAllowedModels: []string{}}
	ApplyRuntimeSettings(cfg, map[string]string{
		"global_allowed_models": `gpt-4o, claude-3, gemini-pro`,
	})
	want := []string{"gpt-4o", "claude-3", "gemini-pro"}
	if len(cfg.GlobalAllowedModels) != len(want) {
		t.Fatalf("GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
	}
	for i := range want {
		if cfg.GlobalAllowedModels[i] != want[i] {
			t.Fatalf("GlobalAllowedModels = %#v, want %#v", cfg.GlobalAllowedModels, want)
		}
	}
}

func TestParseStringListSettingEmptyRawRejected(t *testing.T) {
	if list, ok := parseStringListSetting(""); ok || list != nil {
		t.Fatalf("empty raw should be rejected, got %#v ok=%v", list, ok)
	}
}
