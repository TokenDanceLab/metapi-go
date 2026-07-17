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
	"github.com/tokendancelab/metapi-go/handler/shared"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/service"
)

const (
	routeDecisionBatchMaxItems     = 500
	routeDecisionRefreshTaskType   = "route-decision.refresh"
	routeDecisionRefreshTaskTitle  = "刷新路由选中概率"
	routeDecisionRefreshDedupeKey  = "route-decision-refresh"
	routeDecisionRouterUnavailable = "路由决策引擎未配置"
)

// RouteDecisionExplainer is the router surface used by decision admin APIs.
// Tests inject fakes; production wires routing.TokenRouter.
type RouteDecisionExplainer interface {
	ExplainSelection(ctx context.Context, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error)
	ExplainSelectionForRoute(ctx context.Context, routeID int64, requestedModel string, excludeChannelIDs []int64, policy routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error)
	ExplainSelectionRouteWide(ctx context.Context, routeID int64, policy routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error)
}

// RouteDecisionRefresher refreshes persisted route decision snapshots.
type RouteDecisionRefresher interface {
	RefreshAllRouteDecisionSnapshots(ctx context.Context, refreshPricingCatalog bool) (exactModelCount int, wildcardRouteCount int, err error)
}

// TokenRoutesDeps are optional dependencies for route decision endpoints.
type TokenRoutesDeps struct {
	Router    RouteDecisionExplainer
	Decisions RouteDecisionRefresher
}

// RegisterTokenRoutes registers all /api/routes and /api/channels routes.
func RegisterTokenRoutes(r chi.Router, db *sqlx.DB) {
	RegisterTokenRoutesWithDeps(r, db, TokenRoutesDeps{})
}

// RegisterTokenRoutesWithDeps registers routes with optional decision engine deps.
func RegisterTokenRoutesWithDeps(r chi.Router, db *sqlx.DB, deps TokenRoutesDeps) {
	handler := &tokenRoutesHandler{db: db, router: deps.Router, decisions: deps.Decisions}

	// Route list
	r.Get("/api/routes/lite", handler.listLite)
	r.Get("/api/routes/summary", handler.listSummary)
	r.Get("/api/routes", handler.listRoutes)

	// Route CRUD
	r.Post("/api/routes", handler.createRoute)
	r.Post("/api/routes/batch", handler.batchRoutes)
	r.Put("/api/routes/reorder", handler.reorderRoutes) // static path before /{id}
	r.Post("/api/routes/rebuild", handler.rebuildRoutes)
	r.Put("/api/routes/{id}", handler.updateRoute)
	r.Delete("/api/routes/{id}", handler.deleteRoute)

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
	db        *sqlx.DB
	router    RouteDecisionExplainer
	decisions RouteDecisionRefresher
}

