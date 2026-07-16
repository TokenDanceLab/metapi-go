package canonical

import (
	"encoding/json"
	"strings"
)

// --- helpers ---

func asTrimmedString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func isRecord(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil, false
	}
	return m, true
}


func safeJSONString(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func cloneJSONValue(v any) any {
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

func joinNonEmpty(parts []string) string {
	var result []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return strings.Join(result, "\n\n")
}

func normalizeRole(v any) CanonicalMessageRole {
	role := strings.ToLower(asTrimmedString(v))
	switch role {
	case "system":
		return RoleSystem
	case "developer":
		return RoleDeveloper
	case "assistant":
		return RoleAssistant
	case "tool":
		return RoleTool
	default:
		return RoleUser
	}
}

// --- canonicalRequestFromOpenAiBody ---

// CanonicalRequestFromOpenAiBodyInput mirrors the TS type.
type CanonicalRequestFromOpenAiBodyInput struct {
	Body         map[string]any
	Surface      CanonicalSurface
	CliProfile   CanonicalCliProfile
	Operation    CanonicalOperation
	Metadata     map[string]any
	Passthrough  map[string]any
	Continuation *CanonicalContinuation
}

// CanonicalRequestFromOpenAiBody converts an OpenAI-shaped body into a canonical envelope.
func CanonicalRequestFromOpenAiBody(input CanonicalRequestFromOpenAiBodyInput) (CanonicalRequestEnvelope, error) {
	body := input.Body
	if body == nil {
		body = map[string]any{}
	}

	var metadata map[string]any
	if input.Metadata != nil {
		metadata = input.Metadata
	} else if m, ok := body["metadata"].(map[string]any); ok {
		metadata = cloneJSONValue(m).(map[string]any)
	}

	var attachments []CanonicalAttachment
	if raw, ok := body["attachments"].([]any); ok {
		attachments = cloneJSONValue(raw).([]CanonicalAttachment)
	}

	messages := buildCanonicalMessages(body)

	reasoningResult := normalizeCanonicalReasoningRequest(body)

	continuation := ReadOpenAICompatibleContinuation(body, input.Continuation)

	passthrough := map[string]any{}
	if input.Passthrough != nil {
		for k, v := range input.Passthrough {
			passthrough[k] = v
		}
	}
	if parallelToolCalls, ok := body["parallel_tool_calls"].(bool); ok {
		passthrough["parallel_tool_calls"] = parallelToolCalls
	}
	if reasoningResult.metadata != nil {
		passthrough["transformerMetadata"] = reasoningResult.metadata
	}

	return CreateCanonicalRequestEnvelope(CreateCanonicalRequestEnvelopeInput{
		Operation:      input.Operation,
		Surface:        input.Surface,
		CliProfile:     input.CliProfile,
		RequestedModel: asTrimmedString(body["model"]),
		Stream:         body["stream"] == true,
		Messages:       messages,
		Reasoning:      reasoningResult.reasoning,
		Tools:          parseCanonicalTools(body["tools"]),
		ToolChoice:     parseCanonicalToolChoice(body["tool_choice"]),
		Continuation:   continuation,
		Metadata:       metadata,
		Attachments:    attachments,
		Passthrough:    passthrough,
	})
}

type reasoningNormalizeResult struct {
	reasoning *CanonicalReasoningRequest
	metadata  map[string]any
}

func normalizeCanonicalReasoningRequest(body map[string]any) reasoningNormalizeResult {
	include, _ := body["include"].([]any)
	var rawReasoning map[string]any
	if r, ok := body["reasoning"].(map[string]any); ok {
		rawReasoning = r
	}

	effort := CanonicalReasoningEffort(strings.ToLower(asTrimmedString(body["reasoning_effort"])))
	if effort == "" && rawReasoning != nil {
		effort = CanonicalReasoningEffort(strings.ToLower(asTrimmedString(rawReasoning["effort"])))
	}

	budgetTokens := 0
	for _, key := range []string{"reasoning_budget"} {
		if v, ok := getInt(body[key]); ok && v > 0 {
			budgetTokens = v
			break
		}
	}
	if budgetTokens == 0 && rawReasoning != nil {
		for _, key := range []string{"budget_tokens", "max_tokens"} {
			if v, ok := getInt(rawReasoning[key]); ok && v > 0 {
				budgetTokens = v
				break
			}
		}
	}

	summary := asTrimmedString(body["reasoning_summary"])
	if summary == "" && rawReasoning != nil {
		summary = asTrimmedString(rawReasoning["summary"])
	}

	if effort == "" && budgetTokens == 0 && summary == "" {
		return reasoningNormalizeResult{metadata: buildReasoningMetadata(include)}
	}

	return reasoningNormalizeResult{
		reasoning: &CanonicalReasoningRequest{
			Effort:                  effort,
			BudgetTokens:            budgetTokens,
			Summary:                 summary,
			IncludeEncryptedContent: effort != "",
		},
		metadata: buildReasoningMetadata(include),
	}
}

func buildReasoningMetadata(include []any) map[string]any {
	if len(include) == 0 {
		return nil
	}
	return map[string]any{"include": cloneJSONValue(include)}
}

func getInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func buildCanonicalMessages(body map[string]any) []CanonicalMessage {
	rawMessages, _ := body["messages"].([]any)
	var messages []CanonicalMessage
	for _, raw := range rawMessages {
		m, ok := isRecord(raw)
		if !ok {
			continue
		}
		role := normalizeRole(m["role"])

		if role == RoleTool {
			toolCallID := asTrimmedString(m["tool_call_id"])
			if toolCallID == "" {
				toolCallID = asTrimmedString(m["id"])
			}
			if toolCallID == "" {
				toolCallID = "tool"
			}
			rawContent := m["content"]
			resultText := ""
			if s, ok := rawContent.(string); ok {
				resultText = s
			} else if _, isArr := rawContent.([]any); !isArr {
				if _, isMap := rawContent.(map[string]any); !isMap {
					resultText = safeJSONString(rawContent)
				}
			}
			part := CanonicalContentPart{
				Type:       PartToolResult,
				ToolCallID: toolCallID,
			}
			if resultText != "" {
				part.ResultText = resultText
			}
			if arr, ok := rawContent.([]any); ok {
				part.ResultContent = cloneJSONValue(arr)
			} else if mp, ok := rawContent.(map[string]any); ok {
				part.ResultContent = []any{cloneJSONValue(mp)}
			}
			messages = append(messages, CanonicalMessage{
				Role:  role,
				Parts: []CanonicalContentPart{part},
			})
			continue
		}

		parts := openAIContentToCanonicalParts(m["content"])
		if role == RoleAssistant {
			appendAssistantReasoningPart(&parts, m)
		}
		toolCalls, _ := m["tool_calls"].([]any)
		for _, tc := range toolCalls {
			tcm, ok := isRecord(tc)
			if !ok {
				continue
			}
			fn, _ := tcm["function"].(map[string]any)
			if fn == nil {
				fn = map[string]any{}
			}
			id := asTrimmedString(tcm["id"])
			name := asTrimmedString(tcm["name"])
			if name == "" {
				name = asTrimmedString(fn["name"])
			}
			if name == "" {
				continue
			}
			// Skill-call and ordinary tools: preserve string or object arguments.
			argsJSON := coerceCanonicalToolArgumentsJSON(fn["arguments"])
			if argsJSON == "" {
				argsJSON = coerceCanonicalToolArgumentsJSON(tcm["arguments"])
			}
			if argsJSON == "" {
				argsJSON = "{}"
			}
			if id == "" {
				id = "tool_" + itoa(len(parts))
			}
			parts = append(parts, CanonicalContentPart{
				Type:          PartToolCall,
				ID:            id,
				Name:          name,
				ArgumentsJSON: argsJSON,
			})
		}

		msg := CanonicalMessage{
			Role:  role,
			Parts: parts,
		}
		if phase := asTrimmedString(m["phase"]); phase != "" {
			msg.Phase = phase
		}
		if sig := asTrimmedString(m["reasoning_signature"]); sig != "" {
			msg.ReasoningSignature = sig
		}
		messages = append(messages, msg)
	}
	return messages
}

func openAIContentToCanonicalParts(content any) []CanonicalContentPart {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []CanonicalContentPart{{Type: PartText, Text: s}}
	}

	arr, ok := content.([]any)
	if !ok {
		return nil
	}

	var parts []CanonicalContentPart
	for _, item := range arr {
		if s, ok := item.(string); ok {
			if s != "" {
				parts = append(parts, CanonicalContentPart{Type: PartText, Text: s})
			}
			continue
		}
		m, ok := isRecord(item)
		if !ok {
			continue
		}
		t := strings.ToLower(asTrimmedString(m["type"]))
		switch t {
		case "text", "input_text", "output_text":
			text := asTrimmedString(m["text"])
			if text != "" {
				parts = append(parts, CanonicalContentPart{Type: PartText, Text: text})
			}
		case "reasoning", "thinking", "redacted_reasoning":
			text := asTrimmedString(m["text"])
			if text == "" {
				text = asTrimmedString(m["reasoning"])
			}
			if text == "" {
				text = asTrimmedString(m["thinking"])
			}
			if text != "" {
				parts = append(parts, CanonicalContentPart{Type: PartText, Text: text, Thought: true})
			}
		case "image_url":
			if iu, ok := m["image_url"].(map[string]any); ok {
				url := asTrimmedString(iu["url"])
				if url != "" {
					parts = append(parts, CanonicalContentPart{Type: PartImage, URL: url})
				}
			}
		case "input_image":
			if iu, ok := m["image_url"].(map[string]any); ok {
				url := asTrimmedString(iu["url"])
				if url != "" {
					parts = append(parts, CanonicalContentPart{Type: PartImage, URL: url})
				}
			}
		case "input_file", "file":
			attachment := canonicalAttachmentFromInputFileBlock(m)
			if attachment != nil {
				p := CanonicalContentPart{Type: PartFile}
				if attachment.FileID != "" {
					p.FileID = attachment.FileID
				}
				if attachment.FileURL != "" {
					p.FileURL = attachment.FileURL
				}
				if attachment.FileData != "" {
					p.FileData = attachment.FileData
				}
				if attachment.MimeType != nil {
					p.MimeType = attachment.MimeType
				}
				if attachment.Filename != "" {
					p.Filename = attachment.Filename
				}
				parts = append(parts, p)
			}
		}
	}
	return parts
}

func appendAssistantReasoningPart(parts *[]CanonicalContentPart, rawMessage map[string]any) {
	reasoning := joinNonEmpty([]string{
		asTrimmedString(rawMessage["reasoning_content"]),
		asTrimmedString(rawMessage["reasoning"]),
	})
	if reasoning == "" {
		return
	}
	for _, p := range *parts {
		if p.Type == PartText && p.Thought && p.Text == reasoning {
			return
		}
	}
	*parts = append([]CanonicalContentPart{{Type: PartText, Text: reasoning, Thought: true}}, *parts...)
}

func canonicalAttachmentFromInputFileBlock(item map[string]any) *CanonicalAttachment {
	typeStr := strings.ToLower(asTrimmedString(item["type"]))
	if typeStr != "input_file" && typeStr != "file" {
		return nil
	}
	attachment := &CanonicalAttachment{Kind: "file"}
	if id := asTrimmedString(item["file_id"]); id != "" {
		attachment.FileID = id
	}
	if url := asTrimmedString(item["file_url"]); url != "" {
		attachment.FileURL = url
	}
	if data := asTrimmedString(item["file_data"]); data != "" {
		attachment.FileData = data
	}
	if fn := asTrimmedString(item["filename"]); fn != "" {
		attachment.Filename = fn
	}
	if mt := asTrimmedString(item["mime_type"]); mt != "" {
		attachment.MimeType = &mt
	}
	return attachment
}

// ReadOpenAICompatibleContinuation extracts continuation metadata from an OpenAI request body.
func ReadOpenAICompatibleContinuation(body map[string]any, input *CanonicalContinuation) *CanonicalContinuation {
	cont := &CanonicalContinuation{}
	if input != nil {
		cont.SessionID = input.SessionID
		cont.PreviousResponseID = input.PreviousResponseID
		cont.PromptCacheKey = input.PromptCacheKey
		cont.TurnState = input.TurnState
	}
	if cont.PromptCacheKey == "" {
		cont.PromptCacheKey = asTrimmedString(body["prompt_cache_key"])
	}
	if cont.PreviousResponseID == "" {
		cont.PreviousResponseID = asTrimmedString(body["previous_response_id"])
	}
	if cont.TurnState == "" {
		if m, ok := body["metadata"].(map[string]any); ok {
			cont.TurnState = asTrimmedString(m["metapi_turn_state"])
		}
	}
	return NormalizeCanonicalContinuation(cont)
}

func parseCanonicalTools(raw any) []CanonicalToolItem {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	var tools []CanonicalToolItem
	for _, item := range arr {
		m, ok := isRecord(item)
		if !ok {
			continue
		}
		itemType := strings.ToLower(asTrimmedString(m["type"]))

		if itemType == "function" {
			fn, ok := m["function"].(map[string]any)
			if !ok {
				continue
			}
			name := asTrimmedString(fn["name"])
			if name == "" {
				continue
			}
			t := CanonicalToolItem{FnName: name}
			if desc := asTrimmedString(fn["description"]); desc != "" {
				t.FnDescription = desc
			}
			if strict, ok := fn["strict"].(bool); ok {
				t.FnStrict = strict
			}
			// Clone full schema so required/properties survive Skill bridges.
			if params, ok := fn["parameters"].(map[string]any); ok {
				t.FnInputSchema = cloneJSONValue(params).(map[string]any)
			} else {
				t.FnInputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, t)
			continue
		}

		if (itemType == "" || itemType == "tool") && asTrimmedString(m["name"]) != "" {
			t := CanonicalToolItem{FnName: asTrimmedString(m["name"])}
			if desc := asTrimmedString(m["description"]); desc != "" {
				t.FnDescription = desc
			}
			if strict, ok := m["strict"].(bool); ok {
				t.FnStrict = strict
			}
			if schema, ok := m["input_schema"].(map[string]any); ok {
				t.FnInputSchema = cloneJSONValue(schema).(map[string]any)
			} else if schema, ok := m["inputSchema"].(map[string]any); ok {
				t.FnInputSchema = cloneJSONValue(schema).(map[string]any)
			}
			tools = append(tools, t)
			continue
		}

		if fds, ok := m["functionDeclarations"].([]any); ok {
			for _, decl := range fds {
				dm, ok := isRecord(decl)
				if !ok {
					continue
				}
				name := asTrimmedString(dm["name"])
				if name == "" {
					continue
				}
				t := CanonicalToolItem{FnName: name}
				if desc := asTrimmedString(dm["description"]); desc != "" {
					t.FnDescription = desc
				}
				if schema, ok := dm["parametersJsonSchema"].(map[string]any); ok {
					t.FnInputSchema = cloneJSONValue(schema).(map[string]any)
				} else if schema, ok := dm["parameters"].(map[string]any); ok {
					t.FnInputSchema = cloneJSONValue(schema).(map[string]any)
				}
				tools = append(tools, t)
			}
			continue
		}

		if itemType != "" {
			tools = append(tools, CanonicalToolItem{
				RawType: itemType,
				RawData: cloneJSONValue(m).(map[string]any),
			})
		}
	}
	if len(tools) == 0 {
		return nil
	}
	return tools
}

func parseCanonicalToolChoice(raw any) *CanonicalToolChoice {
	if raw == nil {
		return nil
	}

	if s, ok := raw.(string); ok {
		normalized := strings.TrimSpace(strings.ToLower(s))
		switch normalized {
		case "auto", "none", "required":
			return &CanonicalToolChoice{Type: normalized}
		case "any":
			return &CanonicalToolChoice{Type: "required"}
		default:
			if s != "" {
				return &CanonicalToolChoice{Type: "raw", Value: s}
			}
			return nil
		}
	}

	m, ok := isRecord(raw)
	if !ok {
		return nil
	}

	t := strings.ToLower(asTrimmedString(m["type"]))
	switch t {
	case "auto", "none":
		return &CanonicalToolChoice{Type: t}
	case "any", "required":
		return &CanonicalToolChoice{Type: "required"}
	case "function":
		fn, _ := m["function"].(map[string]any)
		name := asTrimmedString(fn["name"])
		if name == "" {
			name = asTrimmedString(m["name"])
		}
		if name != "" {
			return &CanonicalToolChoice{Type: "tool", Name: name}
		}
		return nil
	default:
		if t != "" && t != "tool" {
			return &CanonicalToolChoice{Type: "raw", Value: cloneJSONValue(m)}
		}
	}

	name := asTrimmedString(m["name"])
	if name == "" {
		if tool, ok := m["tool"].(map[string]any); ok {
			name = asTrimmedString(tool["name"])
		}
	}
	hasExtra := false
	for k := range m {
		if k != "type" && k != "name" && k != "tool" {
			hasExtra = true
			break
		}
	}
	if hasExtra {
		return &CanonicalToolChoice{Type: "raw", Value: cloneJSONValue(m)}
	}
	if name != "" {
		return &CanonicalToolChoice{Type: "tool", Name: name}
	}
	return &CanonicalToolChoice{Type: "raw", Value: cloneJSONValue(m)}
}

// --- canonicalRequestToOpenAiChatBody ---

// CanonicalRequestToOpenAiChatBody converts a canonical envelope to an OpenAI chat completion body.
func CanonicalRequestToOpenAiChatBody(req CanonicalRequestEnvelope) map[string]any {
	var messages []map[string]any

	for _, msg := range req.Messages {
		if msg.Role == RoleTool {
			for _, part := range msg.Parts {
				if part.Type != PartToolResult {
					continue
				}
				content := part.ResultContent
				if content == nil {
					if part.ResultText != "" {
						content = part.ResultText
					} else {
						content = safeJSONString(part.ResultJSON)
					}
				}
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": part.ToolCallID,
					"content":      content,
				})
			}
			continue
		}

		converted := canonicalPartsToOpenAIContent(msg.Role, msg.Parts)
		nextMsg := map[string]any{
			"role":    string(msg.Role),
			"content": converted.content,
		}
		if msg.Role == RoleAssistant && converted.reasoning != "" {
			nextMsg["reasoning_content"] = converted.reasoning
		}
		if msg.Phase != "" {
			nextMsg["phase"] = msg.Phase
		}
		if msg.ReasoningSignature != "" {
			nextMsg["reasoning_signature"] = msg.ReasoningSignature
		}
		if msg.Role == RoleAssistant && len(converted.toolCalls) > 0 {
			nextMsg["tool_calls"] = converted.toolCalls
			if contentArr, ok := nextMsg["content"].([]any); ok && len(contentArr) == 0 {
				nextMsg["content"] = ""
			}
		}
		messages = append(messages, nextMsg)
	}

	body := map[string]any{
		"model":    req.RequestedModel,
		"stream":   req.Stream,
		"messages": messages,
	}

	if req.Reasoning != nil {
		if req.Reasoning.Effort != "" {
			body["reasoning_effort"] = string(req.Reasoning.Effort)
		}
		if req.Reasoning.BudgetTokens > 0 {
			body["reasoning_budget"] = req.Reasoning.BudgetTokens
		}
		if req.Reasoning.Summary != "" {
			body["reasoning_summary"] = req.Reasoning.Summary
		}
	}

	var transformerMetadata map[string]any
	if tm, ok := req.Passthrough["transformerMetadata"].(map[string]any); ok {
		transformerMetadata = tm
	}
	var passthroughInclude []string
	if transformerMetadata != nil {
		if include, ok := transformerMetadata["include"].([]any); ok {
			for _, item := range include {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					passthroughInclude = append(passthroughInclude, strings.TrimSpace(s))
				}
			}
		}
	}
	if req.Reasoning != nil && req.Reasoning.IncludeEncryptedContent {
		passthroughInclude = append(passthroughInclude, "reasoning.encrypted_content")
	}
	// deduplicate
	seen := map[string]bool{}
	var mergedInclude []string
	for _, item := range passthroughInclude {
		if !seen[item] {
			seen[item] = true
			mergedInclude = append(mergedInclude, item)
		}
	}
	if len(mergedInclude) > 0 {
		body["include"] = mergedInclude
	}

	metadata := map[string]any{}
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			metadata[k] = v
		}
	}
	ApplyOpenAICompatibleContinuation(body, req.Continuation, metadata)
	if len(metadata) > 0 {
		// If continuation wrote into metadata, merge it
		body["metadata"] = metadata
	}

	if len(req.Attachments) > 0 {
		body["attachments"] = cloneJSONValue(req.Attachments)
	}

	if len(req.Tools) > 0 {
		var tools []any
		for _, tool := range req.Tools {
			if tool.IsRaw() {
				raw := cloneJSONValue(tool.RawData).(map[string]any)
				if rawType, ok := raw["type"].(string); !ok || rawType == "" {
					raw["type"] = tool.RawType
				}
				tools = append(tools, raw)
			} else {
				params := cloneJSONValue(tool.FnInputSchema)
				if params == nil {
					params = map[string]any{"type": "object"}
				}
				fn := map[string]any{
					"name":       tool.FnName,
					"parameters": params,
				}
				if tool.FnDescription != "" {
					fn["description"] = tool.FnDescription
				}
				if tool.FnStrict {
					fn["strict"] = true
				}
				tools = append(tools, map[string]any{
					"type":     "function",
					"function": fn,
				})
			}
		}
		body["tools"] = tools
	}

	tc := canonicalToolChoiceToOpenAI(req.ToolChoice)
	if tc != nil {
		body["tool_choice"] = tc
	}

	if req.Passthrough != nil {
		for k, v := range req.Passthrough {
			if k == "transformerMetadata" || body[k] != nil {
				continue
			}
			body[k] = cloneJSONValue(v)
		}
	}

	return body
}

