package platform

import (
	"context"
	"testing"
)

func TestCliProxyApiAdapter_Detect(t *testing.T) {
	a := &CliProxyApiAdapter{StandardAdapter: &StandardAdapter{
		BaseAdapter:               NewBaseAdapter("cliproxyapi"),
		LoginUnsupportedMessage:   "CLIProxyAPI does not support login",
		CheckinUnsupportedMessage: "CLIProxyAPI does not support checkin",
	}}

	ctx := context.Background()

	// Keyword tests (no HTTP needed)
	tests := []struct {
		url     string
		matches bool
	}{
		{"http://127.0.0.1:8317/", true},
		{"http://localhost:8317/v1/models", true},
		{"https://cliproxy.example.com", true},
		{"https://CLIPROXY.example.com", true},
		{"https://cli-proxy-api.example.com", false}, // does not contain "cliproxy" substring
		{"https://api.openai.com", false},
		{"https://example.com:8080", false},
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

func TestCliProxyApiAdapter_PlatformName(t *testing.T) {
	a := &CliProxyApiAdapter{StandardAdapter: &StandardAdapter{
		BaseAdapter:               NewBaseAdapter("cliproxyapi"),
		LoginUnsupportedMessage:   "msg",
		CheckinUnsupportedMessage: "msg",
	}}
	if a.PlatformName() != "cliproxyapi" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestCliProxyApiAdapter_CustomMessages(t *testing.T) {
	a := &CliProxyApiAdapter{StandardAdapter: &StandardAdapter{
		BaseAdapter:               NewBaseAdapter("cliproxyapi"),
		LoginUnsupportedMessage:   "CLIProxyAPI does not support login",
		CheckinUnsupportedMessage: "CLIProxyAPI does not support checkin",
	}}
	ctx := context.Background()

	lr, _ := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if lr.Message != "CLIProxyAPI does not support login" {
		t.Errorf("Login message: %q", lr.Message)
	}

	cr, _ := a.Checkin(ctx, "http://x", "t", nil, nil)
	if cr.Message != "CLIProxyAPI does not support checkin" {
		t.Errorf("Checkin message: %q", cr.Message)
	}
}

func TestCliProxyApiAdapter_DetectPort8317(t *testing.T) {
	a := &CliProxyApiAdapter{StandardAdapter: &StandardAdapter{
		BaseAdapter: NewBaseAdapter("cliproxyapi"),
	}}
	ctx := context.Background()

	// Port 8317 should match immediately (no HTTP call)
	ok, err := a.Detect(ctx, "http://192.168.1.1:8317/v1/models")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("should match port 8317")
	}

	// Needs trailing slash or end
	ok2, _ := a.Detect(ctx, "http://192.168.1.1:8317")
	if !ok2 {
		t.Error("should match port 8317 without path")
	}
}
