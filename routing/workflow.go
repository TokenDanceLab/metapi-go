package routing

import (
	"context"
	"fmt"
)

// RouteRefreshWorkflow implements rebuildRoutes and refreshModels operations.
type RouteRefreshWorkflow struct {
	modelProvider    ModelProvider
	routeRebuilder   RouteRebuilder
}

// NewRouteRefreshWorkflow creates a new RouteRefreshWorkflow.
func NewRouteRefreshWorkflow(modelProvider ModelProvider, routeRebuilder RouteRebuilder) *RouteRefreshWorkflow {
	return &RouteRefreshWorkflow{
		modelProvider:  modelProvider,
		routeRebuilder: routeRebuilder,
	}
}

// RebuildRoutesOnly rebuilds token routes from availability data.
func (w *RouteRefreshWorkflow) RebuildRoutesOnly(ctx context.Context) error {
	if w.routeRebuilder == nil {
		return fmt.Errorf("route rebuilder not configured")
	}
	return w.routeRebuilder.RebuildTokenRoutesFromAvailability(ctx)
}

// RebuildRoutesBestEffort attempts to rebuild routes, returning success/failure.
func (w *RouteRefreshWorkflow) RebuildRoutesBestEffort(ctx context.Context) bool {
	err := w.RebuildRoutesOnly(ctx)
	return err == nil
}

// RefreshModelsAndRebuildRoutes refreshes model data and rebuilds routes.
func (w *RouteRefreshWorkflow) RefreshModelsAndRebuildRoutes(ctx context.Context) error {
	if w.modelProvider == nil {
		return fmt.Errorf("model provider not configured")
	}
	// The actual implementation would iterate over all accounts and refresh.
	// This is a stub that delegates to rebuild.
	if w.routeRebuilder != nil {
		return w.routeRebuilder.RebuildTokenRoutesFromAvailability(ctx)
	}
	return nil
}
