package scheduler

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func TestSchedulerLeaseLocalMutualExclusion(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	name := "test-local-" + t.Name()
	lease, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if !acquired || lease == nil {
		t.Fatal("first acquire did not take lease")
	}

	second, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	if acquired || second != nil {
		t.Fatal("second acquire should be skipped while lease is held")
	}

	lease.Release(context.Background())
	third, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("third acquire failed: %v", err)
	}
	if !acquired || third == nil {
		t.Fatal("third acquire should succeed after release")
	}
	third.Release(context.Background())
}

func TestRunWithSchedulerLeaseSkipsWhenLocalLeaseHeld(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	name := "test-run-skip-" + t.Name()
	lease, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("manual acquire failed: %v", err)
	}
	if !acquired {
		t.Fatal("manual acquire did not take lease")
	}
	defer lease.Release(context.Background())

	ran := false
	runWithSchedulerLease(context.Background(), db, name, func() {
		ran = true
	})
	if ran {
		t.Fatal("leased job ran even though another holder owns the lease")
	}
}

func TestSchedulerLeasePostgresAdvisoryLock(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL advisory lock integration test")
	}

	db, err := store.Open(store.DialectPostgres, dsn, false)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	name := "test-pg-" + t.Name()
	lease, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if !acquired || lease == nil {
		t.Fatal("first acquire did not take postgres advisory lock")
	}

	second, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	if acquired || second != nil {
		t.Fatal("second acquire should be skipped while postgres advisory lock is held")
	}

	lease.Release(context.Background())
	third, acquired, err := tryAcquireSchedulerLease(context.Background(), db, name)
	if err != nil {
		t.Fatalf("third acquire failed: %v", err)
	}
	if !acquired || third == nil {
		t.Fatal("third acquire should succeed after postgres advisory lock release")
	}
	third.Release(context.Background())
}

func TestSideEffectSchedulersUseClusterLease(t *testing.T) {
	files := []string{
		"backup_webdav.go",
		"balance.go",
		"channel_recovery.go",
		"checkin.go",
		"daily_summary.go",
		"file_retention.go",
		"log_cleanup.go",
		"log_retention.go",
		"model_probe.go",
		"site_announcement.go",
		"sub2api_refresh.go",
		"update_center.go",
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			if !strings.Contains(string(data), "runWithSchedulerLease(") {
				t.Fatalf("%s does not use runWithSchedulerLease", file)
			}
		})
	}
}
