package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	channelRecoverySweepIntervalMs   = 30_000     // 30s sweep interval
	channelRecoveryMinIntervalMs     = 10_000     // 10s floor
	channelRecoveryMaxBatch          = 4          // max candidates per sweep
	channelRecoveryCooldownRecheckMs = 30_000     // 30s for cooldown
	channelRecoveryActiveRecheckMs   = 5 * 60_000 // 5min for active
)

// ChannelRecoveryScheduler periodically sweeps channels that are in cooldown
// or active to probe if they have recovered.
type ChannelRecoveryScheduler struct {
	cfg                *config.Config
	ticker             *time.Ticker
	stopCh             chan struct{}
	running            bool
	mu                 sync.Mutex
	inFlightKeys       map[string]bool  // "channelId:modelName" -> in flight
	lastStartedAtByKey map[string]int64 // "channelId:modelName" -> start timestamp ms
	sweepInFlight      bool

	// Optional injected probe path (same interfaces as ModelProbeScheduler).
	probe    ChannelHealthProbe
	recorder ChannelHealthRecorder

	// lastProbeStatuses retains recent outcomes for tests / diagnostics.
	lastProbeStatuses map[string]string
}

// NewChannelRecoveryScheduler creates a new channel recovery probe scheduler.
func NewChannelRecoveryScheduler(cfg *config.Config) *ChannelRecoveryScheduler {
	return &ChannelRecoveryScheduler{
		cfg:                cfg,
		inFlightKeys:       make(map[string]bool),
		lastStartedAtByKey: make(map[string]int64),
		lastProbeStatuses:  make(map[string]string),
	}
}

// SetProbeExecutor injects the lightweight probe implementation used by recovery.
func (s *ChannelRecoveryScheduler) SetProbeExecutor(probe ChannelHealthProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe = probe
}

// SetHealthRecorder injects the routing health/cooldown recorder.
func (s *ChannelRecoveryScheduler) SetHealthRecorder(recorder ChannelHealthRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorder = recorder
}

// LastProbeStatus returns the most recent recovery probe status for a channel:model key.
func (s *ChannelRecoveryScheduler) LastProbeStatus(channelID int64, modelName string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastProbeStatuses[fmt.Sprintf("%d:%s", channelID, modelName)]
}

func (s *ChannelRecoveryScheduler) Name() string { return "channel-recovery" }

func (s *ChannelRecoveryScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intervalMs := int64(maxInt(channelRecoverySweepIntervalMs, channelRecoveryMinIntervalMs))
	interval := time.Duration(intervalMs) * time.Millisecond

	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.running = true

	// Immediate first sweep
	go s.runSweep()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				go s.runSweep()
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	slog.Info("channel-recovery scheduler started",
		"interval_ms", intervalMs,
		"max_batch", channelRecoveryMaxBatch,
	)
	return nil
}

func (s *ChannelRecoveryScheduler) Stop() error {
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

// runSweep performs a single recovery probe sweep.
// Serially: if a previous sweep is in flight, it waits.
func (s *ChannelRecoveryScheduler) runSweep() {
	s.mu.Lock()
	if s.sweepInFlight {
		s.mu.Unlock()
		return
	}
	s.sweepInFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.sweepInFlight = false
		s.mu.Unlock()
	}()

	dbw := store.GetDB()
	if dbw == nil {
		return
	}
	runWithSchedulerLease(context.Background(), dbw, s.Name(), func() {
		s.runSweepLocked(dbw)
	})
}

func (s *ChannelRecoveryScheduler) runSweepLocked(dbw *store.DB) {
	nowMs := time.Now().UnixMilli()

	// Query cooling channels
	coolingCandidates := s.loadCoolingCandidates(dbw)
	// TODO: wire active candidate loading via proxyChannelCoordinator
	activeCandidates := s.loadActiveCandidates(dbw)

	merged := s.mergeCandidates(coolingCandidates, activeCandidates)
	due := s.filterDue(merged, nowMs)

	if len(due) == 0 {
		return
	}

	// Sort by priority and limit to max batch
	s.prioritize(due)
	if len(due) > channelRecoveryMaxBatch {
		due = due[:channelRecoveryMaxBatch]
	}

	slog.Info("channel-recovery: running sweep",
		"cooling", len(coolingCandidates),
		"active", len(activeCandidates),
		"due", len(due),
	)

	for _, c := range due {
		s.probeCandidate(dbw, c, nowMs)
	}
}

