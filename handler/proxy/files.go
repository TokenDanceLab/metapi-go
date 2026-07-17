package proxyhandler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterFilesRoutes registers file proxy routes under /v1.
func RegisterFilesRoutes(r chi.Router) {
	r.Post("/files", HandleFilesUpload)
	r.Get("/files/{fileId}/content", HandleFilesDownload)
	r.Get("/files/{fileId}", HandleFilesInfo)
	r.Get("/files", HandleFilesList)
	r.Delete("/files/{fileId}", HandleFilesDelete)
}

// HandleFilesUpload handles POST /v1/files (multipart or JSON).
func HandleFilesUpload(w http.ResponseWriter, r *http.Request) {
	EnsureMultipartBufferParser()
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "files",
		DownstreamPath: "/v1/files",
		RequireModel:   false,
		DefaultModel:   "gpt-4o-mini",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesDownload handles GET /v1/files/{fileId}/content.
func HandleFilesDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimSpace(filesPathID(r, "fileId"))
	if fileID == "" {
		writeJSONError(w, 400, "fileId is required", "invalid_request_error")
		return
	}
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "files",
		DownstreamPath: "/v1/files/" + fileID + "/content",
		RequireModel:   false,
		DefaultModel:   "gpt-4o-mini",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesInfo handles GET /v1/files/{fileId}.
func HandleFilesInfo(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimSpace(filesPathID(r, "fileId"))
	if fileID == "" {
		writeJSONError(w, 400, "fileId is required", "invalid_request_error")
		return
	}
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "files",
		DownstreamPath: "/v1/files/" + fileID,
		RequireModel:   false,
		DefaultModel:   "gpt-4o-mini",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesList handles GET /v1/files.
func HandleFilesList(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "files",
		DownstreamPath: "/v1/files",
		RequireModel:   false,
		DefaultModel:   "gpt-4o-mini",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	// Preserve query string for purpose/limit filters when present.
	if q := r.URL.RawQuery; q != "" {
		ctx.DownstreamPath = ctx.DownstreamPath + "?" + q
	}
	dispatchUpstream(w, r, ctx)
}

// HandleFilesDelete handles DELETE /v1/files/{fileId}.
func HandleFilesDelete(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimSpace(filesPathID(r, "fileId"))
	if fileID == "" {
		writeJSONError(w, 400, "fileId is required", "invalid_request_error")
		return
	}
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "files",
		DownstreamPath: "/v1/files/" + fileID,
		RequireModel:   false,
		DefaultModel:   "gpt-4o-mini",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}
	dispatchUpstream(w, r, ctx)
}

func filesPathID(r *http.Request, param string) string {
	id := strings.TrimSpace(chi.URLParam(r, param))
	if id != "" {
		return id
	}
	// Fallback when handler is invoked without chi route context (unit tests).
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	// expect .../files/{id}[/content]
	for i := 0; i < len(parts); i++ {
		if parts[i] == "files" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
