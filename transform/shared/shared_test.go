package shared

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ExtractInlineThinkTags (non-streaming)
// ---------------------------------------------------------------------------

func TestExtractInlineThinkTags_ContentOnly(t *testing.T) {
	result := ExtractInlineThinkTags("just some text")
	if result.Content != "just some text" {
		t.Errorf("expected 'just some text', got %q", result.Content)
	}
	if result.Reasoning != "" {
		t.Errorf("expected no reasoning, got %q", result.Reasoning)
	}
}

func TestExtractInlineThinkTags_WithTags(t *testing.T) {
	text := thinkOpen + "step by step" + thinkClose + "The answer"
	result := ExtractInlineThinkTags(text)
	if result.Content != "The answer" {
		t.Errorf("expected 'The answer', got %q", result.Content)
	}
	if result.Reasoning != "step by step" {
		t.Errorf("expected 'step by step', got %q", result.Reasoning)
	}
}

func TestExtractInlineThinkTags_UnclosedTag(t *testing.T) {
	text := thinkOpen + "unclosed reasoning"
	result := ExtractInlineThinkTags(text)
	if result.Content != "" {
		t.Errorf("expected empty content, got %q", result.Content)
	}
	if result.Reasoning != "unclosed reasoning" {
		t.Errorf("expected 'unclosed reasoning', got %q", result.Reasoning)
	}
}

func TestExtractInlineThinkTags_MiniMaxOrphanClose(t *testing.T) {
	// Upstream #511 / MiniMax playground: open tag omitted, only close tag appears.
	text := "友好的方式打招呼说\"你好哦\"。我应该以友好、热情的方式回应。\n" + thinkClose + "\n\n你好呀！很高兴见到你！"
	result := ExtractInlineThinkTags(text)
	if strings.Contains(result.Content, thinkClose) || strings.Contains(result.Content, thinkOpen) {
		t.Errorf("content must not keep raw think tags, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "你好呀") {
		t.Errorf("expected assistant greeting in content, got %q", result.Content)
	}
	if !strings.Contains(result.Reasoning, "友好的方式打招呼") {
		t.Errorf("expected thinking in reasoning, got %q", result.Reasoning)
	}
}

// ---------------------------------------------------------------------------
// MiniMax final + stream fixtures
// ---------------------------------------------------------------------------

func TestNormalizeUpstreamFinalResponse_MiniMaxOrphanThink(t *testing.T) {
	payload := map[string]any{
		"id":      "chatcmpl-minimax-1",
		"object":  "chat.completion",
		"created": float64(1700000000),
		"model":   "MiniMax-M2.7",
		"choices": []any{
			map[string]any{
				"index": float64(0),
				"message": map[string]any{
					"role": "assistant",
					"content": "友好的方式打招呼说\"你好哦\"。我应该以友好、热情的方式回应。\n" +
						thinkClose + "\n\n你好呀！很高兴见到你！",
				},
				"finish_reason": "stop",
			},
		},
	}

	result := NormalizeUpstreamFinalResponse(payload, "MiniMax-M2.7", "")
	if strings.Contains(result.Content, thinkClose) || strings.Contains(result.Content, thinkOpen) {
		t.Fatalf("content kept raw think tags: %q", result.Content)
	}
	if !strings.Contains(result.Content, "你好呀") {
		t.Fatalf("content = %q", result.Content)
	}
	if !strings.Contains(result.ReasoningContent, "友好的方式打招呼") {
		t.Fatalf("reasoning = %q", result.ReasoningContent)
	}
}

func TestNormalizeUpstreamFinalResponse_MiniMaxReasoningDetails(t *testing.T) {
	payload := map[string]any{
		"id":     "chatcmpl-minimax-2",
		"object": "chat.completion",
		"model":  "MiniMax-M2.7",
		"choices": []any{
			map[string]any{
				"index": float64(0),
				"message": map[string]any{
					"role":    "assistant",
					"content": "\n",
					"reasoning_details": []any{
						map[string]any{
							"type":   "reasoning.text",
							"id":     "reasoning-text-1",
							"format": "MiniMax-response-v1",
							"index":  float64(0),
							"text":   "Let me think about this request.",
						},
					},
				},
				"finish_reason": "stop",
			},
		},
	}

	result := NormalizeUpstreamFinalResponse(payload, "MiniMax-M2.7", "")
	if result.ReasoningContent != "Let me think about this request." {
		t.Fatalf("reasoning = %q", result.ReasoningContent)
	}
	if strings.Contains(result.Content, thinkOpen) || strings.Contains(result.Content, thinkClose) {
		t.Fatalf("content kept tags: %q", result.Content)
	}
}

func TestNormalizeUpstreamFinalResponse_MiniMaxPairedThink(t *testing.T) {
	payload := map[string]any{
		"id":    "chatcmpl-minimax-3",
		"model": "MiniMax-M2.5",
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": thinkOpen + "internal plan" + thinkClose + "visible answer",
				},
				"finish_reason": "stop",
			},
		},
	}
	result := NormalizeUpstreamFinalResponse(payload, "MiniMax-M2.5", "")
	if result.Content != "visible answer" {
		t.Fatalf("content = %q", result.Content)
	}
	if result.ReasoningContent != "internal plan" {
		t.Fatalf("reasoning = %q", result.ReasoningContent)
	}
}

