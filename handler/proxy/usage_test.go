package proxyhandler

import (
	"encoding/json"
	"testing"
)

func TestParseUsageFromBodyOpenAI(t *testing.T) {
	body := []byte(`{"id":"x","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 22 || got.TotalTokens != 33 {
		t.Fatalf("usage = %+v, want 11/22/33", got)
	}
	if got.Source != usageSourceUpstream {
		t.Fatalf("source = %q, want upstream", got.Source)
	}
}

func TestParseUsageFromBodyOpenAIMissingTotal(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":5,"completion_tokens":7}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.TotalTokens != 12 {
		t.Fatalf("total = %d, want 12 (prompt+completion)", got.TotalTokens)
	}
}

func TestParseUsageFromBodyAnthropic(t *testing.T) {
	// Anthropic input_tokens is non-cached; cache_*_input_tokens are exclusive
	// and must be included in prompt/total accounting (upstream #555).
	body := []byte(`{"type":"message","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 115 {
		t.Fatalf("prompt = %d, want 115 (input+cache_read+cache_creation)", got.PromptTokens)
	}
	if got.CompletionTokens != 50 {
		t.Fatalf("completion = %d, want 50", got.CompletionTokens)
	}
	if got.CacheReadTokens != 10 || got.CacheCreationTokens != 5 {
		t.Fatalf("cache fields = read %d create %d, want 10/5", got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.TotalTokens != 165 {
		t.Fatalf("total = %d, want 165", got.TotalTokens)
	}
}

func TestParseUsageFromBodyAnthropicCacheOnlyPrompt(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":0,"output_tokens":3,"cache_read_input_tokens":40,"cache_creation_input_tokens":2}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 42 {
		t.Fatalf("prompt = %d, want 42", got.PromptTokens)
	}
	if got.TotalTokens != 45 {
		t.Fatalf("total = %d, want 45", got.TotalTokens)
	}
}

func TestParseUsageFromBodyGemini(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"totalTokenCount":13}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 9 || got.CompletionTokens != 4 || got.TotalTokens != 13 {
		t.Fatalf("usage = %+v, want 9/4/13", got)
	}
}

func TestParseUsageFromBodyGeminiThoughtsIncludedInTotal(t *testing.T) {
	// When totalTokenCount is present it already includes thoughts; do not double-count.
	body := []byte(`{"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"thoughtsTokenCount":7,"totalTokenCount":20}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 9 || got.CompletionTokens != 4 || got.TotalTokens != 20 {
		t.Fatalf("usage = %+v, want 9/4/20 (thoughts already in total)", got)
	}
	if got.ReasoningTokens != 7 {
		t.Fatalf("reasoning = %d, want 7 recorded", got.ReasoningTokens)
	}
}

func TestParseUsageFromBodyGeminiThoughtsWithoutTotal(t *testing.T) {
	// Missing total: fold thoughts into completion so stats are not short.
	body := []byte(`{"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"thoughtsTokenCount":7}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 9 {
		t.Fatalf("prompt = %d, want 9", got.PromptTokens)
	}
	if got.CompletionTokens != 11 {
		t.Fatalf("completion = %d, want 11 (candidates+thoughts)", got.CompletionTokens)
	}
	if got.TotalTokens != 20 {
		t.Fatalf("total = %d, want 20", got.TotalTokens)
	}
}

func TestParseUsageFromBodyOpenAICachedTokensSubset(t *testing.T) {
	// OpenAI cached_tokens is a subset of prompt_tokens — do not add twice.
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":30}}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 100 || got.CompletionTokens != 20 || got.TotalTokens != 120 {
		t.Fatalf("usage = %+v, want 100/20/120", got)
	}
	if got.CacheReadTokens != 30 {
		t.Fatalf("cache_read = %d, want 30", got.CacheReadTokens)
	}
}

func TestParseUsageFromBodyOpenAIReasoningNotDoubleCounted(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":50,"total_tokens":60,"completion_tokens_details":{"reasoning_tokens":40}}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.CompletionTokens != 50 || got.TotalTokens != 60 {
		t.Fatalf("usage = %+v, want completion 50 total 60 (no double-count)", got)
	}
	if got.ReasoningTokens != 40 {
		t.Fatalf("reasoning = %d, want 40 recorded", got.ReasoningTokens)
	}
}

func TestParseUsageFromBodyNestedResponseUsage(t *testing.T) {
	body := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":8,"total_tokens":11}}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 3 || got.CompletionTokens != 8 || got.TotalTokens != 11 {
		t.Fatalf("usage = %+v, want 3/8/11", got)
	}
}

func TestParseUsageFromBodyEmpty(t *testing.T) {
	got := ParseUsageFromBody(nil)
	if got.Found {
		t.Fatal("expected not found")
	}
	if got.Source != usageSourceUnknown {
		t.Fatalf("source = %q", got.Source)
	}
}

