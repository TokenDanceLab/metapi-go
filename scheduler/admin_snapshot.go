package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	adminSnapshotWarmIntervalMs = 20_000  // 20s
	adminSnapshotPruneEvery     = 6       // prune every 6 passes (~2 min)
)

// AdminSnapshotScheduler periodically warms admin dashboard snapshots
// and prunes expired ones.
type AdminSnapshotScheduler struct {
	cfg         *config.Config
	ticker      *time.Ticker
	stopCh      chan struct{}
	running     bool
	mu          sync.Mutex
	inFlight    bool
	passCount   int
	aggregation *UsageAggregationScheduler
}

// NewAdminSnapshotScheduler creates a new admin snapshot warm scheduler.
func NewAdminSnapshotScheduler(cfg *config.Config, usage *UsageAggregationScheduler) *AdminSnapshotScheduler {
	return &AdminSnapshotScheduler{
		cfg:         cfg,
		aggregation: usage,
	}
}

func (s *AdminSnapshotScheduler) Name() string { return "admin-snapshot" }

func (s *AdminSnapshotScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ticker = time.NewTicker(time.Duration(adminSnapshotWarmIntervalMs) * time.Millisecond)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run
	go s.runWarm()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runWarm()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("admin-snapshot scheduler started",
		"interval_ms", adminSnapshotWarmIntervalMs,
		"prune_every", adminSnapshotPruneEvery,
	)
	return nil
}

func (s *AdminSnapshotScheduler) Stop() error {
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

// WarmOnce performs a single warm pass. Safe to call externally.
func (s *AdminSnapshotScheduler) WarmOnce() {
	if s.inFlight {
		return
	}
	s.runWarm()
}

func (s *AdminSnapshotScheduler) runWarm() {
	s.mu.Lock()
	if s.inFlight {
		s.mu.Unlock()
		return
	}
	s.inFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()

	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	_ = dbw

	// Step 1: Run usage aggregation projection pass first
	if s.aggregation != nil {
		s.aggregation.RunProjectionPass()
	}

	// Step 2: Warm 4 snapshot targets in parallel
	targets := []string{
		"dashboard-summary",
		"accounts-snapshot",
		"site-stats",
		"dashboard-insights",
	}

	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			s.warmTarget(name)
		}(target)
	}
	wg.Wait()

	s.mu.Lock()
	s.passCount++
	passCount := s.passCount
	s.mu.Unlock()

	// Step 3: Prune expired snapshots every N passes
	if passCount%adminSnapshotPruneEvery == 0 {
		s.pruneExpired()
	}

	slog.Info("admin-snapshot: warm pass complete", "pass", passCount)
}

func (s *AdminSnapshotScheduler) warmTarget(name string) {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	// Stub: regenerate snapshot for this namespace
	// In full implementation, query the relevant tables and update admin_snapshots table
	slog.Debug("admin-snapshot: warming target", "namespace", name)
}

func (s *AdminSnapshotScheduler) pruneExpired() {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := dbw.Exec("DELETE FROM admin_snapshots WHERE expires_at < ?", now)
	if err != nil {
		slog.Warn("admin-snapshot: prune failed", "error", err)
		return
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		slog.Info("admin-snapshot: pruned expired snapshots", "deleted", deleted)
	}
}
