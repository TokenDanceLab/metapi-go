package store

import (
	"testing"
)

// TestSchemaMigrationsTableCreated verifies AutoMigrate creates the
// schema_migrations bookkeeping table even when no additive steps are registered.
func TestSchemaMigrationsTableCreated(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&name)
	if err != nil {
		t.Fatalf("schema_migrations table missing: %v", err)
	}
	if name != "schema_migrations" {
		t.Errorf("unexpected table name %q", name)
	}
}

// TestApplyAdditiveMigrationsRegistryAppliesSC2 verifies production SC2 steps
// are recorded once via schema_migrations after AutoMigrate.
func TestApplyAdditiveMigrationsRegistryAppliesSC2(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// Second Apply is a no-op (idempotent bookkeeping).
	if err := ApplyAdditiveMigrations(db); err != nil {
		t.Fatalf("ApplyAdditiveMigrations second: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(enterpriseAdditiveSteps) {
		t.Errorf("expected %d applied migrations, got %d", len(enterpriseAdditiveSteps), n)
	}
	for _, step := range enterpriseAdditiveSteps {
		var v string
		err := db.QueryRow(`SELECT version FROM schema_migrations WHERE version = ?`, step.Version).Scan(&v)
		if err != nil {
			t.Fatalf("missing migration %s: %v", step.Version, err)
		}
	}
}

// TestEnsureColumnSQLite adds a probe column to an existing table and is idempotent.
// Uses a non-production column name so SC2 registry columns do not pre-exist.
func TestEnsureColumnSQLite(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	const col = "sc2_probe_col"
	exists, err := columnExists(db, "sites", col)
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if exists {
		t.Fatalf("expected sites.%s to be absent before EnsureColumn", col)
	}

	if err := EnsureColumn(db, "sites", col, "INTEGER", "INTEGER", "DEFAULT 0"); err != nil {
		t.Fatalf("EnsureColumn first: %v", err)
	}
	exists, err = columnExists(db, "sites", col)
	if err != nil {
		t.Fatalf("columnExists after add: %v", err)
	}
	if !exists {
		t.Fatalf("expected sites.%s after EnsureColumn", col)
	}

	if err := EnsureColumn(db, "sites", col, "INTEGER", "INTEGER", "DEFAULT 0"); err != nil {
		t.Fatalf("EnsureColumn second: %v", err)
	}

	now := "2026-07-16T00:00:00.000Z"
	_, err = db.Exec(`INSERT INTO sites (name, url, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"s1", "https://example.com", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var mc int
	if err := db.QueryRow(`SELECT `+col+` FROM sites WHERE name = ?`, "s1").Scan(&mc); err != nil {
		t.Fatalf("select %s: %v", col, err)
	}
	if mc != 0 {
		t.Errorf("expected default 0, got %d", mc)
	}
}

// TestEnsureColumnNullableNoDefault models per-key proxy: NULL means fall back.
func TestEnsureColumnNullableNoDefault(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	if err := EnsureColumn(db, "downstream_api_keys", "proxy_url", "TEXT", "TEXT", ""); err != nil {
		t.Fatalf("EnsureColumn proxy_url: %v", err)
	}

	now := "2026-07-16T00:00:00.000Z"
	_, err = db.Exec(`INSERT INTO downstream_api_keys (name, key, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"k1", "sk-test", now, now)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}

	var proxy *string
	if err := db.QueryRow(`SELECT proxy_url FROM downstream_api_keys WHERE key = ?`, "sk-test").Scan(&proxy); err != nil {
		t.Fatalf("select proxy_url: %v", err)
	}
	if proxy != nil {
		t.Errorf("expected NULL proxy_url (old behavior), got %q", *proxy)
	}
}

// TestAdditiveStepRegistryAppliesOnce exercises the version bookkeeping path
// with an explicit step list (does not mutate the production registry).
func TestAdditiveStepRegistryAppliesOnce(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	var applyCount int
	steps := []AdditiveStep{
		{
			Version:     "test_001_sites_context_probe",
			Description: "test-only additive column",
			Apply: func(db *DB) error {
				applyCount++
				return EnsureColumn(db, "token_routes", "context_length", "INTEGER", "INTEGER", "")
			},
		},
	}

	if err := applyAdditiveMigrations(db, steps); err != nil {
		t.Fatalf("first ApplyAdditiveMigrations: %v", err)
	}
	if applyCount != 1 {
		t.Fatalf("expected Apply once, got %d", applyCount)
	}

	// Second run must skip (bookkeeping hit).
	if err := applyAdditiveMigrations(db, steps); err != nil {
		t.Fatalf("second ApplyAdditiveMigrations: %v", err)
	}
	if applyCount != 1 {
		t.Fatalf("expected Apply still once after re-run, got %d", applyCount)
	}

	var version, desc string
	err = db.QueryRow(`SELECT version, description FROM schema_migrations WHERE version = ?`,
		"test_001_sites_context_probe").Scan(&version, &desc)
	if err != nil {
		t.Fatalf("bookkeeping row missing: %v", err)
	}
	if version != "test_001_sites_context_probe" {
		t.Errorf("version = %q", version)
	}

	exists, err := columnExists(db, "token_routes", "context_length")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !exists {
		t.Fatal("expected token_routes.context_length after step")
	}
}

// TestAdditiveStepIdempotentWithoutBookkeeping simulates a crash between DDL
// and INSERT into schema_migrations: re-run must succeed because EnsureColumn
// is idempotent, then bookkeeping is written.
func TestAdditiveStepIdempotentWithoutBookkeeping(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// Pre-apply DDL without bookkeeping.
	if err := EnsureColumn(db, "sites", "max_concurrency", "INTEGER", "INTEGER", "DEFAULT 0"); err != nil {
		t.Fatalf("pre EnsureColumn: %v", err)
	}

	steps := []AdditiveStep{
		{
			Version:     "test_002_max_concurrency",
			Description: "crash-recovery simulation",
			Apply: func(db *DB) error {
				return EnsureColumn(db, "sites", "max_concurrency", "INTEGER", "INTEGER", "DEFAULT 0")
			},
		},
	}

	if err := applyAdditiveMigrations(db, steps); err != nil {
		t.Fatalf("Apply after partial DDL: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		"test_002_max_concurrency").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected bookkeeping row, got count %d", n)
	}
}

// TestApplyAdditiveMigrationsNilDB returns an error instead of panicking.
func TestApplyAdditiveMigrationsNilDB(t *testing.T) {
	if err := ApplyAdditiveMigrations(nil); err == nil {
		t.Fatal("expected error for nil db")
	}
}

// TestEnsureColumnInvalidIdent rejects unsafe identifiers.
func TestEnsureColumnInvalidIdent(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	err = EnsureColumn(db, "sites;drop", "x", "TEXT", "TEXT", "")
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

// TestColumnExistsUnknownTableSQLite returns false without fatal for empty result.
func TestColumnExistsUnknownTableSQLite(t *testing.T) {
	db, err := Open(DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// No AutoMigrate — table missing. PRAGMA table_info on missing table yields empty rows.
	exists, err := columnExists(db, "no_such_table", "id")
	if err != nil {
		t.Fatalf("columnExists on missing table: %v", err)
	}
	if exists {
		t.Error("expected false for missing table")
	}
}

// TestPostgresEnsureColumnAndMigrations exercises dual-dialect helpers when
// PG_TEST_DSN is available (skipped otherwise).
func TestPostgresEnsureColumnAndMigrations(t *testing.T) {
	skipIfNoPG(t)

	db := openTestPG(t)

	// Bookkeeping table must exist after AutoMigrate.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("schema_migrations missing on PG: %v", err)
	}

	// Fixed name is fine — EnsureColumn is idempotent; leave column if present
	// on shared PG test databases.
	col := "sc1_test_probe_col"

	if err := EnsureColumn(db, "sites", col, "INTEGER", "INTEGER", "DEFAULT 0"); err != nil {
		t.Fatalf("PG EnsureColumn: %v", err)
	}
	exists, err := columnExists(db, "sites", col)
	if err != nil {
		t.Fatalf("PG columnExists: %v", err)
	}
	if !exists {
		t.Fatal("PG column not found after EnsureColumn")
	}
	// Idempotent.
	if err := EnsureColumn(db, "sites", col, "INTEGER", "INTEGER", "DEFAULT 0"); err != nil {
		t.Fatalf("PG EnsureColumn second: %v", err)
	}

	steps := []AdditiveStep{
		{
			Version:     "test_pg_001_bookkeeping",
			Description: "pg dual-dialect bookkeeping smoke",
			Apply: func(db *DB) error {
				// No-op DDL; only exercises mark path. Column already present.
				return EnsureColumn(db, "sites", col, "INTEGER", "INTEGER", "DEFAULT 0")
			},
		},
	}

	if err := applyAdditiveMigrations(db, steps); err != nil {
		t.Fatalf("PG ApplyAdditiveMigrations: %v", err)
	}
	if err := applyAdditiveMigrations(db, steps); err != nil {
		t.Fatalf("PG ApplyAdditiveMigrations second: %v", err)
	}

	var cnt int
	err = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
		"test_pg_001_bookkeeping").Scan(&cnt)
	if err != nil {
		t.Fatalf("PG bookkeeping select: %v", err)
	}
	if cnt != 1 {
		t.Errorf("expected 1 bookkeeping row, got %d", cnt)
	}
}
