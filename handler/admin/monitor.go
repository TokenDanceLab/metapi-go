package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
)

// RegisterMonitorRoutes registers all /api/monitor and /monitor-proxy routes.
func RegisterMonitorRoutes(r chi.Router, db *sqlx.DB, cfg *config.Config) {
	handler := &monitorHandler{db: db, cfg: cfg}

	r.Get("/api/monitor/config", handler.getConfig)
	r.Put("/api/monitor/config", handler.saveConfig)
	r.Post("/api/monitor/session", handler.createSession)
	// DELETE clears the HttpOnly meta_monitor_auth cookie on admin logout.
	// Frontend JS cannot clear HttpOnly cookies; this is the honest clear path.
	r.Delete("/api/monitor/session", handler.clearSession)

	// LDOH proxy routes - rate limited at 60 req/min at the router layer when wired.
	r.HandleFunc("/monitor-proxy/ldoh", handler.ldohProxy)
	r.HandleFunc("/monitor-proxy/ldoh/", handler.ldohProxy)
	r.HandleFunc("/monitor-proxy/ldoh/*", handler.ldohProxy)
}

type monitorHandler struct {
	db  *sqlx.DB
	cfg *config.Config
}

const ldohCookieSettingKey = "monitor_ldoh_cookie"
const monitorAuthCookie = "meta_monitor_auth"
const ldohBaseURL = "https://ldoh.105117.xyz"

// monitorCookiePath is scoped to the iframe proxy surface only.
// Path=/ is intentionally avoided so a stolen cookie cannot be presented
// to other same-origin endpoints. Iframe + "open proxy" targets are both
// under /monitor-proxy/, so this does not break the embed flow.
const monitorCookiePath = "/monitor-proxy/"

// monitorSessionMaxAge matches the previous 2h cookie lifetime.
const monitorSessionMaxAge = 7200

// monitorSessionPurpose is the fixed HMAC message. Changing it rotates all
// outstanding monitor cookies without touching AuthToken itself.
const monitorSessionPurpose = "metapi-monitor-session-v1"

// GET /api/monitor/config
func (h *monitorHandler) getConfig(w http.ResponseWriter, r *http.Request) {
	ldohCookie := getSettingValue(h.db, ldohCookieSettingKey)
	writeJSON(w, http.StatusOK, map[string]any{
		"ldohCookieConfigured": ldohCookie != "",
		"ldohCookieMasked":     maskValue(ldohCookie),
	})
}

// PUT /api/monitor/config
func (h *monitorHandler) saveConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LdohCookie *string `json:"ldohCookie"`
	}
	rawBody, readErr := readAdminJSONBody(r.Body)
	if readErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}
	if len(strings.TrimSpace(string(rawBody))) > 0 {
		if err := rejectDuplicateJSONKeys(rawBody); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success": false,
				"message": "Invalid request body",
			})
			return
		}
		if err := json.Unmarshal(rawBody, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success": false,
				"message": "Invalid request body",
			})
			return
		}
	}

	raw := ""
	if body.LdohCookie != nil {
		raw = strings.TrimSpace(*body.LdohCookie)
	}

	if raw == "" {
		if err := upsertSettingDB(h.db, ldohCookieSettingKey, ""); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"success": false,
				"message": "Failed to clear LDOH cookie",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":              true,
			"message":              "LDOH Cookie 已清空",
			"ldohCookieConfigured": false,
			"ldohCookieMasked":     "",
		})
		return
	}

	normalized := normalizeLdohCookie(raw)
	if !strings.HasPrefix(normalized, "ld_auth_session=") || len(normalized) < 24 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Cookie 格式无效，请填写 ld_auth_session 或其值",
		})
		return
	}

	if err := upsertSettingDB(h.db, ldohCookieSettingKey, normalized); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to save LDOH cookie",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":              true,
		"message":              "LDOH Cookie 已保存",
		"ldohCookieConfigured": true,
		"ldohCookieMasked":     maskValue(normalized),
	})
}

