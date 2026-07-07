package scheduler

import (
	"context"
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service/balance"
	"github.com/tokendancelab/metapi-go/store"
)

// RouteRefresher rebuilds token routes after balance refresh.
// Mirrors TS routeRefreshWorkflow.refreshModelsAndRebuildRoutes().
type RouteRefresher interface {
	RebuildTokenRoutes(ctx context.Context) error
}

// BalanceScheduler periodically refreshes account balances and then rebuilds
// routes. Execution order is strict: refreshAllBalances MUST complete before
// rebuildRoutes begins.
type BalanceScheduler struct {
	cfg            *config.Config
	cronRunner     *cronRunner
	routeRefresher RouteRefresher
}

// NewBalanceScheduler creates a new balance refresh scheduler.
func NewBalanceScheduler(cfg *config.Config, routeRefresher RouteRefresher) *BalanceScheduler {
	return &BalanceScheduler{cfg: cfg, routeRefresher: routeRefresher}
}

// Name returns "balance-refresh".
func (s *BalanceScheduler) Name() string { return "balance-refresh" }

// Start begins periodic balance refresh. Loads the cron expression from DB
// settings or falls back to config default.
func (s *BalanceScheduler) Start(ctx context.Context) error {
	activeCron := resolveCronSetting("balance_refresh_cron", s.cfg.BalanceRefreshCron)
	s.cfg.BalanceRefreshCron = activeCron

	s.cronRunner = newCronRunner()
	_, err := s.cronRunner.addJob(activeCron, s.runJob)
	if err != nil {
		slog.Error("balance: failed to add cron job", "error", err, "cron", activeCron)
		return err
	}
	s.cronRunner.start()

	slog.Info("balance scheduler started", "cron", activeCron)
	return nil
}

// Stop halts the balance refresh scheduler.
func (s *BalanceScheduler) Stop() error {
	if s.cronRunner != nil {
		s.cronRunner.stop()
		s.cronRunner = nil
	}
	return nil
}

// UpdateCron updates the cron expression at runtime.
func (s *BalanceScheduler) UpdateCron(cronExpr string) error {
	if !ValidateCronExpr(cronExpr) {
		return formatErr("invalid cron expression: %s", cronExpr)
	}
	s.cfg.BalanceRefreshCron = cronExpr
	if s.cronRunner != nil {
		s.cronRunner.stop()
	}
	s.cronRunner = newCronRunner()
	_, err := s.cronRunner.addJob(cronExpr, s.runJob)
	if err != nil {
		return err
	}
	s.cronRunner.start()
	slog.Info("balance scheduler updated", "cron", cronExpr)
	return nil
}

func (s *BalanceScheduler) runJob() {
	slog.Info("balance: refreshing all balances")
	dbw := store.GetDB()
	if dbw == nil {
		slog.Error("balance: database not available")
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runJobLocked(dbw)
	})
}

func (s *BalanceScheduler) runJobLocked(dbw *store.DB) {
	// Step 1: Refresh all balances
	results := balance.RefreshAllBalances(s.cfg, dbw.DB)
	slog.Info("balance: refresh complete", "accounts", len(results))

	// Step 2: Rebuild routes (strict ordering)
	if err := s.refreshModelsAndRebuildRoutes(); err != nil {
		slog.Error("balance: route rebuild failed", "error", err)
	}

	slog.Info("balance: refresh complete")
}

// refreshModelsAndRebuildRoutes rebuilds routes after balance refresh.
// Mirrors TS routeRefreshWorkflow.refreshModelsAndRebuildRoutes().
func (s *BalanceScheduler) refreshModelsAndRebuildRoutes() error {
	if s.routeRefresher == nil {
		slog.Info("balance: route refresher not configured, skipping route rebuild")
		return nil
	}
	return s.routeRefresher.RebuildTokenRoutes(context.Background())
}
