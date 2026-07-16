package alert

import (
	"strings"

	"github.com/tokendancelab/metapi-go/platform"
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

// IsTokenExpiredError detects if an error indicates an expired token.
// Delegates to platform.ClassifyUpstreamError / IsTokenExpiredError (R0 SSOT).
// Mirrors TS isTokenExpiredError() with tighter non-auth guards.
func IsTokenExpiredError(httpStatus int, message string) bool {
	return platform.IsTokenExpiredError(httpStatus, message)
}

// ShouldMarkAccountExpired is the guard before writing accounts.status='expired'.
func ShouldMarkAccountExpired(httpStatus int, message string) bool {
	return platform.ShouldMarkAccountExpired(httpStatus, message)
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
			strings.Contains(text, "invalid access token") ||
			strings.Contains(text, "access token is invalid") ||
			(strings.Contains(text, "access token") && strings.Contains(raw, "无效"))

	if !looksLikeInvalidAccessToken {
		return raw
	}

	return raw + "，" + sessionTokenRebindHint
}
