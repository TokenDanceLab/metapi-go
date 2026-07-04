package proxy

import "testing"

func TestShouldRetryProxyRequest_StatusCodes(t *testing.T) {
	t.Run(">= 500 always retryable", func(t *testing.T) {
		for _, status := range []int{500, 502, 503, 504, 599} {
			if !ShouldRetryProxyRequest(status, "") {
				t.Errorf("expected retryable for status %d", status)
			}
		}
	})

	t.Run("408 always retryable", func(t *testing.T) {
		if !ShouldRetryProxyRequest(408, "") {
			t.Error("expected retryable for 408")
		}
	})

	t.Run("409 always retryable", func(t *testing.T) {
		if !ShouldRetryProxyRequest(409, "") {
			t.Error("expected retryable for 409")
		}
	})

	t.Run("425 always retryable", func(t *testing.T) {
		if !ShouldRetryProxyRequest(425, "") {
			t.Error("expected retryable for 425")
		}
	})

	t.Run("429 always retryable", func(t *testing.T) {
		if !ShouldRetryProxyRequest(429, "") {
			t.Error("expected retryable for 429")
		}
	})

	t.Run("401 retryable (OAuth refresh)", func(t *testing.T) {
		if !ShouldRetryProxyRequest(401, "") {
			t.Error("expected retryable for 401")
		}
	})

	t.Run("403 retryable (OAuth refresh)", func(t *testing.T) {
		if !ShouldRetryProxyRequest(403, "") {
			t.Error("expected retryable for 403")
		}
	})

	t.Run("400 non-retryable by default", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "") {
			t.Error("expected non-retryable for 400")
		}
	})

	t.Run("404 non-retryable by default", func(t *testing.T) {
		if ShouldRetryProxyRequest(404, "") {
			t.Error("expected non-retryable for 404")
		}
	})

	t.Run("422 non-retryable by default", func(t *testing.T) {
		if ShouldRetryProxyRequest(422, "") {
			t.Error("expected non-retryable for 422")
		}
	})

	t.Run("other status non-retryable", func(t *testing.T) {
		if ShouldRetryProxyRequest(302, "") {
			t.Error("expected non-retryable for 302")
		}
	})
}

func TestShouldRetryProxyRequest_ModelUnsupportedPatterns(t *testing.T) {
	t.Run("model not supported", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "model not supported") {
			t.Error("expected retryable for model-unsupported error")
		}
	})

	t.Run("does not support the model", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "This endpoint does not support the model gpt-5") {
			t.Error("expected retryable for 'does not support the model'")
		}
	})

	t.Run("does not support model (without 'the')", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "OpenAI does not support model gpt-5") {
			t.Error("expected retryable for 'does not support model'")
		}
	})

	t.Run("model not found", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "model_not_found") {
			t.Error("expected retryable for 'model_not_found'")
		}
	})

	t.Run("unknown model", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "unknown model: gpt-5") {
			t.Error("expected retryable for 'unknown model'")
		}
	})

	t.Run("invalid model", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "invalid model specified") {
			t.Error("expected retryable for 'invalid model'")
		}
	})

	t.Run("do not have access to the model", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "you do not have access to the model gpt-4") {
			t.Error("expected retryable for access denied to model")
		}
	})

	t.Run("Chinese model unsupported", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "当前 api 不支持所选模型") {
			t.Error("expected retryable for Chinese model unsupported")
		}
	})

	t.Run("Chinese model not supported (variant)", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "不支持所选模型") {
			t.Error("expected retryable for Chinese model unsupported variant")
		}
	})

	t.Run("no such model", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "no such model") {
			t.Error("expected retryable for 'no such model'")
		}
	})

	t.Run("unknown provider for model", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "unknown provider for model: gpt-4") {
			t.Error("expected retryable for unknown provider")
		}
	})
}

