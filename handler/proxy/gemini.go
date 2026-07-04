package proxy

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

// HandleGeminiModelsList handles GET /v1beta/models.
func HandleGeminiModelsList(w http.ResponseWriter, r *http.Request) {
	authCtx := GetProxyAuth(r)
	if authCtx == nil {
		writeJSONError(w, 401, "unauthorized", "invalid_request_error")
		return
	}

	_ = authCtx

	// Gemini models list response
	stubResp := map[string]any{
		"models": []map[string]any{
			{
				"name":                       "models/gemini-2.5-pro",
				"displayName":                "Gemini 2.5 Pro",
				"description":                "Gemini 2.5 Pro model",
				"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"},
			},
			{
				"name":                       "models/gemini-2.5-flash",
				"displayName":                "Gemini 2.5 Flash",
				"description":                "Gemini 2.5 Flash model",
				"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"},
			},
		},
	}
	writeJSON(w, 200, stubResp)
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
func HandleGeminiGenerateContent(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "gemini",
		DownstreamPath: r.URL.Path,
		RequireModel:   false,
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
func HandleGeminiCLIStreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "gemini-cli",
		DownstreamPath: "/v1internal::streamGenerateContent",
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
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
func ParseGeminiPath(path string) (apiVersion, model, action string) {
	path = strings.TrimPrefix(path, "/")
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
