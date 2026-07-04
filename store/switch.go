package store

import (
	"fmt"
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// SwitchDatabase closes the old database connection pool and opens a new one
// with the given dialect and DSN. On failure, it rolls back to the original
// config values and re-opens the original connection.
//
// Mirrors TS index.ts switchRuntimeDatabase() behavior:
//   1. Close all existing connections.
//   2. Update config in-place (dialect, URL, SSL).
//   3. Open new connection + run auto-migration.
//   4. On failure: close new connection, restore old config, re-open old connection.
func SwitchDatabase(cfg *config.Config, nextDialect, nextDsn string, nextSsl bool) error {
	previousDialect := cfg.DbType
	previousDsn := cfg.DbUrl
	previousSsl := cfg.DbSsl

	slog.Info("switch: runtime database switch requested",
		"from", previousDialect,
		"to", nextDialect,
	)

	// Step 1: Close old connection.
	if err := CloseDatabase(); err != nil {
		slog.Warn("switch: failed to close old database", "error", err)
	}

	// Step 2: Update config in-place.
	cfg.DbType = nextDialect
	cfg.DbUrl = nextDsn
	cfg.DbSsl = nextSsl

	// Step 3: Open new connection.
	dsn := nextDsn
	if nextDialect == DialectSQLite {
		dsn = ResolveSQLitePath(nextDsn, cfg.DataDir)
	}

	newDB, err := Open(nextDialect, dsn, nextSsl)
	if err != nil {
		// Step 4: Rollback — restore old config and re-open.
		slog.Error("switch: failed to open new database, rolling back",
			"error", err,
		)
		return rollbackSwitch(cfg, previousDialect, previousDsn, previousSsl, err)
	}

	// Step 3b: Run auto-migration on the new database.
	if err := AutoMigrate(newDB); err != nil {
		newDB.Close()
		slog.Error("switch: auto-migration on new database failed, rolling back",
			"error", err,
		)
		return rollbackSwitch(cfg, previousDialect, previousDsn, previousSsl, err)
	}

	// Set active DB.
	mu.Lock()
	activeDB = newDB
	initialized = true
	mu.Unlock()

	slog.Info("switch: database switch complete", "dialect", nextDialect)
	return nil
}

// rollbackSwitch restores the original config and re-opens the old database
// connection. It wraps the original error so the caller can report both.
func rollbackSwitch(cfg *config.Config, dialect, dsn string, ssl bool, originalErr error) error {
	cfg.DbType = dialect
	cfg.DbUrl = dsn
	cfg.DbSsl = ssl

	rollbackDsn := dsn
	if dialect == DialectSQLite {
		rollbackDsn = ResolveSQLitePath(dsn, cfg.DataDir)
	}

	rollbackDB, rollbackErr := Open(dialect, rollbackDsn, ssl)
	if rollbackErr != nil {
		return fmt.Errorf("switch: database switch failed AND rollback failed: switch=%w, rollback=%w", originalErr, rollbackErr)
	}

	// Run migration on rollback DB.
	if err := AutoMigrate(rollbackDB); err != nil {
		rollbackDB.Close()
		return fmt.Errorf("switch: database switch failed AND rollback migration failed: switch=%w, rollback=%w", originalErr, err)
	}

	mu.Lock()
	activeDB = rollbackDB
	initialized = true
	mu.Unlock()

	return fmt.Errorf("switch: database switch failed: %w", originalErr)
}
