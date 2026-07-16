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
	if _, ok := got.BillingDetails["breakdown"].(map[string]any); !ok {
		t.Fatalf("breakdown missing: %#v", got.BillingDetails)
	}
}
