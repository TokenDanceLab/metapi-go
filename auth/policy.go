package auth

// CredentialRefKind distinguishes the two flavours of excluded credential references.
type CredentialRefKind string

const (
	CredentialRefAccountToken  CredentialRefKind = "account_token"
	CredentialRefDefaultApiKey CredentialRefKind = "default_api_key"
)

// ExcludedCredentialRef represents a credential that a downstream policy
// explicitly excludes from routing.
//
//   - account_token  variant: excludes a specific account's specific token.
//   - default_api_key variant: excludes a specific account's default API key (no TokenID).
type ExcludedCredentialRef struct {
	Kind      CredentialRefKind `json:"kind"`
	SiteID    int64             `json:"site_id"`
	AccountID int64             `json:"account_id"`
	TokenID   *int64            `json:"token_id,omitempty"` // only account_token
}

// DownstreamRoutingPolicy holds the routing constraints attached to a
// downstream API key (managed) or the global proxy token.
//
// DenyAllWhenEmpty controls the default behaviour when both SupportedModels
// and AllowedRouteIDs are empty:
//   - true  (managed key default): reject all models
//   - false (global token default): allow all models
type DownstreamRoutingPolicy struct {
	SupportedModels        []string                `json:"supported_models"`
	AllowedRouteIDs        []int64                 `json:"allowed_route_ids"`
	SiteWeightMultipliers  map[int64]float64       `json:"site_weight_multipliers"`
	ExcludedSiteIDs        []int64                 `json:"excluded_site_ids"`
	ExcludedCredentialRefs []ExcludedCredentialRef `json:"excluded_credential_refs"`
	DenyAllWhenEmpty       bool                    `json:"deny_all_when_empty"`
}

// EmptyDownstreamRoutingPolicy is the zero-value policy used as the default
// for the global proxy token (DenyAllWhenEmpty=false → allow all).
var EmptyDownstreamRoutingPolicy = DownstreamRoutingPolicy{
	SupportedModels:        []string{},
	AllowedRouteIDs:        []int64{},
	SiteWeightMultipliers:  map[int64]float64{},
	ExcludedSiteIDs:        []int64{},
	ExcludedCredentialRefs: []ExcludedCredentialRef{},
	// DenyAllWhenEmpty defaults to false (zero value), which means "allow all"
}
