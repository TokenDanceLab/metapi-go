package store

import (
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// EnsureRuntimeDatabase creates the data directory and opens an initial
// database connection. P1 will implement the full logic.
func EnsureRuntimeDatabase(cfg *config.Config) error {
	slog.Info("bootstrap: ensuring runtime database", "type", cfg.DbType)
	// P1: create dataDir, open SQLite/MySQL/Postgres connection
	return nil
}
