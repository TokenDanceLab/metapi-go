package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/proxy"
)

// SurfConfig is the configuration for a proxy surface handler.
type SurfConfig struct {
	Endpoint        string
	DownstreamPath  string
	RequireModel    bool
	DefaultModel    string
	Method          string
	ExtraHeaders    map[string]string
	MaxRetries      int
	SurfaceFormat   string // "openai", "claude", or empty
}

// SurfResult is the result of processing a proxy surface request.
type SurfResult struct {
	OK         bool
	Status     int
	Body       []byte
	Error      string
	ErrorType  string
	Stream     bool
	StreamBody io.ReadCloser
	Usage      *proxy.UsageSummary
	LatencyMs  int64
	// Selected   *prouting.SelectedChannel  // populated after channel selection
	ClientCtx proxy.DownstreamClientContext
}

// Ctx holds all context needed for a proxy request.
type Ctx struct {
	Auth      *auth.ProxyAuthContext
	Policy    auth.DownstreamRoutingPolicy
	Body      map[string]any
	Headers   map[string]string
	ClientCtx proxy.DownstreamClientContext
	RequestedModel string
	SurfaceFormat  string
	IsStream  bool
	Retries   int
	MaxRetries int
}

// PrepareCtx extracts all context needed for proxy request handling.
func PrepareCtx(r *http.Request, cfg SurfConfig) (*Ctx, *SurfResult) {
	authCtx := GetProxyAuth(r)
	if authCtx == nil {
		return nil, &SurfResult{OK: false, Status: 401, Error: "unauthorized", ErrorType: "invalid_request_error"}
	}

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, &SurfResult{OK: false, Status: 400, Error: "failed to read request body", ErrorType: "invalid_request_error"}
	}
	r.Body.Close()

	var body map[string]any
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return nil, &SurfResult{OK: false, Status: 400, Error: "invalid JSON body", ErrorType: "invalid_request_error"}
		}
	}
	if body == nil {
		body = make(map[string]any)
	}

	// Validate requested model
	requestedModel, _ := body["model"].(string)
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" && cfg.DefaultModel != "" {
		requestedModel = cfg.DefaultModel
		body["model"] = requestedModel
	}
	if requestedModel == "" && cfg.RequireModel {
		return nil, &SurfResult{OK: false, Status: 400, Error: "model is required", ErrorType: "invalid_request_error"}
	}

	// Downstream policy check: ensure the requested model is allowed
	if requestedModel != "" && !IsModelAllowedByPolicy(requestedModel, authCtx.Policy) {
		return nil, &SurfResult{OK: false, Status: 403, Error: "model not allowed by downstream policy", ErrorType: "invalid_request_error"}
	}

	// Check stream flag
	isStream := isStreamFromBody(body)

	// Client detection
	headers := HeaderMapFromRequest(r.Header)
	clientCtx := DetectClientContext(cfg.DownstreamPath, headers, body)

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = proxy.GetProxyMaxChannelRetries(config.Get().ProxyMaxChannelAttempts)
	}

	return &Ctx{
		Auth:      authCtx,
		Policy:    authCtx.Policy,
		Body:      body,
		Headers:   headers,
		ClientCtx:  clientCtx,
		RequestedModel: requestedModel,
		SurfaceFormat:  cfg.SurfaceFormat,
		IsStream:   isStream,
		MaxRetries: maxRetries,
	}, nil
}

// isStreamFromBody checks the stream flag from the request body.
func isStreamFromBody(body map[string]any) bool {
	if v, ok := body["stream"]; ok {
		if b, ok := v.(bool); ok && b {
			return true
		}
		if s, ok := v.(string); ok && (s == "true" || s == "1") {
			return true
		}
		if n, ok := v.(float64); ok && n != 0 {
			return true
		}
	}
	return false
}

// writeSSEHeaders sets SSE response headers.
func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

// sseEvent formats an SSE data event.
func sseEvent(data string) []byte {
	return []byte("data: " + data + "\n\n")
}

// sseDone returns the SSE [DONE] marker.
func sseDone() []byte {
	return []byte("data: [DONE]\n\n")
}

// Ensure slog is used
var _ = slog.Info
