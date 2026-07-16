package proxy

import (
	"context"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func makeSurfaceSelectedChannel(channelID int64) SurfaceSelectedChannel {
	return SurfaceSelectedChannel{
		Channel: SurfaceChannelRef{
			RouteID: int64Ptr(1),
			ID:      channelID,
		},
		Account: SurfaceAccountRef{
			ID:          100,
			ExtraConfig:  strPtr(`{"credentialMode":"session"}`),
			OAuthProvider: strPtr("claude"),
		},
		Site: SurfaceSiteRef{
			Name:     strPtr("test-site"),
			ID:       1,
			URL:      "https://api.example.com",
			Platform: "openai",
		},
		ActualModel: "gpt-4",
	}
}

func strPtr(s string) *string { return &s }

type toolkitState struct {
	logs   *[]ProxyLogEntry
	alerts *[]string
	router *mockRouter
}

func makeToolkit(maxRetries int) (*SurfaceFailureToolkit, *toolkitState) {
	router := &mockRouter{}
	logs := make([]ProxyLogEntry, 0)
	alerts := make([]string, 0)
	state := &toolkitState{logs: &logs, alerts: &alerts, router: router}

	toolkit := NewSurfaceFailureToolkit(
		ScopeChat,
		"/v1/chat/completions",
		maxRetries,
		router,
		nil,
		func(ctx context.Context, entry ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
		func(model string, reason string) {
			alerts = append(alerts, model+": "+reason)
		},
		nil,
	)
	return toolkit, state
}

func TestConvertToSurfaceSelectedChannel(t *testing.T) {
	sel := routing.SelectedChannel{
		ActualModel: "gpt-4o",
		Channel: store.RouteChannel{
			ID:      42,
			RouteID: 10,
		},
		Account: store.Account{
			ID:           100,
			Username:     strPtr("test-user"),
			ExtraConfig:  strPtr(`{"credentialMode":"session"}`),
			OAuthProvider: strPtr("claude"),
		},
		Site: store.Site{
			ID:       1,
			Name:     "test-site",
			URL:      "https://api.openai.com",
			Platform: "openai",
		},
	}

	surface := ConvertToSurfaceSelectedChannel(&sel)

	if surface.Channel.ID != 42 {
		t.Errorf("expected channel 42, got %d", surface.Channel.ID)
	}
	if *surface.Channel.RouteID != 10 {
		t.Errorf("expected route 10, got %d", *surface.Channel.RouteID)
	}
	if surface.Account.ID != 100 {
		t.Errorf("expected account 100, got %d", surface.Account.ID)
	}
	if *surface.Account.Username != "test-user" {
		t.Errorf("expected username test-user, got %s", *surface.Account.Username)
	}
	if surface.Site.Platform != "openai" {
		t.Errorf("expected platform openai, got %s", surface.Site.Platform)
	}
	if surface.ActualModel != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", surface.ActualModel)
	}
}

func TestHandleUpstreamFailure_TokenExpiredMarkGuard(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		errText    string
		rawErrText string
		wantMark   bool
	}{
		{
			name:       "jwt expired marks",
			status:     401,
			errText:    "jwt expired",
			rawErrText: "jwt expired",
			wantMark:   true,
		},
		{
			name:       "bare 401 empty body marks",
			status:     401,
			errText:    "",
			rawErrText: "",
			wantMark:   true,
		},
		{
			name:       "billing 401 does not mark",
			status:     401,
			errText:    "No payment method. Add a payment method here: https://example.com/billing",
			rawErrText: "No payment method. Add a payment method here: https://example.com/billing",
			wantMark:   false,
		},
		{
			name:       "model unsupported 401 does not mark",
			status:     401,
			errText:    "Model foo is not supported for format openai",
			rawErrText: "Model foo is not supported for format openai",
			wantMark:   false,
		},
		{
			name:       "validation 400 does not mark",
			status:     400,
			errText:    "invalid_argument: input token limit is 202752",
			rawErrText: "invalid_argument: input token limit is 202752",
			wantMark:   false,
		},
		{
			name:       "rate limit does not mark",
			status:     429,
			errText:    "rate limit exceeded",
			rawErrText: "rate limit exceeded",
			wantMark:   false,
		},
		{
			name:       "opaque 401 does not mark",
			status:     401,
			errText:    "upstream rejected the request",
			rawErrText: "upstream rejected the request",
			wantMark:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var marked []string
			toolkit, _ := makeToolkit(0)
			toolkit.ReportTokenExpired = func(accountID int64, username *string, siteName *string, detail string) {
				marked = append(marked, detail)
			}
			selected := makeSurfaceSelectedChannel(7)
			username := "acct"
			selected.Account.Username = &username

			_ = toolkit.HandleUpstreamFailure(
				context.Background(), selected, "gpt-4", "gpt-4",
				tc.status, tc.errText, tc.rawErrText,
				false, 100, 0,
			)

			if tc.wantMark && len(marked) != 1 {
				t.Fatalf("expected mark, got %v", marked)
			}
			if !tc.wantMark && len(marked) != 0 {
				t.Fatalf("expected no mark, got %v", marked)
			}
		})
	}
}

