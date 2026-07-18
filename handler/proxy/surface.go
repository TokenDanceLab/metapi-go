package proxyhandler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/proxy"
)

// SurfConfig is the configuration for a proxy surface handler.
type SurfConfig struct {
	Endpoint       string
	DownstreamPath string
	RequireModel   bool
	DefaultModel   string
	// ForceStream forces IsStream=true even when the body omits stream:true.
	// Used by Gemini path action streamGenerateContent and CLI
	// /v1internal::streamGenerateContent where streaming is path-implied.
	ForceStream   bool
	Method        string
	ExtraHeaders  map[string]string
	MaxRetries    int
	SurfaceFormat string // "openai", "claude", or empty
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
	Auth           *auth.ProxyAuthContext
	Policy         auth.DownstreamRoutingPolicy
	Body           map[string]any
	RawBody        []byte // raw request body bytes for zero-copy model swap in upstream
	Headers        map[string]string
	ClientCtx      proxy.DownstreamClientContext
	DownstreamPath string
	RequestedModel string
	SurfaceFormat  string
	IsStream       bool
	Multipart      bool
	Retries        int
	MaxRetries     int
	// ForcedChannelID pins channel selection to a specific route channel when set
	// (videos sticky pin from mapping #253; tester forced channel paths).
	// When non-nil and >0, SelectProxyChannelForAttempt uses SelectPreferredChannel
	// and does not fall back to other channels on retry.
	ForcedChannelID *int64
}

// PrepareCtx extracts all context needed for proxy request handling.
func PrepareCtx(r *http.Request, cfg SurfConfig) (*Ctx, *SurfResult) {
	authCtx := GetProxyAuth(r)
	if authCtx == nil {
		return nil, &SurfResult{OK: false, Status: 401, Error: "unauthorized", ErrorType: "invalid_request_error"}
	}

	isMultipart := IsMultipartRequest(r)
	var bodyBytes []byte
	body := make(map[string]any)

	if isMultipart {
		mp, err := ParseMultipartFormData(r)
		if err != nil {
			if isRequestBodyTooLarge(err) {
				return nil, &SurfResult{OK: false, Status: http.StatusRequestEntityTooLarge, Error: "request body too large", ErrorType: "invalid_request_error"}
			}
			return nil, &SurfResult{OK: false, Status: 400, Error: err.Error(), ErrorType: "invalid_request_error"}
		}
		if mp != nil {
			for key, values := range mp.Values {
				if len(values) > 0 {
					body[key] = strings.TrimSpace(values[0])
				}
			}
		}
	} else {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			if isRequestBodyTooLarge(err) {
				return nil, &SurfResult{OK: false, Status: http.StatusRequestEntityTooLarge, Error: "request body too large", ErrorType: "invalid_request_error"}
			}
			return nil, &SurfResult{OK: false, Status: 400, Error: "failed to read request body", ErrorType: "invalid_request_error"}
		}
		r.Body.Close()

		if len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				return nil, &SurfResult{OK: false, Status: 400, Error: "invalid JSON body", ErrorType: "invalid_request_error"}
			}
		}
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

	// Check stream flag (body and optional path/surface force).
	isStream := isStreamFromBody(body)
	if cfg.ForceStream {
		isStream = true
	}

	// Client detection
	headers := HeaderMapFromRequest(r.Header)
	clientCtx := DetectClientContext(cfg.DownstreamPath, headers, body)

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = proxy.GetProxyMaxChannelRetries(config.Get().ProxyMaxChannelAttempts)
	}

	return &Ctx{
		Auth:           authCtx,
		Policy:         authCtx.Policy,
		Body:           body,
		RawBody:        bodyBytes,
		Headers:        headers,
		ClientCtx:      clientCtx,
		DownstreamPath: cfg.DownstreamPath,
		RequestedModel: requestedModel,
		SurfaceFormat:  cfg.SurfaceFormat,
		IsStream:       isStream,
		Multipart:      isMultipart,
		MaxRetries:     maxRetries,
	}, nil
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr) || errors.Is(err, ErrMultipartLimitExceeded)
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
