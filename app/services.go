package app

import (
	"context"
	"log/slog"
	"sync"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/scheduler"
)

var (
	servicesMu       sync.RWMutex
	registry         *scheduler.Registry
	checkinScheduler *scheduler.CheckinScheduler
)

// StartBackgroundServices creates and starts all 15 background schedulers.
func StartBackgroundServices() {
	slog.Info("starting background schedulers")

	cfg := config.Get()
	newRegistry := scheduler.NewRegistry()

	// ---- Usage Aggregation (needed by admin-snapshot) ----
	usageAgg := scheduler.NewUsageAggregationScheduler(cfg)
	newRegistry.Register(usageAgg)

	// ---- Scheduler 1: Checkin ----
	checkin := scheduler.NewCheckinScheduler(cfg)
	newRegistry.Register(checkin)

	// ---- Scheduler 2: Balance Refresh ----
	newRegistry.Register(scheduler.NewBalanceScheduler(cfg, nil))

	// ---- Scheduler 3: Daily Summary ----
	newRegistry.Register(scheduler.NewDailySummaryScheduler(cfg))

	// ---- Scheduler 4: Log Cleanup ----
	newRegistry.Register(scheduler.NewLogCleanupScheduler(cfg))

	// ---- Scheduler 5: Backup WebDAV ----
	newRegistry.Register(scheduler.NewBackupWebdavScheduler(cfg))

	// ---- Scheduler 6: Site Announcements ----
	newRegistry.Register(scheduler.NewSiteAnnouncementScheduler(cfg))

	// ---- Scheduler 7: Model Probe ----
	// Inject live ChannelHealthProbe + TokenRouter health recorder when the
	// proxy upstream router is already configured (#170).
	modelProbe := scheduler.NewModelProbeScheduler(cfg)
	WireModelProbeScheduler(modelProbe)
	newRegistry.Register(modelProbe)

	// ---- Scheduler 8: Channel Recovery ----
	newRegistry.Register(scheduler.NewChannelRecoveryScheduler(cfg))

	// ---- Scheduler 9: Sub2API Refresh ----
	newRegistry.Register(scheduler.NewSub2APIRefreshScheduler(cfg))

	// ---- Scheduler 10: Update Center ----
	newRegistry.Register(scheduler.NewUpdateCenterScheduler(cfg))

	// ---- Scheduler 12: Admin Snapshot ----
	newRegistry.Register(scheduler.NewAdminSnapshotScheduler(cfg, usageAgg))

	// ---- Scheduler 13: Proxy File Retention ----
	newRegistry.Register(scheduler.NewProxyFileRetentionScheduler(cfg))

	// ---- Scheduler 13b: Proxy Video Task Retention (#262) ----
	newRegistry.Register(scheduler.NewProxyVideoTaskRetentionScheduler(cfg))

	// ---- Scheduler 14: Proxy Log Retention (legacy fallback) ----
	newRegistry.Register(scheduler.NewProxyLogRetentionScheduler(cfg))

	// ---- Scheduler 15: OAuth Loopback ----
	newRegistry.Register(scheduler.NewOAuthLoopbackScheduler(cfg))

	servicesMu.Lock()
	registry = newRegistry
	checkinScheduler = checkin
	servicesMu.Unlock()

	// Start all
	newRegistry.StartAll(context.Background())

	slog.Info("all background schedulers registered",
		"count", len(newRegistry.List()),
	)
}

// StopBackgroundServices stops all background schedulers.
func StopBackgroundServices() {
	slog.Info("stopping background schedulers")
	servicesMu.Lock()
	activeRegistry := registry
	registry = nil
	checkinScheduler = nil
	servicesMu.Unlock()
	if activeRegistry != nil {
		activeRegistry.StopAll()
	}
}

// UpdateCheckinSchedule applies a persisted checkin schedule to the running
// scheduler. It is a no-op before background services have started.
func UpdateCheckinSchedule(mode, cronExpr string, intervalHours int) error {
	servicesMu.RLock()
	activeScheduler := checkinScheduler
	servicesMu.RUnlock()
	if activeScheduler == nil {
		return nil
	}
	return activeScheduler.UpdateCheckinSchedule(mode, cronExpr, intervalHours)
}
