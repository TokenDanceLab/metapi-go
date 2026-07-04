package platform

import (
	"context"
	"strings"
)

const claudeDefaultAnthropicVersion = "2023-06-01"

// ClaudeAdapter handles api.anthropic.com platforms (native + OpenAI-compat gateways).
type ClaudeAdapter struct {
	*StandardAdapter
}

func init() {
	Register(&ClaudeAdapter{StandardAdapter: NewStandardAdapter("claude")})
}

// Detect matches URL keywords: api.anthropic.com or anthropic.com/v1.
func (c *ClaudeAdapter) Detect(ctx context.Context, url string) (bool, error) {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "api.anthropic.com") || strings.Contains(lower, "anthropic.com/v1"), nil
}

// GetModels tries native Anthropic endpoint first, then falls back to OpenAI-compat
// by stripping the /anthropic suffix from the base URL.
func (c *ClaudeAdapter) GetModels(ctx context.Context, baseURL string, token string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	openAICompatBaseURL := resolveOpenAICompatibleBaseURL(baseURL)

	// Try native Anthropic endpoint
	claudeHeaders := map[string]string{
		"x-api-key":         token,
		"anthropic-version": claudeDefaultAnthropicVersion,
	}
	models, err := c.fetchModelsFromStandardEndpoint(ctx, baseURL, claudeHeaders, proxy)
	if err == nil && len(models) > 0 {
		return models, nil
	}

	// Fallback: strip /anthropic suffix and try OpenAI-compat
	if openAICompatBaseURL == "" {
		return []string{}, nil
	}

	return c.fetchModelsFromStandardEndpoint(ctx, openAICompatBaseURL, authBearerHeaders(token), proxy)
}

// resolveOpenAICompatibleBaseURL strips the /anthropic suffix to get the OpenAI-compat base.
func resolveOpenAICompatibleBaseURL(baseURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(normalized)
	if strings.HasSuffix(lower, "/anthropic") {
		return normalized[:len(normalized)-len("/anthropic")]
	}
	return ""
}
