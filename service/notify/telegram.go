package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/platform"
	"github.com/tokendancelab/metapi-go/service"
)

// TelegramChannel sends notifications via Telegram Bot.
type TelegramChannel struct{}

func (c *TelegramChannel) Name() string { return "telegram" }

func (c *TelegramChannel) Send(cfg *config.Config, title, message, level, timeFootnote string) error {
	if !cfg.TelegramEnabled || cfg.TelegramBotToken == "" || cfg.TelegramChatId == "" {
		return fmt.Errorf("telegram not configured")
	}

	apiBase := strings.TrimRight(cfg.TelegramApiBaseUrl, "/")
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}

	reqURL := fmt.Sprintf("%s/bot%s/sendMessage", apiBase, cfg.TelegramBotToken)
	text := buildTelegramText(title, message, level, timeFootnote)

	bodyMap := map[string]any{
		"chat_id":                  cfg.TelegramChatId,
		"text":                     text,
		"disable_web_page_preview": true,
	}

	threadIDStr := strings.TrimSpace(cfg.TelegramMessageThreadId)
	if threadIDStr != "" {
		if threadID, err := strconv.ParseInt(threadIDStr, 10, 64); err == nil && threadID > 0 {
			bodyMap["message_thread_id"] = threadID
		}
	}

	body, _ := json.Marshal(bodyMap)

	// Use system proxy if configured. Always reject cross-origin redirects so a
	// compromised/misconfigured Telegram API base cannot SSRF via 302.
	var client *http.Client
	if cfg.TelegramUseSystemProxy && cfg.SystemProxyUrl != "" {
		client = service.ProxyAwareHTTPClient(cfg.SystemProxyUrl, 30*time.Second)
	} else {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	client.CheckRedirect = platform.RejectCrossOriginRedirect

	resp, err := client.Post(reqURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram response status %d", resp.StatusCode)
	}

	respBody, err := readNotifyResponseBody(resp.Body)
	if err != nil {
		return fmt.Errorf("telegram response read failed: %w", err)
	}
	var payload struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &payload); err == nil && !payload.OK {
		return fmt.Errorf("telegram error: %s", payload.Description)
	}

	return nil
}

func buildTelegramText(title, message, level, timeFootnote string) string {
	const maxLen = 3900
	raw := fmt.Sprintf("[metapi][%s] %s\n\n%s\n\nLevel: %s\n%s",
		strings.ToUpper(level), title, message, level, timeFootnote)
	if len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen] + "\n\n...(truncated)"
}
