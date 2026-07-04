package store

import (
	"testing"
)

// TestAutoMigrateIdempotent verifies that running AutoMigrate twice on the
// same database produces no errors. This is critical for startup behavior.
func TestAutoMigrateIdempotent(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// First migration.
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("first AutoMigrate failed: %v", err)
	}

	// Second migration — must be idempotent (no errors).
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("second AutoMigrate failed (should be idempotent): %v", err)
	}

	// Verify all 27 tables still exist.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	if err != nil {
		t.Fatalf("table count query failed: %v", err)
	}
	// 27 user tables + sqlite_sequence may exist.
	if count < 27 {
		t.Errorf("expected at least 27 tables after double migrate, got %d", count)
	}
}

// TestAutoMigrateIdempotentWithData verifies that existing data survives
// a second AutoMigrate call.
func TestAutoMigrateIdempotentWithData(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// First migration.
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("first AutoMigrate failed: %v", err)
	}

	// Insert test data.
	_, err = db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "persist.key", "persist.value")
	if err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}

	// Second migration.
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("second AutoMigrate failed: %v", err)
	}

	// Verify data survived.
	var val string
	err = db.QueryRow(`SELECT value FROM settings WHERE key = ?`, "persist.key").Scan(&val)
	if err != nil {
		t.Fatalf("SELECT after second migrate failed: %v", err)
	}
	if val != "persist.value" {
		t.Errorf("expected 'persist.value', got %q", val)
	}
}

// TestAutoMigrateIndexesExist verifies UNIQUE constraints are enforced at runtime.
// SQLite uses internal autoindex names (sqlite_autoindex_*) rather than the
// user-specified CONSTRAINT name, so we test by attempting duplicate key inserts
// rather than querying sqlite_master for specific index names.
func TestAutoMigrateIndexesExist(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	now := "2026-07-04T12:00:00.000Z"

	// Insert a site and verify (platform, url) UNIQUE constraint works.
	_, err = db.Exec(`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"s1", "https://a.com", "openai", now, now)
	if err != nil {
		t.Fatalf("first site INSERT failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"s2", "https://a.com", "openai", now, now)
	if err == nil {
		t.Error("expected UNIQUE constraint violation on (platform, url), but INSERT succeeded")
	}

	// Verify settings text PK UNIQUE constraint.
	_, err = db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "dup.key", "v1")
	if err != nil {
		t.Fatalf("first settings INSERT failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "dup.key", "v2")
	if err == nil {
		t.Error("expected UNIQUE constraint violation on settings.key, but INSERT succeeded")
	}

	// Verify downstream_api_keys.key UNIQUE constraint.
	_, err = db.Exec(`INSERT INTO downstream_api_keys (name, key, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"k1", "sk-dup", now, now)
	if err != nil {
		t.Fatalf("first downstream_api_keys INSERT failed: %v", err)
	}
	_, err = db.Exec(`INSERT INTO downstream_api_keys (name, key, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"k2", "sk-dup", now, now)
	if err == nil {
		t.Error("expected UNIQUE constraint violation on downstream_api_keys.key, but INSERT succeeded")
	}

	// Verify total index count is non-zero (at minimum, autoindexes for PKs and UNIQUEs).
	var indexCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index'`).Scan(&indexCount); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if indexCount == 0 {
		t.Error("expected at least some indexes, found none")
	}
	t.Logf("index count in SQLite schema: %d", indexCount)
}

// TestAutoMigrateTableStructure verifies column-level details after migration.
func TestAutoMigrateTableStructure(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// Verify settings table: key should be TEXT PRIMARY KEY (not INTEGER).
	rows, err := db.Query(`PRAGMA table_info('settings')`)
	if err != nil {
		t.Fatalf("PRAGMA table_info settings failed: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan column info failed: %v", err)
		}
		if name == "key" && pk == 1 {
			found = true
			if colType != "TEXT" {
				t.Errorf("settings.key: expected TEXT type, got %q", colType)
			}
		}
		// There should be no 'id' column.
		if name == "id" {
			t.Error("settings table should not have an 'id' column (text PK)")
		}
	}
	if !found {
		t.Error("settings table missing PRIMARY KEY on 'key' column")
	}
}

// TestAutoMigrateTextPKTables verifies both text-PK tables are correctly structured.
func TestAutoMigrateTextPKTables(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// Verify analytics_projection_checkpoints uses text PK.
	rows, err := db.Query(`PRAGMA table_info('analytics_projection_checkpoints')`)
	if err != nil {
		t.Fatalf("PRAGMA table_info failed: %v", err)
	}
	defer rows.Close()

	hasID := false
	hasProjectorKeyPK := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan column info failed: %v", err)
		}
		if name == "id" {
			hasID = true
		}
		if name == "projector_key" && pk == 1 {
			hasProjectorKeyPK = true
		}
	}
	if hasID {
		t.Error("analytics_projection_checkpoints should not have an 'id' column (text PK)")
	}
	if !hasProjectorKeyPK {
		t.Error("analytics_projection_checkpoints missing PRIMARY KEY on 'projector_key'")
	}
}

// TestMigrateFunctionDoesNotPanic verifies the exported Migrate() function
// handles a nil DB gracefully.
func TestMigrateFunctionDoesNotPanic(t *testing.T) {
	// Close any existing DB to test nil case.
	CloseDatabase()

	// Migrate should not panic when DB is nil.
	// It logs a warning and returns nil.
	// We pass a nil config since Migrate() accesses cfg fields minimally.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Migrate() panicked with nil DB: %v", r)
		}
	}()
	// Migrate(nil) — cfg is only used for logging context, nil is safe here
	// since the function returns early on db==nil.
	err := Migrate(nil)
	if err != nil {
		t.Logf("Migrate with nil DB returned error: %v", err)
	}
}
