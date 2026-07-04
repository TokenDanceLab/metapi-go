package router

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/handler/admin"
	"github.com/tokendancelab/metapi-go/store"
)

// New creates and configures the Chi router with the full middleware stack,
// route groups, SPA fallback, and asset caching.
func New(cfg *config.Config, webDir string) chi.Router {
	r := chi.NewRouter()

	// ---- Middleware stack ----
	r.Use(RealIP)
	r.Use(CORS())
	r.Use(RequestLogger)
	r.Use(Recoverer)

	// ---- /health (design addition, not in TS) ----
	// Registered before route groups so it bypasses auth middleware.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// ---- Route groups ----
	// /api/* routes (excluding public routes) → admin auth middleware
	r.Route("/api", func(r chi.Router) {
		r.Use(auth.AdminAuth(cfg))

		// P3: Sites + Accounts + AccountTokens CRUD API
		db := store.GetDB()
		if db != nil {
			admin.RegisterSitesRoutes(r, db.DB)
			admin.RegisterAccountsRoutes(r, db.DB, cfg)
			admin.RegisterAccountTokensRoutes(r, db.DB)
		} else {
			slog.Warn("router: database not initialized, P3 routes skipped")
		}

		r.Get("/desktop/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		})
	})

	// /v1/* proxy routes → proxy auth middleware
	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.ProxyAuth(cfg))
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
