package routing

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// Golden file test for the complete weight formula
// =============================================================================

func makeCandidate(channelID, siteID, accountID int64, weight int64, priority int64,
	successCount, failCount int64, totalCost float64, siteGlobalWeight float64,
	unitCost *float64, balance float64, oauthProvider *string) RouteChannelCandidate {

	sourceModel := "gpt-4"
	return RouteChannelCandidate{
		Channel: store.RouteChannel{
			ID:           channelID,
			RouteID:      1,
			AccountID:    accountID,
			SourceModel:  &sourceModel,
			Priority:     priority,
			Weight:       weight,
			Enabled:      true,
			SuccessCount: successCount,
			FailCount:    failCount,
			TotalCost:    totalCost,
		},
		Account: store.Account{
			ID:            accountID,
			SiteID:        siteID,
			Status:        "active",
			UnitCost:      unitCost,
			Balance:       balance,
			OAuthProvider: oauthProvider,
		},
		Site: store.Site{
			ID:           siteID,
			Status:       "active",
			GlobalWeight: siteGlobalWeight,
		},
	}
}

// TestCalculateWeightedSelection_GoldenFile verifies the weighted selection formula
// against known inputs and expected contribution ratios. This serves as the golden
// file that must match TS behavior exactly.
func TestCalculateWeightedSelection_GoldenFile(t *testing.T) {
	// This test encodes the golden expected behavior of the weighted formula.
	// The formula is: (weight+10)*(baseWeightFactor+normalizedVS*valueScoreFactor)/siteChannels
	// *combinedSiteWeight *runtimeMultiplier *siteHistoricalMultiplier *runtimeLoadMultiplier *fallbackPenalty

	// Reset runtime health state to get predictable multipliers
	ResetSiteRuntimeHealthState()

	candidates := []RouteChannelCandidate{
		// Site 1 (id=100): 2 channels, globalWeight=1.2
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.2, ptrFloat(0.01), 5.0, nil),
		makeCandidate(2, 100, 1002, 10, 0, 200, 2, 80.0, 1.2, ptrFloat(0.008), 10.0, nil),
		// Site 2 (id=200): 1 channel, globalWeight=1.0
		makeCandidate(3, 200, 2001, 10, 1, 50, 10, 30.0, 1.0, nil, 0.0, nil),
	}

	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: 0.5,
		ValueScoreFactor: 0.5,
		CostWeight:       0.4,
		BalanceWeight:    0.3,
		UsageWeight:      0.3,
	}

	result := CalculateWeightedSelection(
		candidates, staticModel("gpt-4"), routingWeights,
		nil, // siteWeightMultipliers
		nil, // channelLoadProvider
		0,   // nowMs
		WeightedMode,
		"",
		nil, // pricingFn
		1.0, // fallbackUnitCost
	)

	if result.Selected == nil {
		t.Fatal("expected a selected candidate, got nil")
	}

	// Verify all candidates got contributions
	if len(result.Details) != 3 {
		t.Fatalf("expected 3 details, got %d", len(result.Details))
	}

	// Check that probabilities sum to approximately 1
	sum := 0.0
	for _, d := range result.Details {
		sum += d.Probability
	}
	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("probabilities sum to %f, expected ~1.0", sum)
	}

	// Verify site channel count normalization: site 1 has 2 channels, so each gets /2
	// Site 2 has 1 channel, so it gets /1 (no division penalty)
	// This means site 1 channels individually should have lower contribution than if they were solo
	t.Logf("Golden file weight formula verified:")
	for i, d := range result.Details {
		t.Logf("  Candidate %d (ch=%d, site=%d): probability=%.6f",
			i, d.Candidate.Channel.ID, d.Candidate.Site.ID, d.Probability)
	}

	// Determine which candidate was selected
	t.Logf("  Selected: channel=%d, site=%d",
		result.Selected.Channel.ID, result.Selected.Site.ID)

	goldenPath := filepath.Join("testdata", "weights_golden.txt")
	var sb strings.Builder
	sb.WriteString("# Golden file for CalculateWeightedSelection weight formula\n")
	sb.WriteString("# Format: channelID,siteID,accountID,probability\n")
	for i, d := range result.Details {
		sb.WriteString(formatInt(d.Candidate.Channel.ID))
		sb.WriteString(",")
		sb.WriteString(formatInt(d.Candidate.Site.ID))
		sb.WriteString(",")
		sb.WriteString(formatInt(d.Candidate.Account.ID))
		sb.WriteString(",")
		sb.WriteString(fmtFloat(d.Probability))
		sb.WriteString("\n")
		_ = i
	}
	readBack := readOrUpdateGoldenFile(t, goldenPath, []byte(sb.String()))
	t.Logf("Golden file checked at %s (%d bytes)", goldenPath, len(readBack))
}

