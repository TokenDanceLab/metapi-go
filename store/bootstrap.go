package store

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/tokendancelab/metapi-go/config"
)

var (
	activeDB    *DB
	initialized bool
	mu          sync.Mutex
)

// GetDB returns the current active database connection.
// Returns nil if the database has not been initialized.
func GetDB() *DB {
	mu.Lock()
	defer mu.Unlock()
	return activeDB
}

// EnsureRuntimeDatabase creates the data directory, opens the database,
// and runs auto-migration. This replaces the P0 stub.
// Mirrors TS initDb() module-level singleton behavior.
func EnsureRuntimeDatabase(cfg *config.Config) error {
	mu.Lock()
	defer mu.Unlock()

	if initialized && activeDB != nil {
		// Already initialized — idempotent (mirrors TS module-level singleton).
		return nil
	}

	// Ensure data directory exists.
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = config.DefaultDataDir
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("bootstrap: failed to create data directory %q: %w", dataDir, err)
	}

	// Determine DSN based on dialect.
	dialect := cfg.DbType
	dsn := cfg.DbUrl

	if dialect == DialectSQLite {
		sqlitePath := ResolveSQLitePath(dsn, dataDir)
		// Create parent directory for SQLite file if not :memory:.
		if sqlitePath != ":memory:" {
			if err := os.MkdirAll(filepath.Dir(sqlitePath), 0755); err != nil {
				return fmt.Errorf("bootstrap: failed to create SQLite parent dir: %w", err)
			}
		}
		slog.Info("bootstrap: opening SQLite database", "path", sqlitePath)
		dsn = sqlitePath
	} else if dialect == DialectPostgres {
		slog.Info("bootstrap: opening PostgreSQL database", "dsn", maskDSN(dsn))
	} else {
		return fmt.Errorf("bootstrap: unsupported DB_TYPE %q", dialect)
	}

	// Open database connection.
	db, err := OpenWithPostgresSSLMode(dialect, dsn, cfg.PostgresSSLMode())
	if err != nil {
		return fmt.Errorf("bootstrap: failed to open database: %w", err)
	}

	// Run auto-migration (CREATE TABLE IF NOT EXISTS for all 27 tables).
	if err := AutoMigrate(db); err != nil {
		db.Close()
		return fmt.Errorf("bootstrap: auto-migration failed: %w", err)
	}

	activeDB = db
	initialized = true

	slog.Info("bootstrap: database ready", "dialect", dialect)
	return nil
}

// CloseDatabase closes the active database connection and resets the
// initialized flag so EnsureRuntimeDatabase can be called again.
func CloseDatabase() error {
	mu.Lock()
	defer mu.Unlock()
	if activeDB != nil {
		err := activeDB.Close()
		activeDB = nil
		initialized = false
		return err
	}
	return nil
}

// OverrideDB replaces the active database singleton with the given DB.
// Intended for tests that need to inject a pre-configured connection.
func OverrideDB(db *DB) {
	mu.Lock()
	defer mu.Unlock()
	activeDB = db
	initialized = db != nil
}

// maskDSN redacts the password portion of a PostgreSQL connection string for logging.
func maskDSN(dsn string) string {
	result := dsn
	atIdx := -1
	for i := len(dsn) - 1; i >= 0; i-- {
		if dsn[i] == '@' {
			atIdx = i
			break
		}
	}
	if atIdx > 0 {
		for i := atIdx - 1; i >= 0; i-- {
			if dsn[i] == ':' {
				result = dsn[:i+1] + "***" + dsn[atIdx:]
				break
			}
		}
	}
	return result
}
