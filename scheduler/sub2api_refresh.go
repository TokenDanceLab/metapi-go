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
	sub2apiRefreshIntervalMs    = 60_000 // 60s
	sub2apiRefreshMinIntervalMs = 60_000
	sub2apiRefreshConcurrency   = 4
)

// Sub2APIRefreshScheduler periodically refreshes Sub2API managed authentication
// tokens for eligible accounts.
type Sub2APIRefreshScheduler struct {
	cfg          *config.Config
	ticker       *time.Ticker
	stopCh       chan struct{}
	running      bool
	mu           sync.Mutex
	passInFlight bool
}

// NewSub2APIRefreshScheduler creates a new Sub2API refresh scheduler.
func NewSub2APIRefreshScheduler(cfg *config.Config) *Sub2APIRefreshScheduler {
	return &Sub2APIRefreshScheduler{cfg: cfg}
}

func (s *Sub2APIRefreshScheduler) Name() string { return "sub2api-refresh" }

func (s *Sub2APIRefreshScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intervalMs := int64(maxInt(sub2apiRefreshIntervalMs, sub2apiRefreshMinIntervalMs))
	interval := time.Duration(intervalMs) * time.Millisecond

	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first run
	go s.runPass()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runPass()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("sub2api-refresh scheduler started", "interval_ms", intervalMs)
	return nil
}

func (s *Sub2APIRefreshScheduler) Stop() error {
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

// Sub2ApiRefreshResult is the result of a refresh pass.
type Sub2ApiRefreshResult struct {
	Scanned             int
	Refreshed           int
	Failed              int
	Skipped             int
	RefreshedAccountIDs []int64
	FailedAccountIDs    []int64
}

func (s *Sub2APIRefreshScheduler) runPass() {
	s.mu.Lock()
	if s.passInFlight {
		s.mu.Unlock()
		return
	}
	s.passInFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.passInFlight = false
		s.mu.Unlock()
	}()

	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runPassLocked(dbw)
	})
}

func (s *Sub2APIRefreshScheduler) runPassLocked(dbw *store.DB) {
	slog.Info("sub2api-refresh: running pass")

	// Query Sub2API platform active accounts
	rows, err := dbw.Query(`
		SELECT a.id, a.extra_config
		FROM accounts a
		INNER JOIN sites s ON a.site_id = s.id
		WHERE LOWER(TRIM(COALESCE(s.platform, ''))) = 'sub2api'
		  AND a.status = 'active'
		  AND s.status = 'active'
	`)
	if err != nil {
		slog.Error("sub2api-refresh: failed to query accounts", "error", err)
		return
	}
	defer rows.Close()

	type candidate struct {
		id          int64
		extraConfig *string
	}

	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.extraConfig); err != nil {
			continue
		}
		// Filter: must have sub2apiAuth in extraConfig
		// TODO: parse extraConfig and check for refreshToken+tokenExpiresAt
		candidates = append(candidates, c)
	}

	// TODO: Wire actual Sub2API refresh via singleflight
	slog.Info("sub2api-refresh: pass complete",
		"scanned", len(candidates),
		"refreshed", 0,
		"failed", 0,
	)
}
