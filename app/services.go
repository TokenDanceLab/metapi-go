package app

import (
	"context"
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/scheduler"
)

var (
	registry *scheduler.Registry
)

// StartBackgroundServices creates and starts all 15 background schedulers.
func StartBackgroundServices() {
	slog.Info("starting background schedulers")

	cfg := config.Get()
	registry = scheduler.NewRegistry()

	// ---- Usage Aggregation (needed by admin-snapshot) ----
	usageAgg := scheduler.NewUsageAggregationScheduler(cfg)
	registry.Register(usageAgg)

	// ---- Scheduler 1: Checkin ----
	registry.Register(scheduler.NewCheckinScheduler(cfg))

	// ---- Scheduler 2: Balance Refresh ----
	registry.Register(scheduler.NewBalanceScheduler(cfg, nil))

	// ---- Scheduler 3: Daily Summary ----
	registry.Register(scheduler.NewDailySummaryScheduler(cfg))

	// ---- Scheduler 4: Log Cleanup ----
	registry.Register(scheduler.NewLogCleanupScheduler(cfg))

	// ---- Scheduler 5: Backup WebDAV ----
	registry.Register(scheduler.NewBackupWebdavScheduler(cfg))

	// ---- Scheduler 6: Site Announcements ----
	registry.Register(scheduler.NewSiteAnnouncementScheduler(cfg))

	// ---- Scheduler 7: Model Probe ----
	registry.Register(scheduler.NewModelProbeScheduler(cfg))

	// ---- Scheduler 8: Channel Recovery ----
	registry.Register(scheduler.NewChannelRecoveryScheduler(cfg))

	// ---- Scheduler 9: Sub2API Refresh ----
	registry.Register(scheduler.NewSub2APIRefreshScheduler(cfg))

	// ---- Scheduler 10: Update Center ----
	registry.Register(scheduler.NewUpdateCenterScheduler(cfg))

	// ---- Scheduler 12: Admin Snapshot ----
	registry.Register(scheduler.NewAdminSnapshotScheduler(cfg, usageAgg))

	// ---- Scheduler 13: Proxy File Retention ----
	registry.Register(scheduler.NewProxyFileRetentionScheduler(cfg))

	// ---- Scheduler 14: Proxy Log Retention (legacy fallback) ----
	registry.Register(scheduler.NewProxyLogRetentionScheduler(cfg))

	// ---- Scheduler 15: OAuth Loopback ----
	registry.Register(scheduler.NewOAuthLoopbackScheduler(cfg))

	// Start all
	registry.StartAll(context.Background())

	slog.Info("all background schedulers registered",
		"count", len(registry.List()),
	)
}

// StopBackgroundServices stops all background schedulers.
func StopBackgroundServices() {
	slog.Info("stopping background schedulers")
	if registry != nil {
		registry.StopAll()
	}
}
