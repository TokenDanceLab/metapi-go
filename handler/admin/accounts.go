package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/handler/admin/payloads"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/service/alert"
	balanceService "github.com/tokendancelab/metapi-go/service/balance"
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
	mu        sync.RWMutex
	data      []byte
	expiresAt time.Time
	ttl       time.Duration
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

func (c *accountsSnapshotCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = nil
	c.expiresAt = time.Time{}
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
		slog.Error("Failed to load accounts", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load accounts"})
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid account payload."})
		return
	}

	if body.SiteID <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid siteId. Expected positive number.")
		return
	}

	// Check site exists
	var site store.Site
	if err := h.db.Get(&site, h.db.Rebind("SELECT * FROM sites WHERE id = ?"), body.SiteID); err != nil {
		writeError(w, http.StatusBadRequest, "site not found")
		return
	}

	// Resolve credential mode
	credentialMode := service.ResolveRequestedCredentialMode(body.CredentialMode)
	if body.AccessTokens != nil && len(body.AccessTokens) > 0 {
		credentialMode = service.CredentialModeAPIKey
	}

	// Resolve tokens to create
	var requestedTokens []string
	appendRequestedToken := func(token string) {
		token = strings.TrimSpace(token)
		if token != "" {
			requestedTokens = append(requestedTokens, token)
		}
	}
	if credentialMode != service.CredentialModeAPIKey {
		if body.AccessToken != nil && strings.TrimSpace(*body.AccessToken) != "" {
			appendRequestedToken(*body.AccessToken)
		}
	} else {
		if body.AccessTokens != nil && len(body.AccessTokens) > 0 {
			for _, token := range body.AccessTokens {
				appendRequestedToken(token)
			}
		} else if body.AccessToken != nil && strings.TrimSpace(*body.AccessToken) != "" {
			appendRequestedToken(*body.AccessToken)
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

			created, err := h.createSingleAccount(r.Context(), body, site, credentialMode, token, username)
			if err != nil {
				slog.Error("Batch account creation failed", "err", err, "index", i)
				item := map[string]any{
					"index":   i,
					"status":  "failed",
					"message": err.Error(),
				}
				var verifyErr *accountCreateError
				if errors.As(err, &verifyErr) {
					item["message"] = verifyErr.Message
					item["requiresVerification"] = verifyErr.RequiresVerification
				}
				items = append(items, item)
			} else {
				createdCount++
				items = append(items, map[string]any{
					"index":      i,
					"status":     "created",
					"id":         created.ID,
					"username":   coalesceStr(created.Username, ""),
					"queued":     false,
					"message":    nil,
					"modelCount": created.ModelCount,
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

		routing.InvalidateCache()
		globalAccountsCache.clear()
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
	created, err := h.createSingleAccount(r.Context(), body, site, credentialMode, requestedTokens[0], body.Username)
	if err != nil {
		slog.Error("Account creation failed", "err", err)
		message := err.Error()
		requiresVerification := false
		var verifyErr *accountCreateError
		if errors.As(err, &verifyErr) {
			message = verifyErr.Message
			requiresVerification = verifyErr.RequiresVerification
			if credentialMode != service.CredentialModeAPIKey {
				message = alert.AppendSessionTokenRebindHint(message)
			}
		} else if credentialMode != service.CredentialModeAPIKey {
			message = alert.AppendSessionTokenRebindHint(message)
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success":              false,
			"requiresVerification": requiresVerification,
			"message":              message,
		})
		return
	}

	// Fetch created account
	var account store.Account
	h.db.Get(&account, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), created.ID)

	caps := service.BuildCapabilitiesForAccount(&account)
	routing.InvalidateCache()
	globalAccountsCache.clear()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               account.ID,
		"siteId":           account.SiteID,
		"username":         account.Username,
		"accessToken":      account.AccessToken,
		"status":           account.Status,
		"tokenType":        created.TokenType,
		"credentialMode":   string(service.ResolveStoredCredentialMode(&account)),
		"capabilities":     caps,
		"modelCount":       created.ModelCount,
		"apiTokenFound":    created.APITokenFound,
		"usernameDetected": created.UsernameDetected,
		"queued":           false,
	})
}

// accountCreateError is returned when create fails closed on token verification.
type accountCreateError struct {
	Message              string
	RequiresVerification bool
}

