package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	proxypkg "github.com/tokendancelab/metapi-go/proxy"
	routerpkg "github.com/tokendancelab/metapi-go/router"
)

// TestProxyConcurrentRequests verifies 5 concurrent proxy requests all succeed.
func TestProxyConcurrentRequests(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONHelper(w, 200, map[string]any{
			"id": "req", "object": "chat.completion", "model": "gpt-4",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer mockUpstream.Close()

	mockR := newMockRouter()
	for i := int64(1); i <= 10; i++ {
		mockR.addChannel(makeChannel(i, mockUpstream.URL, "gpt-4"))
	}

	coord := proxypkg.NewProxyChannelCoordinator(makeTestConfig())
	r := setupE2ERouter(mockR, coord, injectAuthMiddleware("global", nil, "global-proxy-token"))

	var wg sync.WaitGroup
	errs := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/chat/completions",
				strings.NewReader(fmt.Sprintf(`{"model":"gpt-4","messages":[{"role":"user","content":"%d"}]}`, id)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != 200 {
				errs <- fmt.Errorf("req %d: got %d: %s", id, rec.Code, rec.Body.String())
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

// TestAuthTokenConstantTime validates token auth with subtle.ConstantTimeCompare.
func TestAuthTokenConstantTime(t *testing.T) {
	cfg := &config.Config{AuthToken: "real-secret-token", ProxyToken: "real-proxy-token"}
	config.Set(cfg)

	handler := auth.AdminAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// Valid token
	req := httptest.NewRequest("GET", "/api/sites", nil)
	req.Header.Set("Authorization", "Bearer real-secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("valid token rejected: %d", rec.Code)
	}

	// Same length, wrong case
	req2 := httptest.NewRequest("GET", "/api/sites", nil)
	req2.Header.Set("Authorization", "Bearer real-secret-tokeN")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != 403 {
		t.Errorf("wrong-case token not rejected: %d", rec2.Code)
	}

	// Different length
	req3 := httptest.NewRequest("GET", "/api/sites", nil)
	req3.Header.Set("Authorization", "Bearer short")
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != 403 {
		t.Errorf("short token not rejected: %d", rec3.Code)
	}
}

// TestRateLimitRejection validates rate limiting middleware blocks excess.
func TestRateLimitRejection(t *testing.T) {
	cfg := &config.Config{AuthToken: "test-token"}
	handler := auth.AdminAuth(cfg)(
		auth.AdminRateLimit(1, 1)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			}),
		),
	)

	req1 := httptest.NewRequest("GET", "/api/sites", nil)
	req1.Header.Set("Authorization", "Bearer test-token")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Fatalf("first request should pass: %d", rec1.Code)
	}

	req2 := httptest.NewRequest("GET", "/api/sites", nil)
	req2.Header.Set("Authorization", "Bearer test-token")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != 429 {
		t.Errorf("expected 429 rate limited, got %d", rec2.Code)
	}
}

func TestProxyRequestBodyLimitRejectsBeforeUpstream(t *testing.T) {
	var upstreamHits atomic.Int32
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSONHelper(w, 200, map[string]any{
			"id": "req", "object": "chat.completion", "model": "gpt-4",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer mockUpstream.Close()

	mockR := newMockRouter()
	mockR.staticChannel = makeChannel(1, mockUpstream.URL, "gpt-4")

	coord := proxypkg.NewProxyChannelCoordinator(makeTestConfig())
	proxyRoutes := setupE2ERouter(mockR, coord, injectAuthMiddleware("global", nil, "global-proxy-token"))

	r := chi.NewRouter()
	r.Use(routerpkg.BodyLimit(96))
	r.Mount("/v1", proxyRoutes)

	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "non-stream",
			body: `{"model":"gpt-4","messages":[{"role":"user","content":"` + strings.Repeat("x", 120) + `"}]}`,
		},
		{
			name: "stream",
			body: `{"model":"gpt-4","stream":true,"messages":[{"role":"user","content":"` + strings.Repeat("x", 120) + `"}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := upstreamHits.Load()
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
			}
			if got := upstreamHits.Load(); got != before {
				t.Fatalf("upstream hits = %d, want unchanged at %d", got, before)
			}
		})
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","messages":[{"role":"user","content":"ok"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("small request status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := upstreamHits.Load(); got != 1 {
		t.Fatalf("upstream hits after small request = %d, want 1", got)
	}
}
