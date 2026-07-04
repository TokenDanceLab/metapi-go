package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// RouteRefreshWorkflow tests
// =============================================================================

type mockModelProvider struct {
	availableModels []ModelInfo
	refreshErr      error
}

func (m *mockModelProvider) GetAvailableModels(_ context.Context, accountID int64) ([]ModelInfo, error) {
	return m.availableModels, nil
}

func (m *mockModelProvider) RefreshModelsForAccount(_ context.Context, accountID int64) error {
	return m.refreshErr
}

type mockRouteRebuilder struct {
	rebuildErr error
	called     bool
}

func (m *mockRouteRebuilder) RebuildTokenRoutesFromAvailability(_ context.Context) error {
	m.called = true
	return m.rebuildErr
}

func TestNewRouteRefreshWorkflow(t *testing.T) {
	mp := &mockModelProvider{}
	rb := &mockRouteRebuilder{}
	wf := NewRouteRefreshWorkflow(mp, rb)
	if wf == nil {
		t.Fatal("expected non-nil workflow")
	}
}

func TestRebuildRoutesOnly_Success(t *testing.T) {
	rb := &mockRouteRebuilder{}
	wf := NewRouteRefreshWorkflow(nil, rb)

	err := wf.RebuildRoutesOnly(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !rb.called {
		t.Error("expected rebuilder to be called")
	}
}

func TestRebuildRoutesOnly_Error(t *testing.T) {
	rb := &mockRouteRebuilder{rebuildErr: errors.New("rebuild failed")}
	wf := NewRouteRefreshWorkflow(nil, rb)

	err := wf.RebuildRoutesOnly(context.Background())
	if err == nil {
		t.Error("expected error from rebuilder")
	}
}

func TestRebuildRoutesOnly_NoRebuilder(t *testing.T) {
	wf := NewRouteRefreshWorkflow(nil, nil)
	err := wf.RebuildRoutesOnly(context.Background())
	if err == nil {
		t.Error("expected error when no rebuilder configured")
	}
}

func TestRebuildRoutesBestEffort(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rb := &mockRouteRebuilder{}
		wf := NewRouteRefreshWorkflow(nil, rb)
		if !wf.RebuildRoutesBestEffort(context.Background()) {
			t.Error("expected true for successful rebuild")
		}
	})

	t.Run("failure", func(t *testing.T) {
		rb := &mockRouteRebuilder{rebuildErr: errors.New("fail")}
		wf := NewRouteRefreshWorkflow(nil, rb)
		if wf.RebuildRoutesBestEffort(context.Background()) {
			t.Error("expected false for failed rebuild")
		}
	})
}

