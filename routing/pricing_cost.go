package routing

import (
	"math"
	"strings"
)

// Claude / Anthropic official prompt-cache multipliers used when an upstream
// pricing payload omits cache_ratio / cache_creation_ratio for Claude models.
//
// Source of intent: cita-777/metapi#496 (AnyRouter /api/pricing often lacks
// these fields and the historical normalizeRatio(undefined, 1) fallback made
// cache_read == input and cache_creation == input). Anthropic public pricing
// ratios relative to input are:
//
//	cache read     = 0.10 × input
//	cache creation = 1.25 × input
//
// Non-Claude models keep the original MetAPI fallback of 1.0 so we do not
// invent vendor-specific multipliers without evidence.
const (
	// DefaultCacheRatio is the historical MetAPI fallback when cache_ratio is
	// missing for non-Claude models (input × 1.0).
	DefaultCacheRatio = 1.0
	// DefaultCacheCreationRatio is the historical MetAPI fallback when
	// cache_creation_ratio is missing for non-Claude models.
	DefaultCacheCreationRatio = 1.0

	// ClaudeCacheRatio is Anthropic's cache-read multiplier (input × 0.1).
	ClaudeCacheRatio = 0.1
	// ClaudeCacheCreationRatio is Anthropic's cache-write / creation
	// multiplier (input × 1.25).
	ClaudeCacheCreationRatio = 1.25

	// oneHubPerCallRatio mirrors TS ONE_HUB_PER_CALL_RATIO for times pricing.
	oneHubPerCallRatio = 0.002
)

// PricingModel is a normalized upstream pricing row used for cost estimation.
// Pointer ratios distinguish "missing" from an explicit zero.
type PricingModel struct {
	ModelName           string
	QuotaType           int // 0 = token-based, 1 = per-call
	ModelRatio          float64
	CompletionRatio     float64
	CacheRatio          *float64
	CacheCreationRatio  *float64
	ModelPrice          *ModelPrice
	EnableGroups        []string
}

// ModelPrice is either a flat per-call price or input/output pair.
type ModelPrice struct {
	// Flat is set for scalar model_price values.
	Flat *float64
	// Input/Output are set for object model_price values (one-hub times).
	Input  float64
	Output float64
	IsPair bool
}

// UsageForCost is token usage for a single request cost estimate.
type UsageForCost struct {
	PromptTokens             int64
	CompletionTokens         int64
	TotalTokens              int64
	CacheReadTokens          int64
	CacheCreationTokens      int64
	// PromptTokensIncludeCache:
	//   true/nil → prompt tokens already include cache tokens (subtract for billable input)
	//   false    → prompt tokens are exclusive of cache tokens
	PromptTokensIncludeCache *bool
}

// ProxyBillingPricingOverride supplies ratios from self-log metadata.
type ProxyBillingPricingOverride struct {
	ModelRatio         float64
	CompletionRatio    float64
	CacheRatio         *float64
	CacheCreationRatio *float64
	GroupRatio         *float64
}

// ProxyBillingDetails mirrors the TS ProxyBillingDetails shape (camelCase JSON).
type ProxyBillingDetails struct {
	QuotaType int                    `json:"quotaType"`
	Usage     ProxyBillingUsage      `json:"usage"`
	Pricing   ProxyBillingPricing    `json:"pricing"`
	Breakdown ProxyBillingBreakdown  `json:"breakdown"`
}

// ProxyBillingUsage is the usage section of billing details.
type ProxyBillingUsage struct {
	PromptTokens             int64 `json:"promptTokens"`
	CompletionTokens         int64 `json:"completionTokens"`
	TotalTokens              int64 `json:"totalTokens"`
	CacheReadTokens          int64 `json:"cacheReadTokens"`
	CacheCreationTokens      int64 `json:"cacheCreationTokens"`
	BillablePromptTokens     int64 `json:"billablePromptTokens"`
	PromptTokensIncludeCache *bool `json:"promptTokensIncludeCache"`
}

// ProxyBillingPricing is the ratio section of billing details.
type ProxyBillingPricing struct {
	ModelRatio         float64 `json:"modelRatio"`
	CompletionRatio    float64 `json:"completionRatio"`
	CacheRatio         float64 `json:"cacheRatio"`
	CacheCreationRatio float64 `json:"cacheCreationRatio"`
	GroupRatio         float64 `json:"groupRatio"`
}

