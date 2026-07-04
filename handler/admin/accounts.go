package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/handler/admin/payloads"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

// RegisterAccountsRoutes registers all /api/accounts routes.
func RegisterAccountsRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &accountsHandler{db: db, cfg: cfg}

	r.Get("/api/accounts", handler.listAccounts)
	r.Post("/api/accounts", handler.createAccount)
	r.Post("/api/accounts/login", handler.loginAccount)
	r.Post("/api/accounts/verify-token", handler.verifyToken)
	r.Post("/api/accounts/{id}/rebind-session", handler.rebindSession)
	r.Put("/api/accounts/{id}", handler.updateAccount)
	r.Delete("/api/accounts/{id}", handler.deleteAccount)
	r.Post("/api/accounts/batch", handler.batchAccounts)
	r.Post("/api/accounts/health/refresh", handler.healthRefresh)
	r.Post("/api/accounts/{id}/balance", handler.refreshBalance)
	r.Get("/api/accounts/{id}/models", handler.getAccountModels)
	r.Post("/api/accounts/{id}/models/manual", handler.manualModels)
}

type accountsHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

// accountsSnapshotCache is an in-memory TTL cache for GET /api/accounts responses.
// Mirrors TS getAccountsSnapshot() behavior: cached response, ?refresh=true bypasses.
type accountsSnapshotCache struct {
	mu       sync.RWMutex
	data     []byte
	expiresAt time.Time
	ttl      time.Duration
}

func (c *accountsSnapshotCache) isValid() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data != nil && time.Now().Before(c.expiresAt)
}

func (c *accountsSnapshotCache) get() ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.data != nil && time.Now().Before(c.expiresAt) {
		return c.data, true
	}
	return nil, false
}

func (c *accountsSnapshotCache) set(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.expiresAt = time.Now().Add(c.ttl)
}

var globalAccountsCache = &accountsSnapshotCache{ttl: 30 * time.Second}

// ---- List Accounts ----

func (h *accountsHandler) listAccounts(w http.ResponseWriter, r *http.Request) {
	refresh := strings.TrimSpace(r.URL.Query().Get("refresh"))
	forceRefresh := refresh == "true" || refresh == "1"

	// Check snapshot cache
	if !forceRefresh {
		if cached, hit := globalAccountsCache.get(); hit {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("x-accounts-snapshot-cache", "hit")
			w.WriteHeader(http.StatusOK)
			w.Write(cached)
			return
		}
	}

	accounts, err := service.ListAccountsWithSites(h.db)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Also fetch sites for the response
	var sites []store.Site
	h.db.Select(&sites, "SELECT * FROM sites ORDER BY sort_order, id")

	resp := map[string]any{
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"accounts":    accounts,
		"sites":       sites,
	}
	respBytes, _ := json.Marshal(resp)
	globalAccountsCache.set(respBytes)

	w.Header().Set("x-accounts-snapshot-cache", "miss")
	writeJSON(w, http.StatusOK, resp)
}

// ---- Create Account ----

