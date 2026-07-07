package admin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/app"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/scheduler"
)

type checkinSchedulePatch struct {
	Mode          *string
	Cron          *string
	IntervalHours *int
}

type checkinScheduleState struct {
	Mode          string
	Cron          string
	IntervalHours int
}

func applyCheckinScheduleSettings(db *sqlx.DB, cfg *config.Config, patch checkinSchedulePatch) (checkinScheduleState, error) {
	state := resolveCheckinScheduleState(cfg, patch)
	if state.Mode == "" {
		return checkinScheduleState{}, fmt.Errorf("mode must be cron or interval")
	}
	if patch.Cron != nil || state.Mode == "cron" {
		if state.Cron == "" {
			return checkinScheduleState{}, fmt.Errorf("cron is required when mode is cron")
		}
		if !scheduler.ValidateCronExpr(state.Cron) {
			return checkinScheduleState{}, fmt.Errorf("invalid cron expression")
		}
	}
	if patch.IntervalHours != nil || state.Mode == "interval" {
		if state.IntervalHours < 1 || state.IntervalHours > 24 {
			return checkinScheduleState{}, fmt.Errorf("intervalHours must be between 1 and 24")
		}
	}

	tx, err := db.Beginx()
	if err != nil {
		return checkinScheduleState{}, fmt.Errorf("settings: begin checkin schedule update: %w", err)
	}
	defer tx.Rollback()

	if err := upsertSettingTx(db, tx, "checkin_schedule_mode", state.Mode); err != nil {
		return checkinScheduleState{}, err
	}
	if err := upsertSettingTx(db, tx, "checkin_cron", state.Cron); err != nil {
		return checkinScheduleState{}, err
	}
	if err := upsertSettingTx(db, tx, "checkin_interval_hours", state.IntervalHours); err != nil {
		return checkinScheduleState{}, err
	}
	if err := tx.Commit(); err != nil {
		return checkinScheduleState{}, fmt.Errorf("settings: commit checkin schedule update: %w", err)
	}

	cfg.CheckinScheduleMode = state.Mode
	cfg.CheckinCron = state.Cron
	cfg.CheckinIntervalHours = state.IntervalHours
	if err := app.UpdateCheckinSchedule(state.Mode, state.Cron, state.IntervalHours); err != nil {
		return checkinScheduleState{}, fmt.Errorf("settings: apply checkin schedule runtime update: %w", err)
	}
	return state, nil
}

func resolveCheckinScheduleState(cfg *config.Config, patch checkinSchedulePatch) checkinScheduleState {
	mode := normalizeCheckinScheduleMode(cfg.CheckinScheduleMode)
	if patch.Mode != nil {
		mode = normalizeCheckinScheduleMode(*patch.Mode)
	}

	cron := strings.TrimSpace(cfg.CheckinCron)
	if cron == "" {
		cron = config.DefaultCheckinCron
	}
	if patch.Cron != nil {
		cron = strings.TrimSpace(*patch.Cron)
	}

	intervalHours := cfg.CheckinIntervalHours
	if intervalHours < 1 || intervalHours > 24 {
		intervalHours = config.DefaultCheckinIntervalHours
	}
	if patch.IntervalHours != nil {
		intervalHours = *patch.IntervalHours
	}

	return checkinScheduleState{
		Mode:          mode,
		Cron:          cron,
		IntervalHours: intervalHours,
	}
}

func normalizeCheckinScheduleMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "cron":
		return "cron"
	case "interval":
		return "interval"
	default:
		return ""
	}
}

func upsertSettingTx(db *sqlx.DB, tx *sqlx.Tx, key string, value any) error {
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("settings: marshal %q: %w", key, err)
	}
	query := db.Rebind(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	if _, err := tx.Exec(query, key, string(jsonValue)); err != nil {
		return fmt.Errorf("settings: upsert %q: %w", key, err)
	}
	return nil
}