type openAIContentResult struct {
	content   any
	reasoning string
	toolCalls []any
}

func canonicalPartsToOpenAIContent(role CanonicalMessageRole, parts []CanonicalContentPart) openAIContentResult {
	var contentBlocks []map[string]any
	var toolCalls []any
	var visibleText []string
	var reasoningText []string

	for _, part := range parts {
		switch part.Type {
		case PartText:
			if part.Thought {
				reasoningText = append(reasoningText, part.Text)
			} else {
				visibleText = append(visibleText, part.Text)
			}
		case PartImage:
			url := asTrimmedString(part.URL)
			if url == "" {
				url = asTrimmedString(part.DataURL)
			}
			if url != "" {
				contentBlocks = append(contentBlocks, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": url},
				})
			}
		case PartFile:
			fileBlock := toOpenAIChatFileBlock(map[string]any{
				"fileData": part.FileData,
				"fileUrl":  part.FileURL,
				"filename": part.Filename,
				"mimeType": part.MimeType,
			}, part.FileID)
			if fileBlock != nil {
				contentBlocks = append(contentBlocks, fileBlock)
			}
		case PartToolCall:
			toolCalls = append(toolCalls, map[string]any{
				"id":   part.ID,
				"type": "function",
				"function": map[string]any{
					"name":      part.Name,
					"arguments": part.ArgumentsJSON,
				},
			})
		case PartToolResult:
			if role != RoleTool {
				text := part.ResultText
				if text == "" {
					if s, ok := part.ResultContent.(string); ok {
						text = s
					} else {
						text = safeJSONString(coalesce(part.ResultJSON, part.ResultContent))
					}
				}
				if text != "" {
					visibleText = append(visibleText, text)
				}
			}
		}
	}

	result := openAIContentResult{
		reasoning: strings.Join(reasoningText, ""),
		toolCalls: toolCalls,
	}

	if len(contentBlocks) == 0 {
		result.content = strings.Join(visibleText, "")
		return result
	}

	if len(visibleText) > 0 {
		contentBlocks = append([]map[string]any{{
			"type": "text",
			"text": strings.Join(visibleText, ""),
		}}, contentBlocks...)
	}

	result.content = contentBlocks
	return result
}

