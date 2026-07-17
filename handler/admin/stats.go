package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/scheduler"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
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
	// Usage density heatmap + slow-request ranking (learn #121).
	r.Get("/api/stats/usage-heatmap", handler.usageHeatmap)
	r.Get("/api/stats/slow-requests", handler.slowRequests)
	// Cross-site effective model price comparison (admin).
	// Both paths are registered for discoverability; they share one handler.
	r.Get("/api/stats/model-prices", handler.modelPriceCompare)

	// Model routes under /api/models
	r.Get("/api/models/marketplace", handler.marketplace)
	r.Get("/api/models/price-compare", handler.modelPriceCompare)
	r.Get("/api/models/token-candidates", handler.tokenCandidates)
	r.Post("/api/models/check/{accountId}", handler.modelCheck)
	r.Post("/api/models/probe", handler.modelProbe)
}

type statsHandler struct {
	db *sqlx.DB
}

// effectiveProxyTokensSQL returns a SQL expression that prefers total_tokens
// and falls back to prompt_tokens + completion_tokens. Avoids under-counting
// partial upstream usage payloads and avoids double-counting when both are set.
const effectiveProxyTokensSQL = `CASE
	WHEN COALESCE(pl.total_tokens, 0) > 0 THEN COALESCE(pl.total_tokens, 0)
	ELSE COALESCE(pl.prompt_tokens, 0) + COALESCE(pl.completion_tokens, 0)
END`

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

	generatedAt := nowUTC()
	result := map[string]any{
		"generatedAt": generatedAt,
	}

	if view == "summary" || view == "full" {
		var siteCount, accountCount, activeAccounts int
		_ = h.db.Get(&siteCount, "SELECT COUNT(*) FROM sites")
		_ = h.db.Get(&accountCount, "SELECT COUNT(*) FROM accounts")
		_ = h.db.Get(&activeAccounts, "SELECT COUNT(*) FROM accounts WHERE status = 'active'")

		var totalBalance, totalUsed float64
		_ = h.db.Get(&totalBalance, "SELECT COALESCE(SUM(COALESCE(balance, 0)), 0) FROM accounts")
		_ = h.db.Get(&totalUsed, "SELECT COALESCE(SUM(COALESCE(balance_used, 0)), 0) FROM accounts")

		// 24h proxy window (UTC) — single-pass aggregate with effective tokens.
		since24h := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
		var proxy24h struct {
			Total       int     `db:"total"`
			Success     int     `db:"success"`
			TotalTokens int64   `db:"total_tokens"`
			TotalCost   float64 `db:"total_cost"`
		}
		_ = h.db.Get(&proxy24h, rebindAdminQuery(h.db, `
			SELECT
				COUNT(*) AS total,
				COALESCE(SUM(CASE WHEN pl.status = 'success' THEN 1 ELSE 0 END), 0) AS success,
				COALESCE(SUM(`+effectiveProxyTokensSQL+`), 0) AS total_tokens,
				COALESCE(SUM(COALESCE(pl.estimated_cost, 0)), 0) AS total_cost
			FROM proxy_logs pl
			WHERE pl.created_at >= ?
		`), since24h)

		// Today spend uses UTC day start for single-instance correctness
		// (matches aggregation local_day = UTC day).
		todayStart := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
		var todaySpend float64
		_ = h.db.Get(&todaySpend, rebindAdminQuery(h.db, `
			SELECT COALESCE(SUM(COALESCE(estimated_cost, 0)), 0)
			FROM proxy_logs
			WHERE created_at >= ?
		`), todayStart)

		// Performance window: last 60s request/token rate from proxy_logs.
		windowSeconds := 60
		sincePerf := time.Now().UTC().Add(-time.Duration(windowSeconds) * time.Second).Format(time.RFC3339)
		var perf struct {
			Requests int64 `db:"requests"`
			Tokens   int64 `db:"tokens"`
		}
		_ = h.db.Get(&perf, rebindAdminQuery(h.db, `
			SELECT
				COUNT(*) AS requests,
				COALESCE(SUM(`+effectiveProxyTokensSQL+`), 0) AS tokens
			FROM proxy_logs pl
			WHERE pl.created_at >= ?
		`), sincePerf)

		rpm := float64(perf.Requests) * 60 / float64(windowSeconds)
		tpm := float64(perf.Tokens) * 60 / float64(windowSeconds)

		result["siteCount"] = siteCount
		result["accountCount"] = accountCount
		result["totalAccounts"] = accountCount
		result["activeAccounts"] = activeAccounts
		result["totalBalance"] = roundMicro(totalBalance)
		result["totalUsed"] = roundMicro(totalUsed)
		result["todaySpend"] = roundMicro(todaySpend)
		result["todayReward"] = 0.0
		result["proxy24h"] = map[string]any{
			"total":       proxy24h.Total,
			"success":     proxy24h.Success,
			"totalTokens": proxy24h.TotalTokens,
			"totalCost":   roundMicro(proxy24h.TotalCost),
		}
		result["performance"] = map[string]any{
			"windowSeconds":     windowSeconds,
			"requestsPerMinute": rpm,
			"tokensPerMinute":   tpm,
		}
		// Legacy flat fields kept for older clients / tests.
		result["totalTokens"] = proxy24h.TotalTokens
		result["totalCost"] = roundMicro(proxy24h.TotalCost)
	}

	if view == "insights" || view == "full" {
		// All-time totals with effective token expression (no double count).
		var totalTokens int64
		_ = h.db.Get(&totalTokens, rebindAdminQuery(h.db, `
			SELECT COALESCE(SUM(`+effectiveProxyTokensSQL+`), 0) FROM proxy_logs pl
		`))
		var totalCost float64
		_ = h.db.Get(&totalCost, "SELECT COALESCE(SUM(COALESCE(estimated_cost, 0)), 0) FROM proxy_logs")
		result["totalTokens"] = totalTokens
		result["totalCost"] = roundMicro(totalCost)

		// Site availability over last 24h from proxy_logs join path.
		since24h := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
		rows := queryRows(h.db, `
			SELECT
				s.id AS site_id,
				s.name AS site_name,
				s.url AS site_url,
				s.platform AS platform,
				COUNT(pl.id) AS total_requests,
				COALESCE(SUM(CASE WHEN pl.status = 'success' THEN 1 ELSE 0 END), 0) AS success_count,
				COALESCE(SUM(CASE WHEN COALESCE(pl.status, '') <> 'success' THEN 1 ELSE 0 END), 0) AS failed_count,
				CASE
					WHEN COUNT(pl.id) = 0 THEN NULL
					ELSE ROUND(100.0 * SUM(CASE WHEN pl.status = 'success' THEN 1 ELSE 0 END) / COUNT(pl.id), 2)
				END AS availability_percent,
				CASE
					WHEN SUM(CASE WHEN COALESCE(pl.latency_ms, 0) > 0 THEN 1 ELSE 0 END) = 0 THEN NULL
					ELSE ROUND(1.0 * SUM(CASE WHEN COALESCE(pl.latency_ms, 0) > 0 THEN pl.latency_ms ELSE 0 END)
						/ SUM(CASE WHEN COALESCE(pl.latency_ms, 0) > 0 THEN 1 ELSE 0 END), 2)
				END AS average_latency_ms
			FROM sites s
			LEFT JOIN accounts a ON a.site_id = s.id
			LEFT JOIN proxy_logs pl ON pl.account_id = a.id AND pl.created_at >= ?
			GROUP BY s.id, s.name, s.url, s.platform
			ORDER BY total_requests DESC, s.name ASC
		`, since24h)

		siteAvailability := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			item := map[string]any{
				"siteId":              row["siteId"],
				"siteName":            row["siteName"],
				"siteUrl":             row["siteUrl"],
				"platform":            row["platform"],
				"totalRequests":       row["totalRequests"],
				"successCount":        row["successCount"],
				"failedCount":         row["failedCount"],
				"availabilityPercent": row["availabilityPercent"],
				"averageLatencyMs":    row["averageLatencyMs"],
				"buckets":             []any{},
			}
			siteAvailability = append(siteAvailability, item)
		}
		result["siteAvailability"] = siteAvailability
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
			"totalCount":     0,
			"successCount":   0,
			"failedCount":    0,
			"totalCost":      0.0,
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
		h.db.Get(&total, rebindAdminQuery(h.db, countQuery), args...)
		queryPayload["total"] = total
	}

	if view == "meta" || view == "full" {
		// Use effective token expression so partial logs are not under-counted,
		// and never sum prompt+completion on top of total_tokens.
		summaryQuery := `SELECT COUNT(*) as total_count,
			COALESCE(SUM(CASE WHEN pl.status = 'success' THEN 1 ELSE 0 END), 0) as success_count,
			COALESCE(SUM(CASE WHEN COALESCE(pl.status, '') <> 'success' THEN 1 ELSE 0 END), 0) as failed_count,
			COALESCE(SUM(COALESCE(pl.estimated_cost, 0)), 0) as total_cost,
			COALESCE(SUM(` + effectiveProxyTokensSQL + `), 0) as total_tokens_all
			FROM proxy_logs pl
			LEFT JOIN accounts a ON pl.account_id = a.id
			LEFT JOIN sites s ON a.site_id = s.id` + where
		metaArgs := make([]any, len(args))
		copy(metaArgs, args)

		var summary struct {
			TotalCount     int     `db:"total_count"`
			SuccessCount   int     `db:"success_count"`
			FailedCount    int     `db:"failed_count"`
			TotalCost      float64 `db:"total_cost"`
			TotalTokensAll int64   `db:"total_tokens_all"`
		}
		h.db.Get(&summary, rebindAdminQuery(h.db, summaryQuery), metaArgs...)
		metaPayload["summary"] = map[string]any{
			"totalCount":     summary.TotalCount,
			"successCount":   summary.SuccessCount,
			"failedCount":    summary.FailedCount,
			"totalCost":      roundMicro(summary.TotalCost),
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
	days := clampInt(getQueryInt(r, "days", 7), 1, 365)
	fromDay := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	// Prefer projected site_day_usage spend; include live account balances.
	rows := queryRows(h.db, `
		SELECT
			s.id AS site_id,
			s.name AS site_name,
			s.platform AS platform,
			COALESCE(bal.total_balance, 0) AS total_balance,
			COALESCE(usage.total_spend, 0) AS total_spend,
			COALESCE(bal.account_count, 0) AS account_count
		FROM sites s
		LEFT JOIN (
			SELECT site_id, COALESCE(SUM(COALESCE(balance, 0)), 0) AS total_balance, COUNT(*) AS account_count
			FROM accounts
			GROUP BY site_id
		) bal ON bal.site_id = s.id
		LEFT JOIN (
			SELECT site_id, COALESCE(SUM(COALESCE(total_summary_spend, 0)), 0) AS total_spend
			FROM site_day_usage
			WHERE local_day >= ?
			GROUP BY site_id
		) usage ON usage.site_id = s.id
		ORDER BY total_spend DESC, s.name ASC
	`, fromDay)

	distribution := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		distribution = append(distribution, map[string]any{
			"siteId":       row["siteId"],
			"siteName":     row["siteName"],
			"platform":     row["platform"],
			"totalBalance": coerceFloat(row["totalBalance"]),
			"totalSpend":   coerceFloat(row["totalSpend"]),
			"accountCount": coerceInt(row["accountCount"]),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"distribution": distribution})
}

// ---- Site Trend ----
// GET /api/stats/site-trend?days=&refresh=
func (h *statsHandler) siteTrend(w http.ResponseWriter, r *http.Request) {
	days := clampInt(getQueryInt(r, "days", 7), 1, 365)
	fromDay := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	rows := queryRows(h.db, `
		SELECT
			u.local_day AS local_day,
			s.name AS site_name,
			COALESCE(SUM(u.total_summary_spend), 0) AS spend,
			COALESCE(SUM(u.total_calls), 0) AS calls
		FROM site_day_usage u
		INNER JOIN sites s ON s.id = u.site_id
		WHERE u.local_day >= ?
		GROUP BY u.local_day, s.name
		ORDER BY u.local_day ASC, s.name ASC
	`, fromDay)

	// Shape: [{ date, sites: { [siteName]: { spend, calls } } }]
	byDate := make(map[string]map[string]map[string]any)
	order := make([]string, 0)
	for _, row := range rows {
		day := coerceString(row["localDay"])
		siteName := coerceString(row["siteName"])
		if day == "" || siteName == "" {
			continue
		}
		if _, ok := byDate[day]; !ok {
			byDate[day] = make(map[string]map[string]any)
			order = append(order, day)
		}
		byDate[day][siteName] = map[string]any{
			"spend": coerceFloat(row["spend"]),
			"calls": coerceInt(row["calls"]),
		}
	}

	trend := make([]map[string]any, 0, len(order))
	for _, day := range order {
		trend = append(trend, map[string]any{
			"date":  day,
			"sites": byDate[day],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"trend": trend})
}

// ---- Model by Site ----
// GET /api/stats/model-by-site?siteId=&days=
func (h *statsHandler) modelBySite(w http.ResponseWriter, r *http.Request) {
	days := clampInt(getQueryInt(r, "days", 7), 1, 365)
	siteID := getQueryInt(r, "siteId", 0)
	fromDay := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	query := `SELECT model,
		COALESCE(SUM(total_calls), 0) AS calls,
		COALESCE(SUM(total_spend), 0) AS spend,
		COALESCE(SUM(total_tokens), 0) AS tokens
		FROM model_day_usage
		WHERE local_day >= ?`
	args := []any{fromDay}
	if siteID > 0 {
		query += " AND site_id = ?"
		args = append(args, siteID)
	}
	query += " GROUP BY model ORDER BY calls DESC"

	rows := queryRows(h.db, query, args...)
	models := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		models = append(models, map[string]any{
			"model":  coerceString(row["model"]),
			"calls":  coerceInt(row["calls"]),
			"spend":  coerceFloat(row["spend"]),
			"tokens": coerceInt64(row["tokens"]),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// usageHeatmapCellLimit caps density rows returned to admin clients.
// 31d * 24h * ~50 keys is more than enough for a dense operator view.
const usageHeatmapCellLimit = 2000

// ---- Usage Heatmap ----
// GET /api/stats/usage-heatmap?days=7&dimension=site|model
//
// Returns bounded hour-bucket density cells for admin analytics (#121).
// Site dimension prefers projected site_hour_usage; model dimension aggregates
// proxy_logs with a hard LIMIT (no unbounded scans, no chat content).
func (h *statsHandler) usageHeatmap(w http.ResponseWriter, r *http.Request) {
	days := clampInt(getQueryInt(r, "days", 7), 1, 31)
	dimension := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("dimension")))
	if dimension != "model" {
		dimension = "site"
	}

	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	source := "proxy_logs"
	var cells []map[string]any

	if dimension == "site" {
		// Prefer projected hour aggregates (cheap, already limited by projector).
		rows := queryRows(h.db, `
			SELECT
				u.bucket_start_utc AS bucket,
				CAST(u.site_id AS TEXT) AS key,
				COALESCE(s.name, '') AS label,
				COALESCE(u.total_calls, 0) AS calls,
				COALESCE(u.total_tokens, 0) AS tokens,
				COALESCE(u.total_summary_spend, 0) AS spend
			FROM site_hour_usage u
			LEFT JOIN sites s ON s.id = u.site_id
			WHERE u.bucket_start_utc >= ?
			ORDER BY u.bucket_start_utc ASC, u.total_calls DESC
			LIMIT ?
		`, since, usageHeatmapCellLimit)
		if len(rows) > 0 {
			source = "site_hour_usage"
			cells = makeUsageHeatmapCells(rows)
		} else {
			// Fallback: bounded live aggregate from proxy_logs when projection is empty.
			hourExpr := hourBucketSQLExpr(h.db, "pl.created_at")
			rows = queryRows(h.db, `
				SELECT
					`+hourExpr+` AS bucket,
					CAST(s.id AS TEXT) AS key,
					COALESCE(s.name, '') AS label,
					COUNT(*) AS calls,
					COALESCE(SUM(`+effectiveProxyTokensSQL+`), 0) AS tokens,
					COALESCE(SUM(COALESCE(pl.estimated_cost, 0)), 0) AS spend
				FROM proxy_logs pl
				INNER JOIN accounts a ON a.id = pl.account_id
				INNER JOIN sites s ON s.id = a.site_id
				WHERE pl.created_at >= ?
				GROUP BY `+hourExpr+`, s.id, s.name
				ORDER BY bucket ASC, calls DESC
				LIMIT ?
			`, since, usageHeatmapCellLimit)
			cells = makeUsageHeatmapCells(rows)
		}
	} else {
		// Model density: no model_hour_usage table; aggregate proxy_logs with LIMIT.
		hourExpr := hourBucketSQLExpr(h.db, "pl.created_at")
		modelExpr := `COALESCE(NULLIF(pl.model_actual, ''), NULLIF(pl.model_requested, ''), 'unknown')`
		rows := queryRows(h.db, `
			SELECT
				`+hourExpr+` AS bucket,
				`+modelExpr+` AS key,
				`+modelExpr+` AS label,
				COUNT(*) AS calls,
				COALESCE(SUM(`+effectiveProxyTokensSQL+`), 0) AS tokens,
				COALESCE(SUM(COALESCE(pl.estimated_cost, 0)), 0) AS spend
			FROM proxy_logs pl
			WHERE pl.created_at >= ?
			GROUP BY `+hourExpr+`, `+modelExpr+`
			ORDER BY bucket ASC, calls DESC
			LIMIT ?
		`, since, usageHeatmapCellLimit)
		cells = makeUsageHeatmapCells(rows)
	}

	if cells == nil {
		cells = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dimension": dimension,
		"days":      days,
		"since":     since,
		"source":    source,
		"cellLimit": usageHeatmapCellLimit,
		"count":     len(cells),
		"cells":     cells,
	})
}

// ---- Slow Requests ----
// GET /api/stats/slow-requests?limit=50&minLatencyMs=1000&hours=24
//
// Top proxy_logs by latency_ms within a bounded time window (#121).
// Never returns request/response bodies or chat content.
func (h *statsHandler) slowRequests(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(getQueryInt(r, "limit", 50), 1, 200)
	minLatencyMs := clampInt(getQueryInt(r, "minLatencyMs", 1000), 0, 3_600_000)
	hours := clampInt(getQueryInt(r, "hours", 24), 1, 168)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	rows := queryRows(h.db, `
		SELECT
			pl.id AS id,
			COALESCE(NULLIF(pl.model_actual, ''), NULLIF(pl.model_requested, ''), '') AS model,
			COALESCE(pl.status, '') AS status,
			COALESCE(pl.latency_ms, 0) AS latency_ms,
			COALESCE(pl.first_byte_latency_ms, 0) AS first_byte_latency_ms,
			COALESCE(pl.http_status, 0) AS http_status,
			COALESCE(pl.request_id, '') AS request_id,
			pl.account_id AS account_id,
			s.id AS site_id,
			COALESCE(s.name, '') AS site_name,
			pl.created_at AS created_at
		FROM proxy_logs pl
		LEFT JOIN accounts a ON a.id = pl.account_id
		LEFT JOIN sites s ON s.id = a.site_id
		WHERE pl.created_at >= ?
			AND COALESCE(pl.latency_ms, 0) >= ?
		ORDER BY pl.latency_ms DESC, pl.created_at DESC
		LIMIT ?
	`, since, minLatencyMs, limit)

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id":                 coerceInt64(row["id"]),
			"model":              coerceString(row["model"]),
			"status":             coerceString(row["status"]),
			"latencyMs":          coerceInt64(row["latencyMs"]),
			"firstByteLatencyMs": coerceInt64(row["firstByteLatencyMs"]),
			"httpStatus":         coerceInt(row["httpStatus"]),
			"requestId":          coerceString(row["requestId"]),
			"accountId":          row["accountId"],
			"siteId":             row["siteId"],
			"siteName":           coerceString(row["siteName"]),
			"createdAt":          coerceString(row["createdAt"]),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"hours":        hours,
		"minLatencyMs": minLatencyMs,
		"limit":        limit,
		"since":        since,
		"count":        len(items),
		"items":        items,
	})
}

