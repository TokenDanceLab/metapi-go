package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ExecutorDispatchInput is the input for dispatching an HTTP request.
type ExecutorDispatchInput struct {
	SiteURL   string
	TargetURL string
	Method    string
	Headers   map[string]string
	Body      []byte
	Signal    <-chan struct{}
}

// ExecutorDispatchResult is the result of dispatching an HTTP request.
type ExecutorDispatchResult struct {
	Status     int
	Headers    map[string]string
	Body       []byte
	BodyReader io.ReadCloser
}

// RuntimeExecutor dispatches upstream HTTP requests.
type RuntimeExecutor struct {
	client *http.Client
}

// NewRuntimeExecutor creates a new RuntimeExecutor with the given timeout.
func NewRuntimeExecutor(requestTimeout time.Duration) *RuntimeExecutor {
	return &RuntimeExecutor{
		client: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

// Do sends an HTTP request through the executor's client, returning the raw
// *http.Response. Unlike Dispatch, this does NOT read the body — callers
// (especially streaming handlers) must close resp.Body themselves.
func (e *RuntimeExecutor) Do(req *http.Request) (*http.Response, error) {
	return e.client.Do(req)
}

// Dispatch sends an HTTP request to the upstream.
func (e *RuntimeExecutor) Dispatch(ctx context.Context, input ExecutorDispatchInput) (*ExecutorDispatchResult, error) {
	var bodyReader io.Reader
	if len(input.Body) > 0 {
		bodyReader = bytes.NewReader(input.Body)
	}

	req, err := http.NewRequestWithContext(ctx, input.Method, input.TargetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("dispatch: %w", err)
	}

	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dispatch: %w", err)
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[strings.ToLower(k)] = resp.Header.Get(k)
	}

	// Read body for non-streaming responses.
	body, err := ReadBufferedResponseBody(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("dispatch read body: %w", err)
	}

	return &ExecutorDispatchResult{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    body,
	}, nil
}

// WithObservedFirstByte dispatches a request and observes the first-byte latency.
// Returns a special result with status=0 if the first-byte timeout fires.
func (e *RuntimeExecutor) WithObservedFirstByte(
	ctx context.Context,
	input ExecutorDispatchInput,
	firstByteTimeoutMs int64,
) (*ExecutorDispatchResult, int64, error) {
	startedAt := time.Now()

	var bodyReader io.Reader
	if len(input.Body) > 0 {
		bodyReader = bytes.NewReader(input.Body)
	}

	req, err := http.NewRequestWithContext(ctx, input.Method, input.TargetURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("dispatch: %w", err)
	}

	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	timeoutCtx := ctx
	if firstByteTimeoutMs > 0 {
		var cancel context.CancelFunc
		timeoutCtx, cancel = context.WithTimeout(ctx, time.Duration(firstByteTimeoutMs)*time.Millisecond)
		defer cancel()
		req = req.WithContext(timeoutCtx)
	}

	resp, err := e.client.Do(req)
	firstByteLatencyMs := time.Since(startedAt).Milliseconds()

	if err != nil {
		// Check if this was a first-byte timeout
		if firstByteTimeoutMs > 0 && errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return &ExecutorDispatchResult{
				Status:  0, // timeout marker
				Headers: map[string]string{},
				Body:    nil,
			}, firstByteLatencyMs, nil
		}
		return nil, firstByteLatencyMs, fmt.Errorf("dispatch: %w", err)
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[strings.ToLower(k)] = resp.Header.Get(k)
	}

	body, err := ReadBufferedResponseBody(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, firstByteLatencyMs, fmt.Errorf("dispatch read body: %w", err)
	}

	return &ExecutorDispatchResult{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    body,
	}, firstByteLatencyMs, nil
}

// IsObservedFirstByteTimeout checks if a result represents a first-byte timeout.
func IsObservedFirstByteTimeout(result *ExecutorDispatchResult) bool {
	return result != nil && result.Status == 0
}
