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

// RegisterTokenRoutes registers all /api/routes and /api/channels routes.
func RegisterTokenRoutes(r chi.Router, db *sqlx.DB) {
	handler := &tokenRoutesHandler{db: db}

	// Route list
	r.Get("/api/routes/lite", handler.listLite)
	r.Get("/api/routes/summary", handler.listSummary)
	r.Get("/api/routes", handler.listRoutes)

	// Route CRUD
	r.Post("/api/routes", handler.createRoute)
	r.Put("/api/routes/{id}", handler.updateRoute)
	r.Delete("/api/routes/{id}", handler.deleteRoute)
	r.Post("/api/routes/batch", handler.batchRoutes)
	r.Post("/api/routes/rebuild", handler.rebuildRoutes)

	// Route channels
	r.Get("/api/routes/{id}/channels", handler.getRouteChannels)
	r.Post("/api/routes/{id}/channels", handler.addChannel)
	r.Post("/api/routes/{id}/channels/batch", handler.batchAddChannels)
	r.Post("/api/routes/{id}/cooldown/clear", handler.clearCooldown)

	// Channel operations
	r.Put("/api/channels/batch", handler.batchUpdateChannels)
	r.Put("/api/channels/{channelId}", handler.updateChannel)
	r.Delete("/api/channels/{channelId}", handler.deleteChannel)

	// Route decisions
	r.Get("/api/routes/decision", handler.routeDecision)
	r.Post("/api/routes/decision/batch", handler.routeDecisionBatch)
	r.Post("/api/routes/decision/by-route/batch", handler.routeDecisionByRouteBatch)
	r.Post("/api/routes/decision/route-wide/batch", handler.routeDecisionRouteWideBatch)
	r.Post("/api/routes/decision/refresh", handler.routeDecisionRefresh)
}

type tokenRoutesHandler struct {
	db *sqlx.DB
}

// ---- List Lite ----
// GET /api/routes/lite
func (h *tokenRoutesHandler) listLite(w http.ResponseWriter, r *http.Request) {
	rows := queryRows(h.db, "SELECT id, model_pattern, display_name, display_icon, route_mode, routing_strategy, enabled FROM token_routes ORDER BY id ASC")
	type srcRow struct {
		GroupRouteID  int64 `db:"group_route_id"`
		SourceRouteID int64 `db:"source_route_id"`
	}
	var srcRows []srcRow
	h.db.Select(&srcRows, "SELECT group_route_id, source_route_id FROM route_group_sources")
	sourceIdsByRoute := map[int64][]int64{}
	for _, sr := range srcRows {
		sourceIdsByRoute[sr.GroupRouteID] = append(sourceIdsByRoute[sr.GroupRouteID], sr.SourceRouteID)
	}
	for _, row := range rows {
		rid := toInt64(row["id"])
		if ids, ok := sourceIdsByRoute[rid]; ok {
			row["sourceRouteIds"] = ids
		} else {
			row["sourceRouteIds"] = []int64{}
		}
	}
	writeJSON(w, http.StatusOK, normalizeSlice(rows))
}

// ---- List Summary ----
// GET /api/routes/summary
func (h *tokenRoutesHandler) listSummary(w http.ResponseWriter, r *http.Request) {
	rows := queryRows(h.db, "SELECT * FROM token_routes ORDER BY id ASC")
	result := make([]map[string]any, 0)
	for _, route := range rows {
		routeID := toInt64(route["id"])
		var channelCount, enabledCount int
		h.db.Get(&channelCount, "SELECT COUNT(*) FROM route_channels WHERE route_id = ?", routeID)
		h.db.Get(&enabledCount, "SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND enabled = 1", routeID)

		item := map[string]any{
			"id":                  route["id"],
			"modelPattern":        route["modelPattern"],
			"displayName":         route["displayName"],
			"displayIcon":         route["displayIcon"],
			"routeMode":           route["routeMode"],
			"sourceRouteIds":      []int64{},
			"modelMapping":        route["modelMapping"],
			"routingStrategy":     route["routingStrategy"],
			"enabled":             route["enabled"],
			"channelCount":        channelCount,
			"enabledChannelCount": enabledCount,
			"siteNames":           []string{},
			"decisionSnapshot":    nil,
			"decisionRefreshedAt": route["decisionRefreshedAt"],
		}
		// Parse decision snapshot
		if ds, ok := route["decisionSnapshot"].(string); ok && ds != "" {
			var parsed any
			if json.Unmarshal([]byte(ds), &parsed) == nil {
				item["decisionSnapshot"] = parsed
			}
		}
		result = append(result, item)
	}
	writeJSON(w, http.StatusOK, result)
}

