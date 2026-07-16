package proxyhandler

import (
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func TestBuildRequestCostAttribution_FallbackOpenAI(t *testing.T) {
	usage := ParsedUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		Found:            true,
		Source:           usageSourceUpstream,
	}
	attr := buildRequestCostAttribution("gpt-4o", "new-api", usage, nil, nil)
	if attr.Source != routing.CostSourceFallback {
		t.Fatalf("source = %q, want fallback", attr.Source)
	}
	// new-api fallback: tokens / 5e5
	if math.Abs(attr.EstimatedCost-0.003) > 1e-9 {
		t.Fatalf("cost = %v, want 0.003", attr.EstimatedCost)
	}
	if attr.BillingDetails == nil {
		t.Fatal("expected billing details")
	}
	if attr.BillingDetails.Usage.PromptTokens != 1000 || attr.BillingDetails.Usage.CompletionTokens != 500 {
		t.Fatalf("usage section = %+v", attr.BillingDetails.Usage)
	}
	// Fallback zeros ratios so operators can tell "unknown pricing" from 1.0.
	if attr.BillingDetails.Pricing.CacheRatio != 0 || attr.BillingDetails.Pricing.ModelRatio != 0 {
		t.Fatalf("fallback ratios should be 0, got %+v", attr.BillingDetails.Pricing)
	}
	if attr.BillingDetails.CostSource != routing.CostSourceFallback {
		t.Fatalf("costSource = %q", attr.BillingDetails.CostSource)
	}
}

func TestBuildRequestCostAttribution_AnthropicCacheWithPricingModel(t *testing.T) {
	include := true
	usage := ParsedUsage{
		PromptTokens:             146638,
		CompletionTokens:         172,
		TotalTokens:              146810,
		CacheReadTokens:          145692,
		CacheCreationTokens:      945,
		PromptTokensIncludeCache: &include,
		Found:                    true,
		Source:                   usageSourceUpstream,
	}
	// Missing cache ratios → Claude defaults 0.1 / 1.25 (not silent 1.0).
	model := &routing.PricingModel{
		ModelName:       "claude-opus-4-7",
		QuotaType:       0,
		ModelRatio:      2.5,
		CompletionRatio: 5,
		EnableGroups:    []string{"default"},
	}
	attr := buildRequestCostAttribution("claude-opus-4-7", "new-api", usage, model, map[string]float64{"default": 1})
	if attr.Source != routing.CostSourcePricingModel {
		t.Fatalf("source = %q, want pricing_model", attr.Source)
	}
	if attr.BillingDetails == nil {
		t.Fatal("expected billing details")
	}
	if attr.BillingDetails.Pricing.CacheRatio != routing.ClaudeCacheRatio {
		t.Fatalf("cacheRatio = %v, want %v", attr.BillingDetails.Pricing.CacheRatio, routing.ClaudeCacheRatio)
	}
	if attr.BillingDetails.Pricing.CacheCreationRatio != routing.ClaudeCacheCreationRatio {
		t.Fatalf("cacheCreationRatio = %v, want %v", attr.BillingDetails.Pricing.CacheCreationRatio, routing.ClaudeCacheCreationRatio)
	}
	if math.Abs(attr.EstimatedCost-0.083057) > 1e-9 {
		t.Fatalf("cost = %v, want 0.083057", attr.EstimatedCost)
	}
	if attr.BillingDetails.Usage.CacheReadTokens != 145692 {
		t.Fatalf("cacheReadTokens = %d", attr.BillingDetails.Usage.CacheReadTokens)
	}
}

func TestBuildRequestCostAttribution_ZeroUsage(t *testing.T) {
	attr := buildRequestCostAttribution("gpt-4o", "new-api", ParsedUsage{}, nil, nil)
	if attr.Source != routing.CostSourceZero {
		t.Fatalf("source = %q, want zero", attr.Source)
	}
	if attr.EstimatedCost != 0 {
		t.Fatalf("cost = %v, want 0", attr.EstimatedCost)
	}
	if attr.BillingDetails == nil {
		t.Fatal("expected zero shell billing details")
	}
}

