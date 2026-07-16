package auth

import (
	"sync"
	"time"
)

// KeyAdmissionLimiter is an in-process sliding-window RPM/TPM gate for
// managed downstream keys (learn #116). Default unlimited when limits are nil/<=0.
// Multi-instance deployments need shared state (#118) — this is process-local.
type KeyAdmissionLimiter struct {
	mu   sync.Mutex
	keys map[int64]*keyWindow
	// nowFn is injectable for tests.
	nowFn func() time.Time
}

type keyWindow struct {
	// request timestamps (unix ms) within the last 60s
	reqTimes []int64
	// token events: {atMs, tokens}
	tokenEvents []tokenEvent
}

type tokenEvent struct {
	atMs   int64
	tokens int64
}

// GlobalKeyAdmission is the process-wide limiter used by ProxyAuth.
var GlobalKeyAdmission = NewKeyAdmissionLimiter()

// NewKeyAdmissionLimiter creates an empty limiter.
func NewKeyAdmissionLimiter() *KeyAdmissionLimiter {
	return &KeyAdmissionLimiter{
		keys:  make(map[int64]*keyWindow),
		nowFn: time.Now,
	}
}

// ResetKeyAdmissionForTest clears the global limiter state.
func ResetKeyAdmissionForTest() {
	GlobalKeyAdmission = NewKeyAdmissionLimiter()
}

// AdmissionDecision is the result of Allow.
type AdmissionDecision struct {
	Allowed    bool
	Reason     string // "" | "over_rpm" | "over_tpm"
	RetryAfter time.Duration
	UsedRPM    int64
	UsedTPM    int64
}

// Allow checks and records a request against optional RPM/TPM limits.
// estimatedTokens is reserved against TPM when maxTPM is set; pass 0 to skip TPM accounting.
// When allowed, the request is recorded immediately (admission reservation).
func (l *KeyAdmissionLimiter) Allow(keyID int64, maxRPM, maxTPM *int64, estimatedTokens int64) AdmissionDecision {
	if l == nil || keyID <= 0 {
		return AdmissionDecision{Allowed: true}
	}
	rpmLimit := int64(0)
	tpmLimit := int64(0)
	if maxRPM != nil && *maxRPM > 0 {
		rpmLimit = *maxRPM
	}
	if maxTPM != nil && *maxTPM > 0 {
		tpmLimit = *maxTPM
	}
	if rpmLimit == 0 && tpmLimit == 0 {
		return AdmissionDecision{Allowed: true}
	}

	now := l.nowFn().UTC()
	nowMs := now.UnixMilli()
	windowStart := nowMs - 60_000

	l.mu.Lock()
	defer l.mu.Unlock()

	w := l.keys[keyID]
	if w == nil {
		w = &keyWindow{}
		l.keys[keyID] = w
	}
	// prune
	w.reqTimes = pruneTimes(w.reqTimes, windowStart)
	w.tokenEvents = pruneTokenEvents(w.tokenEvents, windowStart)

	usedRPM := int64(len(w.reqTimes))
	usedTPM := sumTokens(w.tokenEvents)

	if rpmLimit > 0 && usedRPM >= rpmLimit {
		retry := retryAfterMs(w.reqTimes, nowMs)
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "over_rpm",
			RetryAfter: time.Duration(retry) * time.Millisecond,
			UsedRPM:    usedRPM,
			UsedTPM:    usedTPM,
		}
	}
	if tpmLimit > 0 && estimatedTokens > 0 && usedTPM+estimatedTokens > tpmLimit {
		retry := retryAfterTokenMs(w.tokenEvents, nowMs)
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "over_tpm",
			RetryAfter: time.Duration(retry) * time.Millisecond,
			UsedRPM:    usedRPM,
			UsedTPM:    usedTPM,
		}
	}

	// reserve
	w.reqTimes = append(w.reqTimes, nowMs)
	if estimatedTokens > 0 && tpmLimit > 0 {
		w.tokenEvents = append(w.tokenEvents, tokenEvent{atMs: nowMs, tokens: estimatedTokens})
	}
	return AdmissionDecision{
		Allowed: true,
		UsedRPM: usedRPM + 1,
		UsedTPM: usedTPM + max64z(estimatedTokens, 0),
	}
}

// Snapshot returns current window usage for admin display.
func (l *KeyAdmissionLimiter) Snapshot(keyID int64) (usedRPM, usedTPM int64) {
	if l == nil || keyID <= 0 {
		return 0, 0
	}
	nowMs := l.nowFn().UTC().UnixMilli()
	windowStart := nowMs - 60_000
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.keys[keyID]
	if w == nil {
		return 0, 0
	}
	w.reqTimes = pruneTimes(w.reqTimes, windowStart)
	w.tokenEvents = pruneTokenEvents(w.tokenEvents, windowStart)
	return int64(len(w.reqTimes)), sumTokens(w.tokenEvents)
}

func pruneTimes(in []int64, windowStart int64) []int64 {
	if len(in) == 0 {
		return in
	}
	i := 0
	for i < len(in) && in[i] < windowStart {
		i++
	}
	if i == 0 {
		return in
	}
	out := make([]int64, len(in)-i)
	copy(out, in[i:])
	return out
}

func pruneTokenEvents(in []tokenEvent, windowStart int64) []tokenEvent {
	if len(in) == 0 {
		return in
	}
	i := 0
	for i < len(in) && in[i].atMs < windowStart {
		i++
	}
	if i == 0 {
		return in
	}
	out := make([]tokenEvent, len(in)-i)
	copy(out, in[i:])
	return out
}

func sumTokens(in []tokenEvent) int64 {
	var s int64
	for _, e := range in {
		s += e.tokens
	}
	return s
}

func retryAfterMs(times []int64, nowMs int64) int64 {
	if len(times) == 0 {
		return 1000
	}
	// oldest event leaves the window after (oldest+60s - now)
	oldest := times[0]
	remain := (oldest + 60_000) - nowMs
	if remain < 1000 {
		return 1000
	}
	return remain
}

func retryAfterTokenMs(events []tokenEvent, nowMs int64) int64 {
	if len(events) == 0 {
		return 1000
	}
	oldest := events[0].atMs
	remain := (oldest + 60_000) - nowMs
	if remain < 1000 {
		return 1000
	}
	return remain
}

func max64z(v, floor int64) int64 {
	if v < floor {
		return floor
	}
	return v
}
