package redisx

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseURL(t *testing.T) {
	addr, pass, db, err := ParseURL("redis://:s3cret@127.0.0.1:6380/2")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "127.0.0.1:6380" || pass != "s3cret" || db != 2 {
		t.Fatalf("got addr=%q pass=%q db=%d", addr, pass, db)
	}
	addr, pass, db, err = ParseURL("localhost:6379")
	if err != nil || addr != "localhost:6379" || pass != "" || db != 0 {
		t.Fatalf("hostport got addr=%q pass=%q db=%d err=%v", addr, pass, db, err)
	}
	if _, _, _, err := ParseURL("rediss://example.com:6379"); err == nil {
		t.Fatal("expected rediss rejection")
	}
}

func TestMemoryCounter_IncrWindow(t *testing.T) {
	m := NewMemoryCounter()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	m.now = func() time.Time { return now }

	n1, err := m.IncrWindow("k", time.Minute)
	if err != nil || n1 != 1 {
		t.Fatalf("n1=%d err=%v", n1, err)
	}
	n2, _ := m.IncrWindow("k", time.Minute)
	if n2 != 2 {
		t.Fatalf("n2=%d", n2)
	}
	now = base.Add(61 * time.Second)
	n3, _ := m.IncrWindow("k", time.Minute)
	if n3 != 1 {
		t.Fatalf("after window n3=%d", n3)
	}
}

func TestMemoryCounter_IncrWindowBy(t *testing.T) {
	m := NewMemoryCounter()
	n, err := m.IncrWindowBy("tok", 500, time.Minute)
	if err != nil || n != 500 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, _ = m.IncrWindowBy("tok", 400, time.Minute)
	if n != 900 {
		t.Fatalf("n=%d", n)
	}
}

