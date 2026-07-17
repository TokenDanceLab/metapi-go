package routing

import (
	"math"
	"testing"
	"time"
)

// =============================================================================
// Fibonacci backoff tests
// =============================================================================

func TestFibonacciNumber(t *testing.T) {
	tests := []struct {
		index    int64
		expected int64
	}{
		{1, 1},
		{2, 1},
		{3, 2},
		{4, 3},
		{5, 5},
		{6, 8},
		{7, 13},
		{8, 21},
		{9, 34},
		{10, 55},
		{11, 89},
		{12, 144},
		{20, 6765},
	}

	for _, tt := range tests {
		got := FibonacciNumber(tt.index)
		if got != tt.expected {
			t.Errorf("fib(%d) = %d, expected %d", tt.index, got, tt.expected)
		}
	}
}

func TestResolveFailureBackoffSec(t *testing.T) {
	tests := []struct {
		failCount *int64
		expected  int64 // in seconds
	}{
		{nil, 15},                          // nil → 1 → fib(1)*15 = 15
		{ptrInt(0), 15},                    // 0 → max(1,0)=1 → 15
		{ptrInt(1), 15},                    // fib(1)*15 = 15
		{ptrInt(2), 15},                    // fib(2)*15 = 15
		{ptrInt(3), 30},                    // fib(3)*15 = 30
		{ptrInt(4), 45},                    // fib(4)*15 = 45
		{ptrInt(5), 75},                    // fib(5)*15 = 75
		{ptrInt(6), 120},                   // fib(6)*15 = 120
		{ptrInt(7), 195},                   // fib(7)*15 = 195
		{ptrInt(8), 315},                   // fib(8)*15 = 315
		{ptrInt(9), 510},                   // fib(9)*15 = 510
		{ptrInt(10), 825},                  // fib(10)*15 = 825
	}

	for _, tt := range tests {
		got := ResolveFailureBackoffSec(tt.failCount)
		if got != tt.expected {
			t.Errorf("ResolveFailureBackoffSec(%v) = %d, expected %d",
				describePtr(tt.failCount), got, tt.expected)
		}
	}
}

func TestResolveFailureBackoffSec_Ceiling(t *testing.T) {
	// Very high fail count should not exceed 30 days
	hugeFailCount := int64(1000)
	got := ResolveFailureBackoffSec(&hugeFailCount)
	maxSec := int64(30 * 24 * 60 * 60) // 30 days
	if got > maxSec {
		t.Errorf("backoff %d exceeds ceiling %d", got, maxSec)
	}
}

func TestClampFailureCooldownMs(t *testing.T) {
	// Default configured max
	clamped := ClampFailureCooldownMs(1000, 60) // 60s max
	if clamped > 60000 {
		t.Errorf("expected clamped to <= 60000, got %d", clamped)
	}

	// Below floor
	clamped = ClampFailureCooldownMs(500, 60)
	if clamped != 500 {
		t.Errorf("expected 500ms (below max), got %d", clamped)
	}

	// Negative
	clamped = ClampFailureCooldownMs(-100, 60)
	if clamped != 0 {
		t.Errorf("expected 0 for negative, got %d", clamped)
	}

	// Not clamping when no max
	clamped = ClampFailureCooldownMs(999999, 0) // 0 max → uses ceiling
	if clamped <= 0 {
		t.Errorf("expected positive clamped value, got %d", clamped)
	}
}

func TestResolveEffectiveFailureCooldownMs(t *testing.T) {
	fc3 := int64(3)
	ms := ResolveEffectiveFailureCooldownMs(&fc3, 0)
	// fib(3)=2, 2*15=30s, 30*1000=30000ms
	if ms != 30000 {
		t.Errorf("expected 30000ms for failCount=3, got %d", ms)
	}

	fc5 := int64(5)
	ms = ResolveEffectiveFailureCooldownMs(&fc5, 0)
	if ms != 75000 {
		t.Errorf("expected 75000ms for failCount=5, got %d", ms)
	}
}

// =============================================================================
// Round-robin tiered cooldown tests
// =============================================================================

func TestResolveRoundRobinCooldownSec(t *testing.T) {
	tests := []struct {
		level    int
		expected int64
	}{
		{-1, 0},
		{0, 0},
		{1, 10 * 60},
		{2, 60 * 60},
		{3, 24 * 60 * 60},
		{4, 24 * 60 * 60}, // clamped to max level
		{99, 24 * 60 * 60},
	}

	for _, tt := range tests {
		got := ResolveRoundRobinCooldownSec(tt.level)
		if got != tt.expected {
			t.Errorf("ResolveRoundRobinCooldownSec(%d) = %d, expected %d",
				tt.level, got, tt.expected)
		}
	}
}

