package routing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// ChannelSelectorDB defines the DB operations needed by the selector.
type ChannelSelectorDB interface {
	// Route operations
	LoadEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error)
	LoadRouteGroupSources(ctx context.Context, groupRouteIDs []int64) (map[int64][]int64, error)

	// Channel operations
	LoadRouteChannels(ctx context.Context, routeIDs []int64) ([]struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}, error)

	// OAuth route unit operations
	LoadOAuthRouteUnitSummaries(ctx context.Context, unitIDs []int64) (map[int64]OAuthRouteUnitSummary, error)
	LoadOAuthRouteUnitMembers(ctx context.Context, unitIDs []int64) (map[int64][]OAuthRouteUnitMemberCandidate, error)

	// Channel mutation
	UpdateChannelLastSelectedAt(ctx context.Context, channelID int64, lastSelectedAt string) error
	UpdateRouteUnitMemberLastSelectedAt(ctx context.Context, unitID, accountID int64, lastSelectedAt string) error

	// Route unit member routes
	FindRouteIDsByOAuthRouteUnitID(ctx context.Context, unitID int64) ([]int64, error)

	// Load credential-scoped channel IDs
	LoadCredentialScopedChannelIDs(ctx context.Context, channel store.RouteChannel, accountID int64) ([]int64, error)

	// Load channel by ID with joins
	LoadChannelWithAccount(ctx context.Context, channelID int64) (*struct {
		Channel store.RouteChannel
		Account store.Account
	}, error)

	LoadChannelWithAccountAndRoute(ctx context.Context, channelID int64) (*struct {
		Channel store.RouteChannel
		Account store.Account
		Route   store.TokenRoute
	}, error)

	// Batch updates
	UpdateChannelCooldownFields(ctx context.Context, channelIDs []int64, updates map[string]interface{}) error
	UpdateChannelSuccessFields(ctx context.Context, channelID int64, updates map[string]interface{}) error

	// Route unit member updates
	UpdateRouteUnitMemberCooldownFields(ctx context.Context, memberID int64, updates map[string]interface{}) error
	UpdateRouteUnitMemberSuccessFields(ctx context.Context, memberID int64, updates map[string]interface{}) error

	// Load member with account+unit
	LoadRouteUnitMemberWithAccount(ctx context.Context, unitID, accountID int64) (*struct {
		Member  store.OAuthRouteUnitMember
		Account store.Account
		Unit    store.OAuthRouteUnit
	}, error)

	// Find all routes
	FindAllEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error)

	// Credential scoping
	LoadChannelsByTokenID(ctx context.Context, tokenID int64) ([]store.RouteChannel, error)
	LoadChannelsByAccountIDWithoutToken(ctx context.Context, accountID int64) ([]store.RouteChannel, error)

	// Runtime health
	LoadRuntimeHealthChannelRows(ctx context.Context, channelIDs []int64) ([]struct {
		SiteID            int64
		SourceModel       *string
		RouteModelPattern string
	}, error)

	// Clear channel failure states
	ClearChannelFailureStates(ctx context.Context, channelIDs []int64) error
}

// ChannelSelector implements selectChannel, selectNextChannel, selectPreferredChannel.
type ChannelSelector struct {
	db                  ChannelSelectorDB
	cache               *RouteCache
	configuredMaxSec    int
	downstreamPolicy    DownstreamRoutingPolicy
	routingWeights      RoutingWeightsConfig
	pricingFn           func(siteID, accountID int64, modelName string) *float64
	fallbackUnitCost    float64
	channelLoadProvider ChannelLoadSnapshotProvider
}

// NewChannelSelector creates a new ChannelSelector.
func NewChannelSelector(
	db ChannelSelectorDB,
	cache *RouteCache,
	configuredMaxSec int,
	routingWeights RoutingWeightsConfig,
	pricingFn func(siteID, accountID int64, modelName string) *float64,
	fallbackUnitCost float64,
	channelLoadProvider ChannelLoadSnapshotProvider,
) *ChannelSelector {
	return &ChannelSelector{
		db:                  db,
		cache:               cache,
		configuredMaxSec:    configuredMaxSec,
		routingWeights:      routingWeights,
		pricingFn:           pricingFn,
		fallbackUnitCost:    fallbackUnitCost,
		channelLoadProvider: channelLoadProvider,
	}
}

