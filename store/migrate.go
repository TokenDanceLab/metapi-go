package store

import (
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// Migrate runs all compatibility schema migrations in order.
// Each migration is idempotent (IF NOT EXISTS semantics). P1 will implement.
func Migrate(cfg *config.Config) error {
	slog.Info("migration: database ready")
	// P1: run the 11 schema migrations in order:
	//   EnsureSiteCompatibilityColumns
	//   EnsureRouteGroupingCompatibilityColumns
	//   EnsureProxyFileCompatibilityColumns
	//   EnsureProxyLogStreamTimingColumns
	//   EnsureProxyLogClientColumns
	//   EnsureProxyLogDownstreamApiKeyIdColumn
	//   EnsureProxyLogBillingDetailsColumn
	//   RepairStoredCreatedAtValues
	//   MigrateSiteApiKeysToAccounts
	//   EnsureDefaultSitesSeeded
	//   EnsureOauthIdentityBackfill
	return nil
}
