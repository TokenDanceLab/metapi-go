package proxy

import (
	"strings"

	"github.com/tokendancelab/metapi-go/platform"
)

// KeyProxyPrecedence documents the egress proxy selection order for
// FE-KEY-PROXY / upstream #578:
//
//	1. downstream_api_keys.proxy_url  (per-key override; this package)
//	2. account extraConfig.proxyUrl / useSystemProxy
//	3. sites.proxy_url / sites.use_system_proxy
//	4. config.SystemProxyUrl when site/account opt into system proxy
//	5. direct (no proxy)
//
// Product intent: multi-tenant downstream keys may need distinct egress even
// when they share the same upstream site/account channel. A non-empty key
// proxy therefore wins over site and account proxies. NULL / empty key proxy
// preserves pre-SC2 behavior (inherit site/account/system).
const KeyProxyPrecedence = "key > account > site > system > direct"

// ApplyKeyProxyOverride returns a ProxyConfig with ProxyURL replaced by the
// per-key egress proxy when keyProxyURL is non-empty after trim.
//
// base may be nil (no site/account proxy). Custom headers and other fields
// from base are preserved so site custom headers still apply under a key
// proxy override.
//
// Empty / whitespace keyProxyURL returns base unchanged (inherit).
func ApplyKeyProxyOverride(base *platform.ProxyConfig, keyProxyURL *string) *platform.ProxyConfig {
	if keyProxyURL == nil {
		return base
	}
	trimmed := strings.TrimSpace(*keyProxyURL)
	if trimmed == "" {
		return base
	}

	if base == nil {
		return &platform.ProxyConfig{ProxyURL: trimmed}
	}

	// Clone so callers can share base configs safely.
	out := *base
	out.ProxyURL = trimmed
	// Explicit key proxy is not the system proxy path.
	out.UseSystemProxy = false
	if base.CustomHeaders != nil {
		headers := make(map[string]string, len(base.CustomHeaders))
		for k, v := range base.CustomHeaders {
			headers[k] = v
		}
		out.CustomHeaders = headers
	}
	return &out
}

// ResolveKeyProxyURL normalizes a nullable proxy URL pointer.
// Whitespace-only and empty values become nil (inherit).
func ResolveKeyProxyURL(proxyURL *string) *string {
	if proxyURL == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*proxyURL)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
