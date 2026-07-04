package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service/daily"
	notifypkg "github.com/tokendancelab/metapi-go/service/notify"
)

const dailySummaryDefaultCron = "58 23 * * *"

// DailySummaryScheduler sends a daily summary notification at 23:58 daily.
type DailySummaryScheduler struct {
	cfg        *config.Config
	cronRunner *cronRunner
}

// NewDailySummaryScheduler creates a new daily summary scheduler.
func NewDailySummaryScheduler(cfg *config.Config) *DailySummaryScheduler {
	return &DailySummaryScheduler{cfg: cfg}
}

func (s *DailySummaryScheduler) Name() string { return "daily-summary" }

func (s *DailySummaryScheduler) Start(ctx context.Context) error {
	activeCron := resolveCronSetting("daily_summary_cron", dailySummaryDefaultCron)
	s.cronRunner = newCronRunner()
	_, err := s.cronRunner.addJob(activeCron, s.runJob)
	if err != nil {
		slog.Error("daily-summary: failed to add cron job", "error", err)
		return err
	}
	s.cronRunner.start()
	slog.Info("daily-summary scheduler started", "cron", activeCron)
	return nil
}

func (s *DailySummaryScheduler) Stop() error {
	if s.cronRunner != nil {
		s.cronRunner.stop()
		s.cronRunner = nil
	}
	return nil
}

func (s *DailySummaryScheduler) runJob() {
	slog.Info("daily-summary: collecting metrics")
	db := getSqlxDB()
	if db == nil {
		slog.Error("daily-summary: database not available")
		return
	}

	now := time.Now()
	metrics := daily.CollectDailySummaryMetrics(s.cfg, db, now)
	if metrics == nil {
		slog.Error("daily-summary: failed to collect metrics")
		return
	}

	title, message := daily.BuildDailySummaryNotification(metrics)

	_, err := notifypkg.SendNotification(s.cfg, title, message, string(notifypkg.LevelInfo),
		&notifypkg.SendNotificationOptions{
			BypassThrottle: true,
			RequireChannel: true,
			ThrowOnFailure: true,
		},
	)
	if err != nil {
		slog.Error("daily-summary: notification failed", "error", err)
		return
	}
	slog.Info("daily-summary: sent", "title", title)
}
