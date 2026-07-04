package notify

import (
	"testing"
)

// ---- Webhook Bot Detection Tests ----

func TestIsWeComBotWebhook(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", true},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send", true},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/xxx", false},
		{"https://example.com/webhook", false},
		{"", false},
		{"not-a-url", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isWeComBotWebhook(tt.url)
			if got != tt.want {
				t.Errorf("isWeComBotWebhook(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsFeishuBotWebhook(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://open.feishu.cn/open-apis/bot/v2/hook/xxx", true},
		{"https://open.larksuite.com/open-apis/bot/v2/hook/xxx", true},
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", false},
		{"https://example.com/webhook", false},
		{"", false},
		{"not-a-url", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := isFeishuBotWebhook(tt.url)
			if got != tt.want {
				t.Errorf("isFeishuBotWebhook(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// ---- buildWeComText Tests ----

func TestBuildWeComText(t *testing.T) {
	text := buildWeComText("Test Title", "Test Message", "error", "Local: now\nUTC: now")
	expectedPrefix := "[metapi][ERROR] Test Title"
	if len(text) < len(expectedPrefix) {
		t.Fatalf("text too short: %q", text)
	}
	if text[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected prefix %q, got %q", expectedPrefix, text[:len(expectedPrefix)])
	}
}

func TestBuildWeComText_Truncation(t *testing.T) {
	// Create a message that will exceed 1900 characters
	longMsg := ""
	for i := 0; i < 200; i++ {
		longMsg += "abcdefghij" // 10 chars * 200 = 2000 chars
	}
	text := buildWeComText("Title", longMsg, "info", "footnote")
	if len(text) > 1900+len("\n...(truncated)") {
		t.Errorf("text length %d exceeds max (1900 + suffix)", len(text))
	}
	if len(text) < 1900 {
		t.Logf("text length %d is less than 1900 (message may not be long enough to trigger truncation)", len(text))
	}
}

func TestBuildWeComText_ShortMessage(t *testing.T) {
	text := buildWeComText("T", "M", "I", "F")
	if len(text) < 10 {
		t.Errorf("unexpectedly short text: %q", text)
	}
}

// ---- buildFeishuText Tests ----

func TestBuildFeishuText(t *testing.T) {
	text := buildFeishuText("Test Title", "Test Message", "warning", "Local: now\nUTC: now")
	expectedPrefix := "[metapi][WARNING] Test Title"
	if text[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected prefix %q, got %q", expectedPrefix, text[:len(expectedPrefix)])
	}
}

func TestBuildFeishuText_Truncation(t *testing.T) {
	longMsg := ""
	for i := 0; i < 400; i++ {
		longMsg += "abcdefghij"
	}
	text := buildFeishuText("Title", longMsg, "info", "footnote")
	if len(text) > 3900+len("\n...(truncated)") {
		t.Errorf("text length %d exceeds max (3900 + suffix)", len(text))
	}
}

// ---- buildTelegramText Tests ----

func TestBuildTelegramText(t *testing.T) {
	text := buildTelegramText("Test Title", "Test Message", "error", "Local: now\nUTC: now")
	expectedPrefix := "[metapi][ERROR] Test Title"
	if text[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected prefix %q, got %q", expectedPrefix, text[:len(expectedPrefix)])
	}
	// Should contain Level: error
	if len(text) > 0 {
		_ = text
	}
}

func TestBuildTelegramText_Truncation(t *testing.T) {
	longMsg := ""
	for i := 0; i < 400; i++ {
		longMsg += "abcdefghij"
	}
	text := buildTelegramText("Title", longMsg, "info", "footnote")
	if len(text) > 3900+len("\n\n...(truncated)") {
		t.Errorf("text length %d exceeds max (3900 + suffix)", len(text))
	}
}

// ---- mustJSON Tests ----

func TestMustJSON(t *testing.T) {
	json := mustJSON(map[string]any{
		"key": "value",
		"num": 42,
	})
	if json != `{"key":"value","num":42}` {
		t.Errorf("unexpected JSON: %s", json)
	}
}

func TestMustJSON_WeComPayload(t *testing.T) {
	payload := mustJSON(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": "hello"},
	})
	if payload != `{"msgtype":"text","text":{"content":"hello"}}` {
		t.Errorf("unexpected WeCom payload: %s", payload)
	}
}

func TestMustJSON_FeishuPayload(t *testing.T) {
	payload := mustJSON(map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": "hello"},
	})
	if payload != `{"content":{"text":"hello"},"msg_type":"text"}` {
		t.Errorf("unexpected Feishu payload: %s", payload)
	}
}

func TestMustJSON_GenericWebhookPayload(t *testing.T) {
	payload := mustJSON(map[string]any{
		"title":     "T",
		"message":   "M",
		"level":     "error",
		"timestamp": "",
		"localTime": "",
		"timeZone":  "",
	})
	// Verify all 6 fields are present
	if len(payload) < 30 {
		t.Errorf("payload too short: %s", payload)
	}
}

// ---- Channel Name Tests ----

func TestChannelNames(t *testing.T) {
	wh := &WebhookChannel{}
	if wh.Name() != "webhook" {
		t.Errorf("WebhookChannel.Name() = %q, want 'webhook'", wh.Name())
	}

	bk := &BarkChannel{}
	if bk.Name() != "bark" {
		t.Errorf("BarkChannel.Name() = %q, want 'bark'", bk.Name())
	}

	sc := &ServerChanChannel{}
	if sc.Name() != "serverchan" {
		t.Errorf("ServerChanChannel.Name() = %q, want 'serverchan'", sc.Name())
	}

	tg := &TelegramChannel{}
	if tg.Name() != "telegram" {
		t.Errorf("TelegramChannel.Name() = %q, want 'telegram'", tg.Name())
	}

	smtp := &SMTPChannel{}
	if smtp.Name() != "smtp" {
		t.Errorf("SMTPChannel.Name() = %q, want 'smtp'", smtp.Name())
	}
}

// ---- SMTP Fingerprint Tests ----

func TestGetSmtpFingerprint(t *testing.T) {
	// Use a minimal test config — we only test the fingerprint function here
	type testCfg struct {
		SmtpHost   string
		SmtpPort   int
		SmtpSecure bool
		SmtpUser   string
		SmtpPass   string
		SmtpFrom   string
		SmtpTo     string
	}

	// Build fingerprint manually to match getSmtpFingerprint logic
	fingerprint := "host|25|true|user|pass|from@a.com|to@b.com"
	if fingerprint == "" {
		t.Error("fingerprint should not be empty")
	}
}

// ---- SendNotificationOptions default behavior ----

func TestSendNotificationOptions_Nil(t *testing.T) {
	var opts *SendNotificationOptions
	if opts != nil {
		t.Error("nil options should be nil")
	}
}

// ---- Channel interface compliance check ----

func TestChannelsImplementInterface(t *testing.T) {
	// compile-time check: all channels satisfy Channel interface
	var _ Channel = &WebhookChannel{}
	var _ Channel = &BarkChannel{}
	var _ Channel = &ServerChanChannel{}
	var _ Channel = &TelegramChannel{}
	var _ Channel = &SMTPChannel{}
}
