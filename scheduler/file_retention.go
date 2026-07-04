package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const defaultProxyFileRetentionIntervalMin = 60 // 60 minutes default

// ProxyFileRetentionScheduler periodically prunes expired proxy files.
type ProxyFileRetentionScheduler struct {
	cfg      *config.Config
	ticker   *time.Ticker
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewProxyFileRetentionScheduler creates a new proxy file retention scheduler.
func NewProxyFileRetentionScheduler(cfg *config.Config) *ProxyFileRetentionScheduler {
	return &ProxyFileRetentionScheduler{cfg: cfg}
}

func (s *ProxyFileRetentionScheduler) Name() string { return "proxy-file-retention" }

func (s *ProxyFileRetentionScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cfg.ProxyFileRetentionDays <= 0 {
		slog.Info("proxy-file-retention: disabled (retention_days=0)")
		return nil
	}

	intervalMin := maxInt(s.cfg.ProxyFileRetentionPruneIntervalMinutes, 1)
	if intervalMin == 0 {
		intervalMin = defaultProxyFileRetentionIntervalMin
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

	slog.Info("proxy-file-retention scheduler started",
		"interval_min", intervalMin,
		"retention_days", s.cfg.ProxyFileRetentionDays,
	)
	return nil
}

func (s *ProxyFileRetentionScheduler) Stop() error {
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

func (s *ProxyFileRetentionScheduler) runCleanup() {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}

	retentionDays := s.cfg.ProxyFileRetentionDays
	if retentionDays <= 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	cutoffStr := formatTimeToSQL(cutoff)

	result, err := dbw.Exec("DELETE FROM proxy_files WHERE created_at < ? AND deleted_at IS NULL", cutoffStr)
	if err != nil {
		slog.Warn("proxy-file-retention: cleanup failed", "error", err)
		return
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		slog.Info("proxy-file-retention: cleanup complete", "deleted", deleted, "cutoff", cutoffStr)
	}
}
