package shared

import (
	"math"
	"strings"
	"time"
)

// ClaudeDownstreamContext tracks Anthropic content-block state during downstream serialization.
type ClaudeDownstreamContext struct {
	MessageStarted        bool
	ContentBlockStarted    bool
	DoneSent              bool
	TextBlockIndex        *int
	NextContentBlockIndex int
	ToolBlocks            map[int]*ClaudeToolBlockState

	// Extended fields
	ThinkingBlockIndex *int
	RedactedBlockIndex *int
	PendingSignature   string
	ActiveToolSlot     *int
}

type ClaudeToolBlockState struct {
	ContentIndex int
	ID           string
	Name         string
	Open         bool
}

func CreateClaudeDownstreamContext() *ClaudeDownstreamContext {
	return &ClaudeDownstreamContext{
		ToolBlocks: make(map[int]*ClaudeToolBlockState),
	}
}

// --- Downstream parse ---

type ParsedDownstreamChatRequest struct {
	RequestedModel    string
	IsStream          bool
	UpstreamBody      map[string]any
	ClaudeOriginalBody map[string]any
}

func ParseDownstreamChatRequest(body any, format DownstreamFormat) (*ParsedDownstreamChatRequest, *ErrorPayload) {
	raw, ok := body.(map[string]any)
	if !ok {
		raw = map[string]any{}
	}

	if format == FormatClaude {
		converted := convertClaudeRequestToOpenAiBody(raw)
		if converted.model == "" {
			return nil, &ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "model is required", "type": "invalid_request_error"},
			}}
		}
		if len(converted.messages) == 0 {
			return nil, &ErrorPayload{StatusCode: 400, Payload: map[string]any{
				"error": map[string]any{"message": "messages is required", "type": "invalid_request_error"},
			}}
		}
		return &ParsedDownstreamChatRequest{
			RequestedModel:    converted.model,
			IsStream:          converted.stream,
			UpstreamBody:      converted.payload,
			ClaudeOriginalBody: raw,
		}, nil
	}

	model := strings.TrimSpace(AsTrimmedString(raw["model"]))
	if model == "" {
		return nil, &ErrorPayload{StatusCode: 400, Payload: map[string]any{
			"error": map[string]any{"message": "model is required", "type": "invalid_request_error"},
		}}
	}
	msgs, _ := raw["messages"].([]any)
	if len(msgs) == 0 {
		hint := "messages is required"
		if raw["input"] != nil {
			hint = "messages is required for /v1/chat/completions. For Responses payload, use /v1/responses."
		}
		return nil, &ErrorPayload{StatusCode: 400, Payload: map[string]any{
			"error": map[string]any{"message": hint, "type": "invalid_request_error"},
		}}
	}
	return &ParsedDownstreamChatRequest{
		RequestedModel: model,
		IsStream:       raw["stream"] == true,
		UpstreamBody:   raw,
	}, nil
}

type ErrorPayload struct {
	StatusCode int
	Payload    any
}

func (e *ErrorPayload) Error() string { return "parse error" }

// --- Claude -> OpenAI body conversion ---

type claudeConvertResult struct {
	model    string
	stream   bool
	messages []map[string]any
	payload  map[string]any
}

func convertClaudeRequestToOpenAiBody(body map[string]any) claudeConvertResult {
	model := AsTrimmedString(body["model"])
	stream := body["stream"] == true
	var messages []map[string]any

	appendMsg := func(role, content string) {
		if content != "" {
			messages = append(messages, map[string]any{"role": role, "content": content})
		}
	}

	convertToolResult := func(content any) any {
		blocks := []map[string]any{}
		appendBlock := func(item any) {
			if m, ok := item.(map[string]any); ok {
				if b := toOpenAIContentBlockFromClaudeBlock(m); b != nil {
					blocks = append(blocks, b)
					return
				}
			}
			if t := parseClaudeMessageContent(item); t != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
			}
		}

		var toProcess any = content
		if m, ok := content.(map[string]any); ok {
			if arr, ok := m["content"].([]any); ok {
				toProcess = arr
			}
		}

		if arr, ok := toProcess.([]any); ok {
			for _, item := range arr {
				appendBlock(item)
			}
		} else {
			appendBlock(toProcess)
		}
		return collapseOpenAIContentBlocks(blocks)
	}

	appendToolResultMsg := func(toolUseID any, content any) {
		id := AsTrimmedString(toolUseID)
		if id == "" {
			return
		}
		cp := convertToolResult(content)
		if cp == nil {
			return
		}
		messages = append(messages, map[string]any{"role": "tool", "tool_call_id": id, "content": cp})
	}

	sys := body["system"]
	if s, ok := sys.(string); ok {
		appendMsg("system", s)
	} else if arr, ok := sys.([]any); ok {
		var parts []string
		for _, item := range arr {
			if t := parseClaudeMessageContent(item); t != "" {
				parts = append(parts, t)
			}
		}
		if merged := strings.Join(parts, "\n\n"); merged != "" {
			appendMsg("system", merged)
		}
	}

	rawMsgs, _ := body["messages"].([]any)
	for _, msg := range rawMsgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role := AsTrimmedString(m["role"])
		if role == "" {
			role = "user"
		}
		mappedRole := role
		if mappedRole != "assistant" && mappedRole != "system" {
			mappedRole = "user"
		}
		content := m["content"]

		if arr, ok := content.([]any); ok {
			var cbs []map[string]any
			var tcs []map[string]any
			var rps []string

			for _, block := range arr {
				bm, ok := block.(map[string]any)
				if !ok {
					continue
				}
				bt := AsTrimmedString(bm["type"])

				if bt == "tool_result" {
					appendToolResultMsg(bm["tool_use_id"], bm["content"])
					continue
				}
				if mappedRole == "assistant" && bt == "tool_use" {
					id := AsTrimmedString(bm["id"])
					if id == "" {
						id = "call_" + itoa(int64(len(tcs)))
					}
					name := AsTrimmedString(bm["name"])
					args := ""
					if s, ok := bm["input"].(string); ok {
						args = s
					} else {
						args = StringifyUnknownValue(bm["input"])
					}
					if args == "" {
						args = "{}"
					}
					fn := map[string]any{"arguments": args}
					if name != "" {
						fn["name"] = name
					}
					tcs = append(tcs, map[string]any{"id": id, "type": "function", "function": fn})
					continue
				}

				extracted := ExtractInlineThinkTags(StringifyUnknownValue(block))
				if mappedRole == "assistant" && extracted.Reasoning != "" {
					rps = append(rps, extracted.Reasoning)
				}
				if cb := toOpenAIContentBlockFromClaudeBlock(bm); cb != nil {
					cbs = append(cbs, cb)
					continue
				}
				t := extracted.Content
				if t == "" {
					t = parseClaudeMessageContent(block)
				}
				if t != "" {
					cbs = append(cbs, map[string]any{"type": "text", "text": t})
				}
			}

			merged := collapseOpenAIContentBlocks(cbs)
			if len(tcs) > 0 {
				am := map[string]any{"role": "assistant", "tool_calls": tcs}
				if merged != nil {
					am["content"] = merged
				} else {
					am["content"] = ""
				}
				if rc := JoinNonEmpty(rps); rc != "" {
					am["reasoning_content"] = rc
				}
				messages = append(messages, am)
			} else if merged != nil || (mappedRole == "assistant" && len(rps) > 0) {
				nm := map[string]any{"role": mappedRole}
				if merged != nil {
					nm["content"] = merged
				} else {
					nm["content"] = ""
				}
				if mappedRole == "assistant" {
					if rc := JoinNonEmpty(rps); rc != "" {
						nm["reasoning_content"] = rc
					}
				}
				messages = append(messages, nm)
			}
		} else {
			appendMsg(mappedRole, parseClaudeMessageContent(content))
		}
	}

	payload := map[string]any{"model": model, "stream": stream, "messages": messages}

	if t := PickFiniteNumber(body["temperature"]); t != 0 {
		payload["temperature"] = t
	}
	if tp := PickFiniteNumber(body["top_p"]); tp != 0 {
		payload["top_p"] = tp
	}
	if md, ok := body["metadata"].(map[string]any); ok {
		payload["metadata"] = md
	}
	if mt := PickFiniteInt(body["max_tokens"]); mt > 0 {
		payload["max_tokens"] = mt
	} else {
		payload["max_tokens"] = 4096
	}
	if ss, ok := body["stop_sequences"].([]any); ok && len(ss) > 0 {
		payload["stop"] = ss
	}

	if re, rb := extractClaudeReasoning(body); re != "" {
		payload["reasoning_effort"] = re
	} else if rb > 0 {
		payload["reasoning_budget"] = rb
	}

	if tools := body["tools"]; tools != nil {
		payload["tools"] = convertClaudeToolsToOpenAIChat(tools)
	}
	if tc := body["tool_choice"]; tc != nil {
		payload["tool_choice"] = convertClaudeToolChoiceToOpenAI(tc)
	}
	if pck := AsTrimmedString(body["prompt_cache_key"]); pck != "" {
		payload["prompt_cache_key"] = pck
	}
	// previous_response_id is Responses-only. Never copy it onto chat bodies
	// (avoids opaque upstream 400 "Unsupported parameter: previous_response_id").
	// Continuity for Responses is handled in transform/openai/responses and
	// canonical.ApplyOpenAIResponsesContinuation.

	return claudeConvertResult{
		model:    model,
		stream:   stream,
		messages: messages,
		payload:  payload,
	}
}

