package auth

import (
	"strconv"
	"sync"
	"time"
)

// DefaultKeyRPMWindow is the admission window for downstream-key RPM limits.
// Matches LiteLLM/New API "requests per minute" semantics as a soft gate.
const DefaultKeyRPMWindow = time.Minute

// KeyRPMDecision is the result of a TryAdmit call.
type KeyRPMDecision struct {
	Allowed    bool
	Limit      int64
	Used       int
	RetryAfter time.Duration // zero when allowed
}

// KeyRPMLimiter is an in-process sliding-window RPM admission control keyed by
// downstream_api_keys.id. It is intentionally soft/local (not Redis): multi-
// instance deployments do not share counters (see learn #119 for shared state).
//
// Semantics:
//   - limit <= 0 → unlimited (always admit; no tracking)
//   - limit > 0  → at most `limit` admits per key within the rolling window
//
// Callers should invoke TryAdmit only for managed keys after auth succeeds and
// before upstream dispatch / max_requests consumption.
type KeyRPMLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	now     func() time.Time
	keys    map[int64]*keyRPMState
	maxIdle time.Duration
}

type keyRPMState struct {
	// times is a ring of admit timestamps within the active window (oldest first).
	times []time.Time
}

// DefaultKeyRPMLimiter is the process-global limiter used by ProxyAuth.
var DefaultKeyRPMLimiter = NewKeyRPMLimiter(DefaultKeyRPMWindow)

// NewKeyRPMLimiter creates a sliding-window limiter with the given window size.
// window <= 0 falls back to DefaultKeyRPMWindow.
func NewKeyRPMLimiter(window time.Duration) *KeyRPMLimiter {
	if window <= 0 {
		window = DefaultKeyRPMWindow
	}
	return &KeyRPMLimiter{
		window:  window,
		now:     time.Now,
		keys:    make(map[int64]*keyRPMState),
		maxIdle: 5 * time.Minute,
	}
}

// TryAdmit attempts to admit one request for keyID under the given RPM limit.
//
// Returns Allowed=true when the request may proceed. When denied, RetryAfter is
// a positive duration approximating when the oldest slot leaves the window
// (ceil to 1s minimum for Retry-After headers).
func (l *KeyRPMLimiter) TryAdmit(keyID int64, limit int64) KeyRPMDecision {
	if l == nil || keyID <= 0 || limit <= 0 {
		return KeyRPMDecision{Allowed: true, Limit: limit, Used: 0}
	}

	now := l.now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	state := l.keys[keyID]
	if state == nil {
		state = &keyRPMState{}
		l.keys[keyID] = state
	}

	// Drop timestamps outside the window.
	i := 0
	for i < len(state.times) && !state.times[i].After(cutoff) {
		i++
	}
	if i > 0 {
		state.times = append([]time.Time(nil), state.times[i:]...)
	}

	used := len(state.times)
	if int64(used) >= limit {
		// Oldest entry leaves the window at times[0]+window.
		retryAfter := state.times[0].Add(l.window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return KeyRPMDecision{
			Allowed:    false,
			Limit:      limit,
			Used:       used,
			RetryAfter: retryAfter,
		}
	}

	state.times = append(state.times, now)
	return KeyRPMDecision{
		Allowed: true,
		Limit:   limit,
		Used:    used + 1,
	}
}

// Snapshot returns current used count within the window for keyID without
// recording a new admit. Unlimited / unknown keys report Used=0.
func (l *KeyRPMLimiter) Snapshot(keyID int64) (used int, window time.Duration) {
	if l == nil || keyID <= 0 {
		return 0, DefaultKeyRPMWindow
	}
	window = l.window
	now := l.now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	state := l.keys[keyID]
	if state == nil {
		return 0, window
	}
	i := 0
	for i < len(state.times) && !state.times[i].After(cutoff) {
		i++
	}
	if i > 0 {
		state.times = append([]time.Time(nil), state.times[i:]...)
	}
	if len(state.times) == 0 {
		delete(l.keys, keyID)
		return 0, window
	}
	return len(state.times), window
}

// Reset clears tracking for keyID (tests / admin reset helpers).
func (l *KeyRPMLimiter) Reset(keyID int64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.keys, keyID)
	l.mu.Unlock()
}

// PruneIdle drops keys with no recent timestamps (best-effort memory hygiene).
// Safe to call from a background tick; not required for correctness.
func (l *KeyRPMLimiter) PruneIdle() {
	if l == nil {
		return
	}
	now := l.now()
	cutoff := now.Add(-l.maxIdle)

	l.mu.Lock()
	defer l.mu.Unlock()
	for id, state := range l.keys {
		if len(state.times) == 0 {
			delete(l.keys, id)
			continue
		}
		last := state.times[len(state.times)-1]
		if !last.After(cutoff) {
			delete(l.keys, id)
		}
	}
}

// formatRetryAfterSeconds returns a whole-second Retry-After value (min 1).
func formatRetryAfterSeconds(d time.Duration) string {
	sec := int(d.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}
	return strconv.Itoa(sec)
}
