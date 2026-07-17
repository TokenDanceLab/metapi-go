package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

// ---- Mock types ----

type mockRouter struct {
	selectChannel          func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	selectNextChannel      func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error)
	selectPreferredChannel func(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error)
}

func (m *mockRouter) ExplainSelection(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error) {
	return routing.RouteDecisionExplanation{}, nil
}

func (m *mockRouter) SelectChannel(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	if m.selectChannel != nil {
		return m.selectChannel(ctx, requestedModel, policy)
	}
	return nil, nil
}

func (m *mockRouter) SelectNextChannel(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
	if m.selectNextChannel != nil {
		return m.selectNextChannel(ctx, requestedModel, excludeChannelIDs, policy)
	}
	return nil, nil
}

func (m *mockRouter) SelectPreferredChannel(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error) {
	if m.selectPreferredChannel != nil {
		return m.selectPreferredChannel(ctx, requestedModel, preferredChannelID, policy, excludeChannelIDs)
	}
	return nil, nil
}

func (m *mockRouter) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64, cost float64, modelName *string, actualAccountID *int64) error {
	return nil
}
func (m *mockRouter) RecordFailure(ctx context.Context, channelID int64, failureCtx routing.SiteRuntimeFailureContext, actualAccountID *int64) error {
	return nil
}

type mockRouteRefresher struct {
	called  bool
	refresh func(ctx context.Context) error
}

func (m *mockRouteRefresher) RefreshModelsAndRebuildRoutes(ctx context.Context) error {
	m.called = true
	if m.refresh != nil {
		return m.refresh(ctx)
	}
	return nil
}

// ---- Helpers ----

func makeChannel(channelID, routeID, accountID int64) routing.SelectedChannel {
	return routing.SelectedChannel{
		ActualModel: "gpt-4",
		Channel: store.RouteChannel{
			ID:      channelID,
			RouteID: routeID,
		},
		Account: store.Account{
			ID: accountID,
		},
		Site: store.Site{
			ID:       1,
			Name:     "test-site",
			URL:      "https://api.example.com",
			Platform: "openai",
		},
	}
}

func setupCfg() {
	cfg := config.Load(map[string]string{
		"PORT": "8080",
	})
	config.Set(cfg)
}

func sessionScopedExtraConfig() string {
	return `{"credentialMode":"session"}`
}

// ---- Tests ----

