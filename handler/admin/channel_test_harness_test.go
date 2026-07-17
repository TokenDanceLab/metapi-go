package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func setupChannelTestHarness(t *testing.T) (*store.DB, *channelTestHandler, chi.Router) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{}
	h := &channelTestHandler{db: db.DB, cfg: cfg}
	r := chi.NewRouter()
	// Register using the same handler instance so tests can inject transport.
	r.Post("/api/admin/test-channel", h.testChannel)
	r.Post("/api/debug/channel-probe", h.testChannel)
	return db, h, r
}

func insertHarnessFixtures(t *testing.T, db *store.DB) (siteID, accountID, routeID, channelID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "HarnessSite", "https://upstream.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ = res.LastInsertId()

	apiTok := "sk-harness-api-token-xyz"
	res, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'active', 0, ?, ?)`, siteID, "harness-user", "session-token", apiTok, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES (?, ?, 'standard', 'weighted', 1, ?, ?)`, "gpt-*", "Harness Route", now, now)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled)
		VALUES (?, ?, ?, 10, 10, 1)`, routeID, accountID, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	channelID, _ = res.LastInsertId()
	return siteID, accountID, routeID, channelID
}

func TestChannelTest_Validation(t *testing.T) {
	_, _, r := setupChannelTestHarness(t)

	// Missing both ids
	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{"model": "gpt-4o"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	// Invalid mode
	resp = doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"channelId": 1,
		"mode":      "stream",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("mode expected 400, got %d: %s", resp.Code, resp.Body.String())
	}

	// Invalid JSON
	req := httptest.NewRequest("POST", "/api/admin/test-channel", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json expected 400, got %d", rec.Code)
	}
}

func TestChannelTest_ChannelNotFound(t *testing.T) {
	_, _, r := setupChannelTestHarness(t)
	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"channelId": 99999,
		"model":     "gpt-4o-mini",
	})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestChannelTest_ForcedChannelChatSuccess(t *testing.T) {
	db, h, r := setupChannelTestHarness(t)
	siteID, accountID, routeID, channelID := insertHarnessFixtures(t, db)

	var seenAuth string
	var seenPath string
	var seenBody string
	var hits atomic.Int32
	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits.Add(1)
		seenAuth = req.Header.Get("Authorization")
		seenPath = req.URL.Path
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			seenBody = string(b)
		}
		body := `{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"pong"}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"channelId": channelID,
		"model":     "gpt-4o-mini",
		"prompt":    "hello harness",
		"mode":      "chat",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("success=%v error=%v body=%v", out["success"], out["error"], out["truncatedBody"])
	}
	if int(out["statusCode"].(float64)) != 200 {
		t.Fatalf("statusCode=%v", out["statusCode"])
	}
	if out["channelId"].(float64) != float64(channelID) {
		t.Fatalf("channelId=%v want %d", out["channelId"], channelID)
	}
	if out["siteId"].(float64) != float64(siteID) {
		t.Fatalf("siteId=%v want %d", out["siteId"], siteID)
	}
	if out["accountId"].(float64) != float64(accountID) {
		t.Fatalf("accountId=%v", out["accountId"])
	}
	if out["routeId"].(float64) != float64(routeID) {
		t.Fatalf("routeId=%v", out["routeId"])
	}
	if out["upstreamPath"] != "/v1/chat/completions" {
		t.Fatalf("upstreamPath=%v", out["upstreamPath"])
	}
	if !strings.Contains(out["truncatedBody"].(string), "pong") {
		t.Fatalf("truncatedBody=%v", out["truncatedBody"])
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
	if seenAuth != "Bearer sk-harness-api-token-xyz" {
		t.Fatalf("auth=%q", seenAuth)
	}
	if !strings.Contains(seenPath, "/chat/completions") {
		t.Fatalf("path=%q", seenPath)
	}
	if !strings.Contains(seenBody, "hello harness") {
		t.Fatalf("request body missing prompt: %s", seenBody)
	}
}

func TestChannelTest_DebugAliasAndModelsMode(t *testing.T) {
	db, h, r := setupChannelTestHarness(t)
	_, _, _, channelID := insertHarnessFixtures(t, db)

	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("models mode method=%s", req.Method)
		}
		if !strings.HasSuffix(req.URL.Path, "/models") {
			t.Fatalf("path=%s", req.URL.Path)
		}
		body := `{"data":[{"id":"gpt-4o-mini"}]}`
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/debug/channel-probe", map[string]any{
		"channelId": channelID,
		"mode":      "models",
	})
	if resp.Code != 200 {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["success"] != true {
		t.Fatalf("out=%v", out)
	}
	if out["mode"] != "models" {
		t.Fatalf("mode=%v", out["mode"])
	}
	if out["upstreamPath"] != "/v1/models" {
		t.Fatalf("path=%v", out["upstreamPath"])
	}
}

func TestChannelTest_SiteIDResolution(t *testing.T) {
	db, h, r := setupChannelTestHarness(t)
	siteID, _, _, channelID := insertHarnessFixtures(t, db)

	var forcedChannelPath string
	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		forcedChannelPath = req.URL.String()
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"siteId": siteID,
		"model":  "gpt-4o-mini",
		"mode":   "chat",
	})
	if resp.Code != 200 {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["channelId"].(float64) != float64(channelID) {
		t.Fatalf("resolved channelId=%v want %d", out["channelId"], channelID)
	}
	if !strings.Contains(forcedChannelPath, "upstream.example.test") {
		t.Fatalf("url=%s", forcedChannelPath)
	}
}

