package proxyhandler

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
)

const maxIncrementalSsePendingBytes = 1 << 20

// SseEvent represents a parsed SSE event with its fields.
type SseEvent struct {
	Data  string // joined multi-line data content
	Event string // event type (default: "message")
	ID    string // event ID
	Retry int64  // retry interval in milliseconds (0 = not set)
}

// SseParseResult contains the parsed events and aggregate state.
type SseParseResult struct {
	Events        []SseEvent
	HasDataEvent  bool // at least one event with non-empty data
	HasErrorEvent bool // at least one event with error-type data
	HasDoneMarker bool // at least one [DONE] event
}

type incrementalSseAnalysisResult struct {
	ErrorEvents           []SseEvent
	HasDataEvent          bool
	HasErrorEvent         bool
	HasDoneMarker         bool
	EventCount            int
	PendingBytes          int
	DroppedOversizedEvent bool
	// Usage is the best-effort final usage extracted from SSE data events.
	Usage ParsedUsage
}

type incrementalSseAnalyzer struct {
	pending               string
	skippingOversized     bool
	droppedOversizedEvent bool
	result                incrementalSseAnalysisResult
	usage                 ParsedUsage
}

func newIncrementalSseAnalyzer() *incrementalSseAnalyzer {
	return &incrementalSseAnalyzer{}
}

func (a *incrementalSseAnalyzer) Push(chunk []byte) {
	text := string(chunk)
	for len(text) > 0 {
		if a.skippingOversized {
			boundary := strings.Index(text, "\n\n")
			if boundary < 0 {
				return
			}
			text = text[boundary+2:]
			a.skippingOversized = false
		}

		space := maxIncrementalSsePendingBytes - len(a.pending)
		if space <= 0 {
			a.dropCurrentEvent()
			continue
		}
		if len(text) > space {
			a.pending += text[:space]
			text = text[space:]
			a.drainCompleteEvents()
			if len(a.pending) >= maxIncrementalSsePendingBytes {
				a.dropCurrentEvent()
			}
			continue
		}

		a.pending += text
		text = ""
		a.drainCompleteEvents()
	}
}

func (a *incrementalSseAnalyzer) Result() incrementalSseAnalysisResult {
	result := a.result
	result.PendingBytes = len(a.pending)
	result.DroppedOversizedEvent = a.droppedOversizedEvent
	result.Usage = a.usage
	if result.Usage.Source == "" {
		if result.Usage.Found {
			result.Usage.Source = usageSourceUpstream
		} else {
			result.Usage.Source = usageSourceUnknown
		}
	}
	return result
}

func (a *incrementalSseAnalyzer) dropCurrentEvent() {
	a.pending = ""
	a.skippingOversized = true
	a.droppedOversizedEvent = true
}

func (a *incrementalSseAnalyzer) drainCompleteEvents() {
	for {
		boundary, sepLen := nextSseBoundary(a.pending)
		if boundary < 0 {
			return
		}

		block := a.pending[:boundary]
		a.pending = a.pending[boundary+sepLen:]
		if block == "" {
			continue
		}
		ev := parseSseBlock(block)
		if ev == nil {
			continue
		}
		a.recordEvent(*ev)
	}
}

func nextSseBoundary(s string) (int, int) {
	lf := strings.Index(s, "\n\n")
	crlf := strings.Index(s, "\r\n\r\n")
	switch {
	case lf < 0 && crlf < 0:
		return -1, 0
	case lf < 0:
		return crlf, 4
	case crlf < 0:
		return lf, 2
	case crlf < lf:
		return crlf, 4
	default:
		return lf, 2
	}
}

func (a *incrementalSseAnalyzer) recordEvent(ev SseEvent) {
	a.result.EventCount++
	if ev.Data != "" && ev.Data != "[DONE]" {
		a.result.HasDataEvent = true
		// Best-effort usage extraction without retaining full stream bodies.
		// Later events with usage win (stream-end usage / message_delta / response.completed).
		if looksLikeJSONObject(ev.Data) {
			got := ParseUsageFromBody([]byte(ev.Data))
			if got.Found {
				a.usage = mergeUsagePreferLater(a.usage, got)
			}
		}
	}
	if IsSseErrorEvent(ev) {
		a.result.HasErrorEvent = true
		a.result.ErrorEvents = append(a.result.ErrorEvents, ev)
	}
	if IsSseDoneMarker(ev) {
		a.result.HasDoneMarker = true
	}
}

// ParseSseStream parses a raw SSE byte stream into structured events.
//
// SSE protocol (text/event-stream):
//   - Events are separated by double newlines (\n\n)
//   - Lines starting with "data:" contain event data
//   - Lines starting with "event:" set the event type
//   - Lines starting with "id:" set the event ID
//   - Lines starting with "retry:" set the reconnection time in ms
//   - Lines starting with ":" are comments (ignored)
//   - Multi-line data: consecutive "data:" lines are joined with "\n"
//
// Returns the remaining incomplete buffer (partial event) for the next call.
func ParseSseStream(raw string) ([]SseEvent, string) {
	// Normalize line endings
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")

	var events []SseEvent
	rest := normalized

	for {
		boundary := strings.Index(rest, "\n\n")
		if boundary < 0 {
			break
		}

		block := rest[:boundary]
		rest = rest[boundary+2:]

		if block == "" {
			continue
		}

		ev := parseSseBlock(block)
		if ev != nil {
			events = append(events, *ev)
		}
	}

	return events, rest
}

