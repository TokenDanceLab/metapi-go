package store

import (
	"database/sql"
	"testing"
	"time"
)

// openTestSQLite opens an in-memory SQLite database, runs AutoMigrate, and returns the DB.
func openTestSQLite(t *testing.T) *DB {
	t.Helper()
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite :memory:: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := AutoMigrate(db); err != nil {
		db.Close()
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	return db
}

// TestSQLiteOpenMemory verifies that :memory: mode works with all PRAGMAs applied.
func TestSQLiteOpenMemory(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	defer db.Close()

	if db.Dialect != DialectSQLite {
		t.Errorf("expected dialect %q, got %q", DialectSQLite, db.Dialect)
	}

	// Verify connection is alive.
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// Verify journal mode: :memory: DBs report "memory", file DBs report "wal".
	// Both are correct — WAL is only meaningful for file-backed DBs.
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if journalMode != "wal" && journalMode != "memory" {
		t.Errorf("expected journal_mode=wal (file) or memory (:memory:), got %q", journalMode)
	}

	// Verify foreign_keys are enabled.
	var fkEnabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		t.Fatalf("PRAGMA foreign_keys failed: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fkEnabled)
	}
}

// TestSQLiteAutoMigrateAllTables verifies all 28 tables are created.
func TestSQLiteAutoMigrateAllTables(t *testing.T) {
	db := openTestSQLite(t)

	expectedTables := []string{
		"sites",
		"site_api_endpoints",
		"site_disabled_models",
		"accounts",
		"account_tokens",
		"checkin_logs",
		"model_availability",
		"token_model_availability",
		"token_routes",
		"route_group_sources",
		"oauth_route_units",
		"oauth_route_unit_members",
		"route_channels",
		"proxy_logs",
		"proxy_debug_traces",
		"proxy_debug_attempts",
		"proxy_video_tasks",
		"admin_background_tasks",
		"proxy_files",
		"settings",
		"admin_snapshots",
		"analytics_projection_checkpoints",
		"site_day_usage",
		"site_hour_usage",
		"model_day_usage",
		"downstream_api_keys",
		"site_announcements",
		"events",
	}

	for _, table := range expectedTables {
		var count int
		err := db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&count)
		if err != nil {
			t.Errorf("failed to check table %q: %v", table, err)
			continue
		}
		if count != 1 {
			t.Errorf("table %q not found", table)
		}
	}
}

// TestSQLiteCRUDRoundtrip tests a basic CRUD cycle on the settings table.
func TestSQLiteCRUDRoundtrip(t *testing.T) {
	db := openTestSQLite(t)

	store := NewSettingsStore(db)

	// CREATE: Insert a key.
	const testKey = "test.roundtrip.key"
	const testValue = `{"hello":"world"}`
	if err := store.Set(testKey, testValue); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// READ: Get the key back.
	got, err := store.Get(testKey)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != testValue {
		t.Errorf("Get: expected %q, got %q", testValue, got)
	}

	// UPDATE: Overwrite the key.
	const updatedValue = `{"hello":"updated"}`
	if err := store.Set(testKey, updatedValue); err != nil {
		t.Fatalf("Set (update) failed: %v", err)
	}

	got, err = store.Get(testKey)
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if got != updatedValue {
		t.Errorf("Get after update: expected %q, got %q", updatedValue, got)
	}

	// READ: Non-existent key returns empty.
	got, err = store.Get("nonexistent.key")
	if err != nil {
		t.Fatalf("Get non-existent failed: %v", err)
	}
	if got != "" {
		t.Errorf("Get non-existent: expected empty, got %q", got)
	}
}

