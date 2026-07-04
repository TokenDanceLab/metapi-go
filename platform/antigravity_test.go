package platform

import (
	"context"
	"testing"
)

func TestAntigravityAdapter_Detect(t *testing.T) {
	a := &AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://antigravity.example.com/v1/models", true},
		{"https://example.com/antigravity/v1", true},
		{"https://ANTIGRAVITY.example.com", true},
		{"https://api.openai.com", false},
		{"https://example.com/gravity", false},
	}
	for _, tt := range tests {
		ok, err := a.Detect(ctx, tt.url)
		if err != nil {
			t.Errorf("Detect(%q) error: %v", tt.url, err)
			continue
		}
		if ok != tt.matches {
			t.Errorf("Detect(%q) = %v, want %v", tt.url, ok, tt.matches)
		}
	}
}

func TestAntigravityAdapter_PlatformName(t *testing.T) {
	a := &AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")}
	if a.PlatformName() != "antigravity" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestAntigravityAdapter_LoginUnspported(t *testing.T) {
	a := &AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")}
	ctx := context.Background()

	lr, err := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if lr.Success {
		t.Error("Antigravity login should return Success=false")
	}
}

func TestAntigravityAdapter_CheckinUnspported(t *testing.T) {
	a := &AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")}
	ctx := context.Background()

	cr, err := a.Checkin(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cr.Success {
		t.Error("Antigravity checkin should return Success=false")
	}
}

func TestAntigravityAdapter_GetBalanceZero(t *testing.T) {
	a := &AntigravityAdapter{BaseAdapter: NewBaseAdapter("antigravity")}
	ctx := context.Background()

	bi, err := a.GetBalance(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if bi.Balance != 0 || bi.Used != 0 || bi.Quota != 0 {
		t.Error("Antigravity balance should be all zeros")
	}
}

func TestExtractAntigravityModelNames(t *testing.T) {
	// Object form
	obj := map[string]interface{}{
		"models": map[string]interface{}{
			"gpt-4":          map[string]interface{}{},
			"claude-3-opus":  map[string]interface{}{},
			"  ":             map[string]interface{}{}, // whitespace name, should be filtered
		},
	}
	names := extractAntigravityModelNames(obj)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}
	hasGPT4 := false
	hasClaude := false
	for _, n := range names {
		if n == "gpt-4" {
			hasGPT4 = true
		}
		if n == "claude-3-opus" {
			hasClaude = true
		}
	}
	if !hasGPT4 || !hasClaude {
		t.Errorf("missing expected models: %v", names)
	}

	// Array form with id/name fields
	arr := map[string]interface{}{
		"models": []interface{}{
			map[string]interface{}{"id": "model-a", "name": "ignored"},
			map[string]interface{}{"name": "model-b"},
			"model-c",
		},
	}
	names2 := extractAntigravityModelNames(arr)
	if len(names2) != 3 {
		t.Fatalf("expected 3 names, got %d: %v", len(names2), names2)
	}

	// No models key
	names3 := extractAntigravityModelNames(map[string]interface{}{})
	if len(names3) != 0 {
		t.Errorf("expected empty, got %v", names3)
	}

	// Empty models
	names4 := extractAntigravityModelNames(map[string]interface{}{"models": map[string]interface{}{}})
	if len(names4) != 0 {
		t.Errorf("expected empty, got %v", names4)
	}
}