func parseClaudeMessageContent(v any) string {
	c := ExtractInlineThinkTags(StringifyUnknownValue(v))
	return c.Content
}

func toOpenAIContentBlockFromClaudeBlock(block map[string]any) map[string]any {
	bt := AsTrimmedString(block["type"])
	if bt == "text" {
		if t := AsTrimmedString(block["text"]); t != "" {
			return map[string]any{"type": "text", "text": t}
		}
		return nil
	}
	if bt != "image" && bt != "document" {
		t := parseClaudeMessageContent(block)
		if t != "" {
			return map[string]any{"type": "text", "text": t}
		}
		return nil
	}

	title := AsTrimmedString(block["title"])
	src, _ := block["source"].(map[string]any)
	if src == nil {
		return nil
	}
	srcType := AsTrimmedString(src["type"])
	mimeType := AsTrimmedString(src["media_type"])
	if mimeType == "" {
		mimeType = AsTrimmedString(src["mime_type"])
	}

	if bt == "image" || strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		if srcType == "base64" {
			if mt := AsTrimmedString(src["media_type"]); mt != "" {
				if data := AsTrimmedString(src["data"]); data != "" {
					return map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mt + ";base64," + data}}
				}
			}
		}
		if url := AsTrimmedString(src["url"]); url != "" {
			return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
		}
		return nil
	}

	file := map[string]any{}
	if srcType == "base64" && AsTrimmedString(src["data"]) != "" {
		file["file_data"] = AsTrimmedString(src["data"])
	}
	if srcType == "url" && AsTrimmedString(src["url"]) != "" {
		file["file_url"] = AsTrimmedString(src["url"])
	}
	if title != "" {
		file["filename"] = title
	}
	if mimeType != "" {
		file["mime_type"] = mimeType
	}
	if len(file) == 0 {
		return nil
	}
	return map[string]any{"type": "file", "file": file}
}

func collapseOpenAIContentBlocks(blocks []map[string]any) any {
	if len(blocks) == 0 {
		return nil
	}
	allText := true
	var texts []string
	for _, b := range blocks {
		if b["type"] != "text" {
			allText = false
		}
		if s, ok := b["text"].(string); ok {
			texts = append(texts, s)
		}
	}
	if allText {
		return strings.TrimSpace(strings.Join(texts, "\n\n"))
	}
	return blocks
}

func extractClaudeReasoning(body map[string]any) (effort string, budget int) {
	thinking, _ := body["thinking"].(map[string]any)
	outputConfig, _ := body["output_config"].(map[string]any)
	if outputConfig == nil {
		outputConfig, _ = body["outputConfig"].(map[string]any)
	}
	effort = AsTrimmedString(outputConfig["effort"])
	if thinking != nil {
		budget = PickPositiveInt(thinking["budget_tokens"])
		if budget == 0 {
			budget = PickPositiveInt(thinking["budgetTokens"])
		}
	}
	return
}

