package proxy

import (
	"testing"
)

// ---- Helpers ----

func makeSuccessResponse(status int, body string) *ExecutorDispatchResult {
	return &ExecutorDispatchResult{
		Status:  status,
		Headers: map[string]string{},
		Body:    []byte(body),
	}
}

func makeErrorResponse(status int, body string) *ExecutorDispatchResult {
	return &ExecutorDispatchResult{
		Status:  status,
		Headers: map[string]string{},
		Body:    []byte(body),
	}
}

func makeTimeoutResponse() *ExecutorDispatchResult {
	return &ExecutorDispatchResult{
		Status:  0,
		Headers: map[string]string{},
		Body:    nil,
	}
}

func makeEndpointCandidates(endpoints ...UpstreamEndpoint) []UpstreamEndpoint {
	return endpoints
}

// ---- Tests ----

func TestBuildUpstreamURL(t *testing.T) {
	tests := []struct {
		siteURL string
		path    string
		want    string
	}{
		{"https://api.example.com", "/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"https://api.example.com/", "/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"https://api.example.com", "v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"https://api.example.com/path", "/v1/messages", "https://api.example.com/path/v1/messages"},
		{"https://api.example.com/path/", "/v1/messages", "https://api.example.com/path/v1/messages"},
		{"https://api.example.com/v1", "/v1/chat/completions", "https://api.example.com/v1/chat/completions"},
		{"https://ark.cn-beijing.volces.com/api/v3", "/v1/chat/completions", "https://ark.cn-beijing.volces.com/api/v3/chat/completions"},
		{"https://api.example.com/api/v3/", "v1/responses", "https://api.example.com/api/v3/responses"},
	}

	for _, tt := range tests {
		got := BuildUpstreamURL(tt.siteURL, tt.path)
		if got != tt.want {
			t.Errorf("BuildUpstreamURL(%q, %q) = %q, want %q", tt.siteURL, tt.path, got, tt.want)
		}
	}
}

func TestSummarizeUpstreamError(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		got := SummarizeUpstreamError(500, "")
		if got != "HTTP 500" {
			t.Errorf("got %q, want 'HTTP 500'", got)
		}
	})

	t.Run("short text", func(t *testing.T) {
		got := SummarizeUpstreamError(404, "Not Found")
		if got != "HTTP 404: Not Found" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("long text truncated", func(t *testing.T) {
		longText := ""
		for i := 0; i < 300; i++ {
			longText += "x"
		}
		got := SummarizeUpstreamError(502, longText)
		if len(got) > 215 {
			t.Errorf("expected truncated, got length %d", len(got))
		}
	})
}

func TestExecuteEndpointFlow_Success(t *testing.T) {
	t.Run("success on first endpoint", func(t *testing.T) {
		onSuccess := false
		onFailure := false

		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{
					Endpoint: endpoint,
					Path:     "/v1/chat/completions",
				}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeSuccessResponse(200, `{"choices":[{"message":{"content":"hello"}}]}`), nil
			},
			OnAttemptSuccess: func(ctx EndpointAttemptSuccessContext) {
				onSuccess = true
				if ctx.EndpointIndex != 0 {
					t.Errorf("expected endpoint index 0, got %d", ctx.EndpointIndex)
				}
			},
			OnAttemptFailure: func(ctx EndpointAttemptContext) {
				onFailure = true
			},
		})

		if !result.OK {
			t.Error("expected success result")
		}
		if !onSuccess {
			t.Error("expected OnAttemptSuccess to be called")
		}
		if onFailure {
			t.Error("expected OnAttemptFailure NOT to be called")
		}
	})

	t.Run("success on second endpoint via downgrade", func(t *testing.T) {
		count := 0
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointResponses, EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				count++
				if request.Endpoint == EndpointResponses {
					return makeErrorResponse(400, "please use /v1/chat/completions"), nil
				}
				return makeSuccessResponse(200, "ok"), nil
			},
			ShouldDowngrade: func(ctx EndpointAttemptContext) bool {
				return ctx.Request.Endpoint == EndpointResponses
			},
		})

		if !result.OK {
			t.Error("expected success on second endpoint after downgrade")
		}
		if count != 2 {
			t.Errorf("expected 2 dispatch calls, got %d", count)
		}
	})
}

