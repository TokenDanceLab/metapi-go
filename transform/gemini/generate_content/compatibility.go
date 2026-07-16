package generate_content

import (
	"strings"

	"github.com/tokendancelab/metapi-go/transform/shared"
)

// --- 4xx field filtering ---

// AllowedGeminiKeys are the 9 keys forwarded to the Gemini API.
var AllowedGeminiKeys = map[string]bool{
	"contents": true, "systemInstruction": true, "cachedContent": true,
	"safetySettings": true, "generationConfig": true, "tools": true,
	"toolConfig": true, "labels": true, "model": true,
}

// DummyThoughtSignature is the base64 sentinel for "skip_thought_signature_validator".
// Gemini accepts any non-empty base64 string when a real thought signature is unavailable.
const DummyThoughtSignature = "c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I="

// NormalizeRequest filters and normalizes an incoming Gemini request body.
func NormalizeRequest(body map[string]any, modelName string) map[string]any {
	next := map[string]any{}
	if body == nil {
		return next
	}

	if body["contents"] != nil {
		next["contents"] = cloneGeminiContents(body["contents"])
	}
	if body["systemInstruction"] != nil {
		next["systemInstruction"] = cloneJSONValue(body["systemInstruction"])
	}
	if body["cachedContent"] != nil {
		next["cachedContent"] = cloneJSONValue(body["cachedContent"])
	}
	if body["safetySettings"] != nil {
		next["safetySettings"] = cloneJSONValue(body["safetySettings"])
	}
	if body["generationConfig"] != nil {
		next["generationConfig"] = cloneGeminiGenerationConfig(body["generationConfig"])
	}
	if body["tools"] != nil {
		next["tools"] = cloneGeminiTools(body["tools"])
	}
	if body["toolConfig"] != nil {
		next["toolConfig"] = cloneJSONValue(body["toolConfig"])
	}

	// Derive thinking config from reasoning params
	derivedTC := ResolveGeminiThinkingConfigFromRequest(modelName, body)
	if derivedTC != nil {
		gc, _ := next["generationConfig"].(map[string]any)
		if gc == nil {
			gc = map[string]any{}
		}
		tc := mergeThinkingConfig(gc["thinkingConfig"], derivedTC)
		if tc != nil {
			gc["thinkingConfig"] = tc
		}
		next["generationConfig"] = gc
	}

	// Passthrough allowed keys
	for k, v := range body {
		if !AllowedGeminiKeys[k] {
			continue
		}
		if next[k] != nil {
			continue
		}
		next[k] = cloneJSONValue(v)
	}

	// Official Gemini multi-turn tool history requires thoughtSignature on
	// functionCall parts for Gemini 3.x (and thinking-enabled models).
	injectThoughtSignaturesIntoContents(next, modelName)

	return next
}

// --- Gemini -> OpenAI body conversion ---

