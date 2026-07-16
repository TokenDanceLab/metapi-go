package routing

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// TokenRouter is the main routing engine. It composes all sub-modules.
type TokenRouter struct {
	db                  ChannelSelectorDB
	cache               *RouteCache
	selector            *ChannelSelector
	cfg                 *config.Config
	routingWeights      RoutingWeightsConfig
	configuredMaxSec    int
	fallbackUnitCost    float64
	pricingFn           func(siteID, accountID int64, modelName string) *float64
	channelLoadProvider ChannelLoadSnapshotProvider
}

// NewTokenRouter creates a new TokenRouter.
func NewTokenRouter(
	db ChannelSelectorDB,
	cfg *config.Config,
	pricingFn func(siteID, accountID int64, modelName string) *float64,
	channelLoadProvider ChannelLoadSnapshotProvider,
) *TokenRouter {
	cacheTTLMs := int64(cfg.TokenRouterCacheTtlMs)
	cache := NewRouteCache(cacheTTLMs)
	SetGlobalCache(cache)

	configuredMaxSec := cfg.TokenRouterFailureCooldownMaxSec
	if configuredMaxSec <= 0 {
		configuredMaxSec = TokenRouterFailureCooldownMaxSecCeiling
	}

	fallbackUnitCost := cfg.RoutingFallbackUnitCost
	if fallbackUnitCost <= 0 {
		fallbackUnitCost = 1
	}

	routingWeights := RoutingWeightsConfig{
		BaseWeightFactor: cfg.RoutingWeights.BaseWeightFactor,
		ValueScoreFactor: cfg.RoutingWeights.ValueScoreFactor,
		CostWeight:       cfg.RoutingWeights.CostWeight,
		BalanceWeight:    cfg.RoutingWeights.BalanceWeight,
		UsageWeight:      cfg.RoutingWeights.UsageWeight,
	}

	selector := NewChannelSelector(db, cache, configuredMaxSec, routingWeights, pricingFn, fallbackUnitCost, channelLoadProvider)

	return &TokenRouter{
		db:                  db,
		cache:               cache,
		selector:            selector,
		cfg:                 cfg,
		routingWeights:      routingWeights,
		configuredMaxSec:    configuredMaxSec,
		fallbackUnitCost:    fallbackUnitCost,
		pricingFn:           pricingFn,
		channelLoadProvider: channelLoadProvider,
	}
}

// SelectChannel finds a matching route and selects a channel.
func (tr *TokenRouter) SelectChannel(ctx context.Context, requestedModel string, policy DownstreamRoutingPolicy) (*SelectedChannel, error) {
	return tr.selector.SelectChannel(ctx, requestedModel, policy)
}

// SelectNextChannel selects the next channel excluding already-tried ones.
func (tr *TokenRouter) SelectNextChannel(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy DownstreamRoutingPolicy) (*SelectedChannel, error) {
	return tr.selector.SelectNextChannel(ctx, requestedModel, excludeChannelIDs, policy)
}

// SelectPreferredChannel selects a specific preferred channel.
func (tr *TokenRouter) SelectPreferredChannel(ctx context.Context, requestedModel string, preferredChannelID int64, policy DownstreamRoutingPolicy, excludeChannelIDs []int64) (*SelectedChannel, error) {
	return tr.selector.SelectPreferredChannel(ctx, requestedModel, preferredChannelID, policy, excludeChannelIDs)
}

// GetAvailableModels returns all exposed model names from enabled routes.
func (tr *TokenRouter) GetAvailableModels(ctx context.Context) ([]string, error) {
	routes, err := tr.db.FindAllEnabledRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("getAvailableModels: %w", err)
	}

	exposed := buildVisibleEnabledRoutes(routes)
	names := make(map[string]bool)
	for _, route := range exposed {
		name := GetExposedModelNameForRoute(route.DisplayName, route.ModelPattern)
		if name != "" {
			names[name] = true
		}
	}
	var result []string
	for name := range names {
		result = append(result, name)
	}
	return result, nil
}

