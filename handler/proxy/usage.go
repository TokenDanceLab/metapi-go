package proxyhandler

import (
	"encoding/json"
	"strings"

	"github.com/tokendancelab/metapi-go/proxy"
)

const (
	usageSourceUpstream = "upstream"
	usageSourceUnknown  = "unknown"
)

// ParsedUsage is a normalized token usage snapshot extracted from an upstream body/SSE.
type ParsedUsage struct {
	PromptTokens        int64
	CompletionTokens    int64
	TotalTokens         int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	// Source is "upstream" when any token field was present, otherwise "unknown".
	Source string
	Found  bool
}

// ToUsageSummary converts ParsedUsage into the lightweight failure-detection summary.
func (u ParsedUsage) ToUsageSummary() *proxy.UsageSummary {
	return &proxy.UsageSummary{
		PromptTokens:     int(u.PromptTokens),
		CompletionTokens: int(u.CompletionTokens),
		TotalTokens:      int(u.TotalTokens),
	}
}

// ParseUsageFromBody extracts token usage from a non-stream JSON response body.
// Supports OpenAI (prompt_tokens/completion_tokens/total_tokens), Anthropic
// (input_tokens/output_tokens), and Gemini (usageMetadata.*TokenCount).
func ParseUsageFromBody(body []byte) ParsedUsage {
	body = trimJSONNoise(body)
	if len(body) == 0 {
		return ParsedUsage{Source: usageSourceUnknown}
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ParsedUsage{Source: usageSourceUnknown}
	}
	return extractUsageFromValue(payload)
}

// ParseUsageFromSSEEvents extracts the best-effort final usage from SSE data events.
// Later events win when they carry usage so stream-end payloads (message_delta,
// response.completed, final chat.completion.chunk) override partial early values.
func ParseUsageFromSSEEvents(events []SseEvent) ParsedUsage {
	var best ParsedUsage
	best.Source = usageSourceUnknown
	for _, ev := range events {
		if ev.Data == "" || ev.Data == "[DONE]" {
			continue
		}
		if !looksLikeJSONObject(ev.Data) {
			continue
		}
		got := ParseUsageFromBody([]byte(ev.Data))
		if !got.Found {
			continue
		}
		best = mergeUsagePreferLater(best, got)
	}
	return best
}

func mergeUsagePreferLater(prev, next ParsedUsage) ParsedUsage {
	if !next.Found {
		return prev
	}
	out := prev
	out.Found = true
	out.Source = usageSourceUpstream
	// Prefer non-zero fields from the later event; retain earlier values when
	// the later payload is partial (e.g. Anthropic message_delta output only).
	if next.PromptTokens > 0 {
		out.PromptTokens = next.PromptTokens
	} else if !prev.Found {
		out.PromptTokens = next.PromptTokens
	}
	if next.CompletionTokens > 0 {
		out.CompletionTokens = next.CompletionTokens
	} else if !prev.Found {
		out.CompletionTokens = next.CompletionTokens
	}
	// Recompute total from merged prompt+completion unless the later event
	// provides an explicit total that covers both sides (total >= sum).
	sum := out.PromptTokens + out.CompletionTokens
	if next.TotalTokens > 0 && next.TotalTokens >= sum {
		out.TotalTokens = next.TotalTokens
	} else if sum > 0 {
		out.TotalTokens = sum
	} else if next.TotalTokens > 0 {
		out.TotalTokens = next.TotalTokens
	} else if !prev.Found {
		out.TotalTokens = next.TotalTokens
	}
	return out
}

func extractUsageFromValue(v any) ParsedUsage {
	out := ParsedUsage{Source: usageSourceUnknown}
	obj, ok := v.(map[string]any)
	if !ok {
		return out
	}

	// Direct usage object (OpenAI / Anthropic / Responses).
	if usage, ok := obj["usage"].(map[string]any); ok {
		applyUsageMap(&out, usage)
	}
	// Gemini-style usageMetadata.
	if meta, ok := obj["usageMetadata"].(map[string]any); ok {
		applyUsageMap(&out, meta)
	}
	// Nested response.usage (OpenAI Responses API stream events).
	if resp, ok := obj["response"].(map[string]any); ok {
		if usage, ok := resp["usage"].(map[string]any); ok {
			applyUsageMap(&out, usage)
		}
	}
	// Nested message.usage (Anthropic message_start).
	if msg, ok := obj["message"].(map[string]any); ok {
		if usage, ok := msg["usage"].(map[string]any); ok {
			applyUsageMap(&out, usage)
		}
	}

	if out.Found && out.TotalTokens == 0 && (out.PromptTokens > 0 || out.CompletionTokens > 0) {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	if out.Found {
		out.Source = usageSourceUpstream
	}
	return out
}

func applyUsageMap(out *ParsedUsage, usage map[string]any) {
	if out == nil || usage == nil {
		return
	}
	// OpenAI-style
	if n, ok := asInt64(usage["prompt_tokens"]); ok {
		out.PromptTokens = n
		out.Found = true
	}
	if n, ok := asInt64(usage["completion_tokens"]); ok {
		out.CompletionTokens = n
		out.Found = true
	}
	if n, ok := asInt64(usage["total_tokens"]); ok {
		out.TotalTokens = n
		out.Found = true
	}

	// Anthropic-style
	if n, ok := asInt64(usage["input_tokens"]); ok {
		out.PromptTokens = n
		out.Found = true
	}
	if n, ok := asInt64(usage["output_tokens"]); ok {
		out.CompletionTokens = n
		out.Found = true
	}
	// Optional Anthropic cache fields.
	if cacheRead, ok := asInt64(usage["cache_read_input_tokens"]); ok {
		out.CacheReadTokens = cacheRead
		out.Found = true
	}
	if cacheCreate, ok := asInt64(usage["cache_creation_input_tokens"]); ok {
		out.CacheCreationTokens = cacheCreate
		out.Found = true
	}
	if out.PromptTokens == 0 && (out.CacheReadTokens > 0 || out.CacheCreationTokens > 0) {
		out.PromptTokens = out.CacheReadTokens + out.CacheCreationTokens
	}

	// Gemini-style
	if n, ok := asInt64(usage["promptTokenCount"]); ok {
		out.PromptTokens = n
		out.Found = true
	}
	if n, ok := asInt64(usage["candidatesTokenCount"]); ok {
		out.CompletionTokens = n
		out.Found = true
	}
	if n, ok := asInt64(usage["totalTokenCount"]); ok {
		out.TotalTokens = n
		out.Found = true
	}

	// OpenAI Responses API sometimes uses input_tokens/output_tokens inside usage.
	// Already covered by Anthropic keys above.
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			f, err2 := n.Float64()
			if err2 != nil {
				return 0, false
			}
			return int64(f), true
		}
		return i, true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		var parsed int64
		for _, ch := range s {
			if ch < '0' || ch > '9' {
				return 0, false
			}
			parsed = parsed*10 + int64(ch-'0')
		}
		return parsed, true
	default:
		return 0, false
	}
}

func trimJSONNoise(body []byte) []byte {
	return []byte(strings.TrimSpace(string(body)))
}
