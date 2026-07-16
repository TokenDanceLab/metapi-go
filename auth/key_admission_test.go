package auth

import (
	"testing"
	"time"
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
