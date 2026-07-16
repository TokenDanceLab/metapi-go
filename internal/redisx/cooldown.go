package redisx

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// ChannelCooldownKey returns the shared soft-cooldown key for a route channel.
// Format: metapi:cooldown:channel:{id}
func ChannelCooldownKey(channelID int64) string {
	return "metapi:cooldown:channel:" + strconv.FormatInt(channelID, 10)
}

// CooldownMarker is an optional multi-instance soft filter for channel cooldowns.
// DB-backed cooldown_until remains the source of truth; this marker accelerates
// cross-instance awareness when Redis is configured.
//
// Failure mode: fail-open — Redis errors never force a channel into cooldown.
type CooldownMarker interface {
	// Mark sets a soft cooldown for channelID lasting ttl.
	Mark(channelID int64, ttl time.Duration) error
	// Active reports whether a soft cooldown marker is present.
	// On error or when disabled, returns (false, err) / (false, nil).
	Active(channelID int64) (bool, error)
	// Clear removes a soft marker (best-effort).
	Clear(channelID int64) error
}

// NoopCooldown never marks channels and never reports active.
type NoopCooldown struct{}

func (NoopCooldown) Mark(int64, time.Duration) error { return nil }
func (NoopCooldown) Active(int64) (bool, error)     { return false, nil }
func (NoopCooldown) Clear(int64) error               { return nil }

// MemoryCooldown is a process-local soft marker (useful for tests / single-node).
type MemoryCooldown struct {
	mu   sync.Mutex
	keys map[int64]int64 // channelID -> untilMs
	now  func() time.Time
}

// NewMemoryCooldown creates an empty process-local marker store.
func NewMemoryCooldown() *MemoryCooldown {
	return &MemoryCooldown{keys: make(map[int64]int64), now: time.Now}
}

// SetNowFunc overrides the clock (tests only).
func (m *MemoryCooldown) SetNowFunc(fn func() time.Time) {
	if m == nil || fn == nil {
		return
	}
	m.now = fn
}

func (m *MemoryCooldown) Mark(channelID int64, ttl time.Duration) error {
	if channelID <= 0 || ttl <= 0 {
		return nil
	}
	until := m.now().UTC().UnixMilli() + ttl.Milliseconds()
	m.mu.Lock()
	m.keys[channelID] = until
	m.mu.Unlock()
	return nil
}

func (m *MemoryCooldown) Active(channelID int64) (bool, error) {
	if channelID <= 0 {
		return false, nil
	}
	now := m.now().UTC().UnixMilli()
	m.mu.Lock()
	defer m.mu.Unlock()
	until, ok := m.keys[channelID]
	if !ok {
		return false, nil
	}
	if until <= now {
		delete(m.keys, channelID)
		return false, nil
	}
	return true, nil
}

func (m *MemoryCooldown) Clear(channelID int64) error {
	m.mu.Lock()
	delete(m.keys, channelID)
	m.mu.Unlock()
	return nil
}

// RedisCooldown stores soft markers under metapi:cooldown:channel:{id}.
type RedisCooldown struct {
	client *Client
}

// NewRedisCooldown wraps a Client.
func NewRedisCooldown(c *Client) *RedisCooldown {
	return &RedisCooldown{client: c}
}

// NewRedisCooldownFromURL parses REDIS_URL and builds a marker store.
func NewRedisCooldownFromURL(redisURL string) (*RedisCooldown, error) {
	c, err := NewClient(redisURL)
	if err != nil {
		return nil, err
	}
	return NewRedisCooldown(c), nil
}

func (r *RedisCooldown) Mark(channelID int64, ttl time.Duration) error {
	if r == nil || r.client == nil || channelID <= 0 {
		return nil
	}
	if ttl <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.client.timeout+200*time.Millisecond)
	defer cancel()
	return r.client.SetPX(ctx, ChannelCooldownKey(channelID), "1", ttl)
}

func (r *RedisCooldown) Active(channelID int64) (bool, error) {
	if r == nil || r.client == nil || channelID <= 0 {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.client.timeout+200*time.Millisecond)
	defer cancel()
	ok, err := r.client.Exists(ctx, ChannelCooldownKey(channelID))
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (r *RedisCooldown) Clear(channelID int64) error {
	if r == nil || r.client == nil || channelID <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.client.timeout+200*time.Millisecond)
	defer cancel()
	return r.client.Del(ctx, ChannelCooldownKey(channelID))
}

// FakeCooldown is a CooldownMarker with injectable failures for fail-open tests.
type FakeCooldown struct {
	inner    *MemoryCooldown
	FailNext bool
	Err      error
}

// NewFakeCooldown returns a controllable CooldownMarker.
func NewFakeCooldown() *FakeCooldown {
	return &FakeCooldown{inner: NewMemoryCooldown()}
}

func (f *FakeCooldown) Mark(channelID int64, ttl time.Duration) error {
	if f.FailNext {
		f.FailNext = false
		if f.Err != nil {
			return f.Err
		}
		return fmt.Errorf("fake cooldown mark failed")
	}
	return f.inner.Mark(channelID, ttl)
}

func (f *FakeCooldown) Active(channelID int64) (bool, error) {
	if f.FailNext {
		f.FailNext = false
		if f.Err != nil {
			return false, f.Err
		}
		return false, fmt.Errorf("fake cooldown active failed")
	}
	return f.inner.Active(channelID)
}

func (f *FakeCooldown) Clear(channelID int64) error {
	if f.FailNext {
		f.FailNext = false
		if f.Err != nil {
			return f.Err
		}
		return fmt.Errorf("fake cooldown clear failed")
	}
	return f.inner.Clear(channelID)
}

// TTLFromUntilISO computes remaining TTL from an RFC3339 cooldownUntil vs now.
// Returns 0 when the timestamp is missing/invalid/already past.
func TTLFromUntilISO(cooldownUntil *string, now time.Time) time.Duration {
	if cooldownUntil == nil || *cooldownUntil == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, *cooldownUntil)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", *cooldownUntil)
		if err != nil {
			return 0
		}
	}
	remain := t.Sub(now.UTC())
	if remain <= 0 {
		return 0
	}
	return remain
}
