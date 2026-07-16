package routing

import (
	"math"
	"sort"
)

// StrategySelectionResult is the outcome of a named deterministic strategy.
type StrategySelectionResult struct {
	Selected *RouteChannelCandidate
	Details  []StrategySelectionDetail
}

// StrategySelectionDetail explains one candidate under a named strategy.
type StrategySelectionDetail struct {
	Candidate RouteChannelCandidate
	Score     float64
	Reason    string
}

// SelectByNamedStrategy picks a channel under a pure named strategy
// (least_busy / lowest_latency / lowest_cost). Unknown strategies return nil.
//
// All three keep priority-layer filtering outside this function (caller walks P).
// Sticky preferred-channel and exclude lists are also applied by the caller
// before candidates reach this function.
func SelectByNamedStrategy(
	strategy RouteRoutingStrategy,
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
	channelLoadProvider ChannelLoadSnapshotProvider,
	pricingFn func(siteID, accountID int64, modelName string) *float64,
	fallbackUnitCost float64,
) StrategySelectionResult {
	if len(candidates) == 0 {
		return StrategySelectionResult{}
	}
	if modelResolver == nil {
		modelResolver = func(RouteChannelCandidate) string { return "" }
	}
	switch NormalizeRouteRoutingStrategy(string(strategy)) {
	case StrategyLeastBusy:
		return selectLeastBusy(candidates, channelLoadProvider)
	case StrategyLowestLatency:
		return selectLowestLatency(candidates, modelResolver)
	case StrategyLowestCost:
		return selectLowestCost(candidates, modelResolver, pricingFn, fallbackUnitCost)
	default:
		return StrategySelectionResult{}
	}
}

type strategyRank struct {
	idx    int
	score  float64 // lower is better
	reason string
}

func selectLeastBusy(candidates []RouteChannelCandidate, loadProvider ChannelLoadSnapshotProvider) StrategySelectionResult {
	ranks := make([]strategyRank, 0, len(candidates))
	for i, c := range candidates {
		snap := ChannelLoadSnapshot{}
		if loadProvider != nil {
			snap = loadProvider.GetChannelLoadSnapshot(ChannelLoadParams{
				ChannelID:            c.Channel.ID,
				AccountExtraConfig:   c.Account.ExtraConfig,
				AccountOAuthProvider: c.Account.OAuthProvider,
			})
		}
		score, reason := leastBusyScore(snap)
		ranks = append(ranks, strategyRank{idx: i, score: score, reason: reason})
	}
	return pickLowestScore(candidates, ranks)
}

func leastBusyScore(snap ChannelLoadSnapshot) (float64, string) {
	// Without session/concurrency telemetry, treat as idle (score 0).
	if !snap.SessionScoped || snap.ConcurrencyLimit <= 0 {
		return 0, "least_busy: no concurrency telemetry (treated idle)"
	}
	active := float64(snap.ActiveLeaseCount)
	waiting := float64(snap.WaitingCount)
	limit := math.Max(1, float64(snap.ConcurrencyLimit))
	// Prefer free capacity; waiting queue is more expensive than an active lease.
	score := active/limit + 2.0*(waiting/limit)
	if snap.Saturated {
		score += 1.0
	}
	return score, "least_busy: active/limit + 2*waiting/limit (+1 if saturated)"
}

func selectLowestLatency(candidates []RouteChannelCandidate, modelResolver func(RouteChannelCandidate) string) StrategySelectionResult {
	ranks := make([]strategyRank, 0, len(candidates))
	for i, c := range candidates {
		model := modelResolver(c)
		score, reason := lowestLatencyScore(c.Site.ID, model)
		ranks = append(ranks, strategyRank{idx: i, score: score, reason: reason})
	}
	return pickLowestScore(candidates, ranks)
}

func lowestLatencyScore(siteID int64, modelName string) (float64, string) {
	// Prefer TTFT/first-byte EMA (#113), then total latency EMA.
	// Unknown latency sorts last (math.MaxFloat64) so observed channels win.
	const unknown = math.MaxFloat64

	healthStateMu.RLock()
	defer healthStateMu.RUnlock()

	// Model-scoped first, then global site state.
	if state := peekRuntimeLatencyState(siteID, modelName); state != nil {
		if state.FirstByteEMAMs != nil && *state.FirstByteEMAMs > 0 {
			return *state.FirstByteEMAMs, "lowest_latency: first-byte EMA (model)"
		}
		if state.LatencyEMAMs != nil && *state.LatencyEMAMs > 0 {
			return *state.LatencyEMAMs, "lowest_latency: latency EMA (model)"
		}
	}
	if state := siteRuntimeHealthStates[siteID]; state != nil {
		if state.FirstByteEMAMs != nil && *state.FirstByteEMAMs > 0 {
			return *state.FirstByteEMAMs, "lowest_latency: first-byte EMA (site)"
		}
		if state.LatencyEMAMs != nil && *state.LatencyEMAMs > 0 {
			return *state.LatencyEMAMs, "lowest_latency: latency EMA (site)"
		}
	}
	return unknown, "lowest_latency: no latency sample (sorted last)"
}

func peekRuntimeLatencyState(siteID int64, modelName string) *SiteRuntimeHealthState {
	modelKey := NormalizeModelAlias(modelName)
	if modelKey == "" {
		return nil
	}
	if modelStates, ok := siteModelRuntimeHealthStates[siteID]; ok {
		return modelStates[modelKey]
	}
	return nil
}

func selectLowestCost(
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
	pricingFn func(siteID, accountID int64, modelName string) *float64,
	fallbackUnitCost float64,
) StrategySelectionResult {
	ranks := make([]strategyRank, 0, len(candidates))
	for i, c := range candidates {
		model := modelResolver(c)
		signal := EffectiveUnitCost(c, model, pricingFn, fallbackUnitCost)
		ranks = append(ranks, strategyRank{
			idx:    i,
			score:  signal.UnitCost,
			reason: "lowest_cost: unit_cost source=" + signal.Source,
		})
	}
	return pickLowestScore(candidates, ranks)
}

func pickLowestScore(candidates []RouteChannelCandidate, ranks []strategyRank) StrategySelectionResult {
	if len(ranks) == 0 {
		return StrategySelectionResult{}
	}
	// Stable sort: score ASC, then channel ID ASC for determinism.
	sort.SliceStable(ranks, func(a, b int) bool {
		da := ranks[a].score - ranks[b].score
		if math.Abs(da) > 1e-12 {
			return da < 0
		}
		return candidates[ranks[a].idx].Channel.ID < candidates[ranks[b].idx].Channel.ID
	})

	details := make([]StrategySelectionDetail, 0, len(ranks))
	for _, r := range ranks {
		details = append(details, StrategySelectionDetail{
			Candidate: candidates[r.idx],
			Score:     r.score,
			Reason:    r.reason,
		})
	}
	selected := candidates[ranks[0].idx]
	return StrategySelectionResult{
		Selected: &selected,
		Details:  details,
	}
}