func TestExecuteEndpointFlow_NoCandidates(t *testing.T) {
	result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
		SiteURL:            "https://api.example.com",
		EndpointCandidates: []UpstreamEndpoint{},
		BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
			return BuiltEndpointRequest{}
		},
	})

	if result.OK {
		t.Error("expected failure with no candidates")
	}
	if result.Status != 502 {
		t.Errorf("expected status 502, got %d", result.Status)
	}
}

func TestExecuteEndpointFlow_FirstByteTimeout(t *testing.T) {
	t.Run("timeout falls back to next endpoint", func(t *testing.T) {
		callCount := 0
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				if callCount == 1 {
					return makeTimeoutResponse(), nil
				}
				return makeSuccessResponse(200, "ok"), nil
			},
		})

		if !result.OK {
			t.Error("expected success after timeout fallback")
		}
		if callCount != 2 {
			t.Errorf("expected 2 dispatch calls, got %d", callCount)
		}
	})

	t.Run("timeout on last endpoint stops iteration with 502", func(t *testing.T) {
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeTimeoutResponse(), nil
			},
		})

		if result.OK {
			t.Error("expected failure on timeout for last endpoint")
		}
		if result.Status != 502 {
			t.Errorf("expected status 502 for last-endpoint timeout, got %d", result.Status)
		}
	})

	t.Run("timeout with disableCrossProtocolFallback stops iteration", func(t *testing.T) {
		callCount := 0
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: true,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				return makeTimeoutResponse(), nil
			},
		})

		if result.OK {
			t.Error("expected failure with disableCrossProtocolFallback")
		}
		if callCount != 1 {
			t.Errorf("expected only 1 dispatch call (fallback disabled), got %d", callCount)
		}
	})
}

func TestExecuteEndpointFlow_Recovery(t *testing.T) {
	t.Run("OAuth recovery succeeds", func(t *testing.T) {
		callCount := 0
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://api.example.com",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				if callCount == 1 {
					return makeErrorResponse(401, "unauthorized"), nil
				}
				return makeSuccessResponse(200, "recovered"), nil
			},
			TryRecover: func(ctx *EndpointAttemptContext) *RecoverResult {
				return &RecoverResult{
					Response:     makeSuccessResponse(200, "recovered token"),
					UpstreamPath: "/v1/chat/completions",
				}
			},
		})

		if !result.OK {
			t.Error("expected success after recovery")
		}
	})

	t.Run("recovery failure, original error continues", func(t *testing.T) {
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://api.example.com",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeErrorResponse(401, "unauthorized"), nil
			},
			TryRecover: func(ctx *EndpointAttemptContext) *RecoverResult {
				return nil
			},
		})

		if result.OK {
			t.Error("expected failure after recovery failure")
		}
		if result.Status != 401 {
			t.Errorf("expected status 401, got %d", result.Status)
		}
	})
}

func TestExecuteEndpointFlow_ShouldAbortRemainingEndpoints(t *testing.T) {
	t.Run("abort on rate limit", func(t *testing.T) {
		callCount := 0
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages, EndpointResponses),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				if callCount == 1 {
					return makeErrorResponse(429, "rate limit exceeded"), nil
				}
				if callCount == 2 {
					return makeErrorResponse(429, "quota exceeded"), nil
				}
				return makeSuccessResponse(200, "ok"), nil
			},
			ShouldAbortRemainingEndpoints: func(ctx EndpointAttemptContext) bool {
				return ShouldAbortSameSiteEndpointFallback(ctx.Response.Status, ctx.RawErrText)
			},
		})

		if result.OK {
			t.Error("expected failure after abort")
		}
		if callCount >= 3 {
			t.Errorf("expected abort after first failure, got %d calls", callCount)
		}
	})

	t.Run("do not abort on non-systemic 400", func(t *testing.T) {
		// A 400 by itself falls through the endpoint flow without continuing.
		// Generic failure on non-last endpoint breaks by default.
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeErrorResponse(400, "some error"), nil
			},
			ShouldAbortRemainingEndpoints: func(ctx EndpointAttemptContext) bool {
				return ShouldAbortSameSiteEndpointFallback(ctx.Response.Status, ctx.RawErrText)
			},
		})

		// Should NOT abort (status < 500), breaks at first endpoint
		if result.OK {
			t.Error("expected failure")
		}
	})
}

