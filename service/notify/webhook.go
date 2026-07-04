package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/service"
)

// WebhookChannel sends notifications via webhook (WeCom/Feishu/generic).
type WebhookChannel struct{}

func (c *WebhookChannel) Name() string { return "webhook" }

func (c *WebhookChannel) Send(cfg *config.Config, title, message, level, timeFootnote string) error {
	if !cfg.WebhookEnabled || cfg.WebhookUrl == "" {
		return fmt.Errorf("webhook not configured")
	}

	isWeCom := isWeComBotWebhook(cfg.WebhookUrl)
	isFeishu := isFeishuBotWebhook(cfg.WebhookUrl)

	var body string
	if isWeCom {
		content := buildWeComText(title, message, level, timeFootnote)
		body = mustJSON(map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": content},
		})
	} else if isFeishu {
		content := buildFeishuText(title, message, level, timeFootnote)
		body = mustJSON(map[string]any{
			"msg_type": "text",
			"content":  map[string]string{"text": content},
		})
	} else {
		now := time.Now()
		body = mustJSON(map[string]any{
			"title":     title,
			"message":   message,
			"level":     level,
			"timestamp": now.UTC().Format(time.RFC3339),
			"localTime": service.FormatLocalDateTime(now),
			"timeZone":  service.GetResolvedTimeZone(),
		})
	}

	resp, err := http.Post(cfg.WebhookUrl, "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook response status %d", resp.StatusCode)
	}

	// Parse response for known bot types
	respBody, _ := io.ReadAll(resp.Body)
	if isWeCom {
		var payload struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(respBody, &payload); err != nil {
			return fmt.Errorf("wecom webhook returned invalid JSON")
		}
		if payload.ErrCode != 0 {
			return fmt.Errorf("wecom webhook error %d: %s", payload.ErrCode, payload.ErrMsg)
		}
	}
	if isFeishu {
		var payload struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(respBody, &payload); err != nil {
			return fmt.Errorf("feishu webhook returned invalid JSON")
		}
		if payload.Code != 0 {
			return fmt.Errorf("feishu webhook error %d: %s", payload.Code, payload.Msg)
		}
	}

	return nil
}

func isWeComBotWebhook(webhookURL string) bool {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return false
	}
	return u.Hostname() == "qyapi.weixin.qq.com" && strings.Contains(u.Path, "/cgi-bin/webhook/send")
}

func isFeishuBotWebhook(webhookURL string) bool {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return false
	}
	return (u.Hostname() == "open.feishu.cn" || u.Hostname() == "open.larksuite.com") &&
		strings.Contains(u.Path, "/open-apis/bot/v2/hook/")
}

func buildWeComText(title, message, level, timeFootnote string) string {
	const maxLen = 1900
	raw := fmt.Sprintf("[metapi][%s] %s\n\n%s\n\n%s", strings.ToUpper(level), title, message, timeFootnote)
	if len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen] + "\n...(truncated)"
}

func buildFeishuText(title, message, level, timeFootnote string) string {
	const maxLen = 3900
	raw := fmt.Sprintf("[metapi][%s] %s\n\n%s\n\n%s", strings.ToUpper(level), title, message, timeFootnote)
	if len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen] + "\n...(truncated)"
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
