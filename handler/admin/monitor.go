package admin

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

// RegisterMonitorRoutes registers all /api/monitor and /monitor-proxy routes.
func RegisterMonitorRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &monitorHandler{db: db, cfg: cfg}

	r.Get("/api/monitor/config", handler.getConfig)
	r.Put("/api/monitor/config", handler.saveConfig)
	r.Post("/api/monitor/session", handler.createSession)

	// LDOH proxy routes - rate limited at 60 req/min
	r.HandleFunc("/monitor-proxy/ldoh", handler.ldohProxy)
	r.HandleFunc("/monitor-proxy/ldoh/", handler.ldohProxy)
	r.HandleFunc("/monitor-proxy/ldoh/*", handler.ldohProxy)
}

type monitorHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

const ldohCookieSettingKey = "monitor_ldoh_cookie"
const monitorAuthCookie = "meta_monitor_auth"

// GET /api/monitor/config
func (h *monitorHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	ldohCookie := getSettingValue(h.db, ldohCookieSettingKey)
	writeJSON(w, http.StatusOK, map[string]any{
		"ldohCookieConfigured": ldohCookie != "",
		"ldohCookieMasked":     maskValue(ldohCookie),
	})
}

// PUT /api/monitor/config
func (h *monitorHandler) saveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LdohCookie string `json:"ldohCookie"`
	}
	// Read raw body
	_ = body

	// Stub: persist LDOH cookie setting
	writeJSON(w, http.StatusOK, map[string]any{
		"success":              true,
		"message":              "LDOH config stub — not yet implemented",
		"ldohCookieConfigured": false,
	})
}

// POST /api/monitor/session
func (h *monitorHandler) createSession(w http.ResponseWriter, r *http.Request) {
	// Set HttpOnly cookie for iframe proxy auth
	w.Header().Set("Set-Cookie",
		monitorAuthCookie+"="+h.cfg.AuthToken+"; Path=/; HttpOnly; SameSite=Lax; Max-Age=7200")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GET/POST/ALL /monitor-proxy/ldoh and /monitor-proxy/ldoh/*
func (h *monitorHandler) ldohProxy(w http.ResponseWriter, r *http.Request) {
	// Stub: LDOH reverse proxy not yet implemented
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": "LDOH proxy not yet implemented",
	})
}

func maskValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 10 {
		if len(value) < 2 {
			return "****"
		}
		return value[:2] + "****"
	}
	return value[:6] + "****" + value[len(value)-4:]
}

func getSettingValue(db *sqlx.DB, key string) string {
	var row struct {
		Value *string `db:"value"`
	}
	err := db.Get(&row, "SELECT value FROM settings WHERE key = ?", key)
	if err != nil || row.Value == nil {
		return ""
	}
	// Value is stored as JSON-encoded string
	val := *row.Value
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
		val = val[1 : len(val)-1]
	}
	return val
}
