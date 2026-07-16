package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
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

// ErrObservedFirstByteTimeout is returned by DoWithObservedFirstByte when the
// first-byte / response-header deadline fires before headers arrive.
//
// Unit note: firstByteTimeoutMs is milliseconds. Config field
// PROXY_FIRST_BYTE_TIMEOUT_SEC is seconds; convert with FirstByteTimeoutMs.
var ErrObservedFirstByteTimeout = errors.New("first byte timeout")

// FirstByteTimeoutMs converts ProxyFirstByteTimeoutSec (seconds) to the
// internal first-byte observation unit (milliseconds). Values <= 0 disable
// first-byte observation (returns 0).
//
// Matches original TS: firstByteTimeoutMs = proxyFirstByteTimeoutSec * 1000.
func FirstByteTimeoutMs(proxyFirstByteTimeoutSec int) int64 {
	if proxyFirstByteTimeoutSec <= 0 {
		return 0
	}
	return int64(proxyFirstByteTimeoutSec) * 1000
}

// DoWithObservedFirstByte sends req and observes first-byte (response header)
// latency. Unlike WithObservedFirstByte it does NOT buffer the body, so callers
// (including streaming handlers) must close resp.Body themselves.
//
// When firstByteTimeoutMs > 0 and the deadline fires before headers arrive,
// it returns (nil, ErrObservedFirstByteTimeout).
// firstByteTimeoutMs is milliseconds (see FirstByteTimeoutMs).
//
// After headers arrive the first-byte timer is stopped so the body stream is not
// cancelled by the observation deadline.
func (e *RuntimeExecutor) DoWithObservedFirstByte(
	ctx context.Context,
	req *http.Request,
	firstByteTimeoutMs int64,
) (*http.Response, error) {
	if e == nil || e.client == nil {
		return nil, fmt.Errorf("dispatch: executor is not configured")
	}
	if firstByteTimeoutMs <= 0 {
		return e.client.Do(req)
	}

	reqCtx, cancelReq := context.WithCancel(ctx)
	req = req.WithContext(reqCtx)

	var timedOut atomic.Bool
	timer := time.AfterFunc(time.Duration(firstByteTimeoutMs)*time.Millisecond, func() {
		timedOut.Store(true)
		cancelReq()
	})

	resp, err := e.client.Do(req)
	if err != nil {
		_ = timer.Stop()
		cancelReq()
		if timedOut.Load() && ctx.Err() == nil {
			return nil, ErrObservedFirstByteTimeout
		}
		return nil, err
	}

	// Headers received: stop first-byte timer. Keep reqCtx alive for body reads;
	// cancel when the body is closed.
	_ = timer.Stop()
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancelReq}
	return resp, nil
}

// cancelOnCloseBody cancels the request context when the response body is closed.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
	}
	return err
}

// WithObservedFirstByte dispatches a request and observes the first-byte latency.
// Returns a special result with status=0 if the first-byte timeout fires.
// firstByteTimeoutMs is milliseconds (see FirstByteTimeoutMs).
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

	resp, err := e.DoWithObservedFirstByte(ctx, req, firstByteTimeoutMs)
	firstByteLatencyMs := time.Since(startedAt).Milliseconds()

	if err != nil {
		// Check if this was a first-byte timeout
		if errors.Is(err, ErrObservedFirstByteTimeout) {
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

// IsObservedFirstByteTimeoutError reports whether err is a first-byte timeout.
func IsObservedFirstByteTimeoutError(err error) bool {
	return errors.Is(err, ErrObservedFirstByteTimeout)
}
