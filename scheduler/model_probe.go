package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// ModelProbeScheduler periodically probes model availability by queuing
// background tasks. Supports account-level lease to prevent concurrent probes
// on the same account.
type ModelProbeScheduler struct {
	cfg           *config.Config
	ticker        *time.Ticker
	stopCh        chan struct{}
	running       bool
	mu            sync.Mutex
	accountLeases map[int64]bool
}

// NewModelProbeScheduler creates a new model availability probe scheduler.
func NewModelProbeScheduler(cfg *config.Config) *ModelProbeScheduler {
	return &ModelProbeScheduler{
		cfg:           cfg,
		accountLeases: make(map[int64]bool),
	}
}

func (s *ModelProbeScheduler) Name() string { return "model-probe" }

func (s *ModelProbeScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.cfg.ModelAvailabilityProbeEnabled {
		slog.Info("model-probe: disabled (probe not enabled)")
		return nil
	}

	// Hard floor of 60 seconds
	intervalMs := int64(maxInt(s.cfg.ModelAvailabilityProbeIntervalMs, 60_000))
	interval := time.Duration(intervalMs) * time.Millisecond

	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runProbe()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("model-probe scheduler started",
		"interval_ms", intervalMs,
		"concurrency", s.cfg.ModelAvailabilityProbeConcurrency,
	)
	return nil
}

func (s *ModelProbeScheduler) Stop() error {
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

// TryAcquireAccountLease attempts to acquire a lease for probing a specific account.
// Returns true if the lease was acquired.
func (s *ModelProbeScheduler) TryAcquireAccountLease(accountID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accountLeases[accountID] {
		return false
	}
	s.accountLeases[accountID] = true
	return true
}

// ReleaseAccountLease releases the lease for an account.
func (s *ModelProbeScheduler) ReleaseAccountLease(accountID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accountLeases, accountID)
}

// ResetLeases clears all account leases (for tests).
func (s *ModelProbeScheduler) ResetLeases() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountLeases = make(map[int64]bool)
}

func (s *ModelProbeScheduler) runProbe() {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runProbeLocked(dbw)
	})
}

func (s *ModelProbeScheduler) runProbeLocked(dbw *store.DB) {
	slog.Info("model-probe: starting availability probe")

	// Query active accounts
	rows, err := dbw.Query("SELECT id FROM accounts WHERE status = 'active'")
	if err != nil {
		slog.Error("model-probe: failed to query accounts", "error", err)
		return
	}
	defer rows.Close()

	var accountIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		accountIDs = append(accountIDs, id)
	}

	// Filter by account-level lease
	var available []int64
	for _, id := range accountIDs {
		if s.TryAcquireAccountLease(id) {
			available = append(available, id)
		}
	}

	if len(available) == 0 {
		slog.Info("model-probe: no accounts available (all leased)")
		return
	}

	// TODO: Wire actual probe execution via background task system
	// For now, probe is a stub that releases leases immediately
	for _, id := range available {
		s.ReleaseAccountLease(id)
	}

	slog.Info("model-probe: probe complete", "accounts", len(available))
}
