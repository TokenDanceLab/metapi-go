package routing

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// Route cache creation and TTL
// =============================================================================

func TestNewRouteCache_MinimumTTL(t *testing.T) {
	cache := NewRouteCache(0)
	if cache.ttlMs != 100 {
		t.Errorf("expected minimum TTL 100ms, got %d", cache.ttlMs)
	}

	cache = NewRouteCache(50)
	if cache.ttlMs != 100 {
		t.Errorf("expected minimum TTL 100ms for 50ms input, got %d", cache.ttlMs)
	}

	cache = NewRouteCache(5000)
	if cache.ttlMs != 5000 {
		t.Errorf("expected TTL 5000ms, got %d", cache.ttlMs)
	}
}

func TestRouteCache_RoutesGetSet(t *testing.T) {
	cache := NewRouteCache(1000)

	// Initially not fresh
	if cache.IsRoutesFresh() {
		t.Error("expected routes NOT fresh initially")
	}
	if routes := cache.GetRoutes(); routes != nil {
		t.Error("expected nil routes initially")
	}

	// Set routes
	routes := []store.TokenRoute{
		{ID: 1, ModelPattern: "gpt-4", Enabled: true},
		{ID: 2, ModelPattern: "claude-*", Enabled: true},
	}
	cache.SetRoutes(routes)

	// Should be fresh now
	if !cache.IsRoutesFresh() {
		t.Error("expected routes fresh after SetRoutes")
	}
	if got := cache.GetRoutes(); got == nil {
		t.Error("expected non-nil routes")
	} else if len(got) != 2 {
		t.Errorf("expected 2 routes, got %d", len(got))
	}
}

func TestRouteCache_RoutesExpiry(t *testing.T) {
	cache := NewRouteCache(1) // 1ms TTL, clamped to 100ms minimum

	routes := []store.TokenRoute{{ID: 1, ModelPattern: "gpt-4", Enabled: true}}
	cache.SetRoutes(routes)

	// Immediately fresh
	if !cache.IsRoutesFresh() {
		t.Error("expected fresh immediately")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)
	if cache.IsRoutesFresh() {
		t.Error("expected routes NOT fresh after TTL")
	}
	if routes := cache.GetRoutes(); routes != nil {
		t.Error("expected nil routes after expiry")
	}
}

// =============================================================================
// Route match cache
// =============================================================================

func TestRouteCache_MatchGetSet(t *testing.T) {
	cache := NewRouteCache(5000)

	match := &RouteMatch{
		Route:    store.TokenRoute{ID: 1, ModelPattern: "gpt-4"},
		Channels: []RouteChannelCandidate{},
	}

	// Get non-existent
	if got := cache.GetMatch(1); got != nil {
		t.Error("expected nil for non-existent match")
	}

	// Set and get
	cache.SetMatch(1, match)
	got := cache.GetMatch(1)
	if got == nil {
		t.Fatal("expected non-nil match")
	}
	if got.Route.ID != 1 {
		t.Errorf("expected route ID 1, got %d", got.Route.ID)
	}
}

func TestRouteCache_MatchExpiry(t *testing.T) {
	cache := NewRouteCache(1) // 1ms TTL, clamped to 100ms

	match := &RouteMatch{Route: store.TokenRoute{ID: 1}}
	cache.SetMatch(1, match)

	// Immediately fresh
	if got := cache.GetMatch(1); got == nil {
		t.Error("expected non-nil immediately")
	}

	time.Sleep(150 * time.Millisecond)
	if got := cache.GetMatch(1); got != nil {
		t.Error("expected nil after TTL expiry")
	}
}

// =============================================================================
// Cache patching
// =============================================================================