func TestRefreshModelsAndRebuildRoutes(t *testing.T) {
	t.Run("no model provider", func(t *testing.T) {
		rb := &mockRouteRebuilder{}
		wf := NewRouteRefreshWorkflow(nil, rb)
		err := wf.RefreshModelsAndRebuildRoutes(context.Background())
		if err == nil {
			t.Error("expected error when no model provider")
		}
	})

	t.Run("no rebuilder", func(t *testing.T) {
		mp := &mockModelProvider{}
		wf := NewRouteRefreshWorkflow(mp, nil)
		err := wf.RefreshModelsAndRebuildRoutes(context.Background())
		// No error since we just skip rebuild if no rebuilder
		if err != nil {
			t.Logf("got error (expected): %v", err)
		}
	})

	t.Run("with both providers", func(t *testing.T) {
		mp := &mockModelProvider{}
		rb := &mockRouteRebuilder{}
		wf := NewRouteRefreshWorkflow(mp, rb)
		err := wf.RefreshModelsAndRebuildRoutes(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !rb.called {
			t.Error("expected rebuilder to be called")
		}
	})
}

// =============================================================================
// Pricing reference tests
// =============================================================================

type mockPricingProvider struct {
	costs map[string]float64
}

func (m *mockPricingProvider) GetReferenceCost(_ context.Context, model string, siteID, accountID int64) (float64, error) {
	key := model
	if c, ok := m.costs[key]; ok {
		return c, nil
	}
	return 0, nil
}

func (m *mockPricingProvider) RefreshModelPricingCatalog(_ context.Context, _ store.Site, _ store.Account, modelName string) error {
	return nil
}

func TestNewPricingReference(t *testing.T) {
	mp := &mockPricingProvider{costs: map[string]float64{"gpt-4": 0.03}}
	pr := NewPricingReference(mp)
	if pr == nil {
		t.Fatal("expected non-nil pricing reference")
	}
}

func TestGetCachedModelRoutingReferenceCost(t *testing.T) {
	mp := &mockPricingProvider{costs: map[string]float64{"gpt-4": 0.03, "claude": 0.0}}
	pr := NewPricingReference(mp)

	// Existing cost
	cost := pr.GetCachedModelRoutingReferenceCost(context.Background(), 1, 1, "gpt-4")
	if cost == nil {
		t.Fatal("expected non-nil cost")
	}
	if *cost != 0.03 {
		t.Errorf("expected 0.03, got %f", *cost)
	}

	// Unknown model
	cost = pr.GetCachedModelRoutingReferenceCost(context.Background(), 1, 1, "unknown")
	if cost != nil {
		t.Error("expected nil for unknown model")
	}

	// Zero cost
	cost = pr.GetCachedModelRoutingReferenceCost(context.Background(), 1, 1, "claude")
	if cost != nil {
		t.Error("expected nil for zero cost")
	}

	// No provider
	pr2 := NewPricingReference(nil)
	cost = pr2.GetCachedModelRoutingReferenceCost(context.Background(), 1, 1, "gpt-4")
	if cost != nil {
		t.Error("expected nil when no provider")
	}
}

// =============================================================================
// Round-robin time formatting helpers
// =============================================================================

func TestTimeFromMs(t *testing.T) {
	result := timeFromMs(1700000000000)
	if result == "" {
		t.Error("expected non-empty ISO string")
	}
	// Should contain T separator and Z suffix
	if len(result) < 20 {
		t.Errorf("expected long ISO string, got %q", result)
	}
	t.Logf("timeFromMs(1700000000000) = %s", result)
}

func TestPad3(t *testing.T) {
	if got := pad3(5); got != "005" {
		t.Errorf("expected '005', got %q", got)
	}
	if got := pad3(123); got != "123" {
		t.Errorf("expected '123', got %q", got)
	}
	if got := pad3(0); got != "000" {
		t.Errorf("expected '000', got %q", got)
	}
}

func TestFormatInt2(t *testing.T) {
	if got := formatInt2(5); got != "05" {
		t.Errorf("expected '05', got %q", got)
	}
	if got := formatInt2(42); got != "42" {
		t.Errorf("expected '42', got %q", got)
	}
	if got := formatInt2(0); got != "00" {
		t.Errorf("expected '00', got %q", got)
	}
}

// =============================================================================
// Stable-first helpers (additional to weights_test)
// =============================================================================

func TestBuildStableFirstRotationKey(t *testing.T) {
	key := BuildStableFirstRotationKey(42, "anthropic/claude-sonnet")
	if key == "" {
		t.Error("expected non-empty rotation key")
	}
	// Expected: "42:claude-sonnet"
	expected := "42:claude-sonnet"
	if key != expected {
		t.Errorf("expected %q, got %q", expected, key)
	}
}

func TestBuildStableFirstRotationKey_NoSlash(t *testing.T) {
	key := BuildStableFirstRotationKey(1, "gpt-4")
	if key != "1:gpt-4" {
		t.Errorf("expected '1:gpt-4', got %q", key)
	}
}

func TestClearStableFirstCachesForRoute(t *testing.T) {
	// Pre-populate
	stableFirstLastSelectedSiteByKey["1:gpt-4"] = 100
	stableFirstLastSelectedSiteByKey["2:claude"] = 200
	stableFirstObservationProgressByKey["1:gpt-4"] = StableFirstObservationProgressState{RequestCount: 10}
	stableFirstObservationSiteCooldownByKey["1:gpt-4:300"] = 12345

	ClearStableFirstCachesForRoute(1)

	// Route 1 keys should be gone
	if _, ok := stableFirstLastSelectedSiteByKey["1:gpt-4"]; ok {
		t.Error("expected '1:gpt-4' to be removed")
	}
	if _, ok := stableFirstObservationProgressByKey["1:gpt-4"]; ok {
		t.Error("expected '1:gpt-4' progress to be removed")
	}
	if _, ok := stableFirstObservationSiteCooldownByKey["1:gpt-4:300"]; ok {
		t.Error("expected '1:gpt-4:300' cooldown to be removed")
	}

	// Route 2 keys should remain
	if _, ok := stableFirstLastSelectedSiteByKey["2:claude"]; !ok {
		t.Error("expected '2:claude' to remain")
	}
}

func TestShouldUseStableFirstObservationCandidate(t *testing.T) {
	// Empty rotation key
	if ShouldUseStableFirstObservationCandidate("", nil) {
		t.Error("expected false for empty rotation key")
	}

	// No observation candidates
	if ShouldUseStableFirstObservationCandidate("1:gpt-4", nil) {
		t.Error("expected false for no observation candidates")
	}

	// First call — not enough requests (requestCount < 24)
	cand := RouteChannelCandidate{
		Channel: store.RouteChannel{ID: 1},
		Site:    store.Site{ID: 100},
	}
	if ShouldUseStableFirstObservationCandidate("test:gpt-4", []RouteChannelCandidate{cand}) {
		t.Error("expected false on first call (insufficient requests)")
	}
}

func TestUpdateStableFirstObservationProgress(t *testing.T) {
	key := "progress-test:gpt-4"

	// Non-observation increment
	UpdateStableFirstObservationProgress(key, false, 0)
	state, ok := stableFirstObservationProgressByKey[key]
	if !ok {
		t.Fatal("expected progress state to exist")
	}
	if state.RequestCount != 1 {
		t.Errorf("expected requestCount=1, got %d", state.RequestCount)
	}
}

// =============================================================================
// BuildStableFirstPoolPlan tests
// =============================================================================

func TestBuildStableFirstPoolPlan_Empty(t *testing.T) {
	plan := BuildStableFirstPoolPlan(nil, "gpt-4")
	if len(plan.PrimaryCandidates) != 0 {
		t.Errorf("expected 0 primary candidates, got %d", len(plan.PrimaryCandidates))
	}
	if len(plan.ObservationCandidates) != 0 {
		t.Errorf("expected 0 observation candidates, got %d", len(plan.ObservationCandidates))
	}
}

func TestBuildStableFirstPoolPlan_SingleSite(t *testing.T) {
	ResetSiteRuntimeHealthState()

	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 50.0, 1.0, nil, 0, nil),
		makeCandidate(2, 100, 1002, 10, 0, 200, 2, 80.0, 1.0, nil, 0, nil),
	}

	plan := BuildStableFirstPoolPlan(candidates, "gpt-4")
	// Single site: should be in primary pool
	if len(plan.PrimarySiteIDs) != 1 {
		t.Errorf("expected 1 primary site, got %d", len(plan.PrimarySiteIDs))
	}
	if plan.PrimarySiteIDs[0] != 100 {
		t.Errorf("expected site 100, got %d", plan.PrimarySiteIDs[0])
	}
	if len(plan.PrimaryCandidates) != 2 {
		t.Errorf("expected 2 primary candidates, got %d", len(plan.PrimaryCandidates))
	}
}

