package messages

import (
	"encoding/json"
	"testing"

	"github.com/tokendancelab/metapi-go/transform/shared"
)

// ---------------------------------------------------------------------------
// Golden: sanitizeAnthropicMessagesBody (basic, no message assertions)
// ---------------------------------------------------------------------------

func TestSanitizeAnthropicMessagesBody_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model":       "claude-sonnet-4-20250514",
		"stream":      true,
		"max_tokens":  4096,
		"temperature": 0.7,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Hello!"},
				},
			},
		},
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	if sanitized["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model mismatch: %v", sanitized["model"])
	}
}

// ---------------------------------------------------------------------------
// T+P mutual exclusion (individual test, not touching messages)
// ---------------------------------------------------------------------------

func TestSanitizeAnthropicMessagesBody_TPExclusion(t *testing.T) {
	body := map[string]any{
		"model":       "claude-sonnet-4-20250514",
		"max_tokens":  4096,
		"temperature": 0.7,
		"top_p":       0.9,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	if sanitized["temperature"] == nil {
		t.Error("temperature should be kept")
	}
	if sanitized["top_p"] != nil {
		t.Error("top_p should be removed when temperature is present")
	}
}

// ---------------------------------------------------------------------------
// Thinking config (individual sanitization, no message dependency)
// ---------------------------------------------------------------------------

func TestSanitizeAnthropicMessagesBody_ThinkingEnabled(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"thinking": map[string]any{
			"type":          "enabled",
			"budget_tokens": 16000,
		},
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	thinking, ok := sanitized["thinking"].(map[string]any)
	if !ok {
		t.Fatal("thinking missing")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected enabled, got %v", thinking["type"])
	}
	if bt, ok := thinking["budget_tokens"]; !ok || bt.(int) != 16000 {
		t.Errorf("expected budget_tokens 16000, got %v", bt)
	}
}

func TestSanitizeAnthropicMessagesBody_ThinkingEnabledNoBudget(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"thinking": map[string]any{
			"type": "enabled",
		},
	}

	_, err := SanitizeAnthropicMessagesBody(body, true)
	if err == nil {
		t.Fatal("expected error for enabled without budget_tokens")
	}
}

func TestSanitizeAnthropicMessagesBody_ThinkingAdaptive(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"thinking": map[string]any{"type": "adaptive"},
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	thinking, ok := sanitized["thinking"].(map[string]any)
	if !ok {
		t.Fatal("thinking missing")
	}
	if thinking["type"] != "adaptive" {
		t.Errorf("expected adaptive, got %v", thinking["type"])
	}
}

func TestSanitizeAnthropicMessagesBody_ThinkingDisabled(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"thinking": map[string]any{"type": "disabled"},
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	thinking, ok := sanitized["thinking"].(map[string]any)
	if !ok {
		t.Fatal("thinking missing")
	}
	if thinking["type"] != "disabled" {
		t.Errorf("expected disabled, got %v", thinking["type"])
	}
}

func TestSanitizeAnthropicMessagesBody_InvalidThinkingType(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"thinking": map[string]any{"type": "invalid"},
	}

	_, err := SanitizeAnthropicMessagesBody(body, true)
	if err == nil {
		t.Fatal("expected error for invalid thinking type")
	}
}

// ---------------------------------------------------------------------------
// Tool choice (individual sanitization)
// ---------------------------------------------------------------------------

func TestSanitizeAnthropicMessagesBody_ToolChoiceAuto(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "auto",
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	tc, ok := sanitized["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice not object")
	}
	if tc["type"] != "auto" {
		t.Errorf("expected auto, got %v", tc["type"])
	}
}

func TestSanitizeAnthropicMessagesBody_ToolChoiceNone(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "none",
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	tc, ok := sanitized["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice not object")
	}
	if tc["type"] != "none" {
		t.Errorf("expected none, got %v", tc["type"])
	}
}

func TestSanitizeAnthropicMessagesBody_ToolChoiceRequired(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "required",
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	tc, ok := sanitized["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice not object")
	}
	if tc["type"] != "any" {
		t.Errorf("expected any, got %v", tc["type"])
	}
}