// ProxyBillingBreakdown is the money breakdown of billing details.
type ProxyBillingBreakdown struct {
	InputPerMillion         float64 `json:"inputPerMillion"`
	OutputPerMillion        float64 `json:"outputPerMillion"`
	CacheReadPerMillion     float64 `json:"cacheReadPerMillion"`
	CacheCreationPerMillion float64 `json:"cacheCreationPerMillion"`
	InputCost               float64 `json:"inputCost"`
	OutputCost              float64 `json:"outputCost"`
	CacheReadCost           float64 `json:"cacheReadCost"`
	CacheCreationCost       float64 `json:"cacheCreationCost"`
	TotalCost               float64 `json:"totalCost"`
}

// IsClaudeModel reports whether modelName belongs to the Claude / Anthropic
// family. Matching is case-insensitive substring on "claude".
func IsClaudeModel(modelName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "claude")
}

// DefaultCacheRatioForModel returns the intentional cache_ratio fallback when
// an upstream pricing row omits the field. Claude models use Anthropic public
// ratios; everything else keeps the historical MetAPI 1.0 fallback.
func DefaultCacheRatioForModel(modelName string) float64 {
	if IsClaudeModel(modelName) {
		return ClaudeCacheRatio
	}
	return DefaultCacheRatio
}

// DefaultCacheCreationRatioForModel returns the intentional
// cache_creation_ratio fallback when the field is missing.
func DefaultCacheCreationRatioForModel(modelName string) float64 {
	if IsClaudeModel(modelName) {
		return ClaudeCacheCreationRatio
	}
	return DefaultCacheCreationRatio
}

// ResolveCacheRatio picks an explicit ratio when present and finite (>= 0),
// otherwise the model-family default. Explicit 0 is preserved (free cache read).
func ResolveCacheRatio(modelName string, cacheRatio *float64) float64 {
	if cacheRatio != nil && isFiniteNonNeg(*cacheRatio) {
		return *cacheRatio
	}
	return DefaultCacheRatioForModel(modelName)
}

// ResolveCacheCreationRatio picks an explicit creation ratio when present,
// otherwise the model-family default. Explicit 0 is preserved.
func ResolveCacheCreationRatio(modelName string, cacheCreationRatio *float64) float64 {
	if cacheCreationRatio != nil && isFiniteNonNeg(*cacheCreationRatio) {
		return *cacheCreationRatio
	}
	return DefaultCacheCreationRatioForModel(modelName)
}

// NormalizePricingRatio normalizes a raw ratio from an upstream pricing JSON
// field. Missing/NaN/negative values fall back using the model-family default
// selected by kind ("cache" | "cache_creation" | other→fallback).
func NormalizePricingRatio(value any, modelName, kind string, fallback float64) float64 {
	if n, ok := asFiniteNumber(value); ok && n >= 0 {
		return n
	}
	switch kind {
	case "cache":
		return DefaultCacheRatioForModel(modelName)
	case "cache_creation":
		return DefaultCacheCreationRatioForModel(modelName)
	default:
		if isFiniteNonNeg(fallback) {
			return fallback
		}
		return 1
	}
}

// CalculateModelUsageBreakdown computes token-based billing details.
// Returns nil for per-call quotaType (1), matching TS behavior.
func CalculateModelUsageBreakdown(
	model PricingModel,
	usage UsageForCost,
	groupRatio map[string]float64,
) *ProxyBillingDetails {
	if model.QuotaType == 1 {
		return nil
	}

	multiplier := resolveGroupMultiplier(model, groupRatio)
	normalized := normalizeUsageBreakdownInput(usage)

	cacheRatio := ResolveCacheRatio(model.ModelName, model.CacheRatio)
	cacheCreationRatio := ResolveCacheCreationRatio(model.ModelName, model.CacheCreationRatio)

	modelRatio := model.ModelRatio
	if modelRatio <= 0 || !isFiniteFloat(modelRatio) {
		modelRatio = 1
	}
	completionRatio := model.CompletionRatio
	if completionRatio <= 0 || !isFiniteFloat(completionRatio) {
		completionRatio = 1
	}

	inputPerMillion := roundCost(modelRatio * 2 * multiplier)
	outputPerMillion := roundCost(modelRatio * completionRatio * 2 * multiplier)
	cacheReadPerMillion := roundCost(modelRatio * cacheRatio * 2 * multiplier)
	cacheCreationPerMillion := roundCost(modelRatio * cacheCreationRatio * 2 * multiplier)

	inputCost := roundCost((float64(normalized.BillablePromptTokens) / 1_000_000) * inputPerMillion)
	outputCost := roundCost((float64(normalized.CompletionTokens) / 1_000_000) * outputPerMillion)
	cacheReadCost := roundCost((float64(normalized.CacheReadTokens) / 1_000_000) * cacheReadPerMillion)
	cacheCreationCost := roundCost((float64(normalized.CacheCreationTokens) / 1_000_000) * cacheCreationPerMillion)
	totalCost := roundCost(inputCost + outputCost + cacheReadCost + cacheCreationCost)

	return &ProxyBillingDetails{
		QuotaType: model.QuotaType,
		Usage:     normalized,
		Pricing: ProxyBillingPricing{
			ModelRatio:         modelRatio,
			CompletionRatio:    completionRatio,
			CacheRatio:         cacheRatio,
			CacheCreationRatio: cacheCreationRatio,
			GroupRatio:         multiplier,
		},
		Breakdown: ProxyBillingBreakdown{
			InputPerMillion:         inputPerMillion,
			OutputPerMillion:        outputPerMillion,
			CacheReadPerMillion:     cacheReadPerMillion,
			CacheCreationPerMillion: cacheCreationPerMillion,
			InputCost:               inputCost,
			OutputCost:              outputCost,
			CacheReadCost:           cacheReadCost,
			CacheCreationCost:       cacheCreationCost,
			TotalCost:               totalCost,
		},
	}
}

