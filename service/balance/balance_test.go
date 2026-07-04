package balance

import (
	"math"
	"testing"
)

// ---- IsSiteDisabled Tests ----

func TestIsSiteDisabled(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"disabled", true},
		{" disabled ", true},
		{"active", false},
		{"", false},
		{"  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := IsSiteDisabled(tt.status)
			if got != tt.want {
				t.Errorf("IsSiteDisabled(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ---- shouldAttemptAutoReloginBalance Tests ----
// Extra patterns beyond checkin version: unauthorized, forbidden, not login, not logged

func TestShouldAttemptAutoReloginBalance_Positive(t *testing.T) {
	// Same as checkin version
	checkinCases := []string{
		"jwt expired",
		"token expired",
		"invalid access token",
		"new-api-user header required",
		"access token is missing",
	}
	// Extra patterns for balance
	balanceExtras := []string{
		"unauthorized",
		"forbidden",
		"not login",
		"not logged in",
		"user not logged in",
	}

	allCases := append(checkinCases, balanceExtras...)
	for _, msg := range allCases {
		t.Run(msg, func(t *testing.T) {
			if !shouldAttemptAutoReloginBalance(msg) {
				t.Errorf("expected true for: %q", msg)
			}
		})
	}
}

func TestShouldAttemptAutoReloginBalance_Negative(t *testing.T) {
	negativeCases := []string{
		"",
		"some random error",
		"connection timeout",
		"server error",
		"not found",
	}
	for _, msg := range negativeCases {
		t.Run(msg, func(t *testing.T) {
			if shouldAttemptAutoReloginBalance(msg) {
				t.Errorf("expected false for: %q", msg)
			}
		})
	}
}

func TestShouldAttemptAutoReloginBalance_BalanceExtraPatterns(t *testing.T) {
	// These should ONLY match in balance version, not checkin version
	// "unauthorized" → true in balance
	if !shouldAttemptAutoReloginBalance("unauthorized") {
		t.Error("expected true for 'unauthorized' in balance version")
	}
	// "forbidden" → true in balance
	if !shouldAttemptAutoReloginBalance("forbidden") {
		t.Error("expected true for 'forbidden' in balance version")
	}
	// "not login" → true in balance
	if !shouldAttemptAutoReloginBalance("not login") {
		t.Error("expected true for 'not login' in balance version")
	}
	// "not logged" → true in balance
	if !shouldAttemptAutoReloginBalance("not logged") {
		t.Error("expected true for 'not logged' in balance version")
	}
}

// ---- supportsTodayIncomeLogFallback Tests ----

func TestSupportsTodayIncomeLogFallback(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{"new-api", true},
		{"new-api", true},
		{"anyrouter", true},
		{"AnyRouter", true},
		{"one-api", true},
		{"veloera", true},
		{"Veloera", true},
		{"openai", false},
		{"anthropic", false},
		{"gemini", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := supportsTodayIncomeLogFallback(tt.platform)
			if got != tt.want {
				t.Errorf("supportsTodayIncomeLogFallback(%q) = %v, want %v", tt.platform, got, tt.want)
			}
		})
	}
}

// ---- resolveQuotaConversionFactor Tests ----

func TestResolveQuotaConversionFactor(t *testing.T) {
	tests := []struct {
		platform string
		want     float64
	}{
		{"veloera", 1_000_000},
		{"Veloera", 1_000_000},
		{"new-api", 500_000},
		{"anyrouter", 500_000},
		{"one-api", 500_000},
		{"openai", 500_000},   // default
		{"unknown", 500_000},  // default
		{"", 500_000},         // default
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := resolveQuotaConversionFactor(tt.platform)
			if got != tt.want {
				t.Errorf("resolveQuotaConversionFactor(%q) = %v, want %v", tt.platform, got, tt.want)
			}
		})
	}
}

// ---- parsePositiveNumberAny Tests ----

func TestParsePositiveNumberAny(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  float64
	}{
		{"positive float64", float64(100), 100},
		{"zero float64", float64(0), 0},
		{"negative float64", float64(-5), 0},
		{"NaN float64", math.NaN(), 0},
		{"positive string", "50", 50},
		{"zero string", "0", 0},
		{"negative string", "-10", 0},
		{"empty string", "", 0},
		{"invalid string", "abc", 0},
		{"nil", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePositiveNumberAny(tt.input)
			if got != tt.want {
				t.Errorf("parsePositiveNumberAny(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---- BalanceResult Tests ----

func TestBalanceResult_Success(t *testing.T) {
	r := BalanceResult{
		Balance: 100.5,
		Used:    50.2,
		Quota:   200,
		Skipped: false,
	}
	if r.Skipped {
		t.Error("success result should not be skipped")
	}
	if r.Balance != 100.5 {
		t.Errorf("Balance = %v, want 100.5", r.Balance)
	}
}

func TestBalanceResult_Skipped(t *testing.T) {
	r := BalanceResult{
		Balance: 100,
		Used:    50,
		Quota:   200,
		Skipped: true,
		Reason:  "site_disabled",
	}
	if !r.Skipped {
		t.Error("skipped result should have Skipped=true")
	}
	if r.Reason != "site_disabled" {
		t.Errorf("Reason = %q, want 'site_disabled'", r.Reason)
	}
}

func TestBalanceResult_APIKeyProxy(t *testing.T) {
	r := BalanceResult{
		Balance: 0,
		Used:    0,
		Quota:   0,
		Skipped: true,
		Reason:  "proxy_only",
	}
	if r.Reason != "proxy_only" {
		t.Errorf("Reason = %q, want 'proxy_only'", r.Reason)
	}
}

// ---- extractLogItems Tests ----

func TestExtractLogItems_DataItems(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{"quota": float64(100)},
				map[string]any{"quota": float64(200)},
			},
		},
	}
	items := extractLogItems(payload)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestExtractLogItems_DirectItems(t *testing.T) {
	payload := map[string]any{
		"items": []any{
			map[string]any{"quota": float64(50)},
		},
	}
	items := extractLogItems(payload)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestExtractLogItems_DataArray(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{"quota": float64(300)},
		},
	}
	items := extractLogItems(payload)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestExtractLogItems_Empty(t *testing.T) {
	items := extractLogItems(map[string]any{})
	if items != nil {
		t.Errorf("expected nil for empty payload, got %v", items)
	}
}

// ---- extractLogTotal Tests ----

func TestExtractLogTotal(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    int
	}{
		{
			name:    "nested data.total as float64",
			payload: map[string]any{"data": map[string]any{"total": float64(42)}},
			want:    42,
		},
		{
			name:    "direct total as float64",
			payload: map[string]any{"total": float64(100)},
			want:    100,
		},
		{
			name:    "total as string",
			payload: map[string]any{"total": "50"},
			want:    50,
		},
		{
			name:    "no total",
			payload: map[string]any{},
			want:    -1, // returns nil, we check for nil
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLogTotal(tt.payload)
			if tt.want == -1 {
				if got != nil {
					t.Errorf("expected nil, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Errorf("expected %d, got nil", tt.want)
				return
			}
			if *got != tt.want {
				t.Errorf("extractLogTotal() = %d, want %d", *got, tt.want)
			}
		})
	}
}

// ---- parseIncomeFromContent Tests ----

func TestParseIncomeFromContent(t *testing.T) {
	// Current implementation is simplified; tests verify it doesn't panic
	_ = parseIncomeFromContent("some content with 100")
	_ = parseIncomeFromContent("")
	_ = parseIncomeFromContent("no numbers here")
}
