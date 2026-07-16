package admin

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/auth"
)

// RegisterDownstreamKeysRoutes registers all /api/downstream-keys routes.
func RegisterDownstreamKeysRoutes(r chi.Router, db *sqlx.DB) {
	handler := &downstreamKeysHandler{db: db}

	r.Get("/api/downstream-keys/summary", handler.summary)
	r.Get("/api/downstream-keys", handler.listKeys)
	r.Post("/api/downstream-keys", handler.createKey)
	r.Post("/api/downstream-keys/batch", handler.batchKeys)
	r.Get("/api/downstream-keys/{id}/overview", handler.overview)
	r.Get("/api/downstream-keys/{id}/trend", handler.trend)
	r.Put("/api/downstream-keys/{id}", handler.updateKey)
	r.Post("/api/downstream-keys/{id}/reset-usage", handler.resetUsage)
	r.Delete("/api/downstream-keys/{id}", handler.deleteKey)
}

type downstreamKeysHandler struct {
	db *sqlx.DB
}

// GET /api/downstream-keys/summary?range=&status=&search=&group=&tags=&tagMatch=
func (h *downstreamKeysHandler) summary(w http.ResponseWriter, r *http.Request) {
	rangeFilter := normalizeRange(r.URL.Query().Get("range"))
	statusFilter := normalizeStatus(r.URL.Query().Get("status"))
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if len(search) > 80 {
		search = search[:80]
	}
	group := strings.TrimSpace(r.URL.Query().Get("group"))

	query := "SELECT * FROM downstream_api_keys"
	var conditions []string
	var args []any

	if statusFilter == "enabled" {
		conditions = append(conditions, "enabled = ?")
		args = append(args, true)
	} else if statusFilter == "disabled" {
		conditions = append(conditions, "enabled = ?")
		args = append(args, false)
	}
	if search != "" {
		conditions = append(conditions, "(LOWER(name) LIKE ? OR LOWER(COALESCE(description, '')) LIKE ?)")
		like := "%" + strings.ToLower(search) + "%"
		args = append(args, like, like)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY id DESC"

	rows := queryRows(h.db, query, args...)

	// Apply group filter and enrich with usage data
	items := make([]map[string]any, 0)
	for _, row := range rows {
		groupName := existingString(row, "group_name")
		if group == "__ungrouped__" && groupName != "" {
			continue
		}
		if group != "" && group != "__ungrouped__" && groupName != group {
			continue
		}
		// Enrich with masked key and usage data
		key, _ := row["key"].(string)
		row["keyMasked"] = maskKey(key)
		items = append(items, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"range":    rangeFilter,
		"status":   statusFilter,
		"search":   search,
		"group":    group,
		"tags":     []string{},
		"tagMatch": "any",
		"items":    normalizeSlice(items),
	})
}

// GET /api/downstream-keys
func (h *downstreamKeysHandler) listKeys(w http.ResponseWriter, r *http.Request) {
	rows := queryRows(h.db, "SELECT * FROM downstream_api_keys ORDER BY id DESC")
	for _, row := range rows {
		if key, ok := row["key"].(string); ok {
			row["keyMasked"] = maskKey(key)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"items":   normalizeSlice(rows),
	})
}

// POST /api/downstream-keys
func (h *downstreamKeysHandler) createKey(w http.ResponseWriter, r *http.Request) {
	// maxCost/maxRequests use any so clients can send number|string|null (TS parity).
	// Explicit null/0/"" clears to unlimited (NULL); omitted also stores NULL on create.
	var body struct {
		Name                   string   `json:"name"`
		Key                    string   `json:"key"`
		Description            *string  `json:"description"`
		GroupName              *string  `json:"groupName"`
		Tags                   []string `json:"tags"`
		Enabled                *bool    `json:"enabled"`
		ExpiresAt              *string  `json:"expiresAt"`
		MaxCost                any      `json:"maxCost"`
		MaxRequests            any      `json:"maxRequests"`
		RpmLimit               any      `json:"rpmLimit"`
		SupportedModels        []string `json:"supportedModels"`
		AllowedRouteIds        []int64  `json:"allowedRouteIds"`
		SiteWeightMultipliers  any      `json:"siteWeightMultipliers"`
		ExcludedSiteIds        []int64  `json:"excludedSiteIds"`
		ExcludedCredentialRefs []any    `json:"excludedCredentialRefs"`
		ProxyURL               *string  `json:"proxyUrl"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	body.Name = strings.TrimSpace(body.Name)
	body.Key = strings.TrimSpace(body.Key)

	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name 不能为空")
		return
	}
	if body.Key == "" {
		writeError(w, http.StatusBadRequest, "key 不能为空")
		return
	}
	if !strings.HasPrefix(body.Key, "sk-") || len(body.Key) < 6 {
		writeError(w, http.StatusBadRequest, "key 必须以 sk- 开头且长度至少 6")
		return
	}

	// Check for duplicate key
	var count int
	h.db.Get(&count, rebindAdminQuery(h.db, "SELECT COUNT(*) FROM downstream_api_keys WHERE key = ?"), body.Key)
	if count > 0 {
		writeError(w, http.StatusConflict, "API key 已存在")
		return
	}

	// Normalize policy fields.
	normalizedTags := normalizeTagsInput(body.Tags)
	normalizedModels := normalizeSupportedModelsInput(body.SupportedModels)
	normalizedRouteIds := normalizeAllowedRouteIdsInput(body.AllowedRouteIds)
	normalizedSWM := normalizeSiteWeightMultipliersInput(body.SiteWeightMultipliers)
	normalizedExcludedSites := normalizeInt64Set(body.ExcludedSiteIds)
	normalizedCredRefs := normalizeExcludedCredentialRefsInput(body.ExcludedCredentialRefs)
	maxCost := normalizeQuotaFloatOrNull(body.MaxCost)
	maxRequests := normalizeQuotaIntOrNull(body.MaxRequests)
	rpmLimit := normalizeQuotaIntOrNull(body.RpmLimit)

	// Policy reference validation.
	if refErr := h.validateDownstreamPolicyReferences(normalizedRouteIds, normalizedSWM, normalizedExcludedSites, normalizedCredRefs); refErr != "" {
		writeError(w, http.StatusBadRequest, refErr)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	tagsJSON := toPersistenceJSON(normalizedTags)
	modelsJSON := toPersistenceJSON(normalizedModels)
	routeIdsJSON := toPersistenceJSON(normalizedRouteIds)
	swmJSON := toPersistenceJSON(normalizedSWM)
	excludedSitesJSON := toPersistenceJSON(normalizedExcludedSites)
	credRefsJSON := toPersistenceJSON(normalizedCredRefs)

	normalizedGroupName := normalizeGroupNameInput(body.GroupName)
	var desc interface{}
	desc = nil
	if body.Description != nil && strings.TrimSpace(*body.Description) != "" {
		s := strings.TrimSpace(*body.Description)
		desc = &s
	}

	// Per-key egress proxy (FE-KEY-PROXY / #578). NULL/empty inherits site/system.
	proxyURL, proxyErr := normalizeDownstreamProxyURL(body.ProxyURL)
	if proxyErr != "" {
		writeError(w, http.StatusBadRequest, proxyErr)
		return
	}

	id, err := execInsertID(h.db,
		`INSERT INTO downstream_api_keys
		(name, key, description, group_name, tags, enabled, expires_at, max_cost, used_cost, max_requests, used_requests,
		 rpm_limit, supported_models, allowed_route_ids, site_weight_multipliers, excluded_site_ids, excluded_credential_refs,
		 proxy_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		body.Name, body.Key, desc, normalizedGroupName, tagsJSON, enabled, body.ExpiresAt,
		maxCost, maxRequests, rpmLimit,
		modelsJSON, routeIdsJSON, swmJSON, excludedSitesJSON, credRefsJSON,
		proxyURL, now, now,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			writeError(w, http.StatusConflict, "API key 已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建失败")
		return
	}
	created := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	if created != nil {
		if key, ok := created["key"].(string); ok {
			created["keyMasked"] = maskKey(key)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"item":    created,
	})
}

// PUT /api/downstream-keys/:id
func (h *downstreamKeysHandler) updateKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id 无效")
		return
	}

	existing := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	if existing == nil {
		writeError(w, http.StatusNotFound, "API key 不存在")
		return
	}

	// Parse body into typed struct with pointers to detect field presence.
	// nil pointer = field not present in JSON body for most fields.
	// maxCost/maxRequests are decoded from the raw map so number|string|null all work.
	var body struct {
		Name                   *string  `json:"name"`
		Key                    *string  `json:"key"`
		Description            *string  `json:"description"`
		GroupName              *string  `json:"groupName"`
		Tags                   []string `json:"tags"`
		Enabled                *bool    `json:"enabled"`
		ExpiresAt              *string  `json:"expiresAt"`
		SupportedModels        []string `json:"supportedModels"`
		AllowedRouteIds        []int64  `json:"allowedRouteIds"`
		SiteWeightMultipliers  any      `json:"siteWeightMultipliers"`
		ExcludedSiteIds        []int64  `json:"excludedSiteIds"`
		ExcludedCredentialRefs []any    `json:"excludedCredentialRefs"`
		ProxyURL               *string  `json:"proxyUrl"`
	}

	bodyBytes, err := decodeJSONRequestRaw(r, &body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// First unmarshal into map to detect which fields were present in JSON.
	// Omitted maxCost/maxRequests keep existing values; present null/0/"" clear to NULL.
	rawBody := map[string]any{}
	if err := json.Unmarshal(bodyBytes, &rawBody); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	hasField := make(map[string]bool)
	if _, ok := rawBody["name"]; ok {
		hasField["name"] = true
	}
	if _, ok := rawBody["key"]; ok {
		hasField["key"] = true
	}
	if _, ok := rawBody["description"]; ok {
		hasField["description"] = true
	}
	if _, ok := rawBody["groupName"]; ok {
		hasField["groupName"] = true
	}
	if _, ok := rawBody["tags"]; ok {
		hasField["tags"] = true
	}
	if _, ok := rawBody["enabled"]; ok {
		hasField["enabled"] = true
	}
	if _, ok := rawBody["expiresAt"]; ok {
		hasField["expiresAt"] = true
	}
	if _, ok := rawBody["maxCost"]; ok {
		hasField["maxCost"] = true
	}
	if _, ok := rawBody["maxRequests"]; ok {
		hasField["maxRequests"] = true
	}
	if _, ok := rawBody["rpmLimit"]; ok {
		hasField["rpmLimit"] = true
	}
	if _, ok := rawBody["supportedModels"]; ok {
		hasField["supportedModels"] = true
	}
	if _, ok := rawBody["allowedRouteIds"]; ok {
		hasField["allowedRouteIds"] = true
	}
	if _, ok := rawBody["siteWeightMultipliers"]; ok {
		hasField["siteWeightMultipliers"] = true
	}
	if _, ok := rawBody["excludedSiteIds"]; ok {
		hasField["excludedSiteIds"] = true
	}
	if _, ok := rawBody["excludedCredentialRefs"]; ok {
		hasField["excludedCredentialRefs"] = true
	}
	if _, ok := rawBody["proxyUrl"]; ok {
		hasField["proxyUrl"] = true
	}

	// Merge: present fields from body, missing fields from existing record.
	name := existingString(existing, "name")
	if hasField["name"] && body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}

	key := existingString(existing, "key")
	if hasField["key"] && body.Key != nil {
		key = strings.TrimSpace(*body.Key)
	}

	description := existingStringPtr(existing, "description")
	if hasField["description"] {
		if body.Description != nil && strings.TrimSpace(*body.Description) != "" {
			s := strings.TrimSpace(*body.Description)
			description = &s
		} else {
			description = nil
		}
	}

	existingGroupName := existingStringPtr(existing, "group_name")
	groupName := normalizeGroupNameInput(existingGroupName)
	if hasField["groupName"] {
		groupName = normalizeGroupNameInput(body.GroupName)
	}

	existingTags := parseStringArrayFromDB(existing, "tags")
	tags := existingTags
	if hasField["tags"] {
		tags = normalizeTagsInput(body.Tags)
	}

	enabled := existingBool(existing, "enabled")
	if hasField["enabled"] && body.Enabled != nil {
		enabled = *body.Enabled
	}

	expiresAt := existingStringPtr(existing, "expires_at")
	if hasField["expiresAt"] {
		expiresAt = normalizeExpiresAt(body.ExpiresAt)
	}

	maxCost := existingFloat64Ptr(existing, "max_cost")
	if hasField["maxCost"] {
		// Explicit clear (null/0/"") → NULL unlimited; positive → set.
		maxCost = normalizeQuotaFloatOrNull(rawBody["maxCost"])
	}

	maxRequests := existingInt64Ptr(existing, "max_requests")
	if hasField["maxRequests"] {
		maxRequests = normalizeQuotaIntOrNull(rawBody["maxRequests"])
	}

	rpmLimit := existingInt64Ptr(existing, "rpm_limit")
	if hasField["rpmLimit"] {
		rpmLimit = normalizeQuotaIntOrNull(rawBody["rpmLimit"])
	}

	existingSupportedModels := parseStringArrayFromDB(existing, "supported_models")
	supportedModels := existingSupportedModels
	if hasField["supportedModels"] {
		supportedModels = normalizeSupportedModelsInput(body.SupportedModels)
	}

	existingAllowedRouteIds := parseIntArrayFromDB(existing, "allowed_route_ids")
	allowedRouteIds := existingAllowedRouteIds
	if hasField["allowedRouteIds"] {
		allowedRouteIds = normalizeAllowedRouteIdsInput(body.AllowedRouteIds)
	}

	existingSiteWeightMultipliers := parseMapFromDB(existing, "site_weight_multipliers")
	siteWeightMultipliers := existingSiteWeightMultipliers
	if hasField["siteWeightMultipliers"] {
		siteWeightMultipliers = normalizeSiteWeightMultipliersInput(body.SiteWeightMultipliers)
	}

	existingExcludedSiteIds := parseIntArrayFromDB(existing, "excluded_site_ids")
	excludedSiteIds := existingExcludedSiteIds
	if hasField["excludedSiteIds"] {
		excludedSiteIds = normalizeInt64Set(body.ExcludedSiteIds)
	}

	existingExcludedCredentialRefs := parseAnyArrayFromDB(existing, "excluded_credential_refs")
	excludedCredentialRefs := existingExcludedCredentialRefs
	if hasField["excludedCredentialRefs"] {
		excludedCredentialRefs = normalizeExcludedCredentialRefsInput(body.ExcludedCredentialRefs)
	}

	// proxyUrl: absent keeps existing; present empty/null clears to inherit site/system.
	proxyURL := existingStringPtr(existing, "proxy_url")
	if hasField["proxyUrl"] {
		normalized, proxyErr := normalizeDownstreamProxyURL(body.ProxyURL)
		if proxyErr != "" {
			writeError(w, http.StatusBadRequest, proxyErr)
			return
		}
		proxyURL = normalized
	}

	// Validate.
	if name == "" {
		writeError(w, http.StatusBadRequest, "name 不能为空")
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "key 不能为空")
		return
	}
	if !strings.HasPrefix(key, "sk-") || len(key) < 6 {
		writeError(w, http.StatusBadRequest, "key 必须以 sk- 开头且长度至少 6")
		return
	}

	// Duplicate key check: if key is being changed, verify new key doesn't collide.
	if hasField["key"] && key != existingString(existing, "key") {
		var dupCount int
		h.db.Get(&dupCount, rebindAdminQuery(h.db, "SELECT COUNT(*) FROM downstream_api_keys WHERE key = ? AND id != ?"), key, id)
		if dupCount > 0 {
			writeError(w, http.StatusConflict, "API key 已存在")
			return
		}
	}

	// Policy reference validation.
	if refErr := h.validateDownstreamPolicyReferences(allowedRouteIds, siteWeightMultipliers, excludedSiteIds, excludedCredentialRefs); refErr != "" {
		writeError(w, http.StatusBadRequest, refErr)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tagsJSON := toPersistenceJSON(tags)
	modelsJSON := toPersistenceJSON(supportedModels)
	routeIdsJSON := toPersistenceJSON(allowedRouteIds)
	swmJSON := toPersistenceJSON(siteWeightMultipliers)
	excludedSitesJSON := toPersistenceJSON(excludedSiteIds)
	credRefsJSON := toPersistenceJSON(excludedCredentialRefs)

	_, err = h.db.Exec(
		rebindAdminQuery(h.db,
			`UPDATE downstream_api_keys SET
			name = ?, key = ?, description = ?, group_name = ?, tags = ?,
			enabled = ?, expires_at = ?, max_cost = ?, max_requests = ?, rpm_limit = ?,
			supported_models = ?, allowed_route_ids = ?, site_weight_multipliers = ?,
			excluded_site_ids = ?, excluded_credential_refs = ?, proxy_url = ?, updated_at = ?
		WHERE id = ?`),
		name, key, description, groupName, tagsJSON,
		enabled, expiresAt, maxCost, maxRequests, rpmLimit,
		modelsJSON, routeIdsJSON, swmJSON,
		excludedSitesJSON, credRefsJSON, proxyURL, now, id,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			writeError(w, http.StatusConflict, "API key 已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	updated := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	if updated != nil {
		if k, ok := updated["key"].(string); ok {
			updated["keyMasked"] = maskKey(k)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"item":    updated,
	})
}

// POST /api/downstream-keys/:id/reset-usage
func (h *downstreamKeysHandler) resetUsage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id 无效")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := h.db.Exec(rebindAdminQuery(h.db, "UPDATE downstream_api_keys SET used_cost = 0, used_requests = 0, updated_at = ? WHERE id = ?"), now, id); err != nil {
		writeError(w, http.StatusInternalServerError, "重置失败")
		return
	}

	updated := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"item":    updated,
	})
}

