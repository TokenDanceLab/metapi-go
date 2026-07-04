package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterSiteAnnouncementsRoutes registers all /api/site-announcements routes.
func RegisterSiteAnnouncementsRoutes(r chi.Router, db *sqlx.DB) {
	handler := &siteAnnouncementsHandler{db: db}

	r.Get("/api/site-announcements", handler.listAnnouncements)
	r.Post("/api/site-announcements/{id}/read", handler.markRead)
	r.Post("/api/site-announcements/read-all", handler.markAllRead)
	r.Delete("/api/site-announcements", handler.deleteAll)
	r.Post("/api/site-announcements/sync", handler.syncAnnouncements)
}

type siteAnnouncementsHandler struct {
	db *sqlx.DB
}

// GET /api/site-announcements?limit=&offset=&siteId=&platform=&read=&status=
func (h *siteAnnouncementsHandler) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(getQueryInt(r, "limit", 50), 1, 500)
	offset := maxInt(0, getQueryInt(r, "offset", 0))
	readFilter := strings.TrimSpace(r.URL.Query().Get("read"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	siteIDFilter := strings.TrimSpace(r.URL.Query().Get("siteId"))
	platformFilter := strings.TrimSpace(r.URL.Query().Get("platform"))

	var conditions []string
	var args []any

	if siteIDFilter != "" {
		if id, err := strconv.ParseInt(siteIDFilter, 10, 64); err == nil && id > 0 {
			conditions = append(conditions, "site_id = ?")
			args = append(args, id)
		}
	}
	if platformFilter != "" {
		conditions = append(conditions, "platform = ?")
		args = append(args, platformFilter)
	}

	query := "SELECT * FROM site_announcements"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY first_seen_at DESC"

	rows, err := h.db.Queryx(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var all []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			continue
		}
		all = append(all, row)
	}

	// Apply read filter
	if readFilter == "true" {
		filtered := make([]map[string]any, 0)
		for _, row := range all {
			if hasValue(row["read_at"]) {
				filtered = append(filtered, row)
			}
		}
		all = filtered
	} else if readFilter == "false" {
		filtered := make([]map[string]any, 0)
		for _, row := range all {
			if !hasValue(row["read_at"]) {
				filtered = append(filtered, row)
			}
		}
		all = filtered
	}

	// Apply status filter
	now := time.Now()
	if statusFilter == "dismissed" {
		filtered := make([]map[string]any, 0)
		for _, row := range all {
			if hasValue(row["dismissed_at"]) {
				filtered = append(filtered, row)
			}
		}
		all = filtered
	} else if statusFilter == "active" {
		filtered := make([]map[string]any, 0)
		for _, row := range all {
			if !hasValue(row["dismissed_at"]) {
				endsAt := parseTime(row["ends_at"])
				if endsAt == nil || !endsAt.Before(now) {
					filtered = append(filtered, row)
				}
			}
		}
		all = filtered
	} else if statusFilter == "expired" {
		filtered := make([]map[string]any, 0)
		for _, row := range all {
			if !hasValue(row["dismissed_at"]) {
				endsAt := parseTime(row["ends_at"])
				if endsAt != nil && endsAt.Before(now) {
					filtered = append(filtered, row)
				}
			}
		}
		all = filtered
	}

	// Apply pagination after filters
	if offset >= len(all) {
		all = []map[string]any{}
	} else {
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		all = all[offset:end]
	}

	writeJSON(w, http.StatusOK, normalizeSlice(all))
}

// POST /api/site-announcements/:id/read
func (h *siteAnnouncementsHandler) markRead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	h.db.Exec("UPDATE site_announcements SET read_at = ? WHERE id = ?", now, id)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// POST /api/site-announcements/read-all
func (h *siteAnnouncementsHandler) markAllRead(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	h.db.Exec("UPDATE site_announcements SET read_at = ?", now)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// DELETE /api/site-announcements
func (h *siteAnnouncementsHandler) deleteAll(w http.ResponseWriter, r *http.Request) {
	h.db.Exec("DELETE FROM site_announcements")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// POST /api/site-announcements/sync
func (h *siteAnnouncementsHandler) syncAnnouncements(w http.ResponseWriter, r *http.Request) {
	// Stub: sync not yet implemented
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"taskId":  "stub",
	})
}

func hasValue(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val) != ""
	case []byte:
		return strings.TrimSpace(string(val)) != ""
	default:
		return true
	}
}

func parseTime(v any) *time.Time {
	if v == nil {
		return nil
	}
	var s string
	switch val := v.(type) {
	case string:
		s = val
	case []byte:
		s = string(val)
	case time.Time:
		return &val
	default:
		return nil
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Try other formats
		t, err = time.Parse("2006-01-02 15:04:05", s)
		if err != nil {
			return nil
		}
	}
	return &t
}