// hourBucketSQLExpr returns a dual-dialect SQL expression that truncates a
// TEXT RFC3339 timestamp column to an hour bucket string (…T%H:00:00Z).
func hourBucketSQLExpr(db *sqlx.DB, column string) string {
	driver := ""
	if db != nil {
		driver = strings.ToLower(strings.TrimSpace(db.DriverName()))
	}
	switch driver {
	case "pgx", "postgres", "postgresql":
		// created_at is stored as TEXT RFC3339; cast for date_trunc then re-format.
		return `to_char(date_trunc('hour', (` + column + `)::timestamptz), 'YYYY-MM-DD"T"HH24:00:00"Z"')`
	default:
		// SQLite stores UTC RFC3339 without fractional seconds in our writers.
		// substr keeps the query portable and avoids full-table function scans
		// beyond the created_at range predicate + LIMIT.
		return `substr(` + column + `, 1, 13) || ':00:00Z'`
	}
}

func makeUsageHeatmapCells(rows []map[string]any) []map[string]any {
	cells := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		bucket := coerceString(row["bucket"])
		key := coerceString(row["key"])
		if bucket == "" || key == "" {
			continue
		}
		cells = append(cells, map[string]any{
			"bucket": bucket,
			"key":    key,
			"label":  coerceString(row["label"]),
			"calls":  coerceInt64(row["calls"]),
			"tokens": coerceInt64(row["tokens"]),
			"spend":  roundMicro(coerceFloat(row["spend"])),
		})
	}
	return cells
}

