package store

import (
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// SwitchDatabase switches the runtime database connection to a new dialect/DSN.
// On failure, it rolls back to the original config. P1 will implement.
func SwitchDatabase(cfg *config.Config, dialect, dsn string, ssl bool) error {
	slog.Info("switch: runtime database switch requested",
		"from", cfg.DbType,
		"to", dialect,
	)
	// P1: switch connection; on failure, rollback to cfg.DbType/cfg.DbUrl/cfg.DbSsl
	return nil
}
