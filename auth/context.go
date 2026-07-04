package auth

import (
	"context"
	"net/http"
)

// ---------------------------------------------------------------------------
// Context keys — unexported typed keys to avoid collisions.
// ---------------------------------------------------------------------------

type contextKey int

const (
	adminAuthKey contextKey = iota
	proxyAuthKey
)

// ---------------------------------------------------------------------------
// ProxyAuthContext — stored in request context after successful proxy auth.
// ---------------------------------------------------------------------------

// ProxyAuthContext holds the result of a successful downstream token
// authorization. It is stored in the request context by ProxyAuth middleware.
type ProxyAuthContext struct {
	Token   string                  // Normalized token string
	Source  string                  // "managed" | "global"
	KeyID   *int64                  // Managed key row ID; nil for global
	KeyName string                  // Managed key name; "global" for global
	Policy  DownstreamRoutingPolicy // Resolved routing policy
}

// ---------------------------------------------------------------------------
// ProxyResourceOwner — derived from ProxyAuthContext for downstream usage
// attribution (proxy_logs, proxy_files owner fields).
// ---------------------------------------------------------------------------

// ProxyResourceOwner identifies the downstream principal that owns a proxy
// resource (log entry, file, etc.).
type ProxyResourceOwner struct {
	OwnerType string // "managed_key" | "global_proxy_token"
	OwnerID   string // managed_key: String(keyID) or token fallback; global: "global"
}

// GetProxyResourceOwner derives a ProxyResourceOwner from a ProxyAuthContext.
// Returns nil if auth is nil.
func GetProxyResourceOwner(auth *ProxyAuthContext) *ProxyResourceOwner {
	if auth == nil {
		return nil
	}
	if auth.Source == "managed" {
		ownerID := ""
		if auth.KeyID != nil {
			ownerID = formatInt64(*auth.KeyID)
		} else {
			ownerID = auth.Token
		}
		return &ProxyResourceOwner{
			OwnerType: "managed_key",
			OwnerID:   ownerID,
		}
	}
	return &ProxyResourceOwner{
		OwnerType: "global_proxy_token",
		OwnerID:   "global",
	}
}

// formatInt64 converts an int64 to a decimal string without importing strconv
// (we use fmt, but for this package we keep it simple with a small helper).
func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ---------------------------------------------------------------------------
// Admin auth context — simply marks the request as admin-authenticated.
// ---------------------------------------------------------------------------

// WithAdminAuth stores an admin auth marker in the context.
func WithAdminAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, adminAuthKey, true)
}

// IsAdmin checks whether the request passed admin authentication.
func IsAdmin(ctx context.Context) bool {
	v, ok := ctx.Value(adminAuthKey).(bool)
	return ok && v
}

// AdminAuthFromRequest is a convenience helper that stores the admin marker
// on the request context and returns the modified request.
func AdminAuthFromRequest(r *http.Request) *http.Request {
	return r.WithContext(WithAdminAuth(r.Context()))
}

// ---------------------------------------------------------------------------
// Proxy auth context helpers.
// ---------------------------------------------------------------------------

// WithProxyAuth stores a ProxyAuthContext in the context.
func WithProxyAuth(ctx context.Context, pac *ProxyAuthContext) context.Context {
	return context.WithValue(ctx, proxyAuthKey, pac)
}

// GetProxyAuth retrieves the ProxyAuthContext from the context.
// Returns nil if not present.
func GetProxyAuth(ctx context.Context) *ProxyAuthContext {
	v, ok := ctx.Value(proxyAuthKey).(*ProxyAuthContext)
	if !ok {
		return nil
	}
	return v
}

// ProxyAuthFromRequest stores a ProxyAuthContext on the request context and
// returns the modified request.
func ProxyAuthFromRequest(r *http.Request, pac *ProxyAuthContext) *http.Request {
	return r.WithContext(WithProxyAuth(r.Context(), pac))
}