// ---- List Routes ----
// GET /api/routes
func (h *tokenRoutesHandler) listRoutes(w http.ResponseWriter, r *http.Request) {
	rows := queryRows(h.db, "SELECT * FROM token_routes ORDER BY id ASC")
	result := make([]map[string]any, 0)
	for _, route := range rows {
		routeID := toInt64(route["id"])
		channelRows := queryRows(h.db,
			`SELECT rc.*, a.username, a.access_token, a.api_token, a.balance, a.status as account_status,
			        s.id as site_id, s.name as site_name, s.url as site_url, s.platform as site_platform, s.status as site_status
			 FROM route_channels rc
			 LEFT JOIN accounts a ON rc.account_id = a.id
			 LEFT JOIN sites s ON a.site_id = s.id
			 WHERE rc.route_id = ?`, routeID)

		var enrichedChannels []map[string]any
		for _, ch := range channelRows {
			enriched := map[string]any{
				"id":             ch["id"],
				"routeId":        ch["routeId"],
				"accountId":      ch["accountId"],
				"tokenId":        ch["tokenId"],
				"oauthRouteUnitId": ch["oauthRouteUnitId"],
				"sourceModel":    ch["sourceModel"],
				"priority":       ch["priority"],
				"weight":         ch["weight"],
				"enabled":        ch["enabled"],
				"manualOverride": ch["manualOverride"],
				"account": map[string]any{
					"id":          ch["accountId"],
					"username":    ch["username"],
					"accessToken": ch["accessToken"],
					"apiToken":    ch["apiToken"],
					"balance":     ch["balance"],
					"status":      ch["accountStatus"],
				},
				"site": map[string]any{
					"id":       ch["siteId"],
					"name":     ch["siteName"],
					"url":      ch["siteUrl"],
					"platform": ch["sitePlatform"],
					"status":   ch["siteStatus"],
				},
			}
			enrichedChannels = append(enrichedChannels, enriched)
		}

		item := route
		item["channels"] = enrichedChannels
		if ds, ok := route["decisionSnapshot"].(string); ok && ds != "" {
			var parsed any
			if json.Unmarshal([]byte(ds), &parsed) == nil {
				item["decisionSnapshot"] = parsed
			} else {
				item["decisionSnapshot"] = nil
			}
		}
		result = append(result, item)
	}
	writeJSON(w, http.StatusOK, result)
}

// ---- Create Route ----
// POST /api/routes
func (h *tokenRoutesHandler) createRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModelPattern    string  `json:"modelPattern"`
		RouteMode       *string `json:"routeMode"`
		DisplayName     *string `json:"displayName"`
		DisplayIcon     *string `json:"displayIcon"`
		SourceRouteIds  []int64 `json:"sourceRouteIds"`
		RoutingStrategy *string `json:"routingStrategy"`
		ModelMapping    any     `json:"modelMapping"`
		Enabled         *bool   `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	routeMode := "pattern"
	if body.RouteMode != nil {
		routeMode = strings.TrimSpace(*body.RouteMode)
	}

	modelPattern := strings.TrimSpace(body.ModelPattern)
	displayName := ""
	if body.DisplayName != nil {
		displayName = strings.TrimSpace(*body.DisplayName)
	}

	if routeMode == "explicit_group" {
		modelPattern = displayName
	}

	if routeMode != "explicit_group" && modelPattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "模型匹配不能为空"})
		return
	}

	if routeMode == "explicit_group" && displayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "显式群组必须填写对外模型名"})
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	routingStrategy := "weighted"
	if body.RoutingStrategy != nil {
		routingStrategy = *body.RoutingStrategy
	}

	now := time.Now().UTC().Format(time.RFC3339)

	result, err := h.db.Exec(
		`INSERT INTO token_routes (model_pattern, display_name, display_icon, route_mode, model_mapping, routing_strategy, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		modelPattern, strOrNull(&displayName), strOrNull(body.DisplayIcon), routeMode,
		nil, routingStrategy, enabled, now, now,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "创建路由失败"})
		return
	}

	id, _ := result.LastInsertId()
	// For explicit_group, insert source route references
	if routeMode == "explicit_group" && len(body.SourceRouteIds) > 0 {
		for _, srcID := range body.SourceRouteIds {
			h.db.Exec("INSERT INTO route_group_sources (group_route_id, source_route_id) VALUES (?, ?)", id, srcID)
		}
	}

	created := queryRow(h.db, "SELECT * FROM token_routes WHERE id = ?", id)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "创建路由失败"})
		return
	}
	created["sourceRouteIds"] = body.SourceRouteIds
	writeJSON(w, http.StatusOK, created)
}

