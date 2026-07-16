package proxyhandler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestWriteJSONErrorIncludesRequestIDWhenHeaderSet(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-Id", "req-test-123")
	writeJSONError(rec, 502, "Upstream request failed", "upstream_error")
	if rec.Code != 502 {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["request_id"] != "req-test-123" {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestWriteJSONErrorOmitsRequestIDWhenMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONError(rec, 400, "bad", "invalid_request_error")
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errObj, _ := body["error"].(map[string]any)
	if _, ok := errObj["request_id"]; ok {
		t.Fatalf("unexpected request_id in %s", rec.Body.String())
	}
}

func TestRequestIDFromCtx(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "abc-xyz")
	if got := requestIDFromCtx(ctx); got != "abc-xyz" {
		t.Fatalf("got %q", got)
	}
	if got := requestIDFromCtx(context.Background()); got != "" {
		t.Fatalf("empty ctx got %q", got)
	}
}
