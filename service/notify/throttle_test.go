package notify

import (
	"testing"
	"time"
)

// ---- CreateNotificationSignature Tests ----

func TestCreateNotificationSignature(t *testing.T) {
	sig := CreateNotificationSignature("test title", "test message", "error")
	expected := "error||test title||test message"
	if sig != expected {
		t.Errorf("signature = %q, want %q", sig, expected)
	}
}

func TestCreateNotificationSignature_TrimSpace(t *testing.T) {
	// Level is trimmed and lowercased, title and message trimmed
	sig := CreateNotificationSignature("  title  ", "  message  ", "  ERROR  ")
	expected := "error||title||message"
	if sig != expected {
		t.Errorf("signature = %q, want %q", sig, expected)
	}
}

func TestCreateNotificationSignature_SameTitleDifferentMessage(t *testing.T) {
	// Same title, different message → different signature
	sig1 := CreateNotificationSignature("title", "message A", "error")
	sig2 := CreateNotificationSignature("title", "message B", "error")
	if sig1 == sig2 {
		t.Errorf("expected different signatures for different messages: %q vs %q", sig1, sig2)
	}
}

func TestCreateNotificationSignature_DifferentLevels(t *testing.T) {
	sig1 := CreateNotificationSignature("title", "message", "error")
	sig2 := CreateNotificationSignature("title", "message", "warning")
	if sig1 == sig2 {
		t.Errorf("expected different signatures for different levels: %q vs %q", sig1, sig2)
	}
}

// ---- EvaluateNotificationThrottle Tests ----

func TestEvaluateNotificationThrottle_NewSignature(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()
	result := throttle.EvaluateNotificationThrottle("error||title||msg", now, 60_000)
	if !result.ShouldSend {
		t.Error("new signature should be sent")
	}
	if result.MergedCount != 0 {
		t.Errorf("MergedCount = %d, want 0", result.MergedCount)
	}

	// State should be stored
	throttle.mu.Lock()
	if _, ok := throttle.state["error||title||msg"]; !ok {
		t.Error("state should be stored for the signature")
	}
	throttle.mu.Unlock()
}

func TestEvaluateNotificationThrottle_WithinCooldown(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// First: should send
	result := throttle.EvaluateNotificationThrottle("error||title||msg", now, 60_000)
	if !result.ShouldSend {
		t.Error("first call should send")
	}

	// Second: within cooldown → suppressed
	result = throttle.EvaluateNotificationThrottle("error||title||msg", now+1000, 60_000)
	if result.ShouldSend {
		t.Error("second call within cooldown should NOT send")
	}

	// Check suppressedCount
	throttle.mu.Lock()
	state := throttle.state["error||title||msg"]
	throttle.mu.Unlock()
	if state == nil || state.SuppressedCount != 1 {
		t.Errorf("SuppressedCount = %d, want 1", state.SuppressedCount)
	}
}

func TestEvaluateNotificationThrottle_AfterCooldown(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// First: send at now
	throttle.EvaluateNotificationThrottle("error||title||msg", now, 1_000)

	// Second: suppressed
	throttle.EvaluateNotificationThrottle("error||title||msg", now+500, 1_000)

	// Third: after cooldown → should send, mergedCount=1
	result := throttle.EvaluateNotificationThrottle("error||title||msg", now+2_000, 1_000)
	if !result.ShouldSend {
		t.Error("after cooldown should send")
	}
	if result.MergedCount != 1 {
		t.Errorf("MergedCount = %d, want 1", result.MergedCount)
	}

	// SuppressedCount should be reset
	throttle.mu.Lock()
	state := throttle.state["error||title||msg"]
	throttle.mu.Unlock()
	if state == nil || state.SuppressedCount != 0 {
		t.Errorf("SuppressedCount after reset = %d, want 0",
			func() int {
				if state == nil {
					return -1
				}
				return state.SuppressedCount
			}())
	}
}

