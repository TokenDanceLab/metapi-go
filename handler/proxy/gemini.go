package proxyhandler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterGeminiRoutes registers all Gemini surface routes.
// 7 routes total: models list (2), generateContent (2), CLI internal (3).
func RegisterGeminiRoutes(r chi.Router) {
	// Gemini model list
	r.Get("/v1beta/models", HandleGeminiModelsList)
	r.Get("/gemini/{geminiApiVersion}/models", HandleGeminiModelsListDynamic)

	// Gemini generateContent
	r.Post("/v1beta/models/*", HandleGeminiGenerateContent)
	r.Post("/gemini/{geminiApiVersion}/models/*", HandleGeminiGenerateContentDynamic)

	// Gemini CLI internal paths
	r.Post("/v1internal::generateContent", HandleGeminiCLIGenerateContent)
	r.Post("/v1internal::streamGenerateContent", HandleGeminiCLIStreamGenerateContent)
	r.Post("/v1internal::countTokens", HandleGeminiCLICountTokens)
}

// geminiSupportedGenerationMethods is the fixed method set advertised for owned
// Gemini list entries. Typed as []any so writeJSON/appendJSON encodes a JSON
// array (the lightweight encoder does not special-case []string).
// GenerateContent paths still route through dispatchUpstream; this list only
// describes client-visible capabilities.
var geminiSupportedGenerationMethods = []any{
	"generateContent",
	"streamGenerateContent",
	"countTokens",
}

// HandleGeminiModelsList handles GET /v1beta/models.
// Returns a Gemini-shaped models list built from the MetAPI-owned catalog
// (resolveOwnedModelCatalog / AvailableModelsSource), not a hard-coded stub and
// not a live upstream Generative Language models scrape. See
// docs/analysis/gemini-models-list.md.
func HandleGeminiModelsList(w http.ResponseWriter, r *http.Request) {
	authCtx := GetProxyAuth(r)
	if authCtx == nil {
		writeJSONError(w, 401, "unauthorized", "invalid_request_error")
		return
	}

	models := getAvailableModels(r.Context(), authCtx.Policy)
	writeJSON(w, 200, buildGeminiModelsResponse(selectGeminiListModels(models)))
}

// selectGeminiListModels prefers catalog entries that look Gemini-related when
// the owned catalog is mixed (OpenAI/Claude/Gemini). If no gemini-ish names are
// present, the full owned catalog is returned so an empty list is reserved for
// truly empty / deny-all / listing-error cases.
func selectGeminiListModels(models []string) []string {
	if len(models) == 0 {
		return []string{}
	}
	gemini := make([]string, 0, len(models))
	for _, m := range models {
		if isGeminiishModelName(m) {
			gemini = append(gemini, m)
		}
	}
	if len(gemini) > 0 {
		return gemini
	}
	return models
}

func isGeminiishModelName(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "gemini")
}

// buildGeminiModelsResponse maps owned model IDs into the Gemini models.list
// envelope used by Generative Language API clients:
//
//	{ "models": [ { "name": "models/<id>", "displayName", "description?",
//	               "supportedGenerationMethods": [...] } ] }
func buildGeminiModelsResponse(models []string) map[string]any {
	items := make([]map[string]any, 0, len(models))
	for _, m := range models {
		id := normalizeGeminiModelID(m)
		if id == "" {
			continue
		}
		display := geminiDisplayName(id)
		items = append(items, map[string]any{
			"name":                       "models/" + id,
			"displayName":                display,
			"description":                display + " model",
			"supportedGenerationMethods": append([]any(nil), geminiSupportedGenerationMethods...),
		})
	}
	return map[string]any{
		"models": items,
	}
}

// normalizeGeminiModelID strips optional "models/" resource prefixes and blanks.
func normalizeGeminiModelID(model string) string {
	id := strings.TrimSpace(model)
	id = strings.TrimPrefix(id, "models/")
	return strings.TrimSpace(id)
}