func TestExecuteEndpointFlow_ShouldDowngrade(t *testing.T) {
	t.Run("downgrade protocol switch", func(t *testing.T) {
		callCount := 0
		downgradeCalled := false

		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointResponses, EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				callCount++
				if callCount == 1 {
					return BuiltEndpointRequest{Endpoint: EndpointResponses, Path: "/v1/responses"}
				}
				return BuiltEndpointRequest{Endpoint: EndpointChat, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				if request.Endpoint == EndpointResponses {
					return makeErrorResponse(400, "please use /v1/chat/completions"), nil
				}
				return makeSuccessResponse(200, "ok"), nil
			},
			ShouldDowngrade: func(ctx EndpointAttemptContext) bool {
				return ctx.Request.Endpoint == EndpointResponses
			},
			OnDowngrade: func(ctx EndpointAttemptContext) {
				downgradeCalled = true
			},
		})

		if !result.OK {
			t.Error("expected success after downgrade")
		}
		if !downgradeCalled {
			t.Error("expected OnDowngrade to be called")
		}
	})

	t.Run("no downgrade on last endpoint", func(t *testing.T) {
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeErrorResponse(400, "please downgrade"), nil
			},
			ShouldDowngrade: func(ctx EndpointAttemptContext) bool {
				return true
			},
		})

		if result.OK {
			t.Error("expected failure (no more endpoints to downgrade to)")
		}
	})
}

func TestExecuteEndpointFlow_AllEndpointsExhausted(t *testing.T) {
	failureCount := 0
	result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
		SiteURL:                      "https://api.example.com",
		DisableCrossProtocolFallback: false,
		EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages, EndpointResponses),
		BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
			return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
		},
		DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
			return makeErrorResponse(500, "server error"), nil
		},
		OnAttemptFailure: func(ctx EndpointAttemptContext) {
			failureCount++
		},
	})

	if result.OK {
		t.Error("expected failure when all endpoints exhausted")
	}
	// Generic 500 breaks at first endpoint, so only 1 failure hook
	if failureCount != 1 {
		t.Errorf("expected 1 failure hook call (first endpoint), got %d", failureCount)
	}
}

func TestExecuteEndpointFlow_DisableCrossProtocolFallback(t *testing.T) {
	t.Run("disableCrossProtocolFallback stops on first non-timeout failure", func(t *testing.T) {
		callCount := 0
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: true,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				return makeErrorResponse(503, "service unavailable"), nil
			},
		})

		if result.OK {
			t.Error("expected failure with disableCrossProtocolFallback")
		}
		if callCount != 1 {
			t.Errorf("expected only 1 dispatch (cross-protocol fallback disabled), got %d", callCount)
		}
	})
}

