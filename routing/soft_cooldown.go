package routing

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tokendancelab/metapi-go/internal/redisx"
)

// Soft channel cooldown markers (learn #118).
//
// DB-backed route_channels.cooldown_until remains the source of truth for
// durable cooldown. When REDIS_URL is configured, instances also publish a
// short-lived Redis key metapi:cooldown:channel:{id} so peer replicas can
// soft-filter a channel before their local cache reloads from DB.
//
// Failure mode: fail-open — Redis errors never force a channel into cooldown
// and never block selection.

var (
	softCooldownMu     sync.RWMutex
	softCooldownMarker  redisx.CooldownMarker = redisx.NoopCooldown{}
	softCooldownFailOpen atomic.Uint64
)

// ConfigureSoftCooldown installs an optional multi-instance cooldown marker.
// Pass nil or redisx.NoopCooldown{} to disable (default).
func ConfigureSoftCooldown(marker redisx.CooldownMarker) {
	softCooldownMu.Lock()
	defer softCooldownMu.Unlock()
	if marker == nil {
		softCooldownMarker = redisx.NoopCooldown{}
		return
	}
	softCooldownMarker = marker
}

// SoftCooldownEnabled reports whether a non-noop marker is installed.
func SoftCooldownEnabled() bool {
	softCooldownMu.RLock()
	defer softCooldownMu.RUnlock()
	_, noop := softCooldownMarker.(redisx.NoopCooldown)
	return softCooldownMarker != nil && !noop
}

// MarkSoftChannelCooldown publishes a soft marker for channelID lasting ttl.
// Best-effort; errors are logged and counted (fail-open).
func MarkSoftChannelCooldown(channelID int64, ttl time.Duration) {
	if channelID <= 0 || ttl <= 0 {
		return
	}
	softCooldownMu.RLock()
	m := softCooldownMarker
	softCooldownMu.RUnlock()
	if m == nil {
		return
	}
	if _, ok := m.(redisx.NoopCooldown); ok {
		return
	}
	if err := m.Mark(channelID, ttl); err != nil {
		softCooldownFailOpen.Add(1)
		slog.Debug("soft channel cooldown mark failed (fail-open)",
			"channel_id", channelID,
			"error", err,
		)
	}
}

// ClearSoftChannelCooldown removes a soft marker (best-effort).
func ClearSoftChannelCooldown(channelID int64) {
	if channelID <= 0 {
		return
	}
	softCooldownMu.RLock()
	m := softCooldownMarker
	softCooldownMu.RUnlock()
	if m == nil {
		return
	}
	if err := m.Clear(channelID); err != nil {
		softCooldownFailOpen.Add(1)
		slog.Debug("soft channel cooldown clear failed (fail-open)",
			"channel_id", channelID,
			"error", err,
		)
	}
}

// IsSoftChannelCooldownActive reports whether a soft marker is present.
// On Redis/backend error returns false (fail-open).
func IsSoftChannelCooldownActive(channelID int64) bool {
	if channelID <= 0 {
		return false
	}
	softCooldownMu.RLock()
	m := softCooldownMarker
	softCooldownMu.RUnlock()
	if m == nil {
		return false
	}
	ok, err := m.Active(channelID)
	if err != nil {
		softCooldownFailOpen.Add(1)
		return false
	}
	return ok
}

// SoftCooldownFailOpenCount returns how many soft-marker backend errors occurred.
func SoftCooldownFailOpenCount() uint64 {
	return softCooldownFailOpen.Load()
}

// ResetSoftCooldownForTest restores the noop marker and counters.
func ResetSoftCooldownForTest() {
	ConfigureSoftCooldown(redisx.NoopCooldown{})
	softCooldownFailOpen.Store(0)
}

// MarkSoftChannelCooldownFromUntil publishes a soft marker using remaining TTL from
// an RFC3339 cooldownUntil timestamp. No-op when until is nil/past.
func MarkSoftChannelCooldownFromUntil(channelID int64, cooldownUntil *string) {
	ttl := redisx.TTLFromUntilISO(cooldownUntil, time.Now().UTC())
	if ttl <= 0 {
		return
	}
	MarkSoftChannelCooldown(channelID, ttl)
}

// isChannelCoolingDown combines DB cooldown_until with optional soft Redis marker.
func isChannelCoolingDown(channelID int64, cooldownUntil *string, nowISO string) bool {
	if cooldownUntil != nil && *cooldownUntil != "" && *cooldownUntil > nowISO {
		return true
	}
	return IsSoftChannelCooldownActive(channelID)
}