func convertClaudeToolsToOpenAIChat(raw any) any {
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
		t := strings.ToLower(AsTrimmedString(m["type"]))
		name := AsTrimmedString(m["name"])
		if t == "web_search" || t == "web_search_20250305" || t == "google_search" || name == "web_search" || name == "google_search" {
			out = append(out, map[string]any{"type": "web_search", "name": "web_search"})
			continue
		}
		if t == "function" || t == "custom" || t == "image_generation" {
			out = append(out, item)
			continue
		}
		if name == "" {
			out = append(out, item)
			continue
		}
		params := map[string]any{"type": "object"}
		if is, ok := m["input_schema"].(map[string]any); ok {
			params = is
		} else if p, ok := m["parameters"].(map[string]any); ok {
			params = p
		}
		fn := map[string]any{"name": name, "parameters": params}
		if d := AsTrimmedString(m["description"]); d != "" {
			fn["description"] = d
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

func convertClaudeToolChoiceToOpenAI(raw any) any {
	if s, ok := raw.(string); ok {
		n := strings.TrimSpace(strings.ToLower(s))
		if n == "any" {
			return "required"
		}
		return n
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	t := strings.ToLower(AsTrimmedString(m["type"]))
	if t == "auto" || t == "none" {
		return t
	}
	if t == "any" || t == "required" {
		return "required"
	}
	if t == "function" {
		if fn, ok := m["function"].(map[string]any); ok {
			if name := AsTrimmedString(fn["name"]); name != "" {
				return map[string]any{"type": "function", "function": map[string]any{"name": name}}
			}
		}
		return "required"
	}
	if t != "tool" {
		return raw
	}
	name := AsTrimmedString(m["name"])
	if name == "" {
		if tool, ok := m["tool"].(map[string]any); ok {
			name = AsTrimmedString(tool["name"])
		}
	}
	if name != "" {
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}
	}
	return "required"
}

// --- Normalize upstream final response ---

func NormalizeUpstreamFinalResponse(payload any, fallbackModel, fallbackText string) NormalizedFinalResponse {
	now := time.Now().Unix()
	fallbackID := "chatcmpl-meta-" + itoa(time.Now().UnixMilli())

	if m, ok := payload.(map[string]any); ok {
		if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
			choice, _ := choices[0].(map[string]any)
			content := extractAssistantContent(choice)
			if content == "" {
				content = extractAssistantContent(m)
			}
			reasoning := extractAssistantReasoning(choice)
			if reasoning == "" {
				reasoning = extractAssistantReasoning(m)
			}
			tcs := collectToolCallsFromOpenAIChoice(choice)
			id := AsTrimmedString(m["id"])
			if id == "" {
				id = fallbackID
			}
			md := AsTrimmedString(m["model"])
			if md == "" {
				md = fallbackModel
			}
			created := EnsureIntTimestamp(m["created"], now)
			fr := NormalizeStopReason(AsTrimmedString(choice["finish_reason"]))
			if fr == "" {
				fr = NormalizeStopReason(AsTrimmedString(m["stop_reason"]))
			}
			if len(tcs) > 0 && fr == "" {
				fr = "tool_calls"
			}
			if fr == "" {
				fr = "stop"
			}
			return NormalizedFinalResponse{
				ID: id, Model: md, Created: created,
				Content:          content,
				ReasoningContent: reasoning,
				FinishReason:     fr,
				ToolCalls:        tcs,
			}
		}

		if AsTrimmedString(m["type"]) == "message" {
			tcs := collectToolCallsFromClaudeContent(m["content"])
			id := AsTrimmedString(m["id"])
			if id == "" {
				id = fallbackID
			}
			md := AsTrimmedString(m["model"])
			if md == "" {
				md = fallbackModel
			}
			content := parseClaudeMessageContent(m["content"])
			if content == "" && len(tcs) > 0 {
				content = ""
			}
			reasoning := ExtractInlineThinkTags(StringifyUnknownValue(m["content"]))
			fr := NormalizeStopReason(AsTrimmedString(m["stop_reason"]))
			if len(tcs) > 0 && fr == "" {
				fr = "tool_calls"
			}
			if fr == "" {
				fr = "stop"
			}
			return NormalizedFinalResponse{
				ID: id, Model: md, Created: now,
				Content:          content,
				ReasoningContent: reasoning.Reasoning,
				FinishReason:     fr,
				ToolCalls:        tcs,
			}
		}

		if m["object"] == "response" || m["output"] != nil {
			tcs := collectToolCallsFromResponsesPayload(m)
			id := AsTrimmedString(m["id"])
			if id == "" {
				id = fallbackID
			}
			md := AsTrimmedString(m["model"])
			if md == "" {
				md = fallbackModel
			}
			created := EnsureIntTimestamp(m["created_at"], now)
			if created == now {
				created = EnsureIntTimestamp(m["created"], now)
			}
			content := parseResponsesOutputText(m)
			if content == "" && len(tcs) > 0 {
				content = ""
			}
			rr := parseResponsesReasoning(m)
			fr := responsesStatusToChatFinishReason(m["status"], m["incomplete_details"], len(tcs) > 0)
			return NormalizedFinalResponse{
				ID: id, Model: md, Created: created,
				Content:                  content,
				ReasoningContent:         rr.reasoningContent,
				ReasoningSignature:       rr.reasoningSignature,
				FinishReason:             fr,
				ToolCalls:                tcs,
			}
		}

		if candidates, ok := m["candidates"].([]any); ok && len(candidates) > 0 {
			candidate, _ := candidates[0].(map[string]any)
			var parsed TextReasoning
			if cp, ok := candidate["content"].(map[string]any); ok {
				if parts, ok := cp["parts"].([]any); ok {
					parsed = ExtractInlineThinkTags(StringifyUnknownValue(parts))
				} else {
					parsed = ExtractInlineThinkTags(StringifyUnknownValue(cp))
				}
			}
			id := AsTrimmedString(m["responseId"])
			if id == "" {
				id = fallbackID
			}
			md := AsTrimmedString(m["modelVersion"])
			if md == "" {
				md = fallbackModel
			}
			fr := NormalizeStopReason(AsTrimmedString(candidate["finishReason"]))
			if fr == "" {
				fr = NormalizeStopReason(AsTrimmedString(m["finishReason"]))
			}
			if fr == "" {
				fr = "stop"
			}
			return NormalizedFinalResponse{
				ID: id, Model: md, Created: now,
				Content:          parsed.Content,
				ReasoningContent: parsed.Reasoning,
				FinishReason:     fr,
				ToolCalls:        nil,
			}
		}
	}

	if s, ok := payload.(string); ok && strings.TrimSpace(s) != "" {
		return NormalizedFinalResponse{
			ID: fallbackID, Model: fallbackModel, Created: now,
			Content: s, FinishReason: "stop",
		}
	}

	return NormalizedFinalResponse{
		ID: fallbackID, Model: fallbackModel, Created: now,
		Content: fallbackText, FinishReason: "stop",
	}
}

func extractAssistantContent(choice any) string {
	m, _ := choice.(map[string]any)
	if m == nil {
		return ""
	}
	if msg, ok := m["message"].(map[string]any); ok {
		c := ExtractInlineThinkTags(StringifyUnknownValue(msg["content"]))
		if c.Content != "" {
			return c.Content
		}
	}
	c := ExtractInlineThinkTags(StringifyUnknownValue(m["content"]))
	if c.Content != "" {
		return c.Content
	}
	if s, ok := m["text"].(string); ok && s != "" {
		c2 := ExtractInlineThinkTags(s)
		return c2.Content
	}
	if s, ok := m["completion"].(string); ok && s != "" {
		c2 := ExtractInlineThinkTags(s)
		return c2.Content
	}
	if delta, ok := m["delta"].(map[string]any); ok {
		if s, ok := delta["content"].(string); ok && s != "" {
			c2 := ExtractInlineThinkTags(s)
			return c2.Content
		}
	}
	return ""
}

func extractAssistantReasoning(choice any) string {
	m, _ := choice.(map[string]any)
	if m == nil {
		return ""
	}
	msg, _ := m["message"].(map[string]any)
	var candidates []string
	if msg != nil {
		candidates = append(candidates,
			AsTrimmedString(msg["reasoning_content"]),
			AsTrimmedString(msg["reasoning"]),
			ExtractReasoningDetailsText(msg["reasoning_details"]),
			ExtractReasoningDetailsText(msg["reasoning_detail"]),
		)
	}
	candidates = append(candidates,
		AsTrimmedString(m["reasoning_content"]),
		AsTrimmedString(m["reasoning"]),
		ExtractReasoningDetailsText(m["reasoning_details"]),
		ExtractReasoningDetailsText(m["reasoning_detail"]),
	)
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	if msg != nil {
		r := ExtractInlineThinkTags(StringifyUnknownValue(msg["content"]))
		if r.Reasoning != "" {
			return r.Reasoning
		}
	}
	r := ExtractInlineThinkTags(StringifyUnknownValue(m["content"]))
	return r.Reasoning
}

func collectToolCallsFromOpenAIChoice(choice any) []ToolCall {
	m, _ := choice.(map[string]any)
	if m == nil {
		return nil
	}
	msg, _ := m["message"].(map[string]any)
	var raw []any
	if msg != nil {
		raw, _ = msg["tool_calls"].([]any)
	}
	if raw == nil {
		raw, _ = m["tool_calls"].([]any)
	}
	var tcs []ToolCall
	for i, item := range raw {
		tcm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tcm["function"].(map[string]any)
		if fn == nil {
			fn = map[string]any{}
		}
		id := AsTrimmedString(tcm["id"])
		if id == "" {
			id = "call_" + itoa(int64(i))
		}
		name := AsTrimmedString(fn["name"])
		if name == "" {
			name = AsTrimmedString(tcm["name"])
		}
		args := ""
		if s, ok := fn["arguments"].(string); ok {
			args = s
		} else {
			args = StringifyUnknownValue(fn["arguments"])
		}
		tcs = append(tcs, ToolCall{ID: id, Name: name, Arguments: args})
	}
	return tcs
}

func collectToolCallsFromClaudeContent(content any) []ToolCall {
	arr, _ := content.([]any)
	var tcs []ToolCall
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok || m["type"] != "tool_use" {
			continue
		}
		id := AsTrimmedString(m["id"])
		if id == "" {
			id = "toolu_" + itoa(int64(i))
		}
		name := AsTrimmedString(m["name"])
		args := StringifyUnknownValue(m["input"])
		tcs = append(tcs, ToolCall{ID: id, Name: name, Arguments: args})
	}
	return tcs
}

