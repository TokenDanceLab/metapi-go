package notify

import (
	"strings"
	"sync"
)

// NotificationThrottleState tracks the last sent timestamp and suppressed count.
type NotificationThrottleState struct {
	LastSentAtMs   int64
	SuppressedCount int
}

// NotificationThrottle is a global throttle for notification deduplication.
type NotificationThrottle struct {
	mu    sync.Mutex
	state map[string]*NotificationThrottleState
}

// NewNotificationThrottle creates a new throttle instance.
func NewNotificationThrottle() *NotificationThrottle {
	return &NotificationThrottle{
		state: make(map[string]*NotificationThrottleState),
	}
}

// Default global throttle instance (same as TS module-level map).
var GlobalThrottle = NewNotificationThrottle()

// CreateNotificationSignature creates a signature from title, message, and level.
// Format: "level||title||message" (three fields joined by ||).
// Mirrors TS createNotificationSignature().
func CreateNotificationSignature(title, message, level string) string {
	level = strings.TrimSpace(strings.ToLower(level))
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	return level + "||" + title + "||" + message
}

// EvaluateResult is the result of a throttle evaluation.
type EvaluateResult struct {
	ShouldSend  bool
	MergedCount int
}

// EvaluateNotificationThrottle checks whether a notification should be sent
// based on the cooldown period.
// Mirrors TS evaluateNotificationThrottle().
func (t *NotificationThrottle) EvaluateNotificationThrottle(signature string, nowMs int64, cooldownMs int64) EvaluateResult {
	if cooldownMs <= 0 {
		return EvaluateResult{ShouldSend: true, MergedCount: 0}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	current, exists := t.state[signature]
	if !exists {
		t.state[signature] = &NotificationThrottleState{
			LastSentAtMs:   nowMs,
			SuppressedCount: 0,
		}
		return EvaluateResult{ShouldSend: true, MergedCount: 0}
	}

	if nowMs-current.LastSentAtMs < cooldownMs {
		current.SuppressedCount++
		return EvaluateResult{ShouldSend: false, MergedCount: 0}
	}

	mergedCount := current.SuppressedCount
	t.state[signature] = &NotificationThrottleState{
		LastSentAtMs:   nowMs,
		SuppressedCount: 0,
	}
	return EvaluateResult{ShouldSend: true, MergedCount: mergedCount}
}

// PruneNotificationThrottleState removes stale entries older than staleMs.
// Mirrors TS pruneNotificationThrottleState().
func (t *NotificationThrottle) PruneNotificationThrottleState(nowMs int64, staleMs int64) {
	if staleMs <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for key, state := range t.state {
		if nowMs-state.LastSentAtMs > staleMs {
			delete(t.state, key)
		}
	}
}