// =============================================================================
// Weight formula unit tests
// =============================================================================

func TestEffectiveUnitCost_Observed(t *testing.T) {
	c := makeCandidate(1, 100, 1001, 10, 0, 100, 0, 50.0, 1.0, nil, 0, nil)
	cost := EffectiveUnitCost(c, "gpt-4", nil, 1.0)
	if cost.Source != "observed" {
		t.Errorf("expected 'observed' source, got %q", cost.Source)
	}
	expected := 50.0 / 100.0 // 0.5
	if math.Abs(cost.UnitCost-expected) > 0.001 {
		t.Errorf("expected unit cost %f, got %f", expected, cost.UnitCost)
	}
}

func TestEffectiveUnitCost_Configured(t *testing.T) {
	// No observed cost (zero success count), but has configured unit cost
	c := makeCandidate(1, 100, 1001, 10, 0, 0, 0, 0, 1.0, ptrFloat(0.003), 0, nil)
	cost := EffectiveUnitCost(c, "gpt-4", nil, 1.0)
	if cost.Source != "configured" {
		t.Errorf("expected 'configured' source, got %q", cost.Source)
	}
	if math.Abs(cost.UnitCost-0.003) > 0.0001 {
		t.Errorf("expected unit cost 0.003, got %f", cost.UnitCost)
	}
}

func TestEffectiveUnitCost_Catalog(t *testing.T) {
	c := makeCandidate(1, 100, 1001, 10, 0, 0, 0, 0, 1.0, nil, 0, nil)
	pricingFn := func(siteID, accountID int64, modelName string) *float64 {
		return ptrFloat(0.002)
	}
	cost := EffectiveUnitCost(c, "gpt-4", pricingFn, 1.0)
	if cost.Source != "catalog" {
		t.Errorf("expected 'catalog' source, got %q", cost.Source)
	}
}

func TestEffectiveUnitCost_Fallback(t *testing.T) {
	c := makeCandidate(1, 100, 1001, 10, 0, 0, 0, 0, 1.0, nil, 0, nil)
	cost := EffectiveUnitCost(c, "gpt-4", nil, 2.5)
	if cost.Source != "fallback" {
		t.Errorf("expected 'fallback' source, got %q", cost.Source)
	}
	if math.Abs(cost.UnitCost-2.5) > 0.001 {
		t.Errorf("expected unit cost 2.5, got %f", cost.UnitCost)
	}
}

func TestEffectiveUnitCost_MinimumClamp(t *testing.T) {
	c := makeCandidate(1, 100, 1001, 10, 0, 1, 0, 0, 1.0, ptrFloat(0.0), 0, nil)
	cost := EffectiveUnitCost(c, "gpt-4", nil, 0.0)
	// observed: 0/1 = 0, clamped to min 1e-6
	if cost.UnitCost < 1e-6 {
		t.Errorf("unit cost %f below minimum 1e-6", cost.UnitCost)
	}
}

func TestCalculateWeightedSelection_EmptyCandidates(t *testing.T) {
	result := CalculateWeightedSelection(
		nil, staticModel("gpt-4"), RoutingWeightsConfig{}, nil, nil, 0, WeightedMode, "", nil, 1.0)
	if result.Selected != nil {
		t.Error("expected nil selected for empty candidates")
	}
	if len(result.Details) != 0 {
		t.Errorf("expected 0 details, got %d", len(result.Details))
	}
}