// CalculateModelUsageCost returns the estimated cost for a model + usage.
func CalculateModelUsageCost(
	model PricingModel,
	usage UsageForCost,
	groupRatio map[string]float64,
) float64 {
	multiplier := resolveGroupMultiplier(model, groupRatio)
	if model.QuotaType == 1 {
		return roundCost(calculatePerCallCost(model.ModelPrice, multiplier))
	}
	detail := CalculateModelUsageBreakdown(model, usage, groupRatio)
	if detail == nil {
		return 0
	}
	return detail.Breakdown.TotalCost
}

// EstimateProxyCostFromModel is a pure cost estimate given an already-resolved
// pricing model (catalog row or self-log override). Prefer this over inventing
// prices when the catalog is unavailable — callers should use FallbackTokenCost.
func EstimateProxyCostFromModel(model PricingModel, usage UsageForCost, groupRatio map[string]float64) float64 {
	return CalculateModelUsageCost(model, usage, groupRatio)
}

// BuildPricingOverrideModel builds a token-quota PricingModel from self-log
// billing metadata. Missing Claude cache ratios receive Anthropic defaults.
func BuildPricingOverrideModel(modelName string, override ProxyBillingPricingOverride) (PricingModel, map[string]float64) {
	group := 1.0
	if override.GroupRatio != nil && isFiniteNonNeg(*override.GroupRatio) && *override.GroupRatio > 0 {
		group = *override.GroupRatio
	}

	modelRatio := override.ModelRatio
	if modelRatio <= 0 || !isFiniteFloat(modelRatio) {
		modelRatio = 1
	}
	completionRatio := override.CompletionRatio
	if completionRatio <= 0 || !isFiniteFloat(completionRatio) {
		completionRatio = 1
	}

	// Materialize resolved ratios so billing_details always shows the applied
	// values (including Claude-aware fallbacks when the override omitted them).
	cacheRatio := ResolveCacheRatio(modelName, override.CacheRatio)
	cacheCreationRatio := ResolveCacheCreationRatio(modelName, override.CacheCreationRatio)

	return PricingModel{
		ModelName:          modelName,
		QuotaType:          0,
		ModelRatio:         modelRatio,
		CompletionRatio:    completionRatio,
		CacheRatio:         &cacheRatio,
		CacheCreationRatio: &cacheCreationRatio,
		EnableGroups:       []string{"default"},
	}, map[string]float64{"default": group}
}

// FallbackTokenCost is the last-resort estimate when no pricing catalog is
// available. Mirrors TS fallbackTokenCost (veloera uses 1e6 divisor, others 5e5).
func FallbackTokenCost(totalTokens int64, platform string) float64 {
	divisor := 500_000.0
	if strings.EqualFold(strings.TrimSpace(platform), "veloera") {
		divisor = 1_000_000.0
	}
	tokens := float64(toPositiveInt64(totalTokens))
	return roundCost(tokens / divisor)
}