// ---- Model Marketplace ----
// GET /api/models/marketplace?refresh=&includePricing=
//
// Builds the operator marketplace view from local DB availability / routes.
// Does not scrape remote marketplace/pricing catalogs. When includePricing=1,
// pricingSources remain empty and meta.pricingStatus labels the residual gap
// (see docs/analysis/admin-models-surfaces.md).
func (h *statsHandler) marketplace(w http.ResponseWriter, r *http.Request) {
	refreshRequested := parseTruthyQuery(r.URL.Query().Get("refresh"))
	includePricing := parseTruthyQuery(r.URL.Query().Get("includePricing"))

	models := h.buildMarketplaceModels()
	meta := map[string]any{
		"refreshRequested": refreshRequested,
		// No background pricing/catalog job in this surface — DB-derived only.
		"refreshQueued":  false,
		"refreshReused":  false,
		"refreshRunning": false,
		"refreshJobId":   nil,
		"includePricing": includePricing,
		"source":         "db_availability",
	}
	if includePricing {
		// Explicit residual: full remote /api/pricing hydration is out of scope.
		meta["pricingStatus"] = "unavailable"
		meta["pricingNote"] = "Remote marketplace pricing catalog is not hydrated; use /api/models/price-compare for effective rates. pricingSources intentionally empty."
	}
	if refreshRequested {
		meta["refreshNote"] = "refresh=true acknowledged but no remote marketplace scrape is performed; response is rebuilt from local model_availability / token_model_availability / token_routes."
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
		"meta":   meta,
	})
}