// SelectChannel finds a matching route and selects the best channel.
func (s *ChannelSelector) SelectChannel(ctx context.Context, requestedModel string, policy DownstreamRoutingPolicy) (*SelectedChannel, error) {
	if !IsModelAllowedByDownstreamPolicy(requestedModel, policy) {
		return nil, nil
	}
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return nil, err
	}

	match, err := s.findRoute(ctx, requestedModel, policy)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, nil
	}
	return s.selectFromMatch(ctx, match, requestedModel, policy, nil, true)
}

// SelectNextChannel selects a channel excluding already-tried channels.
func (s *ChannelSelector) SelectNextChannel(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy DownstreamRoutingPolicy) (*SelectedChannel, error) {
	if !IsModelAllowedByDownstreamPolicy(requestedModel, policy) {
		return nil, nil
	}
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return nil, err
	}

	match, err := s.findRoute(ctx, requestedModel, policy)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, nil
	}
	return s.selectFromMatch(ctx, match, requestedModel, policy, excludeChannelIDs, true)
}

// SelectPreferredChannel selects a specific preferred channel if eligible.
func (s *ChannelSelector) SelectPreferredChannel(
	ctx context.Context,
	requestedModel string,
	preferredChannelID int64,
	policy DownstreamRoutingPolicy,
	excludeChannelIDs []int64,
) (*SelectedChannel, error) {
	if !IsModelAllowedByDownstreamPolicy(requestedModel, policy) || preferredChannelID <= 0 {
		return nil, nil
	}
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return nil, err
	}

	match, err := s.findRoute(ctx, requestedModel, policy)
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, nil
	}

	mappedModel := ResolveMappedModel(requestedModel, match.Route.ModelMapping)
	requestedByDisplayName := IsRouteDisplayNameMatch(requestedModel, match.Route.DisplayName)
	bypassSourceModelCheck := requestedByDisplayName
	strategy := NormalizeRouteRoutingStrategy(match.Route.RoutingStrategy)

	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()

	// Find available candidates
	var available []RouteChannelCandidate
	for _, c := range match.Channels {
		if len(s.getCandidateEligibilityReasons(c, requestedModel, bypassSourceModelCheck, excludeChannelIDs, nowISO, policy)) == 0 {
			available = append(available, c)
		}
	}

	// Find the preferred one
	var preferred *RouteChannelCandidate
	for i := range available {
		if available[i].Channel.ID == preferredChannelID {
			preferred = &available[i]
			break
		}
	}
	if preferred == nil {
		return nil, nil
	}

	// Check breaker
	runtimeModelResolver := mappedModel
	_ = runtimeModelResolver
	if requestedByDisplayName {
		sm := NormalizeChannelSourceModel(preferred.Channel.SourceModel)
		if sm != "" {
			runtimeModelResolver = sm
		}
	}
	breakerHealthy, _ := FilterSiteRuntimeBrokenCandidatesByModel([]RouteChannelCandidate{*preferred}, runtimeModelResolver)
	if len(breakerHealthy) == 0 {
		return nil, nil
	}

	// Check recent failure (skip for round_robin and stable_first)
	if strategy != StrategyRoundRobin && !IsOAuthRouteUnitCandidate(*preferred) {
		if IsChannelRecentlyFailed(&preferred.Channel.FailCount, preferred.Channel.LastFailAt, nowMs, s.configuredMaxSec) {
			return nil, nil
		}
	}

	recordSelection := strategy == StrategyRoundRobin || strategy == StrategyStableFirst
	rotationKey := BuildStableFirstRotationKey(match.Route.ID, requestedModel)
	return s.finalizeDispatch(ctx, &breakerHealthy[0], match, requestedModel, mappedModel, policy,
		recordSelection, rotationKey, rotationKey+":observe", false, excludeChannelIDs, nowISO, nowMs)
}