func TestResolveEndpointCandidates(t *testing.T) {
	t.Run("chat primary includes multi-protocol order", func(t *testing.T) {
		got := ResolveEndpointCandidates("/v1/chat/completions", false)
		want := []UpstreamEndpoint{EndpointChat, EndpointMessages, EndpointResponses}
		if len(got) != len(want) {
			t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v want %v", got, want)
			}
		}
	})
	t.Run("disableCrossProtocolFallback returns primary only", func(t *testing.T) {
		got := ResolveEndpointCandidates("/v1/messages", true)
		if len(got) != 1 || got[0] != EndpointMessages {
			t.Fatalf("got %v, want [messages]", got)
		}
	})
	t.Run("non chat-family returns nil", func(t *testing.T) {
		got := ResolveEndpointCandidates("/v1/embeddings", false)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestPathForEndpointAndFromPath(t *testing.T) {
	if PathForEndpoint(EndpointChat) != "/v1/chat/completions" {
		t.Fatalf("PathForEndpoint chat = %q", PathForEndpoint(EndpointChat))
	}
	ep, ok := EndpointFromPath("/v1/responses")
	if !ok || ep != EndpointResponses {
		t.Fatalf("EndpointFromPath responses = %v %v", ep, ok)
	}
}

func TestShouldDowngradeToNextEndpoint(t *testing.T) {
	if !ShouldDowngradeToNextEndpoint(400, "please use /v1/chat/completions") {
		t.Fatal("expected protocol redirect to downgrade")
	}
	if !ShouldDowngradeToNextEndpoint(404, "not found") {
		t.Fatal("expected 404 to downgrade")
	}
	if ShouldDowngradeToNextEndpoint(500, "internal") {
		t.Fatal("generic 500 should not auto-downgrade")
	}
	if ShouldDowngradeToNextEndpoint(0, "first byte timeout") {
		t.Fatal("status 0 is handled by timeout path, not downgrade helper")
	}
}

func TestExecuteEndpointFlow_FirstByteTimeoutPassesMsToDispatch(t *testing.T) {
	var gotTimeout int64 = -1
	_ = ExecuteEndpointFlow(ExecuteEndpointFlowInput{
		SiteURL:            "https://api.example.com",
		EndpointCandidates: makeEndpointCandidates(EndpointChat),
		FirstByteTimeoutMs: 1500,
		BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
			return BuiltEndpointRequest{Endpoint: endpoint, Path: PathForEndpoint(endpoint)}
		},
		DispatchRequest: func(request BuiltEndpointRequest, targetURL string, firstByteTimeoutMs int64) (*ExecutorDispatchResult, error) {
			gotTimeout = firstByteTimeoutMs
			return makeSuccessResponse(200, "ok"), nil
		},
	})
	if gotTimeout != 1500 {
		t.Fatalf("firstByteTimeoutMs = %d, want 1500", gotTimeout)
	}
}

func TestExecuteEndpointFlow_TimeoutDoesNotPoisonViaFailureHookSemantics(t *testing.T) {
	// Soft timeout on non-last endpoint should call OnAttemptFailure but still
	// continue; callers must not treat intermediate timeout as terminal poison.
	failures := 0
	result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
		SiteURL:                      "https://api.example.com",
		DisableCrossProtocolFallback: false,
		EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
		BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
			return BuiltEndpointRequest{Endpoint: endpoint, Path: PathForEndpoint(endpoint)}
		},
		DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
			if request.Endpoint == EndpointChat {
				return makeTimeoutResponse(), nil
			}
			return makeSuccessResponse(200, "ok"), nil
		},
		OnAttemptFailure: func(ctx EndpointAttemptContext) {
			failures++
			if ctx.Response == nil || ctx.Response.Status != 0 {
				t.Fatalf("expected timeout marker failure, got %+v", ctx.Response)
			}
		},
	})
	if !result.OK {
		t.Fatal("expected success after timeout fallback")
	}
	if failures != 1 {
		t.Fatalf("failures=%d, want 1 intermediate timeout failure", failures)
	}
}

func TestExecuteEndpointFlow_ProxyURL(t *testing.T) {
	t.Run("uses proxy URL when provided", func(t *testing.T) {
		var capturedURL string
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://real.api.com",
			ProxyURL:           "https://proxy.api.com",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				capturedURL = targetURL
				return makeSuccessResponse(200, "ok"), nil
			},
		})

		if !result.OK {
			t.Error("expected success")
		}
		if capturedURL != "https://proxy.api.com/v1/chat/completions" {
			t.Errorf("expected proxy URL, got %s", capturedURL)
		}
	})

	t.Run("uses site URL when no proxy", func(t *testing.T) {
		var capturedURL string
		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://real.api.com",
			ProxyURL:           "",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				capturedURL = targetURL
				return makeSuccessResponse(200, "ok"), nil
			},
		})

		if !result.OK {
			t.Error("expected success")
		}
		if capturedURL != "https://real.api.com/v1/chat/completions" {
			t.Errorf("expected site URL, got %s", capturedURL)
		}
	})
}

