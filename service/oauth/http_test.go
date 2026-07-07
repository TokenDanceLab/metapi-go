package oauth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadOAuthJSONResponseBodyAllowsLimit(t *testing.T) {
	body := strings.Repeat("x", oauthJSONResponseBodyLimit)

	got, err := readOAuthJSONResponseBody(strings.NewReader(body))
	if err != nil {
		t.Fatalf("readOAuthJSONResponseBody: %v", err)
	}
	if len(got) != oauthJSONResponseBodyLimit {
		t.Fatalf("len = %d, want %d", len(got), oauthJSONResponseBodyLimit)
	}
}

func TestReadOAuthJSONResponseBodyRejectsOversized(t *testing.T) {
	body := strings.Repeat("x", oauthJSONResponseBodyLimit+1)

	_, err := readOAuthJSONResponseBody(strings.NewReader(body))
	if err == nil {
		t.Fatal("readOAuthJSONResponseBody succeeded for oversized response")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want exceeds", err)
	}
}

func TestReadOAuthErrorResponseBodyCapsOutput(t *testing.T) {
	body := bytes.Repeat([]byte("e"), oauthErrorResponseBodyLimit+1024)

	got := readOAuthErrorResponseBody(bytes.NewReader(body))
	if len(got) != oauthErrorResponseBodyLimit {
		t.Fatalf("len = %d, want %d", len(got), oauthErrorResponseBodyLimit)
	}
	if !bytes.Equal(got, bytes.Repeat([]byte("e"), oauthErrorResponseBodyLimit)) {
		t.Fatal("error response body was not capped to the leading bytes")
	}
}

func TestDoHTTPRejectsInvalidProxyURLWithoutRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	proxyURL := "://bad"

	_, err = doHTTP(req, &proxyURL, nil)
	if err == nil {
		t.Fatal("doHTTP succeeded, want invalid proxy URL error")
	}
	if called {
		t.Fatal("request was sent despite invalid proxy URL")
	}
}

func TestDoHTTPIgnoresEnvironmentProxyWithoutProxyURL(t *testing.T) {
	proxyCalled := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(proxy.Close)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/oauth", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	_, _ = doHTTP(req, nil, nil)

	if proxyCalled {
		t.Fatal("doHTTP without proxy URL used HTTP_PROXY from environment")
	}
}
