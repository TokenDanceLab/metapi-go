package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const defaultProxyLogRetentionIntervalMin = 30 // 30 minutes default

// ProxyLogRetentionScheduler is a legacy fallback for cleaning up proxy logs
// when the main log_cleanup system is not configured (logCleanupConfigured=false).
type ProxyLogRetentionScheduler struct {
	cfg     *config.Config
	ticker  *time.Ticker
	stopCh  chan struct{}
	running bool
	mu      sync.Mutex
}

// NewProxyLogRetentionScheduler creates a new proxy log retention scheduler.
func NewProxyLogRetentionScheduler(cfg *config.Config) *ProxyLogRetentionScheduler {
	return &ProxyLogRetentionScheduler{cfg: cfg}
}

func (s *ProxyLogRetentionScheduler) Name() string { return "proxy-log-retention" }

func (s *ProxyLogRetentionScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only activate when logCleanup is NOT configured (legacy fallback)
	if s.cfg.LogCleanupConfigured {
		slog.Info("proxy-log-retention: disabled (log_cleanup configured)")
		return nil
	}
	if s.cfg.ProxyLogRetentionDays <= 0 {
		slog.Info("proxy-log-retention: disabled (retention_days=0)")
		return nil
	}

	intervalMin := maxInt(s.cfg.ProxyLogRetentionPruneIntervalMinutes, 1)
	if intervalMin == 0 {
		intervalMin = defaultProxyLogRetentionIntervalMin
	}
	interval := time.Duration(intervalMin) * time.Minute

	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run
	go s.runCleanup()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runCleanup()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("proxy-log-retention scheduler started (legacy fallback)",
		"interval_min", intervalMin,
		"retention_days", s.cfg.ProxyLogRetentionDays,
	)
	return nil
}

func (s *ProxyLogRetentionScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	if s.ticker != nil {
		s.ticker.Stop()
	}
	if s.stopCh != nil {
		close(s.stopCh)
	}
	return nil
}

func (s *ProxyLogRetentionScheduler) runCleanup() {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}

	retentionDays := s.cfg.ProxyLogRetentionDays
	if retentionDays <= 0 {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runCleanupLocked(dbw, retentionDays)
	})
}

func (s *ProxyLogRetentionScheduler) runCleanupLocked(dbw *store.DB, retentionDays int) {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	cutoffStr := formatTimeToSQL(cutoff)

	result, err := dbw.Exec("DELETE FROM proxy_logs WHERE created_at < ?", cutoffStr)
	if err != nil {
		slog.Warn("proxy-log-retention: cleanup failed", "error", err)
		return
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		slog.Info("proxy-log-retention: cleanup complete", "deleted", deleted, "cutoff", cutoffStr)
	}
}
