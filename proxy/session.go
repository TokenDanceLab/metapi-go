package proxy

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
)

// StickyEntry is a sticky session binding entry.
type StickyEntry struct {
	ChannelID   int64
	ExpiresAtMs int64
}

// ChannelLease is a lease on a session-scoped channel.
type ChannelLease struct {
	ChannelID int64
	leaseID   int
	active    bool
	mu        sync.Mutex
	coord     *ProxyChannelCoordinator
	state     *channelRuntimeState
	expiryCh  chan struct{}
	doneCh    chan struct{}
}

// IsActive returns whether the lease is still active.
func (l *ChannelLease) IsActive() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active
}

// Release releases the lease, freeing the channel slot.
func (l *ChannelLease) Release() {
	l.mu.Lock()
	if !l.active {
		l.mu.Unlock()
		return
	}
	l.active = false
	l.mu.Unlock()

	close(l.doneCh)
	l.coord.releaseLease(l.ChannelID, l.leaseID, l.state)
}

// Touch extends the lease TTL.
func (l *ChannelLease) Touch() {
	l.mu.Lock()
	if !l.active {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	l.coord.touchLease(l)
}

// AcquireResult is the result of acquiring a channel lease.
type AcquireResult struct {
	Status string        // "acquired" or "timeout"
	Lease  *ChannelLease // non-nil only for "acquired"
	WaitMs int64         // wait duration before timeout (0 for instant)
}

// ChannelLoadSnapshot is a snapshot of channel load.
type ChannelLoadSnapshot struct {
	ChannelID        int64
	SessionScoped    bool
	ConcurrencyLimit int
	ActiveLeaseCount int
	WaitingCount     int
	LoadRatio        float64
	Saturated        bool
}

type channelWaiter struct {
	cancelled bool
	resultCh  chan AcquireResult
	timer     *time.Timer
}

type channelRuntimeState struct {
	activeLeaseIDs   map[int]bool
	queue            []*channelWaiter
	concurrencyLimit int
	mu               sync.Mutex
}

// ProxyChannelCoordinator manages sticky session bindings and channel concurrency leases.
// Both are conditional: only activate when the downstream request has a valid sticky
// session key AND the channel's account uses session-scoped credentials.
type ProxyChannelCoordinator struct {
	stickyBindings map[string]StickyEntry
	channelStates  map[int64]*channelRuntimeState
	// nextLeaseID is allocated with atomic ops so createTrackedLease never needs
	// c.mu while state.mu is already held (lock order: c.mu then state.mu).
	nextLeaseID atomic.Int64
	mu          sync.Mutex
	cfg         *config.Config
}

// NewProxyChannelCoordinator creates a new coordinator.
func NewProxyChannelCoordinator(cfg *config.Config) *ProxyChannelCoordinator {
	c := &ProxyChannelCoordinator{
		stickyBindings: make(map[string]StickyEntry),
		channelStates:  make(map[int64]*channelRuntimeState),
		cfg:            cfg,
	}
	c.nextLeaseID.Store(1)
	return c
}

// ---- Sticky Session Key ----

// BuildStickySessionKey constructs a sticky session key from client context.
// Returns empty string if sticky sessions are disabled or sessionId/model are missing.
func (c *ProxyChannelCoordinator) BuildStickySessionKey(
	clientKind string,
	sessionID string,
	requestedModel string,
	downstreamPath string,
	downstreamAPIKeyID *int64,
) string {
	if !c.cfg.ProxyStickySessionEnabled {
		return ""
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ""
	}
	model := strings.ToLower(strings.TrimSpace(requestedModel))
	if model == "" {
		return ""
	}
	path := strings.ToLower(strings.TrimSpace(downstreamPath))
	if path == "" {
		path = "unknown"
	}
	kind := strings.ToLower(strings.TrimSpace(clientKind))
	if kind == "" {
		kind = "generic"
	}
	owner := "key:anonymous"
	if downstreamAPIKeyID != nil {
		owner = fmt.Sprintf("key:%d", *downstreamAPIKeyID)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", owner, kind, path, model, sid)
}

// ---- Sticky Bindings ----

func (c *ProxyChannelCoordinator) stickySessionTTLMs() int64 {
	return maxInt64(30000, int64(c.cfg.ProxyStickySessionTtlMs))
}

// GetStickyChannelID returns the sticky channel ID for a session key, or 0 if none/expired.
func (c *ProxyChannelCoordinator) GetStickyChannelID(key string) int64 {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanupExpiredLocked(time.Now().UnixMilli())
	entry, ok := c.stickyBindings[key]
	if !ok || entry.ExpiresAtMs <= time.Now().UnixMilli() {
		delete(c.stickyBindings, key)
		return 0
	}
	return entry.ChannelID
}

// BindStickyChannel binds a session key to a channel.
func (c *ProxyChannelCoordinator) BindStickyChannel(key string, channelID int64, extraConfig *string, oauthProvider *string) {
	if !c.cfg.ProxyStickySessionEnabled {
		return
	}
	if !isSessionScopedChannel(extraConfig, oauthProvider) {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" || channelID <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanupExpiredLocked(time.Now().UnixMilli())
	c.stickyBindings[key] = StickyEntry{
		ChannelID:   channelID,
		ExpiresAtMs: time.Now().UnixMilli() + c.stickySessionTTLMs(),
	}
}

// ClearStickyChannel clears a sticky binding. If channelID is provided (>0), only clears if it matches.
func (c *ProxyChannelCoordinator) ClearStickyChannel(key string, channelID int64) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.stickyBindings[key]
	if !ok {
		return
	}
	if channelID > 0 && existing.ChannelID != channelID {
		return
	}
	delete(c.stickyBindings, key)
}

func (c *ProxyChannelCoordinator) cleanupExpiredLocked(nowMs int64) {
	for key, entry := range c.stickyBindings {
		if entry.ExpiresAtMs <= nowMs {
			delete(c.stickyBindings, key)
		}
	}
}

// ---- Lease Acquisition ----

func (c *ProxyChannelCoordinator) channelLeaseTTLMs() int64 {
	return maxInt64(5000, int64(c.cfg.ProxySessionChannelLeaseTtlMs))
}

func (c *ProxyChannelCoordinator) channelLeaseKeepaliveMs() int64 {
	return maxInt64(1000, int64(c.cfg.ProxySessionChannelLeaseKeepaliveMs))
}

func (c *ProxyChannelCoordinator) channelQueueWaitMs() int64 {
	return maxInt64(0, int64(c.cfg.ProxySessionChannelQueueWaitMs))
}

func (c *ProxyChannelCoordinator) channelConcurrencyLimit(extraConfig *string, oauthProvider *string) int {
	if !isSessionScopedChannel(extraConfig, oauthProvider) {
		return 0
	}
	limit := int(c.cfg.ProxySessionChannelConcurrencyLimit)
	if limit < 0 {
		return 0
	}
	return limit
}

func (c *ProxyChannelCoordinator) getOrCreateChannelState(channelID int64) *channelRuntimeState {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.channelStates[channelID]
	if !ok {
		state = &channelRuntimeState{
			activeLeaseIDs: make(map[int]bool),
		}
		c.channelStates[channelID] = state
	}
	return state
}

func (c *ProxyChannelCoordinator) nextLeaseIDValue() int {
	// Atomic allocation: safe under state.mu without taking c.mu.
	return int(c.nextLeaseID.Add(1) - 1)
}

// AcquireChannelLease acquires a lease for a channel. Returns noop if channelID <= 0
// or concurrency limit is 0.
func (c *ProxyChannelCoordinator) AcquireChannelLease(
	channelID int64,
	extraConfig *string,
	oauthProvider *string,
) AcquireResult {
	if channelID <= 0 {
		return AcquireResult{
			Status: "acquired",
			Lease:  c.createNoopLease(0),
		}
	}

	concurrencyLimit := c.channelConcurrencyLimit(extraConfig, oauthProvider)
	if concurrencyLimit <= 0 {
		return AcquireResult{
			Status: "acquired",
			Lease:  c.createNoopLease(channelID),
		}
	}

	state := c.getOrCreateChannelState(channelID)

	state.mu.Lock()
	// concurrencyLimit is shared state; always update under state.mu.
	state.concurrencyLimit = concurrencyLimit
	c.pruneCancelledWaitersLocked(state)

	if len(state.activeLeaseIDs) < concurrencyLimit {
		lease := c.createTrackedLease(channelID, state)
		state.mu.Unlock()
		return AcquireResult{
			Status: "acquired",
			Lease:  lease,
		}
	}

	waitMs := c.channelQueueWaitMs()
	if waitMs <= 0 {
		state.mu.Unlock()
		return AcquireResult{
			Status: "timeout",
			WaitMs: 0,
		}
	}

	resultCh := make(chan AcquireResult, 1)
	var waiter *channelWaiter
	waiter = &channelWaiter{
		resultCh: resultCh,
		timer: time.AfterFunc(time.Duration(waitMs)*time.Millisecond, func() {
			state.mu.Lock()
			waiter.cancelled = true
			state.mu.Unlock()
			c.pruneAndMaybeDelete(state, channelID)
			select {
			case resultCh <- AcquireResult{Status: "timeout", WaitMs: waitMs}:
			default:
			}
		}),
	}
	state.queue = append(state.queue, waiter)
	state.mu.Unlock()

	result := <-resultCh
	return result
}

func (c *ProxyChannelCoordinator) createNoopLease(channelID int64) *ChannelLease {
	return &ChannelLease{
		ChannelID: channelID,
		active:    false,
	}
}

func (c *ProxyChannelCoordinator) createTrackedLease(channelID int64, state *channelRuntimeState) *ChannelLease {
	// Caller (AcquireChannelLease / drainQueueLocked) already holds state.mu.
	// Lease IDs are atomic so we never take c.mu while holding state.mu.
	leaseID := c.nextLeaseIDValue()
	state.activeLeaseIDs[leaseID] = true

	lease := &ChannelLease{
		ChannelID: channelID,
		leaseID:   leaseID,
		active:    true,
		coord:     c,
		state:     state,
		expiryCh:  make(chan struct{}, 1),
		doneCh:    make(chan struct{}), // close-only signal; unbuffered is intentional
	}

	// Start expiry timer (single goroutine; resets via expiryCh on Touch)
	go func() {
		timer := time.NewTimer(time.Duration(c.channelLeaseTTLMs()) * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				lease.Release()
				return
			case <-lease.expiryCh:
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(time.Duration(c.channelLeaseTTLMs()) * time.Millisecond)
			case <-lease.doneCh:
				return
			}
		}
	}()

	// Start keepalive timer
	keepaliveMs := c.channelLeaseKeepaliveMs()
	if keepaliveMs > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(keepaliveMs) * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					lease.Touch()
				case <-lease.doneCh:
					return
				}
			}
		}()
	}

	return lease
}

