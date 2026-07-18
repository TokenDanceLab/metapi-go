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
	var gotTarget string
	systemProxyProbeFn = func(ctx context.Context, proxyURL, target string) map[string]any {
		gotTarget = target
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
	if gotTarget != "https://www.gstatic.com/generate_204" {
		t.Fatalf("default targetUrl=%q, want gstatic generate_204", gotTarget)
	}
}

func TestSystemProxyTest_EmptyProxyURL(t *testing.T) {
	old := systemProxyProbeFn
	t.Cleanup(func() { systemProxyProbeFn = old })
	called := false
	systemProxyProbeFn = func(ctx context.Context, proxyURL, target string) map[string]any {
		called = true
		return map[string]any{"success": true}
	}
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = ""
	b, _ := json.Marshal(map[string]any{"proxyUrl": "  "})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/system-proxy/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("probe must not run when proxy URL is empty")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("请先填写系统代理地址")) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestSystemProxyTest_RejectsForbiddenTargetURL(t *testing.T) {
	old := systemProxyProbeFn
	t.Cleanup(func() { systemProxyProbeFn = old })
	called := false
	systemProxyProbeFn = func(ctx context.Context, proxyURL, target string) map[string]any {
		called = true
		return map[string]any{"success": true, "targetUrl": target}
	}

	cases := []string{
		"http://169.254.169.254/latest/meta-data",
		"https://169.254.169.254/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"not-a-url",
		"ftp://example.com/",
	}
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = "http://127.0.0.1:9"
	for _, target := range cases {
		called = false
		b, _ := json.Marshal(map[string]any{
			"proxyUrl":  "http://127.0.0.1:9",
			"targetUrl": target,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/settings/system-proxy/test", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("target %q: status=%d body=%s", target, rec.Code, rec.Body.String())
		}
		if called {
			t.Fatalf("target %q: probe must not be called", target)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("Invalid targetUrl")) {
			t.Fatalf("target %q: body=%s", target, rec.Body.String())
		}
	}
}

func TestSystemProxyTest_CustomSafeTargetURL(t *testing.T) {
	old := systemProxyProbeFn
	t.Cleanup(func() { systemProxyProbeFn = old })
	var gotTarget string
	systemProxyProbeFn = func(ctx context.Context, proxyURL, target string) map[string]any {
		gotTarget = target
		return map[string]any{
			"success": true, "proxyUrl": proxyURL, "targetUrl": target,
			"reachable": true, "ok": true, "statusCode": 204, "latencyMs": 3,
		}
	}
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = "http://127.0.0.1:9"
	want := "https://example.com/healthz"
	b, _ := json.Marshal(map[string]any{
		"proxyUrl":  "http://127.0.0.1:9",
		"targetUrl": want,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/system-proxy/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotTarget != want {
		t.Fatalf("targetUrl=%q, want %q", gotTarget, want)
	}
}

func TestSystemProxyTest_OmittedTargetUsesDefault(t *testing.T) {
	old := systemProxyProbeFn
	t.Cleanup(func() { systemProxyProbeFn = old })
	var gotTarget string
	systemProxyProbeFn = func(ctx context.Context, proxyURL, target string) map[string]any {
		gotTarget = target
		return map[string]any{
			"success": true, "proxyUrl": proxyURL, "targetUrl": target,
			"reachable": true, "ok": true, "statusCode": 204, "latencyMs": 1,
		}
	}
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = "http://127.0.0.1:9"
	// Empty targetUrl must keep the default gstatic probe target.
	b, _ := json.Marshal(map[string]any{
		"proxyUrl":  "http://127.0.0.1:9",
		"targetUrl": "   ",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/system-proxy/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotTarget != "https://www.gstatic.com/generate_204" {
		t.Fatalf("empty targetUrl got %q", gotTarget)
	}
}