func TestShouldRetryProxyRequest_NonRetryableRequestPatterns(t *testing.T) {
	t.Run("invalid request body blocks retry", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "invalid request body") {
			t.Error("expected non-retryable for 'invalid request body'")
		}
	})

	t.Run("validation error blocks retry", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "validation error: field x is required") {
			t.Error("expected non-retryable for validation error")
		}
	})

	t.Run("malformed request blocks retry", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "malformed request") {
			t.Error("expected non-retryable for malformed request")
		}
	})

	t.Run("invalid json blocks retry", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "invalid json in request body") {
			t.Error("expected non-retryable for invalid json")
		}
	})

	t.Run("cannot parse blocks retry", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "cannot parse request") {
			t.Error("expected non-retryable for cannot parse")
		}
	})

	t.Run("500 status always retryable regardless of text", func(t *testing.T) {
		// Status >= 500 always retryable - checked BEFORE text patterns
		if !ShouldRetryProxyRequest(500, "invalid request body") {
			t.Error("expected retryable for 500 (status >= 500 always retryable)")
		}
	})

	t.Run("non-retryable patterns take priority over UNSUPPORTED MODEL patterns", func(t *testing.T) {
		// The non-retryable check comes BEFORE model-unsupported check in flow
		// Wait - actually re-reading the code: model_unsupported is checked FIRST
		// Let me verify...
		// Looking at ShouldRetryProxyRequest:
		// 1. status >= 500 -> true (returns immediately)
		// 2. 408/409/425/429 -> true
		// 3. 401/403 -> true
		// 4. model_unsupported text -> true (returns before non-retryable check)
		// 5. non_retryable_request text -> false
		// ...
		// So if text matches BOTH model_unsupported AND non_retryable, model_unsupported wins
		if !ShouldRetryProxyRequest(400, "does not support the model but also validation error") {
			t.Error("model unsupported pattern wins over non-retryable patterns")
		}
	})
}

func TestShouldRetryProxyRequest_RetryableChannelLocalPatterns(t *testing.T) {
	t.Run("invalid api key", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "invalid api key") {
			t.Error("expected retryable for 'invalid api key'")
		}
	})

	t.Run("forbidden", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "forbidden") {
			t.Error("expected retryable for 'forbidden'")
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "rate limit exceeded") {
			t.Error("expected retryable for 'rate limit'")
		}
	})

	t.Run("quota exceeded", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "quota exceeded") {
			t.Error("expected retryable for 'quota exceeded'")
		}
	})

	t.Run("bad gateway", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "bad gateway error") {
			t.Error("expected retryable for 'bad gateway'")
		}
	})

	t.Run("gateway timeout", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "gateway timeout") {
			t.Error("expected retryable for 'gateway timeout'")
		}
	})

	t.Run("gateway time-out pattern", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "gateway time-out") {
			t.Error("expected retryable for 'gateway time-out'")
		}
	})

	t.Run("service unavailable", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "service unavailable") {
			t.Error("expected retryable for 'service unavailable'")
		}
	})

	t.Run("please use /v1/responses", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "please use /v1/responses") {
			t.Error("expected retryable for protocol switch hint")
		}
	})

	t.Run("please use /v1/messages", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "please use /v1/messages") {
			t.Error("expected retryable for messages switch hint")
		}
	})

	t.Run("please use /v1/chat/completions", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "please use /v1/chat/completions") {
			t.Error("expected retryable for chat/completions switch hint")
		}
	})

	t.Run("unsupported endpoint", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "unsupported endpoint") {
			t.Error("expected retryable for unsupported endpoint")
		}
	})

	t.Run("cpu overloaded", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "cpu overloaded") {
			t.Error("expected retryable for cpu overloaded")
		}
	})

	t.Run("invalid access token", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "invalid access token") {
			t.Error("expected retryable for invalid access token")
		}
	})

	t.Run("unsupported legacy protocol", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "unsupported legacy protocol") {
			t.Error("expected retryable for unsupported legacy protocol")
		}
	})

	t.Run("unrecognized request url", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "unrecognized request url") {
			t.Error("expected retryable for unrecognized request url")
		}
	})

	t.Run("no route matched", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "no route matched") {
			t.Error("expected retryable for no route matched")
		}
	})

	t.Run("connection timed out", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "connection timed out") {
			t.Error("expected retryable for connection timed out")
		}
	})

	t.Run("request timed out", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "request timed out") {
			t.Error("expected retryable for request timed out")
		}
	})

	t.Run("read timeout", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "read timeout") {
			t.Error("expected retryable for read timeout")
		}
	})

	t.Run("timed out", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "the operation timed out") {
			t.Error("expected retryable for 'timed out'")
		}
	})
}