// ---- List Lite ----
// GET /api/routes/lite
func (h *tokenRoutesHandler) listLite(w http.ResponseWriter, r *http.Request) {
	rows := queryRows(h.db, "SELECT id, model_pattern, display_name, display_icon, route_mode, routing_strategy, enabled, context_length FROM token_routes ORDER BY sort_order ASC, id ASC")
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
	rows := queryRows(h.db, "SELECT * FROM token_routes ORDER BY sort_order ASC, id ASC")
	result := make([]map[string]any, 0)
	for _, route := range rows {
		routeID := toInt64(route["id"])
		var channelCount, enabledCount int
		h.db.Get(&channelCount, h.db.Rebind("SELECT COUNT(*) FROM route_channels WHERE route_id = ?"), routeID)
		h.db.Get(&enabledCount, h.db.Rebind("SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND enabled = ?"), routeID, true)

		item := map[string]any{
			"id":                  route["id"],
			"modelPattern":        route["modelPattern"],
			"displayName":         route["displayName"],
			"displayIcon":         route["displayIcon"],
			"routeMode":           route["routeMode"],
			"sourceRouteIds":      []int64{},
			"modelMapping":        route["modelMapping"],
			"routingStrategy":     route["routingStrategy"],
			"contextLength":       route["contextLength"],
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
	rows := queryRows(h.db, "SELECT * FROM token_routes ORDER BY sort_order ASC, id ASC")
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
				"id":               ch["id"],
				"routeId":          ch["routeId"],
				"accountId":        ch["accountId"],
				"tokenId":          ch["tokenId"],
				"oauthRouteUnitId": ch["oauthRouteUnitId"],
				"sourceModel":      ch["sourceModel"],
				"priority":         ch["priority"],
				"weight":           ch["weight"],
				"enabled":          ch["enabled"],
				"manualOverride":   ch["manualOverride"],
				"account":          routeChannelAccountPublic(ch),
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
		// ContextLength is optional route metadata (tokens). NULL/omit/0 means unknown;
		// not enforced at proxy runtime in this wave (admin surface + /v1/models residual).
		ContextLength any   `json:"contextLength"`
		ModelMapping  any   `json:"modelMapping"`
		Enabled       *bool `json:"enabled"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

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

	var modelMapping any
	if body.ModelMapping != nil {
		if b, err := json.Marshal(body.ModelMapping); err == nil {
			modelMapping = string(b)
		}
	}

	contextLength := normalizeContextLengthOrNull(body.ContextLength)

	id, err := execInsertID(h.db,
		`INSERT INTO token_routes (model_pattern, display_name, display_icon, route_mode, model_mapping, routing_strategy, context_length, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		modelPattern, strOrNull(&displayName), strOrNull(body.DisplayIcon), routeMode,
		modelMapping, routingStrategy, contextLength, enabled, now, now,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "创建路由失败"})
		return
	}

	// For explicit_group, insert source route references
	if routeMode == "explicit_group" && len(body.SourceRouteIds) > 0 {
		for _, srcID := range body.SourceRouteIds {
			h.db.Exec(h.db.Rebind("INSERT INTO route_group_sources (group_route_id, source_route_id) VALUES (?, ?)"), id, srcID)
		}
	}

	// Pattern routes: auto-populate channels from exact-model routes + model availability.
	if routeMode != "explicit_group" && modelPattern != "" {
		if _, err := service.PopulateRouteChannelsByModelPattern(r.Context(), h.db, id, modelPattern); err != nil {
			slog.Warn("route create: channel auto-populate failed", "routeId", id, "error", err)
		}
	}

	created := queryRow(h.db, "SELECT * FROM token_routes WHERE id = ?", id)
	if created == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "创建路由失败"})
		return
	}
	created["sourceRouteIds"] = body.SourceRouteIds
	routing.InvalidateCache()
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	if v, ok := body["modelPattern"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec(h.db.Rebind("UPDATE token_routes SET model_pattern = ?, updated_at = ? WHERE id = ?"), strings.TrimSpace(s), now, id)
		}
	}
	if v, ok := body["displayName"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec(h.db.Rebind("UPDATE token_routes SET display_name = ?, updated_at = ? WHERE id = ?"), s, now, id)
		}
	}
	if v, ok := body["displayIcon"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec(h.db.Rebind("UPDATE token_routes SET display_icon = ?, updated_at = ? WHERE id = ?"), s, now, id)
		}
	}
	if v, ok := body["enabled"]; ok {
		h.db.Exec(h.db.Rebind("UPDATE token_routes SET enabled = ?, updated_at = ? WHERE id = ?"), toBool(v), now, id)
	}
	if v, ok := body["routingStrategy"]; ok {
		if s, ok2 := v.(string); ok2 {
			h.db.Exec(h.db.Rebind("UPDATE token_routes SET routing_strategy = ?, updated_at = ? WHERE id = ?"), s, now, id)
		}
	}
	if v, ok := body["modelMapping"]; ok {
		mappingJSON, _ := json.Marshal(v)
		h.db.Exec(h.db.Rebind("UPDATE token_routes SET model_mapping = ?, updated_at = ? WHERE id = ?"), string(mappingJSON), now, id)
	}
	// contextLength: present key updates (including explicit null/0 clear → NULL).
	// Metadata only — no proxy max-token enforcement is wired from this field yet.
	if v, ok := body["contextLength"]; ok {
		h.db.Exec(h.db.Rebind("UPDATE token_routes SET context_length = ?, updated_at = ? WHERE id = ?"), normalizeContextLengthOrNull(v), now, id)
	}

	// Update source route IDs for explicit_group
	if v, ok := body["sourceRouteIds"]; ok {
		if ids, ok2 := v.([]any); ok2 {
			h.db.Exec(h.db.Rebind("DELETE FROM route_group_sources WHERE group_route_id = ?"), id)
			for _, rawID := range ids {
				switch rid := rawID.(type) {
				case float64:
					h.db.Exec(h.db.Rebind("INSERT INTO route_group_sources (group_route_id, source_route_id) VALUES (?, ?)"), id, int64(rid))
				}
			}
		}
	}

	// Pattern change: recompose automatic channels while preserving manual overrides.
	// Unrelated field updates (displayName, routingStrategy, modelMapping, enabled)
	// must not wipe intentional in-route channel configuration.
	if v, ok := body["modelPattern"]; ok {
		if s, ok2 := v.(string); ok2 {
			nextPattern := strings.TrimSpace(s)
			prevPattern, _ := existing["modelPattern"].(string)
			mode, _ := existing["routeMode"].(string)
			if nextPattern != prevPattern && !routing.IsExplicitGroupRoute(mode) {
				if _, err := service.RebuildTokenRoutesFromAvailability(r.Context(), h.db); err != nil {
					slog.Warn("route update: rebuild after modelPattern change failed", "routeId", id, "error", err)
				}
			}
		}
	}

	updated := queryRow(h.db, "SELECT * FROM token_routes WHERE id = ?", id)
	var srcIDs []int64
	h.db.Select(&srcIDs, h.db.Rebind("SELECT source_route_id FROM route_group_sources WHERE group_route_id = ?"), id)
	updated["sourceRouteIds"] = srcIDs
	routing.InvalidateCache()
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

	h.db.Exec(h.db.Rebind("DELETE FROM route_group_sources WHERE group_route_id = ?"), id)
	h.db.Exec(h.db.Rebind("DELETE FROM route_group_sources WHERE source_route_id = ?"), id)
	h.db.Exec(h.db.Rebind("DELETE FROM route_channels WHERE route_id = ?"), id)
	h.db.Exec(h.db.Rebind("DELETE FROM token_routes WHERE id = ?"), id)
	routing.InvalidateCache()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Batch Routes ----
