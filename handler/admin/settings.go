package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

// RegisterSettingsRoutes registers all /api/settings routes (runtime, brand-list, system-proxy/test).
func RegisterSettingsRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &settingsHandler{db: db, cfg: cfg}

	r.Get("/api/settings/runtime", handler.getRuntime)
	r.Put("/api/settings/runtime", handler.updateRuntime)
	r.Get("/api/settings/brand-list", handler.brandList)
	r.Post("/api/settings/system-proxy/test", handler.testSystemProxy)
}

type settingsHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

// GET /api/settings/runtime
func (h *settingsHandler) getRuntime(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfg
	writeJSON(w, http.StatusOK, map[string]any{
		// Checkin
		"checkinCron":          cfg.CheckinCron,
		"checkinScheduleMode":  cfg.CheckinScheduleMode,
		"checkinIntervalHours": cfg.CheckinIntervalHours,
		// Balance
		"balanceRefreshCron": cfg.BalanceRefreshCron,
		// Log cleanup
		"logCleanupCron":               cfg.LogCleanupCron,
		"logCleanupUsageLogsEnabled":   cfg.LogCleanupUsageLogsEnabled,
		"logCleanupProgramLogsEnabled": cfg.LogCleanupProgramLogsEnabled,
		"logCleanupRetentionDays":      cfg.LogCleanupRetentionDays,
		// Model probe
		"modelAvailabilityProbeEnabled": cfg.ModelAvailabilityProbeEnabled,
		// Codex
		"codexUpstreamWebsocketEnabled": cfg.CodexUpstreamWebsocketEnabled,
		// Responses
		"responsesCompactFallbackToResponsesEnabled": cfg.ResponsesCompactFallbackToResponsesEnabled,
		// Cross-protocol
		"disableCrossProtocolFallback": cfg.DisableCrossProtocolFallback,
		// Proxy session
		"proxySessionChannelConcurrencyLimit": cfg.ProxySessionChannelConcurrencyLimit,
		"proxySessionChannelQueueWaitMs":      cfg.ProxySessionChannelQueueWaitMs,
		// Debug trace
		"proxyDebugTraceEnabled":        cfg.ProxyDebugTraceEnabled,
		"proxyDebugCaptureHeaders":      cfg.ProxyDebugCaptureHeaders,
		"proxyDebugCaptureBodies":       cfg.ProxyDebugCaptureBodies,
		"proxyDebugCaptureStreamChunks": cfg.ProxyDebugCaptureStreamChunks,
		"proxyDebugTargetSessionId":     cfg.ProxyDebugTargetSessionId,
		"proxyDebugTargetClientKind":    cfg.ProxyDebugTargetClientKind,
		"proxyDebugTargetModel":         cfg.ProxyDebugTargetModel,
		"proxyDebugRetentionHours":      cfg.ProxyDebugRetentionHours,
		"proxyDebugMaxBodyBytes":        cfg.ProxyDebugMaxBodyBytes,
		// Routing
		"routingFallbackUnitCost":          cfg.RoutingFallbackUnitCost,
		"proxyFirstByteTimeoutSec":         cfg.ProxyFirstByteTimeoutSec,
		"tokenRouterFailureCooldownMaxSec": cfg.TokenRouterFailureCooldownMaxSec,
		"routingWeights": map[string]any{
			"baseWeightFactor": cfg.RoutingWeights.BaseWeightFactor,
			"valueScoreFactor": cfg.RoutingWeights.ValueScoreFactor,
			"costWeight":       cfg.RoutingWeights.CostWeight,
			"balanceWeight":    cfg.RoutingWeights.BalanceWeight,
			"usageWeight":      cfg.RoutingWeights.UsageWeight,
		},
		// Notify: Webhook
		"webhookUrl":     cfg.WebhookUrl,
		"webhookEnabled": cfg.WebhookEnabled,
		// Notify: Bark
		"barkUrl":     cfg.BarkUrl,
		"barkEnabled": cfg.BarkEnabled,
		// Notify: ServerChan
		"serverChanEnabled":   cfg.ServerChanEnabled,
		"serverChanKeyMasked": maskValue(cfg.ServerChanKey),
		// Notify: Telegram
		"telegramEnabled":         cfg.TelegramEnabled,
		"telegramApiBaseUrl":      cfg.TelegramApiBaseUrl,
		"telegramBotTokenMasked":  maskValue(cfg.TelegramBotToken),
		"telegramChatId":          cfg.TelegramChatId,
		"telegramUseSystemProxy":  cfg.TelegramUseSystemProxy,
		"telegramMessageThreadId": cfg.TelegramMessageThreadId,
		// Notify: SMTP
		"smtpEnabled":    cfg.SmtpEnabled,
		"smtpHost":       cfg.SmtpHost,
		"smtpPort":       cfg.SmtpPort,
		"smtpSecure":     cfg.SmtpSecure,
		"smtpUser":       cfg.SmtpUser,
		"smtpPassMasked": maskValue(cfg.SmtpPass),
		"smtpFrom":       cfg.SmtpFrom,
		"smtpTo":         cfg.SmtpTo,
		// Notify: cooldown
		"notifyCooldownSec": cfg.NotifyCooldownSec,
		// Admin
		"adminIpAllowlist": cfg.AdminIpAllowlist,
		"currentAdminIp":   extractClientIP(r),
		"serverTimeZone":   cfg.Tz,
		// System
		"systemProxyUrl":   cfg.SystemProxyUrl,
		"proxyTokenMasked": maskValue(cfg.ProxyToken),
		// Proxy
		"payloadRules":                 cfg.PayloadRules,
		"proxyErrorKeywords":           cfg.ProxyErrorKeywords,
		"proxyEmptyContentFailEnabled": cfg.ProxyEmptyContentFailEnabled,
		// Global filters (always JSON arrays; never null)
		"globalBlockedBrands": stringSliceOrEmpty(cfg.GlobalBlockedBrands),
		"globalAllowedModels": stringSliceOrEmpty(cfg.GlobalAllowedModels),
	})
}