// ---- Token Candidates ----
// GET /api/models/token-candidates
//
// Structured maps for route configuration:
//   - models: token-scoped availability (token_model_availability)
//   - modelsWithoutToken: account-level availability with no matching token coverage
//   - modelsMissingTokenGroups: accounts whose tokens lack group labels when groups are required
//   - endpointTypesByModel: inferred endpoint types from site platforms
func (h *statsHandler) tokenCandidates(w http.ResponseWriter, r *http.Request) {
	allowed := h.loadGlobalAllowedModels()
	models := h.buildTokenCandidateModels(allowed)
	withoutToken := h.buildModelsWithoutToken(allowed)
	missingGroups := h.buildModelsMissingTokenGroups(allowed)
	endpointTypes := h.buildEndpointTypesByModel(allowed)

	writeJSON(w, http.StatusOK, map[string]any{
		"models":                   models,
		"modelsWithoutToken":       withoutToken,
		"modelsMissingTokenGroups": missingGroups,
		"endpointTypesByModel":     endpointTypes,
	})
}

// ---- Model Check ----
// POST /api/models/check/:accountId
//
// Real availability refresh for one account via platform.GetModels, then
// best-effort route rebuild. Never returns fake success when refresh fails.
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

	result := h.refreshAccountModels(r.Context(), id)
	writeJSON(w, http.StatusOK, result)
}

// ---- Model Probe ----
// POST /api/models/probe
func (h *statsHandler) modelProbe(w http.ResponseWriter, r *http.Request) {
	sched := scheduler.GetGlobalModelProbeScheduler()
	if sched == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"message": "model probe scheduler is not running (enable MODEL_AVAILABILITY_PROBE_ENABLED or start schedulers)",
		})
		return
	}
	jobID := fmt.Sprintf("probe-%d", time.Now().UTC().UnixNano())
	go func() {
		sched.TriggerNow(true)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"jobId":   jobID,
		"status":  "pending",
		"message": "已开始模型可用性探测，请稍后查看任务列表或 LastRunSummary",
	})
}

// marketplaceAccount aggregates per-account marketplace detail for one model.
type marketplaceAccountAgg struct {
	ID       int64
	Site     string
	Username *string
	Latency  *float64
	Balance  float64
	Tokens   []map[string]any
	tokenIDs map[int64]struct{}
}

