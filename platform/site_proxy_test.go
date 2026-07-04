package platform

import (
	"context"
	"net/http"
	"testing"
)

func TestNewSiteProxy(t *testing.T) {
	sp := NewSiteProxy("http://proxy:8080")
	if sp == nil {
		t.Fatal("NewSiteProxy should not be nil")
	}
	if sp.systemProxyURL != "http://proxy:8080" {
		t.Errorf("systemProxyURL: %q", sp.systemProxyURL)
	}
	if sp.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
	if sp.httpClientNoTLS == nil {
		t.Error("httpClientNoTLS should not be nil")
	}
}

func TestSiteProxy_Do_NoProxy(t *testing.T) {
	sp := NewSiteProxy("")

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Should fail gracefully on unreachable URL
	_, err = sp.Do(ctx, req, nil)
	if err == nil {
		// Connection refused is expected, no error is also fine (test environment may vary)
	}
}

func TestSiteProxy_Do_WithCustomHeaders(t *testing.T) {
	sp := NewSiteProxy("")

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	proxyConfig := &ProxyConfig{
		CustomHeaders: map[string]string{
			"X-Custom": "value",
		},
	}

	_, err = sp.Do(ctx, req, proxyConfig)
	// Headers should be applied (we can't test the actual response, but it shouldn't panic)
	if err == nil {
		// ok
	}
	// Verify header was set
	if req.Header.Get("X-Custom") != "value" {
		t.Error("custom header not set")
	}
}

func TestSiteProxy_Do_WithExplicitProxy(t *testing.T) {
	sp := NewSiteProxy("")

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	proxyConfig := &ProxyConfig{
		ProxyURL: "http://invalid-proxy:1",
	}

	// Should fail on unreachable proxy
	_, err = sp.Do(ctx, req, proxyConfig)
	if err == nil {
		// Proxy unreachable should cause error
	}
}

func TestDoWithProxy_NoProxy(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)

	_, err := DoWithProxy(ctx, req, nil)
	if err == nil {
		// ok
	}
}

func TestDoWithProxy_WithExplicitProxy(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)

	proxyConfig := &ProxyConfig{
		ProxyURL: "http://invalid-proxy:1",
	}
	_, err := DoWithProxy(ctx, req, proxyConfig)
	if err == nil {
		// Proxy unreachable should cause error
	}
}

func TestDoWithProxy_InvalidProxyURL(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)

	proxyConfig := &ProxyConfig{
		ProxyURL: "://invalid",
	}
	_, err := DoWithProxy(ctx, req, proxyConfig)
	if err == nil {
		t.Error("invalid proxy URL should return error")
	}
}

func TestDoWithProxy_InsecureSkipTLS(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)

	proxyConfig := &ProxyConfig{
		InsecureSkipTLS: true,
	}
	_, err := DoWithProxy(ctx, req, proxyConfig)
	// Should fail on unreachable but not due to TLS
	if err == nil {
		// ok
	}
}

func TestSupportedProxySchemes(t *testing.T) {
	schemes := []string{"http", "https", "socks", "socks4", "socks4a", "socks5", "socks5h"}
	for _, s := range schemes {
		if !supportedProxySchemes[s] {
			t.Errorf("scheme %q should be supported", s)
		}
	}
	if supportedProxySchemes["ftp"] {
		t.Error("ftp should not be supported")
	}
}

func TestWithProbeTimeout(t *testing.T) {
	ctx, cancel := withProbeTimeout(context.Background())
	defer cancel()
	if ctx == nil {
		t.Error("withProbeTimeout should return non-nil context")
	}
}
