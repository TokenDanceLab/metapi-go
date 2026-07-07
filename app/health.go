package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/tokendancelab/metapi-go/store"
)

var readinessDraining atomic.Bool

type HealthStatus struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

// Health handles GET /health as a liveness endpoint.
// It intentionally returns 200 without touching dependencies: orchestrators
// should use /ready when they need dependency-aware routing decisions.
func Health(w http.ResponseWriter, r *http.Request) {
	writeHealthJSON(w, http.StatusOK, HealthStatus{Status: "ok"})
}

// Ready handles GET /ready as a readiness endpoint. It returns 503 when the
// database is not initialized or cannot be pinged.
func Ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	db := store.GetDB()
	if db == nil {
		slog.Warn("readiness: database not initialized")
		writeHealthJSON(w, http.StatusServiceUnavailable, HealthStatus{Status: "degraded", Database: "error"})
		return
	}

	if err := db.Ping(); err != nil {
		slog.Warn("readiness: database ping failed", "error", err)
		writeHealthJSON(w, http.StatusServiceUnavailable, HealthStatus{Status: "degraded", Database: "error"})
		return
	}

	if readinessDraining.Load() {
		writeHealthJSON(w, http.StatusServiceUnavailable, HealthStatus{Status: "draining", Database: "ok"})
		return
	}

	writeHealthJSON(w, http.StatusOK, HealthStatus{Status: "ok", Database: "ok"})
}

func markReadinessDraining(draining bool) {
	readinessDraining.Store(draining)
}

func setReadinessDrainingForTest(draining bool) {
	markReadinessDraining(draining)
}

func writeHealthJSON(w http.ResponseWriter, statusCode int, status HealthStatus) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(status); err != nil {
		slog.Warn("health: failed to write response", "error", err)
	}
}