// geminiDisplayName produces a lightweight human label from a model id
// (e.g. gemini-2.5-pro → "Gemini 2.5 Pro"). Non-hyphenated ids pass through.
func geminiDisplayName(id string) string {
	id = normalizeGeminiModelID(id)
	if id == "" || !strings.Contains(id, "-") {
		return id
	}
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		// Keep purely numeric / dotted version tokens as-is (2.5, 1.5, …).
		if p[0] >= '0' && p[0] <= '9' {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// HandleGeminiModelsListDynamic handles GET /gemini/:geminiApiVersion/models.
func HandleGeminiModelsListDynamic(w http.ResponseWriter, r *http.Request) {
	// Same as static path, just with dynamic API version
	apiVersion := chi.URLParam(r, "geminiApiVersion")
	if apiVersion == "" {
		apiVersion = "v1beta"
	}
	_ = apiVersion
	HandleGeminiModelsList(w, r)
}

// HandleGeminiGenerateContent handles POST /v1beta/models/*.
// Parses {apiVersion}/models/{model}:{action} from the path.
// Official Gemini clients put the model in the path (not body) and imply
// streaming via the :streamGenerateContent action (issue #515).
func HandleGeminiGenerateContent(w http.ResponseWriter, r *http.Request) {
	pathModel, forceStream := geminiPathModelAndStream(r.URL.Path)
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "gemini",
		DownstreamPath: r.URL.Path,
		RequireModel:   false,
		// Path model fills RequestedModel when body omits model (channel selection).
		DefaultModel: pathModel,
		ForceStream:  forceStream,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleGeminiGenerateContentDynamic handles POST /gemini/:geminiApiVersion/models/*.
func HandleGeminiGenerateContentDynamic(w http.ResponseWriter, r *http.Request) {
	apiVersion := chi.URLParam(r, "geminiApiVersion")
	if apiVersion == "" {
		apiVersion = "v1beta"
	}
	_ = apiVersion
	HandleGeminiGenerateContent(w, r)
}

// HandleGeminiCLIGenerateContent handles POST /v1internal::generateContent.
// Gemini CLI internal protocol. downstreamProtocol: "gemini-cli", action: "generateContent".
func HandleGeminiCLIGenerateContent(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "gemini-cli",
		DownstreamPath: "/v1internal::generateContent",
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleGeminiCLIStreamGenerateContent handles POST /v1internal::streamGenerateContent.
// Path action forces stream even when body omits stream:true (issue #515).
func HandleGeminiCLIStreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "gemini-cli",
		DownstreamPath: "/v1internal::streamGenerateContent",
		RequireModel:   true,
		ForceStream:    true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// geminiPathModelAndStream extracts path model + stream-action force for native
// Gemini generateContent paths. Body model still wins via PrepareCtx DefaultModel.
func geminiPathModelAndStream(path string) (model string, forceStream bool) {
	_, rawModel, action := ParseGeminiPath(path)
	model = normalizeGeminiModelID(rawModel)
	forceStream = isGeminiStreamAction(action)
	return model, forceStream
}

// isGeminiStreamAction reports whether a Gemini path/CLI action is streaming.
func isGeminiStreamAction(action string) bool {
	return strings.EqualFold(strings.TrimSpace(action), "streamGenerateContent")
}

// HandleGeminiCLICountTokens handles POST /v1internal::countTokens.
func HandleGeminiCLICountTokens(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "gemini-cli",
		DownstreamPath: "/v1internal::countTokens",
		RequireModel:   false,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// ParseGeminiPath parses a Gemini path into apiVersion, model, action.
// E.g., "/v1beta/models/gemini-2.5-pro:generateContent"
//   -> apiVersion="v1beta", model="gemini-2.5-pro", action="generateContent"
// Also accepts the dynamic surface "/gemini/{apiVersion}/models/{model}:{action}".
func ParseGeminiPath(path string) (apiVersion, model, action string) {
	path = strings.TrimPrefix(path, "/")
	// Strip optional /gemini/ prefix used by HandleGeminiGenerateContentDynamic.
	if strings.HasPrefix(path, "gemini/") {
		path = strings.TrimPrefix(path, "gemini/")
	}
	parts := strings.SplitN(path, "/", 4)

	if len(parts) >= 1 {
		apiVersion = parts[0]
	}
	if len(parts) >= 3 && parts[1] == "models" {
		modelAction := strings.Join(parts[2:], "/")
		if idx := strings.LastIndex(modelAction, ":"); idx >= 0 {
			model = modelAction[:idx]
			action = modelAction[idx+1:]
		} else {
			model = modelAction
		}
	}

	return
}