func (e *accountCreateError) Error() string {
	if e == nil {
		return "account create failed"
	}
	return e.Message
}

// createAccountOutcome holds metadata from a successful createSingleAccount call.
type createAccountOutcome struct {
	ID               int64
	Username         *string
	TokenType        string
	ModelCount       int
	APITokenFound    bool
	UsernameDetected bool
}

func (h *accountsHandler) createSingleAccount(ctx context.Context, body payloads.AccountCreatePayload, site store.Site, credentialMode service.AccountCredentialMode, rawAccessToken string, username *string) (*createAccountOutcome, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rawAccessToken = strings.TrimSpace(rawAccessToken)
	if rawAccessToken == "" {
		return nil, &accountCreateError{Message: "请填写 Token", RequiresVerification: false}
	}

	adp := platform.GetAdapter(site.Platform)
	if adp == nil {
		return nil, &accountCreateError{
			Message:              "platform not supported: " + site.Platform,
			RequiresVerification: false,
		}
	}

	skipModelFetch := body.SkipModelFetch != nil && *body.SkipModelFetch
	proxyCfg := service.BuildPlatformProxyConfig(h.cfg, nil, &site)

	resolvedUsername := strings.TrimSpace(coalesceStr(username, ""))
	usernameProvided := resolvedUsername != ""
	accessTokenVal := rawAccessToken
	apiTokenVal := ""
	if body.APIToken != nil {
		apiTokenVal = strings.TrimSpace(*body.APIToken)
	}
	var tokenType string
	verifiedModels := []string{}

	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	switch credentialMode {
	case service.CredentialModeAPIKey:
		if skipModelFetch {
			// Explicit skip: accept as API-key without upstream model verify.
			tokenType = "apikey"
			accessTokenVal = ""
			if apiTokenVal == "" {
				apiTokenVal = rawAccessToken
			}
		} else {
			models, err := adp.GetModels(verifyCtx, site.URL, rawAccessToken, body.PlatformUserID, proxyCfg)
			if err != nil {
				return nil, &accountCreateError{
					Message:              "API Key 验证失败：" + err.Error(),
					RequiresVerification: true,
				}
			}
			for _, m := range models {
				m = strings.TrimSpace(m)
				if m != "" {
					verifiedModels = append(verifiedModels, m)
				}
			}
			if len(verifiedModels) == 0 {
				return nil, &accountCreateError{
					Message:              "API Key 验证失败：未获取到可用模型",
					RequiresVerification: true,
				}
			}
			tokenType = "apikey"
			accessTokenVal = ""
			if apiTokenVal == "" {
				apiTokenVal = rawAccessToken
			}
		}
	default:
		// auto / session: require platform.VerifyToken success (fail closed).
		result, err := adp.VerifyToken(verifyCtx, site.URL, rawAccessToken, body.PlatformUserID, proxyCfg)
		if err != nil {
			return nil, &accountCreateError{
				Message:              err.Error(),
				RequiresVerification: true,
			}
		}
		if result == nil || result.TokenType == "" || result.TokenType == "unknown" {
			return nil, &accountCreateError{
				Message:              "Token 验证失败，请先点击“验证 Token”，验证成功后再绑定账号",
				RequiresVerification: true,
			}
		}
		tokenType = result.TokenType

		if credentialMode == service.CredentialModeSession && tokenType != "session" {
			return nil, &accountCreateError{
				Message:              "当前凭证是 API Key，请切换到 API Key 模式，或改用 Session Token",
				RequiresVerification: false,
			}
		}

		if tokenType == "session" {
			if resolvedUsername == "" && result.UserInfo != nil {
				if u := strings.TrimSpace(result.UserInfo.Username); u != "" {
					resolvedUsername = u
				} else if u := strings.TrimSpace(result.UserInfo.DisplayName); u != "" {
					resolvedUsername = u
				}
			}
			if apiTokenVal == "" && strings.TrimSpace(result.APIToken) != "" {
				apiTokenVal = strings.TrimSpace(result.APIToken)
			}
			accessTokenVal = rawAccessToken
		} else if tokenType == "apikey" {
			accessTokenVal = ""
			if apiTokenVal == "" {
				apiTokenVal = rawAccessToken
			}
			for _, m := range result.Models {
				m = strings.TrimSpace(m)
				if m != "" {
					verifiedModels = append(verifiedModels, m)
				}
			}
		}
	}

	// Resolve platform user id (explicit body wins; else guess from username).
	var resolvedPlatformUserID *int
	if body.PlatformUserID != nil {
		resolvedPlatformUserID = body.PlatformUserID
	} else if guessed := service.GuessPlatformUserIdFromUsername(&resolvedUsername); guessed > 0 {
		id := int(guessed)
		resolvedPlatformUserID = &id
	}

	resolvedCredentialMode := service.CredentialModeSession
	if tokenType == "apikey" {
		resolvedCredentialMode = service.CredentialModeAPIKey
	}

	checkinEnabled := false
	if tokenType == "session" {
		checkinEnabled = true
		if body.CheckinEnabled != nil {
			checkinEnabled = *body.CheckinEnabled
		}
	}

	extraConfig := map[string]any{
		"credentialMode": string(resolvedCredentialMode),
	}
	if resolvedPlatformUserID != nil {
		extraConfig["platformUserId"] = *resolvedPlatformUserID
	}
	// Store tokenExpiresAt for session-mode accounts
	if body.TokenExpiresAt != nil && resolvedCredentialMode == service.CredentialModeSession {
		extraConfig["tokenExpiresAt"] = *body.TokenExpiresAt
	}
	// Preserve skipModelFetch intent for residual docs / future init queues.
	if skipModelFetch {
		extraConfig["skipModelFetch"] = true
	}

	extraConfigStr := service.MarshalExtraConfig(extraConfig)

	var usernameVal *string
	if resolvedUsername != "" {
		u := resolvedUsername
		usernameVal = &u
	}
	var apiTokenPtr *string
	if apiTokenVal != "" {
		t := apiTokenVal
		apiTokenPtr = &t
	}

	sortOrder, _ := service.GetNextAccountSortOrder(h.db)
	status := "active"
	isPinned := false

	var id int64
	err := h.db.QueryRowx(
		h.db.Rebind(`INSERT INTO accounts (site_id, username, access_token, api_token, status, is_pinned, sort_order,
		 checkin_enabled, extra_config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 RETURNING id`),
		site.ID, usernameVal, accessTokenVal, apiTokenPtr, status, isPinned, sortOrder,
		checkinEnabled, extraConfigStr, now, now,
	).Scan(&id)
	if err != nil {
		return nil, err
	}

	return &createAccountOutcome{
		ID:               id,
		Username:         usernameVal,
		TokenType:        tokenType,
		ModelCount:       len(verifiedModels),
		APITokenFound:    apiTokenPtr != nil,
		UsernameDetected: !usernameProvided && usernameVal != nil,
	}, nil
}

