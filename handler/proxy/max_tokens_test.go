package proxyhandler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func ptrInt64(v int64) *int64 { return &v }

func TestEnforceMaxTokensAgainstContextLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		body          map[string]any
		contextLength *int64
		wantErr       bool
		wantMax       int64
		wantLimit     int64
	}{
		{
			name:          "over limit rejects",
			body:          map[string]any{"max_tokens": float64(9000)},
			contextLength: ptrInt64(8192),
			wantErr:       true,
			wantMax:       9000,
			wantLimit:     8192,
		},
		{
			name:          "equal limit passes",
			body:          map[string]any{"max_tokens": float64(8192)},
			contextLength: ptrInt64(8192),
			wantErr:       false,
		},
		{
			name:          "under limit passes",
			body:          map[string]any{"max_tokens": float64(100)},
			contextLength: ptrInt64(8192),
			wantErr:       false,
		},
		{
			name:          "omitted max_tokens skips",
			body:          map[string]any{"model": "gpt-4o"},
			contextLength: ptrInt64(8192),
			wantErr:       false,
		},
		{
			name:          "nil context_length skips",
			body:          map[string]any{"max_tokens": float64(999999)},
			contextLength: nil,
			wantErr:       false,
		},
		{
			name:          "zero context_length skips",
			body:          map[string]any{"max_tokens": float64(999999)},
			contextLength: ptrInt64(0),
			wantErr:       false,
		},
		{
			name:          "negative context_length skips",
			body:          map[string]any{"max_tokens": float64(100)},
			contextLength: ptrInt64(-1),
			wantErr:       false,
		},
		{
			name:          "null max_tokens skips",
			body:          map[string]any{"max_tokens": nil},
			contextLength: ptrInt64(100),
			wantErr:       false,
		},
		{
			name:          "string max_tokens over limit rejects",
			body:          map[string]any{"max_tokens": "200"},
			contextLength: ptrInt64(100),
			wantErr:       true,
			wantMax:       200,
			wantLimit:     100,
		},
		{
			name:          "json.Number over limit rejects",
			body:          map[string]any{"max_tokens": json.Number("4097")},
			contextLength: ptrInt64(4096),
			wantErr:       true,
			wantMax:       4097,
			wantLimit:     4096,
		},
		{
			name:          "unparseable max_tokens skips",
			body:          map[string]any{"max_tokens": "lots"},
			contextLength: ptrInt64(100),
			wantErr:       false,
		},
		{
			name:          "fractional max_tokens skips",
			body:          map[string]any{"max_tokens": 12.5},
			contextLength: ptrInt64(10),
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := enforceMaxTokensAgainstContextLength(tt.body, tt.contextLength)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var mtErr maxTokensOverContextError
				if !asMaxTokensErr(err, &mtErr) {
					t.Fatalf("error type = %T (%v), want maxTokensOverContextError", err, err)
				}
				if mtErr.MaxTokens != tt.wantMax || mtErr.ContextLength != tt.wantLimit {
					t.Fatalf("err fields = %+v, want max=%d limit=%d", mtErr, tt.wantMax, tt.wantLimit)
				}
				if !strings.Contains(err.Error(), "max_tokens") || !strings.Contains(err.Error(), "context_length") {
					t.Fatalf("error message = %q, want clear max_tokens/context_length wording", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func asMaxTokensErr(err error, out *maxTokensOverContextError) bool {
	if err == nil || out == nil {
		return false
	}
	e, ok := err.(maxTokensOverContextError)
	if !ok {
		return false
	}
	*out = e
	return true
}

func TestShouldEnforceMaxTokensOnPath(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"/v1/chat/completions":             true,
		"/chat/completions":                true,
		"/v1/completions":                  true,
		"/completions":                     true,
		"/v1/messages":                     false,
		"/v1/responses":                    false,
		"/v1/embeddings":                   false,
		"/v1beta/models/x:generateContent": false,
		"":                                 false,
	}
	for path, want := range cases {
		if got := shouldEnforceMaxTokensOnPath(path); got != want {
			t.Errorf("shouldEnforceMaxTokensOnPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestChatCompletions_MaxTokensOverContextLengthReturns400(t *testing.T) {
	// Integration: selected route has positive context_length and body max_tokens exceeds it.
	// Must return honest 400 without calling upstream.
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"should-not-reach"}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(1024)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "gpt-4o",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions",
		`{"model":"gpt-4o","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "max_tokens") || !strings.Contains(body, "context_length") {
		t.Fatalf("body = %q, want clear max_tokens/context_length error", body)
	}
	if !strings.Contains(body, "invalid_request_error") {
		t.Fatalf("body = %q, want invalid_request_error", body)
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream was called %d times; expected 0 (must not silent-forward)", upstreamHits)
	}
}

func TestChatCompletions_MaxTokensAtContextLengthPassesThrough(t *testing.T) {
	upstreamHits := 0
	var sawMaxTokens any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sawMaxTokens = payload["max_tokens"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(2048)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "gpt-4o",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions",
		`{"model":"gpt-4o","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
	// No silent clamp: original max_tokens must be forwarded unchanged.
	if sawMaxTokens != float64(2048) {
		t.Fatalf("upstream max_tokens = %v (%T), want 2048 (no clamp)", sawMaxTokens, sawMaxTokens)
	}
}

func TestChatCompletions_NoContextLengthDoesNotEnforce(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 1, Enabled: true},
			Account:     store.Account{ID: 1, Status: "active"},
			Site:        store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "gpt-4o",
			// ContextLength nil → no enforce
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions",
		`{"model":"gpt-4o","max_tokens":999999,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
}

func TestChatCompletions_OmittedMaxTokensDoesNotEnforce(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[]}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(128)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "gpt-4o",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/chat/completions",
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
}

func TestCompletions_MaxTokensOverContextLengthReturns400(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"nope"}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(512)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "gpt-3.5-turbo-instruct",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/completions",
		`{"model":"gpt-3.5-turbo-instruct","prompt":"hi","max_tokens":1024}`)
	rec := httptest.NewRecorder()
	HandleCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream was called; expected rejection before forward")
	}
}