// POST /api/routes/batch
func (h *tokenRoutesHandler) batchRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string  `json:"action"`
		IDs    []int64 `json:"ids"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

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
		res, err := h.db.Exec(h.db.Rebind("UPDATE token_routes SET enabled = ?, updated_at = ? WHERE id = ?"), enabled, now, id)
		if err == nil {
			n, _ := res.RowsAffected()
			updated += int(n)
		}
	}

	routing.InvalidateCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"updatedCount": updated,
	})
}

// ---- Rebuild Routes ----
// POST /api/routes/rebuild
// Synchronously recomposes pattern-route channels from model availability and
// invalidates the in-process route cache. Response must stay truthful: do not
// claim a background job was queued.
func (h *tokenRoutesHandler) rebuildRoutes(w http.ResponseWriter, r *http.Request) {
	stats, err := service.RebuildTokenRoutesFromAvailability(r.Context(), h.db)
	if err != nil {
		slog.Error("routes rebuild failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"queued":  false,
			"reused":  false,
			"status":  "failed",
			"message": "路由重建失败",
		})
		return
	}
	shared.RecordRouteRebuildCompleted()
	slog.Info("routes rebuild completed",
		"queued", false,
		"status", "completed",
		"routesConsidered", stats.RoutesConsidered,
		"patternRoutes", stats.PatternRoutes,
		"groupRoutes", stats.GroupRoutes,
		"channelsInserted", stats.ChannelsInserted,
		"channelsRemoved", stats.ChannelsRemoved,
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"queued":           false,
		"reused":           false,
		"status":           "completed",
		"message":          "路由通道已重建并刷新缓存",
		"routesConsidered": stats.RoutesConsidered,
		"patternRoutes":    stats.PatternRoutes,
		"groupRoutes":      stats.GroupRoutes,
		"channelsInserted": stats.ChannelsInserted,
		"channelsRemoved":  stats.ChannelsRemoved,
		"channelsKept":     stats.ChannelsKept,
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
			"id":               ch["id"],
			"routeId":          ch["routeId"],
			"accountId":        ch["accountId"],
			"tokenId":          ch["tokenId"],
			"oauthRouteUnitId": ch["oauthRouteUnitId"],
			"sourceModel":      ch["sourceModel"],
			"priority":         ch["priority"],
			"weight":           ch["weight"],
			"enabled":          ch["enabled"],
			"manualOverride":   ch["manualOverride"],
			"account":          routeChannelAccountPublic(ch),
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

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
		h.db.Rebind(`SELECT COUNT(*) FROM route_channels
		 WHERE route_id = ? AND account_id = ?
		 AND (token_id = ? OR (token_id IS NULL AND ? IS NULL))
		 AND (source_model = ? OR (source_model IS NULL AND ? IS NULL))`),
		routeID, body.AccountID, body.TokenID, body.TokenID, body.SourceModel, body.SourceModel)
	if dupCount > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "该来源模型的通道已存在"})
		return
	}

	// Operator-added channels are intentional configuration and must survive
	// RebuildTokenRoutesFromAvailability (manual_override stays true unless
	// the channel is explicitly deleted).
	id, err := execInsertID(h.db,
		"INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		routeID, body.AccountID, body.TokenID, body.SourceModel, priority, weight, true, true,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "创建通道失败"})
		return
	}

	created := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", id)
	routing.InvalidateCache()
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	created := 0
	skipped := 0
	var errors []string

	for _, ch := range body.Channels {
		var dupCount int
		h.db.Get(&dupCount,
			h.db.Rebind(`SELECT COUNT(*) FROM route_channels
			 WHERE route_id = ? AND account_id = ?
			 AND (token_id = ? OR (token_id IS NULL AND ? IS NULL))
			 AND (source_model = ? OR (source_model IS NULL AND ? IS NULL))`),
			routeID, ch.AccountID, ch.TokenID, ch.TokenID, ch.SourceModel, ch.SourceModel)
		if dupCount > 0 {
			skipped++
			continue
		}

		_, err := h.db.Exec(
			h.db.Rebind("INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"),
			routeID, ch.AccountID, ch.TokenID, ch.SourceModel, 0, 10, true, true,
		)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		created++
	}

	routing.InvalidateCache()
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

	h.db.Exec(h.db.Rebind(`UPDATE route_channels SET cooldown_until = NULL, consecutive_fail_count = 0, cooldown_level = 0 WHERE route_id = ?`), routeID)
	routing.InvalidateCache()
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	var updatedIDs []int64
	for _, update := range body.Updates {
		h.db.Exec(h.db.Rebind("UPDATE route_channels SET priority = ?, manual_override = ? WHERE id = ?"), update.Priority, true, update.ID)
		updatedIDs = append(updatedIDs, update.ID)
	}

	var updatedChannels []map[string]any
	for _, cid := range updatedIDs {
		ch := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", cid)
		if ch != nil {
			updatedChannels = append(updatedChannels, ch)
		}
	}

	routing.InvalidateCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"channels": normalizeSlice(updatedChannels),
	})
}

// ---- Update Channel ----
// PUT /api/channels/:channelId

// PUT /api/routes/reorder
// Body: { "items": [ { "id": 1, "sortOrder": 0 }, ... ] }
// Assigns explicit sort_order for admin drag-and-drop route lists (#590/#284).
func (h *tokenRoutesHandler) reorderRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []struct {
			ID        int64 `json:"id"`
			SortOrder int64 `json:"sortOrder"`
		} `json:"items"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid payload"})
		return
	}
	if len(body.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "items is required"})
		return
	}
	if len(body.Items) > 1000 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "too many items (max 1000)"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var successIDs []int64
	var failedItems []map[string]any
	seen := map[int64]bool{}

	for _, item := range body.Items {
		if item.ID <= 0 {
			failedItems = append(failedItems, map[string]any{"id": item.ID, "message": "invalid id"})
			continue
		}
		if item.SortOrder < 0 {
			failedItems = append(failedItems, map[string]any{"id": item.ID, "message": "sortOrder must be >= 0"})
			continue
		}
		if seen[item.ID] {
			failedItems = append(failedItems, map[string]any{"id": item.ID, "message": "duplicate id in payload"})
			continue
		}
		seen[item.ID] = true

		res, err := h.db.Exec(h.db.Rebind(`
			UPDATE token_routes SET sort_order = ?, updated_at = ? WHERE id = ?
		`), item.SortOrder, now, item.ID)
		if err != nil {
			failedItems = append(failedItems, map[string]any{"id": item.ID, "message": err.Error()})
			continue
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			failedItems = append(failedItems, map[string]any{"id": item.ID, "message": "route not found"})
			continue
		}
		successIDs = append(successIDs, item.ID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     len(failedItems) == 0,
		"successIds":  successIDs,
		"failedItems": failedItems,
	})
}