func TestSanitizeAnthropicMessagesBody_ToolChoiceNamedTool(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": map[string]any{"type": "tool", "name": "get_weather"},
	}

	sanitized, err := SanitizeAnthropicMessagesBody(body, true)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	tc, ok := sanitized["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice not object")
	}
	if tc["type"] != "tool" || tc["name"] != "get_weather" {
		t.Errorf("expected {type:tool, name:get_weather}, got %v", tc)
	}
}

func TestSanitizeAnthropicMessagesBody_ToolChoiceToolWithoutName(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": map[string]any{"type": "tool"},
	}

	_, err := SanitizeAnthropicMessagesBody(body, true)
	if err == nil {
		t.Fatal("expected error for tool without name")
	}
}

// ---------------------------------------------------------------------------
// Content block sanitizers (direct calls, not via full pipeline)
// ---------------------------------------------------------------------------

func TestSanitizeAnthropicContentBlock_TextBlock(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type": "text",
		"text": "Hello",
	})
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block["type"] != "text" || block["text"] != "Hello" {
		t.Errorf("unexpected block: %v", block)
	}
}

func TestSanitizeAnthropicContentBlock_InputTextBlock(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type": "input_text",
		"text": "Query",
	})
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block["type"] != "text" {
		t.Errorf("expected normalized to text, got %v", block["type"])
	}
}

func TestSanitizeAnthropicContentBlock_EmptyText(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type": "text",
		"text": "",
	})
	if block != nil {
		t.Errorf("expected nil for empty text, got %v", block)
	}
}

func TestSanitizeAnthropicContentBlock_ImageURLBlock(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": "https://example.com/img.png"},
	})
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block["type"] != "image" {
		t.Errorf("expected image, got %v", block["type"])
	}
}

func TestSanitizeAnthropicContentBlock_ImageDataURLtoBase64(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="},
	})
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block["type"] != "image" {
		t.Errorf("expected image, got %v", block["type"])
	}
	src, _ := block["source"].(map[string]any)
	if src == nil || src["type"] != "base64" {
		t.Errorf("expected base64 source, got %v", src)
	}
}

func TestSanitizeAnthropicContentBlock_DocumentBlock(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type":      "file",
		"file_data": "base64data",
		"mime_type": "application/pdf",
		"filename":  "report.pdf",
	})
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block["type"] != "document" {
		t.Errorf("expected document, got %v", block["type"])
	}
}

func TestSanitizeAnthropicContentBlock_ToolResultBlock(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type":         "tool_result",
		"tool_use_id": "toolu_01",
		"content":      "result text",
	})
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block["type"] != "tool_result" {
		t.Errorf("expected tool_result, got %v", block["type"])
	}
}

func TestSanitizeAnthropicContentBlock_ToolResultNoID(t *testing.T) {
	block := sanitizeAnthropicContentBlock(map[string]any{
		"type":    "tool_result",
		"content": "result text",
	})
	if block != nil {
		t.Errorf("expected nil for tool_result without tool_use_id, got %v", block)
	}
}

// ---------------------------------------------------------------------------
// OpenAI -> Anthropic conversion (main roundtrip pathway)
// ---------------------------------------------------------------------------

func TestConvertOpenAiBodyToAnthropicMessagesBody_Basic(t *testing.T) {
	oaiBody := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{"role": "system", "content": "You are helpful."},
			map[string]any{"role": "user", "content": "Hello!"},
			map[string]any{"role": "assistant", "content": "Hi there!"},
		},
		"temperature": 0.7,
		"max_tokens":  4096,
		"stream":      true,
	}

	anthBody, err := ConvertOpenAiBodyToAnthropicMessagesBody(oaiBody, "claude-sonnet-4-20250514", true)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if anthBody["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model: expected claude-sonnet-4-20250514, got %v", anthBody["model"])
	}
	if anthBody["stream"] != true {
		t.Errorf("stream: expected true")
	}

	// System merged
	sys, ok := anthBody["system"].(string)
	if !ok || sys == "" {
		t.Error("system message missing or empty")
	}

	// Messages
	msgs, ok := anthBody["messages"].([]map[string]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %v", msgs)
	}
}

