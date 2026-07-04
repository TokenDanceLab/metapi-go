package routing

import (
	"encoding/json"
	"math"
	"math/rand"
	"sort"
)

// WeightedSelectionMode is either "weighted" or "stable_first".
type WeightedSelectionMode string

const (
	WeightedMode    WeightedSelectionMode = "weighted"
	StableFirstMode WeightedSelectionMode = "stable_first"
)

// WeightedSelectionResult holds the selection result and detailed info.
type WeightedSelectionResult struct {
	Selected        *RouteChannelCandidate
	Details         []WeightedDetail
	StableSiteCount int
}

// WeightedDetail describes a single candidate's score breakdown.
type WeightedDetail struct {
	Candidate   RouteChannelCandidate
	Probability float64
	Reason      string
}

// EffectiveUnitCost resolves the best available unit cost for a candidate.
func EffectiveUnitCost(candidate RouteChannelCandidate, modelName string, pricingFn func(siteID, accountID int64, modelName string) *float64, fallbackUnitCost float64) CostSignal {
	minCost := 1e-6
	successCount := candidate.Channel.SuccessCount
	if successCount < 0 {
		successCount = 0
	}
	totalCost := candidate.Channel.TotalCost
	if totalCost < 0 {
		totalCost = 0
	}

	if successCount > 0 && totalCost > 0 {
		return CostSignal{
			UnitCost: math.Max(totalCost/float64(successCount), minCost),
			Source:   "observed",
		}
	}

	if candidate.Account.UnitCost != nil && *candidate.Account.UnitCost > 0 && isFiniteFloat(*candidate.Account.UnitCost) {
		return CostSignal{
			UnitCost: math.Max(*candidate.Account.UnitCost, minCost),
			Source:   "configured",
		}
	}

	if pricingFn != nil {
		if catalogCost := pricingFn(candidate.Site.ID, candidate.Account.ID, modelName); catalogCost != nil && *catalogCost > 0 && isFiniteFloat(*catalogCost) {
			return CostSignal{
				UnitCost: math.Max(*catalogCost, minCost),
				Source:   "catalog",
			}
		}
	}

	return CostSignal{
		UnitCost: math.Max(fallbackUnitCost, minCost),
		Source:   "fallback",
	}
}