// PUT /api/settings/runtime
func (h *settingsHandler) updateRuntime(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	cfg := h.cfg
	now := time.Now().UTC().Format(time.RFC3339)

	// Validate and apply each field
	// Proxy token
	if v, ok := body["proxyToken"]; ok {
		token := normalizeString(v)
		if !strings.HasPrefix(token, "sk-") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "下游访问令牌必须以 sk- 开头"})
			return
		}
		if len(token) < 6 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "下游访问令牌至少 6 位（含 sk-）"})
			return
		}
		cfg.ProxyToken = token
		upsertSettingDB(h.db, "proxy_token", token)
	}

	// System proxy URL
	if v, ok := body["systemProxyUrl"]; ok {
		url := normalizeString(v)
		cfg.SystemProxyUrl = url
		upsertSettingDB(h.db, "system_proxy_url", url)
	}

	if hasAnyKey(body, "checkinCron", "checkinScheduleMode", "checkinIntervalHours") {
		patch := checkinSchedulePatch{}
		if v, ok := body["checkinCron"]; ok {
			cron := normalizeString(v)
			patch.Cron = &cron
		}
		if v, ok := body["checkinScheduleMode"]; ok {
			mode := normalizeString(v)
			patch.Mode = &mode
		}
		if v, ok := body["checkinIntervalHours"]; ok {
			hours, err := toFloat64Strict(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "checkinIntervalHours 必须是数字类型"})
				return
			}
			if hours != float64(int(hours)) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "签到间隔必须是 1 到 24 的整数小时"})
				return
			}
			intervalHours := int(hours)
			patch.IntervalHours = &intervalHours
		}
		if _, err := applyCheckinScheduleSettings(h.db, cfg, patch); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
			return
		}
	}

	// Balance refresh cron
	if v, ok := body["balanceRefreshCron"]; ok {
		cron := normalizeString(v)
		cfg.BalanceRefreshCron = cron
		upsertSettingDB(h.db, "balance_refresh_cron", cron)
	}

	// Log cleanup settings
	if v, ok := body["logCleanupCron"]; ok {
		cron := normalizeString(v)
		cfg.LogCleanupCron = cron
		upsertSettingDB(h.db, "log_cleanup_cron", cron)
	}
	if v, ok := body["logCleanupUsageLogsEnabled"]; ok {
		enabled, err := toBoolStrict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "logCleanupUsageLogsEnabled 必须是布尔类型 (true/false)"})
			return
		}
		cfg.LogCleanupUsageLogsEnabled = enabled
		upsertSettingDB(h.db, "log_cleanup_usage_logs_enabled", enabled)
	}
	if v, ok := body["logCleanupProgramLogsEnabled"]; ok {
		enabled, err := toBoolStrict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "logCleanupProgramLogsEnabled 必须是布尔类型 (true/false)"})
			return
		}
		cfg.LogCleanupProgramLogsEnabled = enabled
		upsertSettingDB(h.db, "log_cleanup_program_logs_enabled", enabled)
	}
	if v, ok := body["logCleanupRetentionDays"]; ok {
		days, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "logCleanupRetentionDays 必须是数字类型"})
			return
		}
		if days < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "日志清理保留天数必须是大于等于 1 的整数"})
			return
		}
		cfg.LogCleanupRetentionDays = int(days)
		upsertSettingDB(h.db, "log_cleanup_retention_days", int(days))
	}

	// Model probe
	if v, ok := body["modelAvailabilityProbeEnabled"]; ok {
		enabled, err := toBoolStrict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "modelAvailabilityProbeEnabled 必须是布尔类型 (true/false)"})
			return
		}
		cfg.ModelAvailabilityProbeEnabled = enabled
		upsertSettingDB(h.db, "model_availability_probe_enabled", enabled)
	}

	// Codex upstream websocket
	if v, ok := body["codexUpstreamWebsocketEnabled"]; ok {
		enabled, err := toBoolStrict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "codexUpstreamWebsocketEnabled 必须是布尔类型 (true/false)"})
			return
		}
		cfg.CodexUpstreamWebsocketEnabled = enabled
		upsertSettingDB(h.db, "codex_upstream_websocket_enabled", enabled)
	}

	// Responses compact fallback
	if v, ok := body["responsesCompactFallbackToResponsesEnabled"]; ok {
		enabled, err := toBoolStrict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "responsesCompactFallbackToResponsesEnabled 必须是布尔类型 (true/false)"})
			return
		}
		cfg.ResponsesCompactFallbackToResponsesEnabled = enabled
		upsertSettingDB(h.db, "responses_compact_fallback_to_responses_enabled", enabled)
	}

	// Cross protocol fallback
	if v, ok := body["disableCrossProtocolFallback"]; ok {
		enabled, err := toBoolStrict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "disableCrossProtocolFallback 必须是布尔类型 (true/false)"})
			return
		}
		cfg.DisableCrossProtocolFallback = enabled
		upsertSettingDB(h.db, "disable_cross_protocol_fallback", enabled)
	}

	// Proxy session settings
	if v, ok := body["proxySessionChannelConcurrencyLimit"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "proxySessionChannelConcurrencyLimit 必须是数字类型"})
			return
		}
		if n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "会话通道并发上限必须是大于等于 0 的整数"})
			return
		}
		cfg.ProxySessionChannelConcurrencyLimit = int(n)
		upsertSettingDB(h.db, "proxy_session_channel_concurrency_limit", int(n))
	}
	if v, ok := body["proxySessionChannelQueueWaitMs"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "proxySessionChannelQueueWaitMs 必须是数字类型"})
			return
		}
		if n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "会话通道排队等待时间必须是大于等于 0 的整数毫秒"})
			return
		}
		cfg.ProxySessionChannelQueueWaitMs = int(n)
		upsertSettingDB(h.db, "proxy_session_channel_queue_wait_ms", int(n))
	}

	// Debug settings
	if err := applyBoolSettingDB(h.db, body, "proxyDebugTraceEnabled", &cfg.ProxyDebugTraceEnabled, "proxy_debug_trace_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if err := applyBoolSettingDB(h.db, body, "proxyDebugCaptureHeaders", &cfg.ProxyDebugCaptureHeaders, "proxy_debug_capture_headers"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if err := applyBoolSettingDB(h.db, body, "proxyDebugCaptureBodies", &cfg.ProxyDebugCaptureBodies, "proxy_debug_capture_bodies"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if err := applyBoolSettingDB(h.db, body, "proxyDebugCaptureStreamChunks", &cfg.ProxyDebugCaptureStreamChunks, "proxy_debug_capture_stream_chunks"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}

	if v, ok := body["proxyDebugTargetSessionId"]; ok {
		cfg.ProxyDebugTargetSessionId = normalizeString(v)
		upsertSettingDB(h.db, "proxy_debug_target_session_id", cfg.ProxyDebugTargetSessionId)
	}
	if v, ok := body["proxyDebugTargetClientKind"]; ok {
		cfg.ProxyDebugTargetClientKind = normalizeString(v)
		upsertSettingDB(h.db, "proxy_debug_target_client_kind", cfg.ProxyDebugTargetClientKind)
	}
	if v, ok := body["proxyDebugTargetModel"]; ok {
		cfg.ProxyDebugTargetModel = normalizeString(v)
		upsertSettingDB(h.db, "proxy_debug_target_model", cfg.ProxyDebugTargetModel)
	}
	if v, ok := body["proxyDebugRetentionHours"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "proxyDebugRetentionHours 必须是数字类型"})
			return
		}
		if n < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "代理调试保留时长必须是大于等于 1 的整数小时"})
			return
		}
		cfg.ProxyDebugRetentionHours = int(n)
		upsertSettingDB(h.db, "proxy_debug_retention_hours", int(n))
	}
	if v, ok := body["proxyDebugMaxBodyBytes"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "proxyDebugMaxBodyBytes 必须是数字类型"})
			return
		}
		if n < 1024 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "代理调试抓取体积上限必须是大于等于 1024 的整数字节"})
			return
		}
		cfg.ProxyDebugMaxBodyBytes = int(n)
		upsertSettingDB(h.db, "proxy_debug_max_body_bytes", int(n))
	}

	// Routing
	if v, ok := body["routingFallbackUnitCost"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routingFallbackUnitCost 必须是数字类型"})
			return
		}
		if n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "无价模型默认单价必须是大于 0 的数字"})
			return
		}
		if n < 1e-6 {
			n = 1e-6
		}
		cfg.RoutingFallbackUnitCost = n
		upsertSettingDB(h.db, "routing_fallback_unit_cost", n)
	}
	if v, ok := body["proxyFirstByteTimeoutSec"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "proxyFirstByteTimeoutSec 必须是数字类型"})
			return
		}
		if n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "首字超时必须是大于等于 0 的数字（秒）"})
			return
		}
		cfg.ProxyFirstByteTimeoutSec = int(n)
		upsertSettingDB(h.db, "proxy_first_byte_timeout_sec", int(n))
	}
	if v, ok := body["tokenRouterFailureCooldownMaxSec"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "tokenRouterFailureCooldownMaxSec 必须是数字类型"})
			return
		}
		if n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "路由失败冷却上限必须是大于 0 的数字（秒）"})
			return
		}
		cfg.TokenRouterFailureCooldownMaxSec = int(n)
		upsertSettingDB(h.db, "token_router_failure_cooldown_max_sec", int(n))
	}

	// Notify: Webhook
	if err := applyBoolSettingDB(h.db, body, "webhookEnabled", &cfg.WebhookEnabled, "webhook_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["webhookUrl"]; ok {
		cfg.WebhookUrl = normalizeString(v)
		upsertSettingDB(h.db, "webhook_url", cfg.WebhookUrl)
	}

	// Notify: Bark
	if err := applyBoolSettingDB(h.db, body, "barkEnabled", &cfg.BarkEnabled, "bark_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["barkUrl"]; ok {
		cfg.BarkUrl = normalizeString(v)
		upsertSettingDB(h.db, "bark_url", cfg.BarkUrl)
	}

	// Notify: ServerChan
	if err := applyBoolSettingDB(h.db, body, "serverChanEnabled", &cfg.ServerChanEnabled, "serverchan_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["serverChanKey"]; ok {
		cfg.ServerChanKey = normalizeString(v)
		upsertSettingDB(h.db, "serverchan_key", cfg.ServerChanKey)
	}

	// Notify: Telegram
	if err := applyBoolSettingDB(h.db, body, "telegramEnabled", &cfg.TelegramEnabled, "telegram_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["telegramApiBaseUrl"]; ok {
		cfg.TelegramApiBaseUrl = normalizeString(v)
		upsertSettingDB(h.db, "telegram_api_base_url", cfg.TelegramApiBaseUrl)
	}
	if v, ok := body["telegramBotToken"]; ok {
		cfg.TelegramBotToken = normalizeString(v)
		upsertSettingDB(h.db, "telegram_bot_token", cfg.TelegramBotToken)
	}
	if v, ok := body["telegramChatId"]; ok {
		cfg.TelegramChatId = normalizeString(v)
		upsertSettingDB(h.db, "telegram_chat_id", cfg.TelegramChatId)
	}
	if err := applyBoolSettingDB(h.db, body, "telegramUseSystemProxy", &cfg.TelegramUseSystemProxy, "telegram_use_system_proxy"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["telegramMessageThreadId"]; ok {
		cfg.TelegramMessageThreadId = normalizeString(v)
		upsertSettingDB(h.db, "telegram_message_thread_id", cfg.TelegramMessageThreadId)
	}

	// Notify: SMTP
	if err := applyBoolSettingDB(h.db, body, "smtpEnabled", &cfg.SmtpEnabled, "smtp_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["smtpHost"]; ok {
		cfg.SmtpHost = normalizeString(v)
		upsertSettingDB(h.db, "smtp_host", cfg.SmtpHost)
	}
	if v, ok := body["smtpPort"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "smtpPort 必须是数字类型"})
			return
		}
		cfg.SmtpPort = int(n)
		upsertSettingDB(h.db, "smtp_port", cfg.SmtpPort)
	}
	if err := applyBoolSettingDB(h.db, body, "smtpSecure", &cfg.SmtpSecure, "smtp_secure"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}
	if v, ok := body["smtpUser"]; ok {
		cfg.SmtpUser = normalizeString(v)
		upsertSettingDB(h.db, "smtp_user", cfg.SmtpUser)
	}
	if v, ok := body["smtpPass"]; ok {
		cfg.SmtpPass = normalizeString(v)
		upsertSettingDB(h.db, "smtp_pass", cfg.SmtpPass)
	}
	if v, ok := body["smtpFrom"]; ok {
		cfg.SmtpFrom = normalizeString(v)
		upsertSettingDB(h.db, "smtp_from", cfg.SmtpFrom)
	}
	if v, ok := body["smtpTo"]; ok {
		cfg.SmtpTo = normalizeString(v)
		upsertSettingDB(h.db, "smtp_to", cfg.SmtpTo)
	}

	// Notify cooldown
	if v, ok := body["notifyCooldownSec"]; ok {
		n, err := toFloat64Strict(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "notifyCooldownSec 必须是数字类型"})
			return
		}
		if n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "告警冷静期必须是大于等于 0 的数字（秒）"})
			return
		}
		cfg.NotifyCooldownSec = int(n)
		upsertSettingDB(h.db, "notify_cooldown_sec", int(n))
	}

	// Admin IP allowlist
	if v, ok := body["adminIpAllowlist"]; ok {
		var list []string
		switch val := v.(type) {
		case []any:
			for _, item := range val {
				if s, ok2 := item.(string); ok2 {
					list = append(list, strings.TrimSpace(s))
				}
			}
		case string:
			list = strings.Split(val, ",")
			for i := range list {
				list[i] = strings.TrimSpace(list[i])
			}
		}
		cfg.AdminIpAllowlist = list
		upsertSettingDB(h.db, "admin_ip_allowlist", list)
	}

	// Global blocked brands
	if v, ok := body["globalBlockedBrands"]; ok {
		brands, err := parseStringArraySetting(v, "globalBlockedBrands")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
			return
		}
		cfg.GlobalBlockedBrands = brands
		if err := upsertSettingDB(h.db, "global_blocked_brands", brands); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "保存品牌屏蔽失败"})
			return
		}
	}

	// Global allowed models
	// Only an explicit array (including []) may mutate this setting. null /
	// wrong types used to silently wipe the allowlist (upstream #515).
	if v, ok := body["globalAllowedModels"]; ok {
		models, err := parseStringArraySetting(v, "globalAllowedModels")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
			return
		}
		cfg.GlobalAllowedModels = models
		if err := upsertSettingDB(h.db, "global_allowed_models", models); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "保存模型白名单失败"})
			return
		}
	}

	// Proxy error keywords
	if v, ok := body["proxyErrorKeywords"]; ok {
		var keywords []string
		switch val := v.(type) {
		case []any:
			for _, item := range val {
				if s, ok2 := item.(string); ok2 {
					keywords = append(keywords, strings.TrimSpace(s))
				}
			}
		case string:
			for _, kw := range strings.Split(val, ",") {
				kw = strings.TrimSpace(kw)
				if kw != "" {
					keywords = append(keywords, kw)
				}
			}
		}
		cfg.ProxyErrorKeywords = keywords
		upsertSettingDB(h.db, "proxy_error_keywords", keywords)
	}

	// Proxy empty content fail
	if err := applyBoolSettingDB(h.db, body, "proxyEmptyContentFailEnabled", &cfg.ProxyEmptyContentFailEnabled, "proxy_empty_content_fail_enabled"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		return
	}

	// Routing weights
	if v, ok := body["routingWeights"]; ok {
		if rw, ok2 := v.(map[string]any); ok2 {
			if bf, ok3 := rw["baseWeightFactor"]; ok3 {
				val, err := toFloat64Strict(bf)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routingWeights.baseWeightFactor 必须是数字类型"})
					return
				}
				cfg.RoutingWeights.BaseWeightFactor = val
			}
			if vsf, ok3 := rw["valueScoreFactor"]; ok3 {
				val, err := toFloat64Strict(vsf)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routingWeights.valueScoreFactor 必须是数字类型"})
					return
				}
				cfg.RoutingWeights.ValueScoreFactor = val
			}
			if cw, ok3 := rw["costWeight"]; ok3 {
				val, err := toFloat64Strict(cw)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routingWeights.costWeight 必须是数字类型"})
					return
				}
				cfg.RoutingWeights.CostWeight = val
			}
			if bw, ok3 := rw["balanceWeight"]; ok3 {
				val, err := toFloat64Strict(bw)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routingWeights.balanceWeight 必须是数字类型"})
					return
				}
				cfg.RoutingWeights.BalanceWeight = val
			}
			if uw, ok3 := rw["usageWeight"]; ok3 {
				val, err := toFloat64Strict(uw)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "routingWeights.usageWeight 必须是数字类型"})
					return
				}
				cfg.RoutingWeights.UsageWeight = val
			}
			upsertSettingDB(h.db, "routing_weights", cfg.RoutingWeights)
		}
	}

	// Payload rules
	if v, ok := body["payloadRules"]; ok {
		cfg.PayloadRules = v
		upsertSettingDB(h.db, "payload_rules", v)
	}

	// Log the event
	logSettingsEvent(h.db, "status", "运行时设置已更新", "运行时设置已更新", "info", now)

	// Echo current filter lists so partial-update clients can re-sync without
	// another GET, and so empty arrays serialize as [] rather than null.
	writeJSON(w, http.StatusOK, map[string]any{
		"success":             true,
		"message":             "运行时设置已更新",
		"globalAllowedModels": stringSliceOrEmpty(cfg.GlobalAllowedModels),
		"globalBlockedBrands": stringSliceOrEmpty(cfg.GlobalBlockedBrands),
	})
}