func TestRoundRobinCooldownLevelsSec(t *testing.T) {
	expected := []int64{0, 600, 3600, 86400}
	for i, exp := range expected {
		if i >= len(RoundRobinCooldownLevelsSec) {
			t.Fatalf("level %d out of range", i)
		}
		if RoundRobinCooldownLevelsSec[i] != exp {
			t.Errorf("level %d: expected %d, got %d", i, exp, RoundRobinCooldownLevelsSec[i])
		}
	}
}

func TestApplyRoundRobinCooldown(t *testing.T) {
	// Below threshold: no cooldown applied, count increments
	nextFC, nextLevel, cooldownISO := ApplyRoundRobinCooldown(0, 0, 1000, 0)
	if nextFC != 1 {
		t.Errorf("expected consecutiveFailCount=1, got %d", nextFC)
	}
	if nextLevel != 0 {
		t.Errorf("expected cooldownLevel=0, got %d", nextLevel)
	}
	if cooldownISO != nil {
		t.Errorf("expected no cooldown, got %v", *cooldownISO)
	}

	// At threshold (2 -> 3): cooldown applied, count resets
	nextFC, nextLevel, cooldownISO = ApplyRoundRobinCooldown(2, 0, 1000, 0)
	if nextFC != 0 {
		t.Errorf("expected consecutiveFailCount=0 (reset), got %d", nextFC)
	}
	if nextLevel != 1 {
		t.Errorf("expected cooldownLevel=1, got %d", nextLevel)
	}
	if cooldownISO == nil {
		t.Error("expected cooldown ISO string")
	}

	// Level 1 -> 2 at threshold
	nextFC, nextLevel, cooldownISO = ApplyRoundRobinCooldown(2, 1, 2000, 0)
	if nextLevel != 2 {
		t.Errorf("expected cooldownLevel=2, got %d", nextLevel)
	}
	if nextFC != 0 {
		t.Errorf("expected consecutiveFailCount=0, got %d", nextFC)
	}

	// At max level
	nextFC, nextLevel, cooldownISO = ApplyRoundRobinCooldown(2, 3, 3000, 0)
	if nextLevel != 3 {
		t.Errorf("expected cooldownLevel=3 (max), got %d", nextLevel)
	}
}

func TestApplyFibonacciCooldown(t *testing.T) {
	cooldownISO := ApplyFibonacciCooldown(3, 1000, 0)
	if cooldownISO == nil {
		t.Fatal("expected cooldown ISO")
	}
	// fib(3)=2, 2*15=30s, 1000+30000=31000ms → ISO timestamp
	if *cooldownISO == "" {
		t.Error("expected non-empty ISO string")
	}
}

// =============================================================================
// IsChannelRecentlyFailed tests
// =============================================================================

func TestIsChannelRecentlyFailed(t *testing.T) {
	nowMs := time.Now().UnixMilli()

	// No failures
	if IsChannelRecentlyFailed(nil, nil, nowMs, 0) {
		t.Error("expected false for nil failCount")
	}

	fc0 := int64(0)
	if IsChannelRecentlyFailed(&fc0, nil, nowMs, 0) {
		t.Error("expected false for failCount=0")
	}

	// Recently failed (failCount=1, backoff=15s, just failed now)
	recentFailTime := time.UnixMilli(nowMs - 5000).UTC().Format(time.RFC3339) // 5 seconds ago
	fc1 := int64(1)
	if !IsChannelRecentlyFailed(&fc1, &recentFailTime, nowMs, 0) {
		t.Error("expected true for recent failure (5s ago, 15s backoff)")
	}

	// Old failure (longer than backoff)
	oldFailTime := time.UnixMilli(nowMs - 60000).UTC().Format(time.RFC3339) // 60 seconds ago
	if IsChannelRecentlyFailed(&fc1, &oldFailTime, nowMs, 0) {
		t.Error("expected false for old failure (60s ago, 15s backoff)")
	}

	// Empty lastFailAt
	emptyStr := ""
	if IsChannelRecentlyFailed(&fc1, &emptyStr, nowMs, 0) {
		t.Error("expected false for empty lastFailAt")
	}
}

// =============================================================================
// FilterRecentlyFailedCandidates tests
// =============================================================================

