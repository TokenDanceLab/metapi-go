package platform

import "testing"

func TestClassifyUpstreamError_Matrix(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		message    string
		wantClass  UpstreamErrorClass
		wantMark   bool
	}{
		// expired / mark
		{
			name:       "jwt expired marks",
			httpStatus: 0,
			message:    "jwt expired",
			wantClass:  ClassExpired,
			wantMark:   true,
		},
		{
			name:       "token expired marks",
			httpStatus: 0,
			message:    "token expired",
			wantClass:  ClassExpired,
			wantMark:   true,
		},
		{
			name:       "invalid access token marks",
			httpStatus: 0,
			message:    "invalid access token",
			wantClass:  ClassExpired,
			wantMark:   true,
		},
		{
			name:       "chinese token expired marks",
			httpStatus: 0,
			message:    "令牌已过期",
			wantClass:  ClassExpired,
			wantMark:   true,
		},
		{
			name:       "bare 401 empty body marks (legacy)",
			httpStatus: 401,
			message:    "",
			wantClass:  ClassExpired,
			wantMark:   true,
		},
		{
			name:       "HTTP 401 Unauthorized marks",
			httpStatus: 0,
			message:    "HTTP 401 Unauthorized",
			wantClass:  ClassExpired,
			wantMark:   true,
		},

		// validation — must NOT mark
		{
			name:       "invalid_argument input token limit is validation",
			httpStatus: 400,
			message:    "Error code: 400 - {'error': {'code': 'invalid_argument', 'message': 'input token limit is 202752', 'type': 'invalid_request_error'}}",
			wantClass:  ClassValidation,
			wantMark:   false,
		},
		{
			name:       "401 body that is only validation must not mark",
			httpStatus: 401,
			message:    "invalid_request_error: max_tokens is too large",
			wantClass:  ClassValidation,
			wantMark:   false,
		},
		{
			name:       "dispatch denied is validation",
			httpStatus: 403,
			message:    "does not allow /v1/chat/completions dispatch",
			wantClass:  ClassValidation,
			wantMark:   false,
		},

		// model — must NOT mark
		{
			name:       "401 model not supported is capability failure",
			httpStatus: 401,
			message:    "Model minimax-m3-free is not supported for format openai",
			wantClass:  ClassModel,
			wantMark:   false,
		},
		{
			name:       "message with HTTP 401 model unsupported is not token expiry",
			httpStatus: 0,
			message:    "HTTP 401 - Model gemini-3.1-pro-preview is not supported",
			wantClass:  ClassModel,
			wantMark:   false,
		},
		{
			name:       "chinese model unsupported is not token expiry",
			httpStatus: 400,
			message:    "当前API不支持所选模型",
			wantClass:  ClassModel,
			wantMark:   false,
		},

		// billing — must NOT mark
		{
			name:       "401 billing failure is not token expiry",
			httpStatus: 401,
			message:    "No payment method. Add a payment method here: https://example.com/billing",
			wantClass:  ClassBilling,
			wantMark:   false,
		},
		{
			name:       "insufficient_quota is billing not expired",
			httpStatus: 429,
			message:    "You exceeded your current quota, please check your plan and billing details.",
			wantClass:  ClassBilling,
			wantMark:   false,
		},
		{
			name:       "chinese balance insufficient is billing",
			httpStatus: 403,
			message:    "账户余额不足，请充值",
			wantClass:  ClassBilling,
			wantMark:   false,
		},

		// transient — must NOT mark
		{
			name:       "rate limit is transient",
			httpStatus: 429,
			message:    "rate limit exceeded",
			wantClass:  ClassTransient,
			wantMark:   false,
		},
		{
			name:       "timeout is transient",
			httpStatus: 0,
			message:    "request timed out",
			wantClass:  ClassTransient,
			wantMark:   false,
		},
		{
			name:       "5xx is transient",
			httpStatus: 502,
			message:    "bad gateway",
			wantClass:  ClassTransient,
			wantMark:   false,
		},
		{
			name:       "cloudflare challenge is transient not expired",
			httpStatus: 403,
			message:    "Cloudflare challenge required",
			wantClass:  ClassTransient,
			wantMark:   false,
		},

		// auth residual / unknown — must NOT mark
		{
			name:       "newapi missing access token is auth not expired mark",
			httpStatus: 401,
			message:    "未登录且未提供 access token",
			wantClass:  ClassAuth,
			wantMark:   false,
		},
		{
			name:       "opaque 401 body without auth signal does not mark",
			httpStatus: 401,
			message:    "upstream rejected the request",
			wantClass:  ClassUnknown,
			wantMark:   false,
		},
		{
			name:       "input token alone is not credential expiry",
			httpStatus: 0,
			message:    "input token count too high for model",
			wantClass:  ClassUnknown,
			wantMark:   false,
		},
		{
			name:       "connection timeout unknown/transient path",
			httpStatus: 0,
			message:    "connection timeout",
			wantClass:  ClassTransient,
			wantMark:   false,
		},
		{
			name:       "empty message without status is unknown",
			httpStatus: 0,
			message:    "",
			wantClass:  ClassUnknown,
			wantMark:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotClass := ClassifyUpstreamError(tc.httpStatus, tc.message)
			if gotClass != tc.wantClass {
				t.Fatalf("ClassifyUpstreamError(%d, %q) = %q, want %q",
					tc.httpStatus, tc.message, gotClass, tc.wantClass)
			}
			gotMark := ShouldMarkAccountExpired(tc.httpStatus, tc.message)
			if gotMark != tc.wantMark {
				t.Fatalf("ShouldMarkAccountExpired(%d, %q) = %v, want %v (class=%s)",
					tc.httpStatus, tc.message, gotMark, tc.wantMark, gotClass)
			}
			// IsTokenExpiredError is the historical mark/relogin gate; keep aligned with mark.
			if got := IsTokenExpiredError(tc.httpStatus, tc.message); got != tc.wantMark {
				t.Fatalf("IsTokenExpiredError(%d, %q) = %v, want %v",
					tc.httpStatus, tc.message, got, tc.wantMark)
			}
		})
	}
}