// BuildOpenAiBodyFromGeminiRequest converts a Gemini request to an OpenAI chat body.
func BuildOpenAiBodyFromGeminiRequest(body map[string]any, modelName string) map[string]any {
	if modelName == "" {
		modelName = shared.AsTrimmedString(body["model"])
	}

	var messages []map[string]any

	// System instruction
	if si, ok := body["systemInstruction"].(map[string]any); ok {
		if parts, ok := si["parts"].([]any); ok {
			content := toOpenAIContent(filterParts(parts))
			if hasContent(content) {
				messages = append(messages, map[string]any{"role": "system", "content": content})
			}
		}
	}

	// Contents
	contents, _ := body["contents"].([]any)
	for _, item := range contents {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := "user"
		if shared.AsTrimmedString(m["role"]) == "model" {
			role = "assistant"
		}
		parts, _ := m["parts"].([]any)
		filteredParts := filterParts(parts)

		// Extract tool calls
		var toolCalls []map[string]any
		var functionResponses []map[string]any
		var otherParts []map[string]any

		for i, part := range filteredParts {
			if fc, ok := part["functionCall"].(map[string]any); ok {
				name := shared.AsTrimmedString(fc["name"])
				if name == "" {
					continue
				}
				id := shared.AsTrimmedString(fc["id"])
				if id == "" {
					id = "call_" + itoa(int64(i))
				}
				args := shared.SafeJSONString(fc["args"])
				if args == "" {
					args = "{}"
				}
				toolCall := map[string]any{
					"id": id, "type": "function",
					"function": map[string]any{"name": name, "arguments": args},
				}
				// Preserve thought_signature for OpenAI↔Gemini tool-history round-trips.
				if sig := extractThoughtSignature(part); sig != "" {
					toolCall["provider_specific_fields"] = map[string]any{
						"thought_signature": sig,
					}
				}
				toolCalls = append(toolCalls, toolCall)
			} else if fr, ok := part["functionResponse"].(map[string]any); ok {
				name := shared.AsTrimmedString(fr["name"])
				id := shared.AsTrimmedString(fr["id"])
				response := fr["response"]
				functionResponses = append(functionResponses, map[string]any{
					"role": "tool", "tool_call_id": id,
					"name": name, "content": shared.SafeJSONString(response),
				})
			} else {
				otherParts = append(otherParts, part)
			}
		}

		if role == "assistant" && len(toolCalls) > 0 {
			content := toOpenAIContent(otherParts)
			msg := map[string]any{"role": "assistant", "tool_calls": toolCalls}
			msg["content"] = content
			if s, ok := content.(string); ok && s == "" {
				msg["content"] = ""
			}
			messages = append(messages, msg)
		} else if role == "user" && len(functionResponses) > 0 {
			messages = append(messages, functionResponses...)
		} else {
			content := toOpenAIContent(filteredParts)
			if hasContent(content) {
				messages = append(messages, map[string]any{"role": role, "content": content})
			}
		}
	}

	body2 := map[string]any{
		"model":    modelName,
		"messages": messages,
		"stream":   body["stream"] == true,
	}

	// Generation config
	if gc, ok := body["generationConfig"].(map[string]any); ok {
		if t := gc["temperature"]; t != nil {
			body2["temperature"] = t
		}
		if tp := gc["topP"]; tp != nil {
			body2["top_p"] = tp
		}
		if mt := gc["maxOutputTokens"]; mt != nil {
			body2["max_tokens"] = mt
		}
		if ss, ok := gc["stopSequences"].([]any); ok {
			body2["stop"] = ss
		}
	}

	// Tools
	if tools := body["tools"]; tools != nil {
		if oaiTools := extractToolDeclarations(tools); oaiTools != nil {
			body2["tools"] = oaiTools
		}
	}

	// Tool choice
	if tc := body["toolConfig"]; tc != nil {
		if choice := extractToolChoice(tc); choice != "" {
			body2["tool_choice"] = choice
		}
	}

	return body2
}

// --- OpenAI -> Gemini body conversion ---

