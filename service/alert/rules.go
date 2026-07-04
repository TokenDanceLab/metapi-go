package alert

import (
	"fmt"
	"regexp"
	"strings"
)

const sessionTokenRebindHint = "请在中转站重新生成系统访问令牌后重新绑定账号"

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
	re := regexp.MustCompile(`does\s+not\s+allow\s+/v1/[a-z0-9/_:-]+\s+dispatch`)
	return re.MatchString(message) || strings.Contains(text, "dispatch denied")
}

// IsTokenExpiredError detects if an error indicates an expired token.
// Mirrors TS isTokenExpiredError().
func IsTokenExpiredError(httpStatus int, message string) bool {
	rawMessage := message
	text := strings.ToLower(message)

	// Exclude dispatch denied messages
	if isEndpointDispatchDeniedMessage(rawMessage) {
		return false
	}

	// Check HTTP 401
	if httpStatus == 401 || containsHTTPStatus(rawMessage, 401) {
		return true
	}

	if text == "" {
		return false
	}

	// NewAPI-specific: "未登录且未提供 access token" doesn't always mean token expired
	if strings.Contains(text, "未登录且未提供 access token") {
		return false
	}

	tokenPhrase := strings.Contains(text, "token") || strings.Contains(text, "令牌") || strings.Contains(text, "访问令牌")
	hasInvalid := strings.Contains(text, "invalid") || strings.Contains(text, "无效")
	hasExpired := strings.Contains(text, "expired") || strings.Contains(text, "过期")

	return strings.Contains(text, "jwt expired") ||
		strings.Contains(text, "token expired") ||
		(tokenPhrase && (hasInvalid || hasExpired)) ||
		regexp.MustCompile(`invalid\s+access\s+token`).MatchString(text) ||
		regexp.MustCompile(`access\s+token\s+is\s+invalid`).MatchString(text)
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
			regexp.MustCompile(`invalid\s+access\s+token`).MatchString(text) ||
			regexp.MustCompile(`access\s+token\s+is\s+invalid`).MatchString(text) ||
			regexp.MustCompile(`access\s+token.*无效`).MatchString(raw)

	if !looksLikeInvalidAccessToken {
		return raw
	}

	return raw + "，" + sessionTokenRebindHint
}
