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

	// Residual honesty (#246): this pass is scaffolding only.
	// What runs: SQL scan of active sub2api accounts (id + extra_config) under
	// the scheduler lease. What does NOT run: extraConfig.sub2apiAuth parsing,
	// due-window filter (refreshToken + tokenExpiresAt), concurrency pool, or
	// any call into balance.refreshSub2ApiManagedSessionSingleflight.
	// Logs always report refreshed=0 / failed=0 — not fake success, just no work.
	// Full managed-auth product is out of scope here; see residual-sub2api-auth.md.
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
		// Residual (#246): extraConfig is loaded but not parsed.
		// TODO residual (not wired): parse extraConfig.sub2apiAuth and keep only
		// candidates with non-empty refreshToken + due tokenExpiresAt.
		// Until then every active sub2api account is treated as a scan candidate
		// and none are refreshed.
		_ = c.extraConfig
		candidates = append(candidates, c)
	}

	// Residual (#246): no refresh side effects. singleflight helper lives in
	// service/balance but is not invoked from this scheduler.
	// TODO residual (not wired): call refreshSub2ApiManagedSessionSingleflight
	// with concurrency=sub2apiRefreshConcurrency; do not invent success counts.
	slog.Info("sub2api-refresh: pass complete (scan-only residual; no tokens refreshed)",
		"scanned", len(candidates),
		"refreshed", 0,
		"failed", 0,
	)
}
