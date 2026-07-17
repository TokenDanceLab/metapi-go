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
