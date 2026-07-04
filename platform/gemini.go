package platform

import (
	"context"
	"net/url"
	"strings"
)

// GeminiAdapter handles Google Gemini API platforms with 3-path model discovery.
type GeminiAdapter struct {
	*StandardAdapter
}

func init() {
	Register(&GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini")})
}

// Detect matches URL keywords: generativelanguage.googleapis.com, googleapis.com/v1beta/openai, gemini.google.com.
func (g *GeminiAdapter) Detect(ctx context.Context, urlStr string) (bool, error) {
	lower := strings.ToLower(urlStr)
	return strings.Contains(lower, "generativelanguage.googleapis.com") ||
		strings.Contains(lower, "googleapis.com/v1beta/openai") ||
		strings.Contains(lower, "gemini.google.com"), nil
}

// GetModels uses 3-path discovery: OpenAI-compat (if on /openai/ path) -> native Gemini -> OpenAI-compat fallback.
func (g *GeminiAdapter) GetModels(ctx context.Context, baseURL string, apiToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error) {
	normalizedBase := normalizePlatformBaseURL(baseURL)

	// Path 1: OpenAI-compat if URL contains /openai/
	if isOpenAICompatGeminiBase(normalizedBase) {
		models, err := g.fetchModelsFromStandardEndpoint(ctx, normalizedBase, authBearerHeaders(apiToken), proxy)
		if err == nil && len(models) > 0 {
			return normalizeModelList(models), nil
		}
	}

	// Path 2: Native Gemini endpoint
	nativeURL := resolveGeminiNativeModelsURL(normalizedBase, apiToken)
	resp, err := fetchJSON(ctx, nativeURL, "GET", nil, nil, proxy)
	if err == nil {
		if modelsList, ok := resp["models"].([]interface{}); ok {
			models := make([]string, 0, len(modelsList))
			for _, m := range modelsList {
				if mm, ok := m.(map[string]interface{}); ok {
					if name, ok := mm["name"].(string); ok && strings.TrimSpace(name) != "" {
						models = append(models, strings.TrimSpace(name))
					}
				}
			}
			if len(models) > 0 {
				return normalizeModelList(models), nil
			}
		}
	}

	// Path 3: OpenAI-compat fallback (if not already on /openai/ path)
	if !isOpenAICompatGeminiBase(normalizedBase) {
		openAIURL := normalizedBase + "/v1beta/openai"
		models, err := g.fetchModelsFromStandardEndpoint(ctx, openAIURL, authBearerHeaders(apiToken), proxy)
		if err == nil && len(models) > 0 {
			return normalizeModelList(models), nil
		}
	}

	return []string{}, nil
}

func isOpenAICompatGeminiBase(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), "/openai/") || strings.HasSuffix(strings.ToLower(baseURL), "/openai")
}

func resolveGeminiNativeModelsURL(baseURL, apiToken string) string {
	normalized := normalizePlatformBaseURL(baseURL)

	// Ensure version path
	if !strings.Contains(strings.ToLower(normalized), "/v1beta") && !strings.Contains(strings.ToLower(normalized), "/v1") {
		normalized += "/v1beta"
	}

	listBase := normalized
	if !strings.HasSuffix(strings.ToLower(listBase), "/models") {
		listBase += "/models"
	}

	sep := "?"
	if strings.Contains(listBase, "?") {
		sep = "&"
	}
	return listBase + sep + "key=" + url.QueryEscape(apiToken)
}

func stripModelPrefix(name string) string {
	t := strings.TrimSpace(name)
	return strings.TrimPrefix(t, "models/")
}

func normalizeModelList(models []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(models))
	for _, m := range models {
		normalized := stripModelPrefix(m)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	return result
}
