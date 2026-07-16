// Package messages provides Anthropic Messages API transformer.
package messages

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/tokendancelab/metapi-go/transform/shared"
)

// --- Constants ---

var validAnthropicToolChoiceTypes = map[string]bool{"auto": true, "none": true, "any": true, "tool": true}
var validAnthropicThinkingTypes = map[string]bool{"enabled": true, "disabled": true, "adaptive": true}
var validAnthropicEfforts = map[string]bool{"low": true, "medium": true, "high": true, "max": true}
var anthropicWebSearchToolTypes = map[string]bool{
	"web_search": true, "web_search_20250305": true, "google_search": true,
}

const maxAnthropicCacheControlBreakpoints = 4
const adaptiveAnthropicCacheControlBlockWindow = 20

// --- Sanitization ---

// SanitizeAnthropicMessagesBody validates and normalizes an Anthropic request body.
func SanitizeAnthropicMessagesBody(body map[string]any, autoOptimizeCacheControls bool) (map[string]any, error) {
	sanitized := cloneMap(body)

	// T+P mutual exclusion
	hasTemp := toFiniteNumber(sanitized["temperature"]) != nil
	hasTopP := toFiniteNumber(sanitized["top_p"]) != nil
	if hasTemp && hasTopP {
		delete(sanitized, "top_p")
	}

	// Thinking config
	if tc, err := sanitizeThinkingConfig(sanitized["thinking"]); err != nil {
		return nil, err
	} else if tc != nil {
		sanitized["thinking"] = tc
	} else {
		delete(sanitized, "thinking")
	}

	// Output config
	allowEffort := false
	if t, ok := sanitized["thinking"].(map[string]any); ok {
		allowEffort = strings.ToLower(shared.AsTrimmedString(t["type"])) == "adaptive"
	}
	ocKey := "output_config"
	if _, ok := sanitized["output_config"]; !ok {
		ocKey = "outputConfig"
	}
	if oc, err := sanitizeOutputConfig(sanitized[ocKey], allowEffort); err != nil {
		return nil, err
	} else if oc != nil {
		sanitized["output_config"] = oc
	} else {
		delete(sanitized, "output_config")
	}
	delete(sanitized, "outputConfig")

	// Tool choice
	tcKey := "tool_choice"
	if _, ok := sanitized["tool_choice"]; !ok {
		tcKey = "toolChoice"
	}
	if tc, err := sanitizeToolChoice(sanitized[tcKey]); err != nil {
		return nil, err
	} else if tc != nil {
		sanitized["tool_choice"] = tc
	} else {
		delete(sanitized, "tool_choice")
	}
	delete(sanitized, "toolChoice")

	// Sanitize messages
	if msgs, ok := sanitized["messages"].([]any); ok && len(msgs) > 0 {
		var sanitizedMsgs []map[string]any
		for _, msg := range msgs {
			m, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			sm := sanitizeAnthropicMessage(m)
			if sm != nil {
				sanitizedMsgs = append(sanitizedMsgs, sm)
			}
		}
		sanitized["messages"] = sanitizedMsgs
	}

	// Cache control optimization
	if autoOptimizeCacheControls {
		optimizeAnthropicCacheControls(sanitized)
	} else {
		sanitizeUnsupportedCacheControls(sanitized)
	}

	return sanitized, nil
}

// --- OpenAI body -> Anthropic body ---