// TestSQLiteInsertWithTimestamps verifies that application-layer timestamp
// filling works correctly (the spec says Go fills created_at/updated_at).
func TestSQLiteInsertWithTimestamps(t *testing.T) {
	db := openTestSQLite(t)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Insert a site with application-filled timestamps.
	_, err := db.Exec(
		`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"test-site", "https://example.com", "openai", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT site failed: %v", err)
	}

	// Read it back and verify timestamps.
	var created, updated string
	err = db.QueryRow(
		`SELECT created_at, updated_at FROM sites WHERE name = ?`,
		"test-site",
	).Scan(&created, &updated)
	if err != nil {
		t.Fatalf("SELECT site failed: %v", err)
	}

	if created != now {
		t.Errorf("created_at: expected %q, got %q", now, created)
	}
	if updated != now {
		t.Errorf("updated_at: expected %q, got %q", now, updated)
	}
}

// TestSQLiteForeignKeyCascade verifies that FK CASCADE works in SQLite
// (since foreign_keys PRAGMA is enabled).
func TestSQLiteForeignKeyCascade(t *testing.T) {
	db := openTestSQLite(t)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Insert parent site.
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"parent-site", "https://example.com", "openai", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT site failed: %v", err)
	}
	siteID, _ := res.LastInsertId()

	// Insert child site_api_endpoint.
	res, err = db.Exec(
		`INSERT INTO site_api_endpoints (site_id, url, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		siteID, "https://example.com/v1", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT endpoint failed: %v", err)
	}
	endpointID, _ := res.LastInsertId()

	// Delete parent — child should be cascade-deleted.
	_, err = db.Exec(`DELETE FROM sites WHERE id = ?`, siteID)
	if err != nil {
		t.Fatalf("DELETE site failed: %v", err)
	}

	// Verify child is gone.
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM site_api_endpoints WHERE id = ?`, endpointID).Scan(&count)
	if err != nil {
		t.Fatalf("SELECT endpoint count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected endpoint to be cascade-deleted, but found %d rows", count)
	}
}

// TestSQLiteFKSetNull verifies the critical SET NULL behavior on route_channels.token_id.
func TestSQLiteFKSetNull(t *testing.T) {
	db := openTestSQLite(t)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Insert prerequisites: site, account, account_token, token_route.
	res, err := db.Exec(`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"fk-test", "https://example.com", "openai", now, now)
	if err != nil {
		t.Fatalf("INSERT site failed: %v", err)
	}
	siteID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO accounts (site_id, access_token, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		siteID, "tok-setnull", now, now)
	if err != nil {
		t.Fatalf("INSERT account failed: %v", err)
	}
	accountID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO account_tokens (account_id, name, token, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		accountID, "my-token", "sk-abc-setnull", now, now)
	if err != nil {
		t.Fatalf("INSERT account_token failed: %v", err)
	}
	tokenID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, created_at, updated_at) VALUES (?, ?, ?)`,
		"gpt-4-setnull", now, now)
	if err != nil {
		t.Fatalf("INSERT token_route failed: %v", err)
	}
	routeID, _ := res.LastInsertId()

	// Insert route_channel with token_id.
	res, err = db.Exec(`INSERT INTO route_channels (route_id, account_id, token_id) VALUES (?, ?, ?)`,
		routeID, accountID, tokenID)
	if err != nil {
		t.Fatalf("INSERT route_channel failed: %v", err)
	}
	channelID, _ := res.LastInsertId()

	// Delete the account_token — token_id should be SET NULL, channel preserved.
	_, err = db.Exec(`DELETE FROM account_tokens WHERE id = ?`, tokenID)
	if err != nil {
		t.Fatalf("DELETE account_token failed: %v", err)
	}

	// Verify channel still exists with NULL token_id.
	var remainingTokenID sql.NullInt64
	err = db.QueryRow(`SELECT token_id FROM route_channels WHERE id = ?`, channelID).Scan(&remainingTokenID)
	if err != nil {
		t.Fatalf("SELECT route_channel failed (channel should survive): %v", err)
	}
	if remainingTokenID.Valid {
		t.Errorf("expected token_id to be NULL after SET NULL FK, got %d", remainingTokenID.Int64)
	}
}

// TestSQLiteCheckConstraint verifies non-negative CHECK constraints on usage tables.
func TestSQLiteCheckConstraint(t *testing.T) {
	db := openTestSQLite(t)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Insert a site.
	res, _ := db.Exec(`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"check-test", "https://example.com", "openai", now, now)
	siteID, _ := res.LastInsertId()

	// Valid insert with non-negative values.
	_, err := db.Exec(
		`INSERT INTO site_day_usage (local_day, site_id, total_calls, success_calls, failed_calls,
			total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count,
			created_at, updated_at)
		VALUES (?, ?, 10, 8, 2, 500, 0.01, 0.02, 1500, 10, ?, ?)`,
		"2026-07-04", siteID, now, now,
	)
	if err != nil {
		t.Fatalf("valid INSERT site_day_usage failed: %v", err)
	}

	// Invalid insert with negative value (should fail CHECK constraint).
	_, err = db.Exec(
		`INSERT INTO site_day_usage (local_day, site_id, total_calls, success_calls, failed_calls,
			total_tokens, total_summary_spend, total_site_spend, total_latency_ms, latency_count,
			created_at, updated_at)
		VALUES (?, ?, -1, 8, 2, 500, 0.01, 0.02, 1500, 10, ?, ?)`,
		"2026-07-05", siteID, now, now,
	)
	if err == nil {
		t.Error("expected CHECK constraint violation for negative total_calls, but INSERT succeeded")
	}
}
