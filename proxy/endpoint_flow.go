package proxy

import (
	"fmt"
	"strings"
)

// UpstreamEndpoint represents an upstream API endpoint type.
type UpstreamEndpoint string

const (
	EndpointChat      UpstreamEndpoint = "chat"      // /v1/chat/completions
	EndpointMessages   UpstreamEndpoint = "messages"  // /v1/messages (Anthropic)
	EndpointResponses  UpstreamEndpoint = "responses" // /v1/responses (Codex)
)

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
	SiteURL                     string
	ProxyURL                    string
	DisableCrossProtocolFallback bool
	EndpointCandidates          []UpstreamEndpoint
	BuildRequest                func(endpoint UpstreamEndpoint, endpointIndex int) BuiltEndpointRequest
	DispatchRequest             func(request BuiltEndpointRequest, targetURL string, firstByteTimeoutMs int64) (*ExecutorDispatchResult, error)
	FirstByteTimeoutMs          int64
	TryRecover                  func(ctx *EndpointAttemptContext) *RecoverResult
	ShouldDowngrade             func(ctx EndpointAttemptContext) bool
	ShouldAbortRemainingEndpoints func(ctx EndpointAttemptContext) bool
	OnDowngrade                 func(ctx EndpointAttemptContext)
	OnAttemptFailure            func(ctx EndpointAttemptContext)
	OnAttemptSuccess            func(ctx EndpointAttemptSuccessContext)
}

// BuildUpstreamURL constructs an upstream URL from a site URL and path.
func BuildUpstreamURL(siteURL, path string) string {
	siteURL = strings.TrimRight(siteURL, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return siteURL + path
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
