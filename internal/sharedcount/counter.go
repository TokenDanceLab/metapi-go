package sharedcount

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WindowCounter increments a named key inside a sliding/fixed window and returns
// the post-increment count. Used for multi-instance RPM/TPM admission (#118, #245).
type WindowCounter interface {
	// Incr increments key by 1 within window and returns the new count.
	// Implementations may approximate sliding windows with fixed TTL buckets.
	Incr(ctx context.Context, key string, window time.Duration) (count int64, err error)
	// Decr decrements key by 1 (compensating rollback for a prior Incr, #513).
	// Does not extend/refresh TTL. Count is floored at 0 for memory; Redis DECR may go negative.
	Decr(ctx context.Context, key string, window time.Duration) (count int64, err error)
	// IncrBy increments key by delta within window and returns the new total.
	// Used for TPM token reservations (#245). delta==0 returns the current total.
	// Negative delta is a compensating rollback (#513) and does not refresh TTL.
	IncrBy(ctx context.Context, key string, delta int64, window time.Duration) (count int64, err error)
	// Get returns the current count without incrementing.
	Get(ctx context.Context, key string) (count int64, err error)
}

// MemoryCounter is the default single-process implementation.
type MemoryCounter struct {
	mu   sync.Mutex
	keys map[string]*memWindow
	now  func() time.Time
}

type memWindow struct {
	times  []int64    // unix ms — unit Incr events (RPM)
	points []memPoint // weighted IncrBy events (TPM)
}

type memPoint struct {
	atMs  int64
	delta int64
}

func NewMemoryCounter() *MemoryCounter {
	return &MemoryCounter{keys: make(map[string]*memWindow), now: time.Now}
}

func (m *MemoryCounter) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	_ = ctx
	if window <= 0 {
		window = time.Minute
	}
	nowMs := m.now().UnixMilli()
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
	for i < len(w.times) && w.times[i] < start {
		i++
	}
	if i > 0 {
		w.times = append([]int64(nil), w.times[i:]...)
	}
	w.times = append(w.times, nowMs)
	return int64(len(w.times)), nil
}

func (m *MemoryCounter) Decr(ctx context.Context, key string, window time.Duration) (int64, error) {
	_ = ctx
	if window <= 0 {
		window = time.Minute
	}
	nowMs := m.now().UnixMilli()
	start := nowMs - window.Milliseconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.keys[key]
	if w == nil {
		return 0, nil
	}
	// prune
	i := 0
	for i < len(w.times) && w.times[i] < start {
		i++
	}
	if i > 0 {
		w.times = append([]int64(nil), w.times[i:]...)
	}
	// Compensating rollback: drop the most recent unit event when present.
	if len(w.times) > 0 {
		w.times = w.times[:len(w.times)-1]
	}
	return int64(len(w.times)), nil
}

func (m *MemoryCounter) IncrBy(ctx context.Context, key string, delta int64, window time.Duration) (int64, error) {
	_ = ctx
	if window <= 0 {
		window = time.Minute
	}
	nowMs := m.now().UnixMilli()
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
	for i < len(w.points) && w.points[i].atMs < start {
		i++
	}
	if i > 0 {
		w.points = append([]memPoint(nil), w.points[i:]...)
	}
	// delta==0 is a read-only peek; positive reserves; negative rolls back (#513).
	if delta != 0 {
		w.points = append(w.points, memPoint{atMs: nowMs, delta: delta})
	}
	var sum int64
	for _, p := range w.points {
		sum += p.delta
	}
	if sum < 0 {
		// Keep memory totals non-negative for admission math.
		sum = 0
		w.points = nil
	}
	return sum, nil
}

func (m *MemoryCounter) Get(ctx context.Context, key string) (int64, error) {
	_ = ctx
	nowMs := m.now().UnixMilli()
	start := nowMs - 60_000
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.keys[key]
	if w == nil {
		return 0, nil
	}
	n := 0
	for _, t := range w.times {
		if t >= start {
			n++
		}
	}
	return int64(n), nil
}

// RedisCounter is a minimal RESP client (INCR + PEXPIRE / GET) over TCP.
// No third-party dependency. Failures return errors for callers to fail-open.
type RedisCounter struct {
	addr     string // host:port
	password string
	db       int
	timeout  time.Duration
	// dial is injectable for tests.
	dial func(network, address string, timeout time.Duration) (net.Conn, error)
}

// ParseRedisURL parses redis://[:password@]host:port[/db] into RedisCounter fields.
// Empty password/db are ok. redis://localhost:6379/0 is the common form.
func ParseRedisURL(raw string) (addr, password string, db int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", 0, fmt.Errorf("empty redis url")
	}
	// Accept host:port without scheme.
	if !strings.Contains(raw, "://") {
		if !strings.Contains(raw, ":") {
			raw = raw + ":6379"
		}
		return raw, "", 0, nil
	}
	// Very small parser for redis://[:pass@]host:port[/db]
	u := raw
	u = strings.TrimPrefix(u, "redis://")
	u = strings.TrimPrefix(u, "rediss://")
	// password
	if at := strings.LastIndex(u, "@"); at >= 0 {
		userinfo := u[:at]
		u = u[at+1:]
		if strings.HasPrefix(userinfo, ":") {
			password = userinfo[1:]
		} else if i := strings.Index(userinfo, ":"); i >= 0 {
			password = userinfo[i+1:]
		} else {
			password = userinfo
		}
	}
	// db
	if slash := strings.Index(u, "/"); slash >= 0 {
		dbPart := u[slash+1:]
		u = u[:slash]
		if dbPart != "" {
			n, e := strconv.Atoi(strings.Split(dbPart, "?")[0])
			if e != nil {
				return "", "", 0, fmt.Errorf("invalid redis db: %w", e)
			}
			db = n
		}
	}
	if !strings.Contains(u, ":") {
		u = u + ":6379"
	}
	return u, password, db, nil
}