// ---- Update Route ----
// PUT /api/routes/:id
func (h *tokenRoutesHandler) updateRoute(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "id 无效"})
		return
	}

	existing := queryRow(h.db, "SELECT * FROM token_routes WHERE id = ?", id)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "路由不存在"})
		return
	}

	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)

	now := time.Now().UTC().Format(time.RFC3339)

	if v, ok := body["modelPattern"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec("UPDATE token_routes SET model_pattern = ?, updated_at = ? WHERE id = ?", strings.TrimSpace(s), now, id)
		}
	}
	if v, ok := body["displayName"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec("UPDATE token_routes SET display_name = ?, updated_at = ? WHERE id = ?", s, now, id)
		}
	}
	if v, ok := body["displayIcon"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec("UPDATE token_routes SET display_icon = ?, updated_at = ? WHERE id = ?", s, now, id)
		}
	}
	if v, ok := body["enabled"]; ok {
		h.db.Exec("UPDATE token_routes SET enabled = ?, updated_at = ? WHERE id = ?", toBool(v), now, id)
	}
	if v, ok := body["routingStrategy"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec("UPDATE token_routes SET routing_strategy = ?, updated_at = ? WHERE id = ?", s, now, id)
		}
	}
	if v, ok := body["modelMapping"]; ok {
		mappingJSON, _ := json.Marshal(v)
		h.db.Exec("UPDATE token_routes SET model_mapping = ?, updated_at = ? WHERE id = ?", string(mappingJSON), now, id)
	}

	// Update source route IDs for explicit_group
	if v, ok := body["sourceRouteIds"]; ok {
		if ids, ok2 := v.([]any); ok2 {
			h.db.Exec("DELETE FROM route_group_sources WHERE group_route_id = ?", id)
			for _, rawID := range ids {
				switch rid := rawID.(type) {
				case float64:
					h.db.Exec("INSERT INTO route_group_sources (group_route_id, source_route_id) VALUES (?, ?)", id, int64(rid))
				}
			}
		}
	}

	updated := queryRow(h.db, "SELECT * FROM token_routes WHERE id = ?", id)
	var srcIDs []int64
	h.db.Select(&srcIDs, "SELECT source_route_id FROM route_group_sources WHERE group_route_id = ?", id)
	updated["sourceRouteIds"] = srcIDs
	writeJSON(w, http.StatusOK, updated)
}

// ---- Delete Route ----
// DELETE /api/routes/:id
func (h *tokenRoutesHandler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "id 无效"})
		return
	}

	h.db.Exec("DELETE FROM route_group_sources WHERE group_route_id = ?", id)
	h.db.Exec("DELETE FROM route_group_sources WHERE source_route_id = ?", id)
	h.db.Exec("DELETE FROM route_channels WHERE route_id = ?", id)
	h.db.Exec("DELETE FROM token_routes WHERE id = ?", id)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Batch Routes ----
