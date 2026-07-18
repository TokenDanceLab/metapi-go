package platform

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultProxyConnectTimeout   = 10 * time.Second
	defaultProxyKeepAliveInitial = 60 * time.Second
	siteProxyCacheTTL            = 3 * time.Second
)

var supportedProxySchemes = map[string]bool{
	"http": true, "https": true,
	"socks": true, "socks4": true, "socks4a": true,
	"socks5": true, "socks5h": true,
}

// SiteProxyConfig holds proxy configuration for a site.
type SiteProxyConfig struct {
	ProxyURL       string
	UseSystemProxy bool
	CustomHeaders  map[string]string
}

// SiteProxy is the outbound HTTP client with SOCKS/HTTP proxy support.
type SiteProxy struct {
	systemProxyURL  string
	siteConfigs     map[string]*SiteProxyConfig
	lastLoad        time.Time
	httpClient      *http.Client
	httpClientNoTLS *http.Client
}

// NewSiteProxy creates a new SiteProxy.
func NewSiteProxy(systemProxyURL string) *SiteProxy {
	sp := &SiteProxy{
		systemProxyURL: systemProxyURL,
		siteConfigs:    make(map[string]*SiteProxyConfig),
	}
	sp.buildClients()
	return sp
}

func (sp *SiteProxy) buildClients() {
	transport := &http.Transport{
		Proxy: sp.proxyFunc,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: defaultProxyKeepAliveInitial,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	sp.httpClient = &http.Client{
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: RejectCrossOriginRedirect,
	}

	transportNoTLS := &http.Transport{
		Proxy: sp.proxyFunc,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: defaultProxyKeepAliveInitial,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	}

	sp.httpClientNoTLS = &http.Client{
		Transport:     transportNoTLS,
		Timeout:       30 * time.Second,
		CheckRedirect: RejectCrossOriginRedirect,
	}
}

func (sp *SiteProxy) proxyFunc(req *http.Request) (*url.URL, error) {
	if sp.systemProxyURL != "" {
		return url.Parse(sp.systemProxyURL)
	}
	return nil, nil
}

// Do executes an HTTP request through the site proxy layer.
func (sp *SiteProxy) Do(ctx context.Context, req *http.Request, proxyConfig *ProxyConfig) (*http.Response, error) {
	req = req.WithContext(ctx)

	// Apply custom headers from proxy config (deny-list filters identity/hop-by-hop).
	if proxyConfig != nil {
		ApplyCustomHeaders(req, proxyConfig.CustomHeaders)
	}

	// If specific proxy URL is given, use a dedicated transport
	if proxyConfig != nil && proxyConfig.ProxyURL != "" {
		return sp.doWithExplicitProxy(ctx, req, proxyConfig)
	}

	// Use default client
	client := sp.httpClient
	if proxyConfig != nil && proxyConfig.InsecureSkipTLS {
		client = sp.httpClientNoTLS
	}
	return client.Do(req)
}

func (sp *SiteProxy) doWithExplicitProxy(ctx context.Context, req *http.Request, proxyConfig *ProxyConfig) (*http.Response, error) {
	proxyURL, err := url.Parse(proxyConfig.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	scheme := strings.ToLower(proxyURL.Scheme)
	if !supportedProxySchemes[scheme] {
		return nil, fmt.Errorf("unsupported proxy scheme: %s", scheme)
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: (&net.Dialer{
			Timeout:   defaultProxyConnectTimeout,
			KeepAlive: defaultProxyKeepAliveInitial,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	if proxyConfig.InsecureSkipTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	client := &http.Client{
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: RejectCrossOriginRedirect,
	}

	return client.Do(req)
}

// DoWithProxy is a convenience function that works without a SiteProxy instance.
func DoWithProxy(ctx context.Context, req *http.Request, proxyConfig *ProxyConfig) (*http.Response, error) {
	if proxyConfig != nil {
		// Deny-list sensitive / hop-by-hop / metapi-control headers (#356).
		ApplyCustomHeaders(req, proxyConfig.CustomHeaders)
	}

	insecureSkipTLS := proxyConfig != nil && proxyConfig.InsecureSkipTLS
	if proxyConfig != nil && proxyConfig.ProxyURL != "" {
		proxyURL, err := url.Parse(proxyConfig.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		scheme := strings.ToLower(proxyURL.Scheme)
		if !supportedProxySchemes[scheme] {
			return nil, fmt.Errorf("unsupported proxy scheme: %s", scheme)
		}

		client := newProxyHTTPClient(http.ProxyURL(proxyURL), insecureSkipTLS)
		return client.Do(req.WithContext(ctx))
	}

	client := newProxyHTTPClient(nil, insecureSkipTLS)
	return client.Do(req.WithContext(ctx))
}

func newProxyHTTPClient(proxy func(*http.Request) (*url.URL, error), insecureSkipTLS bool) *http.Client {
	transport := &http.Transport{
		Proxy: proxy,
		DialContext: (&net.Dialer{
			Timeout:   defaultProxyConnectTimeout,
			KeepAlive: defaultProxyKeepAliveInitial,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	if insecureSkipTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: RejectCrossOriginRedirect,
	}
}

// RejectCrossOriginRedirect is the shared CheckRedirect policy for outbound
// HTTP clients that talk to operator-configured upstream sites.
//
// It refuses:
//   - more than 5 redirects
//   - https → non-https scheme downgrades
//   - any host change (blocks 302 to metadata / loopback / private SSRF targets)
//
// Same-origin redirects remain allowed so normal upstream path hops still work.
// Used by platform.DoWithProxy, proxy.RuntimeExecutor, and residual bare clients
// (health probe / admin harness / defaultUpstreamClient).
func RejectCrossOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	if len(via) == 0 {
		return nil
	}
	previous := via[len(via)-1].URL
	if previous.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
	}
	if !strings.EqualFold(previous.Host, req.URL.Host) {
		return fmt.Errorf("refusing cross-origin redirect from %s to %s", previous.Host, req.URL.Host)
	}
	return nil
}

// WithTimeout creates a context with timeout for quick probes.
func withProbeTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}
