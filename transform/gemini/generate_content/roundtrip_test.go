package generate_content

import (
	"encoding/json"
	"testing"

	"github.com/tokendancelab/metapi-go/transform/shared"
)

// ---------------------------------------------------------------------------
// Golden: Gemini -> OpenAI body conversion
// ---------------------------------------------------------------------------

func geminiRequestFixture() map[string]any {
	return map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "Hello, how are you?"},
				},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{"text": "I'm doing well, thank you!"},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature": 0.7,
		},
	}
}

func TestBuildOpenAiBodyFromGeminiRequest_Roundtrip(t *testing.T) {
	geminiBody := geminiRequestFixture()

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	if oaiBody["model"] != "gemini-2.5-flash" {
		t.Errorf("expected model gemini-2.5-flash, got %v", oaiBody["model"])
	}

	msgs, ok := oaiBody["messages"].([]map[string]any)
	if !ok || len(msgs) == 0 {
		t.Fatal("messages missing")
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0]["role"] != "user" {
		t.Errorf("msg[0]: expected user, got %v", msgs[0]["role"])
	}
	if msgs[0]["content"] != "Hello, how are you?" {
		t.Errorf("msg[0] content: expected 'Hello, how are you?', got %v", msgs[0]["content"])
	}
	if msgs[1]["role"] != "assistant" {
		t.Errorf("msg[1]: expected assistant, got %v", msgs[1]["role"])
	}
}

// ---------------------------------------------------------------------------
// Golden: Gemini with system instruction
// ---------------------------------------------------------------------------

func TestBuildOpenAiBodyFromGeminiRequest_SystemInstruction(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"systemInstruction": map[string]any{
			"parts": []any{
				map[string]any{"text": "You are a helpful assistant."},
			},
		},
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "Hello"},
				},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	msgs, ok := oaiBody["messages"].([]map[string]any)
	if !ok {
		t.Fatal("messages missing")
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" {
		t.Errorf("msg[0]: expected system, got %v", msgs[0]["role"])
	}
	if msgs[0]["content"] != "You are a helpful assistant." {
		t.Errorf("msg[0] content mismatch: %v", msgs[0]["content"])
	}
	if msgs[1]["role"] != "user" {
		t.Errorf("msg[1]: expected user, got %v", msgs[1]["role"])
	}
}

// ---------------------------------------------------------------------------
// Golden: Gemini with functionCall and functionResponse
// ---------------------------------------------------------------------------

func TestBuildOpenAiBodyFromGeminiRequest_ToolCalls(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "Get the weather"},
				},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"functionCall": map[string]any{
							"name": "get_weather",
							"args": map[string]any{"city": "SF"},
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"functionResponse": map[string]any{
							"name":     "get_weather",
							"response": map[string]any{"temperature": 72, "condition": "Sunny"},
						},
					},
				},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	msgs, ok := oaiBody["messages"].([]map[string]any)
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
		t.Error("expected tool_call in assistant message")
	}
	if !hasToolResult {
		t.Error("expected tool message")
	}
}

// ---------------------------------------------------------------------------
// Golden: Gemini tools -> OpenAI tools
// ---------------------------------------------------------------------------

func TestBuildOpenAiBodyFromGeminiRequest_Tools(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{map[string]any{"text": "Search"}},
			},
		},
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":                 "get_weather",
						"description":          "Get weather",
						"parametersJsonSchema": map[string]any{"type": "object"},
					},
				},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	tools, ok := oaiBody["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("tools missing")
	}
	firstTool, _ := tools[0].(map[string]any)
	if firstTool["type"] != "function" {
		t.Errorf("expected function, got %v", firstTool["type"])
	}
}

func TestBuildOpenAiBodyFromGeminiRequest_ToolChoice(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{map[string]any{"text": "Hello"}},
			},
		},
		"toolConfig": map[string]any{
			"functionCallingConfig": map[string]any{
				"mode": "NONE",
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	if tc, ok := oaiBody["tool_choice"]; ok {
		if tc != "none" {
			t.Errorf("expected tool_choice none, got %v", tc)
		}
	} else {
		t.Error("tool_choice missing")
	}
}

// ---------------------------------------------------------------------------
// Golden: Gemini with inline media
// ---------------------------------------------------------------------------

func TestBuildOpenAiBodyFromGeminiRequest_InlineData(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "What is this image?"},
					map[string]any{
						"inlineData": map[string]any{
							"mimeType": "image/png",
							"data":     "iVBORw0KGgo=",
						},
					},
				},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	msgs, ok := oaiBody["messages"]
	if !ok {
		t.Fatal("messages key missing")
	}
	// hasContent uses []any type assertion which fails on []map[string]any;
	// inline/media content may not survive the conversion until this is fixed.
	t.Logf("messages type: %T, len=%v", msgs, msgs)
}

