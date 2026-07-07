package routing

import (
	"math"
	"sync"
	"testing"
)

// =============================================================================
// Failure penalty scoring — 8 categories
// =============================================================================

func TestResolveSiteRuntimeFailurePenalty_UsageLimit(t *testing.T) {
	testCases := []struct {
		name     string
		status   int
		errText  string
		expected float64
	}{
		{"usage_limit_reached", 429, "usage_limit_reached", 0.4},
		{"quota exceeded", 429, "quota exceeded", 0.4},
		{"rate limit text", 429, "rate limit reached", 0.4},
		{"limit keyword", 429, "API limit exceeded", 0.4},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := SiteRuntimeFailureContext{
				Status:    &tc.status,
				ErrorText: &tc.errText,
			}
			got := ResolveSiteRuntimeFailurePenalty(ctx)
			if math.Abs(got-tc.expected) > 0.001 {
				t.Errorf("expected %.2f, got %.2f", tc.expected, got)
			}
		})
	}
}

func TestResolveSiteRuntimeFailurePenalty_ModelFailure(t *testing.T) {
	testCases := []struct {
		name    string
		errText string
	}{
		{"unsupported model", "unsupported model"},
		{"model not supported", "model not supported"},
		{"does not support model", "does not support the model"},
		{"no such model", "no such model"},
		{"unknown model", "unknown model"},
		{"invalid model", "invalid model"},
		{"model does not exist", "model does not exist"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status := 400
			ctx := SiteRuntimeFailureContext{
				Status:    &status,
				ErrorText: &tc.errText,
			}
			got := ResolveSiteRuntimeFailurePenalty(ctx)
			if math.Abs(got-0.9) > 0.001 {
				t.Errorf("expected 0.9, got %.2f", got)
			}
		})
	}
}

func TestResolveSiteRuntimeFailurePenalty_ProtocolFailure(t *testing.T) {
	testCases := []string{
		"unsupported legacy protocol",
		"unsupported endpoint",
		"unknown endpoint",
		"unrecognized request url",
		"no route matched",
	}

	for _, text := range testCases {
		t.Run(text, func(t *testing.T) {
			status := 400
			ctx := SiteRuntimeFailureContext{Status: &status, ErrorText: &text}
			got := ResolveSiteRuntimeFailurePenalty(ctx)
			if math.Abs(got-0.6) > 0.001 {
				t.Errorf("expected 0.6, got %.2f for %q", got, text)
			}
		})
	}
}

func TestResolveSiteRuntimeFailurePenalty_ValidationFailure(t *testing.T) {
	testCases := []string{
		"invalid request body",
		"validation error",
		"missing required field",
		"malformed request",
		"cannot parse input",
	}

	for _, text := range testCases {
		t.Run(text, func(t *testing.T) {
			status := 400
			ctx := SiteRuntimeFailureContext{Status: &status, ErrorText: &text}
			got := ResolveSiteRuntimeFailurePenalty(ctx)
			if math.Abs(got-0.25) > 0.001 {
				t.Errorf("expected 0.25, got %.2f for %q", got, text)
			}
		})
	}
}

func TestResolveSiteRuntimeFailurePenalty_Transient(t *testing.T) {
	testCases := []struct {
		status  int
		errText string
	}{
		{500, ""},
		{502, "bad gateway"},
		{503, "service unavailable"},
		{504, "gateway timeout"},
		{500, "connection reset"},
		{500, "econnreset"},
		{500, "timeout"},
	}

	for _, tc := range testCases {
		ctx := SiteRuntimeFailureContext{Status: &tc.status, ErrorText: &tc.errText}
		got := ResolveSiteRuntimeFailurePenalty(ctx)
		if math.Abs(got-2.5) > 0.001 {
			t.Errorf("expected 2.5 for transient, got %.2f (status=%d, text=%q)",
				got, tc.status, tc.errText)
		}
	}
}

func TestResolveSiteRuntimeFailurePenalty_RateLimit429(t *testing.T) {
	status := 429
	ctx := SiteRuntimeFailureContext{Status: &status} // no usage-limit text
	got := ResolveSiteRuntimeFailurePenalty(ctx)
	if math.Abs(got-2.2) > 0.001 {
		t.Errorf("expected 2.2 for plain 429, got %.2f", got)
	}
}