// buildVisibleEnabledRoutes filters out routes covered by explicit_group or wildcard display names.
func buildVisibleEnabledRoutes(routes []store.TokenRoute) []store.TokenRoute {
	exactModelNames := make(map[string]bool)
	for _, r := range routes {
		if !IsExplicitGroupRoute(r.RouteMode) && IsExactRouteModelPattern(r.ModelPattern) {
			name := stringsTrimSpace(r.ModelPattern)
			if name != "" {
				exactModelNames[name] = true
			}
		}
	}

	type coveringRoute struct {
		route store.TokenRoute
	}
	var coveringRoutes []coveringRoute
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		if IsExplicitGroupRoute(r.RouteMode) {
			dn := NormalizeRouteDisplayName(r.DisplayName)
			if dn != "" {
				// We need sourceRouteIds — for now just mark as covering
				coveringRoutes = append(coveringRoutes, coveringRoute{route: r})
			}
			continue
		}
		if !IsExactRouteModelPattern(r.ModelPattern) && HasCustomDisplayName(r.ModelPattern, r.DisplayName) {
			coveringRoutes = append(coveringRoutes, coveringRoute{route: r})
		}
	}

	if len(coveringRoutes) == 0 {
		return routes
	}

	var result []store.TokenRoute
	for _, r := range routes {
		if IsExplicitGroupRoute(r.RouteMode) {
			dn := NormalizeRouteDisplayName(r.DisplayName)
			if dn != "" {
				result = append(result, r)
			}
			continue
		}

		if !IsExactRouteModelPattern(r.ModelPattern) {
			result = append(result, r)
			continue
		}
		if HasCustomDisplayName(r.ModelPattern, r.DisplayName) {
			result = append(result, r)
			continue
		}

		exactModel := stringsTrimSpace(r.ModelPattern)
		if exactModel == "" {
			result = append(result, r)
			continue
		}

		// Check if covered by any covering route
		covered := false
		for _, cr := range coveringRoutes {
			if cr.route.ID == r.ID {
				continue
			}
			groupDisplayName := NormalizeRouteDisplayName(cr.route.DisplayName)
			if groupDisplayName == "" || exactModelNames[groupDisplayName] {
				continue
			}
			if IsExplicitGroupRoute(cr.route.RouteMode) {
				// Check sourceRouteIds — we don't have them loaded here
				// Simplified: skip group route checks without full source info
				_ = cr
				continue
			}
			if MatchesModelPattern(exactModel, cr.route.ModelPattern) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, r)
		}
	}
	return result
}

// ExplainSelection returns a decision explanation for the requested model.
func (tr *TokenRouter) ExplainSelection(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy DownstreamRoutingPolicy) (RouteDecisionExplanation, error) {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return RouteDecisionExplanation{}, err
	}
	match, err := tr.selector.findRoute(ctx, requestedModel, policy)
	return explainSelectionFromMatch(match, requestedModel, excludeChannelIDs, policy, tr.configuredMaxSec), err
}

// ExplainSelectionForRoute returns a decision explanation for a specific route.
func (tr *TokenRouter) ExplainSelectionForRoute(ctx context.Context, routeID int64, requestedModel string, excludeChannelIDs []int64, policy DownstreamRoutingPolicy) (RouteDecisionExplanation, error) {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return RouteDecisionExplanation{}, err
	}

	match, err := tr.findRouteByID(ctx, routeID, policy)
	if err != nil || match == nil {
		if err != nil {
			return RouteDecisionExplanation{}, err
		}
		return RouteDecisionExplanation{RequestedModel: requestedModel, ActualModel: requestedModel, Matched: false, Summary: []string{"未匹配到路由"}}, nil
	}
	return explainSelectionFromMatch(match, requestedModel, excludeChannelIDs, policy, tr.configuredMaxSec), nil
}

// ExplainSelectionRouteWide returns a decision explanation for a route-wide view.
func (tr *TokenRouter) ExplainSelectionRouteWide(ctx context.Context, routeID int64, policy DownstreamRoutingPolicy) (RouteDecisionExplanation, error) {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return RouteDecisionExplanation{}, err
	}

	match, err := tr.findRouteByID(ctx, routeID, policy)
	if err != nil || match == nil {
		if err != nil {
			return RouteDecisionExplanation{}, err
		}
		return RouteDecisionExplanation{RequestedModel: fmt.Sprintf("route:%d", routeID), ActualModel: fmt.Sprintf("route:%d", routeID), Matched: false, Summary: []string{"未匹配到路由"}}, nil
	}

	fallbackRequestedModel := match.Route.ModelPattern
	if fallbackRequestedModel == "" {
		fallbackRequestedModel = fmt.Sprintf("route:%d", routeID)
	}
	return explainSelectionFromMatch(match, fallbackRequestedModel, nil, policy, tr.configuredMaxSec), nil
}

