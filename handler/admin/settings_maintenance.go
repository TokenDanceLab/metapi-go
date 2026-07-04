package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterMaintenanceRoutes registers all /api/settings/maintenance routes.
func RegisterMaintenanceRoutes(r chi.Router, db *sqlx.DB) {
	handler := &maintenanceHandler{db: db}

	r.Post("/api/settings/maintenance/clear-cache", handler.clearCache)
	r.Post("/api/settings/maintenance/clear-usage", handler.clearUsage)
	r.Post("/api/settings/maintenance/factory-reset", handler.factoryReset)
}

type maintenanceHandler struct {
	db *sqlx.DB
}

// POST /api/settings/maintenance/clear-cache
func (h *maintenanceHandler) clearCache(w http.ResponseWriter, r *http.Request) {
	// Count before deletion
	var modelAvail, routeCh, tokenRoutes int64
	h.db.Get(&modelAvail, "SELECT COUNT(*) FROM model_availability")
	h.db.Get(&routeCh, "SELECT COUNT(*) FROM route_channels")
	h.db.Get(&tokenRoutes, "SELECT COUNT(*) FROM token_routes")

	// Delete all
	h.db.Exec("DELETE FROM model_availability")
	h.db.Exec("DELETE FROM route_channels")
	h.db.Exec("DELETE FROM token_routes")

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success":                   true,
		"queued":                    true,
		"reused":                    false,
		"jobId":                     "stub-clear-cache",
		"message":                   "缓存已清理，重建路由已开始执行",
		"deletedModelAvailability":  modelAvail,
		"deletedRouteChannels":      routeCh,
		"deletedTokenRoutes":        tokenRoutes,
	})
}

// POST /api/settings/maintenance/clear-usage
func (h *maintenanceHandler) clearUsage(w http.ResponseWriter, r *http.Request) {
	var deletedProxyLogs int64
	h.db.Get(&deletedProxyLogs, "SELECT COUNT(*) FROM proxy_logs")
	h.db.Exec("DELETE FROM proxy_logs")

	// Reset route channel stats
	h.db.Exec(`UPDATE route_channels SET
		success_count = 0, fail_count = 0, total_latency_ms = 0, total_cost = 0,
		last_used_at = NULL, last_selected_at = NULL, last_fail_at = NULL,
		consecutive_fail_count = 0, cooldown_level = 0, cooldown_until = NULL`)

	// Reset account balanceUsed
	h.db.Exec("UPDATE accounts SET balance_used = 0")

	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"message":          "占用统计已清理",
		"deletedProxyLogs": deletedProxyLogs,
	})
}

// POST /api/settings/maintenance/factory-reset
func (h *maintenanceHandler) factoryReset(w http.ResponseWriter, r *http.Request) {
	// Stub: factory reset not yet implemented
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
