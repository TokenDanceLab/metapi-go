package scheduler

import (
	"context"
	"fmt"
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

func TestSchedulerLeaseTinyPoolUsesLocal(t *testing.T) {
	ResetLeasePressureForTest()
	t.Cleanup(ResetLeasePressureForTest)

	// Simulate a postgres dialect DB with MaxOpen=2 using sqlite driver underneath
	// (pool settings still apply to sql.DB).
	raw, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { raw.Close() })
	// Re-label as postgres for lease path; keep MaxOpen=1 (sqlite default).
	raw.Dialect = store.DialectPostgres
	raw.DB.DB.SetMaxOpenConns(2)
	raw.DB.DB.SetMaxIdleConns(1)

	if !shouldUseLocalLease(raw) {
		t.Fatal("shouldUseLocalLease = false for MaxOpen=2")
	}

	name := "tiny-pool-" + t.Name()
	lease, acquired, err := tryAcquireSchedulerLease(context.Background(), raw, name)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !acquired || lease == nil || !lease.local {
		t.Fatalf("expected local lease, got acquired=%v local=%v err=%v", acquired, lease != nil && lease.local, err)
	}
	lease.Release(context.Background())
}

func TestIsConnectionBudgetError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("pq: sorry, too many clients already"), true},
		{fmt.Errorf("ERROR: too many connections for role \"metapi\" (SQLSTATE 53300)"), true},
		{fmt.Errorf("open lease connection: %w", fmt.Errorf("scheduler lease under connection pressure (retry in 5s)")), true},
		{fmt.Errorf("connection refused"), false},
	}
	for _, tc := range cases {
		if got := isConnectionBudgetError(tc.err); got != tc.want {
			t.Fatalf("isConnectionBudgetError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestNoteLeaseAcquireFailureBackoff(t *testing.T) {
	ResetLeasePressureForTest()
	t.Cleanup(ResetLeasePressureForTest)

	noteLeaseAcquireFailure(fmt.Errorf("SQLSTATE 53300 too many connections"))
	blocked, rem := leasePressureActive()
	if !blocked || rem <= 0 {
		t.Fatalf("expected active backoff, blocked=%v rem=%v", blocked, rem)
	}
}