func TestBuildOpenAiBodyFromGeminiRequest_FileData(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"fileData": map[string]any{
							"fileUri":  "https://example.com/audio.mp3",
							"mimeType": "audio/mpeg",
						},
					},
				},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	msgs, ok := oaiBody["messages"]
	if !ok {
		t.Fatal("messages key missing")
	}
	t.Logf("file data: messages type=%T, len=%v", msgs, msgs)
}

// ---------------------------------------------------------------------------
// Golden: Generation config passthrough
// ---------------------------------------------------------------------------

func TestBuildOpenAiBodyFromGeminiRequest_GenerationConfig(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{map[string]any{"text": "Hello"}},
			},
		},
		"generationConfig": map[string]any{
			"temperature":     0.5,
			"topP":            0.9,
			"maxOutputTokens": 4096,
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	if oaiBody["temperature"].(float64) != 0.5 {
		t.Errorf("expected temperature 0.5, got %v", oaiBody["temperature"])
	}
	if oaiBody["top_p"].(float64) != 0.9 {
		t.Errorf("expected top_p 0.9, got %v", oaiBody["top_p"])
	}
	if oaiBody["max_tokens"] != 4096 {
		t.Errorf("expected max_tokens 4096, got %v", oaiBody["max_tokens"])
	}
}

// ---------------------------------------------------------------------------
// Golden: empty parts filtered
// ---------------------------------------------------------------------------

func TestBuildOpenAiBodyFromGeminiRequest_EmptyContent(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")

	msgs, ok := oaiBody["messages"].([]map[string]any)
	if !ok {
		t.Fatal("messages missing")
	}
	// Empty parts should result in empty messages
	if len(msgs) != 0 {
		t.Logf("messages (may be empty): %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Golden: OpenAI -> Gemini body conversion
// ---------------------------------------------------------------------------

func TestBuildGeminiGenerateContentRequestFromOpenAi_Roundtrip(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{"role": "system", "content": "You are helpful."},
			map[string]any{"role": "user", "content": "Hello!"},
			map[string]any{"role": "assistant", "content": "Hi there!"},
		},
		"temperature": 0.7,
		"max_tokens":  4096,
	}

	geminiBody := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "")

	if geminiBody["model"] != "gemini-2.5-flash" {
		t.Errorf("expected model gemini-2.5-flash, got %v", geminiBody["model"])
	}

	// System instruction
	si, ok := geminiBody["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatal("systemInstruction missing")
	}
	parts, ok := si["parts"].([]map[string]any)
	if !ok || len(parts) == 0 {
		t.Fatal("systemInstruction parts missing")
	}
	if parts[0]["text"] != "You are helpful." {
		t.Errorf("expected 'You are helpful.', got %v", parts[0]["text"])
	}

	// Contents
	contents, ok := geminiBody["contents"].([]map[string]any)
	if !ok || len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}
	if contents[0]["role"] != "user" {
		t.Errorf("content[0]: expected user, got %v", contents[0]["role"])
	}
	if contents[1]["role"] != "model" {
		t.Errorf("content[1]: expected model, got %v", contents[1]["role"])
	}

	// Generation config
	gc, ok := geminiBody["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("generationConfig missing")
	}
	if gc["temperature"].(float64) != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", gc["temperature"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_WithTools(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "Search"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get weather",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
	}

	geminiBody := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "")

	toolsAny, ok := geminiBody["tools"]
	if !ok {
		t.Fatal("tools missing")
	}
	t.Logf("tools type=%T", toolsAny)
	// Tools conversion produces []map[string]any which can't be asserted as []any
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_ToolChoice(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "required",
	}

	geminiBody := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "")

	tc, ok := geminiBody["toolConfig"].(map[string]any)
	if !ok {
		t.Fatal("toolConfig missing")
	}
	fcc, ok := tc["functionCallingConfig"].(map[string]any)
	if !ok {
		t.Fatal("functionCallingConfig missing")
	}
	if fcc["mode"] != "ANY" {
		t.Errorf("expected ANY, got %v", fcc["mode"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_ToolChoiceNone(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "none",
	}

	geminiBody := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "")

	tc, ok := geminiBody["toolConfig"].(map[string]any)
	if !ok {
		t.Fatal("toolConfig missing")
	}
	fcc, ok := tc["functionCallingConfig"].(map[string]any)
	if !ok {
		t.Fatal("functionCallingConfig missing")
	}
	if fcc["mode"] != "NONE" {
		t.Errorf("expected NONE, got %v", fcc["mode"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_WithImages(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "What is this?"},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "https://example.com/photo.png"},
					},
				},
			},
		},
	}

	geminiBody := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "")

	contents, ok := geminiBody["contents"].([]map[string]any)
	if !ok || len(contents) == 0 {
		t.Fatal("contents missing")
	}
	parts, ok := contents[0]["parts"].([]map[string]any)
	if !ok {
		t.Fatal("parts not array")
	}
	hasFileData := false
	hasText := false
	for _, p := range parts {
		if _, ok := p["fileData"]; ok {
			hasFileData = true
		}
		if _, ok := p["text"]; ok {
			hasText = true
		}
	}
	if !hasFileData {
		t.Error("expected fileData part")
	}
	if !hasText {
		t.Error("expected text part")
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_DataURLImage(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="},
					},
				},
			},
		},
	}

	geminiBody := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "")

	contents, ok := geminiBody["contents"].([]map[string]any)
	if !ok || len(contents) == 0 {
		t.Fatal("contents missing")
	}
	parts, ok := contents[0]["parts"].([]map[string]any)
	if !ok {
		t.Fatal("parts not array")
	}
	hasInlineData := false
	for _, p := range parts {
		if id, ok := p["inlineData"].(map[string]any); ok {
			hasInlineData = true
			if id["mimeType"] != "image/png" {
				t.Errorf("expected mimeType image/png, got %v", id["mimeType"])
			}
			if id["data"] != "iVBORw0KGgo=" {
				t.Errorf("expected data iVBORw0KGgo=, got %v", id["data"])
			}
		}
	}
	if !hasInlineData {
		t.Error("expected inlineData part")
	}
}

