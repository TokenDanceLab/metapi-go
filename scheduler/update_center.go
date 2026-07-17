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

// UpdateCenterScheduler is a residual ticker for update-center honesty.
//
// It does NOT poll a remote registry/helper, invent updateAvailable, or
// trigger deploy/rollback. Product path remains external CI/CD / helper.
// See docs/analysis/residual-update-center.md and #283 / #246.
type UpdateCenterScheduler struct {
	cfg      *config.Config
	ticker   *time.Ticker
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
	inFlight bool
}

// NewUpdateCenterScheduler creates the residual update-center ticker.
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

	// Immediate first residual pass (log-only; no remote polling).
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

	slog.Info("update-center residual scheduler started (log-only; no remote registry)", "interval_ms", interval.Milliseconds())
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
	// Residual honesty (#246 / #283): log-only no-op for product purposes.
	// What runs: ticker + in-flight guard + scheduler lease + this log line.
	// What does NOT run: remote registry/helper polling, version compare,
	// persistence of lastCheckedAt, or any deploy/rollback side effect.
	// Admin update-center status/check are local stubs (0.0.0 /
	// updateAvailable=false + residual field); deploy/rollback/SSE are honest
	// 501 residuals (see residual-update-center.md). This scheduler must not
	// invent "update available", fake lastCheckedAt, or fake task completion.
	// TODO residual (not wired): wire actual update-center polling only when a
	// real helper/registry client exists — until then this is scan-free log-only.
	// Do not add fake HTTP polling stubs that invent updateAvailable=true.
	slog.Info("update-center: residual check (no remote polling; no version discovery; no updateAvailable invention)")
}