func (h *accountsHandler) createAccount(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountCreatePayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid account payload."})
		return
	}

	if body.SiteID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid siteId. Expected positive number."})
		return
	}

	// Check site exists
	var site store.Site
	if err := h.db.Get(&site, "SELECT * FROM sites WHERE id = ?", body.SiteID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "site not found"})
		return
	}

	// Resolve credential mode
	credentialMode := service.ResolveRequestedCredentialMode(body.CredentialMode)
	if body.AccessTokens != nil && len(body.AccessTokens) > 0 {
		credentialMode = service.CredentialModeAPIKey
	}

	// Resolve tokens to create
	var requestedTokens []string
	if credentialMode != service.CredentialModeAPIKey {
		if body.AccessToken != nil && strings.TrimSpace(*body.AccessToken) != "" {
			requestedTokens = []string{strings.TrimSpace(*body.AccessToken)}
		}
	} else {
		if body.AccessTokens != nil && len(body.AccessTokens) > 0 {
			requestedTokens = body.AccessTokens
		} else if body.AccessToken != nil && strings.TrimSpace(*body.AccessToken) != "" {
			requestedTokens = []string{strings.TrimSpace(*body.AccessToken)}
		}
	}

	if len(requestedTokens) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "请填写 Token"})
		return
	}

	// Batch API key creation (multiple tokens)
	if credentialMode == service.CredentialModeAPIKey && len(requestedTokens) > 1 {
		var items []map[string]any
		createdCount := 0
		for i, token := range requestedTokens {
			username := body.Username
			if username != nil {
				name := *username
				if len(name) > 0 {
					n := buildBatchAPIKeyName(name, i, len(requestedTokens))
					username = &n
				}
			}

			accountID, err := h.createSingleAccount(body, site, credentialMode, token, username)
			if err != nil {
				items = append(items, map[string]any{
					"index":   i,
					"status":  "failed",
					"message": err.Error(),
				})
			} else {
				createdCount++
				items = append(items, map[string]any{
					"index":    i,
					"status":   "created",
					"id":       accountID,
					"username": coalesceStr(username, ""),
					"queued":   false,
					"message":  nil,
				})
			}
		}

		if createdCount == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success":      false,
				"batch":        true,
				"totalCount":   len(requestedTokens),
				"createdCount": 0,
				"failedCount":  len(requestedTokens),
				"message":      "批量添加失败（0/" + strconv.Itoa(len(requestedTokens)) + "）",
				"items":        items,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success":      true,
			"batch":        true,
			"totalCount":   len(requestedTokens),
			"createdCount": createdCount,
			"failedCount":  len(requestedTokens) - createdCount,
			"message":      "批量添加完成：成功 " + strconv.Itoa(createdCount) + "，失败 " + strconv.Itoa(len(requestedTokens)-createdCount),
			"items":        items,
		})
		return
	}

	// Single token creation
	accountID, err := h.createSingleAccount(body, site, credentialMode, requestedTokens[0], body.Username)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success":  false,
			"message":  err.Error(),
		})
		return
	}

	// Fetch created account
	var account store.Account
	h.db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID)

	caps := service.BuildCapabilitiesForAccount(&account)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               account.ID,
		"siteId":           account.SiteID,
		"username":         account.Username,
		"accessToken":      account.AccessToken,
		"status":           account.Status,
		"tokenType":        string(credentialMode),
		"credentialMode":   string(service.ResolveStoredCredentialMode(&account)),
		"capabilities":     caps,
		"modelCount":       0,
		"apiTokenFound":    account.APIToken != nil,
		"usernameDetected": false,
		"queued":           false,
	})
}

