package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterStatsRoutes registers all /api/stats and /api/models routes.
func RegisterStatsRoutes(r chi.Router, db *sqlx.DB) {
	handler := &statsHandler{db: db}

	r.Get("/api/stats/dashboard", handler.dashboard)
	r.Get("/api/stats/proxy-logs", handler.proxyLogs)
	r.Get("/api/stats/proxy-logs/{id}", handler.proxyLogDetail)
	r.Get("/api/stats/proxy-debug/traces", handler.debugTraces)
	r.Get("/api/stats/proxy-debug/traces/{id}", handler.debugTraceDetail)
	r.Get("/api/stats/site-distribution", handler.siteDistribution)
	r.Get("/api/stats/site-trend", handler.siteTrend)
	r.Get("/api/stats/model-by-site", handler.modelBySite)

	// Model routes under /api/models
	r.Get("/api/models/marketplace", handler.marketplace)
	r.Get("/api/models/token-candidates", handler.tokenCandidates)
	r.Post("/api/models/check/{accountId}", handler.modelCheck)
	r.Post("/api/models/probe", handler.modelProbe)
}

type statsHandler struct {
	db *sqlx.DB
}

// ---- Dashboard ----
// GET /api/stats/dashboard?refresh=&view=
func (h *statsHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	view := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("view")))
	if view != "summary" && view != "insights" {
		view = "full"
	}

	// Set cache headers
	w.Header().Set("x-dashboard-summary-cache", "miss")
	w.Header().Set("x-dashboard-insights-cache", "miss")

	// Stub: return basic stats
	generatedAt := nowUTC()

	var siteCount, accountCount int
	h.db.Get(&siteCount, "SELECT COUNT(*) FROM sites")
	h.db.Get(&accountCount, "SELECT COUNT(*) FROM accounts")

	result := map[string]any{
		"generatedAt": generatedAt,
		"siteCount":   siteCount,
		"accountCount": accountCount,
	}

	if view == "summary" || view == "full" {
		result["siteCount"] = siteCount
		result["accountCount"] = accountCount
	}

	if view == "insights" || view == "full" {
		// Add insights fields
		var totalTokens float64
		h.db.Get(&totalTokens, "SELECT COALESCE(SUM(COALESCE(total_tokens, 0)), 0) FROM proxy_logs")
		result["totalTokens"] = totalTokens

		var totalCost float64
		h.db.Get(&totalCost, "SELECT COALESCE(SUM(COALESCE(estimated_cost, 0)), 0) FROM proxy_logs")
		result["totalCost"] = totalCost
	}

	writeJSON(w, http.StatusOK, result)
}

