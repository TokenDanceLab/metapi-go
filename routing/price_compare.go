package routing

import (
	"strings"
)

// Price comparison provenance labels. Callers must not invent unlabeled prices.
const (
	// PriceSourceObserved uses successful proxy_logs estimated_cost averages.
	PriceSourceObserved = "observed"
	// PriceSourceConfigured uses accounts.unit_cost set by operators.
	PriceSourceConfigured = "configured"
	// PriceSourceBilling uses rates/ratios recovered from proxy_logs.billing_details.
	PriceSourceBilling = "billing_details"
	// PriceSourceFallback uses platform FallbackTokenCost / default model ratios.
	// Always label invented defaults with this source (or ratesSource).
	PriceSourceFallback = "fallback"
)

// DefaultPriceCompareSampleUsage is the sample window used for estimatedCostSample
// when the operator does not supply custom usage (1k in + 1k out).
var DefaultPriceCompareSampleUsage = UsageForCost{
	PromptTokens:     1000,
	CompletionTokens: 1000,
	TotalTokens:      2000,
}

// PriceCompareInput is one site/account/model candidate for aggregation.
// Pointers distinguish missing values from explicit zeros.
type PriceCompareInput struct {
	SiteID    int64
	SiteName  string
	Platform  string
	Model     string
	AccountID int64
	Username  string

	// ObservedAvgCost is AVG(estimated_cost) over successful proxy logs.
	ObservedAvgCost *float64
	ObservedSamples int

	// ConfiguredUnitCost is accounts.unit_cost when set by the operator.
	ConfiguredUnitCost *float64

	// Optional rates recovered from billing_details breakdown.
	InputPerMillion  *float64
	OutputPerMillion *float64

	// Optional ratios recovered from billing_details pricing section.
	ModelRatio      *float64
	CompletionRatio *float64
	GroupRatio      *float64
}

// PriceCompareRow is one cross-site effective price candidate for admin UX.
type PriceCompareRow struct {
	SiteID               int64   `json:"siteId"`
	SiteName             string  `json:"siteName"`
	Platform             string  `json:"platform"`
	Model                string  `json:"model"`
	AccountID            int64   `json:"accountId"`
	Username             string  `json:"username,omitempty"`
	InputPerMillion      float64 `json:"inputPerMillion"`
	OutputPerMillion     float64 `json:"outputPerMillion"`
	Source               string  `json:"source"`
	RatesSource          string  `json:"ratesSource"`
	EstimatedCostSample  float64 `json:"estimatedCostSample"`
	ObservedSamples      int     `json:"observedSamples,omitempty"`
	ConfiguredUnitCost   *float64 `json:"configuredUnitCost,omitempty"`
	MissingPrice         bool    `json:"missingPrice"`
}