// CalculateWeightedSelection performs the full weighted random selection algorithm.
// Implements the TS calculateWeightedSelection exactly.
func CalculateWeightedSelection(
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
	routingWeights RoutingWeightsConfig,
	siteWeightMultipliers map[int64]float64,
	channelLoadProvider ChannelLoadSnapshotProvider,
	nowMs int64,
	mode WeightedSelectionMode,
	stableFirstRotationKey string,
	pricingFn func(siteID, accountID int64, modelName string) *float64,
	fallbackUnitCost float64,
) WeightedSelectionResult {
	if len(candidates) == 0 {
		return WeightedSelectionResult{}
	}

	if modelResolver == nil {
		modelResolver = func(c RouteChannelCandidate) string { return "" }
	}

	n := len(candidates)

	// Step 1: Effective costs
	effectiveCosts := make([]CostSignal, n)
	for i, c := range candidates {
		effectiveCosts[i] = EffectiveUnitCost(c, modelResolver(c), pricingFn, fallbackUnitCost)
	}

	// Step 2: Runtime health details
	runtimeHealthDetails := make([]SiteRuntimeHealthDetails, n)
	for i, c := range candidates {
		runtimeHealthDetails[i] = GetSiteRuntimeHealthDetails(c.Site.ID, modelResolver(c))
	}

	// Step 3: Channel load snapshots
	channelLoadSnapshots := make([]ChannelLoadSnapshot, n)
	if channelLoadProvider != nil {
		for i, c := range candidates {
			channelLoadSnapshots[i] = channelLoadProvider.GetChannelLoadSnapshot(ChannelLoadParams{
				ChannelID:           c.Channel.ID,
				AccountExtraConfig:  c.Account.ExtraConfig,
				AccountOAuthProvider: c.Account.OAuthProvider,
			})
		}
	}

	// Step 4: Value scores
	valueScores := make([]float64, n)
	for i, c := range candidates {
		unitCost := effectiveCosts[i].UnitCost
		balance := c.Account.Balance
		totalUsed := float64(c.Channel.SuccessCount + c.Channel.FailCount)
		recentUsage := math.Max(totalUsed, 1)
		valueScores[i] = routingWeights.CostWeight*(1/unitCost) + routingWeights.BalanceWeight*balance + routingWeights.UsageWeight*(1/recentUsage)
	}

	// Step 5: Min-max normalization
	maxVS := maxFloatSlice(valueScores, 0.001)
	minVS := minFloatSlice(valueScores, 0)
	vsRange := maxVS - minVS
	if vsRange == 0 {
		vsRange = 1
	}
	normalizedVS := make([]float64, n)
	for i := range valueScores {
		normalizedVS[i] = (valueScores[i] - minVS) / vsRange
	}

	// Step 6: Base contributions
	baseContributions := make([]float64, n)
	for i, c := range candidates {
		weight := float64(c.Channel.Weight)
		baseContributions[i] = (weight + 10) * (routingWeights.BaseWeightFactor + normalizedVS[i]*routingWeights.ValueScoreFactor)
	}

	// Step 7: Site channel counts
	siteChannelCounts := make(map[int64]int)
	for _, c := range candidates {
		siteChannelCounts[c.Site.ID]++
	}
	siteHistoricalHealthMetrics := BuildSiteHistoricalHealthMetrics(candidates)

	// Step 8: Contributions
	contributions := make([]float64, n)
	for i, candidate := range candidates {
		siteChannels := math.Max(1, float64(siteChannelCounts[candidate.Site.ID]))
		runtimeMultiplier := runtimeHealthDetails[i].CombinedMultiplier
		runtimeLoadMultiplier := ResolveChannelRuntimeLoadMultiplier(channelLoadSnapshots[i])

		if mode == StableFirstMode {
			recentSuccessRate := ResolveStableFirstSuccessRate(
				runtimeHealthDetails[i],
				siteHistoricalHealthMetrics[candidate.Site.ID].SuccessRate,
			)
			contribution := math.Max(1e-4, recentSuccessRate*recentSuccessRate)
			contribution *= runtimeMultiplier
			contribution *= runtimeLoadMultiplier
			contributions[i] = contribution / siteChannels
			continue
		}

		contribution := baseContributions[i] / siteChannels

		// Site weight
		downstreamSiteMultiplier := 1.0
		if w, ok := siteWeightMultipliers[candidate.Site.ID]; ok && w > 0 && isFiniteFloat(w) {
			downstreamSiteMultiplier = w
		}
		siteGlobalWeight := 1.0
		if candidate.Site.GlobalWeight > 0 && isFiniteFloat(candidate.Site.GlobalWeight) {
			siteGlobalWeight = candidate.Site.GlobalWeight
		}
		combinedSiteWeight := siteGlobalWeight * downstreamSiteMultiplier
		if combinedSiteWeight > 0 && isFiniteFloat(combinedSiteWeight) {
			contribution *= combinedSiteWeight
		}

		contribution *= runtimeMultiplier
		if m, ok := siteHistoricalHealthMetrics[candidate.Site.ID]; ok {
			contribution *= m.Multiplier
		}
		contribution *= runtimeLoadMultiplier

		// Fallback cost penalty
		if effectiveCosts[i].Source == "fallback" {
			contribution *= 1 / math.Max(1, effectiveCosts[i].UnitCost)
		}

		contributions[i] = contribution
	}

	// Total contribution
	totalContribution := 0.0
	for _, c := range contributions {
		totalContribution += c
	}

	// Rank by contribution
	rankedIndices := make([]int, n)
	for i := range rankedIndices {
		rankedIndices[i] = i
	}
	sort.SliceStable(rankedIndices, func(a, b int) bool {
		diff := contributions[rankedIndices[b]] - contributions[rankedIndices[a]]
		if math.Abs(diff) > 1e-9 {
			return diff > 0
		}
		return compareStableFirstCandidateOrder(candidates[rankedIndices[a]], candidates[rankedIndices[b]]) < 0
	})

	// Select
	var selected *RouteChannelCandidate
	if mode == WeightedMode {
		r := rand.Float64() * totalContribution
		selected = &candidates[len(candidates)-1]
		for i := 0; i < n; i++ {
			r -= contributions[i]
			if r <= 0 {
				selected = &candidates[i]
				break
			}
		}
	} else {
		// stable_first: deterministic selection with site rotation
		sel := selectStableFirstCandidate(candidates, contributions, rankedIndices, stableFirstRotationKey)
		if sel != nil {
			selected = sel
		}
	}

	// Build stable site info
	stableSiteLeaderIndices := getStableFirstSiteLeaderIndices(candidates, contributions, rankedIndices)
	stableSiteIDs := make(map[int64]bool)
	for _, idx := range stableSiteLeaderIndices {
		stableSiteIDs[candidates[idx].Site.ID] = true
	}

	result := WeightedSelectionResult{
		Selected:        selected,
		StableSiteCount: len(stableSiteIDs),
	}

	// Build rank map
	rankByIndex := make(map[int]int)
	for rank, idx := range rankedIndices {
		rankByIndex[idx] = rank + 1
	}

	for i, candidate := range candidates {
		probability := 0.0
		if totalContribution > 0 {
			probability = contributions[i] / totalContribution
		}
		detail := WeightedDetail{
			Candidate:   candidate,
			Probability: probability,
		}
		result.Details = append(result.Details, detail)
		_ = detail // used below when we build actual reason strings
	}

	return result
}

