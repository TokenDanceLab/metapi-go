package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const defaultProxyVideoTaskRetentionIntervalMin = 60

// ProxyVideoTaskRetentionScheduler periodically prunes aged proxy_video_tasks rows (#262).
// Disabled when ProxyVideoTaskRetentionDays <= 0 (default).
type ProxyVideoTaskRetentionScheduler struct {
	cfg     *config.Config
	ticker  *time.Ticker
	stopCh  chan struct{}
	running bool
	mu      sync.Mutex
}

// NewProxyVideoTaskRetentionScheduler creates a video task retention scheduler.
func NewProxyVideoTaskRetentionScheduler(cfg *config.Config) *ProxyVideoTaskRetentionScheduler {
	return &ProxyVideoTaskRetentionScheduler{cfg: cfg}
}

func (s *ProxyVideoTaskRetentionScheduler) Name() string { return "proxy-video-task-retention" }

func (s *ProxyVideoTaskRetentionScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cfg.ProxyVideoTaskRetentionDays <= 0 {
		slog.Info("proxy-video-task-retention: disabled (retention_days=0)")
		return nil
	}

	intervalMin := maxInt(s.cfg.ProxyVideoTaskRetentionPruneIntervalMinutes, 1)
	if intervalMin == 0 {
		intervalMin = defaultProxyVideoTaskRetentionIntervalMin
	}
	interval := time.Duration(intervalMin) * time.Minute

	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

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

	slog.Info("proxy-video-task-retention scheduler started",
		"interval_min", intervalMin,
		"retention_days", s.cfg.ProxyVideoTaskRetentionDays,
	)
	return nil
}

func (s *ProxyVideoTaskRetentionScheduler) Stop() error {
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

func (s *ProxyVideoTaskRetentionScheduler) runCleanup() {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	retentionDays := s.cfg.ProxyVideoTaskRetentionDays
	if retentionDays <= 0 {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runCleanupLocked(dbw, retentionDays)
	})
}

func (s *ProxyVideoTaskRetentionScheduler) runCleanupLocked(dbw *store.DB, retentionDays int) {
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	cutoffStr := formatTimeToSQL(cutoff)

	result, err := dbw.Exec("DELETE FROM proxy_video_tasks WHERE created_at < ?", cutoffStr)
	if err != nil {
		slog.Warn("proxy-video-task-retention: cleanup failed", "error", err)
		return
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		slog.Info("proxy-video-task-retention: cleanup complete",
			"deleted", deleted, "cutoff", cutoffStr, "retention_days", retentionDays)
	}
}