func TestTesterHelpers(t *testing.T) {
	t.Run("IsLoopbackClientIP", func(t *testing.T) {
		tests := []struct {
			ip       string
			expected bool
		}{
			{"127.0.0.1", true},
			{"::1", true},
			{"::ffff:127.0.0.1", true},
			{"192.168.1.1", false},
			{"10.0.0.1", false},
			{"", false},
			{"::ffff:192.168.1.1", false},
		}
		for _, tt := range tests {
			if got := IsLoopbackClientIP(tt.ip); got != tt.expected {
				t.Errorf("IsLoopbackClientIP(%q) = %v, want %v", tt.ip, got, tt.expected)
			}
		}
	})

	t.Run("IsTrustedTesterRequest", func(t *testing.T) {
		t.Run("loopback with correct header", func(t *testing.T) {
			headers := map[string]string{"x-metapi-tester-request": "1"}
			if !IsTrustedTesterRequest(headers, "127.0.0.1") {
				t.Error("expected trusted tester request")
			}
		})

		t.Run("loopback without header", func(t *testing.T) {
			headers := map[string]string{}
			if IsTrustedTesterRequest(headers, "127.0.0.1") {
				t.Error("expected NOT trusted (no header)")
			}
		})

		t.Run("non-loopback with header", func(t *testing.T) {
			headers := map[string]string{"x-metapi-tester-request": "1"}
			if IsTrustedTesterRequest(headers, "192.168.1.1") {
				t.Error("expected NOT trusted (non-loopback)")
			}
		})

		t.Run("loopback with wrong header value", func(t *testing.T) {
			headers := map[string]string{"x-metapi-tester-request": "0"}
			if IsTrustedTesterRequest(headers, "127.0.0.1") {
				t.Error("expected NOT trusted (header != 1)")
			}
		})

		t.Run("case insensitive header", func(t *testing.T) {
			headers := map[string]string{"X-Metapi-Tester-Request": "1"}
			if !IsTrustedTesterRequest(headers, "127.0.0.1") {
				t.Error("expected trusted (case-insensitive)")
			}
		})
	})

	t.Run("GetTesterForcedChannelID", func(t *testing.T) {
		t.Run("valid forced channel", func(t *testing.T) {
			headers := map[string]string{
				"x-metapi-tester-request":           "1",
				"x-metapi-tester-forced-channel-id": "42",
			}
			id := GetTesterForcedChannelID(headers, "127.0.0.1")
			if id == nil || *id != 42 {
				t.Errorf("expected channel 42, got %v", id)
			}
		})

		t.Run("invalid channel ID", func(t *testing.T) {
			headers := map[string]string{
				"x-metapi-tester-request":           "1",
				"x-metapi-tester-forced-channel-id": "abc",
			}
			id := GetTesterForcedChannelID(headers, "127.0.0.1")
			if id != nil {
				t.Errorf("expected nil for invalid ID, got %v", *id)
			}
		})

		t.Run("zero channel ID", func(t *testing.T) {
			headers := map[string]string{
				"x-metapi-tester-request":           "1",
				"x-metapi-tester-forced-channel-id": "0",
			}
			id := GetTesterForcedChannelID(headers, "127.0.0.1")
			if id != nil {
				t.Errorf("expected nil for zero ID, got %v", *id)
			}
		})

		t.Run("negative channel ID", func(t *testing.T) {
			headers := map[string]string{
				"x-metapi-tester-request":           "1",
				"x-metapi-tester-forced-channel-id": "-1",
			}
			id := GetTesterForcedChannelID(headers, "127.0.0.1")
			if id != nil {
				t.Errorf("expected nil for negative ID, got %v", *id)
			}
		})

		t.Run("non-loopback", func(t *testing.T) {
			headers := map[string]string{
				"x-metapi-tester-request":           "1",
				"x-metapi-tester-forced-channel-id": "42",
			}
			id := GetTesterForcedChannelID(headers, "192.168.1.1")
			if id != nil {
				t.Error("expected nil for non-loopback IP")
			}
		})

		t.Run("missing tester request header", func(t *testing.T) {
			headers := map[string]string{
				"x-metapi-tester-forced-channel-id": "42",
			}
			id := GetTesterForcedChannelID(headers, "127.0.0.1")
			if id != nil {
				t.Error("expected nil when tester request header missing")
			}
		})
	})

	t.Run("BuildForcedChannelUnavailableMessage", func(t *testing.T) {
		channelID := int64(42)
		msg := BuildForcedChannelUnavailableMessage(&channelID)
		if msg == "" {
			t.Error("expected non-empty message")
		}

		msgNil := BuildForcedChannelUnavailableMessage(nil)
		if msgNil != "No available channels for this model" {
			t.Errorf("unexpected message for nil: %s", msgNil)
		}

		zeroID := int64(0)
		msgZero := BuildForcedChannelUnavailableMessage(&zeroID)
		if msgZero != "No available channels for this model" {
			t.Errorf("unexpected message for zero: %s", msgZero)
		}
	})

	t.Run("CanRetryChannelSelection", func(t *testing.T) {
		t.Run("no forced channel, retries remaining", func(t *testing.T) {
			if !CanRetryChannelSelection(0, 2, nil) {
				t.Error("expected can retry with retries remaining")
			}
		})

		t.Run("no forced channel, no retries remaining", func(t *testing.T) {
			if CanRetryChannelSelection(2, 2, nil) {
				t.Error("expected cannot retry (maxRetries reached)")
			}
		})

		t.Run("forced channel set", func(t *testing.T) {
			fc := int64(42)
			if CanRetryChannelSelection(0, 2, &fc) {
				t.Error("expected cannot retry with forced channel")
			}
		})

		t.Run("forced channel zero", func(t *testing.T) {
			fc := int64(0)
			if !CanRetryChannelSelection(0, 2, &fc) {
				t.Error("expected can retry with zero forced channel (treated as no forced)")
			}
		})
	})
}

