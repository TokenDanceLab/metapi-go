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