func toOpenAIChatFileBlock(input map[string]any, fileID string) map[string]any {
	fileData := asTrimmedString(input["fileData"])
	fileURL := asTrimmedString(input["fileUrl"])
	filename := asTrimmedString(input["filename"])
	mimeType := ""
	if mt, ok := input["mimeType"].(*string); ok && mt != nil {
		mimeType = *mt
	}
	if mimeType == "" {
		mimeType = asTrimmedString(input["mimeType"])
	}

	if fileData == "" && fileURL == "" && fileID == "" {
		return nil
	}

	file := map[string]any{}
	if fileData != "" {
		file["file_data"] = fileData
	}
	if fileURL != "" && fileData == "" {
		file["file_url"] = fileURL
	}
	if fileID != "" {
		file["file_id"] = fileID
	}
	if filename != "" {
		file["filename"] = filename
	}
	if mimeType != "" {
		file["mime_type"] = mimeType
	}
	return map[string]any{
		"type": "file",
		"file": file,
	}
}

func canonicalToolChoiceToOpenAI(tc *CanonicalToolChoice) any {
	if tc == nil {
		return nil
	}
	switch tc.Type {
	case "auto", "none":
		return tc.Type
	case "required":
		return "required"
	case "raw":
		return cloneJSONValue(tc.Value)
	case "tool":
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": tc.Name,
			},
		}
	}
	return nil
}