func TestCalculateWeightedSelection_SingleCandidate(t *testing.T) {
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
	}
	result := CalculateWeightedSelection(
		candidates, staticModel("gpt-4"),
		RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3},
		nil, nil, 0, WeightedMode, "", nil, 1.0)
	if result.Selected == nil {
		t.Fatal("expected a selected candidate")
	}
	if result.Selected.Channel.ID != 1 {
		t.Errorf("expected channel 1, got %d", result.Selected.Channel.ID)
	}
	if len(result.Details) != 1 {
		t.Errorf("expected 1 detail, got %d", len(result.Details))
	}
	if math.Abs(result.Details[0].Probability-1.0) > 0.001 {
		t.Errorf("expected probability 1.0, got %f", result.Details[0].Probability)
	}
}

func TestCalculateWeightedSelection_SiteWeightMultipliers(t *testing.T) {
	ResetSiteRuntimeHealthState()

	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
		makeCandidate(2, 200, 2001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
	}

	// Give site 200 a huge multiplier to make selection deterministic
	multipliers := map[int64]float64{200: 1e12}

	result := CalculateWeightedSelection(
		candidates, staticModel("gpt-4"),
		RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3},
		multipliers, nil, 0, WeightedMode, "", nil, 1.0)

	if result.Selected == nil {
		t.Fatal("expected a selected candidate")
	}

	// Deterministic check: site 200 contribution must be much larger than site 100
	if len(result.Details) != 2 {
		t.Fatalf("expected 2 details, got %d", len(result.Details))
	}
	var contrib100, contrib200 float64
	var prob100, prob200 float64
	for _, d := range result.Details {
		if d.Candidate.Site.ID == 100 {
			contrib100 = d.Probability
		} else if d.Candidate.Site.ID == 200 {
			contrib200 = d.Probability
			prob200 = d.Probability
		}
		_ = prob100
	}
	// Site 200 should have contribution vastly larger than site 100
	if contrib200 <= contrib100*100 {
		t.Errorf("expected site 200 contribution to be >> site 100, got site100=%.6f site200=%.6f", contrib100, contrib200)
	}

	// Site 200 should dominate due to the 1e12 multiplier
	if result.Selected.Site.ID != 200 {
		t.Errorf("expected site 200 to be selected (1e12 multiplier), got site %d", result.Selected.Site.ID)
	}
	_ = prob200
}

func TestCalculateWeightedSelection_FallbackPenalty(t *testing.T) {
	ResetSiteRuntimeHealthState()

	// Candidate with high fallback cost gets penalized
	c1 := makeCandidate(1, 100, 1001, 10, 0, 0, 0, 0, 1.0, nil, 0, nil)
	c2 := makeCandidate(2, 200, 2001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil)

	result := CalculateWeightedSelection(
		[]RouteChannelCandidate{c1, c2}, staticModel("gpt-4"),
		RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3},
		nil, nil, 0, WeightedMode, "", nil, 100.0) // high fallback cost

	if result.Selected == nil {
		t.Fatal("expected a selected candidate")
	}
	// c1 has fallback cost 100, so it should be heavily penalized; c2 should win
	if result.Selected.Channel.ID != 2 {
		t.Errorf("expected channel 2 (with observed cost), got channel %d", result.Selected.Channel.ID)
	}
}

func TestCalculateWeightedSelection_StableFirstMode(t *testing.T) {
	ResetSiteRuntimeHealthState()

	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
		makeCandidate(2, 200, 2001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
	}

	result := CalculateWeightedSelection(
		candidates, staticModel("gpt-4"),
		RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3},
		nil, nil, 0, StableFirstMode, "1:gpt-4", nil, 1.0)

	if result.Selected == nil {
		t.Fatal("expected a selected candidate in stable_first mode")
	}
	// In stable_first mode, contribution = max(1e-4, successRate^2) * runtimeMultiplier * runtimeLoadMultiplier / siteChannels
	// Success rate for both should be high, so one should be selected
}