func TestShouldRetryProxyRequest_EdgeCases(t *testing.T) {
	t.Run("400 with retryable channel-local text", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "quota exceeded on this channel") {
			t.Error("expected retryable for 400 with retryable text")
		}
	})

	t.Run("400 without any retryable text", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "some generic error") {
			t.Error("expected non-retryable for 400 with generic text")
		}
	})

	t.Run("404 without any retryable text", func(t *testing.T) {
		if ShouldRetryProxyRequest(404, "not found") {
			t.Error("expected non-retryable for 404 with generic text")
		}
	})

	t.Run("422 without any retryable text", func(t *testing.T) {
		if ShouldRetryProxyRequest(422, "unprocessable") {
			t.Error("expected non-retryable for 422 with generic text")
		}
	})

	t.Run("empty error text", func(t *testing.T) {
		if ShouldRetryProxyRequest(400, "") {
			t.Error("expected non-retryable for 400 with empty text")
		}
	})

	t.Run("case-insensitive matching", func(t *testing.T) {
		if !ShouldRetryProxyRequest(400, "Model Not Supported") {
			t.Error("expected retryable with case-insensitive model unsupported")
		}
	})
}

func TestShouldAbortSameSiteEndpointFallback(t *testing.T) {
	t.Run("500 with retryable pattern", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(500, "service unavailable") {
			t.Error("expected abort for 500 + service unavailable")
		}
	})

	t.Run("429 with rate limit", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(429, "rate limit exceeded") {
			t.Error("expected abort for 429 + rate limit")
		}
	})

	t.Run("408 with timeout", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(408, "connection timed out") {
			t.Error("expected abort for 408 + timeout")
		}
	})

	t.Run("400 should not abort", func(t *testing.T) {
		if ShouldAbortSameSiteEndpointFallback(400, "rate limit exceeded") {
			t.Error("expected NO abort for 400 even with rate limit text")
		}
	})

	t.Run("503 with matching pattern", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(503, "bad gateway") {
			t.Error("expected abort for 503 + bad gateway")
		}
	})

	t.Run("502 with non-matching text does not abort", func(t *testing.T) {
		// Both status and pattern must match
		if ShouldAbortSameSiteEndpointFallback(502, "some other error") {
			t.Error("expected NO abort for 502 with non-matching pattern")
		}
	})

	t.Run("502 with matching pattern does abort", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(502, "service unavailable") {
			t.Error("expected abort for 502 + service unavailable")
		}
	})

	t.Run("connection reset", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(500, "connection reset by peer") {
			t.Error("expected abort for connection reset")
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(500, "connection refused") {
			t.Error("expected abort for connection refused")
		}
	})

	t.Run("econnreset", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(500, "econnreset") {
			t.Error("expected abort for econnreset")
		}
	})

	t.Run("temporarily unavailable", func(t *testing.T) {
		if !ShouldAbortSameSiteEndpointFallback(500, "service temporarily unavailable") {
			t.Error("expected abort for temporarily unavailable")
		}
	})
}

func TestGetProxyMaxChannelAttempts(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{0, 1},
		{-1, 1},
		{-100, 1},
		{1, 1},
		{3, 3},
		{10, 10},
	}

	for _, tt := range tests {
		got := GetProxyMaxChannelAttempts(tt.input)
		if got != tt.expected {
			t.Errorf("GetProxyMaxChannelAttempts(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestGetProxyMaxChannelRetries(t *testing.T) {
	tests := []struct {
		attempts int
		expected int
	}{
		{1, 0},  // 1 attempt, 0 retries
		{2, 1},  // 2 attempts, 1 retry
		{3, 2},  // 3 attempts, 2 retries
		{5, 4},  // 5 attempts, 4 retries
		{0, 0},  // 0 attempts -> 0 retries (min)
	}

	for _, tt := range tests {
		got := GetProxyMaxChannelRetries(tt.attempts)
		if got != tt.expected {
			t.Errorf("GetProxyMaxChannelRetries(%d) = %d, want %d", tt.attempts, got, tt.expected)
		}
	}
}
