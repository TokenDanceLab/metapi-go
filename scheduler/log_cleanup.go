package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const logCleanupDefaultCron = "0 6 * * *"

// LogCleanupScheduler cleans up proxy log records and event records.
type LogCleanupScheduler struct {
	cfg        *config.Config
	cronRunner *cronRunner
}

func NewLogCleanupScheduler(cfg *config.Config) *LogCleanupScheduler {
	return &LogCleanupScheduler{cfg: cfg}
}

func (s *LogCleanupScheduler) Name() string { return "log-cleanup" }

func (s *LogCleanupScheduler) Start(ctx context.Context) error {
	fallback := s.cfg.LogCleanupCron
	if fallback == "" {
		fallback = logCleanupDefaultCron
	}
	activeCron := resolveCronSetting("log_cleanup_cron", fallback)
	s.cfg.LogCleanupCron = activeCron

	s.cfg.LogCleanupUsageLogsEnabled = resolveBooleanSetting("log_cleanup_usage_logs_enabled", s.cfg.LogCleanupUsageLogsEnabled)
	s.cfg.LogCleanupProgramLogsEnabled = resolveBooleanSetting("log_cleanup_program_logs_enabled", s.cfg.LogCleanupProgramLogsEnabled)
	s.cfg.LogCleanupRetentionDays = clampInt(
		resolvePositiveIntegerSetting("log_cleanup_retention_days", s.cfg.LogCleanupRetentionDays),
		1, 3650,
	)

	s.cronRunner = newCronRunner()
	_, err := s.cronRunner.addJob(activeCron, s.runJob)
	if err != nil {
		slog.Error("log-cleanup: failed to add cron job", "error", err)
		return err
	}
	s.cronRunner.start()

	slog.Info("log-cleanup scheduler started",
		"cron", activeCron,
		"configured", s.cfg.LogCleanupConfigured,
		"usage_enabled", s.cfg.LogCleanupUsageLogsEnabled,
		"program_enabled", s.cfg.LogCleanupProgramLogsEnabled,
		"retention_days", s.cfg.LogCleanupRetentionDays,
	)
	return nil
}

func (s *LogCleanupScheduler) Stop() error {
	if s.cronRunner != nil {
		s.cronRunner.stop()
		s.cronRunner = nil
	}
	return nil
}

func (s *LogCleanupScheduler) UpdateSettings(cronExpr string, usageEnabled, programEnabled bool, retentionDays int) error {
	if !ValidateCronExpr(cronExpr) {
		return formatErr("invalid cron expression: %s", cronExpr)
	}

	s.cfg.LogCleanupCron = cronExpr
	s.cfg.LogCleanupUsageLogsEnabled = usageEnabled
	s.cfg.LogCleanupProgramLogsEnabled = programEnabled
	s.cfg.LogCleanupRetentionDays = clampInt(retentionDays, 1, 3650)

	if s.cronRunner != nil {
		s.cronRunner.stop()
	}
	s.cronRunner = newCronRunner()
	_, err := s.cronRunner.addJob(cronExpr, s.runJob)
	if err != nil {
		return err
	}
	s.cronRunner.start()
	return nil
}

func (s *LogCleanupScheduler) runJob() {
	if !s.cfg.LogCleanupConfigured {
		slog.Info("log-cleanup: skipped, legacy fallback mode active")
		return
	}
	if !s.cfg.LogCleanupUsageLogsEnabled && !s.cfg.LogCleanupProgramLogsEnabled {
		slog.Info("log-cleanup: skipped, no log target enabled")
		return
	}

	slog.Info("log-cleanup: running cleanup")
	dbw := store.GetDB()
	if dbw == nil {
		slog.Error("log-cleanup: database not available")
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runJobLocked(dbw)
	})
}

func (s *LogCleanupScheduler) runJobLocked(dbw *store.DB) {
	now := time.Now()
	cutoff := now.Add(-time.Duration(s.cfg.LogCleanupRetentionDays) * 24 * time.Hour)
	cutoffStr := formatTimeToSQL(cutoff)

	var usageDeleted, programDeleted int64

	if s.cfg.LogCleanupUsageLogsEnabled {
		result, err := dbw.Exec("DELETE FROM proxy_logs WHERE created_at < ?", cutoffStr)
		if err != nil {
			slog.Error("log-cleanup: failed to cleanup proxy_logs", "error", err)
		} else {
			usageDeleted, _ = result.RowsAffected()
		}
	}

	if s.cfg.LogCleanupProgramLogsEnabled {
		result, err := dbw.Exec("DELETE FROM events WHERE created_at < ?", cutoffStr)
		if err != nil {
			slog.Error("log-cleanup: failed to cleanup events", "error", err)
		} else {
			programDeleted, _ = result.RowsAffected()
		}
	}

	slog.Info("log-cleanup: complete",
		"usage_deleted", usageDeleted,
		"program_deleted", programDeleted,
		"cutoff", cutoffStr,
	)
}

// formatTimeToSQL formats a retention cutoff for lexicographic compare against
// TEXT created_at columns written as UTC RFC3339 (e.g. proxy_logs/events/files).
// Space-separated "2006-01-02 15:04:05" is wrong: 'T' > ' ' so same-day old rows
// never satisfy created_at < cutoff (#516).
func formatTimeToSQL(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
