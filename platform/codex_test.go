package platform

import (
	"context"
	"testing"
)

func TestCodexAdapter_Detect(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://chatgpt.com/backend-api/codex/models", true},
		{"https://chatgpt.com/backend-api/codex", true},
		{"https://chatgpt.com/backend-api/codex/", true},
		{"https://api.openai.com", false},
		{"https://chatgpt.com/backend-api/other", false},
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

func TestCodexAdapter_PlatformName(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	if a.PlatformName() != "codex" {
		t.Errorf("PlatformName: %q", a.PlatformName())
	}
}

func TestCodexAdapter_LoginUnspported(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	ctx := context.Background()

	lr, err := a.Login(ctx, "http://x", "u", "p", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if lr.Success {
		t.Error("Codex login should return Success=false")
	}
	if lr.Message != "codex oauth login is managed via OAuth flow" {
		t.Errorf("Login message: %q", lr.Message)
	}
}

func TestCodexAdapter_CheckinUnspported(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	ctx := context.Background()

	cr, err := a.Checkin(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cr.Success {
		t.Error("Codex checkin should return Success=false")
	}
	if cr.Message != "codex oauth connections do not support checkin" {
		t.Errorf("Checkin message: %q", cr.Message)
	}
}

func TestCodexAdapter_GetModelsEmpty(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	ctx := context.Background()

	models, err := a.GetModels(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Error("Codex GetModels should return empty slice")
	}
}

func TestCodexAdapter_GetBalanceZero(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	ctx := context.Background()

	bi, err := a.GetBalance(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if bi.Balance != 0 || bi.Used != 0 || bi.Quota != 0 {
		t.Error("Codex balance should be all zeros")
	}
}

func TestCodexAdapter_GetUserInfoNil(t *testing.T) {
	a := &CodexAdapter{BaseAdapter: NewBaseAdapter("codex")}
	ctx := context.Background()

	ui, err := a.GetUserInfo(ctx, "http://x", "t", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ui != nil {
		t.Error("Codex GetUserInfo should return nil")
	}
}