func TestConvertOpenAiBodyToAnthropicMessagesBody_WithTools(t *testing.T) {
	oaiBody := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{"role": "user", "content": "Get weather"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get weather",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}

	anthBody, err := ConvertOpenAiBodyToAnthropicMessagesBody(oaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	tools, ok := anthBody["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("tools missing")
	}
	firstTool, _ := tools[0].(map[string]any)
	if firstTool["name"] != "get_weather" {
		t.Errorf("expected get_weather, got %v", firstTool["name"])
	}
}

func TestConvertOpenAiBodyToAnthropicMessagesBody_Reasoning(t *testing.T) {
	oaiBody := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{"role": "user", "content": "Complex problem"},
		},
		"reasoning_effort": "high",
		"reasoning_budget": 32000,
	}

	anthBody, err := ConvertOpenAiBodyToAnthropicMessagesBody(oaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if thinking, ok := anthBody["thinking"].(map[string]any); ok {
		if thinking["type"] != "enabled" {
			t.Errorf("expected thinking enabled, got %v", thinking["type"])
		}
		if bt, ok := thinking["budget_tokens"]; !ok || bt != 32000 {
			t.Errorf("expected budget_tokens 32000, got %v", bt)
		}
	} else {
		t.Error("thinking missing")
	}
}

func TestConvertOpenAiBodyToAnthropicMessagesBody_ToolChoice(t *testing.T) {
	oaiBody := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "none",
	}

	anthBody, err := ConvertOpenAiBodyToAnthropicMessagesBody(oaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	tc, ok := anthBody["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice missing")
	}
	if tc["type"] != "none" {
		t.Errorf("expected none, got %v", tc["type"])
	}
}

func TestConvertOpenAiBodyToAnthropicMessagesBody_ParallelToolCallsDisabled(t *testing.T) {
	oaiBody := map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tools": []any{
			map[string]any{
				"type":     "function",
				"function": map[string]any{"name": "test_fn", "parameters": map[string]any{"type": "object"}},
			},
		},
		"parallel_tool_calls": false,
		"tool_choice":         "auto",
	}

	anthBody, err := ConvertOpenAiBodyToAnthropicMessagesBody(oaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	tc, ok := anthBody["tool_choice"].(map[string]any)
	if !ok {
		t.Fatal("tool_choice missing")
	}
	if dp, ok := tc["disable_parallel_tool_use"]; !ok || dp != true {
		t.Errorf("expected disable_parallel_tool_use=true, got %v", tc)
	}
}

// ---------------------------------------------------------------------------
// Anthropic -> OpenAI conversion (ParseDownstreamChatRequest + Claude format)
// ---------------------------------------------------------------------------

func TestParseDownstreamChatRequest_ClaudeFormat_Basic(t *testing.T) {
	claudeBody := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Hello!",
			},
		},
	}

	parsed, errPayload := shared.ParseDownstreamChatRequest(claudeBody, shared.FormatClaude)
	if errPayload != nil {
		t.Fatalf("parse: %v", errPayload)
	}

	if parsed.RequestedModel != "claude-sonnet-4-20250514" {
		t.Errorf("expected claude-sonnet-4-20250514, got %q", parsed.RequestedModel)
	}

	msgs, ok := parsed.UpstreamBody["messages"].([]map[string]any)
	if !ok || len(msgs) == 0 {
		t.Fatal("messages missing")
	}
}