// ---- Login Account ----

func (h *accountsHandler) loginAccount(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountLoginPayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid login payload.")
		return
	}

	if body.SiteID <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid siteId. Expected positive number.")
		return
	}
	if strings.TrimSpace(body.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid username. Expected string."})
		return
	}
	if strings.TrimSpace(body.Password) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid password. Expected string."})
		return
	}

	// Get site
	var site store.Site
	if err := h.db.Get(&site, h.db.Rebind("SELECT * FROM sites WHERE id = ?"), body.SiteID); err != nil {
		writeError(w, http.StatusNotFound, "site not found")
		return
	}

	adp := platform.GetAdapter(site.Platform)
	if adp == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "unsupported platform: " + site.Platform})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	loginResult, err := adp.Login(ctx, site.URL, body.Username, body.Password, nil, service.BuildPlatformProxyConfig(h.cfg, nil, &site))
	if err != nil {
		slog.Warn("Account login failed", "err", err, "site_id", site.ID, "platform", site.Platform)
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}
	if loginResult == nil || !loginResult.Success || strings.TrimSpace(loginResult.AccessToken) == "" {
		message := "login failed"
		if loginResult != nil && strings.TrimSpace(loginResult.Message) != "" {
			message = strings.TrimSpace(loginResult.Message)
		}
		writeError(w, http.StatusUnauthorized, message)
		return
	}
	loginAccessToken := strings.TrimSpace(loginResult.AccessToken)

	// Check for existing account (reusedAccount)
	var existing store.Account
	reused := false
	err = h.db.Get(&existing,
		h.db.Rebind("SELECT * FROM accounts WHERE site_id = ? AND username = ?"),
		body.SiteID, body.Username,
	)
	if err == nil {
		reused = true
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Encrypt password for autoRelogin
	passwordCipher, encErr := service.EncryptPassword(h.cfg, body.Password)
	if encErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to encrypt password."})
		return
	}

	extraConfig := map[string]any{
		"credentialMode": "session",
		"autoRelogin": map[string]any{
			"username":       body.Username,
			"passwordCipher": passwordCipher,
			"updatedAt":      now,
		},
	}

	extraConfigStr := service.MarshalExtraConfig(extraConfig)

	if reused {
		// Update existing account
		if _, err := h.db.Exec(
			h.db.Rebind(`UPDATE accounts SET access_token = ?, checkin_enabled = ?, status = 'active',
			 extra_config = ?, updated_at = ? WHERE id = ?`),
			loginAccessToken, true, extraConfigStr, now, existing.ID,
		); err != nil {
			slog.Error("Failed to update login account", "err", err, "account_id", existing.ID)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to save account."})
			return
		}
	} else {
		sortOrder, _ := service.GetNextAccountSortOrder(h.db)
		if _, err := h.db.Exec(
			h.db.Rebind(`INSERT INTO accounts (site_id, username, access_token, checkin_enabled, status,
			 is_pinned, sort_order, extra_config, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?, ?)`),
			body.SiteID, body.Username, loginAccessToken, true, false, sortOrder, extraConfigStr, now, now,
		); err != nil {
			slog.Error("Failed to insert login account", "err", err, "site_id", body.SiteID)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to save account."})
			return
		}
	}

	// Fetch the created/updated account for response
	var loginAcct store.Account
	if err := h.db.Get(&loginAcct, h.db.Rebind("SELECT * FROM accounts WHERE site_id = ? AND username = ?"), body.SiteID, body.Username); err != nil {
		slog.Error("Failed to load login account", "err", err, "site_id", body.SiteID)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to load account."})
		return
	}
	loginAcctMap := map[string]any{
		"id":             loginAcct.ID,
		"siteId":         loginAcct.SiteID,
		"username":       loginAcct.Username,
		"accessToken":    loginAcct.AccessToken,
		"apiToken":       loginAcct.APIToken,
		"balance":        loginAcct.Balance,
		"status":         loginAcct.Status,
		"isPinned":       loginAcct.IsPinned,
		"sortOrder":      loginAcct.SortOrder,
		"checkinEnabled": loginAcct.CheckinEnabled,
		"extraConfig":    loginAcct.ExtraConfig,
		"createdAt":      loginAcct.CreatedAt,
		"updatedAt":      loginAcct.UpdatedAt,
	}
	routing.InvalidateCache()
	globalAccountsCache.clear()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"account":       loginAcctMap,
		"apiTokenFound": loginAcct.APIToken != nil,
		"tokenCount":    1,
		"reusedAccount": reused,
	})
}

