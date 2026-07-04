package routing

import (
	"context"
	"fmt"
	"time"
)

// RouteDecisionService handles route decision snapshots.
type RouteDecisionService struct {
	router *TokenRouter
	db     DecisionDB
}

// DecisionDB defines the DB operations for decision snapshots.
type DecisionDB interface {
	FindAllEnabledRoutes(ctx context.Context) ([]struct {
		ID           int64
		ModelPattern string
	}, error)
	UpdateRouteDecisionSnapshot(ctx context.Context, routeID int64, snapshot string, refreshedAt string) error
	ClearRouteDecisionSnapshot(ctx context.Context, routeID int64) error
	ClearRouteDecisionSnapshots(ctx context.Context, routeIDs []int64) error
	ClearAllRouteDecisionSnapshots(ctx context.Context) error
}

// NewRouteDecisionService creates a new RouteDecisionService.
func NewRouteDecisionService(router *TokenRouter, db DecisionDB) *RouteDecisionService {
	return &RouteDecisionService{
		router: router,
		db:     db,
	}
}

// RefreshAllRouteDecisionSnapshots refreshes all route decision snapshots.
func (s *RouteDecisionService) RefreshAllRouteDecisionSnapshots(ctx context.Context, refreshPricingCatalog bool) (exactModelCount int, wildcardRouteCount int, err error) {
	routes, err := s.db.FindAllEnabledRoutes(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("refreshAllRouteDecisionSnapshots: %w", err)
	}

	var exactModels []string
	var wildcardRouteIDs []int64

	for _, route := range routes {
		if IsExactRouteModelPattern(route.ModelPattern) {
			exactModels = append(exactModels, route.ModelPattern)
		} else {
			wildcardRouteIDs = append(wildcardRouteIDs, route.ID)
		}
	}

	_ = refreshPricingCatalog

	for _, model := range exactModels {
		// Find matching exact routes
		for _, route := range routes {
			if IsExactRouteModelPattern(route.ModelPattern) && MatchesModelPattern(model, route.ModelPattern) {
				decision, err := s.router.ExplainSelectionForRoute(ctx, route.ID, model, nil, EmptyDownstreamRoutingPolicy)
				if err != nil {
					continue
				}
				_ = s.saveRouteDecisionSnapshot(ctx, route.ID, decision)
			}
		}
	}

	for _, routeID := range wildcardRouteIDs {
		decision, err := s.router.ExplainSelectionRouteWide(ctx, routeID, EmptyDownstreamRoutingPolicy)
		if err != nil {
			continue
		}
		_ = s.saveRouteDecisionSnapshot(ctx, routeID, decision)
	}

	return len(exactModels), len(wildcardRouteIDs), nil
}

func (s *RouteDecisionService) saveRouteDecisionSnapshot(ctx context.Context, routeID int64, decision RouteDecisionExplanation) error {
	json, err := marshalDecision(decision)
	if err != nil {
		return err
	}
	refreshedAt := time.Now().UTC().Format(time.RFC3339)
	return s.db.UpdateRouteDecisionSnapshot(ctx, routeID, json, refreshedAt)
}

// marshalDecision JSON-encodes a RouteDecisionExplanation.
func marshalDecision(d RouteDecisionExplanation) (string, error) {
	// Use a simple JSON builder
	b := []byte{'{'}
	b = append(b, `"requestedModel":"`...)
	b = append(b, escapeJSON(d.RequestedModel)...)
	b = append(b, `","actualModel":"`...)
	b = append(b, escapeJSON(d.ActualModel)...)
	b = append(b, `","matched":`...)
	if d.Matched {
		b = append(b, "true"...)
	} else {
		b = append(b, "false"...)
	}
	if d.RouteID != nil {
		b = append(b, `,"routeId":`...)
		b = append(b, fmtInt(*d.RouteID)...)
	}
	if d.ModelPattern != "" {
		b = append(b, `,"modelPattern":"`...)
		b = append(b, escapeJSON(d.ModelPattern)...)
	}
	if d.SelectedChannelID != nil {
		b = append(b, `,"selectedChannelId":`...)
		b = append(b, fmtInt(*d.SelectedChannelID)...)
	}
	if d.SelectedAccountID != nil {
		b = append(b, `,"selectedAccountId":`...)
		b = append(b, fmtInt(*d.SelectedAccountID)...)
	}
	if d.SelectedLabel != "" {
		b = append(b, `,"selectedLabel":"`...)
		b = append(b, escapeJSON(d.SelectedLabel)...)
	}
	b = append(b, `,"summary":[`...)
	for i, s := range d.Summary {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, escapeJSON(s)...)
		b = append(b, '"')
	}
	b = append(b, `],"candidates":[`...)
	for i, c := range d.Candidates {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '{')
		b = append(b, `"channelId":`...)
		b = append(b, fmtInt(c.ChannelID)...)
		b = append(b, `,"accountId":`...)
		b = append(b, fmtInt(c.AccountID)...)
		b = append(b, `,"username":"`...)
		b = append(b, escapeJSON(c.Username)...)
		b = append(b, `","siteName":"`...)
		b = append(b, escapeJSON(c.SiteName)...)
		b = append(b, `","tokenName":"`...)
		b = append(b, escapeJSON(c.TokenName)...)
		b = append(b, `","priority":`...)
		b = append(b, fmtInt(c.Priority)...)
		b = append(b, `,"weight":`...)
		b = append(b, fmtInt(c.Weight)...)
		b = append(b, `,"eligible":`...)
		if c.Eligible {
			b = append(b, "true"...)
		} else {
			b = append(b, "false"...)
		}
		b = append(b, `,"recentlyFailed":`...)
		if c.RecentlyFailed {
			b = append(b, "true"...)
		} else {
			b = append(b, "false"...)
		}
		b = append(b, `,"avoidedByRecentFailure":`...)
		if c.AvoidedByRecentFailure {
			b = append(b, "true"...)
		} else {
			b = append(b, "false"...)
		}
		b = append(b, `,"probability":`...)
		b = append(b, fmtFloatValue(c.Probability)...)
		b = append(b, `,"reason":"`...)
		b = append(b, escapeJSON(c.Reason)...)
		b = append(b, '"')
		b = append(b, '}')
	}
	b = append(b, ']')
	b = append(b, '}')
	return string(b), nil
}

func fmtFloatValue(v float64) string {
	// Simple float formatting
	return fmtFloat(v)
}

func escapeJSON(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			result = append(result, '\\', '"')
		case '\\':
			result = append(result, '\\', '\\')
		case '\n':
			result = append(result, '\\', 'n')
		case '\r':
			result = append(result, '\\', 'r')
		case '\t':
			result = append(result, '\\', 't')
		default:
			result = append(result, c)
		}
	}
	return string(result)
}