func (h *accountsHandler) createSingleAccount(body payloads.AccountCreatePayload, site store.Site, credentialMode service.AccountCredentialMode, accessToken string, username *string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	sortOrder, _ := service.GetNextAccountSortOrder(h.db)

	checkinEnabled := true
	if body.CheckinEnabled != nil {
		checkinEnabled = *body.CheckinEnabled
	}

	accessTokenVal := accessToken
	apiTokenVal := body.APIToken

	// Stub: Verify token against upstream (P4 adapter)
	// For now, just create the account row

	extraConfig := map[string]any{
		"credentialMode": string(credentialMode),
	}
	if body.PlatformUserID != nil {
		extraConfig["platformUserId"] = *body.PlatformUserID
	}
	// Store tokenExpiresAt for session-mode accounts
	if body.TokenExpiresAt != nil && credentialMode == service.CredentialModeSession {
		extraConfig["tokenExpiresAt"] = *body.TokenExpiresAt
	}

	status := "active"

	extraConfigStr := service.MarshalExtraConfig(extraConfig)

	isPinned := false
	if body.SkipModelFetch != nil {
		_ = *body.SkipModelFetch
	}

	result, err := h.db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, api_token, status, is_pinned, sort_order,
		 checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ID, username, accessTokenVal, apiTokenVal, status, isPinned, sortOrder,
		checkinEnabled, extraConfigStr, now, now,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// ---- Login Account ----

func (h *accountsHandler) loginAccount(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountLoginPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid login payload."})
		return
	}

	if body.SiteID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid siteId. Expected positive number."})
		return
	}
	if strings.TrimSpace(body.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid username. Expected string."})
		return
	}
	if strings.TrimSpace(body.Password) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid password. Expected string."})
		return
	}

	// Get site
	var site store.Site
	if err := h.db.Get(&site, "SELECT * FROM sites WHERE id = ?", body.SiteID); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "site not found"})
		return
	}

	// Stub: P4 platform adapter.login()
	// Simulate login success
	loginAccessToken := "session_" + strconv.Itoa(int(body.SiteID)) + "_" + body.Username

	// Check for existing account (reusedAccount)
	var existing store.Account
	reused := false
	err := h.db.Get(&existing,
		"SELECT * FROM accounts WHERE site_id = ? AND username = ?",
		body.SiteID, body.Username,
	)
	if err == nil {
		reused = true
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Encrypt password for autoRelogin
	passwordCipher := service.EncryptPassword(h.cfg, body.Password)

	extraConfig := map[string]any{
		"credentialMode": "session",
		"autoRelogin": map[string]any{
			"username":      body.Username,
			"passwordCipher": passwordCipher,
			"updatedAt":     now,
		},
	}

	extraConfigStr := service.MarshalExtraConfig(extraConfig)

	if reused {
		// Update existing account
		h.db.Exec(
			`UPDATE accounts SET access_token = ?, checkin_enabled = 1, status = 'active',
			 extra_config = ?, updated_at = ? WHERE id = ?`,
			loginAccessToken, extraConfigStr, now, existing.ID,
		)
	} else {
		sortOrder, _ := service.GetNextAccountSortOrder(h.db)
		h.db.Exec(
			`INSERT INTO accounts (site_id, username, access_token, checkin_enabled, status,
			 is_pinned, sort_order, extra_config, created_at, updated_at)
			 VALUES (?, ?, ?, 1, 'active', 0, ?, ?, ?, ?)`,
			body.SiteID, body.Username, loginAccessToken, sortOrder, extraConfigStr, now, now,
		)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"account":       nil,
		"apiTokenFound": false,
		"tokenCount":    0,
		"reusedAccount": reused,
	})
}

// ---- Verify Token ----

func (h *accountsHandler) verifyToken(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountVerifyTokenPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid verify-token payload."})
		return
	}

	if body.SiteID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid siteId. Expected positive number."})
		return
	}

	accessToken := ""
	if body.AccessToken != nil {
		accessToken = strings.TrimSpace(*body.AccessToken)
	}
	if accessToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "Token 不能为空"})
		return
	}

	// Get site
	var site store.Site
	if err := h.db.Get(&site, "SELECT * FROM sites WHERE id = ?", body.SiteID); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "site not found"})
		return
	}

	// Stub: P4 adapter.verifyToken()
	// Simulate verification - treat as valid apikey
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"tokenType":  "apikey",
		"modelCount": 0,
		"models":     []string{},
	})
}

// ---- Rebind Session ----

func (h *accountsHandler) rebindSession(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	accountID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || accountID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "账号 ID 无效"})
		return
	}

	var body payloads.AccountRebindSessionPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid rebind payload."})
		return
	}

	nextAccessToken := ""
	if body.AccessToken != nil {
		nextAccessToken = strings.TrimSpace(*body.AccessToken)
	}
	if nextAccessToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "请提供新的 Session Token"})
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, accountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// TODO(P4): handle sub2api managed auth (extraConfig.sub2apiAuth.refreshToken / tokenExpiresAt)
	// Spec lines 528-542: when platform is sub2api, rebind-session must update sub2apiAuth fields.
	extraConfigPatch := map[string]any{
		"credentialMode": "session",
	}
	extraConfigStr := service.MergeExtraConfig(row.Account.ExtraConfig, extraConfigPatch)

	h.db.Exec(
		"UPDATE accounts SET access_token = ?, status = 'active', extra_config = ?, updated_at = ? WHERE id = ?",
		nextAccessToken, extraConfigStr, now, accountID,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"tokenType":      "session",
		"credentialMode": "session",
		"capabilities":   service.BuildCapabilitiesFromCredentialMode(service.CredentialModeSession, true),
		"apiTokenFound":  false,
	})
}

// ---- Update Account ----