// POST /api/routes/batch
func (h *tokenRoutesHandler) batchRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string  `json:"action"`
		IDs    []int64 `json:"ids"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "ids 必须是非空数组"})
		return
	}

	action := strings.TrimSpace(body.Action)
	if action != "enable" && action != "disable" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "action 必须是 enable 或 disable"})
		return
	}

	enabled := action == "enable"
	now := time.Now().UTC().Format(time.RFC3339)
	updated := 0
	for _, id := range body.IDs {
		res, err := h.db.Exec("UPDATE token_routes SET enabled = ?, updated_at = ? WHERE id = ?", enabled, now, id)
		if err == nil {
			n, _ := res.RowsAffected()
			updated += int(n)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"updatedCount": updated,
	})
}

// ---- Rebuild Routes ----
// POST /api/routes/rebuild
func (h *tokenRoutesHandler) rebuildRoutes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"jobId":   "stub-rebuild",
		"status":  "pending",
		"message": "已开始路由重建，请稍后查看程序日志",
	})
}

// ---- Get Route Channels ----
// GET /api/routes/:id/channels
func (h *tokenRoutesHandler) getRouteChannels(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "路由不存在"})
		return
	}

	channelRows := queryRows(h.db,
		`SELECT rc.*, a.username, a.access_token, a.api_token, a.balance, a.status as account_status,
		        s.id as site_id, s.name as site_name, s.url as site_url, s.platform as site_platform, s.status as site_status
		 FROM route_channels rc
		 LEFT JOIN accounts a ON rc.account_id = a.id
		 LEFT JOIN sites s ON a.site_id = s.id
		 WHERE rc.route_id = ?`, id)
	var enrichedChans []map[string]any
	for _, ch := range channelRows {
		enriched := map[string]any{
			"id":             ch["id"],
			"routeId":        ch["routeId"],
			"accountId":      ch["accountId"],
			"tokenId":        ch["tokenId"],
			"oauthRouteUnitId": ch["oauthRouteUnitId"],
			"sourceModel":    ch["sourceModel"],
			"priority":       ch["priority"],
			"weight":         ch["weight"],
			"enabled":        ch["enabled"],
			"manualOverride": ch["manualOverride"],
			"account": map[string]any{
				"id":          ch["accountId"],
				"username":    ch["username"],
				"accessToken": ch["accessToken"],
				"apiToken":    ch["apiToken"],
				"balance":     ch["balance"],
				"status":      ch["accountStatus"],
			},
			"site": map[string]any{
				"id":       ch["siteId"],
				"name":     ch["siteName"],
				"url":      ch["siteUrl"],
				"platform": ch["sitePlatform"],
				"status":   ch["siteStatus"],
			},
		}
		enrichedChans = append(enrichedChans, enriched)
	}
	writeJSON(w, http.StatusOK, enrichedChans)
}

// ---- Add Channel ----
// POST /api/routes/:id/channels
func (h *tokenRoutesHandler) addChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	routeID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || routeID <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "路由不存在"})
		return
	}

	var body struct {
		AccountID   int64   `json:"accountId"`
		TokenID     *int64  `json:"tokenId"`
		SourceModel *string `json:"sourceModel"`
		Priority    *int64  `json:"priority"`
		Weight      *int64  `json:"weight"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	priority := int64(0)
	weight := int64(10)
	if body.Priority != nil {
		priority = *body.Priority
	}
	if body.Weight != nil {
		weight = *body.Weight
	}

	// Check for duplicates
	var dupCount int
	h.db.Get(&dupCount,
		"SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND account_id = ? AND (token_id = ? OR (token_id IS NULL AND ? IS NULL))",
		routeID, body.AccountID, body.TokenID, body.TokenID)
	if dupCount > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "该来源模型的通道已存在"})
		return
	}

	result, err := h.db.Exec(
		"INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override) VALUES (?, ?, ?, ?, ?, ?, 1, 0)",
		routeID, body.AccountID, body.TokenID, body.SourceModel, priority, weight,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "创建通道失败"})
		return
	}

	id, _ := result.LastInsertId()
	created := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", id)
	writeJSON(w, http.StatusOK, created)
}

