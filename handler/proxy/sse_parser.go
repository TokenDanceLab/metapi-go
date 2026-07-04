package proxy

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
)

// SseEvent represents a parsed SSE event with its fields.
type SseEvent struct {
	Data  string // joined multi-line data content
	Event string // event type (default: "message")
	ID    string // event ID
	Retry int64  // retry interval in milliseconds (0 = not set)
}

// SseParseResult contains the parsed events and aggregate state.
type SseParseResult struct {
	Events         []SseEvent
	HasDataEvent   bool // at least one event with non-empty data
	HasErrorEvent  bool // at least one event with error-type data
	HasDoneMarker  bool // at least one [DONE] event
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
	var dataLines []string
	var eventType string
	var eventID string
	var retry int64

	lines := strings.Split(block, "\n")
	for _, line := range lines {
		// Skip comment lines
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			dataVal := strings.TrimPrefix(line, "data:")
			dataVal = trimLeadingSpace(dataVal)
			dataLines = append(dataLines, dataVal)
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

	// Join multi-line data
	data := strings.Join(dataLines, "\n")

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
