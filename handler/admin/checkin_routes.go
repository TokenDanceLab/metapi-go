package admin

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	checkinservice "github.com/tokendancelab/metapi-go/service/checkin"
)

// RegisterCheckinRoutes registers all /api/checkin routes.
func RegisterCheckinRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	handler := &checkinHandler{db: db, cfg: cfg}

	r.Post("/api/checkin/trigger", handler.triggerAll)
	r.Post("/api/checkin/trigger/{id}", handler.triggerOne)
	r.Get("/api/checkin/logs", handler.getLogs)
	r.Put("/api/checkin/schedule", handler.updateSchedule)
}

type checkinHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

// POST /api/checkin/trigger
func (h *checkinHandler) triggerAll(w http.ResponseWriter, r *http.Request) {
	results := checkinservice.CheckinAll(h.cfg, h.db, nil, "manual")
	summary := summarizeCheckinResults(results)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": summary.Failed == 0,
		"queued":  false,
		"status":  "completed",
		"message": "签到执行完成",
		"summary": summary,
		"results": results,
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

	result := checkinservice.CheckinAccount(h.cfg, h.db, id, &checkinservice.CheckinOptions{
		ScheduleMode: "manual",
	})
	if result.Message == "account not found" {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false,
			"message": result.Message,
			"id":      id,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": result.Success,
		"status":  result.Status,
		"skipped": result.Skipped,
		"message": result.Message,
		"reward":  result.Reward,
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid body"})
		return
	}

	state, err := applyCheckinScheduleSettings(h.db, h.cfg, checkinSchedulePatch{
		Mode:          body.Mode,
		Cron:          body.Cron,
		IntervalHours: body.IntervalHours,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"mode":          state.Mode,
		"cron":          state.Cron,
		"intervalHours": state.IntervalHours,
	})
}

type checkinSummary struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func summarizeCheckinResults(results []checkinservice.CheckinAllResult) checkinSummary {
	summary := checkinSummary{Total: len(results)}
	for _, item := range results {
		switch item.Result.Status {
		case checkinservice.CheckinSuccess:
			summary.Success++
		case checkinservice.CheckinSkipped:
			summary.Skipped++
		default:
			summary.Failed++
		}
	}
	return summary
}