func collectToolCallsFromResponsesPayload(m map[string]any) []ToolCall {
	output, _ := m["output"].([]any)
	var tcs []ToolCall
	for i, item := range output {
		om, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ot := AsTrimmedString(om["type"])
		if ot != "function_call" && ot != "custom_tool_call" {
			continue
		}
		id := AsTrimmedString(om["call_id"])
		if id == "" {
			id = AsTrimmedString(om["id"])
		}
		if id == "" {
			id = "call_" + itoa(int64(i))
		}
		name := AsTrimmedString(om["name"])
		args := ""
		if s, ok := om["arguments"].(string); ok {
			args = s
		} else if s, ok := om["input"].(string); ok {
			args = s
		} else {
			args = StringifyUnknownValue(om["arguments"])
		}
		if args == "" {
			args = StringifyUnknownValue(om["input"])
		}
		tcs = append(tcs, ToolCall{ID: id, Name: name, Arguments: args})
	}
	return tcs
}

func parseResponsesOutputText(m map[string]any) string {
	if s, ok := m["output_text"].(string); ok && s != "" {
		return s
	}
	output, _ := m["output"].([]any)
	var parts []string
	for _, item := range output {
		om, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := ExtractInlineThinkTags(StringifyUnknownValue(om["content"]))
		if c.Content != "" {
			parts = append(parts, c.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

type responsesReasoningResult struct {
	reasoningContent   string
	reasoningSignature string
}

func parseResponsesReasoning(m map[string]any) responsesReasoningResult {
	// Prefer output[] (final response). Also accept input[] so multi-turn
	// reasoning items replayed by Hermes/Codex can be parsed for round-trip
	// fixtures and stream/final normalize (#50 / upstream #538).
	items := firstResponsesItemArray(m, "output", "input")
	var parts []string
	var sig string
	for _, item := range items {
		om, ok := item.(map[string]any)
		if !ok || strings.ToLower(AsTrimmedString(om["type"])) != "reasoning" {
			continue
		}
		// Required content sources (do not drop any):
		//   summary (array of summary_text / string)
		//   content / text (some gateways / clients)
		// encrypted_content is the continuity signature, not visible text.
		text := extractResponsesReasoningItemText(om)
		if text != "" {
			parts = append(parts, text)
		}
		if sig == "" {
			if ec := AsTrimmedString(om["encrypted_content"]); ec != "" {
				sig = ec
			}
		}
	}
	return responsesReasoningResult{
		reasoningContent:   JoinNonEmpty(parts),
		reasoningSignature: sig,
	}
}

// firstResponsesItemArray returns the first present []any among keys.
func firstResponsesItemArray(m map[string]any, keys ...string) []any {
	if m == nil {
		return nil
	}
	for _, k := range keys {
		if arr, ok := m[k].([]any); ok && len(arr) > 0 {
			return arr
		}
	}
	// Still return empty slice if key exists as empty array (caller iterates).
	for _, k := range keys {
		if arr, ok := m[k].([]any); ok {
			return arr
		}
	}
	return nil
}

// extractResponsesReasoningItemText flattens summary/content/text on a
// type=reasoning item into visible reasoning text. Prefers summary, then
// content, then text — without double-counting identical values. Never uses
// encrypted_content as text.
func extractResponsesReasoningItemText(om map[string]any) string {
	if om == nil {
		return ""
	}
	// Prefer summary (Responses native), then content/text fallbacks.
	for _, key := range []string{"summary", "content", "text"} {
		if t := flattenResponsesReasoningText(om[key]); t != "" {
			tr := ExtractInlineThinkTags(t)
			return JoinNonEmpty([]string{tr.Content, tr.Reasoning})
		}
	}
	return ""
}

func flattenResponsesReasoningText(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		var parts []string
		for _, item := range x {
			if t := flattenResponsesReasoningText(item); t != "" {
				parts = append(parts, t)
			}
		}
		return JoinNonEmpty(parts)
	case map[string]any:
		// summary_text / text / content blocks
		for _, key := range []string{"text", "content", "summary_text", "output_text", "reasoning", "reasoning_content"} {
			if t := AsTrimmedString(x[key]); t != "" {
				return t
			}
		}
		if s, ok := x["summary"]; ok {
			return flattenResponsesReasoningText(s)
		}
		// Fallback: stringify only if it looks like a leaf text container.
		return ""
	default:
		return ""
	}
}

func responsesStatusToChatFinishReason(status, incompleteDetails any, hasToolCalls bool) string {
	ns := strings.ToLower(AsTrimmedString(status))
	if ns == "completed" {
		if hasToolCalls {
			return "tool_calls"
		}
		return "stop"
	}
	if ns == "incomplete" {
		id, _ := incompleteDetails.(map[string]any)
		if id != nil && strings.ToLower(AsTrimmedString(id["reason"])) == "max_output_tokens" {
			return "length"
		}
		return "stop"
	}
	if ns == "failed" {
		return "stop"
	}
	if hasToolCalls {
		return "tool_calls"
	}
	return "stop"
}

// --- Normalize upstream stream event ---

func NormalizeUpstreamStreamEvent(payload any, ctx *StreamTransformContext, fallbackModel string) NormalizedStreamEvent {
	m, ok := payload.(map[string]any)
	if !ok {
		return NormalizedStreamEvent{}
	}

	// OpenAI chat
	if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
		if IsNonEmptyString(m["id"]) {
			ctx.ID = AsTrimmedString(m["id"])
		}
		if IsNonEmptyString(m["model"]) {
			ctx.Model = AsTrimmedString(m["model"])
		}
		ctx.Created = EnsureIntTimestamp(m["created"], ctx.Created)

		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			delta = map[string]any{}
		}

		// Prefer delta.content; only fall back to message.content when delta has no content.
		// Never feed both into the think-tag parser — that would double-consume parser state.
		rawTagged := AsTrimmedString(delta["content"])
		if rawTagged == "" {
			if msgContent, ok := choice["message"].(map[string]any); ok {
				rawTagged = AsTrimmedString(msgContent["content"])
			}
		}
		dpContent, dpReasoning := ConsumeThinkTaggedText(ctx.ThinkTagParser, rawTagged)

		reasoningDelta := AsTrimmedString(delta["reasoning_content"])
		if reasoningDelta == "" {
			reasoningDelta = AsTrimmedString(delta["reasoning"])
		}
		if reasoningDelta == "" {
			reasoningDelta = ExtractReasoningDetailsText(delta["reasoning_details"])
		}
		if reasoningDelta == "" {
			if msgContent, ok := choice["message"].(map[string]any); ok {
				reasoningDelta = AsTrimmedString(msgContent["reasoning_content"])
				if reasoningDelta == "" {
					reasoningDelta = AsTrimmedString(msgContent["reasoning"])
				}
				if reasoningDelta == "" {
					reasoningDelta = ExtractReasoningDetailsText(msgContent["reasoning_details"])
				}
			}
		}
		if reasoningDelta == "" {
			reasoningDelta = dpReasoning
		} else if dpReasoning != "" {
			reasoningDelta = JoinNonEmpty([]string{reasoningDelta, dpReasoning})
		}

		contentDelta := dpContent
		if reasoningDelta != "" && contentDelta == reasoningDelta {
			contentDelta = ""
		}

		finishReason := NormalizeStopReason(AsTrimmedString(choice["finish_reason"]))
		if finishReason != "" {
			fc, fr := FlushThinkTaggedText(ctx.ThinkTagParser)
			if fc != "" {
				contentDelta += fc
			}
			if fr != "" {
				reasoningDelta = JoinNonEmpty([]string{reasoningDelta, fr})
			}
		}

		reasoningSig := ""
		if s := AsTrimmedString(delta["reasoning_signature"]); s != "" {
			reasoningSig = s
		}

		var tcds []ToolCallDelta
		if rawTCs, ok := delta["tool_calls"].([]any); ok {
			for i, item := range rawTCs {
				tcm, ok := item.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := tcm["function"].(map[string]any)
				if fn == nil {
					fn = map[string]any{}
				}
				idx := PickFiniteInt(tcm["index"])
				if idx == 0 && tcm["index"] == nil {
					idx = i
				}
				id := AsTrimmedString(tcm["id"])
				name := AsTrimmedString(fn["name"])
				ad := AsTrimmedString(fn["arguments"])
				if id == "" && name == "" && ad == "" {
					continue
				}
				td := ToolCallDelta{Index: idx}
				if id != "" {
					td.ID = id
				}
				if name != "" {
					td.Name = name
				}
				if ad != "" {
					td.ArgumentsDelta = ad
				}
				tcds = append(tcds, td)
			}
		}

		role := ""
		if AsTrimmedString(delta["role"]) == "assistant" {
			role = "assistant"
		}

		return NormalizedStreamEvent{
			Role:               role,
			ContentDelta:       contentDelta,
			ReasoningDelta:     reasoningDelta,
			ReasoningSignature: reasoningSig,
			ToolCallDeltas:     tcds,
			FinishReason:       finishReason,
		}
	}

	// Anthropic messages
	msg, _ := m["message"].(map[string]any)
	if msg != nil {
		if IsNonEmptyString(msg["id"]) {
			ctx.ID = AsTrimmedString(msg["id"])
		}
		if IsNonEmptyString(msg["model"]) {
			ctx.Model = AsTrimmedString(msg["model"])
		}
	}
	if ctx.Model == "" {
		ctx.Model = fallbackModel
	}

	t := AsTrimmedString(m["type"])
	switch t {
	case "message_start":
		return NormalizedStreamEvent{Role: "assistant"}
	case "content_block_start":
		cb, _ := m["content_block"].(map[string]any)
		idx := PickFiniteInt(m["index"])
		if cb != nil && cb["type"] == "tool_use" {
			id := AsTrimmedString(cb["id"])
			name := AsTrimmedString(cb["name"])
			var ad string
			if s, ok := cb["input"].(string); ok {
				ad = s
			} else {
				ad = SafeJSONString(cb["input"])
			}
			if ad == "{}" || ad == "[]" {
				ad = ""
			}
			return NormalizedStreamEvent{ToolCallDeltas: []ToolCallDelta{{Index: idx, ID: id, Name: name, ArgumentsDelta: ad}}}
		}
		cd, cr := ConsumeThinkTaggedText(ctx.ThinkTagParser, StringifyUnknownValue(cb))
		return NormalizedStreamEvent{ContentDelta: cd, ReasoningDelta: cr}
	case "content_block_delta":
		delta, _ := m["delta"].(map[string]any)
		if delta == nil {
			delta = map[string]any{}
		}
		dt := AsTrimmedString(delta["type"])
		if dt == "input_json_delta" {
			idx := PickFiniteInt(m["index"])
			return NormalizedStreamEvent{ToolCallDeltas: []ToolCallDelta{{Index: idx, ArgumentsDelta: AsTrimmedString(delta["partial_json"])}}}
		}
		cd, cr := ConsumeThinkTaggedText(ctx.ThinkTagParser, StringifyUnknownValue(delta))
		if dt == "thinking_delta" {
			return NormalizedStreamEvent{ReasoningDelta: JoinNonEmpty([]string{cd, cr})}
		}
		return NormalizedStreamEvent{ContentDelta: cd, ReasoningDelta: cr}
	case "message_delta":
		delta, _ := m["delta"].(map[string]any)
		if delta == nil {
			delta = map[string]any{}
		}
		return NormalizedStreamEvent{FinishReason: NormalizeStopReason(AsTrimmedString(delta["stop_reason"]))}
	case "message_stop":
		return NormalizedStreamEvent{Done: true}
	}

	// Gemini
	if candidates, ok := m["candidates"].([]any); ok && len(candidates) > 0 {
		candidate, _ := candidates[0].(map[string]any)
		var parsed struct{ content, reasoning string }
		if cp, ok := candidate["content"].(map[string]any); ok {
			if parts, ok := cp["parts"].([]any); ok {
				parsed.content, parsed.reasoning = ConsumeThinkTaggedText(ctx.ThinkTagParser, StringifyUnknownValue(parts))
			} else {
				parsed.content, parsed.reasoning = ConsumeThinkTaggedText(ctx.ThinkTagParser, StringifyUnknownValue(cp))
			}
		}
		if IsNonEmptyString(m["modelVersion"]) {
			ctx.Model = AsTrimmedString(m["modelVersion"])
		}
		return NormalizedStreamEvent{
			ContentDelta:   parsed.content,
			ReasoningDelta: parsed.reasoning,
			FinishReason:   NormalizeStopReason(AsTrimmedString(candidate["finishReason"])),
		}
	}

	// Responses
	rt := AsTrimmedString(m["type"])
	if strings.HasPrefix(rt, "response.output_text") {
		oi := extractResponsesOutputIndex(m)
		rawText := AsTrimmedString(m["delta"])
		if rawText == "" {
			rawText = AsTrimmedString(m["text"])
		}
		cd, cr := ConsumeThinkTaggedText(ctx.ThinkTagParser, rawText)
		nextContent := ctx.ResponsesTextByIndex[oi] + cd
		novel := cd
		if rt == "response.output_text.done" {
			novel = ComputeNovelResponsesDelta(ctx.ResponsesTextByIndex[oi], cd)
		}
		if nextContent != "" {
			ctx.ResponsesTextByIndex[oi] = nextContent
		}
		return NormalizedStreamEvent{ContentDelta: novel, ReasoningDelta: cr}
	}

	if rt == "response.created" {
		if rp, ok := m["response"].(map[string]any); ok {
			if IsNonEmptyString(rp["id"]) {
				ctx.ID = AsTrimmedString(rp["id"])
			}
			if IsNonEmptyString(rp["model"]) {
				ctx.Model = AsTrimmedString(rp["model"])
			}
			ctx.Created = EnsureIntTimestamp(rp["created_at"], ctx.Created)
		}
		return NormalizedStreamEvent{Role: "assistant"}
	}

	if rt == "response.incomplete" || rt == "response.failed" || rt == "error" {
		var fr string
		if rt == "response.incomplete" {
			_, _ = m["response"].(map[string]any)
			fr = responsesStatusToChatFinishReason("incomplete", m["incomplete_details"], false)
		} else if rt == "response.failed" {
			fr = responsesStatusToChatFinishReason("failed", nil, false)
		} else {
			fr = NormalizeStopReason(AsTrimmedString(m["status"]))
			if fr == "" {
				fr = "error"
			}
		}
		return NormalizedStreamEvent{FinishReason: fr, Done: true}
	}

	if rt == "response.completed" {
		if rp, ok := m["response"].(map[string]any); ok {
			if IsNonEmptyString(rp["id"]) {
				ctx.ID = AsTrimmedString(rp["id"])
			}
			if IsNonEmptyString(rp["model"]) {
				ctx.Model = AsTrimmedString(rp["model"])
			}
			content := parseResponsesOutputText(rp)
			cd := ComputeNovelResponsesDelta(JoinIndexedResponsesText(ctx.ResponsesTextByIndex), content)
			if content != "" {
				ctx.ResponsesTextByIndex = map[int]string{-1: content}
			}
			rr := parseResponsesReasoning(rp)
			rd := ComputeNovelResponsesDelta(JoinIndexedResponsesText(ctx.ResponsesReasoningByIndex), rr.reasoningContent)
			if rr.reasoningContent != "" {
				ctx.ResponsesReasoningByIndex = map[int]string{-1: rr.reasoningContent}
			}
			collectToolCallsFromResponsesPayload(rp)
			// TODO: collectIndexedToolCalls
			fr := NormalizeStopReason(AsTrimmedString(rp["status"]))
			if fr == "" {
				fr = "stop"
			}
			return NormalizedStreamEvent{
				Role:                "assistant",
				ContentDelta:        cd,
				ReasoningDelta:      rd,
				ReasoningSignature:  rr.reasoningSignature,
				FinishReason:        fr,
				Done:                true,
			}
		}
	}

	// Fallback: try to extract text
	cd, cr := ConsumeThinkTaggedText(ctx.ThinkTagParser, StringifyUnknownValue(payload))
	return NormalizedStreamEvent{ContentDelta: cd, ReasoningDelta: cr}
}

func extractResponsesOutputIndex(m map[string]any) int {
	if n, ok := m["output_index"].(float64); ok && !math.IsNaN(n) {
		ni := int(n)
		if ni < 0 {
			ni = 0
		}
		return ni
	}
	return 0
}

// --- Serialize normalized stream events ---

func SerializeNormalizedStreamEvent(format DownstreamFormat, event NormalizedStreamEvent, ctx *StreamTransformContext, cc *ClaudeDownstreamContext) []string {
	if format == FormatOpenAI {
		chunk := buildOpenAIStreamChunk(ctx, event)
		if chunk == nil {
			return nil
		}
		return []string{SerializeSSE("", chunk)}
	}

	// Claude format
	var lines []string
	needsStart := event.Role == "assistant" || event.ContentDelta != "" || event.ReasoningDelta != "" || len(event.ToolCallDeltas) > 0 || event.FinishReason != "" || event.Done
	if needsStart {
		lines = append(lines, ensureClaudeStartEvents(ctx, cc)...)
	}

	mergedText := JoinNonEmpty([]string{event.ReasoningDelta, event.ContentDelta})

	if len(event.ToolCallDeltas) > 0 {
		lines = append(lines, closeClaudeTextBlock(cc)...)
		for _, td := range event.ToolCallDeltas {
			tb := ensureClaudeToolBlockStart(cc, td)
			lines = append(lines, tb.events...)
			if td.ArgumentsDelta != "" {
				lines = append(lines, SerializeSSE("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": tb.contentIndex,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": td.ArgumentsDelta},
				}))
			}
		}
	}

	if mergedText != "" {
		lines = append(lines, closeClaudeToolBlocks(cc)...)
		lines = append(lines, ensureClaudeTextBlockStart(cc)...)
		ti := 0
		if cc.TextBlockIndex != nil {
			ti = *cc.TextBlockIndex
		}
		lines = append(lines, SerializeSSE("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": ti,
			"delta": map[string]any{"type": "text_delta", "text": mergedText},
		}))
	}

	if event.Done || event.FinishReason != "" {
		lines = append(lines, buildClaudeDoneEvents(ctx, cc, event.FinishReason)...)
	}

	return lines
}

