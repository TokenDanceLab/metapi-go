package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const defaultUpdateCenterIntervalMs = 15 * 60 * 1000 // 15 minutes

// UpdateCenterScheduler polls the update center for new versions.
type UpdateCenterScheduler struct {
	cfg      *config.Config
	ticker   *time.Ticker
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
	inFlight bool
}

// NewUpdateCenterScheduler creates a new update center poller.
func NewUpdateCenterScheduler(cfg *config.Config) *UpdateCenterScheduler {
	return &UpdateCenterScheduler{cfg: cfg}
}

func (s *UpdateCenterScheduler) Name() string { return "update-center" }

func (s *UpdateCenterScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	interval := time.Duration(maxInt64(defaultUpdateCenterIntervalMs, 10_000)) * time.Millisecond
	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run
	go s.runSync()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runSync()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("update-center scheduler started", "interval_ms", interval.Milliseconds())
	return nil
}

func (s *UpdateCenterScheduler) Stop() error {
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

func (s *UpdateCenterScheduler) runSync() {
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
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runSyncLocked(dbw)
	})
}

func (s *UpdateCenterScheduler) runSyncLocked(dbw *store.DB) {
	_ = dbw
	slog.Info("update-center: checking for updates")
	// Stub: calls update center API
	// TODO: wire actual update center polling logic
}