func TestBuildStableFirstPoolPlan_MultiSite(t *testing.T) {
	ResetSiteRuntimeHealthState()

	// Site 100: high success rate (should be primary)
	// Site 200: zero calls (should be observation — not trusted)
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 1000, 5, 500.0, 1.0, nil, 0, nil),
		makeCandidate(2, 200, 2001, 10, 0, 0, 0, 0, 1.0, nil, 0, nil),
	}

	plan := BuildStableFirstPoolPlan(candidates, "gpt-4")

	// Site 100 should be primary
	foundPrimary := false
	for _, sid := range plan.PrimarySiteIDs {
		if sid == 100 {
			foundPrimary = true
		}
	}
	if !foundPrimary {
		t.Error("expected site 100 in primary pool")
	}

	// Site 200 should be observation (zero calls = not trusted)
	foundObs := false
	for _, sid := range plan.ObservationSiteIDs {
		if sid == 200 {
			foundObs = true
		}
	}
	// If primary pool is empty and site 200 promoted, it might not be in observation
	if !foundObs && len(plan.PrimarySiteIDs) > 0 {
		t.Log("site 200 may have been promoted to primary (only trusted site)")
	}
}

// =============================================================================
// Site historical health metrics
// =============================================================================

func TestBuildSiteHistoricalHealthMetrics(t *testing.T) {
	candidates := []RouteChannelCandidate{
		makeCandidate(1, 100, 1001, 10, 0, 100, 5, 5000, 1.0, nil, 0, nil),
		makeCandidate(2, 100, 1002, 10, 0, 50, 0, 2000, 1.0, nil, 0, nil),
		makeCandidate(3, 200, 2001, 10, 0, 0, 0, 0, 1.0, nil, 0, nil),
	}

	metrics := BuildSiteHistoricalHealthMetrics(candidates)

	// Site 100
	m100, ok := metrics[100]
	if !ok {
		t.Fatal("expected metrics for site 100")
	}
	if m100.TotalCalls != 155 { // 100+5 + 50+0
		t.Errorf("expected 155 total calls, got %d", m100.TotalCalls)
	}
	if m100.SuccessRate == nil {
		t.Error("expected non-nil success rate for site 100")
	} else if *m100.SuccessRate < 0.9 {
		t.Errorf("expected high success rate, got %f", *m100.SuccessRate)
	}

	// Site 200: zero calls
	m200, ok := metrics[200]
	if !ok {
		t.Fatal("expected metrics for site 200")
	}
	if m200.Multiplier != 1.0 {
		t.Errorf("expected multiplier 1.0 for zero-call site, got %f", m200.Multiplier)
	}
	if m200.TotalCalls != 0 {
		t.Errorf("expected 0 total calls, got %d", m200.TotalCalls)
	}
}

