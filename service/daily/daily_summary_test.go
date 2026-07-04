package daily

import (
	"testing"
)

// ---- BuildDailySummaryNotification Tests ----

func TestBuildDailySummaryNotification(t *testing.T) {
	metrics := &DailySummaryMetrics{
		LocalDay:           "2026-07-04",
		GeneratedAtLocal:   "2026-07-04 23:58:00",
		TimeZone:           "Asia/Shanghai",
		TotalAccounts:      10,
		ActiveAccounts:     8,
		LowBalanceAccounts:  2,
		CheckinTotal:       10,
		CheckinSuccess:     7,
		CheckinSkipped:     2,
		CheckinFailed:      1,
		ProxyTotal:         100,
		ProxySuccess:       95,
		ProxyFailed:        5,
		ProxyTotalTokens:   1234567,
		TodaySpend:         0.123456,
		TodayReward:        0.500000,
	}

	title, message := BuildDailySummaryNotification(metrics)

	if title != "每日总结 2026-07-04" {
		t.Errorf("title = %q, want '每日总结 2026-07-04'", title)
	}

	// Verify key parts are present in the message
	if len(message) == 0 {
		t.Fatal("message is empty")
	}

	checks := []string{
		"2026-07-04",
		"2026-07-04 23:58:00",
		"Asia/Shanghai",
		"总计 10",
		"活跃 8",
		"低余额(<$1) 2",
		"成功 7",
		"跳过 2",
		"失败 1",
		"成功 95",
		"失败 5",
		"1,234,567", // formatted tokens
	}
	for _, check := range checks {
		if !contains(message, check) {
			t.Errorf("message missing %q", check)
		}
	}
}

func TestBuildDailySummaryNotification_ZeroValues(t *testing.T) {
	metrics := &DailySummaryMetrics{
		LocalDay:          "2026-07-04",
		GeneratedAtLocal:  "2026-07-04 00:00:00",
		TimeZone:          "UTC",
		TotalAccounts:     0,
		ActiveAccounts:    0,
		LowBalanceAccounts: 0,
		CheckinTotal:      0,
		CheckinSuccess:    0,
		CheckinSkipped:    0,
		CheckinFailed:     0,
		ProxyTotal:        0,
		ProxySuccess:      0,
		ProxyFailed:       0,
		ProxyTotalTokens:  0,
		TodaySpend:        0,
		TodayReward:       0,
	}

	title, message := BuildDailySummaryNotification(metrics)
	if title == "" {
		t.Error("title should not be empty")
	}
	if len(message) == 0 {
		t.Fatal("message is empty")
	}
	// Should not panic with all zeros
}

func TestBuildDailySummaryNotification_NegativeNet(t *testing.T) {
	metrics := &DailySummaryMetrics{
		LocalDay:          "2026-07-04",
		GeneratedAtLocal:  "2026-07-04 23:58:00",
		TimeZone:          "UTC",
		TotalAccounts:     5,
		ActiveAccounts:    5,
		LowBalanceAccounts: 0,
		CheckinTotal:      5,
		CheckinSuccess:    5,
		CheckinSkipped:    0,
		CheckinFailed:     0,
		ProxyTotal:        50,
		ProxySuccess:      48,
		ProxyFailed:       2,
		ProxyTotalTokens:  500000,
		TodaySpend:        10.000000,
		TodayReward:       5.000000,
	}

	_, message := BuildDailySummaryNotification(metrics)
	// Net should be -5.000000
	if !contains(message, "$-5.000000") {
		t.Logf("message: %s", message)
		t.Error("expected negative net value in message")
	}
}

// ---- formatTokens Tests ----

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{100, "100"},
		{1000, "1,000"},
		{10000, "10,000"},
		{100000, "100,000"},
		{1000000, "1,000,000"},
		{1234567890, "1,234,567,890"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatTokens(tt.input)
			if got != tt.want {
				t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---- Round6 Tests ----

func TestRound6(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{100, 100},
		{12.345678, 12.345678},
		{0.123456789, 0.123457},
		{0, 0},
		{-0.123456789, -0.123457},
	}
	for _, tt := range tests {
		got := Round6(tt.input)
		if got != tt.want {
			t.Errorf("Round6(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ---- DailySummaryMetrics Tests ----

func TestDailySummaryMetrics_Fields(t *testing.T) {
	m := DailySummaryMetrics{
		LocalDay:           "2026-07-04",
		GeneratedAtLocal:   "2026-07-04 23:58:00",
		TimeZone:           "Asia/Shanghai",
		TotalAccounts:      10,
		ActiveAccounts:     8,
		LowBalanceAccounts:  2,
		CheckinTotal:       10,
		CheckinSuccess:     7,
		CheckinSkipped:     2,
		CheckinFailed:      1,
		ProxyTotal:         100,
		ProxySuccess:       95,
		ProxyFailed:        5,
		ProxyTotalTokens:   1234567,
		TodaySpend:         0.5,
		TodayReward:        1.0,
	}

	if m.TodayReward != 1.0 {
		t.Errorf("TodayReward = %v, want 1.0", m.TodayReward)
	}
	if m.TodaySpend != 0.5 {
		t.Errorf("TodaySpend = %v, want 0.5", m.TodaySpend)
	}
	if m.CheckinSuccess+m.CheckinSkipped+m.CheckinFailed != m.CheckinTotal {
		t.Errorf("checkin sub-counts don't sum to total: %d+%d+%d != %d",
			m.CheckinSuccess, m.CheckinSkipped, m.CheckinFailed, m.CheckinTotal)
	}
}

// ---- Net Value Calculation ----

func TestNetValue(t *testing.T) {
	// 净值 = TodayReward - TodaySpend
	reward := 100.0
	spend := 30.5
	net := Round6(reward - spend)
	if net != 69.5 {
		t.Errorf("net = %v, want 69.5", net)
	}
}

// Helper
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
