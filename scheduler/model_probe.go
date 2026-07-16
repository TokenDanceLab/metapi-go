package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// ProbeTarget is one lightweight channel/model pair the background probe may hit.
type ProbeTarget struct {
	ChannelID int64
	AccountID int64
	SiteID    int64
	ModelName string
}

// ProbeOutcome is the result of a single lightweight probe attempt.
// Status is one of: success | failure | inconclusive | skipped.
type ProbeOutcome struct {
	Status    string
	LatencyMs float64
	// HTTPStatus is optional; 0 means unknown / transport error.
	HTTPStatus int
	ErrorText  string
}

// ChannelHealthProbe executes one lightweight request against a channel/model.
// Implementations live outside scheduler (proxy/platform) and are injected at boot.
// Returning an error is treated as an inconclusive outcome (no health mutation).
type ChannelHealthProbe interface {
	ProbeChannel(ctx context.Context, target ProbeTarget) (ProbeOutcome, error)
}

// ChannelHealthRecorder applies probe outcomes to routing health/cooldown.
// Implementations wrap routing.TokenRouter.RecordProbeSuccess/RecordProbeFailure.
// Must never mark credentials/accounts expired.
type ChannelHealthRecorder interface {
	RecordProbeSuccess(ctx context.Context, channelID int64, latencyMs float64, modelName *string, actualAccountID *int64) error
	RecordProbeFailure(ctx context.Context, channelID int64, httpStatus *int, errorText *string, modelName *string, actualAccountID *int64) error
}

// ModelProbeScheduler periodically probes model/channel availability with a
// lightweight request so routing can avoid dead channels before user traffic fails.
// Supports account-level lease to prevent concurrent probes on the same account.
type ModelProbeScheduler struct {
	cfg           *config.Config
	ticker        *time.Ticker
	stopCh        chan struct{}
	running       bool
	mu            sync.Mutex
	accountLeases map[int64]bool

	probe    ChannelHealthProbe
	recorder ChannelHealthRecorder

	// lastRunSummary is retained for tests / operator diagnostics.
	lastRunSummary ProbeRunSummary
}

// ProbeRunSummary summarizes one background probe pass.
type ProbeRunSummary struct {
	AccountsConsidered int
	AccountsProbed     int
	TargetsScanned     int
	Success            int
	Failed             int
	Inconclusive       int
	Skipped            int
	CompletedAtMs      int64
}

// NewModelProbeScheduler creates a new model availability probe scheduler.
func NewModelProbeScheduler(cfg *config.Config) *ModelProbeScheduler {
	return &ModelProbeScheduler{
		cfg:           cfg,
		accountLeases: make(map[int64]bool),
	}
}

// SetProbeExecutor injects the lightweight probe implementation.
func (s *ModelProbeScheduler) SetProbeExecutor(probe ChannelHealthProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe = probe
}