// ---- Verify Token ----

func (h *accountsHandler) verifyToken(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountVerifyTokenPayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid verify-token payload.")
		return
	}

	if body.SiteID <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid siteId. Expected positive number.")
		return
	}

	accessToken := ""
	if body.AccessToken != nil {
		accessToken = strings.TrimSpace(*body.AccessToken)
	}
	if accessToken == "" {
		writeError(w, http.StatusBadRequest, "Token 不能为空")
		return
	}

	// Get site
	var site store.Site
	if err := h.db.Get(&site, h.db.Rebind("SELECT * FROM sites WHERE id = ?"), body.SiteID); err != nil {
		writeError(w, http.StatusNotFound, "site not found")
		return
	}

	adp := platform.GetAdapter(site.Platform)
	if adp == nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "unsupported platform: " + site.Platform})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := adp.VerifyToken(ctx, site.URL, accessToken, body.PlatformUserID, service.BuildPlatformProxyConfig(h.cfg, nil, &site))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if result == nil || result.TokenType == "" || result.TokenType == "unknown" {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":   false,
			"tokenType": "unknown",
			"message":   "token verification failed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"tokenType":     result.TokenType,
		"modelCount":    len(result.Models),
		"models":        result.Models,
		"userInfo":      result.UserInfo,
		"balance":       result.Balance,
		"apiToken":      result.APIToken,
		"apiTokenFound": result.APIToken != "",
	})
}

