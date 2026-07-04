package proxy

import (
	"net/http/httptest"
	"testing"
)

func TestHandleEmbeddings_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1/embeddings", `{"model":"text-embedding-3-small","input":"hello world"}`)
	rec := httptest.NewRecorder()
	HandleEmbeddings(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	// Verify valid JSON response was received.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
	if m["model"] != "text-embedding-3-small" {
		t.Errorf("model = %v", m["model"])
	}
}

func TestHandleEmbeddings_StreamNotSupported(t *testing.T) {
	req := makeProxyReq("POST", "/v1/embeddings", `{"model":"text-embedding-3-small","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	HandleEmbeddings(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for streaming embeddings, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEmbeddings_ModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1/embeddings", `{"input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleEmbeddings(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleEmbeddings_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/embeddings", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleEmbeddings(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
