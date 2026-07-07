package platform

import (
	"context"
	"strings"
)

// AnyRouterAdapter reuses NewAPI-compatible session operations, but token
// management is intentionally disabled because AnyRouter's API-key workflow is
// not the NewAPI /api/token/ contract.
type AnyRouterAdapter struct {
	*NewApiAdapter
}

func init() {
	Register(&AnyRouterAdapter{NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")}})
}

// Detect matches URL keyword "anyrouter" (case-insensitive).
func (a *AnyRouterAdapter) Detect(ctx context.Context, url string) (bool, error) {
	return strings.Contains(strings.ToLower(url), "anyrouter"), nil
}

func (a *AnyRouterAdapter) GetAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*string, error) {
	return nil, nil
}

func (a *AnyRouterAdapter) GetAPITokens(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]ApiTokenInfo, error) {
	return []ApiTokenInfo{}, nil
}

func (a *AnyRouterAdapter) CreateAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, options *CreateAPITokenOptions, proxy *ProxyConfig) (bool, error) {
	return false, nil
}

func (a *AnyRouterAdapter) DeleteAPIToken(ctx context.Context, url, accessToken, tokenKey string, platformUserId *int, proxy *ProxyConfig) error {
	return nil
}