func SerializeStreamDone(format DownstreamFormat, ctx *StreamTransformContext, cc *ClaudeDownstreamContext) []string {
	if ctx.DoneSent {
		return nil
	}
	ctx.DoneSent = true
	if format == FormatOpenAI {
		return []string{SerializeSSE("", "[DONE]")}
	}
	return buildClaudeDoneEvents(ctx, cc, "stop")
}

// --- OpenAI stream chunk building ---

func buildOpenAIStreamChunk(ctx *StreamTransformContext, event NormalizedStreamEvent) map[string]any {
	delta := map[string]any{}
	isInitial := !ctx.RoleSent && event.Role == "assistant" && event.ContentDelta == "" && event.ReasoningDelta == ""

	if !ctx.RoleSent && (event.Role == "assistant" || event.ContentDelta != "" || event.ReasoningDelta != "") {
		delta["role"] = "assistant"
		ctx.RoleSent = true
	} else if event.Role == "assistant" {
		delta["role"] = "assistant"
		ctx.RoleSent = true
	}

	if event.ContentDelta != "" {
		delta["content"] = event.ContentDelta
	}
	if event.ReasoningDelta != "" {
		delta["reasoning_content"] = event.ReasoningDelta
	}

	if len(event.ToolCallDeltas) > 0 {
		var tcs []map[string]any
		for _, td := range event.ToolCallDeltas {
			idx := td.Index
			if idx < 0 {
				idx = 0
			}
			existing := ctx.ToolCalls[idx]
			if existing == nil {
				existing = &ToolCallAccumulator{}
				ctx.ToolCalls[idx] = existing
			}
			if td.ID != "" {
				existing.ID = td.ID
			}
			if td.Name != "" {
				existing.Name = td.Name
			}
			existing.Arguments += td.ArgumentsDelta

			fn := map[string]any{}
			if td.Name != "" {
				fn["name"] = td.Name
			}
			if td.ArgumentsDelta != "" {
				fn["arguments"] = td.ArgumentsDelta
			}
			stc := map[string]any{"index": idx}
			if td.ID != "" {
				stc["id"] = td.ID
			}
			if td.ID != "" || td.Name != "" {
				stc["type"] = "function"
			}
			if len(fn) > 0 {
				stc["function"] = fn
			}
			tcs = append(tcs, stc)
		}
		if len(tcs) > 0 {
			delta["tool_calls"] = tcs
		}
	}

	if isInitial {
		delta["content"] = ""
	}

	fr := event.FinishReason
	if fr == "" {
		fr = ""
	}
	hasDelta := len(delta) > 0
	if !hasDelta && fr == "" {
		return nil
	}

	return map[string]any{
		"id":      ctx.ID,
		"object":  "chat.completion.chunk",
		"created": ctx.Created,
		"model":   ctx.Model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         delta,
			"finish_reason": fr,
		}},
	}
}

