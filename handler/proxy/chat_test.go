package proxyhandler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleChatCompletions_UnconfiguredUpstreamReturns503(t *testing.T) {
	t.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Proxy upstream is not configured") {
		t.Fatalf("body = %q, want unconfigured upstream error", body)
	}
}

func TestHandleChatCompletions_StreamUnconfiguredUpstreamReturns503(t *testing.T) {
	t.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Proxy upstream is not configured") {
		t.Fatalf("body = %q, want unconfigured upstream error", body)
	}
}

func TestHandleChatCompletions_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletions_StreamHeaders(t *testing.T) {
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","stream":true}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Error("missing Cache-Control: no-cache")
	}
	if rec.Header().Get("Connection") != "keep-alive" {
		t.Error("missing Connection: keep-alive")
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkHandleChatCompletions_UnconfiguredUpstream(b *testing.B) {
	b.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
		rec := httptest.NewRecorder()
		HandleChatCompletions(rec, req)
	}
}

func BenchmarkHandleChatCompletions_StreamUnconfiguredUpstream(b *testing.B) {
	b.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		rec := httptest.NewRecorder()
		HandleChatCompletions(rec, req)
	}
}

func BenchmarkHandleChatCompletions_Unauthorized(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		HandleChatCompletions(rec, req)
	}
}
