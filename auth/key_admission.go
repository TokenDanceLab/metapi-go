package auth

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tokendancelab/metapi-go/internal/redisx"
)

// KeyAdmissionLimiter is a sliding/fixed-window RPM/TPM gate for managed
// downstream keys (learn #116). Default unlimited when limits are nil/<=0.
//
// When a SharedCounter backend is configured via ConfigureKeyAdmissionCounter
// (typically Redis from REDIS_URL, learn #118), RPM/TPM counters are shared
// across instances. Redis errors fail-open to the local memory window so a
// Redis outage never hard-blocks traffic.
type KeyAdmissionLimiter struct {
	mu   sync.Mutex
	keys map[int64]*keyWindow
	// nowFn is injectable for tests.
	nowFn func() time.Time

	// shared is an optional multi-instance counter (Redis fixed-window).
	// nil → pure process-local sliding window.
	shared redisx.SharedCounter
	// failOpenCount tracks Redis/shared backend errors (observability).
	failOpenCount atomic.Uint64
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

// ConfigureKeyAdmissionCounter installs a SharedCounter backend on the global
// limiter. Pass nil to disable shared mode (process-local only).
func ConfigureKeyAdmissionCounter(counter redisx.SharedCounter) {
	if GlobalKeyAdmission == nil {
		GlobalKeyAdmission = NewKeyAdmissionLimiter()
	}
	GlobalKeyAdmission.SetSharedCounter(counter)
}

// SetSharedCounter installs a SharedCounter on this limiter (nil disables).
func (l *KeyAdmissionLimiter) SetSharedCounter(counter redisx.SharedCounter) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shared = counter
}

// SharedCounterEnabled reports whether a non-nil shared backend is installed.
func (l *KeyAdmissionLimiter) SharedCounterEnabled() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.shared != nil
}

// FailOpenCount returns how many times shared-backend errors caused fail-open.
func (l *KeyAdmissionLimiter) FailOpenCount() uint64 {
	if l == nil {
		return 0
	}
	return l.failOpenCount.Load()
}

// AdmissionDecision is the result of Allow.
type AdmissionDecision struct {
	Allowed    bool
	Reason     string // "" | "over_rpm" | "over_tpm"
	RetryAfter time.Duration
	UsedRPM    int64
	UsedTPM    int64
	// Backend is "memory" | "shared" | "shared_failopen" for diagnostics.
	Backend string
}

const admissionWindow = time.Minute

func rpmKey(keyID int64) string {
	return fmt.Sprintf("admission:rpm:%d", keyID)
}

func tpmKey(keyID int64) string {
	return fmt.Sprintf("admission:tpm:%d", keyID)
}

// Allow checks and records a request against optional RPM/TPM limits.
// estimatedTokens is reserved against TPM when maxTPM is set; pass 0 to skip TPM accounting.
// When allowed, the request is recorded immediately (admission reservation).
//
// Failure mode with shared backend: on Redis/shared errors the limiter fails
// open by falling back to the process-local window (and increments FailOpenCount).
func (l *KeyAdmissionLimiter) Allow(keyID int64, maxRPM, maxTPM *int64, estimatedTokens int64) AdmissionDecision {
	if l == nil || keyID <= 0 {
		return AdmissionDecision{Allowed: true, Backend: "memory"}
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
		return AdmissionDecision{Allowed: true, Backend: "memory"}
	}

	l.mu.Lock()
	shared := l.shared
	l.mu.Unlock()

	if shared != nil {
		d, ok := l.allowShared(shared, keyID, rpmLimit, tpmLimit, estimatedTokens)
		if ok {
			return d
		}
		// fail-open → local path
		l.failOpenCount.Add(1)
		slog.Warn("key admission shared counter failed; falling back to process-local",
			"key_id", keyID,
			"fail_open_count", l.failOpenCount.Load(),
		)
		d = l.allowLocal(keyID, rpmLimit, tpmLimit, estimatedTokens)
		d.Backend = "shared_failopen"
		return d
	}
	return l.allowLocal(keyID, rpmLimit, tpmLimit, estimatedTokens)
}

// allowShared returns (decision, true) on success, or (_, false) to fail-open.
func (l *KeyAdmissionLimiter) allowShared(
	shared redisx.SharedCounter,
	keyID, rpmLimit, tpmLimit, estimatedTokens int64,
) (AdmissionDecision, bool) {
	// Snapshot current usage first (best-effort) for Retry-After / admin display.
	usedRPM, err := shared.Get(rpmKey(keyID))
	if err != nil {
		return AdmissionDecision{}, false
	}
	usedTPM, err := shared.Get(tpmKey(keyID))
	if err != nil {
		return AdmissionDecision{}, false
	}

	if rpmLimit > 0 && usedRPM >= rpmLimit {
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "over_rpm",
			RetryAfter: time.Second, // fixed-window approximation
			UsedRPM:    usedRPM,
			UsedTPM:    usedTPM,
			Backend:    "shared",
		}, true
	}
	if tpmLimit > 0 && estimatedTokens > 0 && usedTPM+estimatedTokens > tpmLimit {
		return AdmissionDecision{
			Allowed:    false,
			Reason:     "over_tpm",
			RetryAfter: time.Second,
			UsedRPM:    usedRPM,
			UsedTPM:    usedTPM,
			Backend:    "shared",
		}, true
	}

	// Reserve RPM (always, when limited) then TPM.
	if rpmLimit > 0 {
		n, err := shared.IncrWindow(rpmKey(keyID), admissionWindow)
		if err != nil {
			return AdmissionDecision{}, false
		}
		usedRPM = n
		// Race: two instances may both pass the pre-check; enforce post-incr.
		if usedRPM > rpmLimit {
			return AdmissionDecision{
				Allowed:    false,
				Reason:     "over_rpm",
				RetryAfter: time.Second,
				UsedRPM:    usedRPM,
				UsedTPM:    usedTPM,
				Backend:    "shared",
			}, true
		}
	}
	if tpmLimit > 0 && estimatedTokens > 0 {
		n, err := shared.IncrWindowBy(tpmKey(keyID), estimatedTokens, admissionWindow)
		if err != nil {
			return AdmissionDecision{}, false
		}
		usedTPM = n
		if usedTPM > tpmLimit {
			return AdmissionDecision{
				Allowed:    false,
				Reason:     "over_tpm",
				RetryAfter: time.Second,
				UsedRPM:    usedRPM,
				UsedTPM:    usedTPM,
				Backend:    "shared",
			}, true
		}
	}
	return AdmissionDecision{
		Allowed: true,
		UsedRPM: usedRPM,
		UsedTPM: usedTPM,
		Backend: "shared",
	}, true
}

func (l *KeyAdmissionLimiter) allowLocal(keyID, rpmLimit, tpmLimit, estimatedTokens int64) AdmissionDecision {
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
			Backend:    "memory",
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
			Backend:    "memory",
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
		Backend: "memory",
	}
}

// Snapshot returns current window usage for admin display.
func (l *KeyAdmissionLimiter) Snapshot(keyID int64) (usedRPM, usedTPM int64) {
	if l == nil || keyID <= 0 {
		return 0, 0
	}
	l.mu.Lock()
	shared := l.shared
	l.mu.Unlock()

	if shared != nil {
		rpm, err1 := shared.Get(rpmKey(keyID))
		tpm, err2 := shared.Get(tpmKey(keyID))
		if err1 == nil && err2 == nil {
			return rpm, tpm
		}
		// fail-open to local snapshot
		l.failOpenCount.Add(1)
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
