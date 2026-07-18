package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/internal/sharedcount"
)

// fakeWindow is a dual-key WindowCounter for shared RPM/TPM tests.
// Keys containing "tpm" use the tokens field; others use the n field.
type fakeWindow struct {
	n      int64
	tokens int64
	err    error
	// lastDelta records the most recent IncrBy delta (for rollback assertions).
	lastDelta int64
}

func (f *fakeWindow) isTPM(key string) bool {
	return len(key) >= 11 && key[:11] == "metapi:tpm:"
}

func (f *fakeWindow) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	_ = ctx
	_ = window
	if f.err != nil {
		return 0, f.err
	}
	if f.isTPM(key) {
		f.tokens++
		return f.tokens, nil
	}
	f.n++
	return f.n, nil
}

func (f *fakeWindow) Decr(ctx context.Context, key string, window time.Duration) (int64, error) {
	_ = ctx
	_ = window
	if f.err != nil {
		return 0, f.err
	}
	if f.isTPM(key) {
		if f.tokens > 0 {
			f.tokens--
		}
		return f.tokens, nil
	}
	if f.n > 0 {
		f.n--
	}
	return f.n, nil
}

func (f *fakeWindow) IncrBy(ctx context.Context, key string, delta int64, window time.Duration) (int64, error) {
	_ = ctx
	_ = window
	if f.err != nil {
		return 0, f.err
	}
	f.lastDelta = delta
	if f.isTPM(key) {
		f.tokens += delta
		if f.tokens < 0 {
			f.tokens = 0
		}
		return f.tokens, nil
	}
	f.n += delta
	if f.n < 0 {
		f.n = 0
	}
	return f.n, nil
}

func (f *fakeWindow) Get(ctx context.Context, key string) (int64, error) {
	if f.isTPM(key) {
		return f.tokens, f.err
	}
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
	// #513: over_rpm rolls back the denied Incr so the window stays at the limit.
	if fw.n != 2 {
		t.Fatalf("after over_rpm deny want counter=2 got %d", fw.n)
	}
	// A subsequent allow should succeed once capacity frees — with rollback,
	// counter remains at 2 so still deny; bump by rolling window is not simulated.
	// Explicitly free one slot then admit.
	if _, err := fw.Decr(context.Background(), "metapi:rpm:1", time.Minute); err != nil {
		t.Fatal(err)
	}
	if d := l.Allow(1, &limit, nil, 0); !d.Allowed {
		t.Fatalf("after free slot should allow: %#v", d)
	}
	if fw.n != 2 {
		t.Fatalf("after re-admit want counter=2 got %d", fw.n)
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
	// #513: over_tpm rolls back the denied IncrBy.
	if fw.tokens != 1000 {
		t.Fatalf("after over_tpm deny want tokens=1000 got %d", fw.tokens)
	}
	if fw.lastDelta != -1 {
		t.Fatalf("want lastDelta=-1 rollback got %d", fw.lastDelta)
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

func TestKeyAdmission_SharedRPM_ThenTPM_CompoundRollback(t *testing.T) {
	// RPM ok + TPM deny must undo BOTH reservations (#513).
	l := NewKeyAdmissionLimiter()
	fw := &fakeWindow{}
	l.SetSharedRPMCounter(fw)
	l.SetSharedTPMCounter(fw)
	rpm := int64(10)
	tpm := int64(100)
	// Fill TPM almost full.
	if d := l.Allow(21, &rpm, &tpm, 90); !d.Allowed {
		t.Fatalf("seed allow: %#v", d)
	}
	if fw.n != 1 || fw.tokens != 90 {
		t.Fatalf("seed state n=%d tokens=%d", fw.n, fw.tokens)
	}
	// Next: RPM would succeed (2<=10) but TPM 90+20=110 > 100 → deny both.
	if d := l.Allow(21, &rpm, &tpm, 20); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("compound deny expected over_tpm: %#v", d)
	}
	if fw.n != 1 {
		t.Fatalf("RPM reservation must roll back: n=%d want 1", fw.n)
	}
	if fw.tokens != 90 {
		t.Fatalf("TPM reservation must roll back: tokens=%d want 90", fw.tokens)
	}
	// Capacity remains usable for a fitting request.
	if d := l.Allow(21, &rpm, &tpm, 10); !d.Allowed {
		t.Fatalf("fitting request after compound deny: %#v", d)
	}
	if fw.n != 2 || fw.tokens != 100 {
		t.Fatalf("after fit n=%d tokens=%d want 2/100", fw.n, fw.tokens)
	}
}

func TestKeyAdmission_SharedMemoryCounter_Parity(t *testing.T) {
	// MemoryCounter implements the same Decr/negative IncrBy rollback path.
	// Use two separate counters because MemoryCounter keys share one map but
	// RPM vs TPM use different key namespaces via the limiter.
	rpmC := sharedcount.NewMemoryCounter()
	tpmC := sharedcount.NewMemoryCounter()
	l := NewKeyAdmissionLimiter()
	l.SetSharedRPMCounter(rpmC)
	l.SetSharedTPMCounter(tpmC)

	rpm := int64(2)
	tpm := int64(100)

	if d := l.Allow(31, &rpm, &tpm, 40); !d.Allowed {
		t.Fatalf("1: %#v", d)
	}
	if d := l.Allow(31, &rpm, &tpm, 40); !d.Allowed {
		t.Fatalf("2: %#v", d)
	}
	// RPM deny: should roll back so used RPM stays 2.
	if d := l.Allow(31, &rpm, &tpm, 1); d.Allowed || d.Reason != "over_rpm" {
		t.Fatalf("over_rpm: %#v", d)
	}
	// Free one RPM via Decr directly on the counter, then hit TPM deny compound.
	if _, err := rpmC.Decr(context.Background(), "metapi:rpm:31", time.Minute); err != nil {
		t.Fatal(err)
	}
	// tokens currently 80; request 30 → 110 > 100, both roll back.
	if d := l.Allow(31, &rpm, &tpm, 30); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("over_tpm compound: %#v", d)
	}
	// After compound rollback, RPM should still be 1 (post-decr) and TPM 80.
	n, err := rpmC.Get(context.Background(), "metapi:rpm:31")
	if err != nil || n != 1 {
		t.Fatalf("rpm after compound rollback n=%d err=%v", n, err)
	}
	// TPM uses IncrBy points; Get only returns unit times — read via IncrBy 0.
	tok, err := tpmC.IncrBy(context.Background(), "metapi:tpm:31", 0, time.Minute)
	if err != nil || tok != 80 {
		t.Fatalf("tpm after compound rollback tok=%d err=%v", tok, err)
	}
}
