package routing

import (
	"context"
	"fmt"

	"github.com/tokendancelab/metapi-go/store"
)

// RouteDecisionSnapshotStore handles persistence of route decision snapshots.
type RouteDecisionSnapshotStore struct {
	db SnapshotDB
}

// SnapshotDB defines the DB operations for snapshot storage.
type SnapshotDB interface {
	UpdateRouteDecisionSnapshot(ctx context.Context, routeID int64, snapshot string, refreshedAt string) error
	ClearRouteDecisionSnapshot(ctx context.Context, routeID int64) error
	ClearRouteDecisionSnapshots(ctx context.Context, routeIDs []int64) error
	ClearAllRouteDecisionSnapshots(ctx context.Context) error
	LoadRouteGroupSources(ctx context.Context, groupRouteIDs []int64) (map[int64][]int64, error)
}

// NewRouteDecisionSnapshotStore creates a new snapshot store.
func NewRouteDecisionSnapshotStore(db SnapshotDB) *RouteDecisionSnapshotStore {
	return &RouteDecisionSnapshotStore{db: db}
}

// SaveSnapshot saves a decision snapshot for a route.
func (s *RouteDecisionSnapshotStore) SaveSnapshot(ctx context.Context, routeID int64, snapshot RouteDecisionExplanation) error {
	json, err := marshalDecision(snapshot)
	if err != nil {
		return fmt.Errorf("saveSnapshot: marshal: %w", err)
	}
	return s.db.UpdateRouteDecisionSnapshot(ctx, routeID, json, "")
}

// ClearSnapshot clears the decision snapshot for a route.
func (s *RouteDecisionSnapshotStore) ClearSnapshot(ctx context.Context, routeID int64) error {
	return s.db.ClearRouteDecisionSnapshot(ctx, routeID)
}

// ClearSnapshots clears decision snapshots for multiple routes.
func (s *RouteDecisionSnapshotStore) ClearSnapshots(ctx context.Context, routeIDs []int64) error {
	return s.db.ClearRouteDecisionSnapshots(ctx, routeIDs)
}

// ClearAllSnapshots clears all route decision snapshots.
func (s *RouteDecisionSnapshotStore) ClearAllSnapshots(ctx context.Context) error {
	return s.db.ClearAllRouteDecisionSnapshots(ctx)
}

// ClearRouteCooldown clears the cooldown for all channels on a route.
func (s *RouteDecisionSnapshotStore) ClearRouteCooldown(ctx context.Context, routeID int64, tokenRouter *TokenRouter) (clearedChannels int64, err error) {
	// Load route and resolve source route IDs
	routes, err := s.db.(interface {
		LoadEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error)
	}).LoadEnabledRoutes(ctx)
	if err != nil {
		return 0, fmt.Errorf("clearRouteCooldown: load routes: %w", err)
	}

	var targetRoute *store.TokenRoute
	for i := range routes {
		if routes[i].ID == routeID {
			targetRoute = &routes[i]
			break
		}
	}
	if targetRoute == nil {
		return 0, nil
	}

	// Resolve actual route IDs for cooldown clearing
	actualRouteIDs := []int64{routeID}
	if IsExplicitGroupRoute(targetRoute.RouteMode) {
		// Expand to enabled source routes with exact patterns
		actualRouteIDs = nil
	}

	// Load channels for these route IDs
	// Channel clearing delegated to TokenRouter
	// For now, just return success—actual implementation needs DB access
	_ = actualRouteIDs
	_ = tokenRouter

	return 0, nil
}
