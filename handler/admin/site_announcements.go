package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/scheduler"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/service/notify"
)

// RegisterSiteAnnouncementsRoutes registers all /api/site-announcements routes.
// Also wires the background SiteAnnouncementScheduler to real SyncSiteAnnouncements
// (#272). Routes register before StartBackgroundServices, so the package-level
// default is installed for NewSiteAnnouncementScheduler to pick up.
func RegisterSiteAnnouncementsRoutes(r chi.Router, db *sqlx.DB) {
	wireSiteAnnouncementSchedulerSync()

	handler := &siteAnnouncementsHandler{db: db}

	r.Get("/api/site-announcements", handler.listAnnouncements)
	r.Post("/api/site-announcements/{id}/read", handler.markRead)
	r.Post("/api/site-announcements/read-all", handler.markAllRead)
	r.Delete("/api/site-announcements", handler.deleteAll)
	r.Post("/api/site-announcements/sync", handler.syncAnnouncements)
}

// wireSiteAnnouncementSchedulerSync injects admin SyncSiteAnnouncements into the
// scheduler package without creating an app→admin import cycle (admin already
// imports scheduler; app cannot import admin).
func wireSiteAnnouncementSchedulerSync() {
	scheduler.SetDefaultSiteAnnouncementSyncFunc(func(db *sqlx.DB) scheduler.SiteAnnouncementSyncResult {
		result := SyncSiteAnnouncements(db, nil)
		return scheduler.SiteAnnouncementSyncResult{
			ScannedSites:  result.ScannedSites,
			Inserted:      result.Inserted,
			Updated:       result.Updated,
			Unsupported:   result.Unsupported,
			Notifications: result.Notifications,
			Events:        result.Events,
			Failed:        result.Failed,
		}
	})
}

type siteAnnouncementsHandler struct {
	db *sqlx.DB
}

// SiteAnnouncementSyncResult mirrors the TS syncSiteAnnouncements result shape.
// Used by the admin HTTP task path and by SiteAnnouncementScheduler via SyncSiteAnnouncements.
type SiteAnnouncementSyncResult struct {
	ScannedSites  int                          `json:"scannedSites"`
	Inserted      int                          `json:"inserted"`
	Updated       int                          `json:"updated"`
	Unsupported   int                          `json:"unsupported"`
	Notifications int                          `json:"notifications"`
	Events        int                          `json:"events"`
	Failed        int                          `json:"failed"`
	FailedSites   []SiteAnnouncementFailedSite `json:"failedSites"`
}

