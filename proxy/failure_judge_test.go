package proxy

import (
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

func setupFailureCfg(keywords []string, emptyContentFail bool) {
	cfg := config.Load(map[string]string{
		"PORT":                    "8080",
		"PROXY_EMPTY_CONTENT_FAIL": boolToString(emptyContentFail),
	})
	if len(keywords) > 0 {
		cfg.ProxyErrorKeywords = keywords
	}
	cfg.ProxyEmptyContentFailEnabled = emptyContentFail
	config.Set(cfg)
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestDetectProxyFailure_KeywordMatching(t *testing.T) {
	t.Run("matches error keyword", func(t *testing.T) {
		setupFailureCfg([]string{"overloaded", "blocked"}, false)

		result := DetectProxyFailure("The server is overloaded, please try later", nil)
		if result == nil {
			t.Fatal("expected failure detected via keyword")
		}
		if result.Status != 502 {
			t.Errorf("expected status 502, got %d", result.Status)
		}
		if result.Reason == "" {
			t.Error("expected reason message")
		}
	})

	t.Run("case-insensitive matching", func(t *testing.T) {
		setupFailureCfg([]string{"OVERLOADED"}, false)

		result := DetectProxyFailure("Server is overloaded", nil)
		if result == nil {
			t.Fatal("expected case-insensitive match")
		}
	})

	t.Run("multiple keywords, matches first", func(t *testing.T) {
		setupFailureCfg([]string{"error", "blocked", "unavailable"}, false)

		result := DetectProxyFailure("This API is blocked", nil)
		if result == nil {
			t.Fatal("expected keyword match")
		}
	})

	t.Run("no keyword match", func(t *testing.T) {
		setupFailureCfg([]string{"overloaded"}, false)

		result := DetectProxyFailure("Everything is fine", nil)
		if result != nil {
			t.Errorf("expected no failure, got reason: %s", result.Reason)
		}
	})

	t.Run("empty keyword list", func(t *testing.T) {
		setupFailureCfg([]string{}, false)

		result := DetectProxyFailure("Some error occurred", nil)
		if result != nil {
			t.Errorf("expected no failure with empty keywords, got reason: %s", result.Reason)
		}
	})

	t.Run("empty keyword (whitespace only) skips", func(t *testing.T) {
		setupFailureCfg([]string{"  ", "blocked"}, false)

		result := DetectProxyFailure("The user is blocked", nil)
		if result == nil {
			t.Fatal("expected match for non-empty keyword")
		}
	})

	t.Run("empty text input", func(t *testing.T) {
		setupFailureCfg([]string{"error"}, false)

		result := DetectProxyFailure("", nil)
		if result != nil {
			t.Errorf("expected no failure for empty text, got: %s", result.Reason)
		}
	})

	t.Run("whitespace-only text input", func(t *testing.T) {
		setupFailureCfg([]string{"error"}, false)

		result := DetectProxyFailure("   \n  ", nil)
		if result != nil {
			t.Errorf("expected no failure for whitespace text, got: %s", result.Reason)
		}
	})
}

func TestDetectProxyFailure_EmptyContent(t *testing.T) {
	t.Run("empty content fail enabled, no tokens, no output", func(t *testing.T) {
		setupFailureCfg([]string{}, true)

		result := DetectProxyFailure(`{"id":"test"}`, &UsageSummary{
			PromptTokens:     100,
			CompletionTokens: 0,
			TotalTokens:      100,
		})
		if result == nil {
			t.Fatal("expected failure for empty content")
		}
		if result.Status != 502 {
			t.Errorf("expected status 502, got %d", result.Status)
		}
	})

	t.Run("empty content fail enabled, has completion tokens", func(t *testing.T) {
		setupFailureCfg([]string{}, true)

		result := DetectProxyFailure("{}", &UsageSummary{
			CompletionTokens: 50,
			TotalTokens:      150,
		})
		if result != nil {
			t.Errorf("expected no failure (has completion tokens)")
		}
	})

	t.Run("empty content fail enabled, nil usage", func(t *testing.T) {
		setupFailureCfg([]string{}, true)

		result := DetectProxyFailure(`{}`, nil)
		if result == nil {
			t.Fatal("expected failure (nil usage, no output)")
		}
	})

	t.Run("empty content fail disabled", func(t *testing.T) {
		setupFailureCfg([]string{}, false)

		result := DetectProxyFailure(`{}`, &UsageSummary{
			CompletionTokens: 0,
		})
		if result != nil {
			t.Errorf("expected no failure when empty content fail disabled")
		}
	})
}

func TestDetectHasUpstreamOutput(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		if detectHasUpstreamOutput("") {
			t.Error("expected false for empty text")
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		if detectHasUpstreamOutput("   \n  ") {
			t.Error("expected false for whitespace")
		}
	})

	t.Run("JSON with text content", func(t *testing.T) {
		if !detectHasUpstreamOutput(`{"choices":[{"message":{"content":"hello"}}]}`) {
			t.Error("expected true for JSON with choices content")
		}
	})

	t.Run("JSON with no content", func(t *testing.T) {
		if detectHasUpstreamOutput(`{"id":"chat-123","created":123}`) {
			t.Error("expected false for JSON with no content")
		}
	})

	t.Run("JSON with output_text", func(t *testing.T) {
		if !detectHasUpstreamOutput(`{"output_text":"generated text"}`) {
			t.Error("expected true for JSON with output_text")
		}
	})

	t.Run("JSON with tool_calls", func(t *testing.T) {
		if !detectHasUpstreamOutput(`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"test"}}]}}]}`) {
			t.Error("expected true for JSON with tool_calls")
		}
	})

	t.Run("JSON with delta text", func(t *testing.T) {
		if !detectHasUpstreamOutput(`{"choices":[{"delta":{"content":"partial"}}]}`) {
			t.Error("expected true for JSON with delta content")
		}
	})

	t.Run("SSE with [DONE] only", func(t *testing.T) {
		if detectHasUpstreamOutput("data: [DONE]\n") {
			t.Error("expected false for [DONE]-only SSE")
		}
	})

	t.Run("SSE with content events", func(t *testing.T) {
		text := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n"
		if !detectHasUpstreamOutput(text) {
			t.Error("expected true for SSE with content")
		}
	})

	t.Run("SSE with non-JSON payload", func(t *testing.T) {
		text := "data: some raw text\n"
		if !detectHasUpstreamOutput(text) {
			t.Error("expected true for SSE with raw text")
		}
	})

	t.Run("plain text (no JSON, no SSE)", func(t *testing.T) {
		if !detectHasUpstreamOutput("This is a plain text response") {
			t.Error("expected true for plain text")
		}
	})

	t.Run("JSON with output array containing text", func(t *testing.T) {
		json := `{"output":[{"type":"message","content":[{"type":"output_text","text":"result"}]}]}`
		if !detectHasUpstreamOutput(json) {
			t.Error("expected true for output array with text content")
		}
	})

	t.Run("JSON with content parts", func(t *testing.T) {
		json := `{"content":[{"type":"text","text":"Hello world"}]}`
		if !detectHasUpstreamOutput(json) {
			t.Error("expected true for content parts with text")
		}
	})

	t.Run("JSON with message refusal", func(t *testing.T) {
		json := `{"choices":[{"message":{"refusal":"I cannot do that"}}]}`
		if !detectHasUpstreamOutput(json) {
			t.Error("expected true for refusal (considered output)")
		}
	})
}

func TestDetectProxyFailure_Combined(t *testing.T) {
	t.Run("keyword takes priority over empty content", func(t *testing.T) {
		setupFailureCfg([]string{"quota"}, true)

		result := DetectProxyFailure("quota exceeded", &UsageSummary{
			CompletionTokens: 0,
		})
		if result == nil {
			t.Fatal("expected failure from keyword")
		}
		if result.Status != 502 {
			t.Errorf("expected status 502, got %d", result.Status)
		}
	})

	t.Run("no keyword, has content, empty content check skipped", func(t *testing.T) {
		setupFailureCfg([]string{}, true)

		result := DetectProxyFailure("This is fine", nil)
		if result != nil {
			t.Errorf("expected no failure (plain text detected as output)")
		}
	})
}

func TestHasCompletionContentFromPayload(t *testing.T) {
	t.Run("nil payload", func(t *testing.T) {
		if hasCompletionContentFromPayload(nil) {
			t.Error("expected false for nil")
		}
	})

	t.Run("non-map payload", func(t *testing.T) {
		if hasCompletionContentFromPayload([]string{"test"}) {
			t.Error("expected false for non-map")
		}
	})

	t.Run("empty map", func(t *testing.T) {
		if hasCompletionContentFromPayload(map[string]any{}) {
			t.Error("expected false for empty map")
		}
	})

	t.Run("direct text field", func(t *testing.T) {
		if !hasCompletionContentFromPayload(map[string]any{"text": "hello"}) {
			t.Error("expected true for direct text")
		}
	})

	t.Run("empty text field", func(t *testing.T) {
		if hasCompletionContentFromPayload(map[string]any{"text": ""}) {
			t.Error("expected false for empty text")
		}
	})

	t.Run("delta text", func(t *testing.T) {
		if !hasCompletionContentFromPayload(map[string]any{"delta": "content"}) {
			t.Error("expected true for delta string")
		}
	})

	t.Run("function_call key", func(t *testing.T) {
		if !hasCompletionContentFromPayload(map[string]any{"function_call": map[string]any{"name": "test"}}) {
			t.Error("expected true for function_call")
		}
	})

	t.Run("tool_calls array", func(t *testing.T) {
		if !hasCompletionContentFromPayload(map[string]any{"tool_calls": []any{map[string]any{"name": "test"}}}) {
			t.Error("expected true for tool_calls")
		}
	})

	t.Run("outputText field", func(t *testing.T) {
		if !hasCompletionContentFromPayload(map[string]any{"outputText": "result"}) {
			t.Error("expected true for outputText")
		}
	})
}