func (h *tokenRoutesHandler) updateChannel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "channelId")
	channelID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || channelID <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "通道不存在"})
		return
	}

	var body map[string]any
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	// Any intentional channel edit marks manual_override so rebuild cannot
	// wipe operator-tuned priority/weight/enabled/sourceModel.
	if v, ok := body["priority"]; ok {
		h.db.Exec(h.db.Rebind("UPDATE route_channels SET priority = ?, manual_override = ? WHERE id = ?"), toFloat64(v), true, channelID)
	}
	if v, ok := body["weight"]; ok {
		h.db.Exec(h.db.Rebind("UPDATE route_channels SET weight = ?, manual_override = ? WHERE id = ?"), toFloat64(v), true, channelID)
	}
	if v, ok := body["enabled"]; ok {
		h.db.Exec(h.db.Rebind("UPDATE route_channels SET enabled = ?, manual_override = ? WHERE id = ?"), toBool(v), true, channelID)
	}
	if v, ok := body["sourceModel"]; ok {
		var sourceModel any
		switch s := v.(type) {
		case nil:
			sourceModel = nil
		case string:
			trimmed := strings.TrimSpace(s)
			if trimmed == "" {
				sourceModel = nil
			} else {
				sourceModel = trimmed
			}
		default:
			sourceModel = fmt.Sprint(v)
		}
		h.db.Exec(h.db.Rebind("UPDATE route_channels SET source_model = ?, manual_override = ? WHERE id = ?"), sourceModel, true, channelID)
	}

	updated := queryRow(h.db, "SELECT * FROM route_channels WHERE id = ?", channelID)
	routing.InvalidateCache()
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

	h.db.Exec(h.db.Rebind("DELETE FROM route_channels WHERE id = ?"), channelID)
	routing.InvalidateCache()
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
	if h.router == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"message": routeDecisionRouterUnavailable,
		})
		return
	}

	decision, err := h.router.ExplainSelection(r.Context(), model, nil, routing.EmptyDownstreamRoutingPolicy)
	if err != nil {
		slog.Error("route decision explain failed", "model", model, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "路由决策查询失败",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"decision": routeDecisionToMap(decision),
	})
}

