package proxy

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterFilesRoutes registers file proxy routes.
// Mounted under /v1, so paths omit the /v1 prefix.
func RegisterFilesRoutes(r chi.Router) {
	r.Post("/files", HandleFilesUpload)
	r.Get("/files/{fileId}/content", HandleFilesDownload)
	r.Get("/files/{fileId}", HandleFilesInfo)
	r.Get("/files", HandleFilesList)
	r.Delete("/files/{fileId}", HandleFilesDelete)
}

// HandleFilesUpload handles POST /v1/files (file upload).
func HandleFilesUpload(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, 501, "files upload not yet implemented", "server_error")
}

// HandleFilesDownload handles GET /v1/files/{fileId}/content.
func HandleFilesDownload(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, 501, "files download not yet implemented", "server_error")
}

// HandleFilesInfo handles GET /v1/files/{fileId}.
func HandleFilesInfo(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, 501, "files info not yet implemented", "server_error")
}

// HandleFilesList handles GET /v1/files.
func HandleFilesList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"object": "list",
		"data":   []any{},
	})
}

// HandleFilesDelete handles DELETE /v1/files/{fileId}.
func HandleFilesDelete(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, 501, "files delete not yet implemented", "server_error")
}