// BuildGeminiGenerateContentRequestFromOpenAi converts an OpenAI body to Gemini format.
func BuildGeminiGenerateContentRequestFromOpenAi(openaiBody map[string]any, modelName string) map[string]any {
	if modelName == "" {
		modelName = shared.AsTrimmedString(openaiBody["model"])
	}

	var contents []map[string]any
	var systemInstruction map[string]any

	msgs, _ := openaiBody["messages"].([]any)

	// Map tool_call_id -> function name for functionResponse.name when tool messages omit name.
	toolNameByID := map[string]string{}
	// Map tool_call_id -> thought_signature from provider_specific_fields (or top-level aliases).
	thoughtSignatureByID := map[string]string{}
	for _, item := range msgs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(shared.AsTrimmedString(m["role"])) != "assistant" {
			continue
		}
		tcs, ok := m["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, tc := range tcs {
			tcm, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			id := shared.AsTrimmedString(tcm["id"])
			fn, _ := tcm["function"].(map[string]any)
			if fn == nil {
				fn = map[string]any{}
			}
			name := shared.AsTrimmedString(fn["name"])
			if id != "" && name != "" {
				toolNameByID[id] = name
			}
			if id == "" {
				continue
			}
			if sig := extractThoughtSignatureFromToolCall(tcm); sig != "" {
				thoughtSignatureByID[id] = sig
			}
		}
	}

	hasThinkingEnabled := requestHasThinkingEnabled(modelName, openaiBody)
	allowsDummy := isDummyThoughtSafeModel(modelName)
	requiresSig := requiresFunctionCallThoughtSignature(modelName)
	shouldDisableThinkingConfig := false

	for _, item := range msgs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := shared.AsTrimmedString(m["role"])

		if role == "system" || role == "developer" {
			text := extractOpenAIText(m["content"])
			if text != "" {
				systemInstruction = map[string]any{
					"parts": []map[string]any{{"text": text}},
				}
			}
			continue
		}

		geminiRole := "user"
		if role == "assistant" || role == "model" {
			geminiRole = "model"
		}

		var textParts []map[string]any
		var fcParts []map[string]any

		// Reasoning content
		if rc := shared.AsTrimmedString(m["reasoning_content"]); rc != "" {
			textParts = append(textParts, map[string]any{"text": rc, "thought": true})
		}

		// Content blocks
		content := m["content"]
		if s, ok := content.(string); ok {
			if s != "" {
				textParts = append(textParts, map[string]any{"text": s})
			}
		} else if arr, ok := content.([]any); ok {
			for _, block := range arr {
				bm, ok := block.(map[string]any)
				if !ok {
					continue
				}
				bt := shared.AsTrimmedString(bm["type"])
				if bt == "text" {
					if t := shared.AsTrimmedString(bm["text"]); t != "" {
						textParts = append(textParts, map[string]any{"text": t})
					}
				} else if bt == "image_url" {
					if iu, ok := bm["image_url"].(map[string]any); ok {
						url := shared.AsTrimmedString(iu["url"])
						if inline := parseDataURLToGeminiInline(url); inline != nil {
							textParts = append(textParts, inline)
						} else if url != "" {
							textParts = append(textParts, map[string]any{
								"fileData": map[string]any{"fileUri": url, "mimeType": inferMimeFromURL(url)},
							})
						}
					}
				}
			}
		}

		// Tool calls -> functionCall parts, with thoughtSignature when required/available.
		if tcs, ok := m["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				tcm, ok := tc.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := tcm["function"].(map[string]any)
				if fn == nil {
					fn = map[string]any{}
				}
				name := shared.AsTrimmedString(fn["name"])
				if name == "" {
					continue
				}
				args := shared.ParseJSONLike(shared.AsTrimmedString(fn["arguments"]))
				fcPart := map[string]any{
					"functionCall": map[string]any{
						"name": name,
						"args": args,
					},
				}
				id := shared.AsTrimmedString(tcm["id"])
				if id != "" {
					if fc, ok := fcPart["functionCall"].(map[string]any); ok {
						fc["id"] = id
					}
				}
				sig := thoughtSignatureByID[id]
				if sig == "" {
					sig = extractThoughtSignatureFromToolCall(tcm)
				}
				if sig != "" {
					fcPart["thoughtSignature"] = sig
				} else if (hasThinkingEnabled || requiresSig) && allowsDummy {
					fcPart["thoughtSignature"] = DummyThoughtSignature
				} else if hasThinkingEnabled {
					// Non-Gemini thinking targets cannot safely receive dummy signatures.
					shouldDisableThinkingConfig = true
				}
				fcParts = append(fcParts, fcPart)
			}
		}

		// Tool results
		if role == "tool" {
			toolCallID := shared.AsTrimmedString(m["tool_call_id"])
			name := shared.AsTrimmedString(m["name"])
			if name == "" {
				name = toolNameByID[toolCallID]
			}
			if name == "" {
				name = "unknown"
			}
			response := m["content"]
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"name":     name,
						"id":       toolCallID,
						"response": response,
					},
				}},
			})
			continue
		}

		// Official Gemini expects signed functionCall parts separated from sibling text parts.
		hasSigned := false
		for _, p := range fcParts {
			if shared.AsTrimmedString(p["thoughtSignature"]) != "" {
				hasSigned = true
				break
			}
		}
		if hasSigned && len(textParts) > 0 && len(fcParts) > 0 {
			contents = append(contents,
				map[string]any{"role": geminiRole, "parts": textParts},
				map[string]any{"role": geminiRole, "parts": fcParts},
			)
			continue
		}

		allParts := append(append([]map[string]any{}, textParts...), fcParts...)
		if len(allParts) > 0 {
			contents = append(contents, map[string]any{
				"role":  geminiRole,
				"parts": allParts,
			})
		}
	}

	body := map[string]any{
		"model":    modelName,
		"contents": contents,
	}

	if systemInstruction != nil {
		body["systemInstruction"] = systemInstruction
	}

	// Generation config
	gc := map[string]any{}
	if t, ok := openaiBody["temperature"].(float64); ok {
		gc["temperature"] = t
	}
	if tp, ok := openaiBody["top_p"].(float64); ok {
		gc["topP"] = tp
	}
	if mt, ok := openaiBody["max_tokens"].(float64); ok {
		gc["maxOutputTokens"] = int(mt)
	}
	thinkingConfig := ResolveGeminiThinkingConfigFromRequest(modelName, openaiBody)
	if thinkingConfig != nil && !shouldDisableThinkingConfig {
		gc["thinkingConfig"] = thinkingConfig
	}
	if len(gc) > 0 {
		body["generationConfig"] = gc
	}

	// Tools
	if tools := openaiBody["tools"]; tools != nil {
		body["tools"] = convertOpenAiToolsToGemini(tools)
	}

	// Tool choice
	if tc := openaiBody["tool_choice"]; tc != nil {
		body["toolConfig"] = convertOpenAiToolChoiceToGemini(tc)
	}

	return body
}

// --- Thought signature helpers ---

// isDummyThoughtSafeModel reports whether dummy thought signatures are safe for this model.
// Only official Gemini model IDs accept the sentinel; third-party "Gemini-compatible" models may not.
func isDummyThoughtSafeModel(modelName string) bool {
	normalized := strings.ToLower(shared.AsTrimmedString(modelName))
	return strings.HasPrefix(normalized, "gemini-") || strings.HasPrefix(normalized, "models/gemini-")
}

