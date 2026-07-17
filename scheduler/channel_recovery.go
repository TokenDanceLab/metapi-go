package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
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

// Optional process-local provider of coordinator-active channel IDs.
// Wired from app.ConfigureProxyUpstream to ProxyChannelCoordinator.GetActiveChannelIDs
// without importing proxy (avoids scheduler↔proxy cycles). #273
var (
	activeChannelIDsProviderMu sync.RWMutex
	activeChannelIDsProvider   func() []int64
)

// SetActiveChannelIDsProvider registers (or clears with nil) the active-channel ID source
// used by ChannelRecoveryScheduler.loadActiveCandidates.
func SetActiveChannelIDsProvider(fn func() []int64) {
	activeChannelIDsProviderMu.Lock()
	defer activeChannelIDsProviderMu.Unlock()
	activeChannelIDsProvider = fn
}

// GetActiveChannelIDsFromProvider returns IDs from the registered provider.
// Returns nil when no provider is registered.
func GetActiveChannelIDsFromProvider() []int64 {
	fn := getActiveChannelIDsProvider()
	if fn == nil {
		return nil
	}
	return fn()
}

func getActiveChannelIDsProvider() func() []int64 {
	activeChannelIDsProviderMu.RLock()
	defer activeChannelIDsProviderMu.RUnlock()
	return activeChannelIDsProvider
}

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
}

// NewChannelRecoveryScheduler creates a new channel recovery probe scheduler.
func NewChannelRecoveryScheduler(cfg *config.Config) *ChannelRecoveryScheduler {
	return &ChannelRecoveryScheduler{
		cfg:                cfg,
		inFlightKeys:       make(map[string]bool),
		lastStartedAtByKey: make(map[string]int64),
	}
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

	// Query cooling channels (SQL-backed; real candidate list).
	coolingCandidates := s.loadCoolingCandidates(dbw)
	// Active candidates prefer ProxyChannelCoordinator via optional provider
	// hook (#273). When unset, residual SQL LIMIT 50 approximation remains.
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
	// Prefer coordinator-active IDs when app has wired the optional provider (#273).
	// Presence of the provider (not the slice contents) selects this path so an
	// empty active-lease set does not fall back to the residual SQL scan.
	if fn := getActiveChannelIDsProvider(); fn != nil {
		return s.loadActiveCandidatesFromIDs(dbw, fn())
	}

	// Residual fallback (#246/#273): SQL approximation of "active" channels
	// (enabled + no cooldown + active account/site) when no coordinator
	// provider is registered. LIMIT 50 is a local guard, not coordinator
	// semantics. Honest residual only when the optional hook is unset.
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

// loadActiveCandidatesFromIDs resolves model names for coordinator-provided channel IDs.
// Includes enabled channels with active account/site. Null cooldown is preferred;
// channels that still have a source_model are included even when cooldown is set.
func (s *ChannelRecoveryScheduler) loadActiveCandidatesFromIDs(dbw *store.DB, ids []int64) []recoveryCandidate {
	if len(ids) == 0 {
		return nil
	}

	query, args, err := sqlx.In(`
		SELECT rc.id, COALESCE(rc.source_model, '') as source_model
		FROM route_channels rc
		INNER JOIN accounts a ON rc.account_id = a.id
		INNER JOIN sites st ON a.site_id = st.id
		WHERE rc.id IN (?)
		  AND rc.enabled = TRUE
		  AND a.status = 'active'
		  AND st.status = 'active'
		  AND (
		    rc.cooldown_until IS NULL
		    OR (rc.source_model IS NOT NULL AND TRIM(rc.source_model) != '')
		  )
	`, ids)
	if err != nil {
		slog.Error("channel-recovery: failed to build active candidate query", "error", err)
		return nil
	}

	rows, err := dbw.Query(query, args...)
	if err != nil {
		slog.Error("channel-recovery: failed to load active candidates from provider IDs", "error", err)
		return nil
	}
	defer rows.Close()

	// Preserve provider order where possible.
	byID := make(map[int64]recoveryCandidate, len(ids))
	for rows.Next() {
		var c recoveryCandidate
		c.source = "active"
		if err := rows.Scan(&c.channelID, &c.modelName); err != nil {
			continue
		}
		c.modelName = strings.TrimSpace(c.modelName)
		if c.modelName != "" {
			byID[c.channelID] = c
		}
	}

	candidates := make([]recoveryCandidate, 0, len(byID))
	seen := make(map[int64]bool, len(byID))
	for _, id := range ids {
		if c, ok := byID[id]; ok && !seen[id] {
			candidates = append(candidates, c)
			seen[id] = true
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
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inFlightKeys, key)
		s.mu.Unlock()
	}()

	// Prefer shared model probe executor when registered (#154).
	if mp := GetGlobalModelProbeScheduler(); mp != nil {
		target := ProbeTarget{
			ChannelID: candidate.channelID,
			ModelName: candidate.modelName,
		}
		timeoutMs := 15000
		if s.cfg != nil && s.cfg.ModelAvailabilityProbeTimeoutMs >= 3000 {
			timeoutMs = s.cfg.ModelAvailabilityProbeTimeoutMs
		}
		outcome := mp.probeOne(target, timeoutMs)
		slog.Debug("channel-recovery: probe complete",
			"channel_id", candidate.channelID,
			"model", candidate.modelName,
			"source", candidate.source,
			"outcome", outcome,
		)
		return
	}
	slog.Debug("channel-recovery: no model probe scheduler registered; skip live probe",
		"channel_id", candidate.channelID,
		"model", candidate.modelName,
		"source", candidate.source,
	)
}
