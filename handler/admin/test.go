package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterTestRoutes registers all /api/test routes.
func RegisterTestRoutes(r chi.Router) {
	handler := &testHandler{}

	// Proxy test endpoints
	r.Post("/api/test/proxy", handler.proxyTest)
	r.Post("/api/test/proxy/stream", handler.proxyTestStream)
	r.Post("/api/test/proxy/jobs", handler.proxyTestJob)
	r.Get("/api/test/proxy/jobs/{jobId}", handler.proxyTestJobStatus)
	r.Delete("/api/test/proxy/jobs/{jobId}", handler.proxyTestJobCancel)

	// Chat test endpoints
	r.Post("/api/test/chat", handler.chatTest)
	r.Post("/api/test/chat/stream", handler.chatTestStream)
	r.Post("/api/test/chat/jobs", handler.chatTestJob)
	r.Get("/api/test/chat/jobs/{jobId}", handler.chatTestJobStatus)
	r.Delete("/api/test/chat/jobs/{jobId}", handler.chatTestJobCancel)
}

type testHandler struct{}

// POST /api/test/proxy
func (h *testHandler) proxyTest(w http.ResponseWriter, r *http.Request) {
	// Stub: proxy test not yet wired
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Proxy test is not yet implemented in Go",
	})
}

// POST /api/test/proxy/stream
func (h *testHandler) proxyTestStream(w http.ResponseWriter, r *http.Request) {
	// Stub
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Proxy stream test is not yet implemented in Go",
	})
}

// POST /api/test/proxy/jobs
func (h *testHandler) proxyTestJob(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId":     "stub-job",
		"status":    "pending",
		"createdAt": "",
		"expiresAt": "",
	})
}

// GET /api/test/proxy/jobs/:jobId
func (h *testHandler) proxyTestJobStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]any{
			"message": "job not found",
			"type":    "not_found",
		},
	})
}

// DELETE /api/test/proxy/jobs/:jobId
func (h *testHandler) proxyTestJobCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/test/chat
func (h *testHandler) chatTest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Chat test is not yet implemented in Go",
	})
}

// POST /api/test/chat/stream
func (h *testHandler) chatTestStream(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Chat stream test is not yet implemented in Go",
	})
}

// POST /api/test/chat/jobs
func (h *testHandler) chatTestJob(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId":     "stub-chat-job",
		"status":    "pending",
		"createdAt": "",
		"expiresAt": "",
	})
}

// GET /api/test/chat/jobs/:jobId
func (h *testHandler) chatTestJobStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]any{
			"message": "job not found",
			"type":    "not_found",
		},
	})
}

// DELETE /api/test/chat/jobs/:jobId
func (h *testHandler) chatTestJobCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
