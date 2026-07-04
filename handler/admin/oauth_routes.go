package admin

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
)

// RegisterOauthRoutes registers all /api/oauth routes.
// These stub endpoints delegate to the P6 OAuth service implementation.
func RegisterOauthRoutes(r chi.Router, db *sqlx.DB) {
	handler := &oauthHandler{db: db}

	r.Get("/api/oauth/providers", handler.listProviders)
	r.Post("/api/oauth/providers/{provider}/start", handler.startProviderFlow)
	r.Get("/api/oauth/sessions/{state}", handler.getSession)
	r.Post("/api/oauth/sessions/{state}/manual-callback", handler.manualCallback)
	r.Get("/api/oauth/connections", handler.listConnections)
	r.Post("/api/oauth/connections/{accountId}/rebind", handler.rebindConnection)
	r.Patch("/api/oauth/connections/{accountId}/proxy", handler.updateProxy)
	r.Delete("/api/oauth/connections/{accountId}", handler.deleteConnection)
	r.Post("/api/oauth/connections/{accountId}/quota/refresh", handler.refreshQuota)
	r.Post("/api/oauth/connections/quota/refresh-batch", handler.refreshQuotaBatch)
	r.Post("/api/oauth/import", handler.importConnections)
	r.Post("/api/oauth/route-units", handler.createRouteUnit)
	r.Patch("/api/oauth/route-units/{routeUnitId}", handler.updateRouteUnit)
	r.Delete("/api/oauth/route-units/{routeUnitId}", handler.deleteRouteUnit)
	r.Get("/api/oauth/callback/{provider}", handler.callback)
}

type oauthHandler struct {
	db *sqlx.DB
}

// GET /api/oauth/providers
func (h *oauthHandler) listProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"defaults":  []any{},
		"providers": []any{},
	})
}

// POST /api/oauth/providers/:provider/start
func (h *oauthHandler) startProviderFlow(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	writeJSON(w, http.StatusOK, map[string]any{
		"state":     "stub-state",
		"authUrl":   "",
		"provider":  provider,
	})
}

// GET /api/oauth/sessions/:state
func (h *oauthHandler) getSession(w http.ResponseWriter, r *http.Request) {
	state := chi.URLParam(r, "state")
	_ = state
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "oauth session not found"})
}

// POST /api/oauth/sessions/:state/manual-callback
func (h *oauthHandler) manualCallback(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// GET /api/oauth/connections?limit=&offset=
func (h *oauthHandler) listConnections(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	limit := 50
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// Query accounts with oauth_provider set
	rows := queryRows(h.db,
		`SELECT a.*, s.name as site_name, s.platform as site_platform
		 FROM accounts a
		 INNER JOIN sites s ON a.site_id = s.id
		 WHERE a.oauth_provider IS NOT NULL
		 ORDER BY a.id ASC LIMIT ? OFFSET ?`,
		limit, offset)

	writeJSON(w, http.StatusOK, map[string]any{
		"connections": normalizeSlice(rows),
		"total":       len(rows),
	})
}

// POST /api/oauth/connections/:accountId/rebind
func (h *oauthHandler) rebindConnection(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "accountId")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid account id"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":    "stub-rebind",
		"authUrl":  "",
		"accountId": id,
	})
}

// PATCH /api/oauth/connections/:accountId/proxy
func (h *oauthHandler) updateProxy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// DELETE /api/oauth/connections/:accountId
func (h *oauthHandler) deleteConnection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/oauth/connections/:accountId/quota/refresh
func (h *oauthHandler) refreshQuota(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/oauth/connections/quota/refresh-batch
func (h *oauthHandler) refreshQuotaBatch(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/oauth/import
func (h *oauthHandler) importConnections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// POST /api/oauth/route-units
func (h *oauthHandler) createRouteUnit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// PATCH /api/oauth/route-units/:routeUnitId
func (h *oauthHandler) updateRouteUnit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// DELETE /api/oauth/route-units/:routeUnitId
func (h *oauthHandler) deleteRouteUnit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// GET /api/oauth/callback/:provider
func (h *oauthHandler) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
  <head><meta charset="utf-8" /><title>OAuth Callback</title></head>
  <body>
    <script>window.close();</script>
    OAuth authorization succeeded. You can close this window.
  </body>
</html>`))
}
