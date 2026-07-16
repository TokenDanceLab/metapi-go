package proxy

import (
	"fmt"
	"strings"
)

// UpstreamEndpoint represents an upstream API endpoint type.
type UpstreamEndpoint string

const (
	EndpointChat      UpstreamEndpoint = "chat"      // /v1/chat/completions
	EndpointMessages  UpstreamEndpoint = "messages"  // /v1/messages (Anthropic)
	EndpointResponses UpstreamEndpoint = "responses" // /v1/responses (Codex)
)

// PathForEndpoint returns the canonical upstream path for a known endpoint type.
// Unknown endpoints return "".
func PathForEndpoint(endpoint UpstreamEndpoint) string {
	switch endpoint {
	case EndpointChat:
		return "/v1/chat/completions"
	case EndpointMessages:
		return "/v1/messages"
	case EndpointResponses:
		return "/v1/responses"
	default:
		return ""
	}
}

// EndpointFromPath maps a downstream/upstream path to a known chat-family endpoint.
// Non chat-family paths return ("", false).
func EndpointFromPath(path string) (UpstreamEndpoint, bool) {
	path = strings.TrimSpace(path)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimRight(path, "/")
	switch {
	case strings.HasSuffix(path, "/v1/chat/completions") || path == "/chat/completions" || path == "/v1/chat/completions":
		return EndpointChat, true
	case strings.HasSuffix(path, "/v1/messages") || path == "/messages" || path == "/v1/messages" ||
		strings.HasSuffix(path, "/anthropic/v1/messages"):
		return EndpointMessages, true
	case strings.HasSuffix(path, "/v1/responses") || path == "/responses" || path == "/v1/responses":
		return EndpointResponses, true
	default:
		return "", false
	}
}

// ResolveEndpointCandidates builds the ordered multi-protocol candidate list for a
// downstream path. Primary endpoint is first. When disableCrossProtocolFallback is
// true, only the primary is returned. Non chat-family paths yield a single synthetic
// candidate list of length 1 using the original path via PathForEndpoint fallbacks.
//
// Product policy (upstream #387 / issue #38):
// - first-byte timeout or protocol-mismatch may continue to remaining candidates
// - DISABLE_CROSS_PROTOCOL_FALLBACK stops after the primary attempt
func ResolveEndpointCandidates(downstreamPath string, disableCrossProtocolFallback bool) []UpstreamEndpoint {
	primary, ok := EndpointFromPath(downstreamPath)
	if !ok {
		// Non chat-family: no multi-protocol list. Caller should use the original path.
		return nil
	}
	if disableCrossProtocolFallback {
		return []UpstreamEndpoint{primary}
	}
	// Primary first, then remaining chat-family protocols in stable order.
	order := []UpstreamEndpoint{EndpointChat, EndpointMessages, EndpointResponses}
	// Prefer responses→chat→messages when primary is responses (common Codex downgrade).
	switch primary {
	case EndpointResponses:
		order = []UpstreamEndpoint{EndpointResponses, EndpointChat, EndpointMessages}
	case EndpointMessages:
		order = []UpstreamEndpoint{EndpointMessages, EndpointChat, EndpointResponses}
	case EndpointChat:
		order = []UpstreamEndpoint{EndpointChat, EndpointMessages, EndpointResponses}
	}
	out := make([]UpstreamEndpoint, 0, len(order))
	seen := map[UpstreamEndpoint]bool{}
	for _, ep := range order {
		if seen[ep] {
			continue
		}
		seen[ep] = true
		out = append(out, ep)
	}
	return out
}