// DELETE /api/downstream-keys/:id
func (h *downstreamKeysHandler) deleteKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id 无效")
		return
	}

	if _, err := h.db.Exec(rebindAdminQuery(h.db, "DELETE FROM downstream_api_keys WHERE id = ?"), id); err != nil {
		writeError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// GET /api/downstream-keys/:id/overview
func (h *downstreamKeysHandler) overview(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id 无效")
		return
	}

	row := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	if row == nil {
		writeError(w, http.StatusNotFound, "API key 不存在")
		return
	}

	if key, ok := row["key"].(string); ok {
		row["keyMasked"] = maskKey(key)
	}

	// Soft RPM window snapshot (in-process; multi-instance does not share).
	rpmAdmission := map[string]any{
		"windowSeconds": int(auth.DefaultKeyRPMWindow.Seconds()),
		"used":          0,
		"limit":         nil,
	}
	if rpm := existingInt64Ptr(row, "rpm_limit"); rpm != nil && *rpm > 0 {
		rpmAdmission["limit"] = *rpm
	}
	if used, win := auth.DefaultKeyRPMLimiter.Snapshot(id); used >= 0 {
		rpmAdmission["used"] = used
		rpmAdmission["windowSeconds"] = int(win.Seconds())
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"item":    row,
		"usage": map[string]any{
			"last24h": nil,
			"last7d":  nil,
			"all":     nil,
		},
		"rpmAdmission": rpmAdmission,
	})
}