func TestResolveSiteRuntimeFailurePenalty_Auth(t *testing.T) {
	for _, s := range []int{401, 403} {
		ctx := SiteRuntimeFailureContext{Status: &s}
		got := ResolveSiteRuntimeFailurePenalty(ctx)
		if math.Abs(got-1.8) > 0.001 {
			t.Errorf("expected 1.8 for status %d, got %.2f", s, got)
		}
	}
}

func TestResolveSiteRuntimeFailurePenalty_Other4xx(t *testing.T) {
	status := 404
	ctx := SiteRuntimeFailureContext{Status: &status}
	got := ResolveSiteRuntimeFailurePenalty(ctx)
	if math.Abs(got-0.9) > 0.001 {
		t.Errorf("expected 0.9 for 404, got %.2f", got)
	}
}

func TestResolveSiteRuntimeFailurePenalty_Unknown(t *testing.T) {
	status := 100
	ctx := SiteRuntimeFailureContext{Status: &status}
	got := ResolveSiteRuntimeFailurePenalty(ctx)
	if math.Abs(got-1.2) > 0.001 {
		t.Errorf("expected 1.2 for unknown, got %.2f", got)
	}

	// No status at all
	ctx = SiteRuntimeFailureContext{}
	got = ResolveSiteRuntimeFailurePenalty(ctx)
	if math.Abs(got-1.2) > 0.001 {
		t.Errorf("expected 1.2 for empty context, got %.2f", got)
	}
}

// =============================================================================
// Transient classification
// =============================================================================

func TestIsTransientSiteRuntimeFailure(t *testing.T) {
	// Transient: 5xx, 429, transient patterns, but NOT usage-limit/model-failure/protocol/validation
	status500 := 500
	ctx := SiteRuntimeFailureContext{Status: &status500}
	if !IsTransientSiteRuntimeFailure(ctx) {
		t.Error("expected 500 to be transient")
	}

	// 429 without usage-limit text is transient
	status429 := 429
	ctx = SiteRuntimeFailureContext{Status: &status429}
	if !IsTransientSiteRuntimeFailure(ctx) {
		t.Error("expected 429 to be transient")
	}

	// 429 with usage-limit text is NOT transient
	ul := "usage_limit_reached"
	ctx = SiteRuntimeFailureContext{Status: &status429, ErrorText: &ul}
	if IsTransientSiteRuntimeFailure(ctx) {
		t.Error("expected usage-limit 429 NOT to be transient")
	}

	// Model failure is NOT transient
	mf := "unsupported model"
	status400 := 400
	ctx = SiteRuntimeFailureContext{Status: &status400, ErrorText: &mf}
	if IsTransientSiteRuntimeFailure(ctx) {
		t.Error("expected model failure NOT to be transient")
	}

	// Transient text pattern
	tt := "bad gateway"
	ctx = SiteRuntimeFailureContext{ErrorText: &tt}
	if !IsTransientSiteRuntimeFailure(ctx) {
		t.Error("expected bad gateway to be transient")
	}
}

func TestIsUsageLimitRateLimitFailure(t *testing.T) {
	// False for nil
	ctx := SiteRuntimeFailureContext{}
	if IsUsageLimitRateLimitFailure(ctx) {
		t.Error("expected false for empty context")
	}

	// True for 429 + usage limit text
	status := 429
	text := "usage_limit_reached"
	ctx = SiteRuntimeFailureContext{Status: &status, ErrorText: &text}
	if !IsUsageLimitRateLimitFailure(ctx) {
		t.Error("expected true for 429 usage_limit_reached")
	}

	// False for 429 without usage limit text
	ctx = SiteRuntimeFailureContext{Status: &status}
	if IsUsageLimitRateLimitFailure(ctx) {
		t.Error("expected false for 429 without usage limit text")
	}

	// False for non-429
	status = 500
	ctx = SiteRuntimeFailureContext{Status: &status, ErrorText: &text}
	if IsUsageLimitRateLimitFailure(ctx) {
		t.Error("expected false for non-429 status")
	}
}

// =============================================================================
// Runtime health multiplier
// =============================================================================

