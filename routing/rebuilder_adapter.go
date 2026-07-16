package routing

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// SQLRouteRebuilder adapts a DB-backed rebuild function to RouteRebuilder.
// The concrete recompose logic lives in service (avoids circular imports with
// service packages that already depend on routing matchers).
type SQLRouteRebuilder struct {
	DB      *sqlx.DB
	Rebuild func(ctx context.Context, db *sqlx.DB) error
}

// RebuildTokenRoutesFromAvailability implements RouteRebuilder.
func (r *SQLRouteRebuilder) RebuildTokenRoutesFromAvailability(ctx context.Context) error {
	if r == nil || r.Rebuild == nil {
		return nil
	}
	return r.Rebuild(ctx, r.DB)
}