func (s *ChannelSelector) selectFromMatch(
	ctx context.Context,
	match *RouteMatch,
	requestedModel string,
	policy DownstreamRoutingPolicy,
	excludeChannelIDs []int64,
	recordSelection bool,
) (*SelectedChannel, error) {
	mappedModel := ResolveMappedModel(requestedModel, match.Route.ModelMapping)
	requestedByDisplayName := IsRouteDisplayNameMatch(requestedModel, match.Route.DisplayName)
	bypassSourceModelCheck := requestedByDisplayName
	strategy := NormalizeRouteRoutingStrategy(match.Route.RoutingStrategy)
	runtimeModelResolver := mappedModel
	_ = runtimeModelResolver
	if requestedByDisplayName {
		sm := NormalizeChannelSourceModel(nil) // Will be resolved per-candidate
		_ = sm
	}

	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()

	// Filter available
	var available []RouteChannelCandidate
	for _, c := range match.Channels {
		if len(s.getCandidateEligibilityReasons(c, requestedModel, bypassSourceModelCheck, excludeChannelIDs, nowISO, policy)) == 0 {
			available = append(available, c)
		}
	}

	if len(available) == 0 {
		return nil, nil
	}

	// Resolve runtime model per-candidate for display name matches
	resolveModel := func(c RouteChannelCandidate) string {
		if !requestedByDisplayName {
			return mappedModel
		}
		sm := NormalizeChannelSourceModel(c.Channel.SourceModel)
		if sm != "" {
			return sm
		}
		return mappedModel
	}

	if strategy == StrategyRoundRobin {
		// Priority-layer strict soft-filter demotion (#368), same walk as weighted (#358).
		// Per-layer soft filters use softFilterCandidatesStrict (no full-set pin). Without
		// the layer walk, a failed prio-0 channel + global FilterRecentlyFailedCandidates
		// fallback could pin RR onto the broken layer and starve healthy lower-priority
		// siblings. RR still only hard-excludes cooldownUntil after
		// RoundRobinFailureThreshold consecutive fails — soft demotion is the early avoid.
		selected := selectAcrossPriorityLayers(
			available,
			resolveModel,
			nowMs,
			s.configuredMaxSec,
			func(pool []RouteChannelCandidate) *RouteChannelCandidate {
				return SelectRoundRobinCandidate(pool)
			},
		)
		if selected == nil {
			return nil, nil
		}
		return s.finalizeDispatch(ctx, selected, match, requestedModel, mappedModel, policy,
			recordSelection, "", "", false, excludeChannelIDs, nowISO, nowMs)
	}

	if strategy == StrategyStableFirst {
		// Priority-layer strict soft-filter demotion (#368). Primary/observation pool
		// planning runs only on the chosen layer's soft-healthy set so a broken prio-0
		// site does not pin stable_first when a healthy prio-1 alternative exists.
		rotationKey := BuildStableFirstRotationKey(match.Route.ID, requestedModel)
		usedObservation := false
		selected := selectAcrossPriorityLayers(
			available,
			resolveModel,
			nowMs,
			s.configuredMaxSec,
			func(pool []RouteChannelCandidate) *RouteChannelCandidate {
				poolPlan := BuildStableFirstPoolPlan(pool, resolveModel)

				shouldUseObservation := len(poolPlan.ObservationCandidates) > 0 &&
					(len(poolPlan.PrimaryCandidates) == 0 ||
						(recordSelection && ShouldUseStableFirstObservationCandidate(rotationKey, poolPlan.ObservationCandidates)))

				var selectionPool []RouteChannelCandidate
				if shouldUseObservation {
					selectionPool = poolPlan.ObservationCandidates
				} else if len(poolPlan.PrimaryCandidates) > 0 {
					selectionPool = poolPlan.PrimaryCandidates
				} else {
					selectionPool = poolPlan.ObservationCandidates
				}
				if len(selectionPool) == 0 {
					return nil
				}

				picked := s.stableFirstSelect(selectionPool, resolveModel, policy,
					shouldUseObservation, rotationKey)
				if picked != nil {
					usedObservation = shouldUseObservation
				}
				return picked
			},
		)
		if selected == nil {
			return nil, nil
		}

		obsKey := rotationKey + ":observe"
		return s.finalizeDispatch(ctx, selected, match, requestedModel, mappedModel, policy,
			recordSelection, rotationKey, obsKey, usedObservation, excludeChannelIDs, nowISO, nowMs)
	}

	// Deterministic pluggable strategies (#115): least_busy / lowest_latency / lowest_cost.
	// Same eligibility + recent-failure filters as weighted; exclude lists remain upstream.
	if strategy == StrategyLeastBusy || strategy == StrategyLowestLatency || strategy == StrategyLowestCost {
		breakerHealthy, _ := GetBreakerFilteredCandidatesByModelResolver(available, resolveModel)
		filteredCandidates := FilterRecentlyFailedCandidates(breakerHealthy,
			func(c RouteChannelCandidate) (*int64, *string) { return &c.Channel.FailCount, c.Channel.LastFailAt },
			nowMs, s.configuredMaxSec)
		var selected *RouteChannelCandidate
		switch strategy {
		case StrategyLeastBusy:
			selected = SelectLeastBusyCandidate(filteredCandidates, s.channelLoadProvider)
		case StrategyLowestLatency:
			selected = SelectLowestLatencyCandidate(filteredCandidates, resolveModel)
		default:
			selected = SelectLowestCostCandidate(filteredCandidates, resolveModel, s.pricingFn, s.fallbackUnitCost)
		}
		if selected == nil {
			return nil, nil
		}
		return s.finalizeDispatch(ctx, selected, match, requestedModel, mappedModel, policy,
			recordSelection, "", "", false, excludeChannelIDs, nowISO, nowMs)
	}

	// Weighted: priority layers (#358 soft-filter demotion; shared walk with #368).
	selected := selectAcrossPriorityLayers(
		available,
		resolveModel,
		nowMs,
		s.configuredMaxSec,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			return s.weightedRandomSelect(pool, resolveModel, policy)
		},
	)
	if selected == nil {
		return nil, nil
	}
	return s.finalizeDispatch(ctx, selected, match, requestedModel, mappedModel, policy,
		recordSelection, "", "", false, excludeChannelIDs, nowISO, nowMs)
}

