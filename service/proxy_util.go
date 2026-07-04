package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProxyAwareHTTPClient creates an *http.Client that optionally routes through a proxy.
// If proxyURL is empty, returns a standard client.
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
		Transport: transport,
		Timeout:   timeout,
	}
}

// HTTPGet performs a GET request, optionally through a proxy.
func HTTPGet(proxyURL, requestURL string, headers map[string]string) (*http.Response, error) {
	client := ProxyAwareHTTPClient(proxyURL, 30*time.Second)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

// HTTPPost performs a POST request with string body, optionally through a proxy.
func HTTPPost(proxyURL, requestURL, contentType string, body io.Reader, headers map[string]string) (*http.Response, error) {
	client := ProxyAwareHTTPClient(proxyURL, 30*time.Second)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, requestURL, body)
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
func HTTPPostJSON(proxyURL, requestURL, jsonBody string, headers map[string]string) (*http.Response, error) {
	return HTTPPost(proxyURL, requestURL, "application/json", strings.NewReader(jsonBody), headers)
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