type recoveryCandidate struct {
	source    string // "cooldown" or "active"
	channelID int64
	modelName string
}

func (s *ChannelRecoveryScheduler) loadCoolingCandidates(dbw *store.DB) []recoveryCandidate {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	var candidates []recoveryCandidate

	rows, err := dbw.Query(`
		SELECT rc.id, COALESCE(rc.source_model, '') as source_model
		FROM route_channels rc
		INNER JOIN accounts a ON rc.account_id = a.id
		INNER JOIN sites st ON a.site_id = st.id
		WHERE rc.enabled = TRUE
		  AND a.status = 'active'
		  AND st.status = 'active'
		  AND rc.cooldown_until IS NOT NULL
		  AND rc.cooldown_until > ?
	`, nowISO)
	if err != nil {
		slog.Error("channel-recovery: failed to load cooling candidates", "error", err)
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var c recoveryCandidate
		c.source = "cooldown"
		if err := rows.Scan(&c.channelID, &c.modelName); err != nil {
			continue
		}
		// Skip provider-directed cooldown (failCount<=0 && consecutiveFailCount<=0 && cooldownLevel<=0)
		// For simplicity, we don't filter here - the TS does deeper filtering
		if c.modelName != "" {
			candidates = append(candidates, c)
		}
	}
	return candidates
}

