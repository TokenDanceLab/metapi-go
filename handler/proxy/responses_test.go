package proxyhandler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- resolveAliasedResponsesPath ----

func TestResolveAliasedResponsesPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/responses", "/v1/responses"},
		{"/responses/compact", "/v1/responses/compact"},
		{"/responses?query=1", "/v1/responses"},
		{"/responses/compact?foo=bar", "/v1/responses/compact"},
		{"/responses/unknown", ""},
		{"/responses/other", ""},
		{"/random", ""},
	}

	for _, tt := range tests {
		got := resolveAliasedResponsesPath(tt.input)
		if got != tt.want {
			t.Errorf("resolveAliasedResponsesPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---- HandleResponses ----

func TestHandleResponses_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/responses", `{"model":"gpt-4o","input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Fatal("expected non-nil response")
	}
	if m["model"] != "gpt-4o" {
		t.Errorf("model = %v", m["model"])
	}
}

func TestHandleResponses_Stream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/responses", `{"model":"gpt-4o","stream":true,"input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Error("stream missing [DONE] marker")
	}
}

func TestHandleResponses_Compact(t *testing.T) {
	req := makeProxyReq("POST", "/v1/responses/compact", `{"model":"gpt-4o","input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses/compact")

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResponses_ModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/responses", `{"input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// ---- HandleResponsesGet426 ----

func TestHandleResponsesGet426(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	rec := httptest.NewRecorder()
	HandleResponsesGet426(rec, req)

	if rec.Code != 426 {
		t.Errorf("expected 426, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "WebSocket upgrade required") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestHandleResponsesGet426_WebsocketUpgradeRequiresAuth(t *testing.T) {
	// C1: upgrade without ProxyAuth is refused with 401 (not 501 residual theater).
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	HandleResponsesGet426(rec, req)

	if rec.Code != 401 {
		t.Fatalf("expected 401 without auth, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "response.completed") {
		t.Error("must not invent WS completion payloads")
	}
}

// ---- HandleResponsesAliasPost ----

func TestHandleResponsesAliasPost_Valid(t *testing.T) {
	req := makeProxyReq("POST", "/responses", `{"model":"gpt-4o","input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleResponsesAliasPost(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResponsesAliasPost_Compact(t *testing.T) {
	req := makeProxyReq("POST", "/responses/compact", `{"model":"gpt-4o","input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleResponsesAliasPost(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResponsesAliasPost_Unknown(t *testing.T) {
	req := makeProxyReq("POST", "/responses/unknown", `{"model":"gpt-4o"}`)
	rec := httptest.NewRecorder()
	HandleResponsesAliasPost(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- HandleResponsesAliasGet426 ----

func TestHandleResponsesAliasGet426_Valid(t *testing.T) {
	req := httptest.NewRequest("GET", "/responses", nil)
	rec := httptest.NewRecorder()
	HandleResponsesAliasGet426(rec, req)

	if rec.Code != 426 {
		t.Errorf("expected 426, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResponsesAliasGet426_WebsocketUpgradeRequiresAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/responses", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	HandleResponsesAliasGet426(rec, req)

	if rec.Code != 401 {
		t.Fatalf("expected 401 without auth, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResponsesAliasGet426_Unknown(t *testing.T) {
	req := httptest.NewRequest("GET", "/responses/unknown", nil)
	rec := httptest.NewRecorder()
	HandleResponsesAliasGet426(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleResponsesAliasGet426_Compact(t *testing.T) {
	req := httptest.NewRequest("GET", "/responses/compact", nil)
	rec := httptest.NewRecorder()
	HandleResponsesAliasGet426(rec, req)

	if rec.Code != 426 {
		t.Errorf("expected 426, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- HandleResponses_Unauthorized ----

func TestHandleResponses_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

var _ = json.Marshal
