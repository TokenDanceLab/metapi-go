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
		"models":                   map[string]any{},
		"modelsWithoutToken":       map[string]any{},
		"modelsMissingTokenGroups": map[string]any{},
		"endpointTypesByModel":     map[string]any{},
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