func (tr *TokenRouter) findRouteByID(ctx context.Context, routeID int64, policy DownstreamRoutingPolicy) (*RouteMatch, error) {
	if len(policy.AllowedRouteIDs) > 0 {
		found := false
		for _, id := range policy.AllowedRouteIDs {
			if id == routeID {
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
	}

	routes, err := tr.db.LoadEnabledRoutes(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range routes {
		if r.ID == routeID {
			return tr.selector.loadRouteMatch(ctx, r)
		}
	}
	return nil, nil
}

func explainSelectionFromMatch(match *RouteMatch, requestedModel string, excludeChannelIDs []int64, policy DownstreamRoutingPolicy, configuredMaxSec int) RouteDecisionExplanation {
	if match == nil {
		return RouteDecisionExplanation{
			RequestedModel: requestedModel,
			ActualModel:    requestedModel,
			Matched:        false,
			Summary:        []string{"未匹配到启用的路由"},
			Candidates:     nil,
		}
	}

	requestedByDisplayName := IsRouteDisplayNameMatch(requestedModel, match.Route.DisplayName)
	bypassSourceModelCheck := requestedByDisplayName
	mappedModel := ResolveMappedModel(requestedModel, match.Route.ModelMapping)
	strategy := NormalizeRouteRoutingStrategy(match.Route.RoutingStrategy)

	runtimeModelResolver := mappedModel
	if requestedByDisplayName {
		// Will resolve per-candidate
	}

	nowISO := time.Now().UTC().Format(time.RFC3339)
	nowMs := time.Now().UnixMilli()
	summary := []string{
		"命中路由：" + match.Route.ModelPattern,
	}
	switch strategy {
	case StrategyRoundRobin:
		summary = append(summary, "路由策略：轮询")
	case StrategyStableFirst:
		summary = append(summary, "路由策略：稳定优先")
	default:
		summary = append(summary, "路由策略：按权重随机")
	}
	if requestedByDisplayName {
		summary = append(summary, "按显示名命中："+NormalizeRouteDisplayName(match.Route.DisplayName))
		summary = append(summary, "显示名仅用于聚合展示，实际转发模型按选中通道来源模型决定")
	}

	var candidates []RouteDecisionCandidate
	var available []RouteChannelCandidate

	for _, row := range match.Channels {
		reasons := getCandidateEligibilityReasonsExplain(row, requestedModel, bypassSourceModelCheck, excludeChannelIDs, nowISO, policy)
		eligible := len(reasons) == 0
		recentlyFailed := false
		if strategy != StrategyRoundRobin {
			recentlyFailed = IsChannelRecentlyFailed(&row.Channel.FailCount, row.Channel.LastFailAt, nowMs, configuredMaxSec)
		}
		candidate := RouteDecisionCandidate{
			ChannelID:              row.Channel.ID,
			AccountID:              row.Account.ID,
			Username:               formatUsername(row.Account.Username, row.Account.ID),
			SiteName:               formatSiteName(row.Site.Name),
			TokenName:              getTokenName(row.Token),
			Priority:               row.Channel.Priority,
			Weight:                 row.Channel.Weight,
			Eligible:               eligible,
			RecentlyFailed:         recentlyFailed,
			AvoidedByRecentFailure: false,
			Probability:            0,
			Reason:                 formatReasons(reasons),
		}
		candidates = append(candidates, candidate)
		if eligible {
			available = append(available, row)
		}
	}

	if len(available) == 0 {
		summary = append(summary, "没有可用通道（全部被禁用、站点不可用、冷却或令牌不可用）")
		return RouteDecisionExplanation{
			RequestedModel: requestedModel,
			ActualModel:    mappedModel,
			Matched:        true,
			RouteID:        &match.Route.ID,
			ModelPattern:   match.Route.ModelPattern,
			Summary:        summary,
			Candidates:     candidates,
		}
	}

	// Build a decision based on strategy
	explanation := RouteDecisionExplanation{
		RequestedModel: requestedModel,
		ActualModel:    mappedModel,
		Matched:        true,
		RouteID:        &match.Route.ID,
		ModelPattern:   match.Route.ModelPattern,
		Summary:        summary,
		Candidates:     candidates,
	}

	_ = runtimeModelResolver
	return explanation
}

func getCandidateEligibilityReasonsExplain(
	candidate RouteChannelCandidate,
	requestedModel string,
	bypassSourceModelCheck bool,
	excludeChannelIDs []int64,
	nowISO string,
	policy DownstreamRoutingPolicy,
) []string {
	var reasons []string

	if !bypassSourceModelCheck && !ChannelSupportsRequestedModel(candidate.Channel.SourceModel, requestedModel) {
		src := ""
		if candidate.Channel.SourceModel != nil {
			src = *candidate.Channel.SourceModel
		}
		reasons = append(reasons, "来源模型不匹配="+src)
	}
	if !candidate.Channel.Enabled {
		reasons = append(reasons, "通道禁用")
	}
	if candidate.Account.Status != "active" {
		reasons = append(reasons, "账号状态="+candidate.Account.Status)
	}
	if candidate.Site.Status == "disabled" {
		reasons = append(reasons, "站点状态="+candidate.Site.Status)
	}
	for _, id := range excludeChannelIDs {
		if id == candidate.Channel.ID {
			reasons = append(reasons, "当前请求已尝试")
			break
		}
	}
	if candidate.Channel.CooldownUntil != nil && *candidate.Channel.CooldownUntil > nowISO {
		reasons = append(reasons, "冷却中")
	}
	return reasons
}

func formatUsername(username *string, accountID int64) string {
	if username != nil && *username != "" {
		return *username
	}
	return fmt.Sprintf("account-%d", accountID)
}

func formatSiteName(name string) string {
	if name == "" {
		return "unknown"
	}
	return name
}

func getTokenName(token *store.AccountToken) string {
	if token == nil {
		return "default"
	}
	return token.Name
}

func formatReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "可用"
	}
	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += "、"
		}
		result += r
	}
	return result
}

