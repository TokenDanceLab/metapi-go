package proxyhandler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// defaultFilesModel is used when the client does not provide a model for channel
// selection. OpenAI Files API itself is model-agnostic; MetAPI still needs a
// model key so TokenRouter can pick a files-capable upstream channel.
// Operators should bind this model (or an override via body/query/header) to a
// channel whose site exposes /v1/files.
const defaultFilesModel = "gpt-4o"

// RegisterFilesRoutes registers OpenAI-compatible /v1/files proxy routes.
// Mounted under /v1, so paths omit the /v1 prefix.
func RegisterFilesRoutes(r chi.Router) {
	r.Post("/files", HandleFilesUpload)
	r.Get("/files/{fileId}/content", HandleFilesDownload)
	r.Get("/files/{fileId}", HandleFilesInfo)
	r.Get("/files", HandleFilesList)
	r.Delete("/files/{fileId}", HandleFilesDelete)
}

// HandleFilesUpload handles POST /v1/files (multipart file upload).
// Forwards to upstream POST /v1/files after auth + channel selection.
func HandleFilesUpload(w http.ResponseWriter, r *http.Request) {
	EnsureMultipartBufferParser()

	// Multipart is the OpenAI Files upload contract. Reject bare JSON early so
	// clients get a clear 400 instead of an opaque upstream failure.
	if !IsMultipartRequest(r) && r.ContentLength != 0 {
		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if ct != "" && !strings.HasPrefix(ct, "multipart/form-data") {
			writeJSONError(w, http.StatusBadRequest, "multipart/form-data with a file field is required", "invalid_request_error")
			return
		}
	}

	ctx, errResp := prepareFilesCtx(r, "/v1/files")
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	if ctx.IsStream {
		writeJSONError(w, http.StatusBadRequest, "files does not support streaming", "invalid_request_error")
		return
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesList handles GET /v1/files.
// Forwards to upstream GET /v1/files. When no channel can be selected and the
// proxy stub is enabled (tests), returns an empty OpenAI-shaped list.
func HandleFilesList(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := prepareFilesCtx(r, "/v1/files")
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	if ctx.IsStream {
		writeJSONError(w, http.StatusBadRequest, "files does not support streaming", "invalid_request_error")
		return
	}

	// Empty-list fallback only when upstream is intentionally stubbed (tests /
	// demos). Production with wired upstream goes through dispatchUpstream and
	// returns real list/errors.
	if getUpstreamConfig() == nil && isProxyStubEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{
			"object": "list",
			"data":   []any{},
		})
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleFilesInfo handles GET /v1/files/{fileId}.
func HandleFilesInfo(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimSpace(chi.URLParam(r, "fileId"))
	if fileID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing file id", "invalid_request_error")
		return
	}

	ctx, errResp := prepareFilesCtx(r, "/v1/files/"+fileID)
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	if ctx.IsStream {
		writeJSONError(w, http.StatusBadRequest, "files does not support streaming", "invalid_request_error")
		return
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesDownload handles GET /v1/files/{fileId}/content.
// Relays binary/content responses via the standard buffered upstream path.
func HandleFilesDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimSpace(chi.URLParam(r, "fileId"))
	if fileID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing file id", "invalid_request_error")
		return
	}

	ctx, errResp := prepareFilesCtx(r, "/v1/files/"+fileID+"/content")
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	if ctx.IsStream {
		writeJSONError(w, http.StatusBadRequest, "files does not support streaming", "invalid_request_error")
		return
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesDelete handles DELETE /v1/files/{fileId}.
func HandleFilesDelete(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimSpace(chi.URLParam(r, "fileId"))
	if fileID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing file id", "invalid_request_error")
		return
	}

	ctx, errResp := prepareFilesCtx(r, "/v1/files/"+fileID)
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	if ctx.IsStream {
		writeJSONError(w, http.StatusBadRequest, "files does not support streaming", "invalid_request_error")
		return
	}
	dispatchUpstream(w, r, ctx)
}

// prepareFilesCtx builds proxy context for files surfaces.
// Model may come from JSON body, multipart field, query (?model=), or
// X-Metapi-Files-Model header; otherwise defaultFilesModel is used so channel
// selection can proceed for model-agnostic OpenAI Files API calls.
func prepareFilesCtx(r *http.Request, downstreamPath string) (*Ctx, *SurfResult) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "files",
		DownstreamPath: downstreamPath,
		RequireModel:   false,
		DefaultModel:   defaultFilesModel,
	})
	if errResp != nil {
		return nil, errResp
	}

	model := strings.TrimSpace(ctx.RequestedModel)
	if model == "" || model == defaultFilesModel {
		if q := strings.TrimSpace(r.URL.Query().Get("model")); q != "" {
			model = q
		} else if h := strings.TrimSpace(r.Header.Get("X-Metapi-Files-Model")); h != "" {
			model = h
		}
	}
	if model == "" {
		model = defaultFilesModel
	}

	// Policy check when model was resolved from query/header after PrepareCtx.
	if model != ctx.RequestedModel {
		if !IsModelAllowedByPolicy(model, ctx.Policy) {
			return nil, &SurfResult{OK: false, Status: 403, Error: "model not allowed by downstream policy", ErrorType: "invalid_request_error"}
		}
		ctx.RequestedModel = model
		if ctx.Body == nil {
			ctx.Body = map[string]any{}
		}
		ctx.Body["model"] = model
	}

	return ctx, nil
}
