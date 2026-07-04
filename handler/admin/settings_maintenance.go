package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/routing"
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

	routing.InvalidateCache()
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
	// Require confirmation token to prevent accidental invocation.
	var body struct {
		Confirm bool `json:"confirm"`
	}
	// Decode body; if confirm is not true, reject.
	json.NewDecoder(r.Body).Decode(&body)
	if !body.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "工厂重置需要确认。请在请求体中设置 confirm: true",
		})
		return
	}

	tx, err := h.db.Beginx()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": fmt.Sprintf("开启事务失败：%v", err),
		})
		return
	}
	defer tx.Rollback()

	deleted := map[string]int64{}

	// Delete all 27 tables in reverse FK order (children before parents).
	for _, table := range reverseAllTables {
		// Count before deletion
		var count int64
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		if err := tx.Get(&count, countQuery); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"success": false,
				"message": fmt.Sprintf("工厂重置失败：无法读取表 %s：%v", table, err),
			})
			return
		}

		deleteQuery := fmt.Sprintf("DELETE FROM %s", table)
		if _, err := tx.Exec(deleteQuery); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"success": false,
				"message": fmt.Sprintf("工厂重置失败：无法清空表 %s：%v", table, err),
			})
			return
		}

		deleted[table] = count
	}

	// Reset auto-increment sequences.
	// SQLite: DELETE FROM sqlite_sequence removes all autoincrement tracking.
	// PostgreSQL: ALTER SEQUENCE ... RESTART for each table with a serial column.
	// Try SQLite first (silently fails on non-SQLite); then try PG.
	driverName := h.db.DriverName()
	switch driverName {
	case "sqlite", "sqlite3":
		tx.Exec("DELETE FROM sqlite_sequence")
	case "pgx", "postgres":
		for _, table := range allTables {
			seqName := table + "_id_seq"
			tx.Exec(fmt.Sprintf("ALTER SEQUENCE IF EXISTS %s RESTART WITH 1", seqName))
		}
	}
	// For other drivers, skip sequence reset (no-op).

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": fmt.Sprintf("提交事务失败：%v", err),
		})
		return
	}

	routing.InvalidateCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "工厂重置完成",
		"deleted": deleted,
	})
}
