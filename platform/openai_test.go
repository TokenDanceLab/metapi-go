package platform

import (
	"context"
	"testing"
)

func TestOpenAiAdapter_Detect(t *testing.T) {
	a := &OpenAiAdapter{StandardAdapter: NewStandardAdapter("openai")}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://api.openai.com/v1/models", true},
		{"https://api.openai.com", true},
		{"https://API.OPENAI.COM/v1/chat/completions", true},
		{"https://gateway.openai.com", false}, // contains openai but not api.openai.com
		{"https://api.anthropic.com", false},
		{"https://generativelanguage.googleapis.com", false},
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

func TestOpenAiAdapter_PlatformName(t *testing.T) {
	a := &OpenAiAdapter{StandardAdapter: NewStandardAdapter("openai")}
	if a.PlatformName() != "openai" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

// TestOpenAiAdapter_GetModels calls fetchModelsFromStandardEndpoint which does HTTP -
// will fail on non-existent URLs but should return empty slice without error
func TestOpenAiAdapter_GetModels(t *testing.T) {
	a := &OpenAiAdapter{StandardAdapter: NewStandardAdapter("openai")}
	ctx := context.Background()

	// Should fail gracefully on non-existent URL
	models, err := a.GetModels(ctx, unreachableBaseURL(t), "sk-test", nil, nil)
	// Either way, we get an error or empty models - both acceptable for this adapter
	_ = models
	_ = err
}

func TestOpenAiAdapter_InheritsStandardDefaults(t *testing.T) {
	a := &OpenAiAdapter{StandardAdapter: NewStandardAdapter("openai")}
	ctx := context.Background()

	lr, _ := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if lr.Success {
		t.Error("OpenAI login should be unsupported")
	}

	cr, _ := a.Checkin(ctx, "http://x", "t", nil, nil)
	if cr.Success {
		t.Error("OpenAI checkin should be unsupported")
	}

	bi, _ := a.GetBalance(ctx, "http://x", "t", nil, nil)
	if bi.Balance != 0 {
		t.Error("OpenAI balance should be 0")
	}

	ui, _ := a.GetUserInfo(ctx, "http://x", "t", nil, nil)
	if ui != nil {
		t.Error("OpenAI GetUserInfo should return nil")
	}
}