func TestParseDownstreamChatRequest_ClaudeFormat_NoModel(t *testing.T) {
	_, errPayload := shared.ParseDownstreamChatRequest(map[string]any{}, shared.FormatClaude)
	if errPayload == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestParseDownstreamChatRequest_ClaudeFormat_NoMessages(t *testing.T) {
	body := map[string]any{"model": "claude-sonnet-4-20250514"}
	_, errPayload := shared.ParseDownstreamChatRequest(body, shared.FormatClaude)
	if errPayload == nil {
		t.Fatal("expected error for missing messages")
	}
}

func TestParseDownstreamChatRequest_OpenAIFormat_NoModel(t *testing.T) {
	_, errPayload := shared.ParseDownstreamChatRequest(map[string]any{}, shared.FormatOpenAI)
	if errPayload == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestParseDownstreamChatRequest_OpenAIFormat_NoMessages(t *testing.T) {
	body := map[string]any{"model": "gpt-4"}
	_, errPayload := shared.ParseDownstreamChatRequest(body, shared.FormatOpenAI)
	if errPayload == nil {
		t.Fatal("expected error for missing messages")
	}
}

func TestParseDownstreamChatRequest_ClaudeFormat_WithToolUse(t *testing.T) {
	claudeBody := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "tool_use", "id": "toolu_01", "name": "get_weather", "input": map[string]any{"city": "SF"}},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "toolu_01", "content": "Sunny"},
				},
			},
		},
	}

	parsed, errPayload := shared.ParseDownstreamChatRequest(claudeBody, shared.FormatClaude)
	if errPayload != nil {
		t.Fatalf("parse: %v", errPayload)
	}

	msgs, ok := parsed.UpstreamBody["messages"].([]map[string]any)
	if !ok {
		t.Fatal("messages missing")
	}

	hasToolCall := false
	hasToolResult := false
	for _, msg := range msgs {
		if _, ok := msg["tool_calls"]; ok {
			hasToolCall = true
		}
		if msg["role"] == "tool" {
			hasToolResult = true
		}
	}
	if !hasToolCall {
		t.Error("expected assistant tool_calls")
	}
	if !hasToolResult {
		t.Error("expected tool message")
	}
}

func TestParseDownstreamChatRequest_ClaudeFormat_SystemArray(t *testing.T) {
	claudeBody := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"system": []any{
			map[string]any{"type": "text", "text": "You are helpful."},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
	}

	parsed, errPayload := shared.ParseDownstreamChatRequest(claudeBody, shared.FormatClaude)
	if errPayload != nil {
		t.Fatalf("parse: %v", errPayload)
	}
	if parsed == nil {
		t.Fatal("expected parsed result")
	}
}

// ---------------------------------------------------------------------------
// JSON roundtrip sanity
// ---------------------------------------------------------------------------

func TestAnthropicBodyJSONRoundtrip(t *testing.T) {
	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored map[string]any
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored["model"] != "claude-sonnet-4-20250514" {
		t.Error("model mismatch after JSON roundtrip")
	}
}


// ---------------------------------------------------------------------------
// Skill-call multi-turn fixtures (GitHub #51 / upstream #531)
// ---------------------------------------------------------------------------

func skillToolDefinition() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "Skill",
			"description": "Load a Claude Code skill by name",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill": map[string]any{"type": "string"},
				},
				"required": []any{"skill"},
			},
		},
	}
}

