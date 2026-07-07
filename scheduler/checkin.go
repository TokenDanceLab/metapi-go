package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service/checkin"
	"github.com/tokendancelab/metapi-go/store"
)

const checkinPollMs = int64(60_000) // 60 seconds between interval polls

// intervalCandidate holds the fields needed for interval-based checkin filtering.
type intervalCandidate struct {
	id            int64
	lastCheckinAt *string
}

// CheckinScheduler implements dual-mode (cron + interval) checkin scheduling.
// Mirrors TS checkinScheduler.ts.
type CheckinScheduler struct {
	cfg *config.Config

	mu               sync.Mutex
	mode             string
	cronRunner       *cronRunner
	intervalTimer    *time.Ticker
	intervalStop     chan struct{}
	attemptByAccount map[int64]int64 // accountId -> last attempt timestamp (ms)
}

// NewCheckinScheduler creates a new checkin scheduler.
func NewCheckinScheduler(cfg *config.Config) *CheckinScheduler {
	return &CheckinScheduler{
		cfg:              cfg,
		mode:             cfg.CheckinScheduleMode,
		attemptByAccount: make(map[int64]int64),
	}
}

// Name returns "checkin".
func (s *CheckinScheduler) Name() string { return "checkin" }

// Start starts the checkin scheduler. Loads settings from DB, applies fallbacks,
// and runs the selected mode.
func (s *CheckinScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	activeCron := resolveCronSetting("checkin_cron", s.cfg.CheckinCron)
	activeMode := resolveCheckinScheduleMode(s.cfg)
	activeIntervalHours := clampInt(
		resolvePositiveIntegerSetting("checkin_interval_hours", s.cfg.CheckinIntervalHours),
		1, 24,
	)

	s.cfg.CheckinCron = activeCron
	s.cfg.CheckinScheduleMode = activeMode
	s.cfg.CheckinIntervalHours = activeIntervalHours
	s.mode = activeMode

	s.startLocked()

	slog.Info("checkin scheduler started",
		"mode", activeMode,
		"cron", activeCron,
		"interval_hours", activeIntervalHours,
	)
	return nil
}

// Stop stops the checkin scheduler.
func (s *CheckinScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	return nil
}

func (s *CheckinScheduler) startLocked() {
	s.stopLocked()

	if s.mode == "interval" {
		s.intervalTimer = time.NewTicker(time.Duration(checkinPollMs) * time.Millisecond)
		s.intervalStop = make(chan struct{})
		go func() {
			for {
				select {
				case <-s.intervalTimer.C:
					s.runIntervalPass()
				case <-s.intervalStop:
					return
				}
			}
		}()
		return
	}

	// Cron mode
	s.cronRunner = newCronRunner()
	_, err := s.cronRunner.addJob(s.cfg.CheckinCron, s.runCronJob)
	if err != nil {
		slog.Error("checkin: failed to add cron job", "error", err)
		return
	}
	s.cronRunner.start()
}

func (s *CheckinScheduler) stopLocked() {
	if s.cronRunner != nil {
		s.cronRunner.stop()
		s.cronRunner = nil
	}
	if s.intervalTimer != nil {
		s.intervalTimer.Stop()
	}
	// Only close intervalStop if it hasn't been closed yet.
	// The background goroutine reads intervalStop without holding the lock,
	// so we must NOT nil it out — closing is the signal, and Go's closed-channel
	// read returns immediately without a data race.
	if s.intervalStop != nil {
		select {
		case <-s.intervalStop:
			// already closed, skip
		default:
			close(s.intervalStop)
		}
	}
}