// POST /api/monitor/session
// Mints an opaque monitor-proxy cookie derived from AuthToken via HMAC.
// The raw AUTH_TOKEN is never written into the cookie value: cookie theft
// must not yield a usable admin Bearer credential for /api/*.
func (h *monitorHandler) createSession(w http.ResponseWriter, r *http.Request) {
	session := deriveMonitorSessionToken(h.cfg.AuthToken)
	http.SetCookie(w, newMonitorAuthCookie(session, monitorSessionMaxAge, isMonitorHTTPS(r)))
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// DELETE /api/monitor/session
// Clears the HttpOnly meta_monitor_auth cookie with Path matching createSession.
// Admin logout must call this while the Bearer is still valid: after frontend
// clears localStorage, the opaque cookie would otherwise remain usable for
// /monitor-proxy/* until Max-Age (AuthToken is unchanged on logout).
func (h *monitorHandler) clearSession(w http.ResponseWriter, r *http.Request) {
	clearMonitorAuthCookies(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// newMonitorAuthCookie builds the shared cookie attributes for mint + clear so
// Path/HttpOnly/SameSite/Secure stay in lockstep (browsers only clear when the
// Path matches the cookie that was originally set).
func newMonitorAuthCookie(value string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     monitorAuthCookie,
		Value:    value,
		Path:     monitorCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
		Secure:   secure,
	}
}

// clearMonitorAuthCookies expires meta_monitor_auth for the scoped path used
// by createSession. Also expires a legacy Path=/ cookie in case an older
// release minted one before the /monitor-proxy/ scope landed (#407).
//
// net/http serializes MaxAge < 0 as Max-Age=0 (immediate expiry).
func clearMonitorAuthCookies(w http.ResponseWriter, r *http.Request) {
	secure := isMonitorHTTPS(r)
	http.SetCookie(w, newMonitorAuthCookie("", -1, secure))
	// Legacy Path=/ residual from pre-#407 createSession.
	http.SetCookie(w, &http.Cookie{
		Name:     monitorAuthCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Secure:   secure,
	})
}

// GET/POST/ALL /monitor-proxy/ldoh and /monitor-proxy/ldoh/*
func (h *monitorHandler) ldohProxy(w http.ResponseWriter, r *http.Request) {
	if !h.ensureMonitorAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "Missing or invalid monitor session",
		})
		return
	}

	storedCookie := getSettingValue(h.db, ldohCookieSettingKey)
	if storedCookie == "" {
		// Keep plain-text 400 parity with TS for the unconfigured cookie path.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("LDOH cookie not configured"))
		return
	}

	wildcardPath := resolveLdohProxyPath(r)
	targetURL, err := url.Parse(ldohBaseURL + "/" + wildcardPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid proxy path"})
		return
	}
	q := targetURL.Query()
	for key, values := range r.URL.Query() {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	targetURL.RawQuery = q.Encode()

	method := strings.ToUpper(r.Method)
	var body io.Reader
	if method != http.MethodGet && method != http.MethodHead {
		body = r.Body
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), method, targetURL.String(), body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "failed to build upstream request"})
		return
	}
	upstreamReq.Header.Set("Cookie", storedCookie)
	upstreamReq.Header.Set("Accept", firstNonEmpty(r.Header.Get("Accept"), "*/*"))
	upstreamReq.Header.Set("Accept-Language", firstNonEmpty(r.Header.Get("Accept-Language"), "zh-CN,zh;q=0.9,en;q=0.8"))
	upstreamReq.Header.Set("User-Agent", firstNonEmpty(r.Header.Get("User-Agent"), "metapiMonitorProxy/1.0"))
	if ct := r.Header.Get("Content-Type"); ct != "" {
		upstreamReq.Header.Set("Content-Type", ct)
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		upstreamReq.Header.Set("Referer", strings.ReplaceAll(referer, "/monitor-proxy/ldoh", ""))
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	upstreamResp, err := client.Do(upstreamReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": "LDOH upstream request failed: " + err.Error(),
		})
		return
	}
	defer upstreamResp.Body.Close()

	contentType := upstreamResp.Header.Get("Content-Type")
	if location := rewriteLocationHeader(upstreamResp.Header.Get("Location")); location != "" {
		w.Header().Set("Location", location)
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if cacheControl := upstreamResp.Header.Get("Cache-Control"); cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}

	w.WriteHeader(upstreamResp.StatusCode)

	if shouldRewriteProxyBody(contentType) {
		raw, readErr := io.ReadAll(upstreamResp.Body)
		if readErr != nil {
			return
		}
		_, _ = w.Write([]byte(rewriteProxyText(string(raw))))
		return
	}

	_, _ = io.Copy(w, upstreamResp.Body)
}

