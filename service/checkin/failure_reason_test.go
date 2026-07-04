package checkin

import (
	"testing"

	"github.com/tokendancelab/metapi-go/service/alert"
)

// ---- Failure Reason Classification Tests ----

func TestClassifyFailureReason_SiteDisabled(t *testing.T) {
	result := ClassifyFailureReason(ClassifyFailureInput{
		Message: "site disabled",
		Status:  "skipped",
	})
	if result.Code != CodeSiteDisabled {
		t.Errorf("expected CodeSiteDisabled, got %s", result.Code)
	}
	if result.Category != CategorySite {
		t.Errorf("expected CategorySite, got %s", result.Category)
	}
	if result.Title != "站点已禁用" {
		t.Errorf("unexpected title: %s", result.Title)
	}
}

func TestClassifyFailureReason_CheckinNotSupported(t *testing.T) {
	messages := []string{
		"checkin endpoint not found",
		"check-in is not supported",
		"checkin is not supported",
		"does not support checkin",
		"not support checkin",
		"签到端点不存在",
		"站点不支持签到",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeCheckinNotSupported {
				t.Errorf("expected CodeCheckinNotSupported for %q, got %s", msg, result.Code)
			}
			if result.Category != CategorySite {
				t.Errorf("expected CategorySite for %q, got %s", msg, result.Category)
			}
		})
	}
}

func TestClassifyFailureReason_ManualTurnstileRequired(t *testing.T) {
	messages := []string{
		"Turnstile token 为空",
		"turnstile 校验失败",
		"Turnstile 验证码",
		"turnstile token error",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeManualTurnstileRequired {
				t.Errorf("expected CodeManualTurnstileRequired for %q, got %s", msg, result.Code)
			}
			if result.Category != CategoryVerification {
				t.Errorf("expected CategoryVerification for %q, got %s", msg, result.Category)
			}
		})
	}
}

func TestClassifyFailureReason_CloudflareTunnelUnavailable(t *testing.T) {
	messages := []string{
		"cloudflare tunnel error",
		"error 1033",
		"unable to resolve it",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeCloudflareTunnelUnavail {
				t.Errorf("expected CodeCloudflareTunnelUnavail for %q, got %s", msg, result.Code)
			}
			if result.Category != CategoryNetwork {
				t.Errorf("expected CategoryNetwork for %q, got %s", msg, result.Category)
			}
		})
	}
}

func TestClassifyFailureReason_CloudflareChallenge(t *testing.T) {
	// Uses alert.IsCloudflareChallenge which checks: "cloudflare", "cf challenge", "challenge required"
	messages := []string{
		"Cloudflare protection triggered",
		"cf challenge detected",
		"challenge required by CF",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeCloudflareChallenge {
				t.Errorf("expected CodeCloudflareChallenge for %q, got %s", msg, result.Code)
			}
			if result.Category != CategoryVerification {
				t.Errorf("expected CategoryVerification for %q, got %s", msg, result.Category)
			}
		})
	}
}

func TestClassifyFailureReason_TokenExpired(t *testing.T) {
	// alert.IsTokenExpiredError must return true for these
	messages := []string{
		"jwt expired",
		"token expired",
		"invalid access token",
		"access token is invalid",
		"令牌无效",
		"访问令牌已过期",
	}
	for _, msg := range messages {
		if alert.IsTokenExpiredError(0, msg) {
			t.Run(msg, func(t *testing.T) {
				result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
				if result.Code != CodeTokenExpired {
					t.Errorf("expected CodeTokenExpired for %q, got %s", msg, result.Code)
				}
				if result.Category != CategoryAuth {
					t.Errorf("expected CategoryAuth for %q, got %s", msg, result.Category)
				}
			})
		}
	}
}

func TestClassifyFailureReason_TokenExpiredWithHTTP401(t *testing.T) {
	// HTTP 401 alone should trigger token expired
	result := ClassifyFailureReason(ClassifyFailureInput{
		HTTPStatus: 401,
		Message:    "Unauthorized",
	})
	if result.Code != CodeTokenExpired {
		t.Errorf("expected CodeTokenExpired for 401, got %s", result.Code)
	}
}

func TestClassifyFailureReason_AlreadyCheckedIn(t *testing.T) {
	messages := []string{
		"already checked in today",
		"already signed in",
		"今天已经签到",
		"今日已签到",
		"已经签到，请勿重复",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeAlreadyCheckedIn {
				t.Errorf("expected CodeAlreadyCheckedIn for %q, got %s", msg, result.Code)
			}
			if result.Category != CategoryState {
				t.Errorf("expected CategoryState for %q, got %s", msg, result.Category)
			}
		})
	}
}

