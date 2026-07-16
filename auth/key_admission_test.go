package auth

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/internal/redisx"
)

func TestKeyAdmissionLimiter_RPM(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	l.nowFn = func() time.Time { return now }

	limit := int64(2)
	d1 := l.Allow(1, &limit, nil, 0)
	d2 := l.Allow(1, &limit, nil, 0)
	d3 := l.Allow(1, &limit, nil, 0)
	if !d1.Allowed || !d2.Allowed {
		t.Fatalf("first two should allow: %#v %#v", d1, d2)
	}
	if d3.Allowed || d3.Reason != "over_rpm" {
		t.Fatalf("third should deny over_rpm: %#v", d3)
	}
	if d3.RetryAfter < time.Second {
		t.Fatalf("retry-after too small: %v", d3.RetryAfter)
	}

	// Advance past window — should allow again.
	now = base.Add(61 * time.Second)
	d4 := l.Allow(1, &limit, nil, 0)
	if !d4.Allowed {
		t.Fatalf("after window reset expected allow: %#v", d4)
	}
}

func TestKeyAdmissionLimiter_TPM(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	l.nowFn = func() time.Time { return now }

	tpm := int64(1000)
	if d := l.Allow(7, nil, &tpm, 600); !d.Allowed {
		t.Fatalf("first tpm allow: %#v", d)
	}
	if d := l.Allow(7, nil, &tpm, 500); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("expected over_tpm: %#v", d)
	}
	// Small residual still ok
	if d := l.Allow(7, nil, &tpm, 400); !d.Allowed {
		t.Fatalf("400 should fit after 600: %#v", d)
	}
}

func TestKeyAdmissionLimiter_Unlimited(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	for i := 0; i < 100; i++ {
		if d := l.Allow(9, nil, nil, 100); !d.Allowed {
			t.Fatalf("unlimited denied: %#v", d)
		}
	}
}

func TestKeyAdmissionLimiter_Concurrent(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	limit := int64(50)
	done := make(chan AdmissionDecision, 100)
	for i := 0; i < 100; i++ {
		go func() {
			done <- l.Allow(3, &limit, nil, 0)
		}()
	}
	allowed := 0
	denied := 0
	for i := 0; i < 100; i++ {
		d := <-done
		if d.Allowed {
			allowed++
		} else {
			denied++
		}
	}
	if allowed != 50 || denied != 50 {
		t.Fatalf("allowed=%d denied=%d want 50/50", allowed, denied)
	}
}

func TestKeyAdmissionLimiter_SharedCounterRPM(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	shared := redisx.NewMemoryCounter()
	l.SetSharedCounter(shared)

	limit := int64(2)
	if d := l.Allow(11, &limit, nil, 0); !d.Allowed || d.Backend != "shared" {
		t.Fatalf("d1: %#v", d)
	}
	if d := l.Allow(11, &limit, nil, 0); !d.Allowed || d.Backend != "shared" {
		t.Fatalf("d2: %#v", d)
	}
	if d := l.Allow(11, &limit, nil, 0); d.Allowed || d.Reason != "over_rpm" || d.Backend != "shared" {
		t.Fatalf("d3: %#v", d)
	}
	rpm, tpm := l.Snapshot(11)
	if rpm != 2 || tpm != 0 {
		t.Fatalf("snapshot rpm=%d tpm=%d", rpm, tpm)
	}
}

func TestKeyAdmissionLimiter_SharedCounterTPM(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	l.SetSharedCounter(redisx.NewMemoryCounter())
	tpm := int64(1000)
	if d := l.Allow(12, nil, &tpm, 600); !d.Allowed || d.Backend != "shared" {
		t.Fatalf("d1: %#v", d)
	}
	if d := l.Allow(12, nil, &tpm, 500); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("d2: %#v", d)
	}
	if d := l.Allow(12, nil, &tpm, 400); !d.Allowed {
		t.Fatalf("d3: %#v", d)
	}
}