func TestEvaluateNotificationThrottle_MultipleSuppression(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// First send
	throttle.EvaluateNotificationThrottle("error||title||msg", now, 10_000)
	// Suppress 5 times
	for i := 0; i < 5; i++ {
		throttle.EvaluateNotificationThrottle("error||title||msg", now+int64(i+1)*1000, 10_000)
	}
	// After cooldown, mergedCount should be 5
	result := throttle.EvaluateNotificationThrottle("error||title||msg", now+20_000, 10_000)
	if !result.ShouldSend {
		t.Error("after cooldown should send")
	}
	if result.MergedCount != 5 {
		t.Errorf("MergedCount = %d, want 5", result.MergedCount)
	}
}

func TestEvaluateNotificationThrottle_CooldownZero(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	result := throttle.EvaluateNotificationThrottle("error||title||msg", now, 0)
	if !result.ShouldSend {
		t.Error("cooldown=0 should always send")
	}
	if result.MergedCount != 0 {
		t.Errorf("MergedCount = %d, want 0 for cooldown=0", result.MergedCount)
	}

	// Second call should also send (cooldown bypassed)
	result = throttle.EvaluateNotificationThrottle("error||title||msg", now, 0)
	if !result.ShouldSend {
		t.Error("cooldown=0 should always send")
	}
}

func TestEvaluateNotificationThrottle_NegativeCooldown(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// Negative cooldown → treated as no cooldown
	result := throttle.EvaluateNotificationThrottle("error||title||msg", now, -1)
	if !result.ShouldSend {
		t.Error("negative cooldown should always send")
	}
}

func TestEvaluateNotificationThrottle_DifferentSignatures(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// Sig A sent
	resultA := throttle.EvaluateNotificationThrottle("error||title||msgA", now, 60_000)
	if !resultA.ShouldSend {
		t.Error("sig A should send")
	}

	// Sig B sent (different message, should not be blocked by A's cooldown)
	resultB := throttle.EvaluateNotificationThrottle("error||title||msgB", now+1000, 60_000)
	if !resultB.ShouldSend {
		t.Error("sig B should send (different signature)")
	}

	// Sig A again within cooldown → suppressed
	resultA2 := throttle.EvaluateNotificationThrottle("error||title||msgA", now+2000, 60_000)
	if resultA2.ShouldSend {
		t.Error("sig A should be suppressed")
	}
}

// ---- PruneNotificationThrottleState Tests ----

func TestPruneNotificationThrottleState(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// Add some old and new entries
	throttle.EvaluateNotificationThrottle("error||old||msg", now-200_000, 1_000)
	throttle.EvaluateNotificationThrottle("error||new||msg", now, 1_000)

	// Prune entries older than 100_000ms
	throttle.PruneNotificationThrottleState(now, 100_000)

	throttle.mu.Lock()
	_, hasOld := throttle.state["error||old||msg"]
	_, hasNew := throttle.state["error||new||msg"]
	throttle.mu.Unlock()

	if hasOld {
		t.Error("old entry should be pruned")
	}
	if !hasNew {
		t.Error("new entry should remain")
	}
}

func TestPruneNotificationThrottleState_StaleMsZero(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	throttle.EvaluateNotificationThrottle("error||test||msg", now, 1_000)

	// staleMs <= 0 → no-op
	throttle.PruneNotificationThrottleState(now, 0)

	throttle.mu.Lock()
	entry := throttle.state["error||test||msg"]
	throttle.mu.Unlock()
	if entry == nil {
		t.Error("entry should not be pruned when staleMs=0")
	}
}

func TestPruneNotificationThrottleState_NegativeStaleMs(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	throttle.EvaluateNotificationThrottle("error||test||msg", now, 1_000)

	// staleMs < 0 → no-op
	throttle.PruneNotificationThrottleState(now, -1)

	throttle.mu.Lock()
	entry := throttle.state["error||test||msg"]
	throttle.mu.Unlock()
	if entry == nil {
		t.Error("entry should not be pruned when staleMs<0")
	}
}

func TestPruneNotificationThrottleState_EmptyState(t *testing.T) {
	throttle := NewNotificationThrottle()
	now := time.Now().UnixMilli()

	// Should not panic on empty state
	throttle.PruneNotificationThrottleState(now, 60_000)
}

