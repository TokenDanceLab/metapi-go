package routing

import (
	"math"
	"testing"
)

// mockLoadProvider implements ChannelLoadSnapshotProvider for strategy tests.
type mockLoadProvider map[int64]ChannelLoadSnapshot

func (m mockLoadProvider) GetChannelLoadSnapshot(params ChannelLoadParams) ChannelLoadSnapshot {
	if snap, ok := m[params.ChannelID]; ok {
		return snap
	}
	return ChannelLoadSnapshot{}
}

func strategyCandidates() []RouteChannelCandidate {
	// Same priority so named strategies compare purely on their score signal.
	// successCount=0 so EffectiveUnitCost uses configured unitCost (not observed avg).
	return []RouteChannelCandidate{
		makeCandidate(1, 10, 101, 100, 0, 0, 0, 0, 1.0, ptrFloat(0.05), 100.0, nil), // expensive
		makeCandidate(2, 20, 201, 10, 0, 0, 0, 0, 1.0, ptrFloat(0.01), 100.0, nil),  // cheap
		makeCandidate(3, 30, 301, 50, 0, 0, 0, 0, 1.0, ptrFloat(0.02), 100.0, nil),  // mid
	}
}

func TestNormalizeRouteRoutingStrategy_NamedAndAliases(t *testing.T) {
	cases := []struct {
		in   string
		want RouteRoutingStrategy
	}{
		{"", StrategyWeighted},
		{"weighted", StrategyWeighted},
		{"unknown", StrategyWeighted},
		{"round_robin", StrategyRoundRobin},
		{"round-robin", StrategyRoundRobin},
		{"stable_first", StrategyStableFirst},
		{"stable-first", StrategyStableFirst},
		{"least_busy", StrategyLeastBusy},
		{"least-busy", StrategyLeastBusy},
		{"LEAST_BUSY", StrategyLeastBusy},
		{"lowest_latency", StrategyLowestLatency},
		{"lowest-latency", StrategyLowestLatency},
		{"latency", StrategyLowestLatency},
		{"lowest_cost", StrategyLowestCost},
		{"lowest-cost", StrategyLowestCost},
		{"cost", StrategyLowestCost},
	}
	for _, tc := range cases {
		got := NormalizeRouteRoutingStrategy(tc.in)
		if got != tc.want {
			t.Errorf("NormalizeRouteRoutingStrategy(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsPriorityLayerRoutingStrategy(t *testing.T) {
	if !IsPriorityLayerRoutingStrategy(StrategyWeighted) {
		t.Fatal("weighted should use priority layers")
	}
	if !IsPriorityLayerRoutingStrategy(StrategyLeastBusy) {
		t.Fatal("least_busy should use priority layers")
	}
	if !IsPriorityLayerRoutingStrategy(StrategyLowestLatency) {
		t.Fatal("lowest_latency should use priority layers")
	}
	if !IsPriorityLayerRoutingStrategy(StrategyLowestCost) {
		t.Fatal("lowest_cost should use priority layers")
	}
	if IsPriorityLayerRoutingStrategy(StrategyRoundRobin) {
		t.Fatal("round_robin must not walk priority layers")
	}
	if IsPriorityLayerRoutingStrategy(StrategyStableFirst) {
		t.Fatal("stable_first must not walk priority layers")
	}
}

func TestSelectByNamedStrategy_LeastBusy(t *testing.T) {
	ResetSiteRuntimeHealthState()
	candidates := strategyCandidates()
	load := mockLoadProvider{
		1: {SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 9, WaitingCount: 2, Saturated: true},
		2: {SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 1, WaitingCount: 0, Saturated: false},
		3: {SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 5, WaitingCount: 1, Saturated: false},
	}
	result := SelectByNamedStrategy(StrategyLeastBusy, candidates, staticModel("gpt-4"), load, nil, 1.0)
	if result.Selected == nil {
		t.Fatal("expected selection")
	}
	if result.Selected.Channel.ID != 2 {
		t.Fatalf("least_busy selected ch=%d want 2 (lowest load)", result.Selected.Channel.ID)
	}
	// Invariant: selected score is min among details.
	if len(result.Details) != 3 {
		t.Fatalf("details=%d want 3", len(result.Details))
	}
	min := result.Details[0].Score
	for _, d := range result.Details[1:] {
		if d.Score < min-1e-12 {
			t.Fatalf("details not sorted by ascending score: first=%v later=%v", min, d.Score)
		}
	}
}

func TestSelectByNamedStrategy_LowestLatency(t *testing.T) {
	ResetSiteRuntimeHealthState()
	// Seed site latency: site 10 slow, site 20 fast, site 30 mid.
	// Use package-private helpers via RecordSiteRuntimeSuccess.
	// firstByte optional.
	RecordSiteRuntimeSuccess(10, 5000, strPtr("gpt-4"), 4000)
	RecordSiteRuntimeSuccess(20, 800, strPtr("gpt-4"), 120)
	RecordSiteRuntimeSuccess(30, 2000, strPtr("gpt-4"), 900)

	candidates := strategyCandidates()
	result := SelectByNamedStrategy(StrategyLowestLatency, candidates, staticModel("gpt-4"), nil, nil, 1.0)
	if result.Selected == nil {
		t.Fatal("expected selection")
	}
	if result.Selected.Channel.ID != 2 {
		t.Fatalf("lowest_latency selected ch=%d want 2 (site 20 TTFT=120)", result.Selected.Channel.ID)
	}
	// Prefer TTFT over total latency: site 10 has slow TTFT.
	if result.Details[0].Score >= 4000 {
		t.Fatalf("expected TTFT-based score < 4000, got %v", result.Details[0].Score)
	}
}

func TestSelectByNamedStrategy_LowestCost(t *testing.T) {
	ResetSiteRuntimeHealthState()
	candidates := strategyCandidates()
	// Channel 2 has configured unitCost 0.01 (cheapest).
	result := SelectByNamedStrategy(StrategyLowestCost, candidates, staticModel("gpt-4"), nil, nil, 1.0)
	if result.Selected == nil {
		t.Fatal("expected selection")
	}
	if result.Selected.Channel.ID != 2 {
		t.Fatalf("lowest_cost selected ch=%d want 2 (unitCost=0.01)", result.Selected.Channel.ID)
	}
	// Score equals EffectiveUnitCost of selected.
	signal := EffectiveUnitCost(*result.Selected, "gpt-4", nil, 1.0)
	if math.Abs(result.Details[0].Score-signal.UnitCost) > 1e-9 {
		t.Fatalf("score=%v want unitCost=%v", result.Details[0].Score, signal.UnitCost)
	}
}

func TestSelectByNamedStrategy_PriorityDoesNotChangeArgminInsideLayer(t *testing.T) {
	// Named strategies are pure argmin; priority layering is selector responsibility.
	// With mixed priorities still passed in, pure function still picks global min cost.
	// successCount=0 so EffectiveUnitCost uses configured unitCost (not observed).
	ResetSiteRuntimeHealthState()
	candidates := []RouteChannelCandidate{
		makeCandidate(10, 1, 1, 10, 0, 0, 0, 0, 1.0, ptrFloat(0.10), 1, nil),
		makeCandidate(11, 2, 2, 10, 1, 0, 0, 0, 1.0, ptrFloat(0.01), 1, nil), // cheaper but lower priority
	}
	result := SelectByNamedStrategy(StrategyLowestCost, candidates, staticModel("gpt-4"), nil, nil, 1.0)
	if result.Selected == nil || result.Selected.Channel.ID != 11 {
		t.Fatalf("pure lowest_cost should pick cheapest regardless of priority, got %#v", result.Selected)
	}
}

func TestSelectByNamedStrategy_EmptyAndUnknown(t *testing.T) {
	if res := SelectByNamedStrategy(StrategyLeastBusy, nil, nil, nil, nil, 1); res.Selected != nil {
		t.Fatal("empty candidates should not select")
	}
	if res := SelectByNamedStrategy(StrategyWeighted, strategyCandidates(), staticModel("gpt-4"), nil, nil, 1); res.Selected != nil {
		t.Fatal("weighted is not a pure named strategy path")
	}
}

func TestSelectByNamedStrategy_DeterministicTieBreakByChannelID(t *testing.T) {
	ResetSiteRuntimeHealthState()
	// Identical costs → lower channel ID wins.
	candidates := []RouteChannelCandidate{
		makeCandidate(30, 1, 1, 10, 0, 0, 0, 0, 1.0, ptrFloat(0.02), 1, nil),
		makeCandidate(20, 2, 2, 10, 0, 0, 0, 0, 1.0, ptrFloat(0.02), 1, nil),
		makeCandidate(10, 3, 3, 10, 0, 0, 0, 0, 1.0, ptrFloat(0.02), 1, nil),
	}
	result := SelectByNamedStrategy(StrategyLowestCost, candidates, staticModel("gpt-4"), nil, nil, 1.0)
	if result.Selected == nil || result.Selected.Channel.ID != 10 {
		t.Fatalf("tie-break want channel 10, got %#v", result.Selected)
	}
}

func strPtr(s string) *string { return &s }