// ConvertOpenAiBodyToAnthropicMessagesBody converts an OpenAI chat body to Anthropic format.
//
// Skill-call multi-turn history depends on preserving:
//   - assistant tool_calls[].id / function.name / function.arguments → tool_use
//   - role=tool tool_call_id + content → tool_result (optionally coalesced with the next user turn)
// Empty object arguments ({}) are kept as empty maps so Claude Code can still see the tool_use
// block even when an upstream model omitted required Skill.input fields (client residual limit).
func ConvertOpenAiBodyToAnthropicMessagesBody(openaiBody map[string]any, modelName string, stream bool) (map[string]any, error) {
	rawMsgs, _ := openaiBody["messages"].([]any)
	var systemContents []string
	type claudeMsg struct {
		Role    string
		Content any
	}
	var msgs []claudeMsg

	for messageIndex := 0; messageIndex < len(rawMsgs); messageIndex++ {
		item := rawMsgs[messageIndex]
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(shared.AsTrimmedString(m["role"]))
		if role == "" {
			role = "user"
		}

		if role == "system" || role == "developer" {
			cbs := convertOpenAIContentToAnthropicBlocks(m["content"])
			var texts []string
			for _, cb := range cbs {
				if cb["type"] == "text" {
					if s, ok := cb["text"].(string); ok && s != "" {
						texts = append(texts, s)
					}
				}
			}
			if t := strings.Join(texts, "\n\n"); t != "" {
				systemContents = append(systemContents, t)
			}
			continue
		}

		// OpenAI role=tool rows become Anthropic user tool_result blocks.
		// Consecutive tool messages are coalesced; if the next message is user text,
		// it is appended so Claude Code multi-turn Skill history stays contiguous.
		if role == "tool" {
			var toolResultBlocks []map[string]any
			cursor := messageIndex
			for cursor < len(rawMsgs) {
				candidate, ok := rawMsgs[cursor].(map[string]any)
				if !ok {
					break
				}
				if strings.ToLower(shared.AsTrimmedString(candidate["role"])) != "tool" {
					break
				}
				toolUseID := shared.AsTrimmedString(candidate["tool_call_id"])
				if toolUseID == "" {
					toolUseID = shared.AsTrimmedString(candidate["id"])
				}
				toolResultContent := normalizeToolMessageContent(candidate["content"])
				hasContent := toolResultContentHasPayload(toolResultContent)
				if toolUseID != "" && hasContent {
					block := map[string]any{
						"type":        "tool_result",
						"tool_use_id": toolUseID,
						"content":     toolResultContent,
					}
					if candidate["is_error"] == true {
						block["is_error"] = true
					}
					toolResultBlocks = append(toolResultBlocks, block)
				}
				cursor++
			}

			if len(toolResultBlocks) > 0 && cursor < len(rawMsgs) {
				if nextItem, ok := rawMsgs[cursor].(map[string]any); ok {
					if strings.ToLower(shared.AsTrimmedString(nextItem["role"])) == "user" {
						userBlocks := convertOpenAIContentToAnthropicBlocks(nextItem["content"])
						toolResultBlocks = append(toolResultBlocks, userBlocks...)
						cursor++
					}
				}
			}

			if len(toolResultBlocks) > 0 {
				// convert to []any for consistent message content shape
				content := make([]any, 0, len(toolResultBlocks))
				for _, b := range toolResultBlocks {
					content = append(content, b)
				}
				msgs = append(msgs, claudeMsg{Role: "user", Content: content})
			}
			messageIndex = cursor - 1
			continue
		}

		if role == "assistant" {
			cbs := convertOpenAIContentToAnthropicBlocks(m["content"])
			rc := shared.AsTrimmedString(m["reasoning_content"])
			if rc == "" {
				rc = shared.AsTrimmedString(m["reasoning"])
			}
			if rc != "" {
				tb := map[string]any{"type": "thinking", "thinking": rc}
				if sig := shared.AsTrimmedString(m["reasoning_signature"]); sig != "" {
					tb["reasoning_signature"] = sig
				}
				cbs = append([]map[string]any{tb}, cbs...)
			}
			if tcs, ok := m["tool_calls"].([]any); ok {
				for i, tc := range tcs {
					tcm, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := tcm["function"].(map[string]any)
					if fn == nil {
						fn = map[string]any{}
					}
					id := shared.AsTrimmedString(tcm["id"])
					if id == "" {
						id = "toolu_" + itoa(int64(i))
					}
					name := shared.AsTrimmedString(fn["name"])
					if name == "" {
						name = shared.AsTrimmedString(tcm["name"])
					}
					if name == "" {
						name = "tool_" + itoa(int64(i))
					}
					// Prefer function.arguments; fall back to top-level arguments for
					// non-standard OpenAI-compatible providers (Skill-call edge cases).
					rawArgs := fn["arguments"]
					if rawArgs == nil {
						rawArgs = tcm["arguments"]
					}
					input := normalizeAnthropicToolInput(rawArgs)
					cbs = append(cbs, map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
				}
			}
			if len(cbs) == 0 {
				continue
			}
			if len(cbs) == 1 && cbs[0]["type"] == "text" {
				if s, ok := cbs[0]["text"].(string); ok && s != "" {
					msgs = append(msgs, claudeMsg{Role: "assistant", Content: s})
				}
				continue
			}
			msgs = append(msgs, claudeMsg{Role: "assistant", Content: cbs})
			continue
		}

		cbs := convertOpenAIContentToAnthropicBlocks(m["content"])
		if len(cbs) == 0 {
			continue
		}
		if len(cbs) == 1 && cbs[0]["type"] == "text" {
			if s, ok := cbs[0]["text"].(string); ok && s != "" {
				msgs = append(msgs, claudeMsg{Role: "user", Content: s})
			}
			continue
		}
		msgs = append(msgs, claudeMsg{Role: "user", Content: cbs})
	}

	// Convert messages to []map[string]any
	var anthropicMsgs []map[string]any
	for _, cm := range msgs {
		anthropicMsgs = append(anthropicMsgs, map[string]any{
			"role":    cm.Role,
			"content": cm.Content,
		})
	}

	body := map[string]any{
		"model":     modelName,
		"stream":    stream,
		"messages":  anthropicMsgs,
		"max_tokens": toFiniteInt(openaiBody["max_tokens"], 4096),
	}

	if len(systemContents) > 0 {
		body["system"] = strings.Join(systemContents, "\n\n")
	}

	if t := toFiniteNumber(openaiBody["temperature"]); t != nil {
		body["temperature"] = *t
	}
	if tp := toFiniteNumber(openaiBody["top_p"]); tp != nil {
		body["top_p"] = *tp
	}
	if stop, ok := openaiBody["stop"].([]any); ok && len(stop) > 0 {
		body["stop_sequences"] = stop
	}
	if tools := openaiBody["tools"]; tools != nil {
		body["tools"] = convertOpenAiToolsToAnthropic(tools)
	}

	parallelTC := openaiBody["parallel_tool_calls"]
	atc := convertOpenAiToolChoiceToAnthropic(openaiBody["tool_choice"])
	if parallelTC == false {
		choiceRecord := ensureAnthropicToolChoiceRecord(atc)
		if choiceRecord == nil {
			choiceRecord = map[string]any{"type": "auto"}
		}
		choiceRecord["disable_parallel_tool_use"] = true
		atc = choiceRecord
	}
	if atc != nil {
		body["tool_choice"] = atc
	}

	if re, rb := resolveOpenAIReasoningSettings(openaiBody); re != nil {
		body["thinking"] = re
	} else if rb != nil {
		body["thinking"] = rb
	}

	return body, nil
}

// --- Conversion helpers ---

func convertOpenAIContentToAnthropicBlocks(content any) []map[string]any {
	if s, ok := content.(string); ok {
		if t := strings.TrimSpace(s); t != "" {
			return []map[string]any{{"type": "text", "text": t}}
		}
		return nil
	}
	arr, ok := content.([]any)
	if !ok {
		if m, ok := content.(map[string]any); ok {
			if b := sanitizeAnthropicContentBlock(m); b != nil {
				return []map[string]any{b}
			}
		}
		return nil
	}
	var blocks []map[string]any
	for _, item := range arr {
		if s, ok := item.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
			}
			continue
		}
		if m, ok := item.(map[string]any); ok {
			if b := sanitizeAnthropicContentBlock(m); b != nil {
				blocks = append(blocks, b)
			}
		}
	}
	return blocks
}

