package router

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// New creates and configures the Chi router with the full middleware stack,
// route groups, SPA fallback, and asset caching.
func New(webDir string) chi.Router {
	r := chi.NewRouter()

	// ---- Middleware stack ----
	r.Use(RealIP)
	r.Use(CORS())
	r.Use(RequestLogger)
	r.Use(Recoverer)

	// ---- /health (design addition, not in TS) ----
	// Registered before route groups so it bypasses auth middleware.
	// The actual handler lives in app/health.go; we register it from app.go.
	// This placeholder allows the router to compile without the app package.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// ---- Route groups ----
	// /api/* routes (excluding public routes) → admin auth middleware
	// For P0, the auth middleware is a placeholder that passes through.
	r.Route("/api", func(r chi.Router) {
		r.Use(adminAuthPlaceholder)

		// P3-P11: register specific /api routes here
		r.Get("/desktop/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})
	})

	// /v1/* proxy routes → internal auth handled by P10
	r.Route("/v1", func(r chi.Router) {
		r.Use(proxyAuthPlaceholder)
		// P10: register proxy handler
	})

	// ---- SPA static file fallback ----
	if webDir != "" {
		if info, err := os.Stat(webDir); err == nil && info.IsDir() {
			setupSPAFallback(r, webDir)
		} else {
			slog.Warn("web directory not found, SPA fallback disabled", "dir", webDir)
		}
	}

	return r
}

// setupSPAFallback configures static asset serving and SPA fallback.
func setupSPAFallback(r chi.Router, webDir string) {
	// /assets/* → immutable cache for 1 year
	assetsDir := filepath.Join(webDir, "assets")
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.FileServer(http.Dir(assetsDir)).ServeHTTP(w, r)
	})))

	// SPA fallback: non-API paths → index.html; API → 404 JSON
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"Not found"}`))
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
}

// adminAuthPlaceholder is a P0 stub for admin bearer token + IP allowlist auth.
// It passes through all requests. P3 will implement actual auth.
func adminAuthPlaceholder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// P3: validate Bearer AUTH_TOKEN, check AdminIpAllowlist
		// For now, skip public routes
		if isPublicApiRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// proxyAuthPlaceholder is a P0 stub for proxy token auth.
// P10 will implement actual proxy auth.
func proxyAuthPlaceholder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// isPublicApiRoute checks if the URL path is on the public API whitelist.
// Whitelist: /api/desktop/health, /api/oauth/callback/*
func isPublicApiRoute(urlPath string) bool {
	if urlPath == "/api/desktop/health" {
		return true
	}
	if strings.HasPrefix(urlPath, "/api/oauth/callback/") {
		return true
	}
	return false
}