func TestGetRuntimeHealthMultiplier(t *testing.T) {
	// Nil state = multiplier 1
	if got := GetRuntimeHealthMultiplier(nil); math.Abs(got-1.0) > 0.001 {
		t.Errorf("expected 1.0 for nil state, got %.4f", got)
	}
}

func TestGetRuntimeHealthMultiplier_BreakerOpen(t *testing.T) {
	ResetSiteRuntimeHealthState()

	// Create a state with open breaker
	n := nowMs()
	breakerUntil := n + 60000 // 60s from now
	state := &SiteRuntimeHealthState{
		PenaltyScore:    0,
		BreakerLevel:    1,
		BreakerUntilMs:  &breakerUntil,
		LastUpdatedAtMs: n,
	}

	got := GetRuntimeHealthMultiplier(state)
	if math.Abs(got-SiteRuntimeMinMultiplier) > 0.001 {
		t.Errorf("expected %.4f (min multiplier) when breaker is open, got %.4f", SiteRuntimeMinMultiplier, got)
	}
}

func TestGetRuntimeHealthMultiplier_WithPenalty(t *testing.T) {
	ResetSiteRuntimeHealthState()

	n := nowMs()
	state := &SiteRuntimeHealthState{
		PenaltyScore:    2.0, // significant penalty
		BreakerLevel:    0,
		BreakerUntilMs:  nil,
		LastUpdatedAtMs: n,
	}

	got := GetRuntimeHealthMultiplier(state)
	// failurePenaltyFactor = 1/(1+2.0) = 0.333, latencyFactor = 1.0
	// combined = 0.333, which is > 0.08
	expected := 1.0 / (1.0 + 2.0)
	if math.Abs(got-expected) > 0.001 {
		t.Errorf("expected %.4f, got %.4f", expected, got)
	}
}

func TestGetRuntimeHealthMultiplier_WithLatency(t *testing.T) {
	ResetSiteRuntimeHealthState()

	n := nowMs()
	highLatency := 5000.0 // double baseline
	state := &SiteRuntimeHealthState{
		PenaltyScore:    0,
		LatencyEMAMs:    &highLatency,
		BreakerLevel:    0,
		BreakerUntilMs:  nil,
		LastUpdatedAtMs: n,
	}

	got := GetRuntimeHealthMultiplier(state)
	// latencyPenaltyRatio = clamp((5000-2500)/30000, 0, 1) = 0.0833
	// latencyFactor = 1 - (0.0833 * 0.35) = 0.9708
	// failurePenaltyFactor = 1/(1+0) = 1
	// clamp(0.9708, 0.08, 1) = 0.9708
	if got >= 1.0 {
		t.Errorf("expected < 1.0 due to latency penalty, got %.4f", got)
	}
}

// =============================================================================
// Breaker state machine
// =============================================================================

func TestRuntimeHealthBreaker_TriggerOnTransientStreak(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9999)
	status500 := 500
	// Record 3 transient failures in quick succession
	for i := 0; i < 3; i++ {
		RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status500})
	}

	// After 3 transient failures, breaker should be open
	state := siteRuntimeHealthStates[siteID]
	if state == nil {
		t.Fatal("expected state to exist")
	}
	if state.BreakerLevel < 1 {
		t.Errorf("expected breaker level >= 1, got %d", state.BreakerLevel)
	}
	if state.BreakerUntilMs == nil {
		t.Error("expected breakerUntilMs to be set")
	}
	if state.TransientFailureStreak != 0 {
		t.Errorf("expected streak reset to 0 after breaker trigger, got %d", state.TransientFailureStreak)
	}

	// Multiplier should be at minimum when breaker is open
	if !isRuntimeHealthBreakerOpen(state) {
		t.Error("expected breaker to be open")
	}
	m := GetRuntimeHealthMultiplier(state)
	if math.Abs(m-SiteRuntimeMinMultiplier) > 0.001 {
		t.Errorf("expected min multiplier %.4f, got %.4f", SiteRuntimeMinMultiplier, m)
	}
}

func TestRuntimeHealthBreaker_NonTransientResetsStreak(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9998)
	status500 := 500
	status401 := 401

	// 2 transient failures
	RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status500})
	RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status500})

	state := siteRuntimeHealthStates[siteID]
	if state.TransientFailureStreak != 2 {
		t.Errorf("expected streak 2, got %d", state.TransientFailureStreak)
	}

	// 1 non-transient failure resets the streak
	RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status401})

	state = siteRuntimeHealthStates[siteID]
	if state.TransientFailureStreak != 0 {
		t.Errorf("expected streak reset to 0 after non-transient, got %d", state.TransientFailureStreak)
	}
}