func (s *ChannelRecoveryScheduler) loadActiveCandidates(dbw *store.DB) []recoveryCandidate {
	// Stub: queries active channels
	var candidates []recoveryCandidate

	rows, err := dbw.Query(`
		SELECT rc.id, COALESCE(rc.source_model, '') as source_model
		FROM route_channels rc
		INNER JOIN accounts a ON rc.account_id = a.id
		INNER JOIN sites st ON a.site_id = st.id
		WHERE rc.enabled = TRUE
		  AND a.status = 'active'
		  AND st.status = 'active'
		  AND rc.cooldown_until IS NULL
		LIMIT 50
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var c recoveryCandidate
		c.source = "active"
		if err := rows.Scan(&c.channelID, &c.modelName); err != nil {
			continue
		}
		if c.modelName != "" {
			candidates = append(candidates, c)
		}
	}
	return candidates
}

func (s *ChannelRecoveryScheduler) mergeCandidates(cooling, active []recoveryCandidate) []recoveryCandidate {
	seen := make(map[int64]recoveryCandidate)
	// Cooling candidates first (higher priority)
	for _, c := range cooling {
		if _, exists := seen[c.channelID]; !exists {
			seen[c.channelID] = c
		}
	}
	// Active candidates only if not already in cooling
	for _, c := range active {
		if _, exists := seen[c.channelID]; !exists {
			seen[c.channelID] = c
		}
	}
	result := make([]recoveryCandidate, 0, len(seen))
	for _, c := range seen {
		result = append(result, c)
	}
	return result
}

func (s *ChannelRecoveryScheduler) filterDue(candidates []recoveryCandidate, nowMs int64) []recoveryCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []recoveryCandidate
	for _, c := range candidates {
		key := fmt.Sprintf("%d:%s", c.channelID, c.modelName)
		if s.inFlightKeys[key] {
			continue
		}
		lastStarted, exists := s.lastStartedAtByKey[key]
		recheckMs := int64(channelRecoveryActiveRecheckMs)
		if c.source == "cooldown" {
			recheckMs = channelRecoveryCooldownRecheckMs
		}
		if exists && (nowMs-lastStarted) < recheckMs {
			continue
		}
		due = append(due, c)
	}
	return due
}

func (s *ChannelRecoveryScheduler) prioritize(candidates []recoveryCandidate) {
	// Sort: never-probed first, then earliest-probed first
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			ki := fmt.Sprintf("%d:%s", candidates[i].channelID, candidates[i].modelName)
			kj := fmt.Sprintf("%d:%s", candidates[j].channelID, candidates[j].modelName)
			li, hasI := s.lastStartedAtByKey[ki]
			lj, hasJ := s.lastStartedAtByKey[kj]

			swap := false
			if !hasI && hasJ {
				swap = false // never-probed i stays before probed j
			} else if hasI && !hasJ {
				swap = true // probed i should move behind never-probed j
			} else if !hasI && !hasJ {
				swap = candidates[i].channelID > candidates[j].channelID
			} else {
				if li != lj {
					swap = li > lj
				} else {
					swap = candidates[i].channelID > candidates[j].channelID
				}
			}
			if swap {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
}

func (s *ChannelRecoveryScheduler) probeCandidate(dbw *store.DB, candidate recoveryCandidate, nowMs int64) {
	key := fmt.Sprintf("%d:%s", candidate.channelID, candidate.modelName)

	s.mu.Lock()
	s.inFlightKeys[key] = true
	s.lastStartedAtByKey[key] = nowMs
	probe := s.probe
	recorder := s.recorder
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inFlightKeys, key)
		s.mu.Unlock()
	}()

	slog.Debug("channel-recovery: probing candidate",
		"channel_id", candidate.channelID,
		"model", candidate.modelName,
		"source", candidate.source,
	)

	status := s.executeCandidateProbe(dbw, candidate, probe, recorder)
	s.mu.Lock()
	s.lastProbeStatuses[key] = status
	s.mu.Unlock()
}

// executeCandidateProbe runs one recovery probe through the injected executor and
// applies the outcome via ApplyProbeOutcome (shared with model-probe).
func (s *ChannelRecoveryScheduler) executeCandidateProbe(
	dbw *store.DB,
	candidate recoveryCandidate,
	probe ChannelHealthProbe,
	recorder ChannelHealthRecorder,
) string {
	if probe == nil || recorder == nil {
		// Safe no-op when composition root has not wired deps yet.
		slog.Debug("channel-recovery: probe executor or recorder not configured; skipping",
			"channel_id", candidate.channelID,
			"model", candidate.modelName,
		)
		return "skipped"
	}

	target, err := loadRecoveryProbeTarget(dbw, candidate)
	if err != nil {
		slog.Warn("channel-recovery: load target failed",
			"channel_id", candidate.channelID,
			"model", candidate.modelName,
			"error", err,
		)
		return "inconclusive"
	}

	timeoutMs := 10_000
	if s.cfg != nil && s.cfg.ModelAvailabilityProbeTimeoutMs > 0 {
		timeoutMs = s.cfg.ModelAvailabilityProbeTimeoutMs
		if timeoutMs < 3000 {
			timeoutMs = 3000
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	outcome, err := probe.ProbeChannel(ctx, target)
	if err != nil {
		slog.Warn("channel-recovery: probe executor error (inconclusive)",
			"channel_id", candidate.channelID,
			"model", candidate.modelName,
			"error", err,
		)
		return "inconclusive"
	}

	status, applyErr := ApplyProbeOutcome(ctx, recorder, target, outcome)
	if applyErr != nil {
		slog.Warn("channel-recovery: ApplyProbeOutcome failed",
			"channel_id", candidate.channelID,
			"model", candidate.modelName,
			"error", applyErr,
		)
		return "inconclusive"
	}
	return status
}

func loadRecoveryProbeTarget(dbw *store.DB, candidate recoveryCandidate) (ProbeTarget, error) {
	if dbw == nil {
		return ProbeTarget{}, fmt.Errorf("db unavailable")
	}
	var accountID, siteID int64
	var sourceModel string
	err := dbw.QueryRow(`
		SELECT rc.account_id, a.site_id, COALESCE(rc.source_model, '')
		FROM route_channels rc
		INNER JOIN accounts a ON a.id = rc.account_id
		WHERE rc.id = ?
	`, candidate.channelID).Scan(&accountID, &siteID, &sourceModel)
	if err != nil {
		return ProbeTarget{}, err
	}
	modelName := strings.TrimSpace(candidate.modelName)
	if modelName == "" {
		modelName = strings.TrimSpace(sourceModel)
	}
	if modelName == "" {
		return ProbeTarget{}, fmt.Errorf("empty model name")
	}
	return ProbeTarget{
		ChannelID: candidate.channelID,
		AccountID: accountID,
		SiteID:    siteID,
		ModelName: modelName,
	}, nil
}