func NewRedisCounter(redisURL string) (*RedisCounter, error) {
	addr, pass, db, err := ParseRedisURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &RedisCounter{
		addr:     addr,
		password: pass,
		db:       db,
		timeout:  800 * time.Millisecond,
		dial:     net.DialTimeout,
	}, nil
}

func (r *RedisCounter) withConn(ctx context.Context, fn func(net.Conn) error) error {
	_ = ctx
	if r.dial == nil {
		r.dial = net.DialTimeout
	}
	conn, err := r.dial("tcp", r.addr, r.timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(r.timeout))
	if r.password != "" {
		if err := redisDo(conn, "AUTH", r.password); err != nil {
			return err
		}
	}
	if r.db != 0 {
		if err := redisDo(conn, "SELECT", strconv.Itoa(r.db)); err != nil {
			return err
		}
	}
	return fn(conn)
}

func (r *RedisCounter) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	if window <= 0 {
		window = time.Minute
	}
	var count int64
	err := r.withConn(ctx, func(conn net.Conn) error {
		// Fixed-window approximation: INCR + PEXPIRE only when count==1.
		n, err := redisDoInt(conn, "INCR", key)
		if err != nil {
			return err
		}
		count = n
		if n == 1 {
			ms := strconv.FormatInt(window.Milliseconds(), 10)
			if err := redisDo(conn, "PEXPIRE", key, ms); err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
}

func (r *RedisCounter) Decr(ctx context.Context, key string, window time.Duration) (int64, error) {
	_ = window // compensating rollback must not refresh TTL
	var count int64
	err := r.withConn(ctx, func(conn net.Conn) error {
		n, err := redisDoInt(conn, "DECR", key)
		if err != nil {
			return err
		}
		count = n
		return nil
	})
	return count, err
}

func (r *RedisCounter) IncrBy(ctx context.Context, key string, delta int64, window time.Duration) (int64, error) {
	if window <= 0 {
		window = time.Minute
	}
	if delta == 0 {
		// No reservation — read current total without changing TTL.
		return r.Get(ctx, key)
	}
	var count int64
	err := r.withConn(ctx, func(conn net.Conn) error {
		// Fixed-window approximation: INCRBY + PEXPIRE when this is the first positive write
		// in the window (post-increment equals delta ⇒ key was absent/0).
		// Negative delta is a compensating rollback (#513) and must not refresh TTL.
		n, err := redisDoInt(conn, "INCRBY", key, strconv.FormatInt(delta, 10))
		if err != nil {
			return err
		}
		count = n
		if delta > 0 && n == delta {
			ms := strconv.FormatInt(window.Milliseconds(), 10)
			if err := redisDo(conn, "PEXPIRE", key, ms); err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
}

func (r *RedisCounter) Get(ctx context.Context, key string) (int64, error) {
	var count int64
	err := r.withConn(ctx, func(conn net.Conn) error {
		// GET may return null bulk → 0
		n, err := redisDoIntNullable(conn, "GET", key)
		if err != nil {
			return err
		}
		count = n
		return nil
	})
	return count, err
}

// ---- minimal RESP ----

func redisDo(conn net.Conn, parts ...string) error {
	_, err := redisDoRaw(conn, parts...)
	return err
}

func redisDoInt(conn net.Conn, parts ...string) (int64, error) {
	raw, err := redisDoRaw(conn, parts...)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func redisDoIntNullable(conn net.Conn, parts ...string) (int64, error) {
	raw, err := redisDoRaw(conn, parts...)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func redisDoRaw(conn net.Conn, parts ...string) (string, error) {
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(strconv.Itoa(len(parts)))
	b.WriteString("\r\n")
	for _, p := range parts {
		b.WriteString("$")
		b.WriteString(strconv.Itoa(len(p)))
		b.WriteString("\r\n")
		b.WriteString(p)
		b.WriteString("\r\n")
	}
	if _, err := conn.Write([]byte(b.String())); err != nil {
		return "", err
	}
	return redisRead(conn)
}

func redisRead(conn net.Conn) (string, error) {
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil && len(buf) == 0 {
			return "", err
		}
		if len(buf) == 0 {
			continue
		}
		switch buf[0] {
		case '+':
			if i := indexCRLF(buf); i >= 0 {
				return string(buf[1:i]), nil
			}
		case '-':
			if i := indexCRLF(buf); i >= 0 {
				return "", fmt.Errorf("redis error: %s", string(buf[1:i]))
			}
		case ':':
			if i := indexCRLF(buf); i >= 0 {
				return string(buf[1:i]), nil
			}
		case '$':
			// bulk: $<len>\r\n<data>\r\n or hmtBc1\r\n
			if i := indexCRLF(buf); i >= 0 {
				lenStr := string(buf[1:i])
				if lenStr == "-1" {
					return "", nil
				}
				ln, err := strconv.Atoi(lenStr)
				if err != nil {
					return "", err
				}
				start := i + 2
				if len(buf) >= start+ln+2 {
					return string(buf[start : start+ln]), nil
				}
			}
		default:
			return "", fmt.Errorf("unexpected redis reply prefix %q", buf[0])
		}
		if err != nil {
			return "", err
		}
	}
}

func indexCRLF(b []byte) int {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 13 && b[i+1] == 10 {
			return i
		}
	}
	return -1
}