// ShouldDowngradeToNextEndpoint reports whether an upstream error indicates the
// current protocol/path is wrong and a different endpoint candidate should be tried.
func ShouldDowngradeToNextEndpoint(status int, rawErrText string) bool {
	if status <= 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(rawErrText))
	if text == "" {
		return false
	}
	// Protocol redirect hints from upstream (also in retry_policy patterns).
	if strings.Contains(text, "please use /v1/chat/completions") ||
		strings.Contains(text, "please use /v1/messages") ||
		strings.Contains(text, "please use /v1/responses") ||
		strings.Contains(text, "unsupported endpoint") ||
		strings.Contains(text, "unsupported path") ||
		strings.Contains(text, "unknown endpoint") ||
		strings.Contains(text, "unrecognized request url") ||
		strings.Contains(text, "unsupported legacy protocol") {
		return true
	}
	// 404 on a protocol path is a common wrong-endpoint signal.
	if status == 404 {
		return true
	}
	return false
}

// BuiltEndpointRequest is a built upstream request for a specific endpoint.
type BuiltEndpointRequest struct {
	Endpoint UpstreamEndpoint
	Path     string
	Headers  map[string]string
	Body     map[string]any
	Runtime  *EndpointRuntime
}

// EndpointRuntime holds runtime metadata for a request.
type EndpointRuntime struct {
	Executor       string // "default", "codex", "gemini-cli", "antigravity", "claude"
	ModelName      string
	Stream         bool
	OAuthProjectID string
	Action         string // "generateContent", "streamGenerateContent", "countTokens"
}

// Dispatcher is a function that dispatches an HTTP request.
// If firstByteTimeoutMs > 0, the dispatcher should observe first-byte latency
// and return a status-0 response on timeout.
type Dispatcher func(request BuiltEndpointRequest, targetURL string, firstByteTimeoutMs int64) (*ExecutorDispatchResult, error)

// EndpointAttemptContext holds context for an endpoint attempt.
type EndpointAttemptContext struct {
	EndpointIndex  int
	EndpointCount  int
	Request        BuiltEndpointRequest
	TargetURL      string
	Response       *ExecutorDispatchResult
	RawErrText     string
	ErrText        string
	RecoverApplied bool
}

// EndpointAttemptSuccessContext holds context for a successful endpoint attempt.
type EndpointAttemptSuccessContext struct {
	EndpointIndex  int
	EndpointCount  int
	Request        BuiltEndpointRequest
	TargetURL      string
	Response       *ExecutorDispatchResult
	RecoverApplied bool
}

// RecoverResult is the result of a recovery attempt.
type RecoverResult struct {
	Response     *ExecutorDispatchResult
	UpstreamPath string
	Request      *BuiltEndpointRequest
	TargetURL    string
}

// EndpointFlowResult is the result of ExecuteEndpointFlow.
type EndpointFlowResult struct {
	OK           bool
	Response     *ExecutorDispatchResult
	UpstreamPath string
	Status       int
	ErrText      string
	RawErrText   string
}

// ExecuteEndpointFlowInput is the input for ExecuteEndpointFlow.
type ExecuteEndpointFlowInput struct {
	SiteURL                       string
	ProxyURL                      string
	DisableCrossProtocolFallback  bool
	EndpointCandidates            []UpstreamEndpoint
	BuildRequest                  func(endpoint UpstreamEndpoint, endpointIndex int) BuiltEndpointRequest
	DispatchRequest               func(request BuiltEndpointRequest, targetURL string, firstByteTimeoutMs int64) (*ExecutorDispatchResult, error)
	FirstByteTimeoutMs            int64
	TryRecover                    func(ctx *EndpointAttemptContext) *RecoverResult
	ShouldDowngrade               func(ctx EndpointAttemptContext) bool
	ShouldAbortRemainingEndpoints func(ctx EndpointAttemptContext) bool
	OnDowngrade                   func(ctx EndpointAttemptContext)
	OnAttemptFailure              func(ctx EndpointAttemptContext)
	OnAttemptSuccess              func(ctx EndpointAttemptSuccessContext)
}

