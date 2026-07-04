package proxyhandler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCompletions_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/completions", `{"model":"gpt-3.5-turbo-instruct","prompt":"hello"}`)
	rec := httptest.NewRecorder()
	HandleCompletions(rec, req)

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
	ch := choices[0].(map[string]any)
	msg := ch["message"].(map[string]any)
	if !strings.Contains(msg["content"].(string), "MetAPI Go") {
		t.Errorf("content = %q", msg["content"])
	}
	if ch["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", ch["finish_reason"])
	}
}

func TestHandleCompletions_Stream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/completions", `{"model":"gpt-3.5-turbo-instruct","stream":true,"prompt":"hello"}`)
	rec := httptest.NewRecorder()
	HandleCompletions(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("missing [DONE]")
	}
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Error("missing chat.completion.chunk")
	}

	// Parse SSE events
	lines := strings.Split(strings.TrimSpace(body), "\n")
	var events []map[string]any
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data: [DONE]") {
			var evt map[string]any
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt)
			events = append(events, evt)
		}
	}

	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}

	// First event should have content delta
	ch := events[0]["choices"].([]any)[0].(map[string]any)
	delta := ch["delta"].(map[string]any)
	if delta["content"] == nil || delta["content"] == "" {
		t.Error("first chunk missing content delta")
	}

	// Last event should have finish_reason "stop"
	lc := events[len(events)-1]["choices"].([]any)[0].(map[string]any)
	if lc["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", lc["finish_reason"])
	}
}

func TestHandleCompletions_ModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/completions", `{"prompt":"hello"}`)
	rec := httptest.NewRecorder()
	HandleCompletions(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCompletions_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/completions", strings.NewReader(`{"model":"test","prompt":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleCompletions(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