// ---- Proxy Logs ----
// GET /api/stats/proxy-logs?view=&limit=&offset=&status=&search=&client=&siteId=&from=&to=
func (h *statsHandler) proxyLogs(w http.ResponseWriter, r *http.Request) {
	view := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("view")))
	if view != "query" && view != "meta" {
		view = "full"
	}

	limit := clampInt(getQueryInt(r, "limit", 50), 1, 100)
	offset := maxInt(0, getQueryInt(r, "offset", 0))
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	search := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("search")))
	siteID := getQueryInt(r, "siteId", 0)

	var conditions []string
	var args []any

	if status == "success" {
		conditions = append(conditions, "pl.status = 'success'")
	} else if status == "failed" {
		conditions = append(conditions, "COALESCE(pl.status, '') <> 'success'")
	}
	if search != "" {
		conditions = append(conditions, "(LOWER(COALESCE(pl.model_requested, '')) LIKE ? OR LOWER(COALESCE(pl.model_actual, '')) LIKE ?)")
		like := "%" + search + "%"
		args = append(args, like, like)
	}
	if siteID > 0 {
		conditions = append(conditions, "s.id = ?")
		args = append(args, siteID)
	}

	var where string
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	queryPayload := map[string]any{
		"items":    []any{},
		"total":    0,
		"page":     (offset / limit) + 1,
		"pageSize": limit,
	}
	metaPayload := map[string]any{
		"clientOptions": []any{},
		"summary": map[string]any{
			"totalCount":    0,
			"successCount":  0,
			"failedCount":   0,
			"totalCost":     0.0,
			"totalTokensAll": 0,
		},
		"sites": []any{},
	}

	if view == "query" || view == "full" {
		query := `SELECT pl.*, a.username, s.id as site_id, s.name as site_name, s.url as site_url
			FROM proxy_logs pl
			LEFT JOIN accounts a ON pl.account_id = a.id
			LEFT JOIN sites s ON a.site_id = s.id` + where +
			" ORDER BY pl.created_at DESC LIMIT ? OFFSET ?"
		qArgs := make([]any, len(args))
		copy(qArgs, args)
		qArgs = append(qArgs, limit, offset)
		items := queryRows(h.db, query, qArgs...)
		queryPayload["items"] = normalizeSlice(items)

		var total int
		countQuery := "SELECT COUNT(*) FROM proxy_logs pl LEFT JOIN accounts a ON pl.account_id = a.id LEFT JOIN sites s ON a.site_id = s.id" + where
		h.db.Get(&total, countQuery, args...)
		queryPayload["total"] = total
	}

	if view == "meta" || view == "full" {
		summaryQuery := `SELECT COUNT(*) as total_count,
			COALESCE(SUM(CASE WHEN pl.status = 'success' THEN 1 ELSE 0 END), 0) as success_count,
			COALESCE(SUM(CASE WHEN COALESCE(pl.status, '') <> 'success' THEN 1 ELSE 0 END), 0) as failed_count,
			COALESCE(SUM(COALESCE(pl.estimated_cost, 0)), 0) as total_cost,
			COALESCE(SUM(COALESCE(pl.total_tokens, 0)), 0) as total_tokens_all
			FROM proxy_logs pl
			LEFT JOIN accounts a ON pl.account_id = a.id
			LEFT JOIN sites s ON a.site_id = s.id` + where
		metaArgs := make([]any, len(args))
		copy(metaArgs, args)

		var summary struct {
			TotalCount    int     `db:"total_count"`
			SuccessCount  int     `db:"success_count"`
			FailedCount   int     `db:"failed_count"`
			TotalCost     float64 `db:"total_cost"`
			TotalTokensAll int64  `db:"total_tokens_all"`
		}
		h.db.Get(&summary, summaryQuery, metaArgs...)
		metaPayload["summary"] = map[string]any{
			"totalCount":    summary.TotalCount,
			"successCount":  summary.SuccessCount,
			"failedCount":   summary.FailedCount,
			"totalCost":     roundMicro(summary.TotalCost),
			"totalTokensAll": summary.TotalTokensAll,
		}

		sites := queryRows(h.db, "SELECT id, name, status FROM sites")
		metaPayload["sites"] = normalizeSlice(sites)
	}

	if view == "query" {
		writeJSON(w, http.StatusOK, queryPayload)
	} else if view == "meta" {
		writeJSON(w, http.StatusOK, metaPayload)
	} else {
		result := queryPayload
		result["clientOptions"] = metaPayload["clientOptions"]
		result["summary"] = metaPayload["summary"]
		result["sites"] = metaPayload["sites"]
		writeJSON(w, http.StatusOK, result)
	}
}

// ---- Proxy Log Detail ----
// GET /api/stats/proxy-logs/:id
func (h *statsHandler) proxyLogDetail(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "proxy log id is invalid"})
		return
	}

	row := queryRow(h.db,
		`SELECT pl.*, a.username, s.id as site_id, s.name as site_name, s.url as site_url
		 FROM proxy_logs pl
		 LEFT JOIN accounts a ON pl.account_id = a.id
		 LEFT JOIN sites s ON a.site_id = s.id
		 WHERE pl.id = ?`, id)
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "proxy log not found"})
		return
	}

	// Parse billing details if present
	if bd, ok := row["billingDetails"]; ok {
		if bdStr, ok2 := bd.(string); ok2 && bdStr != "" {
			var parsed any
			if err := json.Unmarshal([]byte(bdStr), &parsed); err == nil {
				row["billingDetails"] = parsed
			}
		}
	}

	writeJSON(w, http.StatusOK, row)
}

// ---- Debug Traces ----
// GET /api/stats/proxy-debug/traces?limit=
func (h *statsHandler) debugTraces(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(getQueryInt(r, "limit", 50), 1, 100)

	rows := queryRows(h.db, "SELECT * FROM proxy_debug_traces ORDER BY created_at DESC LIMIT ?", limit)
	writeJSON(w, http.StatusOK, map[string]any{"items": normalizeSlice(rows)})
}

