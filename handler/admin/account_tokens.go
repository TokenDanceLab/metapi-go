package admin

import (
	"context"
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
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

// RegisterAccountTokensRoutes registers all /api/account-tokens routes.
func RegisterAccountTokensRoutes(r chi.Router, db *sqlx.DB) {
	RegisterAccountTokensRoutesWithConfig(r, db, nil)
}

// RegisterAccountTokensRoutesWithConfig is like RegisterAccountTokensRoutes but
// accepts optional runtime config for proxy-aware upstream calls.
func RegisterAccountTokensRoutesWithConfig(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &accountTokensHandler{db: db, cfg: cfg}

	r.Get("/api/account-tokens", handler.listTokens)
	r.Post("/api/account-tokens", handler.createToken)
	r.Post("/api/account-tokens/batch", handler.batchTokens)
	r.Put("/api/account-tokens/{id}", handler.updateToken)
	r.Post("/api/account-tokens/{id}/default", handler.setDefault)
	r.Get("/api/account-tokens/{id}/value", handler.getTokenValue)
	r.Delete("/api/account-tokens/{id}", handler.deleteToken)
	r.Post("/api/account-tokens/sync/{accountId}", handler.syncAccount)
	r.Post("/api/account-tokens/sync-all", handler.syncAll)
	r.Get("/api/account-tokens/groups/{accountId}", handler.getGroups)
	r.Get("/api/account-tokens/account/{accountId}/default", handler.getAccountDefault)
}

type accountTokensHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

// ---- List Tokens ----

func (h *accountTokensHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	accountIDStr := r.URL.Query().Get("accountId")
	var accountID *int64
	if accountIDStr != "" {
		if id, err := strconv.ParseInt(accountIDStr, 10, 64); err == nil {
			accountID = &id
		}
	}

	tokens, err := service.ListTokensWithRelations(h.db, accountID)
	if err != nil {
		slog.Error("Failed to load tokens", "err", err)
		writeError(w, http.StatusInternalServerError, "Failed to load tokens")
		return
	}

	if tokens == nil {
		tokens = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, tokens)
}

// ---- Create Token ----

func (h *accountTokensHandler) createToken(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountTokenCreatePayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid account token payload.")
		return
	}

	if body.AccountID <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid accountId. Expected positive number.")
		return
	}

	// Get account with site
	row, err := service.GetAccountWithSiteByID(h.db, int64(body.AccountID))
	if err != nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}

	if service.IsAPIKeyConnection(&row.Account) {
		writeError(w, http.StatusBadRequest, "API Key 连接不支持创建账号令牌")
		return
	}

	tokenValue := ""
	if body.Token != nil {
		tokenValue = strings.TrimSpace(*body.Token)
	}

	// Local path: token value provided
	if tokenValue != "" {
		result, err := h.createLocalToken(body, row, tokenValue)
		if err != nil {
			slog.Error("Token creation failed", "err", err)
			writeError(w, http.StatusInternalServerError, "Token creation failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Upstream path: create token on the target site
	if row.Site.Status == "disabled" || strings.TrimSpace(row.Site.Status) == "disabled" {
		writeError(w, http.StatusBadRequest, "站点已禁用，无法创建令牌")
		return
	}
	if strings.TrimSpace(row.Account.AccessToken) == "" {
		writeError(w, http.StatusBadRequest, "账号缺少访问令牌，无法创建站点令牌")
		return
	}

	options, optErr := buildCreateAPITokenOptions(body)
	if optErr != nil {
		writeError(w, http.StatusBadRequest, optErr.Error())
		return
	}

	adapter := platform.GetAdapter(row.Site.Platform)
	if adapter == nil {
		writeError(w, http.StatusBadGateway, "不支持的平台，无法创建站点令牌: "+row.Site.Platform)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	proxyCfg := service.BuildPlatformProxyConfig(h.cfg, &row.Account, &row.Site)
	platformUserID := service.ResolvePlatformUserIDPtr(&row.Account)

	created, createErr := adapter.CreateAPIToken(
		ctx,
		row.Site.URL,
		row.Account.AccessToken,
		platformUserID,
		options,
		proxyCfg,
	)
	if createErr != nil {
		slog.Warn("Upstream create API token failed", "err", createErr, "account_id", row.Account.ID, "platform", row.Site.Platform)
		writeError(w, http.StatusBadGateway, "站点创建令牌失败: "+createErr.Error())
		return
	}
	if !created {
		writeError(w, http.StatusBadGateway, "站点创建令牌失败（上游返回失败）")
		return
	}

	syncResult, syncErr := executeAccountTokenSync(ctx, h.db, h.cfg, row)
	if syncErr != nil {
		slog.Warn("Token sync after upstream create failed", "err", syncErr, "account_id", row.Account.ID)
		writeError(w, http.StatusBadGateway, "站点令牌已创建，但同步本地令牌失败: "+syncErr.Error())
		return
	}

	var token *store.AccountToken
	if syncResult != nil {
		if defaultID, ok := syncResult["defaultTokenId"].(int64); ok {
			token, _ = service.GetTokenByID(h.db, defaultID)
		}
	}
	if token == nil {
		token, _ = service.GetDefaultTokenForAccount(h.db, row.Account.ID)
	}
	if token == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"synced":  true,
			"created": syncResult["created"],
			"updated": syncResult["updated"],
			"total":   syncResult["total"],
			"message": "上游令牌已创建并完成同步",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   tokenToMap(*token, row.Site.Platform),
		"synced":  true,
		"created": syncResult["created"],
		"updated": syncResult["updated"],
		"total":   syncResult["total"],
	})
}

func (h *accountTokensHandler) createLocalToken(body payloads.AccountTokenCreatePayload, row *service.AccountWithSite, tokenValue string) (map[string]any, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	var existingTokens []store.AccountToken
	if err := h.db.Select(&existingTokens, h.db.Rebind("SELECT * FROM account_tokens WHERE account_id = ?"), body.AccountID); err != nil {
		return nil, fmt.Errorf("加载已有令牌失败: %w", err)
	}

	valueStatus := TokenValueStatusReady
	if IsMaskedTokenValue(tokenValue) {
		valueStatus = TokenValueStatusMaskedPending
	}
	enabled := valueStatus == TokenValueStatusReady
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	isDefault := false
	if valueStatus == TokenValueStatusReady && body.IsDefault != nil {
		isDefault = *body.IsDefault
	}

	name := ""
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	if name == "" {
		if len(existingTokens) == 0 {
			name = "default"
		} else {
			name = fmt.Sprintf("token-%d", len(existingTokens)+1)
		}
	}

	tokenGroup := ""
	if body.Group != nil {
		tokenGroup = strings.TrimSpace(*body.Group)
	}

	source := "manual"
	if body.Source != nil {
		source = strings.TrimSpace(*body.Source)
	}

	result, err := h.db.Exec(
		h.db.Rebind(`INSERT INTO account_tokens (account_id, name, token, token_group, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		body.AccountID, name, tokenValue, nullIfEmpty(tokenGroup), valueStatus, source, enabled, isDefault, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("创建令牌失败: %w", err)
	}

	tokenID, err := result.LastInsertId()
	if err != nil {
		// Fallback for Postgres which doesn't support LastInsertId.
		if err := h.db.Get(&tokenID, h.db.Rebind("SELECT id FROM account_tokens WHERE account_id = ? AND token = ? ORDER BY id DESC LIMIT 1"), body.AccountID, tokenValue); err != nil {
			return nil, fmt.Errorf("读取新令牌失败: %w", err)
		}
	}
	cleanupInsertedToken := func() {
		_, _ = h.db.Exec(h.db.Rebind("DELETE FROM account_tokens WHERE id = ?"), tokenID)
	}

	// Set as default if appropriate
	if valueStatus == TokenValueStatusReady && (isDefault || (len(existingTokens) == 0 && enabled)) {
		if ok, err := service.SetDefaultToken(h.db, tokenID); err != nil {
			cleanupInsertedToken()
			return nil, fmt.Errorf("设置默认令牌失败: %w", err)
		} else if !ok {
			cleanupInsertedToken()
			return nil, fmt.Errorf("设置默认令牌失败")
		}
	} else if valueStatus == TokenValueStatusReady && !hasDefaultToken(existingTokens) && enabled {
		if ok, err := service.SetDefaultToken(h.db, tokenID); err != nil {
			cleanupInsertedToken()
			return nil, fmt.Errorf("设置默认令牌失败: %w", err)
		} else if !ok {
			cleanupInsertedToken()
			return nil, fmt.Errorf("设置默认令牌失败")
		}
	}

	// Fetch the created token
	var token store.AccountToken
	if err := h.db.Get(&token, h.db.Rebind("SELECT * FROM account_tokens WHERE id = ?"), tokenID); err != nil {
		return nil, fmt.Errorf("读取新令牌失败: %w", err)
	}

	return map[string]any{
		"success": true,
		"token":   tokenToMap(token, row.Site.Platform),
	}, nil
}

// ---- Batch Tokens ----

func (h *accountTokensHandler) batchTokens(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountTokenBatchPayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid account token batch payload.")
		return
	}

	if len(body.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}

	action := strings.TrimSpace(body.Action)
	validActions := map[string]bool{"enable": true, "disable": true, "delete": true}
	if !validActions[action] {
		writeError(w, http.StatusBadRequest, "Invalid action")
		return
	}

	var successIDs []int64
	var failedItems []map[string]any

	for _, rawID := range body.IDs {
		id := int64(rawID)

		existing, err := service.GetTokenByID(h.db, id)
		if err != nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "Token not found"})
			continue
		}

		owner, ownerErr := service.GetAccountByID(h.db, existing.AccountID)
		if ownerErr != nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "Account not found"})
			continue
		}
		if owner == nil {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "Account not found"})
			continue
		}
		if service.IsAPIKeyConnection(owner) {
			failedItems = append(failedItems, map[string]any{"id": id, "message": "API Key 连接不支持管理账号令牌"})
			continue
		}

		if action == "delete" {
			if err := h.deleteTokenWithUpstream(r.Context(), existing); err != nil {
				slog.Error("Token deletion failed", "err", err, "token_id", id)
				failedItems = append(failedItems, map[string]any{"id": id, "message": err.Error()})
				continue
			}
		} else {
			if service.IsMaskedPendingAccountToken(existing) {
				failedItems = append(failedItems, map[string]any{"id": id, "message": "待补全令牌不能修改启用状态，请先补全明文 token"})
				continue
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if _, err := h.db.Exec(h.db.Rebind("UPDATE account_tokens SET enabled = ?, updated_at = ? WHERE id = ?"), action == "enable", now, id); err != nil {
				slog.Error("Token status update failed", "err", err, "token_id", id)
				failedItems = append(failedItems, map[string]any{"id": id, "message": "Token status update failed"})
				continue
			}

			if existing.IsDefault && action == "disable" {
				if _, err := service.RepairDefaultToken(h.db, existing.AccountID); err != nil {
					slog.Error("Default token repair failed", "err", err, "account_id", existing.AccountID)
					failedItems = append(failedItems, map[string]any{"id": id, "message": "Default token repair failed"})
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

// ---- Update Token ----

func (h *accountTokensHandler) updateToken(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	tokenID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "令牌 ID 无效")
		return
	}

	var body payloads.AccountTokenUpdatePayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid account token payload.")
		return
	}

	existing, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}

	owner, err := service.GetAccountByID(h.db, existing.AccountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}
	if owner == nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeError(w, http.StatusBadRequest, "API Key 连接不支持管理账号令牌")
		return
	}

	updates := map[string]any{}

	nextValueStatus := service.ResolveAccountTokenValueStatus(existing)

	if body.Name != nil {
		updates["name"] = strings.TrimSpace(*body.Name)
	}
	if body.Token != nil {
		tv := strings.TrimSpace(*body.Token)
		if tv == "" {
			writeError(w, http.StatusBadRequest, "令牌不能为空")
			return
		}
		updates["token"] = tv
		if IsMaskedTokenValue(tv) {
			nextValueStatus = TokenValueStatusMaskedPending
		} else {
			nextValueStatus = TokenValueStatusReady
		}
		updates["valueStatus"] = nextValueStatus
	}
	if body.Group != nil {
		updates["tokenGroup"] = nullIfEmpty(strings.TrimSpace(*body.Group))
	}
	if body.Source != nil {
		updates["source"] = strings.TrimSpace(*body.Source)
	}

	if nextValueStatus == TokenValueStatusMaskedPending {
		updates["enabled"] = false
		updates["isDefault"] = false
	} else {
		if body.Enabled != nil {
			updates["enabled"] = *body.Enabled
		}
		if body.IsDefault != nil {
			updates["isDefault"] = *body.IsDefault
		}
	}

	if err := service.UpdateTokenFields(h.db, tokenID, updates); err != nil {
		writeError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	// Refresh and handle default logic
	latest, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	if latest == nil {
		writeError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	if body.IsDefault != nil && *body.IsDefault && service.IsUsableAccountToken(latest) {
		if ok, err := service.SetDefaultToken(h.db, tokenID); err != nil {
			writeError(w, http.StatusInternalServerError, "设置默认令牌失败")
			return
		} else if !ok {
			writeError(w, http.StatusBadRequest, "令牌不能设为默认")
			return
		}
	} else if latest.IsDefault && service.IsUsableAccountToken(latest) {
		if ok, err := service.SetDefaultToken(h.db, tokenID); err != nil {
			writeError(w, http.StatusInternalServerError, "设置默认令牌失败")
			return
		} else if !ok {
			writeError(w, http.StatusBadRequest, "令牌不能设为默认")
			return
		}
	} else if existing.IsDefault && !service.IsUsableAccountToken(latest) {
		if _, err := service.RepairDefaultToken(h.db, existing.AccountID); err != nil {
			writeError(w, http.StatusInternalServerError, "修复默认令牌失败")
			return
		}
	} else if body.IsDefault != nil && !(*body.IsDefault) && existing.IsDefault {
		if _, err := service.RepairDefaultToken(h.db, existing.AccountID); err != nil {
			writeError(w, http.StatusInternalServerError, "修复默认令牌失败")
			return
		}
	}

	latest, err = service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取令牌失败")
		return
	}
	// Update response is a list/get-style surface: return masked token only (#367).
	var site store.Site
	var ownerAcct store.Account
	if err := h.db.Get(&ownerAcct, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), latest.AccountID); err == nil {
		_ = h.db.Get(&site, h.db.Rebind("SELECT "+service.SiteSelectColumns+" FROM sites WHERE id = ?"), ownerAcct.SiteID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   tokenToMap(*latest, site.Platform),
	})
}

// ---- Set Default ----

func (h *accountTokensHandler) setDefault(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	tokenID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "令牌 ID 无效")
		return
	}

	token, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}

	owner, err := service.GetAccountByID(h.db, token.AccountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}
	if owner == nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeError(w, http.StatusBadRequest, "API Key 连接不支持管理账号令牌")
		return
	}
	if service.IsMaskedPendingAccountToken(token) {
		writeError(w, http.StatusBadRequest, "待补全令牌不能设为默认，请先补全明文 token")
		return
	}

	ok, err := service.SetDefaultToken(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "设置默认令牌失败")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Get Token Value ----

func (h *accountTokensHandler) getTokenValue(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	tokenID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "令牌 ID 无效")
		return
	}

	token, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}

	owner, err := service.GetAccountByID(h.db, token.AccountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	if owner == nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeError(w, http.StatusBadRequest, "API Key 连接不支持管理账号令牌")
		return
	}

	if service.IsMaskedPendingAccountToken(token) || IsMaskedTokenValue(token.Token) {
		writeError(w, http.StatusConflict, "当前仅保存了脱敏令牌，无法展开/复制。请在站点重新生成并同步，或手动更新为完整令牌。")
		return
	}

	// Get site platform
	var site store.Site
	var account store.Account
	if err := h.db.Get(&account, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), token.AccountID); err != nil {
		writeError(w, http.StatusInternalServerError, "读取账号失败")
		return
	}
	if err := h.db.Get(&site, h.db.Rebind("SELECT "+service.SiteSelectColumns+" FROM sites WHERE id = ?"), account.SiteID); err != nil {
		writeError(w, http.StatusInternalServerError, "读取站点失败")
		return
	}

	// Intentional export-secret path: full token returned only on explicit /value
	// fetch after admin auth. List/create/update use tokenMasked instead (#367).
	displayToken := service.NormalizeTokenForDisplay(token.Token, site.Platform)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"id":          token.ID,
		"name":        token.Name,
		"token":       displayToken,
		"tokenMasked": service.MaskToken(token.Token, site.Platform),
	})
}

// ---- Delete Token ----

func (h *accountTokensHandler) deleteToken(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	tokenID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "令牌 ID 无效")
		return
	}

	token, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}

	owner, err := service.GetAccountByID(h.db, token.AccountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	if owner == nil {
		writeError(w, http.StatusNotFound, "令牌不存在")
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeError(w, http.StatusBadRequest, "API Key 连接不支持管理账号令牌")
		return
	}

	if err := h.deleteTokenWithUpstream(r.Context(), token); err != nil {
		slog.Error("Token deletion failed", "err", err, "token_id", tokenID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *accountTokensHandler) deleteTokenWithUpstream(ctx context.Context, token *store.AccountToken) error {
	if token == nil {
		return fmt.Errorf("令牌不存在")
	}

	// Residual: masked_pending tokens only store redacted key material, so remote
	// delete cannot match a real upstream key and is intentionally skipped.
	shouldSkipUpstream := service.IsMaskedPendingAccountToken(token) || IsMaskedTokenValue(token.Token)

	row, err := service.GetAccountWithSiteByID(h.db, token.AccountID)
	if err != nil {
		return service.DeleteTokenByID(h.db, token.ID)
	}

	if !shouldSkipUpstream {
		siteDisabled := strings.EqualFold(strings.TrimSpace(row.Site.Status), "disabled")
		accessToken := strings.TrimSpace(row.Account.AccessToken)
		adapter := platform.GetAdapter(row.Site.Platform)

		if !siteDisabled && accessToken != "" && adapter != nil {
			callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			proxyCfg := service.BuildPlatformProxyConfig(h.cfg, &row.Account, &row.Site)
			platformUserID := service.ResolvePlatformUserIDPtr(&row.Account)
			if delErr := adapter.DeleteAPIToken(callCtx, row.Site.URL, accessToken, token.Token, platformUserID, proxyCfg); delErr != nil {
				return fmt.Errorf("上游删除令牌失败: %w", delErr)
			}
		}
	}

	return service.DeleteTokenByID(h.db, token.ID)
}

// ---- Sync Account ----

func (h *accountTokensHandler) syncAccount(w http.ResponseWriter, r *http.Request) {
	accountIDStr := chi.URLParam(r, "accountId")
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "账号 ID 无效")
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, syncErr := executeAccountTokenSync(ctx, h.db, h.cfg, row)
	if syncErr != nil {
		writeError(w, http.StatusBadGateway, syncErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ---- Sync All ----

func (h *accountTokensHandler) syncAll(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountTokenSyncAllPayload
	if err := decodeJSONRequest(r, &body); err != nil {
		// Empty body is allowed for background mode.
		body = payloads.AccountTokenSyncAllPayload{}
	}

	wait := body.Wait != nil && *body.Wait
	db := h.db
	cfg := h.cfg

	if wait {
		summary, results := executeSyncAllAccountTokens(context.Background(), db, cfg)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"summary": summary,
			"results": results,
		})
		return
	}

	task, reused := StartBackgroundTask(BackgroundTaskStartOptions{
		Type:      "sync-all-account-tokens",
		Title:     "同步全部账号令牌",
		DedupeKey: "sync-all-account-tokens",
	}, func() (any, error) {
		summary, results := executeSyncAllAccountTokens(context.Background(), db, cfg)
		return map[string]any{
			"summary": summary,
			"results": results,
		}, nil
	})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  reused,
		"jobId":   task.ID,
		"taskId":  task.ID,
		"status":  string(task.Status),
		"message": "已开始全部账号令牌同步，请稍后查看程序日志",
	})
}

// ---- Get Groups ----

func (h *accountTokensHandler) getGroups(w http.ResponseWriter, r *http.Request) {
	accountIDStr := chi.URLParam(r, "accountId")
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "账号 ID 无效")
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "账号不存在")
		return
	}

	if service.IsAPIKeyConnection(&row.Account) {
		writeError(w, http.StatusBadRequest, "API Key 连接不支持拉取账号令牌分组")
		return
	}

	accessToken := strings.TrimSpace(row.Account.AccessToken)
	adapter := platform.GetAdapter(row.Site.Platform)
	proxyCfg := service.BuildPlatformProxyConfig(h.cfg, &row.Account, &row.Site)
	platformUserID := service.ResolvePlatformUserIDPtr(&row.Account)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	groups, err := service.GetTokenGroups(
		ctx,
		h.db,
		accountID,
		adapter,
		row.Site.URL,
		accessToken,
		platformUserID,
		proxyCfg,
	)
	if err != nil {
		slog.Error("Failed to load token groups", "err", err, "account_id", accountID)
		writeError(w, http.StatusInternalServerError, "Failed to load token groups")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"groups":  groups,
	})
}

// ---- Get Account Default Token ----

func (h *accountTokensHandler) getAccountDefault(w http.ResponseWriter, r *http.Request) {
	accountIDStr := chi.URLParam(r, "accountId")
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "账号 ID 无效")
		return
	}

	var account store.Account
	if err := h.db.Get(&account, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), accountID); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "token": nil})
		return
	}

	if service.IsAPIKeyConnection(&account) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "token": nil})
		return
	}

	token, err := service.GetDefaultTokenForAccount(h.db, accountID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "token": nil})
		return
	}

	var site store.Site
	if err := h.db.Get(&site, h.db.Rebind("SELECT "+service.SiteSelectColumns+" FROM sites WHERE id = ?"), account.SiteID); err != nil {
		writeError(w, http.StatusInternalServerError, "读取站点失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   tokenToMapMasked(*token, site.Platform),
	})
}

// ---- Upstream helpers ----

func buildCreateAPITokenOptions(body payloads.AccountTokenCreatePayload) (*platform.CreateAPITokenOptions, error) {
	unlimitedQuota := true
	if body.UnlimitedQuota != nil {
		unlimitedQuota = *body.UnlimitedQuota
	}

	var remainQuota float64
	if body.RemainQuota != nil {
		parsed, ok := parseFlexibleFloat(body.RemainQuota)
		if !ok {
			return nil, fmt.Errorf("Invalid remainQuota. Expected number.")
		}
		remainQuota = parsed
	} else if !unlimitedQuota {
		return nil, fmt.Errorf("remainQuota is required when unlimitedQuota is false")
	}

	expiredTime := int64(-1)
	if body.ExpiredTime != nil {
		parsed, ok := parseFlexibleEpochSeconds(body.ExpiredTime)
		if !ok {
			return nil, fmt.Errorf("Invalid expiredTime. Expected unix seconds or ISO date string.")
		}
		expiredTime = parsed
	}

	name := ""
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	group := ""
	if body.Group != nil {
		group = strings.TrimSpace(*body.Group)
	}
	allowIPs := ""
	if body.AllowIPs != nil {
		allowIPs = strings.TrimSpace(*body.AllowIPs)
	}
	modelLimits := ""
	if body.ModelLimits != nil {
		modelLimits = strings.TrimSpace(*body.ModelLimits)
	}
	modelLimitsEnabled := false
	if body.ModelLimitsEnabled != nil {
		modelLimitsEnabled = *body.ModelLimitsEnabled
	}

	return &platform.CreateAPITokenOptions{
		Name:               name,
		Group:              group,
		UnlimitedQuota:     unlimitedQuota,
		RemainQuota:        remainQuota,
		ExpiredTime:        expiredTime,
		AllowIPs:           allowIPs,
		ModelLimitsEnabled: modelLimitsEnabled,
		ModelLimits:        modelLimits,
	}, nil
}

func parseFlexibleFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseFloat(s, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func parseFlexibleEpochSeconds(v any) (int64, bool) {
	switch val := v.(type) {
	case float64:
		return int64(val), true
	case float32:
		return int64(val), true
	case int:
		return int64(val), true
	case int64:
		return val, true
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return 0, false
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Unix(), true
		}
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t.Unix(), true
		}
		return 0, false
	default:
		if n, ok := parseFlexibleFloat(v); ok {
			return int64(n), true
		}
		return 0, false
	}
}

func executeAccountTokenSync(ctx context.Context, db *sqlx.DB, cfg *config.Config, row *service.AccountWithSite) (map[string]any, error) {
	if row == nil {
		return nil, fmt.Errorf("账号不存在")
	}
	accountID := row.Account.ID

	base := map[string]any{
		"success":   true,
		"accountId": accountID,
		"synced":    false,
		"created":   0,
		"updated":   0,
		"total":     0,
	}

	if strings.EqualFold(strings.TrimSpace(row.Site.Status), "disabled") {
		base["status"] = "skipped"
		base["reason"] = "site_disabled"
		base["message"] = "站点已禁用，跳过令牌同步"
		return base, nil
	}
	if service.IsAPIKeyConnection(&row.Account) {
		base["status"] = "skipped"
		base["reason"] = "apikey_connection"
		base["message"] = "API Key 连接不支持同步账号令牌"
		return base, nil
	}

	accessToken := strings.TrimSpace(row.Account.AccessToken)
	if accessToken == "" {
		if row.Account.APIToken != nil && strings.TrimSpace(*row.Account.APIToken) != "" {
			if _, err := service.EnsureDefaultTokenForAccount(
				db,
				accountID,
				strings.TrimSpace(*row.Account.APIToken),
				"default",
				"legacy",
				"default",
				true,
			); err != nil {
				return nil, err
			}
			base["status"] = "skipped"
			base["reason"] = "no_access_token"
			base["message"] = "账号缺少访问令牌，已保留本地默认令牌"
			return base, nil
		}
		base["status"] = "skipped"
		base["reason"] = "no_access_token"
		base["message"] = "账号缺少访问令牌，无法同步站点令牌"
		return base, nil
	}

	adapter := platform.GetAdapter(row.Site.Platform)
	if adapter == nil {
		base["status"] = "skipped"
		base["reason"] = "unsupported_platform"
		base["message"] = "不支持的平台，无法同步站点令牌: " + row.Site.Platform
		return base, nil
	}

	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if _, hasDeadline := callCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, 15*time.Second)
		defer cancel()
	}

	proxyCfg := service.BuildPlatformProxyConfig(cfg, &row.Account, &row.Site)
	platformUserID := service.ResolvePlatformUserIDPtr(&row.Account)

	upstreamTokens, err := service.FetchUpstreamAPITokens(
		callCtx,
		adapter,
		row.Site.URL,
		accessToken,
		platformUserID,
		proxyCfg,
	)
	if err != nil {
		_ = service.CreateEvent(db, "token_sync", "账号令牌同步失败", err.Error(), "error", accountID, "account")
		return nil, fmt.Errorf("同步上游令牌失败: %w", err)
	}
	if len(upstreamTokens) == 0 {
		base["status"] = "skipped"
		base["reason"] = "no_upstream_tokens"
		base["message"] = "upstream returned no api tokens"
		return base, nil
	}

	syncResult, syncErr := service.SyncTokensFromUpstream(db, accountID, upstreamTokens)
	if syncErr != nil {
		_ = service.CreateEvent(db, "token_sync", "账号令牌同步失败", syncErr.Error(), "error", accountID, "account")
		return nil, syncErr
	}

	base["status"] = "synced"
	base["synced"] = true
	base["created"] = syncResult.Created
	base["updated"] = syncResult.Updated
	base["total"] = syncResult.Total
	base["maskedPending"] = syncResult.MaskedPending
	if syncResult.DefaultTokenID != nil {
		base["defaultTokenId"] = *syncResult.DefaultTokenID
	}
	base["message"] = fmt.Sprintf("同步完成：新增 %d，更新 %d，合计 %d", syncResult.Created, syncResult.Updated, syncResult.Total)

	_ = service.CreateEvent(
		db,
		"token_sync",
		"账号令牌同步完成",
		base["message"].(string),
		"info",
		accountID,
		"account",
	)
	return base, nil
}

func executeSyncAllAccountTokens(ctx context.Context, db *sqlx.DB, cfg *config.Config) (map[string]int, []map[string]any) {
	type accountIDRow struct {
		ID int64 `db:"id"`
	}
	var ids []accountIDRow
	if err := db.Select(&ids, `SELECT id FROM accounts WHERE status = 'active' ORDER BY id`); err != nil {
		slog.Error("sync-all account tokens: list accounts failed", "err", err)
		return map[string]int{
			"total": 0, "synced": 0, "skipped": 0, "failed": 0, "created": 0, "updated": 0,
		}, []map[string]any{}
	}

	summary := map[string]int{
		"total": len(ids), "synced": 0, "skipped": 0, "failed": 0, "created": 0, "updated": 0,
	}
	results := make([]map[string]any, 0, len(ids))

	const batchSize = 3
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		type batchItem struct {
			result map[string]any
		}
		out := make([]batchItem, len(batch))
		var wg sync.WaitGroup
		for idx, item := range batch {
			wg.Add(1)
			go func(idx int, accountID int64) {
				defer wg.Done()
				row, err := service.GetAccountWithSiteByID(db, accountID)
				if err != nil {
					out[idx] = batchItem{result: map[string]any{
						"success":   false,
						"accountId": accountID,
						"status":    "failed",
						"reason":    "account_not_found",
						"message":   "账号不存在",
						"synced":    false,
						"created":   0,
						"updated":   0,
						"total":     0,
					}}
					return
				}
				result, syncErr := executeAccountTokenSync(ctx, db, cfg, row)
				if syncErr != nil {
					out[idx] = batchItem{result: map[string]any{
						"success":   false,
						"accountId": accountID,
						"status":    "failed",
						"reason":    "sync_failed",
						"message":   syncErr.Error(),
						"synced":    false,
						"created":   0,
						"updated":   0,
						"total":     0,
					}}
					return
				}
				out[idx] = batchItem{result: result}
			}(idx, item.ID)
		}
		wg.Wait()

		for _, item := range out {
			result := item.result
			results = append(results, result)
			status, _ := result["status"].(string)
			switch status {
			case "synced":
				summary["synced"]++
			case "skipped":
				summary["skipped"]++
			default:
				summary["failed"]++
			}
			if created, ok := asInt(result["created"]); ok {
				summary["created"] += created
			}
			if updated, ok := asInt(result["updated"]); ok {
				summary["updated"] += updated
			}
		}
	}

	return summary, results
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// ---- Helper functions ----

// TokenValueStatusReady and MaskedPending
const (
	TokenValueStatusReady         = "ready"
	TokenValueStatusMaskedPending = "masked_pending"
)

// IsMaskedTokenValue checks if a token value contains masking characters.
func IsMaskedTokenValue(token string) bool {
	value := strings.TrimSpace(token)
	return value != "" && (strings.Contains(value, "*") || strings.Contains(value, "•"))
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func hasDefaultToken(tokens []store.AccountToken) bool {
	for _, t := range tokens {
		if t.IsDefault {
			return true
		}
	}
	return false
}

func tokenToMap(token store.AccountToken, platform string) map[string]any {
	vs := service.ResolveAccountTokenValueStatus(&token)
	return map[string]any{
		"id":          token.ID,
		"accountId":   token.AccountID,
		"name":        token.Name,
		"tokenMasked": service.MaskToken(token.Token, platform),
		"tokenGroup":  token.TokenGroup,
		"valueStatus": vs,
		"source":      token.Source,
		"enabled":     token.Enabled,
		"isDefault":   token.IsDefault,
		"createdAt":   token.CreatedAt,
		"updatedAt":   token.UpdatedAt,
	}
}

func tokenToMapMasked(token store.AccountToken, platform string) map[string]any {
	return map[string]any{
		"id":          token.ID,
		"accountId":   token.AccountID,
		"name":        token.Name,
		"tokenGroup":  token.TokenGroup,
		"valueStatus": service.ResolveAccountTokenValueStatus(&token),
		"source":      token.Source,
		"enabled":     token.Enabled,
		"isDefault":   token.IsDefault,
		"tokenMasked": service.MaskToken(token.Token, platform),
		"createdAt":   token.CreatedAt,
		"updatedAt":   token.UpdatedAt,
	}
}