func sanitizeAnthropicContentBlock(item map[string]any) map[string]any {
	t := strings.ToLower(shared.AsTrimmedString(item["type"]))
	if t == "" {
		text := shared.AsTrimmedString(item["text"])
		if text == "" {
			text = shared.AsTrimmedString(item["content"])
		}
		if text == "" {
			text = shared.AsTrimmedString(item["output_text"])
		}
		if text != "" {
			return map[string]any{"type": "text", "text": text}
		}
		return nil
	}

	if t == "text" || t == "input_text" || t == "output_text" {
		text := shared.AsTrimmedString(item["text"])
		if text == "" {
			return nil
		}
		next := map[string]any{"type": "text", "text": text}
		if cc, ok := item["cache_control"].(map[string]any); ok && shared.AsTrimmedString(cc["type"]) == "ephemeral" {
			next["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		return next
	}

	if t == "image_url" || t == "input_image" {
		return toAnthropicImageBlock(item)
	}

	if t == "file" || t == "input_file" {
		return toAnthropicDocumentBlock(item)
	}

	if t == "thinking" || t == "redacted_thinking" || t == "reasoning" {
		text := shared.AsTrimmedString(item["thinking"])
		if text == "" {
			text = shared.AsTrimmedString(item["text"])
		}
		if text == "" {
			text = shared.AsTrimmedString(item["content"])
		}
		if text == "" {
			text = shared.AsTrimmedString(item["data"])
		}

		if t != "redacted_thinking" {
			sig := shared.AsTrimmedString(item["signature"])
			if sig == "" {
				sig = shared.AsTrimmedString(item["reasoning_signature"])
			}
			hasSig := sig != ""
			if text == "" && !hasSig {
				return nil
			}
			next := map[string]any{}
			for k, v := range item {
				next[k] = v
			}
			if t == "reasoning" {
				next["type"] = "thinking"
			}
			next["thinking"] = text
			if hasSig {
				next["signature"] = sig
			} else {
				delete(next, "signature")
			}
			delete(next, "cache_control")
			delete(next, "text")
			delete(next, "content")
			delete(next, "reasoning_signature")
			return next
		}

		next := map[string]any{}
		for k, v := range item {
			next[k] = v
		}
		next["type"] = "redacted_thinking"
		next["data"] = text
		delete(next, "thinking")
		delete(next, "cache_control")
		return next
	}

	if t == "tool_result" {
		toolUseID := shared.AsTrimmedString(item["tool_use_id"])
		if toolUseID == "" {
			toolUseID = shared.AsTrimmedString(item["toolUseId"])
		}
		content := normalizeToolMessageContent(item["content"])
		if toolUseID == "" {
			return nil
		}
		if s, ok := content.(string); ok && s == "" {
			return nil
		}
		if arr, ok := content.([]any); ok && len(arr) == 0 {
			return nil
		}
		next := map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": content}
		if item["is_error"] == true {
			next["is_error"] = true
		}
		if cc, ok := item["cache_control"].(map[string]any); ok && shared.AsTrimmedString(cc["type"]) == "ephemeral" {
			next["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		return next
	}

	// tool_use is the Claude Code Skill path; server_tool_use is Anthropic server tools.
	if t == "tool_use" || t == "server_tool_use" {
		id := shared.AsTrimmedString(item["id"])
		name := shared.AsTrimmedString(item["name"])
		if name == "" {
			name = shared.AsTrimmedString(item["toolName"])
		}
		if id == "" || name == "" {
			return nil
		}
		input := normalizeAnthropicToolInput(item["input"])
		if input == nil {
			input = normalizeAnthropicToolInput(item["arguments"])
		}
		if input == nil {
			input = normalizeAnthropicToolInput(item["argumentsText"])
		}
		next := map[string]any{"type": t, "id": id, "name": name, "input": input}
		if cc, ok := item["cache_control"].(map[string]any); ok && shared.AsTrimmedString(cc["type"]) == "ephemeral" {
			next["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		return next
	}

	if t == "tool_reference" {
		toolName := shared.AsTrimmedString(item["tool_name"])
		if toolName == "" {
			toolName = shared.AsTrimmedString(item["toolName"])
		}
		if toolName == "" {
			toolName = shared.AsTrimmedString(item["name"])
		}
		if toolName == "" {
			return nil
		}
		next := cloneMap(item)
		next["type"] = "tool_reference"
		next["tool_name"] = toolName
		delete(next, "toolName")
		delete(next, "name")
		return next
	}

	return cloneMap(item)
}

func toAnthropicImageBlock(item map[string]any) map[string]any {
	rawURL := item["image_url"]
	if rawURL == nil {
		rawURL = item["url"]
	}
	if s, ok := rawURL.(string); ok && strings.TrimSpace(s) != "" {
		parsed := maybeParseDataURLImage(strings.TrimSpace(s))
		if parsed != nil {
			return parsed
		}
		return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": strings.TrimSpace(s)}}
	}
	if m, ok := rawURL.(map[string]any); ok {
		nested := shared.AsTrimmedString(m["url"])
		if nested == "" {
			nested = shared.AsTrimmedString(m["image_url"])
		}
		if nested == "" {
			return nil
		}
		parsed := maybeParseDataURLImage(nested)
		if parsed != nil {
			return parsed
		}
		return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": nested}}
	}
	return nil
}

func maybeParseDataURLImage(url string) map[string]any {
	const prefix = "data:"
	if !strings.HasPrefix(url, prefix) {
		return nil
	}
	rest := url[len(prefix):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return nil
	}
	mimeType := rest[:comma]
	if !strings.HasPrefix(mimeType, "image/") {
		return nil
	}
	if !strings.HasSuffix(mimeType, ";base64") {
		return nil
	}
	mimeType = mimeType[:len(mimeType)-7]
	data := rest[comma+1:]
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type":       "base64",
			"media_type": mimeType,
			"data":       data,
		},
	}
}

func toAnthropicDocumentBlock(item map[string]any) map[string]any {
	// input_file / file block -> document
	fd := shared.AsTrimmedString(item["file_data"])
	fu := shared.AsTrimmedString(item["file_url"])
	fn := shared.AsTrimmedString(item["filename"])
	mt := shared.AsTrimmedString(item["mime_type"])

	if fd == "" && fu == "" {
		return nil
	}

	if fd != "" {
		return map[string]any{
			"type": "document",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mt,
				"data":       fd,
			},
		}
	}

	doc := map[string]any{
		"type": "document",
		"source": map[string]any{
			"type": "url",
			"url":  fu,
		},
	}
	if mt != "" {
		doc["source"].(map[string]any)["media_type"] = mt
	}
	if fn != "" {
		doc["title"] = fn
	}
	return doc
}

func normalizeAnthropicToolInput(raw any) any {
	if raw == nil {
		return map[string]any{}
	}
	if _, ok := raw.(map[string]any); ok {
		return raw
	}
	if _, ok := raw.([]any); ok {
		return raw
	}
	if s, ok := raw.(string); ok {
		return shared.ParseJSONLike(s)
	}
	if _, ok := raw.(float64); ok {
		return raw
	}
	if _, ok := raw.(bool); ok {
		return raw
	}
	return map[string]any{}
}


func toolResultContentHasPayload(content any) bool {
	switch c := content.(type) {
	case string:
		return strings.TrimSpace(c) != ""
	case []any:
		return len(c) > 0
	case []map[string]any:
		return len(c) > 0
	default:
		// normalizeToolMessageContent returns concrete string/slice values.
		s := shared.StringifyUnknownValue(content)
		return strings.TrimSpace(s) != "" && s != "{}" && s != "null"
	}
}

func normalizeToolMessageContent(raw any) any {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	if _, ok := raw.(float64); ok {
		return shared.StringifyUnknownValue(raw)
	}
	if _, ok := raw.(bool); ok {
		return shared.StringifyUnknownValue(raw)
	}
	if arr, ok := raw.([]any); ok {
		var blocks []map[string]any
		for _, item := range arr {
			if s, ok := item.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": t})
				}
				continue
			}
			if m, ok := item.(map[string]any); ok {
				t := strings.ToLower(shared.AsTrimmedString(m["type"]))
				if t == "tool_reference" {
					if b := sanitizeAnthropicContentBlock(m); b != nil {
						blocks = append(blocks, b)
					}
					continue
				}
				if t == "text" || t == "input_text" || t == "output_text" {
					text := shared.AsTrimmedString(m["text"])
					if text == "" {
						text = shared.AsTrimmedString(m["content"])
					}
					if text != "" {
						blocks = append(blocks, map[string]any{"type": "text", "text": text})
					}
					continue
				}
				if t == "image_url" || t == "input_image" {
					if b := toAnthropicImageBlock(m); b != nil {
						blocks = append(blocks, b)
					}
					continue
				}
				if t == "file" || t == "input_file" {
					if b := toAnthropicDocumentBlock(m); b != nil {
						blocks = append(blocks, b)
					}
				}
			}
		}
		if len(blocks) > 0 {
			return blocks
		}
		var texts []string
		for _, item := range arr {
			text := shared.AsTrimmedString(item)
			if text != "" {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
		return shared.SafeJSONString(raw)
	}
	if m, ok := raw.(map[string]any); ok {
		t := strings.ToLower(shared.AsTrimmedString(m["type"]))
		if t == "image_url" || t == "input_image" {
			if b := toAnthropicImageBlock(m); b != nil {
				return []map[string]any{b}
			}
			return ""
		}
		if t == "file" || t == "input_file" {
			if b := toAnthropicDocumentBlock(m); b != nil {
				return []map[string]any{b}
			}
			return ""
		}
		text := shared.AsTrimmedString(m["text"])
		if text == "" {
			text = shared.AsTrimmedString(m["content"])
		}
		if text != "" {
			return text
		}
		return shared.SafeJSONString(raw)
	}
	return ""
}

func ensureAnthropicToolChoiceRecord(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return map[string]any{"type": strings.TrimSpace(strings.ToLower(s))}
	}
	return nil
}

// --- Sanitization functions ---

func sanitizeThinkingConfig(v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	t := strings.ToLower(shared.AsTrimmedString(m["type"]))
	if t == "" {
		return nil, nil
	}
	if !validAnthropicThinkingTypes[t] {
		return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
			"error": map[string]any{"message": "thinking.type must be one of: enabled, disabled, adaptive", "type": "invalid_request_error"},
		}}
	}
	if t == "enabled" {
		budget := shared.PickPositiveInt(m["budget_tokens"])
		if budget == 0 {
			budget = shared.PickPositiveInt(m["budgetTokens"])
		}
		if budget == 0 {
			return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "budget_tokens is required and must be positive when thinking.type is enabled", "type": "invalid_request_error"},
			}}
		}
		return map[string]any{"type": t, "budget_tokens": budget}, nil
	}
	if t == "adaptive" {
		budget := shared.PickPositiveInt(m["budget_tokens"])
		if budget == 0 {
			budget = shared.PickPositiveInt(m["budgetTokens"])
		}
		if budget > 0 {
			return map[string]any{"type": "enabled", "budget_tokens": budget}, nil
		}
	}
	return map[string]any{"type": t}, nil
}

