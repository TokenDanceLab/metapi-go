package sharedcount

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCounter_IncrWindow(t *testing.T) {
	m := NewMemoryCounter()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	m.now = func() time.Time { return now }

	for i := 1; i <= 3; i++ {
		n, err := m.Incr(context.Background(), "k", time.Minute)
		if err != nil || n != int64(i) {
			t.Fatalf("i=%d n=%d err=%v", i, n, err)
		}
	}
	now = base.Add(61 * time.Second)
	n, err := m.Incr(context.Background(), "k", time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("after window n=%d err=%v", n, err)
	}
}

func TestMemoryCounter_IncrByWindow(t *testing.T) {
	m := NewMemoryCounter()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	m.now = func() time.Time { return now }

	n, err := m.IncrBy(context.Background(), "tpm", 600, time.Minute)
	if err != nil || n != 600 {
		t.Fatalf("first: n=%d err=%v", n, err)
	}
	n, err = m.IncrBy(context.Background(), "tpm", 400, time.Minute)
	if err != nil || n != 1000 {
		t.Fatalf("second: n=%d err=%v", n, err)
	}
	now = base.Add(61 * time.Second)
	n, err = m.IncrBy(context.Background(), "tpm", 50, time.Minute)
	if err != nil || n != 50 {
		t.Fatalf("after window: n=%d err=%v", n, err)
	}
}

func TestMemoryCounter_DecrRollback(t *testing.T) {
	m := NewMemoryCounter()
	for i := 0; i < 3; i++ {
		if _, err := m.Incr(context.Background(), "k", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	n, err := m.Decr(context.Background(), "k", time.Minute)
	if err != nil || n != 2 {
		t.Fatalf("decr: n=%d err=%v", n, err)
	}
	n, err = m.Decr(context.Background(), "k", time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("decr2: n=%d err=%v", n, err)
	}
	// Extra Decr floors at 0.
	n, err = m.Decr(context.Background(), "k", time.Minute)
	if err != nil || n != 0 {
		t.Fatalf("decr3: n=%d err=%v", n, err)
	}
	n, err = m.Decr(context.Background(), "k", time.Minute)
	if err != nil || n != 0 {
		t.Fatalf("decr floor: n=%d err=%v", n, err)
	}
}

func TestMemoryCounter_IncrByNegativeRollback(t *testing.T) {
	m := NewMemoryCounter()
	n, err := m.IncrBy(context.Background(), "tpm", 100, time.Minute)
	if err != nil || n != 100 {
		t.Fatalf("seed: n=%d err=%v", n, err)
	}
	n, err = m.IncrBy(context.Background(), "tpm", -30, time.Minute)
	if err != nil || n != 70 {
		t.Fatalf("rollback: n=%d err=%v", n, err)
	}
	// Zero delta is a peek.
	n, err = m.IncrBy(context.Background(), "tpm", 0, time.Minute)
	if err != nil || n != 70 {
		t.Fatalf("peek: n=%d err=%v", n, err)
	}
	// Over-rollback floors at 0.
	n, err = m.IncrBy(context.Background(), "tpm", -1000, time.Minute)
	if err != nil || n != 0 {
		t.Fatalf("floor: n=%d err=%v", n, err)
	}
}

func TestParseRedisURL(t *testing.T) {
	addr, pass, db, err := ParseRedisURL("redis://:s3cret@localhost:6380/2")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "localhost:6380" || pass != "s3cret" || db != 2 {
		t.Fatalf("addr=%s pass=%s db=%d", addr, pass, db)
	}
	addr, _, _, err = ParseRedisURL("127.0.0.1")
	if err != nil || addr != "127.0.0.1:6379" {
		t.Fatalf("host only: %s %v", addr, err)
	}
}