func TestFilterRecentlyFailedCandidates(t *testing.T) {
	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 5000).UTC().Format(time.RFC3339)
	oldISO := time.UnixMilli(nowMs - 60000).UTC().Format(time.RFC3339)

	type testCand struct {
		id         int
		failCount  int64
		lastFailAt string
	}

	getInfo := func(c testCand) (*int64, *string) {
		fc := c.failCount
		lfa := c.lastFailAt
		if lfa == "" {
			return &fc, nil
		}
		return &fc, &lfa
	}

	t.Run("all healthy", func(t *testing.T) {
		candidates := []testCand{
			{1, 0, ""},
			{2, 1, oldISO},
		}
		filtered := FilterRecentlyFailedCandidates(candidates, getInfo, nowMs, 0)
		if len(filtered) != 2 {
			t.Errorf("expected 2 candidates, got %d", len(filtered))
		}
	})

	t.Run("one recently failed", func(t *testing.T) {
		candidates := []testCand{
			{1, 0, ""},
			{2, 1, recentISO},
		}
		filtered := FilterRecentlyFailedCandidates(candidates, getInfo, nowMs, 0)
		if len(filtered) != 1 {
			t.Errorf("expected 1 candidate, got %d", len(filtered))
		}
		if filtered[0].id != 1 {
			t.Errorf("expected candidate 1 (healthy), got %d", filtered[0].id)
		}
	})

	t.Run("all recently failed", func(t *testing.T) {
		candidates := []testCand{
			{1, 1, recentISO},
			{2, 2, recentISO},
		}
		filtered := FilterRecentlyFailedCandidates(candidates, getInfo, nowMs, 0)
		// All are recently failed, but we return all to avoid empty list
		if len(filtered) != 2 {
			t.Errorf("expected all 2 candidates (fallback), got %d", len(filtered))
		}
	})

	t.Run("single candidate", func(t *testing.T) {
		candidates := []testCand{
			{1, 5, recentISO},
		}
		filtered := FilterRecentlyFailedCandidates(candidates, getInfo, nowMs, 0)
		// Single candidate always returned (<=1 check)
		if len(filtered) != 1 {
			t.Errorf("expected 1 candidate, got %d", len(filtered))
		}
	})
}

// =============================================================================
// Short-window limit cooldown tests (constants)
// =============================================================================

func TestShortWindowLimitCooldownMs(t *testing.T) {
	if ShortWindowLimitCooldownMs != 5*60*1000 {
		t.Errorf("expected ShortWindowLimitCooldownMs=300000, got %d", ShortWindowLimitCooldownMs)
	}
}

// =============================================================================
// IsOAuthRouteUnitMemberCoolingDown tests
// =============================================================================

func TestIsOAuthRouteUnitMemberCoolingDown(t *testing.T) {
	nowISO := time.Now().UTC().Format(time.RFC3339)
	pastISO := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	futureISO := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)

	if IsOAuthRouteUnitMemberCoolingDown(nil, nowISO) {
		t.Error("expected false for nil cooldownUntil")
	}

	emptyStr := ""
	if IsOAuthRouteUnitMemberCoolingDown(&emptyStr, nowISO) {
		t.Error("expected false for empty cooldownUntil")
	}

	if IsOAuthRouteUnitMemberCoolingDown(&pastISO, nowISO) {
		t.Error("expected false for past cooldown")
	}

	if !IsOAuthRouteUnitMemberCoolingDown(&futureISO, nowISO) {
		t.Error("expected true for future cooldown")
	}
}

// Regression #424: writers emit millis via timeMsToISO; readers used RFC3339 without millis.
// Lexical compare of "…T15:04:05.500Z" vs "…T15:04:05Z" treats still-cooling channels as eligible.
func TestIsCooldownActive_MillisVsSecondPrecision(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	nowISO := now.UTC().Format(time.RFC3339) // no fractional seconds
	cooldownUntil := timeMsToISO(now.UnixMilli() + 500) // always-present millis

	// Document the lexical trap the parse path must avoid.
	if cooldownUntil > nowISO {
		t.Fatalf("precondition failed: expected lexical cool>now to be false; cool=%q now=%q", cooldownUntil, nowISO)
	}

	if !IsCooldownActive(&cooldownUntil, nowISO) {
		t.Fatalf("cooldownUntil=now+500ms must still be active; cool=%q now=%q", cooldownUntil, nowISO)
	}
	if !IsOAuthRouteUnitMemberCoolingDown(&cooldownUntil, nowISO) {
		t.Fatalf("OAuth member cool path must treat now+500ms as cooling; cool=%q now=%q", cooldownUntil, nowISO)
	}

	// Expired (past) millis cooldown must be inactive against second-precision now.
	pastCool := timeMsToISO(now.UnixMilli() - 500)
	if IsCooldownActive(&pastCool, nowISO) {
		t.Fatalf("past cooldown must be inactive; cool=%q now=%q", pastCool, nowISO)
	}

	// Equal second boundary (no remaining cool window) is not active.
	exact := timeMsToISO(now.UnixMilli())
	if IsCooldownActive(&exact, nowISO) {
		t.Fatalf("cooldownUntil==now must not be active; cool=%q now=%q", exact, nowISO)
	}
}

