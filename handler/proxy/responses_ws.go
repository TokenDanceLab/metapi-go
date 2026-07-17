package proxyhandler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/tokendancelab/metapi-go/auth"
)

// Residual status for Responses WebSocket transport (#217).
// There is intentionally no gorilla/websocket (or other WS) dependency and no
// Codex multi-turn WS runtime. Boot still calls EnsureResponsesWebsocketTransport
// so registration is explicit rather than a silent stub that is never invoked.
const (
	ResponsesWebsocketResidualStatus = "not_implemented"
	ResponsesWebsocketResidualDoc    = "docs/analysis/responses-websocket-residual.md"
)

// responsesWebsocketTransportRegistered is set by EnsureResponsesWebsocketTransport.
// Tests assert the boot path actually registers the residual.
var responsesWebsocketTransportRegistered atomic.Bool

// ResponsesWebsocketTransportRegistered reports whether the residual transport
// registration entrypoint has been called in this process.
func ResponsesWebsocketTransportRegistered() bool {
	return responsesWebsocketTransportRegistered.Load()
}

// ResetResponsesWebsocketTransportForTest clears residual registration state.
// Test-only helper.
func ResetResponsesWebsocketTransportForTest() {
	responsesWebsocketTransportRegistered.Store(false)
}

// EnsureResponsesWebsocketTransport registers the Responses WebSocket residual
// for /v1/responses on the given HTTP server.
//
// Honest residual behavior (no fake WS completions):
//   - No net/http ConnState / Hijacker upgrade loop is installed.
//   - Plain GET /v1/responses continues to return 426 (upgrade required).
//   - GET with Upgrade: websocket is refused with 501 (not implemented).
//   - Codex profile SupportsResponsesWebsocketIncremental describes client
//     capability detection only — not server transport readiness.
//
// Full Codex WS runtime (message loop, pre-warm synthesis on wire, HTTP
// fallback from an open socket) remains out of scope for this residual.
func EnsureResponsesWebsocketTransport(srv *http.Server, cfg WebSocketConfig) {
	responsesWebsocketTransportRegistered.Store(true)

	// Keep cfg/srv referenced so future Hijacker scaffolding can plug in
	// without changing the boot call site. No silent success theater.
	_ = srv
	_ = cfg

	slog.Info("responses WebSocket transport residual registered",
		"status", ResponsesWebsocketResidualStatus,
		"http_get", "426 Upgrade Required (non-upgrade GET)",
		"upgrade", "501 Not Implemented (no Codex WS runtime)",
		"doc", ResponsesWebsocketResidualDoc,
	)
}

// WebSocketConfig holds optional hooks for a future Responses WebSocket transport.
// Residual registration accepts the config so boot wiring stays stable.
type WebSocketConfig struct {
	// AuthTokenValidation is used to validate tokens during WS upgrade.
	// Unused while transport is residual-only.
	AuthTokenValidation func(token string) (*auth.ProxyAuthContext, error)
}

// IsWebsocketUpgradeRequest reports whether r is a WebSocket upgrade attempt.
// Used to distinguish plain GET 426 from residual 501 on upgrade.
func IsWebsocketUpgradeRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}

// HandleResponsesWebsocketUpgradeResidual refuses a WebSocket upgrade without
// inventing frames or fake completions. Prefer 501 over Hijack+silent close so
// clients get a clear residual JSON error on the HTTP path.
func HandleResponsesWebsocketUpgradeResidual(w http.ResponseWriter, r *http.Request) {
	path := "/v1/responses"
	if r != nil && r.URL != nil && strings.TrimSpace(r.URL.Path) != "" {
		path = r.URL.Path
	}
	writeJSONError(w, http.StatusNotImplemented,
		"Responses WebSocket transport is not implemented for "+path+" (residual; see "+ResponsesWebsocketResidualDoc+")",
		"invalid_request_error")
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
// Parsing helpers exist for a future runtime; residual path does not open sockets.
type ResponsesWSMessage struct {
	Type               string         `json:"type"`
	Model              string         `json:"model,omitempty"`
	Generate           *bool          `json:"generate,omitempty"`
	Input              []any          `json:"input,omitempty"`
	Instructions       any            `json:"instructions,omitempty"`
	PreviousResponseID string         `json:"previous_response_id,omitempty"`
	Raw                map[string]any `json:"-"`
}

// ParseResponsesWSMessage parses a WebSocket JSON message.
func ParseResponsesWSMessage(raw []byte) (*ResponsesWSMessage, error) {
	var msg ResponsesWSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	msg.Raw = make(map[string]any)
	_ = json.Unmarshal(raw, &msg.Raw)
	return &msg, nil
}

// ResponsesWSError builds a WebSocket-shaped error object (not written on residual path).
func ResponsesWSError(status int, message string) map[string]any {
	return map[string]any{
		"type":   "error",
		"status": status,
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": message,
		},
	}
}

// SynthesizePrewarmResponsePayloads generates pre-warm response.created + response.completed
// for a future Codex file-search pre-warm mode (generate=false without incremental input).
// Residual transport does not emit these on the wire.
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
				"id":     responseID,
				"object": "response",
				"status": "in_progress",
				"model":  modelName,
				"output": []any{},
			},
		},
		{
			"type": "response.completed",
			"response": map[string]any{
				"id":     responseID,
				"object": "response",
				"status": "completed",
				"model":  modelName,
				"output": []any{},
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
					"total_tokens":  0,
				},
			},
		},
	}
}