func (c *ProxyChannelCoordinator) touchLease(lease *ChannelLease) {
	select {
	case lease.expiryCh <- struct{}{}:
	default:
	}
}

func (c *ProxyChannelCoordinator) releaseLease(channelID int64, leaseID int, state *channelRuntimeState) {
	state.mu.Lock()
	delete(state.activeLeaseIDs, leaseID)
	c.pruneCancelledWaitersLocked(state)
	c.drainQueueLocked(channelID, state)
	state.mu.Unlock()

	c.maybeDeleteChannelState(channelID)
}

func (c *ProxyChannelCoordinator) drainQueueLocked(channelID int64, state *channelRuntimeState) {
	limit := state.concurrencyLimit
	if limit <= 0 {
		return
	}

	for len(state.activeLeaseIDs) < limit && len(state.queue) > 0 {
		waiter := state.queue[0]
		state.queue = state.queue[1:]
		if waiter == nil || waiter.cancelled {
			continue
		}
		if waiter.timer != nil {
			waiter.timer.Stop()
			waiter.timer = nil
		}
		lease := c.createTrackedLease(channelID, state)
		select {
		case waiter.resultCh <- AcquireResult{Status: "acquired", Lease: lease}:
		default:
		}
	}
}

func (c *ProxyChannelCoordinator) pruneCancelledWaitersLocked(state *channelRuntimeState) {
	if len(state.queue) == 0 {
		return
	}
	filtered := make([]*channelWaiter, 0, len(state.queue))
	for _, w := range state.queue {
		if !w.cancelled {
			filtered = append(filtered, w)
		}
	}
	state.queue = filtered
}