// ---- GlobalThrottle Tests ----

func TestGlobalThrottle_Exists(t *testing.T) {
	if GlobalThrottle == nil {
		t.Fatal("GlobalThrottle should not be nil")
	}
	// Clean up after test
	GlobalThrottle.mu.Lock()
	GlobalThrottle.state = make(map[string]*NotificationThrottleState)
	GlobalThrottle.mu.Unlock()
}

// ---- NotificationThrottleState Tests ----

func TestNotificationThrottleState(t *testing.T) {
	state := NotificationThrottleState{
		LastSentAtMs:   1000,
		SuppressedCount: 3,
	}
	if state.LastSentAtMs != 1000 {
		t.Errorf("LastSentAtMs = %d, want 1000", state.LastSentAtMs)
	}
	if state.SuppressedCount != 3 {
		t.Errorf("SuppressedCount = %d, want 3", state.SuppressedCount)
	}
}

// ---- NotificationLevel and NotificationChannel Constants ----

func TestNotificationLevels(t *testing.T) {
	if LevelInfo != "info" {
		t.Errorf("LevelInfo = %q, want 'info'", LevelInfo)
	}
	if LevelWarning != "warning" {
		t.Errorf("LevelWarning = %q, want 'warning'", LevelWarning)
	}
	if LevelError != "error" {
		t.Errorf("LevelError = %q, want 'error'", LevelError)
	}
}

func TestNotificationChannels(t *testing.T) {
	if ChannelWebhook != "webhook" {
		t.Errorf("ChannelWebhook = %q, want 'webhook'", ChannelWebhook)
	}
	if ChannelBark != "bark" {
		t.Errorf("ChannelBark = %q, want 'bark'", ChannelBark)
	}
	if ChannelServerChan != "serverchan" {
		t.Errorf("ChannelServerChan = %q, want 'serverchan'", ChannelServerChan)
	}
	if ChannelTelegram != "telegram" {
		t.Errorf("ChannelTelegram = %q, want 'telegram'", ChannelTelegram)
	}
	if ChannelSMTP != "smtp" {
		t.Errorf("ChannelSMTP = %q, want 'smtp'", ChannelSMTP)
	}
}

// ---- DispatchResult Tests ----

func TestDispatchResult_Throttle(t *testing.T) {
	r := DispatchResult{
		Throttled: true,
		Attempted: 0,
		Succeeded: 0,
		Failed:    0,
	}
	if !r.Throttled {
		t.Error("throttle result should be throttled")
	}
}

func TestDispatchResult_Success(t *testing.T) {
	r := DispatchResult{
		Throttled: false,
		Attempted: 3,
		Succeeded: 3,
		Failed:    0,
	}
	if r.Succeeded != 3 {
		t.Errorf("Succeeded = %d, want 3", r.Succeeded)
	}
	if len(r.FailedChannels) != 0 {
		t.Errorf("FailedChannels should be empty, got %v", r.FailedChannels)
	}
}

func TestDispatchResult_PartialFailure(t *testing.T) {
	r := DispatchResult{
		Throttled:      false,
		Attempted:      3,
		Succeeded:      1,
		Failed:         2,
		FailedChannels: []NotificationChannel{ChannelBark, ChannelSMTP},
	}
	if r.Failed != 2 {
		t.Errorf("Failed = %d, want 2", r.Failed)
	}
	if len(r.FailedChannels) != 2 {
		t.Errorf("expected 2 failed channels, got %d", len(r.FailedChannels))
	}
}

// ---- SendNotificationOptions Tests ----

func TestSendNotificationOptions(t *testing.T) {
	opts := &SendNotificationOptions{
		BypassThrottle: true,
		RequireChannel: false,
		ThrowOnFailure: true,
	}
	if !opts.BypassThrottle {
		t.Error("BypassThrottle should be true")
	}
	if !opts.ThrowOnFailure {
		t.Error("ThrowOnFailure should be true")
	}
}
