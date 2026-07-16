package platform

import (
	"fmt"
	"regexp"
	"strings"
)

// UpstreamErrorClass is the high-level class for upstream failure signals.
// Used for account-status decisions, retry UX, and residual-risk documentation.
type UpstreamErrorClass string

const (
	// ClassExpired means the upstream credential/session looks expired or invalid.
	// Only this class may mark accounts.status = 'expired'.
	ClassExpired UpstreamErrorClass = "expired"
	// ClassAuth means an auth/session problem that is not clearly token expiry.
	ClassAuth UpstreamErrorClass = "auth"
	// ClassBilling means payment / quota / credit failures.
	ClassBilling UpstreamErrorClass = "billing"
	// ClassModel means model capability / unsupported-model failures.
	ClassModel UpstreamErrorClass = "model"
	// ClassValidation means request validation / argument errors.
	ClassValidation UpstreamErrorClass = "validation"
	// ClassTransient means rate-limit / timeout / 5xx / network-like failures.
	ClassTransient UpstreamErrorClass = "transient"
	// ClassUnknown is the residual bucket.
	ClassUnknown UpstreamErrorClass = "unknown"
)

var (
	endpointDispatchDeniedRe = regexp.MustCompile(`does\s+not\s+allow\s+/v1/[a-z0-9/_:-]+\s+dispatch`)
	invalidAccessTokenRe     = regexp.MustCompile(`invalid\s+access\s+token`)
	accessTokenIsInvalidRe   = regexp.MustCompile(`access\s+token\s+is\s+invalid`)
	modelNotSupportedRe      = regexp.MustCompile(`model\s+.+\s+is\s+not\s+supported`)
	// Auth-oriented token references. Intentionally avoids bare "token"
	// so phrases like "input token limit" do not look like credential failures.
	authTokenRefRe = regexp.MustCompile(
		`access\s+token|api[_\s-]?key|jwt|访问令牌|令牌|\binvalid\s+token\b|\btoken\s+(?:is\s+)?(?:invalid|expired)\b|\btoken\s+expired\b`,
	)
	authFailureSignalRe = regexp.MustCompile(
		`unauthorized|unauthenticated|authentication\s+(?:failed|required|error)|not\s+login|not\s+logged|未登录|未授权|无权|forbidden`,
	)
)

// ClassifyUpstreamError maps an upstream HTTP status + message to a class.
// Classification is intentionally conservative for ClassExpired: non-auth
// 401/403 bodies (billing, model, validation, rate-limit) must not mark keys expired.
func ClassifyUpstreamError(httpStatus int, message string) UpstreamErrorClass {
	raw := message
	text := strings.ToLower(strings.TrimSpace(message))

	// Explicit non-auth exclusions first (even when status is 401).
	if isEndpointDispatchDeniedMessage(raw) {
		return ClassValidation
	}
	if text != "" && strings.Contains(text, "未登录且未提供 access token") {
		// NewAPI probe noise: missing credential header, not a stored-token expiry.
		return ClassAuth
	}
	if isRequestValidationFailure(text) {
		return ClassValidation
	}
	if isCapabilityFailure(text) {
		return ClassModel
	}
	if isBillingFailure(text) {
		return ClassBilling
	}

	// Strong credential-expiry phrases beat mixed residual text such as
	// "jwt expired, connection timeout" (checkin failure-reason priority 6 > 8).
	if isStrongTokenExpiredSignal(text) {
		return ClassExpired
	}

	if isTransientFailure(httpStatus, text) {
		return ClassTransient
	}
	if isCloudflareChallengeMessage(text) {
		return ClassTransient
	}

	// Bare HTTP 401 with empty body is treated as expired (legacy auto-relogin /
	// mark path). Non-empty 401/403 without auth signal stay unknown/auth.
	if httpStatus == 401 || containsHTTPStatus(raw, 401) {
		if text == "" {
			return ClassExpired
		}
		if hasAuthFailureSignal(text) || hasAuthTokenReference(text) {
			// Unauthorized without an explicit expiry phrase is still auth, but
			// historical IsTokenExpiredError treated 401 as expired once non-auth
			// exclusions were ruled out. Keep that for empty-adjacent auth 401s.
			if hasExplicitExpiryOrInvalidCredential(text) || text == "unauthorized" ||
				strings.Contains(text, "http 401") || strings.Contains(text, "401 unauthorized") {
				return ClassExpired
			}
			return ClassAuth
		}
		// 401 with an opaque/non-auth residual body must not mark expired.
		return ClassUnknown
	}

	if httpStatus == 403 || containsHTTPStatus(raw, 403) {
		if hasAuthFailureSignal(text) || hasAuthTokenReference(text) {
			return ClassAuth
		}
		return ClassUnknown
	}

	if hasAuthFailureSignal(text) && hasAuthTokenReference(text) {
		return ClassAuth
	}

	return ClassUnknown
}

// IsTokenExpiredError reports whether the signal should be treated as an
// expired/invalid stored credential for auto-relogin and account marking.
// Mirrors historical TS isTokenExpiredError() with tighter non-auth guards.
func IsTokenExpiredError(httpStatus int, message string) bool {
	switch ClassifyUpstreamError(httpStatus, message) {
	case ClassExpired:
		return true
	case ClassAuth:
		// Auth without clear expiry does not mark accounts expired, but some
		// callers historically used IsTokenExpiredError for auto-relogin only.
		// Keep auto-relogin-compatible true only for strong credential signals.
		text := strings.ToLower(strings.TrimSpace(message))
		return hasExplicitExpiryOrInvalidCredential(text)
	default:
		return false
	}
}