// ---- Batch Add Channels ----
// POST /api/routes/:id/channels/batch
func (h *tokenRoutesHandler) batchAddChannels(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	routeID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || routeID <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "路由不存在"})
		return
	}

	var body struct {
		Channels []struct {
			AccountID   int64   `json:"accountId"`
			TokenID     *int64  `json:"tokenId"`
			SourceModel *string `json:"sourceModel"`
		} `json:"channels"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	created := 0
	skipped := 0
	var errors []string

	for _, ch := range body.Channels {
		var dupCount int
		h.db.Get(&dupCount,
			"SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND account_id = ? AND (token_id = ? OR (token_id IS NULL AND ? IS NULL))",
			routeID, ch.AccountID, ch.TokenID, ch.TokenID)
		if dupCount > 0 {
			skipped++
			continue
		}

		_, err := h.db.Exec(
			"INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override) VALUES (?, ?, ?, ?, 0, 10, 1, 1)",
			routeID, ch.AccountID, ch.TokenID, ch.SourceModel,
		)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		created++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"created": created,
		"skipped": skipped,
		"errors":  errors,
	})
}

// ---- Clear Cooldown ----
// POST /api/routes/:id/cooldown/clear
func (h *tokenRoutesHandler) clearCooldown(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	routeID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || routeID <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "路由不存在"})
		return
	}

	h.db.Exec(`UPDATE route_channels SET cooldown_until = NULL, consecutive_fail_count = 0, cooldown_level = 0 WHERE route_id = ?`, routeID)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Batch Update Channels ----
// PUT /api/channels/batch
func (h *tokenRoutesHandler) batchUpdateChannels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Updates []struct {
			ID       int64 `json:"id"`
			Priority int64 `json:"priority"`
		} `json:"updates"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	var updatedIDs []int64
	for _, update := range body.Updates {
		h.db.Exec("UPDATE route_channels SET priority = ?, manual_override = 1 WHERE id = ?", update.Priority, update.ID)
		updatedIDs = append(updatedIDs, update.ID)
	}

	var updatedChannels []map[string]any
	for _, cid := range updatedIDs {
		ch := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", cid)
		if ch != nil {
			updatedChannels = append(updatedChannels, ch)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"channels": normalizeSlice(updatedChannels),
	})
}

// ---- Update Channel ----
// PUT /api/channels/:channelId
func (h *tokenRoutesHandler) updateChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "channelId")
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || channelID <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "通道不存在"})
		return
	}

	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)

	if v, ok := body["priority"]; ok {
		h.db.Exec("UPDATE route_channels SET priority = ?, manual_override = 1 WHERE id = ?", toFloat64(v), channelID)
	}
	if v, ok := body["weight"]; ok {
		h.db.Exec("UPDATE route_channels SET weight = ?, manual_override = 1 WHERE id = ?", toFloat64(v), channelID)
	}
	if v, ok := body["enabled"]; ok {
		h.db.Exec("UPDATE route_channels SET enabled = ?, manual_override = 1 WHERE id = ?", toBool(v), channelID)
	}

	updated := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", channelID)
	writeJSON(w, http.StatusOK, updated)
}

// ---- Delete Channel ----
// DELETE /api/channels/:channelId
func (h *tokenRoutesHandler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "channelId")
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || channelID <= 0 {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}

	h.db.Exec("DELETE FROM route_channels WHERE id = ?", channelID)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Route Decisions ----

// GET /api/routes/decision?model=
func (h *tokenRoutesHandler) routeDecision(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "model 不能为空"})
		return
	}

	// Stub: decision engine not yet wired
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"decision": map[string]any{},
	})
}

// POST /api/routes/decision/batch
func (h *tokenRoutesHandler) routeDecisionBatch(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"decisions": map[string]any{},
	})
}

// POST /api/routes/decision/by-route/batch
func (h *tokenRoutesHandler) routeDecisionByRouteBatch(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"decisions": map[string]any{},
	})
}

// POST /api/routes/decision/route-wide/batch
func (h *tokenRoutesHandler) routeDecisionRouteWideBatch(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"decisions": map[string]any{},
	})
}

// POST /api/routes/decision/refresh
func (h *tokenRoutesHandler) routeDecisionRefresh(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"jobId":   "stub-decision-refresh",
		"status":  "pending",
		"message": "已开始后台刷新路由选中概率，可稍后返回查看",
	})
}

func strOrNull(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	default:
		return 0
	}
}