func (h *statsHandler) buildMarketplaceModels() []map[string]any {
	// Collect model names from availability tables + exact token_routes patterns.
	type modelKey = string
	type modelAgg struct {
		accounts map[int64]*marketplaceAccountAgg
		latSum   float64
		latCount int
	}
	byModel := map[modelKey]*modelAgg{}

	ensure := func(model string) *modelAgg {
		if agg, ok := byModel[model]; ok {
			return agg
		}
		agg := &modelAgg{accounts: map[int64]*marketplaceAccountAgg{}}
		byModel[model] = agg
		return agg
	}
	ensureAccount := func(agg *modelAgg, accountID int64, site, username string, balance float64, latency *float64) *marketplaceAccountAgg {
		acc, ok := agg.accounts[accountID]
		if !ok {
			var userPtr *string
			if strings.TrimSpace(username) != "" {
				u := username
				userPtr = &u
			}
			acc = &marketplaceAccountAgg{
				ID:       accountID,
				Site:     site,
				Username: userPtr,
				Balance:  balance,
				Tokens:   []map[string]any{},
				tokenIDs: map[int64]struct{}{},
			}
			agg.accounts[accountID] = acc
		}
		if latency != nil {
			if acc.Latency == nil || *latency < *acc.Latency {
				v := *latency
				acc.Latency = &v
			}
			agg.latSum += *latency
			agg.latCount++
		}
		return acc
	}

	// Account-level availability.
	accountRows := queryRows(h.db, `
		SELECT
			ma.model_name AS model_name,
			ma.latency_ms AS latency_ms,
			a.id AS account_id,
			COALESCE(a.username, '') AS username,
			COALESCE(a.balance, 0) AS balance,
			COALESCE(s.name, '') AS site_name,
			COALESCE(s.platform, '') AS platform
		FROM model_availability ma
		INNER JOIN accounts a ON a.id = ma.account_id
		INNER JOIN sites s ON s.id = a.site_id
		WHERE COALESCE(ma.available, 0) = 1
			AND COALESCE(a.status, '') <> 'disabled'
			AND COALESCE(s.status, '') <> 'disabled'
		ORDER BY ma.model_name ASC, a.id ASC
	`)
	for _, row := range accountRows {
		model := strings.TrimSpace(coerceString(row["modelName"]))
		if model == "" {
			continue
		}
		agg := ensure(model)
		var latency *float64
		if row["latencyMs"] != nil && coerceString(row["latencyMs"]) != "" {
			v := coerceFloat(row["latencyMs"])
			latency = &v
		}
		acc := ensureAccount(agg,
			coerceInt64(row["accountId"]),
			coerceString(row["siteName"]),
			coerceString(row["username"]),
			coerceFloat(row["balance"]),
			latency,
		)
		// Attach enabled tokens for this account (may also appear via token availability).
		tokenRows := queryRows(h.db, `
			SELECT id, name, is_default
			FROM account_tokens
			WHERE account_id = ?
				AND enabled = 1
				AND (value_status IS NULL OR value_status <> 'expired')
			ORDER BY is_default DESC, id ASC
		`, acc.ID)
		for _, tr := range tokenRows {
			tid := coerceInt64(tr["id"])
			if tid <= 0 {
				continue
			}
			if _, exists := acc.tokenIDs[tid]; exists {
				continue
			}
			acc.tokenIDs[tid] = struct{}{}
			acc.Tokens = append(acc.Tokens, map[string]any{
				"id":        tid,
				"name":      coerceString(tr["name"]),
				"isDefault": coerceInt(tr["isDefault"]) == 1 || coerceString(tr["isDefault"]) == "true" || coerceString(tr["isDefault"]) == "1",
			})
		}
	}

	// Token-level availability.
	tokenAvailRows := queryRows(h.db, `
		SELECT
			tma.model_name AS model_name,
			tma.latency_ms AS latency_ms,
			at.id AS token_id,
			COALESCE(at.name, '') AS token_name,
			at.is_default AS is_default,
			a.id AS account_id,
			COALESCE(a.username, '') AS username,
			COALESCE(a.balance, 0) AS balance,
			COALESCE(s.name, '') AS site_name
		FROM token_model_availability tma
		INNER JOIN account_tokens at ON at.id = tma.token_id
		INNER JOIN accounts a ON a.id = at.account_id
		INNER JOIN sites s ON s.id = a.site_id
		WHERE COALESCE(tma.available, 0) = 1
			AND at.enabled = 1
			AND (at.value_status IS NULL OR at.value_status <> 'expired')
			AND COALESCE(a.status, '') <> 'disabled'
			AND COALESCE(s.status, '') <> 'disabled'
		ORDER BY tma.model_name ASC, a.id ASC, at.id ASC
	`)
	for _, row := range tokenAvailRows {
		model := strings.TrimSpace(coerceString(row["modelName"]))
		if model == "" {
			continue
		}
		agg := ensure(model)
		var latency *float64
		if row["latencyMs"] != nil && coerceString(row["latencyMs"]) != "" {
			v := coerceFloat(row["latencyMs"])
			latency = &v
		}
		acc := ensureAccount(agg,
			coerceInt64(row["accountId"]),
			coerceString(row["siteName"]),
			coerceString(row["username"]),
			coerceFloat(row["balance"]),
			latency,
		)
		tid := coerceInt64(row["tokenId"])
		if tid <= 0 {
			continue
		}
		if _, exists := acc.tokenIDs[tid]; exists {
			continue
		}
		acc.tokenIDs[tid] = struct{}{}
		acc.Tokens = append(acc.Tokens, map[string]any{
			"id":        tid,
			"name":      coerceString(row["tokenName"]),
			"isDefault": coerceInt(row["isDefault"]) == 1 || coerceString(row["isDefault"]) == "true" || coerceString(row["isDefault"]) == "1",
		})
	}

	// Exact-model token_routes contribute model names even when availability is empty
	// so operators still see configured routes in the marketplace list.
	routeRows := queryRows(h.db, `
		SELECT model_pattern
		FROM token_routes
		WHERE enabled = 1
			AND route_mode <> 'explicit_group'
	`)
	for _, row := range routeRows {
		pattern := strings.TrimSpace(coerceString(row["modelPattern"]))
		if pattern == "" {
			continue
		}
		// Only exact patterns (no wildcards / regex markers) become marketplace models.
		if strings.ContainsAny(pattern, "*?^$[]()|\\+") {
			continue
		}
		if _, ok := byModel[pattern]; !ok {
			byModel[pattern] = &modelAgg{accounts: map[int64]*marketplaceAccountAgg{}}
		}
	}

	// Optional success-rate from recent proxy_logs (bounded 7d window).
	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	successByModel := map[string]struct {
		total   int
		success int
	}{}
	logRows := queryRows(h.db, `
		SELECT
			COALESCE(NULLIF(TRIM(model_actual), ''), NULLIF(TRIM(model_requested), ''), '') AS model,
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) AS success
		FROM proxy_logs
		WHERE created_at >= ?
			AND COALESCE(NULLIF(TRIM(model_actual), ''), NULLIF(TRIM(model_requested), ''), '') <> ''
		GROUP BY COALESCE(NULLIF(TRIM(model_actual), ''), NULLIF(TRIM(model_requested), ''), '')
	`, since)
	for _, row := range logRows {
		model := strings.TrimSpace(coerceString(row["model"]))
		if model == "" {
			continue
		}
		successByModel[model] = struct {
			total   int
			success int
		}{total: coerceInt(row["total"]), success: coerceInt(row["success"])}
	}

	names := make([]string, 0, len(byModel))
	for name := range byModel {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		agg := byModel[name]
		accounts := make([]map[string]any, 0, len(agg.accounts))
		accountIDs := make([]int64, 0, len(agg.accounts))
		for id := range agg.accounts {
			accountIDs = append(accountIDs, id)
		}
		sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
		tokenCount := 0
		for _, id := range accountIDs {
			acc := agg.accounts[id]
			tokenCount += len(acc.Tokens)
			var latency any
			if acc.Latency != nil {
				latency = int64(math.Round(*acc.Latency))
			} else {
				latency = nil
			}
			accounts = append(accounts, map[string]any{
				"id":       acc.ID,
				"site":     acc.Site,
				"username": acc.Username,
				"latency":  latency,
				"balance":  roundMicro(acc.Balance),
				"tokens":   acc.Tokens,
			})
		}
		var avgLatency any
		if agg.latCount > 0 {
			avgLatency = int64(math.Round(agg.latSum / float64(agg.latCount)))
		} else {
			avgLatency = nil
		}
		var successRate any
		if stats, ok := successByModel[name]; ok && stats.total > 0 {
			successRate = math.Round(1000.0*float64(stats.success)/float64(stats.total)) / 10.0
		} else {
			successRate = nil
		}
		out = append(out, map[string]any{
			"name":                   name,
			"accountCount":           len(accounts),
			"tokenCount":             tokenCount,
			"avgLatency":             avgLatency,
			"successRate":            successRate,
			"description":            nil,
			"tags":                   []string{},
			"supportedEndpointTypes": inferEndpointTypesForModel(name, accounts),
			// Pricing intentionally empty — labeled residual; use price-compare for rates.
			"pricingSources": []any{},
			"accounts":       accounts,
		})
	}
	return out
}

