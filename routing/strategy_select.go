package routing

import (
	"math"
	"sort"
)

// SelectLeastBusyCandidate picks the candidate with the lowest load pressure.
// Prefer providers that expose lease/concurrency data; fall back to historical
// success/fail volume so the strategy still works without a load provider.
// Sticky exclude lists are applied upstream — this only ranks the filtered pool.
func SelectLeastBusyCandidate(
	candidates []RouteChannelCandidate,
	loadProvider ChannelLoadSnapshotProvider,
) *RouteChannelCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return &candidates[0]
	}

	type scored struct {
		idx   int
		score float64
	}
	ranked := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		score := leastBusyScore(c, loadProvider)
		ranked = append(ranked, scored{idx: i, score: score})
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].score != ranked[b].score {
			return ranked[a].score < ranked[b].score
		}
		ca, cb := candidates[ranked[a].idx], candidates[ranked[b].idx]
		if ca.Channel.Weight != cb.Channel.Weight {
			return ca.Channel.Weight > cb.Channel.Weight
		}
		return ca.Channel.ID < cb.Channel.ID
	})
	return &candidates[ranked[0].idx]
}

func leastBusyScore(c RouteChannelCandidate, loadProvider ChannelLoadSnapshotProvider) float64 {
	if loadProvider != nil {
		snap := loadProvider.GetChannelLoadSnapshot(ChannelLoadParams{
			ChannelID:            c.Channel.ID,
			AccountExtraConfig:   c.Account.ExtraConfig,
			AccountOAuthProvider: c.Account.OAuthProvider,
		})
		if snap.SessionScoped && snap.ConcurrencyLimit > 0 {
			active := float64(snap.ActiveLeaseCount) / math.Max(1, float64(snap.ConcurrencyLimit))
			waiting := float64(snap.WaitingCount) / math.Max(1, float64(snap.ConcurrencyLimit))
			sat := 0.0
			if snap.Saturated {
				sat = 1.0
			}
			return active + 0.5*waiting + sat
		}
	}
	total := float64(c.Channel.SuccessCount + c.Channel.FailCount)
	return total
}

// SelectLowestLatencyCandidate ranks by site runtime first-byte/total latency EMA.
func SelectLowestLatencyCandidate(
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
) *RouteChannelCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if modelResolver == nil {
		modelResolver = func(RouteChannelCandidate) string { return "" }
	}
	if len(candidates) == 1 {
		return &candidates[0]
	}

	type scored struct {
		idx     int
		latency float64
		known   bool
	}
	ranked := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		lat, known := resolveStrategyLatencyMs(c.Site.ID, modelResolver(c))
		ranked = append(ranked, scored{idx: i, latency: lat, known: known})
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].known != ranked[b].known {
			return ranked[a].known
		}
		if ranked[a].latency != ranked[b].latency {
			return ranked[a].latency < ranked[b].latency
		}
		ca, cb := candidates[ranked[a].idx], candidates[ranked[b].idx]
		return ca.Channel.ID < cb.Channel.ID
	})
	return &candidates[ranked[0].idx]
}

func resolveStrategyLatencyMs(siteID int64, model string) (float64, bool) {
	healthStateMu.RLock()
	defer healthStateMu.RUnlock()

	if model != "" {
		if byModel, ok := siteModelRuntimeHealthStates[siteID]; ok {
			if ms := byModel[model]; ms != nil {
				if ms.FirstByteEMAMs != nil && *ms.FirstByteEMAMs > 0 {
					return *ms.FirstByteEMAMs, true
				}
				if ms.LatencyEMAMs != nil && *ms.LatencyEMAMs > 0 {
					return *ms.LatencyEMAMs, true
				}
			}
		}
	}
	if state := siteRuntimeHealthStates[siteID]; state != nil {
		if state.FirstByteEMAMs != nil && *state.FirstByteEMAMs > 0 {
			return *state.FirstByteEMAMs, true
		}
		if state.LatencyEMAMs != nil && *state.LatencyEMAMs > 0 {
			return *state.LatencyEMAMs, true
		}
	}
	return math.Inf(1), false
}

// SelectLowestCostCandidate picks the candidate with the lowest effective unit cost.
func SelectLowestCostCandidate(
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
	pricingFn func(siteID, accountID int64, modelName string) *float64,
	fallbackUnitCost float64,
) *RouteChannelCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if modelResolver == nil {
		modelResolver = func(RouteChannelCandidate) string { return "" }
	}
	if len(candidates) == 1 {
		return &candidates[0]
	}

	type scored struct {
		idx  int
		cost float64
	}
	ranked := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		sig := EffectiveUnitCost(c, modelResolver(c), pricingFn, fallbackUnitCost)
		ranked = append(ranked, scored{idx: i, cost: sig.UnitCost})
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].cost != ranked[b].cost {
			return ranked[a].cost < ranked[b].cost
		}
		return candidates[ranked[a].idx].Channel.ID < candidates[ranked[b].idx].Channel.ID
	})
	return &candidates[ranked[0].idx]
}