// --- Claude downstream serialization ---

func buildClaudeMessageID(sourceID string) string {
	if strings.HasPrefix(sourceID, "msg_") {
		return sourceID
	}
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, sourceID)
	if sanitized == "" || sanitized == "_" {
		sanitized = itoa(time.Now().UnixMilli())
	}
	return "msg_" + sanitized
}

func ensureClaudeStartEvents(ctx *StreamTransformContext, cc *ClaudeDownstreamContext) []string {
	if cc.MessageStarted {
		return nil
	}
	cc.MessageStarted = true
	return []string{SerializeSSE("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":           buildClaudeMessageID(ctx.ID),
			"type":         "message",
			"role":         "assistant",
			"model":        ctx.Model,
			"content":      []any{},
			"stop_reason":  nil,
			"stop_sequence": nil,
			"usage":        map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})}
}

func ensureClaudeTextBlockStart(cc *ClaudeDownstreamContext) []string {
	if cc.ContentBlockStarted && cc.TextBlockIndex != nil {
		return nil
	}
	ci := cc.NextContentBlockIndex
	cc.NextContentBlockIndex++
	cc.ContentBlockStarted = true
	cc.TextBlockIndex = &ci
	return []string{SerializeSSE("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": ci,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})}
}

func closeClaudeTextBlock(cc *ClaudeDownstreamContext) []string {
	if !cc.ContentBlockStarted || cc.TextBlockIndex == nil {
		return nil
	}
	ci := *cc.TextBlockIndex
	cc.ContentBlockStarted = false
	cc.TextBlockIndex = nil
	return []string{SerializeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": ci})}
}

type toolBlockStartResult struct {
	events       []string
	contentIndex int
}

func ensureClaudeToolBlockStart(cc *ClaudeDownstreamContext, td ToolCallDelta) toolBlockStartResult {
	slot := td.Index
	if slot < 0 {
		slot = 0
	}
	state := cc.ToolBlocks[slot]
	if state == nil {
		state = &ClaudeToolBlockState{
			ContentIndex: cc.NextContentBlockIndex,
			ID:           td.ID,
			Name:         td.Name,
			Open:         false,
		}
		if state.ID == "" {
			state.ID = "toolu_meta_" + itoa(int64(slot))
		}
		if state.Name == "" {
			state.Name = "tool_" + itoa(int64(slot))
		}
		cc.NextContentBlockIndex++
		cc.ToolBlocks[slot] = state
	} else {
		if td.ID != "" {
			state.ID = td.ID
		}
		if td.Name != "" {
			state.Name = td.Name
		}
	}

	var events []string
	if !state.Open {
		state.Open = true
		events = append(events, SerializeSSE("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": state.ContentIndex,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    state.ID,
				"name":  state.Name,
				"input": map[string]any{},
			},
		}))
	}
	return toolBlockStartResult{events: events, contentIndex: state.ContentIndex}
}

