package checkin

import (
	"math"
	"testing"
)

// ---- Reward Parser Tests ----

func TestParseCheckinRewardAmount_NumberInputs(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  float64
	}{
		{"float64 positive", float64(100), 100},
		{"float64 large", float64(500000), 500000},
		{"float64 decimal", float64(12.5), 12.5},
		{"int positive", 42, 42},
		{"int64 positive", int64(999), 999},
		{"float64 zero", float64(0), 0},
		{"float64 negative", float64(-5), 0},
		{"float64 NaN", math.NaN(), 0}, // NaN check handled by toFiniteNumber returning nil
		{"int zero", 0, 0},
		{"int negative", -10, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCheckinRewardAmount(tt.input)
			if got != tt.want {
				t.Errorf("ParseCheckinRewardAmount(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCheckinRewardAmount_StringInputs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{"simple number", "100", 100},
		{"with commas", "1,000", 1000},
		{"with text prefix", "恭喜获得 200 积分", 200},
		{"with text suffix", "500 points earned", 500},
		{"decimal value", "50.5", 50.5},
		{"negative number", "-100", 0},
		{"zero", "0", 0},
		{"no number", "checkin success", 0},
		{"empty string", "", 0},
		{"large with commas", "1,234,567.89", 1234567.89},
		{"multiple numbers picks first", "100 then 200", 100},
		{"Chinese text with number", "您已获得 150 积分奖励", 150},
		{"reward with plus sign", "+50", 50},
		{"Korean won style", "500,000", 500000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCheckinRewardAmount(tt.input)
			if got != tt.want {
				t.Errorf("ParseCheckinRewardAmount(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCheckinRewardAmount_NonStringNonNumber(t *testing.T) {
	// bool, nil, map etc should return 0
	got := ParseCheckinRewardAmount(true)
	if got != 0 {
		t.Errorf("expected 0 for bool, got %v", got)
	}
	got = ParseCheckinRewardAmount(nil)
	if got != 0 {
		t.Errorf("expected 0 for nil, got %v", got)
	}
}

// ---- InferRewardFromBalanceDelta Tests ----

func TestInferRewardFromBalanceDelta(t *testing.T) {
	tests := []struct {
		name         string
		previous     float64
		latest       float64
		want         float64
	}{
		{"positive delta", 0, 100, 100},
		{"zero delta", 50, 50, 0},
		{"negative delta", 100, 50, 0},
		{"small positive delta", 10, 10.5, 0.5},
		{"rounds to 6 decimal places", 0, 0.123456789, 0.123457},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferRewardFromBalanceDelta(tt.previous, tt.latest)
			if got != tt.want {
				t.Errorf("InferRewardFromBalanceDelta(%v, %v) = %v, want %v",
					tt.previous, tt.latest, got, tt.want)
			}
		})
	}
}

// ---- Round6 Tests ----

func TestRound6(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{"whole number", 100, 100},
		{"decimal within precision", 12.345678, 12.345678},
		{"decimal beyond precision", 0.123456789, 0.123457},
		{"zero", 0, 0},
		{"negative", -0.123456789, -0.123457},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Round6(tt.input)
			if got != tt.want {
				t.Errorf("Round6(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
