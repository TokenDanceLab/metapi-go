package store

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// AdditiveStep is one forward-only schema change applied after the base
// CREATE TABLE IF NOT EXISTS bootstrap. Steps must be additive (new tables,
// columns, indexes) and safe to re-attempt if bookkeeping is incomplete.
//
// Version IDs are stable strings (not integers) so SC2+ can insert descriptive
// IDs such as "sc2_001_downstream_proxy_url" without renumbering history.
type AdditiveStep struct {
	// Version is the primary key stored in schema_migrations.
	Version string
	// Description is a short human-readable note for operators and logs.
	Description string
	// Apply performs the DDL/DML for this step. It should be idempotent where
	// practical (e.g. EnsureColumn) so a crash between DDL and bookkeeping
	// does not break the next startup.
	Apply func(db *DB) error
}

// enterpriseAdditiveSteps is the ordered registry of production additive
// upgrades. SC2 registers the enterprise columns documented in
// docs/analysis/schema-parity.md §5. Fresh installs also get these columns
// from base CREATE TABLE builders; EnsureColumn keeps old installs converging.
//
// Keep this list append-only. Never edit or remove a shipped Version string.
var enterpriseAdditiveSteps = []AdditiveStep{
	{
		Version:     "sc2_001_downstream_proxy_url",
		Description: "downstream_api_keys.proxy_url TEXT NULL — per-key egress proxy; NULL falls back to site/system",
		Apply: func(db *DB) error {
			return EnsureColumn(db, "downstream_api_keys", "proxy_url", "TEXT", "TEXT", "")
		},
	},
	{
		Version:     "sc2_002_site_max_concurrency",
		Description: "sites.max_concurrency INTEGER DEFAULT 0 — 0 means unlimited (legacy behavior)",
		Apply: func(db *DB) error {
			return EnsureColumn(db, "sites", "max_concurrency", "INTEGER", "INTEGER", "DEFAULT 0")
		},
	},
	{
		// SC0 Option A: route-level context window metadata. No model_catalog
		// table exists; token_routes is the clear column home until a catalog lands.
		Version:     "sc2_003_route_context_length",
		Description: "token_routes.context_length INTEGER NULL — unknown/no enforcement when NULL",
		Apply: func(db *DB) error {
			return EnsureColumn(db, "token_routes", "context_length", "INTEGER", "INTEGER", "")
		},
	},
	{
		// Learn #110: end-to-end request/trace ids across channel retries.
		Version:     "sc2_004_proxy_logs_request_id",
		Description: "proxy_logs.request_id TEXT NULL — ingress X-Request-Id shared across retry/failover attempts",
		Apply: func(db *DB) error {
			if err := EnsureColumn(db, "proxy_logs", "request_id", "TEXT", "TEXT", ""); err != nil {
				return err
			}
			return EnsureIndex(db, "proxy_logs_request_id_created_at_idx",
				`CREATE INDEX IF NOT EXISTS proxy_logs_request_id_created_at_idx ON proxy_logs (request_id, created_at)`)
		},
	},
	{
		// Learn #116: soft RPM admission per managed downstream key.
		Version:     "sc2_005_downstream_rpm_limit",
		Description: "downstream_api_keys.rpm_limit INTEGER NULL - soft RPM admission; NULL/0 unlimited",
		Apply: func(db *DB) error {
			return EnsureColumn(db, "downstream_api_keys", "rpm_limit", "INTEGER", "INTEGER", "")
		},
	},
}

