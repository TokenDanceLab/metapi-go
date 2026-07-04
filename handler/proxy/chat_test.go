package proxyhandler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleChatCompletions_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	if m["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", m["object"])
	}
	choices, _ := m["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("expected choices")
	}
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	if msg["role"] != "assistant" {
		t.Errorf("role = %v", msg["role"])
	}
	if !strings.Contains(msg["content"].(string), "MetAPI Go") {
		t.Errorf("content = %q", msg["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	if m["model"] != "gpt-4o" {
		t.Errorf("model = %v", m["model"])
	}
}

func TestHandleChatCompletions_Stream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("SSE stream should contain [DONE] marker")
	}
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Error("SSE stream should contain chat.completion.chunk")
	}

	// Parse SSE lines
	lines := strings.Split(strings.TrimSpace(body), "\n")
	var events []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data: [DONE]") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
	}

	if len(events) < 2 {
		t.Errorf("expected at least 2 SSE events, got %d", len(events))
	}

	// First event should have content delta
	var firstEvent map[string]any
	json.Unmarshal([]byte(events[0]), &firstEvent)
	choices := firstEvent["choices"].([]any)
	ch := choices[0].(map[string]any)
	delta := ch["delta"].(map[string]any)
	if delta["content"] != "Hello from MetAPI Go (stub)" {
		t.Errorf("delta content = %v", delta["content"])
	}

	// Last event should have finish_reason stop
	var lastEvent map[string]any
	json.Unmarshal([]byte(events[len(events)-1]), &lastEvent)
	lc := lastEvent["choices"].([]any)[0].(map[string]any)
	if lc["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", lc["finish_reason"])
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

func BenchmarkHandleChatCompletions_NonStream(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
		rec := httptest.NewRecorder()
		HandleChatCompletions(rec, req)
	}
}

func BenchmarkHandleChatCompletions_Stream(b *testing.B) {
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
