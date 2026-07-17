package alert

import (
	"testing"
)

// ---- IsCloudflareChallenge Tests ----

func TestIsCloudflareChallenge_Positive(t *testing.T) {
	cases := []string{
		"Cloudflare protection triggered",
		"cf challenge detected",
		"challenge required",
		"CLOUDFLARE",
		"CF CHALLENGE",       // case insensitive
		"Challenge Required", // case insensitive
		"site has cloudflare CDN",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !IsCloudflareChallenge(msg) {
				t.Errorf("expected true for: %q", msg)
			}
		})
	}
}

func TestIsCloudflareChallenge_Negative(t *testing.T) {
	cases := []string{
		"",
		" ",
		"normal error",
		"connection timeout",
		"token expired",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if IsCloudflareChallenge(msg) {
				t.Errorf("expected false for: %q", msg)
			}
		})
	}
}

func TestIsCloudflareChallenge_WhitespaceOnly(t *testing.T) {
	if IsCloudflareChallenge("   ") {
		t.Error("expected false for whitespace only")
	}
}

// ---- IsTokenExpiredError Tests ----

func TestIsTokenExpiredError_DirectPatterns(t *testing.T) {
	cases := []string{
		"jwt expired",
		"token expired",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !IsTokenExpiredError(0, msg) {
				t.Errorf("expected true for: %q", msg)
			}
		})
	}
}

func TestIsTokenExpiredError_InvalidTokenPatterns(t *testing.T) {
	cases := []string{
		"invalid access token",
		"access token is invalid",
		"access token无效",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !IsTokenExpiredError(0, msg) {
				t.Errorf("expected true for: %q", msg)
			}
		})
	}
}

func TestIsTokenExpiredError_ChineseTokenPatterns(t *testing.T) {
	cases := []string{
		"访问令牌无效", // token + invalid
		"令牌已过期",  // token + expired
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !IsTokenExpiredError(0, msg) {
				t.Errorf("expected true for: %q", msg)
			}
		})
	}
}

func TestIsTokenExpiredError_HTTPStatus(t *testing.T) {
	// HTTP 401 directly
	if !IsTokenExpiredError(401, "") {
		t.Error("expected true for HTTP 401 status")
	}

	// HTTP 401 in message
	if !IsTokenExpiredError(0, "HTTP 401 Unauthorized") {
		t.Error("expected true for message containing HTTP 401")
	}
}

func TestIsTokenExpiredError_Excludes(t *testing.T) {
	// Dispatch denied should NOT be treated as token expired
	if IsTokenExpiredError(0, "does not allow /v1/chat/completions dispatch") {
		t.Error("dispatch denied should NOT be token expired")
	}
	if IsTokenExpiredError(0, "A dispatch denied error occurred") {
		t.Error("dispatch denied (lowercase) should NOT be token expired")
	}

	// NewAPI specific: "未登录且未提供 access token" should NOT be token expired
	if IsTokenExpiredError(0, "未登录且未提供 access token") {
		t.Error("'未登录且未提供 access token' should NOT be token expired (NewAPI specific)")
	}
}

func TestIsTokenExpiredError_ExcludesNonAuthUpstreamFailures(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		message    string
	}{
		{
			name:       "invalid_argument input token limit is request validation",
			httpStatus: 400,
			message:    "Error code: 400 - {'error': {'code': 'invalid_argument', 'message': 'input token limit is 202752', 'type': 'invalid_request_error'}}",
		},
		{
			name:       "401 model not supported is capability failure",
			httpStatus: 401,
			message:    "Model minimax-m3-free is not supported for format openai",
		},
		{
			name:       "401 billing failure is not token expiry",
			httpStatus: 401,
			message:    "No payment method. Add a payment method here: https://example.com/billing",
		},
		{
			name:       "message with HTTP 401 model unsupported is not token expiry",
			httpStatus: 0,
			message:    "HTTP 401 - Model gemini-3.1-pro-preview is not supported",
		},
		{
			name:       "insufficient_quota is billing not expired",
			httpStatus: 429,
			message:    "You exceeded your current quota, please check your plan and billing details.",
		},
		{
			name:       "rate limit is not expired",
			httpStatus: 429,
			message:    "rate limit exceeded",
		},
		{
			name:       "opaque 401 without auth signal is not expired",
			httpStatus: 401,
			message:    "upstream rejected the request",
		},
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

func TestIsTokenExpiredError_EmptyMessage(t *testing.T) {
	if IsTokenExpiredError(0, "") {
		t.Error("empty message should not be token expired")
	}
	if IsTokenExpiredError(0, "  ") {
		t.Error("whitespace message should not be token expired")
	}
}

func TestIsTokenExpiredError_Negative(t *testing.T) {
	cases := []string{
		"connection timeout",
		"checkin success",
		"already checked in",
		"",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if IsTokenExpiredError(0, msg) {
				t.Errorf("expected false for: %q", msg)
			}
		})
	}
}

// ---- AppendSessionTokenRebindHint Tests ----

func TestAppendSessionTokenRebindHint_Appends(t *testing.T) {
	cases := []string{
		"invalid access token",
		"access token is invalid",
		"无权进行此操作，access token 无效",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			result := AppendSessionTokenRebindHint(msg)
			if result == msg {
				t.Errorf("expected hint to be appended for: %q", msg)
			}
			if len(result) <= len(msg) {
				t.Errorf("result (%q) should be longer than input (%q)", result, msg)
			}
		})
	}
}

func TestAppendSessionTokenRebindHint_NoAppend(t *testing.T) {
	cases := []string{
		"connection timeout",
		"checkin success",
		"already checked in",
		"",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			result := AppendSessionTokenRebindHint(msg)
			if result != msg {
				t.Errorf("hint should NOT be appended for: %q, got %q", msg, result)
			}
		})
	}
}

