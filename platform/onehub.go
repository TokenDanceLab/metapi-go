package platform

import (
	"context"
	"strings"
)

// OneHubAdapter extends OneApiAdapter with title-first detect and /api/available_model fallback.
type OneHubAdapter struct {
	*OneApiAdapter
}

func init() {
	Register(&OneHubAdapter{OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("one-hub")}})
}

// Detect: URL keyword match "onehub" or "one-hub" (case-insensitive, title-first platform).
func (o *OneHubAdapter) Detect(ctx context.Context, url string) (bool, error) {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "onehub") || strings.Contains(lower, "one-hub"), nil
}

// GetModels: try /v1/models (OneApi), fall back to /api/available_model.
func (o *OneHubAdapter) GetModels(ctx context.Context, baseURL string, apiToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	models, err := o.OneApiAdapter.GetModels(ctx, baseURL, apiToken, platformUserId, proxy)
	if err == nil && len(models) > 0 {
		return models, nil
	}

	// Fallback: /api/available_model
	headers := authBearerHeaders(apiToken)
	resp, fetchErr := fetchJSON(ctx, baseURL+"/api/available_model", "GET", nil, headers, proxy)
	if fetchErr != nil {
		return []string{}, nil
	}

	payload := resp
	if data, ok := getMap(resp, "data"); ok {
		payload = data
	}

	if payload != nil {
		models := make([]string, 0, len(payload))
		for k := range payload {
			if t := strings.TrimSpace(k); t != "" {
				models = append(models, t)
			}
		}
		if len(models) > 0 {
			return models, nil
		}
	}

	return []string{}, nil
}

// GetUserGroups: /api/user_group_map with data-object keys, fallback to OneApi's implementation.
func (o *OneHubAdapter) GetUserGroups(ctx context.Context, baseURL, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	headers := authBearerHeaders(accessToken)
	resp, err := fetchJSON(ctx, baseURL+"/api/user_group_map", "GET", nil, headers, proxy)
	if err == nil {
		source := resp
		if data, ok := getMap(resp, "data"); ok {
			source = data
		}
		if source != nil {
			groups := make([]string, 0, len(source))
			for k := range source {
				if t := strings.TrimSpace(k); t != "" {
					groups = append(groups, t)
				}
			}
			if len(groups) > 0 {
				return dedupeStrings(groups), nil
			}
		}
	}

	// Fallback to OneApi's implementation
	return o.OneApiAdapter.GetUserGroups(ctx, baseURL, accessToken, platformUserId, proxy)
}