func inferEndpointTypesForModel(modelName string, accounts []map[string]any) []string {
	// Prefer model-name heuristics; fall back to empty when unknown.
	lower := strings.ToLower(modelName)
	switch {
	case strings.Contains(lower, "claude") || strings.HasPrefix(lower, "anthropic"):
		return []string{"anthropic"}
	case strings.Contains(lower, "gemini"):
		return []string{"gemini"}
	case strings.Contains(lower, "gpt") || strings.Contains(lower, "o1") || strings.Contains(lower, "o3") || strings.Contains(lower, "o4"):
		return []string{"openai"}
	default:
		return []string{}
	}
}

func (h *statsHandler) buildTokenCandidateModels(allowed map[string]struct{}) map[string][]map[string]any {
	rows := queryRows(h.db, `
		SELECT
			tma.model_name AS model_name,
			a.id AS account_id,
			at.id AS token_id,
			COALESCE(at.name, '') AS token_name,
			at.is_default AS is_default,
			COALESCE(a.username, '') AS username,
			s.id AS site_id,
			COALESCE(s.name, '') AS site_name
		FROM token_model_availability tma
		INNER JOIN account_tokens at ON at.id = tma.token_id
		INNER JOIN accounts a ON a.id = at.account_id
		INNER JOIN sites s ON s.id = a.site_id
		WHERE COALESCE(tma.available, 0) = 1
			AND at.enabled = 1
			AND (at.value_status IS NULL OR at.value_status <> 'expired')
			AND COALESCE(a.status, '') <> 'disabled'
			AND COALESCE(s.status, '') <> 'disabled'
		ORDER BY tma.model_name ASC, a.id ASC, at.id ASC
	`)

	out := map[string][]map[string]any{}
	seen := map[string]map[int64]struct{}{} // model -> tokenIDs
	for _, row := range rows {
		model := strings.TrimSpace(coerceString(row["modelName"]))
		if model == "" || !modelAllowed(model, allowed) {
			continue
		}
		tokenID := coerceInt64(row["tokenId"])
		if tokenID <= 0 {
			continue
		}
		if _, ok := seen[model]; !ok {
			seen[model] = map[int64]struct{}{}
		}
		if _, dup := seen[model][tokenID]; dup {
			continue
		}
		seen[model][tokenID] = struct{}{}
		var username any
		if u := strings.TrimSpace(coerceString(row["username"])); u != "" {
			username = u
		} else {
			username = nil
		}
		out[model] = append(out[model], map[string]any{
			"accountId": coerceInt64(row["accountId"]),
			"tokenId":   tokenID,
			"tokenName": coerceString(row["tokenName"]),
			"isDefault": coerceInt(row["isDefault"]) == 1 || coerceString(row["isDefault"]) == "true" || coerceString(row["isDefault"]) == "1",
			"username":  username,
			"siteId":    coerceInt64(row["siteId"]),
			"siteName":  coerceString(row["siteName"]),
		})
	}
	return out
}

func (h *statsHandler) buildModelsWithoutToken(allowed map[string]struct{}) map[string][]map[string]any {
	// Accounts with available model_availability but no token_model_availability
	// coverage for that model AND no enabled account_tokens at all (or none that
	// list the model). Operators use this for zero-channel route hints.
	rows := queryRows(h.db, `
		SELECT
			ma.model_name AS model_name,
			a.id AS account_id,
			COALESCE(a.username, '') AS username,
			s.id AS site_id,
			COALESCE(s.name, '') AS site_name
		FROM model_availability ma
		INNER JOIN accounts a ON a.id = ma.account_id
		INNER JOIN sites s ON s.id = a.site_id
		WHERE COALESCE(ma.available, 0) = 1
			AND COALESCE(a.status, '') <> 'disabled'
			AND COALESCE(s.status, '') <> 'disabled'
			AND NOT EXISTS (
				SELECT 1
				FROM account_tokens at
				INNER JOIN token_model_availability tma ON tma.token_id = at.id
				WHERE at.account_id = a.id
					AND at.enabled = 1
					AND (at.value_status IS NULL OR at.value_status <> 'expired')
					AND COALESCE(tma.available, 0) = 1
					AND tma.model_name = ma.model_name
			)
			AND NOT EXISTS (
				-- API-key style accounts that store a single key without managed tokens
				-- are still "without token" for route channel binding when no account_tokens rows exist.
				SELECT 1 FROM account_tokens at
				WHERE at.account_id = a.id
					AND at.enabled = 1
					AND (at.value_status IS NULL OR at.value_status <> 'expired')
			)
		ORDER BY ma.model_name ASC, a.id ASC
	`)

	out := map[string][]map[string]any{}
	seen := map[string]map[int64]struct{}{}
	for _, row := range rows {
		model := strings.TrimSpace(coerceString(row["modelName"]))
		if model == "" || !modelAllowed(model, allowed) {
			continue
		}
		accountID := coerceInt64(row["accountId"])
		if accountID <= 0 {
			continue
		}
		if _, ok := seen[model]; !ok {
			seen[model] = map[int64]struct{}{}
		}
		if _, dup := seen[model][accountID]; dup {
			continue
		}
		seen[model][accountID] = struct{}{}
		var username any
		if u := strings.TrimSpace(coerceString(row["username"])); u != "" {
			username = u
		} else {
			username = nil
		}
		out[model] = append(out[model], map[string]any{
			"accountId": accountID,
			"username":  username,
			"siteId":    coerceInt64(row["siteId"]),
			"siteName":  coerceString(row["siteName"]),
		})
	}
	return out
}

