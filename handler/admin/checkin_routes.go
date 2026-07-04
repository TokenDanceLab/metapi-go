package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterCheckinRoutes registers all /api/checkin routes.
func RegisterCheckinRoutes(r chi.Router, db *sqlx.DB) {
	handler := &checkinHandler{db: db}

	r.Post("/api/checkin/trigger", handler.triggerAll)
	r.Post("/api/checkin/trigger/{id}", handler.triggerOne)
	r.Get("/api/checkin/logs", handler.getLogs)
	r.Put("/api/checkin/schedule", handler.updateSchedule)
}

type checkinHandler struct {
	db *sqlx.DB
}

// POST /api/checkin/trigger
func (h *checkinHandler) triggerAll(w http.ResponseWriter, r *http.Request) {
	// Stub: background task not yet wired
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"jobId":   "stub-checkin-all",
		"status":  "pending",
		"message": "已开始全部签到，请稍后查看签到日志",
	})
}

// POST /api/checkin/trigger/:id
func (h *checkinHandler) triggerOne(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account id"})
		return
	}

	// Stub: checkin not yet wired
	writeJSON(w, http.StatusOK, map[string]any{
		"success": false,
		"message": "checkin not yet implemented",
		"id":      id,
	})
}

// GET /api/checkin/logs?limit=&offset=&accountId=
func (h *checkinHandler) getLogs(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(getQueryInt(r, "limit", 50), 1, 500)
	offset := maxInt(0, getQueryInt(r, "offset", 0))
	accountIDStr := r.URL.Query().Get("accountId")

	query := `SELECT cl.*, a.username as account_username, s.name as site_name
		FROM checkin_logs cl
		INNER JOIN accounts a ON cl.account_id = a.id
		INNER JOIN sites s ON a.site_id = s.id`
	var args []any

	if accountIDStr != "" {
		if aid, err := strconv.ParseInt(accountIDStr, 10, 64); err == nil && aid > 0 {
			query += " WHERE cl.account_id = ?"
			args = append(args, aid)
		}
	}

	query += " ORDER BY cl.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows := queryRows(h.db, query, args...)
	writeJSON(w, http.StatusOK, normalizeSlice(rows))
}

// PUT /api/checkin/schedule
func (h *checkinHandler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode          *string `json:"mode,omitempty"`
		Cron          *string `json:"cron,omitempty"`
		IntervalHours *int    `json:"intervalHours,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": "Invalid body"})
		return
	}

	// Stub: persist to settings
	now := time.Now().UTC().Format(time.RFC3339)
	_ = now

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"mode":         coalesceStr(body.Mode, "cron"),
		"cron":         coalesceStr(body.Cron, ""),
		"intervalHours": coalesceInt(body.IntervalHours, 0),
	})
}

func coalesceInt(i *int, fallback int) int {
	if i == nil {
		return fallback
	}
	return *i
}