// =============================================================================
// ParseISOTimeMs tests
// =============================================================================

func TestParseISOTimeMs(t *testing.T) {
	result := ParseISOTimeMs(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}

	emptyStr := ""
	result = ParseISOTimeMs(&emptyStr)
	if result != nil {
		t.Error("expected nil for empty string")
	}

	validISO := "2024-01-15T10:30:00Z"
	result = ParseISOTimeMs(&validISO)
	if result == nil {
		t.Fatal("expected non-nil for valid ISO")
	}
	if *result <= 0 {
		t.Error("expected positive timestamp")
	}
}

// =============================================================================
// CompareNullableTimeAsc tests
// =============================================================================

func TestCompareNullableTimeAsc(t *testing.T) {
	earlier := "2024-01-01T00:00:00Z"
	later := "2024-06-01T00:00:00Z"

	if CompareNullableTimeAsc(nil, nil) != 0 {
		t.Error("expected 0 for both nil")
	}
	if CompareNullableTimeAsc(nil, &earlier) >= 0 {
		t.Error("expected <0 for nil < earlier")
	}
	if CompareNullableTimeAsc(&earlier, nil) <= 0 {
		t.Error("expected >0 for earlier > nil")
	}
	if CompareNullableTimeAsc(&earlier, &later) >= 0 {
		t.Error("expected <0 for earlier < later")
	}
	if CompareNullableTimeAsc(&later, &earlier) <= 0 {
		t.Error("expected >0 for later > earlier")
	}
	if CompareNullableTimeAsc(&earlier, &earlier) != 0 {
		t.Error("expected 0 for same")
	}
}

// =============================================================================
// Clamp helpers
// =============================================================================

func TestClampNumber(t *testing.T) {
	if v := ClampNumber(0.5, 0, 1); math.Abs(v-0.5) > 0.001 {
		t.Errorf("expected 0.5, got %f", v)
	}
	if v := ClampNumber(-1.0, 0, 1); math.Abs(v-0.0) > 0.001 {
		t.Errorf("expected 0.0 for -1.0 clamped, got %f", v)
	}
	if v := ClampNumber(2.0, 0, 1); math.Abs(v-1.0) > 0.001 {
		t.Errorf("expected 1.0 for 2.0 clamped, got %f", v)
	}
	if v := ClampNumber(1.5, 0, 1.5); math.Abs(v-1.5) > 0.001 {
		t.Errorf("expected 1.5 at exact max, got %f", v)
	}
}

func TestClampInt(t *testing.T) {
	if v := ClampInt(5, 0, 10); v != 5 {
		t.Errorf("expected 5, got %d", v)
	}
	if v := ClampInt(-1, 0, 10); v != 0 {
		t.Errorf("expected 0 for -1 clamped, got %d", v)
	}
	if v := ClampInt(20, 0, 10); v != 10 {
		t.Errorf("expected 10 for 20 clamped, got %d", v)
	}
}

// =============================================================================
// IsContributionCloseToBest tests
// =============================================================================

func TestIsContributionCloseToBest(t *testing.T) {
	if !IsContributionCloseToBest(0.95, 1.0, 0.92) {
		t.Error("expected 0.95 to be close to 1.0 with ratio 0.92")
	}
	if IsContributionCloseToBest(0.90, 1.0, 0.92) {
		t.Error("expected 0.90 NOT to be close to 1.0 with ratio 0.92")
	}
	if !IsContributionCloseToBest(0.5, 0.0, 0.92) {
		t.Error("expected true when bestValue=0")
	}
}

// =============================================================================
// Helpers
// =============================================================================

func ptrInt(v int64) *int64 { return &v }

func describePtr(p *int64) string {
	if p == nil {
		return "nil"
	}
	return formatInt(*p)
}