func sanitizeOutputConfig(v any, allowEffort bool) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil
	}
	next := map[string]any{}
	if effort, ok := m["effort"]; ok {
		e := strings.ToLower(shared.AsTrimmedString(effort))
		if e == "" {
			// skip blank
		} else if !allowEffort {
			// ignore outside adaptive
		} else if !validAnthropicEfforts[e] {
			return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "output_config.effort must be one of: low, medium, high, max", "type": "invalid_request_error"},
			}}
		} else {
			next["effort"] = e
		}
	}
	for k, v := range m {
		if k == "effort" {
			continue
		}
		next[k] = v
	}
	if len(next) == 0 {
		return nil, nil
	}
	return next, nil
}

func sanitizeToolChoice(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok {
		n := strings.TrimSpace(strings.ToLower(s))
		if n == "" {
			return nil, nil
		}
		if n == "required" {
			return map[string]any{"type": "any"}, nil
		}
		if n == "tool" {
			return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "tool_choice.name is required when type is tool", "type": "invalid_request_error"},
			}}
		}
		if !validAnthropicToolChoiceTypes[n] {
			return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "tool_choice.type must be one of: auto, none, any, tool", "type": "invalid_request_error"},
			}}
		}
		return map[string]any{"type": n}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
			"error": map[string]any{"message": "tool_choice must be an object or string", "type": "invalid_request_error"},
		}}
	}
	t := strings.ToLower(shared.AsTrimmedString(m["type"]))
	if !validAnthropicToolChoiceTypes[t] {
		return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
			"error": map[string]any{"message": "tool_choice.type must be one of: auto, none, any, tool", "type": "invalid_request_error"},
		}}
	}
	next := cloneMap(m)
	next["type"] = t
	if t == "tool" {
		name := shared.AsTrimmedString(m["name"])
		if name == "" {
			if tool, ok := m["tool"].(map[string]any); ok {
				name = shared.AsTrimmedString(tool["name"])
			}
		}
		if name == "" {
			return nil, &shared.ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "tool_choice.name is required when type is tool", "type": "invalid_request_error"},
			}}
		}
		next["name"] = name
		delete(next, "tool")
	} else {
		delete(next, "name")
		delete(next, "tool")
	}
	return next, nil
}