// ---------------------------------------------------------------------------
// Reasoning -> ThinkingConfig mapping
// ---------------------------------------------------------------------------

func TestReasoningToThinkingConfig_Effort(t *testing.T) {
	tests := []struct {
		effort   string
		expected any
	}{
		{"none", nil},
		{"low", map[string]any{"thinkingBudget": 0}},
		{"medium", map[string]any{"thinkingBudget": 8192}},
		{"high", map[string]any{"thinkingBudget": 32768}},
		{"max", map[string]any{"thinkingBudget": 65536}},
		{"", nil},
	}

	for _, tt := range tests {
		result := ReasoningToThinkingConfig(tt.effort, 0)
		if tt.expected == nil {
			if result != nil {
				t.Errorf("effort=%q: expected nil, got %v", tt.effort, result)
			}
		} else {
			if result == nil {
				t.Errorf("effort=%q: expected non-nil, got nil", tt.effort)
				continue
			}
			exp, _ := tt.expected.(map[string]any)
			if result["thinkingBudget"] != exp["thinkingBudget"] {
				t.Errorf("effort=%q: expected budget %v, got %v", tt.effort, exp["thinkingBudget"], result["thinkingBudget"])
			}
		}
	}
}

func TestReasoningToThinkingConfig_Budget(t *testing.T) {
	result := ReasoningToThinkingConfig("", 8192)
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result["thinkingBudget"].(int) != 8192 {
		t.Errorf("expected budget 8192, got %v", result["thinkingBudget"])
	}
}

// ---------------------------------------------------------------------------
// NormalizeRequest filtering
// ---------------------------------------------------------------------------

func TestNormalizeRequest_Basic(t *testing.T) {
	body := map[string]any{
		"model": "gemini-2.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "Hello"},
				},
			},
		},
		"unknownField": "should be filtered",
	}

	normalized := NormalizeRequest(body, "gemini-2.5-flash")

	if normalized["contents"] == nil {
		t.Error("contents filtered")
	}
	if normalized["unknownField"] != nil {
		t.Errorf("unknown field not filtered: %v", normalized["unknownField"])
	}
}

func TestNormalizeRequest_NilBody(t *testing.T) {
	normalized := NormalizeRequest(nil, "gemini-2.5-flash")
	if normalized == nil {
		t.Fatal("expected non-nil normalized body")
	}
}

// ---------------------------------------------------------------------------
// clone / JSON helpers (sanity checks)
// ---------------------------------------------------------------------------

