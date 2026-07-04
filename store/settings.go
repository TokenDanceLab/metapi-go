package store

import (
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// LoadRuntimeSettings reads the settings table and applies runtime overrides
// to the config. P1 will implement full DB-backed settings.
func LoadRuntimeSettings(cfg *config.Config) error {
	slog.Info("settings: loading runtime overrides (stub)")
	// P1: read settings table → toSettingsMap → ApplyRuntimeSettings(cfg, map)
	//      also: set cfg.LogCleanupConfigured = HasExplicitLogCleanupSettings(map)
	//      also: auto-enable log cleanup if retention > 0 and not configured
	return nil
}
