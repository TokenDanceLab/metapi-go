package app

import (
	"log/slog"
	"net/http"

	"github.com/tokendancelab/metapi-go/store"
)

// Health handles GET /health -- a K8s/Docker liveness endpoint.
// Always returns HTTP 200 for Kubernetes compatibility, but the JSON body
// signals actual database connectivity:
//   - {"status":"ok","database":"ok"} when the DB is reachable
//   - {"status":"degraded","database":"error"} when the DB is nil or ping fails
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	db := store.GetDB()
	if db == nil {
		slog.Warn("health: database not initialized")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"degraded","database":"error"}`))
		return
	}

	if err := db.Ping(); err != nil {
		slog.Warn("health: database ping failed", "error", err)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"degraded","database":"error"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","database":"ok"}`))
}
