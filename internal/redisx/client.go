// Package redisx provides a minimal optional Redis RESP client for shared
// multi-instance counters and soft cooldown markers (learn #118).
//
// It intentionally uses only the Go standard library (net) so single-node
// installs have no Redis dependency. When REDIS_URL is empty, callers keep
// process-local state.
package redisx

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a tiny connection-per-command Redis RESP client.
// It is safe for concurrent use; each command opens its own short-lived TCP conn.
type Client struct {
	addr     string
	password string
	db       int
	timeout  time.Duration
	// dial is injectable for tests.
	dial func(network, address string, timeout time.Duration) (net.Conn, error)
}

// ParseURL parses redis://[:password@]host:port[/db] or host:port.
// rediss:// is accepted but TLS is NOT implemented — returns an error.
func ParseURL(raw string) (addr, password string, db int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", 0, fmt.Errorf("empty redis url")
	}
	if strings.HasPrefix(strings.ToLower(raw), "rediss://") {
		return "", "", 0, fmt.Errorf("rediss:// (TLS) is not supported by the minimal redisx client")
	}
	if !strings.Contains(raw, "://") {
		if !strings.Contains(raw, ":") {
			raw = raw + ":6379"
		}
		return raw, "", 0, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid redis url: %w", err)
	}
	if u.Scheme != "" && u.Scheme != "redis" {
		return "", "", 0, fmt.Errorf("unsupported redis scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return "", "", 0, fmt.Errorf("redis url missing host")
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	addr = net.JoinHostPort(host, port)
	if u.User != nil {
		if p, ok := u.User.Password(); ok {
			password = p
		} else if name := u.User.Username(); name != "" {
			// redis://:pass@host form stores pass as username in some parsers;
			// also accept username-only as password for redis://pass@host.
			password = name
		}
	}
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		// strip query leftovers if any
		path = strings.Split(path, "?")[0]
		n, e := strconv.Atoi(path)
		if e != nil {
			return "", "", 0, fmt.Errorf("invalid redis db: %w", e)
		}
		db = n
	}
	return addr, password, db, nil
}

// NewClient builds a Client from a REDIS_URL-style string.
func NewClient(redisURL string) (*Client, error) {
	addr, pass, db, err := ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		addr:     addr,
		password: pass,
		db:       db,
		timeout:  800 * time.Millisecond,
		dial:     net.DialTimeout,
	}, nil
}

// Ping issues PING; useful at startup (non-fatal).
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Do(ctx, "PING")
	return err
}

// Do runs a Redis command and returns a simplified string payload.
// Integer replies are decimal strings; bulk null is "".
func (c *Client) Do(ctx context.Context, parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("empty redis command")
	}
	return c.withConn(ctx, func(conn net.Conn) (string, error) {
		return redisDoRaw(conn, parts...)
	})
}

// Incr increments key by 1.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	raw, err := c.Do(ctx, "INCR", key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

// IncrBy increments key by delta (may be negative).
func (c *Client) IncrBy(ctx context.Context, key string, delta int64) (int64, error) {
	raw, err := c.Do(ctx, "INCRBY", key, strconv.FormatInt(delta, 10))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

// GetInt returns 0 when the key is missing.
func (c *Client) GetInt(ctx context.Context, key string) (int64, error) {
	raw, err := c.Do(ctx, "GET", key)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

// PExpire sets a millisecond TTL.
func (c *Client) PExpire(ctx context.Context, key string, window time.Duration) error {
	if window <= 0 {
		window = time.Minute
	}
	ms := window.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	_, err := c.Do(ctx, "PEXPIRE", key, strconv.FormatInt(ms, 10))
	return err
}

// SetPX sets key to value with millisecond TTL (overwrites).
func (c *Client) SetPX(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Second
	}
	ms := ttl.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	_, err := c.Do(ctx, "SET", key, value, "PX", strconv.FormatInt(ms, 10))
	return err
}

// Exists returns whether key is present.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	raw, err := c.Do(ctx, "EXISTS", key)
	if err != nil {
		return false, err
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Del deletes a key (best-effort).
func (c *Client) Del(ctx context.Context, key string) error {
	_, err := c.Do(ctx, "DEL", key)
	return err
}

func (c *Client) withConn(ctx context.Context, fn func(net.Conn) (string, error)) (string, error) {
	if c == nil {
		return "", fmt.Errorf("nil redis client")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dial := c.dial
	if dial == nil {
		dial = net.DialTimeout
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}
	// Honor context deadline if tighter.
	if dl, ok := ctx.Deadline(); ok {
		if remain := time.Until(dl); remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	conn, err := dial("tcp", c.addr, timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if c.password != "" {
		if _, err := redisDoRaw(conn, "AUTH", c.password); err != nil {
			return "", err
		}
	}
	if c.db != 0 {
		if _, err := redisDoRaw(conn, "SELECT", strconv.Itoa(c.db)); err != nil {
			return "", err
		}
	}
	return fn(conn)
}

// ---- minimal RESP ----

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
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}
