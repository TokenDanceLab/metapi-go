package platform

import (
	"context"
	"testing"
)

func TestClaudeAdapter_Detect(t *testing.T) {
	a := &ClaudeAdapter{StandardAdapter: NewStandardAdapter("claude")}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://api.anthropic.com/v1/messages", true},
		{"https://api.anthropic.com", true},
		{"https://anthropic.com/v1/messages", true},
		{"https://api.openai.com", false},
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

func TestClaudeAdapter_PlatformName(t *testing.T) {
	a := &ClaudeAdapter{StandardAdapter: NewStandardAdapter("claude")}
	if a.PlatformName() != "claude" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestResolveOpenAICompatibleBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://gateway.example.com/anthropic", "https://gateway.example.com"},
		{"https://gateway.example.com/Anthropic", "https://gateway.example.com"},
		{"https://gateway.example.com/v1", ""},
		{"https://gateway.example.com", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := resolveOpenAICompatibleBaseURL(tt.input); got != tt.expected {
			t.Errorf("resolveOpenAICompatibleBaseURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestClaudeAdapter_InheritsStandardDefaults(t *testing.T) {
	a := &ClaudeAdapter{StandardAdapter: NewStandardAdapter("claude")}
	ctx := context.Background()

	lr, _ := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if lr.Success {
		t.Error("Claude login should be unsupported")
	}

	bi, _ := a.GetBalance(ctx, "http://x", "t", nil, nil)
	if bi.Balance != 0 {
		t.Error("Claude balance should be 0")
	}
}

func TestClaudeDefaultAnthropicVersion(t *testing.T) {
	if claudeDefaultAnthropicVersion != "2023-06-01" {
		t.Errorf("expected anthropic version '2023-06-01', got %q", claudeDefaultAnthropicVersion)
	}
}