// ShouldMarkAccountExpired is the guard used before writing accounts.status='expired'.
// Today it matches IsTokenExpiredError; kept separate so mark policy can tighten later
// without changing auto-relogin heuristics.
func ShouldMarkAccountExpired(httpStatus int, message string) bool {
	return ClassifyUpstreamError(httpStatus, message) == ClassExpired
}

// IsAuthRelatedUpstreamError is true for expired or other auth classes.
func IsAuthRelatedUpstreamError(httpStatus int, message string) bool {
	c := ClassifyUpstreamError(httpStatus, message)
	return c == ClassExpired || c == ClassAuth
}

func isEndpointDispatchDeniedMessage(message string) bool {
	text := strings.ToLower(message)
	if text == "" {
		return false
	}
	return endpointDispatchDeniedRe.MatchString(text) || strings.Contains(text, "dispatch denied")
}

func isRequestValidationFailure(text string) bool {
	if text == "" {
		return false
	}
	return strings.Contains(text, "invalid_argument") ||
		strings.Contains(text, "invalid_request_error") ||
		strings.Contains(text, "input token limit") ||
		strings.Contains(text, "context length") ||
		strings.Contains(text, "maximum context") ||
		strings.Contains(text, "max_tokens") ||
		strings.Contains(text, "max tokens") ||
		strings.Contains(text, "string_above_max_length") ||
		strings.Contains(text, "invalid request body") ||
		strings.Contains(text, "validation error") ||
		strings.Contains(text, "missing required")
}

func isCapabilityFailure(text string) bool {
	if text == "" {
		return false
	}
	return modelNotSupportedRe.MatchString(text) ||
		strings.Contains(text, "not supported for format") ||
		strings.Contains(text, "model not supported") ||
		strings.Contains(text, "unsupported model") ||
		strings.Contains(text, "does not support the model") ||
		strings.Contains(text, "does not support model") ||
		strings.Contains(text, "no such model") ||
		strings.Contains(text, "unknown model") ||
		strings.Contains(text, "model_not_found") ||
		strings.Contains(text, "不支持所选模型") ||
		(strings.Contains(text, "不支持") && strings.Contains(text, "模型"))
}

func isBillingFailure(text string) bool {
	if text == "" {
		return false
	}
	return strings.Contains(text, "no payment method") ||
		strings.Contains(text, "payment method") ||
		strings.Contains(text, "billing") ||
		strings.Contains(text, "insufficient_quota") ||
		strings.Contains(text, "insufficient quota") ||
		strings.Contains(text, "quota exceeded") ||
		strings.Contains(text, "exceeded your current quota") ||
		strings.Contains(text, "credit balance") ||
		strings.Contains(text, "out of credits") ||
		strings.Contains(text, "余额不足") ||
		strings.Contains(text, "额度不足") ||
		strings.Contains(text, "配额不足") ||
		strings.Contains(text, "充值")
}

func isTransientFailure(httpStatus int, text string) bool {
	if httpStatus == 408 || httpStatus == 409 || httpStatus == 425 || httpStatus == 429 || httpStatus >= 500 {
		return true
	}
	if text == "" {
		return false
	}
	return strings.Contains(text, "rate limit") ||
		strings.Contains(text, "rate_limit") ||
		strings.Contains(text, "too many requests") ||
		strings.Contains(text, "timeout") ||
		strings.Contains(text, "timed out") ||
		strings.Contains(text, "temporar") ||
		strings.Contains(text, "service unavailable") ||
		strings.Contains(text, "bad gateway") ||
		strings.Contains(text, "gateway time") ||
		strings.Contains(text, "connection reset") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "econnreset") ||
		strings.Contains(text, "econnrefused")
}

func isCloudflareChallengeMessage(text string) bool {
	if text == "" {
		return false
	}
	return strings.Contains(text, "cloudflare") ||
		strings.Contains(text, "cf challenge") ||
		strings.Contains(text, "challenge required")
}

func isStrongTokenExpiredSignal(text string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, "jwt expired") ||
		strings.Contains(text, "token expired") ||
		strings.Contains(text, "access token expired") ||
		invalidAccessTokenRe.MatchString(text) ||
		accessTokenIsInvalidRe.MatchString(text) {
		return true
	}
	return hasExplicitExpiryOrInvalidCredential(text)
}

func hasExplicitExpiryOrInvalidCredential(text string) bool {
	if text == "" {
		return false
	}
	if strings.Contains(text, "jwt expired") ||
		strings.Contains(text, "token expired") ||
		strings.Contains(text, "access token expired") ||
		invalidAccessTokenRe.MatchString(text) ||
		accessTokenIsInvalidRe.MatchString(text) {
		return true
	}
	// Chinese credential expiry / invalidity.
	if (strings.Contains(text, "令牌") || strings.Contains(text, "访问令牌")) &&
		(strings.Contains(text, "过期") || strings.Contains(text, "无效")) {
		return true
	}
	// Auth-oriented token ref + invalid/expired (not bare "token").
	if hasAuthTokenReference(text) &&
		(strings.Contains(text, "invalid") || strings.Contains(text, "expired") ||
			strings.Contains(text, "无效") || strings.Contains(text, "过期")) {
		return true
	}
	return false
}

func hasAuthTokenReference(text string) bool {
	return authTokenRefRe.MatchString(text)
}

func hasAuthFailureSignal(text string) bool {
	return authFailureSignalRe.MatchString(text)
}

func containsHTTPStatus(message string, status int) bool {
	pattern := fmt.Sprintf(`(?:^|\b)(?:http\s*)?%d(?:\b|:)`, status)
	re := regexp.MustCompile(pattern)
	return re.MatchString(strings.ToLower(message))
}
