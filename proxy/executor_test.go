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

func TestFirstByteTimeoutMsSecToMs(t *testing.T) {
	tests := []struct {
		sec  int
		want int64
	}{
		{0, 0},
		{-1, 0},
		{1, 1000},
		{5, 5000},
		{90, 90000},
	}
	for _, tt := range tests {
		if got := FirstByteTimeoutMs(tt.sec); got != tt.want {
			t.Errorf("FirstByteTimeoutMs(%d) = %d, want %d", tt.sec, got, tt.want)
		}
	}
}

func TestDoWithObservedFirstByteTimeoutError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	executor := NewRuntimeExecutor(time.Second)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := executor.DoWithObservedFirstByte(context.Background(), req, 5)
	if resp != nil {
		resp.Body.Close()
		t.Fatalf("expected nil response on first-byte timeout")
	}
	if !IsObservedFirstByteTimeoutError(err) {
		t.Fatalf("err = %v, want ErrObservedFirstByteTimeout", err)
	}
}

func TestDoWithObservedFirstByteDoesNotCancelBodyAfterHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(40 * time.Millisecond)
		_, _ = w.Write([]byte("body-after-headers"))
	}))
	t.Cleanup(upstream.Close)

	executor := NewRuntimeExecutor(time.Second)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// first-byte window is short but headers arrive immediately; body should still complete.
	resp, err := executor.DoWithObservedFirstByte(context.Background(), req, 10)
	if err != nil {
		t.Fatalf("DoWithObservedFirstByte: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "body-after-headers" {
		t.Fatalf("body = %q, want body-after-headers", body)
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

func TestRuntimeExecutorRejectsCrossOriginRedirect(t *testing.T) {
	// SSRF surface: 302 to a different host (e.g. metadata/loopback) must not be followed.
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ssrf-payload")
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/metadata", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	executor := NewRuntimeExecutor(time.Second)
	result, err := executor.Dispatch(context.Background(), ExecutorDispatchInput{
		Method:    http.MethodGet,
		TargetURL: source.URL + "/start",
	})
	if err == nil {
		t.Fatalf("Dispatch allowed cross-origin redirect: result=%+v", result)
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin redirect rejection", err)
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was called (SSRF)")
	}
}

func TestRuntimeExecutorAllowsSameHostRedirect(t *testing.T) {
	// Mirror platform policy: same-host redirects remain allowed.
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/landing", http.StatusFound)
	})
	mux.HandleFunc("/landing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "same-host-ok")
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	executor := NewRuntimeExecutor(time.Second)
	result, err := executor.Dispatch(context.Background(), ExecutorDispatchInput{
		Method:    http.MethodGet,
		TargetURL: upstream.URL + "/start",
	})
	if err != nil {
		t.Fatalf("same-host redirect Dispatch: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	if string(result.Body) != "same-host-ok" {
		t.Fatalf("body = %q, want same-host-ok", result.Body)
	}
}

func TestRejectCrossOriginRedirectPolicy(t *testing.T) {
	viaHTTPS, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1", nil)
	if err != nil {
		t.Fatalf("NewRequest via: %v", err)
	}
	toHTTP, err := http.NewRequest(http.MethodGet, "http://api.example.com/v1", nil)
	if err != nil {
		t.Fatalf("NewRequest to: %v", err)
	}
	if err = rejectCrossOriginRedirect(toHTTP, []*http.Request{viaHTTPS}); err == nil {
		t.Fatal("expected https→http redirect rejection")
	}

	toOtherHost, err := http.NewRequest(http.MethodGet, "https://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatalf("NewRequest metadata: %v", err)
	}
	if err = rejectCrossOriginRedirect(toOtherHost, []*http.Request{viaHTTPS}); err == nil {
		t.Fatal("expected cross-origin metadata redirect rejection")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin", err)
	}

	sameHost, err := http.NewRequest(http.MethodGet, "https://api.example.com/v2", nil)
	if err != nil {
		t.Fatalf("NewRequest same: %v", err)
	}
	if err = rejectCrossOriginRedirect(sameHost, []*http.Request{viaHTTPS}); err != nil {
		t.Fatalf("same-host https redirect should be allowed: %v", err)
	}
}
