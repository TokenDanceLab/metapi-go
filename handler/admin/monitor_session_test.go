package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorSession_CookieIsOpaqueNotAuthToken(t *testing.T) {
	_, r, cfg := setupOpsAdminStubsTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/monitor/session", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success = %v, want true", body["success"])
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == monitorAuthCookie {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("Set-Cookie missing %s; headers=%v", monitorAuthCookie, rec.Header().Values("Set-Cookie"))
	}
	if sessionCookie.Value == "" {
		t.Fatal("monitor session cookie value is empty")
	}
	if sessionCookie.Value == cfg.AuthToken {
		t.Fatalf("cookie value must not equal AuthToken (cookie theft must not yield admin bearer)")
	}
	if !sessionCookie.HttpOnly {
		t.Fatal("monitor session cookie must be HttpOnly")
	}
	if sessionCookie.Path != monitorCookiePath {
		t.Fatalf("cookie Path = %q, want %q (scoped to proxy surface)", sessionCookie.Path, monitorCookiePath)
	}
	if sessionCookie.MaxAge != monitorSessionMaxAge {
		t.Fatalf("cookie MaxAge = %d, want %d", sessionCookie.MaxAge, monitorSessionMaxAge)
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v, want Lax", sessionCookie.SameSite)
	}
	expected := deriveMonitorSessionToken(cfg.AuthToken)
	if sessionCookie.Value != expected {
		t.Fatalf("cookie value = %q, want derived session %q", sessionCookie.Value, expected)
	}
}

func TestMonitorSession_CookieSecureWhenHTTPS(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/monitor/session", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == monitorAuthCookie {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("missing monitor session cookie")
	}
	if !sessionCookie.Secure {
		t.Fatal("cookie Secure must be set when X-Forwarded-Proto=https")
	}
}

func TestMonitorAuth_ValidOpaqueCookieAccepted(t *testing.T) {
	_, r, cfg := setupOpsAdminStubsTest(t)

	req := httptest.NewRequest(http.MethodGet, "/monitor-proxy/ldoh/", nil)
	req.AddCookie(&http.Cookie{
		Name:  monitorAuthCookie,
		Value: deriveMonitorSessionToken(cfg.AuthToken),
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Auth ok; LDOH cookie not configured → 400 plain text (not 401).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400 after valid session", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LDOH cookie not configured") {
		t.Fatalf("body = %q, want unconfigured cookie message", rec.Body.String())
	}
}

func TestMonitorAuth_InvalidCookieDenied(t *testing.T) {
	_, r, cfg := setupOpsAdminStubsTest(t)

	cases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "garbage", value: "not-a-valid-session"},
		{name: "raw-auth-token", value: cfg.AuthToken},
		{name: "wrong-hmac", value: deriveMonitorSessionToken(cfg.AuthToken + "-other")},
		{name: "truncated", value: deriveMonitorSessionToken(cfg.AuthToken)[:8]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/monitor-proxy/ldoh/", nil)
			if tc.value != "" {
				req.AddCookie(&http.Cookie{Name: monitorAuthCookie, Value: tc.value})
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body=%s, want 401", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "Missing or invalid monitor session") {
				t.Fatalf("body = %q, want invalid session message", rec.Body.String())
			}
		})
	}
}

