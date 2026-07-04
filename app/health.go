package app

import (
	"net/http"
)

// Health handles GET /health — a design addition for K8s/Docker health checks.
// TS does not have this endpoint; this is a Go-native decision.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