// ApplyOpenAICompatibleContinuation writes continuation fields into the body/metadata.
//
// previous_response_id is a Responses-API field. Chat Completions (and other
// non-Responses OpenAI-compatible surfaces built by CanonicalRequestToOpenAiChatBody)
// must not receive it — many upstreams return HTTP 400
// "Unsupported parameter: previous_response_id". Continuity is still preserved
// on the canonical envelope (ReadOpenAICompatibleContinuation /
// CanonicalContinuation.PreviousResponseID) and re-emitted only when building a
// Responses body via ApplyOpenAIResponsesContinuation.
func ApplyOpenAICompatibleContinuation(body map[string]any, cont *CanonicalContinuation, metadata map[string]any) {
	if cont == nil {
		return
	}
	if cont.PromptCacheKey != "" {
		body["prompt_cache_key"] = cont.PromptCacheKey
	}
	// Intentionally do NOT write previous_response_id onto chat-shaped bodies.
	if cont.TurnState != "" {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["metapi_turn_state"] = cont.TurnState
	}
}

// ApplyOpenAIResponsesContinuation writes Responses-protocol continuation
// fields (including previous_response_id) into a Responses request body.
func ApplyOpenAIResponsesContinuation(body map[string]any, cont *CanonicalContinuation, metadata map[string]any) {
	if cont == nil {
		return
	}
	ApplyOpenAICompatibleContinuation(body, cont, metadata)
	if cont.PreviousResponseID != "" {
		body["previous_response_id"] = cont.PreviousResponseID
	}
}


// coerceCanonicalToolArgumentsJSON normalizes OpenAI tool arguments to a JSON string.
// Skill tool calls may arrive with object arguments rather than a JSON string.
func coerceCanonicalToolArgumentsJSON(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		s := safeJSONString(v)
		if s == "" || s == "null" {
			return ""
		}
		return s
	}
}

func coalesce(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func itoa(n int) string {
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