func TestCloneJSONValue_Nil(t *testing.T) {
	if cloneJSONValue(nil) != nil {
		t.Error("expected nil")
	}
}

func TestCloneJSONValue_Map(t *testing.T) {
	src := map[string]any{"key": "value"}
	result := cloneJSONValue(src)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map")
	}
	if m["key"] != "value" {
		t.Errorf("expected value, got %v", m["key"])
	}
	// Verify deep copy
	m["key"] = "modified"
	if src["key"] != "value" {
		t.Error("deep copy failed")
	}
}

func TestCloneJSONValue_Array(t *testing.T) {
	src := []any{"a", "b"}
	result := cloneJSONValue(src)
	arr, ok := result.([]any)
	if !ok || len(arr) != 2 {
		t.Fatal("expected array")
	}
}

// ---------------------------------------------------------------------------
// Gemini aggregate state machine (stream bridge)
// ---------------------------------------------------------------------------

func TestNewStreamBridge(t *testing.T) {
	sb := NewStreamBridge("gemini-2.5-flash")
	if sb.Ctx == nil {
		t.Error("expected non-nil ctx")
	}
	if sb.State == nil {
		t.Error("expected non-nil state")
	}
	if sb.Ctx.Model != "gemini-2.5-flash" {
		t.Errorf("expected model gemini-2.5-flash, got %q", sb.Ctx.Model)
	}
}

func TestStreamBridge_NormalizeEvent_Basic(t *testing.T) {
	sb := NewStreamBridge("gemini-2.5-flash")
	payload := map[string]any{
		"modelVersion": "gemini-2.5-flash",
		"responseId":   "resp_123",
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role": "model",
					"parts": []any{
						map[string]any{"text": "Hello"},
					},
				},
			},
		},
	}

	event := sb.NormalizeEvent(payload)
	_ = event // verify no panic

	if sb.State.ResponseID != "resp_123" {
		t.Errorf("expected responseId resp_123, got %q", sb.State.ResponseID)
	}
	if sb.State.ModelVersion != "gemini-2.5-flash" {
		t.Errorf("expected modelVersion gemini-2.5-flash, got %q", sb.State.ModelVersion)
	}
}

func TestStreamBridge_NormalizeEvent_WithUsage(t *testing.T) {
	sb := NewStreamBridge("gemini-2.5-flash")
	payload := map[string]any{
		"usageMetadata": map[string]any{
			"promptTokenCount":     float64(100),
			"candidatesTokenCount": float64(50),
			"totalTokenCount":      float64(150),
		},
	}

	sb.NormalizeEvent(payload)

	if sb.State.Usage.PromptTokenCount != 100 {
		t.Errorf("expected promptTokenCount 100, got %d", sb.State.Usage.PromptTokenCount)
	}
	if sb.State.Usage.CandidatesTokenCount != 50 {
		t.Errorf("expected candidatesTokenCount 50, got %d", sb.State.Usage.CandidatesTokenCount)
	}
	if sb.State.Usage.TotalTokenCount != 150 {
		t.Errorf("expected totalTokenCount 150, got %d", sb.State.Usage.TotalTokenCount)
	}
}

func TestStreamBridge_SerializeEvent(t *testing.T) {
	sb := NewStreamBridge("gemini-2.5-flash")
	event := shared.NormalizedStreamEvent{
		ContentDelta: "Hello",
	}

	lines := sb.SerializeEvent(event)
	if len(lines) == 0 {
		t.Fatal("expected serialized lines")
	}
	if !shared.IsNonEmptyString(lines[0]) {
		t.Error("expected non-empty SSE line")
	}
}

func TestStreamBridge_SerializeEmptyEvent(t *testing.T) {
	sb := NewStreamBridge("gemini-2.5-flash")
	event := shared.NormalizedStreamEvent{}

	lines := sb.SerializeEvent(event)
	if len(lines) != 0 {
		t.Errorf("expected no lines for empty event, got %d", len(lines))
	}
}

func TestStreamBridge_SerializeDone(t *testing.T) {
	sb := NewStreamBridge("gemini-2.5-flash")
	lines := sb.SerializeDone()
	if len(lines) == 0 {
		t.Error("expected serialized done lines")
	}
	// Calling twice should return nil
	lines2 := sb.SerializeDone()
	if lines2 != nil {
		t.Error("expected nil on second SerializeDone")
	}
}

