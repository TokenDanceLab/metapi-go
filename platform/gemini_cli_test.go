package platform

import (
	"context"
	"testing"
)

func TestGeminiCliAdapter_Detect(t *testing.T) {
	a := &GeminiCliAdapter{
		GeminiAdapter: &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini-cli")},
	}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://cloudcode-pa.googleapis.com/v1/models", true},
		{"https://cloudcode-pa.googleapis.com", true},
		{"https://CLOUDCODE-PA.GOOGLEAPIS.COM/v1", true},
		{"https://generativelanguage.googleapis.com", false}, // GeminiAdapter would match, but GeminiCli overrides
		{"https://api.openai.com", false},
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

func TestGeminiCliAdapter_PlatformName(t *testing.T) {
	a := &GeminiCliAdapter{
		GeminiAdapter: &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini-cli")},
	}
	if a.PlatformName() != "gemini-cli" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestGeminiCliAdapter_DoesNotMatchGenericGemini(t *testing.T) {
	// GeminiCliAdapter should NOT match generativelanguage.googleapis.com (only GeminiAdapter does)
	a := &GeminiCliAdapter{
		GeminiAdapter: &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini-cli")},
	}

	ctx := context.Background()
	ok, err := a.Detect(ctx, "https://generativelanguage.googleapis.com/v1/models")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ok {
		t.Error("GeminiCliAdapter should NOT match generativelanguage.googleapis.com")
	}
}

func TestGeminiCliAdapter_InheritsGeminiDefaults(t *testing.T) {
	a := &GeminiCliAdapter{
		GeminiAdapter: &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini-cli")},
	}
	ctx := context.Background()

	lr, _ := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if lr.Success {
		t.Error("GeminiCli login should be unsupported (inherits from StandardAdapter)")
	}

	bi, _ := a.GetBalance(ctx, "http://x", "t", nil, nil)
	if bi.Balance != 0 {
		t.Error("GeminiCli balance should be 0")
	}
}
