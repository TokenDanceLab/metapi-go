package shared

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

// --- SSE Parsing ---

func PullSseEventsWithDone(buffer string) (events []ParsedSseEvent, rest string) {
	normalized := strings.ReplaceAll(buffer, "\r\n", "\n")
	rest = normalized
	for {
		boundary := strings.Index(rest, "\n\n")
		if boundary < 0 {
			break
		}
		block := rest[:boundary]
		rest = rest[boundary+2:]
		if strings.TrimSpace(block) == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		var eventName string
		var dataLines []string
		for _, line := range lines {
			if strings.HasPrefix(line, "event:") {
				eventName = strings.TrimSpace(line[6:])
			} else if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimLeft(line[5:], " "))
			}
		}
		if len(dataLines) == 0 {
			continue
		}
		events = append(events, ParsedSseEvent{Event: eventName, Data: strings.TrimSpace(strings.Join(dataLines, "\n"))})
	}
	return
}

// --- Context ---

type ToolCallAccumulator struct {
	ID, Name, Arguments string
}

type StreamTransformContext struct {
	ID                                string
	Model                             string
	Created                           int64
	RoleSent                          bool
	DoneSent                          bool
	ToolCalls                         map[int]*ToolCallAccumulator
	ResponsesToolCallIndexByOutputIndex map[int]int
	ResponsesToolCallIndexByID         map[string]int
	NextResponsesToolCallIndex         int
	ResponsesTextByIndex               map[int]string
	ResponsesReasoningByIndex          map[int]string
	ThinkTagParser                     *ThinkTagParserState
}

func CreateStreamTransformContext(modelName string) *StreamTransformContext {
	now := time.Now()
	return &StreamTransformContext{
		ID:                                "chatcmpl-meta-" + itoa(now.UnixMilli()),
		Model:                             modelName,
		Created:                           now.Unix(),
		ToolCalls:                         make(map[int]*ToolCallAccumulator),
		ResponsesToolCallIndexByOutputIndex: make(map[int]int),
		ResponsesToolCallIndexByID:         make(map[string]int),
		ResponsesTextByIndex:               make(map[int]string),
		ResponsesReasoningByIndex:          make(map[int]string),
		ThinkTagParser:                     CreateThinkTagParserState(),
	}
}

// --- Stop Reason ---

func NormalizeStopReason(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return ""
	}
	switch {
	case v == "failed" || v == "error":
		return "error"
	case v == "end_turn" || v == "stop" || v == "end" || v == "eos" ||
		v == "finished" || v == "completed" || v == "stop_sequence":
		return "stop"
	case v == "incomplete" || v == "max_tokens" || v == "length" ||
		v == "max_output_tokens" || v == "max_tokens_exceeded" || strings.Contains(v, "max"):
		return "length"
	case v == "tool_use" || v == "tool_calls" || strings.Contains(v, "tool"):
		return "tool_calls"
	}
	return ""
}

func ToClaudeStopReason(finishReason string) string {
	switch NormalizeStopReason(finishReason) {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	}
	return "end_turn"
}

// --- Helpers ---

func IsRecord(v any) bool {
	m, ok := v.(map[string]any)
	return ok && m != nil
}

func AsRecord(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok && m != nil
}

func IsNonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

func AsTrimmedString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func PickFiniteNumber(v any) float64 {
	n, ok := toFloat(v)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	return n
}

func PickFiniteInt(v any) int  { return int(PickFiniteNumber(v)) }

func PickPositiveInt(v any) int {
	n := PickFiniteInt(v)
	if n <= 0 {
		return 0
	}
	return n
}

func EnsureIntTimestamp(v any, fallback int64) int64 {
	n, ok := toFloat(v)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return fallback
	}
	return int64(n)
}

func SafeJSONString(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func StringifyUnknownValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case float64:
		if x == float64(int64(x)) {
			return itoa(int64(x))
		}
		return strings.TrimRight(strings.TrimRight(jsonNum(x), "0"), ".")
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func JoinNonEmpty(parts []string) string {
	var r []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			r = append(r, t)
		}
	}
	return strings.Join(r, "\n\n")
}

func ComputeNovelResponsesDelta(existing, incoming string) string {
	if incoming == "" {
		return ""
	}
	if existing == "" {
		return incoming
	}
	if strings.HasPrefix(incoming, existing) {
		return incoming[len(existing):]
	}
	if strings.HasSuffix(existing, incoming) {
		return ""
	}
	ml := len(existing)
	if len(incoming) < ml {
		ml = len(incoming)
	}
	for o := ml; o > 0; o-- {
		if strings.HasSuffix(existing, incoming[:o]) {
			return incoming[o:]
		}
	}
	return incoming
}