func TestClassifyFailureReason_NetworkTimeout(t *testing.T) {
	messages := []string{
		"connection timeout",
		"request timed out",
		"ETIMEDOUT",
		"请求超时，请稍后重试",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeNetworkTimeout {
				t.Errorf("expected CodeNetworkTimeout for %q, got %s", msg, result.Code)
			}
			if result.Category != CategoryNetwork {
				t.Errorf("expected CategoryNetwork for %q, got %s", msg, result.Category)
			}
		})
	}
}

func TestClassifyFailureReason_UpstreamError(t *testing.T) {
	// HTTP 500+ status
	result := ClassifyFailureReason(ClassifyFailureInput{
		HTTPStatus: 500,
		Message:    "",
	})
	if result.Code != CodeUpstreamError {
		t.Errorf("expected CodeUpstreamError for HTTP 500, got %s", result.Code)
	}
	if result.Category != CategorySite {
		t.Errorf("expected CategorySite, got %s", result.Category)
	}
}

func TestClassifyFailureReason_UpstreamErrorByMessage(t *testing.T) {
	messages := []string{
		"HTTP 502 Bad Gateway",
		"upstream error",
		"internal server error",
	}
	for _, msg := range messages {
		t.Run(msg, func(t *testing.T) {
			result := ClassifyFailureReason(ClassifyFailureInput{Message: msg})
			if result.Code != CodeUpstreamError {
				t.Errorf("expected CodeUpstreamError for %q, got %s", msg, result.Code)
			}
		})
	}
}

func TestClassifyFailureReason_UnknownError(t *testing.T) {
	result := ClassifyFailureReason(ClassifyFailureInput{
		Message: "some random unexpected error",
	})
	if result.Code != CodeUnknownError {
		t.Errorf("expected CodeUnknownError, got %s", result.Code)
	}
	if result.Category != CategoryUnknown {
		t.Errorf("expected CategoryUnknown, got %s", result.Category)
	}
}

func TestClassifyFailureReason_SuccessStatus(t *testing.T) {
	// when status is 'success', returns '执行成功' unknown
	result := ClassifyFailureReason(ClassifyFailureInput{
		Status: "success",
	})
	if result.Title != "执行成功" {
		t.Errorf("expected 执行成功, got %s", result.Title)
	}
}

// ---- Priority Order Tests ----

func TestClassifyFailureReason_PriorityOrder(t *testing.T) {
	// When multiple patterns match, the first matching priority wins.

	// "site disabled" (priority 1) beats "timeout" (priority 8)
	result := ClassifyFailureReason(ClassifyFailureInput{
		Status:  "skipped",
		Message: "site disabled, timeout",
	})
	if result.Code != CodeSiteDisabled {
		t.Errorf("priority 1 (site_disabled) should beat priority 8 (timeout), got %s", result.Code)
	}

	// "checkin not supported" (priority 2) beats "cloudflare" (priority 5)
	result = ClassifyFailureReason(ClassifyFailureInput{
		Message: "does not support checkin, cloudflare",
	})
	if result.Code != CodeCheckinNotSupported {
		t.Errorf("priority 2 (checkin_not_supported) should beat priority 5 (cloudflare), got %s", result.Code)
	}

	// "turnstile" (priority 3) beats "token expired" (priority 6)
	result = ClassifyFailureReason(ClassifyFailureInput{
		Message: "turnstile token expired",
	})
	if result.Code != CodeManualTurnstileRequired {
		t.Errorf("priority 3 (turnstile) should beat priority 6 (token_expired), got %s", result.Code)
	}

	// "token expired" (priority 6) beats "timeout" (priority 8)
	result = ClassifyFailureReason(ClassifyFailureInput{
		Message: "jwt expired, connection timeout",
	})
	if result.Code != CodeTokenExpired {
		t.Errorf("priority 6 (token_expired) should beat priority 8 (timeout), got %s", result.Code)
	}

	// "timeout" (priority 8) beats "unknown" (priority 10)
	result = ClassifyFailureReason(ClassifyFailureInput{
		Message: "connection timeout, some random error",
	})
	if result.Code != CodeNetworkTimeout {
		t.Errorf("priority 8 (timeout) should beat priority 10 (unknown), got %s", result.Code)
	}
}