func TestSelectProxyChannelForAttempt(t *testing.T) {
	setupCfg()
	ctx := context.Background()
	defaultPolicy := routing.EmptyDownstreamRoutingPolicy

	coord := NewProxyChannelCoordinator(config.Get())

	t.Run("tester forced channel - first attempt", func(t *testing.T) {
		forcedID := int64(42)
		router := &mockRouter{
			selectPreferredChannel: func(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error) {
				if preferredChannelID == 42 {
					ch := makeChannel(42, 1, 100)
					return &ch, nil
				}
				return nil, nil
			},
		}

		refresher := &mockRouteRefresher{}
		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: defaultPolicy,
			RetryCount:       0,
			ForcedChannelID:  &forcedID,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel")
		}
		if selected.Channel.ID != 42 {
			t.Errorf("expected channel 42, got %d", selected.Channel.ID)
		}
	})

	t.Run("tester forced channel - retryCount > 0 returns nil immediately", func(t *testing.T) {
		forcedID := int64(42)
		router := &mockRouter{}
		refresher := &mockRouteRefresher{}
		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: defaultPolicy,
			RetryCount:       1,
			ForcedChannelID:  &forcedID,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected != nil {
			t.Error("expected nil for forced channel on retry > 0")
		}
	})

	t.Run("normal selection - first attempt via SelectChannel", func(t *testing.T) {
		router := &mockRouter{
			selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				ch := makeChannel(99, 2, 200)
				return &ch, nil
			},
		}
		refresher := &mockRouteRefresher{}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: defaultPolicy,
			RetryCount:       0,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel")
		}
		if selected.Channel.ID != 99 {
			t.Errorf("expected channel 99, got %d", selected.Channel.ID)
		}
	})

	t.Run("normal selection - retry via SelectNextChannel", func(t *testing.T) {
		exclude := []int64{1, 2}
		var capturedExclude []int64
		router := &mockRouter{
			selectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				capturedExclude = excludeChannelIDs
				ch := makeChannel(100, 3, 300)
				return &ch, nil
			},
		}
		refresher := &mockRouteRefresher{}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:    "gpt-4",
			DownstreamPolicy:  defaultPolicy,
			RetryCount:        1,
			ExcludeChannelIDs: exclude,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel")
		}
		if selected.Channel.ID != 100 {
			t.Errorf("expected channel 100, got %d", selected.Channel.ID)
		}
		if len(capturedExclude) != 2 || capturedExclude[0] != 1 || capturedExclude[1] != 2 {
			t.Errorf("excluded channels not passed correctly: %v", capturedExclude)
		}
	})

	t.Run("sticky session preference - first attempt", func(t *testing.T) {
		coord := NewProxyChannelCoordinator(config.Get())
		coord.cfg.ProxyStickySessionEnabled = true
		coord.cfg.ProxyStickySessionTtlMs = 60000

		keyID := int64(1)
		stickyKey := coord.BuildStickySessionKey("codex", "session-123", "gpt-4", "/v1/chat/completions", &keyID)

		// Bind with session-scoped credentials
		ec := sessionScopedExtraConfig()
		coord.BindStickyChannel(stickyKey, 55, &ec, nil)

		preferredCalled := false
		selectCalled := false

		router := &mockRouter{
			selectPreferredChannel: func(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error) {
				preferredCalled = true
				ch := makeChannel(55, 1, 100)
				return &ch, nil
			},
			selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				selectCalled = true
				return nil, nil
			},
		}
		refresher := &mockRouteRefresher{}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: defaultPolicy,
			RetryCount:       0,
			StickySessionKey: stickyKey,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel via sticky preference")
		}
		if !preferredCalled {
			t.Error("expected SelectPreferredChannel to be called for sticky session")
		}
		if selectCalled {
			t.Error("expected SelectChannel NOT to be called when sticky preference succeeds")
		}
		if selected.Channel.ID != 55 {
			t.Errorf("expected sticky channel 55, got %d", selected.Channel.ID)
		}
	})

	t.Run("sticky channel unavailable - route refresh and retry", func(t *testing.T) {
		coord := NewProxyChannelCoordinator(config.Get())
		coord.cfg.ProxyStickySessionEnabled = true
		coord.cfg.ProxyStickySessionTtlMs = 60000

		keyID := int64(1)
		stickyKey := coord.BuildStickySessionKey("codex", "session-abc", "gpt-4", "/v1/chat/completions", &keyID)
		ec := sessionScopedExtraConfig()
		coord.BindStickyChannel(stickyKey, 55, &ec, nil)

		refresher := &mockRouteRefresher{}
		callCount := 0

		router := &mockRouter{
			selectPreferredChannel: func(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error) {
				callCount++
				if callCount == 1 {
					return nil, nil // first call: unavailable
				}
				ch := makeChannel(55, 1, 100)
				return &ch, nil
			},
		}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: defaultPolicy,
			RetryCount:       0,
			StickySessionKey: stickyKey,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel after refresh")
		}
		if callCount != 2 {
			t.Errorf("expected 2 preferred channel calls, got %d", callCount)
		}
		if !refresher.called {
			t.Error("expected route refresh to be called")
		}
	})
}