func JoinIndexedResponsesText(m map[int]string) string {
	var keys []int
	for k := range m {
		keys = append(keys, k)
	}
	sortInts(keys)
	var parts []string
	for _, k := range keys {
		if t := strings.TrimSpace(m[k]); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	if s, ok := m[-1]; ok {
		return s
	}
	return ""
}

func SerializeSSE(event string, data any) string {
	var p string
	if s, ok := data.(string); ok {
		p = s
	} else {
		p = SafeJSONString(data)
	}
	if event != "" {
		return "event: " + event + "\ndata: " + p + "\n\n"
	}
	return "data: " + p + "\n\n"
}

// --- Think Tag Parser ---

// THINK_OPEN and THINK_CLOSE are the literal strings that must appear verbatim
// in the source but cannot be written in raw text because the model processes them.
// MiniMax (and several other thinking models) embed reasoning inside content as
// <think>...</think>. Some MiniMax responses omit the open tag and only emit the
// close tag; ExtractInlineThinkTags / ConsumeThinkTaggedText treat that orphan
// close form as "reasoning before close, content after".
var (
	thinkOpen  = string([]byte{0x3c, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x3e})        // <think>
	thinkClose = string([]byte{0x3c, 0x2f, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x3e}) // </think>
)

// partialTagSuffixLen returns the length of the longest suffix of text that is a
// proper prefix of tag. Used so stream parsers only buffer incomplete tags and
// release confirmed content/reasoning immediately.
func partialTagSuffixLen(text, tag string) int {
	max := len(tag) - 1
	if max > len(text) {
		max = len(text)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(tag, text[len(text)-n:]) {
			return n
		}
	}
	return 0
}

// releaseSafePrefix emits all of text except a possible partial tag suffix of
// either open or close tags (or only close tags when onlyClose is true).
func releaseSafePrefix(text string, onlyClose bool) (emitted, pending string) {
	n := partialTagSuffixLen(text, thinkClose)
	if !onlyClose {
		if o := partialTagSuffixLen(text, thinkOpen); o > n {
			n = o
		}
	}
	if n == 0 {
		return text, ""
	}
	return text[:len(text)-n], text[len(text)-n:]
}

func ConsumeThinkTaggedText(state *ThinkTagParserState, chunk string) (content, reasoning string) {
	text := state.Pending + chunk
	state.Pending = ""

	if state.Mode == "reasoning" {
		endIdx := strings.Index(text, thinkClose)
		if endIdx < 0 {
			// Still inside an open think block: emit safe reasoning, keep partial close.
			emitted, pending := releaseSafePrefix(text, true)
			state.Mode = "reasoning"
			state.Pending = pending
			return "", emitted
		}
		reasoning = text[:endIdx]
		state.Mode = "content"
		text = text[endIdx+len(thinkClose):]
	}

	for {
		startIdx := strings.Index(text, thinkOpen)
		closeIdx := strings.Index(text, thinkClose)

		// MiniMax orphan close: reasoning streamed without a visible open tag.
		if startIdx < 0 && closeIdx >= 0 {
			reasoning += text[:closeIdx]
			text = text[closeIdx+len(thinkClose):]
			continue
		}
		if startIdx < 0 {
			// No complete open/close tags: emit confirmed content, buffer partial tag.
			emitted, pending := releaseSafePrefix(text, false)
			content += emitted
			state.Pending = pending
			return
		}

		// Open tag wins over a later close; content before open is visible text.
		content += text[:startIdx]
		text = text[startIdx+len(thinkOpen):]

		endIdx := strings.Index(text, thinkClose)
		if endIdx < 0 {
			// Opened think without close yet — buffer remaining as reasoning mode.
			emitted, pending := releaseSafePrefix(text, true)
			reasoning += emitted
			state.Mode = "reasoning"
			state.Pending = pending
			return
		}
		reasoning += text[:endIdx]
		text = text[endIdx+len(thinkClose):]
	}
}

func FlushThinkTaggedText(state *ThinkTagParserState) (content, reasoning string) {
	if state == nil {
		return "", ""
	}
	pending := state.Pending
	state.Pending = ""
	if state.Mode == "reasoning" {
		state.Mode = "content"
		// Unclosed think block: remaining buffer is reasoning, not client content.
		return "", pending
	}
	// Content mode: release any buffered content so stream finish does not drop it.
	return pending, ""
}

func ExtractInlineThinkTags(text string) TextReasoning {
	var content, reasoning string
	for {
		start := strings.Index(text, thinkOpen)
		closeIdx := strings.Index(text, thinkClose)

		// Orphan </think> (MiniMax often omits the open tag in final content).
		if start < 0 && closeIdx >= 0 {
			reasoning += text[:closeIdx]
			text = text[closeIdx+len(thinkClose):]
			continue
		}
		if start < 0 {
			content += text
			return TextReasoning{Content: content, Reasoning: reasoning}
		}

		content += text[:start]
		text = text[start+len(thinkOpen):]

		end := strings.Index(text, thinkClose)
		if end < 0 {
			reasoning += text
			return TextReasoning{Content: content, Reasoning: reasoning}
		}
		reasoning += text[:end]
		text = text[end+len(thinkClose):]
	}
}

// ExtractReasoningDetailsText pulls MiniMax-style reasoning_details[].text
// (and similar nested text fields) into a single reasoning string.
func ExtractReasoningDetailsText(raw any) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			if s := ExtractReasoningDetailsText(item); s != "" {
				parts = append(parts, s)
			}
		}
		return JoinNonEmpty(parts)
	case map[string]any:
		if s := AsTrimmedString(v["text"]); s != "" {
			return s
		}
		if s := AsTrimmedString(v["content"]); s != "" {
			return s
		}
		if s := AsTrimmedString(v["reasoning"]); s != "" {
			return s
		}
		if nested, ok := v["reasoning_details"]; ok {
			return ExtractReasoningDetailsText(nested)
		}
		return ""
	default:
		return ""
	}
}

func ParseJSONLike(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return map[string]any{"value": raw}
	}
	return v
}

// --- internal ---

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
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func jsonNum(f float64) string {
	return strings.TrimRight(strings.TrimRight(
		strings.Replace(jsonNumRaw(f), "e+", "e", 1), "0"), ".")
}

func jsonNumRaw(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func itoa(n int64) string {
	return strings.TrimSpace(strings.Replace(
		strings.Replace(jsonNumRaw(float64(n)), "e+00", "", 1), ".0", "", 1))
}

func sortInts(s []int) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
