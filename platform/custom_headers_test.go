package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsDeniedCustomHeader(t *testing.T) {
	t.Parallel()
	denied := []string{
		"Authorization",
		"authorization",
		" AUTHORIZATION ",
		"Host",
		"Content-Length",
		"Transfer-Encoding",
		"Connection",
		"Cookie",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Keep-Alive",
		"TE",
		"Upgrade",
		"X-Metapi-Responses-Only",
		"x-metapi-stream-only",
	}
	for _, h := range denied {
		if !IsDeniedCustomHeader(h) {
			t.Fatalf("IsDeniedCustomHeader(%q) = false, want true", h)
		}
	}
	allowed := []string{"X-Custom", "X-Metapi-Trace", "User-Agent", "Accept-Language"}
	for _, h := range allowed {
		if IsDeniedCustomHeader(h) {
			t.Fatalf("IsDeniedCustomHeader(%q) = true, want false", h)
		}
	}
}

func TestApplyCustomHeaders_DeniesSensitiveAllowsCustom(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/v1", nil)
	req.Header.Set("Authorization", "Bearer original-token")
	req.Host = "example.test"

	ApplyCustomHeaders(req, map[string]string{
		"Authorization":         "Bearer attacker",
		"Host":                  "evil.example",
		"Content-Length":        "999",
		"Transfer-Encoding":     "chunked",
		"Connection":            "close",
		"Cookie":                "session=stolen",
		"Proxy-Authorization":   "Basic evil",
		"X-Metapi-Responses-Only": "1",
		"X-Custom":              "ok",
	})

	if got := req.Header.Get("Authorization"); got != "Bearer original-token" {
		t.Fatalf("Authorization = %q, want original Bearer", got)
	}
	if got := req.Host; got != "example.test" {
		t.Fatalf("Host = %q, want example.test", got)
	}
	if req.Header.Get("Host") != "" {
		t.Fatalf("Host header set to %q, want empty (not applied)", req.Header.Get("Host"))
	}
	if req.Header.Get("Content-Length") != "" {
		t.Fatalf("Content-Length leaked: %q", req.Header.Get("Content-Length"))
	}
	if req.Header.Get("Transfer-Encoding") != "" {
		t.Fatalf("Transfer-Encoding leaked: %q", req.Header.Get("Transfer-Encoding"))
	}
	if req.Header.Get("Connection") != "" {
		t.Fatalf("Connection leaked: %q", req.Header.Get("Connection"))
	}
	if req.Header.Get("Cookie") != "" {
		t.Fatalf("Cookie leaked: %q", req.Header.Get("Cookie"))
	}
	if req.Header.Get("Proxy-Authorization") != "" {
		t.Fatalf("Proxy-Authorization leaked: %q", req.Header.Get("Proxy-Authorization"))
	}
	if req.Header.Get("X-Metapi-Responses-Only") != "" {
		t.Fatalf("control header leaked: %q", req.Header.Get("X-Metapi-Responses-Only"))
	}
	if got := req.Header.Get("X-Custom"); got != "ok" {
		t.Fatalf("X-Custom = %q, want ok", got)
	}
}

func TestDoWithProxy_CustomHeadersDenySensitive(t *testing.T) {
	var gotAuth, gotHost, gotCustom, gotCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHost = r.Host
		gotCustom = r.Header.Get("X-Custom")
		gotCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer good-token")
	resp, err := DoWithProxy(req.Context(), req, &ProxyConfig{
		CustomHeaders: map[string]string{
			"Authorization": "Bearer evil",
			"Host":          "evil.example",
			"Cookie":        "x=1",
			"X-Custom":      "still-set",
		},
	})
	if err != nil {
		t.Fatalf("DoWithProxy: %v", err)
	}
	_ = resp.Body.Close()

	if gotAuth != "Bearer good-token" {
		t.Fatalf("Authorization on wire = %q, want Bearer good-token", gotAuth)
	}
	if gotCustom != "still-set" {
		t.Fatalf("X-Custom = %q, want still-set", gotCustom)
	}
	if gotCookie != "" {
		t.Fatalf("Cookie on wire = %q, want empty", gotCookie)
	}
	// Host is set by net/http from the request URL; custom Host must not replace it with evil.
	if gotHost == "evil.example" {
		t.Fatalf("Host was overridden to evil.example")
	}
}

func TestApplyCustomHeaders_RequestWinsDefault(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/v1", nil)
	req.Header.Set("User-Agent", "client-sdk/1.0")
	req.Header.Set("X-Only-Client", "client")

	// Default ApplyCustomHeaders = request-wins (#584).
	ApplyCustomHeaders(req, map[string]string{
		"User-Agent":     "site-forced-ua",
		"X-Site-Only":    "site",
		"X-Only-Client":  "site-should-not-win",
	})

	if got := req.Header.Get("User-Agent"); got != "client-sdk/1.0" {
		t.Fatalf("User-Agent = %q, want client (request-wins)", got)
	}
	if got := req.Header.Get("X-Only-Client"); got != "client" {
		t.Fatalf("X-Only-Client = %q, want client", got)
	}
	if got := req.Header.Get("X-Site-Only"); got != "site" {
		t.Fatalf("X-Site-Only = %q, want site fill-in", got)
	}
}

func TestApplyCustomHeaders_SiteWinsWhenOverride(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/v1", nil)
	req.Header.Set("User-Agent", "client-sdk/1.0")
	req.Header.Set("Originator", "client-originator")

	ApplyCustomHeadersWithOptions(req, map[string]string{
		"User-Agent":  "site-forced-ua",
		"Originator":  "site-originator",
		"X-Site-Only": "site",
	}, ApplyCustomHeadersOptions{OverrideRequest: true})

	if got := req.Header.Get("User-Agent"); got != "site-forced-ua" {
		t.Fatalf("User-Agent = %q, want site-forced-ua", got)
	}
	if got := req.Header.Get("Originator"); got != "site-originator" {
		t.Fatalf("Originator = %q, want site-originator", got)
	}
	if got := req.Header.Get("X-Site-Only"); got != "site" {
		t.Fatalf("X-Site-Only = %q, want site", got)
	}
}

func TestApplyCustomHeaders_SiteWinsStillDeniesSensitive(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/v1", nil)
	req.Header.Set("Authorization", "Bearer original")

	ApplyCustomHeadersWithOptions(req, map[string]string{
		"Authorization": "Bearer attacker",
		"X-Custom":      "ok",
	}, ApplyCustomHeadersOptions{OverrideRequest: true})

	if got := req.Header.Get("Authorization"); got != "Bearer original" {
		t.Fatalf("Authorization = %q, want original", got)
	}
	if got := req.Header.Get("X-Custom"); got != "ok" {
		t.Fatalf("X-Custom = %q, want ok", got)
	}
}
