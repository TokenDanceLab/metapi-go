package responses

import (
	"fmt"
	"strings"
)

// Reasoning item types that must carry required multi-turn content fields
// (summary / content / encrypted_content) when replayed into input.
const (
	ResponsesInputTypeReasoning = "reasoning"
	ResponsesInputTypeMessage   = "message"
)

// ReasoningInputError is a clear client-facing validation error when a
// multi-turn reasoning item omits every required content field.
// Avoids opaque upstream 400 "Missing required parameter: 'input[n].content'".
type ReasoningInputError struct {
	Message string
	Index   int
	Reason  string
}

func (e *ReasoningInputError) Error() string {
	if e == nil {
		return "reasoning input error"
	}
	if e.Message != "" {
		return e.Message
	}
	return "reasoning input item is missing required content"
}

// SanitizeResponsesInputItems normalizes the Responses `input` field for
// multi-turn Hermes/Codex traffic:
//
//  1. Reasoning items keep encrypted_content / summary (never dropped).
//  2. Reasoning items always expose a top-level `content` key derived from
//     summary text (or "" when only encrypted_content is present) so strict
//     OpenAI-compatible gateways that validate input[n].content do not 400.
//  3. Message items keep empty content as "" rather than omitting the key.
//  4. Returns ReasoningInputError when a reasoning item has no summary text,
//     no content, and no encrypted_content.
//
// String / non-array inputs are returned unchanged.
func SanitizeResponsesInputItems(input any) (any, error) {
	switch v := input.(type) {
	case nil:
		return input, nil
	case string:
		return input, nil
	case []any:
		return sanitizeResponsesInputArray(v)
	case map[string]any:
		// Single-item object form (rare); normalize as a one-element array then unwrap.
		arr, err := sanitizeResponsesInputArray([]any{v})
		if err != nil {
			return nil, err
		}
		if len(arr) == 1 {
			return arr[0], nil
		}
		return arr, nil
	default:
		return input, nil
	}
}

func sanitizeResponsesInputArray(items []any) ([]any, error) {
	if items == nil {
		return nil, nil
	}
	out := make([]any, 0, len(items))
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		next, err := sanitizeResponsesInputItem(item, i)
		if err != nil {
			return nil, err
		}
		out = append(out, next)
	}
	return out, nil
}

func sanitizeResponsesInputItem(item map[string]any, index int) (map[string]any, error) {
	if item == nil {
		return item, nil
	}
	typ := strings.ToLower(asTrimmedString(item["type"]))
	switch typ {
	case ResponsesInputTypeReasoning:
		return sanitizeReasoningInputItem(item, index)
	case ResponsesInputTypeMessage, "":
		// Role-bearing items without type are treated as messages by Responses clients.
		if typ == ResponsesInputTypeMessage || asTrimmedString(item["role"]) != "" {
			return preserveMessageContent(item), nil
		}
		return cloneMapShallow(item), nil
	default:
		// function_call / function_call_output / custom_tool_call* / etc.
		// Pass through without inventing a content field (schema differs by type).
		return cloneMapShallow(item), nil
	}
}

func sanitizeReasoningInputItem(item map[string]any, index int) (map[string]any, error) {
	next := cloneMapShallow(item)
	next["type"] = ResponsesInputTypeReasoning

	// Never drop encrypted_content / summary — these are the real multi-turn
	// continuity payload for reasoning models (stateless + tool loops).
	encrypted := asTrimmedString(next["encrypted_content"])
	if encrypted != "" {
		next["encrypted_content"] = encrypted
	}

	summaryText := extractReasoningSummaryText(next["summary"])
	// Also accept plain-string summary or content blocks already present.
	existingContent := extractReasoningContentText(next["content"])
	if existingContent == "" {
		existingContent = extractReasoningContentText(next["text"])
	}

	hasEncrypted := encrypted != ""
	hasSummaryText := summaryText != ""
	hasContentText := existingContent != ""

	if !hasEncrypted && !hasSummaryText && !hasContentText {
		return nil, &ReasoningInputError{
			Index:  index,
			Reason: "reasoning_item_missing_required_content",
			Message: fmt.Sprintf(
				"input[%d] type=reasoning is missing required content: provide non-empty summary text, content, or encrypted_content for multi-turn /v1/responses (Hermes/Codex)",
				index,
			),
		}
	}

	// Strict gateways validate input[n].content on every item. Prefer real text
	// (summary/content); fall back to empty string when only encrypted_content
	// is present so the key exists without fabricating reasoning prose.
	if !hasContentKey(next) || !hasContentText {
		if hasContentText {
			next["content"] = existingContent
		} else if hasSummaryText {
			next["content"] = summaryText
		} else {
			next["content"] = ""
		}
	}

	// Normalize empty summary arrays: keep as [] so clients that require the
	// key still see it, without inventing summary_text blocks.
	if _, ok := next["summary"]; ok {
		if arr, ok := next["summary"].([]any); ok && len(arr) == 0 {
			next["summary"] = arr
		}
	}

	return next, nil
}

func preserveMessageContent(item map[string]any) map[string]any {
	next := cloneMapShallow(item)
	if typ := asTrimmedString(next["type"]); typ == "" {
		next["type"] = ResponsesInputTypeMessage
	}
	// Empty assistant content is valid for tool-call turns. Do not delete the
	// key — some clients send content:"" / content:[] and upstream expects it.
	if _, ok := next["content"]; !ok {
		// Only inject when role is present and content is completely absent.
		// Leave non-message shapes alone.
		if asTrimmedString(next["role"]) != "" {
			next["content"] = ""
		}
	}
	return next
}

func hasContentKey(item map[string]any) bool {
	if item == nil {
		return false
	}
	_, ok := item["content"]
	return ok
}

// extractReasoningSummaryText flattens Responses summary arrays / strings into text.
// Accepts:
//   - string
//   - []any of {type:summary_text|text, text: "..."} or plain strings
//   - map with text / content
func extractReasoningSummaryText(summary any) string {
	switch s := summary.(type) {
	case string:
		return strings.TrimSpace(s)
	case []any:
		var parts []string
		for _, item := range s {
			if t := extractReasoningContentText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return extractReasoningContentText(s)
	default:
		return ""
	}
}

// extractReasoningContentText pulls human-readable text from content-ish values.
func extractReasoningContentText(content any) string {
	switch c := content.(type) {
	case string:
		return strings.TrimSpace(c)
	case []any:
		var parts []string
		for _, item := range c {
			if t := extractReasoningContentText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content", "summary_text", "output_text", "input_text", "reasoning", "reasoning_content"} {
			if t := asTrimmedString(c[key]); t != "" {
				return t
			}
		}
		// Nested summary array inside a content object.
		if t := extractReasoningSummaryText(c["summary"]); t != "" {
			return t
		}
		return ""
	default:
		return ""
	}
}

// HasReasoningInputItems reports whether body.input contains any reasoning items.
func HasReasoningInputItems(body map[string]any) bool {
	if body == nil {
		return false
	}
	arr, ok := body["input"].([]any)
	if !ok {
		return false
	}
	for _, raw := range arr {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(asTrimmedString(item["type"])) == ResponsesInputTypeReasoning {
			return true
		}
	}
	return false
}

func asTrimmedString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func cloneMapShallow(body map[string]any) map[string]any {
	next := map[string]any{}
	if body == nil {
		return next
	}
	for k, v := range body {
		next[k] = v
	}
	return next
}
