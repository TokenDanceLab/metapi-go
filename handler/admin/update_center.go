package admin

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterUpdateCenterRoutes registers all /api/update-center routes.
func RegisterUpdateCenterRoutes(r chi.Router) {
	handler := &updateCenterHandler{}

	r.Get("/api/update-center/status", handler.status)
	r.Post("/api/update-center/check", handler.check)
	r.Put("/api/update-center/config", handler.saveConfig)
	r.Post("/api/update-center/deploy", handler.deploy)
	r.Post("/api/update-center/rollback", handler.rollback)
	r.Get("/api/update-center/tasks/{id}/stream", handler.taskStream)
}

type updateCenterHandler struct{}

// GET /api/update-center/status
func (h *updateCenterHandler) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"currentVersion":  "0.0.0",
		"latestVersion":   "0.0.0",
		"updateAvailable": false,
		"lastCheckedAt":   nil,
	})
}

// POST /api/update-center/check
func (h *updateCenterHandler) check(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"currentVersion":  "0.0.0",
		"latestVersion":   "0.0.0",
		"updateAvailable": false,
		"lastCheckedAt":   nil,
	})
}

// PUT /api/update-center/config
func (h *updateCenterHandler) saveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled             *bool   `json:"enabled"`
		HelperBaseUrl       *string `json:"helperBaseUrl"`
		Namespace           *string `json:"namespace"`
		ReleaseName         *string `json:"releaseName"`
		ChartRef            *string `json:"chartRef"`
		ImageRepository     *string `json:"imageRepository"`
		DefaultDeploySource *string `json:"defaultDeploySource"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	enableVal := false
	if body.Enabled != nil {
		enableVal = *body.Enabled
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"config": map[string]any{
			"enabled":             enableVal,
			"helperBaseUrl":       coalesceStr(body.HelperBaseUrl, ""),
			"namespace":           coalesceStr(body.Namespace, ""),
			"releaseName":         coalesceStr(body.ReleaseName, ""),
			"chartRef":            coalesceStr(body.ChartRef, ""),
			"imageRepository":     coalesceStr(body.ImageRepository, ""),
			"defaultDeploySource": coalesceStr(body.DefaultDeploySource, "docker-hub-tag"),
		},
	})
}

// POST /api/update-center/deploy
func (h *updateCenterHandler) deploy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source        *string `json:"source"`
		TargetTag     *string `json:"targetTag"`
		TargetDigest  *string `json:"targetDigest"`
		TargetVersion *string `json:"targetVersion"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	targetTag := ""
	if body.TargetTag != nil {
		targetTag = strings.TrimSpace(*body.TargetTag)
	}
	if targetTag == "" && body.TargetVersion != nil {
		targetTag = strings.TrimSpace(*body.TargetVersion)
	}
	if targetTag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "targetTag is required",
		})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"reused":  false,
		"task": map[string]any{
			"id":     "stub-deploy",
			"status": "pending",
		},
	})
}

// POST /api/update-center/rollback
func (h *updateCenterHandler) rollback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetRevision *string `json:"targetRevision"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	targetRevision := ""
	if body.TargetRevision != nil {
		targetRevision = strings.TrimSpace(*body.TargetRevision)
	}
	if targetRevision == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "targetRevision is required",
		})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"reused":  false,
		"task": map[string]any{
			"id":     "stub-rollback",
			"status": "pending",
		},
	})
}

// GET /api/update-center/tasks/:id/stream
func (h *updateCenterHandler) taskStream(w http.ResponseWriter, r *http.Request) {
	// SSE stream for task logs
	idStr := chi.URLParam(r, "id")
	_ = idStr

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	// Send done event immediately (stub)
	sseWrite(w, flusher, "done", map[string]any{"status": "stub"})
}