func TestHandleUpstreamFailure(t *testing.T) {
	t.Run("retryable failure within maxRetries", func(t *testing.T) {
		toolkit, state := makeToolkit(2)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleUpstreamFailure(
			context.Background(), selected, "gpt-4", "gpt-4",
			503, "service unavailable", "service unavailable body",
			false, 1000, 0,
		)

		if resp.Action != "retry" {
			t.Errorf("expected 'retry' action, got %q", resp.Action)
		}
		if len(*state.logs) != 1 {
			t.Errorf("expected 1 log entry, got %d", len(*state.logs))
		}
		if len(*state.logs) >= 1 && (*state.logs)[0].Status != "failed" {
			t.Errorf("expected log status 'failed', got %q", (*state.logs)[0].Status)
		}
		if len(*state.alerts) != 0 {
			t.Error("expected no alerts for retryable failure within retries")
		}
	})

	t.Run("retryable failure exceeded maxRetries", func(t *testing.T) {
		toolkit, state := makeToolkit(1)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleUpstreamFailure(
			context.Background(), selected, "gpt-4", "gpt-4",
			503, "service unavailable", "body",
			false, 1000, 2,
		)

		if resp.Action != "respond" {
			t.Errorf("expected 'respond' action, got %q", resp.Action)
		}
		if len(*state.logs) != 1 {
			t.Errorf("expected 1 log entry, got %d", len(*state.logs))
		}
		if len(*state.alerts) != 1 {
			t.Error("expected alert for all-failed")
		}
	})

	t.Run("non-retryable failure", func(t *testing.T) {
		toolkit, state := makeToolkit(3)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleUpstreamFailure(
			context.Background(), selected, "gpt-4", "gpt-4",
			400, "invalid request body", "body",
			false, 500, 0,
		)

		if resp.Action != "respond" {
			t.Errorf("expected 'respond' action, got %q", resp.Action)
		}
		if resp.Status != 400 {
			t.Errorf("expected status 400, got %d", resp.Status)
		}
		if len(*state.alerts) == 0 {
			t.Error("expected alert for non-retryable failure")
		}
	})
}

func TestHandleDetectedFailure(t *testing.T) {
	t.Run("content-based failure retryable", func(t *testing.T) {
		toolkit, state := makeToolkit(2)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleDetectedFailure(
			context.Background(), selected, "gpt-4", "gpt-4",
			&FailureResult{Status: 502, Reason: "Upstream returned empty content"},
			800, 0, 100, 0, 100,
			"/v1/chat/completions",
		)

		if resp.Action != "retry" {
			t.Errorf("expected 'retry', got %q", resp.Action)
		}
		if len(*state.logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(*state.logs))
		}
	})

	t.Run("content-based failure non-retryable", func(t *testing.T) {
		toolkit, state := makeToolkit(1)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleDetectedFailure(
			context.Background(), selected, "gpt-4", "gpt-4",
			&FailureResult{Status: 502, Reason: "some generic failure"},
			800, 1, 100, 50, 150,
			"/v1/chat/completions",
		)

		if resp.Action != "respond" {
			t.Errorf("expected 'respond', got %q", resp.Action)
		}
		if len(*state.logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(*state.logs))
		}
		if len(*state.alerts) != 1 {
			t.Error("expected alert")
		}
	})
}

func TestHandleExecutionError(t *testing.T) {
	t.Run("execution error retryable", func(t *testing.T) {
		toolkit, state := makeToolkit(2)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleExecutionError(
			context.Background(), selected, "gpt-4", "gpt-4",
			"connection refused", 0, 0,
		)

		if resp.Action != "retry" {
			t.Errorf("expected 'retry' for execution error, got %q", resp.Action)
		}
		if len(*state.logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(*state.logs))
		}
	})

	t.Run("execution error exceeds max retries", func(t *testing.T) {
		toolkit, state := makeToolkit(1)

		selected := makeSurfaceSelectedChannel(1)
		resp := toolkit.HandleExecutionError(
			context.Background(), selected, "gpt-4", "gpt-4",
			"network timeout", 0, 1,
		)

		if resp.Action != "respond" {
			t.Errorf("expected 'respond', got %q", resp.Action)
		}
		if resp.Status != 502 {
			t.Errorf("expected status 502, got %d", resp.Status)
		}
		if len(*state.alerts) != 1 {
			t.Error("expected alert")
		}
		if len(*state.logs) != 1 {
			t.Errorf("expected 1 log, got %d", len(*state.logs))
		}
	})
}

