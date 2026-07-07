package alert

import (
	"fmt"
	"regexp"
	"strings"
)

const sessionTokenRebindHint = "请在中转站重新生成系统访问令牌后重新绑定账号"

var (
	endpointDispatchDeniedRe    = regexp.MustCompile(`does\s+not\s+allow\s+/v1/[a-z0-9/_:-]+\s+dispatch`)
	invalidAccessTokenRe        = regexp.MustCompile(`invalid\s+access\s+token`)
	accessTokenIsInvalidRe      = regexp.MustCompile(`access\s+token\s+is\s+invalid`)
	modelNotSupportedRe         = regexp.MustCompile(`model\s+.+\s+is\s+not\s+supported`)
	accessTokenChineseInvalidRe = regexp.MustCompile(`access\s+token.*无效`)
)

// IsCloudflareChallenge detects Cloudflare challenge messages.
// Mirrors TS isCloudflareChallenge().
func IsCloudflareChallenge(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return false
	}
	return strings.Contains(text, "cloudflare") ||
		strings.Contains(text, "cf challenge") ||
		strings.Contains(text, "challenge required")
}

// isEndpointDispatchDeniedMessage detects dispatch denied messages (not token expired).
func isEndpointDispatchDeniedMessage(message string) bool {
	text := strings.ToLower(message)
	if text == "" {
		return false
	}
	return endpointDispatchDeniedRe.MatchString(text) || strings.Contains(text, "dispatch denied")
}

// IsTokenExpiredError detects if an error indicates an expired token.
// Mirrors TS isTokenExpiredError().
func IsTokenExpiredError(httpStatus int, message string) bool {
	rawMessage := message
	text := strings.ToLower(strings.TrimSpace(message))

	// Exclude dispatch denied messages
	if isEndpointDispatchDeniedMessage(rawMessage) {
		return false
	}

	if text == "" {
		return httpStatus == 401
	}

	// NewAPI-specific: "未登录且未提供 access token" doesn't always mean token expired
	if strings.Contains(text, "未登录且未提供 access token") {
		return false
	}

	if isRequestValidationFailure(text) || isCapabilityOrBillingFailure(text) {
		return false
	}

	// Check HTTP 401 after explicit non-auth failure exclusions.
	if httpStatus == 401 || containsHTTPStatus(rawMessage, 401) {
		return true
	}

	tokenPhrase := strings.Contains(text, "token") || strings.Contains(text, "令牌") || strings.Contains(text, "访问令牌")
	hasInvalid := strings.Contains(text, "invalid") || strings.Contains(text, "无效")
	hasExpired := strings.Contains(text, "expired") || strings.Contains(text, "过期")

	return strings.Contains(text, "jwt expired") ||
		strings.Contains(text, "token expired") ||
		(tokenPhrase && (hasInvalid || hasExpired)) ||
		invalidAccessTokenRe.MatchString(text) ||
		accessTokenIsInvalidRe.MatchString(text)
}

func isRequestValidationFailure(text string) bool {
	return strings.Contains(text, "invalid_argument") ||
		strings.Contains(text, "invalid_request_error") ||
		strings.Contains(text, "input token limit") ||
		strings.Contains(text, "context length") ||
		strings.Contains(text, "maximum context")
}

func isCapabilityOrBillingFailure(text string) bool {
	return modelNotSupportedRe.MatchString(text) ||
		strings.Contains(text, "not supported for format") ||
		strings.Contains(text, "no payment method") ||
		strings.Contains(text, "payment method") ||
		strings.Contains(text, "billing")
}

// containsHTTPStatus checks if a message contains an HTTP status code.
func containsHTTPStatus(message string, status int) bool {
	pattern := fmt.Sprintf(`(?:^|\b)(?:http\s*)?%d(?:\b|:)`, status)
	re := regexp.MustCompile(pattern)
	return re.MatchString(strings.ToLower(message))
}

// AppendSessionTokenRebindHint appends rebind hint to invalid access token messages.
// Mirrors TS appendSessionTokenRebindHint().
func AppendSessionTokenRebindHint(message string) string {
	raw := strings.TrimSpace(message)
	if raw == "" {
		return raw
	}
	if strings.Contains(raw, sessionTokenRebindHint) {
		return raw
	}

	text := strings.ToLower(raw)
	looksLikeInvalidAccessToken :=
		strings.Contains(raw, "无权进行此操作，access token 无效") ||
			invalidAccessTokenRe.MatchString(text) ||
			accessTokenIsInvalidRe.MatchString(text) ||
			accessTokenChineseInvalidRe.MatchString(raw)

	if !looksLikeInvalidAccessToken {
		return raw
	}

	return raw + "，" + sessionTokenRebindHint
}
