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
