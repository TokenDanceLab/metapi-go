package proxy

import (
	"context"

	"github.com/go-chi/chi/v5/middleware"
)

// requestIDCtxKey is a private context key for MetAPI request/trace IDs.
// Values mirror chi middleware.RequestID so retries and proxy_logs share one id.
type requestIDCtxKey struct{}

// WithRequestID stores requestID on ctx. Empty ids are ignored.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil || requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDCtxKey{}, requestID)
}

// RequestIDFromContext returns the MetAPI request/trace id when present.
// Falls back to chi's middleware.GetReqID so ingress middleware and
// attempt loops share the same correlation value without extra wiring.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDCtxKey{}).(string); ok {
		if id != "" {
			return id
		}
	}
	return middleware.GetReqID(ctx)
}

// EnsureRequestID returns ctx carrying a non-empty request id when possible.
// Prefer the existing chi/MetAPI id; only copies an explicit fallback when
// neither context source already has a value.
func EnsureRequestID(ctx context.Context, fallback string) (context.Context, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id := RequestIDFromContext(ctx); id != "" {
		return ctx, id
	}
	if fallback == "" {
		return ctx, ""
	}
	return WithRequestID(ctx, fallback), fallback
}
