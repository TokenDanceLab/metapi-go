package scheduler

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// #516: retention cutoffs must be RFC3339 so lexicographic compares work against
// TEXT created_at values written by proxy_logs/events/files/video tasks.

func openRetentionTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func countRows(t *testing.T, db *store.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

func TestLogCleanupDeletesOldRFC3339RowsKeepsInWindow(t *testing.T) {
	db := openRetentionTestDB(t)

	// Seed absolute ages relative to real now (cleanup uses time.Now() - retentionDays).
	// Old morning-style RFC3339 rows must delete; in-window rows stay.
	// Space-format cutoffs used to shield same-day old rows ('T' > ' ').
	oldAt := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	keepAt := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)

	if _, err := db.Exec(
		`INSERT INTO proxy_logs (status, created_at) VALUES (?, ?), (?, ?)`,
		"ok", oldAt, "ok", keepAt,
	); err != nil {
		t.Fatalf("seed proxy_logs: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO events (type, title, level, created_at) VALUES (?, ?, ?, ?), (?, ?, ?, ?)`,
		"info", "old", "info", oldAt,
		"info", "new", "info", keepAt,
	); err != nil {
		t.Fatalf("seed events: %v", err)
	}

	cfg := &config.Config{
		LogCleanupConfigured:         true,
		LogCleanupUsageLogsEnabled:   true,
		LogCleanupProgramLogsEnabled: true,
		LogCleanupRetentionDays:      1,
	}
	s := NewLogCleanupScheduler(cfg)

	// Sanity: historical space-format bug shields same-day old RFC3339 rows.
	sameDayOld := "2026-07-10T08:00:00Z"
	if sameDayOld < "2026-07-10 12:00:00" {
		t.Fatal("expected space-format bug: same-day RFC3339 row should NOT be < space cutoff")
	}

	s.runJobLocked(db)

	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_logs`); got != 1 {
		t.Fatalf("proxy_logs count = %d, want 1 (old deleted, in-window kept)", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_logs WHERE created_at = ?`, keepAt); got != 1 {
		t.Fatalf("in-window proxy_logs missing after cleanup")
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM events`); got != 1 {
		t.Fatalf("events count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM events WHERE created_at = ?`, keepAt); got != 1 {
		t.Fatalf("in-window events missing after cleanup")
	}
}

func TestProxyLogRetentionDeletesOldRFC3339RowsKeepsInWindow(t *testing.T) {
	db := openRetentionTestDB(t)
	oldAt := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339)
	keepAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)

	if _, err := db.Exec(
		`INSERT INTO proxy_logs (status, created_at) VALUES (?, ?), (?, ?)`,
		"ok", oldAt, "ok", keepAt,
	); err != nil {
		t.Fatalf("seed proxy_logs: %v", err)
	}

	s := NewProxyLogRetentionScheduler(&config.Config{ProxyLogRetentionDays: 1})
	s.runCleanupLocked(db, 1)

	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_logs`); got != 1 {
		t.Fatalf("proxy_logs count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_logs WHERE created_at = ?`, keepAt); got != 1 {
		t.Fatalf("in-window proxy_logs deleted")
	}
}

func TestProxyFileRetentionDeletesOldRFC3339RowsKeepsInWindow(t *testing.T) {
	db := openRetentionTestDB(t)
	oldAt := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339)
	keepAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)

	if _, err := db.Exec(
		`INSERT INTO proxy_files (
			public_id, owner_type, owner_id, filename, mime_type, byte_size, sha256, content_base64, created_at, updated_at
		) VALUES
			(?, 'user', '1', 'old.bin', 'application/octet-stream', 1, 'a', 'YQ==', ?, ?),
			(?, 'user', '1', 'new.bin', 'application/octet-stream', 1, 'b', 'Yg==', ?, ?)`,
		"file-old", oldAt, oldAt,
		"file-new", keepAt, keepAt,
	); err != nil {
		t.Fatalf("seed proxy_files: %v", err)
	}

	s := NewProxyFileRetentionScheduler(&config.Config{ProxyFileRetentionDays: 1})
	s.runCleanupLocked(db, 1)

	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_files`); got != 1 {
		t.Fatalf("proxy_files count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_files WHERE public_id = ?`, "file-new"); got != 1 {
		t.Fatalf("in-window proxy_files deleted")
	}
}

func TestProxyVideoTaskRetentionDeletesOldRFC3339RowsKeepsInWindow(t *testing.T) {
	db := openRetentionTestDB(t)
	oldAt := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339)
	keepAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)

	if _, err := db.Exec(
		`INSERT INTO proxy_video_tasks (
			public_id, upstream_video_id, site_url, token_value, created_at, updated_at
		) VALUES
			(?, ?, 'https://example.com', 'tok', ?, ?),
			(?, ?, 'https://example.com', 'tok', ?, ?)`,
		"vid-old", "up-old", oldAt, oldAt,
		"vid-new", "up-new", keepAt, keepAt,
	); err != nil {
		t.Fatalf("seed proxy_video_tasks: %v", err)
	}

	s := NewProxyVideoTaskRetentionScheduler(&config.Config{ProxyVideoTaskRetentionDays: 1})
	s.runCleanupLocked(db, 1)

	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_video_tasks`); got != 1 {
		t.Fatalf("proxy_video_tasks count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM proxy_video_tasks WHERE public_id = ?`, "vid-new"); got != 1 {
		t.Fatalf("in-window proxy_video_tasks deleted")
	}
}

func TestFormatTimeToSQLCutoffComparesWithRFC3339CreatedAt(t *testing.T) {
	// Direct string compare mirrors SQLite TEXT comparison used by DELETE ... created_at < ?.
	cutoff := formatTimeToSQL(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	if cutoff != "2026-07-10T12:00:00Z" {
		t.Fatalf("cutoff = %q, want RFC3339 UTC", cutoff)
	}

	cases := []struct {
		createdAt string
		wantOlder bool
	}{
		{"2026-07-10T08:00:00Z", true},  // same day, older — must delete
		{"2026-07-09T23:59:59Z", true},  // previous day — must delete
		{"2026-07-10T12:00:00Z", false}, // equal — keep (< is strict)
		{"2026-07-10T14:00:00Z", false}, // same day, newer — keep
		{"2026-07-11T00:00:00Z", false}, // next day — keep
	}
	for _, tc := range cases {
		older := tc.createdAt < cutoff
		if older != tc.wantOlder {
			t.Errorf("created_at=%q < cutoff=%q => %v, want %v", tc.createdAt, cutoff, older, tc.wantOlder)
		}
	}
}
