package router

import (
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
		"Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'",
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
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/healthz", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin route without auth status = %d body=%s", rec.Code, rec.Body.String())
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
