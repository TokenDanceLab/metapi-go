package admin

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Credential export format version. Bump when profile payload shapes change
// in a non-compatible way; adapters should pin against this field.
const credentialExportFormatVersion = "1"

// Known export profile IDs. Keep stable for clients and docs.
const (
	exportProfileOpenAI  = "openai"
	exportProfileCherry  = "cherry"
	exportProfileGeneric = "generic"
)

// GET /api/downstream-keys/{id}/export?profile=openai|cherry|generic&baseUrl=
//
// Admin-only (inherits /api/* auth group). Emits ready-to-copy client config
// snippets for the selected downstream key. Secrets are limited to the key
// value the admin list/overview APIs already expose — never invents upstream
// tokens or other credentials.
func (h *downstreamKeysHandler) exportKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id 无效")
		return
	}

	row := queryRow(h.db, "SELECT * FROM downstream_api_keys WHERE id = ?", id)
	if row == nil {
		writeError(w, http.StatusNotFound, "API key 不存在")
		return
	}

	key := strings.TrimSpace(existingString(row, "key"))
	if key == "" {
		writeError(w, http.StatusInternalServerError, "API key 数据异常")
		return
	}
	name := existingString(row, "name")

	baseURL, baseErr := resolveExportBaseURL(r, h.lookupPublicBaseURLSetting)
	if baseErr != "" {
		writeError(w, http.StatusBadRequest, baseErr)
		return
	}

	requested := normalizeExportProfileFilter(r.URL.Query().Get("profile"))
	if requested != "" && !isKnownExportProfile(requested) {
		writeError(w, http.StatusBadRequest, "profile 无效，支持: openai, cherry, generic")
		return
	}

	profiles := buildCredentialExportProfiles(name, key, baseURL, requested)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"formatVersion": credentialExportFormatVersion,
		"keyId":         id,
		"keyName":       name,
		"keyMasked":     maskKey(key),
		"baseUrl":       baseURL,
		"profiles":      profiles,
	})
}

// lookupPublicBaseURLSetting reads an optional settings-table override.
// Empty when the table is missing the key or the DB is unavailable.
func (h *downstreamKeysHandler) lookupPublicBaseURLSetting() string {
	if h == nil || h.db == nil {
		return ""
	}
	var value string
	// Settings values may be JSON-encoded strings ("\"https://…\"") or plain text.
	err := h.db.Get(&value, rebindAdminQuery(h.db, "SELECT value FROM settings WHERE key = ? LIMIT 1"), "public_base_url")
	if err != nil {
		return ""
	}
	return unwrapSettingString(value)
}

func unwrapSettingString(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	// JSON string encoding: "\"https://example.com\""
	if strings.HasPrefix(v, "\"") {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return strings.TrimSpace(unquoted)
		}
	}
	return v
}

func normalizeExportProfileFilter(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isKnownExportProfile(id string) bool {
	switch id {
	case exportProfileOpenAI, exportProfileCherry, exportProfileGeneric:
		return true
	default:
		return false
	}
}

// resolveExportBaseURL picks a public base URL for client config snippets.
//
// Priority:
//  1. ?baseUrl= query (explicit operator override)
//  2. PUBLIC_BASE_URL environment variable
//  3. settings.public_base_url (optional DB override, when lookup provided)
//  4. Request host (+ X-Forwarded-* when present)
func resolveExportBaseURL(r *http.Request, settingsLookup func() string) (string, string) {
	if r != nil {
		if q := strings.TrimSpace(r.URL.Query().Get("baseUrl")); q != "" {
			normalized, errMsg := normalizeExportBaseURL(q)
			if errMsg != "" {
				return "", errMsg
			}
			return normalized, ""
		}
	}

	if env := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); env != "" {
		normalized, errMsg := normalizeExportBaseURL(env)
		if errMsg != "" {
			return "", "PUBLIC_BASE_URL 无效: " + errMsg
		}
		return normalized, ""
	}

	if settingsLookup != nil {
		if s := strings.TrimSpace(settingsLookup()); s != "" {
			normalized, errMsg := normalizeExportBaseURL(s)
			if errMsg == "" {
				return normalized, ""
			}
			// Fall through to request host when settings value is malformed.
		}
	}

	if r != nil {
		if hostDerived := deriveBaseURLFromRequest(r); hostDerived != "" {
			return hostDerived, ""
		}
	}

	return "", "无法确定 baseUrl：请传 ?baseUrl= 或配置 PUBLIC_BASE_URL"
}