func sanitizeAnthropicMessage(m map[string]any) map[string]any {
	next := cloneMap(m)
	rawContent := next["content"]
	if arr, ok := rawContent.([]any); ok {
		var content []map[string]any
		for _, item := range arr {
			if im, ok := item.(map[string]any); ok {
				if b := sanitizeAnthropicContentBlock(im); b != nil {
					content = append(content, b)
				}
			} else if _, ok := item.(string); ok {
				text := shared.AsTrimmedString(item)
				if text != "" {
					content = append(content, map[string]any{"type": "text", "text": text})
				}
			}
		}
		if len(content) == 0 {
			return nil
		}
		next["content"] = content
		return next
	}
	if rawContent == nil {
		return nil
	}
	if im, ok := rawContent.(map[string]any); ok {
		if b := sanitizeAnthropicContentBlock(im); b != nil {
			next["content"] = []map[string]any{b}
			return next
		}
		return nil
	}
	if _, ok := rawContent.(float64); ok {
		next["content"] = shared.StringifyUnknownValue(rawContent)
	}
	if _, ok := rawContent.(bool); ok {
		next["content"] = shared.StringifyUnknownValue(rawContent)
	}
	if _, ok := next["content"].(string); ok {
		return next
	}
	return nil
}

// --- OpenAI -> Anthropic tool conversion ---

