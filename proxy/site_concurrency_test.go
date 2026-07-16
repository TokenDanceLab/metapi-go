package proxy

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSiteConcurrencyLimiter_Unlimited(t *testing.T) {
	lim := NewSiteConcurrencyLimiter()

	for _, limit := range []int64{0, -1} {
		slots := make([]*SiteSlot, 0, 8)
		for i := 0; i < 8; i++ {
			slot, ok := lim.TryAcquire(11, limit)
			if !ok || slot == nil {
				t.Fatalf("limit=%d attempt=%d: expected acquire", limit, i)
			}
			slots = append(slots, slot)
		}
		if got := lim.ActiveCount(11); got != 0 {
			t.Fatalf("limit=%d ActiveCount=%d, want 0 (unlimited tracks nothing)", limit, got)
		}
		for _, s := range slots {
			s.Release()
		}
	}
}

func TestSiteConcurrencyLimiter_SaturateAndRelease(t *testing.T) {
	lim := NewSiteConcurrencyLimiter()
	const siteID int64 = 42

	s1, ok := lim.TryAcquire(siteID, 2)
	if !ok {
		t.Fatal("first acquire failed")
	}
	s2, ok := lim.TryAcquire(siteID, 2)
	if !ok {
		t.Fatal("second acquire failed")
	}
	if got := lim.ActiveCount(siteID); got != 2 {
		t.Fatalf("ActiveCount=%d, want 2", got)
	}

	if slot, ok := lim.TryAcquire(siteID, 2); ok || slot != nil {
		t.Fatalf("expected saturate, got ok=%v slot=%v", ok, slot)
	}

	s1.Release()
	if got := lim.ActiveCount(siteID); got != 1 {
		t.Fatalf("after release ActiveCount=%d, want 1", got)
	}

	s3, ok := lim.TryAcquire(siteID, 2)
	if !ok {
		t.Fatal("re-acquire after release failed")
	}
	s2.Release()
	s3.Release()
	// Double-release is idempotent.
	s3.Release()

	if got := lim.ActiveCount(siteID); got != 0 {
		t.Fatalf("final ActiveCount=%d, want 0", got)
	}
}

func TestSiteConcurrencyLimiter_SitesIndependent(t *testing.T) {
	lim := NewSiteConcurrencyLimiter()

	a, ok := lim.TryAcquire(1, 1)
	if !ok {
		t.Fatal("site1 acquire failed")
	}
	if _, ok := lim.TryAcquire(1, 1); ok {
		t.Fatal("site1 should be saturated")
	}
	b, ok := lim.TryAcquire(2, 1)
	if !ok {
		t.Fatal("site2 must remain independent")
	}
	a.Release()
	b.Release()
}

func TestSiteConcurrencyLimiter_ConcurrentRace(t *testing.T) {
	lim := NewSiteConcurrencyLimiter()
	const (
		siteID  int64 = 7
		limit   int64 = 4
		workers       = 32
	)

	var acquired atomic.Int64
	var rejected atomic.Int64
	var maxSeen atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				slot, ok := lim.TryAcquire(siteID, limit)
				if !ok {
					rejected.Add(1)
					continue
				}
				acquired.Add(1)
				cur := int64(lim.ActiveCount(siteID))
				for {
					prev := maxSeen.Load()
					if cur <= prev || maxSeen.CompareAndSwap(prev, cur) {
						break
					}
				}
				time.Sleep(time.Microsecond)
				slot.Release()
			}
		}()
	}
	wg.Wait()

	if got := lim.ActiveCount(siteID); got != 0 {
		t.Fatalf("leaked ActiveCount=%d", got)
	}
	if max := maxSeen.Load(); max > limit {
		t.Fatalf("observed concurrent holders=%d > limit=%d", max, limit)
	}
	if acquired.Load() == 0 {
		t.Fatal("expected some acquires")
	}
	if rejected.Load() == 0 {
		t.Fatal("expected some rejections under contention")
	}
}

func TestSiteSlot_NilSafe(t *testing.T) {
	var slot *SiteSlot
	slot.Release() // must not panic

	lim := NewSiteConcurrencyLimiter()
	s, ok := lim.TryAcquire(0, 5)
	if !ok || s == nil {
		t.Fatal("invalid siteID should no-op acquire")
	}
	s.Release()
}