func closeClaudeToolBlocks(cc *ClaudeDownstreamContext) []string {
	var openBlocks []*ClaudeToolBlockState
	for _, b := range cc.ToolBlocks {
		if b.Open {
			openBlocks = append(openBlocks, b)
		}
	}
	if len(openBlocks) == 0 {
		return nil
	}
	// sort by content index
	for i := 0; i < len(openBlocks); i++ {
		for j := i + 1; j < len(openBlocks); j++ {
			if openBlocks[i].ContentIndex > openBlocks[j].ContentIndex {
				openBlocks[i], openBlocks[j] = openBlocks[j], openBlocks[i]
			}
		}
	}
	var events []string
	for _, b := range openBlocks {
		b.Open = false
		events = append(events, SerializeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": b.ContentIndex}))
	}
	return events
}

func buildClaudeDoneEvents(ctx *StreamTransformContext, cc *ClaudeDownstreamContext, finishReason string) []string {
	if cc.DoneSent {
		return nil
	}
	lines := ensureClaudeStartEvents(ctx, cc)
	lines = append(lines, closeClaudeTextBlock(cc)...)
	lines = append(lines, closeClaudeToolBlocks(cc)...)
	lines = append(lines, SerializeSSE("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   ToClaudeStopReason(finishReason),
			"stop_sequence": nil,
		},
		"usage": map[string]any{"output_tokens": 0},
	}))
	lines = append(lines, SerializeSSE("message_stop", map[string]any{"type": "message_stop"}))
	cc.DoneSent = true
	return lines
}

