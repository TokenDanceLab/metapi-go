package service

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// ---- TodayIncomeSnapshot Tests ----

func makeExtraConfig(data map[string]any) *string {
	if data == nil {
		return nil
	}
	b, _ := json.Marshal(data)
	s := string(b)
	return &s
}

func snapshotConfig(day string, baseline, latest float64) *string {
	return makeExtraConfig(map[string]any{
		"todayIncomeSnapshot": map[string]any{
			"day":       day,
			"baseline":  baseline,
			"latest":    latest,
			"updatedAt": "2026-07-04T00:00:00Z",
		},
	})
}

// ---- GetTodayIncomeDelta Tests ----

func TestGetTodayIncomeDelta_Positive(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	delta := GetTodayIncomeDelta(cfg, "2026-07-04")
	if delta != 50 {
		t.Errorf("expected delta 50 (150-100), got %v", delta)
	}
}

func TestGetTodayIncomeDelta_DifferentDay(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	delta := GetTodayIncomeDelta(cfg, "2026-07-03")
	if delta != 0 {
		t.Errorf("expected 0 for different day, got %v", delta)
	}
}

func TestGetTodayIncomeDelta_NilConfig(t *testing.T) {
	delta := GetTodayIncomeDelta(nil, "2026-07-04")
	if delta != 0 {
		t.Errorf("expected 0 for nil config, got %v", delta)
	}
}

func TestGetTodayIncomeDelta_EmptyDay(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	delta := GetTodayIncomeDelta(cfg, "")
	if delta != 0 {
		t.Errorf("expected 0 for empty day, got %v", delta)
	}
}

func TestGetTodayIncomeDelta_NegativeDelta(t *testing.T) {
	// baseline > latest → delta negative → returns 0
	cfg := snapshotConfig("2026-07-04", 200, 150)
	delta := GetTodayIncomeDelta(cfg, "2026-07-04")
	if delta != 0 {
		t.Errorf("expected 0 for negative delta, got %v", delta)
	}
}

func TestGetTodayIncomeDelta_ZeroDelta(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 100)
	delta := GetTodayIncomeDelta(cfg, "2026-07-04")
	if delta != 0 {
		t.Errorf("expected 0 for zero delta, got %v", delta)
	}
}

func TestGetTodayIncomeDelta_NoSnapshotInConfig(t *testing.T) {
	// Config without todayIncomeSnapshot should return 0
	cfg := makeExtraConfig(map[string]any{
		"otherField": "value",
	})
	delta := GetTodayIncomeDelta(cfg, "2026-07-04")
	if delta != 0 {
		t.Errorf("expected 0 when no snapshot, got %v", delta)
	}
}

// ---- GetTodayIncomeValue Tests ----

func TestGetTodayIncomeValue_Normal(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	val := GetTodayIncomeValue(cfg, "2026-07-04")
	if val != 150 {
		t.Errorf("expected 150 (latest), got %v", val)
	}
}

func TestGetTodayIncomeValue_DifferentDay(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	val := GetTodayIncomeValue(cfg, "2026-07-03")
	if val != 0 {
		t.Errorf("expected 0 for different day, got %v", val)
	}
}

func TestGetTodayIncomeValue_NilConfig(t *testing.T) {
	val := GetTodayIncomeValue(nil, "2026-07-04")
	if val != 0 {
		t.Errorf("expected 0 for nil config, got %v", val)
	}
}

func TestGetTodayIncomeValue_EmptyDay(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	val := GetTodayIncomeValue(cfg, "")
	if val != 0 {
		t.Errorf("expected 0 for empty day, got %v", val)
	}
}

// ---- UpdateTodayIncomeSnapshot Tests ----