// selectAcrossPriorityLayers walks priority layers low→high for weighted (#358),
// round_robin and stable_first (#368). Per-layer soft filters use
// softFilterCandidatesStrict (no full-set pin). If a layer is entirely soft-empty,
// try the next priority. Only when ALL layers are soft-empty does the global
// FilterRecentlyFailed / breaker full-set fallback apply (starvation guard).
//
// Algorithm-specific pick happens via selectFromPool on each non-empty layer.
// Hard excludes (eligibility / excludeChannelIDs) remain upstream.
//
// Honest global fallback: when every layer is soft-empty we deliberately re-enter
// FilterRecentlyFailedCandidates (full-set pin when soft-empty) so the request still
// attempts something rather than returning nil. That pin is a last resort, not the
// happy path — priority demotion above is what prevents a broken prio-0 layer from
// starving healthy lower-priority siblings.
func selectAcrossPriorityLayers(
	available []RouteChannelCandidate,
	resolveModel func(RouteChannelCandidate) string,
	nowMs int64,
	configuredMaxSec int,
	selectFromPool func([]RouteChannelCandidate) *RouteChannelCandidate,
) *RouteChannelCandidate {
	if len(available) == 0 || selectFromPool == nil {
		return nil
	}

	layers := make(map[int64][]RouteChannelCandidate)
	for _, c := range available {
		layers[c.Channel.Priority] = append(layers[c.Channel.Priority], c)
	}

	priorities := make([]int64, 0, len(layers))
	for p := range layers {
		priorities = append(priorities, p)
	}
	// Sort ascending (lower number = higher priority)
	for i := 0; i < len(priorities); i++ {
		for j := i + 1; j < len(priorities); j++ {
			if priorities[j] < priorities[i] {
				priorities[i], priorities[j] = priorities[j], priorities[i]
			}
		}
	}

	for _, priority := range priorities {
		rawLayer := layers[priority]
		filteredLayer := softFilterCandidatesStrict(rawLayer, resolveModel, nowMs, configuredMaxSec)
		if len(filteredLayer) == 0 {
			continue
		}
		if selected := selectFromPool(filteredLayer); selected != nil {
			return selected
		}
	}

	// All priority layers soft-empty → honest global fallback with existing full-set
	// pin behavior (FilterRecentlyFailedCandidates returns the input when soft-empty).
	// Prefer this over returning nil when every candidate is in soft-failure state —
	// request must still attempt something; hard excludes already ran upstream.
	breakerHealthy, _ := GetBreakerFilteredCandidatesByModelResolver(available, resolveModel)
	filteredGlobal := FilterRecentlyFailedCandidates(breakerHealthy,
		func(c RouteChannelCandidate) (*int64, *string) { return &c.Channel.FailCount, c.Channel.LastFailAt },
		nowMs, configuredMaxSec)
	return selectFromPool(filteredGlobal)
}

