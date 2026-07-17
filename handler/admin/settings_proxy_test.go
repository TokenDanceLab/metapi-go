package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrandList_FromRegistry(t *testing.T) {
	_, r, _ := setupEdgeTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/brand-list", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	brands, _ := body["brands"].([]any)
	if len(brands) < 5 {
		t.Fatalf("brands=%v", brands)
	}
}

func TestSystemProxyTest_UsesProbe(t *testing.T) {
	old := systemProxyProbeFn
	t.Cleanup(func() { systemProxyProbeFn = old })
	systemProxyProbeFn = func(ctx context.Context, proxyURL, target string) map[string]any {
		return map[string]any{
			"success": true, "proxyUrl": proxyURL, "targetUrl": target,
			"reachable": true, "ok": true, "statusCode": 204, "latencyMs": 12,
		}
	}
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = "http://127.0.0.1:9"
	b, _ := json.Marshal(map[string]any{"proxyUrl": "http://127.0.0.1:9"})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/system-proxy/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["reachable"] != true {
		t.Fatalf("body=%v", body)
	}
}
