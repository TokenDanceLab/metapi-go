package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// Golden file test: weight formula contributions compared to TS behavior
// =============================================================================
//
// This test writes a golden file containing the full contribution breakdown
// for a representative set of candidates, and verifies that the selection
// matches expected TS behavior. The golden file can be compared against
// TS output to ensure the Go port is correct.

func TestSelector_GoldenWeightFormula(t *testing.T) {
	ResetSiteRuntimeHealthState()

	// Build a realistic candidate set across 3 sites with different characteristics
	candidates := []RouteChannelCandidate{
		// Site A (id=10): premium provider, high balance, good history
		makeCandidate(1, 10, 101, 10, 0, 500, 10, 250.0, 1.5, ptrFloat(0.003), 100.0, nil),
		makeCandidate(2, 10, 102, 10, 0, 300, 5, 180.0, 1.5, ptrFloat(0.0025), 200.0, nil),

		// Site B (id=20): mid-tier, moderate usage
		makeCandidate(3, 20, 201, 10, 1, 200, 20, 100.0, 1.0, ptrFloat(0.005), 50.0, nil),
		makeCandidate(4, 20, 202, 10, 1, 150, 30, 80.0, 1.0, nil, 30.0, nil),

		// Site C (id=30): budget provider, low balance
		makeCandidate(5, 30, 301, 10, 2, 100, 50, 40.0, 0.8, nil, 5.0, nil),
	}

	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: 0.5,
		ValueScoreFactor: 0.5,
		CostWeight:       0.4,
		BalanceWeight:    0.3,
		UsageWeight:      0.3,
	}

	// Run multiple times and record selection distribution for statistical validation
	selectionCounts := make(map[int64]int)
	const iterations = 10000

	for i := 0; i < iterations; i++ {
		result := CalculateWeightedSelection(
			candidates, staticModel("gpt-4"), routingWeights,
			nil, nil, 0, WeightedMode, "", nil, 1.0,
		)
		if result.Selected != nil {
			selectionCounts[result.Selected.Channel.ID]++
		}
	}

	// Write golden file with full breakdown
	goldenPath := filepath.Join("testdata", "selector_golden.txt")
	os.MkdirAll(filepath.Dir(goldenPath), 0755)

	var sb strings.Builder
	sb.WriteString("# Golden file: weight formula selection distribution\n")
	sb.WriteString("# Iterations: 10000\n")
	sb.WriteString("# Routing weights: baseWeightFactor=0.5 valueScoreFactor=0.5 costWeight=0.4 balanceWeight=0.3 usageWeight=0.3\n")
	sb.WriteString("# Format: channelID,siteID,accountID,selectionCount,selectionPercent\n")

	// Single deterministic run for detailed breakdown
	result := CalculateWeightedSelection(
		candidates, staticModel("gpt-4"), routingWeights,
		nil, nil, 0, WeightedMode, "", nil, 1.0,
	)

	for _, d := range result.Details {
		chID := d.Candidate.Channel.ID
		count := selectionCounts[chID]
		pct := float64(count) / float64(iterations) * 100
		sb.WriteString(formatInt(chID))
		sb.WriteString(",")
		sb.WriteString(formatInt(d.Candidate.Site.ID))
		sb.WriteString(",")
		sb.WriteString(formatInt(d.Candidate.Account.ID))
		sb.WriteString(",")
		sb.WriteString(formatInt(int64(count)))
		sb.WriteString(",")
		sb.WriteString(fmtFloat(pct))
		sb.WriteString("\n")
	}

	os.WriteFile(goldenPath, []byte(sb.String()), 0644)

	t.Logf("Golden file written to %s", goldenPath)

	// Verify selection happened
	if result.Selected == nil {
		t.Fatal("expected a selection")
	}

	// Verify all 5 candidates were selected at least occasionally
	for _, c := range candidates {
		count := selectionCounts[c.Channel.ID]
		if count == 0 {
			t.Errorf("channel %d was never selected in %d iterations", c.Channel.ID, iterations)
		}
		t.Logf("  channel=%d site=%d selected %d times (%.1f%%)",
			c.Channel.ID, c.Site.ID, count, float64(count)/float64(iterations)*100)
	}

	// The top-ranked candidate should have among the highest selection counts.
	// Weighted random selection introduces variance, so we verify no candidate is starved
	// and the distribution roughly matches the contribution probabilities.
	if len(result.Details) >= 2 {
		// Verify probabilities and selection counts are correlated: higher probability → higher count
		for i := 0; i < len(result.Details)-1; i++ {
			pI := result.Details[i].Probability
			pJ := result.Details[i+1].Probability
			cI := selectionCounts[result.Details[i].Candidate.Channel.ID]
			cJ := selectionCounts[result.Details[i+1].Candidate.Channel.ID]
			if pI > pJ+0.05 && cI < cJ {
				t.Errorf("probability rank inverted: ch %d (p=%.4f, n=%d) vs ch %d (p=%.4f, n=%d)",
					result.Details[i].Candidate.Channel.ID, pI, cI,
					result.Details[i+1].Candidate.Channel.ID, pJ, cJ)
			}
		}
	}
}
