package proxy

import (
	"regexp"
	"strings"
)

// Retryable patterns from the TS implementation.
// MODEL_UNSUPPORTED_PATTERNS — model not supported on this channel
var modelUnsupportedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`当前\s*api\s*不支持所选模型`),
	regexp.MustCompile(`不支持所选模型`),
	regexp.MustCompile(`不支持.*模型`),
	regexp.MustCompile(`模型.*不支持`),
	regexp.MustCompile(`unsupported\s+model`),
	regexp.MustCompile(`model\s+not\s+supported`),
	regexp.MustCompile(`does\s+not\s+support(?:\s+the)?\s+model`),
	regexp.MustCompile(`model.*does\s+not\s+exist`),
	regexp.MustCompile(`no\s+such\s+model`),
	regexp.MustCompile(`unknown\s+model`),
	regexp.MustCompile(`unknown\s+provider\s+for\s+model`),
	regexp.MustCompile(`invalid\s+model`),
	regexp.MustCompile(`model[_\s-]?not[_\s-]?found`),
	regexp.MustCompile(`you\s+do\s+not\s+have\s+access\s+to\s+the\s+model`),
}

// RETRYABLE_TIMEOUT_PATTERNS — timeout-like errors
var retryableTimeoutPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:request timed out|connection timed out|read timeout|first byte timeout|\btimed out\b)`),
}

// RETRYABLE_CHANNEL_LOCAL_PATTERNS — errors that justify retrying another channel
var retryableChannelLocalPatterns = appendMany(
	[]*regexp.Regexp{
		regexp.MustCompile(`unsupported\s+legacy\s+protocol`),
		regexp.MustCompile(`please\s+use\s+/v1/responses`),
		regexp.MustCompile(`please\s+use\s+/v1/messages`),
		regexp.MustCompile(`please\s+use\s+/v1/chat/completions`),
		regexp.MustCompile(`does\s+not\s+allow\s+/v1/[a-z0-9/_:-]+\s+dispatch`),
		regexp.MustCompile(`unsupported\s+endpoint`),
		regexp.MustCompile(`unsupported\s+path`),
		regexp.MustCompile(`unknown\s+endpoint`),
		regexp.MustCompile(`unrecognized\s+request\s+url`),
		regexp.MustCompile(`no\s+route\s+matched`),
		regexp.MustCompile(`invalid\s+api\s+key`),
		regexp.MustCompile(`invalid\s+access\s+token`),
		regexp.MustCompile(`forbidden`),
		regexp.MustCompile(`rate\s+limit`),
		regexp.MustCompile(`quota`),
		regexp.MustCompile(`bad\s+gateway`),
		regexp.MustCompile(`gateway\s+time-?out`),
		regexp.MustCompile(`service\s+unavailable`),
		regexp.MustCompile(`cpu\s+overloaded`),
	},
	retryableTimeoutPatterns,
)

// NON_RETRYABLE_REQUEST_PATTERNS — indicates a bad request, not a channel problem
var nonRetryableRequestPatterns = []*regexp.Regexp{
	regexp.MustCompile(`invalid\s+request\s+body`),
	regexp.MustCompile(`validation`),
	regexp.MustCompile(`missing\s+required`),
	regexp.MustCompile(`required\s+parameter`),
	regexp.MustCompile(`unknown\s+parameter`),
	regexp.MustCompile(`unrecognized\s+(?:field|key|parameter)`),
	regexp.MustCompile(`malformed`),
	regexp.MustCompile(`invalid\s+json`),
	regexp.MustCompile(`cannot\s+parse`),
	regexp.MustCompile(`unsupported\s+media\s+type`),
}

// SAME_SITE_ENDPOINT_ABORT_PATTERNS — errors that should abort same-site endpoint fallback
var sameSiteEndpointAbortPatterns = appendMany(
	[]*regexp.Regexp{
		regexp.MustCompile(`\b429\b`),
		regexp.MustCompile(`too\s+many\s+requests`),
		regexp.MustCompile(`rate\s+limit`),
		regexp.MustCompile(`quota(?:\s+exceeded)?`),
		regexp.MustCompile(`bad\s+gateway`),
		regexp.MustCompile(`gateway\s+time-?out`),
		regexp.MustCompile(`service\s+unavailable`),
		regexp.MustCompile(`temporar(?:y|ily)\s+unavailable`),
		regexp.MustCompile(`cpu\s+overloaded`),
		regexp.MustCompile(`connection\s+reset`),
		regexp.MustCompile(`connection\s+refused`),
		regexp.MustCompile(`econnreset`),
		regexp.MustCompile(`econnrefused`),
	},
	retryableTimeoutPatterns,
)

func appendMany(base []*regexp.Regexp, extras []*regexp.Regexp) []*regexp.Regexp {
	result := make([]*regexp.Regexp, len(base)+len(extras))
	copy(result, base)
	copy(result[len(base):], extras)
	return result
}

func isModelUnsupportedError(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return matchesAnyPattern(modelUnsupportedPatterns, text)
}

func matchesAnyPattern(patterns []*regexp.Regexp, text string) bool {
	text = strings.TrimSpace(text)
	text = trimToLower(text)
	for _, p := range patterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

// trimToLower trims whitespace and lowercases ASCII characters.
// This matches TS behavior of (rawMessage || '').trim().toLowerCase().
func trimToLower(s string) string {
	s = strings.TrimSpace(s)
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result = append(result, c+32)
		} else {
			result = append(result, c)
		}
	}
	return string(result)
}

// ShouldRetryProxyRequest classifies an upstream failure for channel-level retry decisions.
// Returns true if the proxy should retry with a different channel or the same channel.
//
// Decision matrix:
//   - >= 500: always retryable
//   - 408/409/425/429: always retryable
//   - 401/403: retryable (may be resolved by OAuth token refresh)
//   - Error text matches model-unsupported patterns: retryable (try another channel)
//   - Error text matches non-retryable request patterns: non-retryable (bad request)
//   - Error text matches retryable channel-local patterns: retryable
//   - 400/404/422: non-retryable (unless error text overrides)
//   - Other: non-retryable
func ShouldRetryProxyRequest(status int, upstreamErrorText string) bool {
	if status >= 500 {
		return true
	}
	if status == 408 || status == 409 || status == 425 || status == 429 {
		return true
	}
	if status == 401 || status == 403 {
		return true
	}
	if isModelUnsupportedError(upstreamErrorText) {
		return true
	}
	if matchesAnyPattern(nonRetryableRequestPatterns, upstreamErrorText) {
		return false
	}
	if matchesAnyPattern(retryableChannelLocalPatterns, upstreamErrorText) {
		return true
	}
	if status == 400 || status == 404 || status == 422 {
		return false
	}
	return false
}

// ShouldAbortSameSiteEndpointFallback decides whether to abort endpoint fallback
// within the same site. Used by the endpoint flow to stop trying other endpoints
// when the failure looks systemic (rate limit, gateway error, etc.).
func ShouldAbortSameSiteEndpointFallback(status int, upstreamErrorText string) bool {
	if status < 500 && status != 408 && status != 429 {
		return false
	}
	return matchesAnyPattern(sameSiteEndpointAbortPatterns, upstreamErrorText)
}

// GetProxyMaxChannelAttempts returns the configured max channel attempts (min 1).
func GetProxyMaxChannelAttempts(cfgMaxChannelAttempts int) int {
	if cfgMaxChannelAttempts <= 0 {
		return 1
	}
	return cfgMaxChannelAttempts
}

// GetProxyMaxChannelRetries returns max channel retries (attempts - 1, min 0).
func GetProxyMaxChannelRetries(maxChannelAttempts int) int {
	maxRetries := maxChannelAttempts - 1
	if maxRetries < 0 {
		return 0
	}
	return maxRetries
}
