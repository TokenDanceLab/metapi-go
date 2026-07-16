package proxyhandler

import (
	"net/http/httptest"
	"testing"
)

func TestHandleRerank_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/rerank", `{
		"model":"rerank-english-v3.0",
		"query":"What is the capital of France?",
		"documents":["Paris is the capital of France.","Berlin is in Germany."]
	}`)
	rec := httptest.NewRecorder()
	HandleRerank(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns a generic response.
	// Verify valid JSON and model echo.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
	if m["model"] != "rerank-english-v3.0" {
		t.Errorf("model = %v", m["model"])
	}
}

func TestHandleRerank_StreamNotSupported(t *testing.T) {
	req := makeProxyReq("POST", "/v1/rerank", `{
		"model":"rerank-english-v3.0",
		"query":"q",
		"documents":["a"],
		"stream":true
	}`)
	rec := httptest.NewRecorder()
	HandleRerank(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for streaming rerank, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRerank_ModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/rerank", `{
		"query":"q",
		"documents":["a"]
	}`)
	rec := httptest.NewRecorder()
	HandleRerank(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRerank_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/rerank", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleRerank(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