func TestParseUsageFromBodyDoesNotInventUsage(t *testing.T) {
	body := []byte(`{"id":"x","choices":[{"message":{"content":"hi"}}]}`)
	got := ParseUsageFromBody(body)
	if got.Found {
		t.Fatalf("must not invent usage when omitted: %+v", got)
	}
	if got.PromptTokens != 0 || got.CompletionTokens != 0 || got.TotalTokens != 0 {
		t.Fatalf("tokens must stay zero: %+v", got)
	}
}

func TestParseUsageFromSSEEventsPrefersFinalUsage(t *testing.T) {
	events := []SseEvent{
		{Data: `{"choices":[{"delta":{"content":"a"}}]}`},
		{Data: `{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		{Data: `{"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 10 || got.CompletionTokens != 20 || got.TotalTokens != 30 {
		t.Fatalf("usage = %+v, want final 10/20/30", got)
	}
}

func TestParseUsageFromSSEEventsAnthropicMessageDelta(t *testing.T) {
	events := []SseEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"usage":{"input_tokens":40,"output_tokens":0,"cache_read_input_tokens":8,"cache_creation_input_tokens":2}}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`},
		{Event: "message_delta", Data: `{"type":"message_delta","usage":{"output_tokens":12}}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	// message_start expands cache into prompt; message_delta supplies completion.
	if got.PromptTokens != 50 {
		t.Fatalf("prompt = %d, want 50 (40+8+2 retained from message_start)", got.PromptTokens)
	}
	if got.CompletionTokens != 12 {
		t.Fatalf("completion = %d, want 12 from message_delta", got.CompletionTokens)
	}
	if got.CacheReadTokens != 8 || got.CacheCreationTokens != 2 {
		t.Fatalf("cache fields lost on merge: read=%d create=%d", got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.TotalTokens != 62 {
		t.Fatalf("total = %d, want 62", got.TotalTokens)
	}
}

// P0-555 residual honesty (#486): Claude/Anthropic stream merge must retain early
// input_tokens when later message_delta only reports output_tokens, recompute total
// from the observed split, and never invent tokens beyond what upstream reported.
// This locks residual merge honesty — not a claim of perfect multi-instance billing.
func TestParseUsageFromSSEEventsAnthropicMessageStartDeltaMergeHonesty(t *testing.T) {
	events := []SseEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":120,"output_tokens":0}}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`},
		// Later partial usage: output only — prompt must be retained, not zeroed/invented.
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":34}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	}
	got := ParseUsageFromSSEEvents(events)
	if !got.Found {
		t.Fatal("expected usage found after message_start + message_delta")
	}
	if got.PromptTokens != 120 {
		t.Fatalf("prompt = %d, want 120 retained from message_start input_tokens", got.PromptTokens)
	}
	if got.CompletionTokens != 34 {
		t.Fatalf("completion = %d, want 34 from message_delta output_tokens", got.CompletionTokens)
	}
	// Total is derived from observed prompt+completion only — no invented extras.
	if got.TotalTokens != 154 {
		t.Fatalf("total = %d, want 154 (120+34 observed sum; no invented totals)", got.TotalTokens)
	}
	if got.Source != usageSourceUpstream {
		t.Fatalf("source = %q, want upstream", got.Source)
	}
	if got.CacheReadTokens != 0 || got.CacheCreationTokens != 0 || got.ReasoningTokens != 0 {
		t.Fatalf("must not invent cache/reasoning fields: %+v", got)
	}
}

// P0-555 residual honesty (#486): SSE streams that never emit usage must stay Found=false
// with zero tokens — never invent billing fields from content/event shapes alone.
func TestParseUsageFromSSEEventsNoUsageDoesNotInvent(t *testing.T) {
	events := []SseEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"msg_1","role":"assistant","content":[]}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
		{Data: `{"choices":[{"delta":{"content":"x"},"finish_reason":null}]}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if got.Found {
		t.Fatalf("must not invent usage when SSE omits usage fields: %+v", got)
	}
	if got.PromptTokens != 0 || got.CompletionTokens != 0 || got.TotalTokens != 0 {
		t.Fatalf("tokens must stay zero when usage omitted: %+v", got)
	}
	if got.Source != usageSourceUnknown {
		t.Fatalf("source = %q, want unknown", got.Source)
	}
}

// P0-555 residual honesty (#486): OpenAI chat stream end usage (include_usage style)
// wins over earlier empty/partial chunks; intermediate content deltas do not invent usage.
func TestParseUsageFromSSEEventsOpenAIStreamEndUsageWins(t *testing.T) {
	events := []SseEvent{
		{Data: `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`},
		{Data: `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`},
		// Empty usage object must not invent non-zero tokens, but Found may flip if keys present.
		// Prefer a later non-empty usage chunk as the stream-end winner.
		{Data: `{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":4,"total_tokens":19}}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if !got.Found {
		t.Fatal("expected usage found from final OpenAI stream chunk")
	}
	if got.PromptTokens != 15 || got.CompletionTokens != 4 || got.TotalTokens != 19 {
		t.Fatalf("usage = %+v, want final stream-end 15/4/19", got)
	}
	if got.Source != usageSourceUpstream {
		t.Fatalf("source = %q, want upstream", got.Source)
	}
}

// Same honesty contracts via the incremental SSE analyzer used on the live proxy path.
func TestIncrementalSseAnalyzerAnthropicUsageMergeHonesty(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()
	stream := "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":80,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	// Push in awkward chunk boundaries to ensure merge is order-based, not buffer-based.
	analyzer.Push([]byte(stream[:len(stream)/3]))
	analyzer.Push([]byte(stream[len(stream)/3 : 2*len(stream)/3]))
	analyzer.Push([]byte(stream[2*len(stream)/3:]))

	got := analyzer.Result().Usage
	if !got.Found {
		t.Fatal("expected incremental analyzer to find merged Anthropic usage")
	}
	if got.PromptTokens != 80 {
		t.Fatalf("prompt = %d, want 80 retained from message_start", got.PromptTokens)
	}
	if got.CompletionTokens != 9 {
		t.Fatalf("completion = %d, want 9 from message_delta", got.CompletionTokens)
	}
	if got.TotalTokens != 89 {
		t.Fatalf("total = %d, want 89 (observed sum only)", got.TotalTokens)
	}
}

func TestIncrementalSseAnalyzerNoUsageDoesNotInvent(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()
	stream := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"b\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	analyzer.Push([]byte(stream))
	got := analyzer.Result().Usage
	if got.Found {
		t.Fatalf("must not invent usage on content-only SSE: %+v", got)
	}
	if got.PromptTokens != 0 || got.CompletionTokens != 0 || got.TotalTokens != 0 {
		t.Fatalf("tokens must stay zero: %+v", got)
	}
	if got.Source != usageSourceUnknown {
		t.Fatalf("source = %q, want unknown", got.Source)
	}
}

func TestToUsageSummary(t *testing.T) {
	u := ParsedUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, Found: true}
	s := u.ToUsageSummary()
	if s.PromptTokens != 1 || s.CompletionTokens != 2 || s.TotalTokens != 3 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestParseUsageFromBodyJSONNumberSafety(t *testing.T) {
	// Ensure non-usage bodies do not panic / falsely report found.
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "x"}}}})
	got := ParseUsageFromBody(body)
	if got.Found {
		t.Fatalf("unexpected found usage: %+v", got)
	}
}