func stringsTrimSpace(s string) string {
	return stringsTrimSpaceImpl(s)
}

func stringsTrimSpaceImpl(s string) string {
	// trim leading and trailing whitespace
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// RecordSuccess records a successful channel usage.
func (tr *TokenRouter) RecordSuccess(ctx context.Context, channelID int64, latencyMs float64, cost float64, modelName *string, actualAccountID *int64) error {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return err
	}

	row, err := tr.db.LoadChannelWithAccount(ctx, channelID)
	if err != nil || row == nil {
		return err
	}

	ch := row.Channel
	account := row.Account
	nowISO := time.Now().UTC().Format(time.RFC3339)
	nextSuccessCount := max64(0, ch.SuccessCount) + 1
	nextTotalLatencyMs := max64(0, ch.TotalLatencyMs) + int64(latencyMs)
	nextTotalCost := math.Max(0, ch.TotalCost) + cost

	if ch.OAuthRouteUnitID != nil && *ch.OAuthRouteUnitID > 0 {
		targetAccountID := account.ID
		if actualAccountID != nil && *actualAccountID > 0 {
			targetAccountID = *actualAccountID
		}

		memberRow, err := tr.db.LoadRouteUnitMemberWithAccount(ctx, *ch.OAuthRouteUnitID, targetAccountID)
		if err == nil && memberRow != nil {
			memberSuccessCount := max64(0, memberRow.Member.SuccessCount) + 1
			memberTotalLatencyMs := max64(0, memberRow.Member.TotalLatencyMs) + int64(latencyMs)
			memberTotalCost := math.Max(0, memberRow.Member.TotalCost) + cost
			_ = tr.db.UpdateRouteUnitMemberSuccessFields(ctx, memberRow.Member.ID, map[string]interface{}{
				"successCount":   memberSuccessCount,
				"totalLatencyMs": memberTotalLatencyMs,
				"totalCost":      memberTotalCost,
				"lastUsedAt":     nowISO,
				"cooldownUntil":  nil,
				"lastFailAt":     nil,
				"consecutiveFailCount": int64(0),
				"cooldownLevel":  int64(0),
				"updatedAt":      nowISO,
			})
			RecordSiteRuntimeSuccess(memberRow.Account.SiteID, latencyMs, modelName)
		} else {
			RecordSiteRuntimeSuccess(account.SiteID, latencyMs, modelName)
		}
		tr.cache.InvalidateRouteScopedCache(ch.RouteID)
	} else {
		RecordSiteRuntimeSuccess(account.SiteID, latencyMs, modelName)
	}

	_ = tr.db.UpdateChannelSuccessFields(ctx, channelID, map[string]interface{}{
		"successCount":   nextSuccessCount,
		"totalLatencyMs": nextTotalLatencyMs,
		"totalCost":      nextTotalCost,
		"lastUsedAt":     nowISO,
		"cooldownUntil":  nil,
		"lastFailAt":     nil,
		"consecutiveFailCount": int64(0),
		"cooldownLevel":  int64(0),
	})

	tr.cache.PatchCachedChannel(channelID, func(ch *store.RouteChannel) {
		ch.SuccessCount = nextSuccessCount
		ch.TotalLatencyMs = nextTotalLatencyMs
		ch.TotalCost = nextTotalCost
		ch.LastUsedAt = &nowISO
		ch.CooldownUntil = nil
		ch.LastFailAt = nil
		ch.ConsecutiveFailCount = 0
		ch.CooldownLevel = 0
	})

	return nil
}

