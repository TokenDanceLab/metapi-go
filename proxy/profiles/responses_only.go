package profiles

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Site protocol preference control headers (site.custom_headers).
// These are configuration signals for MetAPI and must not be forwarded upstream.
const (
	HeaderResponsesOnly       = "x-metapi-responses-only"
	HeaderStreamOnly          = "x-metapi-stream-only"
	HeaderEndpointPreference  = "x-metapi-endpoint-preference"
	HeaderProtocolPreference  = "x-metapi-protocol-preference"
)

// SiteProtocolPreference describes how a site should be reached for chat-family traffic.
// Issue #56 / upstream #340: some relays only expose /v1/responses (+ streaming).
type SiteProtocolPreference struct {
	// ResponsesOnly means the site should not be tried on chat/messages; only /v1/responses.
	ResponsesOnly bool
	// PreferResponses reorders multi-protocol candidates so responses is first (fallback allowed).
	PreferResponses bool
	// PreferStream forces upstream stream=true for responses (and stream-only sites).
	PreferStream bool
	// Source is a short debug label: platform | header | url | none.
	Source string
}

// DetectSiteProtocolPreference resolves responses-only / stream preference from
// site platform, URL, and optional custom headers.
//
// Precedence (first match wins for Source; flags OR together across sources):
//  1. custom headers (operator mark)
//  2. platform (codex / known responses-stream platforms)
//  3. URL host markers (known responses-only relays)
func DetectSiteProtocolPreference(platform, siteURL string, customHeaders map[string]string) SiteProtocolPreference {
	pref := SiteProtocolPreference{Source: "none"}

	if h := preferenceFromHeaders(customHeaders); h.ResponsesOnly || h.PreferResponses || h.PreferStream {
		mergePreference(&pref, h)
		if pref.Source == "none" || pref.Source == "" {
			pref.Source = "header"
		}
	}

	if p := preferenceFromPlatform(platform); p.ResponsesOnly || p.PreferResponses || p.PreferStream {
		mergePreference(&pref, p)
		if pref.Source == "none" {
			pref.Source = "platform"
		}
	}

	if u := preferenceFromURL(siteURL); u.ResponsesOnly || u.PreferResponses || u.PreferStream {
		mergePreference(&pref, u)
		if pref.Source == "none" {
			pref.Source = "url"
		}
	}

	// Responses-only implies prefer-responses + stream preference (upstream #340).
	if pref.ResponsesOnly {
		pref.PreferResponses = true
		pref.PreferStream = true
	}
	return pref
}

// IsResponsesOnlySite is a convenience wrapper for DetectSiteProtocolPreference(...).ResponsesOnly.
func IsResponsesOnlySite(platform, siteURL string, customHeaders map[string]string) bool {
	return DetectSiteProtocolPreference(platform, siteURL, customHeaders).ResponsesOnly
}

// IsMetapiControlHeader reports whether a custom header is a MetAPI control signal
// that must not be forwarded to upstream. Only known preference keys are blocked;
// other x-metapi-* values (e.g. operator tracing headers) still pass through.
func IsMetapiControlHeader(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case HeaderResponsesOnly, HeaderStreamOnly, HeaderEndpointPreference, HeaderProtocolPreference:
		return true
	default:
		return false
	}
}

