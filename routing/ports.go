// Package routing implements the TokenRouter route selection engine.
// Split from the TS 3800-line monolith into independent modules with
// interface contracts and unidirectional dependencies.
package routing

import (
	"context"
	"strings"

	"github.com/tokendancelab/metapi-go/store"
)

// ModelProvider supplies model availability data.
type ModelProvider interface {
	GetAvailableModels(ctx context.Context, accountID int64) ([]ModelInfo, error)
	RefreshModelsForAccount(ctx context.Context, accountID int64) error
}

// ModelInfo is a lightweight model availability record.
type ModelInfo struct {
	ModelName string
	Available bool
	LatencyMs *int64
}

// TokenProvider supplies token data.
type TokenProvider interface {
	GetTokens(ctx context.Context, accountID int64) ([]store.AccountToken, error)
	GetDefaultToken(ctx context.Context, accountID int64) (*store.AccountToken, error)
}

// PricingProvider supplies model pricing reference costs.
type PricingProvider interface {
	GetReferenceCost(ctx context.Context, model string, siteID int64, accountID int64) (float64, error)
	RefreshModelPricingCatalog(ctx context.Context, site store.Site, account store.Account, modelName string) error
}

// ChannelLoadSnapshotProvider supplies per-channel concurrency load snapshots.
type ChannelLoadSnapshotProvider interface {
	GetChannelLoadSnapshot(params ChannelLoadParams) ChannelLoadSnapshot
}

// ChannelLoadParams are the parameters for resolving a channel's load snapshot.
type ChannelLoadParams struct {
	ChannelID           int64
	AccountExtraConfig  *string
	AccountOAuthProvider *string
}

// ChannelLoadSnapshot is a snapshot of a channel's concurrency load.
type ChannelLoadSnapshot struct {
	SessionScoped    bool
	ConcurrencyLimit int
	ActiveLeaseCount int
	WaitingCount     int
	Saturated        bool
}

// RouteRebuilder rebuilds token routes from model availability data.
type RouteRebuilder interface {
	RebuildTokenRoutesFromAvailability(ctx context.Context) error
}

// DownstreamRoutingPolicy mirrors TS DownstreamRoutingPolicy.
type DownstreamRoutingPolicy struct {
	ExcludedSiteIDs        []int64
	ExcludedCredentialRefs []CredentialRef
	AllowedRouteIDs        []int64
	SupportedModels        []string
	DenyAllWhenEmpty       bool
	SiteWeightMultipliers  map[int64]float64
}

// CredentialRef identifies a specific credential to exclude.
type CredentialRef struct {
	Kind      string `json:"kind"` // "account_token" or empty
	TokenID   int64  `json:"tokenId"`
	AccountID int64  `json:"accountId"`
	SiteID    int64  `json:"siteId"`
}

// EmptyDownstreamRoutingPolicy is the default allow-all policy.
var EmptyDownstreamRoutingPolicy = DownstreamRoutingPolicy{
	SiteWeightMultipliers: map[int64]float64{},
}

// SelectedChannel is the result of a successful channel selection.
type SelectedChannel struct {
	Channel    store.RouteChannel
	Account    store.Account
	Site       store.Site
	Token      *store.AccountToken
	TokenValue string
	TokenName  string
	ActualModel string
}

// RouteDecisionExplanation mirrors TS RouteDecisionExplanation.
type RouteDecisionExplanation struct {
	RequestedModel    string
	ActualModel       string
	Matched           bool
	RouteID           *int64
	ModelPattern      string
	SelectedChannelID *int64
	SelectedAccountID *int64
	SelectedLabel     string
	Summary           []string
	Candidates        []RouteDecisionCandidate
}

// RouteDecisionCandidate mirrors TS RouteDecisionCandidate.
type RouteDecisionCandidate struct {
	ChannelID             int64
	AccountID             int64
	Username              string
	SiteName              string
	TokenName             string
	Priority              int64
	Weight                int64
	Eligible              bool
	RecentlyFailed        bool
	AvoidedByRecentFailure bool
	Probability           float64
	Reason                string
}

// RouteRoutingStrategy is the strategy for a route.
// Named strategies are operator-selectable per route (token_routes.routing_strategy).
// Default remains weighted for parity with historical MetAPI behavior.
type RouteRoutingStrategy string