func (h *monitorHandler) ensureMonitorAuth(r *http.Request) bool {
	cookies := parseCookieHeader(r.Header.Get("Cookie"))
	got := cookies[monitorAuthCookie]
	if got == "" {
		return false
	}
	// Never accept the raw AuthToken as a monitor session.
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.cfg.AuthToken)) == 1 {
		return false
	}
	expected := deriveMonitorSessionToken(h.cfg.AuthToken)
	if len(got) != len(expected) {
		// Length mismatch: still run a dummy compare to keep timing flat-ish,
		// then deny.
		_ = subtle.ConstantTimeCompare([]byte(expected), []byte(expected))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// deriveMonitorSessionToken returns an opaque, AuthToken-bound session value.
// HMAC-SHA256(key=AuthToken, msg=purpose) means:
//   - cookie value is never the live AUTH_TOKEN
//   - AuthToken rotation automatically invalidates outstanding cookies
//   - no server-side session store is required
func deriveMonitorSessionToken(authToken string) string {
	mac := hmac.New(sha256.New, []byte(authToken))
	_, _ = mac.Write([]byte(monitorSessionPurpose))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// isMonitorHTTPS reports whether the request arrived over TLS (directly or
// via a reverse proxy that sets X-Forwarded-Proto). Cookie.Secure is set
// when true; deployments that terminate TLS elsewhere should forward that
// header so the Secure flag is applied.
func isMonitorHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func parseCookieHeader(raw string) map[string]string {
	result := map[string]string{}
	if raw == "" {
		return result
	}
	for _, part := range strings.Split(raw, ";") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		idx := strings.Index(entry, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(entry[:idx])
		value := strings.TrimSpace(entry[idx+1:])
		if key == "" {
			continue
		}
		result[key] = value
	}
	return result
}

func normalizeLdohCookie(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "ld_auth_session=") {
		firstPair := strings.TrimSpace(strings.Split(trimmed, ";")[0])
		if strings.HasPrefix(firstPair, "ld_auth_session=") {
			return firstPair
		}
	}
	return "ld_auth_session=" + trimmed
}

func resolveLdohProxyPath(r *http.Request) string {
	cleanPath := r.URL.Path
	prefix := "/monitor-proxy/ldoh"
	if cleanPath == prefix || cleanPath == prefix+"/" {
		return ""
	}
	if strings.HasPrefix(cleanPath, prefix+"/") {
		return strings.TrimPrefix(cleanPath, prefix+"/")
	}
	if star := chi.URLParam(r, "*"); star != "" {
		return strings.TrimPrefix(star, "/")
	}
	return ""
}

func rewriteProxyText(text string) string {
	replacer := strings.NewReplacer(
		"https://ldoh.105117.xyz/", "/monitor-proxy/ldoh/",
		`https:\/\/ldoh.105117.xyz\/`, `\/monitor-proxy\/ldoh\/`,
		`src="/`, `src="/monitor-proxy/ldoh/`,
		`src='/`, `src='/monitor-proxy/ldoh/`,
		`href="/`, `href="/monitor-proxy/ldoh/`,
		`href='/`, `href='/monitor-proxy/ldoh/`,
		`action="/`, `action="/monitor-proxy/ldoh/`,
		`action='/`, `action='/monitor-proxy/ldoh/`,
		`"/_next/`, `"/monitor-proxy/ldoh/_next/`,
		`'/_next/`, `'/monitor-proxy/ldoh/_next/`,
		`"\/api/`, `"\/monitor-proxy\/ldoh\/api/`,
		`'/api/`, `'/monitor-proxy/ldoh/api/`,
		`"/api/`, `"/monitor-proxy/ldoh/api/`,
	)
	return replacer.Replace(text)
}

func rewriteLocationHeader(location string) string {
	if location == "" {
		return ""
	}
	if strings.HasPrefix(location, ldohBaseURL+"/") {
		return "/monitor-proxy/ldoh/" + strings.TrimPrefix(location, ldohBaseURL+"/")
	}
	if strings.HasPrefix(location, "/") {
		return "/monitor-proxy/ldoh" + location
	}
	return location
}

func shouldRewriteProxyBody(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "application/javascript") ||
		strings.Contains(ct, "text/javascript") ||
		strings.Contains(ct, "text/css") ||
		strings.Contains(ct, "application/json")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func maskValue(value string) string {
	if value == "" {
		return ""
	}
	// Prefer masking the cookie value portion after '=' when present.
	raw := value
	if idx := strings.Index(value, "="); idx >= 0 {
		raw = value[idx+1:]
	}
	if len(raw) <= 10 {
		if len(raw) < 2 {
			return "****"
		}
		return raw[:2] + "****"
	}
	return raw[:6] + "****" + raw[len(raw)-4:]
}

func getSettingValue(db *sqlx.DB, key string) string {
	var row struct {
		Value *string `db:"value"`
	}
	err := db.Get(&row, db.Rebind("SELECT value FROM settings WHERE key = ?"), key)
	if err != nil || row.Value == nil {
		return ""
	}
	// Value is stored as JSON-encoded string
	val := strings.TrimSpace(*row.Value)
	if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
		var unquoted string
		if err := json.Unmarshal([]byte(val), &unquoted); err == nil {
			return unquoted
		}
		val = val[1 : len(val)-1]
	}
	return val
}