func TestSelectProxyChannelForAttempt_RouteRefreshOnEmpty(t *testing.T) {
	setupCfg()
	ctx := context.Background()
	coord := NewProxyChannelCoordinator(config.Get())

	t.Run("route refresh on empty selection - first attempt only", func(t *testing.T) {
		count := 0
		refresher := &mockRouteRefresher{}
		router := &mockRouter{
			selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				count++
				if count == 1 {
					return nil, nil
				}
				ch := makeChannel(101, 5, 500)
				return &ch, nil
			},
		}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
			RetryCount:       0,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel after route refresh")
		}
		if selected.Channel.ID != 101 {
			t.Errorf("expected channel 101, got %d", selected.Channel.ID)
		}
		if !refresher.called {
			t.Error("expected route refresh to be called")
		}
		if count != 2 {
			t.Errorf("expected 2 select channel calls, got %d", count)
		}
	})

	t.Run("no route refresh on retry > 0", func(t *testing.T) {
		refresher2 := &mockRouteRefresher{}
		router := &mockRouter{
			selectNextChannel: func(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				return nil, nil
			},
		}

		_, _ = SelectProxyChannelForAttempt(ctx, router, coord, refresher2, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
			RetryCount:       1,
		})
		if refresher2.called {
			t.Error("expected route refresh NOT to be called on retry > 0")
		}
	})

	t.Run("route refresh when refresher is nil", func(t *testing.T) {
		count := 0
		router := &mockRouter{
			selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				count++
				if count == 1 {
					return nil, nil
				}
				ch := makeChannel(200, 6, 600)
				return &ch, nil
			},
		}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, nil, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: routing.EmptyDownstreamRoutingPolicy,
			RetryCount:       0,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selected == nil {
			t.Fatal("expected selected channel even without refresher")
		}
	})
}