func (c *ProxyChannelCoordinator) pruneAndMaybeDelete(state *channelRuntimeState, channelID int64) {
	state.mu.Lock()
	c.pruneCancelledWaitersLocked(state)
	shouldDelete := len(state.activeLeaseIDs) == 0
	for _, w := range state.queue {
		if !w.cancelled {
			shouldDelete = false
			break
		}
	}
	state.mu.Unlock()

	if shouldDelete {
		c.mu.Lock()
		// Re-check under global lock
		state.mu.Lock()
		c.pruneCancelledWaitersLocked(state)
		empty := len(state.activeLeaseIDs) == 0
		allCancelled := true
		for _, w := range state.queue {
			if !w.cancelled {
				allCancelled = false
				break
			}
		}
		state.mu.Unlock()
		if empty && allCancelled {
			delete(c.channelStates, channelID)
		}
		c.mu.Unlock()
	}
}

func (c *ProxyChannelCoordinator) maybeDeleteChannelState(channelID int64) {
	c.pruneAndMaybeDelete(c.getOrCreateChannelState(channelID), channelID)
}

// GetActiveChannelIDs returns channel IDs with active leases.
func (c *ProxyChannelCoordinator) GetActiveChannelIDs() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	var ids []int64
	for channelID, state := range c.channelStates {
		state.mu.Lock()
		c.pruneCancelledWaitersLocked(state)
		if len(state.activeLeaseIDs) > 0 {
			ids = append(ids, channelID)
		}
		state.mu.Unlock()
	}
	return ids
}

