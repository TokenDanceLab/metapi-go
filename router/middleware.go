package router

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/tokendancelab/metapi-go/config"
)

// CORS returns a CORS middleware handler configured for MetAPI.
// It is intentionally permissive for public health and proxy endpoints.
func CORS() func(http.Handler) http.Handler {
	return cors.Handler(corsOptions([]string{"*"}))
}

// AdminCORS returns a CORS middleware for admin API routes. By default it does
// not allow cross-origin browser access; operators can opt in with
// ADMIN_CORS_ALLOWED_ORIGINS for separately hosted admin frontends.
func AdminCORS(cfg *config.Config) func(http.Handler) http.Handler {
	if cfg == nil || len(cfg.AdminCorsAllowedOrigins) == 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	return cors.Handler(corsOptions(cfg.AdminCorsAllowedOrigins))
}

func corsOptions(allowedOrigins []string) cors.Options {
	return cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Link", "X-Request-Id"},
		AllowCredentials: false,
		MaxAge:           300,
	}
}

// RequestLogger logs every incoming request using slog.
// Includes request_id for log correlation when RequestID middleware is active.
// Equivalent to TS Fastify `logger: true`.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := middleware.GetReqID(r.Context())
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"request_id", reqID,
		)
		next.ServeHTTP(w, r)
	})
}

// TrustedRealIP reads X-Forwarded-For / X-Real-IP only from explicitly
// configured proxy CIDRs. Direct clients cannot spoof rate-limit or admin
// allowlist identity by sending forwarded headers themselves.
func TrustedRealIP(cfg *config.Config) func(http.Handler) http.Handler {
	var prefixes []netip.Prefix
	if cfg != nil {
		prefixes = parseTrustedProxyPrefixes(cfg.TrustedProxyCidrs)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(prefixes) > 0 && isTrustedProxyPeer(r.RemoteAddr, prefixes) {
				if ip := forwardedClientIP(r); ip != "" {
					r.RemoteAddr = ip
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func parseTrustedProxyPrefixes(raw []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, item := range raw {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
		if err == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}

func isTrustedProxyPeer(remoteAddr string, prefixes []netip.Prefix) bool {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func forwardedClientIP(r *http.Request) string {
	for _, xff := range r.Header.Values("X-Forwarded-For") {
		part, _, _ := strings.Cut(xff, ",")
		if ip := normalizeForwardedIP(part); ip != "" {
			return ip
		}
	}
	return normalizeForwardedIP(r.Header.Get("X-Real-IP"))
}

func normalizeForwardedIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

// Recoverer catches panics and returns 500.
// Equivalent to TS Fastify default error handling.
func Recoverer(next http.Handler) http.Handler {
	return middleware.Recoverer(next)
}

// BodyLimit enforces a maximum request body size using http.MaxBytesReader.
// Returns a middleware that wraps the request body so that reads beyond the
// limit return an error, causing the handler to receive a closed body.
func BodyLimit(limitBytes int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limitBytes > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, int64(limitBytes))
			}
			next.ServeHTTP(w, r)
		})
	}
}