func TestSelectProxyChannelForAttempt_ErrorPropagation(t *testing.T) {
	setupCfg()
	ctx := context.Background()
	defaultPolicy := routing.EmptyDownstreamRoutingPolicy
	coord := NewProxyChannelCoordinator(config.Get())

	t.Run("refresh error is silently swallowed", func(t *testing.T) {
		refresherErr := &mockRouteRefresher{
			refresh: func(ctx context.Context) error {
				return errors.New("refresh failed")
			},
		}
		router := &mockRouter{
			selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
				return nil, nil
			},
		}

		selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresherErr, ChannelSelectionInput{
			RequestedModel:   "gpt-4",
			DownstreamPolicy: defaultPolicy,
			RetryCount:       0,
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if selected != nil {
			t.Error("expected nil selected when all channels exhausted")
		}
	})
}

func TestStickySessionChannelSelectionExpired(t *testing.T) {
	setupCfg()
	ctx := context.Background()
	defaultPolicy := routing.EmptyDownstreamRoutingPolicy

	coord := NewProxyChannelCoordinator(config.Get())
	coord.cfg.ProxyStickySessionEnabled = true

	keyID := int64(1)
	stickyKey := coord.BuildStickySessionKey("codex", "session-old", "gpt-4", "/v1/chat/completions", &keyID)

	ec := sessionScopedExtraConfig()
	coord.BindStickyChannel(stickyKey, 55, &ec, nil)
	// Manually clear the binding to simulate expiry
	coord.ClearStickyChannel(stickyKey, 55)

	selectPreferredCalled := false
	selectChannelCalled := false

	router := &mockRouter{
		selectPreferredChannel: func(ctx context.Context, requestedModel string, preferredChannelID int64, policy routing.DownstreamRoutingPolicy, excludeChannelIDs []int64) (*routing.SelectedChannel, error) {
			selectPreferredCalled = true
			return nil, nil
		},
		selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			selectChannelCalled = true
			ch := makeChannel(88, 2, 200)
			return &ch, nil
		},
	}

	refresher := &mockRouteRefresher{}

	selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
		RequestedModel:   "gpt-4",
		DownstreamPolicy: defaultPolicy,
		RetryCount:       0,
		StickySessionKey: stickyKey,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected == nil {
		t.Fatal("expected selected channel via normal selection (sticky cleared)")
	}
	if selectPreferredCalled {
		t.Error("expected SelectPreferredChannel NOT to be called for missing sticky binding")
	}
	if !selectChannelCalled {
		t.Error("expected SelectChannel to be called as fallback")
	}
}

func TestStickyPreferredChannelInExcludeList(t *testing.T) {
	setupCfg()
	ctx := context.Background()
	defaultPolicy := routing.EmptyDownstreamRoutingPolicy

	coord := NewProxyChannelCoordinator(config.Get())
	coord.cfg.ProxyStickySessionEnabled = true
	coord.cfg.ProxyStickySessionTtlMs = 60000

	keyID := int64(1)
	stickyKey := coord.BuildStickySessionKey("codex", "session-exclude", "gpt-4", "/v1/chat/completions", &keyID)
	ec := sessionScopedExtraConfig()
	coord.BindStickyChannel(stickyKey, 55, &ec, nil)

	selectChannelCalled := false

	router := &mockRouter{
		selectChannel: func(ctx context.Context, requestedModel string, policy routing.DownstreamRoutingPolicy) (*routing.SelectedChannel, error) {
			selectChannelCalled = true
			ch := makeChannel(88, 2, 200)
			return &ch, nil
		},
	}

	refresher := &mockRouteRefresher{}

	selected, err := SelectProxyChannelForAttempt(ctx, router, coord, refresher, ChannelSelectionInput{
		RequestedModel:    "gpt-4",
		DownstreamPolicy:  defaultPolicy,
		RetryCount:        0,
		StickySessionKey:  stickyKey,
		ExcludeChannelIDs: []int64{55},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected == nil {
		t.Fatal("expected selected channel via normal selection")
	}
	if !selectChannelCalled {
		t.Error("expected SelectChannel to be called (sticky in exclude list)")
	}
}