func TestBuildRequestCostAttribution_ReasoningTokensOperatorOnly(t *testing.T) {
	usage := ParsedUsage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
		ReasoningTokens:  12,
		Found:            true,
	}
	attr := buildRequestCostAttribution("gpt-4o", "new-api", usage, nil, nil)
	if attr.ReasoningTokens != 12 {
		t.Fatalf("reasoning = %d", attr.ReasoningTokens)
	}
	if attr.BillingDetails == nil || attr.BillingDetails.Usage.ReasoningTokens != 12 {
		t.Fatalf("billing reasoning = %+v", attr.BillingDetails)
	}
}

func TestApplyCostAttributionHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	usage := ParsedUsage{
		PromptTokens:        11,
		CompletionTokens:    22,
		TotalTokens:         33,
		CacheReadTokens:     5,
		CacheCreationTokens: 2,
		ReasoningTokens:     7,
		Found:               true,
	}
	attr := routing.RequestCostAttribution{
		EstimatedCost: 0.003,
		Source:        routing.CostSourceFallback,
	}
	applyCostAttributionHeaders(rec, usage, attr)
	if got := rec.Header().Get(headerMetapiCost); got != "0.003" {
		t.Fatalf("X-Metapi-Cost = %q", got)
	}
	if got := rec.Header().Get(headerMetapiCostSource); got != routing.CostSourceFallback {
		t.Fatalf("X-Metapi-Cost-Source = %q", got)
	}
	if got := rec.Header().Get(headerMetapiPromptTokens); got != "11" {
		t.Fatalf("prompt header = %q", got)
	}
	if got := rec.Header().Get(headerMetapiCacheReadTokens); got != "5" {
		t.Fatalf("cache read header = %q", got)
	}
	if got := rec.Header().Get(headerMetapiReasoningTokens); got != "7" {
		t.Fatalf("reasoning header = %q", got)
	}
}

func TestWriteSuccessProxyLogPersistsBillingDetails(t *testing.T) {
	var got proxy.ProxyLogEntry
	cfg := &UpstreamConfig{
		LogProxy: func(_ context.Context, entry proxy.ProxyLogEntry) error {
			got = entry
			return nil
		},
	}
	selected := &routing.SelectedChannel{
		Channel: store.RouteChannel{ID: 9, RouteID: 3},
		Account: store.Account{ID: 4},
		Site:    store.Site{ID: 2, Platform: "new-api", Name: "demo"},
	}
	usage := ParsedUsage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		Found:            true,
		Source:           usageSourceUpstream,
	}
	writeSuccessProxyLog(context.Background(), cfg, selected, nil, "gpt-4o", "/v1/chat/completions", 42, 200, false, usage, 0)

	if math.Abs(got.EstimatedCost-0.003) > 1e-9 {
		t.Fatalf("EstimatedCost = %v, want 0.003", got.EstimatedCost)
	}
	if got.BillingDetails == nil {
		t.Fatal("BillingDetails is nil")
	}
	raw, err := json.Marshal(got.BillingDetails)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var details routing.ProxyBillingDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if details.Usage.PromptTokens != 1000 || details.Usage.CompletionTokens != 500 {
		t.Fatalf("details.usage = %+v", details.Usage)
	}
	if details.CostSource != routing.CostSourceFallback {
		t.Fatalf("costSource = %q", details.CostSource)
	}
}

func TestWriteSuccessProxyLogWithHeadersSetsResponseHeaders(t *testing.T) {
	cfg := &UpstreamConfig{
		LogProxy: func(_ context.Context, _ proxy.ProxyLogEntry) error { return nil },
	}
	selected := &routing.SelectedChannel{
		Channel: store.RouteChannel{ID: 1},
		Account: store.Account{ID: 1},
		Site:    store.Site{Platform: "new-api"},
	}
	usage := ParsedUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Found:            true,
	}
	rec := httptest.NewRecorder()
	writeSuccessProxyLogWithHeaders(context.Background(), rec, cfg, selected, nil, "gpt-4o", "/v1/chat/completions", 10, 200, false, usage, 0)
	if got := rec.Header().Get(headerMetapiCost); got == "" {
		t.Fatal("expected X-Metapi-Cost header")
	}
	if got := rec.Header().Get(headerMetapiTotalTokens); got != "150" {
		t.Fatalf("total tokens header = %q", got)
	}
}