// RecordProbeSuccess records a successful background health probe.
// It clears cooldown for credential-scoped channels, feeds runtime health
// success, and stamps last probe status. It never marks credentials expired.
func (tr *TokenRouter) RecordProbeSuccess(ctx context.Context, channelID int64, latencyMs float64, modelName *string, actualAccountID *int64) error {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return err
	}

	row, err := tr.db.LoadChannelWithAccount(ctx, channelID)
	if err != nil || row == nil {
		return err
	}

	ch := row.Channel
	account := row.Account
	nowISO := time.Now().UTC().Format(time.RFC3339)
	channelIDCopy := channelID

	if ch.OAuthRouteUnitID != nil && *ch.OAuthRouteUnitID > 0 {
		targetAccountID := account.ID
		if actualAccountID != nil && *actualAccountID > 0 {
			targetAccountID = *actualAccountID
		}

		memberRow, err := tr.db.LoadRouteUnitMemberWithAccount(ctx, *ch.OAuthRouteUnitID, targetAccountID)
		if err == nil && memberRow != nil {
			_ = tr.db.UpdateRouteUnitMemberCooldownFields(ctx, memberRow.Member.ID, map[string]interface{}{
				"cooldownUntil":        nil,
				"lastFailAt":           nil,
				"consecutiveFailCount": int64(0),
				"cooldownLevel":        int64(0),
				"updatedAt":            nowISO,
			})
			RecordSiteRuntimeSuccess(memberRow.Account.SiteID, latencyMs, modelName)
			RecordSiteProbeOutcome(memberRow.Account.SiteID, "success", latencyMs, modelName, &channelIDCopy, nil)
		} else {
			RecordSiteRuntimeSuccess(account.SiteID, latencyMs, modelName)
			RecordSiteProbeOutcome(account.SiteID, "success", latencyMs, modelName, &channelIDCopy, nil)
		}

		_ = tr.db.UpdateChannelCooldownFields(ctx, []int64{channelID}, map[string]interface{}{
			"cooldownUntil":        nil,
			"lastFailAt":           nil,
			"consecutiveFailCount": int64(0),
			"cooldownLevel":        int64(0),
		})
		tr.cache.PatchCachedChannel(channelID, func(ch *store.RouteChannel) {
			ch.CooldownUntil = nil
			ch.LastFailAt = nil
			ch.ConsecutiveFailCount = 0
			ch.CooldownLevel = 0
		})
		tr.cache.InvalidateRouteScopedCache(ch.RouteID)
		return nil
	}

	affectedChannelIDs, err := tr.db.LoadCredentialScopedChannelIDs(ctx, ch, account.ID)
	if err != nil {
		return err
	}

	needsReset := ch.CooldownUntil != nil || ch.LastFailAt != nil || ch.ConsecutiveFailCount > 0 || ch.CooldownLevel > 0
	if needsReset {
		_ = tr.db.UpdateChannelCooldownFields(ctx, affectedChannelIDs, map[string]interface{}{
			"cooldownUntil":        nil,
			"lastFailAt":           nil,
			"consecutiveFailCount": int64(0),
			"cooldownLevel":        int64(0),
		})
		for _, id := range affectedChannelIDs {
			tr.cache.PatchCachedChannel(id, func(ch *store.RouteChannel) {
				ch.CooldownUntil = nil
				ch.LastFailAt = nil
				ch.ConsecutiveFailCount = 0
				ch.CooldownLevel = 0
			})
		}
	}

	RecordSiteRuntimeSuccess(account.SiteID, latencyMs, modelName)
	RecordSiteProbeOutcome(account.SiteID, "success", latencyMs, modelName, &channelIDCopy, nil)
	return nil
}

