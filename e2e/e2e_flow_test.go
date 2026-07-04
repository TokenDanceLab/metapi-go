// Package e2e contains end-to-end integration tests for the MetAPI Go proxy flow.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/handler/admin"
	proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/store"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test: Site-Create-to-Proxy Integration Flow
// ──────────────────────────────────────────────────────────────────────────────
// Full pipeline: create site -> create account -> create account_token ->
// query /v1/models -> POST /v1/chat/completions -> verify response.
// Uses SQLite :memory: for the DB and httptest.Server for the mock upstream.

func TestSiteCreateToProxyFlow(t *testing.T) {
	// ══════════════════════════════════════════════════════════════════════════
	// Phase 1: Infrastructure setup
	// ══════════════════════════════════════════════════════════════════════════

	adminToken := "admin-integration-test-token"
	proxyToken := "proxy-integration-test-sk-token"

	// 1a. Build config first - EnsureRuntimeDatabase reads DbType and DbUrl.
	cfg := &config.Config{
		AuthToken:                    adminToken,
		ProxyToken:                   proxyToken,
		AccountCredentialSecret:      "test-cred-secret",
		DbType:                       "sqlite",
		DbUrl:                        ":memory:",
		DataDir:                      t.TempDir(),
		ProxyMaxChannelAttempts:      3,
		ProxyStickySessionEnabled:    false,
		ProxyStickySessionTtlMs:      30000,
		TokenRouterCacheTtlMs:        1500,
		TokenRouterFailureCooldownMaxSec: 60,
		RoutingFallbackUnitCost:      1,
		RequestBodyLimit:             20 * 1024 * 1024,
	}
	config.Set(cfg)

	// 1b. Open in-memory SQLite + AutoMigrate + set singleton via EnsureRuntimeDatabase.
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase failed: %v", err)
	}
	defer store.CloseDatabase()

	db := store.GetDB()
	if db == nil {
		t.Fatal("store.GetDB() returned nil after EnsureRuntimeDatabase")
	}

	// 1c. Mock upstream HTTP server that returns a valid chat completion.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("upstream: expected POST, got %s", r.Method)
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(bodyBytes, &reqBody)

		if authH := r.Header.Get("Authorization"); authH == "" {
			t.Error("upstream: Authorization header missing")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("upstream: expected Content-Type application/json, got %q", ct)
		}

		modelName, _ := reqBody["model"].(string)
		writeJSONHelper(w, 200, map[string]any{
			"id":      "chatcmpl-integration-test-001",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   modelName,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Integration test response from mock upstream",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 15,
				"total_tokens":      25,
			},
		})
	}))
	defer mockUpstream.Close()

	// 1d. Mock router that selects channels pointing to the mock upstream.
	mockR := newMockRouter()
	mockR.addChannel(makeChannel(1, mockUpstream.URL, "gpt-4o"))

	// 1e. Proxy channel coordinator.
	coord := proxy.NewProxyChannelCoordinator(cfg)

	// 1f. Wire upstream config for proxy dispatch.
	proxyhandler.SetUpstreamConfig(&proxyhandler.UpstreamConfig{
		Router:         mockR,
		RouteRefresher: &mockRouteRefresher{},
		Coordinator:    coord,
	})

	// 1g. Build chi router with admin and proxy route groups.
	r := chi.NewRouter()

	// Admin routes: /api/* with AdminAuth middleware.
	// Using r.Group (not r.Route) because Register*Routes use full paths
	// like /api/sites, /api/accounts, etc.
	r.Group(func(r chi.Router) {
		r.Use(auth.AdminAuth(cfg))
		admin.RegisterSitesRoutes(r, db.DB)
		admin.RegisterAccountsRoutes(r, db.DB, cfg)
		admin.RegisterAccountTokensRoutes(r, db.DB)
	})

	// Proxy routes: /v1/* with ProxyAuth middleware.
	// Using r.Route("/v1") which strips the prefix, so the internal
	// /chat/completions and /models become /v1/chat/completions and /v1/models.
	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.ProxyAuth(cfg))
		proxyhandler.RegisterProxyRoutes(r)
	})

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 2: Create site via admin API
	// ══════════════════════════════════════════════════════════════════════════

	siteURL := mockUpstream.URL
	siteName := "integration-test-site"

	createSiteBody := map[string]any{
		"name":     siteName,
		"url":      siteURL,
		"platform": "openai",
		"status":   "active",
	}
	siteRec := doAdminPost(t, r, "/api/sites", adminToken, createSiteBody)

	if siteRec.Code != 200 && siteRec.Code != 201 {
		t.Fatalf("create site: expected 200/201, got %d: %s", siteRec.Code, siteRec.Body.String())
	}

	var siteResp map[string]any
	if err := json.Unmarshal(siteRec.Body.Bytes(), &siteResp); err != nil {
		t.Fatalf("create site: failed to parse response: %v", err)
	}

	siteIDFloat, ok := siteResp["id"].(float64)
	if !ok {
		t.Fatalf("create site: expected numeric id in response, got %v", siteResp)
	}
	siteID := int64(siteIDFloat)
	t.Logf("created site: id=%d, name=%s", siteID, siteName)

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 3: Create account for the site via admin API
	// ══════════════════════════════════════════════════════════════════════════

	upstreamToken := "sk-test-upstream-token-abc123"

	createAccountBody := map[string]any{
		"siteId":      siteID,
		"accessToken": upstreamToken,
	}
	accountRec := doAdminPost(t, r, "/api/accounts", adminToken, createAccountBody)

	if accountRec.Code != 200 && accountRec.Code != 201 {
		t.Fatalf("create account: expected 200/201, got %d: %s", accountRec.Code, accountRec.Body.String())
	}

	var accountResp map[string]any
	if err := json.Unmarshal(accountRec.Body.Bytes(), &accountResp); err != nil {
		t.Fatalf("create account: failed to parse response: %v", err)
	}

	accountIDFloat, ok := accountResp["id"].(float64)
	if !ok {
		t.Fatalf("create account: expected numeric id in response, got %v", accountResp)
	}
	accountID := int64(accountIDFloat)
	t.Logf("created account: id=%d for site %d", accountID, siteID)

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 4: Create account_token for the account via admin API
	// ══════════════════════════════════════════════════════════════════════════

	tokenName := "default-token"
	createTokenBody := map[string]any{
		"accountId": int(accountID),
		"name":      tokenName,
		"token":     upstreamToken,
	}
	tokenRec := doAdminPost(t, r, "/api/account-tokens", adminToken, createTokenBody)

	if tokenRec.Code != 200 && tokenRec.Code != 201 {
		t.Fatalf("create account_token: expected 200/201, got %d: %s", tokenRec.Code, tokenRec.Body.String())
	}

	var tokenResp map[string]any
	if err := json.Unmarshal(tokenRec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatalf("create account_token: failed to parse response: %v", err)
	}

	// The account_token response may nest the token under a "token" key.
	// Handle both flat and nested response shapes.
	tokenData, ok := tokenResp["token"].(map[string]any)
	if !ok {
		// Flat response: id is at top level.
		tokenData = tokenResp
	}
	tokenID := mapGetInt64(tokenData, "id")
	if tokenID == 0 {
		t.Fatalf("create account_token: expected numeric id in response, got %v", tokenResp)
	}
	t.Logf("created account_token: id=%d for account %d", tokenID, accountID)

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 5: Query /v1/models via proxy
	// ══════════════════════════════════════════════════════════════════════════

	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Bearer "+proxyToken)
	modelsRec := httptest.NewRecorder()
	r.ServeHTTP(modelsRec, modelsReq)

	if modelsRec.Code != 200 {
		t.Fatalf("GET /v1/models: expected 200, got %d: %s", modelsRec.Code, modelsRec.Body.String())
	}

	var modelsResp map[string]any
	if err := json.Unmarshal(modelsRec.Body.Bytes(), &modelsResp); err != nil {
		t.Fatalf("GET /v1/models: failed to parse response: %v", err)
	}

	// Verify OpenAI models format.
	if obj, _ := modelsResp["object"].(string); obj != "list" {
		t.Errorf("GET /v1/models: expected object='list', got %q", obj)
	}
	data, ok := modelsResp["data"].([]any)
	if !ok || len(data) == 0 {
		t.Errorf("GET /v1/models: expected non-empty data array, got %v", modelsResp["data"])
	} else {
		for i, item := range data {
			modelEntry, ok := item.(map[string]any)
			if !ok {
				t.Errorf("GET /v1/models: data[%d] is not an object", i)
				continue
			}
			if _, hasID := modelEntry["id"]; !hasID {
				t.Errorf("GET /v1/models: data[%d] missing 'id' field", i)
			}
			if obj, _ := modelEntry["object"].(string); obj != "model" {
				t.Errorf("GET /v1/models: data[%d] expected object='model', got %q", i, obj)
			}
		}
		t.Logf("GET /v1/models: returned %d models", len(data))
	}

	// ══════════════════════════════════════════════════════════════════════════
	// Phase 6: POST /v1/chat/completions via proxy
	// ══════════════════════════════════════════════════════════════════════════

	chatBody := map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": "Hello from integration test"},
		},
		"max_tokens": 50,
	}
	chatBodyBytes, _ := json.Marshal(chatBody)

	chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatBodyBytes)))
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("Authorization", "Bearer "+proxyToken)
	chatRec := httptest.NewRecorder()
	r.ServeHTTP(chatRec, chatReq)

	if chatRec.Code != 200 {
		t.Fatalf("POST /v1/chat/completions: expected 200, got %d: %s", chatRec.Code, chatRec.Body.String())
	}

	var chatResp map[string]any
	if err := json.Unmarshal(chatRec.Body.Bytes(), &chatResp); err != nil {
		t.Fatalf("POST /v1/chat/completions: failed to parse response: %v", err)
	}

	// Verify the response is a valid chat completion.
	if obj, _ := chatResp["object"].(string); obj != "chat.completion" {
		t.Errorf("POST /v1/chat/completions: expected object='chat.completion', got %q", obj)
	}

	if id, _ := chatResp["id"].(string); id == "" {
		t.Error("POST /v1/chat/completions: expected non-empty 'id' field")
	}

	choices, ok := chatResp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Errorf("POST /v1/chat/completions: expected non-empty choices array, got %v", chatResp["choices"])
	} else {
		choice0, ok := choices[0].(map[string]any)
		if !ok {
			t.Errorf("POST /v1/chat/completions: choices[0] is not an object")
		} else {
			msg, ok := choice0["message"].(map[string]any)
			if !ok {
				t.Errorf("POST /v1/chat/completions: choices[0].message is not an object")
			} else {
				content, _ := msg["content"].(string)
				if content == "" {
					t.Error("POST /v1/chat/completions: expected non-empty content in response")
				}
				t.Logf("chat completion response content: %q", content)
			}
			if finishReason, _ := choice0["finish_reason"].(string); finishReason != "stop" {
				t.Errorf("POST /v1/chat/completions: expected finish_reason='stop', got %q", finishReason)
			}
		}
	}

	// Verify usage was included.
	usage, ok := chatResp["usage"].(map[string]any)
	if !ok {
		t.Error("POST /v1/chat/completions: expected 'usage' field in response")
	} else {
		if tt, _ := usage["total_tokens"].(float64); tt == 0 {
			t.Error("POST /v1/chat/completions: expected non-zero total_tokens in usage")
		}
	}

	// Verify the mock router selected a channel.
	if len(mockR.selectedIDs) == 0 {
		t.Error("POST /v1/chat/completions: expected at least one channel to be selected")
	} else {
		t.Logf("channels selected: %v", mockR.selectedIDs)
	}

	t.Log("integration flow completed successfully: site -> account -> token -> models -> chat")
}