// BuildPriceCompareRow resolves effective rates + sample cost without inventing
// unlabeled prices. Provenance priority for estimatedCostSample:
//
//  1. billing_details rates/ratios → source=billing_details
//  2. observed avg estimated_cost → source=observed
//  3. configured account unit_cost → source=configured
//  4. platform FallbackTokenCost + default ratios → source=fallback
//
// Per-million rates prefer explicit billing rates, then derived ratios, then
// CacheAwarePerMillionRates defaults (ratesSource=fallback).
func BuildPriceCompareRow(in PriceCompareInput, sample UsageForCost) PriceCompareRow {
	model := strings.TrimSpace(in.Model)
	platform := strings.TrimSpace(in.Platform)
	if sample.TotalTokens <= 0 && sample.PromptTokens <= 0 && sample.CompletionTokens <= 0 {
		sample = DefaultPriceCompareSampleUsage
	}

	row := PriceCompareRow{
		SiteID:             in.SiteID,
		SiteName:           strings.TrimSpace(in.SiteName),
		Platform:           platform,
		Model:              model,
		AccountID:          in.AccountID,
		Username:           strings.TrimSpace(in.Username),
		ObservedSamples:    maxIntNonNeg(in.ObservedSamples),
		ConfiguredUnitCost: clonePositiveFloat(in.ConfiguredUnitCost),
		Source:             PriceSourceFallback,
		RatesSource:        PriceSourceFallback,
		MissingPrice:       true,
	}

	inputPerM, outputPerM, ratesSource := resolvePriceCompareRates(in, model)
	row.InputPerMillion = inputPerM
	row.OutputPerMillion = outputPerM
	row.RatesSource = ratesSource

	// Prefer billing-derived sample cost when rates/ratios exist.
	if ratesSource == PriceSourceBilling {
		pm := PricingModel{
			ModelName:       model,
			QuotaType:       0,
			ModelRatio:      1,
			CompletionRatio: 1,
		}
		if in.ModelRatio != nil && isFiniteFloat(*in.ModelRatio) && *in.ModelRatio > 0 {
			pm.ModelRatio = *in.ModelRatio
		}
		if in.CompletionRatio != nil && isFiniteFloat(*in.CompletionRatio) && *in.CompletionRatio > 0 {
			pm.CompletionRatio = *in.CompletionRatio
		}
		group := map[string]float64{"default": 1}
		if in.GroupRatio != nil && isFiniteFloat(*in.GroupRatio) && *in.GroupRatio > 0 {
			group["default"] = *in.GroupRatio
		}
		// When only absolute $/M rates are known, rebuild cost from rates directly.
		if in.InputPerMillion != nil || in.OutputPerMillion != nil {
			row.EstimatedCostSample = estimateCostFromPerMillion(inputPerM, outputPerM, sample)
		} else {
			row.EstimatedCostSample = CalculateModelUsageCost(pm, sample, group)
		}
		row.Source = PriceSourceBilling
		row.MissingPrice = false
		return row
	}

	if in.ObservedAvgCost != nil && isFiniteFloat(*in.ObservedAvgCost) && *in.ObservedAvgCost > 0 {
		row.EstimatedCostSample = roundCost(*in.ObservedAvgCost)
		row.Source = PriceSourceObserved
		row.MissingPrice = false
		return row
	}

	if in.ConfiguredUnitCost != nil && isFiniteFloat(*in.ConfiguredUnitCost) && *in.ConfiguredUnitCost > 0 {
		row.EstimatedCostSample = roundCost(*in.ConfiguredUnitCost)
		row.Source = PriceSourceConfigured
		row.MissingPrice = false
		return row
	}

	// Last resort: platform token divisor + default ratio rates (already filled).
	total := sample.TotalTokens
	if total <= 0 {
		total = sample.PromptTokens + sample.CompletionTokens
	}
	fallback := FallbackTokenCost(total, platform)
	// Prefer ratio-based sample when defaults produce a positive cost.
	pm := PricingModel{ModelName: model, QuotaType: 0, ModelRatio: 1, CompletionRatio: 1}
	ratioCost := CalculateModelUsageCost(pm, sample, map[string]float64{"default": 1})
	if ratioCost > 0 {
		row.EstimatedCostSample = ratioCost
	} else {
		row.EstimatedCostSample = fallback
	}
	row.Source = PriceSourceFallback
	// Fallback is always "known" as an estimate, but operators must treat it as
	// non-catalog. missingPrice stays true so UI can mark empty/missing states.
	row.MissingPrice = true
	return row
}

// AggregatePriceCompareRows sorts cheaper first, then by site name / account id.
// Rows with missingPrice sort after priced rows with the same cost.
func AggregatePriceCompareRows(rows []PriceCompareRow) []PriceCompareRow {
	if len(rows) == 0 {
		return []PriceCompareRow{}
	}
	out := make([]PriceCompareRow, len(rows))
	copy(out, rows)
	// Stable hand-rolled sort keeps the routing package free of sort.Slice
	// allocation patterns in tiny unit tests; for N site rows this is fine.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if priceCompareLess(out[j], out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func priceCompareLess(a, b PriceCompareRow) bool {
	if a.MissingPrice != b.MissingPrice {
		return !a.MissingPrice && b.MissingPrice
	}
	if a.EstimatedCostSample != b.EstimatedCostSample {
		return a.EstimatedCostSample < b.EstimatedCostSample
	}
	if a.SiteName != b.SiteName {
		return strings.ToLower(a.SiteName) < strings.ToLower(b.SiteName)
	}
	if a.AccountID != b.AccountID {
		return a.AccountID < b.AccountID
	}
	return a.Model < b.Model
}