// RoutingWeightsConfig mirrors config.RoutingWeights for the routing package.
type RoutingWeightsConfig struct {
	BaseWeightFactor float64
	ValueScoreFactor float64
	CostWeight       float64
	BalanceWeight    float64
	UsageWeight      float64
}

// ResolveChannelRuntimeLoadMultiplier converts a load snapshot to a multiplier.
func ResolveChannelRuntimeLoadMultiplier(snapshot ChannelLoadSnapshot) float64 {
	if !snapshot.SessionScoped || snapshot.ConcurrencyLimit <= 0 {
		return 1
	}
	activeRatio := ClampNumber(float64(snapshot.ActiveLeaseCount)/math.Max(1, float64(snapshot.ConcurrencyLimit)), 0, 1.5)
	waitingRatio := ClampNumber(float64(snapshot.WaitingCount)/math.Max(1, float64(snapshot.ConcurrencyLimit)), 0, 3)
	activePenalty := activeRatio * 0.28
	waitingPenalty := waitingRatio * 0.32
	saturationPenalty := 0.0
	if snapshot.Saturated {
		saturationPenalty = 0.12
	}
	return ClampNumber(1-activePenalty-waitingPenalty-saturationPenalty, 0.18, 1)
}

// ---- Stable-first helpers ----

func compareStableFirstCandidateOrder(left, right RouteChannelCandidate) int {
	// Compare by lastSelectedAt || lastUsedAt
	lo := left.Channel.LastSelectedAt
	if lo == nil {
		lo = left.Channel.LastUsedAt
	}
	ro := right.Channel.LastSelectedAt
	if ro == nil {
		ro = right.Channel.LastUsedAt
	}
	cmp := CompareNullableTimeAsc(lo, ro)
	if cmp != 0 {
		return cmp
	}
	cmp = CompareNullableTimeAsc(left.Channel.LastUsedAt, right.Channel.LastUsedAt)
	if cmp != 0 {
		return cmp
	}
	return int(left.Channel.ID - right.Channel.ID)
}