// SetHealthRecorder injects the routing health/cooldown recorder.
func (s *ModelProbeScheduler) SetHealthRecorder(recorder ChannelHealthRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorder = recorder
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
		"timeout_ms", s.cfg.ModelAvailabilityProbeTimeoutMs,
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

// LastRunSummary returns a copy of the most recent probe pass summary.
func (s *ModelProbeScheduler) LastRunSummary() ProbeRunSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRunSummary
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

	summary := ProbeRunSummary{}
	defer func() {
		summary.CompletedAtMs = time.Now().UnixMilli()
		s.mu.Lock()
		s.lastRunSummary = summary
		s.mu.Unlock()
	}()

	targets, err := s.loadProbeTargets(dbw)
	if err != nil {
		slog.Error("model-probe: failed to load probe targets", "error", err)
		return
	}
	if len(targets) == 0 {
		slog.Info("model-probe: no probe targets")
		return
	}

	// Group by account so account leases still gate concurrent work.
	byAccount := make(map[int64][]ProbeTarget)
	for _, t := range targets {
		byAccount[t.AccountID] = append(byAccount[t.AccountID], t)
	}
	summary.AccountsConsidered = len(byAccount)

	var availableAccounts []int64
	for accountID := range byAccount {
		if s.TryAcquireAccountLease(accountID) {
			availableAccounts = append(availableAccounts, accountID)
		}
	}
	if len(availableAccounts) == 0 {
		slog.Info("model-probe: no accounts available (all leased)")
		return
	}

	timeoutMs := s.cfg.ModelAvailabilityProbeTimeoutMs
	if timeoutMs < 3000 {
		timeoutMs = 3000
	}
	concurrency := s.cfg.ModelAvailabilityProbeConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 16 {
		concurrency = 16
	}

	// Flatten leased targets and run with bounded concurrency.
	var work []ProbeTarget
	for _, accountID := range availableAccounts {
		work = append(work, byAccount[accountID]...)
	}
	summary.AccountsProbed = len(availableAccounts)
	summary.TargetsScanned = len(work)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, target := range work {
		target := target
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			outcome := s.probeOne(target, timeoutMs)
			mu.Lock()
			switch outcome {
			case "success":
				summary.Success++
			case "failure":
				summary.Failed++
			case "inconclusive":
				summary.Inconclusive++
			default:
				summary.Skipped++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	for _, accountID := range availableAccounts {
		s.ReleaseAccountLease(accountID)
	}

	slog.Info("model-probe: probe complete",
		"accounts", summary.AccountsProbed,
		"targets", summary.TargetsScanned,
		"success", summary.Success,
		"failed", summary.Failed,
		"inconclusive", summary.Inconclusive,
		"skipped", summary.Skipped,
	)
}

func (s *ModelProbeScheduler) probeOne(target ProbeTarget, timeoutMs int) string {
	s.mu.Lock()
	probe := s.probe
	recorder := s.recorder
	s.mu.Unlock()

	if probe == nil || recorder == nil {
		// Without an injected executor/recorder we cannot safely touch health.
		// Keep the pass no-op so operators can enable the flag without crash.
		slog.Debug("model-probe: probe executor or recorder not configured; skipping target",
			"channel_id", target.ChannelID,
			"model", target.ModelName,
		)
		return "skipped"
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	outcome, err := probe.ProbeChannel(ctx, target)
	if err != nil {
		slog.Warn("model-probe: probe executor error (inconclusive)",
			"channel_id", target.ChannelID,
			"model", target.ModelName,
			"error", err,
		)
		return "inconclusive"
	}

	modelName := target.ModelName
	var modelPtr *string
	if modelName != "" {
		modelPtr = &modelName
	}
	accountID := target.AccountID
	accountPtr := &accountID

	switch outcome.Status {
	case "success":
		if err := recorder.RecordProbeSuccess(ctx, target.ChannelID, outcome.LatencyMs, modelPtr, accountPtr); err != nil {
			slog.Warn("model-probe: RecordProbeSuccess failed",
				"channel_id", target.ChannelID,
				"error", err,
			)
			return "inconclusive"
		}
		return "success"
	case "failure":
		var statusPtr *int
		if outcome.HTTPStatus > 0 {
			st := outcome.HTTPStatus
			statusPtr = &st
		}
		var errPtr *string
		if outcome.ErrorText != "" {
			e := outcome.ErrorText
			errPtr = &e
		}
		if err := recorder.RecordProbeFailure(ctx, target.ChannelID, statusPtr, errPtr, modelPtr, accountPtr); err != nil {
			slog.Warn("model-probe: RecordProbeFailure failed",
				"channel_id", target.ChannelID,
				"error", err,
			)
			return "inconclusive"
		}
		return "failure"
	case "skipped":
		return "skipped"
	default:
		// inconclusive / unknown — do not mutate health or cooldown.
		return "inconclusive"
	}
}

// loadProbeTargets selects a budgeted set of active route channels for probing.
// Prefers channels that are currently cooling down, then other enabled channels.
// Caps the batch so default-on budgets stay small (issue #114 out of scope: every model).
func (s *ModelProbeScheduler) loadProbeTargets(dbw *store.DB) ([]ProbeTarget, error) {
	const maxBatch = 16
	nowISO := time.Now().UTC().Format(time.RFC3339)

	// Cooling channels first (proactive recovery signal).
	cooling, err := queryProbeTargets(dbw, `
		SELECT rc.id, rc.account_id, a.site_id, COALESCE(rc.source_model, '') AS source_model
		FROM route_channels rc
		INNER JOIN accounts a ON rc.account_id = a.id
		INNER JOIN sites st ON a.site_id = st.id
		WHERE rc.enabled = TRUE
		  AND a.status = 'active'
		  AND st.status = 'active'
		  AND rc.cooldown_until IS NOT NULL
		  AND rc.cooldown_until > ?
		  AND COALESCE(rc.source_model, '') <> ''
		ORDER BY rc.cooldown_until ASC
		LIMIT ?
	`, nowISO, maxBatch)
	if err != nil {
		return nil, err
	}

	if len(cooling) >= maxBatch {
		return cooling, nil
	}

	// Fill remaining budget with active (non-cooling) channels.
	remaining := maxBatch - len(cooling)
	active, err := queryProbeTargets(dbw, `
		SELECT rc.id, rc.account_id, a.site_id, COALESCE(rc.source_model, '') AS source_model
		FROM route_channels rc
		INNER JOIN accounts a ON rc.account_id = a.id
		INNER JOIN sites st ON a.site_id = st.id
		WHERE rc.enabled = TRUE
		  AND a.status = 'active'
		  AND st.status = 'active'
		  AND rc.cooldown_until IS NULL
		  AND COALESCE(rc.source_model, '') <> ''
		ORDER BY COALESCE(rc.last_used_at, '') ASC, rc.id ASC
		LIMIT ?
	`, remaining)
	if err != nil {
		return cooling, err
	}

	// De-dupe by channel id.
	seen := make(map[int64]struct{}, len(cooling)+len(active))
	out := make([]ProbeTarget, 0, len(cooling)+len(active))
	for _, t := range cooling {
		if _, ok := seen[t.ChannelID]; ok {
			continue
		}
		seen[t.ChannelID] = struct{}{}
		out = append(out, t)
	}
	for _, t := range active {
		if _, ok := seen[t.ChannelID]; ok {
			continue
		}
		seen[t.ChannelID] = struct{}{}
		out = append(out, t)
	}
	return out, nil
}

func queryProbeTargets(dbw *store.DB, query string, args ...any) ([]ProbeTarget, error) {
	rows, err := dbw.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProbeTarget
	for rows.Next() {
		var t ProbeTarget
		if err := rows.Scan(&t.ChannelID, &t.AccountID, &t.SiteID, &t.ModelName); err != nil {
			continue
		}
		if t.ChannelID <= 0 || t.ModelName == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// ApplyProbeOutcome is a pure helper used by tests and optional direct callers.
// It maps a ProbeOutcome onto the recorder without requiring a live DB target load.
func ApplyProbeOutcome(
	ctx context.Context,
	recorder ChannelHealthRecorder,
	target ProbeTarget,
	outcome ProbeOutcome,
) (string, error) {
	if recorder == nil {
		return "skipped", nil
	}
	modelName := target.ModelName
	var modelPtr *string
	if modelName != "" {
		modelPtr = &modelName
	}
	accountID := target.AccountID
	accountPtr := &accountID

	switch outcome.Status {
	case "success":
		if err := recorder.RecordProbeSuccess(ctx, target.ChannelID, outcome.LatencyMs, modelPtr, accountPtr); err != nil {
			return "inconclusive", err
		}
		return "success", nil
	case "failure":
		var statusPtr *int
		if outcome.HTTPStatus > 0 {
			st := outcome.HTTPStatus
			statusPtr = &st
		}
		var errPtr *string
		if outcome.ErrorText != "" {
			e := outcome.ErrorText
			errPtr = &e
		}
		if err := recorder.RecordProbeFailure(ctx, target.ChannelID, statusPtr, errPtr, modelPtr, accountPtr); err != nil {
			return "inconclusive", err
		}
		return "failure", nil
	case "skipped":
		return "skipped", nil
	default:
		return "inconclusive", nil
	}
}
