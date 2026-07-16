package proxy

import (
	"strings"

	"github.com/tokendancelab/metapi-go/proxy/profiles"
	"github.com/tokendancelab/metapi-go/proxy/types"
)

// SiteProtocolPreference re-exports profiles.SiteProtocolPreference for callers
// that already depend on proxy (handler layer).
type SiteProtocolPreference = profiles.SiteProtocolPreference

// DetectSiteProtocolPreference resolves responses-only / stream preference for a site.
// See docs/analysis/responses-only-sites.md (issue #56 / upstream #340).
func DetectSiteProtocolPreference(platform, siteURL string, customHeaders map[string]string) SiteProtocolPreference {
	return profiles.DetectSiteProtocolPreference(platform, siteURL, customHeaders)
}

// DetectSiteProtocolPreferenceFromSite is a convenience for store.Site-like fields.
func DetectSiteProtocolPreferenceFromSite(platform, siteURL string, customHeadersJSON *string) SiteProtocolPreference {
	var headers map[string]string
	if customHeadersJSON != nil {
		headers = profiles.ParseCustomHeadersJSON(*customHeadersJSON)
	}
	return profiles.DetectSiteProtocolPreference(platform, siteURL, headers)
}

// IsResponsesOnlySite reports whether the site is responses-only.
func IsResponsesOnlySite(platform, siteURL string, customHeaders map[string]string) bool {
	return profiles.IsResponsesOnlySite(platform, siteURL, customHeaders)
}

// IsMetapiControlHeader reports whether a custom header is a MetAPI control signal
// that must not be forwarded upstream (responses-only / stream preference marks).
func IsMetapiControlHeader(name string) bool {
	return profiles.IsMetapiControlHeader(name)
}

// EndpointCandidateOptions controls multi-protocol candidate ordering.
type EndpointCandidateOptions struct {
	// DisableCrossProtocolFallback limits candidates to the primary endpoint only.
	// When ResponsesOnly is set, primary is forced to responses regardless of
	// downstream path (chat/messages clients are rewritten to responses).
	DisableCrossProtocolFallback bool
	// Preference is the site protocol preference (responses-only / stream).
	Preference SiteProtocolPreference
	// DownstreamClient may influence preference when Codex profile is detected.
	DownstreamClient *types.DetectedProfile
}

// ResolveEndpointCandidatesWithOptions builds the ordered multi-protocol candidate
// list honoring responses-only / prefer-responses site preference (#56).
//
// Policy:
//   - responses-only site → only EndpointResponses (even if client called chat/messages)
//   - prefer-responses    → responses first, then remaining chat-family endpoints
//   - disableCrossProtocolFallback → single primary (responses when responses-only)
//   - non chat-family path → nil (caller uses original path)
func ResolveEndpointCandidatesWithOptions(downstreamPath string, opts EndpointCandidateOptions) []UpstreamEndpoint {
	primary, ok := EndpointFromPath(downstreamPath)
	if !ok {
		return nil
	}

	pref := opts.Preference
	// Codex client profile on responses path reinforces prefer-responses, but does
	// not by itself force responses-only (that is a site property).
	if opts.DownstreamClient != nil && opts.DownstreamClient.ID == types.ProfileCodex {
		if !pref.PreferResponses && !pref.ResponsesOnly {
			// no-op; site preference is authoritative for responses-only.
		}
	}

	if pref.ResponsesOnly {
		return []UpstreamEndpoint{EndpointResponses}
	}

	if opts.DisableCrossProtocolFallback {
		return []UpstreamEndpoint{primary}
	}

	order := []UpstreamEndpoint{EndpointChat, EndpointMessages, EndpointResponses}
	switch {
	case pref.PreferResponses:
		order = []UpstreamEndpoint{EndpointResponses, EndpointChat, EndpointMessages}
	case primary == EndpointResponses:
		order = []UpstreamEndpoint{EndpointResponses, EndpointChat, EndpointMessages}
	case primary == EndpointMessages:
		order = []UpstreamEndpoint{EndpointMessages, EndpointChat, EndpointResponses}
	case primary == EndpointChat:
		order = []UpstreamEndpoint{EndpointChat, EndpointMessages, EndpointResponses}
	}

	out := make([]UpstreamEndpoint, 0, len(order))
	seen := map[UpstreamEndpoint]bool{}
	for _, ep := range order {
		if seen[ep] {
			continue
		}
		seen[ep] = true
		out = append(out, ep)
	}
	return out
}

// ResolveEndpointCandidates builds the ordered multi-protocol candidate list for a
// downstream path. Primary endpoint is first. When disableCrossProtocolFallback is
// true, only the primary is returned. Non chat-family paths yield nil.
//
// Product policy (upstream #387 / issue #38):
// - first-byte timeout or protocol-mismatch may continue to remaining candidates
// - DISABLE_CROSS_PROTOCOL_FALLBACK stops after the primary attempt
//
// For responses-only sites, prefer ResolveEndpointCandidatesWithOptions.
func ResolveEndpointCandidates(downstreamPath string, disableCrossProtocolFallback bool) []UpstreamEndpoint {
	return ResolveEndpointCandidatesWithOptions(downstreamPath, EndpointCandidateOptions{
		DisableCrossProtocolFallback: disableCrossProtocolFallback,
	})
}

// ShouldForceUpstreamStream reports whether the request body must set stream=true
// for the given upstream path / site preference.
//
// Forces stream when:
//   - site PreferStream / ResponsesOnly and path is responses (non-compact)
//   - platform-level ShouldForceResponsesUpstreamStream (codex/sub2api) — callers
//     may combine with transform/openai/responses helpers
func ShouldForceUpstreamStream(pref SiteProtocolPreference, upstreamPath string, isCompact bool) bool {
	if isCompact {
		return false
	}
	if !pref.PreferStream && !pref.ResponsesOnly {
		return false
	}
	ep, ok := EndpointFromPath(upstreamPath)
	if !ok {
		// Treat explicit responses compact/non-compact paths.
		p := strings.ToLower(strings.TrimSpace(upstreamPath))
		if strings.Contains(p, "/responses") && !strings.Contains(p, "/compact") {
			return true
		}
		return false
	}
	return ep == EndpointResponses
}

// ApplyStreamPreference mutates a shallow copy of body to set stream=true when
// ShouldForceUpstreamStream is true. Returns the (possibly same) body map and
// whether stream was forced.
func ApplyStreamPreference(body map[string]any, pref SiteProtocolPreference, upstreamPath string, isCompact bool) (map[string]any, bool) {
	if !ShouldForceUpstreamStream(pref, upstreamPath, isCompact) {
		return body, false
	}
	if body == nil {
		body = map[string]any{}
	}
	// Already streaming?
	if v, ok := body["stream"]; ok {
		switch t := v.(type) {
		case bool:
			if t {
				return body, false
			}
		case string:
			if t == "true" || t == "1" {
				return body, false
			}
		}
	}
	next := make(map[string]any, len(body)+1)
	for k, v := range body {
		next[k] = v
	}
	next["stream"] = true
	return next, true
}

// ResponsesOnlyChatUnsupportedMessage is the clear client error when a chat/completions
// (or messages) client hits a responses-only site and transform is not available.
// Used when DISABLE_CROSS_PROTOCOL_FALLBACK is true and body cannot be rewritten.
func ResponsesOnlyChatUnsupportedMessage(downstreamPath string) string {
	path := strings.TrimSpace(downstreamPath)
	if path == "" {
		path = "this endpoint"
	}
	return "site is responses-only and only supports /v1/responses (streaming); " +
		"client called " + path + " which cannot be served without protocol transform"
}