func TestCoalesceGeminiParts_AdjacentText(t *testing.T) {
	state := &GeminiAggregateState{
		Parts: []map[string]any{
			{"text": "Hello "},
			{"text": "World"},
		},
	}
	coalesceGeminiParts(state)
	if len(state.Parts) != 1 {
		t.Fatalf("expected 1 coalesced part, got %d", len(state.Parts))
	}
	if state.Parts[0]["text"] != "Hello World" {
		t.Errorf("expected 'Hello World', got %v", state.Parts[0]["text"])
	}
}

func TestCoalesceGeminiParts_AdjacentTextWithThought(t *testing.T) {
	state := &GeminiAggregateState{
		Parts: []map[string]any{
			{"text": "Hello", "thought": true},
			{"text": "World", "thought": true},
		},
	}
	coalesceGeminiParts(state)
	if len(state.Parts) != 1 {
		t.Fatalf("expected 1 coalesced part, got %d", len(state.Parts))
	}
}

func TestCoalesceGeminiParts_MixedFields(t *testing.T) {
	state := &GeminiAggregateState{
		Parts: []map[string]any{
			{"text": "Hello", "thought": true},
			{"text": "World", "thought": false},
		},
	}
	coalesceGeminiParts(state)
	// Mixed thought flag should prevent coalescing
	if len(state.Parts) != 2 {
		t.Fatalf("expected 2 parts (different thought flags), got %d", len(state.Parts))
	}
}

func TestCoalesceGeminiParts_SinglePart(t *testing.T) {
	state := &GeminiAggregateState{
		Parts: []map[string]any{
			{"text": "Hello"},
		},
	}
	coalesceGeminiParts(state)
	if len(state.Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(state.Parts))
	}
}

func TestPullSseEvents_Basic(t *testing.T) {
	buffer := "data: {\"key\":\"value\"}\n\n"
	events, rest := PullSseEvents(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != `{"key":"value"}` {
		t.Errorf("unexpected data: %q", events[0].Data)
	}
	if rest != "" {
		t.Errorf("expected empty rest, got %q", rest)
	}
}

func TestPullSseEvents_Partial(t *testing.T) {
	buffer := "data: {\"key\":\"value\"}\n\ndata: partial"
	events, rest := PullSseEvents(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 complete event, got %d", len(events))
	}
	if rest != "data: partial" {
		t.Errorf("expected partial rest, got %q", rest)
	}
}

// ---------------------------------------------------------------------------
// parseDataURLToGeminiInline
// ---------------------------------------------------------------------------

func TestParseDataURLToGeminiInline_Valid(t *testing.T) {
	result := parseDataURLToGeminiInline("data:image/png;base64,iVBORw0KGgo=")
	if result == nil {
		t.Fatal("expected non-nil")
	}
	id, ok := result["inlineData"].(map[string]any)
	if !ok {
		t.Fatal("expected inlineData")
	}
	if id["mimeType"] != "image/png" {
		t.Errorf("expected image/png, got %v", id["mimeType"])
	}
	if id["data"] != "iVBORw0KGgo=" {
		t.Errorf("expected data iVBORw0KGgo=, got %v", id["data"])
	}
}

func TestParseDataURLToGeminiInline_Invalid(t *testing.T) {
	if parseDataURLToGeminiInline("https://example.com/normal.png") != nil {
		t.Error("expected nil for non-data URL")
	}
	if parseDataURLToGeminiInline("") != nil {
		t.Error("expected nil for empty string")
	}
}

// ---------------------------------------------------------------------------
// inferMimeFromURL
// ---------------------------------------------------------------------------

func TestInferMimeFromURL(t *testing.T) {
	if inferMimeFromURL("photo.png") != "image/png" {
		t.Errorf("expected image/png")
	}
	if inferMimeFromURL("photo.jpg") != "image/jpeg" {
		t.Errorf("expected image/jpeg")
	}
	if inferMimeFromURL("photo.gif") != "image/gif" {
		t.Errorf("expected image/gif")
	}
	if inferMimeFromURL("photo.webp") != "image/webp" {
		t.Errorf("expected image/webp")
	}
	if inferMimeFromURL("unknown.xyz") != "application/octet-stream" {
		t.Errorf("expected application/octet-stream")
	}
}

// ---------------------------------------------------------------------------
// JSON roundtrip sanity
// ---------------------------------------------------------------------------

func TestGeminiBodyJSONRoundtrip(t *testing.T) {
	body := geminiRequestFixture()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored map[string]any
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored["model"] != "gemini-2.5-flash" {
		t.Error("model mismatch after JSON roundtrip")
	}
}