// POST /api/routes/decision/batch
func (h *tokenRoutesHandler) routeDecisionBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Models                []string `json:"models"`
		RefreshPricingCatalog bool     `json:"refreshPricingCatalog"`
		PersistSnapshots      bool     `json:"persistSnapshots"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}
	models := uniqueNonEmptyStrings(body.Models)
	if len(models) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "models 必须是非空数组"})
		return
	}
	if len(models) > routeDecisionBatchMaxItems {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": fmt.Sprintf("models 最多 %d 项", routeDecisionBatchMaxItems),
		})
		return
	}
	if h.router == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"message": routeDecisionRouterUnavailable,
		})
		return
	}

	_ = body.RefreshPricingCatalog
	_ = body.PersistSnapshots

	decisions := make(map[string]any, len(models))
	for _, model := range models {
		decision, err := h.router.ExplainSelection(r.Context(), model, nil, routing.EmptyDownstreamRoutingPolicy)
		if err != nil {
			slog.Warn("route decision batch item failed", "model", model, "error", err)
			continue
		}
		decisions[model] = routeDecisionToMap(decision)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"decisions": decisions,
	})
}

// POST /api/routes/decision/by-route/batch
func (h *tokenRoutesHandler) routeDecisionByRouteBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []struct {
			RouteID int64  `json:"routeId"`
			Model   string `json:"model"`
		} `json:"items"`
		RefreshPricingCatalog bool `json:"refreshPricingCatalog"`
		PersistSnapshots      bool `json:"persistSnapshots"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}
	if len(body.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "items 必须是非空数组"})
		return
	}
	if len(body.Items) > routeDecisionBatchMaxItems {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": fmt.Sprintf("items 最多 %d 项", routeDecisionBatchMaxItems),
		})
		return
	}
	if h.router == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"message": routeDecisionRouterUnavailable,
		})
		return
	}

	_ = body.RefreshPricingCatalog
	_ = body.PersistSnapshots

	// Nested map: routeId -> model -> decision
	decisions := map[string]map[string]any{}
	for _, item := range body.Items {
		model := strings.TrimSpace(item.Model)
		if item.RouteID <= 0 || model == "" {
			continue
		}
		decision, err := h.router.ExplainSelectionForRoute(r.Context(), item.RouteID, model, nil, routing.EmptyDownstreamRoutingPolicy)
		if err != nil {
			slog.Warn("route decision by-route item failed", "routeId", item.RouteID, "model", model, "error", err)
			continue
		}
		routeKey := strconv.FormatInt(item.RouteID, 10)
		if decisions[routeKey] == nil {
			decisions[routeKey] = map[string]any{}
		}
		decisions[routeKey][model] = routeDecisionToMap(decision)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"decisions": decisions,
	})
}

