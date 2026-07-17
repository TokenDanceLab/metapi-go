package proxyhandler

import "testing"

func TestEstimateBillingCostFromUsage_FallbackPositive(t *testing.T) {
	got := EstimateBillingCostFromUsage("gpt-4o", "openai", ParsedUsage{
		PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500, Found: true, Source: "upstream",
	})
	if got.EstimatedCost <= 0 {
		t.Fatalf("expected positive cost, got %v", got.EstimatedCost)
	}
	if got.BillingDetails == nil {
		t.Fatal("nil details")
	}
}

func TestEstimateBillingCostFromUsage_ClaudeCacheFields(t *testing.T) {
	got := EstimateBillingCostFromUsage("claude-sonnet-4", "anthropic", ParsedUsage{
		PromptTokens: 100, CompletionTokens: 50, CacheReadTokens: 20, CacheCreationTokens: 10,
		Found: true, Source: "upstream",
	})
	if got.BillingDetails == nil {
		t.Fatal("nil details")
	}

	breakdown, ok := got.BillingDetails["breakdown"].(map[string]any)
	if !ok {
		t.Fatalf("breakdown missing: %#v", got.BillingDetails)
	}

	pricing, ok := got.BillingDetails["pricing"].(map[string]any)
	if !ok {
		t.Fatalf("pricing missing: %#v", got.BillingDetails)
	}

	// EstimateBillingCostFromUsage builds PricingModel without CacheRatio pointers.
	// CalculateModelUsageBreakdown must surface Claude Anthropic defaults via
	// ResolveCacheRatio / ResolveCacheCreationRatio (0.1 / 1.25).
	if got := asFloat(t, pricing["cache_ratio"]); got != 0.1 {
		t.Fatalf("claude pricing.cache_ratio=%v want 0.1", got)
	}
	if got := asFloat(t, pricing["cache_creation_ratio"]); got != 1.25 {
		t.Fatalf("claude pricing.cache_creation_ratio=%v want 1.25", got)
	}

	// Cache token costs must be present and positive when cache tokens > 0.
	cacheReadCost := asFloat(t, breakdown["cache_read_cost"])
	if cacheReadCost <= 0 {
		t.Fatalf("cache_read_cost should be > 0 when cache_read_tokens > 0, got %v", cacheReadCost)
	}
	cacheCreationCost := asFloat(t, breakdown["cache_creation_cost"])
	if cacheCreationCost <= 0 {
		t.Fatalf("cache_creation_cost should be > 0 when cache_creation_tokens > 0, got %v", cacheCreationCost)
	}
}

func TestEstimateBillingCostFromUsage_NonClaudeMissingCacheRatiosStayOne(t *testing.T) {
	got := EstimateBillingCostFromUsage("gpt-4o", "openai", ParsedUsage{
		PromptTokens: 100, CompletionTokens: 50, CacheReadTokens: 20, CacheCreationTokens: 10,
		Found: true, Source: "upstream",
	})
	if got.BillingDetails == nil {
		t.Fatal("nil details")
	}

	pricing, ok := got.BillingDetails["pricing"].(map[string]any)
	if !ok {
		t.Fatalf("pricing missing: %#v", got.BillingDetails)
	}

	// Non-Claude models keep historical MetAPI fallback of 1.0 when ratios are missing.
	if got := asFloat(t, pricing["cache_ratio"]); got != 1.0 {
		t.Fatalf("non-claude pricing.cache_ratio=%v want 1.0", got)
	}
	if got := asFloat(t, pricing["cache_creation_ratio"]); got != 1.0 {
		t.Fatalf("non-claude pricing.cache_creation_ratio=%v want 1.0", got)
	}

	breakdown, ok := got.BillingDetails["breakdown"].(map[string]any)
	if !ok {
		t.Fatalf("breakdown missing: %#v", got.BillingDetails)
	}
	if asFloat(t, breakdown["cache_read_cost"]) <= 0 {
		t.Fatalf("cache_read_cost should be present and > 0 when tokens > 0: %#v", breakdown)
	}
	if asFloat(t, breakdown["cache_creation_cost"]) <= 0 {
		t.Fatalf("cache_creation_cost should be present and > 0 when tokens > 0: %#v", breakdown)
	}
}

func asFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		t.Fatalf("expected numeric value, got %T (%#v)", v, v)
		return 0
	}
}