func convertOpenAiToolsToAnthropic(raw any) any {
	arr, ok := raw.([]any)
	if !ok {
		return raw
	}
	var out []any
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		t := strings.ToLower(shared.AsTrimmedString(m["type"]))
		if isAnthropicWebSearchTool(m) {
			out = append(out, map[string]any{"type": "web_search_20250305", "name": "web_search"})
			continue
		}
		if t == "function" {
			if fn, ok := m["function"].(map[string]any); ok {
				name := shared.AsTrimmedString(fn["name"])
				if name == "" {
					continue
				}
				mapped := map[string]any{"name": name}
				if d := shared.AsTrimmedString(fn["description"]); d != "" {
					mapped["description"] = d
				}
				// Preserve full parameters (incl. required / properties) so Skill and
				// other Claude Code tools keep schema semantics across OpenAI bridges.
				if fn["parameters"] != nil {
					mapped["input_schema"] = deepCloneJSON(fn["parameters"])
				} else {
					mapped["input_schema"] = map[string]any{"type": "object", "properties": map[string]any{}}
				}
				if cc := m["cache_control"]; cc != nil {
					mapped["cache_control"] = deepCloneJSON(cc)
				}
				out = append(out, mapped)
			}
			continue
		}
		if shared.AsTrimmedString(m["name"]) != "" && m["input_schema"] != nil {
			// Already Anthropic-shaped; deep-clone so callers cannot mutate shared maps.
			if cloned := deepCloneJSON(item); cloned != nil {
				out = append(out, cloned)
			} else {
				out = append(out, item)
			}
			continue
		}
	}
	if len(out) == 0 {
		return raw
	}
	return out
}