// UpdateCheckinSchedule updates the checkin configuration at runtime.
// Mirrors TS updateCheckinSchedule().
func (s *CheckinScheduler) UpdateCheckinSchedule(mode, cronExpr string, intervalHours int) error {
	mode = stringsTrimLower(mode)
	if mode != "cron" && mode != "interval" {
		return formatErr("invalid checkin schedule mode: %s", mode)
	}
	if mode == "cron" && !ValidateCronExpr(cronExpr) {
		return formatErr("invalid cron expression: %s", cronExpr)
	}
	if mode == "interval" && (intervalHours < 1 || intervalHours > 24) {
		return formatErr("invalid interval hours: %d (must be 1-24)", intervalHours)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.mode = mode
	if mode == "cron" {
		s.cfg.CheckinCron = cronExpr
	}
	s.cfg.CheckinScheduleMode = mode
	s.cfg.CheckinIntervalHours = clampInt(intervalHours, 1, 24)
	s.startLocked()
	return nil
}

// ResetAttempts clears the interval attempt map (for tests).
func (s *CheckinScheduler) ResetAttempts() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attemptByAccount = make(map[int64]int64)
}

// ---- Internal ----

func (s *CheckinScheduler) runCronJob() {
	slog.Info("checkin: cron job starting")
	dbw := store.GetDB()
	if dbw == nil {
		slog.Error("checkin: database not available")
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		results := checkin.CheckinAll(s.cfg, dbw.DB, nil, "cron")
		ok, bad := countResults(results)
		slog.Info("checkin: cron job done", "success", ok, "failed", bad)
	})
}

func (s *CheckinScheduler) runIntervalPass() {
	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runIntervalPassLocked(dbw)
	})
}

func (s *CheckinScheduler) runIntervalPassLocked(dbw *store.DB) {
	now := time.Now()

	// Query all account+site pairs
	rows, err := dbw.Query(`
		SELECT a.id, a.last_checkin_at
		FROM accounts a
		INNER JOIN sites s ON a.site_id = s.id
		WHERE a.checkin_enabled = TRUE
		  AND a.status = 'active'
		  AND s.status <> 'disabled'
	`)
	if err != nil {
		slog.Error("checkin interval: query failed", "error", err)
		return
	}
	defer rows.Close()

	var candidates []intervalCandidate
	for rows.Next() {
		var c intervalCandidate
		if err := rows.Scan(&c.id, &c.lastCheckinAt); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}

	dueIDs := s.filterDue(candidates, now)
	if len(dueIDs) == 0 {
		return
	}

	results := checkin.CheckinAll(s.cfg, dbw.DB, dueIDs, "interval")

	nowMs := now.UnixMilli()
	s.mu.Lock()
	for _, r := range results {
		s.attemptByAccount[r.AccountID] = nowMs
	}
	s.mu.Unlock()

	ok, bad := countResults(results)
	slog.Info("checkin: interval pass done",
		"due", len(dueIDs), "success", ok, "failed", bad)
}

// filterDue mirrors TS selectDueIntervalCheckinAccountIds().
func (s *CheckinScheduler) filterDue(rows []intervalCandidate, now time.Time) []int64 {
	nowMs := now.UnixMilli()
	intervalHours := clampInt(s.cfg.CheckinIntervalHours, 1, 24)
	intervalMs := int64(intervalHours) * 3600 * 1000

	s.mu.Lock()
	defer s.mu.Unlock()

	var due []int64
	for _, row := range rows {
		hasCheckin := false
		var checkinMs int64
		if row.lastCheckinAt != nil && *row.lastCheckinAt != "" {
			if t, err := time.Parse(time.RFC3339, *row.lastCheckinAt); err == nil {
				checkinMs = t.UnixMilli()
				hasCheckin = true
			}
		}
		attemptMs, hasAttempt := s.attemptByAccount[row.id]

		if hasCheckin {
			if nowMs-checkinMs < intervalMs {
				continue // already checked in within window
			}
			if hasAttempt && attemptMs >= checkinMs && nowMs-attemptMs < intervalMs {
				continue // mid-flight checkin
			}
		} else {
			if hasAttempt && nowMs-attemptMs < intervalMs {
				continue // recently attempted, no known checkin
			}
		}
		due = append(due, row.id)
	}
	return due
}

func countResults(results []checkin.CheckinAllResult) (success, failed int) {
	for _, r := range results {
		if r.Result.Success {
			success++
		} else {
			failed++
		}
	}
	return
}