// ──────────────────────────────────────────────────────────────────────────────
// Negative Cases: Unauthorized Access
// ──────────────────────────────────────────────────────────────────────────────

func TestSiteCreateToProxyFlow_UnauthorizedAccess(t *testing.T) {
	adminToken := "admin-test-token-ua"
	proxyToken := "proxy-test-token-ua"

	cfg := &config.Config{
		AuthToken:               adminToken,
		ProxyToken:              proxyToken,
		AccountCredentialSecret: "test-cred-secret",
		DbType:                  "sqlite",
		DbUrl:                   ":memory:",
		DataDir:                 t.TempDir(),
		ProxyMaxChannelAttempts: 3,
		RequestBodyLimit:        20 * 1024 * 1024,
	}
	config.Set(cfg)

	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase failed: %v", err)
	}
	defer store.CloseDatabase()

	db := store.GetDB()
	if db == nil {
		t.Fatal("store.GetDB() returned nil")
	}

	coord := proxy.NewProxyChannelCoordinator(cfg)
	proxyhandler.SetUpstreamConfig(&proxyhandler.UpstreamConfig{
		Router:         newMockRouter(),
		RouteRefresher: &mockRouteRefresher{},
		Coordinator:    coord,
	})

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.AdminAuth(cfg))
		admin.RegisterSitesRoutes(r, db.DB)
	})
	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.ProxyAuth(cfg))
		proxyhandler.RegisterProxyRoutes(r)
	})

	t.Run("admin endpoint without auth", func(t *testing.T) {
		rec := doAdminPost(t, r, "/api/sites", "", map[string]any{
			"name": "test",
			"url":  "https://example.com",
		})
		if rec.Code != 401 && rec.Code != 403 {
			t.Errorf("expected 401 or 403 without auth token, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin endpoint with wrong token", func(t *testing.T) {
		rec := doAdminPost(t, r, "/api/sites", "wrong-token", map[string]any{
			"name": "test",
			"url":  "https://example.com",
		})
		if rec.Code != 403 {
			t.Errorf("expected 403 with wrong token, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("proxy endpoint without auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Errorf("expected 401 without proxy auth, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("proxy endpoint with wrong token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer wrong-proxy-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("expected 403 with wrong proxy token, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("proxy endpoint with admin token (not proxy token)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("expected 403 with admin token on proxy endpoint, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Streaming variant: site-create-to-streaming-proxy
// ──────────────────────────────────────────────────────────────────────────────

func TestSiteCreateToProxyFlow_Streaming(t *testing.T) {
	adminToken := "admin-streaming-test-token"
	proxyToken := "proxy-streaming-test-token"

	cfg := &config.Config{
		AuthToken:               adminToken,
		ProxyToken:              proxyToken,
		AccountCredentialSecret: "test-cred-secret",
		DbType:                  "sqlite",
		DbUrl:                   ":memory:",
		DataDir:                 t.TempDir(),
		ProxyMaxChannelAttempts: 3,
		RequestBodyLimit:        20 * 1024 * 1024,
	}
	config.Set(cfg)

	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase failed: %v", err)
	}
	defer store.CloseDatabase()

	db := store.GetDB()
	if db == nil {
		t.Fatal("store.GetDB() returned nil")
	}

	// Mock upstream that returns SSE streaming response.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream: expected flusher")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(200)

		events := []string{
			`{"id":"chatcmpl-stream-001","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Streaming"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-002","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":" response"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream-003","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, evt := range events {
			w.Write([]byte("data: " + evt + "\n\n"))
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer mockUpstream.Close()

	mockR := newMockRouter()
	mockR.addChannel(makeChannel(1, mockUpstream.URL, "gpt-4o"))

	coord := proxy.NewProxyChannelCoordinator(cfg)
	proxyhandler.SetUpstreamConfig(&proxyhandler.UpstreamConfig{
		Router:         mockR,
		RouteRefresher: &mockRouteRefresher{},
		Coordinator:    coord,
	})

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.AdminAuth(cfg))
		admin.RegisterSitesRoutes(r, db.DB)
		admin.RegisterAccountsRoutes(r, db.DB, cfg)
		admin.RegisterAccountTokensRoutes(r, db.DB)
	})
	r.Route("/v1", func(r chi.Router) {
		r.Use(auth.ProxyAuth(cfg))
		proxyhandler.RegisterProxyRoutes(r)
	})

	// Create site.
	siteRec := doAdminPost(t, r, "/api/sites", adminToken, map[string]any{
		"name": "streaming-site", "url": mockUpstream.URL, "platform": "openai", "status": "active",
	})
	if siteRec.Code < 200 || siteRec.Code > 299 {
		t.Fatalf("create site failed: %d: %s", siteRec.Code, siteRec.Body.String())
	}
	var siteResp map[string]any
	json.Unmarshal(siteRec.Body.Bytes(), &siteResp)
	siteID := int64(siteResp["id"].(float64))

	// Create account.
	accountRec := doAdminPost(t, r, "/api/accounts", adminToken, map[string]any{
		"siteId": siteID, "accessToken": "sk-stream-token",
	})
	if accountRec.Code < 200 || accountRec.Code > 299 {
		t.Fatalf("create account failed: %d: %s", accountRec.Code, accountRec.Body.String())
	}
	var accountResp map[string]any
	json.Unmarshal(accountRec.Body.Bytes(), &accountResp)
	accountID := int64(accountResp["id"].(float64))

	// Create account_token.
	doAdminPost(t, r, "/api/account-tokens", adminToken, map[string]any{
		"accountId": int(accountID), "name": "default", "token": "sk-stream-token",
	})

	// Streaming chat request.
	streamBody := map[string]any{
		"model":    "gpt-4o",
		"messages": []map[string]any{{"role": "user", "content": "stream test"}},
		"stream":   true,
	}
	streamBytes, _ := json.Marshal(streamBody)

	streamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(streamBytes)))
	streamReq.Header.Set("Content-Type", "application/json")
	streamReq.Header.Set("Authorization", "Bearer "+proxyToken)
	streamRec := httptest.NewRecorder()
	r.ServeHTTP(streamRec, streamReq)

	if streamRec.Code != 200 {
		t.Fatalf("streaming chat: expected 200, got %d: %s", streamRec.Code, streamRec.Body.String())
	}

	// Verify SSE headers.
	ct := streamRec.Header().Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		t.Errorf("streaming chat: expected text/event-stream Content-Type, got %q", ct)
	}

	// Verify SSE body format.
	body := streamRec.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Error("streaming chat: expected 'data: ' prefix in SSE body")
	}
	if !strings.Contains(body, "[DONE]") {
		t.Error("streaming chat: expected [DONE] marker in SSE body")
	}
	if !strings.Contains(body, "Streaming") {
		t.Error("streaming chat: expected 'Streaming' content in SSE body")
	}

	t.Log("streaming integration flow completed successfully")
}

// ──────────────────────────────────────────────────────────────────────────────
// Test Helpers
// ──────────────────────────────────────────────────────────────────────────────

// doAdminPost sends a POST request to the given path with admin auth.
func doAdminPost(t *testing.T, r chi.Router, path string, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// mapGetInt64 extracts an int64 value from a decoded JSON map, handling
// all numeric types that encoding/json may produce (float64, json.Number).
func mapGetInt64(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
