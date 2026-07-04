package platform

import (
	"context"
	"testing"
)

func TestAnyRouterAdapter_PlatformName(t *testing.T) {
	a := &AnyRouterAdapter{
		NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")},
	}
	if a.PlatformName() != "anyrouter" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestAnyRouterAdapter_Detect(t *testing.T) {
	a := &AnyRouterAdapter{
		NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")},
	}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://anyrouter.example.com", true},
		{"https://ANYROUTER.example.com", true},
		{"https://example.com/anyrouter/v1", true},
		{"https://newapi.example.com", false},
		{"https://oneapi.example.com", false},
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

func TestAnyRouterAdapter_InheritsNewApiMethods(t *testing.T) {
	a := &AnyRouterAdapter{
		NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")},
	}
	ctx := context.Background()

	// AnyRouter inherits all NewApi methods including cookie fallback, shield challenge, user-ID probing
	// All HTTP-based methods should gracefully handle unreachable URLs

	// Login
	lr, err := a.Login(ctx, "http://127.0.0.1:1", "user", "pass", nil, nil)
	if err != nil {
		t.Errorf("Login error: %v", err)
	}
	if lr.Success {
		t.Error("Login on unreachable URL should fail")
	}

	// Checkin
	cr, err := a.Checkin(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("Checkin error: %v", err)
	}
	if cr.Success {
		t.Error("Checkin on unreachable URL should fail")
	}

	// GetBalance
	bi, err := a.GetBalance(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err == nil {
		// GetBalance returns error on total failure (different from other adapters)
		if bi != nil && bi.Balance > 0 {
			t.Error("Balance should be 0 on unreachable URL")
		}
	}

	// GetModels
	models, err := a.GetModels(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		t.Errorf("GetModels error: %v", err)
	}
	if len(models) != 0 {
		t.Error("GetModels on unreachable should return empty")
	}

	// GetUserGroups - may return error or default on unreachable URL
	groups, err := a.GetUserGroups(ctx, "http://127.0.0.1:1", "token", nil, nil)
	if err != nil {
		// Error is acceptable on unreachable URL (terminalError propagation)
		t.Logf("GetUserGroups error (expected on unreachable): %v", err)
	} else {
		if len(groups) == 0 || groups[0] != "default" {
			t.Errorf("GetUserGroups should return ['default'], got %v", groups)
		}
	}

	// CreateAPIToken
	created, err := a.CreateAPIToken(ctx, "http://127.0.0.1:1", "token", nil, nil, nil)
	if err != nil {
		t.Errorf("CreateAPIToken error: %v", err)
	}
	if created {
		t.Error("CreateAPIToken on unreachable should return false")
	}

	// DeleteAPIToken
	err = a.DeleteAPIToken(ctx, "http://127.0.0.1:1", "token", "sk-test", nil, nil)
	if err != nil {
		t.Errorf("DeleteAPIToken error: %v", err)
	}
}

func TestAnyRouterAdapter_UserIDHeaders(t *testing.T) {
	// AnyRouter inherits NewApi's 7-header injection
	a := &AnyRouterAdapter{
		NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")},
	}

	id := 99
	h := a.userIDHeaders(&id)
	if len(h) != 7 {
		t.Fatalf("expected 7 headers, got %d: %v", len(h), h)
	}
}
