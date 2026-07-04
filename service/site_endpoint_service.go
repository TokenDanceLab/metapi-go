package service

import (
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
func IsValidHTTPURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
