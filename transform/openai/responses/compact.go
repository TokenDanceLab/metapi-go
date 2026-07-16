// Package responses provides OpenAI Responses API transformer and compact mode.
package responses

import (
	"strings"
)

// ShouldStripCompactResponsesStore returns true for codex/sub2api platforms.
func ShouldStripCompactResponsesStore(sitePlatform string) bool {
	n := strings.ToLower(strings.TrimSpace(sitePlatform))
	return n == "codex" || n == "sub2api"
}

// ShouldForceResponsesUpstreamStream returns true when stream must be forced for upstream.
func ShouldForceResponsesUpstreamStream(sitePlatform string, isCompactRequest bool) bool {
	if isCompactRequest {
		return false
	}
	n := strings.ToLower(strings.TrimSpace(sitePlatform))
	return n == "codex" || n == "sub2api"
}

// SanitizeCompactResponsesRequestBody removes stream/stream_options and
// conditionally store. Compact never continues a prior stored response, so
// previous_response_id is always stripped here (see previous_response_id.go).
func SanitizeCompactResponsesRequestBody(body map[string]any, sitePlatform string) map[string]any {
	next := map[string]any{}
	for k, v := range body {
		next[k] = v
	}
	delete(next, "stream")
	delete(next, "stream_options")
	delete(next, PreviousResponseIDField)
	if ShouldStripCompactResponsesStore(sitePlatform) {
		delete(next, "store")
	}
	return next
}

// EnsureCompactResponsesJSONAcceptHeader forces accept: application/json for codex/sub2api.
func EnsureCompactResponsesJSONAcceptHeader(headers map[string]string, sitePlatform string) map[string]string {
	if !ShouldStripCompactResponsesStore(sitePlatform) {
		return headers
	}
	next := map[string]string{}
	for k, v := range headers {
		next[k] = v
	}
	delete(next, "Accept")
	delete(next, "accept")
	next["accept"] = "application/json"
	return next
}

// ShouldFallbackCompactResponsesToResponses checks error conditions for compact fallback.
func ShouldFallbackCompactResponsesToResponses(status int, rawErrText, requestPath string) bool {
	compact := strings.ToLower(strings.TrimSpace(rawErrText))
	rp := strings.ToLower(strings.TrimSpace(requestPath))

	hasRawCompactHint := strings.Contains(compact, "/responses/compact") ||
		strings.Contains(compact, "responses/compact") ||
		strings.Contains(compact, "compact endpoint") ||
		strings.Contains(compact, " compact ") ||
		strings.HasPrefix(compact, "compact ") ||
		strings.HasSuffix(compact, " compact")

	hasCompactPathHint := strings.HasSuffix(rp, "/responses/compact") ||
		strings.HasSuffix(rp, "/v1/responses/compact")

	if status == 404 || status == 405 || status == 501 {
		return true
	}

	if strings.Contains(compact, "unknown parameter: 'stream'") && (hasRawCompactHint || hasCompactPathHint) {
		return true
	}

	if strings.Contains(compact, "invalid url") && hasRawCompactHint {
		return true
	}

	if hasRawCompactHint && (strings.Contains(compact, "not supported") || strings.Contains(compact, "unsupported")) {
		return true
	}

	return false
}

// Inbound parses a responses request body.
func Inbound(body any) (map[string]any, error) {
	m, ok := body.(map[string]any)
	if !ok {
		return map[string]any{}, nil
	}
	return m, nil
}