func TestRuntimeHealthBreaker_LevelProgression(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9997)
	status500 := 500

	// Trigger breaker level 1
	for i := 0; i < 3; i++ {
		RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status500})
	}
	if siteRuntimeHealthStates[siteID].BreakerLevel != 1 {
		t.Errorf("expected level 1, got %d", siteRuntimeHealthStates[siteID].BreakerLevel)
	}

	// Trigger 3 more -> level 2
	for i := 0; i < 3; i++ {
		RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status500})
	}
	if siteRuntimeHealthStates[siteID].BreakerLevel < 2 {
		t.Errorf("expected level >= 2, got %d", siteRuntimeHealthStates[siteID].BreakerLevel)
	}
}

func TestBreakerLevelsMs(t *testing.T) {
	expected := []int64{0, 60_000, 5 * 60_000, 30 * 60 * 1000}
	for i, exp := range expected {
		if i >= len(SiteRuntimeBreakerLevelsMs) {
			break
		}
		if SiteRuntimeBreakerLevelsMs[i] != exp {
			t.Errorf("breaker level %d: expected %d, got %d", i, exp, SiteRuntimeBreakerLevelsMs[i])
		}
	}
}

// =============================================================================
// Success recording
// =============================================================================

func TestRecordSiteRuntimeSuccess_ClearsBreaker(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9996)
	status500 := 500

	// Trigger breaker
	for i := 0; i < 3; i++ {
		RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status500})
	}

	state := siteRuntimeHealthStates[siteID]
	if !isRuntimeHealthBreakerOpen(state) {
		t.Fatal("expected breaker open before success")
	}

	// Record success → clears breaker
	modelName := "gpt-4"
	RecordSiteRuntimeSuccess(siteID, 500.0, &modelName)

	state = siteRuntimeHealthStates[siteID]
	if state.BreakerLevel != 0 {
		t.Errorf("expected breaker level 0 after success, got %d", state.BreakerLevel)
	}
	if state.BreakerUntilMs != nil {
		t.Error("expected breakerUntilMs nil after success")
	}
}

func TestRecordSiteRuntimeSuccess_ReducesPenalty(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9995)
	status401 := 401

	// Record an auth failure (penalty 1.8) then success
	RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{Status: &status401})
	modelName := "gpt-4"
	RecordSiteRuntimeSuccess(siteID, 800.0, &modelName)

	state := siteRuntimeHealthStates[siteID]
	// max(0, 1.8*0.2 - 0.3) = max(0, 0.36-0.3) = max(0, 0.06) = 0.06
	if state.PenaltyScore > 0.1 {
		t.Errorf("expected penalty ~0.06 after success, got %.4f", state.PenaltyScore)
	}
}

func TestRecordSiteRuntimeSuccess_UpdatesLatencyEMA(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9994)
	modelName := "gpt-4"
	RecordSiteRuntimeSuccess(siteID, 1000.0, &modelName)

	state := siteRuntimeHealthStates[siteID]
	if state.LatencyEMAMs == nil {
		t.Fatal("expected latency EMA to be set")
	}
	if math.Abs(*state.LatencyEMAMs-1000.0) > 0.001 {
		t.Errorf("expected first EMA = 1000, got %.2f", *state.LatencyEMAMs)
	}

	// Second call: EMA = 1000*0.7 + 500*0.3 = 850
	RecordSiteRuntimeSuccess(siteID, 500.0, &modelName)
	state = siteRuntimeHealthStates[siteID]
	if math.Abs(*state.LatencyEMAMs-850.0) > 0.001 {
		t.Errorf("expected EMA = 850, got %.2f", *state.LatencyEMAMs)
	}
}

// =============================================================================
// IsSiteRuntimeBreakerOpen / Filter tests
// =============================================================================