// requiresFunctionCallThoughtSignature reports models that reject tool history without thoughtSignature
// even when the client did not enable thinking/reasoning explicitly (Gemini 3.x).
func requiresFunctionCallThoughtSignature(modelName string) bool {
	normalized := strings.ToLower(shared.AsTrimmedString(modelName))
	normalized = strings.TrimPrefix(normalized, "models/")
	// gemini-3, gemini-3.5-flash, gemini-3-pro-preview, etc.
	return strings.HasPrefix(normalized, "gemini-3")
}

// requestHasThinkingEnabled detects thinking either via derived reasoning params or explicit thinkingConfig.
func requestHasThinkingEnabled(modelName string, body map[string]any) bool {
	if ResolveGeminiThinkingConfigFromRequest(modelName, body) != nil {
		return true
	}
	if body == nil {
		return false
	}
	if gc, ok := body["generationConfig"].(map[string]any); ok && gc != nil {
		if tc := gc["thinkingConfig"]; tc != nil {
			return true
		}
	}
	return false
}

func extractThoughtSignature(part map[string]any) string {
	if part == nil {
		return ""
	}
	if sig := shared.AsTrimmedString(part["thoughtSignature"]); sig != "" {
		return sig
	}
	if sig := shared.AsTrimmedString(part["thought_signature"]); sig != "" {
		return sig
	}
	return ""
}

func extractThoughtSignatureFromToolCall(toolCall map[string]any) string {
	if toolCall == nil {
		return ""
	}
	if sig := shared.AsTrimmedString(toolCall["thoughtSignature"]); sig != "" {
		return sig
	}
	if sig := shared.AsTrimmedString(toolCall["thought_signature"]); sig != "" {
		return sig
	}
	if psf, ok := toolCall["provider_specific_fields"].(map[string]any); ok && psf != nil {
		if sig := shared.AsTrimmedString(psf["thought_signature"]); sig != "" {
			return sig
		}
		if sig := shared.AsTrimmedString(psf["thoughtSignature"]); sig != "" {
			return sig
		}
	}
	return ""
}

// injectThoughtSignaturesIntoContents ensures native Gemini contents with functionCall parts
// carry thoughtSignature when official Gemini would reject unsigned tool history.
// Existing real signatures are preserved; dummy is only injected when missing.
func injectThoughtSignaturesIntoContents(body map[string]any, modelName string) {
	if body == nil {
		return
	}
	allowsDummy := isDummyThoughtSafeModel(modelName)
	requiresSig := requiresFunctionCallThoughtSignature(modelName)
	hasThinking := requestHasThinkingEnabled(modelName, body)
	if !allowsDummy || (!requiresSig && !hasThinking) {
		return
	}

	// contents may be []any (from clone) or []map[string]any in tests.
	switch contents := body["contents"].(type) {
	case []any:
		for _, item := range contents {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			injectThoughtSignaturesIntoParts(m)
		}
	case []map[string]any:
		for _, m := range contents {
			injectThoughtSignaturesIntoParts(m)
		}
	}
}

func injectThoughtSignaturesIntoParts(content map[string]any) {
	if content == nil {
		return
	}
	switch parts := content["parts"].(type) {
	case []any:
		for _, p := range parts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if _, hasFC := pm["functionCall"]; !hasFC {
				continue
			}
			if extractThoughtSignature(pm) != "" {
				continue
			}
			pm["thoughtSignature"] = DummyThoughtSignature
		}
	case []map[string]any:
		for _, pm := range parts {
			if pm == nil {
				continue
			}
			if _, hasFC := pm["functionCall"]; !hasFC {
				continue
			}
			if extractThoughtSignature(pm) != "" {
				continue
			}
			pm["thoughtSignature"] = DummyThoughtSignature
		}
	}
}

// ApplyThoughtSignaturesToFunctionCallParts injects/preserves thought signatures on functionCall parts.
// Prefer real signatures from priorAggregate or existing part fields; fall back to dummy when required.
func ApplyThoughtSignaturesToFunctionCallParts(parts []map[string]any, modelName string, priorAggregate *GeminiAggregateState) []map[string]any {
	if len(parts) == 0 {
		return parts
	}
	allowsDummy := isDummyThoughtSafeModel(modelName)
	requiresSig := requiresFunctionCallThoughtSignature(modelName)
	// Without aggregate thinking flag, still inject for Gemini 3.x tool history.
	shouldInject := allowsDummy && requiresSig
	if !shouldInject && allowsDummy {
		// Also inject when aggregate already collected signatures (multi-turn follow-up).
		if priorAggregate != nil && len(priorAggregate.ThoughtSignatures) > 0 {
			shouldInject = true
		}
	}
	if !shouldInject && !allowsDummy {
		return parts
	}

	aggIdx := 0
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		next := cloneMapSimple(part)
		if _, hasFC := next["functionCall"]; hasFC {
			if extractThoughtSignature(next) == "" {
				var sig string
				if priorAggregate != nil && aggIdx < len(priorAggregate.ThoughtSignatures) {
					sig = priorAggregate.ThoughtSignatures[aggIdx]
				}
				if sig == "" && shouldInject {
					sig = DummyThoughtSignature
				}
				if sig != "" {
					next["thoughtSignature"] = sig
				}
			}
			aggIdx++
		}
		out = append(out, next)
	}
	return out
}