func shouldMirrorAPIKeyToken(account store.Account, requestedAPIToken any) bool {
	if service.ResolveStoredCredentialMode(&account) != service.CredentialModeAPIKey {
		return false
	}
	if requestedAPIToken == nil {
		return true
	}
	requested, ok := requestedAPIToken.(string)
	if !ok {
		return false
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}
	current := ""
	if account.APIToken != nil {
		current = strings.TrimSpace(*account.APIToken)
	}
	return requested == current
}

func normalizeAPITokenUpdate(value any) any {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return value
}

func normalizeAccountStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "disabled", "expired":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid rebind payload."})
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

	if _, err := h.db.Exec(
		h.db.Rebind("UPDATE accounts SET access_token = ?, status = 'active', extra_config = ?, updated_at = ? WHERE id = ?"),
		nextAccessToken, extraConfigStr, now, accountID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "failed to update account"})
		return
	}

	// Fetch updated account for response
	var rebindAcct store.Account
	if err := h.db.Get(&rebindAcct, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), accountID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "failed to read updated account"})
		return
	}
	rebindAcctMap := map[string]any{
		"id":             rebindAcct.ID,
		"siteId":         rebindAcct.SiteID,
		"username":       rebindAcct.Username,
		"accessToken":    rebindAcct.AccessToken,
		"apiToken":       rebindAcct.APIToken,
		"balance":        rebindAcct.Balance,
		"status":         rebindAcct.Status,
		"isPinned":       rebindAcct.IsPinned,
		"sortOrder":      rebindAcct.SortOrder,
		"checkinEnabled": rebindAcct.CheckinEnabled,
		"extraConfig":    rebindAcct.ExtraConfig,
		"createdAt":      rebindAcct.CreatedAt,
		"updatedAt":      rebindAcct.UpdatedAt,
	}
	routing.InvalidateCache()
	globalAccountsCache.clear()
	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"account":        rebindAcctMap,
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
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account payload."})
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "account not found"})
		return
	}

	updates := map[string]any{}
	pendingExtraConfig := row.Account.ExtraConfig
	mergeExtraConfigUpdate := func(patch map[string]any) {
		merged := service.MergeExtraConfig(pendingExtraConfig, patch)
		if merged != nil {
			pendingExtraConfig = merged
			updates["extraConfig"] = *merged
		}
	}
	if body.Username != nil {
		updates["username"] = *body.Username
	}
	mirrorAPIKeyToken := false
	if body.AccessToken != nil {
		nextAccessToken := strings.TrimSpace(*body.AccessToken)
		updates["accessToken"] = nextAccessToken
		mirrorAPIKeyToken = shouldMirrorAPIKeyToken(row.Account, body.APIToken)
		if mirrorAPIKeyToken {
			updates["apiToken"] = nextAccessToken
		}
	}
	if body.APIToken != nil && !mirrorAPIKeyToken {
		updates["apiToken"] = normalizeAPITokenUpdate(body.APIToken)
	}
	if body.Status != nil {
		status := normalizeAccountStatus(*body.Status)
		if status == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account status. Expected active, disabled, or expired."})
			return
		}
		updates["status"] = status
	}
	if body.UnitCost != nil {
		if *body.UnitCost <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid unitCost value. Expected positive number."})
			return
		}
		updates["unitCost"] = *body.UnitCost
	}
	if body.ExtraConfig != nil {
		// Merge with existing extraConfig to preserve keys not in the update
		if ecMap, ok := body.ExtraConfig.(map[string]any); ok {
			mergeExtraConfigUpdate(ecMap)
		}
	}
	nextAccount := row.Account
	nextAccount.ExtraConfig = pendingExtraConfig
	if accessToken, ok := updates["accessToken"].(string); ok {
		nextAccount.AccessToken = accessToken
	}
	if apiToken, ok := updates["apiToken"].(*string); ok {
		nextAccount.APIToken = apiToken
	}
	if service.BuildCapabilitiesForAccount(&nextAccount).CanCheckin {
		if body.CheckinEnabled != nil {
			updates["checkinEnabled"] = *body.CheckinEnabled
		}
	} else if row.Account.CheckinEnabled || body.CheckinEnabled != nil {
		updates["checkinEnabled"] = false
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
		mergeExtraConfigUpdate(map[string]any{
			"proxyUrl": service.NormalizeNullable(body.ProxyURL),
		})
	}
	// TODO(P4): handle sub2api managed auth (extraConfig.sub2apiAuth.refreshToken / tokenExpiresAt)
	// Spec lines 530-542: updateAccount for sub2api platform must merge sub2apiAuth fields.
	// TODO(P4): expired API key recovery — when updating an expired API key account,
	// trigger model refresh with allowInactive:true and reactivateAfterSuccessfulModelRefresh:true
	// (spec lines 594-602, TS accounts.ts:1550-1556)

	if err := service.UpdateAccountFields(h.db, id, updates); err != nil {
		slog.Error("Failed to update account", "err", err, "account_id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Failed to update account"})
		return
	}

	// When status is forced to expired, align stored runtime health with R0 auth class.
	if status, ok := updates["status"].(string); ok && status == "expired" {
		_ = service.SetAccountRuntimeHealth(h.db, id, service.RuntimeHealthEntry{
			State:  service.HealthUnhealthy,
			Reason: "连接凭证已过期，请更新凭证",
			Source: service.HealthSourceAuth,
		})
	}

	updatedRow, err := service.GetAccountWithSiteByID(h.db, id)
	if err != nil {
		slog.Error("Failed to load updated account", "err", err, "account_id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Failed to load updated account"})
		return
	}
	updated := updatedRow.Account
	caps := service.BuildCapabilitiesForAccount(&updated)
	sessionCapable := caps.CanRefreshBalance
	resp := map[string]any{
		"id":                 updated.ID,
		"siteId":             updated.SiteID,
		"username":           updated.Username,
		"accessToken":        updated.AccessToken,
		"apiToken":           updated.APIToken,
		"balance":            updated.Balance,
		"balanceUsed":        updated.BalanceUsed,
		"quota":              updated.Quota,
		"unitCost":           updated.UnitCost,
		"valueScore":         updated.ValueScore,
		"status":             updated.Status,
		"isPinned":           updated.IsPinned,
		"sortOrder":          updated.SortOrder,
		"checkinEnabled":     updated.CheckinEnabled,
		"lastCheckinAt":      updated.LastCheckinAt,
		"lastBalanceRefresh": updated.LastBalanceRefresh,
		"oauthProvider":      updated.OAuthProvider,
		"oauthAccountKey":    updated.OAuthAccountKey,
		"oauthProjectId":     updated.OAuthProjectID,
		"extraConfig":        updated.ExtraConfig,
		"createdAt":          updated.CreatedAt,
		"updatedAt":          updated.UpdatedAt,
		"credentialMode":     string(service.ResolveStoredCredentialMode(&updated)),
		"capabilities":       caps,
		"runtimeHealth": service.BuildRuntimeHealthForAccount(service.RuntimeHealthInput{
			AccountStatus:  updated.Status,
			SiteStatus:     updatedRow.Site.Status,
			ExtraConfig:    updated.ExtraConfig,
			SessionCapable: &sessionCapable,
			OAuthProvider:  updated.OAuthProvider,
		}),
		"site": map[string]any{
			"id":       updatedRow.Site.ID,
			"name":     updatedRow.Site.Name,
			"url":      updatedRow.Site.URL,
			"platform": updatedRow.Site.Platform,
			"status":   updatedRow.Site.Status,
		},
	}
	routing.InvalidateCache()
	globalAccountsCache.clear()
	writeJSON(w, http.StatusOK, resp)
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
		slog.Error("Failed to delete account", "err", err, "account_id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Failed to delete account"})
		return
	}
	service.RebuildRoutesBestEffort()
	globalAccountsCache.clear()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---- Batch Accounts ----