const (
	StrategyWeighted       RouteRoutingStrategy = "weighted"
	StrategyRoundRobin     RouteRoutingStrategy = "round_robin"
	StrategyStableFirst    RouteRoutingStrategy = "stable_first"
	StrategyLeastBusy      RouteRoutingStrategy = "least_busy"
	StrategyLowestLatency  RouteRoutingStrategy = "lowest_latency"
	StrategyLowestCost     RouteRoutingStrategy = "lowest_cost"
)

// KnownRouteRoutingStrategies lists accepted strategy names (excluding aliases).
var KnownRouteRoutingStrategies = []RouteRoutingStrategy{
	StrategyWeighted,
	StrategyRoundRobin,
	StrategyStableFirst,
	StrategyLeastBusy,
	StrategyLowestLatency,
	StrategyLowestCost,
}

// NormalizeRouteRoutingStrategy normalizes a strategy string.
// Unknown / empty values fall back to weighted (documented default).
// Accepts hyphen aliases (least-busy, lowest-latency, lowest-cost) for operator convenience.
func NormalizeRouteRoutingStrategy(value string) RouteRoutingStrategy {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "round_robin", "round-robin":
		return StrategyRoundRobin
	case "stable_first", "stable-first":
		return StrategyStableFirst
	case "least_busy", "least-busy":
		return StrategyLeastBusy
	case "lowest_latency", "lowest-latency", "latency":
		return StrategyLowestLatency
	case "lowest_cost", "lowest-cost", "cost":
		return StrategyLowestCost
	case "weighted", "":
		return StrategyWeighted
	default:
		return StrategyWeighted
	}
}

// IsRoundRobinRouteRoutingStrategy checks if a strategy is round_robin.
func IsRoundRobinRouteRoutingStrategy(value string) bool {
	return NormalizeRouteRoutingStrategy(value) == StrategyRoundRobin
}

// IsPriorityLayerRoutingStrategy reports strategies that walk priority layers
// (P is a hard gate) then pick inside the first non-empty layer.
func IsPriorityLayerRoutingStrategy(strategy RouteRoutingStrategy) bool {
	switch strategy {
	case StrategyWeighted, StrategyLeastBusy, StrategyLowestLatency, StrategyLowestCost:
		return true
	default:
		return false
	}
}

// IsDeterministicNamedStrategy reports pure argmin strategies (no random blend).
func IsDeterministicNamedStrategy(strategy RouteRoutingStrategy) bool {
	switch strategy {
	case StrategyLeastBusy, StrategyLowestLatency, StrategyLowestCost:
		return true
	default:
		return false
	}
}

// RouteMatch holds a matched route with its resolved channels.
type RouteMatch struct {
	Route    store.TokenRoute
	Channels []RouteChannelCandidate
}

// RouteChannelCandidate is a channel joined with account, site, token, and optional OAuth route unit.
type RouteChannelCandidate struct {
	Channel          store.RouteChannel
	Account          store.Account
	Site             store.Site
	Token            *store.AccountToken
	RouteUnit        *OAuthRouteUnitSummary
	RouteUnitMembers []OAuthRouteUnitMemberCandidate
}

// OAuthRouteUnitSummary is a light summary of an OAuth route unit.
type OAuthRouteUnitSummary struct {
	ID       int64
	SiteID   int64
	Provider string
	Name     string
	Strategy string
	Enabled  bool
}

// OAuthRouteUnitMemberCandidate is a member candidate with account and site info.
type OAuthRouteUnitMemberCandidate struct {
	Member  store.OAuthRouteUnitMember
	Account store.Account
	Site    store.Site
}

// SiteRuntimeFailureContext describes a failure event for runtime health tracking.
type SiteRuntimeFailureContext struct {
	Status    *int
	ErrorText *string
	ModelName *string
}

// CostSignal describes the unit cost and its provenance.
type CostSignal struct {
	UnitCost float64
	Source   string // "observed", "configured", "catalog", "fallback"
}

// PricingReferenceRefreshOptions configures pricing refresh behavior.
type PricingReferenceRefreshOptions struct {
	UseChannelSourceModelForCost bool
	DownstreamPolicy             DownstreamRoutingPolicy
	RefreshedKeys                *map[string]struct{}
}

// ExplainSelectionOptions configures explain-selection behavior.
type ExplainSelectionOptions struct {
	ExcludeChannelIDs           []int64
	BypassSourceModelCheck      bool
	UseChannelSourceModelForCost bool
	DownstreamPolicy             DownstreamRoutingPolicy
}
