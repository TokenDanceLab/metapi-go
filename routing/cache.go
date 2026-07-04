package routing

import (
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// RouteCache caches enabled routes and per-route matches with TTL.
type RouteCache struct {
	mu           sync.RWMutex
	routesLoaded bool
	routesAt     int64
	routes       []store.TokenRoute
	matchCache   map[int64]*routeMatchEntry
	ttlMs        int64
}

type routeMatchEntry struct {
	loadedAt int64
	match    *RouteMatch
}

// NewRouteCache creates a new route cache with the given TTL in milliseconds.
func NewRouteCache(ttlMs int64) *RouteCache {
	if ttlMs < 100 {
		ttlMs = 100
	}
	return &RouteCache{
		matchCache: make(map[int64]*routeMatchEntry),
		ttlMs:      ttlMs,
	}
}

// IsRoutesFresh checks if the routes list is still fresh.
func (c *RouteCache) IsRoutesFresh() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.routesLoaded && (time.Now().UnixMilli()-c.routesAt < c.ttlMs)
}

// GetRoutes returns cached routes if fresh, nil otherwise.
func (c *RouteCache) GetRoutes() []store.TokenRoute {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.routesLoaded && (time.Now().UnixMilli()-c.routesAt < c.ttlMs) {
		return c.routes
	}
	return nil
}

// SetRoutes sets the cached routes.
func (c *RouteCache) SetRoutes(routes []store.TokenRoute) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes = routes
	c.routesAt = time.Now().UnixMilli()
	c.routesLoaded = true
}

// GetMatch returns a cached route match if fresh.
func (c *RouteCache) GetMatch(routeID int64) *RouteMatch {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.matchCache[routeID]
	if !ok {
		return nil
	}
	if time.Now().UnixMilli()-entry.loadedAt >= c.ttlMs {
		return nil
	}
	return entry.match
}

// SetMatch caches a route match.
func (c *RouteCache) SetMatch(routeID int64, match *RouteMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.matchCache[routeID] = &routeMatchEntry{
		loadedAt: time.Now().UnixMilli(),
		match:    match,
	}
}

// PatchCachedChannel applies a mutation to a channel in all cached matches.
func (c *RouteCache) PatchCachedChannel(channelID int64, apply func(ch *store.RouteChannel)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.matchCache {
		if entry.match == nil {
			continue
		}
		for i := range entry.match.Channels {
			if entry.match.Channels[i].Channel.ID == channelID {
				apply(&entry.match.Channels[i].Channel)
				break
			}
		}
	}
}

// InvalidateRouteScopedCache clears the cache for a specific route.
func (c *RouteCache) InvalidateRouteScopedCache(routeID int64) {
	if routeID <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.matchCache, routeID)
	ClearStableFirstCachesForRoute(routeID)
}

// InvalidateAll clears all caches.
func (c *RouteCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routesLoaded = false
	c.routes = nil
	c.routesAt = 0
	c.matchCache = make(map[int64]*routeMatchEntry)
	// Clear stable first global state
	clearAllStableFirstCaches()
}

func clearAllStableFirstCaches() {
	for k := range stableFirstLastSelectedSiteByKey {
		delete(stableFirstLastSelectedSiteByKey, k)
	}
	for k := range stableFirstObservationProgressByKey {
		delete(stableFirstObservationProgressByKey, k)
	}
	for k := range stableFirstObservationSiteCooldownByKey {
		delete(stableFirstObservationSiteCooldownByKey, k)
	}
}
