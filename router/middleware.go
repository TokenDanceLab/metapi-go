package router

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// CORS returns a CORS middleware handler configured for MetAPI.
// Matches the intent of TS @fastify/cors (no-args) with explicit Go config.
func CORS() func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	})
}

// RequestLogger logs every incoming request using slog.
// Equivalent to TS Fastify `logger: true`.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)
		next.ServeHTTP(w, r)
	})
}

// RealIP reads X-Forwarded-For / X-Real-IP headers.
// Equivalent to TS Fastify `trustProxy: true`.
func RealIP(next http.Handler) http.Handler {
	return middleware.RealIP(next)
}

// Recoverer catches panics and returns 500.
// Equivalent to TS Fastify default error handling.
func Recoverer(next http.Handler) http.Handler {
	return middleware.Recoverer(next)
}
