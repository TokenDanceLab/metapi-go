package balance

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
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

func TestRefreshBalance_UsesSiteCustomHeaders(t *testing.T) {
	headerSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/user/self" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Metapi-Site") == "site-header" {
			headerSeen = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"id":         42,
				"username":   "anyrouter-user",
				"quota":      1_000_000,
				"used_quota": 250_000,
			},
		})
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteRes, err := db.Exec(
		"INSERT INTO sites (name, url, platform, custom_headers, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?)",
		"AnyRouter balance headers", server.URL, "anyrouter", `{"X-Metapi-Site":"site-header"}`, now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := siteRes.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	accountRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?, ?)",
		siteID, "anyrouter-user", "session-token", true, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := accountRes.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}

	result, err := RefreshBalance(&config.Config{}, db.DB, accountID)
	if err != nil {
		t.Fatalf("RefreshBalance: %v", err)
	}
	if result == nil || result.Balance != 2 || result.Used != 0.5 || result.Quota != 2.5 {
		t.Fatalf("balance result = %+v, want (2,0.5,2.5)", result)
	}
	if !headerSeen {
		t.Fatal("site custom header was not sent to /api/user/self")
	}
}

func TestRefreshBalance_DisabledAnyRouterAccountIsSkippedWithoutUpstream(t *testing.T) {
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteRes, err := db.Exec(
		"INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)",
		"AnyRouter disabled balance", server.URL, "anyrouter", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := siteRes.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	accountRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, balance, balance_used, quota, created_at, updated_at) VALUES (?, ?, ?, 'disabled', ?, ?, ?, ?, ?, ?)",
		siteID, "anyrouter-user", "session-token", true, 7.0, 2.0, 9.0, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := accountRes.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}
	if _, err := db.Exec("UPDATE accounts SET status = 'DISABLED' WHERE id = ?", accountID); err != nil {
		t.Fatalf("upper-case disable account: %v", err)
	}

	result, err := RefreshBalance(&config.Config{}, db.DB, accountID)
	if err != nil {
		t.Fatalf("RefreshBalance: %v", err)
	}
	if result == nil || !result.Skipped || result.Reason != "account_disabled" {
		t.Fatalf("balance result = %+v, want account_disabled skip", result)
	}
	if result.Balance != 7.0 || result.Used != 2.0 || result.Quota != 9.0 {
		t.Fatalf("balance fields = (%v,%v,%v), want original (7,2,9)", result.Balance, result.Used, result.Quota)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
	}

	var extraConfig *string
	if err := db.Get(&extraConfig, "SELECT extra_config FROM accounts WHERE id = ?", accountID); err != nil {
		t.Fatalf("read extra_config: %v", err)
	}
	if extraConfig == nil {
		t.Fatal("extra_config is nil; want runtimeHealth")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(*extraConfig), &cfg); err != nil {
		t.Fatalf("unmarshal extra_config: %v", err)
	}
	health, ok := cfg["runtimeHealth"].(map[string]any)
	if !ok || health["state"] != "disabled" || health["source"] != "balance" {
		t.Fatalf("runtimeHealth = %#v, want disabled/balance", cfg["runtimeHealth"])
	}
}

func TestRefreshBalance_LegacyMirroredAPIKeyAccountIsSkippedWithoutUpstream(t *testing.T) {
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteRes, err := db.Exec(
		"INSERT INTO sites (name, url, platform, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)",
		"AnyRouter legacy api key balance", server.URL, "anyrouter", now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := siteRes.LastInsertId()
	if err != nil {
		t.Fatalf("site LastInsertId: %v", err)
	}
	accountRes, err := db.Exec(
		"INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, balance, balance_used, quota, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?)",
		siteID, nil, "legacy-api-key", "legacy-api-key", true, 3.0, 1.0, 4.0, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, err := accountRes.LastInsertId()
	if err != nil {
		t.Fatalf("account LastInsertId: %v", err)
	}

	result, err := RefreshBalance(&config.Config{}, db.DB, accountID)
	if err != nil {
		t.Fatalf("RefreshBalance: %v", err)
	}
	if result == nil || !result.Skipped || result.Reason != "proxy_only" {
		t.Fatalf("balance result = %+v, want proxy_only skip", result)
	}
	if result.Balance != 3.0 || result.Used != 1.0 || result.Quota != 4.0 {
		t.Fatalf("balance fields = (%v,%v,%v), want original (3,1,4)", result.Balance, result.Used, result.Quota)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstreamCalls = %d, want 0", upstreamCalls)
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

func TestSub2APIRefreshPromiseBroadcastsResultToAllWaiters(t *testing.T) {
	p := newSub2APIRefreshPromise()

	done := make(chan sub2apiRefreshResult, 2)
	go func() { done <- p.wait() }()
	go func() { done <- p.wait() }()

	want := sub2apiRefreshResult{accessToken: "shared-sub2api-token"}
	p.resolve(want)

	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if got.accessToken != want.accessToken || got.err != want.err {
				t.Fatalf("waiter %d got %+v, want %+v", i, got, want)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("waiter %d did not receive broadcast result", i)
		}
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
		{"openai", 500_000},  // default
		{"unknown", 500_000}, // default
		{"", 500_000},        // default
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

func TestDecodeIncomeLogPayloadRejectsOversizedResponse(t *testing.T) {
	var payload map[string]any
	err := decodeIncomeLogPayload(strings.NewReader(`{"data":[]}`+strings.Repeat(" ", incomeLogResponseBodyLimit+1)), &payload)
	if err == nil {
		t.Fatal("decodeIncomeLogPayload succeeded, want oversized response error")
	}
	if !strings.Contains(err.Error(), "income log response exceeds") {
		t.Fatalf("error = %v, want size limit", err)
	}
}

func TestDecodeIncomeLogPayloadAcceptsNormalResponse(t *testing.T) {
	var payload map[string]any
	if err := decodeIncomeLogPayload(strings.NewReader(`{"data":{"items":[{"quota":500000}]}}`), &payload); err != nil {
		t.Fatalf("decodeIncomeLogPayload: %v", err)
	}
	items := extractLogItems(payload)
	if len(items) != 1 || parsePositiveNumberAny(items[0]["quota"]) != 500000 {
		t.Fatalf("items = %#v, want one quota item", items)
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
