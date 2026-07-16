package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GET /api/stats/usage-heatmap?days=7&dimension=site|model
// Bounded density cells for admin analytics (#121).
func (h *statsHandler) usageHeatmap(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	if days > 31 {
		days = 31
	}
	dim := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dimension")))
	if dim != "model" {
		dim = "site"
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)

	type cell struct {
		Bucket string `db:"bucket" json:"bucket"`
		Key    string `db:"key" json:"key"`
		Count  int64  `db:"cnt" json:"count"`
		Tokens int64  `db:"tokens" json:"tokens"`
	}
	var cells []cell

	if dim == "model" {
		q := rebindAdminQuery(h.db, "SELECT substr(created_at, 1, 13) AS bucket, COALESCE(NULLIF(model_actual, ''), COALESCE(model_requested, 'unknown')) AS key, COUNT(*) AS cnt, COALESCE(SUM(CASE WHEN COALESCE(total_tokens,0)>0 THEN total_tokens ELSE COALESCE(prompt_tokens,0)+COALESCE(completion_tokens,0) END), 0) AS tokens FROM proxy_logs WHERE created_at >= ? GROUP BY bucket, key ORDER BY bucket ASC, cnt DESC LIMIT 2000")
		_ = h.db.Select(&cells, q, since)
	} else {
		q := rebindAdminQuery(h.db, "SELECT bucket_start_utc AS bucket, CAST(site_id AS TEXT) AS key, COALESCE(total_calls, 0) AS cnt, COALESCE(total_tokens, 0) AS tokens FROM site_hour_usage WHERE bucket_start_utc >= ? ORDER BY bucket_start_utc ASC, total_calls DESC LIMIT 2000")
		if err := h.db.Select(&cells, q, since); err != nil || len(cells) == 0 {
			q2 := rebindAdminQuery(h.db, "SELECT substr(pl.created_at, 1, 13) AS bucket, CAST(a.site_id AS TEXT) AS key, COUNT(*) AS cnt, COALESCE(SUM(CASE WHEN COALESCE(pl.total_tokens,0)>0 THEN pl.total_tokens ELSE COALESCE(pl.prompt_tokens,0)+COALESCE(pl.completion_tokens,0) END), 0) AS tokens FROM proxy_logs pl JOIN accounts a ON a.id = pl.account_id WHERE pl.created_at >= ? GROUP BY bucket, key ORDER BY bucket ASC, cnt DESC LIMIT 2000")
			cells = nil
			_ = h.db.Select(&cells, q2, since)
		}
	}
	if cells == nil {
		cells = []cell{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"dimension": dim,
		"days":      days,
		"since":     since,
		"count":     len(cells),
		"cells":     cells,
	})
}

// GET /api/stats/slow-requests?limit=50&minLatencyMs=1000&hours=24
func (h *statsHandler) slowRequests(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	minLat := int64(1000)
	if v := strings.TrimSpace(r.URL.Query().Get("minLatencyMs")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			minLat = n
		}
	}
	hours := 24
	if v := strings.TrimSpace(r.URL.Query().Get("hours")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	if hours > 168 {
		hours = 168
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)

	type row struct {
		ID        int64  `db:"id" json:"id"`
		Model     string `db:"model" json:"model"`
		Status    string `db:"status" json:"status"`
		LatencyMs int64  `db:"latency_ms" json:"latencyMs"`
		SiteID    *int64 `db:"site_id" json:"siteId,omitempty"`
		CreatedAt string `db:"created_at" json:"createdAt"`
	}
	var rows []row
	q := rebindAdminQuery(h.db, "SELECT pl.id AS id, COALESCE(NULLIF(pl.model_actual, ''), COALESCE(pl.model_requested, '')) AS model, COALESCE(pl.status, '') AS status, COALESCE(pl.latency_ms, 0) AS latency_ms, a.site_id AS site_id, pl.created_at AS created_at FROM proxy_logs pl LEFT JOIN accounts a ON a.id = pl.account_id WHERE pl.created_at >= ? AND COALESCE(pl.latency_ms, 0) >= ? ORDER BY pl.latency_ms DESC, pl.id DESC LIMIT ?")
	_ = h.db.Select(&rows, q, since, minLat, limit)
	if rows == nil {
		rows = []row{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"hours":        hours,
		"minLatencyMs": minLat,
		"limit":        limit,
		"since":        since,
		"count":        len(rows),
		"items":        rows,
	})
}