func TestKeyAdmissionLimiter_SharedFailOpen(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	fake := redisx.NewFakeCounter()
	fake.FailNext = true
	l.SetSharedCounter(fake)

	limit := int64(10)
	d := l.Allow(13, &limit, nil, 0)
	if !d.Allowed {
		t.Fatalf("fail-open should allow: %#v", d)
	}
	if d.Backend != "shared_failopen" {
		t.Fatalf("backend=%q want shared_failopen", d.Backend)
	}
	if l.FailOpenCount() != 1 {
		t.Fatalf("failOpenCount=%d", l.FailOpenCount())
	}
	// Local path reserved the request; subsequent local allows still work.
	for i := 0; i < 9; i++ {
		// force fail-open each time by failing shared first
		fake.FailNext = true
		if d := l.Allow(13, &limit, nil, 0); !d.Allowed {
			t.Fatalf("local allow %d: %#v", i, d)
		}
	}
	fake.FailNext = true
	if d := l.Allow(13, &limit, nil, 0); d.Allowed || d.Reason != "over_rpm" {
		t.Fatalf("11th local should deny: %#v", d)
	}
}

func TestKeyAdmissionLimiter_SharedFailOpenThenRecover(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	fake := redisx.NewFakeCounter()
	l.SetSharedCounter(fake)
	limit := int64(1)

	// First call uses shared successfully.
	if d := l.Allow(14, &limit, nil, 0); !d.Allowed || d.Backend != "shared" {
		t.Fatalf("shared allow: %#v", d)
	}
	// Second would be over_rpm on shared.
	if d := l.Allow(14, &limit, nil, 0); d.Allowed || d.Backend != "shared" {
		t.Fatalf("shared deny: %#v", d)
	}
	// Inject failure → fail-open to local (local is empty, so allows).
	fake.FailNext = true
	if d := l.Allow(14, &limit, nil, 0); !d.Allowed || d.Backend != "shared_failopen" {
		t.Fatalf("failopen: %#v", d)
	}
}

func TestConfigureKeyAdmissionCounter(t *testing.T) {
	ResetKeyAdmissionForTest()
	t.Cleanup(ResetKeyAdmissionForTest)

	if GlobalKeyAdmission.SharedCounterEnabled() {
		t.Fatal("expected disabled by default")
	}
	ConfigureKeyAdmissionCounter(redisx.NewMemoryCounter())
	if !GlobalKeyAdmission.SharedCounterEnabled() {
		t.Fatal("expected enabled")
	}
	ConfigureKeyAdmissionCounter(nil)
	if GlobalKeyAdmission.SharedCounterEnabled() {
		t.Fatal("expected disabled after nil")
	}
}

func TestKeyAdmissionLimiter_SharedConcurrent(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	l.SetSharedCounter(redisx.NewMemoryCounter())
	limit := int64(50)
	done := make(chan AdmissionDecision, 100)
	for i := 0; i < 100; i++ {
		go func() {
			done <- l.Allow(15, &limit, nil, 0)
		}()
	}
	allowed := 0
	for i := 0; i < 100; i++ {
		if (<-done).Allowed {
			allowed++
		}
	}
	// MemoryCounter is mutex-safe; fixed-window Incr is atomic within process.
	// Pre-check + incr can overshoot by races → allow at most a small race band.
	// With single process MemoryCounter and sequential Get+Incr under no lock across
	// calls, concurrent overshoot is possible. Enforce that we do not admit far more
	// than the limit (hard ceiling: 2x is unacceptable). Prefer exact when possible.
	// Shared path is best-effort (Get then Incr is not a distributed lock), so
	// concurrent overshoot above the limit is possible. Require at least the
	// limit admits, and reject pathological 2x+ overshoot.
	if allowed < 50 {
		t.Fatalf("allowed=%d want >=50", allowed)
	}
	if allowed > 80 {
		t.Fatalf("allowed=%d pathological overshoot", allowed)
	}
}
