package oauth

import (
	"encoding/json"
	"testing"
)

// ---- OauthQuotaSnapshot Types ----

func TestOauthQuotaSnapshot_ZeroValue(t *testing.T) {
	snap := OauthQuotaSnapshot{}
	if snap.Status != "" {
		t.Errorf("zero Status should be empty, got %q", snap.Status)
	}
	if snap.Source != "" {
		t.Errorf("zero Source should be empty, got %q", snap.Source)
	}
	if snap.Windows != nil {
		t.Error("zero Windows should be nil")
	}
}

func TestOauthQuotaSnapshot_JSONRoundTrip(t *testing.T) {
	limit := 1000.0
	used := 500.0
	remaining := 500.0
	snap := OauthQuotaSnapshot{
		Status:        "reverse_engineered",
		Source:        "codex_probe",
		LastSyncAt:    "2026-07-04T00:00:00Z",
		ProviderMessage: "Rate limits active",
		Windows: &OauthQuotaWindows{
			FiveHour: &OauthQuotaWindowSnapshot{
				Supported: true,
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				ResetAt:   "2026-07-04T05:00:00Z",
			},
			SevenDay: &OauthQuotaWindowSnapshot{
				Supported: true,
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				ResetAt:   "2026-07-11T00:00:00Z",
			},
		},
	}

	bytes, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored OauthQuotaSnapshot
	if err := json.Unmarshal(bytes, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if restored.Status != "reverse_engineered" {
		t.Errorf("expected status 'reverse_engineered', got %q", restored.Status)
	}
	if restored.Windows.FiveHour.Supported != true {
		t.Error("fiveHour should be supported")
	}
	if restored.Windows.SevenDay.Supported != true {
		t.Error("sevenDay should be supported")
	}
	if restored.LastSyncAt != "2026-07-04T00:00:00Z" {
		t.Errorf("expected LastSyncAt, got %q", restored.LastSyncAt)
	}
}

// ---- OauthQuotaWindowSnapshot Tests ----

func TestOauthQuotaWindowSnapshot_NilFields(t *testing.T) {
	w := OauthQuotaWindowSnapshot{Supported: false}
	if w.Limit != nil {
		t.Error("Limit should be nil when not set")
	}
	if w.Used != nil {
		t.Error("Used should be nil when not set")
	}
	if w.Remaining != nil {
		t.Error("Remaining should be nil when not set")
	}
}

func TestOauthQuotaWindowSnapshot_ErrorSnapshot(t *testing.T) {
	w := OauthQuotaWindowSnapshot{
		Supported: false,
		Message:   "probe timeout after 10s",
	}
	bytes, _ := json.Marshal(w)
	if len(bytes) == 0 {
		t.Error("error snapshot should serialize")
	}
}

// ---- OauthQuotaWindows Tests ----

func TestOauthQuotaWindows_NilWindows(t *testing.T) {
	w := OauthQuotaWindows{}
	if w.FiveHour != nil {
		t.Error("FiveHour should be nil by default")
	}
	if w.SevenDay != nil {
		t.Error("SevenDay should be nil by default")
	}
}

// ---- OauthSubscription Tests ----

func TestOauthSubscription_ZeroValue(t *testing.T) {
	s := OauthSubscription{}
	if s.PlanType != "" {
		t.Errorf("zero PlanType should be empty, got %q", s.PlanType)
	}
}

// ---- Quota Snapshot Status Values ----

func TestQuotaSnapshotStatuses(t *testing.T) {
	validStatuses := []string{"reverse_engineered", "error", "unsupported"}
	for _, status := range validStatuses {
		snap := OauthQuotaSnapshot{
			Status: status,
			Source: "codex_probe",
		}
		if snap.Status != status {
			t.Errorf("expected status %q, got %q", status, snap.Status)
		}
	}
}

// ---- asQuotaSnapshotGo Tests ----

func TestAsQuotaSnapshotGo_ValidSnapshot(t *testing.T) {
	limit := 50.0
	raw := map[string]interface{}{
		"status": "reverse_engineered",
		"source": "codex_probe",
		"lastSyncAt": "2026-07-04T12:00:00Z",
		"lastError": "none",
		"providerMessage": "ok",
		"windows": map[string]interface{}{
			"fiveHour": map[string]interface{}{
				"supported": true,
				"limit":     limit,
				"used":      25.0,
				"remaining": 25.0,
				"resetAt":   "2026-07-04T17:00:00Z",
			},
			"sevenDay": map[string]interface{}{
				"supported": false,
				"message":   "not available",
			},
		},
	}

	snap := asQuotaSnapshotGo(raw)
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Status != "reverse_engineered" {
		t.Errorf("expected status 'reverse_engineered', got %q", snap.Status)
	}
	if snap.Source != "codex_probe" {
		t.Errorf("expected source 'codex_probe', got %q", snap.Source)
	}
	if snap.LastSyncAt != "2026-07-04T12:00:00Z" {
		t.Errorf("expected LastSyncAt, got %q", snap.LastSyncAt)
	}
	if snap.LastError != "none" {
		t.Errorf("expected LastError 'none', got %q", snap.LastError)
	}
	if snap.ProviderMessage != "ok" {
		t.Errorf("expected ProviderMessage 'ok', got %q", snap.ProviderMessage)
	}
	if snap.Windows.FiveHour == nil {
		t.Fatal("FiveHour should be non-nil")
	}
	if !snap.Windows.FiveHour.Supported {
		t.Error("FiveHour should be supported")
	}
	if *snap.Windows.FiveHour.Limit != 50.0 {
		t.Errorf("expected FiveHour limit 50.0, got %f", *snap.Windows.FiveHour.Limit)
	}
	if snap.Windows.SevenDay == nil {
		t.Fatal("SevenDay should be non-nil")
	}
	if snap.Windows.SevenDay.Supported {
		t.Error("SevenDay should be unsupported")
	}
}

func TestAsQuotaSnapshotGo_Nil(t *testing.T) {
	snap := asQuotaSnapshotGo(nil)
	if snap != nil {
		t.Error("expected nil for nil input")
	}
}

func TestAsQuotaSnapshotGo_NonMap(t *testing.T) {
	snap := asQuotaSnapshotGo("not a map")
	if snap != nil {
		t.Error("expected nil for non-map input")
	}
}

func TestAsQuotaSnapshotGo_MissingStatus(t *testing.T) {
	raw := map[string]interface{}{
		"source": "codex_probe",
	}
	snap := asQuotaSnapshotGo(raw)
	if snap != nil {
		t.Error("expected nil when status is missing")
	}
}

func TestAsQuotaSnapshotGo_MissingSource(t *testing.T) {
	raw := map[string]interface{}{
		"status": "reverse_engineered",
	}
	snap := asQuotaSnapshotGo(raw)
	if snap != nil {
		t.Error("expected nil when source is missing")
	}
}

// ---- normalizeQuotaWindowGo Tests ----

func TestNormalizeQuotaWindowGo_Supported(t *testing.T) {
	limit := 100.0
	raw := map[string]interface{}{
		"supported": true,
		"limit":     limit,
		"used":      30.0,
		"remaining": 70.0,
		"resetAt":   "2026-07-04T23:00:00Z",
	}
	w := normalizeQuotaWindowGo(raw)
	if w == nil {
		t.Fatal("expected non-nil window")
	}
	if !w.Supported {
		t.Error("expected supported true")
	}
	if *w.Limit != 100.0 {
		t.Errorf("expected limit 100.0, got %f", *w.Limit)
	}
	if *w.Used != 30.0 {
		t.Errorf("expected used 30.0, got %f", *w.Used)
	}
	if *w.Remaining != 70.0 {
		t.Errorf("expected remaining 70.0, got %f", *w.Remaining)
	}
	if w.ResetAt != "2026-07-04T23:00:00Z" {
		t.Errorf("expected resetAt, got %q", w.ResetAt)
	}
	if w.Message != "" {
		t.Errorf("expected no message, got %q", w.Message)
	}
}

func TestNormalizeQuotaWindowGo_Unsupported(t *testing.T) {
	raw := map[string]interface{}{
		"supported": false,
		"message":   "quota window not available",
	}
	w := normalizeQuotaWindowGo(raw)
	if w == nil {
		t.Fatal("expected non-nil window")
	}
	if w.Supported {
		t.Error("expected supported false")
	}
	if w.Message != "quota window not available" {
		t.Errorf("expected message, got %q", w.Message)
	}
	if w.Limit != nil {
		t.Error("limit should be nil for unsupported")
	}
}

func TestNormalizeQuotaWindowGo_OptionalFields(t *testing.T) {
	raw := map[string]interface{}{
		"supported": true,
	}
	w := normalizeQuotaWindowGo(raw)
	if w == nil {
		t.Fatal("expected non-nil window")
	}
	if w.Limit != nil {
		t.Error("limit should be nil when not provided")
	}
	if w.ResetAt != "" {
		t.Errorf("resetAt should be empty, got %q", w.ResetAt)
	}
}