func (h *accountsHandler) batchAccounts(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountBatchPayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Invalid account batch payload."})
		return
	}

	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "ids is required"})
		return
	}

	action := strings.TrimSpace(body.Action)
	validActions := map[string]bool{"enable": true, "disable": true, "delete": true, "refreshBalance": true}
	if !validActions[action] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Invalid action"})
		return
	}

	successIDs := []int64{}
	failedItems := []map[string]any{}
	skippedItems := []map[string]any{}
	shouldRebuildRoutes := false
	shouldInvalidateRoutes := false

	for _, rawID := range body.IDs {
		id := int64(rawID)

		if action == "refreshBalance" {
			result, err := balanceService.RefreshBalance(h.cfg, h.db, id)
			if result == nil && err == nil {
				failedItems = append(failedItems, map[string]any{"id": id, "message": "Account not found"})
				continue
			}
			if err != nil {
				slog.Warn("Batch balance refresh failed", "err", err, "account_id", id)
				failedItems = append(failedItems, map[string]any{"id": id, "message": "Balance refresh failed"})
				continue
			}
			if result.Skipped {
				skippedItems = append(skippedItems, map[string]any{"id": id, "reason": result.Reason})
				continue
			}
			successIDs = append(successIDs, id)
			continue
		}

		var existing store.Account
		err := h.db.Get(&existing, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), id)
		if err != nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "Account not found"})
			continue
		}

		now := time.Now().UTC().Format(time.RFC3339)
		var execErr error
		switch action {
		case "delete":
			_, execErr = h.db.Exec(h.db.Rebind("DELETE FROM accounts WHERE id = ?"), id)
		case "enable":
			_, execErr = h.db.Exec(h.db.Rebind("UPDATE accounts SET status = 'active', updated_at = ? WHERE id = ?"), now, id)
		case "disable":
			_, execErr = h.db.Exec(h.db.Rebind("UPDATE accounts SET status = 'disabled', updated_at = ? WHERE id = ?"), now, id)
		}
		if execErr != nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "Account update failed"})
			continue
		}
		if action == "delete" {
			shouldRebuildRoutes = true
		} else if action == "enable" || action == "disable" {
			shouldInvalidateRoutes = true
		}
		successIDs = append(successIDs, id)
	}

	if shouldRebuildRoutes {
		service.RebuildRoutesBestEffort()
	} else if shouldInvalidateRoutes {
		routing.InvalidateCache()
	}
	if len(successIDs) > 0 {
		globalAccountsCache.clear()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"successIds":   successIDs,
		"failedItems":  failedItems,
		"skippedItems": skippedItems,
	})
}

