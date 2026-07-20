package platform

import (
	"net/http"
	"strings"

	"github.com/tokendancelab/metapi-go/proxy/profiles"
)

// IsDeniedCustomHeader reports whether a site custom_headers entry must not be
// forwarded upstream. Blocks identity, framing, hop-by-hop, cookies, Proxy-*,
// and MetAPI control preference headers.
//
// Security: custom Authorization must never override the Bearer set from the
// selected account token; Host/Connection/etc. must not be attacker-controlled.
func IsDeniedCustomHeader(name string) bool {
	if profiles.IsMetapiControlHeader(name) {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	// Proxy-* covers Proxy-Authorization, Proxy-Connection, Proxy-Authenticate, etc.
	if strings.HasPrefix(n, "proxy-") {
		return true
	}
	switch n {
	case "authorization",
		"host",
		"content-length",
		"transfer-encoding",
		"connection",
		"cookie",
		// RFC 7230 hop-by-hop (beyond Connection / Transfer-Encoding).
		"keep-alive",
		"te",
		"trailer",
		"trailers",
		"upgrade":
		return true
	default:
		return false
	}
}

// ApplyCustomHeadersOptions controls collision policy for site custom headers.
// When OverrideRequest is false (default / #584 request-wins), a site header is
// applied only if the request does not already carry that header. When true
// (site-wins), site headers always Set over existing request values — matching
// pre-#584 Go behavior and upstream customHeadersOverrideRequestHeaders=true.
type ApplyCustomHeadersOptions struct {
	OverrideRequest bool
}

// ApplyCustomHeaders sets non-denied custom headers onto req using the default
// request-wins policy (does not overwrite existing request headers). Prefer
// ApplyCustomHeadersWithOptions when the site flag is known.
// Nil req or empty map is a no-op.
func ApplyCustomHeaders(req *http.Request, custom map[string]string) {
	ApplyCustomHeadersWithOptions(req, custom, ApplyCustomHeadersOptions{})
}

// ApplyCustomHeadersWithOptions sets non-denied custom headers onto req.
// OverrideRequest=true → site-wins (Header.Set). OverrideRequest=false →
// request-wins (skip when req already has a non-empty value for that header).
func ApplyCustomHeadersWithOptions(req *http.Request, custom map[string]string, opts ApplyCustomHeadersOptions) {
	if req == nil || len(custom) == 0 {
		return
	}
	for k, v := range custom {
		if IsDeniedCustomHeader(k) {
			continue
		}
		if !opts.OverrideRequest && requestHasHeader(req, k) {
			continue
		}
		req.Header.Set(k, v)
	}
}

// requestHasHeader reports whether req already carries a non-empty value for name
// (case-insensitive CanonicalHeaderKey lookup).
func requestHasHeader(req *http.Request, name string) bool {
	if req == nil || req.Header == nil {
		return false
	}
	return strings.TrimSpace(req.Header.Get(name)) != ""
}