// CollectThoughtSignaturesFromParts records unique thoughtSignature values from parts into state.
func CollectThoughtSignaturesFromParts(state *GeminiAggregateState, parts []map[string]any) {
	if state == nil {
		return
	}
	for _, part := range parts {
		sig := extractThoughtSignature(part)
		if sig == "" {
			continue
		}
		found := false
		for _, existing := range state.ThoughtSignatures {
			if existing == sig {
				found = true
				break
			}
		}
		if !found {
			state.ThoughtSignatures = append(state.ThoughtSignatures, sig)
		}
	}
}

// BuildSignedModelContentForToolHistory builds a model content entry for replaying tool calls
// into the next Gemini request, attaching thought signatures from aggregate state when present.
func BuildSignedModelContentForToolHistory(parts []map[string]any, modelName string, priorAggregate *GeminiAggregateState) map[string]any {
	signed := ApplyThoughtSignaturesToFunctionCallParts(parts, modelName, priorAggregate)
	return map[string]any{
		"role":  "model",
		"parts": signed,
	}
}

func filterParts(parts []any) []map[string]any {
	var out []map[string]any
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

func toOpenAIContent(parts []map[string]any) any {
	var blocks []map[string]any
	var textContent string

	for _, part := range parts {
		if t, ok := part["text"].(string); ok && t != "" {
			if part["thought"] == true {
				continue
			}
			textContent += t
			continue
		}
		if block := toOpenAIBlockFromGeminiPart(part); block != nil {
			blocks = append(blocks, block)
		}
	}

	if len(blocks) == 0 {
		return textContent
	}
	if textContent != "" {
		blocks = append([]map[string]any{{"type": "text", "text": textContent}}, blocks...)
	}
	return blocks
}

func toOpenAIBlockFromGeminiPart(part map[string]any) map[string]any {
	if inline := buildGeminiInlineData(part); inline != nil {
		mimeType := strings.ToLower(inline["mimeType"].(string))
		if strings.HasPrefix(mimeType, "image/") {
			return map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": buildDataURL(inline)},
			}
		}
		return map[string]any{
			"type": "file",
			"file": map[string]any{
				"file_data": inline["data"],
				"mime_type": inline["mimeType"],
			},
		}
	}

	if fd := buildGeminiFileData(part); fd != nil {
		if strings.HasPrefix(strings.ToLower(fd["mimeType"].(string)), "image/") {
			return map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": fd["fileUri"]},
			}
		}
		return map[string]any{
			"type": "file",
			"file": map[string]any{
				"file_url":  fd["fileUri"],
				"mime_type": fd["mimeType"],
			},
		}
	}

	return nil
}

