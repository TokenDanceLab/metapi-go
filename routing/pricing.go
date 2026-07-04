package routing

import (
	"context"
	"fmt"
)

// PricingReference provides model pricing cost resolution.
type PricingReference struct {
	provider PricingProvider
}

// NewPricingReference creates a new PricingReference.
func NewPricingReference(provider PricingProvider) *PricingReference {
	return &PricingReference{provider: provider}
}

// GetCachedModelRoutingReferenceCost returns the cached routing reference cost.
func (p *PricingReference) GetCachedModelRoutingReferenceCost(ctx context.Context, siteID, accountID int64, modelName string) *float64 {
	if p.provider == nil {
		return nil
	}
	cost, err := p.provider.GetReferenceCost(ctx, modelName, siteID, accountID)
	if err != nil || cost <= 0 {
		return nil
	}
	return &cost
}

// RefreshPricingReferenceCostsForMatch asynchronously refreshes pricing for all candidates.
func (p *PricingReference) RefreshPricingReferenceCostsForMatch(
	ctx context.Context,
	match *RouteMatch,
	requestedModel string,
	options PricingReferenceRefreshOptions,
) error {
	if match == nil || p.provider == nil {
		return nil
	}

	requestedByDisplayName := IsRouteDisplayNameMatch(requestedModel, match.Route.DisplayName)
	useChannelSourceModelForCost := options.UseChannelSourceModelForCost || requestedByDisplayName
	mappedModel := ResolveMappedModel(requestedModel, match.Route.ModelMapping)

	refreshedKeys := options.RefreshedKeys
	if refreshedKeys == nil {
		m := make(map[string]struct{})
		refreshedKeys = &m
	}

	for _, candidate := range match.Channels {
		refreshKey := fmt.Sprintf("%d:%d", candidate.Site.ID, candidate.Account.ID)
		if _, ok := (*refreshedKeys)[refreshKey]; ok {
			continue
		}
		(*refreshedKeys)[refreshKey] = struct{}{}

		modelName := mappedModel
		if useChannelSourceModelForCost {
			srcModel := NormalizeChannelSourceModel(candidate.Channel.SourceModel)
			if srcModel != "" {
				modelName = srcModel
			}
		}
		if modelName == "" {
			continue
		}

		go func() {
			_ = p.provider.RefreshModelPricingCatalog(ctx, candidate.Site, candidate.Account, modelName)
		}()
	}

	return nil
}
