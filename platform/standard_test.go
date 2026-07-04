package platform

import (
	"context"
	"testing"
)

// --- StandardAdapter defaults ---

func TestStandardAdapterDefaults(t *testing.T) {
	s := NewStandardAdapter("test-std")

	if s.PlatformName() != "test-std" {
		t.Errorf("expected PlatformName=%q, got %q", "test-std", s.PlatformName())
	}

	ctx := context.Background()

	// Login returns unsupported
	lr, err := s.Login(ctx, "http://x", "u", "p", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if lr.Success {
		t.Error("StandardAdapter.Login should return Success=false")
	}
	if lr.Message != "login endpoint not supported" {
		t.Errorf("Login message: %q", lr.Message)
	}

	// GetUserInfo returns nil
	ui, err := s.GetUserInfo(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ui != nil {
		t.Error("StandardAdapter.GetUserInfo should return nil")
	}

	// Checkin returns unsupported
	cr, err := s.Checkin(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cr.Success {
		t.Error("StandardAdapter.Checkin should return Success=false")
	}

	// GetBalance returns zeros
	bi, err := s.GetBalance(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if bi.Balance != 0 || bi.Used != 0 || bi.Quota != 0 {
		t.Error("StandardAdapter.GetBalance should return all zeros")
	}
}

func TestStandardAdapterCustomMessages(t *testing.T) {
	s := &StandardAdapter{
		BaseAdapter:               NewBaseAdapter("custom"),
		LoginUnsupportedMessage:   "no login",
		CheckinUnsupportedMessage: "no checkin",
	}

	ctx := context.Background()
	lr, _ := s.Login(ctx, "http://x", "u", "p", nil, nil)
	if lr.Message != "no login" {
		t.Errorf("custom login message: %q", lr.Message)
	}

	cr, _ := s.Checkin(ctx, "http://x", "t", nil, nil)
	if cr.Message != "no checkin" {
		t.Errorf("custom checkin message: %q", cr.Message)
	}
}

// --- URL normalization ---

func TestNormalizePlatformBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/", "http://example.com"},
		{"http://example.com", "http://example.com"},
		{"http://example.com///", "http://example.com"},
	}
	for _, tt := range tests {
		if got := normalizePlatformBaseURL(tt.input); got != tt.expected {
			t.Errorf("normalizePlatformBaseURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestResolveVersionedModelsURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/v1", "http://example.com/v1/models"},
		{"http://example.com/v1beta", "http://example.com/v1beta/models"},
		{"http://example.com/v1.0", "http://example.com/v1.0/models"},
		{"http://example.com", "http://example.com/v1/models"},
		{"http://example.com/other", "http://example.com/other/v1/models"},
		{"http://example.com/other/v1", "http://example.com/other/v1/models"},
	}
	for _, tt := range tests {
		if got := resolveVersionedModelsURL(tt.input); got != tt.expected {
			t.Errorf("resolveVersionedModelsURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/path", "http://example.com"},
		{"http://example.com", "http://example.com"},
		{"http://example.com/", "http://example.com"},
		{"http://example.com:8080/path", "http://example.com:8080"},
		{"example.com", "example.com"}, // no scheme, treated as opaque
	}
	for _, tt := range tests {
		if got := normalizeBaseURL(tt.input); got != tt.expected {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeURLForDetection(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com/path/", "https://example.com/path"},
		{"http://example.com/path/", "http://example.com/path"},
		{"https://example.com", "https://example.com"},
	}
	for _, tt := range tests {
		if got := normalizeURLForDetection(tt.input); got != tt.expected {
			t.Errorf("normalizeURLForDetection(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeURLProtocol(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "https://example.com"},
		{"http://example.com/path", "http://example.com"},
		{"https://example.com/path/", "https://example.com"},
	}
	for _, tt := range tests {
		if got := normalizeURLProtocol(tt.input); got != tt.expected {
			t.Errorf("normalizeURLProtocol(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestVersionedPathRE(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"/v1", true},
		{"/v1beta", true},
		{"/v1.5", true},
		{"/v123", true},
		{"/v1.0beta", true},
		{"/api/v1", true}, // matches because ends with /v1
		{"/other", false},
		{"/v1/models", false},
	}
	for _, tt := range tests {
		if got := versionedPathRE.MatchString(tt.input); got != tt.match {
			t.Errorf("versionedPathRE.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
		}
	}
}

// --- Integration: StandardAdapter embedding ---

func TestStandardAdapterIsBaseAdapter(t *testing.T) {
	s := NewStandardAdapter("test")
	// StandardAdapter should have BaseAdapter embedded
	if s.BaseAdapter == nil {
		t.Error("StandardAdapter.BaseAdapter should not be nil")
	}
	if s.BaseAdapter.name != "test" {
		t.Errorf("base name: %q", s.BaseAdapter.name)
	}
}