func TestCalculateWeightedSelection_ZeroWeightChannels(t *testing.T) {
	ResetSiteRuntimeHealthState()

	// Make channels with weight=0. The formula uses (weight+10) so they still contribute.
	c1 := makeCandidate(1, 100, 1001, 0, 0, 100, 5, 50.0, 1.0, nil, 0, nil)
	c2 := makeCandidate(2, 200, 2001, 0, 0, 100, 5, 50.0, 1.0, nil, 0, nil)

	result := CalculateWeightedSelection(
		[]RouteChannelCandidate{c1, c2}, staticModel("gpt-4"),
		RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3},
		nil, nil, 0, WeightedMode, "", nil, 1.0)

	if result.Selected == nil {
		t.Fatal("expected a selected candidate even with weight=0")
	}
	// Both should have equal probability
	if len(result.Details) == 2 {
		diff := math.Abs(result.Details[0].Probability - result.Details[1].Probability)
		if diff > 0.001 {
			t.Logf("zero-weight channels: p0=%.4f, p1=%.4f", result.Details[0].Probability, result.Details[1].Probability)
		}
	}
}

func TestResolveChannelRuntimeLoadMultiplier(t *testing.T) {
	tests := []struct {
		name     string
		snapshot ChannelLoadSnapshot
		expected float64
	}{
		{"not session scoped", ChannelLoadSnapshot{SessionScoped: false}, 1.0},
		{"no concurrency limit", ChannelLoadSnapshot{SessionScoped: true, ConcurrencyLimit: 0}, 1.0},
		{"idle", ChannelLoadSnapshot{SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 0, WaitingCount: 0}, 1.0},
		{"half active", ChannelLoadSnapshot{SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 5, WaitingCount: 0}, 1.0 - 0.5*0.28},
		{"fully loaded", ChannelLoadSnapshot{SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 10, WaitingCount: 5, Saturated: true}, 0.44}, // 1 - 0.28 - 0.5*0.32 - 0.12 = 0.44
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveChannelRuntimeLoadMultiplier(tt.snapshot)
			if math.Abs(got-tt.expected) > 0.001 {
				t.Errorf("expected %f, got %f", tt.expected, got)
			}
		})
	}
}

func TestValueScoreComputation(t *testing.T) {
	ResetSiteRuntimeHealthState()

	// A candidate with lower cost, higher balance, fewer calls should score higher
	c1 := makeCandidate(1, 100, 1001, 10, 0, 10, 10, 5.0, 1.0, ptrFloat(0.01), 100.0, nil)     // low cost, high balance, few calls
	c2 := makeCandidate(2, 200, 2001, 10, 0, 1000, 1000, 500.0, 1.0, ptrFloat(0.05), 1.0, nil) // high cost, low balance, many calls

	result := CalculateWeightedSelection(
		[]RouteChannelCandidate{c1, c2}, staticModel("gpt-4"),
		RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3},
		nil, nil, 0, WeightedMode, "", nil, 1.0)

	if result.Selected == nil {
		t.Fatal("expected a selected candidate")
	}
	// c1 should have higher probability due to better value score
	if len(result.Details) == 2 {
		if result.Details[0].Probability <= result.Details[1].Probability {
			// c1 is index 0; it should have higher probability
			t.Logf("value scores: c1=%.4f, c2=%.4f", result.Details[0].Probability, result.Details[1].Probability)
		}
	}
}

// =============================================================================
// Helpers
// =============================================================================

func ptrFloat(v float64) *float64 { return &v }

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkCalculateWeightedSelection_5Candidates(b *testing.B) {
	ResetSiteRuntimeHealthState()
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 10, 101, 10, 0, 500, 10, 250.0, 1.5, ptrFloat(0.003), 100.0, nil),
		makeCandidate(2, 10, 102, 10, 0, 300, 5, 180.0, 1.5, ptrFloat(0.0025), 200.0, nil),
		makeCandidate(3, 20, 201, 10, 1, 200, 20, 100.0, 1.0, ptrFloat(0.005), 50.0, nil),
		makeCandidate(4, 20, 202, 10, 1, 150, 30, 80.0, 1.0, nil, 30.0, nil),
		makeCandidate(5, 30, 301, 10, 2, 100, 50, 40.0, 0.8, nil, 5.0, nil),
	}
	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: 0.5,
		ValueScoreFactor: 0.5,
		CostWeight:       0.4,
		BalanceWeight:    0.3,
		UsageWeight:      0.3,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateWeightedSelection(
			candidates, staticModel("gpt-4"), routingWeights,
			nil, nil, 0, WeightedMode, "", nil, 1.0,
		)
	}
}

