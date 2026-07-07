package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

func TestTrustedRealIPIgnoresForwardedHeadersWithoutTrustedProxy(t *testing.T) {
	handler := TrustedRealIP(&config.Config{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "203.0.113.10:12345" {
			t.Fatalf("RemoteAddr = %q, want original direct peer", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTrustedRealIPUsesForwardedHeaderFromTrustedProxy(t *testing.T) {
	handler := TrustedRealIP(&config.Config{
		TrustedProxyCidrs: []string{"127.0.0.1/32"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "198.51.100.99" {
			t.Fatalf("RemoteAddr = %q, want forwarded client IP", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "198.51.100.99, 127.0.0.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTrustedRealIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	handler := TrustedRealIP(&config.Config{
		TrustedProxyCidrs: []string{"127.0.0.1/32"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr != "203.0.113.10:12345" {
			t.Fatalf("RemoteAddr = %q, want original untrusted peer", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}
