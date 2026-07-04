package proxy

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
)

func newTestCoordinator() *ProxyChannelCoordinator {
	cfg := config.Load(map[string]string{
		"PORT":                                    "8080",
		"PROXY_STICKY_SESSION_ENABLED":             "true",
		"PROXY_STICKY_SESSION_TTL_MS":              "60000",
		"PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT":  "0",
		"PROXY_SESSION_CHANNEL_QUEUE_WAIT_MS":      "0",
		"PROXY_SESSION_CHANNEL_LEASE_TTL_MS":       "5000",
		"PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS": "1000",
	})
	config.Set(cfg)
	return NewProxyChannelCoordinator(cfg)
}

func sessionScopedConfig() (*string, *string) {
	ec := `{"credentialMode":"session"}`
	op := "claude"
	return &ec, &op
}

func nonSessionScopedConfig() (*string, *string) {
	ec := `{"credentialMode":"apikey"}`
	return &ec, nil
}

func int64Ptr(v int64) *int64 {
	return &v
}

// ---- Sticky Session Tests ----

func TestBuildStickySessionKey(t *testing.T) {
	coord := newTestCoordinator()

	t.Run("disabled sticky sessions returns empty", func(t *testing.T) {
		coord.cfg.ProxyStickySessionEnabled = false
		key := coord.BuildStickySessionKey("codex", "session-1", "gpt-4", "/v1/chat/completions", int64Ptr(1))
		if key != "" {
			t.Errorf("expected empty key when sticky disabled, got %q", key)
		}
		coord.cfg.ProxyStickySessionEnabled = true
	})

	t.Run("empty sessionID returns empty", func(t *testing.T) {
		key := coord.BuildStickySessionKey("codex", "", "gpt-4", "/v1/chat/completions", int64Ptr(1))
		if key != "" {
			t.Errorf("expected empty key for empty sessionID, got %q", key)
		}
	})

	t.Run("empty model returns empty", func(t *testing.T) {
		key := coord.BuildStickySessionKey("codex", "session-1", "", "/v1/chat/completions", int64Ptr(1))
		if key != "" {
			t.Errorf("expected empty key for empty model, got %q", key)
		}
	})

	t.Run("valid key constructed", func(t *testing.T) {
		key := coord.BuildStickySessionKey("codex", "session-1", "gpt-4", "/v1/chat/completions", int64Ptr(1))
		if key == "" {
			t.Fatal("expected non-empty key")
		}
		if key != "key:1|codex|/v1/chat/completions|gpt-4|session-1" {
			t.Errorf("unexpected key format: %q", key)
		}
	})

	t.Run("nil downstream API key", func(t *testing.T) {
		key := coord.BuildStickySessionKey("codex", "session-1", "gpt-4", "/v1/chat/completions", nil)
		if key == "" {
			t.Fatal("expected non-empty key")
		}
		if key != "key:anonymous|codex|/v1/chat/completions|gpt-4|session-1" {
			t.Errorf("unexpected key format for nil API key: %q", key)
		}
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		key := coord.BuildStickySessionKey("  Codex  ", " session-1 ", "  gpt-4 ", " /v1/chat/completions ", int64Ptr(1))
		if key != "key:1|codex|/v1/chat/completions|gpt-4|session-1" {
			t.Errorf("whitespace not trimmed: %q", key)
		}
	})

	t.Run("empty path defaults to unknown", func(t *testing.T) {
		key := coord.BuildStickySessionKey("codex", "session-1", "gpt-4", "", int64Ptr(1))
		if key == "" {
			t.Fatal("expected non-empty key")
		}
		if key != "key:1|codex|unknown|gpt-4|session-1" {
			t.Errorf("unexpected key format for empty path: %q", key)
		}
	})

	t.Run("empty client kind defaults to generic", func(t *testing.T) {
		key := coord.BuildStickySessionKey("", "session-1", "gpt-4", "/v1/chat/completions", int64Ptr(1))
		if key == "" {
			t.Fatal("expected non-empty key")
		}
		if key != "key:1|generic|/v1/chat/completions|gpt-4|session-1" {
			t.Errorf("unexpected key format for empty kind: %q", key)
		}
	})
}