func TestIsSiteRuntimeBreakerOpen(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9993)
	if IsSiteRuntimeBreakerOpen(siteID) {
		t.Error("expected false for non-existent site")
	}

	// Create a state with open breaker
	n := nowMs()
	breakerUntil := n + 60000
	siteRuntimeHealthStates[siteID] = &SiteRuntimeHealthState{
		BreakerLevel:    1,
		BreakerUntilMs:  &breakerUntil,
		LastUpdatedAtMs: n,
	}
	if !IsSiteRuntimeBreakerOpen(siteID) {
		t.Error("expected true for open breaker")
	}
}

// =============================================================================
// ResolveStableFirstSuccessRate
// =============================================================================

func TestResolveStableFirstSuccessRate(t *testing.T) {
	details := SiteRuntimeHealthDetails{
		RecentSuccessRate: 0.9,
		RecentConfidence:  0.8,
	}
	histRate := 0.6
	got := ResolveStableFirstSuccessRate(details, &histRate)
	// 0.9 * 0.8 + 0.6 * (1 - 0.8) = 0.72 + 0.12 = 0.84
	if math.Abs(got-0.84) > 0.001 {
		t.Errorf("expected 0.84, got %.4f", got)
	}

	// Nil historical → use fallback 0.5
	got = ResolveStableFirstSuccessRate(details, nil)
	// 0.9 * 0.8 + 0.5 * 0.2 = 0.72 + 0.10 = 0.82
	if math.Abs(got-0.82) > 0.001 {
		t.Errorf("expected 0.82 with fallback, got %.4f", got)
	}
}

// =============================================================================
// Recent outcome tracking
// =============================================================================

func TestBuildRecentOutcomeSnapshot(t *testing.T) {
	snap := buildRecentOutcomeSnapshot(10, 2)
	// successRate = (10+1)/(12+1+1) = 11/14 = 0.7857
	expectedSR := 11.0 / 14.0
	if math.Abs(snap.SuccessRate-expectedSR) > 0.001 {
		t.Errorf("expected success rate %.4f, got %.4f", expectedSR, snap.SuccessRate)
	}
	if math.Abs(snap.SampleCount-12.0) > 0.001 {
		t.Errorf("expected sample count 12, got %.1f", snap.SampleCount)
	}
	// confidence = clamp(12/12, 0, 1) = 1.0
	if math.Abs(snap.Confidence-1.0) > 0.001 {
		t.Errorf("expected confidence 1.0, got %.4f", snap.Confidence)
	}
}

func TestBuildRecentOutcomeSnapshot_Zero(t *testing.T) {
	snap := buildRecentOutcomeSnapshot(0, 0)
	// successRate = (0+1)/(0+1+1) = 1/2 = 0.5 (Bayesian prior)
	if math.Abs(snap.SuccessRate-0.5) > 0.001 {
		t.Errorf("expected 0.5, got %.4f", snap.SuccessRate)
	}
	if snap.SampleCount != 0 {
		t.Errorf("expected sample count 0, got %.1f", snap.SampleCount)
	}
}

// =============================================================================
// Blending
// =============================================================================

func TestBlendRecentOutcomeSnapshots(t *testing.T) {
	global := buildRecentOutcomeSnapshot(10, 5)
	model := buildRecentOutcomeSnapshot(3, 0)

	blended := blendRecentOutcomeSnapshots(global, &model)
	// modelWeight=0.65, globalWeight=0.35
	// success = 10*0.35 + 3*0.65 = 3.5+1.95 = 5.45
	// failure = 5*0.35 + 0*0.65 = 1.75
	// successRate = (5.45+1)/(7.2+1+1) = 6.45/9.2 = 0.7011
	if blended.SampleCount <= 0 {
		t.Error("expected positive sample count")
	}

	// Nil model snapshot → returns global
	blended = blendRecentOutcomeSnapshots(global, nil)
	if blended.SuccessCount != global.SuccessCount {
		t.Errorf("expected global success count %.1f, got %.1f",
			global.SuccessCount, blended.SuccessCount)
	}
}

// =============================================================================
// GetSiteRuntimeHealthDetails
// =============================================================================

