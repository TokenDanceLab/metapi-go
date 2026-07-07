package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterSearchRoutes registers all /api/search routes.
func RegisterSearchRoutes(r chi.Router, db *sqlx.DB) {
	handler := &searchHandler{db: db}
	r.Post("/api/search", handler.search)
}

type searchHandler struct {
	db *sqlx.DB
}

type searchRequestBody struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// POST /api/search
func (h *searchHandler) search(w http.ResponseWriter, r *http.Request) {
	var body searchRequestBody
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"accounts":      []any{},
			"accountTokens": []any{},
			"sites":         []any{},
			"checkinLogs":   []any{},
			"proxyLogs":     []any{},
			"models":        []any{},
		})
		return
	}

	q := strings.TrimSpace(body.Query)
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"accounts":      []any{},
			"accountTokens": []any{},
			"sites":         []any{},
			"checkinLogs":   []any{},
			"proxyLogs":     []any{},
			"models":        []any{},
		})
		return
	}

	limit := body.Limit
	if limit <= 0 {
		limit = 20
	}
	perCategory := limit / 6
	if perCategory < 1 {
		perCategory = 1
	}
	if perCategory > 10 {
		perCategory = 10
	}

	likePattern := "%" + q + "%"

	// Search sites
	sites := queryRows(h.db, "SELECT * FROM sites WHERE name LIKE ? OR url LIKE ? OR platform LIKE ? LIMIT ?",
		likePattern, likePattern, likePattern, perCategory)

	// Search accounts
	accounts := queryRows(h.db,
		`SELECT a.*, s.name as site_name, s.platform as site_platform
		 FROM accounts a INNER JOIN sites s ON a.site_id = s.id
		 WHERE a.username LIKE ? OR s.name LIKE ? OR s.platform LIKE ?
		 LIMIT ?`,
		likePattern, likePattern, likePattern, perCategory)

	// Search account tokens
	accountTokens := queryRows(h.db,
		`SELECT at.*, a.username as account_username, s.name as site_name
		 FROM account_tokens at
		 INNER JOIN accounts a ON at.account_id = a.id
		 INNER JOIN sites s ON a.site_id = s.id
		 WHERE at.name LIKE ? OR coalesce(at.token_group,'') LIKE ? OR a.username LIKE ? OR s.name LIKE ?
		 ORDER BY at.updated_at DESC LIMIT ?`,
		likePattern, likePattern, likePattern, likePattern, perCategory)

	// Search checkin logs
	checkinLogs := queryRows(h.db,
		`SELECT cl.*, a.username as account_username
		 FROM checkin_logs cl
		 INNER JOIN accounts a ON cl.account_id = a.id
		 WHERE coalesce(cl.message,'') LIKE ?
		 ORDER BY cl.created_at DESC LIMIT ?`,
		likePattern, perCategory)

	// Search proxy logs
	proxyLogs := queryRows(h.db,
		"SELECT * FROM proxy_logs WHERE coalesce(model_requested,'') LIKE ? ORDER BY created_at DESC LIMIT ?",
		likePattern, perCategory)

	// Search models
	modelRows := queryRows(h.db,
		`SELECT DISTINCT tma.model_name, COUNT(DISTINCT at.id) as token_count,
		        COUNT(DISTINCT a.id) as account_count, COUNT(DISTINCT s.id) as site_count
		 FROM token_model_availability tma
		 INNER JOIN account_tokens at ON tma.token_id = at.id
		 INNER JOIN accounts a ON at.account_id = a.id
		 INNER JOIN sites s ON a.site_id = s.id
		 WHERE tma.model_name LIKE ? AND tma.available = TRUE AND at.enabled = TRUE AND a.status = 'active'
		 GROUP BY tma.model_name
		 ORDER BY account_count DESC LIMIT ?`,
		likePattern, perCategory)

	writeJSON(w, http.StatusOK, map[string]any{
		"accounts":      normalizeSlice(accounts),
		"accountTokens": normalizeSlice(accountTokens),
		"sites":         normalizeSlice(sites),
		"checkinLogs":   normalizeSlice(checkinLogs),
		"proxyLogs":     normalizeSlice(proxyLogs),
		"models":        normalizeSlice(modelRows),
	})
}

func queryRows(db *sqlx.DB, query string, args ...any) []map[string]any {
	rows, err := db.Queryx(rebindAdminQuery(db, query), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			continue
		}
		result = append(result, mapKeysToCamel(row))
	}
	return result
}

func rebindAdminQuery(db *sqlx.DB, query string) string {
	if db == nil {
		return query
	}
	return db.Rebind(query)
}

func normalizeSlice(rows []map[string]any) []map[string]any {
	if rows == nil {
		return []map[string]any{}
	}
	return rows
}

// snakeToCamel converts snake_case to camelCase.
// e.g. "model_pattern" -> "modelPattern", "id" -> "id"
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// mapKeysToCamel returns a new map with all keys converted from snake_case to camelCase.
func mapKeysToCamel(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[snakeToCamel(k)] = v
	}
	return result
}

func getQueryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