func TestUpdateTodayIncomeSnapshot_FirstTimeToday(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	cfg := UpdateTodayIncomeSnapshot(nil, 100, now)
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	cfgMap := ParseExtraConfig(cfg)
	snapshot := cfgMap["todayIncomeSnapshot"].(map[string]any)
	if snapshot["day"] != "2026-07-04" {
		t.Errorf("day = %v, want 2026-07-04", snapshot["day"])
	}
	if snapshot["baseline"].(float64) != 100 {
		t.Errorf("baseline = %v, want 100", snapshot["baseline"])
	}
	if snapshot["latest"].(float64) != 100 {
		t.Errorf("latest = %v, want 100", snapshot["latest"])
	}
}

func TestUpdateTodayIncomeSnapshot_SecondTimeToday_Higher(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	// First: income=100
	cfg1 := snapshotConfig("2026-07-04", 100, 100)
	// Second: income=200
	cfg2 := UpdateTodayIncomeSnapshot(cfg1, 200, now)
	if cfg2 == nil {
		t.Fatal("expected non-nil config")
	}
	cfgMap := ParseExtraConfig(cfg2)
	snapshot := cfgMap["todayIncomeSnapshot"].(map[string]any)
	if snapshot["baseline"].(float64) != 100 {
		t.Errorf("baseline = %v, want 100 (lower income is kept)", snapshot["baseline"])
	}
	if snapshot["latest"].(float64) != 200 {
		t.Errorf("latest = %v, want 200", snapshot["latest"])
	}
}

func TestUpdateTodayIncomeSnapshot_SecondTimeToday_Lower(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	// First: income=200
	cfg1 := snapshotConfig("2026-07-04", 200, 200)
	// Second: income=100 (lower → update baseline)
	cfg2 := UpdateTodayIncomeSnapshot(cfg1, 100, now)
	if cfg2 == nil {
		t.Fatal("expected non-nil config")
	}
	cfgMap := ParseExtraConfig(cfg2)
	snapshot := cfgMap["todayIncomeSnapshot"].(map[string]any)
	if snapshot["baseline"].(float64) != 100 {
		t.Errorf("baseline = %v, want 100 (lower income replaces baseline)", snapshot["baseline"])
	}
	if snapshot["latest"].(float64) != 100 {
		t.Errorf("latest = %v, want 100", snapshot["latest"])
	}
}

func TestUpdateTodayIncomeSnapshot_NewDay(t *testing.T) {
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	// Previous snapshot from 2026-07-04
	cfg1 := snapshotConfig("2026-07-04", 100, 150)
	// New day refresh with income=50
	cfg2 := UpdateTodayIncomeSnapshot(cfg1, 50, now)
	if cfg2 == nil {
		t.Fatal("expected non-nil config")
	}
	cfgMap := ParseExtraConfig(cfg2)
	snapshot := cfgMap["todayIncomeSnapshot"].(map[string]any)
	if snapshot["day"] != "2026-07-05" {
		t.Errorf("day = %v, want 2026-07-05", snapshot["day"])
	}
	if snapshot["baseline"].(float64) != 50 {
		t.Errorf("baseline = %v, want 50 (new day, fresh baseline)", snapshot["baseline"])
	}
	if snapshot["latest"].(float64) != 50 {
		t.Errorf("latest = %v, want 50", snapshot["latest"])
	}
}

func TestUpdateTodayIncomeSnapshot_NonPositiveIncome(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	cfg1 := snapshotConfig("2026-07-04", 100, 150)

	// Non-positive income should keep existing config unchanged
	cfg2 := UpdateTodayIncomeSnapshot(cfg1, 0, now)
	if cfg2 == nil {
		t.Fatal("expected non-nil config (existing unchanged)")
	}
	cfgMap := ParseExtraConfig(cfg2)
	snapshot := cfgMap["todayIncomeSnapshot"].(map[string]any)
	if snapshot["latest"].(float64) != 150 {
		t.Errorf("latest = %v, want unchanged 150", snapshot["latest"])
	}

	cfg3 := UpdateTodayIncomeSnapshot(cfg1, -50, now)
	if cfg3 == nil {
		t.Fatal("expected non-nil config")
	}
	cfgMap3 := ParseExtraConfig(cfg3)
	snapshot3 := cfgMap3["todayIncomeSnapshot"].(map[string]any)
	if snapshot3["latest"].(float64) != 150 {
		t.Errorf("latest = %v, want unchanged 150", snapshot3["latest"])
	}
}

