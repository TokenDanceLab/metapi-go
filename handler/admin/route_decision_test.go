package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"
)

func TestRouteDecision_RequiresRouter(t *testing.T) {
	proxyhandler.SetUpstreamConfig(nil)
	_, r := setupTokenRoutesTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/routes/decision?model=gpt-4o", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["success"] != false {
		t.Fatalf("body=%v", body)
	}
}

func TestRouteDecision_RequiresModel(t *testing.T) {
	_, r := setupTokenRoutesTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/routes/decision", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