// RecordProbeFailure records a failed background health probe.
//
// Unlike RecordFailure (user traffic path), probe failures:
//   - update site/model runtime health + breaker streak (so routing can avoid dead channels)
//   - apply a short channel cooldown only for the probed channel (no credential cascade)
//   - never mark accounts/keys expired
//   - never treat auth-looking probe errors as credential expiry
func (tr *TokenRouter) RecordProbeFailure(ctx context.Context, channelID int64, failureCtx SiteRuntimeFailureContext, actualAccountID *int64) error {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return err
	}

	row, err := tr.db.LoadChannelWithAccountAndRoute(ctx, channelID)
	if err != nil || row == nil {
		return err
	}

	ch := row.Channel
	account := row.Account
	route := row.Route
	nowMs := time.Now().UnixMilli()
	nowISO := time.Now().UTC().Format(time.RFC3339)
	channelIDCopy := channelID

	// Normalize model onto failure context for runtime health model-level state.
	if failureCtx.ModelName == nil || *failureCtx.ModelName == "" {
		if ch.SourceModel != nil && *ch.SourceModel != "" {
			failureCtx.ModelName = ch.SourceModel
		}
	}

	// OAuth unit: update member cooldown only; outer channel fields stay clean.
	if ch.OAuthRouteUnitID != nil && *ch.OAuthRouteUnitID > 0 {
		targetAccountID := account.ID
		if actualAccountID != nil && *actualAccountID > 0 {
			targetAccountID = *actualAccountID
		}

		memberRow, err := tr.db.LoadRouteUnitMemberWithAccount(ctx, *ch.OAuthRouteUnitID, targetAccountID)
		if err == nil && memberRow != nil {
			// Probe path intentionally ignores usage-limit credential cascade and
			// never zeros failCount for short-window limits: probes are synthetic.
			failCount := max64(0, memberRow.Member.FailCount) + 1
			routeUnitStrategy := memberRow.Unit.Strategy
			if routeUnitStrategy == "" {
				routeUnitStrategy = "round_robin"
			}

			var cooldownUntil *string
			consecutiveFailCount := max64(0, memberRow.Member.ConsecutiveFailCount) + 1
			cooldownLevel := max64(0, memberRow.Member.CooldownLevel)

			if routeUnitStrategy == "round_robin" {
				nextCF, nextCL, cu := ApplyRoundRobinCooldown(consecutiveFailCount, cooldownLevel, nowMs, tr.configuredMaxSec)
				consecutiveFailCount = nextCF
				cooldownLevel = nextCL
				cooldownUntil = cu
			} else {
				cu := ApplyFibonacciCooldown(failCount, nowMs, tr.configuredMaxSec)
				consecutiveFailCount = 0
				cooldownLevel = 0
				cooldownUntil = cu
			}

			_ = tr.db.UpdateRouteUnitMemberCooldownFields(ctx, memberRow.Member.ID, map[string]interface{}{
				"failCount":            failCount,
				"lastFailAt":           nowISO,
				"consecutiveFailCount": consecutiveFailCount,
				"cooldownLevel":        cooldownLevel,
				"cooldownUntil":        cooldownUntil,
				"updatedAt":            nowISO,
			})
			RecordSiteRuntimeFailure(memberRow.Account.SiteID, failureCtx)
			RecordSiteProbeOutcome(memberRow.Account.SiteID, "failure", 0, failureCtx.ModelName, &channelIDCopy, failureCtx.ErrorText)
			tr.cache.InvalidateRouteScopedCache(route.ID)
		}
		return nil
	}

	// Regular channel: probe failure cools only the probed channel (no credential cascade).
	failCount := max64(0, ch.FailCount) + 1
	routeStrategy := NormalizeRouteRoutingStrategy(route.RoutingStrategy)
	affectedChannelIDs := []int64{channelID}

	var cooldownUntil *string
	consecutiveFailCount := max64(0, ch.ConsecutiveFailCount) + 1
	cooldownLevel := max64(0, ch.CooldownLevel)

	if routeStrategy == StrategyRoundRobin {
		nextCF, nextCL, cu := ApplyRoundRobinCooldown(consecutiveFailCount, cooldownLevel, nowMs, tr.configuredMaxSec)
		consecutiveFailCount = nextCF
		cooldownLevel = nextCL
		cooldownUntil = cu
	} else {
		cu := ApplyFibonacciCooldown(failCount, nowMs, tr.configuredMaxSec)
		consecutiveFailCount = 0
		cooldownLevel = 0
		cooldownUntil = cu
	}

	_ = tr.db.UpdateChannelCooldownFields(ctx, affectedChannelIDs, map[string]interface{}{
		"failCount":            failCount,
		"lastFailAt":           nowISO,
		"consecutiveFailCount": consecutiveFailCount,
		"cooldownLevel":        cooldownLevel,
		"cooldownUntil":        cooldownUntil,
	})

	for _, id := range affectedChannelIDs {
		tr.cache.PatchCachedChannel(id, func(ch *store.RouteChannel) {
			ch.FailCount = failCount
			ch.LastFailAt = &nowISO
			ch.ConsecutiveFailCount = consecutiveFailCount
			ch.CooldownLevel = cooldownLevel
			ch.CooldownUntil = cooldownUntil
		})
	}

	RecordSiteRuntimeFailure(account.SiteID, failureCtx)
	RecordSiteProbeOutcome(account.SiteID, "failure", 0, failureCtx.ModelName, &channelIDCopy, failureCtx.ErrorText)
	return nil
}

