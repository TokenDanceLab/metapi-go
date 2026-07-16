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
	body := []byte(`{"type":"message","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 100 || got.CompletionTokens != 50 {
		t.Fatalf("usage = %+v, want prompt=100 completion=50", got)
	}
	if got.TotalTokens != 150 {
		t.Fatalf("total = %d, want 150", got.TotalTokens)
	}
	if got.CacheReadTokens != 10 || got.CacheCreationTokens != 5 {
		t.Fatalf("cache tokens = %d/%d, want 10/5", got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.PromptTokensIncludeCache == nil || !*got.PromptTokensIncludeCache {
		t.Fatalf("PromptTokensIncludeCache = %v, want true", got.PromptTokensIncludeCache)
	}
}

func TestParseUsageFromBodyOpenAICachedAndReasoning(t *testing.T) {
	body := []byte(`{
		"usage":{
			"prompt_tokens":200,
			"completion_tokens":80,
			"total_tokens":280,
			"prompt_tokens_details":{"cached_tokens":120},
			"completion_tokens_details":{"reasoning_tokens":40}
		}
	}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 200 || got.CompletionTokens != 80 || got.TotalTokens != 280 {
		t.Fatalf("usage = %+v", got)
	}
	if got.CacheReadTokens != 120 {
		t.Fatalf("cache read = %d, want 120", got.CacheReadTokens)
	}
	if got.ReasoningTokens != 40 {
		t.Fatalf("reasoning = %d, want 40", got.ReasoningTokens)
	}
}

func TestParseUsageFromBodyPartialMissingFields(t *testing.T) {
	// Partial payload: only completion side — must not invent prompt/cache.
	body := []byte(`{"usage":{"completion_tokens":3}}`)
	got := ParseUsageFromBody(body)
	if !got.Found {
		t.Fatal("expected found")
	}
	if got.PromptTokens != 0 || got.CacheReadTokens != 0 || got.CompletionTokens != 3 {
		t.Fatalf("partial usage = %+v", got)
	}
	if got.TotalTokens != 3 {
		t.Fatalf("total = %d, want 3", got.TotalTokens)
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
		{Event: "message_start", Data: `{"type":"message_start","message":{"usage":{"input_tokens":40,"output_tokens":0}}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`},
		{Event: "message_delta", Data: `{"type":"message_delta","usage":{"output_tokens":12}}`},
		{Data: "[DONE]"},
	}
	got := ParseUsageFromSSEEvents(events)
	if !got.Found {
		t.Fatal("expected usage found")
	}
	if got.PromptTokens != 40 {
		t.Fatalf("prompt = %d, want 40 retained from message_start", got.PromptTokens)
	}
	if got.CompletionTokens != 12 {
		t.Fatalf("completion = %d, want 12 from message_delta", got.CompletionTokens)
	}
	if got.TotalTokens != 52 {
		t.Fatalf("total = %d, want 52", got.TotalTokens)
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