func convertOpenAiToolChoiceToAnthropic(raw any) any {
	if raw == nil {
		return nil
	}
	var mapped any = raw
	if s, ok := raw.(string); ok {
		n := strings.TrimSpace(strings.ToLower(s))
		switch n {
		case "required":
			mapped = map[string]any{"type": "any"}
		case "none":
			mapped = map[string]any{"type": "none"}
		case "auto":
			mapped = map[string]any{"type": "auto"}
		case "any":
			mapped = map[string]any{"type": "any"}
		}
	}
	if m, ok := raw.(map[string]any); ok {
		t := strings.ToLower(shared.AsTrimmedString(m["type"]))
		if t == "function" {
			if fn, ok := m["function"].(map[string]any); ok {
				if name := shared.AsTrimmedString(fn["name"]); name != "" {
					mapped = map[string]any{"type": "tool", "name": name}
				} else {
					return nil
				}
			}
		}
	}
	tc, _ := sanitizeToolChoice(mapped)
	return tc
}

func isAnthropicWebSearchTool(m map[string]any) bool {
	t := strings.ToLower(shared.AsTrimmedString(m["type"]))
	name := strings.ToLower(shared.AsTrimmedString(m["name"]))
	return anthropicWebSearchToolTypes[t] || name == "web_search" || name == "google_search"
}

func resolveOpenAIReasoningSettings(body map[string]any) (thinking, outputConfig map[string]any) {
	explicit, _ := body["reasoning"].(map[string]any)
	effort := strings.ToLower(shared.AsTrimmedString(body["reasoning_effort"]))
	if effort == "" && explicit != nil {
		effort = strings.ToLower(shared.AsTrimmedString(explicit["effort"]))
	}
	budget := shared.PickPositiveInt(body["reasoning_budget"])
	if budget == 0 && explicit != nil {
		budget = shared.PickPositiveInt(explicit["budget_tokens"])
		if budget == 0 {
			budget = shared.PickPositiveInt(explicit["max_tokens"])
		}
	}

	if budget > 0 {
		thinking = map[string]any{"type": "enabled", "budget_tokens": budget}
	} else if effort != "" && validAnthropicEfforts[effort] {
		thinking = map[string]any{"type": "adaptive"}
	}
	if effort != "" && validAnthropicEfforts[effort] {
		outputConfig = map[string]any{"effort": effort}
	}
	return
}

// --- Cache control optimization ---

func optimizeAnthropicCacheControls(body map[string]any) {
	normalizeAnthropicMessageContents(body)
	normalizeAnthropicSystemPrompts(body)
	clearAnthropicCacheControls(body)

	structural := ensureStructuralCacheControls(body)
	remaining := maxAnthropicCacheControlBreakpoints - structural
	if remaining <= 0 {
		sanitizeUnsupportedCacheControls(body)
		return
	}

	refs := collectCacheableMessageRefs(body)
	if len(refs) == 0 {
		sanitizeUnsupportedCacheControls(body)
		return
	}

	desired := 1
	if len(refs) >= adaptiveAnthropicCacheControlBlockWindow {
		desired = 2
	}
	target := desired
	if target > remaining {
		target = remaining
	}
	if target <= 0 {
		sanitizeUnsupportedCacheControls(body)
		return
	}

	used := make(map[int]bool)
	applyAnchor := func(idx int) {
		if idx < 0 || idx >= len(refs) || used[idx] {
			return
		}
		used[idx] = true
		m := cloneMap(refs[idx])
		m["cache_control"] = map[string]any{"type": "ephemeral"}
		refs[idx] = m
	}

	applyAnchor(len(refs) - 1)
	if target > 1 {
		ti := len(refs) - 1 - adaptiveAnthropicCacheControlBlockWindow
		if ti < 0 {
			ti = 0
		}
		chosen := -1
		for i := ti; i >= 0; i-- {
			if !used[i] {
				chosen = i
				break
			}
		}
		if chosen < 0 {
			for i := ti + 1; i < len(refs); i++ {
				if !used[i] {
					chosen = i
					break
				}
			}
		}
		applyAnchor(chosen)
	}

	// Write back
	msgs, _ := body["messages"].([]any)
	writeIdx := 0
	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			writeIdx++
			continue
		}
		content, ok := m["content"].([]any)
		if !ok {
			writeIdx++
			continue
		}
		for ci := range content {
			if writeIdx+ci < len(refs) && refs[writeIdx+ci] != nil {
				content[ci] = refs[writeIdx+ci]
			}
		}
		writeIdx += len(content)
	}

	sanitizeUnsupportedCacheControls(body)
}

func collectCacheableMessageRefs(body map[string]any) []map[string]any {
	var refs []map[string]any
	msgs, _ := body["messages"].([]any)
	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := m["content"].([]any)
		if !ok {
			continue
		}
		for _, item := range content {
			im, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if isCacheableBlock(im) {
				refs = append(refs, im)
			}
		}
	}
	return refs
}

