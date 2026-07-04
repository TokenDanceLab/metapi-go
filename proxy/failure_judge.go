package proxy

import (
	"encoding/json"
	"strings"

	"github.com/tokendancelab/metapi-go/config"
)

// FailureResult is a content-based failure detection result.
type FailureResult struct {
	Status int
	Reason string
}

// UsageSummary is a lightweight usage summary for failure detection.
type UsageSummary struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// DetectProxyFailure detects proxy failures from response content.
// This is PURELY content-based — it does NOT look at HTTP status codes.
// Called AFTER a non-stream response body is received.
//
// Detection:
// 1. Keyword matching: if config.ProxyErrorKeywords is non-empty, checks case-insensitive
// 2. Empty content check: if ProxyEmptyContentFailEnabled and no completion tokens + no output
func DetectProxyFailure(rawText string, usage *UsageSummary) *FailureResult {
	cfg := config.Get()
	rawText = strings.TrimSpace(rawText)

	// 1. Keyword matching
	if len(cfg.ProxyErrorKeywords) > 0 {
		normalizedText := strings.ToLower(rawText)
		for _, kw := range cfg.ProxyErrorKeywords {
			kw = strings.TrimSpace(strings.ToLower(kw))
			if kw == "" {
				continue
			}
			if strings.Contains(normalizedText, kw) {
				return &FailureResult{
					Status: 502,
					Reason: "Upstream response matched failure keyword: " + kw,
				}
			}
		}
	}

	// 2. Empty content check
	if cfg.ProxyEmptyContentFailEnabled {
		compTokens := 0
		if usage != nil {
			compTokens = usage.CompletionTokens
		}
		hasOutput := detectHasUpstreamOutput(rawText)
		if !hasOutput && compTokens <= 0 {
			return &FailureResult{
				Status: 502,
				Reason: "Upstream returned empty content",
			}
		}
	}

	return nil
}

// detectHasUpstreamOutput checks if raw text contains actual upstream output.
// Parses JSON or SSE event streams and looks for non-empty content.
func detectHasUpstreamOutput(rawText string) bool {
	trimmed := strings.TrimSpace(rawText)
	if trimmed == "" {
		return false
	}

	// Try JSON parse
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return hasCompletionContentFromPayload(parsed)
	}

	// Try SSE event stream parsing
	sseEvents := pullSseDataEvents(trimmed)
	if len(sseEvents) > 0 {
		for _, event := range sseEvents {
			payload := strings.TrimSpace(event)
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var parsedEvent any
			if err := json.Unmarshal([]byte(payload), &parsedEvent); err == nil {
				if hasCompletionContentFromPayload(parsedEvent) {
					return true
				}
			} else {
				// Non-JSON payload still counts as output
				return true
			}
		}
		// SSE payloads exist but none contain output
		return false
	}

	// Looks like SSE but contains no non-DONE payloads
	if strings.Contains(rawText, "data:") {
		return false
	}

	// Not JSON and not SSE: assume plain-text output
	return true
}

// pullSseDataEvents extracts "data:" lines from SSE text.
func pullSseDataEvents(rawText string) []string {
	var events []string
	lines := strings.Split(rawText, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			events = append(events, payload)
		}
	}
	return events
}

// hasCompletionContentFromPayload checks if a parsed JSON payload has upstream output.
// This mirrors the TS hasCompletionContentFromPayload function.
func hasCompletionContentFromPayload(payload any) bool {
	if payload == nil {
		return false
	}

	obj, ok := payload.(map[string]any)
	if !ok {
		return false
	}

	// Check choices
	if choices, ok := obj["choices"].([]any); ok {
		for _, choice := range choices {
			if hasCompletionContentFromChoice(choice) {
				return true
			}
		}
	}

	// Direct output_text
	if s, ok := obj["output_text"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if s, ok := obj["outputText"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}

	// Check output array
	if output, ok := obj["output"].([]any); ok {
		for _, item := range output {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemType := strings.ToLower(stringValue(itemMap["type"]))
			if strings.Contains(itemType, "function_call") || strings.Contains(itemType, "tool_call") {
				return true
			}
			if s, ok := itemMap["text"].(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
			if s, ok := itemMap["output_text"].(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
			if content, ok := itemMap["content"].([]any); ok {
				for _, part := range content {
					if partHasContent(part) {
						return true
					}
				}
			}
			if hasToolCallLikeMap(itemMap) {
				return true
			}
		}
	}

	// Check content parts
	if content, ok := obj["content"].([]any); ok {
		for _, part := range content {
			if partHasContent(part) {
				return true
			}
		}
	}

	// Direct text/delta
	if s, ok := obj["delta"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if s, ok := obj["text"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if hasToolCallLikeMap(obj) {
		return true
	}

	return false
}

func hasCompletionContentFromChoice(choice any) bool {
	cm, ok := choice.(map[string]any)
	if !ok {
		return false
	}
	if s, ok := cm["text"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if s, ok := cm["completion"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if s, ok := cm["output_text"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}

	message, _ := cm["message"].(map[string]any)
	if message != nil {
		if s, ok := message["content"].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
		if contentArr, ok := message["content"].([]any); ok {
			for _, part := range contentArr {
				if partHasContent(part) {
					return true
				}
			}
		}
		if s, ok := message["refusal"].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
		if hasToolCallLikeMap(message) {
			return true
		}
	}

	// Direct tool calls on choice
	if hasToolCallLikeMap(cm) {
		return true
	}

	// Delta
	delta, _ := cm["delta"].(map[string]any)
	if delta != nil {
		if s, ok := delta["content"].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
		if s, ok := delta["refusal"].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
		if hasToolCallLikeMap(delta) {
			return true
		}
	}

	return false
}

func partHasContent(part any) bool {
	pm, ok := part.(map[string]any)
	if !ok {
		return false
	}
	if s, ok := pm["text"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if s, ok := pm["output_text"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if s, ok := pm["content"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	partType := strings.ToLower(stringValue(pm["type"]))
	if strings.Contains(partType, "function_call") || strings.Contains(partType, "tool_call") {
		return true
	}
	return false
}

func hasToolCallLikeMap(m map[string]any) bool {
	for _, key := range []string{"tool_calls", "toolCalls", "function_call", "functionCall"} {
		if v, ok := m[key]; ok {
			return hasToolCallLike(v)
		}
	}
	return false
}

func hasToolCallLike(v any) bool {
	if v == nil {
		return false
	}
	if arr, ok := v.([]any); ok {
		return len(arr) > 0
	}
	if m, ok := v.(map[string]any); ok {
		return len(m) > 0
	}
	return false
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
