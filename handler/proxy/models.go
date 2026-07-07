package proxyhandler

import (
	"net/http"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/auth"
)

// HandleModels handles GET /v1/models.
// Returns model list in OpenAI or Claude format based on request headers.
func HandleModels(w http.ResponseWriter, r *http.Request) {
	authCtx := GetProxyAuth(r)
	if authCtx == nil {
		writeJSONError(w, 401, "unauthorized", "invalid_request_error")
		return
	}

	// Detect response format: if anthropic-version or x-api-key header present → Claude format
	wantsClaude := r.Header.Get("anthropic-version") != "" || r.Header.Get("x-api-key") != ""

	// Build model list
	models := getAvailableModels(authCtx.Policy)

	if wantsClaude {
		writeJSON(w, 200, buildClaudeModelsResponse(models))
	} else {
		writeJSON(w, 200, buildOpenAIModelsResponse(models))
	}
}

// buildOpenAIModelsResponse builds OpenAI-format models response.
func buildOpenAIModelsResponse(models []string) map[string]any {
	items := make([]map[string]any, 0, len(models))
	now := time.Now().Unix()
	for _, m := range models {
		items = append(items, map[string]any{
			"id":       m,
			"object":   "model",
			"created":  now,
			"owned_by": "metapi",
		})
	}
	return map[string]any{
		"object": "list",
		"data":   items,
	}
}

// buildClaudeModelsResponse builds Claude-format models response.
func buildClaudeModelsResponse(models []string) map[string]any {
	items := make([]map[string]any, 0, len(models))
	for _, m := range models {
		items = append(items, map[string]any{
			"id":           m,
			"display_name": m,
			"type":         "model",
		})
	}
	return map[string]any{
		"data": items,
	}
}

// getAvailableModels returns the list of available model names.
// Stub: returns common model names.
func getAvailableModels(policy auth.DownstreamRoutingPolicy) []string {
	// Stub model list. In production, this queries the tokenRouter.
	models := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4-turbo",
		"gpt-3.5-turbo",
		"claude-sonnet-4-20250514",
		"claude-3-5-sonnet-latest",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
	}
	if len(policy.SupportedModels) == 0 && len(policy.AllowedRouteIDs) == 0 {
		if policy.DenyAllWhenEmpty {
			return []string{}
		}
		return models
	}
	if len(policy.SupportedModels) == 0 {
		return []string{}
	}

	filtered := make([]string, 0, len(models))
	for _, model := range models {
		if IsModelAllowedByPolicy(model, policy) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// IsModelAllowedByPolicy checks if a model is allowed by the downstream policy.
func IsModelAllowedByPolicy(requestedModel string, policy auth.DownstreamRoutingPolicy) bool {
	if len(policy.SupportedModels) == 0 && len(policy.AllowedRouteIDs) == 0 {
		if policy.DenyAllWhenEmpty {
			return false
		}
		return true
	}

	if len(policy.SupportedModels) > 0 {
		for _, m := range policy.SupportedModels {
			if matchModelPattern(requestedModel, m) {
				return true
			}
		}
		return false
	}

	// AllowedRouteIDs is checked by the token router at channel selection time
	return true
}

// matchModelPattern does simple wildcard matching for model patterns.
func matchModelPattern(model, pattern string) bool {
	if pattern == "*" || pattern == model {
		return true
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(model, strings.TrimPrefix(pattern, "*"))
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// Ensure auth/policy imports are used
var _ = auth.ProxyAuthContext{}