func buildGeminiInlineData(part map[string]any) map[string]any {
	id, ok := part["inlineData"].(map[string]any)
	if !ok {
		return nil
	}
	mimeType := shared.AsTrimmedString(id["mime_type"])
	if mimeType == "" {
		mimeType = shared.AsTrimmedString(id["mimeType"])
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	data := shared.AsTrimmedString(id["data"])
	if data == "" {
		return nil
	}
	return map[string]any{"mimeType": mimeType, "data": data}
}

func buildDataURL(inline map[string]any) string {
	return "data:" + inline["mimeType"].(string) + ";base64," + inline["data"].(string)
}

func buildGeminiFileData(part map[string]any) map[string]any {
	fd, ok := part["fileData"].(map[string]any)
	if !ok {
		return nil
	}
	uri := shared.AsTrimmedString(fd["fileUri"])
	if uri == "" {
		uri = shared.AsTrimmedString(fd["file_uri"])
	}
	if uri == "" {
		return nil
	}
	mt := shared.AsTrimmedString(fd["mimeType"])
	if mt == "" {
		mt = shared.AsTrimmedString(fd["mime_type"])
	}
	return map[string]any{"fileUri": uri, "mimeType": mt}
}

func hasContent(content any) bool {
	if s, ok := content.(string); ok {
		return s != ""
	}
	if arr, ok := content.([]any); ok {
		return len(arr) > 0
	}
	return false
}

func extractOpenAIText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]any); ok {
		var texts []string
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

func extractToolDeclarations(tools any) []any {
	arr, ok := tools.([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, tool := range arr {
		m, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		if fds, ok := m["functionDeclarations"].([]any); ok {
			for _, decl := range fds {
				dm, ok := decl.(map[string]any)
				if !ok {
					continue
				}
				name := shared.AsTrimmedString(dm["name"])
				if name == "" {
					continue
				}
				params := dm["parametersJsonSchema"]
				if params == nil {
					params = dm["parameters"]
				}
				if params == nil {
					params = map[string]any{"type": "object", "properties": map[string]any{}}
				}
				fn := map[string]any{"name": name, "parameters": params}
				if d := shared.AsTrimmedString(dm["description"]); d != "" {
					fn["description"] = d
				}
				out = append(out, map[string]any{"type": "function", "function": fn})
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractToolChoice(toolConfig any) string {
	m, ok := toolConfig.(map[string]any)
	if !ok {
		return ""
	}
	fcc, ok := m["functionCallingConfig"].(map[string]any)
	if !ok {
		return ""
	}
	mode := strings.ToUpper(shared.AsTrimmedString(fcc["mode"]))
	switch mode {
	case "NONE":
		return "none"
	case "ANY", "VALIDATED":
		return "required"
	case "AUTO":
		return "auto"
	}
	return ""
}

func convertOpenAiToolsToGemini(tools any) any {
	arr, ok := tools.([]any)
	if !ok {
		return tools
	}
	var fds []map[string]any
	var otherTools []map[string]any
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t := strings.ToLower(shared.AsTrimmedString(m["type"]))
		if t == "function" {
			if fn, ok := m["function"].(map[string]any); ok {
				name := shared.AsTrimmedString(fn["name"])
				if name == "" {
					continue
				}
				fd := map[string]any{"name": name}
				if d := shared.AsTrimmedString(fn["description"]); d != "" {
					fd["description"] = d
				}
				if params, ok := fn["parameters"].(map[string]any); ok {
					fd["parametersJsonSchema"] = params
				}
				fds = append(fds, fd)
			}
		} else if t == "web_search" || t == "google_search" {
			otherTools = append(otherTools, map[string]any{"googleSearch": map[string]any{}})
		} else if t == "code_interpreter" {
			otherTools = append(otherTools, map[string]any{"codeExecution": map[string]any{}})
		}
	}
	var result []map[string]any
	if len(fds) > 0 {
		result = append(result, map[string]any{"functionDeclarations": fds})
	}
	result = append(result, otherTools...)
	if len(result) == 0 {
		return tools
	}
	return result
}

func convertOpenAiToolChoiceToGemini(tc any) any {
	var mode string
	if s, ok := tc.(string); ok {
		n := strings.ToUpper(strings.TrimSpace(s))
		switch n {
		case "NONE":
			mode = "NONE"
		case "REQUIRED", "ANY":
			mode = "ANY"
		case "AUTO":
			mode = "AUTO"
		default:
			mode = "AUTO"
		}
	} else if m, ok := tc.(map[string]any); ok {
		t := strings.ToLower(shared.AsTrimmedString(m["type"]))
		switch t {
		case "none":
			mode = "NONE"
		case "required", "any":
			mode = "ANY"
		case "function", "tool":
			mode = "ANY"
		default:
			mode = "AUTO"
		}
	} else {
		mode = "AUTO"
	}
	return map[string]any{
		"functionCallingConfig": map[string]any{"mode": mode},
	}
}

func parseDataURLToGeminiInline(url string) map[string]any {
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
	if strings.HasSuffix(mimeType, ";base64") {
		mimeType = mimeType[:len(mimeType)-7]
	}
	data := rest[comma+1:]
	return map[string]any{
		"inlineData": map[string]any{
			"mimeType": mimeType,
			"data":     data,
		},
	}
}

func inferMimeFromURL(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, ".png") {
		return "image/png"
	}
	if strings.Contains(lower, ".jpg") || strings.Contains(lower, ".jpeg") {
		return "image/jpeg"
	}
	if strings.Contains(lower, ".gif") {
		return "image/gif"
	}
	if strings.Contains(lower, ".webp") {
		return "image/webp"
	}
	return "application/octet-stream"
}

// --- Thinking config conversion ---

// ReasoningToThinkingConfig converts OpenAI reasoning settings to Gemini thinkingConfig.
func ReasoningToThinkingConfig(effort string, budgetTokens int) map[string]any {
	if effort != "" {
		switch strings.ToLower(effort) {
		case "none":
			return nil
		case "low":
			return map[string]any{"thinkingBudget": 0}
		case "medium":
			return map[string]any{"thinkingBudget": 8192}
		case "high":
			return map[string]any{"thinkingBudget": 32768}
		case "max":
			return map[string]any{"thinkingBudget": 65536}
		}
	}
	if budgetTokens > 0 {
		return map[string]any{"thinkingBudget": budgetTokens}
	}
	return nil
}

// ResolveGeminiThinkingConfigFromRequest extracts thinking config from request params.
func ResolveGeminiThinkingConfigFromRequest(modelName string, body map[string]any) map[string]any {
	// Check if model supports thinking levels
	gc, _ := body["generationConfig"].(map[string]any)
	if gc != nil {
		if tc := gc["thinkingConfig"]; tc != nil {
			return nil // User already provided explicit config
		}
	}

	// Check for reasoning params
	effort := shared.AsTrimmedString(body["reasoning_effort"])
	budget := 0
	if n, ok := body["reasoning_budget"].(float64); ok {
		budget = int(n)
	}

	return ReasoningToThinkingConfig(effort, budget)
}

func mergeThinkingConfig(current any, derived map[string]any) map[string]any {
	cm, _ := current.(map[string]any)
	if cm == nil {
		return cloneMapSimple(derived)
	}
	// Current takes precedence for thinking fields
	merged := cloneMapSimple(cm)
	for k, v := range derived {
		if merged[k] == nil {
			merged[k] = v
		}
	}
	return merged
}

// --- Stream bridge ---

// StreamBridge provides Gemini GenerateContent SSE stream processing.
type StreamBridge struct {
	Ctx   *shared.StreamTransformContext
	State *GeminiAggregateState
}

// GeminiAggregateState tracks coalesced state across Gemini stream chunks.
type GeminiAggregateState struct {
	ResponseID        string
	ModelVersion      string
	FinishReason      string
	Parts             []map[string]any
	Citations         []map[string]any
	GroundingMetadata []map[string]any
	ThoughtSignatures []string
	Usage             GeminiUsageSummary
	Candidates        []GeminiCandidateAggregate
}

// GeminiUsageSummary holds token counts.
type GeminiUsageSummary struct {
	PromptTokenCount       int
	CandidatesTokenCount   int
	TotalTokenCount        int
	CachedContentTokenCount int
	ThoughtsTokenCount     int
}

// GeminiCandidateAggregate holds per-candidate state.
type GeminiCandidateAggregate struct {
	Index        int
	FinishReason string
	Content      string
	Reasoning    string
}

// NewStreamBridge creates a new Gemini stream bridge.
func NewStreamBridge(modelName string) *StreamBridge {
	return &StreamBridge{
		Ctx:   shared.CreateStreamTransformContext(modelName),
		State: &GeminiAggregateState{},
	}
}

// NormalizeEvent normalizes an upstream Gemini event.
func (sb *StreamBridge) NormalizeEvent(payload any) shared.NormalizedStreamEvent {
	event := shared.NormalizeUpstreamStreamEvent(payload, sb.Ctx, sb.Ctx.Model)

	// Accumulate state
	if m, ok := payload.(map[string]any); ok {
		if mv := shared.AsTrimmedString(m["modelVersion"]); mv != "" {
			sb.State.ModelVersion = mv
		}
		if rid := shared.AsTrimmedString(m["responseId"]); rid != "" {
			sb.State.ResponseID = rid
		}

		if candidates, ok := m["candidates"].([]any); ok && len(candidates) > 0 {
			if c, ok := candidates[0].(map[string]any); ok {
				if fr := shared.AsTrimmedString(c["finishReason"]); fr != "" {
					sb.State.FinishReason = fr
				}
				if cp, ok := c["content"].(map[string]any); ok {
					if parts, ok := cp["parts"].([]any); ok {
						var collected []map[string]any
						for _, p := range parts {
							if pm, ok := p.(map[string]any); ok {
								sb.State.Parts = append(sb.State.Parts, pm)
								collected = append(collected, pm)
							}
						}
						// Stream collect thought signatures for next-turn tool-history injection.
						CollectThoughtSignaturesFromParts(sb.State, collected)
					}
				}
			}
		}

		// Coalesce adjacent text parts
		coalesceGeminiParts(sb.State)

		// Usage
		if um, ok := m["usageMetadata"].(map[string]any); ok {
			if n, ok := um["promptTokenCount"].(float64); ok {
				sb.State.Usage.PromptTokenCount = int(n)
			}
			if n, ok := um["candidatesTokenCount"].(float64); ok {
				sb.State.Usage.CandidatesTokenCount = int(n)
			}
			if n, ok := um["totalTokenCount"].(float64); ok {
				sb.State.Usage.TotalTokenCount = int(n)
			}
		}
	}

	return event
}

// SerializeEvent serializes to Gemini SSE format.
func (sb *StreamBridge) SerializeEvent(event shared.NormalizedStreamEvent) []string {
	// Gemini downstream uses direct event serialization
	var chunk map[string]any
	if event.ContentDelta != "" || event.ReasoningDelta != "" {
		chunk = map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{{
						"text": event.ContentDelta + event.ReasoningDelta,
					}},
				},
			}},
		}
	}
	if chunk == nil {
		return nil
	}
	return []string{shared.SerializeSSE("", chunk)}
}

// SerializeDone sends final events.
func (sb *StreamBridge) SerializeDone() []string {
	if sb.Ctx.DoneSent {
		return nil
	}
	sb.Ctx.DoneSent = true
	chunk := map[string]any{
		"candidates": []map[string]any{{
			"finishReason": sb.State.FinishReason,
			"content": map[string]any{
				"parts": sb.State.Parts,
			},
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     sb.State.Usage.PromptTokenCount,
			"candidatesTokenCount": sb.State.Usage.CandidatesTokenCount,
			"totalTokenCount":      sb.State.Usage.TotalTokenCount,
		},
	}
	return []string{shared.SerializeSSE("", chunk)}
}

// PullSseEvents extracts SSE events from a buffer.
func PullSseEvents(buffer string) ([]shared.ParsedSseEvent, string) {
	return shared.PullSseEventsWithDone(buffer)
}

func coalesceGeminiParts(state *GeminiAggregateState) {
	if len(state.Parts) < 2 {
		return
	}
	var coalesced []map[string]any
	for _, part := range state.Parts {
		if len(coalesced) > 0 {
			last := coalesced[len(coalesced)-1]
			// Coalesce adjacent text parts with same shape
			lastText, lastHasText := last["text"]
			curText, curHasText := part["text"]
			if lastHasText && curHasText {
				// Check all other fields match
				allMatch := true
				for k := range last {
					if k == "text" {
						continue
					}
					if last[k] != part[k] {
						allMatch = false
						break
					}
				}
				for k := range part {
					if k == "text" {
						continue
					}
					if last[k] != part[k] {
						allMatch = false
						break
					}
				}
				if allMatch {
					coalesced[len(coalesced)-1]["text"] = lastText.(string) + curText.(string)
					continue
				}
			}
		}
		coalesced = append(coalesced, part)
	}
	state.Parts = coalesced
}

// --- Cloning helpers ---

func cloneJSONValue(v any) any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return cloneMapSimple(m)
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, len(arr))
		for i, item := range arr {
			out[i] = cloneJSONValue(item)
		}
		return out
	}
	return v
}

