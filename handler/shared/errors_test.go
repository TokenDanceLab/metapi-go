package shared

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError_StatusAndCamelCaseJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "Invalid site payload.")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if body["error"] != "Invalid site payload." {
		t.Fatalf("error field = %v", body["error"])
	}
	if _, ok := body["message"]; ok {
		t.Fatalf("unexpected message field in unified error body: %#v", body)
	}
	if _, ok := body["success"]; ok {
		t.Fatalf("unexpected success field in unified error body: %#v", body)
	}
}

func TestWriteErrorDetail_IncludesDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErrorDetail(rec, http.StatusConflict, "API key 已存在", "duplicate_key")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != "API key 已存在" {
		t.Fatalf("error = %v", body["error"])
	}
	if body["detail"] != "duplicate_key" {
		t.Fatalf("detail = %v", body["detail"])
	}
}

func TestWriteAPIError_RejectsSilent200(t *testing.T) {
	rec := httptest.NewRecorder()
	// Code 200 must not produce a silent success with error body.
	WriteAPIError(rec, &APIError{Code: http.StatusOK, Message: "should not be 200"})

	if rec.Code == http.StatusOK {
		t.Fatalf("WriteAPIError must not emit HTTP 200 for error bodies, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 fallback", rec.Code)
	}
}

func TestWriteErrorWithRequestID_IncludesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErrorWithRequestID(rec, http.StatusServiceUnavailable, "upstream exhausted", "req-abc-123")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "req-abc-123" {
		t.Fatalf("X-Request-Id = %q, want req-abc-123", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if body["error"] != "upstream exhausted" {
		t.Fatalf("error = %v", body["error"])
	}
	if body["request_id"] != "req-abc-123" {
		t.Fatalf("request_id = %v", body["request_id"])
	}
}

func TestWriteAPIError_DoesNotOverrideExistingRequestHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-Id", "ingress-id")
	WriteAPIError(rec, &APIError{
		Code:      http.StatusBadGateway,
		Message:   "boom",
		RequestID: "body-id",
	})
	if got := rec.Header().Get("X-Request-Id"); got != "ingress-id" {
		t.Fatalf("header overwritten: %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["request_id"] != "body-id" {
		t.Fatalf("request_id = %v, want body-id", body["request_id"])
	}
}