// BuildUpstreamURL constructs an upstream URL from a site URL and path.
func BuildUpstreamURL(siteURL, path string) string {
	siteURL = strings.TrimRight(siteURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if hasVersionedBasePath(siteURL) {
		path = stripLeadingVersionSegment(path)
	}
	return siteURL + path
}

func hasVersionedBasePath(siteURL string) bool {
	base := siteURL
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		return false
	}
	lastSlash := strings.LastIndex(base, "/")
	if lastSlash < 0 || lastSlash == len(base)-1 {
		return false
	}
	return isVersionSegment(base[lastSlash+1:])
}

func stripLeadingVersionSegment(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	segment, rest, found := strings.Cut(trimmed, "/")
	if !found || !isVersionSegment(segment) {
		return path
	}
	if rest == "" {
		return "/"
	}
	return "/" + rest
}

func isVersionSegment(segment string) bool {
	if len(segment) < 2 || (segment[0] != 'v' && segment[0] != 'V') || !isASCIIDigit(segment[1]) {
		return false
	}
	for i := 2; i < len(segment); i++ {
		c := segment[i]
		if !isASCIIDigit(c) && !isASCIIAlpha(c) {
			return false
		}
	}
	return true
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isASCIIAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// SummarizeUpstreamError creates a human-readable upstream error summary.
func SummarizeUpstreamError(status int, rawErrText string) string {
	trimmed := strings.TrimSpace(rawErrText)
	if trimmed == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	if len(trimmed) > 200 {
		trimmed = trimmed[:200]
	}
	return fmt.Sprintf("HTTP %d: %s", status, trimmed)
}

// WithUpstreamPath prefixes a message with the upstream path context.
func WithUpstreamPath(path, message string) string {
	return fmt.Sprintf("[upstream:%s] %s", path, message)
}

// ExecuteEndpointFlow runs the endpoint-level iteration loop.
// Iterates over endpointCandidates until success or exhaustion.
func ExecuteEndpointFlow(input ExecuteEndpointFlowInput) EndpointFlowResult {
	endpointCount := len(input.EndpointCandidates)
	if endpointCount <= 0 {
		return EndpointFlowResult{
			OK:      false,
			Status:  502,
			ErrText: "Upstream request failed",
		}
	}

	finalStatus := 0
	finalErrText := "unknown error"
	var finalRawErrText string

	for endpointIndex := 0; endpointIndex < endpointCount; endpointIndex++ {
		endpoint := input.EndpointCandidates[endpointIndex]
		request := input.BuildRequest(endpoint, endpointIndex)
		defaultTarget := BuildUpstreamURL(input.SiteURL, request.Path)
		targetURL := defaultTarget
		if input.ProxyURL != "" {
			targetURL = BuildUpstreamURL(input.ProxyURL, request.Path)
		}

		var response *ExecutorDispatchResult
		var err error
		if input.DispatchRequest != nil {
			response, err = input.DispatchRequest(request, targetURL, input.FirstByteTimeoutMs)
		}
		if err != nil {
			response = &ExecutorDispatchResult{
				Status: 0,
				Body:   []byte(err.Error()),
			}
		}

		// Success
		if response != nil && response.Status >= 200 && response.Status < 300 {
			runHook(func() {
				if input.OnAttemptSuccess != nil {
					input.OnAttemptSuccess(EndpointAttemptSuccessContext{
						EndpointIndex:  endpointIndex,
						EndpointCount:  endpointCount,
						Request:        request,
						TargetURL:      targetURL,
						Response:       response,
						RecoverApplied: false,
					})
				}
			})
			return EndpointFlowResult{
				OK:           true,
				Response:     response,
				UpstreamPath: request.Path,
			}
		}

		// Failure processing
		rawErrText := ""
		if response != nil {
			rawErrText = string(response.Body)
		}
		baseCtx := EndpointAttemptContext{
			EndpointIndex:  endpointIndex,
			EndpointCount:  endpointCount,
			Request:        request,
			TargetURL:      targetURL,
			Response:       response,
			RawErrText:     rawErrText,
			RecoverApplied: false,
		}
		isLastEndpoint := endpointIndex >= endpointCount-1

		// First-byte timeout (status 0) + not last endpoint -> cross-protocol fallback
		if IsObservedFirstByteTimeout(response) && !isLastEndpoint {
			errText := strings.TrimSpace(rawErrText)
			if errText == "" {
				errText = "first byte timeout"
			}
			timeoutCtx := baseCtx
			timeoutCtx.ErrText = errText
			runHook(func() {
				if input.OnAttemptFailure != nil {
					input.OnAttemptFailure(timeoutCtx)
				}
			})
			finalStatus = 408
			finalErrText = errText
			finalRawErrText = rawErrText
			if input.DisableCrossProtocolFallback {
				break
			}
			continue
		}

		// Try recovery (OAuth refresh, etc.)
		if input.TryRecover != nil {
			recovered := input.TryRecover(&baseCtx)
			baseCtx.RecoverApplied = recovered != nil
			if recovered != nil && recovered.Response != nil &&
				recovered.Response.Status >= 200 && recovered.Response.Status < 300 {
				recoveredRequest := baseCtx.Request
				recoveredTargetURL := baseCtx.TargetURL
				if recovered.Request != nil {
					recoveredRequest = *recovered.Request
				}
				if recovered.TargetURL != "" {
					recoveredTargetURL = recovered.TargetURL
				} else {
					recoveredTargetURL = BuildUpstreamURL(input.SiteURL, recovered.UpstreamPath)
				}
				runHook(func() {
					if input.OnAttemptSuccess != nil {
						input.OnAttemptSuccess(EndpointAttemptSuccessContext{
							EndpointIndex:  endpointIndex,
							EndpointCount:  endpointCount,
							Request:        recoveredRequest,
							TargetURL:      recoveredTargetURL,
							Response:       recovered.Response,
							RecoverApplied: true,
						})
					}
				})
				return EndpointFlowResult{
					OK:           true,
					Response:     recovered.Response,
					UpstreamPath: recovered.UpstreamPath,
				}
			}
			// Re-read context after recovery failure — recovery may have mutated
			// RawErrText, Request, Response, or TargetURL via the pointer.
			rawErrText = baseCtx.RawErrText
			response = baseCtx.Response
		}

		// After recovery (may have mutated context)
		errText := WithUpstreamPath(
			baseCtx.Request.Path,
			SummarizeUpstreamError(response.Status, baseCtx.RawErrText),
		)
		failureCtx := baseCtx
		failureCtx.ErrText = errText
		runHook(func() {
			if input.OnAttemptFailure != nil {
				input.OnAttemptFailure(failureCtx)
			}
		})

		if input.DisableCrossProtocolFallback && !isLastEndpoint {
			finalStatus = response.Status
			finalErrText = errText
			finalRawErrText = baseCtx.RawErrText
			break
		}

		// Abort remaining endpoints
		if !isLastEndpoint && input.ShouldAbortRemainingEndpoints != nil {
			abortCtx := baseCtx
			abortCtx.ErrText = errText
			if input.ShouldAbortRemainingEndpoints(abortCtx) {
				finalStatus = response.Status
				finalErrText = errText
				finalRawErrText = baseCtx.RawErrText
				break
			}
		}

		// Downgrade
		if !isLastEndpoint && input.ShouldDowngrade != nil {
			if input.ShouldDowngrade(baseCtx) {
				runHook(func() {
					if input.OnDowngrade != nil {
						downgradeCtx := baseCtx
						downgradeCtx.ErrText = errText
						input.OnDowngrade(downgradeCtx)
					}
				})
				continue
			}
		}

		finalStatus = response.Status
		finalErrText = errText
		finalRawErrText = baseCtx.RawErrText
		break
	}

	if finalStatus == 0 {
		finalStatus = 502
	}
	if finalErrText == "" {
		finalErrText = "unknown error"
	}
	return EndpointFlowResult{
		OK:         false,
		Status:     finalStatus,
		ErrText:    finalErrText,
		RawErrText: finalRawErrText,
	}
}

func runHook(fn func()) {
	if fn != nil {
		fn()
	}
}
