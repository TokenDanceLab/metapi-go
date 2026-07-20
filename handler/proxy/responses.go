package proxyhandler

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HandleResponses handles POST /v1/responses and /v1/responses/compact.
func HandleResponses(w http.ResponseWriter, r *http.Request, downstreamPath string) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "responses",
		DownstreamPath: downstreamPath,
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	if ctx.IsStream {
		dispatchUpstream(w, r, ctx)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleResponsesGet426 handles GET /v1/responses.
// Plain GET → 426 Upgrade Required. WebSocket upgrade attempt → C1 transport
// (coder/websocket + HTTP SSE bridge; see EnsureResponsesWebsocketTransport).
func HandleResponsesGet426(w http.ResponseWriter, r *http.Request) {
	if IsWebsocketUpgradeRequest(r) {
		HandleResponsesWebsocket(w, r)
		return
	}
	path := r.URL.Path
	writeJSONError(w, 426,
		fmt.Sprintf("WebSocket upgrade required for GET %s", path),
		"invalid_request_error")
}

// HandleResponsesAliasPost handles POST /responses and /responses/* (alias paths).
func HandleResponsesAliasPost(w http.ResponseWriter, r *http.Request) {
	downstreamPath := resolveAliasedResponsesPath(r.URL.Path)
	if downstreamPath == "" {
		writeJSONError(w, 404, "Unknown /responses alias path", "invalid_request_error")
		return
	}
	HandleResponses(w, r, downstreamPath)
}

// HandleResponsesAliasGet426 handles GET /responses and /responses/*.
// Unknown alias → 404. Upgrade attempt → C1 WS transport. Plain GET → 426.
func HandleResponsesAliasGet426(w http.ResponseWriter, r *http.Request) {
	downstreamPath := resolveAliasedResponsesPath(r.URL.Path)
	if downstreamPath == "" {
		writeJSONError(w, 404, "Unknown /responses alias path", "invalid_request_error")
		return
	}
	if IsWebsocketUpgradeRequest(r) {
		HandleResponsesWebsocket(w, r)
		return
	}
	writeJSONError(w, 426,
		fmt.Sprintf("WebSocket upgrade required for GET %s", downstreamPath),
		"invalid_request_error")
}

// resolveAliasedResponsesPath resolves /responses alias paths to /v1/responses paths.
func resolveAliasedResponsesPath(path string) string {
	// Strip query string
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if path == "/responses" {
		return "/v1/responses"
	}
	if strings.HasSuffix(path, "/compact") {
		return "/v1/responses/compact"
	}
	return ""
}

func currentUnix() int64 {
	return time.Now().Unix()
}
