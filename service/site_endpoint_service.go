package service

import (
	"net"
	"net/url"
	"strings"
)

// NormalizeSiteAPIEndpointBaseUrl normalizes a site API endpoint URL.
// Mirrors TS normalizeSiteApiEndpointBaseUrl(): parse URL, strip search/hash, remove trailing slash.
func NormalizeSiteAPIEndpointBaseUrl(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.TrimRight(trimmed, "/")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

// IsValidAPIEndpointURL checks whether a normalized URL is a valid http(s) URL.
func IsValidAPIEndpointURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// IsValidProxyURL checks whether a string is a valid http(s)/socks proxy URL.
func IsValidProxyURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true // empty/null is valid (no proxy)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https", "socks", "socks5", "socks5h":
		return true
	}
	return false
}

// IsValidHTTPURL checks whether a string is a valid http/https URL (not socks).
// Empty is valid (optional field). Also rejects cloud metadata / link-local
// targets via IsForbiddenSiteTargetURL so externalCheckin and similar callers
// cannot store first-hop SSRF URLs (#382 / extends #376).
func IsValidHTTPURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if IsForbiddenSiteTargetURL(trimmed) {
		return false
	}
	return true
}

// IsForbiddenSiteTargetURL reports whether a site/endpoint URL must be rejected
// as a first-hop SSRF risk to cloud metadata / link-local targets (#376).
// RFC1918 private and localhost remain allowed for lab/docker operators.
func IsForbiddenSiteTargetURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	lower := strings.ToLower(host)
	switch lower {
	case "metadata.google.internal", "metadata", "instance-data":
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname: only block well-known metadata names above.
		return false
	}
	// Link-local IPv4 169.254.0.0/16 (includes AWS/GCP/Azure metadata 169.254.169.254).
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		return false
	}
	// Link-local IPv6 fe80::/10
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	return false
}
