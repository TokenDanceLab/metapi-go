package proxy

import "sync"

// SiteConcurrencyLimiter caps concurrent upstream dispatches per site.
// It is orthogonal to ProxyChannelCoordinator channel leases: a request may
// hold both a site slot and a channel lease independently.
//
// Semantics (sites.max_concurrency / Site.MaxConcurrency):
//   - limit <= 0  → unlimited (always acquire; no tracking)
//   - limit > 0   → at most that many concurrent held slots per siteID
//
// On saturation callers must skip to the next channel/site and must NOT mark
// the channel expired or record a routing failure cascade (FE-SITE-CONC / #594).
type SiteConcurrencyLimiter struct {
	mu    sync.Mutex
	sites map[int64]*siteConcurrencyState
}

type siteConcurrencyState struct {
	active int
}

// SiteSlot is a held (or no-op) site concurrency permit.
// Always call Release exactly once when the upstream attempt finishes.
type SiteSlot struct {
	siteID  int64
	limiter *SiteConcurrencyLimiter
	held    bool
}

// DefaultSiteConcurrencyLimiter is the process-global site limiter used by the
// proxy data plane when UpstreamConfig does not inject a custom instance.
var DefaultSiteConcurrencyLimiter = NewSiteConcurrencyLimiter()

// NewSiteConcurrencyLimiter creates an empty site-scoped limiter.
func NewSiteConcurrencyLimiter() *SiteConcurrencyLimiter {
	return &SiteConcurrencyLimiter{
		sites: make(map[int64]*siteConcurrencyState),
	}
}

// TryAcquire attempts to take one concurrency slot for siteID.
//
// Returns (slot, true) when the call may proceed. slot is never nil on success;
// for unlimited sites the slot is a no-op (Release is still safe).
// Returns (nil, false) when the site is saturated — caller should skip this
// site/channel without recording failure.
func (l *SiteConcurrencyLimiter) TryAcquire(siteID int64, maxConcurrency int64) (*SiteSlot, bool) {
	if l == nil || siteID <= 0 || maxConcurrency <= 0 {
		return &SiteSlot{siteID: siteID, limiter: l, held: false}, true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state := l.sites[siteID]
	if state == nil {
		state = &siteConcurrencyState{}
		l.sites[siteID] = state
	}
	if state.active >= int(maxConcurrency) {
		return nil, false
	}
	state.active++
	return &SiteSlot{siteID: siteID, limiter: l, held: true}, true
}

// ActiveCount returns the number of currently held slots for siteID.
// Unlimited / unknown sites report 0.
func (l *SiteConcurrencyLimiter) ActiveCount(siteID int64) int {
	if l == nil || siteID <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.sites[siteID]
	if state == nil {
		return 0
	}
	return state.active
}

// Release frees the slot. Idempotent and safe on no-op / nil slots.
func (s *SiteSlot) Release() {
	if s == nil || !s.held || s.limiter == nil {
		if s != nil {
			s.held = false
		}
		return
	}

	s.limiter.mu.Lock()
	state := s.limiter.sites[s.siteID]
	if state != nil {
		if state.active > 0 {
			state.active--
		}
		if state.active == 0 {
			delete(s.limiter.sites, s.siteID)
		}
	}
	s.limiter.mu.Unlock()
	s.held = false
}