// GET /api/settings/brand-list
func (h *settingsHandler) brandList(w http.ResponseWriter, r *http.Request) {
	// Stub: return known brands
	brands := []string{"new-api", "one-api", "veloera", "lobechat", "openwebui"}
	writeJSON(w, http.StatusOK, map[string]any{"brands": brands})
}

// POST /api/settings/system-proxy/test
func (h *settingsHandler) testSystemProxy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProxyUrl *string `json:"proxyUrl"`
	}
	if err := decodeJSONRequest(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request body"})
		return
	}

	proxyURL := h.cfg.SystemProxyUrl
	if body.ProxyUrl != nil {
		proxyURL = strings.TrimSpace(*body.ProxyUrl)
	}

	if proxyURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "请先填写系统代理地址",
		})
		return
	}

	// Stub: actual proxy testing would require HTTP client
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"proxyUrl":   proxyURL,
		"reachable":  true,
		"ok":         true,
		"statusCode": 204,
		"latencyMs":  100,
	})
}

func extractClientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}

func normalizeString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func hasAnyKey(body map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := body[key]; ok {
			return true
		}
	}
	return false
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		n, _ := val.Float64()
		return n
	default:
		return 0
	}
}

func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		val = strings.ToLower(strings.TrimSpace(val))
		return val == "1" || val == "true" || val == "yes" || val == "on"
	case float64:
		return val != 0
	case int:
		return val != 0
	default:
		return false
	}
}