var (
	stableFirstLastSelectedSiteByKey              = make(map[string]int64)
	stableFirstObservationProgressByKey           = make(map[string]StableFirstObservationProgressState)
	stableFirstObservationSiteCooldownByKey       = make(map[string]int64)
	stableFirstStateMu                            sync_RWMutex
)

const (
	MaxStableFirstRotationKeys               = 1024
	MaxStableFirstObservationProgressKeys    = 1024
	MaxStableFirstObservationSiteCooldownKeys = 4096
)

// StableFirstObservationProgressState tracks observation gating progress.
type StableFirstObservationProgressState struct {
	RequestCount      int64
	LastObservationAtMs *int64
}

type sync_RWMutex = RWMutexStub

// RWMutexStub is a no-op mutex for single-goroutine usage.
type RWMutexStub struct{}

func (m *RWMutexStub) Lock()   {}
func (m *RWMutexStub) Unlock() {}
func (m *RWMutexStub) RLock()  {}
func (m *RWMutexStub) RUnlock() {}

func rememberStableFirstSiteSelectionForKey(rotationKey string, siteID int64) {
	if rotationKey == "" || siteID <= 0 {
		return
	}
	delete(stableFirstLastSelectedSiteByKey, rotationKey)
	stableFirstLastSelectedSiteByKey[rotationKey] = siteID
	for len(stableFirstLastSelectedSiteByKey) > MaxStableFirstRotationKeys {
		for k := range stableFirstLastSelectedSiteByKey {
			delete(stableFirstLastSelectedSiteByKey, k)
			break
		}
	}
}

func rememberStableFirstObservationProgressForKey(rotationKey string, state StableFirstObservationProgressState) {
	if rotationKey == "" {
		return
	}
	delete(stableFirstObservationProgressByKey, rotationKey)
	stableFirstObservationProgressByKey[rotationKey] = state
	for len(stableFirstObservationProgressByKey) > MaxStableFirstObservationProgressKeys {
		for k := range stableFirstObservationProgressByKey {
			delete(stableFirstObservationProgressByKey, k)
			break
		}
	}
}

func rememberStableFirstObservationSiteCooldown(rotationKey string, siteID int64, observedAtMs int64) {
	if rotationKey == "" || siteID <= 0 {
		return
	}
	scopedKey := rotationKey + ":" + formatInt(siteID)
	delete(stableFirstObservationSiteCooldownByKey, scopedKey)
	stableFirstObservationSiteCooldownByKey[scopedKey] = observedAtMs
	for len(stableFirstObservationSiteCooldownByKey) > MaxStableFirstObservationSiteCooldownKeys {
		for k := range stableFirstObservationSiteCooldownByKey {
			delete(stableFirstObservationSiteCooldownByKey, k)
			break
		}
	}
}

func getStableFirstSiteOrder(candidates []RouteChannelCandidate, siteID int64) int64 {
	order := int64(1<<63 - 1) // max int64
	for _, c := range candidates {
		if c.Site.ID != siteID {
			continue
		}
		if c.Channel.Priority < order {
			order = c.Channel.Priority
		}
	}
	if order == (1<<63 - 1) {
		return 0
	}
	return order
}

func getStableFirstOrderedSiteLeaderIndices(candidates []RouteChannelCandidate, stableSiteLeaderIndices []int) []int {
	sorted := make([]int, len(stableSiteLeaderIndices))
	copy(sorted, stableSiteLeaderIndices)
	sort.SliceStable(sorted, func(a, b int) bool {
		leftSiteID := candidates[sorted[a]].Site.ID
		rightSiteID := candidates[sorted[b]].Site.ID
		ordDiff := getStableFirstSiteOrder(candidates, leftSiteID) - getStableFirstSiteOrder(candidates, rightSiteID)
		if ordDiff != 0 {
			return ordDiff < 0
		}
		return candidates[sorted[a]].Channel.ID < candidates[sorted[b]].Channel.ID
	})
	return sorted
}