func TestUpdateTodayIncomeSnapshot_NilExisting_NonPositive(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	cfg := UpdateTodayIncomeSnapshot(nil, 0, now)
	if cfg != nil {
		t.Errorf("expected nil config for non-positive income with nil existing, got %v", *cfg)
	}
}

// ---- EstimateRewardWithTodayIncomeFallback Tests ----

func TestEstimateRewardWithTodayIncomeFallback_ZeroSuccessCount(t *testing.T) {
	input := EstimateRewardInput{
		Day:               "2026-07-04",
		SuccessCount:      0,
		ParsedRewardCount: 0,
		RewardSum:         100,
		ExtraConfig:       nil,
	}
	result := EstimateRewardWithTodayIncomeFallback(input)
	if result != 100 {
		t.Errorf("expected 100 (rewardSum) for 0 success, got %v", result)
	}
}

func TestEstimateRewardWithTodayIncomeFallback_AllParsed(t *testing.T) {
	// All rewards were parsed → use rewardSum
	input := EstimateRewardInput{
		Day:               "2026-07-04",
		SuccessCount:      5,
		ParsedRewardCount: 5,
		RewardSum:         500,
		ExtraConfig:       nil,
	}
	result := EstimateRewardWithTodayIncomeFallback(input)
	if result != 500 {
		t.Errorf("expected 500 (rewardSum), got %v", result)
	}
}

func TestEstimateRewardWithTodayIncomeFallback_MissingRewards(t *testing.T) {
	// Some checkins succeeded but reward couldn't be parsed → fallback to todayIncome
	cfg := snapshotConfig("2026-07-04", 100, 300)
	input := EstimateRewardInput{
		Day:               "2026-07-04",
		SuccessCount:      5,
		ParsedRewardCount: 2,
		RewardSum:         200,
		ExtraConfig:       cfg,
	}
	result := EstimateRewardWithTodayIncomeFallback(input)
	// incomeValue(300) > rewardSum(200) → use 300
	if result != 300 {
		t.Errorf("expected 300 (todayIncome > rewardSum), got %v", result)
	}
}

func TestEstimateRewardWithTodayIncomeFallback_MissingRewards_RewardSumWins(t *testing.T) {
	cfg := snapshotConfig("2026-07-04", 100, 150)
	input := EstimateRewardInput{
		Day:               "2026-07-04",
		SuccessCount:      5,
		ParsedRewardCount: 2,
		RewardSum:         200,
		ExtraConfig:       cfg,
	}
	result := EstimateRewardWithTodayIncomeFallback(input)
	// rewardSum(200) > incomeValue(150) → use 200
	if result != 200 {
		t.Errorf("expected 200 (rewardSum > todayIncome), got %v", result)
	}
}

func TestEstimateRewardWithTodayIncomeFallback_NilConfig(t *testing.T) {
	input := EstimateRewardInput{
		Day:               "2026-07-04",
		SuccessCount:      3,
		ParsedRewardCount: 1,
		RewardSum:         50,
		ExtraConfig:       nil,
	}
	result := EstimateRewardWithTodayIncomeFallback(input)
	// No todayIncome available → use rewardSum 50
	if result != 50 {
		t.Errorf("expected 50 (rewardSum fallback), got %v", result)
	}
}

func TestEstimateRewardWithTodayIncomeFallback_NaN(t *testing.T) {
	input := EstimateRewardInput{
		Day:               "2026-07-04",
		SuccessCount:      1,
		ParsedRewardCount: 0,
		RewardSum:         math.NaN(),
		ExtraConfig:       snapshotConfig("2026-07-04", 100, 200),
	}
	result := EstimateRewardWithTodayIncomeFallback(input)
	if result != 200 {
		t.Errorf("expected 200 (todayIncome fallback for NaN rewardSum), got %v", result)
	}
}
