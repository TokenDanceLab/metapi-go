package service

import (
	"encoding/json"
	"math"
	"time"
)

// TodayIncomeSnapshot tracks daily income baseline and latest.
type TodayIncomeSnapshot struct {
	Day       string  `json:"day"`
	Baseline  float64 `json:"baseline"`
	Latest    float64 `json:"latest"`
	UpdatedAt string  `json:"updatedAt"`
}

// GetTodayIncomeDelta returns the income delta for the current day.
// Mirrors TS getTodayIncomeDelta().
func GetTodayIncomeDelta(extraConfig *string, day string) float64 {
	if day == "" {
		return 0
	}
	snapshot := extractTodayIncomeSnapshot(extraConfig)
	if snapshot == nil || snapshot.Day != day {
		return 0
	}
	baseline := snapshot.Baseline
	delta := snapshot.Latest - baseline
	if math.IsNaN(delta) || math.IsInf(delta, 0) || delta <= 0 {
		return 0
	}
	return delta
}

// GetTodayIncomeValue returns the latest income value for the given day.
// Mirrors TS getTodayIncomeValue().
func GetTodayIncomeValue(extraConfig *string, day string) float64 {
	if day == "" {
		return 0
	}
	snapshot := extractTodayIncomeSnapshot(extraConfig)
	if snapshot == nil || snapshot.Day != day {
		return 0
	}
	return snapshot.Latest
}

// UpdateTodayIncomeSnapshot updates the todayIncome snapshot in extraConfig.
// Mirrors TS updateTodayIncomeSnapshot().
func UpdateTodayIncomeSnapshot(extraConfig *string, todayIncome float64, now time.Time) *string {
	if math.IsNaN(todayIncome) || math.IsInf(todayIncome, 0) || todayIncome <= 0 {
		// Keep existing extraConfig unchanged for non-positive income
		if extraConfig == nil {
			return nil
		}
		return extraConfig
	}

	day := FormatLocalDate(now)
	existing := extractTodayIncomeSnapshot(extraConfig)

	baseline := todayIncome
	latest := todayIncome

	if existing != nil && existing.Day == day {
		if existing.Baseline != 0 && todayIncome < existing.Baseline {
			baseline = todayIncome
		} else if existing.Baseline != 0 {
			baseline = existing.Baseline
		}
		latest = todayIncome
	}

	return MergeExtraConfig(extraConfig, map[string]any{
		"todayIncomeSnapshot": map[string]any{
			"day":       day,
			"baseline":  baseline,
			"latest":    latest,
			"updatedAt": now.UTC().Format(time.RFC3339),
		},
	})
}

// EstimateRewardInput is the input for EstimateRewardWithTodayIncomeFallback.
type EstimateRewardInput struct {
	Day               string
	SuccessCount      int
	ParsedRewardCount int
	RewardSum         float64
	ExtraConfig       *string
}

// EstimateRewardWithTodayIncomeFallback estimates reward using todayIncome fallback.
// Mirrors TS estimateRewardWithTodayIncomeFallback().
func EstimateRewardWithTodayIncomeFallback(input EstimateRewardInput) float64 {
	rewardSum := input.RewardSum
	if !math.IsNaN(rewardSum) && !math.IsInf(rewardSum, 0) && rewardSum > 0 {
		// keep
	} else {
		rewardSum = 0
	}

	if input.SuccessCount <= 0 {
		return rewardSum
	}

	hasMissingReward := input.ParsedRewardCount < input.SuccessCount
	if !hasMissingReward && rewardSum > 0 {
		return rewardSum
	}

	incomeValue := GetTodayIncomeValue(input.ExtraConfig, input.Day)
	if incomeValue > rewardSum {
		return incomeValue
	}
	return rewardSum
}

// extractTodayIncomeSnapshot extracts the snapshot from extraConfig.
func extractTodayIncomeSnapshot(extraConfig *string) *TodayIncomeSnapshot {
	cfg := ParseExtraConfig(extraConfig)
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["todayIncomeSnapshot"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snapshot TodayIncomeSnapshot
	if err := json.Unmarshal(b, &snapshot); err != nil {
		return nil
	}
	if snapshot.Day == "" {
		return nil
	}
	return &snapshot
}