func (h *statsHandler) buildModelsMissingTokenGroups(allowed map[string]struct{}) map[string][]map[string]any {
	// When an account has model availability and managed tokens, but none of the
	// enabled tokens have a resolvable token_group label, group coverage is uncertain
	// / missing. We do not invent required groups from a remote pricing catalog.
	rows := queryRows(h.db, `
		SELECT
			ma.model_name AS model_name,
			a.id AS account_id,
			COALESCE(a.username, '') AS username,
			s.id AS site_id,
			COALESCE(s.name, '') AS site_name,
			COALESCE(at.token_group, '') AS token_group,
			COALESCE(at.name, '') AS token_name
		FROM model_availability ma
		INNER JOIN accounts a ON a.id = ma.account_id
		INNER JOIN sites s ON s.id = a.site_id
		INNER JOIN account_tokens at ON at.account_id = a.id
		WHERE COALESCE(ma.available, 0) = 1
			AND COALESCE(a.status, '') <> 'disabled'
			AND COALESCE(s.status, '') <> 'disabled'
			AND at.enabled = 1
			AND (at.value_status IS NULL OR at.value_status <> 'expired')
		ORDER BY ma.model_name ASC, a.id ASC, at.id ASC
	`)

	type accGroups struct {
		accountID int64
		username  string
		siteID    int64
		siteName  string
		available []string
		uncertain bool
	}
	// model -> accountID -> groups
	byModel := map[string]map[int64]*accGroups{}
	for _, row := range rows {
		model := strings.TrimSpace(coerceString(row["modelName"]))
		if model == "" || !modelAllowed(model, allowed) {
			continue
		}
		accountID := coerceInt64(row["accountId"])
		if accountID <= 0 {
			continue
		}
		if _, ok := byModel[model]; !ok {
			byModel[model] = map[int64]*accGroups{}
		}
		ag, ok := byModel[model][accountID]
		if !ok {
			ag = &accGroups{
				accountID: accountID,
				username:  coerceString(row["username"]),
				siteID:    coerceInt64(row["siteId"]),
				siteName:  coerceString(row["siteName"]),
			}
			byModel[model][accountID] = ag
		}
		group := resolveTokenGroupLabel(coerceString(row["tokenGroup"]), coerceString(row["tokenName"]))
		if group == "" {
			ag.uncertain = true
			continue
		}
		// de-dupe
		found := false
		for _, g := range ag.available {
			if strings.EqualFold(g, group) {
				found = true
				break
			}
		}
		if !found {
			ag.available = append(ag.available, group)
		}
	}

	out := map[string][]map[string]any{}
	for model, accounts := range byModel {
		for _, ag := range accounts {
			// Only surface accounts where no token has a resolvable group label.
			if len(ag.available) > 0 {
				continue
			}
			var username any
			if strings.TrimSpace(ag.username) != "" {
				username = ag.username
			} else {
				username = nil
			}
			item := map[string]any{
				"accountId":              ag.accountID,
				"username":               username,
				"siteId":                 ag.siteID,
				"siteName":               ag.siteName,
				"missingGroups":          []string{},
				"requiredGroups":         []string{},
				"availableGroups":        []string{},
				"groupCoverageUncertain": true,
			}
			out[model] = append(out[model], item)
		}
	}
	return out
}

func resolveTokenGroupLabel(tokenGroup, tokenName string) string {
	group := strings.TrimSpace(tokenGroup)
	if group != "" {
		return group
	}
	name := strings.TrimSpace(tokenName)
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	if lower == "default" || name == "默认" {
		return ""
	}
	// Names like token-1 / token-N are not group labels.
	if strings.HasPrefix(lower, "token-") {
		return ""
	}
	return name
}

func (h *statsHandler) buildEndpointTypesByModel(allowed map[string]struct{}) map[string][]string {
	// Union of endpoint types inferred from model names present in availability.
	models := map[string]struct{}{}
	for _, row := range queryRows(h.db, `
		SELECT DISTINCT model_name AS model_name FROM model_availability WHERE COALESCE(available, 0) = 1
		UNION
		SELECT DISTINCT model_name AS model_name FROM token_model_availability WHERE COALESCE(available, 0) = 1
	`) {
		model := strings.TrimSpace(coerceString(row["modelName"]))
		if model == "" || !modelAllowed(model, allowed) {
			continue
		}
		models[model] = struct{}{}
	}
	out := map[string][]string{}
	for model := range models {
		types := inferEndpointTypesForModel(model, nil)
		if len(types) == 0 {
			// default OpenAI-compatible when unknown
			types = []string{"openai"}
		}
		out[model] = types
	}
	return out
}