// ---- Health Refresh ----

func (h *accountsHandler) healthRefresh(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountHealthRefreshPayload
	if err := decodeJSONRequest(r, &body); err != nil {
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

	result, err := balanceService.RefreshBalance(h.cfg, h.db, id)
	if result == nil && err == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "account not found or platform not supported"})
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "unsupported platform") {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "account not found or platform not supported"})
			return
		}
		slog.Warn("Balance refresh failed", "err", err, "account_id", id)
		writeJSON(w, http.StatusBadGateway, map[string]string{"message": "balance refresh failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"balance":     result.Balance,
		"balanceUsed": result.Used,
		"quota":       result.Quota,
		"skipped":     result.Skipped,
		"reason":      result.Reason,
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
		ModelName string `db:"model_name"`
		Available int    `db:"available"`
		LatencyMs *int64 `db:"latency_ms"`
		IsManual  int    `db:"is_manual"`
	}
	var modelRows []modelRow
	h.db.Select(&modelRows, h.db.Rebind("SELECT model_name, CASE WHEN available THEN 1 ELSE 0 END AS available, latency_ms, CASE WHEN is_manual THEN 1 ELSE 0 END AS is_manual FROM model_availability WHERE account_id = ?"), id)

	// Get disabled models for this site
	var disabledRows []string
	h.db.Select(&disabledRows, h.db.Rebind("SELECT model_name FROM site_disabled_models WHERE site_id = ?"), row.Account.SiteID)

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
	if err := decodeJSONRequest(r, &body); err != nil {
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
	if err := h.db.Get(&account, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "账号不存在"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := h.db.Beginx()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, m := range models {
		var existingID int64
		err := tx.Get(&existingID, tx.Rebind("SELECT id FROM model_availability WHERE account_id = ? AND model_name = ?"), id, m)
		if err == nil {
			if _, err := tx.Exec(tx.Rebind("UPDATE model_availability SET available = ?, latency_ms = NULL, is_manual = ?, checked_at = ? WHERE id = ?"), true, true, now, existingID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update manual model"})
				return
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read manual model"})
			return
		}
		if _, err := tx.Exec(tx.Rebind("INSERT INTO model_availability (account_id, model_name, available, is_manual, latency_ms, checked_at) VALUES (?, ?, ?, ?, NULL, ?)"), id, m, true, true, now); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to insert manual model"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to commit manual models"})
		return
	}
	committed = true

	routing.InvalidateCache()
	globalAccountsCache.clear()
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