func TestStickyBindings(t *testing.T) {
	coord := newTestCoordinator()
	ec, op := sessionScopedConfig()

	t.Run("bind and get", func(t *testing.T) {
		coord.BindStickyChannel("test-key-1", 42, ec, op)
		id := coord.GetStickyChannelID("test-key-1")
		if id != 42 {
			t.Errorf("expected channel 42, got %d", id)
		}
	})

	t.Run("get non-existent key returns 0", func(t *testing.T) {
		id := coord.GetStickyChannelID("non-existent")
		if id != 0 {
			t.Errorf("expected 0 for non-existent key, got %d", id)
		}
	})

	t.Run("get empty key returns 0", func(t *testing.T) {
		id := coord.GetStickyChannelID("")
		if id != 0 {
			t.Errorf("expected 0 for empty key, got %d", id)
		}
	})

	t.Run("clear with matching channelID", func(t *testing.T) {
		coord.BindStickyChannel("test-key-2", 100, ec, op)
		coord.ClearStickyChannel("test-key-2", 100)
		id := coord.GetStickyChannelID("test-key-2")
		if id != 0 {
			t.Errorf("expected 0 after clear, got %d", id)
		}
	})

	t.Run("clear with non-matching channelID does not clear", func(t *testing.T) {
		coord.BindStickyChannel("test-key-3", 100, ec, op)
		coord.ClearStickyChannel("test-key-3", 200)
		id := coord.GetStickyChannelID("test-key-3")
		if id != 100 {
			t.Errorf("expected sticky to remain (channelID mismatch), got %d", id)
		}
	})

	t.Run("clear with channelID=0 clears regardless", func(t *testing.T) {
		coord.BindStickyChannel("test-key-4", 100, ec, op)
		coord.ClearStickyChannel("test-key-4", 0)
		id := coord.GetStickyChannelID("test-key-4")
		if id != 0 {
			t.Errorf("expected 0 after clear with channelID=0, got %d", id)
		}
	})

	t.Run("expired binding via manual clear", func(t *testing.T) {
		expireCoord := newTestCoordinator()
		expireCoord.BindStickyChannel("expiring-key", 42, ec, op)

		// Simulate expiry
		expireCoord.ClearStickyChannel("expiring-key", 42)
		id := expireCoord.GetStickyChannelID("expiring-key")
		if id != 0 {
			t.Errorf("expected 0 after clear, got %d", id)
		}
	})

	t.Run("bind disabled clears all", func(t *testing.T) {
		disabledCoord := newTestCoordinator()
		disabledCoord.cfg.ProxyStickySessionEnabled = false

		disabledCoord.BindStickyChannel("disabled-key", 42, ec, op)
		id := disabledCoord.GetStickyChannelID("disabled-key")
		if id != 0 {
			t.Errorf("expected 0 when sticky disabled, got %d", id)
		}
	})

	t.Run("bind non-session-scoped channel", func(t *testing.T) {
		coord.Reset()
		nonEC, nonOP := nonSessionScopedConfig()
		coord.BindStickyChannel("non-session-key", 42, nonEC, nonOP)
		id := coord.GetStickyChannelID("non-session-key")
		if id != 0 {
			t.Errorf("expected 0 for non-session-scoped channel, got %d", id)
		}
	})
}

func TestIsSessionScopedChannel(t *testing.T) {
	t.Run("session credential mode", func(t *testing.T) {
		ec := `{"credentialMode":"session"}`
		if !IsSessionScopedChannel(&ec, nil) {
			t.Error("expected session-scoped with credentialMode=session")
		}
	})

	t.Run("apikey credential mode, no oauth", func(t *testing.T) {
		ec := `{"credentialMode":"apikey"}`
		if IsSessionScopedChannel(&ec, nil) {
			t.Error("expected NOT session-scoped with credentialMode=apikey and no oauth")
		}
	})

	t.Run("apikey with oauth", func(t *testing.T) {
		ec := `{"credentialMode":"apikey"}`
		op := "claude"
		if !IsSessionScopedChannel(&ec, &op) {
			t.Error("expected session-scoped when oauth provider is present")
		}
	})

	t.Run("empty oauth provider", func(t *testing.T) {
		ec := `{"credentialMode":"apikey"}`
		op := ""
		if IsSessionScopedChannel(&ec, &op) {
			t.Error("expected NOT session-scoped with empty oauth provider")
		}
	})

	t.Run("nil configs", func(t *testing.T) {
		if IsSessionScopedChannel(nil, nil) {
			t.Error("expected NOT session-scoped with nil configs")
		}
	})

	t.Run("auto credential mode no oauth", func(t *testing.T) {
		ec := `{"credentialMode":"auto"}`
		if IsSessionScopedChannel(&ec, nil) {
			t.Error("expected NOT session-scoped with credentialMode=auto")
		}
	})

	t.Run("nil extraConfig with oauth", func(t *testing.T) {
		op := "claude"
		if !IsSessionScopedChannel(nil, &op) {
			t.Error("expected session-scoped when oauth provider present even with nil extraConfig")
		}
	})
}