func resolvePriceCompareRates(in PriceCompareInput, model string) (inputPerM, outputPerM float64, ratesSource string) {
	if in.InputPerMillion != nil && in.OutputPerMillion != nil &&
		isFiniteFloat(*in.InputPerMillion) && isFiniteFloat(*in.OutputPerMillion) &&
		*in.InputPerMillion >= 0 && *in.OutputPerMillion >= 0 {
		return roundCost(*in.InputPerMillion), roundCost(*in.OutputPerMillion), PriceSourceBilling
	}

	if (in.ModelRatio != nil && isFiniteFloat(*in.ModelRatio) && *in.ModelRatio > 0) ||
		(in.CompletionRatio != nil && isFiniteFloat(*in.CompletionRatio) && *in.CompletionRatio > 0) {
		pm := PricingModel{
			ModelName:       model,
			QuotaType:       0,
			ModelRatio:      1,
			CompletionRatio: 1,
		}
		if in.ModelRatio != nil && isFiniteFloat(*in.ModelRatio) && *in.ModelRatio > 0 {
			pm.ModelRatio = *in.ModelRatio
		}
		if in.CompletionRatio != nil && isFiniteFloat(*in.CompletionRatio) && *in.CompletionRatio > 0 {
			pm.CompletionRatio = *in.CompletionRatio
		}
		groupMul := 1.0
		if in.GroupRatio != nil && isFiniteFloat(*in.GroupRatio) && *in.GroupRatio > 0 {
			groupMul = *in.GroupRatio
		}
		inM, outM, _, _ := CacheAwarePerMillionRates(pm, groupMul)
		return inM, outM, PriceSourceBilling
	}

	// Explicit single-sided rates still count as billing when one side is known.
	if in.InputPerMillion != nil && isFiniteFloat(*in.InputPerMillion) && *in.InputPerMillion >= 0 {
		out := 0.0
		if in.OutputPerMillion != nil && isFiniteFloat(*in.OutputPerMillion) && *in.OutputPerMillion >= 0 {
			out = *in.OutputPerMillion
		}
		return roundCost(*in.InputPerMillion), roundCost(out), PriceSourceBilling
	}
	if in.OutputPerMillion != nil && isFiniteFloat(*in.OutputPerMillion) && *in.OutputPerMillion >= 0 {
		return 0, roundCost(*in.OutputPerMillion), PriceSourceBilling
	}

	pm := PricingModel{ModelName: model, QuotaType: 0, ModelRatio: 1, CompletionRatio: 1}
	inM, outM, _, _ := CacheAwarePerMillionRates(pm, 1)
	return inM, outM, PriceSourceFallback
}

func estimateCostFromPerMillion(inputPerM, outputPerM float64, sample UsageForCost) float64 {
	prompt := float64(toPositiveInt64(sample.PromptTokens))
	completion := float64(toPositiveInt64(sample.CompletionTokens))
	if prompt == 0 && completion == 0 {
		total := float64(toPositiveInt64(sample.TotalTokens))
		// Split unknown totals evenly so sample remains non-zero.
		prompt = total / 2
		completion = total / 2
	}
	cost := (prompt/1_000_000)*inputPerM + (completion/1_000_000)*outputPerM
	return roundCost(cost)
}

func clonePositiveFloat(v *float64) *float64 {
	if v == nil || !isFiniteFloat(*v) || *v <= 0 {
		return nil
	}
	x := *v
	return &x
}

func maxIntNonNeg(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