// ---- Debug Trace Detail ----
// GET /api/stats/proxy-debug/traces/:id
func (h *statsHandler) debugTraceDetail(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "proxy debug trace id is invalid"})
		return
	}

	row := queryRow(h.db, "SELECT * FROM proxy_debug_traces WHERE id = ?", id)
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "proxy debug trace not found"})
		return
	}

	// Load related attempts
	attempts := queryRows(h.db, "SELECT * FROM proxy_debug_attempts WHERE trace_id = ? ORDER BY attempt_index ASC", id)
	row["attempts"] = normalizeSlice(attempts)

	writeJSON(w, http.StatusOK, row)
}

// ---- Site Distribution ----
// GET /api/stats/site-distribution?days=&refresh=
func (h *statsHandler) siteDistribution(w http.ResponseWriter, r *http.Request) {
	days := getQueryInt(r, "days", 7)
	if days < 1 {
		days = 7
	}

	// Stub: return empty distribution
	_ = days
	writeJSON(w, http.StatusOK, map[string]any{"distribution": []any{}})
}

// ---- Site Trend ----
// GET /api/stats/site-trend?days=&refresh=
func (h *statsHandler) siteTrend(w http.ResponseWriter, r *http.Request) {
	days := getQueryInt(r, "days", 7)
	if days < 1 {
		days = 7
	}

	// Stub: return empty trend
	_ = days
	writeJSON(w, http.StatusOK, map[string]any{"trend": []any{}})
}

// ---- Model by Site ----
// GET /api/stats/model-by-site?siteId=&days=
func (h *statsHandler) modelBySite(w http.ResponseWriter, r *http.Request) {
	days := getQueryInt(r, "days", 7)
	if days < 1 {
		days = 7
	}
	siteID := getQueryInt(r, "siteId", 0)

	// Stub: query model_day_usage
	query := "SELECT model, SUM(total_calls) as calls, SUM(total_spend) as spend, SUM(total_tokens) as tokens FROM model_day_usage"
	var args []any
	if siteID > 0 {
		query += " WHERE site_id = ?"
		args = append(args, siteID)
	}
	query += " GROUP BY model ORDER BY calls DESC"

	rows := queryRows(h.db, query, args...)
	writeJSON(w, http.StatusOK, map[string]any{"models": normalizeSlice(rows)})
}

// ---- Model Marketplace ----
// GET /api/models/marketplace?refresh=&includePricing=
func (h *statsHandler) marketplace(w http.ResponseWriter, r *http.Request) {
	// Stub: market place not yet implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"models": []any{},
		"meta": map[string]any{
			"refreshRequested": false,
			"refreshQueued":    false,
			"refreshReused":    false,
			"refreshRunning":   false,
			"refreshJobId":     nil,
			"includePricing":   false,
		},
	})
}

// ---- Token Candidates ----
// GET /api/models/token-candidates
func (h *statsHandler) tokenCandidates(w http.ResponseWriter, r *http.Request) {
	// Stub: token candidates not yet implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"models":                    map[string]any{},
		"modelsWithoutToken":        map[string]any{},
		"modelsMissingTokenGroups":  map[string]any{},
		"endpointTypesByModel":      map[string]any{},
	})
}

// ---- Model Check ----
// POST /api/models/check/:accountId
func (h *statsHandler) modelCheck(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "accountId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   "Invalid account id",
		})
		return
	}

	// Stub: model refresh not yet implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"refresh": map[string]any{"id": id},
		"rebuild": map[string]any{},
	})
}

// ---- Model Probe ----
// POST /api/models/probe
func (h *statsHandler) modelProbe(w http.ResponseWriter, r *http.Request) {
	// Stub: model probe not yet implemented
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"jobId":   "stub-probe",
		"status":  "pending",
		"message": "已开始模型可用性探测，请稍后查看任务列表",
	})
}

func queryRow(db *sqlx.DB, query string, args ...any) map[string]any {
	rows, err := db.Queryx(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	if rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			return nil
		}
		return mapKeysToCamel(row)
	}
	return nil
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func roundMicro(v float64) float64 {
	return float64(int64(v*1_000_000)) / 1_000_000
}