func (h *statsHandler) loadGlobalAllowedModels() map[string]struct{} {
	// Optional whitelist from settings table. Empty / missing → allow all.
	var raw string
	err := h.db.Get(&raw, rebindAdminQuery(h.db, `SELECT value FROM settings WHERE key = ?`), "global_allowed_models")
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		// also accept CSV
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				list = append(list, part)
			}
		}
	}
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(list))
	for _, m := range list {
		m = strings.TrimSpace(m)
		if m != "" {
			out[strings.ToLower(m)] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func modelAllowed(model string, allowed map[string]struct{}) bool {
	if allowed == nil || len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.ToLower(strings.TrimSpace(model))]
	return ok
}

func parseTruthyQuery(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// refreshAccountModels performs a real platform.GetModels refresh and persists
// results into model_availability. Returns the operator-facing check payload.
func (h *statsHandler) refreshAccountModels(ctx context.Context, accountID int64) map[string]any {
	row, err := service.GetAccountWithSiteByID(h.db, accountID)
	if err != nil || row == nil {
		return map[string]any{
			"success": false,
			"error":   "account not found",
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    "not_found",
				"errorMessage": "账号不存在",
			},
			"rebuild": map[string]any{},
		}
	}

	account := row.Account
	site := row.Site
	if strings.EqualFold(strings.TrimSpace(account.Status), "disabled") {
		return map[string]any{
			"success": false,
			"message": "账号已禁用，无法刷新模型",
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    "disabled",
				"errorMessage": "账号已禁用",
			},
			"rebuild": map[string]any{},
		}
	}

	adapter := platform.GetAdapter(site.Platform)
	if adapter == nil {
		return map[string]any{
			"success": false,
			"message": "unsupported platform: " + site.Platform,
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    "unsupported_platform",
				"errorMessage": "不支持的平台: " + site.Platform,
			},
			"rebuild": map[string]any{},
		}
	}

	token := resolveAccountModelToken(&account)
	if token == "" {
		return map[string]any{
			"success": false,
			"message": "账号缺少可用凭证",
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    "missing_credential",
				"errorMessage": "账号缺少 access_token / api_token",
			},
			"rebuild": map[string]any{},
		}
	}

	platformUserID := resolvePlatformUserIDPtr(account.ExtraConfig, account.Username)
	proxyCfg := service.BuildPlatformProxyConfig(nil, &account, &site)

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	models, getErr := adapter.GetModels(callCtx, site.URL, token, platformUserID, proxyCfg)
	if getErr != nil {
		code, msg := classifyModelRefreshError(getErr, callCtx)
		return map[string]any{
			"success": false,
			"message": msg,
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    code,
				"errorMessage": msg,
			},
			"rebuild": map[string]any{},
		}
	}

	// Deduplicate / trim.
	seen := map[string]struct{}{}
	clean := make([]string, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		key := strings.ToLower(m)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		clean = append(clean, m)
	}
	if len(clean) == 0 {
		return map[string]any{
			"success": false,
			"message": "未获取到可用模型",
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    "empty_models",
				"errorMessage": "未获取到可用模型",
				"modelCount":   0,
				"models":       []string{},
			},
			"rebuild": map[string]any{},
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := h.persistAccountModelAvailability(accountID, clean, now); err != nil {
		return map[string]any{
			"success": false,
			"message": "模型写入失败: " + err.Error(),
			"refresh": map[string]any{
				"id":           accountID,
				"status":       "failed",
				"errorCode":    "persist_failed",
				"errorMessage": err.Error(),
				"modelCount":   len(clean),
				"models":       clean,
			},
			"rebuild": map[string]any{},
		}
	}

	// Best-effort route rebuild from updated availability.
	rebuildStats, rebuildErr := service.RebuildTokenRoutesFromAvailability(context.Background(), h.db)
	rebuildPayload := map[string]any{
		"routesConsidered": rebuildStats.RoutesConsidered,
		"patternRoutes":    rebuildStats.PatternRoutes,
		"groupRoutes":      rebuildStats.GroupRoutes,
		"channelsInserted": rebuildStats.ChannelsInserted,
		"channelsRemoved":  rebuildStats.ChannelsRemoved,
		"channelsKept":     rebuildStats.ChannelsKept,
	}
	if rebuildErr != nil {
		rebuildPayload["success"] = false
		rebuildPayload["error"] = rebuildErr.Error()
	} else {
		rebuildPayload["success"] = true
	}
	routing.InvalidateCache()

	return map[string]any{
		"success": true,
		"refresh": map[string]any{
			"id":         accountID,
			"status":     "success",
			"modelCount": len(clean),
			"models":     clean,
			"checkedAt":  now,
		},
		"rebuild": rebuildPayload,
	}
}

func resolveAccountModelToken(account *store.Account) string {
	if account == nil {
		return ""
	}
	if account.APIToken != nil && strings.TrimSpace(*account.APIToken) != "" {
		return strings.TrimSpace(*account.APIToken)
	}
	return strings.TrimSpace(account.AccessToken)
}

func resolvePlatformUserIDPtr(extraConfig *string, username *string) *int {
	id := service.ResolvePlatformUserID(extraConfig, username)
	if id <= 0 {
		return nil
	}
	v := int(id)
	return &v
}

func classifyModelRefreshError(err error, ctx context.Context) (code, message string) {
	if err == nil {
		return "unknown", "模型获取失败"
	}
	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)
	timedOut := (ctx != nil && ctx.Err() == context.DeadlineExceeded) ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "deadline exceeded")
	if timedOut {
		return "timeout", "模型获取失败（请求超时）"
	}
	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401") || strings.Contains(lower, "invalid api key") || strings.Contains(lower, "authentication") {
		return "unauthorized", "模型获取失败，API Key 已无效"
	}
	if msg == "" {
		msg = "模型获取失败"
	}
	return "upstream_error", msg
}

func (h *statsHandler) persistAccountModelAvailability(accountID int64, models []string, now string) error {
	tx, err := h.db.Beginx()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Preserve manual rows; mark non-manual previous rows unavailable then upsert.
	if _, err := tx.Exec(tx.Rebind(`
		UPDATE model_availability
		SET available = ?, checked_at = ?
		WHERE account_id = ? AND COALESCE(is_manual, 0) = 0
	`), false, now, accountID); err != nil {
		return err
	}

	for _, model := range models {
		var existingID int64
		err := tx.Get(&existingID, tx.Rebind(`
			SELECT id FROM model_availability WHERE account_id = ? AND model_name = ?
		`), accountID, model)
		if err == nil {
			if _, err := tx.Exec(tx.Rebind(`
				UPDATE model_availability
				SET available = ?, latency_ms = NULL, checked_at = ?
				WHERE id = ?
			`), true, now, existingID); err != nil {
				return err
			}
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(tx.Rebind(`
			INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at)
			VALUES (?, ?, ?, ?, NULL, ?)
		`), accountID, model, true, false, now); err != nil {
			if _, err2 := tx.Exec(tx.Rebind(`
				UPDATE model_availability
				SET available = ?, latency_ms = NULL, checked_at = ?
				WHERE account_id = ? AND model_name = ?
			`), true, now, accountID, model); err2 != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func queryRow(db *sqlx.DB, query string, args ...any) map[string]any {
	rows, err := db.Queryx(rebindAdminQuery(db, query), args...)
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

func coerceFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case []byte:
		f, _ := strconv.ParseFloat(string(n), 64)
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

func coerceInt(v any) int {
	return int(coerceInt64(v))
}

func coerceInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case []byte:
		i, _ := strconv.ParseInt(string(n), 10, 64)
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func coerceString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(strconv.FormatInt(coerceInt64(v), 10))
	}
}
