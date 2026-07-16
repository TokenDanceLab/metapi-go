package routing

import (
	"math"
	"testing"
)

func TestBuildPriceCompareRow_BillingRatesPreferred(t *testing.T) {
	inRate := 3.0
	outRate := 15.0
	in := PriceCompareInput{
		SiteID:           1,
		SiteName:         "Alpha",
		Platform:         "newapi",
		Model:            "gpt-4o",
		AccountID:        10,
		Username:         "ops",
		InputPerMillion:  &inRate,
		OutputPerMillion: &outRate,
	}
	// Also supply observed/configured so we prove billing wins.
	obs := 9.9
	cfg := 8.8
	in.ObservedAvgCost = &obs
	in.ConfiguredUnitCost = &cfg

	row := BuildPriceCompareRow(in, DefaultPriceCompareSampleUsage)
	if row.Source != PriceSourceBilling {
		t.Fatalf("source=%q want %q", row.Source, PriceSourceBilling)
	}
	if row.RatesSource != PriceSourceBilling {
		t.Fatalf("ratesSource=%q want %q", row.RatesSource, PriceSourceBilling)
	}
	if row.MissingPrice {
		t.Fatal("billing rates should not be missingPrice")
	}
	// 1000/1e6 * 3 + 1000/1e6 * 15 = 0.018
	if math.Abs(row.EstimatedCostSample-0.018) > 1e-9 {
		t.Fatalf("estimatedCostSample=%v want 0.018", row.EstimatedCostSample)
	}
	if row.InputPerMillion != 3 || row.OutputPerMillion != 15 {
		t.Fatalf("rates = %v/%v", row.InputPerMillion, row.OutputPerMillion)
	}
}

func TestBuildPriceCompareRow_ObservedBeforeConfiguredAndFallback(t *testing.T) {
	obs := 0.42
	cfg := 0.11
	in := PriceCompareInput{
		SiteID:             2,
		SiteName:           "Beta",
		Platform:           "openai",
		Model:              "claude-sonnet-4",
		AccountID:          20,
		ObservedAvgCost:    &obs,
		ObservedSamples:    7,
		ConfiguredUnitCost: &cfg,
	}
	row := BuildPriceCompareRow(in, DefaultPriceCompareSampleUsage)
	if row.Source != PriceSourceObserved {
		t.Fatalf("source=%q want observed", row.Source)
	}
	if row.EstimatedCostSample != 0.42 {
		t.Fatalf("sample=%v want 0.42", row.EstimatedCostSample)
	}
	if row.ObservedSamples != 7 {
		t.Fatalf("samples=%d", row.ObservedSamples)
	}
	if row.MissingPrice {
		t.Fatal("observed should not be missing")
	}
	// Rates still fall back to default model_ratio=1 display rates.
	if row.RatesSource != PriceSourceFallback {
		t.Fatalf("ratesSource=%q want fallback", row.RatesSource)
	}
}

func TestBuildPriceCompareRow_ConfiguredThenFallback(t *testing.T) {
	cfg := 0.25
	in := PriceCompareInput{
		SiteID:             3,
		SiteName:           "Gamma",
		Platform:           "openai",
		Model:              "gpt-4o-mini",
		AccountID:          30,
		ConfiguredUnitCost: &cfg,
	}
	row := BuildPriceCompareRow(in, DefaultPriceCompareSampleUsage)
	if row.Source != PriceSourceConfigured {
		t.Fatalf("source=%q want configured", row.Source)
	}
	if row.EstimatedCostSample != 0.25 {
		t.Fatalf("sample=%v", row.EstimatedCostSample)
	}
	if row.MissingPrice {
		t.Fatal("configured should not be missing")
	}

	// No signals → labeled fallback + missingPrice=true.
	empty := PriceCompareInput{
		SiteID:   4,
		SiteName: "Delta",
		Platform: "openai",
		Model:    "gpt-4o-mini",
	}
	fb := BuildPriceCompareRow(empty, DefaultPriceCompareSampleUsage)
	if fb.Source != PriceSourceFallback {
		t.Fatalf("source=%q want fallback", fb.Source)
	}
	if !fb.MissingPrice {
		t.Fatal("fallback must set missingPrice so UI can flag empty catalog data")
	}
	if fb.EstimatedCostSample <= 0 {
		t.Fatalf("fallback sample should still be positive estimate, got %v", fb.EstimatedCostSample)
	}
	if fb.RatesSource != PriceSourceFallback {
		t.Fatalf("ratesSource=%q", fb.RatesSource)
	}
}

func TestBuildPriceCompareRow_BillingRatiosDeriveRates(t *testing.T) {
	mr := 2.5
	cr := 4.0
	in := PriceCompareInput{
		SiteID:          5,
		SiteName:        "RatioSite",
		Platform:        "newapi",
		Model:           "gpt-x",
		AccountID:       50,
		ModelRatio:      &mr,
		CompletionRatio: &cr,
	}
	row := BuildPriceCompareRow(in, DefaultPriceCompareSampleUsage)
	if row.Source != PriceSourceBilling || row.RatesSource != PriceSourceBilling {
		t.Fatalf("source=%q ratesSource=%q", row.Source, row.RatesSource)
	}
	// input = 2.5*2=5; output=2.5*4*2=20
	if row.InputPerMillion != 5 || row.OutputPerMillion != 20 {
		t.Fatalf("rates=%v/%v want 5/20", row.InputPerMillion, row.OutputPerMillion)
	}
	// sample 1k/1k → 0.005 + 0.02 = 0.025
	if math.Abs(row.EstimatedCostSample-0.025) > 1e-9 {
		t.Fatalf("sample=%v want 0.025", row.EstimatedCostSample)
	}
}

func TestAggregatePriceCompareRows_SortCheapestFirstMissingLast(t *testing.T) {
	rows := []PriceCompareRow{
		{SiteName: "B", EstimatedCostSample: 0.5, MissingPrice: false, AccountID: 2},
		{SiteName: "A", EstimatedCostSample: 0.1, MissingPrice: true, AccountID: 1},
		{SiteName: "C", EstimatedCostSample: 0.2, MissingPrice: false, AccountID: 3},
		{SiteName: "A", EstimatedCostSample: 0.2, MissingPrice: false, AccountID: 4},
	}
	got := AggregatePriceCompareRows(rows)
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	// Priced first by cost: A/0.2, C/0.2, B/0.5, then missing A/0.1
	if got[0].SiteName != "A" || got[0].AccountID != 4 {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].SiteName != "C" {
		t.Fatalf("second=%+v", got[1])
	}
	if got[2].SiteName != "B" {
		t.Fatalf("third=%+v", got[2])
	}
	if !got[3].MissingPrice || got[3].SiteName != "A" {
		t.Fatalf("last should be missing A, got %+v", got[3])
	}
}