// selectWeightedAcrossPriorityLayers is retained as a thin alias for tests and
// any external references to the #358 name.
func selectWeightedAcrossPriorityLayers(
	available []RouteChannelCandidate,
	resolveModel func(RouteChannelCandidate) string,
	nowMs int64,
	configuredMaxSec int,
	selectFromPool func([]RouteChannelCandidate) *RouteChannelCandidate,
) *RouteChannelCandidate {
	return selectAcrossPriorityLayers(available, resolveModel, nowMs, configuredMaxSec, selectFromPool)
}

// softFilterCandidatesStrict applies site/model breaker then recent-failure
// filters without full-set fallback. An empty result means every candidate is
// soft-unhealthy and the caller should try the next priority layer (or global
// fallback). Unlike FilterRecentlyFailedCandidates / FilterSiteRuntimeBroken*,
// a single soft-unhealthy candidate still yields empty so a lone broken
// priority-0 channel cannot block demotion to priority>=1.
func softFilterCandidatesStrict(
	candidates []RouteChannelCandidate,
	resolveModel func(RouteChannelCandidate) string,
	nowMs int64,
	configuredMaxSec int,
) []RouteChannelCandidate {
	if len(candidates) == 0 {
		return nil
	}
	healthy := make([]RouteChannelCandidate, 0, len(candidates))
	for _, c := range candidates {
		modelName := ""
		if resolveModel != nil {
			modelName = resolveModel(c)
		}
		details := GetSiteRuntimeHealthDetails(c.Site.ID, modelName)
		if details.GlobalBreakerOpen || details.ModelBreakerOpen {
			continue
		}
		if IsChannelRecentlyFailed(&c.Channel.FailCount, c.Channel.LastFailAt, nowMs, configuredMaxSec) {
			continue
		}
		healthy = append(healthy, c)
	}
	return healthy
}

func (s *ChannelSelector) findRoute(ctx context.Context, model string, policy DownstreamRoutingPolicy) (*RouteMatch, error) {
	routes, err := s.db.LoadEnabledRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("findRoute: load routes: %w", err)
	}

	// Apply allowedRouteIds filter
	matchedSupportedPattern := false
	for _, pattern := range policy.SupportedModels {
		if MatchesModelPattern(model, pattern) {
			matchedSupportedPattern = true
			break
		}
	}
	if len(policy.AllowedRouteIDs) > 0 && !matchedSupportedPattern {
		allowSet := make(map[int64]bool)
		for _, id := range policy.AllowedRouteIDs {
			allowSet[id] = true
		}
		filtered := make([]store.TokenRoute, 0, len(routes))
		for _, r := range routes {
			if allowSet[r.ID] {
				filtered = append(filtered, r)
			}
		}
		routes = filtered
	}

	// Match priority: 1) explicit_group displayName exact, 2) exact model pattern, 3) displayName exact, 4) wildcard
	var matchedRoute *store.TokenRoute
	for i := range routes {
		r := &routes[i]
		if IsExplicitGroupRoute(r.RouteMode) && IsRouteDisplayNameMatch(model, r.DisplayName) {
			matchedRoute = r
			break
		}
	}
	if matchedRoute == nil {
		for i := range routes {
			r := &routes[i]
			if !IsExplicitGroupRoute(r.RouteMode) && IsExactRouteModelPattern(r.ModelPattern) && strings.TrimSpace(r.ModelPattern) == model {
				matchedRoute = r
				break
			}
		}
	}
	if matchedRoute == nil {
		for i := range routes {
			r := &routes[i]
			if !IsExplicitGroupRoute(r.RouteMode) && IsRouteDisplayNameMatch(model, r.DisplayName) {
				matchedRoute = r
				break
			}
		}
	}
	if matchedRoute == nil {
		for i := range routes {
			r := &routes[i]
			if !IsExplicitGroupRoute(r.RouteMode) && MatchesModelPattern(model, r.ModelPattern) {
				matchedRoute = r
				break
			}
		}
	}

	if matchedRoute == nil {
		return nil, nil
	}

	return s.loadRouteMatch(ctx, *matchedRoute)
}