// GET /api/downstream-keys/:id/trend?range=&timeZone=
func (h *downstreamKeysHandler) trend(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id 无效")
		return
	}

	row := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	if row == nil {
		writeError(w, http.StatusNotFound, "API key 不存在")
		return
	}

	rangeFilter := normalizeRange(r.URL.Query().Get("range"))

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"range":         rangeFilter,
		"item":          map[string]any{"id": id, "name": row["name"]},
		"bucketSeconds": 3600,
		"timeZone":      "UTC",
		"buckets":       []any{},
	})
}

// POST /api/downstream-keys/batch
func (h *downstreamKeysHandler) batchKeys(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs            []int64  `json:"ids"`
		Action         string   `json:"action"`
		GroupName      *string  `json:"groupName"`
		GroupOperation *string  `json:"groupOperation"`
		Tags           []string `json:"tags"`
		TagOperation   *string  `json:"tagOperation"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}

	action := strings.TrimSpace(body.Action)
	validActions := map[string]bool{
		"enable": true, "disable": true, "delete": true,
		"resetUsage": true, "updateMetadata": true,
	}
	if !validActions[action] {
		writeError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	var successIDs []int64
	var failedItems []map[string]any

	for _, id := range body.IDs {
		row := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
		if row == nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "API key 不存在"})
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		switch action {
		case "delete":
			if _, err := h.db.Exec(rebindAdminQuery(h.db, "DELETE FROM downstream_api_keys WHERE id = ?"), id); err != nil {
				failedItems = append(failedItems, map[string]any{"id": id, "message": "delete failed"})
				continue
			}
		case "resetUsage":
			if _, err := h.db.Exec(rebindAdminQuery(h.db, "UPDATE downstream_api_keys SET used_cost = 0, used_requests = 0, updated_at = ? WHERE id = ?"), now, id); err != nil {
				failedItems = append(failedItems, map[string]any{"id": id, "message": "reset failed"})
				continue
			}
		case "enable":
			if _, err := h.db.Exec(rebindAdminQuery(h.db, "UPDATE downstream_api_keys SET enabled = ?, updated_at = ? WHERE id = ?"), true, now, id); err != nil {
				failedItems = append(failedItems, map[string]any{"id": id, "message": "enable failed"})
				continue
			}
		case "disable":
			if _, err := h.db.Exec(rebindAdminQuery(h.db, "UPDATE downstream_api_keys SET enabled = ?, updated_at = ? WHERE id = ?"), false, now, id); err != nil {
				failedItems = append(failedItems, map[string]any{"id": id, "message": "disable failed"})
				continue
			}
		case "updateMetadata":
			if body.GroupOperation != nil && *body.GroupOperation == "set" && body.GroupName != nil {
				if _, err := h.db.Exec(rebindAdminQuery(h.db, "UPDATE downstream_api_keys SET group_name = ?, updated_at = ? WHERE id = ?"), *body.GroupName, now, id); err != nil {
					failedItems = append(failedItems, map[string]any{"id": id, "message": "metadata update failed"})
					continue
				}
			} else if body.GroupOperation != nil && *body.GroupOperation == "clear" {
				if _, err := h.db.Exec(rebindAdminQuery(h.db, "UPDATE downstream_api_keys SET group_name = NULL, updated_at = ? WHERE id = ?"), now, id); err != nil {
					failedItems = append(failedItems, map[string]any{"id": id, "message": "metadata update failed"})
					continue
				}
			}
		}
		successIDs = append(successIDs, id)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"successIds":  successIDs,
		"failedItems": failedItems,
	})
}

func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func normalizeRange(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "24h", "7d", "all":
		return v
	default:
		return "24h"
	}
}

func normalizeStatus(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "enabled", "disabled":
		return v
	default:
		return "all"
	}
}

// --- helpers for extracting existing values from DB row maps ---

func rowValue(row map[string]any, key string) (any, bool) {
	if v, ok := row[key]; ok {
		return v, true
	}
	camelKey := snakeToCamel(key)
	if camelKey != key {
		v, ok := row[camelKey]
		return v, ok
	}
	return nil, false
}

func existingString(row map[string]any, key string) string {
	if v, ok := rowValue(row, key); ok {
		if s, ok2 := v.(string); ok2 {
			return s
		}
	}
	return ""
}

func existingStringPtr(row map[string]any, key string) *string {
	v, ok := rowValue(row, key)
	if !ok || v == nil {
		return nil
	}
	s, ok2 := v.(string)
	if !ok2 || s == "" {
		return nil
	}
	return &s
}

func existingBool(row map[string]any, key string) bool {
	v, ok := rowValue(row, key)
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val == "1" || strings.EqualFold(val, "true")
	}
	return false
}

func existingFloat64Ptr(row map[string]any, key string) *float64 {
	v, ok := rowValue(row, key)
	if !ok || v == nil {
		return nil
	}
	switch val := v.(type) {
	case float64:
		return &val
	case int64:
		f := float64(val)
		return &f
	case json.Number:
		f, err := val.Float64()
		if err != nil {
			return nil
		}
		return &f
	}
	return nil
}

func existingInt64Ptr(row map[string]any, key string) *int64 {
	v, ok := rowValue(row, key)
	if !ok || v == nil {
		return nil
	}
	switch val := v.(type) {
	case int64:
		return &val
	case float64:
		i := int64(val)
		return &i
	case json.Number:
		i, err := val.Int64()
		if err != nil {
			return nil
		}
		return &i
	}
	return nil
}

// parseJsonField unmarshals a DB column value (string or already-parsed) into target.
func parseJsonField(row map[string]any, key string, target any) {
	v, ok := rowValue(row, key)
	if !ok || v == nil {
		return
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return
		}
		json.Unmarshal([]byte(val), target)
	case []byte:
		if len(val) == 0 {
			return
		}
		json.Unmarshal(val, target)
	default:
		// Already parsed by sqlx (e.g. JSON column in PostgreSQL)
		data, _ := json.Marshal(val)
		json.Unmarshal(data, target)
	}
}

func parseStringArrayFromDB(row map[string]any, key string) []string {
	var arr []string
	parseJsonField(row, key, &arr)
	return arr
}

func parseIntArrayFromDB(row map[string]any, key string) []int64 {
	var arr []int64
	parseJsonField(row, key, &arr)
	return arr
}

func parseMapFromDB(row map[string]any, key string) map[string]float64 {
	v, ok := rowValue(row, key)
	if !ok || v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		if val == "" || val == "{}" {
			return nil
		}
		var raw map[string]float64
		if err := json.Unmarshal([]byte(val), &raw); err != nil {
			return nil
		}
		return raw
	case []byte:
		if len(val) == 0 || string(val) == "{}" {
			return nil
		}
		var raw map[string]float64
		if err := json.Unmarshal(val, &raw); err != nil {
			return nil
		}
		return raw
	default:
		data, _ := json.Marshal(val)
		var raw map[string]float64
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil
		}
		return raw
	}
}

func parseAnyArrayFromDB(row map[string]any, key string) []any {
	var arr []any
	parseJsonField(row, key, &arr)
	return arr
}

// --- normalization helpers (mirrors TS normalizeDownstreamApiKeyPayload) ---

func normalizeGroupNameInput(input *string) *string {
	if input == nil {
		return nil
	}
	v := strings.TrimSpace(*input)
	if v == "" {
		return nil
	}
	if len(v) > 64 {
		v = v[:64]
	}
	return &v
}

func normalizeTagsInput(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, raw := range input {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if len(v) > 32 {
			v = v[:32]
		}
		lower := strings.ToLower(v)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		result = append(result, v)
		if len(result) >= 20 {
			break
		}
	}
	return result
}

func normalizeSupportedModelsInput(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, raw := range input {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}
	return result
}

func normalizeAllowedRouteIdsInput(input []int64) []int64 {
	seen := make(map[int64]bool)
	result := make([]int64, 0, len(input))
	for _, raw := range input {
		if raw <= 0 {
			continue
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		result = append(result, raw)
		if len(result) >= 500 {
			break
		}
	}
	return result
}

func normalizeSiteWeightMultipliersInput(input any) map[string]float64 {
	if input == nil {
		return nil
	}
	// Accept both map[string]float64 (from struct) and raw JSON object.
	var raw map[string]any
	switch val := input.(type) {
	case map[string]float64:
		if len(val) == 0 {
			return nil
		}
		return val
	case map[string]any:
		raw = val
	default:
		data, err := json.Marshal(input)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil
		}
	}
	if len(raw) == 0 {
		return nil
	}
	result := make(map[string]float64, len(raw))
	for k, v := range raw {
		siteId, _ := strconv.ParseFloat(k, 64)
		if siteId <= 0 {
			continue
		}
		var multiplier float64
		switch mv := v.(type) {
		case float64:
			multiplier = mv
		case json.Number:
			multiplier, _ = mv.Float64()
		case int64:
			multiplier = float64(mv)
		default:
			continue
		}
		if multiplier <= 0 {
			continue
		}
		result[fmt.Sprintf("%.0f", siteId)] = multiplier
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeInt64Set(input []int64) []int64 {
	seen := make(map[int64]bool)
	result := make([]int64, 0, len(input))
	for _, raw := range input {
		if raw <= 0 {
			continue
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		result = append(result, raw)
		if len(result) >= 500 {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func normalizeExcludedCredentialRefsInput(input []any) []any {
	seen := make(map[string]bool)
	result := make([]any, 0, len(input))
	for _, item := range input {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := obj["kind"].(string)
		kind = strings.TrimSpace(kind)
		siteId := int64FromAny(obj["siteId"])
		accountId := int64FromAny(obj["accountId"])
		if siteId <= 0 || accountId <= 0 {
			continue
		}

		var dedupeKey string
		if kind == "account_token" {
			tokenId := int64FromAny(obj["tokenId"])
			if tokenId <= 0 {
				continue
			}
			dedupeKey = fmt.Sprintf("account_token:%d:%d:%d", siteId, accountId, tokenId)
			if seen[dedupeKey] {
				continue
			}
			seen[dedupeKey] = true
			result = append(result, map[string]any{
				"kind":      "account_token",
				"siteId":    siteId,
				"accountId": accountId,
				"tokenId":   tokenId,
			})
		} else if kind == "default_api_key" {
			dedupeKey = fmt.Sprintf("default_api_key:%d:%d", siteId, accountId)
			if seen[dedupeKey] {
				continue
			}
			seen[dedupeKey] = true
			result = append(result, map[string]any{
				"kind":      "default_api_key",
				"siteId":    siteId,
				"accountId": accountId,
			})
		}
		// Unknown kinds are silently skipped
		if len(result) >= 1000 {
			break
		}
	}
	return result
}

func int64FromAny(v any) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case json.Number:
		i, _ := val.Int64()
		return i
	case int:
		return int64(val)
	}
	return 0
}

// normalizeDownstreamProxyURL trims proxyUrl for create/update.
// Empty / whitespace / null -> nil (inherit site/account/system proxy).
// Non-empty values must include a supported scheme (http/https/socks*).
func normalizeDownstreamProxyURL(input *string) (*string, string) {
	if input == nil {
		return nil, ""
	}
	v := strings.TrimSpace(*input)
	if v == "" {
		return nil, ""
	}
	lower := strings.ToLower(v)
	supported := false
	for _, scheme := range []string{"http://", "https://", "socks://", "socks4://", "socks4a://", "socks5://", "socks5h://"} {
		if strings.HasPrefix(lower, scheme) {
			supported = true
			break
		}
	}
	if !supported {
		return nil, "proxyUrl 必须以 http://、https:// 或 socks 代理 scheme 开头"
	}
	return &v, ""
}

func normalizeExpiresAt(input *string) *string {
	if input == nil {
		return nil
	}
	v := strings.TrimSpace(*input)
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		// Try other common formats.
		t, err = time.Parse("2006-01-02T15:04:05Z", v)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05.000Z", v)
			if err != nil {
				// If we can't parse it, keep the original string.
				// TS throws an error; we are lenient and just store the trimmed value.
				return &v
			}
		}
	}
	iso := t.UTC().Format(time.RFC3339)
	return &iso
}

// normalizePositiveFloatOrNull keeps positive values; nil/negative/NaN/Inf become NULL.
// Prefer normalizeQuotaFloatOrNull for maxCost (null/0 clear contract).
func normalizePositiveFloatOrNull(input *float64) *float64 {
	if input == nil {
		return nil
	}
	if *input < 0 || math.IsNaN(*input) || math.IsInf(*input, 0) {
		return nil
	}
	return input
}

// normalizePositiveIntOrNull keeps non-negative values; nil/negative become NULL.
// Prefer normalizeQuotaIntOrNull for maxRequests (null/0 clear contract).
func normalizePositiveIntOrNull(input *int64) *int64 {
	if input == nil {
		return nil
	}
	if *input < 0 {
		return nil
	}
	return input
}

// normalizeQuotaFloatOrNull implements maxCost clear/set semantics for create/update.
// Contract (FE-KEYS-CORR / #405 / #41):
//   - omitted on update → caller keeps existing (do not call this)
//   - null / "" / missing any → NULL (unlimited)
//   - 0 / negative / NaN / Inf → NULL (clear)
//   - positive number or numeric string → stored value
func normalizeQuotaFloatOrNull(input any) *float64 {
	if input == nil {
		return nil
	}
	switch v := input.(type) {
	case float64:
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return &v
	case float32:
		f := float64(v)
		if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return &f
	case int:
		if v <= 0 {
			return nil
		}
		f := float64(v)
		return &f
	case int64:
		if v <= 0 {
			return nil
		}
		f := float64(v)
		return &f
	case json.Number:
		f, err := v.Float64()
		if err != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return &f
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return &f
	case *float64:
		return normalizeQuotaFloatOrNull(derefAnyFloat(v))
	default:
		return nil
	}
}

// normalizeQuotaIntOrNull implements maxRequests clear/set semantics for create/update.
// Same contract as normalizeQuotaFloatOrNull: null/0/"" clear to unlimited.
func normalizeQuotaIntOrNull(input any) *int64 {
	if input == nil {
		return nil
	}
	switch v := input.(type) {
	case float64:
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		i := int64(v)
		return &i
	case float32:
		f := float64(v)
		if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		i := int64(f)
		return &i
	case int:
		if v <= 0 {
			return nil
		}
		i := int64(v)
		return &i
	case int64:
		if v <= 0 {
			return nil
		}
		return &v
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			f, ferr := v.Float64()
			if ferr != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil
			}
			n := int64(f)
			return &n
		}
		if i <= 0 {
			return nil
		}
		return &i
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			f, ferr := strconv.ParseFloat(s, 64)
			if ferr != nil || f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil
			}
			n := int64(f)
			return &n
		}
		if i <= 0 {
			return nil
		}
		return &i
	case *int64:
		if v == nil {
			return nil
		}
		return normalizeQuotaIntOrNull(*v)
	default:
		return nil
	}
}

func derefAnyFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func toPersistenceJSON(v any) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []string:
		if len(val) == 0 {
			return nil
		}
	case []int64:
		if len(val) == 0 {
			return nil
		}
	case []any:
		if len(val) == 0 {
			return nil
		}
	case map[string]float64:
		if len(val) == 0 {
			return nil
		}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(data)
}

// --- policy reference validation (mirrors TS validatePolicyReferences) ---

func (h *downstreamKeysHandler) validateDownstreamPolicyReferences(
	allowedRouteIds []int64,
	siteWeightMultipliers map[string]float64,
	excludedSiteIds []int64,
	excludedCredentialRefs []any,
) string {
	// Validate allowedRouteIds exist in token_routes.
	if len(allowedRouteIds) > 0 {
		query, args, err := sqlx.In("SELECT id FROM token_routes WHERE id IN (?)", allowedRouteIds)
		if err == nil {
			rows := queryRows(h.db, h.db.Rebind(query), args...)
			existingIds := make(map[int64]bool)
			for _, row := range rows {
				if id, ok := row["id"].(int64); ok {
					existingIds[id] = true
				}
			}
			var missing []string
			for _, rid := range allowedRouteIds {
				if !existingIds[rid] {
					missing = append(missing, strconv.FormatInt(rid, 10))
				}
			}
			if len(missing) > 0 {
				return fmt.Sprintf("allowedRouteIds 包含不存在的路由: %s", strings.Join(missing, ", "))
			}
		}
	}

	// Collect all site IDs to validate.
	siteIdSet := make(map[int64]bool)
	for k := range siteWeightMultipliers {
		id, err := strconv.ParseInt(k, 10, 64)
		if err == nil && id > 0 {
			siteIdSet[id] = true
		}
	}
	for _, id := range excludedSiteIds {
		if id > 0 {
			siteIdSet[id] = true
		}
	}
	if len(siteIdSet) > 0 {
		ids := make([]int64, 0, len(siteIdSet))
		for id := range siteIdSet {
			ids = append(ids, id)
		}
		query, args, err := sqlx.In("SELECT id FROM sites WHERE id IN (?)", ids)
		if err == nil {
			rows := queryRows(h.db, h.db.Rebind(query), args...)
			existingIds := make(map[int64]bool)
			for _, row := range rows {
				if id, ok := row["id"].(int64); ok {
					existingIds[id] = true
				}
			}
			var missing []string
			for _, sid := range ids {
				if !existingIds[sid] {
					missing = append(missing, strconv.FormatInt(sid, 10))
				}
			}
			if len(missing) > 0 {
				return fmt.Sprintf("策略中包含不存在的站点: %s", strings.Join(missing, ", "))
			}
		}
	}

	// Validate excludedCredentialRefs.
	for _, ref := range excludedCredentialRefs {
		obj, ok := ref.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := obj["kind"].(string)
		if kind == "account_token" {
			tokenId := int64FromAny(obj["tokenId"])
			accountId := int64FromAny(obj["accountId"])
			siteId := int64FromAny(obj["siteId"])
			if tokenId <= 0 {
				return fmt.Sprintf("excludedCredentialRefs 包含不存在的令牌: %v", obj["tokenId"])
			}
			row := queryRow(h.db,
				`SELECT at.id as token_id, at.account_id, a.site_id
				 FROM account_tokens at
				 INNER JOIN accounts a ON at.account_id = a.id
				 WHERE at.id = ?`, tokenId)
			if row == nil {
				return fmt.Sprintf("excludedCredentialRefs 包含不存在的令牌: %d", tokenId)
			}
			dbAccountId := int64FromAny(mustRowValue(row, "account_id"))
			dbSiteId := int64FromAny(mustRowValue(row, "site_id"))
			if dbAccountId != accountId || dbSiteId != siteId {
				return fmt.Sprintf("excludedCredentialRefs 中的 account_token 引用与账号/站点不匹配: %d", tokenId)
			}
		} else if kind == "default_api_key" {
			accountId := int64FromAny(obj["accountId"])
			siteId := int64FromAny(obj["siteId"])
			row := queryRow(h.db,
				`SELECT id as account_id, site_id, api_token
				 FROM accounts WHERE id = ?`, accountId)
			if row == nil {
				return fmt.Sprintf("excludedCredentialRefs 包含不存在的账号: %d", accountId)
			}
			dbSiteId := int64FromAny(mustRowValue(row, "site_id"))
			if dbSiteId != siteId {
				return fmt.Sprintf("excludedCredentialRefs 中的 default_api_key 引用与站点不匹配: %d", accountId)
			}
			apiToken, _ := mustRowValue(row, "api_token").(string)
			if strings.TrimSpace(apiToken) == "" {
				return fmt.Sprintf("excludedCredentialRefs 中的 default_api_key 账号缺少默认 API Key: %d", accountId)
			}
		}
	}

	return ""
}

func mustRowValue(row map[string]any, key string) any {
	v, _ := rowValue(row, key)
	return v
}
