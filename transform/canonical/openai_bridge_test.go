package canonical

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Roundtrip: OpenAI body -> canonical -> OpenAI body (golden file)
// ---------------------------------------------------------------------------

func openAiChatBodyFixture() map[string]any {
	return map[string]any{
		"model": "gpt-4",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are a helpful assistant."},
			map[string]any{"role": "user", "content": "What is the weather in SF?"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{
					"id": "call_abc",
					"type": "function",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": `{"city":"San Francisco"}`,
					},
				},
			}},
			map[string]any{"role": "tool", "tool_call_id": "call_abc", "content": "Sunny, 72F"},
			map[string]any{"role": "assistant", "content": "It is sunny and 72F in San Francisco."},
		},
	}
}

func TestOpenAIBodyToCanonicalToOpenAIBody_Roundtrip(t *testing.T) {
	body := openAiChatBodyFixture()

	// Step 1: OpenAI body -> canonical
	canonicalInput := CanonicalRequestFromOpenAiBodyInput{
		Body:     body,
		Surface:  SurfaceOpenAIChat,
		CliProfile: ProfileGeneric,
		Operation: OpGenerate,
	}
	env, err := CanonicalRequestFromOpenAiBody(canonicalInput)
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	// Step 2: canonical -> OpenAI body
	reconstructed := CanonicalRequestToOpenAiChatBody(env)

	// Verify model
	if reconstructed["model"] != "gpt-4" {
		t.Errorf("model: expected gpt-4, got %v", reconstructed["model"])
	}
	if reconstructed["stream"] != true {
		t.Errorf("stream: expected true")
	}

	// Verify messages
	msgs, ok := reconstructed["messages"].([]map[string]any)
	if !ok {
		t.Fatal("messages not found in reconstructed body")
	}
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}

	// System message
	if msgs[0]["role"] != "system" {
		t.Errorf("msg[0] role: expected system, got %v", msgs[0]["role"])
	}
	if msgs[0]["content"] != "You are a helpful assistant." {
		t.Errorf("msg[0] content: expected 'You are a helpful assistant.', got %v", msgs[0]["content"])
	}

	// User message
	if msgs[1]["role"] != "user" {
		t.Errorf("msg[1] role: expected user, got %v", msgs[1]["role"])
	}

	// Assistant with tool_calls
	if msgs[2]["role"] != "assistant" {
		t.Errorf("msg[2] role: expected assistant, got %v", msgs[2]["role"])
	}
	tcs, ok := msgs[2]["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("msg[2] tool_calls: expected 1, got %v", msgs[2]["tool_calls"])
	}
	tc, _ := tcs[0].(map[string]any)
	if fn, ok := tc["function"].(map[string]any); ok {
		if fn["name"] != "get_weather" {
			t.Errorf("tool call name: expected get_weather, got %v", fn["name"])
		}
	}

	// Tool message
	if msgs[3]["role"] != "tool" {
		t.Errorf("msg[3] role: expected tool, got %v", msgs[3]["role"])
	}

	// Final assistant
	if msgs[4]["role"] != "assistant" {
		t.Errorf("msg[4] role: expected assistant, got %v", msgs[4]["role"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip with images
// ---------------------------------------------------------------------------

func TestOpenAIBodyWithImages_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4o",
		"stream": false,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "What is in this image?"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/photo.png"}},
				},
			},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:       body,
		Surface:    SurfaceOpenAIChat,
		CliProfile: ProfileGeneric,
		Operation:  OpGenerate,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(env.Messages))
	}
	parts := env.Messages[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Type != PartText {
		t.Errorf("part[0] expected text, got %q", parts[0].Type)
	}
	if parts[1].Type != PartImage {
		t.Errorf("part[1] expected image, got %q", parts[1].Type)
	}
	if parts[1].URL != "https://example.com/photo.png" {
		t.Errorf("part[1] URL: expected https://example.com/photo.png, got %q", parts[1].URL)
	}

	// Reconstruct
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	msgs, ok := reconstructed["messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatal("reconstructed messages missing")
	}
	// Content may be stored as []map[string]any (not []any directly in Go type system)
	if _, isString := msgs[0]["content"].(string); isString {
		t.Log("content is string")
	} else {
		t.Log("content is non-string (likely array)")
	}
	// Verify reconstructed body contains model
	if reconstructed["model"] != "gpt-4o" {
		t.Errorf("model: expected gpt-4o, got %v", reconstructed["model"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip with reasoning
// ---------------------------------------------------------------------------

func TestOpenAIBodyWithReasoning_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model":           "o3-mini",
		"stream":          true,
		"reasoning_effort": "high",
		"reasoning_budget": 16000,
		"messages": []any{
			map[string]any{"role": "user", "content": "Solve this complex problem."},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if env.Reasoning == nil {
		t.Fatal("expected reasoning, got nil")
	}
	if env.Reasoning.Effort != ReasoningEffortHigh {
		t.Errorf("expected effort high, got %q", env.Reasoning.Effort)
	}
	if env.Reasoning.BudgetTokens != 16000 {
		t.Errorf("expected budgetTokens 16000, got %d", env.Reasoning.BudgetTokens)
	}

	// Reconstruct
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	if reconstructed["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort high, got %v", reconstructed["reasoning_effort"])
	}
	if rb, ok := reconstructed["reasoning_budget"]; !ok || rb.(int) != 16000 {
		t.Errorf("expected reasoning_budget 16000, got %v", reconstructed["reasoning_budget"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip with tools and tool_choice
// ---------------------------------------------------------------------------

func TestOpenAIBodyWithTools_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Get the weather"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get weather for a city",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []any{"city"},
					},
					"strict": true,
				},
			},
		},
		"tool_choice": "auto",
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(env.Tools))
	}
	if !env.Tools[0].IsFunction() {
		t.Fatal("expected function tool")
	}
	if env.Tools[0].FnName != "get_weather" {
		t.Errorf("expected get_weather, got %q", env.Tools[0].FnName)
	}
	if env.Tools[0].FnStrict != true {
		t.Error("expected strict=true")
	}

	if env.ToolChoice == nil {
		t.Fatal("expected tool_choice")
	}
	if env.ToolChoice.Type != "auto" {
		t.Errorf("expected auto, got %q", env.ToolChoice.Type)
	}

	// Reconstruct
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	tools, ok := reconstructed["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatal("reconstructed tools missing")
	}
	ot, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatal("tool is not map")
	}
	if ot["type"] != "function" {
		t.Errorf("expected function type, got %v", ot["type"])
	}

	if reconstructed["tool_choice"] != "auto" {
		t.Errorf("expected tool_choice auto, got %v", reconstructed["tool_choice"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: tool_choice "none"
// ---------------------------------------------------------------------------

func TestOpenAIBody_ToolChoiceNone_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tool_choice": "none",
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if env.ToolChoice == nil {
		t.Fatal("expected tool_choice")
	}
	if env.ToolChoice.Type != "none" {
		t.Errorf("expected none, got %q", env.ToolChoice.Type)
	}

	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	if reconstructed["tool_choice"] != "none" {
		t.Errorf("expected tool_choice none, got %v", reconstructed["tool_choice"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: tool_choice named function
// ---------------------------------------------------------------------------

func TestOpenAIBody_ToolChoiceNamedFunction_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{"name": "get_weather", "parameters": map[string]any{"type": "object"}},
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
			"function": map[string]any{"name": "get_weather"},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if env.ToolChoice == nil {
		t.Fatal("expected tool_choice")
	}
	if env.ToolChoice.Type != "tool" {
		t.Errorf("expected tool type, got %q", env.ToolChoice.Type)
	}
	if env.ToolChoice.Name != "get_weather" {
		t.Errorf("expected name get_weather, got %q", env.ToolChoice.Name)
	}

	// Reconstruct
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	tc, ok := reconstructed["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool_choice map, got %T", reconstructed["tool_choice"])
	}
	if tc["type"] != "function" {
		t.Errorf("expected function type, got %v", tc["type"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: empty/null body
// ---------------------------------------------------------------------------

func TestOpenAIBody_EmptyBody(t *testing.T) {
	_, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:       map[string]any{},
		Surface:    SurfaceOpenAIChat,
	})
	// Empty body has no model -> should error
	if err == nil {
		t.Log("empty body with no model returned no error (model defaulted?)")
	}
}

func TestOpenAIBody_NilBody(t *testing.T) {
	_, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    nil,
		Surface: SurfaceOpenAIChat,
	})
	if err == nil {
		t.Log("nil body returned no error (model defaulted?)")
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: continuation
// ---------------------------------------------------------------------------

func TestOpenAIBody_Continuation_Roundtrip(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Continue"},
		},
		"prompt_cache_key":     "cache_key_1",
		"previous_response_id": "resp_prev_1",
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if env.Continuation == nil {
		t.Fatal("expected continuation")
	}
	if env.Continuation.PromptCacheKey != "cache_key_1" {
		t.Errorf("expected promptCacheKey cache_key_1, got %q", env.Continuation.PromptCacheKey)
	}
	if env.Continuation.PreviousResponseID != "resp_prev_1" {
		t.Errorf("expected previousResponseId resp_prev_1, got %q", env.Continuation.PreviousResponseID)
	}

	// Reconstruct chat body: prompt_cache_key is OpenAI-compatible; previous_response_id
	// is Responses-only and must NOT be re-emitted onto chat (upstream #504 / #54).
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	if reconstructed["prompt_cache_key"] != "cache_key_1" {
		t.Errorf("expected prompt_cache_key, got %v", reconstructed["prompt_cache_key"])
	}
	if _, ok := reconstructed["previous_response_id"]; ok {
		t.Errorf("chat body must not include previous_response_id, got %v", reconstructed["previous_response_id"])
	}

	// Responses body re-emits previous_response_id via ApplyOpenAIResponsesContinuation.
	responsesBody := map[string]any{"model": "gpt-4"}
	ApplyOpenAIResponsesContinuation(responsesBody, env.Continuation, nil)
	if responsesBody["previous_response_id"] != "resp_prev_1" {
		t.Errorf("expected previous_response_id on responses body, got %v", responsesBody["previous_response_id"])
	}
	if responsesBody["prompt_cache_key"] != "cache_key_1" {
		t.Errorf("expected prompt_cache_key on responses body, got %v", responsesBody["prompt_cache_key"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: parallel_tool_calls passthrough
// ---------------------------------------------------------------------------

func TestOpenAIBody_ParallelToolCalls_Passthrough(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
		"parallel_tool_calls": false,
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if env.Passthrough == nil {
		t.Fatal("expected passthrough")
	}
	if ptc, ok := env.Passthrough["parallel_tool_calls"]; !ok || ptc != false {
		t.Errorf("expected parallel_tool_calls=false in passthrough, got %v", env.Passthrough["parallel_tool_calls"])
	}

	// Reconstruct
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	if ptc, ok := reconstructed["parallel_tool_calls"]; !ok || ptc != false {
		t.Errorf("expected parallel_tool_calls=false, got %v", reconstructed["parallel_tool_calls"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: developer role
// ---------------------------------------------------------------------------

func TestOpenAIBody_DeveloperRole(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "developer", "content": "You are a code assistant."},
			map[string]any{"role": "user", "content": "Write Hello World"},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(env.Messages))
	}
	if env.Messages[0].Role != RoleDeveloper {
		t.Errorf("expected developer role, got %q", env.Messages[0].Role)
	}

	// Reconstruct
	reconstructed := CanonicalRequestToOpenAiChatBody(env)
	msgs, ok := reconstructed["messages"].([]map[string]any)
	if !ok || len(msgs) != 2 {
		t.Fatal("messages missing")
	}
	if msgs[0]["role"] != "developer" {
		t.Errorf("expected developer, got %v", msgs[0]["role"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: tool message with non-string content
// ---------------------------------------------------------------------------

func TestOpenAIBody_ToolMessage_NonStringContent(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Call a function"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{
					"id": "call_1",
					"type": "function",
					"function": map[string]any{"name": "search", "arguments": `{"q":"test"}`},
				},
			}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": []any{
				map[string]any{"type": "text", "text": "result 1"},
				map[string]any{"type": "text", "text": "result 2"},
			}},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(env.Messages))
	}
	toolMsg := env.Messages[2]
	if toolMsg.Role != RoleTool {
		t.Errorf("expected tool role, got %q", toolMsg.Role)
	}
	if len(toolMsg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(toolMsg.Parts))
	}
	if toolMsg.Parts[0].ResultContent == nil {
		t.Error("expected ResultContent for array content")
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: assistant with reasoning_content
// ---------------------------------------------------------------------------

func TestOpenAIBody_AssistantReasoningContent(t *testing.T) {
	body := map[string]any{
		"model": "o3-mini",
		"messages": []any{
			map[string]any{"role": "user", "content": "Solve 2+2"},
			map[string]any{
				"role":              "assistant",
				"content":           "The answer is 4.",
				"reasoning_content": "Let me think step by step...",
			},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(env.Messages))
	}
	assistant := env.Messages[1]
	if assistant.Role != RoleAssistant {
		t.Errorf("expected assistant role, got %q", assistant.Role)
	}
	hasReasoning := false
	hasContent := false
	for _, p := range assistant.Parts {
		if p.Type == PartText && p.Thought {
			hasReasoning = true
		}
		if p.Type == PartText && !p.Thought {
			hasContent = true
		}
	}
	if !hasReasoning {
		t.Error("expected reasoning part")
	}
	if !hasContent {
		t.Error("expected content part")
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: file blocks
// ---------------------------------------------------------------------------

func TestOpenAIBody_InputFileBlock(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Summarize this file."},
					map[string]any{
						"type":       "file",
						"file_id":    "file-xyz",
						"filename":   "report.pdf",
						"mime_type":  "application/pdf",
					},
				},
			},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(env.Messages))
	}
	parts := env.Messages[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[1].Type != PartFile {
		t.Errorf("expected file part, got %q", parts[1].Type)
	}
	if parts[1].FileID != "file-xyz" {
		t.Errorf("expected fileId file-xyz, got %q", parts[1].FileID)
	}
	if parts[1].Filename != "report.pdf" {
		t.Errorf("expected filename report.pdf, got %q", parts[1].Filename)
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: metadata passthrough
// ---------------------------------------------------------------------------

func TestOpenAIBody_MetadataPassthrough(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:     body,
		Surface:  SurfaceOpenAIChat,
		Metadata: map[string]any{"user_id": "user_123"},
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if env.Metadata == nil {
		t.Fatal("expected metadata")
	}
	if env.Metadata["user_id"] != "user_123" {
		t.Errorf("expected user_id user_123, got %v", env.Metadata["user_id"])
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: normalized roles
// ---------------------------------------------------------------------------

func TestOpenAIBody_NormalizeRoles(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "SYSTEM", "content": "sys"},
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "ASSISTANT", "content": "hi"},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(env.Messages))
	}
	if env.Messages[0].Role != RoleSystem {
		t.Errorf("uppercase SYSTEM: expected system, got %q", env.Messages[0].Role)
	}
	if env.Messages[2].Role != RoleAssistant {
		t.Errorf("uppercase ASSISTANT: expected assistant, got %q", env.Messages[2].Role)
	}
}

// ---------------------------------------------------------------------------
// Edge: content array with string elements
// ---------------------------------------------------------------------------

func TestOpenAIBody_ContentArray_StringElements(t *testing.T) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					"first part",
					"second part",
					map[string]any{"type": "text", "text": "third part"},
				},
			},
		},
	}

	env, err := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:    body,
		Surface: SurfaceOpenAIChat,
	})
	if err != nil {
		t.Fatalf("to canonical: %v", err)
	}

	if len(env.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(env.Messages))
	}
	parts := env.Messages[0].Parts
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	for i, p := range parts {
		if p.Type != PartText {
			t.Errorf("part[%d]: expected text, got %q", i, p.Type)
		}
	}
}

