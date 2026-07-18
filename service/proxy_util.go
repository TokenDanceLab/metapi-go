package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/platform"
)

// ProxyAwareHTTPClient creates an *http.Client that optionally routes through a proxy.
// If proxyURL is empty, returns a standard client.
//
// The client always wires platform.RejectCrossOriginRedirect so HTTPGet/HTTPPost
// callers (and Telegram when it reuses this helper) cannot follow a public-origin
// 302 onto a different host.
func ProxyAwareHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
	}

	if proxyURL != "" {
		proxyURL = strings.TrimSpace(proxyURL)
		parsed, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}

	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: platform.RejectCrossOriginRedirect,
	}
}

// HTTPGet performs a GET request, optionally through a proxy.
func HTTPGet(ctx context.Context, proxyURL, requestURL string, headers map[string]string) (*http.Response, error) {
	client := ProxyAwareHTTPClient(proxyURL, 30*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

// HTTPPost performs a POST request with string body, optionally through a proxy.
func HTTPPost(ctx context.Context, proxyURL, requestURL, contentType string, body io.Reader, headers map[string]string) (*http.Response, error) {
	client := ProxyAwareHTTPClient(proxyURL, 30*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

// HTTPPostJSON posts a JSON string body.
func HTTPPostJSON(ctx context.Context, proxyURL, requestURL, jsonBody string, headers map[string]string) (*http.Response, error) {
	return HTTPPost(ctx, proxyURL, requestURL, "application/json", strings.NewReader(jsonBody), headers)
}

// WithAccountProxyOverride wraps a function that uses HTTP with proxy from extraConfig.
// This is a convenience wrapper. In P5, most adapter calls are direct —
// the proxy URI is resolved from extraConfig via GetProxyURLFromExtraConfig.
func WithAccountProxyOverride(proxyURL string, fn func() error) error {
	// The proxy is applied within the adapter call itself via HTTPPost/HTTPGet.
	// This function exists for API symmetry with TS. The actual proxy application
	// happens through HTTP helper functions above.
	return fn()
}

// ReadResponseBody reads and returns the full response body as string.
func ReadResponseBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
