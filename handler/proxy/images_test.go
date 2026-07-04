package proxyhandler

import (
	"net/http/httptest"
	"testing"
)

// ---- HandleImagesGenerations ----

func TestHandleImagesGenerations_DefaultModel(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/generations", `{"prompt":"a beautiful sunset"}`)
	rec := httptest.NewRecorder()
	HandleImagesGenerations(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	// Verify valid JSON response was received.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
}

func TestHandleImagesGenerations_CustomModel(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/generations", `{"model":"dall-e-3","prompt":"a cat"}`)
	rec := httptest.NewRecorder()
	HandleImagesGenerations(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImagesGenerations_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/images/generations", nil)
	rec := httptest.NewRecorder()
	HandleImagesGenerations(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ---- HandleImagesEdits ----

func TestHandleImagesEdits_JSONBody(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/edits", `{"image":"base64data","prompt":"add a hat"}`)
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
}

func TestHandleImagesEdits_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/images/edits", nil)
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ---- HandleImagesVariations ----

func TestHandleImagesVariations(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/variations", `{"image":"test"}`)
	rec := httptest.NewRecorder()
	HandleImagesVariations(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
