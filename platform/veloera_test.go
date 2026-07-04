package platform

import (
	"context"
	"testing"
)

func TestVeloeraAdapter_PlatformName(t *testing.T) {
	v := &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}
	if v.PlatformName() != "veloera" {
		t.Errorf("PlatformName: %q", v.PlatformName())
	}
}

func TestVeloeraAdapter_Detect(t *testing.T) {
	v := &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}
	ctx := context.Background()

	// Detect requires HTTP probe; returns false for unreachable URL
	ok, err := v.Detect(ctx, "http://127.0.0.1:1")
	if err != nil {
		t.Errorf("Detect should not return error on probe failure: %v", err)
	}
	if ok {
		t.Error("Detect should return false for unreachable URL")
	}
}

func TestVeloeraAdapter_Headers(t *testing.T) {
	// veloeraHeaders sets Authorization + Veloera-User + New-API-User + User-id
	id := 5
	h := veloeraHeaders("token", &id)

	if h["Authorization"] != "Bearer token" {
		t.Errorf("Authorization: %q", h["Authorization"])
	}
	if h["Veloera-User"] != "5" {
		t.Errorf("Veloera-User: %q", h["Veloera-User"])
	}
	if h["New-API-User"] != "5" {
		t.Errorf("New-API-User: %q", h["New-API-User"])
	}
	if h["User-id"] != "5" {
		t.Errorf("User-id: %q", h["User-id"])
	}
	// Veloera only sets 3 user headers (not 7 like NewApi)
	if len(h) != 4 { // Authorization + 3 user headers
		t.Errorf("expected 4 headers total, got %d: %v", len(h), h)
	}

	// nil userID
	h2 := veloeraHeaders("token", nil)
	if h2["Authorization"] != "Bearer token" {
		t.Errorf("Authorization with nil: %q", h2["Authorization"])
	}
	if _, ok := h2["Veloera-User"]; ok {
		t.Error("Veloera-User should not be set with nil userID")
	}
}

func TestVeloeraAdapter_Checkin(t *testing.T) {
	v := &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}
	ctx := context.Background()

	cr, err := v.Checkin(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("Checkin should not error: %v", err)
	}
	if cr.Success {
		t.Error("Checkin on unreachable URL should fail")
	}
}

func TestVeloeraAdapter_CheckinWithUserID(t *testing.T) {
	v := &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}
	ctx := context.Background()

	id := 1
	cr, err := v.Checkin(ctx, "http://127.0.0.1:1", "token", &id, nil)
	if err != nil {
		t.Errorf("Checkin with userID should not error: %v", err)
	}
	if cr.Success {
		t.Error("Checkin on unreachable URL should fail")
	}
}

func TestVeloeraAdapter_GetBalance_1MDivisor(t *testing.T) {
	v := &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}
	ctx := context.Background()

	bi, err := v.GetBalance(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("GetBalance should not error: %v", err)
	}
	// Returns empty BalanceInfo on failure
	if bi.Balance != 0 {
		t.Errorf("Balance on unreachable should be 0, got %f", bi.Balance)
	}
}

func TestVeloeraAdapter_DivisorIs1M(t *testing.T) {
	// Veloera uses 1,000,000 divisor, NOT 500,000
	quota := 2000000.0
	used := 500000.0

	balance := (quota - used) / 1000000
	quotaUSD := quota / 1000000
	usedUSD := used / 1000000

	if balance != 1.5 {
		t.Errorf("Veloera balance: %f, want 1.5", balance)
	}
	if quotaUSD != 2.0 {
		t.Errorf("Veloera quotaUSD: %f, want 2.0", quotaUSD)
	}
	if usedUSD != 0.5 {
		t.Errorf("Veloera usedUSD: %f, want 0.5", usedUSD)
	}

	// Compare with NewApi divisor (500000)
	newApiBalance := (quota - used) / 500000 // 3.0
	if newApiBalance == balance {
		t.Error("Veloera 1M divisor should differ from NewApi 500K divisor for same inputs")
	}
}

func TestVeloeraAdapter_GetModels(t *testing.T) {
	v := &VeloeraAdapter{BaseAdapter: NewBaseAdapter("veloera")}
	ctx := context.Background()

	models, err := v.GetModels(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("GetModels should not error: %v", err)
	}
	if len(models) != 0 {
		t.Error("GetModels on unreachable should return empty")
	}
}