// SiteAnnouncementFailedSite is one site that failed during announcement sync.
type SiteAnnouncementFailedSite struct {
	SiteID   int64  `json:"siteId"`
	SiteName string `json:"siteName"`
	Message  string `json:"message"`
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

	rows, err := h.db.Queryx(h.db.Rebind(query), args...)
	if err != nil {
		slog.Error("Failed to load announcements", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load announcements"})
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
	h.db.Exec(h.db.Rebind("UPDATE site_announcements SET read_at = ? WHERE id = ?"), now, id)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// POST /api/site-announcements/read-all
func (h *siteAnnouncementsHandler) markAllRead(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	h.db.Exec(h.db.Rebind("UPDATE site_announcements SET read_at = ?"), now)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// DELETE /api/site-announcements
func (h *siteAnnouncementsHandler) deleteAll(w http.ResponseWriter, r *http.Request) {
	h.db.Exec("DELETE FROM site_announcements")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// POST /api/site-announcements/sync
// Queues a real background sync against active sites (or a single siteId).
func (h *siteAnnouncementsHandler) syncAnnouncements(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SiteID any `json:"siteId"`
	}
	// Body is optional; ignore decode errors for empty payloads.
	_ = decodeJSONRequest(r, &body)

	var siteID *int64
	if body.SiteID != nil {
		switch v := body.SiteID.(type) {
		case float64:
			if v > 0 {
				id := int64(v)
				siteID = &id
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && parsed > 0 {
				siteID = &parsed
			}
		case json.Number:
			if parsed, err := v.Int64(); err == nil && parsed > 0 {
				siteID = &parsed
			}
		}
	}

	title := "同步站点公告"
	dedupeKey := "site-announcements:all"
	if siteID != nil {
		title = fmt.Sprintf("同步站点公告 #%d", *siteID)
		dedupeKey = fmt.Sprintf("site-announcements:%d", *siteID)
	}

	db := h.db
	task, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:      "site-announcements-sync",
		Title:     title,
		DedupeKey: dedupeKey,
	}, func() (any, error) {
		result := SyncSiteAnnouncements(db, siteID)
		return result, nil
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  reused,
		"taskId":  task.ID,
	})
}

// SyncSiteAnnouncements fetches announcements for active sites (or one site)
// via platform adapters and upserts site_announcements. Exported so the
// background SiteAnnouncementScheduler can reuse this path without HTTP (#272).
func SyncSiteAnnouncements(db *sqlx.DB, siteID *int64) SiteAnnouncementSyncResult {
	result := SiteAnnouncementSyncResult{
		FailedSites: []SiteAnnouncementFailedSite{},
	}

	type siteRow struct {
		ID       int64   `db:"id"`
		Name     string  `db:"name"`
		URL      string  `db:"url"`
		Platform string  `db:"platform"`
		APIKey   *string `db:"api_key"`
	}

	var sites []siteRow
	var err error
	if siteID != nil && *siteID > 0 {
		err = db.Select(&sites, db.Rebind(`SELECT id, name, url, platform, api_key FROM sites WHERE id = ?`), *siteID)
	} else {
		err = db.Select(&sites, `SELECT id, name, url, platform, api_key FROM sites WHERE status = 'active'`)
	}
	if err != nil {
		slog.Error("site-announcement sync: failed to query sites", "error", err)
		result.Failed++
		result.FailedSites = append(result.FailedSites, SiteAnnouncementFailedSite{
			SiteID:   0,
			SiteName: "",
			Message:  err.Error(),
		})
		return result
	}

	ctx := context.Background()
	for _, site := range sites {
		result.ScannedSites++
		adapter := platform.GetAdapter(site.Platform)
		if adapter == nil {
			result.Unsupported++
			continue
		}

		accessToken := strings.TrimSpace(stringPtrValue(site.APIKey))
		if accessToken == "" {
			accessToken = resolveSiteAccessToken(db, site.ID)
		}

		anns, annErr := adapter.GetSiteAnnouncements(ctx, site.URL, accessToken, nil, nil)
		if annErr != nil {
			result.Failed++
			result.FailedSites = append(result.FailedSites, SiteAnnouncementFailedSite{
				SiteID:   site.ID,
				SiteName: site.Name,
				Message:  annErr.Error(),
			})
			continue
		}

		seenAt := service.FormatUtcSqlDateTime(time.Now())
		for _, announcement := range anns {
			sourceKey := strings.TrimSpace(announcement.SourceKey)
			if sourceKey == "" {
				continue
			}

			var existingID int64
			lookupErr := db.Get(&existingID, db.Rebind(`
				SELECT id FROM site_announcements WHERE site_id = ? AND source_key = ? LIMIT 1
			`), site.ID, sourceKey)

			rawPayload := ""
			if len(announcement.RawPayload) > 0 {
				rawPayload = string(announcement.RawPayload)
			}

			if lookupErr == nil && existingID > 0 {
				_, _ = db.Exec(db.Rebind(`
					UPDATE site_announcements
					SET platform = ?, title = ?, content = ?, level = ?, source_url = ?,
					    starts_at = ?, ends_at = ?, upstream_created_at = ?, upstream_updated_at = ?,
					    last_seen_at = ?, raw_payload = ?
					WHERE id = ?
				`),
					site.Platform,
					announcement.Title,
					announcement.Content,
					defaultLevel(announcement.Level),
					emptyToNil(announcement.SourceURL),
					emptyToNil(announcement.StartsAt),
					emptyToNil(announcement.EndsAt),
					emptyToNil(announcement.UpstreamCreatedAt),
					emptyToNil(announcement.UpstreamUpdatedAt),
					seenAt,
					emptyToNil(rawPayload),
					existingID,
				)
				result.Updated++
				continue
			}

			insertRes, insertErr := db.Exec(db.Rebind(`
				INSERT INTO site_announcements (
					site_id, platform, source_key, title, content, level, source_url,
					starts_at, ends_at, upstream_created_at, upstream_updated_at,
					first_seen_at, last_seen_at, raw_payload
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`),
				site.ID,
				site.Platform,
				sourceKey,
				announcement.Title,
				announcement.Content,
				defaultLevel(announcement.Level),
				emptyToNil(announcement.SourceURL),
				emptyToNil(announcement.StartsAt),
				emptyToNil(announcement.EndsAt),
				emptyToNil(announcement.UpstreamCreatedAt),
				emptyToNil(announcement.UpstreamUpdatedAt),
				seenAt,
				seenAt,
				emptyToNil(rawPayload),
			)
			if insertErr != nil {
				result.Failed++
				result.FailedSites = append(result.FailedSites, SiteAnnouncementFailedSite{
					SiteID:   site.ID,
					SiteName: site.Name,
					Message:  insertErr.Error(),
				})
				continue
			}
			announcementID, _ := insertRes.LastInsertId()
			result.Inserted++

			title := "站点公告：" + site.Name
			message := buildAnnouncementMessage(announcement)
			_, _ = db.Exec(db.Rebind(`
				INSERT INTO events (type, title, message, level, related_id, related_type, created_at, read)
				VALUES ('site_notice', ?, ?, ?, ?, 'site_announcement', ?, 0)
			`), title, message, defaultLevel(announcement.Level), announcementID, seenAt)
			result.Events++

			// Best-effort notify; failures do not fail the whole sync.
			cfg := safeConfig()
			if cfg != nil {
				_, _ = notify.SendNotification(cfg, title, message, defaultLevel(announcement.Level), nil)
				result.Notifications++
			}
		}
	}

	return result
}

func resolveSiteAccessToken(db *sqlx.DB, siteID int64) string {
	var token *string
	err := db.Get(&token, db.Rebind(`
		SELECT access_token FROM accounts
		WHERE site_id = ? AND status = 'active'
		ORDER BY id ASC
		LIMIT 1
	`), siteID)
	if err != nil || token == nil {
		return ""
	}
	return strings.TrimSpace(*token)
}

func buildAnnouncementMessage(row platform.SiteAnnouncement) string {
	title := strings.TrimSpace(row.Title)
	content := strings.TrimSpace(row.Content)
	if title != "" && content != "" && title != content && strings.ToLower(title) != "site notice" {
		return title + "\n" + content
	}
	if content != "" {
		return content
	}
	return title
}

func defaultLevel(level string) string {
	level = strings.TrimSpace(level)
	if level == "" {
		return "info"
	}
	return level
}

func emptyToNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func safeConfig() *config.Config {
	defer func() { _ = recover() }()
	return config.Get()
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
