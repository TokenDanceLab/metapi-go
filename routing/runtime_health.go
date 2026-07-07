package routing

import (
	"encoding/json"
	"math"
	"regexp"
	"sync"
	"time"
)

// ---- Site runtime health constants ----

const (
	SiteRuntimeHealthDecayHalfLifeMs   = 10 * 60 * 1000
	SiteRuntimeMinMultiplier           = 0.08
	SiteRuntimeLatencyBaselineMs       = 2500
	SiteRuntimeLatencyWindowMs         = 30000
	SiteRuntimeMaxLatencyPenalty       = 0.35
	SiteRuntimeLatencyEMAAlpha         = 0.3
	SiteRuntimeBreakerStreakThreshold  = 3
	SiteTransientStreakWindowMs        = 5 * 60 * 1000
	SiteRecentOutcomeHalfLifeMs        = 30 * 60 * 1000
	SiteRecentSuccessConfidenceSamples = 12
	SiteRecentSuccessPriorSuccesses    = 1
	SiteRecentSuccessPriorFailures     = 1
	SiteRecentSuccessFallbackRate      = 0.5
	SiteRecentModelWeight              = 0.65

	SiteHistoricalHealthMinMultiplier = 0.45
	SiteHistoricalHealthMaxSample     = 24
	SiteHistoricalLatencyBaselineMs   = 2000
	SiteHistoricalLatencyWindowMs     = 20000
	SiteHistoricalMaxLatencyPenalty   = 0.18

	SiteRuntimeHealthSettingKey        = "token_router_site_runtime_health_v1"
	SiteRuntimeHealthPersistDebounceMs = 500
	SiteRuntimeHealthPersistStaleTTLMs = 7 * 24 * 60 * 60 * 1000
	SiteRuntimeHealthPersistIdleTTLMs  = 12 * 60 * 60 * 1000
	SiteRuntimeHealthPersistMinPenalty = 0.02

	StableFirstPrimarySuccessRateRatio    = 0.92
	StableFirstTrustedRecentConfidence    = 0.5
	StableFirstTrustedHistoricalCalls     = 8
	StableFirstObservationRequestInterval = 24
	StableFirstObservationSiteCooldownMs  = 30 * 60 * 1000
)

// SiteRuntimeBreakerLevelsMs defines breaker durations: [0ms, 60s, 5min, 30min].
var SiteRuntimeBreakerLevelsMs = []int64{0, 60_000, 5 * 60_000, 30 * 60 * 1000}

// ---- Failure classification patterns ----

var siteTransientFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bad\s+gateway`),
	regexp.MustCompile(`(?i)gateway\s+time-?out`),
	regexp.MustCompile(`(?i)service\s+unavailable`),
	regexp.MustCompile(`(?i)temporar(?:y|ily)\s+unavailable`),
	regexp.MustCompile(`(?i)cpu\s+overloaded`),
	regexp.MustCompile(`(?i)overloaded`),
	regexp.MustCompile(`(?i)connection\s+reset`),
	regexp.MustCompile(`(?i)connection\s+refused`),
	regexp.MustCompile(`(?i)econnreset`),
	regexp.MustCompile(`(?i)econnrefused`),
	regexp.MustCompile(`(?i)timeout`),
	regexp.MustCompile(`(?i)timed\s*out`),
}

var siteModelFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)unsupported\s+model`),
	regexp.MustCompile(`(?i)model\s+not\s+supported`),
	regexp.MustCompile(`(?i)does\s+not\s+support(?:\s+the)?\s+model`),
	regexp.MustCompile(`(?i)no\s+such\s+model`),
	regexp.MustCompile(`(?i)unknown\s+model`),
	regexp.MustCompile(`(?i)unknown\s+provider\s+for\s+model`),
	regexp.MustCompile(`(?i)invalid\s+model`),
	regexp.MustCompile(`(?i)model.*does\s+not\s+exist`),
}

var siteProtocolFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)unsupported\s+legacy\s+protocol`),
	regexp.MustCompile(`(?i)please\s+use\s+/v1/responses`),
	regexp.MustCompile(`(?i)please\s+use\s+/v1/messages`),
	regexp.MustCompile(`(?i)please\s+use\s+/v1/chat/completions`),
	regexp.MustCompile(`(?i)does\s+not\s+allow\s+/v1/`),
	regexp.MustCompile(`(?i)unsupported\s+endpoint`),
	regexp.MustCompile(`(?i)unsupported\s+path`),
	regexp.MustCompile(`(?i)unknown\s+endpoint`),
	regexp.MustCompile(`(?i)unrecognized\s+request\s+url`),
	regexp.MustCompile(`(?i)no\s+route\s+matched`),
}

var siteValidationFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)invalid\s+request\s+body`),
	regexp.MustCompile(`(?i)validation`),
	regexp.MustCompile(`(?i)missing\s+required`),
	regexp.MustCompile(`(?i)required\s+parameter`),
	regexp.MustCompile(`(?i)unknown\s+parameter`),
	regexp.MustCompile(`(?i)unrecognized\s+(?:field|key|parameter)`),
	regexp.MustCompile(`(?i)malformed`),
	regexp.MustCompile(`(?i)invalid\s+json`),
	regexp.MustCompile(`(?i)cannot\s+parse`),
	regexp.MustCompile(`(?i)unsupported\s+media\s+type`),
}

var usageLimitRateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)usage_limit_reached`),
	regexp.MustCompile(`(?i)usage\s+limit\s+has\s+been\s+reached`),
	regexp.MustCompile(`(?i)quota\s+exceeded`),
	regexp.MustCompile(`(?i)rate\s+limit`),
	regexp.MustCompile(`(?i)\blimit\b`),
}

// ---- State types ----

// SiteRuntimeHealthState tracks runtime health for a site or (site, model).
type SiteRuntimeHealthState struct {
	PenaltyScore             float64  `json:"penaltyScore"`
	LatencyEMAMs             *float64 `json:"latencyEmaMs,omitempty"`
	TransientFailureStreak   int64    `json:"transientFailureStreak"`
	LastTransientFailureAtMs *int64   `json:"lastTransientFailureAtMs,omitempty"`
	RecentSuccessCount       float64  `json:"recentSuccessCount"`
	RecentFailureCount       float64  `json:"recentFailureCount"`
	RecentWindowUpdatedAtMs  int64    `json:"recentWindowUpdatedAtMs"`
	BreakerLevel             int64    `json:"breakerLevel"`
	BreakerUntilMs           *int64   `json:"breakerUntilMs,omitempty"`
	LastUpdatedAtMs          int64    `json:"lastUpdatedAtMs"`
	LastFailureAtMs          *int64   `json:"lastFailureAtMs,omitempty"`
	LastSuccessAtMs          *int64   `json:"lastSuccessAtMs,omitempty"`
}

// SiteRuntimeHealthDetails is the resolved health for selection.
type SiteRuntimeHealthDetails struct {
	GlobalMultiplier   float64
	ModelMultiplier    float64
	CombinedMultiplier float64
	GlobalBreakerOpen  bool
	ModelBreakerOpen   bool
	ModelKey           string
	RecentSuccessRate  float64
	RecentSampleCount  float64
	RecentConfidence   float64
}

// RecentOutcomeSnapshot is a snapshot of recent success/failure counts.
type RecentOutcomeSnapshot struct {
	SuccessCount float64
	FailureCount float64
	SampleCount  float64
	SuccessRate  float64
	Confidence   float64
}

// SiteHistoricalHealthMetrics tracks historical health per site.
type SiteHistoricalHealthMetrics struct {
	Multiplier   float64
	TotalCalls   int64
	SuccessRate  *float64
	AvgLatencyMs *int64
}

// ---- Global state ----

var (
	siteRuntimeHealthStates      = make(map[int64]*SiteRuntimeHealthState)
	siteModelRuntimeHealthStates = make(map[int64]map[string]*SiteRuntimeHealthState)
	healthStateMu                sync.RWMutex
	siteRuntimeHealthLoaded      bool
	healthPersistInFlight        bool
	healthPersistTimer           *time.Timer
)

// SiteRuntimeHealthPersistencePayload is the serialization format.
type SiteRuntimeHealthPersistencePayload struct {
	Version        int                                           `json:"version"`
	SavedAtMs      int64                                         `json:"savedAtMs"`
	GlobalBySiteID map[string]*SiteRuntimeHealthState            `json:"globalBySiteId"`
	ModelBySiteID  map[string]map[string]*SiteRuntimeHealthState `json:"modelBySiteId"`
}

// ---- Persistence callbacks ----

// SettingsStore defines the interface for persisting runtime health state.
type SettingsStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

var healthSettingsStore SettingsStore

// SetHealthSettingsStore sets the settings store for health persistence.
func SetHealthSettingsStore(store SettingsStore) {
	healthSettingsStore = store
}

// ---- Classification helpers ----

func matchesAnyPattern(patterns []*regexp.Regexp, text string) bool {
	if text == "" {
		return false
	}
	for _, p := range patterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

// IsUsageLimitRateLimitFailure checks if a 429 is from a usage/rate limit.
func IsUsageLimitRateLimitFailure(ctx SiteRuntimeFailureContext) bool {
	status := 0
	if ctx.Status != nil {
		status = *ctx.Status
	}
	if status != 429 {
		return false
	}
	errorText := ""
	if ctx.ErrorText != nil {
		errorText = *ctx.ErrorText
	}
	return matchesAnyPattern(usageLimitRateLimitPatterns, errorText)
}

func isModelScopedRuntimeFailure(ctx SiteRuntimeFailureContext) bool {
	text := ""
	if ctx.ErrorText != nil {
		text = *ctx.ErrorText
	}
	return matchesAnyPattern(siteModelFailurePatterns, text)
}

func isProtocolRuntimeFailure(ctx SiteRuntimeFailureContext) bool {
	text := ""
	if ctx.ErrorText != nil {
		text = *ctx.ErrorText
	}
	return matchesAnyPattern(siteProtocolFailurePatterns, text)
}

func isValidationRuntimeFailure(ctx SiteRuntimeFailureContext) bool {
	text := ""
	if ctx.ErrorText != nil {
		text = *ctx.ErrorText
	}
	return matchesAnyPattern(siteValidationFailurePatterns, text)
}

// ResolveSiteRuntimeFailurePenalty assigns a penalty score based on failure context.
func ResolveSiteRuntimeFailurePenalty(ctx SiteRuntimeFailureContext) float64 {
	status := 0
	if ctx.Status != nil {
		status = *ctx.Status
	}
	errorText := ""
	if ctx.ErrorText != nil {
		errorText = *ctx.ErrorText
	}

	if IsUsageLimitRateLimitFailure(ctx) {
		return 0.4
	}
	if isModelScopedRuntimeFailure(ctx) {
		return 0.9
	}
	if isProtocolRuntimeFailure(ctx) {
		return 0.6
	}
	if isValidationRuntimeFailure(ctx) {
		return 0.25
	}
	if status >= 500 || matchesAnyPattern(siteTransientFailurePatterns, errorText) {
		return 2.5
	}
	if status == 429 {
		return 2.2
	}
	if status == 401 || status == 403 {
		return 1.8
	}
	if status >= 400 && status < 500 {
		return 0.9
	}
	return 1.2
}

// IsTransientSiteRuntimeFailure checks if a failure is transient.
func IsTransientSiteRuntimeFailure(ctx SiteRuntimeFailureContext) bool {
	status := 0
	if ctx.Status != nil {
		status = *ctx.Status
	}
	errorText := ""
	if ctx.ErrorText != nil {
		errorText = *ctx.ErrorText
	}

	if IsUsageLimitRateLimitFailure(ctx) {
		return false
	}
	if isModelScopedRuntimeFailure(ctx) {
		return false
	}
	if isProtocolRuntimeFailure(ctx) {
		return false
	}
	if isValidationRuntimeFailure(ctx) {
		return false
	}
	return status >= 500 || status == 429 || matchesAnyPattern(siteTransientFailurePatterns, errorText)
}

// ---- State management ----

func nowMs() int64 {
	return time.Now().UnixMilli()
}

func getDecayedSiteRuntimePenalty(state *SiteRuntimeHealthState) float64 {
	if state.PenaltyScore <= 0 || !isFiniteFloat(state.PenaltyScore) {
		return 0
	}
	elapsedMs := float64(nowMs() - state.LastUpdatedAtMs)
	if elapsedMs <= 0 {
		return state.PenaltyScore
	}
	decayFactor := math.Pow(0.5, elapsedMs/float64(SiteRuntimeHealthDecayHalfLifeMs))
	return state.PenaltyScore * decayFactor
}

func getOrCreateRuntimeHealthState(states map[int64]*SiteRuntimeHealthState, siteID int64) *SiteRuntimeHealthState {
	if state, ok := states[siteID]; ok {
		nextPenalty := getDecayedSiteRuntimePenalty(state)
		now := nowMs()
		if nextPenalty != state.PenaltyScore || state.LastUpdatedAtMs != now {
			state.PenaltyScore = nextPenalty
			state.LastUpdatedAtMs = now
		}
		return state
	}
	n := nowMs()
	s := &SiteRuntimeHealthState{
		PenaltyScore:             0,
		LatencyEMAMs:             nil,
		TransientFailureStreak:   0,
		LastTransientFailureAtMs: nil,
		RecentSuccessCount:       0,
		RecentFailureCount:       0,
		RecentWindowUpdatedAtMs:  n,
		BreakerLevel:             0,
		BreakerUntilMs:           nil,
		LastUpdatedAtMs:          n,
		LastFailureAtMs:          nil,
		LastSuccessAtMs:          nil,
	}
	states[siteID] = s
	return s
}

func getOrCreateSiteRuntimeHealthState(siteID int64) *SiteRuntimeHealthState {
	return getOrCreateRuntimeHealthState(siteRuntimeHealthStates, siteID)
}

func getSiteModelRuntimeHealthState(siteID int64, modelName string) *SiteRuntimeHealthState {
	modelKey := NormalizeModelAlias(modelName)
	if modelKey == "" {
		return nil
	}
	if modelStates, ok := siteModelRuntimeHealthStates[siteID]; ok {
		if state, ok := modelStates[modelKey]; ok {
			return state
		}
	}
	return nil
}

func getOrCreateSiteModelRuntimeHealthState(siteID int64, modelName string) *SiteRuntimeHealthState {
	modelKey := NormalizeModelAlias(modelName)
	if modelKey == "" {
		return nil
	}
	modelStates, ok := siteModelRuntimeHealthStates[siteID]
	if !ok {
		modelStates = make(map[string]*SiteRuntimeHealthState)
		siteModelRuntimeHealthStates[siteID] = modelStates
	}
	state, ok := modelStates[modelKey]
	if ok {
		nextPenalty := getDecayedSiteRuntimePenalty(state)
		now := nowMs()
		if nextPenalty != state.PenaltyScore || state.LastUpdatedAtMs != now {
			state.PenaltyScore = nextPenalty
			state.LastUpdatedAtMs = now
		}
		return state
	}
	n := nowMs()
	s := &SiteRuntimeHealthState{
		PenaltyScore:             0,
		LatencyEMAMs:             nil,
		TransientFailureStreak:   0,
		LastTransientFailureAtMs: nil,
		RecentSuccessCount:       0,
		RecentFailureCount:       0,
		RecentWindowUpdatedAtMs:  n,
		BreakerLevel:             0,
		BreakerUntilMs:           nil,
		LastUpdatedAtMs:          n,
		LastFailureAtMs:          nil,
		LastSuccessAtMs:          nil,
	}
	modelStates[modelKey] = s
	return s
}

func isRuntimeHealthBreakerOpen(state *SiteRuntimeHealthState) bool {
	if state == nil {
		return false
	}
	return state.BreakerUntilMs != nil && *state.BreakerUntilMs > nowMs()
}

func resolveSiteRuntimeBreakerMs(level int64) int64 {
	if level < 0 {
		level = 0
	}
	if level >= int64(len(SiteRuntimeBreakerLevelsMs)) {
		level = int64(len(SiteRuntimeBreakerLevelsMs) - 1)
	}
	return SiteRuntimeBreakerLevelsMs[level]
}

// GetRuntimeHealthMultiplier returns the health multiplier for a state.
func GetRuntimeHealthMultiplier(state *SiteRuntimeHealthState) float64 {
	if state == nil {
		return 1
	}
	if isRuntimeHealthBreakerOpen(state) {
		return SiteRuntimeMinMultiplier
	}
	penaltyScore := getDecayedSiteRuntimePenalty(state)
	failurePenaltyFactor := 1.0 / (1.0 + penaltyScore)

	latencyPenaltyRatio := 0.0
	if state.LatencyEMAMs != nil {
		latencyPenaltyRatio = ClampNumber(
			(*state.LatencyEMAMs-SiteRuntimeLatencyBaselineMs)/SiteRuntimeLatencyWindowMs,
			0, 1,
		)
	}
	latencyFactor := 1.0 - (latencyPenaltyRatio * SiteRuntimeMaxLatencyPenalty)
	return ClampNumber(failurePenaltyFactor*latencyFactor, SiteRuntimeMinMultiplier, 1)
}

// GetSiteRuntimeHealthDetails returns combined health details for a site and model.
func GetSiteRuntimeHealthDetails(siteID int64, modelName string) SiteRuntimeHealthDetails {
	healthStateMu.RLock()
	defer healthStateMu.RUnlock()

	modelKey := NormalizeModelAlias(modelName)
	globalState := siteRuntimeHealthStates[siteID]
	var modelState *SiteRuntimeHealthState
	if modelKey != "" {
		modelState = getSiteModelRuntimeHealthState(siteID, modelKey)
	}
	globalMultiplier := GetRuntimeHealthMultiplier(globalState)
	modelMultiplier := 1.0
	if modelState != nil {
		modelMultiplier = GetRuntimeHealthMultiplier(modelState)
	}
	globalRecentSnapshot := getRecentOutcomeSnapshot(globalState)
	var modelRecentSnapshot *RecentOutcomeSnapshot
	if modelState != nil {
		snap := getRecentOutcomeSnapshot(modelState)
		modelRecentSnapshot = &snap
	}
	recentSnapshot := blendRecentOutcomeSnapshots(globalRecentSnapshot, modelRecentSnapshot)
	return SiteRuntimeHealthDetails{
		GlobalMultiplier:   globalMultiplier,
		ModelMultiplier:    modelMultiplier,
		CombinedMultiplier: ClampNumber(globalMultiplier*modelMultiplier, SiteRuntimeMinMultiplier*SiteRuntimeMinMultiplier, 1),
		GlobalBreakerOpen:  isRuntimeHealthBreakerOpen(globalState),
		ModelBreakerOpen:   isRuntimeHealthBreakerOpen(modelState),
		ModelKey:           modelKey,
		RecentSuccessRate:  recentSnapshot.SuccessRate,
		RecentSampleCount:  recentSnapshot.SampleCount,
		RecentConfidence:   recentSnapshot.Confidence,
	}
}

// GetSiteRuntimeHealthMultiplier is a convenience wrapper.
func GetSiteRuntimeHealthMultiplier(siteID int64) float64 {
	healthStateMu.RLock()
	defer healthStateMu.RUnlock()

	state := siteRuntimeHealthStates[siteID]
	return GetRuntimeHealthMultiplier(state)
}

// IsSiteRuntimeBreakerOpen checks if a site's global breaker is open.
func IsSiteRuntimeBreakerOpen(siteID int64) bool {
	healthStateMu.RLock()
	defer healthStateMu.RUnlock()

	state := siteRuntimeHealthStates[siteID]
	return isRuntimeHealthBreakerOpen(state)
}

// FilterSiteRuntimeBrokenCandidates filters candidates whose site breaker is open.
func FilterSiteRuntimeBrokenCandidates[T interface{ GetSiteID() int64 }](candidates []T) []T {
	if len(candidates) <= 1 {
		return candidates
	}
	healthy := make([]T, 0, len(candidates))
	for _, c := range candidates {
		if !IsSiteRuntimeBreakerOpen(c.GetSiteID()) {
			healthy = append(healthy, c)
		}
	}
	if len(healthy) > 0 {
		return healthy
	}
	return candidates
}

// FilterSiteRuntimeBrokenCandidatesByModel filters by model-level breaker.
func FilterSiteRuntimeBrokenCandidatesByModel(
	candidates []RouteChannelCandidate,
	modelName string,
) (healthy []RouteChannelCandidate, avoided []struct {
	Candidate RouteChannelCandidate
	Reason    string
}) {
	if len(candidates) <= 1 {
		return candidates, nil
	}

	for _, candidate := range candidates {
		details := GetSiteRuntimeHealthDetails(candidate.Site.ID, modelName)
		blocked := details.GlobalBreakerOpen || details.ModelBreakerOpen
		if blocked {
			reason := buildRuntimeBreakerReason(details)
			avoided = append(avoided, struct {
				Candidate RouteChannelCandidate
				Reason    string
			}{candidate, reason})
		} else {
			healthy = append(healthy, candidate)
		}
	}

	if len(healthy) > 0 {
		return healthy, avoided
	}
	return candidates, nil
}

// FilterSiteRuntimeBrokenCandidatesByModelResolver filters by model-level breaker,
// resolving the model name per-candidate via the provided function.
// This is needed for display-name-matched routes where each candidate may have a different source model.
func FilterSiteRuntimeBrokenCandidatesByModelResolver(
	candidates []RouteChannelCandidate,
	resolveModel func(RouteChannelCandidate) string,
) (healthy []RouteChannelCandidate, avoided []struct {
	Candidate RouteChannelCandidate
	Reason    string
}) {
	if len(candidates) <= 1 {
		return candidates, nil
	}

	for _, candidate := range candidates {
		modelName := resolveModel(candidate)
		details := GetSiteRuntimeHealthDetails(candidate.Site.ID, modelName)
		blocked := details.GlobalBreakerOpen || details.ModelBreakerOpen
		if blocked {
			reason := buildRuntimeBreakerReason(details)
			avoided = append(avoided, struct {
				Candidate RouteChannelCandidate
				Reason    string
			}{candidate, reason})
		} else {
			healthy = append(healthy, candidate)
		}
	}

	if len(healthy) > 0 {
		return healthy, avoided
	}
	return candidates, nil
}

func buildRuntimeBreakerReason(details SiteRuntimeHealthDetails) string {
	if details.GlobalBreakerOpen && details.ModelBreakerOpen {
		return "站点熔断中，模型熔断中，优先避让"
	}
	if details.GlobalBreakerOpen {
		return "站点熔断中，优先避让"
	}
	if details.ModelBreakerOpen {
		return "模型熔断中，优先避让"
	}
	return "运行时熔断中，优先避让"
}

// ---- Outcome tracking ----

func decayRecentOutcomeCount(value float64, elapsedMs float64) float64 {
	if value <= 0 || !isFiniteFloat(value) {
		return 0
	}
	if elapsedMs <= 0 {
		return value
	}
	return value * math.Pow(0.5, elapsedMs/float64(SiteRecentOutcomeHalfLifeMs))
}

func buildRecentOutcomeSnapshot(successCount, failureCount float64) RecentOutcomeSnapshot {
	sc := math.Max(0, successCount)
	fc := math.Max(0, failureCount)
	sampleCount := sc + fc
	successRate := (sc + SiteRecentSuccessPriorSuccesses) / (sampleCount + SiteRecentSuccessPriorSuccesses + SiteRecentSuccessPriorFailures)
	return RecentOutcomeSnapshot{
		SuccessCount: sc,
		FailureCount: fc,
		SampleCount:  sampleCount,
		SuccessRate:  successRate,
		Confidence:   ClampNumber(sampleCount/SiteRecentSuccessConfidenceSamples, 0, 1),
	}
}

func getRecentOutcomeSnapshot(state *SiteRuntimeHealthState) RecentOutcomeSnapshot {
	if state == nil {
		return buildRecentOutcomeSnapshot(0, 0)
	}
	n := nowMs()
	updatedAtMs := state.RecentWindowUpdatedAtMs
	if updatedAtMs <= 0 {
		updatedAtMs = state.LastUpdatedAtMs
	}
	elapsedMs := float64(max64(0, n-updatedAtMs))
	return buildRecentOutcomeSnapshot(
		decayRecentOutcomeCount(state.RecentSuccessCount, elapsedMs),
		decayRecentOutcomeCount(state.RecentFailureCount, elapsedMs),
	)
}

func refreshRecentOutcomeWindow(state *SiteRuntimeHealthState) {
	snapshot := getRecentOutcomeSnapshot(state)
	state.RecentSuccessCount = snapshot.SuccessCount
	state.RecentFailureCount = snapshot.FailureCount
	state.RecentWindowUpdatedAtMs = nowMs()
}

func blendRecentOutcomeSnapshots(globalSnapshot RecentOutcomeSnapshot, modelSnapshot *RecentOutcomeSnapshot) RecentOutcomeSnapshot {
	if modelSnapshot == nil || modelSnapshot.SampleCount <= 0 {
		return globalSnapshot
	}
	modelWeight := SiteRecentModelWeight
	globalWeight := 1.0 - modelWeight
	return buildRecentOutcomeSnapshot(
		(globalSnapshot.SuccessCount*globalWeight)+(modelSnapshot.SuccessCount*modelWeight),
		(globalSnapshot.FailureCount*globalWeight)+(modelSnapshot.FailureCount*modelWeight),
	)
}

// ResolveStableFirstSuccessRate blends recent runtime success rate with historical rate.
func ResolveStableFirstSuccessRate(details SiteRuntimeHealthDetails, historicalSuccessRate *float64) float64 {
	fallbackRate := SiteRecentSuccessFallbackRate
	if historicalSuccessRate != nil {
		fallbackRate = *historicalSuccessRate
	}
	return (details.RecentSuccessRate * details.RecentConfidence) + (fallbackRate * (1 - details.RecentConfidence))
}

// ---- Failure / Success recording ----

func applyRuntimeHealthFailure(state *SiteRuntimeHealthState, ctx SiteRuntimeFailureContext) {
	n := nowMs()
	refreshRecentOutcomeWindow(state)
	state.RecentFailureCount += 1
	state.PenaltyScore += ResolveSiteRuntimeFailurePenalty(ctx)

	if IsTransientSiteRuntimeFailure(ctx) {
		if state.LastTransientFailureAtMs != nil && (n-*state.LastTransientFailureAtMs) <= SiteTransientStreakWindowMs {
			state.TransientFailureStreak += 1
		} else {
			state.TransientFailureStreak = 1
		}
		state.LastTransientFailureAtMs = &n
		if state.TransientFailureStreak >= SiteRuntimeBreakerStreakThreshold {
			state.BreakerLevel = min64(state.BreakerLevel+1, int64(len(SiteRuntimeBreakerLevelsMs)-1))
			breakerMs := resolveSiteRuntimeBreakerMs(state.BreakerLevel)
			if breakerMs > 0 {
				until := n + breakerMs
				state.BreakerUntilMs = &until
			} else {
				state.BreakerUntilMs = nil
			}
			state.TransientFailureStreak = 0
		}
	} else {
		state.TransientFailureStreak = 0
		state.LastTransientFailureAtMs = nil
	}
	state.LastFailureAtMs = &n
}

func applyRuntimeHealthSuccess(state *SiteRuntimeHealthState, latencyMs float64) {
	n := nowMs()
	refreshRecentOutcomeWindow(state)
	state.RecentSuccessCount += 1
	state.PenaltyScore = math.Max(0, state.PenaltyScore*0.2-0.3)
	state.TransientFailureStreak = 0
	state.LastTransientFailureAtMs = nil
	state.BreakerLevel = 0
	state.BreakerUntilMs = nil
	state.LastSuccessAtMs = &n

	if state.LatencyEMAMs == nil {
		state.LatencyEMAMs = &latencyMs
	} else {
		ema := (*state.LatencyEMAMs)*(1-SiteRuntimeLatencyEMAAlpha) + latencyMs*SiteRuntimeLatencyEMAAlpha
		state.LatencyEMAMs = &ema
	}
}

// RecordSiteRuntimeFailure records a failure against a site and model.
func RecordSiteRuntimeFailure(siteID int64, ctx SiteRuntimeFailureContext) {
	healthStateMu.Lock()
	defer healthStateMu.Unlock()

	globalState := getOrCreateSiteRuntimeHealthState(siteID)
	applyRuntimeHealthFailure(globalState, ctx)

	if ctx.ModelName != nil && *ctx.ModelName != "" {
		modelState := getOrCreateSiteModelRuntimeHealthState(siteID, *ctx.ModelName)
		if modelState != nil {
			applyRuntimeHealthFailure(modelState, ctx)
		}
	}
	scheduleSiteRuntimeHealthPersistence()
}

// RecordSiteRuntimeSuccess records a success against a site and model.
func RecordSiteRuntimeSuccess(siteID int64, latencyMs float64, modelName *string) {
	healthStateMu.Lock()
	defer healthStateMu.Unlock()

	globalState := getOrCreateSiteRuntimeHealthState(siteID)
	applyRuntimeHealthSuccess(globalState, latencyMs)

	if modelName != nil && *modelName != "" {
		modelState := getOrCreateSiteModelRuntimeHealthState(siteID, *modelName)
		if modelState != nil {
			applyRuntimeHealthSuccess(modelState, latencyMs)
		}
	}
	scheduleSiteRuntimeHealthPersistence()
}

// ---- Breaker filter helpers ----

// GetBreakerFilteredCandidatesByModel is public for use by selector.
func GetBreakerFilteredCandidatesByModel(
	candidates []RouteChannelCandidate,
	modelName string,
) (healthy []RouteChannelCandidate, avoided []struct {
	Candidate RouteChannelCandidate
	Reason    string
}) {
	return FilterSiteRuntimeBrokenCandidatesByModel(candidates, modelName)
}

// GetBreakerFilteredCandidatesByModelResolver is the per-candidate resolver variant.
// Use this for display-name-matched routes where each candidate may have a different source model.
func GetBreakerFilteredCandidatesByModelResolver(
	candidates []RouteChannelCandidate,
	resolveModel func(RouteChannelCandidate) string,
) (healthy []RouteChannelCandidate, avoided []struct {
	Candidate RouteChannelCandidate
	Reason    string
}) {
	return FilterSiteRuntimeBrokenCandidatesByModelResolver(candidates, resolveModel)
}

// ---- Historical health ----

// BuildSiteHistoricalHealthMetrics aggregates historical health per site.
func BuildSiteHistoricalHealthMetrics(candidates []RouteChannelCandidate) map[int64]SiteHistoricalHealthMetrics {
	type siteTotal struct {
		totalCalls     int64
		successCount   int64
		failCount      int64
		totalLatencyMs int64
		latencySamples int64
	}
	totals := make(map[int64]*siteTotal)
	for _, c := range candidates {
		siteID := c.Site.ID
		st, ok := totals[siteID]
		if !ok {
			st = &siteTotal{}
			totals[siteID] = st
		}
		sc := c.Channel.SuccessCount
		fc := c.Channel.FailCount
		if sc < 0 {
			sc = 0
		}
		if fc < 0 {
			fc = 0
		}
		st.successCount += sc
		st.failCount += fc
		st.totalCalls += sc + fc
		if sc > 0 {
			st.totalLatencyMs += max64(0, c.Channel.TotalLatencyMs)
			st.latencySamples += sc
		}
	}

	metrics := make(map[int64]SiteHistoricalHealthMetrics)
	for siteID, st := range totals {
		if st.totalCalls <= 0 {
			metrics[siteID] = SiteHistoricalHealthMetrics{Multiplier: 1, TotalCalls: 0}
			continue
		}
		sampleFactor := ClampNumber(float64(st.totalCalls)/SiteHistoricalHealthMaxSample, 0, 1)
		successRate := float64(st.successCount) / float64(st.totalCalls)
		successPenaltyFactor := 1.0 - ((1.0 - successRate) * 0.55 * sampleFactor)

		var avgLatencyMs *int64
		if st.latencySamples > 0 {
			avg := int64(math.Round(float64(st.totalLatencyMs) / float64(st.latencySamples)))
			avgLatencyMs = &avg
		}

		latencyPenaltyRatio := 0.0
		if avgLatencyMs != nil {
			latencyPenaltyRatio = ClampNumber(
				(float64(*avgLatencyMs)-SiteHistoricalLatencyBaselineMs)/SiteHistoricalLatencyWindowMs,
				0, 1,
			) * sampleFactor
		}
		latencyFactor := 1.0 - (latencyPenaltyRatio * SiteHistoricalMaxLatencyPenalty)

		metrics[siteID] = SiteHistoricalHealthMetrics{
			Multiplier:   ClampNumber(successPenaltyFactor*latencyFactor, SiteHistoricalHealthMinMultiplier, 1),
			TotalCalls:   st.totalCalls,
			SuccessRate:  &successRate,
			AvgLatencyMs: avgLatencyMs,
		}
	}
	return metrics
}

// ---- Persistence ----

func shouldPersistSiteRuntimeHealthState(state *SiteRuntimeHealthState) bool {
	n := nowMs()
	lastTouchedAtMs := n
	for _, v := range []*int64{&state.LastUpdatedAtMs, state.LastFailureAtMs, state.LastSuccessAtMs, state.LastTransientFailureAtMs} {
		if v != nil && *v > lastTouchedAtMs {
			lastTouchedAtMs = *v
		}
	}
	if n-lastTouchedAtMs > SiteRuntimeHealthPersistStaleTTLMs {
		return false
	}
	if isRuntimeHealthBreakerOpen(state) {
		return true
	}
	if getDecayedSiteRuntimePenalty(state) >= SiteRuntimeHealthPersistMinPenalty {
		return true
	}
	if getRecentOutcomeSnapshot(state).SampleCount > 0.01 {
		return true
	}
	if state.LatencyEMAMs != nil && *state.LatencyEMAMs > 0 {
		return true
	}
	return n-lastTouchedAtMs <= SiteRuntimeHealthPersistIdleTTLMs
}

func cloneSiteRuntimeHealthState(state *SiteRuntimeHealthState) *SiteRuntimeHealthState {
	clone := &SiteRuntimeHealthState{
		PenaltyScore:            state.PenaltyScore,
		TransientFailureStreak:  state.TransientFailureStreak,
		RecentSuccessCount:      state.RecentSuccessCount,
		RecentFailureCount:      state.RecentFailureCount,
		RecentWindowUpdatedAtMs: state.RecentWindowUpdatedAtMs,
		BreakerLevel:            state.BreakerLevel,
		LastUpdatedAtMs:         state.LastUpdatedAtMs,
	}
	if state.LatencyEMAMs != nil {
		v := *state.LatencyEMAMs
		clone.LatencyEMAMs = &v
	}
	if state.LastTransientFailureAtMs != nil {
		v := *state.LastTransientFailureAtMs
		clone.LastTransientFailureAtMs = &v
	}
	if state.BreakerUntilMs != nil {
		v := *state.BreakerUntilMs
		clone.BreakerUntilMs = &v
	}
	if state.LastFailureAtMs != nil {
		v := *state.LastFailureAtMs
		clone.LastFailureAtMs = &v
	}
	if state.LastSuccessAtMs != nil {
		v := *state.LastSuccessAtMs
		clone.LastSuccessAtMs = &v
	}
	return clone
}

func scheduleSiteRuntimeHealthPersistence() {
	if healthPersistTimer != nil {
		return
	}
	healthPersistTimer = time.AfterFunc(SiteRuntimeHealthPersistDebounceMs*time.Millisecond, func() {
		healthPersistTimer = nil
		persistSiteRuntimeHealthState()
	})
}

func persistSiteRuntimeHealthState() {
	if healthSettingsStore == nil {
		return
	}
	if healthPersistInFlight {
		return
	}
	healthPersistInFlight = true
	defer func() { healthPersistInFlight = false }()

	// Build payload under read lock, then serialize
	healthStateMu.RLock()
	payload := buildSiteRuntimeHealthPersistencePayload()
	healthStateMu.RUnlock()

	data, err := marshalJSON(payload)
	if err != nil {
		return
	}
	_ = healthSettingsStore.Set(SiteRuntimeHealthSettingKey, data)
}

func buildSiteRuntimeHealthPersistencePayload() SiteRuntimeHealthPersistencePayload {
	n := nowMs()
	payload := SiteRuntimeHealthPersistencePayload{
		Version:        1,
		SavedAtMs:      n,
		GlobalBySiteID: make(map[string]*SiteRuntimeHealthState),
		ModelBySiteID:  make(map[string]map[string]*SiteRuntimeHealthState),
	}

	for siteID, state := range siteRuntimeHealthStates {
		if shouldPersistSiteRuntimeHealthState(state) {
			payload.GlobalBySiteID[formatInt(siteID)] = cloneSiteRuntimeHealthState(state)
		}
	}
	for siteID, modelStates := range siteModelRuntimeHealthStates {
		persistedModels := make(map[string]*SiteRuntimeHealthState)
		for modelKey, state := range modelStates {
			if shouldPersistSiteRuntimeHealthState(state) {
				persistedModels[modelKey] = cloneSiteRuntimeHealthState(state)
			}
		}
		if len(persistedModels) > 0 {
			payload.ModelBySiteID[formatInt(siteID)] = persistedModels
		}
	}
	return payload
}

// ---- Reset / Flush / Load ----

// ResetSiteRuntimeHealthState clears all in-memory runtime health state.
func ResetSiteRuntimeHealthState() {
	healthStateMu.Lock()
	defer healthStateMu.Unlock()

	siteRuntimeHealthStates = make(map[int64]*SiteRuntimeHealthState)
	siteModelRuntimeHealthStates = make(map[int64]map[string]*SiteRuntimeHealthState)
	siteRuntimeHealthLoaded = false
	if healthPersistTimer != nil {
		healthPersistTimer.Stop()
		healthPersistTimer = nil
	}
	healthPersistInFlight = false
}

// ChannelRuntimeHealthRow describes a channel-row pair for health clearing.
type ChannelRuntimeHealthRow struct {
	SiteID            int64
	SourceModel       *string
	RouteModelPattern string
}

// ClearRuntimeHealthStatesForChannels clears the in-memory runtime health state
// for the site+model combinations of the given channel rows. Returns true if any
// state was cleared and persistence should be triggered.
func ClearRuntimeHealthStatesForChannels(rows []ChannelRuntimeHealthRow) bool {
	if len(rows) == 0 {
		return false
	}
	healthStateMu.Lock()
	defer healthStateMu.Unlock()

	cleared := false
	clearedSites := make(map[int64]bool)
	for _, row := range rows {
		if row.SiteID <= 0 {
			continue
		}
		if row.SourceModel != nil && *row.SourceModel != "" {
			modelKey := NormalizeChannelSourceModel(row.SourceModel)
			if modelKey != "" {
				if siteModels, ok := siteModelRuntimeHealthStates[row.SiteID]; ok {
					if _, exists := siteModels[modelKey]; exists {
						delete(siteModels, modelKey)
						cleared = true
					}
				}
			}
		}
		// Also clear global site health for sites we touched
		if !clearedSites[row.SiteID] {
			clearedSites[row.SiteID] = true
			if _, exists := siteRuntimeHealthStates[row.SiteID]; exists {
				delete(siteRuntimeHealthStates, row.SiteID)
				cleared = true
			}
		}
	}
	return cleared
}

// EnsureSiteRuntimeHealthStateLoaded lazy-loads health state from settings.
func EnsureSiteRuntimeHealthStateLoaded() error {
	if siteRuntimeHealthLoaded {
		return nil
	}
	healthStateMu.Lock()
	defer healthStateMu.Unlock()

	if siteRuntimeHealthLoaded {
		return nil
	}

	if healthSettingsStore != nil {
		raw, err := healthSettingsStore.Get(SiteRuntimeHealthSettingKey)
		if err == nil && raw != "" {
			var payload SiteRuntimeHealthPersistencePayload
			if err := unmarshalJSON(raw, &payload); err == nil && payload.Version == 1 {
				for key, state := range payload.GlobalBySiteID {
					siteID := parseInt64(key)
					if siteID > 0 {
						siteRuntimeHealthStates[siteID] = state
					}
				}
				for siteIDKey, modelStates := range payload.ModelBySiteID {
					siteID := parseInt64(siteIDKey)
					if siteID > 0 {
						hydrated := make(map[string]*SiteRuntimeHealthState)
						for modelKey, state := range modelStates {
							if modelKey != "" {
								hydrated[modelKey] = state
							}
						}
						if len(hydrated) > 0 {
							siteModelRuntimeHealthStates[siteID] = hydrated
						}
					}
				}
			}
		}
	}
	siteRuntimeHealthLoaded = true
	return nil
}

// FlushSiteRuntimeHealthPersistence flushes any pending persistence immediately.
func FlushSiteRuntimeHealthPersistence() {
	if healthPersistTimer != nil {
		healthPersistTimer.Stop()
		healthPersistTimer = nil
	}
	persistSiteRuntimeHealthState()
}

// ---- JSON helpers (avoid importing encoding/json for now, use simple marshaling) ----

func formatInt(v int64) string {
	return fmtInt(v)
}

func parseInt64(s string) int64 {
	var v int64
	// Simple ASCII parsing
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int64(c-'0')
		} else {
			return 0
		}
	}
	return v
}

func fmtInt(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte(v%10) + '0'
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func marshalJSON(v interface{}) (string, error) {
	// Use a simple JSON marshaler
	b, err := jsonMarshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalJSON(data string, v interface{}) error {
	return jsonUnmarshal([]byte(data), v)
}

// jsonMarshal marshals known types with encoding/json.
func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// jsonUnmarshal unmarshals known types with encoding/json.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func marshalPayload(p SiteRuntimeHealthPersistencePayload) ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')

	buf = append(buf, `"version":1`...)

	buf = append(buf, `,"savedAtMs":`...)
	buf = append(buf, fmtInt(p.SavedAtMs)...)

	buf = append(buf, `,"globalBySiteId":{`...)
	first := true
	for k, v := range p.GlobalBySiteID {
		if !first {
			buf = append(buf, ',')
		}
		first = false
		buf = append(buf, '"')
		buf = append(buf, k...)
		buf = append(buf, '"', ':')
		b := marshalState(v)
		buf = append(buf, b...)
	}
	buf = append(buf, '}')

	buf = append(buf, `,"modelBySiteId":{`...)
	first = true
	for k, v := range p.ModelBySiteID {
		if !first {
			buf = append(buf, ',')
		}
		first = false
		buf = append(buf, '"')
		buf = append(buf, k...)
		buf = append(buf, '"', ':', '{')
		innerFirst := true
		for mk, mv := range v {
			if !innerFirst {
				buf = append(buf, ',')
			}
			innerFirst = false
			buf = append(buf, '"')
			buf = append(buf, mk...)
			buf = append(buf, '"', ':')
			b := marshalState(mv)
			buf = append(buf, b...)
		}
		buf = append(buf, '}')
	}
	buf = append(buf, '}')

	buf = append(buf, '}')
	return buf, nil
}

func marshalState(s *SiteRuntimeHealthState) []byte {
	var buf []byte
	buf = append(buf, '{')
	buf = append(buf, `"penaltyScore":`...)
	buf = append(buf, fmtFloat(s.PenaltyScore)...)
	if s.LatencyEMAMs != nil {
		buf = append(buf, `,"latencyEmaMs":`...)
		buf = append(buf, fmtFloat(*s.LatencyEMAMs)...)
	} else {
		buf = append(buf, `,"latencyEmaMs":null`...)
	}
	buf = append(buf, `,"transientFailureStreak":`...)
	buf = append(buf, fmtInt(s.TransientFailureStreak)...)
	if s.LastTransientFailureAtMs != nil {
		buf = append(buf, `,"lastTransientFailureAtMs":`...)
		buf = append(buf, fmtInt(*s.LastTransientFailureAtMs)...)
	} else {
		buf = append(buf, `,"lastTransientFailureAtMs":null`...)
	}
	buf = append(buf, `,"recentSuccessCount":`...)
	buf = append(buf, fmtFloat(s.RecentSuccessCount)...)
	buf = append(buf, `,"recentFailureCount":`...)
	buf = append(buf, fmtFloat(s.RecentFailureCount)...)
	buf = append(buf, `,"recentWindowUpdatedAtMs":`...)
	buf = append(buf, fmtInt(s.RecentWindowUpdatedAtMs)...)
	buf = append(buf, `,"breakerLevel":`...)
	buf = append(buf, fmtInt(s.BreakerLevel)...)
	if s.BreakerUntilMs != nil {
		buf = append(buf, `,"breakerUntilMs":`...)
		buf = append(buf, fmtInt(*s.BreakerUntilMs)...)
	} else {
		buf = append(buf, `,"breakerUntilMs":null`...)
	}
	buf = append(buf, `,"lastUpdatedAtMs":`...)
	buf = append(buf, fmtInt(s.LastUpdatedAtMs)...)
	if s.LastFailureAtMs != nil {
		buf = append(buf, `,"lastFailureAtMs":`...)
		buf = append(buf, fmtInt(*s.LastFailureAtMs)...)
	} else {
		buf = append(buf, `,"lastFailureAtMs":null`...)
	}
	if s.LastSuccessAtMs != nil {
		buf = append(buf, `,"lastSuccessAtMs":`...)
		buf = append(buf, fmtInt(*s.LastSuccessAtMs)...)
	} else {
		buf = append(buf, `,"lastSuccessAtMs":null`...)
	}
	buf = append(buf, '}')
	return buf
}

func fmtFloat(v float64) string {
	// Simple formatting: use sprintf-like
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	s := ""
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	intPart := int64(math.Trunc(v))
	fracPart := v - float64(intPart)
	s = fmtInt(intPart)
	s += "."
	for i := 0; i < 6; i++ {
		fracPart *= 10
		d := int64(math.Trunc(fracPart))
		s += string(rune('0' + d))
		fracPart -= float64(d)
	}
	// Trim trailing zeros
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	if neg {
		return "-" + s
	}
	return s
}

func unmarshalPayload(data []byte, p *SiteRuntimeHealthPersistencePayload) error {
	// Simple JSON parser for the payload
	// This is called rarely; keep it simple.
	idx := 0
	skipWhitespace := func() {
		for idx < len(data) && (data[idx] == ' ' || data[idx] == '\n' || data[idx] == '\r' || data[idx] == '\t') {
			idx++
		}
	}
	readString := func() string {
		skipWhitespace()
		if idx >= len(data) || data[idx] != '"' {
			return ""
		}
		idx++ // skip opening quote
		start := idx
		for idx < len(data) {
			if data[idx] == '\\' {
				idx += 2
				continue
			}
			if data[idx] == '"' {
				break
			}
			idx++
		}
		s := string(data[start:idx])
		if idx < len(data) {
			idx++ // skip closing quote
		}
		return s
	}
	readNumber := func() int64 {
		skipWhitespace()
		neg := false
		if idx < len(data) && data[idx] == '-' {
			neg = true
			idx++
		}
		var v int64
		for idx < len(data) && data[idx] >= '0' && data[idx] <= '9' {
			v = v*10 + int64(data[idx]-'0')
			idx++
		}
		if neg {
			v = -v
		}
		return v
	}
	readObj := func() {
		skipWhitespace()
		if idx < len(data) && data[idx] == '{' {
			idx++
		}
	}
	skipToColon := func() {
		skipWhitespace()
		if idx < len(data) && data[idx] == ':' {
			idx++
		}
	}

	readObj()
	for idx < len(data) {
		skipWhitespace()
		if idx >= len(data) || data[idx] == '}' {
			break
		}
		key := readString()
		skipToColon()
		switch key {
		case "version":
			_ = readNumber()
		case "savedAtMs":
			p.SavedAtMs = readNumber()
		case "globalBySiteId":
			p.GlobalBySiteID = make(map[string]*SiteRuntimeHealthState)
			readObj()
			for {
				skipWhitespace()
				if idx >= len(data) || data[idx] == '}' {
					if idx < len(data) {
						idx++
					}
					break
				}
				if data[idx] == ',' {
					idx++
					continue
				}
				siteKey := readString()
				skipToColon()
				state := readHealthState()
				if state != nil {
					p.GlobalBySiteID[siteKey] = state
				}
			}
		case "modelBySiteId":
			p.ModelBySiteID = make(map[string]map[string]*SiteRuntimeHealthState)
			readObj()
			for {
				skipWhitespace()
				if idx >= len(data) || data[idx] == '}' {
					if idx < len(data) {
						idx++
					}
					break
				}
				if data[idx] == ',' {
					idx++
					continue
				}
				siteKey := readString()
				skipToColon()
				modelStates := make(map[string]*SiteRuntimeHealthState)
				readObj()
				for {
					skipWhitespace()
					if idx >= len(data) || data[idx] == '}' {
						if idx < len(data) {
							idx++
						}
						break
					}
					if data[idx] == ',' {
						idx++
						continue
					}
					modelKey := readString()
					skipToColon()
					state := readHealthState()
					if state != nil {
						modelStates[modelKey] = state
					}
				}
				if len(modelStates) > 0 {
					p.ModelBySiteID[siteKey] = modelStates
				}
			}
		default:
			// Skip unknown key
			skipValue()
		}
		skipWhitespace()
		if idx < len(data) && data[idx] == ',' {
			idx++
		}
	}
	return nil
}

func readHealthState() *SiteRuntimeHealthState {
	// Stub: the full deserialization is handled by unmarshalHealthPayload via encoding/json
	return nil
}

func skipValue() {
	// Simple value skipper
	depth := 0
	for depth >= 0 {
		// NOTE: this is a stub; in practice we should properly skip values
		// but since we're pre-filling after unmarshalPayload, we handle known keys only
		break
	}
}
