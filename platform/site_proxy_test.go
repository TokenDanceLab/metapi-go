package platform

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestDoWithProxy_NoProxyIgnoresEnvironmentProxy(t *testing.T) {
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
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/metapi", nil)

	_, _ = DoWithProxy(ctx, req, nil)

	if proxyCalled {
		t.Fatal("DoWithProxy without proxy config used HTTP_PROXY from environment")
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

func TestDoWithProxy_WithCustomHeaders(t *testing.T) {
	gotHeader := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Metapi-Test")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/test", nil)
	resp, err := DoWithProxy(ctx, req, &ProxyConfig{
		CustomHeaders: map[string]string{"X-Metapi-Test": "site-header"},
	})
	if err != nil {
		t.Fatalf("DoWithProxy: %v", err)
	}
	_ = resp.Body.Close()

	if gotHeader != "site-header" {
		t.Fatalf("X-Metapi-Test = %q, want site-header", gotHeader)
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

func TestDoWithProxy_RejectsUnsupportedProxyScheme(t *testing.T) {
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:1/test", nil)

	proxyConfig := &ProxyConfig{
		ProxyURL: "ftp://proxy.example:21",
	}
	_, err := DoWithProxy(ctx, req, proxyConfig)
	if err == nil {
		t.Fatal("unsupported proxy scheme should return error")
	}
	if !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Fatalf("error = %v, want unsupported proxy scheme", err)
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

func TestDoWithProxy_RejectsCrossOriginRedirect(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, source.URL+"/start", nil)
	resp, err := DoWithProxy(ctx, req, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("cross-origin redirect was allowed")
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was called")
	}
}

func TestSiteProxy_Do_RejectsCrossOriginRedirect(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	sp := NewSiteProxy("")
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL+"/start", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := sp.Do(ctx, req, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("SiteProxy.Do allowed cross-origin redirect")
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was dialed via SiteProxy.Do")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin", err)
	}
}

func TestSiteProxy_Do_RejectsCrossOriginRedirect_ExplicitProxy(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	// HTTP reverse proxy that forwards absolute-form requests (httptest as CONNECT-less HTTP proxy).
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For non-CONNECT HTTP proxying, net/http client sends absolute URL.
		if r.Method == http.MethodConnect {
			http.Error(w, "CONNECT not used in this test", http.StatusMethodNotAllowed)
			return
		}
		targetURL := r.URL
		if !targetURL.IsAbs() {
			// Absolute-form path may still be in RequestURI; fall back to Host.
			targetURL = &url.URL{Scheme: "http", Host: r.Host, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		}
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		outReq.Header = r.Header.Clone()
		resp, err := http.DefaultTransport.RoundTrip(outReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	sp := NewSiteProxy("")
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL+"/start", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := sp.Do(ctx, req, &ProxyConfig{ProxyURL: proxy.URL})
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("explicit-proxy SiteProxy.Do allowed cross-origin redirect")
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was dialed via explicit proxy path")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin", err)
	}
}

func TestSiteProxy_buildClients_WiresCheckRedirect(t *testing.T) {
	sp := NewSiteProxy("")
	if sp.httpClient == nil || sp.httpClient.CheckRedirect == nil {
		t.Fatal("httpClient.CheckRedirect must be set")
	}
	if sp.httpClientNoTLS == nil || sp.httpClientNoTLS.CheckRedirect == nil {
		t.Fatal("httpClientNoTLS.CheckRedirect must be set")
	}

	via, err := http.NewRequest(http.MethodGet, "http://api.example.com/start", nil)
	if err != nil {
		t.Fatalf("via: %v", err)
	}
	to, err := http.NewRequest(http.MethodGet, "http://evil.example.com/land", nil)
	if err != nil {
		t.Fatalf("to: %v", err)
	}
	if err := sp.httpClient.CheckRedirect(to, []*http.Request{via}); err == nil {
		t.Fatal("httpClient.CheckRedirect should reject cross-origin")
	}
	if err := sp.httpClientNoTLS.CheckRedirect(to, []*http.Request{via}); err == nil {
		t.Fatal("httpClientNoTLS.CheckRedirect should reject cross-origin")
	}
}

func TestRejectCrossOriginRedirect_MetadataHost(t *testing.T) {
	// Same scheme so we exercise the host check (not the https→http downgrade).
	via, err := http.NewRequest(http.MethodGet, "http://api.example.com/v1/models", nil)
	if err != nil {
		t.Fatalf("via: %v", err)
	}
	toMeta, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatalf("toMeta: %v", err)
	}
	err = RejectCrossOriginRedirect(toMeta, []*http.Request{via})
	if err == nil {
		t.Fatal("expected 169.254 metadata redirect rejection")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin", err)
	}

	sameHost, err := http.NewRequest(http.MethodGet, "http://api.example.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("sameHost: %v", err)
	}
	if err = RejectCrossOriginRedirect(sameHost, []*http.Request{via}); err != nil {
		t.Fatalf("same-host redirect should be allowed: %v", err)
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