func cloneGeminiContents(v any) any {
	arr, ok := v.([]any)
	if !ok {
		return v
	}
	var out []any
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		next := cloneMapSimple(m)
		if parts, ok := m["parts"].([]any); ok {
			var clonedParts []any
			for _, p := range parts {
				clonedParts = append(clonedParts, cloneJSONValue(p))
			}
			next["parts"] = clonedParts
		}
		out = append(out, next)
	}
	return out
}

func cloneGeminiGenerationConfig(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	allowedKeys := []string{
		"stopSequences", "responseModalities", "responseMimeType", "responseSchema",
		"candidateCount", "maxOutputTokens", "temperature", "topP", "topK",
		"presencePenalty", "frequencyPenalty", "seed", "responseLogprobs", "logprobs",
		"thinkingConfig", "imageConfig",
	}
	next := map[string]any{}
	for _, k := range allowedKeys {
		if v, ok := m[k]; ok {
			if k == "thinkingConfig" {
				next[k] = cloneThinkingConfig(v)
				if next[k] == nil {
					next[k] = cloneJSONValue(v)
				}
			} else {
				next[k] = cloneJSONValue(v)
			}
		}
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

func cloneGeminiTools(v any) any {
	arr, ok := v.([]any)
	if !ok {
		return v
	}
	var out []any
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		next := map[string]any{}
		if m["functionDeclarations"] != nil {
			next["functionDeclarations"] = cloneJSONValue(m["functionDeclarations"])
		}
		if m["googleSearch"] != nil {
			next["googleSearch"] = cloneJSONValue(m["googleSearch"])
		}
		if m["urlContext"] != nil {
			next["urlContext"] = cloneJSONValue(m["urlContext"])
		}
		if m["codeExecution"] != nil {
			next["codeExecution"] = cloneJSONValue(m["codeExecution"])
		}
		if len(next) == 0 {
			next = cloneMapSimple(m)
		}
		out = append(out, next)
	}
	return out
}

func cloneThinkingConfig(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	next := map[string]any{}
	for k, val := range m {
		if k == "thinkingLevel" {
			n := strings.ToLower(shared.AsTrimmedString(val))
			if n == "minimal" {
				next[k] = "low"
			} else if n != "" {
				next[k] = n
			}
		} else if k == "thinkingBudget" {
			if n, ok := val.(float64); ok {
				next[k] = max(0, int(n))
			}
		} else {
			next[k] = cloneJSONValue(val)
		}
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

func cloneMapSimple(src map[string]any) map[string]any {
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