// P0-555 residual honesty (#530): Gemini streamGenerateContent-style SSE emits
// early partial usageMetadata then a final chunk with complete counts. Later
// non-zero fields must win; intermediate content-only events must not invent tokens.
// Status remains present-with-residual (no multi-instance lag / media-zero proof).
func TestParseUsageFromSSEEventsGeminiUsageMetadataLaterWins(t *testing.T) {
	events := []SseEvent{
		{Data: `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":0,"totalTokenCount":10}}`},
		{Data: `{"candidates":[{"content":{"parts":[{"text":" there"}]}}]}`},
		{Data: `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":7,"totalTokenCount":17,"thoughtsTokenCount":2}}`},
	}
	got := ParseUsageFromSSEEvents(events)
	if !got.Found {
		t.Fatal("expected usage found from Gemini usageMetadata stream")
	}
	if got.PromptTokens != 10 {
		t.Fatalf("prompt = %d, want 10", got.PromptTokens)
	}
	// Completion should reflect final candidates (+ thoughts fold when total present).
	if got.CompletionTokens < 7 {
		t.Fatalf("completion = %d, want >= 7 from final chunk", got.CompletionTokens)
	}
	if got.TotalTokens < 17 {
		t.Fatalf("total = %d, want >= 17 from final chunk", got.TotalTokens)
	}
	if got.Source != usageSourceUpstream {
		t.Fatalf("source = %q, want upstream", got.Source)
	}
}

// P0-555 residual honesty (#530): empty usage object / zero-only usage must not
// invent non-zero tokens from content alone.
func TestParseUsageFromSSEEventsEmptyUsageObjectDoesNotInvent(t *testing.T) {
	events := []SseEvent{
		{Data: `{"choices":[{"delta":{"content":"hello"}}],"usage":{}}`},
		{Data: `{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if got.PromptTokens != 0 || got.CompletionTokens != 0 || got.TotalTokens != 0 {
		t.Fatalf("must not invent non-zero tokens from empty/zero usage: %+v", got)
	}
}
