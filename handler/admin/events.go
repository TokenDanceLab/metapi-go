package admin

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterEventsRoutes registers all /api/events routes.
func RegisterEventsRoutes(r chi.Router, db *sqlx.DB) {
	handler := &eventsHandler{db: db}

	r.Get("/api/events", handler.listEvents)
	r.Get("/api/events/count", handler.countUnread)
	r.Post("/api/events/{id}/read", handler.markRead)
	r.Post("/api/events/read-all", handler.markAllRead)
	r.Delete("/api/events", handler.deleteAll)
}

type eventsHandler struct {
	db *sqlx.DB
}

// ---- List Events ----
// GET /api/events?limit=&offset=&type=&read=
func (h *eventsHandler) listEvents(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(getQueryInt(r, "limit", 30), 1, 500)
	offset := maxInt(0, getQueryInt(r, "offset", 0))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	readFilter := strings.TrimSpace(r.URL.Query().Get("read"))

	var conditions []string
	var args []any

	if typeFilter != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, typeFilter)
	}
	if readFilter == "true" {
		conditions = append(conditions, "read = ?")
		args = append(args, true)
	} else if readFilter == "false" {
		conditions = append(conditions, "read = ?")
		args = append(args, false)
	}

	query := "SELECT * FROM events"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.db.Queryx(h.db.Rebind(query), args...)
	if err != nil {
		slog.Error("Failed to load events", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load events"})
		return
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			slog.Error("Failed to read event row", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load events"})
			return
		}
		events = append(events, row)
	}
	if events == nil {
		events = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, events)
}

// ---- Unread Count ----
// GET /api/events/count
func (h *eventsHandler) countUnread(w http.ResponseWriter, r *http.Request) {
	var count int
	err := h.db.Get(&count, h.db.Rebind("SELECT COUNT(*) FROM events WHERE read = ?"), false)
	if err != nil {
		slog.Error("Failed to count unread events", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load events"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

// ---- Mark One Read ----
// POST /api/events/:id/read
func (h *eventsHandler) markRead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}
	h.db.Exec(h.db.Rebind("UPDATE events SET read = ? WHERE id = ?"), true, id)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---- Mark All Read ----
// POST /api/events/read-all
func (h *eventsHandler) markAllRead(w http.ResponseWriter, r *http.Request) {
	h.db.Exec(h.db.Rebind("UPDATE events SET read = ? WHERE read = ?"), true, false)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---- Delete All ----
// DELETE /api/events
func (h *eventsHandler) deleteAll(w http.ResponseWriter, r *http.Request) {
	h.db.Exec("DELETE FROM events")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
