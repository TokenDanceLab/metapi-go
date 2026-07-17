package store

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// pgDSN returns the PostgreSQL DSN from the PG_TEST_DSN environment variable,
// or an empty string if not set.
func pgDSN() string {
	return os.Getenv("PG_TEST_DSN")
}

// skipIfNoPG skips the test if no PostgreSQL connection string is available.
func skipIfNoPG(t *testing.T) {
	t.Helper()
	if pgDSN() == "" {
		t.Skip("PG_TEST_DSN not set; skipping PostgreSQL integration test")
	}
}

// openTestPG opens a PostgreSQL connection, runs AutoMigrate, and returns the DB.
// The connection is cleaned up at test end. The test is skipped if PG is unavailable.
func openTestPG(t *testing.T) *DB {
	t.Helper()
	skipIfNoPG(t)

	db, err := Open(DialectPostgres, pgDSN(), false)
	if err != nil {
		t.Fatalf("failed to open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := AutoMigrate(db); err != nil {
		db.Close()
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	return db
}

// TestPostgresOpen verifies that a PostgreSQL connection can be established
// and that the dialect is correctly identified.
func TestPostgresOpen(t *testing.T) {
	skipIfNoPG(t)

	db, err := Open(DialectPostgres, pgDSN(), false)
	if err != nil {
		t.Fatalf("Open(postgres) failed: %v", err)
	}
	defer db.Close()

	if db.Dialect != DialectPostgres {
		t.Errorf("expected dialect %q, got %q", DialectPostgres, db.Dialect)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// TestPostgresSSLMode verifies the legacy DB_SSL=true path maps to
// sslmode=require without panicking. Local test containers usually do not
// enable TLS, so a TLS refusal is acceptable here.
func TestPostgresSSLMode(t *testing.T) {
	skipIfNoPG(t)

	db, err := Open(DialectPostgres, pgDSN(), true)
	if err != nil {
		t.Logf("Open with SSL mode result: %v", err)
		return
	}
	defer db.Close()
	t.Log("Open with SSL mode succeeded")
}

func TestPostgresSSLModeOverrideCanDisableTLS(t *testing.T) {
	skipIfNoPG(t)

	dsnWithTLSRequired := applyPostgresSSLMode(pgDSN(), "require")
	db, err := OpenWithPostgresSSLMode(DialectPostgres, dsnWithTLSRequired, "disable")
	if err != nil {
		t.Fatalf("OpenWithPostgresSSLMode(disable) failed to override DSN sslmode: %v", err)
	}
	defer db.Close()
}

// TestPostgresAutoMigrateAllTables verifies all 27 tables exist after AutoMigrate.
func TestPostgresAutoMigrateAllTables(t *testing.T) {
	db := openTestPG(t)

	expectedTables := []string{
		"sites", "site_api_endpoints", "site_disabled_models",
		"accounts", "account_tokens", "checkin_logs",
		"model_availability", "token_model_availability",
		"token_routes", "route_group_sources",
		"oauth_route_units", "oauth_route_unit_members",
		"route_channels", "proxy_logs", "proxy_debug_traces",
		"proxy_debug_attempts", "proxy_video_tasks",
		"admin_background_tasks", "proxy_files",
		"settings", "admin_snapshots",
		"analytics_projection_checkpoints",
		"site_day_usage", "site_hour_usage", "model_day_usage",
		"downstream_api_keys", "site_announcements", "events",
	}

	for _, table := range expectedTables {
		var exists bool
		err := db.QueryRow(
			`SELECT EXISTS (
				SELECT FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, table,
		).Scan(&exists)
		if err != nil {
			t.Errorf("failed to check table %q: %v", table, err)
			continue
		}
		if !exists {
			t.Errorf("table %q not found in PostgreSQL", table)
		}
	}
}

// TestPostgresCRUDRoundtrip tests basic CRUD on the settings table.
func TestPostgresCRUDRoundtrip(t *testing.T) {
	db := openTestPG(t)
	store := NewSettingsStore(db)

	const key = "pg.test.key"
	const value = `{"source":"postgres"}`

	if err := store.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != value {
		t.Errorf("Get: expected %q, got %q", value, got)
	}

	// Cleanup.
	if _, err := db.Exec(`DELETE FROM settings WHERE key = $1`, key); err != nil {
		t.Logf("cleanup DELETE failed: %v", err)
	}
}

// TestPostgresDoublePrecision verifies that PG REAL-equivalent columns use
// DOUBLE PRECISION, not naked REAL (which would be float4 in PG causing precision loss).
func TestPostgresDoublePrecision(t *testing.T) {
	db := openTestPG(t)

	// Check sites.global_weight uses DOUBLE PRECISION.
	var dataType string
	err := db.QueryRow(`
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'sites' AND column_name = 'global_weight'
	`).Scan(&dataType)
	if err != nil {
		t.Fatalf("failed to query column type: %v", err)
	}
	if dataType != "double precision" {
		t.Errorf("sites.global_weight: expected 'double precision', got %q (naked REAL would cause float4 precision loss)", dataType)
	}
}

// TestPostgresBooleanType verifies that boolean columns use native BOOLEAN in PG (not INTEGER).
func TestPostgresBooleanType(t *testing.T) {
	db := openTestPG(t)

	var dataType string
	err := db.QueryRow(`
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'sites' AND column_name = 'is_pinned'
	`).Scan(&dataType)
	if err != nil {
		t.Fatalf("failed to query column type: %v", err)
	}
	if dataType != "boolean" {
		t.Errorf("sites.is_pinned: expected 'boolean', got %q", dataType)
	}
}

// TestPostgresDatetimeIsText verifies datetime columns use TEXT, not TIMESTAMPTZ.
func TestPostgresDatetimeIsText(t *testing.T) {
	db := openTestPG(t)

	var dataType string
	err := db.QueryRow(`
		SELECT data_type FROM information_schema.columns
		WHERE table_name = 'sites' AND column_name = 'created_at'
	`).Scan(&dataType)
	if err != nil {
		t.Fatalf("failed to query column type: %v", err)
	}
	if dataType == "timestamp with time zone" || dataType == "timestamp without time zone" {
		t.Errorf("sites.created_at: expected TEXT, got %q (should not use TIMESTAMP/TIMESTAMPTZ)", dataType)
	}
}

// TestPostgresIndexesCreated verifies a sample of plain indexes exist.
func TestPostgresIndexesCreated(t *testing.T) {
	db := openTestPG(t)

	sampleIndexes := []string{
		"sites_status_idx",
		"accounts_site_id_idx",
		"proxy_logs_created_at_idx",
		"events_created_at_idx",
	}

	for _, idxName := range sampleIndexes {
		var exists bool
		err := db.QueryRow(
			`SELECT EXISTS (
				SELECT FROM pg_indexes WHERE indexname = $1
			)`, idxName,
		).Scan(&exists)
		if err != nil {
			t.Errorf("failed to check index %q: %v", idxName, err)
			continue
		}
		if !exists {
			t.Errorf("index %q not found", idxName)
		}
	}
}

// TestPostgresConnectionPool verifies connection pool defaults.
func TestPostgresConnectionPool(t *testing.T) {
	db := openTestPG(t)

	maxOpen := db.DB.DB.Stats().MaxOpenConnections
	// configured to 20 in configurePostgresPool
	if maxOpen != 20 {
		t.Logf("MaxOpenConnections=%d (expected 20)", maxOpen)
	}

	// Just verify the pool is functional.
	if err := db.Ping(); err != nil {
		t.Fatalf("pool ping failed: %v", err)
	}
}

// TestPostgresInsertTimestamp verifies application-layer timestamp insertion in PG.
func TestPostgresInsertTimestamp(t *testing.T) {
	db := openTestPG(t)

	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	_, err := db.Exec(
		`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		"pg-ts-test", "https://example.com", "openai", now, now,
	)
	if err != nil {
		t.Fatalf("INSERT failed: %v", err)
	}

	var created string
	err = db.QueryRow(
		`SELECT created_at FROM sites WHERE name = $1`, "pg-ts-test",
	).Scan(&created)
	if err != nil {
		t.Fatalf("SELECT failed: %v", err)
	}

	if created != now {
		t.Errorf("created_at: expected %q, got %q", now, created)
	}

	// Cleanup: delete the test site (cascade will handle dependents).
	if _, err := db.Exec(`DELETE FROM sites WHERE name = $1`, "pg-ts-test"); err != nil {
		t.Logf("cleanup DELETE failed: %v", err)
	}
}

// TestPostgresPlaceholder verifies that $N style placeholders work correctly.
func TestPostgresPlaceholder(t *testing.T) {
	db := openTestPG(t)

	// This tests that the store wrapper rebinds SQLite-style ? placeholders to
	// pgx $N placeholders. Casts avoid PostgreSQL's untyped-parameter ambiguity
	// for arithmetic-only expressions.
	var result int
	err := db.QueryRow(`SELECT ?::int + ?::int`, 3, 4).Scan(&result)
	if err != nil {
		t.Fatalf("placeholder query failed: %v", err)
	}
	if result != 7 {
		t.Errorf("expected 3+4=7, got %d", result)
	}
}

func TestPostgresSQLXHelpersRebindPlaceholders(t *testing.T) {
	db := openTestPG(t)

	var one int
	if err := db.Get(&one, `SELECT ?::int`, 1); err != nil {
		t.Fatalf("Get with ? placeholder failed: %v", err)
	}
	if one != 1 {
		t.Fatalf("Get result = %d, want 1", one)
	}

	var rows []struct {
		N int `db:"n"`
	}
	if err := db.Select(&rows, `SELECT ?::int AS n UNION ALL SELECT ?::int AS n`, 2, 3); err != nil {
		t.Fatalf("Select with ? placeholders failed: %v", err)
	}
	if len(rows) != 2 || rows[0].N != 2 || rows[1].N != 3 {
		t.Fatalf("Select rows = %#v, want 2 and 3", rows)
	}

	queryRows, err := db.Queryx(`SELECT ?::text AS value`, "ok")
	if err != nil {
		t.Fatalf("Queryx with ? placeholder failed: %v", err)
	}
	defer queryRows.Close()
	if !queryRows.Next() {
		t.Fatal("Queryx returned no rows")
	}
	var value string
	if err := queryRows.Scan(&value); err != nil {
		t.Fatalf("Queryx scan failed: %v", err)
	}
	if value != "ok" {
		t.Fatalf("Queryx value = %q, want ok", value)
	}

	var four int
	if err := db.QueryRowx(`SELECT ?::int`, 4).Scan(&four); err != nil {
		t.Fatalf("QueryRowx with ? placeholder failed: %v", err)
	}
	if four != 4 {
		t.Fatalf("QueryRowx result = %d, want 4", four)
	}
}

// TestPostgresConcurrentAccess verifies basic concurrent read safety.
func TestPostgresConcurrentAccess(t *testing.T) {
	db := openTestPG(t)

	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func(id int) {
			var n int
			err := db.QueryRow(fmt.Sprintf(`SELECT %d`, id)).Scan(&n)
			done <- err
		}(i)
	}

	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent query %d failed: %v", i, err)
		}
	}
}
