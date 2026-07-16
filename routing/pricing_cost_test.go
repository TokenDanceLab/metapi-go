package routing

import (
	"math"
	"testing"
)

func ptrF(v float64) *float64 { return &v }

func boolPtr(v bool) *bool { return &v }

func TestIsClaudeModel(t *testing.T) {
	cases := []struct {
		name  string
		want  bool
	}{
		{"claude-opus-4-7", true},
		{"Claude-Sonnet-4", true},
		{"anthropic/claude-3-5-sonnet", true},
		{"gpt-4o", false},
		{"gemini-2.0-flash", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsClaudeModel(tc.name); got != tc.want {
			t.Errorf("IsClaudeModel(%q)=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveCacheRatio_ClaudeMissingFallsBackToAnthropic(t *testing.T) {
	got := ResolveCacheRatio("claude-haiku-4-5-20251001", nil)
	if got != ClaudeCacheRatio {
		t.Fatalf("missing Claude cache_ratio: got %v want %v", got, ClaudeCacheRatio)
	}
	got = ResolveCacheCreationRatio("claude-haiku-4-5-20251001", nil)
	if got != ClaudeCacheCreationRatio {
		t.Fatalf("missing Claude cache_creation_ratio: got %v want %v", got, ClaudeCacheCreationRatio)
	}
}

func TestResolveCacheRatio_NonClaudeMissingFallsBackToOne(t *testing.T) {
	got := ResolveCacheRatio("gpt-4o", nil)
	if got != DefaultCacheRatio {
		t.Fatalf("missing non-Claude cache_ratio: got %v want %v", got, DefaultCacheRatio)
	}
	got = ResolveCacheCreationRatio("gpt-4o", nil)
	if got != DefaultCacheCreationRatio {
		t.Fatalf("missing non-Claude cache_creation_ratio: got %v want %v", got, DefaultCacheCreationRatio)
	}
}

func TestResolveCacheRatio_ExplicitPresentWins(t *testing.T) {
	// Explicit ratios always win — even for Claude, even if "odd".
	got := ResolveCacheRatio("claude-sonnet", ptrF(0.3))
	if got != 0.3 {
		t.Fatalf("explicit cache_ratio ignored: got %v", got)
	}
	// Explicit zero is free cache read, not "missing".
	got = ResolveCacheRatio("claude-sonnet", ptrF(0))
	if got != 0 {
		t.Fatalf("explicit zero cache_ratio should stay 0, got %v", got)
	}
	// NaN / negative treated as missing → Claude default.
	nan := math.NaN()
	got = ResolveCacheRatio("claude-sonnet", &nan)
	if got != ClaudeCacheRatio {
		t.Fatalf("NaN should fall back to Claude default, got %v", got)
	}
	got = ResolveCacheRatio("claude-sonnet", ptrF(-1))
	if got != ClaudeCacheRatio {
		t.Fatalf("negative should fall back to Claude default, got %v", got)
	}
}

func TestNormalizePricingRatio_KindAware(t *testing.T) {
	// Missing cache fields on Claude → Anthropic defaults.
	if got := NormalizePricingRatio(nil, "claude-opus-4-7", "cache", 1); got != ClaudeCacheRatio {
		t.Fatalf("cache kind: got %v", got)
	}
	if got := NormalizePricingRatio(nil, "claude-opus-4-7", "cache_creation", 1); got != ClaudeCacheCreationRatio {
		t.Fatalf("cache_creation kind: got %v", got)
	}
	// Present value wins.
	if got := NormalizePricingRatio(0.05, "claude-opus-4-7", "cache", 1); got != 0.05 {
		t.Fatalf("present cache ratio: got %v", got)
	}
	// Non-Claude missing → 1.0
	if got := NormalizePricingRatio(nil, "gpt-4o", "cache", 1); got != 1 {
		t.Fatalf("non-claude missing: got %v", got)
	}
}

func TestCalculateModelUsageBreakdown_WithExplicitCacheRatio(t *testing.T) {
	// Mirrors TS modelPricingService.test.ts "splits cache read and cache creation"
	model := PricingModel{
		ModelName:          "gpt-4o",
		QuotaType:          0,
		ModelRatio:         2.5,
		CompletionRatio:    5,
		CacheRatio:         ptrF(0.1),
		CacheCreationRatio: ptrF(1.25),
		EnableGroups:       []string{"default"},
	}
	detail := CalculateModelUsageBreakdown(model, UsageForCost{
		PromptTokens:             146638,
		CompletionTokens:         172,
		TotalTokens:              146810,
		CacheReadTokens:          145692,
		CacheCreationTokens:      945,
		PromptTokensIncludeCache: boolPtr(true),
	}, map[string]float64{"default": 1})

	if detail == nil {
		t.Fatal("expected billing details")
	}
	if detail.Usage.BillablePromptTokens != 1 {
		t.Errorf("billablePromptTokens=%d want 1", detail.Usage.BillablePromptTokens)
	}
	if detail.Pricing.CacheRatio != 0.1 || detail.Pricing.CacheCreationRatio != 1.25 {
		t.Errorf("pricing ratios = %+v", detail.Pricing)
	}
	if math.Abs(detail.Breakdown.InputPerMillion-5) > 1e-9 {
		t.Errorf("inputPerMillion=%v want 5", detail.Breakdown.InputPerMillion)
	}
	if math.Abs(detail.Breakdown.OutputPerMillion-25) > 1e-9 {
		t.Errorf("outputPerMillion=%v want 25", detail.Breakdown.OutputPerMillion)
	}
	if math.Abs(detail.Breakdown.CacheReadPerMillion-0.5) > 1e-9 {
		t.Errorf("cacheReadPerMillion=%v want 0.5", detail.Breakdown.CacheReadPerMillion)
	}
	if math.Abs(detail.Breakdown.CacheCreationPerMillion-6.25) > 1e-9 {
		t.Errorf("cacheCreationPerMillion=%v want 6.25", detail.Breakdown.CacheCreationPerMillion)
	}
	if math.Abs(detail.Breakdown.TotalCost-0.083057) > 1e-9 {
		t.Errorf("totalCost=%v want 0.083057", detail.Breakdown.TotalCost)
	}
}

func TestCalculateModelUsageBreakdown_ClaudeMissingCacheRatioUsesAnthropicDefaults(t *testing.T) {
	// Upstream #496 signature: missing cache_ratio used to force 1.0 so
	// cacheReadPerMillion == inputPerMillion. With the fix, Claude defaults to
	// 0.1 / 1.25 → cacheRead = input×0.1, cacheCreation = input×1.25.
	model := PricingModel{
		ModelName:       "claude-opus-4-7",
		QuotaType:       0,
		ModelRatio:      2.5, // input $/M = 5
		CompletionRatio: 5,   // output $/M = 25
		// CacheRatio / CacheCreationRatio intentionally nil
		EnableGroups: []string{"default"},
	}
	detail := CalculateModelUsageBreakdown(model, UsageForCost{
		PromptTokens:             146638,
		CompletionTokens:         172,
		TotalTokens:              146810,
		CacheReadTokens:          145692,
		CacheCreationTokens:      945,
		PromptTokensIncludeCache: boolPtr(true),
	}, map[string]float64{"default": 1})

	if detail == nil {
		t.Fatal("expected billing details")
	}
	if detail.Pricing.CacheRatio != ClaudeCacheRatio {
		t.Fatalf("cacheRatio=%v want %v", detail.Pricing.CacheRatio, ClaudeCacheRatio)
	}
	if detail.Pricing.CacheCreationRatio != ClaudeCacheCreationRatio {
		t.Fatalf("cacheCreationRatio=%v want %v", detail.Pricing.CacheCreationRatio, ClaudeCacheCreationRatio)
	}
	// input=5, cacheRead=0.5, cacheCreation=6.25 — NOT equal to input.
	if math.Abs(detail.Breakdown.InputPerMillion-5) > 1e-9 {
		t.Errorf("inputPerMillion=%v want 5", detail.Breakdown.InputPerMillion)
	}
	if math.Abs(detail.Breakdown.CacheReadPerMillion-0.5) > 1e-9 {
		t.Errorf("cacheReadPerMillion=%v want 0.5 (was wrongly 5.0 with fallback 1.0)", detail.Breakdown.CacheReadPerMillion)
	}
	if math.Abs(detail.Breakdown.CacheCreationPerMillion-6.25) > 1e-9 {
		t.Errorf("cacheCreationPerMillion=%v want 6.25 (was wrongly 5.0 with fallback 1.0)", detail.Breakdown.CacheCreationPerMillion)
	}
	if math.Abs(detail.Breakdown.CacheReadPerMillion-detail.Breakdown.InputPerMillion) < 1e-9 {
		t.Fatal("cacheReadPerMillion must not equal inputPerMillion for Claude missing cache_ratio")
	}
	if math.Abs(detail.Breakdown.TotalCost-0.083057) > 1e-9 {
		t.Errorf("totalCost=%v want 0.083057", detail.Breakdown.TotalCost)
	}
}

func TestCalculateModelUsageBreakdown_NonClaudeMissingCacheRatioStaysOne(t *testing.T) {
	model := PricingModel{
		ModelName:       "gpt-4o",
		QuotaType:       0,
		ModelRatio:      2.5,
		CompletionRatio: 5,
		EnableGroups:    []string{"default"},
	}
	detail := CalculateModelUsageBreakdown(model, UsageForCost{
		PromptTokens:             1000,
		CompletionTokens:         0,
		TotalTokens:              1000,
		CacheReadTokens:          500,
		PromptTokensIncludeCache: boolPtr(true),
	}, map[string]float64{"default": 1})

	if detail == nil {
		t.Fatal("expected billing details")
	}
	if detail.Pricing.CacheRatio != 1.0 {
		t.Fatalf("non-Claude missing cache_ratio should stay 1.0, got %v", detail.Pricing.CacheRatio)
	}
	// inputPerMillion = 5; cacheReadPerMillion with ratio 1.0 also 5
	if math.Abs(detail.Breakdown.CacheReadPerMillion-5) > 1e-9 {
		t.Errorf("cacheReadPerMillion=%v want 5", detail.Breakdown.CacheReadPerMillion)
	}
}

func TestCalculateModelUsageCost_PromptTokensExcludeCache(t *testing.T) {
	// Mirrors TS "keeps prompt tokens intact when upstream reports cache tokens separately"
	model := PricingModel{
		ModelName:          "claude-sonnet",
		QuotaType:          0,
		ModelRatio:         3,
		CompletionRatio:    5,
		CacheRatio:         ptrF(0.3),
		CacheCreationRatio: ptrF(1.25),
		EnableGroups:       []string{"default"},
	}
	cost := CalculateModelUsageCost(model, UsageForCost{
		PromptTokens:             120,
		CompletionTokens:         30,
		TotalTokens:              150,
		CacheReadTokens:          1000,
		CacheCreationTokens:      40,
		PromptTokensIncludeCache: boolPtr(false),
	}, map[string]float64{"default": 1})
	if math.Abs(cost-0.00372) > 1e-9 {
		t.Fatalf("cost=%v want 0.00372", cost)
	}
}

func TestCalculateModelUsageCost_ZeroCacheRatio(t *testing.T) {
	model := PricingModel{
		ModelName:       "claude-sonnet",
		QuotaType:       0,
		ModelRatio:      1,
		CompletionRatio: 1,
		CacheRatio:      ptrF(0), // free cache reads
		EnableGroups:    []string{"default"},
	}
	detail := CalculateModelUsageBreakdown(model, UsageForCost{
		PromptTokens:             1000,
		CompletionTokens:         0,
		TotalTokens:              1000,
		CacheReadTokens:          1000,
		PromptTokensIncludeCache: boolPtr(true),
	}, map[string]float64{"default": 1})
	if detail == nil {
		t.Fatal("expected details")
	}
	if detail.Pricing.CacheRatio != 0 {
		t.Fatalf("cacheRatio=%v want 0", detail.Pricing.CacheRatio)
	}
	if detail.Breakdown.CacheReadCost != 0 {
		t.Fatalf("cacheReadCost=%v want 0", detail.Breakdown.CacheReadCost)
	}
	if detail.Breakdown.TotalCost != 0 {
		// billable prompt = 0, cache cost = 0
		t.Fatalf("totalCost=%v want 0", detail.Breakdown.TotalCost)
	}
}

func TestBuildPricingOverrideModel_ClaudeMissingCache(t *testing.T) {
	model, groups := BuildPricingOverrideModel("claude-haiku-4-5", ProxyBillingPricingOverride{
		ModelRatio:      2.5,
		CompletionRatio: 5,
		// CacheRatio / CacheCreationRatio omitted
	})
	if model.CacheRatio == nil || *model.CacheRatio != ClaudeCacheRatio {
		t.Fatalf("override cacheRatio=%v want %v", model.CacheRatio, ClaudeCacheRatio)
	}
	if model.CacheCreationRatio == nil || *model.CacheCreationRatio != ClaudeCacheCreationRatio {
		t.Fatalf("override cacheCreationRatio=%v want %v", model.CacheCreationRatio, ClaudeCacheCreationRatio)
	}
	cost := CalculateModelUsageCost(model, UsageForCost{
		PromptTokens:             146638,
		CompletionTokens:         172,
		TotalTokens:              146810,
		CacheReadTokens:          145692,
		CacheCreationTokens:      945,
		PromptTokensIncludeCache: boolPtr(true),
	}, groups)
	if math.Abs(cost-0.083057) > 1e-9 {
		t.Fatalf("override cost=%v want 0.083057", cost)
	}
}

func TestCacheAwarePerMillionRates_ClaudeMissing(t *testing.T) {
	in, out, cr, cc := CacheAwarePerMillionRates(PricingModel{
		ModelName:       "claude-opus-4-7",
		ModelRatio:      2.5,
		CompletionRatio: 5,
	}, 1)
	if in != 5 || out != 25 || cr != 0.5 || cc != 6.25 {
		t.Fatalf("rates in=%v out=%v cr=%v cc=%v", in, out, cr, cc)
	}
}

func TestFallbackTokenCost(t *testing.T) {
	if got := FallbackTokenCost(1500, "new-api"); math.Abs(got-0.003) > 1e-9 {
		t.Fatalf("new-api fallback=%v want 0.003", got)
	}
	if got := FallbackTokenCost(1500, "veloera"); math.Abs(got-0.0015) > 1e-9 {
		t.Fatalf("veloera fallback=%v want 0.0015", got)
	}
}

func TestCalculateModelUsageCost_TokenBasedNoCache(t *testing.T) {
	// Mirrors TS first unit test without cache fields.
	model := PricingModel{
		ModelName:       "gpt-4o",
		QuotaType:       0,
		ModelRatio:      2,
		CompletionRatio: 1.5,
		EnableGroups:    []string{"vip"},
	}
	cost := CalculateModelUsageCost(model, UsageForCost{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	}, map[string]float64{"default": 1, "vip": 2})
	if math.Abs(cost-0.014) > 1e-9 {
		t.Fatalf("cost=%v want 0.014", cost)
	}
}