// --- Serialize final response ---

func SerializeFinalResponse(format DownstreamFormat, normalized NormalizedFinalResponse, usage struct{ PromptTokens, CompletionTokens, TotalTokens int }) map[string]any {
	tcs := normalized.ToolCalls
	if tcs == nil {
		tcs = []ToolCall{}
	}

	if format == FormatClaude {
		var cbs []map[string]any
		if normalized.ReasoningContent != "" {
			tb := map[string]any{"type": "thinking", "thinking": normalized.ReasoningContent}
			if normalized.ReasoningSignature != "" {
				tb["signature"] = normalized.ReasoningSignature
			}
			cbs = append(cbs, tb)
		}
		if normalized.RedactedReasoningContent != "" {
			cbs = append(cbs, map[string]any{"type": "redacted_thinking", "data": normalized.RedactedReasoningContent})
		}
		if normalized.Content != "" {
			cbs = append(cbs, map[string]any{"type": "text", "text": normalized.Content})
		}
		for i, tc := range tcs {
			id := tc.ID
			if id == "" {
				id = "toolu_" + itoa(int64(i))
			}
			name := tc.Name
			if name == "" {
				name = "tool_" + itoa(int64(i))
			}
			input := ParseJSONLike(tc.Arguments)
			cbs = append(cbs, map[string]any{"type": "tool_use", "id": id, "name": name, "input": input})
		}
		content := cbs
		if len(content) == 0 {
			content = []map[string]any{{"type": "text", "text": ""}}
		}
		return map[string]any{
			"id":           buildClaudeMessageID(normalized.ID),
			"type":         "message",
			"role":         "assistant",
			"model":        normalized.Model,
			"content":      content,
			"stop_reason":  ToClaudeStopReason(normalized.FinishReason),
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  usage.PromptTokens,
				"output_tokens": usage.CompletionTokens,
			},
		}
	}

	// OpenAI format
	msg := map[string]any{"role": "assistant", "content": normalized.Content}
	if normalized.ReasoningContent != "" {
		msg["reasoning_content"] = normalized.ReasoningContent
	}
	if normalized.ReasoningSignature != "" {
		msg["reasoning_signature"] = normalized.ReasoningSignature
	}
	if len(tcs) > 0 {
		var otcs []map[string]any
		for i, tc := range tcs {
			id := tc.ID
			if id == "" {
				id = "call_" + itoa(int64(i))
			}
			otcs = append(otcs, map[string]any{
				"index": i,
				"id":    id,
				"type":  "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		msg["tool_calls"] = otcs
		if normalized.Content == "" {
			msg["content"] = ""
		}
	}
	fr := NormalizeStopReason(normalized.FinishReason)
	if len(tcs) > 0 && fr == "" {
		fr = "tool_calls"
	}
	if fr == "" {
		fr = "stop"
	}
	return map[string]any{
		"id":      normalized.ID,
		"object":  "chat.completion",
		"created": normalized.Created,
		"model":   normalized.Model,
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": fr}},
		"usage": map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		},
	}
}

// --- Synthetic chunks for non-streaming ---

func BuildSyntheticOpenAiChunks(normalized NormalizedFinalResponse) []map[string]any {
	tcs := normalized.ToolCalls
	if tcs == nil {
		tcs = []ToolCall{}
	}
	fr := NormalizeStopReason(normalized.FinishReason)
	if len(tcs) > 0 && fr == "" {
		fr = "tool_calls"
	}
	if fr == "" {
		fr = "stop"
	}

	sd := map[string]any{"role": "assistant"}
	if normalized.Content != "" {
		sd["content"] = normalized.Content
	} else {
		sd["content"] = ""
	}
	if normalized.ReasoningContent != "" {
		sd["reasoning_content"] = normalized.ReasoningContent
	}
	if normalized.ReasoningSignature != "" {
		sd["reasoning_signature"] = normalized.ReasoningSignature
	}
	if len(tcs) > 0 {
		var otcs []map[string]any
		for i, tc := range tcs {
			id := tc.ID
			if id == "" {
				id = "call_" + itoa(int64(i))
			}
			otcs = append(otcs, map[string]any{
				"index": i, "id": id, "type": "function",
				"function": map[string]any{"name": tc.Name, "arguments": tc.Arguments},
			})
		}
		sd["tool_calls"] = otcs
	}

	return []map[string]any{
		{
			"id": normalized.ID, "object": "chat.completion.chunk",
			"created": normalized.Created, "model": normalized.Model,
			"choices": []map[string]any{{"index": 0, "delta": sd, "finish_reason": nil}},
		},
		{
			"id": normalized.ID, "object": "chat.completion.chunk",
			"created": normalized.Created, "model": normalized.Model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
		},
	}
}
