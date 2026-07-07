package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"

	"github.com/tokendancelab/metapi-go/config"
)

// ---------------------------------------------------------------------------
// AdminAuth — Bearer token + IP CIDR allowlist middleware.
// Applied to /api/* routes (except public endpoints).
//
// Order of checks (first failure returns immediately):
//   1. Extract client IP from RemoteAddr + normalize.
//   2. If allowlist is non-empty and IP is not allowed → 403
//   3. If Authorization header is missing → 401
//   4. If Bearer token does not match config.AuthToken → 403
//
// Public routes that bypass this middleware:
//   - GET /api/desktop/health
//   - GET /api/oauth/callback/*
// ---------------------------------------------------------------------------

// AdminAuth returns a chi-compatible middleware that enforces admin
// authentication using the given configuration.
func AdminAuth(cfg *config.Config) func(http.Handler) http.Handler {
	// Pre-parse the allowlist once at factory creation time so we don't
	// re-parse string entries on every request.
	parsedAllowlist := parseAllowlist(cfg.AdminIpAllowlist)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ---- Public route bypass ----
			if isPublicAPIRoute(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// ---- 1. Extract client IP ----
			clientIP := extractClientIP(r)
			if !isIPAllowed(clientIP, parsedAllowlist) {
				writeJSON(w, http.StatusForbidden, jsonError("IP not allowed"))
				return
			}

			// ---- 2. Check Authorization header ----
			auth := r.Header.Get("Authorization")
			if auth == "" {
				writeJSON(w, http.StatusUnauthorized, jsonError("Missing Authorization header"))
				return
			}

			// Case-sensitive simple replace of the first "Bearer " literal.
			// TS: token = auth.replace('Bearer ', '')
			token := strings.Replace(auth, "Bearer ", "", 1)
			if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AuthToken)) != 1 {
				writeJSON(w, http.StatusForbidden, jsonError("Invalid token"))
				return
			}

			// ---- Store admin auth marker in context ----
			r = r.WithContext(WithAdminAuth(r.Context()))
			next.ServeHTTP(w, r)
		})
	}
}

// isPublicAPIRoute returns true for routes that do not require admin auth.
// Whitelist: /api/desktop/health, /api/oauth/callback/*
func isPublicAPIRoute(urlPath string) bool {
	if urlPath == "/api/desktop/health" {
		return true
	}
	if strings.HasPrefix(urlPath, "/api/oauth/callback/") {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// IP extraction and normalization.
// ---------------------------------------------------------------------------

// extractClientIP extracts the client IP from RemoteAddr. Forwarded headers are
// intentionally ignored here; router.TrustedRealIP rewrites RemoteAddr only
// when the direct peer matches an explicitly configured trusted proxy CIDR.
func extractClientIP(r *http.Request) string {
	return normalizeIP(stripPort(r.RemoteAddr))
}

// stripPort removes the port suffix from an "IP:port" string.
// If there is no colon, returns the string as-is.
// For IPv6 addresses like "[::1]:1234", strips the brackets and port.
func stripPort(addr string) string {
	// Handle IPv6 bracket notation: [::1]:1234
	if strings.HasPrefix(addr, "[") {
		if idx := strings.LastIndex(addr, "]"); idx >= 0 {
			return addr[1:idx]
		}
		return addr
	}
	// IPv4: 1.2.3.4:1234
	if idx := strings.LastIndexByte(addr, ':'); idx >= 0 {
		return addr[:idx]
	}
	return addr
}

// normalizeIP normalizes an IP address string:
//   - "::ffff:x.x.x.x" → "x.x.x.x" (IPv4-mapped IPv6 → pure IPv4)
//   - "::1" → "127.0.0.1" (IPv6 loopback)
//   - Other values are trimmed and returned as-is.
//
// Mirrors TS normalizeIp() exactly.
func normalizeIP(raw string) string {
	ip := strings.TrimSpace(raw)
	if ip == "" {
		return ""
	}
	if strings.HasPrefix(ip, "::ffff:") {
		return strings.TrimSpace(ip[len("::ffff:"):])
	}
	if ip == "::1" {
		return "127.0.0.1"
	}
	return ip
}

// ---------------------------------------------------------------------------
// IP allowlist parsing and matching.
// ---------------------------------------------------------------------------

// parsedAllowlistEntry represents a single parsed allowlist entry.
type parsedAllowlistEntry struct {
	kind       string // "exact" or "cidr"
	exactIP    string // normalized IP string for exact match
	cidrPrefix netip.Prefix
}

// parseAllowlist converts a list of raw allowlist strings into parsed entries.
// Invalid entries are silently skipped (matches TS parseAllowlistEntry → null).
func parseAllowlist(entries []string) []parsedAllowlistEntry {
	result := make([]parsedAllowlistEntry, 0, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		slashIdx := strings.IndexByte(entry, '/')
		if slashIdx < 0 {
			// Exact IP match
			normalized := normalizeIP(entry)
			if normalized == "" {
				continue
			}
			// Validate it's a real IP (or at least non-empty after normalization)
			if _, err := netip.ParseAddr(normalized); err != nil {
				continue
			}
			result = append(result, parsedAllowlistEntry{
				kind:    "exact",
				exactIP: normalized,
			})
			continue
		}

		// CIDR match — only supports IPv4 CIDR (matches TS behavior)
		// Check for multiple slashes (invalid)
		if strings.IndexByte(entry[slashIdx+1:], '/') >= 0 {
			continue
		}

		networkIP := normalizeIP(entry[:slashIdx])
		prefixText := strings.TrimSpace(entry[slashIdx+1:])

		// Must be a valid IPv4 address and a numeric prefix
		addr, err := netip.ParseAddr(networkIP)
		if err != nil || !addr.Is4() {
			continue
		}

		// Build the CIDR string and parse
		prefix, err := netip.ParsePrefix(networkIP + "/" + prefixText)
		if err != nil {
			continue
		}

		// TS only supports prefix 0-32 for IPv4
		if !prefix.Addr().Is4() {
			continue
		}

		result = append(result, parsedAllowlistEntry{
			kind:       "cidr",
			cidrPrefix: prefix,
		})
	}
	return result
}

// isIPAllowed checks whether the given client IP is allowed by the allowlist.
// An empty allowlist means ALL IPs are allowed.
//
// Matching logic mirrors TS isIpAllowed():
//   - exact entries: compare normalized IP strings
//   - CIDR entries: only match IPv4 clients; pure IPv6 clients cannot pass CIDR checks
func isIPAllowed(clientIP string, allowlist []parsedAllowlistEntry) bool {
	if len(allowlist) == 0 {
		return true // empty allowlist = allow all
	}

	normalized := normalizeIP(clientIP)
	if normalized == "" {
		return false
	}

	clientAddr, clientParseErr := netip.ParseAddr(normalized)
	clientIsAddr := clientParseErr == nil

	for _, entry := range allowlist {
		switch entry.kind {
		case "exact":
			if entry.exactIP == normalized {
				return true
			}
		case "cidr":
			// CIDR entries only match IPv4 clients (matches TS: parseIpv4Value for CIDR)
			if !clientIsAddr || !clientAddr.Is4() {
				continue
			}
			if entry.cidrPrefix.Contains(clientAddr) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// JSON response helpers.
// ---------------------------------------------------------------------------

type jsonErrorBody struct {
	Error string `json:"error"`
}

func jsonError(msg string) jsonErrorBody {
	return jsonErrorBody{Error: msg}
}

func writeJSON(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(body)
}