func TestStickySessionSurfaceHelpers(t *testing.T) {
	cfg := config.Load(map[string]string{
		"PORT":                            "8080",
		"PROXY_STICKY_SESSION_ENABLED":     "true",
		"PROXY_STICKY_SESSION_TTL_MS":      "60000",
		"PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT": "0",
	})
	config.Set(cfg)
	coord := NewProxyChannelCoordinator(cfg)

	t.Run("build surface sticky session key", func(t *testing.T) {
		key := BuildSurfaceStickySessionKey(coord, "codex", "sess-1", "gpt-4", "/v1/chat/completions", int64Ptr(1))
		if key == "" {
			t.Fatal("expected non-empty key")
		}
	})

	t.Run("bind and get surface sticky channel", func(t *testing.T) {
		ec, op := sessionScopedConfig()
		key := "test-surface-key"

		BindSurfaceStickyChannel(coord, key, 42, ec, op)
		id := GetSurfaceStickyPreferredChannelID(coord, key)
		if id == nil || *id != 42 {
			t.Errorf("expected channel 42, got %v", id)
		}
	})

	t.Run("clear surface sticky channel", func(t *testing.T) {
		ec, op := sessionScopedConfig()
		key := "test-surface-clear"
		BindSurfaceStickyChannel(coord, key, 42, ec, op)
		ClearSurfaceStickyChannel(coord, key, 42)

		id := GetSurfaceStickyPreferredChannelID(coord, key)
		if id != nil {
			t.Errorf("expected nil after clear, got %v", *id)
		}
	})
}

func TestAcquireSurfaceChannelLease(t *testing.T) {
	cfg := config.Load(map[string]string{
		"PORT":                            "8080",
		"PROXY_STICKY_SESSION_ENABLED":     "true",
		"PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT": "0",
	})
	config.Set(cfg)
	coord := NewProxyChannelCoordinator(cfg)

	t.Run("empty sticky session key returns noop lease", func(t *testing.T) {
		ec, op := sessionScopedConfig()
		result := AcquireSurfaceChannelLease(coord, "", 42, ec, op)
		if result.Status != "acquired" {
			t.Errorf("expected 'acquired', got %q", result.Status)
		}
		if result.Lease.IsActive() {
			t.Error("expected noop (inactive) lease")
		}
	})

	t.Run("sticky session key triggers real lease acquisition", func(t *testing.T) {
		cfg2 := config.Load(map[string]string{
			"PORT":                            "8080",
			"PROXY_STICKY_SESSION_ENABLED":     "true",
			"PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT": "1",
			"PROXY_SESSION_CHANNEL_LEASE_TTL_MS": "30000",
		})
		config.Set(cfg2)
		coord2 := NewProxyChannelCoordinator(cfg2)
		ec, op := sessionScopedConfig()

		result := AcquireSurfaceChannelLease(coord2, "sticky-key", 42, ec, op)
		if result.Status != "acquired" {
			t.Errorf("expected 'acquired', got %q", result.Status)
		}
		if !result.Lease.IsActive() {
			t.Error("expected active tracked lease with sticky key")
		}
		result.Lease.Release()
	})
}

func TestBuildSurfaceChannelBusyMessage(t *testing.T) {
	msg := BuildSurfaceChannelBusyMessage(5000)
	if msg == "" {
		t.Error("expected non-empty message")
	}

	msg2 := BuildSurfaceChannelBusyMessage(0)
	if msg2 != "Channel busy: no session slot available" {
		t.Error("expected specific message for 0 wait")
	}
}

func TestRecordStreamFailure(t *testing.T) {
	logs := make([]ProxyLogEntry, 0)
	router := &mockRouter{}

	toolkit := NewSurfaceFailureToolkit(
		ScopeChat, "/v1/chat/completions", 2, router, nil,
		func(ctx context.Context, entry ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
		nil, nil,
	)

	selected := makeSurfaceSelectedChannel(1)
	failStatus := 502
	toolkit.RecordStreamFailure(
		context.Background(), selected, "gpt-4", "gpt-4",
		"stream connection lost", 5000, 0, 100, 50, 150,
		"/v1/chat/completions", 0, &failStatus,
	)

	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Status != "failed" {
		t.Errorf("expected status 'failed', got %q", logs[0].Status)
	}
}

func TestRecordStreamFailure_NoRuntimeStatus(t *testing.T) {
	logs := make([]ProxyLogEntry, 0)
	router := &mockRouter{}

	toolkit := NewSurfaceFailureToolkit(
		ScopeChat, "/v1/chat/completions", 2, router, nil,
		func(ctx context.Context, entry ProxyLogEntry) error {
			logs = append(logs, entry)
			return nil
		},
		nil, nil,
	)

	selected := makeSurfaceSelectedChannel(1)
	toolkit.RecordStreamFailure(
		context.Background(), selected, "gpt-4", "gpt-4",
		"stream error", 3000, 1, 200, 100, 300,
		"/v1/messages", 502, nil,
	)

	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].RetryCount != 1 {
		t.Errorf("expected retryCount 1, got %d", logs[0].RetryCount)
	}
}
