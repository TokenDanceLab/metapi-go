package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	// Checkin/GetBalance may probe many user-id candidates against an unreachable host.
	// Bound the whole test so dial timeouts cannot blow past pre-push (-timeout 120s).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// AnyRouter inherits NewAPI-compatible session methods including cookie fallback,
	// shield challenge, and user-ID probing.
	// All HTTP-based methods should gracefully handle unreachable URLs

	// Login
	lr, err := a.Login(ctx, unreachableBaseURL(t), "user", "pass", nil, nil)
	if err != nil {
		t.Errorf("Login error: %v", err)
	}
	if lr.Success {
		t.Error("Login on unreachable URL should fail")
	}

	// Checkin
	cr, err := a.Checkin(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		t.Errorf("Checkin error: %v", err)
	}
	if cr.Success {
		t.Error("Checkin on unreachable URL should fail")
	}

	// GetBalance
	bi, err := a.GetBalance(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err == nil {
		// GetBalance returns error on total failure (different from other adapters)
		if bi != nil && bi.Balance > 0 {
			t.Error("Balance should be 0 on unreachable URL")
		}
	}

	// GetModels
	models, err := a.GetModels(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		t.Errorf("GetModels error: %v", err)
	}
	if len(models) != 0 {
		t.Error("GetModels on unreachable should return empty")
	}

	// GetUserGroups - may return error or default on unreachable URL
	groups, err := a.GetUserGroups(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		// Error is acceptable on unreachable URL (terminalError propagation)
		t.Logf("GetUserGroups error (expected on unreachable): %v", err)
	} else {
		if len(groups) == 0 || groups[0] != "default" {
			t.Errorf("GetUserGroups should return ['default'], got %v", groups)
		}
	}

	assertAnyRouterTokenManagementUnsupported(t, a)
}

func TestAnyRouterAdapter_TokenManagementDoesNotCallNewApiEndpoints(t *testing.T) {
	a := &AnyRouterAdapter{
		NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")},
	}

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer server.Close()

	assertAnyRouterTokenManagementUnsupported(t, a, server.URL)
	if calls != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
}

func assertAnyRouterTokenManagementUnsupported(t *testing.T, a *AnyRouterAdapter, urls ...string) {
	t.Helper()
	ctx := context.Background()
	baseURL := unreachableBaseURL(t)
	if len(urls) > 0 {
		baseURL = urls[0]
	}

	token, err := a.GetAPIToken(ctx, baseURL, "token", nil, nil)
	if err != nil {
		t.Fatalf("GetAPIToken error: %v", err)
	}
	if token != nil {
		t.Fatalf("GetAPIToken = %v, want nil", *token)
	}

	tokens, err := a.GetAPITokens(ctx, baseURL, "token", nil, nil)
	if err != nil {
		t.Fatalf("GetAPITokens error: %v", err)
	}
	if len(tokens) != 0 {
		t.Fatalf("GetAPITokens = %#v, want empty", tokens)
	}

	created, err := a.CreateAPIToken(ctx, baseURL, "token", nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken error: %v", err)
	}
	if created {
		t.Fatal("CreateAPIToken = true, want false")
	}

	if err := a.DeleteAPIToken(ctx, baseURL, "token", "test-key", nil, nil); err != nil {
		t.Fatalf("DeleteAPIToken error: %v", err)
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