func (h *accountsHandler) updateAccount(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account id"})
		return
	}

	var body payloads.AccountUpdatePayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account payload."})
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "account not found"})
		return
	}

	updates := map[string]any{}
	if body.Username != nil {
		updates["username"] = *body.Username
	}
	if body.AccessToken != nil {
		updates["accessToken"] = *body.AccessToken
	}
	if body.APIToken != nil {
		updates["apiToken"] = body.APIToken
	}
	if body.Status != nil {
		updates["status"] = strings.TrimSpace(*body.Status)
	}
	if body.CheckinEnabled != nil {
		updates["checkinEnabled"] = *body.CheckinEnabled
	}
	if body.UnitCost != nil {
		updates["unitCost"] = *body.UnitCost
	}
	if body.ExtraConfig != nil {
		// Merge with existing extraConfig to preserve keys not in the update
		if ecMap, ok := body.ExtraConfig.(map[string]any); ok {
			baseEC := row.Account.ExtraConfig
			ecStr := ""
			if baseEC != nil {
				ecStr = *baseEC
			}
			merged := service.MergeExtraConfig(&ecStr, ecMap)
			if merged != nil {
				updates["extraConfig"] = *merged
			}
		}
	}
	if body.IsPinned != nil {
		updates["isPinned"] = *body.IsPinned
	}
	if body.SortOrder != nil {
		so := service.NormalizeSortOrder(body.SortOrder)
		if so == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid sortOrder value. Expected non-negative integer."})
			return
		}
		updates["sortOrder"] = int64(*so)
	}
	if body.ProxyURL != nil {
		baseEC := row.Account.ExtraConfig
		ecStr := ""
		if baseEC != nil {
			ecStr = *baseEC
		}
		ec := service.MergeExtraConfig(&ecStr, map[string]any{
			"proxyUrl": service.NormalizeNullable(body.ProxyURL),
		})
		updates["extraConfig"] = *ec
	}
	// TODO(P4): handle sub2api managed auth (extraConfig.sub2apiAuth.refreshToken / tokenExpiresAt)
	// Spec lines 530-542: updateAccount for sub2api platform must merge sub2apiAuth fields.
	// TODO(P4): expired API key recovery — when updating an expired API key account,
	// trigger model refresh with allowInactive:true and reactivateAfterSuccessfulModelRefresh:true
	// (spec lines 594-602, TS accounts.ts:1550-1556)

	if err := service.UpdateAccountFields(h.db, id, updates); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	var updated store.Account
	h.db.Get(&updated, "SELECT * FROM accounts WHERE id = ?", id)
	writeJSON(w, http.StatusOK, updated)
}

// ---- Delete Account ----

func (h *accountsHandler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account id"})
		return
	}
	if err := service.DeleteAccount(h.db, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	service.RebuildRoutesBestEffort()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---- Batch Accounts ----

func (h *accountsHandler) batchAccounts(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountBatchPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account batch payload."})
		return
	}

	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return
	}

	action := strings.TrimSpace(body.Action)
	validActions := map[string]bool{"enable": true, "disable": true, "delete": true, "refreshBalance": true}
	if !validActions[action] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid action"})
		return
	}

	var successIDs []int64
	var failedItems []map[string]any
	shouldRebuildRoutes := false

	for _, rawID := range body.IDs {
		id := int64(rawID)

		if action == "refreshBalance" {
			// Stub: P4 balance refresh
			successIDs = append(successIDs, id)
			continue
		}

		var existing store.Account
		err := h.db.Get(&existing, "SELECT * FROM accounts WHERE id = ?", id)
		if err != nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "Account not found"})
			continue
		}

		now := time.Now().UTC().Format(time.RFC3339)
		switch action {
		case "delete":
			h.db.Exec("DELETE FROM accounts WHERE id = ?", id)
			shouldRebuildRoutes = true
		case "enable":
			h.db.Exec("UPDATE accounts SET status = 'active', updated_at = ? WHERE id = ?", now, id)
		case "disable":
			h.db.Exec("UPDATE accounts SET status = 'disabled', updated_at = ? WHERE id = ?", now, id)
		}
		successIDs = append(successIDs, id)
	}

	if shouldRebuildRoutes {
		service.RebuildRoutesBestEffort()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"successIds":  successIDs,
		"failedItems": failedItems,
	})
}

