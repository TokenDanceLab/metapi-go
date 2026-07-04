package checkin

import (
	"strings"

	"github.com/tokendancelab/metapi-go/service/alert"
)

// FailureReasonCategory classifies the nature of a failure.
type FailureReasonCategory string

const (
	CategoryVerification FailureReasonCategory = "verification"
	CategoryAuth         FailureReasonCategory = "auth"
	CategoryNetwork      FailureReasonCategory = "network"
	CategorySite         FailureReasonCategory = "site"
	CategoryState        FailureReasonCategory = "state"
	CategoryUnknown      FailureReasonCategory = "unknown"
)

// FailureReasonCode identifies the specific failure.
type FailureReasonCode string

const (
	CodeSiteDisabled             FailureReasonCode = "site_disabled"
	CodeCheckinNotSupported      FailureReasonCode = "checkin_not_supported"
	CodeManualTurnstileRequired  FailureReasonCode = "manual_turnstile_required"
	CodeCloudflareTunnelUnavail  FailureReasonCode = "cloudflare_tunnel_unavailable"
	CodeCloudflareChallenge      FailureReasonCode = "cloudflare_challenge"
	CodeTokenExpired             FailureReasonCode = "token_expired"
	CodeAlreadyCheckedIn         FailureReasonCode = "already_checked_in"
	CodeNetworkTimeout           FailureReasonCode = "network_timeout"
	CodeUpstreamError            FailureReasonCode = "upstream_error"
	CodeUnknownError             FailureReasonCode = "unknown_error"
)

// FailureReason describes why an operation failed.
type FailureReason struct {
	Code       FailureReasonCode     `json:"code"`
	Category   FailureReasonCategory `json:"category"`
	Title      string                `json:"title"`
	ActionHint string                `json:"actionHint"`
	DetailHint string                `json:"detailHint"`
}

// ClassifyFailureInput is the input to classifyFailureReason.
type ClassifyFailureInput struct {
	Message    string
	Status     string
	HTTPStatus int
}

// ClassifyFailureReason classifies a failure into a structured reason.
// Mirrors TS classifyFailureReason().
func ClassifyFailureReason(input ClassifyFailureInput) FailureReason {
	rawMessage := strings.TrimSpace(input.Message)
	text := strings.ToLower(rawMessage)
	status := strings.ToLower(input.Status)
	httpStatus := input.HTTPStatus

	// Priority 1: Site disabled
	if status == "skipped" && includesAny(text, []string{"site disabled"}) {
		return FailureReason{
			Code: CodeSiteDisabled, Category: CategorySite,
			Title: "站点已禁用", ActionHint: "启用站点后再试",
			DetailHint: "该账号所属站点处于禁用状态，任务会自动跳过。",
		}
	}

	// Priority 2: Checkin not supported
	if includesAny(text, []string{
		"checkin endpoint not found", "签到端点不存在", "站点不支持签到",
		"not support checkin", "check-in is not supported",
		"checkin is not supported", "does not support checkin",
	}) {
		return FailureReason{
			Code: CodeCheckinNotSupported, Category: CategorySite,
			Title: "站点未开启签到", ActionHint: "无需重试（非故障）",
			DetailHint: "该站点未提供签到端点，账号会被自动跳过。",
		}
	}

	// Priority 3: Manual Turnstile required
	if includesAny(text, []string{"turnstile token 为空", "turnstile"}) &&
		includesAny(text, []string{"校验", "token", "验证", "manual"}) {
		return FailureReason{
			Code: CodeManualTurnstileRequired, Category: CategoryVerification,
			Title: "需要人工验证", ActionHint: "浏览器先人工签到一次",
			DetailHint: "站点开启了 Turnstile 人机验证，自动签到无法直接通过。",
		}
	}

	// Priority 4: Cloudflare tunnel unavailable
	if includesAny(text, []string{"cloudflare tunnel error", "error 1033", "unable to resolve it"}) {
		return FailureReason{
			Code: CodeCloudflareTunnelUnavail, Category: CategoryNetwork,
			Title: "站点隧道不可用", ActionHint: "稍后重试或联系站点方",
			DetailHint: "Cloudflare Tunnel 当前不可达，通常是站点侧网络或隧道进程问题。",
		}
	}

	// Priority 5: Cloudflare challenge
	if alert.IsCloudflareChallenge(rawMessage) {
		return FailureReason{
			Code: CodeCloudflareChallenge, Category: CategoryVerification,
			Title: "触发 Cloudflare 验证", ActionHint: "降低频率并稍后重试",
			DetailHint: "请求触发了防护挑战，建议稍后再试或更换稳定站点。",
		}
	}

	// Priority 6: Token expired
	var tokStatus int
	if httpStatus > 0 {
		tokStatus = httpStatus
	}
	if alert.IsTokenExpiredError(tokStatus, rawMessage) {
		return FailureReason{
			Code: CodeTokenExpired, Category: CategoryAuth,
			Title: "令牌失效", ActionHint: "重新登录或同步新令牌",
			DetailHint: "账号访问令牌可能过期或无效，需更新认证信息。",
		}
	}

	// Priority 7: Already checked in
	if includesAny(text, []string{"already checked in", "already signed", "今天已经签到", "今日已签到", "已经签到"}) {
		return FailureReason{
			Code: CodeAlreadyCheckedIn, Category: CategoryState,
			Title: "今日已签到", ActionHint: "无需重复执行",
			DetailHint: "该账号当天签到已完成，重复请求会被站点拒绝或跳过。",
		}
	}

	// Priority 8: Network timeout
	if includesAny(text, []string{"timeout", "timed out", "etimedout", "请求超时"}) {
		return FailureReason{
			Code: CodeNetworkTimeout, Category: CategoryNetwork,
			Title: "请求超时", ActionHint: "稍后重试并检查网络",
			DetailHint: "请求在超时时间内未完成，可能是网络波动或站点响应慢。",
		}
	}

	// Priority 9: Upstream error
	if httpStatus >= 500 || includesAny(text, []string{"http 5", "upstream", "internal server error"}) {
		return FailureReason{
			Code: CodeUpstreamError, Category: CategorySite,
			Title: "上游站点错误", ActionHint: "稍后重试",
			DetailHint: "站点返回服务端错误，通常需要站点恢复后才可成功。",
		}
	}

	// Priority 10: Unknown
	if status == "success" {
		return FailureReason{
			Code: CodeUnknownError, Category: CategoryUnknown,
			Title: "执行成功", ActionHint: "无需操作",
			DetailHint: "任务已成功完成。",
		}
	}
	return FailureReason{
		Code: CodeUnknownError, Category: CategoryUnknown,
		Title: "未知错误", ActionHint: "查看详细日志后重试",
		DetailHint: "暂未识别到明确错误类型，可根据原始信息进一步排查。",
	}
}

func includesAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