func getStableFirstSiteLeaderIndices(candidates []RouteChannelCandidate, contributions []float64, rankedIndices []int) []int {
	if len(rankedIndices) <= 1 {
		return rankedIndices
	}

	// Collect site leaders
	var siteLeaderIndices []int
	seenSiteIDs := make(map[int64]bool)
	for _, idx := range rankedIndices {
		siteID := candidates[idx].Site.ID
		if seenSiteIDs[siteID] {
			continue
		}
		seenSiteIDs[siteID] = true
		siteLeaderIndices = append(siteLeaderIndices, idx)
	}

	if len(siteLeaderIndices) <= 1 {
		return siteLeaderIndices
	}

	// Filter to leaders close to best
	bestIdx := siteLeaderIndices[0]
	if len(rankedIndices) > 0 {
		bestIdx = rankedIndices[0]
	}
	bestContribution := contributions[bestIdx]
	var stableLeaderIndices []int
	for _, idx := range siteLeaderIndices {
		if IsContributionCloseToBest(contributions[idx], bestContribution, StableFirstSiteScoreRatio) {
			stableLeaderIndices = append(stableLeaderIndices, idx)
		}
	}
	if len(stableLeaderIndices) > 0 {
		return stableLeaderIndices
	}
	return siteLeaderIndices
}

const StableFirstSiteScoreRatio = 0.92

func selectStableFirstCandidate(candidates []RouteChannelCandidate, contributions []float64, rankedIndices []int, rotationKey string) *RouteChannelCandidate {
	stableSiteLeaderIndices := getStableFirstSiteLeaderIndices(candidates, contributions, rankedIndices)
	if len(stableSiteLeaderIndices) == 0 {
		if len(rankedIndices) > 0 {
			return &candidates[rankedIndices[0]]
		}
		return nil
	}

	orderedSiteLeaderIndices := getStableFirstOrderedSiteLeaderIndices(candidates, stableSiteLeaderIndices)

	lastSelectedSiteID := stableFirstLastSelectedSiteByKey[rotationKey]
	lastSelectedIndex := -1
	for i, idx := range orderedSiteLeaderIndices {
		if candidates[idx].Site.ID == lastSelectedSiteID {
			lastSelectedIndex = i
			break
		}
	}

	nextIndex := 0
	if lastSelectedIndex >= 0 {
		nextIndex = (lastSelectedIndex + 1) % len(orderedSiteLeaderIndices)
	}
	selectedSiteLeader := orderedSiteLeaderIndices[nextIndex]
	selectedSiteID := candidates[selectedSiteLeader].Site.ID

	// Pick the highest-ranked candidate on that site
	for _, idx := range rankedIndices {
		if candidates[idx].Site.ID == selectedSiteID {
			return &candidates[idx]
		}
	}
	return &candidates[selectedSiteLeader]
}

// BuildStableFirstRotationKey creates a rotation key from route ID and model.
func BuildStableFirstRotationKey(routeID int64, requestedModel string) string {
	normalizedModel := NormalizeModelAlias(requestedModel)
	if normalizedModel == "" {
		normalizedModel = NormalizeRouteDisplayName(nil) // won't work, fallback
		if normalizedModel == "" {
			normalizedModel = formatInt(routeID)
		}
	}
	return formatInt(routeID) + ":" + normalizedModel
}

// ClearStableFirstCachesForRoute clears rotation/progress/cooldown for a route.
func ClearStableFirstCachesForRoute(routeID int64) {
	routePrefix := formatInt(routeID) + ":"
	for k := range stableFirstLastSelectedSiteByKey {
		if len(k) >= len(routePrefix) && k[:len(routePrefix)] == routePrefix {
			delete(stableFirstLastSelectedSiteByKey, k)
		}
	}
	for k := range stableFirstObservationProgressByKey {
		if len(k) >= len(routePrefix) && k[:len(routePrefix)] == routePrefix {
			delete(stableFirstObservationProgressByKey, k)
		}
	}
	for k := range stableFirstObservationSiteCooldownByKey {
		if len(k) >= len(routePrefix) && k[:len(routePrefix)] == routePrefix {
			delete(stableFirstObservationSiteCooldownByKey, k)
		}
	}
}