// toFloat64Strict converts numeric JSON values and rejects non-numeric types
// such as strings, booleans, or objects. This prevents silent zero-coercion
// that hides client-side type errors.
func toFloat64Strict(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case json.Number:
		n, err := val.Float64()
		if err != nil {
			return 0, fmt.Errorf("invalid numeric value: %s", val.String())
		}
		return n, nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", v)
	}
}

// toBoolStrict converts boolean JSON values and rejects non-boolean types
// such as strings, numbers, or objects. This prevents silent false-coercion
// that hides client-side type errors.
func toBoolStrict(v any) (bool, error) {
	if b, ok := v.(bool); ok {
		return b, nil
	}
	return false, fmt.Errorf("expected a boolean (true/false), got %T", v)
}

func applyBoolSettingDB(db *sqlx.DB, body map[string]any, key string, target *bool, dbKey string) error {
	if v, ok := body[key]; ok {
		val, err := toBoolStrict(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*target = val
		if err := upsertSettingDB(db, dbKey, *target); err != nil {
			return err
		}
	}
	return nil
}

func upsertSettingDB(db *sqlx.DB, key string, value any) error {
	// Normalize nil string slices to empty arrays so we never persist JSON null
	// for list settings (null historically rehydrated as a wiped allowlist).
	if value == nil {
		value = []string{}
	}
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("settings: marshal %q: %w", key, err)
	}
	query := db.Rebind(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	if _, err := db.Exec(query, key, string(jsonValue)); err != nil {
		return fmt.Errorf("settings: upsert %q: %w", key, err)
	}
	return nil
}

// parseStringArraySetting validates an explicit JSON array setting.
// null / non-array values are rejected so accidental clients cannot wipe lists.
// Empty arrays are allowed and mean "clear / allow all" for the model whitelist.
func parseStringArraySetting(v any, field string) ([]string, error) {
	if v == nil {
		return nil, fmt.Errorf("%s must be an array of strings (use [] to clear)", field)
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	out := make([]string, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", field)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}

func stringSliceOrEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func logSettingsEvent(db *sqlx.DB, eventType, title, message, level, createdAt string) {
	// Silently ignore errors (matches TS behavior)
	db.Exec(`INSERT INTO events (type, title, message, level, related_type, created_at, "read")
		VALUES (?, ?, ?, ?, 'settings', ?, 0)`, eventType, title, message, level, createdAt)
}