func (s *ChannelSelector) loadRouteMatch(ctx context.Context, route store.TokenRoute) (*RouteMatch, error) {
	// Check cache
	if cached := s.cache.GetMatch(route.ID); cached != nil {
		return cached, nil
	}

	// Load channels for this route
	routeIDs := []int64{route.ID}
	if IsExplicitGroupRoute(route.RouteMode) {
		// Load source route IDs
		sourceIDs, err := s.db.LoadRouteGroupSources(ctx, []int64{route.ID})
		if err != nil {
			return nil, err
		}
		if ids, ok := sourceIDs[route.ID]; ok {
			routeIDs = ids
		}
	}

	// Load channels
	joined, err := s.db.LoadRouteChannels(ctx, routeIDs)
	if err != nil {
		return nil, err
	}

	// Collect OAuth route unit IDs
	unitIDsMap := make(map[int64]bool)
	for _, j := range joined {
		if j.Channel.OAuthRouteUnitID != nil && *j.Channel.OAuthRouteUnitID > 0 {
			unitIDsMap[*j.Channel.OAuthRouteUnitID] = true
		}
	}
	var unitIDs []int64
	for id := range unitIDsMap {
		unitIDs = append(unitIDs, id)
	}

	var unitSummaries map[int64]OAuthRouteUnitSummary
	var unitMembers map[int64][]OAuthRouteUnitMemberCandidate
	if len(unitIDs) > 0 {
		unitSummaries, err = s.db.LoadOAuthRouteUnitSummaries(ctx, unitIDs)
		if err != nil {
			return nil, err
		}
		unitMembers, err = s.db.LoadOAuthRouteUnitMembers(ctx, unitIDs)
		if err != nil {
			return nil, err
		}
	}

	// Resolve source model fallback
	fallbackSourceModelByRouteID := make(map[int64]string)
	for _, rID := range routeIDs {
		// We'd need route details here; for simplicity, use model pattern
		// In a real implementation, load the source route's model pattern
		fallbackSourceModelByRouteID[rID] = "" // Will be resolved from the route
	}

	candidates := make([]RouteChannelCandidate, 0, len(joined))
	for _, j := range joined {
		sourceModel := j.Channel.SourceModel
		// Fallback source model resolution
		resolvedSourceModel := NormalizeChannelSourceModel(sourceModel)
		if resolvedSourceModel == "" {
			if fb, ok := fallbackSourceModelByRouteID[j.Channel.RouteID]; ok && fb != "" {
				resolvedSourceModel = fb
			}
		}

		candidate := RouteChannelCandidate{
			Channel: j.Channel,
			Account: j.Account,
			Site:    j.Site,
			Token:   j.Token,
		}

		if j.Channel.OAuthRouteUnitID != nil && *j.Channel.OAuthRouteUnitID > 0 {
			unitID := *j.Channel.OAuthRouteUnitID
			if summary, ok := unitSummaries[unitID]; ok {
				candidate.RouteUnit = &summary
			}
			if members, ok := unitMembers[unitID]; ok {
				candidate.RouteUnitMembers = members
			}
		}

		_ = resolvedSourceModel
		candidates = append(candidates, candidate)
	}

	match := &RouteMatch{
		Route:    route,
		Channels: candidates,
	}
	s.cache.SetMatch(route.ID, match)
	return match, nil
}