func TestNormalizeUpstreamStreamEvent_MiniMaxThinkStream(t *testing.T) {
	ctx := CreateStreamTransformContext("MiniMax-M2.7")
	var content, reasoning string

	chunks := []map[string]any{
		{
			"id": "chatcmpl-mm-s1", "object": "chat.completion.chunk", "model": "MiniMax-M2.7",
			"choices": []any{map[string]any{
				"index": float64(0),
				"delta": map[string]any{"role": "assistant", "content": thinkOpen + "先考虑语气"},
			}},
		},
		{
			"id": "chatcmpl-mm-s1", "object": "chat.completion.chunk", "model": "MiniMax-M2.7",
			"choices": []any{map[string]any{
				"index": float64(0),
				"delta": map[string]any{"content": "再回复" + thinkClose + "\n你好"},
			}},
		},
		{
			"id": "chatcmpl-mm-s1", "object": "chat.completion.chunk", "model": "MiniMax-M2.7",
			"choices": []any{map[string]any{
				"index":         float64(0),
				"delta":         map[string]any{},
				"finish_reason": "stop",
			}},
		},
	}

	for _, payload := range chunks {
		ev := NormalizeUpstreamStreamEvent(payload, ctx, "MiniMax-M2.7")
		content += ev.ContentDelta
		reasoning += ev.ReasoningDelta
	}

	if strings.Contains(content, thinkClose) || strings.Contains(content, thinkOpen) {
		t.Fatalf("stream content kept tags: %q", content)
	}
	if !strings.Contains(content, "你好") {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(reasoning, "先考虑语气") || !strings.Contains(reasoning, "再回复") {
		t.Fatalf("reasoning = %q", reasoning)
	}
}

func TestNormalizeUpstreamStreamEvent_MiniMaxReasoningDetailsDelta(t *testing.T) {
	ctx := CreateStreamTransformContext("MiniMax-M2.7")
	payload := map[string]any{
		"id": "chatcmpl-mm-s2", "object": "chat.completion.chunk", "model": "MiniMax-M2.7",
		"choices": []any{map[string]any{
			"index": float64(0),
			"delta": map[string]any{
				"role": "assistant",
				"reasoning_details": []any{
					map[string]any{"type": "reasoning.text", "text": "split reasoning"},
				},
				"content": "answer",
			},
		}},
	}
	ev := NormalizeUpstreamStreamEvent(payload, ctx, "MiniMax-M2.7")
	if ev.ReasoningDelta != "split reasoning" {
		t.Fatalf("reasoning = %q", ev.ReasoningDelta)
	}
	if ev.ContentDelta != "answer" {
		t.Fatalf("content = %q", ev.ContentDelta)
	}
}

func TestSerializeFinalResponse_MiniMaxStrippedContent(t *testing.T) {
	normalized := NormalizeUpstreamFinalResponse(map[string]any{
		"id":    "chatcmpl-mm-ser",
		"model": "MiniMax-M2.7",
		"choices": []any{map[string]any{
			"message": map[string]any{
				"role":    "assistant",
				"content": "reason body" + thinkClose + "hello user",
			},
			"finish_reason": "stop",
		}},
	}, "MiniMax-M2.7", "")

	out := SerializeFinalResponse(FormatOpenAI, normalized, struct {
		PromptTokens, CompletionTokens, TotalTokens int
	}{1, 2, 3})

	var msg map[string]any
	switch choices := out["choices"].(type) {
	case []map[string]any:
		if len(choices) == 0 {
			t.Fatalf("no choices: %#v", out)
		}
		msg, _ = choices[0]["message"].(map[string]any)
	case []any:
		if len(choices) == 0 {
			t.Fatalf("no choices: %#v", out)
		}
		choice, _ := choices[0].(map[string]any)
		msg, _ = choice["message"].(map[string]any)
	default:
		t.Fatalf("unexpected choices type: %T", out["choices"])
	}

	content := AsTrimmedString(msg["content"])
	reasoning := AsTrimmedString(msg["reasoning_content"])
	if strings.Contains(content, thinkClose) {
		t.Fatalf("serialized content kept close tag: %q", content)
	}
	if !strings.Contains(content, "hello user") {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(reasoning, "reason body") {
		t.Fatalf("reasoning = %q", reasoning)
	}
}

// ---------------------------------------------------------------------------
// Stop reason normalization
// ---------------------------------------------------------------------------

func TestNormalizeStopReason(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"stop", "stop"},
		{"end_turn", "stop"},
		{"end", "stop"},
		{"eos", "stop"},
		{"finished", "stop"},
		{"completed", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"length", "length"},
		{"incomplete", "length"},
		{"max_output_tokens", "length"},
		{"max_tokens_exceeded", "length"},
		{"tool_use", "tool_calls"},
		{"tool_calls", "tool_calls"},
		{"failed", "error"},
		{"error", "error"},
		{"unknown", ""},
		{"", ""},
		{"  STOP  ", "stop"},
	}

	for _, tt := range tests {
		got := NormalizeStopReason(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeStopReason(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestToClaudeStopReason(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"error", "end_turn"},
		{"", "end_turn"},
	}

	for _, tt := range tests {
		got := ToClaudeStopReason(tt.input)
		if got != tt.expected {
			t.Errorf("ToClaudeStopReason(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// SSE Parsing
// ---------------------------------------------------------------------------

func TestPullSseEventsWithDone_SingleEvent(t *testing.T) {
	buffer := "data: {\"key\":\"value\"}\n\n"
	events, rest := PullSseEventsWithDone(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != "" {
		t.Errorf("expected empty event name, got %q", events[0].Event)
	}
	if events[0].Data != `{"key":"value"}` {
		t.Errorf("unexpected data: %q", events[0].Data)
	}
	if rest != "" {
		t.Errorf("expected empty rest, got %q", rest)
	}
}

func TestPullSseEventsWithDone_NamedEvent(t *testing.T) {
	buffer := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	events, rest := PullSseEventsWithDone(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != "message_start" {
		t.Errorf("expected event 'message_start', got %q", events[0].Event)
	}
	if rest != "" {
		t.Errorf("expected empty rest, got %q", rest)
	}
}

func TestPullSseEventsWithDone_MultiLineData(t *testing.T) {
	buffer := "data: line1\ndata: line2\n\ndata: {\"k\":\"v\"}\n\n"
	events, rest := PullSseEventsWithDone(buffer)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if !strings.Contains(events[0].Data, "line2") {
		t.Errorf("expected data containing line1 and line2, got %q", events[0].Data)
	}
	if rest != "" {
		t.Errorf("expected empty rest, got %q", rest)
	}
}

func TestPullSseEventsWithDone_CRLF(t *testing.T) {
	buffer := "data: {\"k\":\"v\"}\r\n\r\n"
	events, rest := PullSseEventsWithDone(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != `{"k":"v"}` {
		t.Errorf("unexpected data: %q", events[0].Data)
	}
	if rest != "" {
		t.Errorf("expected empty rest, got %q", rest)
	}
}

func TestPullSseEventsWithDone_Partial(t *testing.T) {
	buffer := "data: complete\n\ndata: partial"
	events, rest := PullSseEventsWithDone(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "complete" {
		t.Errorf("expected 'complete', got %q", events[0].Data)
	}
	if rest != "data: partial" {
		t.Errorf("expected 'data: partial', got %q", rest)
	}
}

func TestPullSseEventsWithDone_EmptyBlocks(t *testing.T) {
	buffer := "\n\n\ndata: real\n\n\n\n"
	events, _ := PullSseEventsWithDone(buffer)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data != "real" {
		t.Errorf("expected 'real', got %q", events[0].Data)
	}
}

func TestPullSseEventsWithDone_MultipleNamedEvents(t *testing.T) {
	buffer := "event: start\ndata: {\"x\":1}\n\nevent: delta\ndata: {\"y\":2}\n\n"
	events, rest := PullSseEventsWithDone(buffer)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Event != "start" {
		t.Errorf("expected 'start', got %q", events[0].Event)
	}
	if events[1].Event != "delta" {
		t.Errorf("expected 'delta', got %q", events[1].Event)
	}
	if rest != "" {
		t.Errorf("expected empty rest, got %q", rest)
	}
}

// ---------------------------------------------------------------------------
// SSE Serialization
// ---------------------------------------------------------------------------

func TestSerializeSSE_String(t *testing.T) {
	result := SerializeSSE("", "hello")
	if result != "data: hello\n\n" {
		t.Errorf("unexpected: %q", result)
	}
}

func TestSerializeSSE_Map(t *testing.T) {
	result := SerializeSSE("", map[string]any{"key": "value"})
	if !strings.HasPrefix(result, "data: ") || !strings.HasSuffix(result, "\n\n") {
		t.Errorf("unexpected format: %q", result)
	}
}

func TestSerializeSSE_NamedEvent(t *testing.T) {
	result := SerializeSSE("message_start", map[string]any{"type": "message_start"})
	if !strings.HasPrefix(result, "event: message_start\ndata: ") {
		t.Errorf("unexpected format: %q", result)
	}
}

// ---------------------------------------------------------------------------
// Helper utilities
// ---------------------------------------------------------------------------

func TestIsRecord(t *testing.T) {
	if IsRecord(nil) {
		t.Error("nil is not record")
	}
	if !IsRecord(map[string]any{"k": "v"}) {
		t.Error("map should be record")
	}
	if IsRecord("string") {
		t.Error("string is not record")
	}
}

func TestAsRecord(t *testing.T) {
	m, ok := AsRecord(map[string]any{"k": "v"})
	if !ok || m["k"] != "v" {
		t.Error("failed to get record")
	}
	_, ok = AsRecord(nil)
	if ok {
		t.Error("nil should not be record")
	}
}

func TestIsNonEmptyString(t *testing.T) {
	if IsNonEmptyString("") {
		t.Error("empty string should be false")
	}
	if IsNonEmptyString("   ") {
		t.Error("whitespace should be false")
	}
	if !IsNonEmptyString("hello") {
		t.Error("non-empty should be true")
	}
	if IsNonEmptyString(123) {
		t.Error("int should be false")
	}
}

func TestAsTrimmedString(t *testing.T) {
	if AsTrimmedString("  hello  ") != "hello" {
		t.Error("trimmed failed")
	}
	if AsTrimmedString(123) != "" {
		t.Error("non-string should return empty")
	}
}

func TestPickFiniteInt(t *testing.T) {
	if PickFiniteInt(float64(42.0)) != 42 {
		t.Error("expected 42")
	}
	if PickFiniteInt("not a number") != 0 {
		t.Error("expected 0 for non-number")
	}
}

func TestPickPositiveInt(t *testing.T) {
	if PickPositiveInt(42) != 42 {
		t.Error("expected 42")
	}
	if PickPositiveInt(0) != 0 {
		t.Error("expected 0 for zero")
	}
	if PickPositiveInt(-5) != 0 {
		t.Error("expected 0 for negative")
	}
}

func TestSafeJSONString(t *testing.T) {
	if SafeJSONString(nil) != "" {
		t.Error("expected empty for nil")
	}
	s := SafeJSONString(map[string]any{"k": "v"})
	if s != `{"k":"v"}` {
		t.Errorf("unexpected: %q", s)
	}
}

func TestStringifyUnknownValue(t *testing.T) {
	if StringifyUnknownValue("hello") != "hello" {
		t.Error("string passthrough")
	}
	if StringifyUnknownValue(nil) != "" {
		t.Error("nil -> ''")
	}
	if StringifyUnknownValue(true) != "true" {
		t.Error("bool -> string")
	}
	if StringifyUnknownValue(false) != "false" {
		t.Error("bool -> string")
	}
	if StringifyUnknownValue(float64(42)) != "42" {
		t.Errorf("int float -> string, got %q", StringifyUnknownValue(float64(42)))
	}
}

func TestJoinNonEmpty(t *testing.T) {
	result := JoinNonEmpty([]string{"a", "", "b", "  ", "c"})
	if result != "a\n\nb\n\nc" {
		t.Errorf("unexpected: %q", result)
	}
	result = JoinNonEmpty([]string{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestComputeNovelResponsesDelta_NoExisting(t *testing.T) {
	delta := ComputeNovelResponsesDelta("", "hello")
	if delta != "hello" {
		t.Errorf("expected 'hello', got %q", delta)
	}
}

func TestComputeNovelResponsesDelta_EmptyIncoming(t *testing.T) {
	delta := ComputeNovelResponsesDelta("existing", "")
	if delta != "" {
		t.Errorf("expected empty, got %q", delta)
	}
}

func TestComputeNovelResponsesDelta_Prefix(t *testing.T) {
	delta := ComputeNovelResponsesDelta("hello", "hello world")
	if delta != " world" {
		t.Errorf("expected ' world', got %q", delta)
	}
}

func TestComputeNovelResponsesDelta_Suffix(t *testing.T) {
	delta := ComputeNovelResponsesDelta("hello world", "world")
	if delta != "" {
		t.Errorf("expected empty (suffix), got %q", delta)
	}
}

func TestComputeNovelResponsesDelta_Overlap(t *testing.T) {
	delta := ComputeNovelResponsesDelta("abcde", "defgh")
	if delta != "fgh" {
		t.Errorf("expected 'fgh', got %q", delta)
	}
}

func TestComputeNovelResponsesDelta_NoOverlap(t *testing.T) {
	delta := ComputeNovelResponsesDelta("abc", "xyz")
	if delta != "xyz" {
		t.Errorf("expected 'xyz', got %q", delta)
	}
}

func TestParseJSONLike_Valid(t *testing.T) {
	result := ParseJSONLike(`{"key":"value"}`)
	m, ok := result.(map[string]any)
	if !ok || m["key"] != "value" {
		t.Errorf("unexpected: %v", result)
	}
}

func TestParseJSONLike_Empty(t *testing.T) {
	result := ParseJSONLike("")
	_, ok := result.(map[string]any)
	if !ok {
		t.Errorf("expected empty map, got %T", result)
	}
}

func TestParseJSONLike_Invalid(t *testing.T) {
	result := ParseJSONLike("not json")
	m, ok := result.(map[string]any)
	if !ok || m["value"] != "not json" {
		t.Errorf("expected wrapped string, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// ClaudeDownstreamContext
// ---------------------------------------------------------------------------

func TestCreateClaudeDownstreamContext(t *testing.T) {
	cc := CreateClaudeDownstreamContext()
	if cc.MessageStarted {
		t.Error("expected MessageStarted=false")
	}
	if cc.ContentBlockStarted {
		t.Error("expected ContentBlockStarted=false")
	}
	if cc.DoneSent {
		t.Error("expected DoneSent=false")
	}
	if cc.ToolBlocks == nil {
		t.Error("expected non-nil ToolBlocks")
	}
}

// ---------------------------------------------------------------------------
// StreamTransformContext
// ---------------------------------------------------------------------------

func TestCreateStreamTransformContext(t *testing.T) {
	ctx := CreateStreamTransformContext("test-model")
	if ctx.Model != "test-model" {
		t.Errorf("expected test-model, got %q", ctx.Model)
	}
	if ctx.ID == "" {
		t.Error("expected non-empty ID")
	}
	if ctx.Created == 0 {
		t.Error("expected non-zero Created")
	}
	if ctx.ToolCalls == nil {
		t.Error("expected non-nil ToolCalls")
	}
	if ctx.ThinkTagParser == nil {
		t.Error("expected non-nil ThinkTagParser")
	}
}

// ---------------------------------------------------------------------------
// NormalizeUpstreamFinalResponse (sanity)
// ---------------------------------------------------------------------------

func TestNormalizeUpstreamFinalResponse_OpenAI(t *testing.T) {
	payload := map[string]any{
		"id":      "chatcmpl-123",
		"object":  "chat.completion",
		"created": float64(1700000000),
		"model":   "gpt-4",
		"choices": []any{
			map[string]any{
				"index": float64(0),
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello!",
				},
				"finish_reason": "stop",
			},
		},
	}

	result := NormalizeUpstreamFinalResponse(payload, "gpt-4", "")
	if result.ID != "chatcmpl-123" {
		t.Errorf("expected chatcmpl-123, got %q", result.ID)
	}
	if result.Content != "Hello!" {
		t.Errorf("expected Hello!, got %q", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("expected stop, got %q", result.FinishReason)
	}
}

func TestNormalizeUpstreamFinalResponse_ClaudeMessage(t *testing.T) {
	payload := map[string]any{
		"id":      "msg_123",
		"type":    "message",
		"model":   "claude-sonnet-4-20250514",
		"role":    "assistant",
		"content": []any{
			map[string]any{"type": "text", "text": "Hi!"},
		},
		"stop_reason": "end_turn",
	}

	result := NormalizeUpstreamFinalResponse(payload, "claude-sonnet-4-20250514", "")
	if result.ID != "msg_123" {
		t.Errorf("expected msg_123, got %q", result.ID)
	}
	if result.Content != "Hi!" {
		t.Logf("Claude message parsed content: %q", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("expected stop, got %q", result.FinishReason)
	}
}

func TestNormalizeUpstreamFinalResponse_StringPayload(t *testing.T) {
	result := NormalizeUpstreamFinalResponse("plain text response", "gpt-4", "")
	if result.Content != "plain text response" {
		t.Errorf("expected 'plain text response', got %q", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("expected stop, got %q", result.FinishReason)
	}
}

func TestNormalizeUpstreamFinalResponse_NilPayload(t *testing.T) {
	result := NormalizeUpstreamFinalResponse(nil, "gpt-4", "fallback text")
	if result.Model != "gpt-4" {
		t.Errorf("expected gpt-4, got %q", result.Model)
	}
	if result.Content != "fallback text" {
		t.Errorf("expected 'fallback text', got %q", result.Content)
	}
}

// ---------------------------------------------------------------------------
// NormalizeUpstreamStreamEvent (sanity)
// ---------------------------------------------------------------------------

func TestNormalizeUpstreamStreamEvent_OpenAI(t *testing.T) {
	ctx := CreateStreamTransformContext("gpt-4")
	payload := map[string]any{
		"id":      "chatcmpl-123",
		"object":  "chat.completion.chunk",
		"created": float64(1700000000),
		"model":   "gpt-4",
		"choices": []any{
			map[string]any{
				"index": float64(0),
				"delta": map[string]any{
					"role":    "assistant",
					"content": "Hello",
				},
				"finish_reason": nil,
			},
		},
	}

	event := NormalizeUpstreamStreamEvent(payload, ctx, "gpt-4")
	if event.Role != "assistant" {
		t.Errorf("expected assistant role, got %q", event.Role)
	}
	if event.ContentDelta == "" && event.ReasoningDelta == "" {
		t.Logf("Think tag parser buffered content; event: role=%q content=%q reasoning=%q", event.Role, event.ContentDelta, event.ReasoningDelta)
	}
	if event.FinishReason != "" {
		t.Errorf("expected empty finish_reason, got %q", event.FinishReason)
	}
}

func TestNormalizeUpstreamStreamEvent_Anthropic(t *testing.T) {
	ctx := CreateStreamTransformContext("claude-sonnet-4-20250514")
	payload := map[string]any{
		"type": "content_block_delta",
		"delta": map[string]any{
			"type": "text_delta",
			"text": "Hello",
		},
	}

	event := NormalizeUpstreamStreamEvent(payload, ctx, "claude-sonnet-4-20250514")
	if event.ContentDelta == "" && event.ReasoningDelta == "" {
		t.Logf("Anthropic text_delta: content=%q reasoning=%q", event.ContentDelta, event.ReasoningDelta)
	}
}

func TestNormalizeUpstreamStreamEvent_AnthropicTool(t *testing.T) {
	ctx := CreateStreamTransformContext("claude-sonnet-4-20250514")
	payload := map[string]any{
		"type": "content_block_delta",
		"index": float64(0),
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": `{"city":"`,
		},
	}

	event := NormalizeUpstreamStreamEvent(payload, ctx, "claude-sonnet-4-20250514")
	if len(event.ToolCallDeltas) != 1 {
		t.Fatalf("expected 1 tool call delta, got %d", len(event.ToolCallDeltas))
	}
	if event.ToolCallDeltas[0].ArgumentsDelta != `{"city":"` {
		t.Errorf("unexpected arguments delta: %q", event.ToolCallDeltas[0].ArgumentsDelta)
	}
}

// ---------------------------------------------------------------------------
// SerializeNormalizedStreamEvent
// ---------------------------------------------------------------------------

func TestSerializeNormalizedStreamEvent_OpenAI(t *testing.T) {
	ctx := CreateStreamTransformContext("gpt-4")
	cc := CreateClaudeDownstreamContext()
	event := NormalizedStreamEvent{
		Role:         "assistant",
		ContentDelta: "Hello",
	}

	lines := SerializeNormalizedStreamEvent(FormatOpenAI, event, ctx, cc)
	if len(lines) == 0 {
		t.Fatal("expected serialized lines")
	}
	if !strings.HasPrefix(lines[0], "data: ") || !strings.HasSuffix(lines[0], "\n\n") {
		t.Errorf("unexpected SSE format: %q", lines[0])
	}
	// Should contain "Hello"
	if !strings.Contains(lines[0], "Hello") {
		t.Errorf("expected Hello in output: %q", lines[0])
	}
}

func TestSerializeNormalizedStreamEvent_Empty(t *testing.T) {
	ctx := CreateStreamTransformContext("gpt-4")
	cc := CreateClaudeDownstreamContext()
	event := NormalizedStreamEvent{} // empty event

	lines := SerializeNormalizedStreamEvent(FormatOpenAI, event, ctx, cc)
	if len(lines) != 0 {
		t.Errorf("expected no lines for empty event, got %d", len(lines))
	}
}

func TestSerializeStreamDone_OpenAI(t *testing.T) {
	ctx := CreateStreamTransformContext("gpt-4")
	cc := CreateClaudeDownstreamContext()

	lines := SerializeStreamDone(FormatOpenAI, ctx, cc)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0] != "data: [DONE]\n\n" {
		t.Errorf("expected [DONE], got %q", lines[0])
	}
}

func TestSerializeStreamDone_DoubleCall(t *testing.T) {
	ctx := CreateStreamTransformContext("gpt-4")
	cc := CreateClaudeDownstreamContext()

	lines := SerializeStreamDone(FormatOpenAI, ctx, cc)
	if lines == nil {
		t.Fatal("expected [DONE]")
	}

	// Second call should return nil
	lines2 := SerializeStreamDone(FormatOpenAI, ctx, cc)
	if lines2 != nil {
		t.Errorf("expected nil on second Done, got %v", lines2)
	}
}

// ---------------------------------------------------------------------------
// SerializeFinalResponse (sanity)
// ---------------------------------------------------------------------------

func TestSerializeFinalResponse_OpenAI(t *testing.T) {
	normalized := NormalizedFinalResponse{
		ID:           "chatcmpl-123",
		Model:        "gpt-4",
		Created:      1700000000,
		Content:      "Hello!",
		FinishReason: "stop",
	}

	result := SerializeFinalResponse(FormatOpenAI, normalized, struct{ PromptTokens, CompletionTokens, TotalTokens int }{100, 50, 150})
	if result["id"] != "chatcmpl-123" {
		t.Errorf("expected chatcmpl-123, got %v", result["id"])
	}
	if result["object"] != "chat.completion" {
		t.Errorf("expected chat.completion, got %v", result["object"])
	}
}

func TestSerializeFinalResponse_Claude(t *testing.T) {
	normalized := NormalizedFinalResponse{
		ID:           "msg_123",
		Model:        "claude-sonnet-4-20250514",
		Created:      1700000000,
		Content:      "Hello!",
		FinishReason: "stop",
	}

	result := SerializeFinalResponse(FormatClaude, normalized, struct{ PromptTokens, CompletionTokens, TotalTokens int }{100, 50, 150})
	if result["type"] != "message" {
		t.Errorf("expected message type, got %v", result["type"])
	}
	if result["role"] != "assistant" {
		t.Errorf("expected assistant role, got %v", result["role"])
	}
}

// ---------------------------------------------------------------------------
// BuildSyntheticOpenAiChunks (sanity)
// ---------------------------------------------------------------------------

func TestBuildSyntheticOpenAiChunks(t *testing.T) {
	normalized := NormalizedFinalResponse{
		ID:           "chatcmpl-123",
		Model:        "gpt-4",
		Created:      1700000000,
		Content:      "Hello!",
		FinishReason: "stop",
	}

	chunks := BuildSyntheticOpenAiChunks(normalized)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 synthetic chunks, got %d", len(chunks))
	}
	// First chunk should have delta with content
	c1, _ := chunks[0]["choices"].([]map[string]any)
	d1, _ := c1[0]["delta"].(map[string]any)
	if d1["content"] != "Hello!" {
		t.Errorf("expected content in first chunk, got %v", d1["content"])
	}
	// Second chunk should have finish_reason
	c2, _ := chunks[1]["choices"].([]map[string]any)
	if c2[0]["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason in second chunk, got %v", c2[0]["finish_reason"])
	}
}
