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

// ApplyCustomHeaders sets non-denied custom headers onto req.
// Nil req or empty map is a no-op.
func ApplyCustomHeaders(req *http.Request, custom map[string]string) {
	if req == nil || len(custom) == 0 {
		return
	}
	for k, v := range custom {
		if IsDeniedCustomHeader(k) {
			continue
		}
		req.Header.Set(k, v)
	}
}
