package proxyhandler

import (
	"strings"

	"github.com/tokendancelab/metapi-go/routing"
)

// BillingCostResult is the cost estimate attached to a successful proxy log.
type BillingCostResult struct {
	EstimatedCost  float64
	BillingDetails map[string]any
}

// EstimateBillingCostFromUsage builds estimated_cost + billing_details for proxy_logs.
func EstimateBillingCostFromUsage(modelName, platform string, usage ParsedUsage) BillingCostResult {
	total := usage.TotalTokens
	if total <= 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	cost := routing.FallbackTokenCost(total, platform)
	details := map[string]any{
		"quota_type": 0,
		"usage": map[string]any{
			"prompt_tokens":         usage.PromptTokens,
			"completion_tokens":     usage.CompletionTokens,
			"total_tokens":          total,
			"cache_read_tokens":     usage.CacheReadTokens,
			"cache_creation_tokens": usage.CacheCreationTokens,
		},
		"pricing": map[string]any{
			"source":   "fallback_token_cost",
			"platform": strings.TrimSpace(platform),
			"model":    strings.TrimSpace(modelName),
		},
		"breakdown": map[string]any{
			"total_cost": cost,
		},
		"usage_source": usage.Source,
	}
	if usage.Found {
		pm := routing.PricingModel{
			ModelName:       modelName,
			QuotaType:       0,
			ModelRatio:      1,
			CompletionRatio: 1,
		}
		u := routing.UsageForCost{
			PromptTokens:        usage.PromptTokens,
			CompletionTokens:    usage.CompletionTokens,
			TotalTokens:         total,
			CacheReadTokens:     usage.CacheReadTokens,
			CacheCreationTokens: usage.CacheCreationTokens,
		}
		if detail := routing.CalculateModelUsageBreakdown(pm, u, map[string]float64{"default": 1}); detail != nil {
			details["breakdown"] = map[string]any{
				"input_cost":          detail.Breakdown.InputCost,
				"output_cost":         detail.Breakdown.OutputCost,
				"cache_read_cost":     detail.Breakdown.CacheReadCost,
				"cache_creation_cost": detail.Breakdown.CacheCreationCost,
				"total_cost":          detail.Breakdown.TotalCost,
				"fallback_total_cost": cost,
			}
			details["pricing"] = map[string]any{
				"source":               "model_ratio_defaults",
				"model_ratio":          detail.Pricing.ModelRatio,
				"completion_ratio":     detail.Pricing.CompletionRatio,
				"cache_ratio":          detail.Pricing.CacheRatio,
				"cache_creation_ratio": detail.Pricing.CacheCreationRatio,
				"platform":             strings.TrimSpace(platform),
				"model":                strings.TrimSpace(modelName),
			}
			if detail.Breakdown.TotalCost > 0 {
				cost = detail.Breakdown.TotalCost
			}
		}
	}
	return BillingCostResult{EstimatedCost: cost, BillingDetails: details}
}