// CacheAwarePerMillionRates derives input/output/cache $/M rates for catalog
// display and routing reference helpers. Uses Claude-aware cache defaults.
func CacheAwarePerMillionRates(model PricingModel, groupMultiplier float64) (input, output, cacheRead, cacheCreation float64) {
	if groupMultiplier <= 0 || !isFiniteFloat(groupMultiplier) {
		groupMultiplier = 1
	}
	modelRatio := model.ModelRatio
	if modelRatio <= 0 || !isFiniteFloat(modelRatio) {
		modelRatio = 1
	}
	completionRatio := model.CompletionRatio
	if completionRatio <= 0 || !isFiniteFloat(completionRatio) {
		completionRatio = 1
	}
	cacheRatio := ResolveCacheRatio(model.ModelName, model.CacheRatio)
	cacheCreationRatio := ResolveCacheCreationRatio(model.ModelName, model.CacheCreationRatio)

	input = roundCost(modelRatio * 2 * groupMultiplier)
	output = roundCost(modelRatio * completionRatio * 2 * groupMultiplier)
	cacheRead = roundCost(modelRatio * cacheRatio * 2 * groupMultiplier)
	cacheCreation = roundCost(modelRatio * cacheCreationRatio * 2 * groupMultiplier)
	return
}

func normalizeUsageBreakdownInput(usage UsageForCost) ProxyBillingUsage {
	promptTokens := toPositiveInt64(usage.PromptTokens)
	completionTokens := toPositiveInt64(usage.CompletionTokens)
	totalTokensRaw := toPositiveInt64(usage.TotalTokens)
	totalTokens := totalTokensRaw
	if promptTokens+completionTokens > totalTokens {
		totalTokens = promptTokens + completionTokens
	}
	cacheReadTokens := toPositiveInt64(usage.CacheReadTokens)
	cacheCreationTokens := toPositiveInt64(usage.CacheCreationTokens)
	promptTokensIncludeCache := usage.PromptTokensIncludeCache

	hasSplit := promptTokens > 0 || completionTokens > 0
	effectivePromptTokens := promptTokens
	if !hasSplit {
		effectivePromptTokens = totalTokens
	}

	var billablePromptTokens int64
	if promptTokensIncludeCache != nil && !*promptTokensIncludeCache {
		billablePromptTokens = effectivePromptTokens
	} else {
		billable := effectivePromptTokens - cacheReadTokens - cacheCreationTokens
		if billable < 0 {
			billable = 0
		}
		billablePromptTokens = billable
	}

	return ProxyBillingUsage{
		PromptTokens:             promptTokens,
		CompletionTokens:         completionTokens,
		TotalTokens:              totalTokens,
		CacheReadTokens:          cacheReadTokens,
		CacheCreationTokens:      cacheCreationTokens,
		BillablePromptTokens:     billablePromptTokens,
		PromptTokensIncludeCache: promptTokensIncludeCache,
	}
}

func resolveGroupMultiplier(model PricingModel, groupRatio map[string]float64) float64 {
	if groupRatio == nil {
		groupRatio = map[string]float64{}
	}
	groups := model.EnableGroups
	if len(groups) == 0 {
		groups = []string{"default"}
	}

	if containsString(groups, "default") {
		if r, ok := groupRatio["default"]; ok && r > 0 && isFiniteFloat(r) {
			return r
		}
	}
	for _, g := range groups {
		if r, ok := groupRatio[g]; ok && r > 0 && isFiniteFloat(r) {
			return r
		}
	}
	for _, r := range groupRatio {
		if r > 0 && isFiniteFloat(r) {
			return r
		}
	}
	return 1
}

func calculatePerCallCost(price *ModelPrice, multiplier float64) float64 {
	if price == nil {
		return 0
	}
	if price.Flat != nil {
		return *price.Flat * multiplier
	}
	if price.IsPair {
		// done-hub/one-hub times pricing follows input ratio only.
		return price.Input * multiplier * oneHubPerCallRatio
	}
	return 0
}

func roundCost(value float64) float64 {
	if value < 0 || !isFiniteFloat(value) {
		return 0
	}
	return math.Round(value*1_000_000) / 1_000_000
}

func toPositiveInt64(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func isFiniteNonNeg(v float64) bool {
	return isFiniteFloat(v) && v >= 0
}

func asFiniteNumber(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		if isFiniteFloat(n) {
			return n, true
		}
	case float32:
		f := float64(n)
		if isFiniteFloat(f) {
			return f, true
		}
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case jsonNumber:
		f, err := n.Float64()
		if err == nil && isFiniteFloat(f) {
			return f, true
		}
	}
	return 0, false
}

// jsonNumber is satisfied by encoding/json.Number without importing encoding/json
// into every call site of asFiniteNumber.
type jsonNumber interface {
	Float64() (float64, error)
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
