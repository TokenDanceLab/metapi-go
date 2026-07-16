package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyRPMLimiter_Unlimited(t *testing.T) {
	lim := NewKeyRPMLimiter(time.Minute)
	for _, limit := range []int64{0, -1} {
		for i := 0; i < 20; i++ {
			d := lim.TryAdmit(1, limit)
			if !d.Allowed {
				t.Fatalf("limit=%d attempt=%d: expected allow", limit, i)
			}
		}
		used, _ := lim.Snapshot(1)
		if used != 0 {
			t.Fatalf("limit=%d Snapshot used=%d, want 0 (unlimited tracks nothing)", limit, used)
		}
	}
}

func TestKeyRPMLimiter_AllowWithinWindow(t *testing.T) {
	lim := NewKeyRPMLimiter(time.Minute)
	const keyID int64 = 7
	const limit int64 = 3

	for i := 0; i < 3; i++ {
		d := lim.TryAdmit(keyID, limit)
		if !d.Allowed {
			t.Fatalf("attempt %d denied unexpectedly (used=%d limit=%d)", i, d.Used, d.Limit)
		}
		if d.Used != i+1 {
			t.Fatalf("attempt %d Used=%d want %d", i, d.Used, i+1)
		}
	}

	d := lim.TryAdmit(keyID, limit)
	if d.Allowed {
		t.Fatal("expected 4th request denied")
	}
	if d.Used != 3 {
		t.Fatalf("denied Used=%d want 3", d.Used)
	}
	if d.RetryAfter < time.Second {
		t.Fatalf("RetryAfter=%v want >= 1s", d.RetryAfter)
	}
}

func TestKeyRPMLimiter_WindowReset(t *testing.T) {
	lim := NewKeyRPMLimiter(100 * time.Millisecond)
	// Inject a controllable clock.
	var now atomic.Value
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now.Store(base)
	lim.now = func() time.Time { return now.Load().(time.Time) }

	const keyID int64 = 9
	const limit int64 = 2

	if d := lim.TryAdmit(keyID, limit); !d.Allowed {
		t.Fatal("first admit failed")
	}
	if d := lim.TryAdmit(keyID, limit); !d.Allowed {
		t.Fatal("second admit failed")
	}
	if d := lim.TryAdmit(keyID, limit); d.Allowed {
		t.Fatal("expected deny at limit")
	}

	// Advance just past the window so both timestamps fall out.
	now.Store(base.Add(101 * time.Millisecond))
	d := lim.TryAdmit(keyID, limit)
	if !d.Allowed {
		t.Fatalf("expected allow after window reset, got used=%d retry=%v", d.Used, d.RetryAfter)
	}
	if d.Used != 1 {
		t.Fatalf("after reset Used=%d want 1", d.Used)
	}
}

func TestKeyRPMLimiter_KeysIndependent(t *testing.T) {
	lim := NewKeyRPMLimiter(time.Minute)

	if d := lim.TryAdmit(1, 1); !d.Allowed {
		t.Fatal("key1 first admit failed")
	}
	if d := lim.TryAdmit(1, 1); d.Allowed {
		t.Fatal("key1 should be saturated")
	}
	if d := lim.TryAdmit(2, 1); !d.Allowed {
		t.Fatal("key2 must remain independent")
	}
}

func TestKeyRPMLimiter_ConcurrentAdmission(t *testing.T) {
	lim := NewKeyRPMLimiter(time.Minute)
	const (
		keyID   int64 = 42
		limit   int64 = 10
		workers       = 32
	)

	var allowed atomic.Int64
	var denied atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if lim.TryAdmit(keyID, limit).Allowed {
					allowed.Add(1)
				} else {
					denied.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != limit {
		t.Fatalf("allowed=%d want exactly limit=%d (denied=%d)", got, limit, denied.Load())
	}
	used, _ := lim.Snapshot(keyID)
	if int64(used) != limit {
		t.Fatalf("Snapshot used=%d want %d", used, limit)
	}
}

func TestKeyRPMLimiter_SnapshotAndReset(t *testing.T) {
	lim := NewKeyRPMLimiter(time.Minute)
	_ = lim.TryAdmit(3, 5)
	_ = lim.TryAdmit(3, 5)
	used, win := lim.Snapshot(3)
	if used != 2 {
		t.Fatalf("Snapshot used=%d want 2", used)
	}
	if win != time.Minute {
		t.Fatalf("window=%v want 1m", win)
	}
	lim.Reset(3)
	used, _ = lim.Snapshot(3)
	if used != 0 {
		t.Fatalf("after Reset used=%d want 0", used)
	}
}

func TestFormatRetryAfterSeconds(t *testing.T) {
	if got := formatRetryAfterSeconds(1500 * time.Millisecond); got != "2" && got != "1" {
		// Round(1.5s)=2s; accept either if platform rounding differs.
		t.Fatalf("got %q", got)
	}
	if got := formatRetryAfterSeconds(200 * time.Millisecond); got != "1" {
		t.Fatalf("min 1s, got %q", got)
	}
}
