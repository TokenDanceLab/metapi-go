package platform

import (
	"context"
	"testing"
)

func TestGeminiAdapter_Detect(t *testing.T) {
	a := &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini")}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://generativelanguage.googleapis.com/v1beta/models", true},
		{"https://us-central1-aiplatform.googleapis.com/v1beta/openai/models", true},
		{"https://gemini.google.com", true},
		{"https://api.openai.com", false},
		{"https://api.anthropic.com", false},
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

func TestGeminiAdapter_PlatformName(t *testing.T) {
	a := &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini")}
	if a.PlatformName() != "gemini" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestIsOpenAICompatGeminiBase(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://example.com/openai/", true},
		{"https://example.com/openai", true},
		{"https://example.com/OPENAI/", true},
		{"https://example.com/v1beta/openai", true},
		{"https://example.com/v1/models", false},
		{"https://example.com", false},
	}
	for _, tt := range tests {
		if got := isOpenAICompatGeminiBase(tt.input); got != tt.expected {
			t.Errorf("isOpenAICompatGeminiBase(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestResolveGeminiNativeModelsURL(t *testing.T) {
	u := resolveGeminiNativeModelsURL("https://generativelanguage.googleapis.com", "apikey123")
	expected := "https://generativelanguage.googleapis.com/v1beta/models?key=apikey123"
	if u != expected {
		t.Errorf("resolveGeminiNativeModelsURL: %q, want %q", u, expected)
	}

	// Already has /v1beta
	u2 := resolveGeminiNativeModelsURL("https://generativelanguage.googleapis.com/v1beta", "key")
	expected2 := "https://generativelanguage.googleapis.com/v1beta/models?key=key"
	if u2 != expected2 {
		t.Errorf("resolveGeminiNativeModelsURL (has v1beta): %q, want %q", u2, expected2)
	}

	// Already has /models
	u3 := resolveGeminiNativeModelsURL("https://generativelanguage.googleapis.com/v1beta/models", "key")
	expected3 := "https://generativelanguage.googleapis.com/v1beta/models?key=key"
	if u3 != expected3 {
		t.Errorf("resolveGeminiNativeModelsURL (has models): %q, want %q", u3, expected3)
	}
}

func TestStripModelPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"models/gemini-pro", "gemini-pro"},
		{"gemini-pro", "gemini-pro"},
		{"  models/gpt-4  ", "gpt-4"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := stripModelPrefix(tt.input); got != tt.expected {
			t.Errorf("stripModelPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeModelList(t *testing.T) {
	models := []string{"models/gemini-pro", "gemini-pro", "models/gpt-4"}
	result := normalizeModelList(models)
	if len(result) != 2 {
		t.Fatalf("expected 2 unique models, got %d: %v", len(result), result)
	}
	if result[0] != "gemini-pro" {
		t.Errorf("first model: %q", result[0])
	}
	if result[1] != "gpt-4" {
		t.Errorf("second model: %q", result[1])
	}
}

func TestGeminiAdapter_InheritsStandardDefaults(t *testing.T) {
	a := &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini")}
	ctx := context.Background()

	lr, _ := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if lr.Success {
		t.Error("Gemini login should be unsupported")
	}

	bi, _ := a.GetBalance(ctx, "http://x", "t", nil, nil)
	if bi.Balance != 0 {
		t.Error("Gemini balance should be 0")
	}
}
