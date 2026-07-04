package checkin

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// ParseCheckinRewardAmount parses a checkin reward value into a numeric amount.
// Returns 0 if the value is not a valid positive number.
// Mirrors TS parseCheckinRewardAmount().
func ParseCheckinRewardAmount(value any) float64 {
	// Try direct number
	if num := toFiniteNumber(value); num != nil {
		if *num > 0 {
			return *num
		}
		return 0
	}

	// Try string parsing
	if str, ok := value.(string); ok {
		text := strings.TrimSpace(str)
		if text == "" {
			return 0
		}
		// Remove commas
		normalized := strings.ReplaceAll(text, ",", "")
		// Extract first number
		re := regexp.MustCompile(`[-+]?\d+(?:\.\d+)?`)
		match := re.FindString(normalized)
		if match == "" {
			return 0
		}
		parsed, err := parseFloat(match)
		if err != nil || parsed <= 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0
		}
		return parsed
	}

	return 0
}

// InferRewardFromBalanceDelta infers a reward from balance change.
// Mirrors TS inferRewardFromBalanceDelta().
func InferRewardFromBalanceDelta(previousBalance, latestBalance float64) float64 {
	delta := latestBalance - previousBalance
	if delta <= 0 || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return 0
	}
	return math.Round(delta*1_000_000) / 1_000_000
}

// Round6 rounds a value to 6 decimal places.
func Round6(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}

// toFiniteNumber converts an any to a finite number, or returns nil.
func toFiniteNumber(value any) *float64 {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return &v
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	default:
		return nil
	}
}

// parseFloat parses a string as float64.
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}
