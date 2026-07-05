package router

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// WithRequestID wraps chi's middleware.RequestID for explicit import.
// It reads/generates a request ID, sets X-Request-Id response header,
// and stores it in the context for downstream use.
func WithRequestID(next http.Handler) http.Handler {
	return middleware.RequestID(next)
}

// RequestIDFromContext extracts the request ID from context.
// Returns an empty string if not set.
func RequestIDFromContext(r *http.Request) string {
	return middleware.GetReqID(r.Context())
}