func TestIsTokenExpiredError_NonAuthUpstreamNeverMarks(t *testing.T) {
	// Table focused on the #24 false-positive guard: non-auth upstream errors
	// must never be treated as token expiry for accounts.status='expired'.
	cases := []struct {
		name       string
		httpStatus int
		message    string
	}{
		{"validation invalid_argument", 400, "invalid_argument: input token limit is 202752"},
		{"validation invalid_request_error", 400, "type: invalid_request_error"},
		{"validation context length", 400, "This model's maximum context length is 128000 tokens"},
		{"model unsupported openai format", 401, "Model foo is not supported for format openai"},
		{"model unsupported generic", 401, "HTTP 401 - Model bar is not supported"},
		{"model not found", 404, "model_not_found: no such model"},
		{"billing payment method", 401, "No payment method. Add a payment method here: https://example.com/billing"},
		{"billing insufficient quota", 429, "insufficient_quota: You exceeded your current quota"},
		{"billing chinese balance", 403, "余额不足"},
		{"rate limit", 429, "Rate limit reached for requests"},
		{"too many requests", 429, "too many requests"},
		{"dispatch denied", 403, "site does not allow /v1/chat/completions dispatch"},
		{"dispatch denied phrase", 403, "A dispatch denied error occurred"},
		{"newapi missing access token", 401, "未登录且未提供 access token"},
		{"cloudflare", 403, "cf challenge detected"},
		{"timeout", 0, "request timed out"},
		{"5xx", 502, "bad gateway from upstream"},
		{"opaque 401", 401, "upstream rejected the request"},
		{"bare token word without auth", 0, "input token encoding failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsTokenExpiredError(tc.httpStatus, tc.message) {
				t.Fatalf("IsTokenExpiredError(%d, %q) = true, want false", tc.httpStatus, tc.message)
			}
			if ShouldMarkAccountExpired(tc.httpStatus, tc.message) {
				t.Fatalf("ShouldMarkAccountExpired(%d, %q) = true, want false", tc.httpStatus, tc.message)
			}
		})
	}
}

func TestIsTokenExpiredError_PositiveAuthSignals(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		message    string
	}{
		{"jwt expired", 0, "jwt expired"},
		{"token expired", 0, "token expired"},
		{"invalid access token", 0, "invalid access token"},
		{"access token is invalid", 0, "access token is invalid"},
		{"access token chinese invalid", 0, "access token无效"},
		{"访问令牌无效", 0, "访问令牌无效"},
		{"令牌已过期", 0, "令牌已过期"},
		{"bare 401", 401, ""},
		{"HTTP 401 Unauthorized", 0, "HTTP 401 Unauthorized"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsTokenExpiredError(tc.httpStatus, tc.message) {
				t.Fatalf("IsTokenExpiredError(%d, %q) = false, want true", tc.httpStatus, tc.message)
			}
			if !ShouldMarkAccountExpired(tc.httpStatus, tc.message) {
				t.Fatalf("ShouldMarkAccountExpired(%d, %q) = false, want true", tc.httpStatus, tc.message)
			}
		})
	}
}