// ShouldUseStableFirstObservationCandidate checks if it's time to probe observation pool.
func ShouldUseStableFirstObservationCandidate(rotationKey string, observationCandidates []RouteChannelCandidate) bool {
	if rotationKey == "" || len(observationCandidates) == 0 {
		return false
	}
	state, ok := stableFirstObservationProgressByKey[rotationKey]
	if !ok {
		state = StableFirstObservationProgressState{}
	}
	if (state.RequestCount + 1) < StableFirstObservationRequestInterval {
		return false
	}
	n := nowMs()
	for _, c := range observationCandidates {
		scopedKey := rotationKey + ":" + formatInt(c.Site.ID)
		observedAtMs, exists := stableFirstObservationSiteCooldownByKey[scopedKey]
		if !exists || (n-observedAtMs) >= StableFirstObservationSiteCooldownMs {
			return true
		}
	}
	return false
}

// UpdateStableFirstObservationProgress updates observation progress after a selection.
func UpdateStableFirstObservationProgress(rotationKey string, usedObservation bool, selectedSiteID int64) {
	if rotationKey == "" {
		return
	}
	n := nowMs()
	previous, ok := stableFirstObservationProgressByKey[rotationKey]
	if !ok {
		previous = StableFirstObservationProgressState{}
	}
	if usedObservation {
		rememberStableFirstObservationProgressForKey(rotationKey, StableFirstObservationProgressState{
			RequestCount:      0,
			LastObservationAtMs: &n,
		})
		if selectedSiteID > 0 {
			rememberStableFirstObservationSiteCooldown(rotationKey, selectedSiteID, n)
		}
		return
	}
	rememberStableFirstObservationProgressForKey(rotationKey, StableFirstObservationProgressState{
		RequestCount:      previous.RequestCount + 1,
		LastObservationAtMs: previous.LastObservationAtMs,
	})
}