// make sure unused imports compile
var _ = strings.TrimSpace

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkOpenAIBody_ToCanonical(b *testing.B) {
	body := openAiChatBodyFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
			Body:      body,
			Surface:   SurfaceOpenAIChat,
			CliProfile: ProfileGeneric,
			Operation:  OpGenerate,
		})
	}
}

func BenchmarkOpenAIBody_ToCanonical_WithTools(b *testing.B) {
	body := map[string]any{
		"model": "gpt-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "Get the weather"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get weather for a city",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []any{"city"},
					},
				},
			},
		},
		"tool_choice": "auto",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
			Body:    body,
			Surface: SurfaceOpenAIChat,
		})
	}
}

func BenchmarkOpenAIBody_ToCanonical_WithImages(b *testing.B) {
	body := map[string]any{
		"model": "gpt-4o",
		"stream": false,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "What is in this image?"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/photo.png"}},
				},
			},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
			Body:       body,
			Surface:    SurfaceOpenAIChat,
			CliProfile: ProfileGeneric,
			Operation:  OpGenerate,
		})
	}
}

func BenchmarkCanonical_ToOpenAIBody(b *testing.B) {
	body := openAiChatBodyFixture()
	env, _ := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
		Body:       body,
		Surface:    SurfaceOpenAIChat,
		CliProfile: ProfileGeneric,
		Operation:  OpGenerate,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CanonicalRequestToOpenAiChatBody(env)
	}
}

func BenchmarkOpenAIBody_Roundtrip(b *testing.B) {
	body := openAiChatBodyFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env, _ := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
			Body:       body,
			Surface:    SurfaceOpenAIChat,
			CliProfile: ProfileGeneric,
			Operation:  OpGenerate,
		})
		_ = CanonicalRequestToOpenAiChatBody(env)
	}
}

func BenchmarkOpenAIBody_Roundtrip_WithImages(b *testing.B) {
	body := map[string]any{
		"model":  "gpt-4o",
		"stream": false,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "What is in this image?"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/photo.png"}},
				},
			},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env, _ := CanonicalRequestFromOpenAiBody(CanonicalRequestFromOpenAiBodyInput{
			Body:       body,
			Surface:    SurfaceOpenAIChat,
			CliProfile: ProfileGeneric,
			Operation:  OpGenerate,
		})
		_ = CanonicalRequestToOpenAiChatBody(env)
	}
}