func TestMonitorAuth_AuthTokenRotationInvalidatesCookie(t *testing.T) {
	_, r, cfg := setupOpsAdminStubsTest(t)

	oldToken := "old-admin-token"
	newToken := "new-admin-token"
	if deriveMonitorSessionToken(oldToken) == deriveMonitorSessionToken(newToken) {
		t.Fatal("session must change when AuthToken rotates")
	}

	cfg.AuthToken = oldToken
	minted := deriveMonitorSessionToken(cfg.AuthToken)

	// Valid under current token.
	reqOK := httptest.NewRequest(http.MethodGet, "/monitor-proxy/ldoh/", nil)
	reqOK.AddCookie(&http.Cookie{Name: monitorAuthCookie, Value: minted})
	recOK := httptest.NewRecorder()
	r.ServeHTTP(recOK, reqOK)
	if recOK.Code == http.StatusUnauthorized {
		t.Fatalf("pre-rotation cookie should be accepted; body=%s", recOK.Body.String())
	}

	// Rotate AuthToken: previous cookie must be rejected.
	cfg.AuthToken = newToken
	reqDenied := httptest.NewRequest(http.MethodGet, "/monitor-proxy/ldoh/", nil)
	reqDenied.AddCookie(&http.Cookie{Name: monitorAuthCookie, Value: minted})
	recDenied := httptest.NewRecorder()
	r.ServeHTTP(recDenied, reqDenied)
	if recDenied.Code != http.StatusUnauthorized {
		t.Fatalf("post-rotation status = %d body=%s, want 401", recDenied.Code, recDenied.Body.String())
	}
}

func TestMonitorSession_ClearCookieOnLogout(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/monitor/session", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success = %v, want true", body["success"])
	}

	cookies := rec.Result().Cookies()
	var scoped *http.Cookie
	var legacyRoot *http.Cookie
	for _, c := range cookies {
		if c.Name != monitorAuthCookie {
			continue
		}
		switch c.Path {
		case monitorCookiePath:
			scoped = c
		case "/":
			legacyRoot = c
		}
	}
	if scoped == nil {
		t.Fatalf("missing clear cookie for Path=%q; cookies=%v", monitorCookiePath, cookies)
	}
	if scoped.MaxAge >= 0 {
		t.Fatalf("scoped clear MaxAge = %d, want < 0 (Max-Age=0 on wire)", scoped.MaxAge)
	}
	if scoped.Value != "" {
		t.Fatalf("scoped clear Value = %q, want empty", scoped.Value)
	}
	if !scoped.HttpOnly {
		t.Fatal("scoped clear cookie must remain HttpOnly")
	}
	if scoped.SameSite != http.SameSiteLaxMode {
		t.Fatalf("scoped clear SameSite = %v, want Lax", scoped.SameSite)
	}
	// Legacy Path=/ residual clear keeps older pre-#407 cookies from surviving logout.
	if legacyRoot == nil {
		t.Fatal("missing legacy Path=/ clear cookie")
	}
	if legacyRoot.MaxAge >= 0 {
		t.Fatalf("legacy clear MaxAge = %d, want < 0", legacyRoot.MaxAge)
	}
}

func TestMonitorSession_ClearCookieSecureWhenHTTPS(t *testing.T) {
	_, r, _ := setupOpsAdminStubsTest(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/monitor/session", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var scoped *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == monitorAuthCookie && c.Path == monitorCookiePath {
			scoped = c
			break
		}
	}
	if scoped == nil {
		t.Fatal("missing scoped clear cookie")
	}
	if !scoped.Secure {
		t.Fatal("clear cookie Secure must match createSession when X-Forwarded-Proto=https")
	}
}

func TestClearMonitorAuthCookies_HelperContract(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/monitor/session", nil)
	clearMonitorAuthCookies(rec, req)

	headers := rec.Header().Values("Set-Cookie")
	if len(headers) < 2 {
		t.Fatalf("Set-Cookie count = %d, want >= 2 (scoped + legacy)", len(headers))
	}
	joined := strings.Join(headers, " | ")
	if !strings.Contains(joined, monitorAuthCookie+"=") {
		t.Fatalf("Set-Cookie missing %s: %s", monitorAuthCookie, joined)
	}
	if !strings.Contains(joined, "Path="+monitorCookiePath) && !strings.Contains(joined, "Path=/monitor-proxy/") {
		t.Fatalf("Set-Cookie missing scoped Path: %s", joined)
	}
	// net/http renders MaxAge < 0 as Max-Age=0.
	if !strings.Contains(joined, "Max-Age=0") {
		t.Fatalf("Set-Cookie missing Max-Age=0: %s", joined)
	}
}
