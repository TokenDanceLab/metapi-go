package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
	"github.com/tokendancelab/metapi-go/web"
)

func TestHealthAndReadyBypassAuthAndIncludeSecurityHeaders(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
		DbType:           store.DialectSQLite,
		DbUrl:            filepath.Join(dataDir, "router-ready.db"),
		DataDir:          dataDir,
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}
	t.Cleanup(func() {
		_ = store.CloseDatabase()
	})

	r := New(cfg, web.Dist)

	for _, path := range []string{"/health", "/ready"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
			}
			assertSecurityHeaders(t, rec)
			if got := rec.Header().Get("X-Request-Id"); strings.TrimSpace(got) == "" {
				t.Fatal("X-Request-Id response header is empty")
			}
		})
	}
}

func assertSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	expected := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
		"Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline' https://static.cloudflareinsights.com; style-src 'self' 'unsafe-inline'; img-src 'self' https://api.dicebear.com; connect-src 'self'; frame-src 'self' https://check.linux.do; frame-ancestors 'none'",
	}
	for header, want := range expected {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestAdminRouteStillRequiresAuth(t *testing.T) {
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/debug/vars", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin route without auth status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminRoutesAreMountedWithoutDoubleAPIPrefix(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
		DbType:           store.DialectSQLite,
		DbUrl:            filepath.Join(dataDir, "router-admin.db"),
		DataDir:          dataDir,
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}
	t.Cleanup(func() {
		_ = store.CloseDatabase()
	})

	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/auth/info", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin auth info status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode auth info: %v", err)
	}
	if got := body["masked"]; got != "admi****oken" {
		t.Fatalf("masked token = %q, want %q", got, "admi****oken")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/api/settings/auth/info", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("double-prefixed admin route status = %d, want 404", rec.Code)
	}
}

func TestAdminCORSDefaultDoesNotAllowCrossOrigin(t *testing.T) {
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("admin CORS allow origin = %q, want empty by default", got)
	}
}

func TestAdminCORSAllowsConfiguredOriginsOnly(t *testing.T) {
	cfg := &config.Config{
		AuthToken:               "admin-token",
		ProxyToken:              "proxy-token",
		RequestBodyLimit:        config.DefaultRequestBodyLimit,
		AdminCorsAllowedOrigins: []string{"https://admin.example.com"},
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/health", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("configured admin origin header = %q, want https://admin.example.com", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/desktop/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured admin origin header = %q, want empty", got)
	}
}

func TestProxyCORSRemainsWildcard(t *testing.T) {
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Origin", "https://client.example.com")
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("proxy CORS allow origin = %q, want *", got)
	}
}

func TestSPAFallbackRootBypassesProxyAuth(t *testing.T) {
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SPA root status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("SPA root content-type = %q, want text/html", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<html") {
		t.Fatalf("SPA root body does not look like HTML: %q", body[:min(len(body), 120)])
	}
}

func TestNonV1ProxyAliasStillRequiresProxyAuth(t *testing.T) {
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{"model":"gpt-test"}`))
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("non-/v1 proxy alias without auth status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHealthCORSRemainsWildcard(t *testing.T) {
	cfg := &config.Config{
		AuthToken:        "admin-token",
		ProxyToken:       "proxy-token",
		RequestBodyLimit: config.DefaultRequestBodyLimit,
	}
	r := New(cfg, web.Dist)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://monitor.example.com")
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("health CORS allow origin = %q, want *", got)
	}
}