func TestExecuteEndpointFlow_OnAttemptSuccessWithRecovery(t *testing.T) {
	t.Run("onAttemptSuccess records recovery flag", func(t *testing.T) {
		var capturedSuccess EndpointAttemptSuccessContext

		result := ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://api.example.com",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeErrorResponse(401, "expired"), nil
			},
			TryRecover: func(ctx *EndpointAttemptContext) *RecoverResult {
				return &RecoverResult{
					Response:     makeSuccessResponse(200, "ok"),
					UpstreamPath: "/v1/chat/completions",
				}
			},
			OnAttemptSuccess: func(ctx EndpointAttemptSuccessContext) {
				capturedSuccess = ctx
			},
		})

		if !result.OK {
			t.Error("expected success after recovery")
		}
		if !capturedSuccess.RecoverApplied {
			t.Error("expected RecoverApplied to be true")
		}
	})
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkBuildUpstreamURL(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BuildUpstreamURL("https://api.example.com/path", "/v1/chat/completions")
	}
}

func BenchmarkSummarizeUpstreamError(b *testing.B) {
	b.ReportAllocs()
	errText := `{"error":{"message":"The model ` + `gpt-4` + ` is overloaded. Please try again later.","type":"server_error","code":503}}` + ``
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SummarizeUpstreamError(503, errText)
	}
}

func BenchmarkSummarizeUpstreamError_Long(b *testing.B) {
	b.ReportAllocs()
	longText := ""
	for j := 0; j < 500; j++ {
		longText += "x"
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SummarizeUpstreamError(502, longText)
	}
}

func BenchmarkExecuteEndpointFlow_Success(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{
					Endpoint: endpoint,
					Path:     "/v1/chat/completions",
				}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeSuccessResponse(200, `{"choices":[{"message":{"content":"hello"}}]}`), nil
			},
		})
	}
}

func BenchmarkExecuteEndpointFlow_TimeoutFallback(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		callCount := 0
		_ = ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				if callCount == 1 {
					return makeTimeoutResponse(), nil
				}
				return makeSuccessResponse(200, "ok"), nil
			},
		})
	}
}

func BenchmarkExecuteEndpointFlow_Recovery(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		callCount := 0
		_ = ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://api.example.com",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				callCount++
				if callCount == 1 {
					return makeErrorResponse(401, "unauthorized"), nil
				}
				return makeSuccessResponse(200, "recovered"), nil
			},
			TryRecover: func(ctx *EndpointAttemptContext) *RecoverResult {
				return &RecoverResult{
					Response:     makeSuccessResponse(200, "recovered token"),
					UpstreamPath: "/v1/chat/completions",
				}
			},
		})
	}
}

func BenchmarkExecuteEndpointFlow_AllExhausted(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:                      "https://api.example.com",
			DisableCrossProtocolFallback: false,
			EndpointCandidates:           makeEndpointCandidates(EndpointChat, EndpointMessages, EndpointResponses),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeErrorResponse(500, "server error"), nil
			},
		})
	}
}

func BenchmarkExecuteEndpointFlow_ProxyURL(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ExecuteEndpointFlow(ExecuteEndpointFlowInput{
			SiteURL:            "https://real.api.com",
			ProxyURL:           "https://proxy.api.com",
			EndpointCandidates: makeEndpointCandidates(EndpointChat),
			BuildRequest: func(endpoint UpstreamEndpoint, index int) BuiltEndpointRequest {
				return BuiltEndpointRequest{Endpoint: endpoint, Path: "/v1/chat/completions"}
			},
			DispatchRequest: func(request BuiltEndpointRequest, targetURL string, _ int64) (*ExecutorDispatchResult, error) {
				return makeSuccessResponse(200, "ok"), nil
			},
		})
	}
}