func TestFakeCounter_FailOpenSignal(t *testing.T) {
	f := NewFakeCounter()
	f.FailNext = true
	if _, err := f.IncrWindow("x", time.Minute); err == nil {
		t.Fatal("expected error")
	}
	// next call succeeds
	n, err := f.IncrWindow("x", time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestMemoryCooldown(t *testing.T) {
	c := NewMemoryCooldown()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	c.SetNowFunc(func() time.Time { return now })

	if err := c.Mark(42, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	ok, err := c.Active(42)
	if err != nil || !ok {
		t.Fatalf("active=%v err=%v", ok, err)
	}
	now = base.Add(31 * time.Second)
	ok, err = c.Active(42)
	if err != nil || ok {
		t.Fatalf("expired active=%v err=%v", ok, err)
	}
}

func TestTTLFromUntilISO(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	future := now.Add(45 * time.Second).Format(time.RFC3339)
	ttl := TTLFromUntilISO(&future, now)
	if ttl < 44*time.Second || ttl > 46*time.Second {
		t.Fatalf("ttl=%v", ttl)
	}
	past := now.Add(-5 * time.Second).Format(time.RFC3339)
	if TTLFromUntilISO(&past, now) != 0 {
		t.Fatal("past should be 0")
	}
}

// fakeRedis is a minimal in-process RESP server for unit tests.
type fakeRedis struct {
	ln   net.Listener
	mu   sync.Mutex
	data map[string]string
	ttl  map[string]time.Time
}

func startFakeRedis(t *testing.T) (*fakeRedis, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fr := &fakeRedis{
		ln:   ln,
		data: make(map[string]string),
		ttl:  make(map[string]time.Time),
	}
	go fr.serve()
	return fr, ln.Addr().String()
}

func (fr *fakeRedis) Close() { _ = fr.ln.Close() }

func (fr *fakeRedis) serve() {
	for {
		conn, err := fr.ln.Accept()
		if err != nil {
			return
		}
		go fr.handle(conn)
	}
}

func (fr *fakeRedis) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		args, err := readRESPArray(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			return
		}
		cmd := strings.ToUpper(args[0])
		var reply string
		fr.mu.Lock()
		fr.expireLocked()
		switch cmd {
		case "PING":
			reply = "+PONG\r\n"
		case "AUTH", "SELECT":
			reply = "+OK\r\n"
		case "INCR":
			key := args[1]
			n := fr.getIntLocked(key) + 1
			fr.data[key] = strconv.FormatInt(n, 10)
			reply = fmt.Sprintf(":%d\r\n", n)
		case "INCRBY":
			key := args[1]
			delta, _ := strconv.ParseInt(args[2], 10, 64)
			n := fr.getIntLocked(key) + delta
			fr.data[key] = strconv.FormatInt(n, 10)
			reply = fmt.Sprintf(":%d\r\n", n)
		case "GET":
			key := args[1]
			v, ok := fr.data[key]
			if !ok {
				reply = "$-1\r\n"
			} else {
				reply = fmt.Sprintf("$%d\r\n%s\r\n", len(v), v)
			}
		case "PEXPIRE":
			key := args[1]
			ms, _ := strconv.ParseInt(args[2], 10, 64)
			fr.ttl[key] = time.Now().Add(time.Duration(ms) * time.Millisecond)
			reply = ":1\r\n"
		case "SET":
			// SET key value [PX ms]
			key, val := args[1], args[2]
			fr.data[key] = val
			if len(args) >= 5 && strings.EqualFold(args[3], "PX") {
				ms, _ := strconv.ParseInt(args[4], 10, 64)
				fr.ttl[key] = time.Now().Add(time.Duration(ms) * time.Millisecond)
			}
			reply = "+OK\r\n"
		case "EXISTS":
			_, ok := fr.data[args[1]]
			if ok {
				reply = ":1\r\n"
			} else {
				reply = ":0\r\n"
			}
		case "DEL":
			delete(fr.data, args[1])
			delete(fr.ttl, args[1])
			reply = ":1\r\n"
		default:
			reply = "-ERR unknown\r\n"
		}
		fr.mu.Unlock()
		if _, err := conn.Write([]byte(reply)); err != nil {
			return
		}
	}
}

func (fr *fakeRedis) expireLocked() {
	now := time.Now()
	for k, until := range fr.ttl {
		if !until.After(now) {
			delete(fr.data, k)
			delete(fr.ttl, k)
		}
	}
}

func (fr *fakeRedis) getIntLocked(key string) int64 {
	v, ok := fr.data[key]
	if !ok {
		return 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func readRESPArray(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("want array, got %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hlen, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hlen = strings.TrimRight(hlen, "\r\n")
		if !strings.HasPrefix(hlen, "$") {
			return nil, fmt.Errorf("want bulk, got %q", hlen)
		}
		ln, err := strconv.Atoi(hlen[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, ln+2)
		if _, err := readFull(r, buf); err != nil {
			return nil, err
		}
		out = append(out, string(buf[:ln]))
	}
	return out, nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func TestRedisCounter_AgainstFakeServer(t *testing.T) {
	fr, addr := startFakeRedis(t)
	defer fr.Close()

	rc, err := NewRedisCounter(addr)
	if err != nil {
		t.Fatal(err)
	}
	n1, err := rc.IncrWindow("rpm:1", time.Minute)
	if err != nil || n1 != 1 {
		t.Fatalf("n1=%d err=%v", n1, err)
	}
	n2, err := rc.IncrWindow("rpm:1", time.Minute)
	if err != nil || n2 != 2 {
		t.Fatalf("n2=%d err=%v", n2, err)
	}
	n3, err := rc.IncrWindowBy("tpm:1", 500, time.Minute)
	if err != nil || n3 != 500 {
		t.Fatalf("n3=%d err=%v", n3, err)
	}
	got, err := rc.Get("rpm:1")
	if err != nil || got != 2 {
		t.Fatalf("get=%d err=%v", got, err)
	}
}

func TestRedisCooldown_AgainstFakeServer(t *testing.T) {
	fr, addr := startFakeRedis(t)
	defer fr.Close()

	c, err := NewClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	cd := NewRedisCooldown(c)
	if err := cd.Mark(99, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	ok, err := cd.Active(99)
	if err != nil || !ok {
		t.Fatalf("active=%v err=%v", ok, err)
	}
	if err := cd.Clear(99); err != nil {
		t.Fatal(err)
	}
	ok, err = cd.Active(99)
	if err != nil || ok {
		t.Fatalf("cleared active=%v err=%v", ok, err)
	}
}

func TestRedisCounter_DialFailure(t *testing.T) {
	rc, err := NewRedisCounter("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	// Force very short timeout.
	rc.client.timeout = 50 * time.Millisecond
	if _, err := rc.IncrWindow("x", time.Minute); err == nil {
		t.Fatal("expected dial error")
	}
}
