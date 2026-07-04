package app

import (
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// PrintStartupSummary logs a summary of the startup configuration.
// Mirrors the TS buildStartupSummaryLines behavior.
func PrintStartupSummary(cfg *config.Config) {
	slog.Info("========================================")
	slog.Info("  MetAPI Go Server started")
	slog.Info("========================================")
	slog.Info("listening", "port", cfg.Port, "host", cfg.ListenHost)
	slog.Info("database", "type", cfg.DbType)
	if cfg.AuthToken != "" {
		slog.Info("auth", "admin_token", "configured")
	} else {
		slog.Warn("auth", "admin_token", "not configured")
	}
	if cfg.ProxyToken != "" {
		slog.Info("auth", "proxy_token", "configured")
	} else {
		slog.Warn("auth", "proxy_token", "not configured")
	}
	slog.Info("========================================")
}

// StartBackgroundServices starts all background service stubs.
// Each is wrapped in a goroutine+recover; failures only warn, never crash.
// P12 will replace stubs with real implementations.
func StartBackgroundServices() {
	slog.Info("starting background services (stubs)")

	services := []struct {
		name string
		fn   func()
	}{
		{"checkinScheduler", startCheckinSchedulerStub},
		{"backupWebdavReload", reloadBackupWebdavStub},
		{"siteAnnouncementPolling", startSiteAnnouncementPollingStub},
		{"modelAvailabilityProbe", startModelAvailabilityProbeStub},
		{"channelRecoveryProbe", startChannelRecoveryProbeStub},
		{"sub2apiRefresh", startSub2apiRefreshStub},
		{"updateCenterPolling", startUpdateCenterPollingStub},
		{"usageAggregation", startUsageAggregationStub},
		{"adminSnapshot", startAdminSnapshotStub},
	}

	for _, svc := range services {
		go func(name string, fn func()) {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("background service panicked", "service", name, "panic", r)
				}
			}()
			slog.Info("background service started (stub)", "service", name)
			fn()
		}(svc.name, svc.fn)
	}

	// OAuth loopback servers: wrapped in try/catch equivalent
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("OAuth loopback callback servers failed", "panic", r)
			}
		}()
		startOAuthLoopbackCallbackServersStub()
	}()
}

// StopBackgroundServices stops all background service stubs.
// Registered as onClose callbacks.
func StopBackgroundServices() {
	slog.Info("stopping background services (stubs)")
	stopSiteAnnouncementPollingStub()
	stopUpdateCenterPollingStub()
	stopProxyFileRetentionStub()
	stopProxyLogRetentionStub()
	stopModelAvailabilityProbeStub()
	stopChannelRecoveryProbeStub()
	stopUsageAggregationStub()
	stopAdminSnapshotStub()
	stopSub2apiRefreshStub()
	stopOAuthLoopbackCallbackServersStub()
}

// ---- Individual service stubs ----

func startCheckinSchedulerStub()        {}
func reloadBackupWebdavStub()           {}
func startSiteAnnouncementPollingStub()  {}
func startModelAvailabilityProbeStub()   {}
func startChannelRecoveryProbeStub()     {}
func startSub2apiRefreshStub()           {}
func startUpdateCenterPollingStub()      {}
func startUsageAggregationStub()         {}
func startAdminSnapshotStub()            {}
func startOAuthLoopbackCallbackServersStub() {}

func stopSiteAnnouncementPollingStub()    {}
func stopUpdateCenterPollingStub()        {}
func stopProxyFileRetentionStub()         {}
func stopProxyLogRetentionStub()          {}
func stopModelAvailabilityProbeStub()     {}
func stopChannelRecoveryProbeStub()       {}
func stopUsageAggregationStub()           {}
func stopAdminSnapshotStub()              {}
func stopSub2apiRefreshStub()             {}
func stopOAuthLoopbackCallbackServersStub() {}
