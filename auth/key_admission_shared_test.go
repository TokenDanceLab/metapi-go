package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeWindow struct {
	n   int64
	err error
}

func (f *fakeWindow) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.n++
	return f.n, nil
}

func (f *fakeWindow) IncrBy(ctx context.Context, key string, delta int64, window time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	if delta > 0 {
		f.n += delta
	}
	return f.n, nil
}

func (f *fakeWindow) Get(ctx context.Context, key string) (int64, error) {
	return f.n, f.err
}

func TestKeyAdmission_SharedRPM(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	fw := &fakeWindow{}
	l.SetSharedRPMCounter(fw)
	limit := int64(2)
	if d := l.Allow(1, &limit, nil, 0); !d.Allowed {
		t.Fatalf("1: %#v", d)
	}
	if d := l.Allow(1, &limit, nil, 0); !d.Allowed {
		t.Fatalf("2: %#v", d)
	}
	if d := l.Allow(1, &limit, nil, 0); d.Allowed || d.Reason != "over_rpm" {
		t.Fatalf("3 should deny: %#v", d)
	}
}

func TestKeyAdmission_SharedRPM_FailOpen(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	fw := &fakeWindow{err: errors.New("redis down")}
	l.SetSharedRPMCounter(fw)
	limit := int64(100)
	// Should not hard-fail; local path allows.
	if d := l.Allow(2, &limit, nil, 0); !d.Allowed {
		t.Fatalf("fail-open expected allow: %#v", d)
	}
}

func TestKeyAdmission_SharedTPM(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	fw := &fakeWindow{}
	l.SetSharedTPMCounter(fw)
	tpm := int64(1000)
	d := l.Allow(11, nil, &tpm, 600)
	if !d.Allowed {
		t.Fatalf("1: %#v", d)
	}
	if d.UsedTPM != 600 {
		t.Fatalf("used tpm want 600 got %d", d.UsedTPM)
	}
	// 400 fits (600+400=1000); shared IncrBy-before-check semantics (same as RPM).
	if d := l.Allow(11, nil, &tpm, 400); !d.Allowed {
		t.Fatalf("2 should allow 400 after 600: %#v", d)
	}
	if d := l.Allow(11, nil, &tpm, 1); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("3 should deny over_tpm: %#v", d)
	}
}

func TestKeyAdmission_SharedTPM_FailOpen(t *testing.T) {
	l := NewKeyAdmissionLimiter()
	fw := &fakeWindow{err: errors.New("redis down")}
	l.SetSharedTPMCounter(fw)
	tpm := int64(1000)
	// Fail-open to local tokenEvents — first 600 should allow.
	if d := l.Allow(12, nil, &tpm, 600); !d.Allowed {
		t.Fatalf("fail-open expected allow: %#v", d)
	}
	// Local path still enforces after fail-open.
	if d := l.Allow(12, nil, &tpm, 500); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("local over_tpm after fail-open: %#v", d)
	}
}