// POST /api/routes/decision/route-wide/batch
func (h *tokenRoutesHandler) routeDecisionRouteWideBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RouteIDs              []int64 `json:"routeIds"`
		RefreshPricingCatalog bool    `json:"refreshPricingCatalog"`
		PersistSnapshots      bool    `json:"persistSnapshots"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}
	routeIDs := uniquePositiveInt64(body.RouteIDs)
	if len(routeIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routeIds 必须是非空数组"})
		return
	}
	if len(routeIDs) > routeDecisionBatchMaxItems {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": fmt.Sprintf("routeIds 最多 %d 项", routeDecisionBatchMaxItems),
		})
		return
	}
	if h.router == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"message": routeDecisionRouterUnavailable,
		})
		return
	}

	_ = body.RefreshPricingCatalog
	_ = body.PersistSnapshots

	decisions := make(map[string]any, len(routeIDs))
	for _, routeID := range routeIDs {
		decision, err := h.router.ExplainSelectionRouteWide(r.Context(), routeID, routing.EmptyDownstreamRoutingPolicy)
		if err != nil {
			slog.Warn("route decision route-wide item failed", "routeId", routeID, "error", err)
			continue
		}
		decisions[strconv.FormatInt(routeID, 10)] = routeDecisionToMap(decision)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"decisions": decisions,
	})
}

// POST /api/routes/decision/refresh
func (h *tokenRoutesHandler) routeDecisionRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshPricingCatalog bool `json:"refreshPricingCatalog"`
	}
	// Empty body is allowed; ignore decode errors for {}.
	_ = decodeJSONRequest(r, &body)

	if h.decisions == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"success": false,
			"queued":  false,
			"message": routeDecisionRouterUnavailable,
		})
		return
	}

	refresher := h.decisions
	refreshPricing := body.RefreshPricingCatalog
	task, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:      routeDecisionRefreshTaskType,
		Title:     routeDecisionRefreshTaskTitle,
		DedupeKey: routeDecisionRefreshDedupeKey,
	}, func() (any, error) {
		exact, wildcard, err := refresher.RefreshAllRouteDecisionSnapshots(context.Background(), refreshPricing)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"exactModelCount":       exact,
			"wildcardRouteCount":    wildcard,
			"refreshPricingCatalog": refreshPricing,
		}, nil
	})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  reused,
		"jobId":   task.ID,
		"taskId":  task.ID,
		"status":  string(task.Status),
		"message": "已开始后台刷新路由选中概率，可稍后返回查看",
	})
}

func routeDecisionToMap(d routing.RouteDecisionExplanation) map[string]any {
	candidates := make([]map[string]any, 0, len(d.Candidates))
	for _, c := range d.Candidates {
		candidates = append(candidates, map[string]any{
			"channelId":              c.ChannelID,
			"accountId":              c.AccountID,
			"username":               c.Username,
			"siteName":               c.SiteName,
			"tokenName":              c.TokenName,
			"priority":               c.Priority,
			"weight":                 c.Weight,
			"eligible":               c.Eligible,
			"recentlyFailed":         c.RecentlyFailed,
			"avoidedByRecentFailure": c.AvoidedByRecentFailure,
			"probability":            c.Probability,
			"reason":                 c.Reason,
		})
	}
	out := map[string]any{
		"requestedModel": d.RequestedModel,
		"actualModel":    d.ActualModel,
		"matched":        d.Matched,
		"modelPattern":   d.ModelPattern,
		"selectedLabel":  d.SelectedLabel,
		"summary":        d.Summary,
		"candidates":     candidates,
	}
	if d.RouteID != nil {
		out["routeId"] = *d.RouteID
	}
	if d.SelectedChannelID != nil {
		out["selectedChannelId"] = *d.SelectedChannelID
	}
	if d.SelectedAccountID != nil {
		out["selectedAccountId"] = *d.SelectedAccountID
	}
	if d.Summary == nil {
		out["summary"] = []string{}
	}
	return out
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func uniquePositiveInt64(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// normalizeContextLengthOrNull parses admin camelCase contextLength.
// Contract: null / omit (caller) / "" / 0 / negative → NULL (unknown, no enforcement).
// Positive integers (or numeric strings) are stored as token window metadata only.
func normalizeContextLengthOrNull(input any) any {
	if input == nil {
		return nil
	}
	switch v := input.(type) {
	case float64:
		if v <= 0 {
			return nil
		}
		return int64(v)
	case float32:
		if v <= 0 {
			return nil
		}
		return int64(v)
	case int:
		if v <= 0 {
			return nil
		}
		return int64(v)
	case int64:
		if v <= 0 {
			return nil
		}
		return v
	case json.Number:
		i, err := v.Int64()
		if err != nil || i <= 0 {
			return nil
		}
		return i
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil || i <= 0 {
			return nil
		}
		return i
	default:
		return nil
	}
}

func strOrNull(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func execInsertID(db *sqlx.DB, query string, args ...any) (int64, error) {
	if db.DriverName() == "pgx" {
		var id int64
		err := db.QueryRowx(db.Rebind(query+" RETURNING id"), args...).Scan(&id)
		return id, err
	}

	result, err := db.Exec(db.Rebind(query), args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
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

// routeChannelAccountPublic returns account fields for admin route channel lists
// without plaintext credentials (#375).
func routeChannelAccountPublic(ch map[string]any) map[string]any {
	out := map[string]any{
		"id":       ch["accountId"],
		"username": ch["username"],
		"balance":  ch["balance"],
		"status":   ch["accountStatus"],
	}
	if v, ok := ch["accessToken"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out["accessTokenMasked"] = maskAdminSecret(s)
		}
	}
	if v, ok := ch["apiToken"]; ok {
		switch tv := v.(type) {
		case string:
			if strings.TrimSpace(tv) != "" {
				out["apiTokenMasked"] = maskAdminSecret(tv)
			}
		case *string:
			if tv != nil && strings.TrimSpace(*tv) != "" {
				out["apiTokenMasked"] = maskAdminSecret(*tv)
			}
		}
	}
	return out
}

func maskAdminSecret(secret string) string {
	s := strings.TrimSpace(secret)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
