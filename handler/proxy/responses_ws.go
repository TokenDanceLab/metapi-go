package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tokendancelab/metapi-go/auth"
)

// EnsureResponsesWebsocketTransport registers the WebSocket upgrade handler
// for /v1/responses on the given HTTP server.
//
// In the TS codebase, this is installed via server.on('upgrade') and only
// handles pathname === '/v1/responses'. The Go implementation uses
// the net/http Hijacker interface for WebSocket upgrades.
//
// For the initial P10 implementation, WebSocket support is stubbed.
// The full implementation (Codex WS runtime, message serialization,
// pre-warm synthesis, HTTP fallback) will be completed in a follow-up pass.
func EnsureResponsesWebsocketTransport(srv *http.Server, cfg WebSocketConfig) {
	slog.Info("registering responses WebSocket transport (stub)")

	// Store the config for use during upgrade
	_ = srv
	_ = cfg
}

// WebSocketConfig holds configuration for the responses WebSocket transport.
type WebSocketConfig struct {
	// AuthTokenValidation is used to validate tokens during WS upgrade.
	AuthTokenValidation func(token string) (*auth.ProxyAuthContext, error)
}

// extractWSHeaders extracts relevant headers for WebSocket context detection.
func extractWSHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}

// extractWSTurnState extracts the x-codex-turn-state header value.
func extractWSTurnState(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("x-codex-turn-state"))
}

// ResponsesWSMessage is a WebSocket message from the downstream client.
type ResponsesWSMessage struct {
	Type             string         `json:"type"`
	Model            string         `json:"model,omitempty"`
	Generate         *bool          `json:"generate,omitempty"`
	Input            []any          `json:"input,omitempty"`
	Instructions     any            `json:"instructions,omitempty"`
	PreviousResponseID string       `json:"previous_response_id,omitempty"`
	Raw              map[string]any `json:"-"`
}

// ParseResponsesWSMessage parses a WebSocket JSON message.
func ParseResponsesWSMessage(raw []byte) (*ResponsesWSMessage, error) {
	var msg ResponsesWSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	msg.Raw = make(map[string]any)
	json.Unmarshal(raw, &msg.Raw)
	return &msg, nil
}

// ResponsesWSError writes a WebSocket error frame.
func ResponsesWSError(status int, message string) map[string]any {
	return map[string]any{
		"type":  "error",
		"status": status,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
		},
	}
}

// SynthesizePrewarmResponsePayloads generates pre-warm response.created + response.completed
// for Codex file-search pre-warm mode (generate=false without incremental input).
func SynthesizePrewarmResponsePayloads(model string, responseID string) []map[string]any {
	if responseID == "" {
		responseID = "resp_prewarm_metapi"
	}
	modelName := model
	if modelName == "" {
		modelName = "unknown"
	}
	return []map[string]any{
		{
			"type": "response.created",
			"response": map[string]any{
				"id":       responseID,
				"object":   "response",
				"status":   "in_progress",
				"model":    modelName,
				"output":   []any{},
			},
		},
		{
			"type": "response.completed",
			"response": map[string]any{
				"id":       responseID,
				"object":   "response",
				"status":   "completed",
				"model":    modelName,
				"output":   []any{},
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
					"total_tokens":  0,
				},
			},
		},
	}
}