// ---- Health Refresh ----

func (h *accountsHandler) healthRefresh(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountHealthRefreshPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid health refresh payload."})
		return
	}

	wait := body.Wait != nil && *body.Wait

	if wait {
		// Stub: sync health refresh
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"summary": map[string]int{
				"total": 0, "healthy": 0, "unhealthy": 0,
				"degraded": 0, "disabled": 0, "unknown": 0,
				"success": 0, "failed": 0, "skipped": 0,
			},
			"results": []any{},
		})
		return
	}

	// Background task (stub)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"jobId":   "stub-health-refresh",
		"status":  "pending",
		"message": "已开始刷新账号运行健康状态，请稍后查看账号列表",
	})
}

// ---- Refresh Balance ----

func (h *accountsHandler) refreshBalance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "account not found or platform not supported"})
		return
	}

	var account store.Account
	if err := h.db.Get(&account, "SELECT * FROM accounts WHERE id = ?", id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "account not found or platform not supported"})
		return
	}

	// Stub: P4 balance refresh
	writeJSON(w, http.StatusOK, map[string]any{
		"balance":     account.Balance,
		"balanceUsed": account.BalanceUsed,
		"quota":       account.Quota,
	})
}

// ---- Account Models ----

func (h *accountsHandler) getAccountModels(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "账号 ID 无效"})
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "账号不存在"})
		return
	}

	// Get available models for this account
	type modelRow struct {
		ModelName string  `db:"model_name"`
		Available int     `db:"available"`
		LatencyMs *int64  `db:"latency_ms"`
		IsManual  int     `db:"is_manual"`
	}
	var modelRows []modelRow
	h.db.Select(&modelRows, "SELECT model_name, available, latency_ms, is_manual FROM model_availability WHERE account_id = ?", id)

	// Get disabled models for this site
	var disabledRows []string
	h.db.Select(&disabledRows, "SELECT model_name FROM site_disabled_models WHERE site_id = ?", row.Account.SiteID)

	disabledSet := map[string]bool{}
	for _, m := range disabledRows {
		disabledSet[m] = true
	}

	var models []map[string]any
	for _, r := range modelRows {
		if r.Available == 0 {
			continue
		}
		models = append(models, map[string]any{
			"name":      r.ModelName,
			"latencyMs": r.LatencyMs,
			"disabled":  disabledSet[r.ModelName],
			"isManual":  r.IsManual == 1,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"siteId":        row.Account.SiteID,
		"siteName":      row.Site.Name,
		"models":        models,
		"totalCount":    len(models),
		"disabledCount": countDisabled(models),
	})
}

func (h *accountsHandler) manualModels(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "账号 ID 无效"})
		return
	}

	var body payloads.AccountManualModelsPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid models. Expected string[]."})
		return
	}

	if len(body.Models) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "模型列表不能为空"})
		return
	}

	// Deduplicate
	seen := map[string]bool{}
	var models []string
	for _, m := range body.Models {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "模型列表不能为空"})
		return
	}

	var account store.Account
	if err := h.db.Get(&account, "SELECT * FROM accounts WHERE id = ?", id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "账号不存在"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, m := range models {
		var existingID int64
		err := h.db.Get(&existingID, "SELECT id FROM model_availability WHERE account_id = ? AND model_name = ?", id, m)
		if err == nil {
			h.db.Exec("UPDATE model_availability SET available = 1, latency_ms = NULL, is_manual = 1, checked_at = ? WHERE id = ?", now, existingID)
		} else {
			h.db.Exec("INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at) VALUES (?, ?, 1, 1, NULL, ?)", id, m, now)
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---- Helpers ----

func buildBatchAPIKeyName(username string, index, total int) string {
	if username == "" {
		return fmt.Sprintf("key-%d", index+1)
	}
	return fmt.Sprintf("%s-%d", username, index+1)
}

func coalesceStr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

func countDisabled(models []map[string]any) int {
	count := 0
	for _, m := range models {
		if d, ok := m["disabled"].(bool); ok && d {
			count++
		}
	}
	return count
}