func isCacheableBlock(m map[string]any) bool {
	t := strings.ToLower(shared.AsTrimmedString(m["type"]))
	if t == "thinking" || t == "redacted_thinking" {
		return false
	}
	if t == "text" || t == "input_text" || t == "output_text" {
		return shared.AsTrimmedString(m["text"]) != ""
	}
	return true
}

func normalizeAnthropicMessageContents(body map[string]any) {
	msgs, _ := body["messages"].([]any)
	for i, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok || m["content"] == nil {
			continue
		}
		if _, isArr := m["content"].([]any); isArr {
			continue
		}
		if s, ok := m["content"].(string); ok && s != "" {
			m["content"] = []any{map[string]any{"type": "text", "text": s}}
			msgs[i] = m
		}
	}
	body["messages"] = msgs
}

func normalizeAnthropicSystemPrompts(body map[string]any) {
	sys, ok := body["system"]
	if !ok {
		return
	}
	if _, isArr := sys.([]any); isArr {
		return
	}
	if s, ok := sys.(string); ok && s != "" {
		body["system"] = []any{map[string]any{"type": "text", "text": s}}
	}
}

func clearAnthropicCacheControls(body map[string]any) {
	if tools, ok := body["tools"].([]any); ok {
		for i, t := range tools {
			if m, ok := t.(map[string]any); ok {
				delete(m, "cache_control")
				tools[i] = m
			}
		}
	}
	if sys, ok := body["system"].([]any); ok {
		for i, s := range sys {
			if m, ok := s.(map[string]any); ok {
				delete(m, "cache_control")
				sys[i] = m
			}
		}
	}
	msgs, _ := body["messages"].([]any)
	for i, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		if content, ok := m["content"].([]any); ok {
			for j, item := range content {
				if im, ok := item.(map[string]any); ok {
					delete(im, "cache_control")
					content[j] = im
				}
			}
		}
		msgs[i] = m
	}
	body["messages"] = msgs
}

func ensureStructuralCacheControls(body map[string]any) int {
	count := 0
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		if m, ok := tools[len(tools)-1].(map[string]any); ok {
			m["cache_control"] = map[string]any{"type": "ephemeral"}
			tools[len(tools)-1] = m
			count++
		}
	}
	if sys, ok := body["system"].([]any); ok && len(sys) > 0 {
		if m, ok := sys[len(sys)-1].(map[string]any); ok {
			m["cache_control"] = map[string]any{"type": "ephemeral"}
			sys[len(sys)-1] = m
			count++
		}
	}
	return count
}

func sanitizeUnsupportedCacheControls(body map[string]any) {
	msgs, _ := body["messages"].([]any)
	for i, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		if content, ok := m["content"].([]any); ok {
			for j, item := range content {
				im, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if !isCacheableBlock(im) {
					delete(im, "cache_control")
					content[j] = im
				}
			}
		}
		msgs[i] = m
	}
	body["messages"] = msgs
}

// --- Stream bridge ---

// StreamBridge provides Anthropic Messages SSE stream processing.
type StreamBridge struct {
	Ctx *shared.StreamTransformContext
	CC  *shared.ClaudeDownstreamContext
}

// NewStreamBridge creates a new stream bridge.
func NewStreamBridge(modelName string) *StreamBridge {
	return &StreamBridge{
		Ctx: shared.CreateStreamTransformContext(modelName),
		CC:  shared.CreateClaudeDownstreamContext(),
	}
}

// NormalizeEvent normalizes an upstream event.
func (sb *StreamBridge) NormalizeEvent(payload any) shared.NormalizedStreamEvent {
	return shared.NormalizeUpstreamStreamEvent(payload, sb.Ctx, sb.Ctx.Model)
}

// SerializeEvent serializes a normalized event to Anthropic SSE lines.
func (sb *StreamBridge) SerializeEvent(event shared.NormalizedStreamEvent) []string {
	return shared.SerializeNormalizedStreamEvent(shared.FormatClaude, event, sb.Ctx, sb.CC)
}

// SerializeDone writes the message_delta and message_stop events.
func (sb *StreamBridge) SerializeDone() []string {
	return shared.SerializeStreamDone(shared.FormatClaude, sb.Ctx, sb.CC)
}

// PullSseEvents extracts SSE events from a buffer.
func PullSseEvents(buffer string) ([]shared.ParsedSseEvent, string) {
	return shared.PullSseEventsWithDone(buffer)
}

// --- Helpers ---

func toFiniteNumber(v any) *float64 {
	n, ok := toFloat(v)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}

func toFiniteInt(v any, fallback int) int {
	n := toFiniteNumber(v)
	if n == nil {
		return fallback
	}
	return int(*n)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}


func deepCloneJSON(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var dst any
	if err := json.Unmarshal(b, &dst); err != nil {
		return nil
	}
	return dst
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