func (s *ChannelSelector) getCandidateEligibilityReasons(
	candidate RouteChannelCandidate,
	requestedModel string,
	bypassSourceModelCheck bool,
	excludeChannelIDs []int64,
	nowISO string,
	policy DownstreamRoutingPolicy,
) []string {
	var reasons []string

	if !bypassSourceModelCheck && !ChannelSupportsRequestedModel(candidate.Channel.SourceModel, requestedModel) {
		srcModel := ""
		if candidate.Channel.SourceModel != nil {
			srcModel = *candidate.Channel.SourceModel
		}
		reasons = append(reasons, "来源模型不匹配="+srcModel)
	}

	if !candidate.Channel.Enabled {
		reasons = append(reasons, "通道禁用")
	}

	// OAuth route unit: check member availability
	if IsOAuthRouteUnitCandidate(candidate) {
		eligibleMembers := getEligibleRouteUnitMembers(candidate, requestedModel, nowISO)
		if len(eligibleMembers) == 0 {
			name := "round_robin"
			if candidate.RouteUnit != nil {
				name = candidate.RouteUnit.Name
			}
			reasons = append(reasons, "路由池成员不可用（"+name+"）")
		}
		return reasons
	}

	// Account status
	if IsExplicitTokenChannel(candidate) {
		if candidate.Account.Status == "disabled" {
			reasons = append(reasons, "账号状态="+candidate.Account.Status)
		}
	} else if candidate.Account.Status != "active" {
		reasons = append(reasons, "账号状态="+candidate.Account.Status)
	}

	// Site status
	if candidate.Site.Status == "disabled" {
		reasons = append(reasons, "站点状态="+candidate.Site.Status)
	}

	// Downstream exclusion
	if reason := s.resolveDownstreamExclusionReason(candidate, policy); reason != "" {
		reasons = append(reasons, reason)
	}

	// Already tried
	for _, id := range excludeChannelIDs {
		if id == candidate.Channel.ID {
			reasons = append(reasons, "当前请求已尝试")
			break
		}
	}

	// Token available
	tokenValue := s.resolveChannelTokenValue(candidate)
	if tokenValue == "" {
		reasons = append(reasons, "令牌不可用")
	}

	// Cooldown — parse timestamps (millis-aware); do not lex-compare ISO strings.
	if IsCooldownActive(candidate.Channel.CooldownUntil, nowISO) {
		reasons = append(reasons, "冷却中")
	}

	return reasons
}

func (s *ChannelSelector) resolveChannelTokenValue(candidate RouteChannelCandidate) string {
	if candidate.Channel.TokenID != nil && *candidate.Channel.TokenID > 0 {
		if candidate.Token == nil {
			return ""
		}
		// Check usable
		if candidate.Token.Token == "" || !candidate.Token.Enabled {
			return ""
		}
		return candidate.Token.Token
	}

	// OAuth account: use accessToken
	if candidate.Account.OAuthProvider != nil && *candidate.Account.OAuthProvider != "" {
		if candidate.Account.AccessToken != "" {
			return candidate.Account.AccessToken
		}
		return ""
	}

	// Fallback: apiToken
	if candidate.Account.APIToken != nil && *candidate.Account.APIToken != "" {
		return *candidate.Account.APIToken
	}

	return ""
}

func (s *ChannelSelector) resolveDownstreamExclusionReason(candidate RouteChannelCandidate, policy DownstreamRoutingPolicy) string {
	for _, siteID := range policy.ExcludedSiteIDs {
		if siteID == candidate.Site.ID {
			return "站点已被下游密钥排除"
		}
	}

	if len(policy.ExcludedCredentialRefs) == 0 {
		return ""
	}

	for _, ref := range policy.ExcludedCredentialRefs {
		if ref.Kind == "account_token" {
			if candidate.Channel.TokenID != nil && *candidate.Channel.TokenID == ref.TokenID &&
				candidate.Token != nil && candidate.Token.ID == ref.TokenID &&
				candidate.Account.ID == ref.AccountID &&
				candidate.Site.ID == ref.SiteID {
				return "API Key/令牌已被下游密钥排除"
			}
			continue
		}

		if candidate.Channel.TokenID == nil &&
			candidate.Account.ID == ref.AccountID &&
			candidate.Site.ID == ref.SiteID {
			tokenValue := s.resolveChannelTokenValue(candidate)
			apiToken := ""
			if candidate.Account.APIToken != nil {
				apiToken = *candidate.Account.APIToken
			}
			if tokenValue != "" && apiToken != "" && tokenValue == apiToken {
				return "API Key/令牌已被下游密钥排除"
			}
		}
	}
	return ""
}

func (s *ChannelSelector) weightedRandomSelect(
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
	policy DownstreamRoutingPolicy,
) *RouteChannelCandidate {
	result := CalculateWeightedSelection(
		candidates,
		modelResolver,
		s.routingWeights,
		policy.SiteWeightMultipliers,
		s.channelLoadProvider,
		time.Now().UnixMilli(),
		WeightedMode,
		"",
		s.pricingFn,
		s.fallbackUnitCost,
	)
	return result.Selected
}