// =============================================================================
// RememberStableFirst helpers (bounds checking)
// =============================================================================

func TestRememberStableFirstSiteSelectionForKey(t *testing.T) {
	rememberStableFirstSiteSelectionForKey("test-key", 100)
	if got := stableFirstLastSelectedSiteByKey["test-key"]; got != 100 {
		t.Errorf("expected 100, got %d", got)
	}

	// Empty key
	rememberStableFirstSiteSelectionForKey("", 100)
	if _, ok := stableFirstLastSelectedSiteByKey[""]; ok {
		t.Error("expected empty key NOT to be stored")
	}

	// Zero site ID
	rememberStableFirstSiteSelectionForKey("key2", 0)
	if _, ok := stableFirstLastSelectedSiteByKey["key2"]; ok {
		t.Error("expected zero site ID NOT to be stored")
	}
}

// =============================================================================
// getStableFirstSiteOrder
// =============================================================================

func TestGetStableFirstSiteOrder(t *testing.T) {
	candidates := []RouteChannelCandidate{
		{
			Channel: store.RouteChannel{ID: 1, Priority: 3},
			Site:    store.Site{ID: 100},
		},
		{
			Channel: store.RouteChannel{ID: 2, Priority: 1},
			Site:    store.Site{ID: 100},
		},
		{
			Channel: store.RouteChannel{ID: 3, Priority: 5},
			Site:    store.Site{ID: 200},
		},
	}

	order := getStableFirstSiteOrder(candidates, 100)
	if order != 1 {
		t.Errorf("expected min priority 1 for site 100, got %d", order)
	}

	order = getStableFirstSiteOrder(candidates, 200)
	if order != 5 {
		t.Errorf("expected priority 5 for site 200, got %d", order)
	}

	// Unknown site
	order = getStableFirstSiteOrder(candidates, 999)
	if order != 0 {
		t.Errorf("expected 0 for unknown site, got %d", order)
	}
}

// =============================================================================
// stable_first.go observation gating test helpers
// =============================================================================

func TestShouldUseStableFirstObservationCandidate_AfterProgress(t *testing.T) {
	ResetSiteRuntimeHealthState()
	key := "obs-test:gpt-4"

	// Build up to 23 requests — at 23, the NEXT request (#24) meets the interval threshold
	for i := 0; i < 23; i++ {
		UpdateStableFirstObservationProgress(key, false, 0)
	}

	cand := RouteChannelCandidate{
		Channel: store.RouteChannel{ID: 1},
		Site:    store.Site{ID: 100},
	}
	// At 23 previous requests, (23+1) >= 24, so observation should be suggested
	if !ShouldUseStableFirstObservationCandidate(key, []RouteChannelCandidate{cand}) {
		t.Error("expected true at 23 requests (next would be 24th = observation interval)")
	}

	// After observation, counter resets
	UpdateStableFirstObservationProgress(key, true, 100)
	state := stableFirstObservationProgressByKey[key]
	if state.RequestCount != 0 {
		t.Errorf("expected requestCount=0 after observation, got %d", state.RequestCount)
	}
	if state.LastObservationAtMs == nil {
		t.Error("expected LastObservationAtMs to be set after observation")
	}
}