// GetChannelLoadSnapshot returns a snapshot of a channel's load.
func (c *ProxyChannelCoordinator) GetChannelLoadSnapshot(
	channelID int64,
	extraConfig *string,
	oauthProvider *string,
) ChannelLoadSnapshot {
	scoped := isSessionScopedChannel(extraConfig, oauthProvider)
	limit := c.channelConcurrencyLimit(extraConfig, oauthProvider)

	c.mu.Lock()
	state, ok := c.channelStates[channelID]
	c.mu.Unlock()

	activeCount := 0
	waitingCount := 0
	if ok && channelID > 0 {
		state.mu.Lock()
		c.pruneCancelledWaitersLocked(state)
		activeCount = len(state.activeLeaseIDs)
		waitingCount = len(state.queue)
		state.mu.Unlock()
	}

	denom := 1
	if limit > 0 {
		denom = limit
	}
	loadRatio := float64(activeCount+waitingCount) / float64(denom)
	saturated := limit > 0 && activeCount >= limit

	return ChannelLoadSnapshot{
		ChannelID:        channelID,
		SessionScoped:    scoped,
		ConcurrencyLimit: limit,
		ActiveLeaseCount: activeCount,
		WaitingCount:     waitingCount,
		LoadRatio:        loadRatio,
		Saturated:        saturated,
	}
}

// ---- Helpers ----

// IsSessionScopedChannel checks if a channel uses session-scoped credentials.
func IsSessionScopedChannel(extraConfig *string, oauthProvider *string) bool {
	return isSessionScopedChannel(extraConfig, oauthProvider)
}

func isSessionScopedChannel(extraConfig *string, oauthProvider *string) bool {
	if service.GetCredentialModeFromExtraConfig(extraConfig) == service.CredentialModeSession {
		return true
	}
	if oauthProvider != nil && *oauthProvider != "" {
		return true
	}
	return false
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ResetProxyChannelCoordinator resets all state (for testing).
func (c *ProxyChannelCoordinator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stickyBindings = make(map[string]StickyEntry)
	c.channelStates = make(map[int64]*channelRuntimeState)
	c.nextLeaseID.Store(1)
}
