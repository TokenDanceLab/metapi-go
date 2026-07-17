package proxyhandler

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/auth"
)

// modelsOwnedBy is the OpenAI-compatible owned_by value for MetAPI-owned listings.
// Clients that special-case owned_by (e.g. Hermes llama.cpp detection) treat this
// as a generic OpenAI-compatible gateway, not llamacpp.
const modelsOwnedBy = "metapi"

// AvailableModelsSource is implemented by routers that can list MetAPI-owned models
// from enabled token_routes (notably routing.TokenRouter.GetAvailableModels).
// Channel-selection-only mocks may omit this method; listing then uses the
// documented last-resort catalog (see resolveOwnedModelCatalog).
type AvailableModelsSource interface {
	GetAvailableModels(ctx context.Context) ([]string, error)
}

var emptyModelsCatalogLogOnce sync.Once

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

	// Build model list (MetAPI-owned listing; see docs/analysis/models-response-shape.md)
	models := getAvailableModels(r.Context(), authCtx.Policy)
	now := time.Now().UTC()

	if wantsClaude {
		writeJSON(w, 200, buildClaudeModelsResponse(models, now))
	} else {
		writeJSON(w, 200, buildOpenAIModelsResponse(models, now))
	}
}

// buildOpenAIModelsResponse builds OpenAI-format models response:
//
//	{ "object": "list", "data": [ { "id", "object":"model", "created", "owned_by", ... } ] }
//
// Optional context_length is included when known so OpenAI-compatible clients
// (Hermes and similar) can auto-detect context windows from /v1/models.
func buildOpenAIModelsResponse(models []string, now time.Time) map[string]any {
	items := make([]map[string]any, 0, len(models))
	created := now.Unix()
	for _, m := range models {
		item := map[string]any{
			"id":       m,
			"object":   "model",
			"created":  created,
			"owned_by": modelsOwnedBy,
		}
		if ctxLen, ok := knownModelContextLength(m); ok {
			item["context_length"] = ctxLen
		}
		items = append(items, item)
	}
	return map[string]any{
		"object": "list",
		"data":   items,
	}
}

// buildClaudeModelsResponse builds Claude-format models response matching
// Anthropic Models API pagination fields used by upstream metapi:
//
//	{ "data": [...], "first_id", "last_id", "has_more": false }
func buildClaudeModelsResponse(models []string, now time.Time) map[string]any {
	items := make([]map[string]any, 0, len(models))
	createdAt := now.UTC().Format(time.RFC3339Nano)
	// Prefer millisecond precision like upstream ISO strings; trim trailing zeros noise
	// by using RFC3339 when nanoseconds are zero.
	if now.Nanosecond() == 0 {
		createdAt = now.UTC().Format(time.RFC3339)
	}
	for _, m := range models {
		items = append(items, map[string]any{
			"id":           m,
			"display_name": m,
			"type":         "model",
			"created_at":   createdAt,
		})
	}

	var firstID any = nil
	var lastID any = nil
	if len(models) > 0 {
		firstID = models[0]
		lastID = models[len(models)-1]
	}

	return map[string]any{
		"data":      items,
		"first_id":  firstID,
		"last_id":   lastID,
		"has_more":  false,
	}
}