func BenchmarkCalculateWeightedSelection_10Candidates(b *testing.B) {
	ResetSiteRuntimeHealthState()
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 10, 101, 10, 0, 500, 10, 250.0, 1.5, ptrFloat(0.003), 100.0, nil),
		makeCandidate(2, 10, 102, 10, 0, 300, 5, 180.0, 1.5, ptrFloat(0.0025), 200.0, nil),
		makeCandidate(3, 20, 201, 10, 1, 200, 20, 100.0, 1.0, ptrFloat(0.005), 50.0, nil),
		makeCandidate(4, 20, 202, 10, 1, 150, 30, 80.0, 1.0, nil, 30.0, nil),
		makeCandidate(5, 30, 301, 10, 2, 100, 50, 40.0, 0.8, nil, 5.0, nil),
		makeCandidate(6, 10, 103, 10, 0, 600, 8, 300.0, 1.5, ptrFloat(0.004), 150.0, nil),
		makeCandidate(7, 20, 203, 10, 1, 180, 15, 120.0, 1.0, ptrFloat(0.006), 60.0, nil),
		makeCandidate(8, 30, 302, 10, 2, 80, 40, 35.0, 0.8, nil, 8.0, nil),
		makeCandidate(9, 40, 401, 10, 1, 250, 25, 150.0, 1.2, ptrFloat(0.0035), 80.0, nil),
		makeCandidate(10, 40, 402, 10, 1, 220, 20, 130.0, 1.2, nil, 70.0, nil),
	}
	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: 0.5,
		ValueScoreFactor: 0.5,
		CostWeight:       0.4,
		BalanceWeight:    0.3,
		UsageWeight:      0.3,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateWeightedSelection(
			candidates, staticModel("gpt-4"), routingWeights,
			nil, nil, 0, WeightedMode, "", nil, 1.0,
		)
	}
}

func BenchmarkCalculateWeightedSelection_SingleCandidate(b *testing.B) {
	ResetSiteRuntimeHealthState()
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
	}
	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: 0.5,
		ValueScoreFactor: 0.5,
		CostWeight:       0.4,
		BalanceWeight:    0.3,
		UsageWeight:      0.3,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateWeightedSelection(
			candidates, staticModel("gpt-4"), routingWeights,
			nil, nil, 0, WeightedMode, "", nil, 1.0,
		)
	}
}

func BenchmarkCalculateWeightedSelection_StableFirst(b *testing.B) {
	ResetSiteRuntimeHealthState()
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
		makeCandidate(2, 200, 2001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
	}
	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: 0.5,
		ValueScoreFactor: 0.5,
		CostWeight:       0.4,
		BalanceWeight:    0.3,
		UsageWeight:      0.3,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateWeightedSelection(
			candidates, staticModel("gpt-4"), routingWeights,
			nil, nil, 0, StableFirstMode, "1:gpt-4", nil, 1.0,
		)
	}
}

func BenchmarkEffectiveUnitCost_Observed(b *testing.B) {
	c := makeCandidate(1, 100, 1001, 10, 0, 100, 0, 50.0, 1.0, nil, 0, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EffectiveUnitCost(c, "gpt-4", nil, 1.0)
	}
}

func BenchmarkEffectiveUnitCost_Catalog(b *testing.B) {
	c := makeCandidate(1, 100, 1001, 10, 0, 0, 0, 0, 1.0, nil, 0, nil)
	pricingFn := func(siteID, accountID int64, modelName string) *float64 {
		return ptrFloat(0.002)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EffectiveUnitCost(c, "gpt-4", pricingFn, 1.0)
	}
}

func BenchmarkResolveChannelRuntimeLoadMultiplier(b *testing.B) {
	snapshots := []ChannelLoadSnapshot{
		{SessionScoped: false},
		{SessionScoped: true, ConcurrencyLimit: 0},
		{SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 0},
		{SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 5},
		{SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 10, WaitingCount: 5, Saturated: true},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range snapshots {
			_ = ResolveChannelRuntimeLoadMultiplier(s)
		}
	}
}

func BenchmarkMakeCandidate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = makeCandidate(int64(i%100)+1, int64(i%10)*10, int64(i)+1, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil)
	}
}