func TestGetSiteRuntimeHealthDetails(t *testing.T) {
	ResetSiteRuntimeHealthState()

	siteID := int64(9992)
	details := GetSiteRuntimeHealthDetails(siteID, "gpt-4")

	// No state exists, so multipliers should be 1
	if math.Abs(details.GlobalMultiplier-1.0) > 0.001 {
		t.Errorf("expected global multiplier 1.0, got %.4f", details.GlobalMultiplier)
	}
	if math.Abs(details.ModelMultiplier-1.0) > 0.001 {
		t.Errorf("expected model multiplier 1.0, got %.4f", details.ModelMultiplier)
	}
	if math.Abs(details.CombinedMultiplier-1.0) > 0.001 {
		t.Errorf("expected combined multiplier 1.0, got %.4f", details.CombinedMultiplier)
	}
	if details.GlobalBreakerOpen {
		t.Error("expected global breaker NOT open")
	}
	if details.ModelBreakerOpen {
		t.Error("expected model breaker NOT open")
	}
}

func TestRuntimeHealthPublicReadersConcurrentWithUpdates(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	const workers = 4
	const iterations = 500
	modelName := "gpt-4"
	status500 := 500
	errText := "bad gateway"

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		siteID := int64(9100 + i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%3 == 0 {
					RecordSiteRuntimeFailure(siteID, SiteRuntimeFailureContext{
						Status:    &status500,
						ErrorText: &errText,
						ModelName: &modelName,
					})
					continue
				}
				RecordSiteRuntimeSuccess(siteID, float64(100+j), &modelName)
			}
		}()
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := 0; j < iterations*2; j++ {
				siteID := int64(9100 + ((j + offset) % workers))
				_ = GetSiteRuntimeHealthDetails(siteID, modelName)
				_ = GetSiteRuntimeHealthMultiplier(siteID)
				_ = IsSiteRuntimeBreakerOpen(siteID)
			}
		}(i)
	}

	wg.Wait()
}

// =============================================================================
// Reset / Persistence constants
// =============================================================================

func TestSiteRuntimeHealthConstants(t *testing.T) {
	if SiteRuntimeHealthDecayHalfLifeMs != 10*60*1000 {
		t.Errorf("expected decay half-life 600000, got %d", SiteRuntimeHealthDecayHalfLifeMs)
	}
	if SiteRuntimeMinMultiplier != 0.08 {
		t.Errorf("expected min multiplier 0.08, got %f", SiteRuntimeMinMultiplier)
	}
	if SiteRuntimeLatencyBaselineMs != 2500 {
		t.Errorf("expected latency baseline 2500, got %d", SiteRuntimeLatencyBaselineMs)
	}
	if SiteRuntimeBreakerStreakThreshold != 3 {
		t.Errorf("expected breaker streak threshold 3, got %d", SiteRuntimeBreakerStreakThreshold)
	}
	if SiteTransientStreakWindowMs != 5*60*1000 {
		t.Errorf("expected transient streak window 300000, got %d", SiteTransientStreakWindowMs)
	}
}

func TestResetSiteRuntimeHealthState(t *testing.T) {
	// Seed some state
	siteID := int64(9991)
	modelName := "gpt-4"
	RecordSiteRuntimeSuccess(siteID, 100.0, &modelName)

	if len(siteRuntimeHealthStates) == 0 {
		t.Fatal("expected state to exist before reset")
	}

	ResetSiteRuntimeHealthState()

	if len(siteRuntimeHealthStates) != 0 {
		t.Errorf("expected empty states after reset, got %d", len(siteRuntimeHealthStates))
	}
	if len(siteModelRuntimeHealthStates) != 0 {
		t.Errorf("expected empty model states after reset, got %d", len(siteModelRuntimeHealthStates))
	}
}

// =============================================================================
// Breaker reason formatting
// =============================================================================

func TestBuildRuntimeBreakerReason(t *testing.T) {
	both := buildRuntimeBreakerReason(SiteRuntimeHealthDetails{GlobalBreakerOpen: true, ModelBreakerOpen: true})
	if both == "" {
		t.Error("expected non-empty reason")
	}

	globalOnly := buildRuntimeBreakerReason(SiteRuntimeHealthDetails{GlobalBreakerOpen: true, ModelBreakerOpen: false})
	if globalOnly == "" {
		t.Error("expected non-empty reason")
	}

	modelOnly := buildRuntimeBreakerReason(SiteRuntimeHealthDetails{GlobalBreakerOpen: false, ModelBreakerOpen: true})
	if modelOnly == "" {
		t.Error("expected non-empty reason")
	}
}
