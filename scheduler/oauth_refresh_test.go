package scheduler

import (
	"testing"
)

func TestNewOAuthRefreshScheduler(t *testing.T) {
	s := NewOAuthRefreshScheduler(nil)
	if s.Name() != "oauth-refresh" {
		t.Fatalf("expected name 'oauth-refresh', got %q", s.Name())
	}
}

func TestOAuthRefreshScheduler_Stop_NoPanic(t *testing.T) {
	s := NewOAuthRefreshScheduler(nil)
	if err := s.Stop(); err != nil {
		t.Fatalf("expected no error on stop before start, got %v", err)
	}
}

func TestOAuthRefreshResult(t *testing.T) {
	r := &OAuthRefreshResult{
		Scanned:             5,
		Refreshed:           2,
		Failed:              1,
		Skipped:             2,
		RefreshedAccountIDs: []int64{1, 2},
		FailedAccountIDs:    []int64{3},
	}
	if r.Scanned != 5 || r.Refreshed != 2 {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestOAuthRefreshScheduler_StartStopNoPanic(t *testing.T) {
	s := NewOAuthRefreshScheduler(nil)
	if err := s.Stop(); err != nil {
		t.Fatalf("expected no error on stop before start, got %v", err)
	}
	// Double stop should not panic.
	if err := s.Stop(); err != nil {
		t.Fatalf("expected no error on double stop, got %v", err)
	}
}

func TestGetOauthRefreshLeadMs(t *testing.T) {
	tests := []struct {
		provider string
		want     int64
	}{
		{"codex", 5 * 24 * 60 * 60 * 1000},
		{"claude", 4 * 60 * 60 * 1000},
		{"gemini-cli", 5 * 60 * 1000},
		{"antigravity", 5 * 60 * 1000},
		{"unknown-provider", defaultOauthRefreshLeadMs},
		{"", defaultOauthRefreshLeadMs},
	}
	for _, tt := range tests {
		got := getOauthRefreshLeadMs(tt.provider)
		if got != tt.want {
			t.Errorf("getOauthRefreshLeadMs(%q) = %d, want %d", tt.provider, got, tt.want)
		}
	}
}