// ParseCustomHeadersJSON parses a site.custom_headers JSON object into a flat map.
// Invalid / empty input yields nil. Bool values become "true"/"false"; numbers become
// their JSON text; other types are skipped.
func ParseCustomHeadersJSON(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return nil
	}
	if len(generic) == 0 {
		return nil
	}
	out := make(map[string]string, len(generic))
	for k, v := range generic {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool:
			if t {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		case float64:
			b, err := json.Marshal(t)
			if err == nil {
				out[k] = string(b)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func preferenceFromHeaders(headers map[string]string) SiteProtocolPreference {
	if len(headers) == 0 {
		return SiteProtocolPreference{}
	}
	pref := SiteProtocolPreference{Source: "header"}
	if isTruthyHeader(headers, HeaderResponsesOnly) {
		pref.ResponsesOnly = true
		pref.PreferResponses = true
		pref.PreferStream = true
	}
	if isTruthyHeader(headers, HeaderStreamOnly) {
		pref.PreferStream = true
	}
	if ep := getHeaderCI(headers, HeaderEndpointPreference); ep != "" {
		switch strings.ToLower(strings.TrimSpace(ep)) {
		case "responses", "responses-only", "response", "v1/responses", "/v1/responses":
			pref.PreferResponses = true
			if strings.Contains(strings.ToLower(ep), "only") || strings.EqualFold(ep, "responses-only") {
				pref.ResponsesOnly = true
				pref.PreferStream = true
			}
		}
	}
	if proto := getHeaderCI(headers, HeaderProtocolPreference); proto != "" {
		lower := strings.ToLower(proto)
		if strings.Contains(lower, "responses-only") || strings.Contains(lower, "responses_only") {
			pref.ResponsesOnly = true
			pref.PreferResponses = true
			pref.PreferStream = true
		} else if strings.Contains(lower, "responses") {
			pref.PreferResponses = true
		}
		if strings.Contains(lower, "stream") {
			pref.PreferStream = true
		}
	}
	if !pref.ResponsesOnly && !pref.PreferResponses && !pref.PreferStream {
		return SiteProtocolPreference{}
	}
	return pref
}

func preferenceFromPlatform(platform string) SiteProtocolPreference {
	n := strings.ToLower(strings.TrimSpace(platform))
	switch n {
	case "codex", "chatgpt-codex", "chatgpt codex":
		// Official Codex backend is responses + stream oriented.
		return SiteProtocolPreference{
			ResponsesOnly:   true,
			PreferResponses: true,
			PreferStream:    true,
			Source:          "platform",
		}
	case "sub2api":
		// sub2api often needs forced stream on responses; not strictly responses-only.
		return SiteProtocolPreference{
			PreferStream: true,
			Source:       "platform",
		}
	default:
		return SiteProtocolPreference{}
	}
}

func preferenceFromURL(siteURL string) SiteProtocolPreference {
	raw := strings.TrimSpace(siteURL)
	if raw == "" {
		return SiteProtocolPreference{}
	}
	host := strings.ToLower(raw)
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = strings.ToLower(u.Hostname())
	} else {
		// Fallback: strip scheme-ish prefix.
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		if i := strings.IndexAny(host, "/?"); i >= 0 {
			host = host[:i]
		}
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return SiteProtocolPreference{}
	}

	// Known responses-only relays from upstream #340 discussions.
	// Keep this list narrow; operators can always mark via custom headers.
	for _, marker := range responsesOnlyHostMarkers {
		if host == marker || strings.HasSuffix(host, "."+marker) {
			return SiteProtocolPreference{
				ResponsesOnly:   true,
				PreferResponses: true,
				PreferStream:    true,
				Source:          "url",
			}
		}
	}
	return SiteProtocolPreference{}
}

// responsesOnlyHostMarkers are host suffixes known to expose only /v1/responses (+ stream).
var responsesOnlyHostMarkers = []string{
	"aabb1.pro",
}

func mergePreference(dst *SiteProtocolPreference, src SiteProtocolPreference) {
	if src.ResponsesOnly {
		dst.ResponsesOnly = true
	}
	if src.PreferResponses {
		dst.PreferResponses = true
	}
	if src.PreferStream {
		dst.PreferStream = true
	}
	if dst.Source == "none" || dst.Source == "" {
		dst.Source = src.Source
	}
}

func isTruthyHeader(headers map[string]string, key string) bool {
	v := strings.ToLower(strings.TrimSpace(getHeaderCI(headers, key)))
	switch v {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}