// RecordFailure records a channel failure and sets cooldown.
func (tr *TokenRouter) RecordFailure(ctx context.Context, channelID int64, failureCtx SiteRuntimeFailureContext, actualAccountID *int64) error {
	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return err
	}

	row, err := tr.db.LoadChannelWithAccountAndRoute(ctx, channelID)
	if err != nil || row == nil {
		return err
	}

	ch := row.Channel
	account := row.Account
	route := row.Route
	nowMs := time.Now().UnixMilli()
	nowISO := time.Now().UTC().Format(time.RFC3339)

	// Handle OAuth route unit member
	if ch.OAuthRouteUnitID != nil && *ch.OAuthRouteUnitID > 0 {
		targetAccountID := account.ID
		if actualAccountID != nil && *actualAccountID > 0 {
			targetAccountID = *actualAccountID
		}

		memberRow, err := tr.db.LoadRouteUnitMemberWithAccount(ctx, *ch.OAuthRouteUnitID, targetAccountID)
		if err == nil && memberRow != nil {
			shortWindowCooldown := resolveShortWindowLimitCooldownTS(memberRow.Account, failureCtx, nowMs)
			failCount := max64(0, memberRow.Member.FailCount)
			if shortWindowCooldown == nil {
				failCount++
			} else {
				failCount = 0
			}

			routeUnitStrategy := memberRow.Unit.Strategy
			if routeUnitStrategy == "" {
				routeUnitStrategy = "round_robin"
			}

			var cooldownUntil *string
			consecutiveFailCount := max64(0, memberRow.Member.ConsecutiveFailCount) + 1
			cooldownLevel := max64(0, memberRow.Member.CooldownLevel)

			if shortWindowCooldown != nil {
				cooldownUntil = shortWindowCooldown
				consecutiveFailCount = 0
				cooldownLevel = 0
			} else if routeUnitStrategy == "round_robin" {
				nextCF, nextCL, cu := ApplyRoundRobinCooldown(consecutiveFailCount, cooldownLevel, nowMs, tr.configuredMaxSec)
				consecutiveFailCount = nextCF
				cooldownLevel = nextCL
				cooldownUntil = cu
			} else {
				cu := ApplyFibonacciCooldown(failCount, nowMs, tr.configuredMaxSec)
				consecutiveFailCount = 0
				cooldownLevel = 0
				cooldownUntil = cu
			}

			_ = tr.db.UpdateRouteUnitMemberCooldownFields(ctx, memberRow.Member.ID, map[string]interface{}{
				"failCount":          failCount,
				"lastFailAt":         nowISO,
				"consecutiveFailCount": consecutiveFailCount,
				"cooldownLevel":      cooldownLevel,
				"cooldownUntil":      cooldownUntil,
				"updatedAt":          nowISO,
			})
			RecordSiteRuntimeFailure(memberRow.Account.SiteID, failureCtx)
			tr.cache.InvalidateRouteScopedCache(route.ID)
		}
		return nil
	}

	// Regular channel
	shortWindowCooldown := resolveShortWindowLimitCooldownTS(account, failureCtx, nowMs)
	failCount := max64(0, ch.FailCount)
	if shortWindowCooldown == nil {
		failCount++
	} else {
		failCount = 0
	}

	routeStrategy := NormalizeRouteRoutingStrategy(route.RoutingStrategy)
	var affectedChannelIDs []int64
	if shortWindowCooldown != nil {
		ids, err := tr.db.LoadCredentialScopedChannelIDs(ctx, ch, account.ID)
		if err != nil {
			return err
		}
		affectedChannelIDs = ids
	} else {
		affectedChannelIDs = []int64{channelID}
	}

	var cooldownUntil *string
	consecutiveFailCount := max64(0, ch.ConsecutiveFailCount) + 1
	cooldownLevel := max64(0, ch.CooldownLevel)

	if shortWindowCooldown != nil {
		cooldownUntil = shortWindowCooldown
		consecutiveFailCount = 0
		cooldownLevel = 0
	} else if routeStrategy == StrategyRoundRobin {
		nextCF, nextCL, cu := ApplyRoundRobinCooldown(consecutiveFailCount, cooldownLevel, nowMs, tr.configuredMaxSec)
		consecutiveFailCount = nextCF
		cooldownLevel = nextCL
		cooldownUntil = cu
	} else {
		cu := ApplyFibonacciCooldown(failCount, nowMs, tr.configuredMaxSec)
		consecutiveFailCount = 0
		cooldownLevel = 0
		cooldownUntil = cu
	}

	_ = tr.db.UpdateChannelCooldownFields(ctx, affectedChannelIDs, map[string]interface{}{
		"failCount":          failCount,
		"lastFailAt":         nowISO,
		"consecutiveFailCount": consecutiveFailCount,
		"cooldownLevel":      cooldownLevel,
		"cooldownUntil":      cooldownUntil,
	})

	for _, id := range affectedChannelIDs {
		tr.cache.PatchCachedChannel(id, func(ch *store.RouteChannel) {
			ch.FailCount = failCount
			ch.LastFailAt = &nowISO
			ch.ConsecutiveFailCount = consecutiveFailCount
			ch.CooldownLevel = cooldownLevel
			ch.CooldownUntil = cooldownUntil
		})
	}

	RecordSiteRuntimeFailure(account.SiteID, failureCtx)
	return nil
}

