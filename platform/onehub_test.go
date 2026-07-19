package platform

import (
	"context"
	"testing"
)

func TestOneHubAdapter_PlatformName(t *testing.T) {
	o := &OneHubAdapter{
		OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-hub")},
	}
	if o.PlatformName() != "one-hub" {
		t.Errorf("PlatformName: %q", o.PlatformName())
	}
}

func TestOneHubAdapter_Detect(t *testing.T) {
	o := &OneHubAdapter{
		OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-hub")},
	}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://onehub.example.com", true},
		{"https://ONEHUB.example.com", true},
		{"https://one-hub.example.com", true},
		{"https://ONE-HUB.example.com", true},
		{"https://oneapi.example.com", false},
		{"https://newapi.example.com", false},
	}
	for _, tt := range tests {
		ok, err := o.Detect(ctx, tt.url)
		if err != nil {
			t.Errorf("Detect(%q) error: %v", tt.url, err)
			continue
		}
		if ok != tt.matches {
			t.Errorf("Detect(%q) = %v, want %v", tt.url, ok, tt.matches)
		}
	}
}

func TestOneHubAdapter_GetModels_Fallback(t *testing.T) {
	o := &OneHubAdapter{
		OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-hub")},
	}
	ctx := context.Background()

	// On unreachable URL, /v1/models fails, /api/available_model fails
	// Should return empty slice without error
	models, err := o.GetModels(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Error("GetModels should return empty for unreachable URL")
	}
}

func TestOneHubAdapter_GetUserGroups_Fallback(t *testing.T) {
	o := &OneHubAdapter{
		OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-hub")},
	}
	ctx := context.Background()

	// On unreachable URL, may error or fall back to ["default"]
	_, err := o.GetUserGroups(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		t.Logf("GetUserGroups error on unreachable (expected): %v", err)
	}
}