func TestChannelTest_UpstreamErrorAndRedaction(t *testing.T) {
	db, h, r := setupChannelTestHarness(t)
	_, _, _, channelID := insertHarnessFixtures(t, db)

	// Oversized body with secret-like token near the start (still truncated later).
	big := "error unauthorized sk-abcdefghijklmnopqrstuvwxyz012345 " + strings.Repeat("A", 3000)
	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(strings.NewReader(big)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"channelId": channelID,
		"model":     "gpt-4o-mini",
	})
	if resp.Code != 200 {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["success"] != false {
		t.Fatalf("expected success=false, got %v", out)
	}
	if int(out["statusCode"].(float64)) != 401 {
		t.Fatalf("statusCode=%v", out["statusCode"])
	}
	body := out["truncatedBody"].(string)
	if strings.Contains(body, "sk-abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("secret leaked in truncatedBody: %s", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatalf("expected redaction marker: %s", body)
	}
	if out["bodyTruncated"] != true {
		t.Fatalf("expected bodyTruncated=true")
	}
	if len(body) > channelTestMaxBodyBytes+40 { // room for suffix/redaction
		t.Fatalf("body too large: %d", len(body))
	}
}

func TestChannelTest_TransportError(t *testing.T) {
	db, h, r := setupChannelTestHarness(t)
	_, _, _, channelID := insertHarnessFixtures(t, db)

	h.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, contextDeadlineErr{}
	})

	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"channelId": channelID,
		"timeoutMs": 1000,
	})
	if resp.Code != 200 {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["success"] != false {
		t.Fatalf("out=%v", out)
	}
	if out["error"] == nil || out["error"] == "" {
		t.Fatalf("expected error summary: %v", out)
	}
}

func TestChannelTest_RejectsCrossOriginRedirect(t *testing.T) {
	// SSRF residual (#416): bare harness client must refuse public-origin 302
	// to a different host and must not return the redirect target body.
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ssrf":"payload-from-169"}`))
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/latest/meta-data/", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	db, _, r := setupChannelTestHarness(t)
	// Leave transport nil so doRequest uses the real bare http.Client path.
	_, _, _, channelID := insertHarnessFixturesAtURL(t, db, source.URL)

	resp := doPostJSON(t, r, "/api/admin/test-channel", map[string]any{
		"channelId": channelID,
		"mode":      "models",
		"timeoutMs": 5000,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["success"] != false {
		t.Fatalf("expected success=false on cross-origin redirect, got %v", out)
	}
	errMsg, _ := out["error"].(string)
	if !strings.Contains(errMsg, "cross-origin") {
		t.Fatalf("error = %q, want cross-origin redirect rejection", errMsg)
	}
	body, _ := out["truncatedBody"].(string)
	if strings.Contains(body, "ssrf") || strings.Contains(body, "payload-from-169") {
		t.Fatalf("redirect target body leaked: %q", body)
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was called (SSRF)")
	}
}

func insertHarnessFixturesAtURL(t *testing.T, db *store.DB, siteURL string) (siteID, accountID, routeID, channelID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "HarnessRedirectSite", siteURL, "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ = res.LastInsertId()

	apiTok := "sk-harness-api-token-xyz"
	res, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, api_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'active', 0, ?, ?)`, siteID, "harness-user", "session-token", apiTok, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO token_routes (model_pattern, display_name, route_mode, routing_strategy, enabled, created_at, updated_at)
		VALUES (?, ?, 'standard', 'weighted', 1, ?, ?)`, "gpt-*", "Harness Redirect Route", now, now)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO route_channels (route_id, account_id, source_model, priority, weight, enabled)
		VALUES (?, ?, ?, 10, 10, 1)`, routeID, accountID, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	channelID, _ = res.LastInsertId()
	return siteID, accountID, routeID, channelID
}

type contextDeadlineErr struct{}

func (contextDeadlineErr) Error() string { return "context deadline exceeded" }
func (contextDeadlineErr) Timeout() bool { return true }

func TestTruncateAndRedact(t *testing.T) {
	s, trunc := truncateAndRedact([]byte(`ok Bearer sk-abc123456789 and more`), 100)
	if trunc {
		t.Fatalf("should not truncate short body")
	}
	if strings.Contains(s, "sk-abc") || strings.Contains(s, "Bearer sk-") {
		t.Fatalf("not redacted: %s", s)
	}
	if !strings.Contains(s, "[redacted]") {
		t.Fatalf("missing marker: %s", s)
	}

	big := bytes.Repeat([]byte("x"), 5000)
	s2, trunc2 := truncateAndRedact(big, channelTestMaxBodyBytes)
	if !trunc2 {
		t.Fatalf("expected truncate")
	}
	if !strings.Contains(s2, "[truncated]") {
		t.Fatalf("missing truncated marker: %s", s2)
	}
	if len(s2) > channelTestMaxBodyBytes+20 {
		t.Fatalf("len=%d", len(s2))
	}
}
