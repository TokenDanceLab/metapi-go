package auth

import (
	"net/http"
	"strconv"
	"regexp"
	"strings"

	"github.com/tokendancelab/metapi-go/config"
)

// bearerPrefixRe is a case-insensitive regex that matches the "Bearer " prefix
// followed by optional whitespace. Used for proxy auth token extraction.
// Matches TS: /^Bearer\s+/i
var bearerPrefixRe = regexp.MustCompile(`(?i)^Bearer\s+`)

// ProxyAuth returns a chi-compatible middleware that authenticates downstream
// proxy clients using managed API keys or the global proxy token.
//
// Token extraction priority (EXCLUSIVE, not a fallback chain):
//   1. If Authorization header exists and is a string → use it exclusively
//      (even if Bearer token is empty; NO fallback to other sources).
//   2. Otherwise, try in order: x-api-key, x-goog-api-key, ?key= query param.
//
// After extraction, the token is passed to AuthorizeDownstreamToken which:
//   - Queries the downstream_api_keys table for a managed key match
//   - Falls back to checking against config.ProxyToken
//   - Returns a ProxyAuthContext on success
//
// On successful managed key auth, the middleware atomically increments
// used_requests via consumeManagedKeyRequest.
func ProxyAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ---- Extract token (EXCLUSIVE priority semantics) ----
			token := extractProxyToken(r)

			if token == "" {
				writeJSON(w, http.StatusUnauthorized, jsonError(
					"Missing Authorization, x-api-key, x-goog-api-key, or key query parameter",
				))
				return
			}

			// ---- Authorize downstream token ----
			result := AuthorizeDownstreamToken(token, cfg)

			if !result.OK {
				writeJSON(w, result.StatusCode, jsonError(result.Error))
				return
			}

			// ---- Reserve managed key request count (atomic quota gate) ----
			if result.Source == "managed" && result.Key != nil {
				if !consumeManagedKeyRequest(result.Key.ID) {
					writeJSON(w, http.StatusForbidden, jsonError("API key has exceeded max requests"))
					return
				}
				// Soft RPM/TPM admission (learn #116). Fail closed with 429 + Retry-After.
				adm := GlobalKeyAdmission.Allow(result.Key.ID, result.Key.MaxRPM, result.Key.MaxTPM, 0)
				if !adm.Allowed {
					msg := "API key rate limit exceeded"
					if adm.Reason == "over_tpm" {
						msg = "API key token-per-minute limit exceeded"
					}
					sec := int(adm.RetryAfter.Seconds())
					if sec < 1 {
						sec = 1
					}
					w.Header().Set("Retry-After", strconv.Itoa(sec))
					writeJSON(w, http.StatusTooManyRequests, jsonError(msg))
					return
				}
			}

			// ---- Build ProxyAuthContext and store in request context ----
			pac := &ProxyAuthContext{
				Token:  result.Token,
				Source: result.Source,
				Policy: result.Policy,
			}
			if result.Key != nil {
				pac.KeyID = &result.Key.ID
				pac.KeyName = result.Key.Name
				// Per-key egress proxy override (FE-KEY-PROXY / #578).
				// Prefer over site/account proxy when non-empty; nil inherits.
				if result.Key.ProxyURL != nil {
					if trimmed := strings.TrimSpace(*result.Key.ProxyURL); trimmed != "" {
						pac.ProxyURL = &trimmed
					}
				}
			} else {
				pac.KeyName = "global"
			}

			r = r.WithContext(WithProxyAuth(r.Context(), pac))
			next.ServeHTTP(w, r)
		})
	}
}

// extractProxyToken extracts the downstream auth token from the request using
// the exact priority rules from TS proxyAuthMiddleware:
//
//   1. If Authorization header is present → EXCLUSIVE source:
//      Strip "Bearer " prefix (case-insensitive regex), then trim whitespace.
//      Result may be empty — this is intentional; NO fallback to other headers.
//   2. If Authorization header is absent → fall back in order:
//      a. x-api-key header (trimmed)
//      b. x-goog-api-key header (trimmed)
//      c. ?key= query parameter (trimmed)
func extractProxyToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")

	// Authorization header is present — exclusive extraction.
	// TS: typeof request.headers.authorization === 'string' ? auth : ''
	// In Go, r.Header.Get returns "" if not present, so this check works.
	if auth != "" {
		// Strip case-insensitive "Bearer " prefix (TS: /^Bearer\s+/i)
		token := bearerPrefixRe.ReplaceAllString(auth, "")
		return strings.TrimSpace(token)
	}

	// Fallback chain — only reached when Authorization is absent/empty.
	if apiKey := strings.TrimSpace(r.Header.Get("x-api-key")); apiKey != "" {
		return apiKey
	}
	if googKey := strings.TrimSpace(r.Header.Get("x-goog-api-key")); googKey != "" {
		return googKey
	}
	if queryKey := strings.TrimSpace(r.URL.Query().Get("key")); queryKey != "" {
		return queryKey
	}

	return ""
}
