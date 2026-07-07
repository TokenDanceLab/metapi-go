package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWithObservedFirstByteReturnsTransportErrors(t *testing.T) {
	wantErr := errors.New("connection refused")
	executor := &RuntimeExecutor{client: &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	}}

	result, _, err := executor.WithObservedFirstByte(context.Background(), ExecutorDispatchInput{
		Method:    http.MethodGet,
		TargetURL: "http://example.invalid/v1/chat/completions",
	}, 1000)

	if err == nil {
		t.Fatalf("WithObservedFirstByte returned nil error and result=%+v, want transport error", result)
	}
	if IsObservedFirstByteTimeout(result) {
		t.Fatalf("transport error was classified as first-byte timeout: %+v", result)
	}
}

func TestWithObservedFirstByteReturnsTimeoutMarkerOnDeadline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	executor := NewRuntimeExecutor(time.Second)
	result, _, err := executor.WithObservedFirstByte(context.Background(), ExecutorDispatchInput{
		Method:    http.MethodGet,
		TargetURL: upstream.URL,
	}, 5)

	if err != nil {
		t.Fatalf("WithObservedFirstByte returned error for first-byte timeout: %v", err)
	}
	if !IsObservedFirstByteTimeout(result) {
		t.Fatalf("result = %+v, want first-byte timeout marker", result)
	}
}

func TestDispatchRejectsOversizedBufferedResponse(t *testing.T) {
	t.Setenv("PROXY_MAX_BUFFERED_RESPONSE_BYTES", "8")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "123456789")
	}))
	t.Cleanup(upstream.Close)

	executor := NewRuntimeExecutor(time.Second)
	result, err := executor.Dispatch(context.Background(), ExecutorDispatchInput{
		Method:    http.MethodGet,
		TargetURL: upstream.URL,
	})

	if err == nil {
		t.Fatalf("Dispatch returned result=%+v, want oversized response error", result)
	}
	if !strings.Contains(err.Error(), "response body exceeded") {
		t.Fatalf("error = %v, want response body exceeded", err)
	}
}

func TestWithObservedFirstByteRejectsOversizedBufferedResponse(t *testing.T) {
	t.Setenv("PROXY_MAX_BUFFERED_RESPONSE_BYTES", "8")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "123456789")
	}))
	t.Cleanup(upstream.Close)

	executor := NewRuntimeExecutor(time.Second)
	result, _, err := executor.WithObservedFirstByte(context.Background(), ExecutorDispatchInput{
		Method:    http.MethodGet,
		TargetURL: upstream.URL,
	}, 1000)

	if err == nil {
		t.Fatalf("WithObservedFirstByte returned result=%+v, want oversized response error", result)
	}
	if !strings.Contains(err.Error(), "response body exceeded") {
		t.Fatalf("error = %v, want response body exceeded", err)
	}
}
