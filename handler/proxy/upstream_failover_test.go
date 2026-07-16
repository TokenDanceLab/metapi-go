package proxyhandler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/proxy"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func TestResolveUpstreamCandidatePaths(t *testing.T) {
	paths := resolveUpstreamCandidatePaths("/v1/chat/completions", false, proxy.SiteProtocolPreference{})
	if len(paths) < 2 {
		t.Fatalf("expected multi-protocol candidates, got %v", paths)
	}
	if paths[0] != "/v1/chat/completions" {
		t.Fatalf("primary path = %q", paths[0])
	}
	disabled := resolveUpstreamCandidatePaths("/v1/chat/completions", true, proxy.SiteProtocolPreference{})
	if len(disabled) != 1 || disabled[0] != "/v1/chat/completions" {
		t.Fatalf("disabled fallback paths = %v", disabled)
	}
	nonChat := resolveUpstreamCandidatePaths("/v1/embeddings", false, proxy.SiteProtocolPreference{})
	if len(nonChat) != 1 || nonChat[0] != "/v1/embeddings" {
		t.Fatalf("non-chat paths = %v", nonChat)
	}
	// Responses-only site: chat client rewrites candidate list to responses only.
	only := resolveUpstreamCandidatePaths("/v1/chat/completions", false, proxy.SiteProtocolPreference{
		ResponsesOnly:   true,
		PreferResponses: true,
		PreferStream:    true,
	})
	if len(only) != 1 || only[0] != "/v1/responses" {
		t.Fatalf("responses-only paths = %v, want [/v1/responses]", only)
	}
	// Prefer-responses (not only): responses first with fallbacks.
	prefer := resolveUpstreamCandidatePaths("/v1/chat/completions", false, proxy.SiteProtocolPreference{
		PreferResponses: true,
	})
	if len(prefer) < 2 || prefer[0] != "/v1/responses" {
		t.Fatalf("prefer-responses paths = %v, want responses first", prefer)
	}
}

func TestDispatchCrossProtocolFallbackOnProtocolHintWithoutPoison(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/chat/completions" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"please use /v1/messages"}}`))
			return
		}
		if r.URL.Path == "/v1/messages" {
			_, _ = w.Write([]byte(`{"id":"msg_ok","content":[{"type":"text","text":"ok"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	t.Cleanup(upstream.Close)

	prev := config.Get()
	cfgCopy := *prev
	cfgCopy.DisableCrossProtocolFallback = false
	cfgCopy.ProxyFirstByteTimeoutSec = 0
	config.Set(&cfgCopy)
	t.Cleanup(func() { config.Set(prev) })

	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
		TokenValue:  "upstream-token",
		ActualModel: "claude-3",
	}}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s paths=%v", rec.Code, rec.Body.String(), paths)
	}
	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	mu.Unlock()
	if len(gotPaths) < 2 || gotPaths[0] != "/v1/chat/completions" || gotPaths[1] != "/v1/messages" {
		t.Fatalf("paths = %v, want chat then messages", gotPaths)
	}
	if len(router.failures) != 0 {
		t.Fatalf("failures = %#v, want none (protocol miss must not poison channel)", router.failures)
	}
	if len(router.successes) != 1 || router.successes[0].channelID != 42 {
		t.Fatalf("successes = %#v, want one success on channel 42", router.successes)
	}
}

func TestDispatchDisableCrossProtocolFallbackStopsAfterPrimary(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"please use /v1/messages"}}`))
	}))
	t.Cleanup(upstream.Close)

	prev := config.Get()
	cfgCopy := *prev
	cfgCopy.DisableCrossProtocolFallback = true
	config.Set(&cfgCopy)
	t.Cleanup(func() { config.Set(prev) })

	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
		TokenValue:  "upstream-token",
		ActualModel: "claude-3",
	}}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	mu.Unlock()
	if len(gotPaths) != 1 || gotPaths[0] != "/v1/chat/completions" {
		t.Fatalf("paths = %v, want only primary chat path", gotPaths)
	}
}

func TestDispatchFirstByteTimeoutFallsBackToNextEndpointWithoutPoison(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/v1/chat/completions" {
			// Exceed 1s first-byte timeout.
			time.Sleep(1200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"late"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_fast","choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	prev := config.Get()
	cfgCopy := *prev
	cfgCopy.DisableCrossProtocolFallback = false
	// PROXY_FIRST_BYTE_TIMEOUT_SEC is seconds; convert to ms internally.
	cfgCopy.ProxyFirstByteTimeoutSec = 1
	config.Set(&cfgCopy)
	t.Cleanup(func() { config.Set(prev) })

	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o",
	}}
	SetUpstreamConfig(&UpstreamConfig{
		Router:   router,
		Executor: proxy.NewRuntimeExecutor(5 * time.Second),
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	mu.Unlock()
	if len(gotPaths) < 2 || gotPaths[0] != "/v1/chat/completions" {
		t.Fatalf("paths = %v, want timeout on chat then fallback", gotPaths)
	}
	if len(router.failures) != 0 {
		t.Fatalf("failures = %#v, want none for intermediate first-byte timeout", router.failures)
	}
	if body := rec.Body.String(); !strings.Contains(body, "chatcmpl_fast") {
		t.Fatalf("body = %q, want fast fallback response", body)
	}
}


func TestDispatchResponsesOnlySiteRoutesToResponsesAndForcesStream(t *testing.T) {
	var (
		mu       sync.Mutex
		paths    []string
		streamed bool
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/responses" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not responses"}`))
			return
		}
		if v, ok := body["stream"].(bool); ok && v {
			streamed = true
		}
		// SSE-ish response for forced stream path.
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"resp_ok\",\"type\":\"response.completed\"}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	customHeaders := `{"x-metapi-responses-only":"true"}`
	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active", Platform: "openai", CustomHeaders: &customHeaders},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o",
	}}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	// Chat path + responses-shaped body (input, no messages) should rewrite to /v1/responses.
	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","input":"hello"}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s paths=%v", rec.Code, rec.Body.String(), paths)
	}
	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	mu.Unlock()
	if len(gotPaths) != 1 || gotPaths[0] != "/v1/responses" {
		t.Fatalf("paths = %v, want only /v1/responses", gotPaths)
	}
	if !streamed {
		t.Fatal("expected stream=true forced for responses-only site")
	}
}

func TestDispatchResponsesOnlySiteRejectsChatShapedBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called; got %s", r.URL.Path)
	}))
	t.Cleanup(upstream.Close)

	customHeaders := `{"x-metapi-responses-only":"1"}`
	router := &upstreamTestRouter{selected: routing.SelectedChannel{
		Channel:     store.RouteChannel{ID: 42, Enabled: true},
		Account:     store.Account{ID: 7, Status: "active"},
		Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active", Platform: "openai", CustomHeaders: &customHeaders},
		TokenValue:  "upstream-token",
		ActualModel: "gpt-4o",
	}}
	SetUpstreamConfig(&UpstreamConfig{Router: router})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions", `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "responses-only") {
		t.Fatalf("body = %q, want clear responses-only error", body)
	}
}
