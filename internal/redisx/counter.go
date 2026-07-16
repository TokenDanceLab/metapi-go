package redisx

import (
	"context"
	"sync"
	"time"
)

// SharedCounter increments a named key inside a time window and returns the
// post-increment count. Used for multi-instance RPM/TPM admission (#118).
//
// Memory implementation uses a sliding window of event timestamps.
// Redis implementation uses a fixed-window approximation (INCR + PEXPIRE on first hit).
type SharedCounter interface {
	// IncrWindow increments key by 1 within window and returns the new count.
	IncrWindow(key string, window time.Duration) (count int64, err error)
	// IncrWindowBy increments key by delta within window and returns the new count.
	// delta <= 0 is a no-op that returns the current count (best-effort).
	IncrWindowBy(key string, delta int64, window time.Duration) (count int64, err error)
	// Get returns the current count without incrementing.
	Get(key string) (count int64, err error)
}

// MemoryCounter is the default single-process implementation.
type MemoryCounter struct {
	mu   sync.Mutex
	keys map[string]*memWindow
	now  func() time.Time
}

type memWindow struct {
	// events are unix-ms timestamps; for token deltas we store repeated stamps
	// only for unit increments. For multi-token we store weighted events.
	events []memEvent
}

type memEvent struct {
	atMs  int64
	delta int64
}

// NewMemoryCounter creates an empty in-process counter.
func NewMemoryCounter() *MemoryCounter {
	return &MemoryCounter{
		keys: make(map[string]*memWindow),
		now:  time.Now,
	}
}

func (m *MemoryCounter) IncrWindow(key string, window time.Duration) (int64, error) {
	return m.IncrWindowBy(key, 1, window)
}

func (m *MemoryCounter) IncrWindowBy(key string, delta int64, window time.Duration) (int64, error) {
	if window <= 0 {
		window = time.Minute
	}
	nowMs := m.now().UTC().UnixMilli()
	start := nowMs - window.Milliseconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.keys[key]
	if w == nil {
		w = &memWindow{}
		m.keys[key] = w
	}
	// prune
	i := 0
	for i < len(w.events) && w.events[i].atMs < start {
		i++
	}
	if i > 0 {
		w.events = append([]memEvent(nil), w.events[i:]...)
	}
	if delta > 0 {
		w.events = append(w.events, memEvent{atMs: nowMs, delta: delta})
	}
	return sumMemEvents(w.events), nil
}

func (m *MemoryCounter) Get(key string) (int64, error) {
	nowMs := m.now().UTC().UnixMilli()
	start := nowMs - 60_000
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.keys[key]
	if w == nil {
		return 0, nil
	}
	var n int64
	for _, e := range w.events {
		if e.atMs >= start {
			n += e.delta
		}
	}
	return n, nil
}

func sumMemEvents(in []memEvent) int64 {
	var s int64
	for _, e := range in {
		s += e.delta
	}
	return s
}

// RedisCounter implements SharedCounter via fixed-window INCR/INCRBY + PEXPIRE.
// Callers must treat errors as fail-open opportunities.
type RedisCounter struct {
	client *Client
	// keyPrefix is prepended to all keys (default "metapi:").
	keyPrefix string
}

// NewRedisCounter wraps a Client. redisURL is parsed via NewClient.
func NewRedisCounter(redisURL string) (*RedisCounter, error) {
	c, err := NewClient(redisURL)
	if err != nil {
		return nil, err
	}
	return &RedisCounter{client: c, keyPrefix: "metapi:"}, nil
}

// NewRedisCounterFromClient wraps an existing Client (for tests/fakes).
func NewRedisCounterFromClient(c *Client) *RedisCounter {
	return &RedisCounter{client: c, keyPrefix: "metapi:"}
}

func (r *RedisCounter) fullKey(key string) string {
	if r.keyPrefix == "" {
		return key
	}
	return r.keyPrefix + key
}

func (r *RedisCounter) IncrWindow(key string, window time.Duration) (int64, error) {
	return r.IncrWindowBy(key, 1, window)
}

func (r *RedisCounter) IncrWindowBy(key string, delta int64, window time.Duration) (int64, error) {
	if r == nil || r.client == nil {
		return 0, errNilClient
	}
	if window <= 0 {
		window = time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.client.timeout+200*time.Millisecond)
	defer cancel()
	fk := r.fullKey(key)
	var (
		count int64
		err   error
	)
	if delta <= 0 {
		return r.Get(key)
	}
	if delta == 1 {
		count, err = r.client.Incr(ctx, fk)
	} else {
		count, err = r.client.IncrBy(ctx, fk, delta)
	}
	if err != nil {
		return 0, err
	}
	// Fixed window: set TTL only when the counter is first created in this window.
	// For INCRBY of large deltas the first write also lands at count==delta.
	if count == delta {
		if err := r.client.PExpire(ctx, fk, window); err != nil {
			return count, err
		}
	}
	return count, nil
}

func (r *RedisCounter) Get(key string) (int64, error) {
	if r == nil || r.client == nil {
		return 0, errNilClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.client.timeout+200*time.Millisecond)
	defer cancel()
	return r.client.GetInt(ctx, r.fullKey(key))
}

// FakeCounter is an in-memory SharedCounter with injectable error for fail-open tests.
type FakeCounter struct {
	inner    *MemoryCounter
	FailNext bool
	Err      error
}

// NewFakeCounter returns a controllable SharedCounter for tests.
func NewFakeCounter() *FakeCounter {
	return &FakeCounter{inner: NewMemoryCounter()}
}

func (f *FakeCounter) IncrWindow(key string, window time.Duration) (int64, error) {
	return f.IncrWindowBy(key, 1, window)
}

func (f *FakeCounter) IncrWindowBy(key string, delta int64, window time.Duration) (int64, error) {
	if f.FailNext {
		f.FailNext = false
		if f.Err != nil {
			return 0, f.Err
		}
		return 0, context.DeadlineExceeded
	}
	return f.inner.IncrWindowBy(key, delta, window)
}

func (f *FakeCounter) Get(key string) (int64, error) {
	if f.FailNext {
		f.FailNext = false
		if f.Err != nil {
			return 0, f.Err
		}
		return 0, context.DeadlineExceeded
	}
	return f.inner.Get(key)
}