func TestConvertOpenAiBodyToAnthropic_SkillCallPreservesInput(t *testing.T) {
	openaiBody := map[string]any{
		"model": "gpt-4.1",
		"messages": []any{
			map[string]any{"role": "user", "content": "Use the superpowers skill"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_skill_01",
						"type": "function",
						"function": map[string]any{
							"name":      "Skill",
							"arguments": `{"skill":"superpowers:using-superpowers"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_skill_01",
				"content":      "Skill loaded: superpowers:using-superpowers",
			},
			map[string]any{"role": "user", "content": "Continue with the skill"},
		},
		"tools": []any{skillToolDefinition()},
	}

	body, err := ConvertOpenAiBodyToAnthropicMessagesBody(openaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	msgs := asMessageMaps(body["messages"])
	if len(msgs) < 3 {
		t.Fatalf("expected multi-turn messages, got %d: %#v", len(msgs), body["messages"])
	}

	var foundSkillToolUse bool
	for _, msg := range msgs {
		if msg["role"] != "assistant" {
			continue
		}
		for _, block := range asBlockMaps(msg["content"]) {
			if block["type"] != "tool_use" || block["name"] != "Skill" {
				continue
			}
			foundSkillToolUse = true
			if block["id"] != "call_skill_01" {
				t.Errorf("expected tool_use id call_skill_01, got %v", block["id"])
			}
			input, _ := block["input"].(map[string]any)
			if input == nil {
				t.Fatalf("expected Skill input map, got %#v", block["input"])
			}
			if input["skill"] != "superpowers:using-superpowers" {
				t.Errorf("expected skill name preserved, got %#v", input)
			}
		}
	}
	if !foundSkillToolUse {
		t.Fatal("expected Skill tool_use block in assistant message")
	}

	var foundToolResult bool
	for _, msg := range msgs {
		if msg["role"] != "user" {
			continue
		}
		for _, block := range asBlockMaps(msg["content"]) {
			if block["type"] != "tool_result" {
				continue
			}
			foundToolResult = true
			if block["tool_use_id"] != "call_skill_01" {
				t.Errorf("expected tool_use_id call_skill_01, got %v", block["tool_use_id"])
			}
			if block["content"] != "Skill loaded: superpowers:using-superpowers" {
				t.Errorf("unexpected tool_result content: %#v", block["content"])
			}
		}
	}
	if !foundToolResult {
		t.Fatal("expected tool_result linked to Skill tool_use")
	}

	tools, _ := body["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("expected tools on anthropic body")
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "Skill" {
		t.Errorf("expected Skill tool, got %#v", tool["name"])
	}
	schema, _ := tool["input_schema"].(map[string]any)
	if schema == nil {
		t.Fatal("expected input_schema")
	}
	required, _ := schema["required"].([]any)
	if len(required) == 0 || required[0] != "skill" {
		t.Errorf("expected required=[skill], got %#v", schema["required"])
	}
}

func TestConvertOpenAiBodyToAnthropic_SkillCallObjectArguments(t *testing.T) {
	openaiBody := map[string]any{
		"model": "gpt-4.1",
		"messages": []any{
			map[string]any{"role": "user", "content": "Load skill"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_skill_obj",
						"type": "function",
						"function": map[string]any{
							"name": "Skill",
							"arguments": map[string]any{
								"skill": "docs:writing-docs",
							},
						},
					},
				},
			},
		},
	}
	body, err := ConvertOpenAiBodyToAnthropicMessagesBody(openaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	found := false
	for _, msg := range asMessageMaps(body["messages"]) {
		if msg["role"] != "assistant" {
			continue
		}
		for _, block := range asBlockMaps(msg["content"]) {
			if block["type"] == "tool_use" && block["name"] == "Skill" {
				found = true
				input, _ := block["input"].(map[string]any)
				if input["skill"] != "docs:writing-docs" {
					t.Errorf("object arguments not preserved: %#v", block["input"])
				}
			}
		}
	}
	if !found {
		t.Fatal("expected Skill tool_use from object arguments")
	}
}

func TestConvertOpenAiBodyToAnthropic_SkillCallEmptyArgumentsResidual(t *testing.T) {
	openaiBody := map[string]any{
		"model": "gpt-4.1",
		"messages": []any{
			map[string]any{"role": "user", "content": "Use skill"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_skill_empty",
						"type": "function",
						"function": map[string]any{
							"name":      "Skill",
							"arguments": "{}",
						},
					},
				},
			},
		},
	}
	body, err := ConvertOpenAiBodyToAnthropicMessagesBody(openaiBody, "claude-sonnet-4-20250514", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	found := false
	for _, msg := range asMessageMaps(body["messages"]) {
		if msg["role"] != "assistant" {
			continue
		}
		for _, block := range asBlockMaps(msg["content"]) {
			if block["type"] == "tool_use" && block["name"] == "Skill" {
				found = true
				input, _ := block["input"].(map[string]any)
				if input == nil {
					t.Fatalf("expected empty map input, got %#v", block["input"])
				}
				if len(input) != 0 {
					t.Errorf("expected empty input object residual, got %#v", input)
				}
			}
		}
	}
	if !found {
		t.Fatal("expected Skill tool_use even with empty arguments")
	}
}

func TestParseDownstreamChatRequest_ClaudeSkillMultiTurn(t *testing.T) {
	claudeBody := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"tools": []any{
			map[string]any{
				"name":        "Skill",
				"description": "Load a Claude Code skill",
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"skill": map[string]any{"type": "string"},
					},
					"required": []any{"skill"},
				},
			},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "Use superpowers"},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_skill_01",
						"name":  "Skill",
						"input": map[string]any{"skill": "superpowers:using-superpowers"},
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_skill_01",
						"content":     "loaded",
					},
				},
			},
		},
	}

	parsed, errPayload := shared.ParseDownstreamChatRequest(claudeBody, shared.FormatClaude)
	if errPayload != nil {
		t.Fatalf("parse: %v", errPayload)
	}
	msgs, ok := parsed.UpstreamBody["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages missing: %#v", parsed.UpstreamBody["messages"])
	}

	var skillCallID, skillArgs string
	var toolResultID, toolResultContent string
	for _, msg := range msgs {
		switch tcs := msg["tool_calls"].(type) {
		case []map[string]any:
			for _, tc := range tcs {
				fn, _ := tc["function"].(map[string]any)
				if fn != nil && fn["name"] == "Skill" {
					skillCallID = shared.AsTrimmedString(tc["id"])
					skillArgs = shared.AsTrimmedString(fn["arguments"])
				}
			}
		case []any:
			for _, raw := range tcs {
				tc, _ := raw.(map[string]any)
				fn, _ := tc["function"].(map[string]any)
				if fn != nil && fn["name"] == "Skill" {
					skillCallID = shared.AsTrimmedString(tc["id"])
					skillArgs = shared.AsTrimmedString(fn["arguments"])
				}
			}
		}
		if msg["role"] == "tool" {
			toolResultID = shared.AsTrimmedString(msg["tool_call_id"])
			toolResultContent = shared.AsTrimmedString(msg["content"])
		}
	}
	if skillCallID != "toolu_skill_01" {
		t.Errorf("expected skill call id preserved, got %q", skillCallID)
	}
	if skillArgs == "" || skillArgs == "{}" {
		t.Errorf("expected Skill arguments JSON, got %q", skillArgs)
	}
	if !json.Valid([]byte(skillArgs)) {
		t.Fatalf("skill args not valid JSON: %q", skillArgs)
	}
	var parsedArgs map[string]any
	if err := json.Unmarshal([]byte(skillArgs), &parsedArgs); err != nil {
		t.Fatalf("unmarshal skill args: %v", err)
	}
	if parsedArgs["skill"] != "superpowers:using-superpowers" {
		t.Errorf("skill argument lost: %#v", parsedArgs)
	}
	if toolResultID != "toolu_skill_01" {
		t.Errorf("tool_result linkage broken: %q", toolResultID)
	}
	if toolResultContent != "loaded" {
		t.Errorf("tool_result content lost: %q", toolResultContent)
	}

	tools := parsed.UpstreamBody["tools"]
	arr, _ := tools.([]any)
	if len(arr) == 0 {
		t.Fatal("expected tools on OpenAI body")
	}
	tool, _ := arr[0].(map[string]any)
	fn, _ := tool["function"].(map[string]any)
	params, _ := fn["parameters"].(map[string]any)
	required, _ := params["required"].([]any)
	if len(required) == 0 || required[0] != "skill" {
		t.Errorf("expected required skill on OpenAI tools, got %#v", params)
	}
}

func TestSerializeFinalResponse_SkillToolUseInput(t *testing.T) {
	normalized := shared.NormalizedFinalResponse{
		ID:           "chatcmpl-skill",
		Model:        "gpt-4.1",
		Created:      1,
		FinishReason: "tool_calls",
		ToolCalls: []shared.ToolCall{
			{
				ID:        "call_skill_stream",
				Name:      "Skill",
				Arguments: `{"skill":"superpowers:using-superpowers"}`,
			},
		},
	}
	result := shared.SerializeFinalResponse(shared.FormatClaude, normalized, struct {
		PromptTokens, CompletionTokens, TotalTokens int
	}{10, 5, 15})
	content := asBlockMaps(result["content"])
	if len(content) == 0 {
		t.Fatalf("expected content blocks, got %#v", result["content"])
	}
	block := content[len(content)-1]
	if block["type"] != "tool_use" || block["name"] != "Skill" {
		t.Fatalf("expected Skill tool_use, got %#v", block)
	}
	input, _ := block["input"].(map[string]any)
	if input["skill"] != "superpowers:using-superpowers" {
		t.Errorf("skill input lost in Claude final serialize: %#v", block["input"])
	}
}

func asMessageMaps(v any) []map[string]any {
	switch msgs := v.(type) {
	case []map[string]any:
		return msgs
	case []any:
		out := make([]map[string]any, 0, len(msgs))
		for _, item := range msgs {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func asBlockMaps(v any) []map[string]any {
	switch content := v.(type) {
	case []map[string]any:
		return content
	case []any:
		out := make([]map[string]any, 0, len(content))
		for _, item := range content {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