// parseSseBlock parses a single SSE event block (everything between \n\n).
func parseSseBlock(block string) *SseEvent {
	var data string
	var dataBuilder strings.Builder
	dataLineCount := 0
	dataBuilderUsed := false
	var eventType string
	var eventID string
	var retry int64

	for len(block) > 0 {
		line := block
		if idx := strings.IndexByte(block, '\n'); idx >= 0 {
			line = block[:idx]
			block = block[idx+1:]
		} else {
			block = ""
		}
		line = strings.TrimSuffix(line, "\r")
		// Skip comment lines
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataVal := strings.TrimPrefix(line, "data:")
			dataVal = trimLeadingSpace(dataVal)
			switch dataLineCount {
			case 0:
				data = dataVal
			case 1:
				dataBuilder.WriteString(data)
				dataBuilder.WriteByte('\n')
				dataBuilder.WriteString(dataVal)
				dataBuilderUsed = true
			default:
				dataBuilder.WriteByte('\n')
				dataBuilder.WriteString(dataVal)
			}
			dataLineCount++
		} else if strings.HasPrefix(line, "event:") {
			eventVal := strings.TrimPrefix(line, "event:")
			eventType = strings.TrimSpace(eventVal)
		} else if strings.HasPrefix(line, "id:") {
			idVal := strings.TrimPrefix(line, "id:")
			eventID = strings.TrimSpace(idVal)
		} else if strings.HasPrefix(line, "retry:") {
			retryVal := strings.TrimPrefix(line, "retry:")
			if v, err := strconv.ParseInt(strings.TrimSpace(retryVal), 10, 64); err == nil {
				retry = v
			}
		}
	}

	if dataBuilderUsed {
		data = dataBuilder.String()
	}

	// An event block is valid if it has either data or a named event type
	if data == "" && eventType == "" {
		return nil
	}

	if eventType == "" {
		eventType = "message"
	}

	return &SseEvent{
		Data:  data,
		Event: eventType,
		ID:    eventID,
		Retry: retry,
	}
}

// trimLeadingSpace removes a single leading space if present (SSE spec: colon+space).
func trimLeadingSpace(s string) string {
	if len(s) > 0 && s[0] == ' ' {
		return s[1:]
	}
	return s
}

// IsSseErrorEvent checks if an SSE event's data payload represents an error.
// Detects JSON objects containing "error" keys at the top level, or error
// event types like "response.failed", "error".
func IsSseErrorEvent(ev SseEvent) bool {
	if ev.Event == "error" || ev.Event == "response.failed" {
		return true
	}

	if ev.Data == "" || ev.Data == "[DONE]" {
		return false
	}
	if !looksLikeJSONObject(ev.Data) {
		return false
	}

	// Try to parse data as JSON and check for error structure
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(ev.Data), &parsed); err != nil {
		return false
	}

	if _, hasError := parsed["error"]; hasError {
		return true
	}

	// Check for type: "error" field
	if typ, ok := parsed["type"].(string); ok {
		return typ == "error" || typ == "response.failed"
	}

	return false
}

func looksLikeJSONObject(s string) bool {
	s = strings.TrimLeft(s, " \t\r\n")
	return strings.HasPrefix(s, "{")
}

// IsSseDoneMarker checks if an SSE event is a [DONE] marker.
func IsSseDoneMarker(ev SseEvent) bool {
	return ev.Data == "[DONE]"
}

// BuildSseParseResult aggregates parse state from parsed events.
func BuildSseParseResult(events []SseEvent) SseParseResult {
	result := SseParseResult{
		Events: events,
	}

	for _, ev := range events {
		if ev.Data != "" && ev.Data != "[DONE]" {
			result.HasDataEvent = true
		}
		if IsSseErrorEvent(ev) {
			result.HasErrorEvent = true
		}
		if IsSseDoneMarker(ev) {
			result.HasDoneMarker = true
		}
	}

	return result
}

// ParseAndAnalyzeSseStream parses an SSE stream and returns the aggregate result.
// Convenience wrapper around ParseSseStream + BuildSseParseResult.
func ParseAndAnalyzeSseStream(raw string) SseParseResult {
	events, _ := ParseSseStream(raw)
	return BuildSseParseResult(events)
}

// LogSseErrorEvents logs each SSE error event at WARN level.
func LogSseErrorEvents(events []SseEvent) {
	for _, ev := range events {
		if IsSseErrorEvent(ev) {
			slog.Warn("SSE error event detected",
				"event_type", ev.Event,
				"event_id", ev.ID,
				"data", ev.Data,
			)
		}
	}
}