// getAvailableModels returns the list of available model names for this caller.
// This is the MetAPI-owned listing path (not a live upstream /v1/models proxy):
// resolveOwnedModelCatalog loads route-backed names when TokenRouter is wired,
// then downstream routing policy filters the catalog.
func getAvailableModels(ctx context.Context, policy auth.DownstreamRoutingPolicy) []string {
	models := resolveOwnedModelCatalog(ctx)
	if len(policy.SupportedModels) == 0 && len(policy.AllowedRouteIDs) == 0 {
		if policy.DenyAllWhenEmpty {
			return []string{}
		}
		return models
	}
	// SupportedModels empty but AllowedRouteIDs set: keep empty until listing can
	// join route IDs (AllowedRouteIDs is enforced at channel selection today).
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

// resolveOwnedModelCatalog loads the MetAPI-owned model name catalog.
//
// Priority:
//  1. UpstreamConfig.Router implementing AvailableModelsSource (TokenRouter.GetAvailableModels)
//  2. Last-resort stub catalog when METAPI_ENABLE_PROXY_STUB is on (unit tests),
//     or when a channel-selection router is present but does not implement listing
//     (e2e / selection mocks that only wire SelectChannel*)
//  3. Empty list with a one-shot warning when no router is configured in production
//
// This never scrapes live upstream /v1/models.
func resolveOwnedModelCatalog(ctx context.Context) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := getUpstreamConfig()
	if src := availableModelsSourceFromConfig(cfg); src != nil {
		models, err := src.GetAvailableModels(ctx)
		if err != nil {
			slog.Warn("getAvailableModels: router listing failed; returning empty catalog", "err", err)
			return []string{}
		}
		return normalizeModelCatalog(models)
	}
	// Router is wired for channel selection but does not expose listing (common in
	// selection-only test doubles). Prefer a stable last-resort catalog over empty
	// so OpenAI clients still receive a shape-compatible owned list.
	if cfg != nil && cfg.Router != nil {
		return lastResortStubCatalog()
	}
	if isProxyStubEnabled() {
		return lastResortStubCatalog()
	}
	emptyModelsCatalogLogOnce.Do(func() {
		slog.Warn("getAvailableModels: no AvailableModelsSource wired; returning empty model list")
	})
	return []string{}
}

// availableModelsSourceFromConfig returns the optional listing capability on the
// configured TokenRouter. Kept as a thin UpstreamConfig accessor so models listing
// does not require expanding proxy.TokenRouterInterface (channel-selection surface).
func availableModelsSourceFromConfig(cfg *UpstreamConfig) AvailableModelsSource {
	if cfg == nil || cfg.Router == nil {
		return nil
	}
	src, _ := cfg.Router.(AvailableModelsSource)
	return src
}

// lastResortStubCatalog is used only when route-backed listing is unavailable
// (proxy stub tests / selection-only mocks). Production with TokenRouter uses
// GetAvailableModels instead.
func lastResortStubCatalog() []string {
	return []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4-turbo",
		"gpt-3.5-turbo",
		"claude-sonnet-4-20250514",
		"claude-3-5-sonnet-latest",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
	}
}

// normalizeModelCatalog drops blanks, de-duplicates, and sorts for stable responses.
func normalizeModelCatalog(models []string) []string {
	if len(models) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		name := strings.TrimSpace(m)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// knownModelContextLength returns a published context window for well-known models
// in the owned listing. Unknown models omit the field (clients fall back to their
// own defaults/probes). Values are token counts, not bytes.
//
// When route-level token_routes.context_length is wired into this handler, that
// value should take precedence over these defaults.
func knownModelContextLength(model string) (int64, bool) {
	// Exact matches first for the owned stub catalog.
	switch model {
	case "gpt-4o", "gpt-4o-mini", "gpt-4-turbo":
		return 128000, true
	case "gpt-3.5-turbo":
		return 16385, true
	case "claude-sonnet-4-20250514", "claude-3-5-sonnet-latest":
		return 200000, true
	case "gemini-2.5-pro", "gemini-2.5-flash":
		return 1048576, true
	}

	// Light prefix heuristics for common families so managed-key wildcards still
	// surface a usable context_length for OpenAI-compatible clients.
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "gpt-4o"),
		strings.HasPrefix(lower, "gpt-4-turbo"),
		strings.HasPrefix(lower, "gpt-4.1"),
		strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"):
		return 128000, true
	case strings.HasPrefix(lower, "gpt-3.5"):
		return 16385, true
	case strings.HasPrefix(lower, "claude-"):
		return 200000, true
	case strings.HasPrefix(lower, "gemini-"):
		return 1048576, true
	default:
		return 0, false
	}
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