func normalizeExportBaseURL(raw string) (string, string) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", "baseUrl 不能为空"
	}
	// Allow scheme-less hosts by assuming https.
	if !strings.Contains(v, "://") {
		v = "https://" + v
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return "", "baseUrl 格式无效"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "baseUrl 必须以 http:// 或 https:// 开头"
	}
	// Strip path/query/fragment — clients append /v1 themselves when needed.
	// Keep trailing-slash free origin.
	normalized := scheme + "://" + u.Host
	return normalized, ""
}

func deriveBaseURLFromRequest(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	// X-Forwarded-Host may be a comma-separated list; take the first.
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if host == "" {
		return ""
	}
	// Reject obviously unsafe hosts (empty port-only, etc.).
	if h, _, err := net.SplitHostPort(host); err == nil {
		if strings.TrimSpace(h) == "" {
			return ""
		}
	}

	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if i := strings.IndexByte(scheme, ','); i >= 0 {
		scheme = strings.TrimSpace(scheme[:i])
	}
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	normalized, errMsg := normalizeExportBaseURL(scheme + "://" + host)
	if errMsg != "" {
		return ""
	}
	return normalized
}

func buildCredentialExportProfiles(name, key, baseURL, filter string) []map[string]any {
	openaiBase := strings.TrimRight(baseURL, "/") + "/v1"
	all := []map[string]any{
		buildOpenAIExportProfile(key, openaiBase),
		buildCherryExportProfile(name, key, baseURL),
		buildGenericExportProfile(name, key, baseURL, openaiBase),
	}
	if filter == "" {
		return all
	}
	out := make([]map[string]any, 0, 1)
	for _, p := range all {
		if id, _ := p["id"].(string); id == filter {
			out = append(out, p)
		}
	}
	return out
}

func buildOpenAIExportProfile(key, openaiBaseURL string) map[string]any {
	envBlock := "OPENAI_API_KEY=" + key + "\nOPENAI_BASE_URL=" + openaiBaseURL + "\n"
	content := map[string]any{
		"OPENAI_API_KEY":  key,
		"OPENAI_BASE_URL": openaiBaseURL,
	}
	return map[string]any{
		"id":          exportProfileOpenAI,
		"label":       "OpenAI-compatible env",
		"format":      "env",
		"description": "Environment variables for OpenAI SDKs and CLI tools (base URL includes /v1).",
		"content":     content,
		"snippet":     envBlock,
	}
}

func buildCherryExportProfile(name, key, baseURL string) map[string]any {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = "MetAPI"
	}
	// Cherry Studio / CCR-style provider entry: OpenAI-compatible relay.
	content := map[string]any{
		"id":      "metapi",
		"name":    providerName,
		"type":    "openai",
		"apiKey":  key,
		"apiHost": baseURL,
		"baseUrl": baseURL,
		"enabled": true,
	}
	snippetBytes, _ := jsonMarshalPretty(content)
	return map[string]any{
		"id":          exportProfileCherry,
		"label":       "Cherry Studio / CCR",
		"format":      "json",
		"description": "OpenAI-compatible provider block for Cherry Studio and similar clients.",
		"content":     content,
		"snippet":     string(snippetBytes),
	}
}

func buildGenericExportProfile(name, key, baseURL, openaiBaseURL string) map[string]any {
	content := map[string]any{
		"name":             name,
		"apiKey":           key,
		"baseUrl":          baseURL,
		"openaiBaseUrl":    openaiBaseURL,
		"openaiCompatible": true,
		"authHeader":       "Authorization",
		"authScheme":       "Bearer",
	}
	snippetBytes, _ := jsonMarshalPretty(content)
	return map[string]any{
		"id":          exportProfileGeneric,
		"label":       "Generic JSON",
		"format":      "json",
		"description": "Neutral JSON for custom clients and automation.",
		"content":     content,
		"snippet":     string(snippetBytes),
	}
}

func jsonMarshalPretty(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