func (s *ChannelSelector) stableFirstSelect(
	candidates []RouteChannelCandidate,
	modelResolver func(RouteChannelCandidate) string,
	policy DownstreamRoutingPolicy,
	shouldUseObservation bool,
	rotationKey string,
) *RouteChannelCandidate {
	key := rotationKey
	if shouldUseObservation {
		key = rotationKey + ":observe"
	}
	result := CalculateWeightedSelection(
		candidates,
		modelResolver,
		s.routingWeights,
		policy.SiteWeightMultipliers,
		s.channelLoadProvider,
		time.Now().UnixMilli(),
		StableFirstMode,
		key,
		s.pricingFn,
		s.fallbackUnitCost,
	)
	return result.Selected
}

func (s *ChannelSelector) finalizeDispatch(
	ctx context.Context,
	selected *RouteChannelCandidate,
	match *RouteMatch,
	requestedModel string,
	mappedModel string,
	policy DownstreamRoutingPolicy,
	recordSelection bool,
	stableFirstRotationKey string,
	stableFirstObservationKey string,
	usedObservation bool,
	excludeChannelIDs []int64,
	nowISO string,
	nowMs int64,
) (*SelectedChannel, error) {
	dispatchCandidate := *selected
	var resolvedRouteUnitMemberTokenValue string

	if IsOAuthRouteUnitCandidate(*selected) {
		member := SelectRouteUnitMember(*selected, requestedModel, nowISO, nowMs, s.configuredMaxSec, excludeChannelIDs)
		if member == nil || selected.RouteUnit == nil {
			return nil, nil
		}
		resolvedRouteUnitMemberTokenValue = ResolveRouteUnitMemberTokenValue(member.Account)
		dispatchCandidate.Account = member.Account
		dispatchCandidate.Site = member.Site
		dispatchCandidate.Token = nil

		if recordSelection {
			if err := s.db.UpdateRouteUnitMemberLastSelectedAt(ctx, selected.RouteUnit.ID, member.Account.ID, nowISO); err != nil {
				return nil, err
			}
			// Invalidate caches for routes using this unit
			routeIDs, _ := s.db.FindRouteIDsByOAuthRouteUnitID(ctx, selected.RouteUnit.ID)
			for _, rid := range routeIDs {
				s.cache.InvalidateRouteScopedCache(rid)
			}
		}
	}

	tokenValue := resolvedRouteUnitMemberTokenValue
	if tokenValue == "" {
		tokenValue = s.resolveChannelTokenValue(dispatchCandidate)
	}
	if tokenValue == "" {
		return nil, nil
	}

	if recordSelection {
		if stableFirstRotationKey != "" && stableFirstObservationKey != "" {
			targetKey := stableFirstRotationKey
			if usedObservation {
				targetKey = stableFirstObservationKey
			}
			rememberStableFirstSiteSelectionForKey(targetKey, dispatchCandidate.Site.ID)
			UpdateStableFirstObservationProgress(stableFirstRotationKey, usedObservation, dispatchCandidate.Site.ID)
		}
		if err := s.db.UpdateChannelLastSelectedAt(ctx, selected.Channel.ID, nowISO); err != nil {
			return nil, err
		}
		s.cache.PatchCachedChannel(selected.Channel.ID, func(ch *store.RouteChannel) {
			ch.LastSelectedAt = &nowISO
		})
	}

	actualModel := ResolveActualModelForSelectedChannel(requestedModel, match.Route.DisplayName, mappedModel, selected.Channel.SourceModel)

	tokenName := "default"
	if dispatchCandidate.Token != nil {
		tokenName = dispatchCandidate.Token.Name
	}

	return &SelectedChannel{
		Channel:       selected.Channel,
		Account:       dispatchCandidate.Account,
		Site:          dispatchCandidate.Site,
		Token:         dispatchCandidate.Token,
		TokenValue:    tokenValue,
		TokenName:     tokenName,
		ActualModel:   actualModel,
		ContextLength: match.Route.ContextLength,
	}, nil
}