// ClearChannelFailureState clears failure/cooldown for channels.
func (tr *TokenRouter) ClearChannelFailureState(ctx context.Context, channelIDs []int64) (int64, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	if err := EnsureSiteRuntimeHealthStateLoaded(); err != nil {
		return 0, err
	}

	// Clear runtime health states for affected channels
	runtimeHealthRows, _ := tr.db.LoadRuntimeHealthChannelRows(ctx, channelIDs)
	if len(runtimeHealthRows) > 0 {
		healthRows := make([]ChannelRuntimeHealthRow, len(runtimeHealthRows))
		for i, r := range runtimeHealthRows {
			healthRows[i] = ChannelRuntimeHealthRow{
				SiteID:            r.SiteID,
				SourceModel:       r.SourceModel,
				RouteModelPattern: r.RouteModelPattern,
			}
		}
		if ClearRuntimeHealthStatesForChannels(healthRows) {
			persistSiteRuntimeHealthState()
		}
	}

	if err := tr.db.ClearChannelFailureStates(ctx, channelIDs); err != nil {
		return 0, err
	}

	tr.cache.InvalidateAll()
	return int64(len(channelIDs)), nil
}

// InvalidateRouteScopedCache clears cache for a specific route.
func (tr *TokenRouter) InvalidateRouteScopedCache(routeID int64) {
	tr.cache.InvalidateRouteScopedCache(routeID)
}

// InvalidateAllCaches clears all caches.
func (tr *TokenRouter) InvalidateAllCaches() {
	tr.cache.InvalidateAll()
}

// ResolveShortWindowLimitCooldown resolves the short-window limit cooldown for a failure.
func resolveShortWindowLimitCooldownTS(account store.Account, ctx SiteRuntimeFailureContext, nowMs int64) *string {
	status := 0
	if ctx.Status != nil {
		status = *ctx.Status
	}
	errorText := ""
	if ctx.ErrorText != nil {
		errorText = *ctx.ErrorText
	}
	if !IsUsageLimitRateLimitFailure(SiteRuntimeFailureContext{Status: &status, ErrorText: &errorText}) {
		return nil
	}

	// Check for quota reset hint in error text
	// Simple check — real implementation would parse structured hints
	// For now, default to 5 minute cooldown
	untilMs := nowMs + ShortWindowLimitCooldownMs

	// Check OAuth quota lastLimitResetAt
	if account.OAuthProvider != nil && *account.OAuthProvider == "codex" {
		// Could read from extraConfig, simplified for now
	}

	iso := timeMsToISO(untilMs)
	return &iso
}