func TestAppendSessionTokenRebindHint_NoDoubleAppend(t *testing.T) {
	hint := "请在中转站重新生成系统访问令牌后重新绑定账号"
	// Message already contains the hint
	msg1 := "invalid access token，" + hint
	result1 := AppendSessionTokenRebindHint(msg1)
	if result1 != msg1 {
		t.Errorf("should not double-append hint: got %q", result1)
	}

	// Message already contains the hint fully
	msg2 := "access token is invalid，" + hint
	result2 := AppendSessionTokenRebindHint(msg2)
	if result2 != msg2 {
		t.Errorf("should not double-append hint: got %q", result2)
	}
}

func TestAppendSessionTokenRebindHint_EmptyInput(t *testing.T) {
	result := AppendSessionTokenRebindHint("")
	if result != "" {
		t.Errorf("expected empty string for empty input, got %q", result)
	}
}

func TestAppendSessionTokenRebindHint_WhitespaceOnly(t *testing.T) {
	t.Run("whitespace-only returns empty", func(t *testing.T) {
		result := AppendSessionTokenRebindHint("   ")
		// TrimSpace turns "   " into "", so result is "" (empty string normalization)
		if result != "" {
			t.Errorf("expected empty string for whitespace-only input, got %q", result)
		}
	})

	// Also test the whitespace case more directly
	t.Run("empty returns empty", func(t *testing.T) {
		result := AppendSessionTokenRebindHint("")
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})
}

// ---- isEndpointDispatchDeniedMessage Tests (internal, tested via IsTokenExpiredError) ----

func TestEndpointDispatchDeniedExclusion(t *testing.T) {
	// These should NOT be classified as token_expired even if they contain "token"
	cases := []string{
		"does not allow /v1/chat/completions dispatch",
		"API does not allow /v1/images/generations dispatch for this model",
		"dispatch denied by policy",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if IsTokenExpiredError(0, msg) {
				t.Errorf("dispatch denied should NOT be token expired: %q", msg)
			}
		})
	}
}

// ---- orID Tests ----

func TestOrID(t *testing.T) {
	name := "testuser"
	empty := ""

	if got := orID(&name, 123); got != "testuser" {
		t.Errorf("orID(valid) = %q, want 'testuser'", got)
	}
	if got := orID(nil, 456); got != "ID:456" {
		t.Errorf("orID(nil) = %q, want 'ID:456'", got)
	}
	if got := orID(&empty, 789); got != "ID:789" {
		t.Errorf("orID(empty) = %q, want 'ID:789'", got)
	}
}

// ---- TokenExpiredParams Tests ----

func TestTokenExpiredParams(t *testing.T) {
	username := "testuser"
	siteName := "example.com"
	params := TokenExpiredParams{
		AccountID: 1,
		Username:  &username,
		SiteName:  &siteName,
		Detail:    "token expired",
	}
	if params.AccountID != 1 {
		t.Errorf("AccountID = %d, want 1", params.AccountID)
	}
	if *params.Username != "testuser" {
		t.Errorf("Username = %q, want 'testuser'", *params.Username)
	}
	if *params.SiteName != "example.com" {
		t.Errorf("SiteName = %q, want 'example.com'", *params.SiteName)
	}
	if params.Detail != "token expired" {
		t.Errorf("Detail = %q, want 'token expired'", params.Detail)
	}
}

func TestTokenExpiredParams_NilFields(t *testing.T) {
	params := TokenExpiredParams{
		AccountID: 1,
		Detail:    "error",
	}
	if params.Username != nil {
		t.Error("Username should be nil")
	}
	if params.SiteName != nil {
		t.Error("SiteName should be nil")
	}
}

// ---- ProxyAllFailedParams Tests ----

func TestProxyAllFailedParams(t *testing.T) {
	params := ProxyAllFailedParams{
		Model:  "gpt-4",
		Reason: "all channels exhausted",
	}
	if params.Model != "gpt-4" {
		t.Errorf("Model = %q, want 'gpt-4'", params.Model)
	}
	if params.Reason != "all channels exhausted" {
		t.Errorf("Reason = %q, want 'all channels exhausted'", params.Reason)
	}
}

func TestShouldMarkAccountExpired_NotTransientOrBilling(t *testing.T) {
	// Non-auth / transient must never mark accounts expired (#568/#298).
	negatives := []struct {
		status int
		msg    string
	}{
		{0, "connection timeout"},
		{0, "i/o timeout"},
		{429, "rate limit exceeded"},
		{500, "internal server error"},
		{502, "bad gateway"},
		{503, "service unavailable"},
		{401, "insufficient_quota"},
		{401, "billing hard limit reached"},
		{403, "model is not supported"},
		{400, "invalid_request_error"},
		{0, "cloudflare challenge required"},
	}
	for _, tc := range negatives {
		if ShouldMarkAccountExpired(tc.status, tc.msg) {
			t.Fatalf("ShouldMarkAccountExpired(%d, %q) = true, want false", tc.status, tc.msg)
		}
	}
}

func TestShouldMarkAccountExpired_PositiveAuthExpiry(t *testing.T) {
	positives := []struct {
		status int
		msg    string
	}{
		{0, "jwt expired"},
		{0, "token expired"},
		{0, "invalid access token"},
		{401, ""}, // bare 401 empty body — legacy expired
		{0, "访问令牌无效"},
	}
	for _, tc := range positives {
		if !ShouldMarkAccountExpired(tc.status, tc.msg) {
			t.Fatalf("ShouldMarkAccountExpired(%d, %q) = false, want true", tc.status, tc.msg)
		}
	}
}
