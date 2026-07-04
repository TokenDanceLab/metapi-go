package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleClaudeMessages_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/messages", `{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	if m["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", m["object"])
	}
	if m["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model = %v", m["model"])
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
}

func TestHandleClaudeMessages_Stream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/messages", `{"model":"claude-sonnet-4-20250514","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("SSE stream should contain [DONE] marker")
	}
	if !strings.Contains(body, "data: ") {
		t.Error("SSE stream should contain data events")
	}
}

func TestHandleClaudeMessages_ModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/messages", `{"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleClaudeMessages_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ---- count_tokens ----

func TestHandleClaudeCountTokens(t *testing.T) {
	req := makeProxyReq("POST", "/v1/messages/count_tokens", `{"messages":[{"role":"user","content":"hello world"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeCountTokens(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	// When upstream forwarding is wired, response contains upstream data.
	// In stub mode, a generic chat completion is returned.
	if m["object"] != nil {
		// Generic chat completion response from stub
		if m["object"] != "chat.completion" {
			t.Errorf("unexpected object type: %v", m["object"])
		}
	}
}

func TestHandleClaudeCountTokens_NoModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/messages/count_tokens", `{"messages":[{"role":"user","content":"test"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeCountTokens(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200 even without model, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- SSE event content verification for Claude stream ----

func TestHandleClaudeMessages_StreamContentVerification(t *testing.T) {
	req := makeProxyReq("POST", "/v1/messages", `{"model":"claude-sonnet-4-20250514","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	body := rec.Body.String()

	// Verify stream contains model reference
	if !strings.Contains(body, `"model":"claude-sonnet-4-20250514"`) {
		t.Error("stream missing model reference")
	}

	// Verify stream contains content and finish markers
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("stream missing [DONE] marker")
	}
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Error("stream missing chat.completion.chunk")
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Error("stream missing finish_reason:stop")
	}
}

// Ensure json import is used
var _ = json.Marshal