func TestRouteCache_PatchCachedChannel(t *testing.T) {
	cache := NewRouteCache(5000)

	ch := store.RouteChannel{ID: 42, Priority: 1}
	match := &RouteMatch{
		Route: store.TokenRoute{ID: 1},
		Channels: []RouteChannelCandidate{
			{Channel: store.RouteChannel{ID: 10, Priority: 5}},
			{Channel: ch},
			{Channel: store.RouteChannel{ID: 99, Priority: 9}},
		},
	}
	cache.SetMatch(1, match)

	// Patch channel 42
	var patchedPriority int64
	cache.PatchCachedChannel(42, func(ch *store.RouteChannel) {
		ch.Priority = 100
		patchedPriority = ch.Priority
	})

	if patchedPriority != 100 {
		t.Errorf("expected patched priority 100, got %d", patchedPriority)
	}

	// Verify patch persisted in cache
	got := cache.GetMatch(1)
	if got == nil {
		t.Fatal("match should still be in cache")
	}
	for _, c := range got.Channels {
		if c.Channel.ID == 42 {
			if c.Channel.Priority != 100 {
				t.Errorf("expected priority 100 in cached match, got %d", c.Channel.Priority)
			}
		}
	}

	// Patch non-existent channel (no-op)
	cache.PatchCachedChannel(99999, func(ch *store.RouteChannel) {
		ch.Priority = 999
	})
}

// =============================================================================
// Cache invalidation
// =============================================================================

func TestRouteCache_InvalidateRouteScopedCache(t *testing.T) {
	cache := NewRouteCache(5000)

	cache.SetMatch(1, &RouteMatch{Route: store.TokenRoute{ID: 1}})
	cache.SetMatch(2, &RouteMatch{Route: store.TokenRoute{ID: 2}})

	// Invalidate route 1
	cache.InvalidateRouteScopedCache(1)

	if got := cache.GetMatch(1); got != nil {
		t.Error("expected nil match for route 1 after invalidation")
	}
	if got := cache.GetMatch(2); got == nil {
		t.Error("expected non-nil match for route 2 (unaffected)")
	}

	// Invalidate with 0 or negative ID (no-op)
	cache.InvalidateRouteScopedCache(0)
	cache.InvalidateRouteScopedCache(-1)
}

func TestRouteCache_InvalidateAll(t *testing.T) {
	cache := NewRouteCache(5000)

	routes := []store.TokenRoute{{ID: 1}}
	cache.SetRoutes(routes)
	cache.SetMatch(1, &RouteMatch{Route: store.TokenRoute{ID: 1}})
	cache.SetMatch(2, &RouteMatch{Route: store.TokenRoute{ID: 2}})

	cache.InvalidateAll()

	if cache.IsRoutesFresh() {
		t.Error("routes should NOT be fresh after InvalidateAll")
	}
	if got := cache.GetRoutes(); got != nil {
		t.Error("routes should be nil after InvalidateAll")
	}
	if got := cache.GetMatch(1); got != nil {
		t.Error("match 1 should be nil after InvalidateAll")
	}
	if got := cache.GetMatch(2); got != nil {
		t.Error("match 2 should be nil after InvalidateAll")
	}
}

// =============================================================================
// Stable-first cache clearing via InvalidateAll
// =============================================================================

func TestInvalidateAll_ClearsStableFirstCaches(t *testing.T) {
	// Pre-populate stable-first caches
	stableFirstLastSelectedSiteByKey["1:gpt-4"] = 100
	stableFirstObservationProgressByKey["1:gpt-4"] = StableFirstObservationProgressState{RequestCount: 5}
	scopedKey := "1:gpt-4:200"
	stableFirstObservationSiteCooldownByKey[scopedKey] = 12345

	cache := NewRouteCache(5000)
	cache.InvalidateAll()

	if len(stableFirstLastSelectedSiteByKey) != 0 {
		t.Errorf("expected empty lastSelectedSite map, got %d", len(stableFirstLastSelectedSiteByKey))
	}
	if len(stableFirstObservationProgressByKey) != 0 {
		t.Errorf("expected empty observation progress map, got %d", len(stableFirstObservationProgressByKey))
	}
	if len(stableFirstObservationSiteCooldownByKey) != 0 {
		t.Errorf("expected empty site cooldown map, got %d", len(stableFirstObservationSiteCooldownByKey))
	}
}

// =============================================================================
// Stability under concurrent access (single-goroutine proxy)
// =============================================================================

func TestRouteCache_ConcurrentReadsDontPanic(t *testing.T) {
	cache := NewRouteCache(5000)
	routes := []store.TokenRoute{{ID: 1}}
	cache.SetRoutes(routes)
	cache.SetMatch(1, &RouteMatch{Route: store.TokenRoute{ID: 1}})

	// Multiple reads should not panic (mutex is no-op stub, so this is trivial)
	for i := 0; i < 100; i++ {
		cache.IsRoutesFresh()
		cache.GetRoutes()
		cache.GetMatch(1)
	}
}