// BuildStableFirstPoolPlan splits candidates into primary and observation pools.
func BuildStableFirstPoolPlan(candidates []RouteChannelCandidate, modelResolver func(RouteChannelCandidate) string) StableFirstPoolPlan {
	if len(candidates) == 0 {
		return StableFirstPoolPlan{}
	}

	historicalBySiteID := BuildSiteHistoricalHealthMetrics(candidates)
	leaderBySiteID := make(map[int64]RouteChannelCandidate)
	siteStateByID := make(map[int64]StableFirstSitePoolState)

	for _, c := range candidates {
		siteID := c.Site.ID
		currentLeader, ok := leaderBySiteID[siteID]
		if !ok || compareStableFirstCandidateOrder(c, currentLeader) < 0 {
			leaderBySiteID[siteID] = c
		}
	}

	for siteID, leader := range leaderBySiteID {
		resolvedModel := ""
		if modelResolver != nil {
			resolvedModel = modelResolver(leader)
		}
		healthDetails := GetSiteRuntimeHealthDetails(siteID, resolvedModel)
		historical := historicalBySiteID[siteID]
		historicalTotalCalls := historical.TotalCalls
		effectiveSuccessRate := ResolveStableFirstSuccessRate(healthDetails, historical.SuccessRate)
		trusted := healthDetails.RecentConfidence >= StableFirstTrustedRecentConfidence || historicalTotalCalls >= StableFirstTrustedHistoricalCalls
		siteStateByID[siteID] = StableFirstSitePoolState{
			SiteID:               siteID,
			Leader:               leader,
			EffectiveSuccessRate: effectiveSuccessRate,
			Trusted:              trusted,
		}
	}

	// Sort all site states by effective success rate descending
	type siteStateEntry struct {
		siteID int64
		state  StableFirstSitePoolState
	}
	var allEntries []siteStateEntry
	for siteID, state := range siteStateByID {
		allEntries = append(allEntries, siteStateEntry{siteID, state})
	}
	sort.SliceStable(allEntries, func(a, b int) bool {
		diff := allEntries[b].state.EffectiveSuccessRate - allEntries[a].state.EffectiveSuccessRate
		if math.Abs(diff) > 1e-9 {
			return diff > 0
		}
		return compareStableFirstCandidateOrder(allEntries[a].state.Leader, allEntries[b].state.Leader) < 0
	})

	// Build trusted leader pool
	var trustedEntries []siteStateEntry
	for _, e := range allEntries {
		if e.state.Trusted {
			trustedEntries = append(trustedEntries, e)
		}
	}
	leaderPool := trustedEntries
	if len(leaderPool) == 0 {
		leaderPool = allEntries
	}

	thresholdRate := 0.0
	if len(leaderPool) > 0 && leaderPool[0].state.EffectiveSuccessRate > 0 {
		thresholdRate = leaderPool[0].state.EffectiveSuccessRate * StableFirstPrimarySuccessRateRatio
	}

	plan := StableFirstPoolPlan{}

	for _, e := range allEntries {
		inPrimary := false
		if len(leaderPool) > 0 {
			for _, le := range leaderPool {
				if le.siteID == e.siteID && e.state.EffectiveSuccessRate >= thresholdRate {
					inPrimary = true
					break
				}
			}
		}
		if inPrimary {
			plan.PrimarySiteIDs = append(plan.PrimarySiteIDs, e.siteID)
		} else {
			plan.ObservationSiteIDs = append(plan.ObservationSiteIDs, e.siteID)
			state := e.state
			if state.Trusted {
				state.ObservationReason = "观察池：近期成功率暂时落后，仅灰度真实流量会命中"
			} else {
				state.ObservationReason = "观察池：近期样本不足，仅灰度真实流量会命中"
			}
			siteStateByID[e.siteID] = state
		}
	}

	// If primary pool is empty, promote top site
	if len(plan.PrimarySiteIDs) == 0 && len(allEntries) > 0 {
		plan.PrimarySiteIDs = append(plan.PrimarySiteIDs, allEntries[0].siteID)
		// Remove from observation
		filtered := make([]int64, 0)
		for _, sid := range plan.ObservationSiteIDs {
			if sid != allEntries[0].siteID {
				filtered = append(filtered, sid)
			}
		}
		plan.ObservationSiteIDs = filtered
	}

	// Collect candidates
	primarySet := make(map[int64]bool)
	for _, sid := range plan.PrimarySiteIDs {
		primarySet[sid] = true
	}
	observationSet := make(map[int64]bool)
	for _, sid := range plan.ObservationSiteIDs {
		observationSet[sid] = true
	}
	for _, c := range candidates {
		if primarySet[c.Site.ID] {
			plan.PrimaryCandidates = append(plan.PrimaryCandidates, c)
		} else if observationSet[c.Site.ID] {
			plan.ObservationCandidates = append(plan.ObservationCandidates, c)
		}
	}
	plan.SiteStateByID = siteStateByID

	return plan
}

// StableFirstPoolPlan is the result of splitting candidates into pools.
type StableFirstPoolPlan struct {
	PrimaryCandidates     []RouteChannelCandidate
	ObservationCandidates []RouteChannelCandidate
	PrimarySiteIDs        []int64
	ObservationSiteIDs    []int64
	SiteStateByID         map[int64]StableFirstSitePoolState
}

// StableFirstSitePoolState is per-site state in a pool plan.
type StableFirstSitePoolState struct {
	SiteID               int64
	Leader               RouteChannelCandidate
	EffectiveSuccessRate float64
	Trusted              bool
	ObservationReason    string
}

// MarshalJSON helpers for the runtime health payload
func marshalHealthPayload(v SiteRuntimeHealthPersistencePayload) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func unmarshalHealthPayload(data string) (*SiteRuntimeHealthPersistencePayload, error) {
	var v SiteRuntimeHealthPersistencePayload
	err := json.Unmarshal([]byte(data), &v)
	return &v, err
}
