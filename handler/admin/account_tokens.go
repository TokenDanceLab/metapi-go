package admin

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/handler/admin/payloads"
	"github.com/tokendancelab/metapi-go/service"
	"github.com/tokendancelab/metapi-go/store"
)

// RegisterAccountTokensRoutes registers all /api/account-tokens routes.
func RegisterAccountTokensRoutes(r chi.Router, db *sqlx.DB) {
	handler := &accountTokensHandler{db: db}

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
	db *sqlx.DB
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to load tokens"})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid account token payload."})
		return
	}

	if body.AccountID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid accountId. Expected positive number."})
		return
	}

	// Get account with site
	row, err := service.GetAccountWithSiteByID(h.db, int64(body.AccountID))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}

	if service.IsAPIKeyConnection(&row.Account) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "API Key 连接不支持创建账号令牌"})
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
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Token creation failed"})
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Upstream path: create token on the target site
	if row.Site.Status == "disabled" || strings.TrimSpace(row.Site.Status) == "disabled" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "站点已禁用，无法创建令牌"})
		return
	}
	if strings.TrimSpace(row.Account.AccessToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "账号缺少访问令牌，无法创建站点令牌"})
		return
	}

	// Stub: P4 adapter.createApiToken()
	writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "message": "站点创建令牌失败（上游适配器未实现）"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid account token batch payload."})
		return
	}

	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return
	}

	action := strings.TrimSpace(body.Action)
	validActions := map[string]bool{"enable": true, "disable": true, "delete": true}
	if !validActions[action] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid action"})
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
			// Upstream-first delete stub: just do local delete
			if err := service.DeleteTokenByID(h.db, id); err != nil {
				slog.Error("Token deletion failed", "err", err, "token_id", id)
				failedItems = append(failedItems, map[string]any{"id": id, "message": "Token deletion failed"})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "令牌 ID 无效"})
		return
	}

	var body payloads.AccountTokenUpdatePayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid account token payload."})
		return
	}

	existing, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}

	owner, err := service.GetAccountByID(h.db, existing.AccountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}
	if owner == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "API Key 连接不支持管理账号令牌"})
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
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "令牌不能为空"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "更新失败"})
		return
	}

	// Refresh and handle default logic
	latest, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "更新失败"})
		return
	}
	if latest == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "更新失败"})
		return
	}

	if body.IsDefault != nil && *body.IsDefault && service.IsUsableAccountToken(latest) {
		if ok, err := service.SetDefaultToken(h.db, tokenID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "设置默认令牌失败"})
			return
		} else if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "令牌不能设为默认"})
			return
		}
	} else if latest.IsDefault && service.IsUsableAccountToken(latest) {
		if ok, err := service.SetDefaultToken(h.db, tokenID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "设置默认令牌失败"})
			return
		} else if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "令牌不能设为默认"})
			return
		}
	} else if existing.IsDefault && !service.IsUsableAccountToken(latest) {
		if _, err := service.RepairDefaultToken(h.db, existing.AccountID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "修复默认令牌失败"})
			return
		}
	} else if body.IsDefault != nil && !(*body.IsDefault) && existing.IsDefault {
		if _, err := service.RepairDefaultToken(h.db, existing.AccountID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "修复默认令牌失败"})
			return
		}
	}

	latest, err = service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "读取令牌失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   latest,
	})
}

// ---- Set Default ----

func (h *accountTokensHandler) setDefault(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	tokenID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "令牌 ID 无效"})
		return
	}

	token, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}

	owner, err := service.GetAccountByID(h.db, token.AccountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}
	if owner == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "API Key 连接不支持管理账号令牌"})
		return
	}
	if service.IsMaskedPendingAccountToken(token) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "待补全令牌不能设为默认，请先补全明文 token"})
		return
	}

	ok, err := service.SetDefaultToken(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "设置默认令牌失败"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Get Token Value ----

func (h *accountTokensHandler) getTokenValue(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	tokenID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "令牌 ID 无效"})
		return
	}

	token, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}

	owner, err := service.GetAccountByID(h.db, token.AccountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}
	if owner == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "API Key 连接不支持管理账号令牌"})
		return
	}

	if service.IsMaskedPendingAccountToken(token) || IsMaskedTokenValue(token.Token) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"success": false,
			"message": "当前仅保存了脱敏令牌，无法展开/复制。请在站点重新生成并同步，或手动更新为完整令牌。",
		})
		return
	}

	// Get site platform
	var site store.Site
	var account store.Account
	if err := h.db.Get(&account, h.db.Rebind("SELECT * FROM accounts WHERE id = ?"), token.AccountID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "读取账号失败"})
		return
	}
	if err := h.db.Get(&site, h.db.Rebind("SELECT * FROM sites WHERE id = ?"), account.SiteID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "读取站点失败"})
		return
	}

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
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "令牌 ID 无效"})
		return
	}

	token, err := service.GetTokenByID(h.db, tokenID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}

	owner, err := service.GetAccountByID(h.db, token.AccountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}
	if owner == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "令牌不存在"})
		return
	}
	if service.IsAPIKeyConnection(owner) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "API Key 连接不支持管理账号令牌"})
		return
	}

	// Stub: upstream-first delete - just local delete for now
	if err := service.DeleteTokenByID(h.db, tokenID); err != nil {
		slog.Error("Token deletion failed", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "message": "Token deletion failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- Sync Account ----

func (h *accountTokensHandler) syncAccount(w http.ResponseWriter, r *http.Request) {
	accountIDStr := chi.URLParam(r, "accountId")
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "账号 ID 无效"})
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, accountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}

	// Stub: P4 adapter sync
	_ = row
	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"accountId": accountID,
		"status":    "skipped",
		"reason":    "no_upstream_tokens",
		"message":   "upstream returned no api tokens (sync stub)",
		"synced":    false,
		"created":   0,
		"updated":   0,
		"total":     0,
	})
}

// ---- Sync All ----

func (h *accountTokensHandler) syncAll(w http.ResponseWriter, r *http.Request) {
	var body payloads.AccountTokenSyncAllPayload
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	wait := body.Wait != nil && *body.Wait

	if wait {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"summary": map[string]int{
				"total": 0, "synced": 0, "skipped": 0, "failed": 0,
				"created": 0, "updated": 0,
			},
			"results": []any{},
		})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"success": true,
		"queued":  true,
		"reused":  false,
		"jobId":   "stub-sync-all",
		"status":  "pending",
		"message": "已开始全部账号令牌同步，请稍后查看程序日志",
	})
}

// ---- Get Groups ----

func (h *accountTokensHandler) getGroups(w http.ResponseWriter, r *http.Request) {
	accountIDStr := chi.URLParam(r, "accountId")
	accountID, err := strconv.ParseInt(accountIDStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "账号 ID 无效"})
		return
	}

	row, err := service.GetAccountWithSiteByID(h.db, accountID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "message": "账号不存在"})
		return
	}

	if service.IsAPIKeyConnection(&row.Account) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "API Key 连接不支持拉取账号令牌分组"})
		return
	}

	groups, err := service.GetTokenGroups(h.db, accountID)
	if err != nil {
		slog.Error("Failed to load token groups", "err", err, "account_id", accountID)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to load token groups"})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "账号 ID 无效"})
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
	if err := h.db.Get(&site, h.db.Rebind("SELECT * FROM sites WHERE id = ?"), account.SiteID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "读取站点失败"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   tokenToMapMasked(*token, site.Platform),
	})
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