// schemaMigrationsDDL creates the version bookkeeping table.
// Dual-dialect identical: text PK + ISO-8601 applied_at filled by the app.
const schemaMigrationsDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL,
	description TEXT
)`

// ensureSchemaMigrationsTable creates the schema_migrations bookkeeping table.
func ensureSchemaMigrationsTable(db *DB) error {
	if _, err := db.Exec(schemaMigrationsDDL); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}
	return nil
}

// appliedVersions returns the set of Version strings already recorded.
func appliedVersions(db *DB) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("store: list schema_migrations: %w", err)
	}
	defer rows.Close()

	out := make(map[string]struct{})
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: scan schema_migrations: %w", err)
		}
		out[v] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate schema_migrations: %w", err)
	}
	return out, nil
}

// markMigrationApplied records a successful step. Uses INSERT OR IGNORE /
// ON CONFLICT DO NOTHING so a concurrent or retried mark is safe.
func markMigrationApplied(db *DB, version, description string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	var q string
	if db.Dialect == DialectPostgres {
		q = `INSERT INTO schema_migrations (version, applied_at, description)
			VALUES (?, ?, ?)
			ON CONFLICT (version) DO NOTHING`
	} else {
		q = `INSERT OR IGNORE INTO schema_migrations (version, applied_at, description)
			VALUES (?, ?, ?)`
	}
	if _, err := db.Exec(q, version, now, description); err != nil {
		return fmt.Errorf("store: mark migration %s: %w", version, err)
	}
	return nil
}

// ApplyAdditiveMigrations ensures the bookkeeping table exists, then runs any
// pending enterpriseAdditiveSteps in registry order. Safe on fresh and old DBs.
func ApplyAdditiveMigrations(db *DB) error {
	return applyAdditiveMigrations(db, enterpriseAdditiveSteps)
}

// applyAdditiveMigrations is the testable core that accepts an explicit step list.
func applyAdditiveMigrations(db *DB, steps []AdditiveStep) error {
	if db == nil {
		return fmt.Errorf("store: ApplyAdditiveMigrations: db is nil")
	}

	if err := ensureSchemaMigrationsTable(db); err != nil {
		return err
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, step := range steps {
		if step.Version == "" {
			return fmt.Errorf("store: additive step missing version")
		}
		if _, ok := applied[step.Version]; ok {
			continue
		}
		if step.Apply == nil {
			return fmt.Errorf("store: additive step %s has nil Apply", step.Version)
		}

		slog.Info("store: applying additive migration",
			"version", step.Version,
			"description", step.Description,
			"dialect", db.Dialect,
		)
		if err := step.Apply(db); err != nil {
			return fmt.Errorf("store: additive migration %s: %w", step.Version, err)
		}
		if err := markMigrationApplied(db, step.Version, step.Description); err != nil {
			return err
		}
		applied[step.Version] = struct{}{}
	}

	return nil
}

// columnExists reports whether table.column is present.
// SQLite: PRAGMA table_info; PostgreSQL: information_schema.columns.
func columnExists(db *DB, table, column string) (bool, error) {
	table = strings.TrimSpace(table)
	column = strings.TrimSpace(column)
	if table == "" || column == "" {
		return false, fmt.Errorf("store: columnExists: empty table or column")
	}

	if db.Dialect == DialectPostgres {
		var n int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = ?
			  AND column_name = ?`, table, column).Scan(&n)
		if err != nil {
			return false, fmt.Errorf("store: columnExists(%s.%s) pg: %w", table, column, err)
		}
		return n > 0, nil
	}

	// SQLite — PRAGMA table_info does not accept bound parameters for the table name.
	// Table names come only from our own registry / tests (not user input).
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, quoteIdentSQLite(table)))
	if err != nil {
		return false, fmt.Errorf("store: columnExists(%s.%s) sqlite: %w", table, column, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("store: columnExists scan: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// quoteIdentSQLite double-quotes a simple identifier for PRAGMA use.
// Rejects identifiers that are not alphanumeric/underscore to avoid injection.
func quoteIdentSQLite(name string) string {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		// Fall back to empty quoted form; callers validate names from registry.
		return `""`
	}
	return `"` + name + `"`
}

// EnsureColumn adds table.column if missing. Types are dialect-specific
// fragments (e.g. sqliteType="TEXT", pgType="TEXT", or INTEGER vs BOOLEAN).
// defaultClause is optional SQL after the type (e.g. "DEFAULT 0" or "DEFAULT FALSE");
// pass "" for nullable columns with no default (NULL → old behavior).
//
// This is the primary primitive SC2 uses for enterprise columns.
func EnsureColumn(db *DB, table, column, sqliteType, pgType, defaultClause string) error {
	exists, err := columnExists(db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	colType := sqliteType
	if db.Dialect == DialectPostgres {
		colType = pgType
	}
	colType = strings.TrimSpace(colType)
	if colType == "" {
		return fmt.Errorf("store: EnsureColumn %s.%s: empty type for dialect %s", table, column, db.Dialect)
	}

	// Validate identifiers from registry only.
	if quoteIdentSQLite(table) == `""` || quoteIdentSQLite(column) == `""` {
		return fmt.Errorf("store: EnsureColumn: invalid identifier %q.%q", table, column)
	}

	def := strings.TrimSpace(defaultClause)
	var ddl string
	if def == "" {
		ddl = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, colType)
	} else {
		ddl = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s %s`, table, column, colType, def)
	}

	if _, err := db.Exec(ddl); err != nil {
		// Race / concurrent startup: column may already exist.
		if existsNow, checkErr := columnExists(db, table, column); checkErr == nil && existsNow {
			return nil
		}
		return fmt.Errorf("store: EnsureColumn %s.%s: %w", table, column, err)
	}
	slog.Info("store: added column", "table", table, "column", column, "dialect", db.Dialect)
	return nil
}

// EnsureIndex creates a non-unique index if missing. Both dialects support
// CREATE INDEX IF NOT EXISTS.
func EnsureIndex(db *DB, name, createSQL string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(createSQL) == "" {
		return fmt.Errorf("store: EnsureIndex: empty name or SQL")
	}
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("store: EnsureIndex %s: %w", name, err)
	}
	return nil
}