// ---- Lease Tests ----

func TestAcquireChannelLease(t *testing.T) {
	t.Run("channelID <= 0 returns noop", func(t *testing.T) {
		coord := newTestCoordinator()
		result := coord.AcquireChannelLease(0, nil, nil)
		if result.Status != "acquired" {
			t.Errorf("expected 'acquired', got %q", result.Status)
		}
		if result.Lease == nil {
			t.Fatal("expected non-nil lease")
		}
		if result.Lease.IsActive() {
			t.Error("expected noop lease to be inactive")
		}
	})

	t.Run("non-session-scoped channel returns noop", func(t *testing.T) {
		coord := newTestCoordinator()
		ec, _ := nonSessionScopedConfig()
		result := coord.AcquireChannelLease(1, ec, nil)
		if result.Status != "acquired" {
			t.Errorf("expected 'acquired', got %q", result.Status)
		}
		if result.Lease == nil {
			t.Fatal("expected non-nil lease")
		}
		if result.Lease.IsActive() {
			t.Error("expected noop lease for non-session-scoped channel")
		}
	})

	t.Run("session-scoped channel acquires tracked lease", func(t *testing.T) {
		coord := newTestCoordinator()
		coord.cfg.ProxySessionChannelConcurrencyLimit = 1
		coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
		coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000
		ec, op := sessionScopedConfig()

		result := coord.AcquireChannelLease(500, ec, op)
		if result.Status != "acquired" {
			t.Errorf("expected 'acquired', got %q", result.Status)
		}
		if result.Lease == nil {
			t.Fatal("expected non-nil lease")
		}
		if !result.Lease.IsActive() {
			t.Error("expected tracked lease to be active")
		}
		if result.Lease.ChannelID != 500 {
			t.Errorf("expected channel 500, got %d", result.Lease.ChannelID)
		}
		result.Lease.Release()
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("concurrency limit reached returns timeout", func(t *testing.T) {
		coord := newTestCoordinator()
		coord.cfg.ProxySessionChannelConcurrencyLimit = 1
		coord.cfg.ProxySessionChannelQueueWaitMs = 50
		coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
		coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000
		ec, op := sessionScopedConfig()

		result1 := coord.AcquireChannelLease(501, ec, op)
		if result1.Status != "acquired" {
			t.Fatalf("expected acquired, got %q", result1.Status)
		}

		result2 := coord.AcquireChannelLease(501, ec, op)
		if result2.Status != "timeout" {
			t.Errorf("expected 'timeout' for second request, got %q", result2.Status)
		}

		result1.Lease.Release()
		time.Sleep(100 * time.Millisecond)
	})
}

func TestLeaseLifecycle(t *testing.T) {
	coord := newTestCoordinator()
	coord.cfg.ProxySessionChannelConcurrencyLimit = 1
	coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
	coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000

	ec, op := sessionScopedConfig()

	t.Run("release makes lease inactive", func(t *testing.T) {
		result := coord.AcquireChannelLease(600, ec, op)
		if result.Status != "acquired" {
			t.Fatalf("expected acquired, got %q", result.Status)
		}
		if !result.Lease.IsActive() {
			t.Fatal("expected active lease")
		}

		result.Lease.Release()
		time.Sleep(100 * time.Millisecond)

		if result.Lease.IsActive() {
			t.Error("expected inactive after release")
		}
	})

	t.Run("double release is safe", func(t *testing.T) {
		result := coord.AcquireChannelLease(601, ec, op)
		if result.Status != "acquired" {
			t.Fatalf("expected acquired, got %q", result.Status)
		}

		result.Lease.Release()
		time.Sleep(100 * time.Millisecond)
		result.Lease.Release()
		time.Sleep(100 * time.Millisecond)

		result2 := coord.AcquireChannelLease(601, ec, op)
		if result2.Status != "acquired" {
			t.Errorf("expected acquired after double release, got %q", result2.Status)
		}
		result2.Lease.Release()
	})
}

func TestLeaseQueueDrain(t *testing.T) {
	t.Run("queue timeout with zero waitMs", func(t *testing.T) {
		coord := newTestCoordinator()
		coord.cfg.ProxySessionChannelConcurrencyLimit = 1
		coord.cfg.ProxySessionChannelQueueWaitMs = 0
		coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
		coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000

		ec, op := sessionScopedConfig()

		result1 := coord.AcquireChannelLease(700, ec, op)
		if result1.Status != "acquired" {
			t.Fatalf("expected acquired, got %q", result1.Status)
		}

		result2 := coord.AcquireChannelLease(700, ec, op)
		if result2.Status != "timeout" {
			t.Errorf("expected 'timeout' with zero waitMs, got %q", result2.Status)
		}
		if result2.WaitMs != 0 {
			t.Errorf("expected WaitMs=0 for instant timeout, got %d", result2.WaitMs)
		}

		result1.Lease.Release()
		time.Sleep(100 * time.Millisecond)
	})
}

func TestChannelLoadSnapshot(t *testing.T) {
	coord := newTestCoordinator()
	coord.cfg.ProxySessionChannelConcurrencyLimit = 2
	coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
	coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000

	ec, op := sessionScopedConfig()

	snap := coord.GetChannelLoadSnapshot(800, ec, op)
	if snap.SessionScoped != true {
		t.Error("expected session-scoped in snapshot")
	}
	if snap.ActiveLeaseCount != 0 {
		t.Errorf("expected 0 active, got %d", snap.ActiveLeaseCount)
	}
	if snap.Saturated {
		t.Error("expected not saturated")
	}

	result1 := coord.AcquireChannelLease(800, ec, op)
	result2 := coord.AcquireChannelLease(800, ec, op)

	snap = coord.GetChannelLoadSnapshot(800, ec, op)
	if snap.ActiveLeaseCount != 2 {
		t.Errorf("expected 2 active, got %d", snap.ActiveLeaseCount)
	}
	if !snap.Saturated {
		t.Error("expected saturated with 2 active and limit 2")
	}
	if snap.ConcurrencyLimit != 2 {
		t.Errorf("expected limit=2, got %d", snap.ConcurrencyLimit)
	}

	result1.Lease.Release()
	result2.Lease.Release()
	time.Sleep(100 * time.Millisecond)

	snap = coord.GetChannelLoadSnapshot(800, ec, op)
	if snap.ActiveLeaseCount != 0 {
		t.Errorf("expected 0 active after release, got %d", snap.ActiveLeaseCount)
	}
}

func TestGetActiveChannelIDs(t *testing.T) {
	coord := newTestCoordinator()
	coord.cfg.ProxySessionChannelConcurrencyLimit = 1
	coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
	coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000

	ec, op := sessionScopedConfig()

	ids := coord.GetActiveChannelIDs()
	if len(ids) != 0 {
		t.Errorf("expected 0 active channels, got %d", len(ids))
	}

	result1 := coord.AcquireChannelLease(900, ec, op)
	result2 := coord.AcquireChannelLease(901, ec, op)

	ids = coord.GetActiveChannelIDs()
	if len(ids) != 2 {
		t.Errorf("expected 2 active channels, got %d", len(ids))
	}

	result1.Lease.Release()
	result2.Lease.Release()
	time.Sleep(100 * time.Millisecond)

	ids = coord.GetActiveChannelIDs()
	if len(ids) != 0 {
		t.Errorf("expected 0 active channels after release, got %d", len(ids))
	}
}

func TestCoordinatorReset(t *testing.T) {
	coord := newTestCoordinator()
	coord.cfg.ProxySessionChannelLeaseTtlMs = 30000
	coord.cfg.ProxySessionChannelLeaseKeepaliveMs = 10000

	ec, op := sessionScopedConfig()

	coord.BindStickyChannel("key-1", 42, ec, op)
	coord.AcquireChannelLease(950, ec, op)

	coord.Reset()

	id := coord.GetStickyChannelID("key-1")
	if id != 0 {
		t.Errorf("expected 0 after reset (sticky cleared)")
	}

	ids := coord.GetActiveChannelIDs()
	if len(ids) != 0 {
		t.Errorf("expected 0 active after reset")
	}
}

func TestCredentialModeIntegration(t *testing.T) {
	t.Run("correct mode string from service", func(t *testing.T) {
		ec := `{"credentialMode":"session"}`
		mode := service.GetCredentialModeFromExtraConfig(&ec)
		if mode != service.CredentialModeSession {
			t.Errorf("expected session mode, got %q", mode)
		}
	})

	t.Run("auto mode string", func(t *testing.T) {
		ec := `{"credentialMode":"auto"}`
		mode := service.GetCredentialModeFromExtraConfig(&ec)
		if mode != "auto" {
			t.Errorf("expected auto mode, got %q", mode)
		}
	})

	t.Run("nil extra config", func(t *testing.T) {
		mode := service.GetCredentialModeFromExtraConfig(nil)
		if mode != "" {
			t.Errorf("expected empty mode for nil config, got %q", mode)
		}
	})
}
