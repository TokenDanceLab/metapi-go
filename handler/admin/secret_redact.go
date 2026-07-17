package admin

import (
	"encoding/json"
	"strings"

	"github.com/tokendancelab/metapi-go/service"
)

// maskAccountSecret masks account-level credential material for list/get/summary
// responses. Prefer this over returning plaintext accessToken/apiToken (#367).
// Empty input stays empty so absence remains distinguishable from presence.
func maskAccountSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	return service.MaskToken(secret, "")
}

// maskOptionalAccountSecret masks a nullable account secret pointer for JSON.
func maskOptionalAccountSecret(secret *string) any {
	if secret == nil {
		return nil
	}
	masked := maskAccountSecret(*secret)
	if masked == "" {
		return nil
	}
	return masked
}

// redactAccountSecrets rewrites account-shaped admin JSON maps so list/get/summary
// never return plaintext accessToken/apiToken. Masked values stay under the same
// field names so existing clients that only need presence/prefix keep working.
// Nested extraConfig secrets (refreshToken, passwordCipher, oauth tokens) are
// also scrubbed when present as JSON text or nested maps.
//
// Intentional create-once / rebind / verify / export paths must not call this.
func redactAccountSecrets(row map[string]any) {
	if row == nil {
		return
	}
	if v, ok := row["accessToken"].(string); ok {
		row["accessToken"] = maskAccountSecret(v)
	}
	switch v := row["apiToken"].(type) {
	case string:
		row["apiToken"] = maskAccountSecret(v)
	case *string:
		if v == nil {
			row["apiToken"] = nil
		} else {
			masked := maskAccountSecret(*v)
			if masked == "" {
				row["apiToken"] = nil
			} else {
				row["apiToken"] = masked
			}
		}
	}
	if ec, ok := row["extraConfig"]; ok {
		row["extraConfig"] = redactExtraConfigSecrets(ec)
	}
}

// redactAccountTokenSecrets removes plaintext token material from account-token
// list/search/default maps. Prefer tokenMasked (set by callers when known).
func redactAccountTokenSecrets(row map[string]any) {
	if row == nil {
		return
	}
	if token, ok := row["token"].(string); ok && token != "" {
		if _, hasMasked := row["tokenMasked"]; !hasMasked {
			platform := ""
			if site, ok := row["site"].(map[string]any); ok {
				if p, ok := site["platform"].(string); ok {
					platform = p
				}
			}
			if p, ok := row["sitePlatform"].(string); ok && platform == "" {
				platform = p
			}
			row["tokenMasked"] = service.MaskToken(token, platform)
		}
	}
	delete(row, "token")
	// Nested account join fields from search must not leak session secrets.
	if account, ok := row["account"].(map[string]any); ok {
		redactAccountSecrets(account)
	}
	// Flat join columns from SELECT at.*, a.access_token ...
	if v, ok := row["accessToken"].(string); ok {
		row["accessToken"] = maskAccountSecret(v)
	}
	if v, ok := row["apiToken"].(string); ok {
		row["apiToken"] = maskAccountSecret(v)
	}
}

// redactExtraConfigSecrets scrubs nested secret fields while preserving non-secret
// config used by the admin UI (credentialMode, proxyUrl, platformUserId, etc.).
func redactExtraConfigSecrets(extra any) any {
	switch typed := extra.(type) {
	case nil:
		return nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return typed
		}
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return typed
		}
		redacted := scrubExtraConfigValue(parsed)
		b, err := json.Marshal(redacted)
		if err != nil {
			return typed
		}
		return string(b)
	case *string:
		if typed == nil {
			return nil
		}
		out := redactExtraConfigSecrets(*typed)
		if s, ok := out.(string); ok {
			return s
		}
		return out
	case map[string]any:
		return scrubExtraConfigValue(typed)
	default:
		return extra
	}
}

func scrubExtraConfigValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, child := range typed {
			switch k {
			case "refreshToken", "passwordCipher", "accessToken", "apiToken",
				"idToken", "access_token", "refresh_token", "api_token":
				if s, ok := child.(string); ok {
					out[k] = maskAccountSecret(s)
				} else if child == nil {
					out[k] = nil
				} else {
					out[k] = "****"
				}
			case "oauth":
				out[k] = scrubOAuthConfig(child)
			case "sub2apiAuth", "autoRelogin":
				out[k] = scrubExtraConfigValue(child)
			default:
				out[k] = scrubExtraConfigValue(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = scrubExtraConfigValue(child)
		}
		return out
	default:
		return v
	}
}

func scrubOAuthConfig(v any) any {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return v
	}
	out := make(map[string]any, len(m))
	for k, child := range m {
		switch k {
		case "refreshToken", "accessToken", "idToken", "access_token", "refresh_token", "token":
			if s, ok := child.(string); ok {
				out[k] = maskAccountSecret(s)
			} else if child == nil {
				out[k] = nil
			} else {
				out[k] = "****"
			}
		default:
			out[k] = scrubExtraConfigValue(child)
		}
	}
	return out
}

// enrichRouteChannelAccount builds a redacted account summary for route channel
// list/get surfaces. Presence of accessToken/apiToken is preserved via masks so
// FE connection-mode heuristics remain usable without plaintext secrets (#367).
func enrichRouteChannelAccount(ch map[string]any) map[string]any {
	access, _ := ch["accessToken"].(string)
	apiRaw := ch["apiToken"]
	var apiMasked any
	switch v := apiRaw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			apiMasked = nil
		} else {
			apiMasked = maskAccountSecret(v)
		}
	case []byte:
		s := string(v)
		if strings.TrimSpace(s) == "" {
			apiMasked = nil
		} else {
			apiMasked = maskAccountSecret(s)
		}
	default:
		if apiRaw == nil {
			apiMasked = nil
		} else if s, ok := apiRaw.(string); ok {
			apiMasked = maskAccountSecret(s)
		} else {
			apiMasked = nil
		}
	}
	return map[string]any{
		"id":          ch["accountId"],
		"username":    ch["username"],
		"accessToken": maskAccountSecret(access),
		"apiToken":    apiMasked,
		"balance":     ch["balance"],
		"status":      ch["accountStatus"],
	}
}
