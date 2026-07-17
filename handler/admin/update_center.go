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
// Local status only — remote version discovery is residual.
func (h *updateCenterHandler) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"currentVersion":  "0.0.0",
		"latestVersion":   "0.0.0",
		"updateAvailable": false,
		"lastCheckedAt":   nil,
	})
}

// POST /api/update-center/check
// Local check only — no remote registry polling yet.
func (h *updateCenterHandler) check(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"currentVersion":  "0.0.0",
		"latestVersion":   "0.0.0",
		"updateAvailable": false,
		"lastCheckedAt":   nil,
	})
}

// PUT /api/update-center/config
// Accepts and echoes config for UI round-trip; deploy/rollback remain residual.
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
// Remote binary/Helm deploy is out of process scope — honest 501 residual.
// Never invent stub-deploy task ids.
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

	writeNotImplementedResidual(w,
		"Update-center deploy is not implemented in Go",
		"remote binary/Helm deploy via helper service is out of scope; use external deploy tooling (CI/CD or helper) instead of inventing task ids",
	)
}

// POST /api/update-center/rollback
// Remote rollback is out of process scope — honest 501 residual.
// Never invent stub-rollback task ids.
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

	writeNotImplementedResidual(w,
		"Update-center rollback is not implemented in Go",
		"remote Helm/release rollback via helper service is out of scope; use external deploy tooling instead of inventing task ids",
	)
}

// GET /api/update-center/tasks/:id/stream
// No deploy/rollback task registry exists — honest 501 residual (no fake SSE done).
func (h *updateCenterHandler) taskStream(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimSpace(chi.URLParam(r, "id"))
	_ = idStr

	writeNotImplementedResidual(w,
		"Update-center task SSE stream is not implemented in Go",
		"deploy/rollback tasks are residual; no in-process update-center task registry or SSE log stream exists",
	)
}
