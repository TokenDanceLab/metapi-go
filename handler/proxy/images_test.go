package proxyhandler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandleImagesEdits_MultipartUnauthorized(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("model", "gpt-image-1"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/images/edits", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImagesEdits_InvalidMultipartReturnsBadRequest(t *testing.T) {
	req := makeProxyReqNoBody("POST", "/v1/images/edits")
	req.Body = io.NopCloser(strings.NewReader("not a valid multipart body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing-boundary")
	rec := httptest.NewRecorder()

	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid multipart, got %d: %s", rec.Code, rec.Body.String())
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
