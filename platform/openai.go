package platform

import (
	"context"
	"strings"
)

// OpenAiAdapter handles api.openai.com platforms.
type OpenAiAdapter struct {
	*StandardAdapter
}

func init() {
	Register(&OpenAiAdapter{StandardAdapter: NewStandardAdapter("openai")})
}

// Detect matches by URL keyword: api.openai.com.
func (o *OpenAiAdapter) Detect(ctx context.Context, url string) (bool, error) {
	return strings.Contains(strings.ToLower(url), "api.openai.com"), nil
}

// GetModels fetches models from the standard /v1/models OpenAI endpoint.
func (o *OpenAiAdapter) GetModels(ctx context.Context, baseURL string, token string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	return o.fetchModelsFromStandardEndpoint(ctx, baseURL, authBearerHeaders(token), proxy)
}
