package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/platform"
)

type systemProxyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f systemProxyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withSystemProxyTestTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	prev := systemProxyTestTransport
	systemProxyTestTransport = rt
	t.Cleanup(func() { systemProxyTestTransport = prev })
}

func TestBrandListFromPlatformRegistry(t *testing.T) {
	platform.InitRegistry()
	_, r, _ := setupEdgeTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/brand-list", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}

	var body struct {
		Brands []string `json:"brands"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := platform.ListRegisteredPlatformNames()
	if len(body.Brands) != len(want) {
		t.Fatalf("brands len = %d, want %d (%v)", len(body.Brands), len(want), body.Brands)
	}
	for i := range want {
		if body.Brands[i] != want[i] {
			t.Fatalf("brands[%d] = %q, want %q (full=%v)", i, body.Brands[i], want[i], body.Brands)
		}
	}

	// Must not be the old hardcoded 5-name stub.
	if len(body.Brands) <= 5 {
		t.Fatalf("brand list still looks stub-sized: %v", body.Brands)
	}
	// Registry platforms must include known adapters.
	joined := strings.Join(body.Brands, ",")
	for _, name := range []string{"openai", "new-api", "one-api", "veloera", "claude"} {
		if !strings.Contains(joined, name) {
			t.Fatalf("brand list missing %q: %v", name, body.Brands)
		}
	}
}

func TestSystemProxyTest_SuccessWithFakeTransport(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	var sawURL string
	var sawMethod string
	var sawUA string
	withSystemProxyTestTransport(t, systemProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawURL = req.URL.String()
		sawMethod = req.Method
		sawUA = req.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}))

	resp := doPostJSON(t, r, "/api/settings/system-proxy/test", map[string]any{
		"proxyUrl": "http://127.0.0.1:7890",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success = %v, want true", body["success"])
	}
	if body["proxyUrl"] != "http://127.0.0.1:7890" {
		t.Fatalf("proxyUrl = %v", body["proxyUrl"])
	}
	if body["probeUrl"] != systemProxyTestProbeURL {
		t.Fatalf("probeUrl = %v, want %s", body["probeUrl"], systemProxyTestProbeURL)
	}
	if body["finalUrl"] != systemProxyTestProbeURL {
		t.Fatalf("finalUrl = %v, want %s", body["finalUrl"], systemProxyTestProbeURL)
	}
	if body["reachable"] != true {
		t.Fatalf("reachable = %v, want true", body["reachable"])
	}
	if body["ok"] != true {
		t.Fatalf("ok = %v, want true", body["ok"])
	}
	if body["statusCode"] != float64(204) {
		t.Fatalf("statusCode = %v, want 204", body["statusCode"])
	}
	latency, ok := body["latencyMs"].(float64)
	if !ok || latency < 1 {
		t.Fatalf("latencyMs = %v, want >= 1", body["latencyMs"])
	}
	if sawURL != systemProxyTestProbeURL {
		t.Fatalf("probe target = %q, want %s", sawURL, systemProxyTestProbeURL)
	}
	if sawMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", sawMethod)
	}
	if sawUA != "metapi-system-proxy-tester/1.0" {
		t.Fatalf("user-agent = %q", sawUA)
	}
}

func TestSystemProxyTest_UsesSavedConfigWhenBodyEmpty(t *testing.T) {
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = "socks5://127.0.0.1:1080"

	withSystemProxyTestTransport(t, systemProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}))

	resp := doPostJSON(t, r, "/api/settings/system-proxy/test", map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["proxyUrl"] != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxyUrl = %v, want saved socks5 url", body["proxyUrl"])
	}
}

func TestSystemProxyTest_RejectsMissingProxyURL(t *testing.T) {
	_, r, cfg := setupEdgeTest(t)
	cfg.SystemProxyUrl = ""

	called := false
	withSystemProxyTestTransport(t, systemProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("should not be called")
	}))

	resp := doPostJSON(t, r, "/api/settings/system-proxy/test", map[string]any{})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("probe transport was called despite missing proxy URL")
	}
	if !strings.Contains(resp.Body.String(), "请先填写系统代理地址") {
		t.Fatalf("body = %s, want missing-proxy message", resp.Body.String())
	}
}

func TestSystemProxyTest_RejectsInvalidProxyURL(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	called := false
	withSystemProxyTestTransport(t, systemProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("should not be called")
	}))

	resp := doPostJSON(t, r, "/api/settings/system-proxy/test", map[string]any{
		"proxyUrl": "ftp://proxy.example:21",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("probe transport was called despite invalid proxy URL")
	}
	if !strings.Contains(resp.Body.String(), "系统代理地址无效") {
		t.Fatalf("body = %s, want invalid-proxy message", resp.Body.String())
	}
}

func TestSystemProxyTest_Returns502OnProbeFailure(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	withSystemProxyTestTransport(t, systemProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("connect ECONNREFUSED 127.0.0.1:7890")
	}))

	resp := doPostJSON(t, r, "/api/settings/system-proxy/test", map[string]any{
		"proxyUrl": "http://127.0.0.1:7890",
	})
	if resp.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s, want 502", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, "连接被拒绝") {
		t.Fatalf("message = %q, want connection-refused wording", msg)
	}
}

func TestSystemProxyTest_Non2xxStillReachable(t *testing.T) {
	_, r, _ := setupEdgeTest(t)

	withSystemProxyTestTransport(t, systemProxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("blocked")),
			Request:    req,
		}, nil
	}))

	resp := doPostJSON(t, r, "/api/settings/system-proxy/test", map[string]any{
		"proxyUrl": "http://127.0.0.1:7890",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["reachable"] != true {
		t.Fatalf("reachable = %v, want true (got a response)", body["reachable"])
	}
	if body["ok"] != false {
		t.Fatalf("ok = %v, want false for 403", body["ok"])
	}
	if body["statusCode"] != float64(403) {
		t.Fatalf("statusCode = %v, want 403", body["statusCode"])
	}
}

func TestNormalizeSystemProxyURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"http://127.0.0.1:7890", "http://127.0.0.1:7890"},
		{"http://127.0.0.1:7890/", "http://127.0.0.1:7890"},
		{"socks5://user:pass@127.0.0.1:1080", "socks5://user:pass@127.0.0.1:1080"},
		{"ftp://bad", ""},
		{"not-a-url", ""},
		{"://invalid", ""},
	}
	for _, tt := range tests {
		if got := normalizeSystemProxyURL(tt.in); got != tt.want {
			t.Fatalf("normalizeSystemProxyURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDescribeSystemProxyTestFailure(t *testing.T) {
	msg := describeSystemProxyTestFailure(errors.New("connect ECONNREFUSED 127.0.0.1:1"))
	if !strings.Contains(msg, "连接被拒绝") {
		t.Fatalf("refused msg = %q", msg)
	}
	msg = describeSystemProxyTestFailure(errors.New("i/o timeout"))
	if !strings.Contains(msg, "连接超时") {
		t.Fatalf("timeout msg = %q", msg)
	}
	// Nested cause preferred over outer generic.
	outer := errors.New("fetch failed")
	_ = outer
	nested := &wrappedError{msg: "fetch failed", cause: errors.New("proxy authentication required")}
	msg = describeSystemProxyTestFailure(nested)
	if !strings.Contains(msg, "代理要求认证") {
		t.Fatalf("auth msg = %q", msg)
	}
}

type wrappedError struct {
	msg   string
	cause error
}

func (e *wrappedError) Error() string { return e.msg }
func (e *wrappedError) Unwrap() error { return e.cause }

func TestSystemProxyTest_TimeoutBounded(t *testing.T) {
	// Guard against accidental timeout regression: constant must stay bounded.
	if systemProxyTestTimeout <= 0 || systemProxyTestTimeout > 30*time.Second {
		t.Fatalf("systemProxyTestTimeout = %s, want (0, 30s]", systemProxyTestTimeout)
	}
	if systemProxyTestProbeURL == "" {
		t.Fatal("probe URL empty")
	}
}
