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
		wantField     string
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
		// OpenAI Responses max_output_tokens (issue #450)
		{
			name:          "max_output_tokens over limit rejects",
			body:          map[string]any{"max_output_tokens": float64(9000)},
			contextLength: ptrInt64(8192),
			wantErr:       true,
			wantMax:       9000,
			wantLimit:     8192,
			wantField:     "max_output_tokens",
		},
		{
			name:          "max_output_tokens equal limit passes",
			body:          map[string]any{"max_output_tokens": float64(8192)},
			contextLength: ptrInt64(8192),
			wantErr:       false,
		},
		{
			name:          "max_output_tokens under limit passes",
			body:          map[string]any{"max_output_tokens": float64(100)},
			contextLength: ptrInt64(8192),
			wantErr:       false,
		},
		{
			name:          "null max_output_tokens skips",
			body:          map[string]any{"max_output_tokens": nil},
			contextLength: ptrInt64(100),
			wantErr:       false,
		},
		{
			name:          "unparseable max_output_tokens skips",
			body:          map[string]any{"max_output_tokens": "lots"},
			contextLength: ptrInt64(100),
			wantErr:       false,
		},
		{
			name:          "string max_output_tokens over limit rejects",
			body:          map[string]any{"max_output_tokens": "200"},
			contextLength: ptrInt64(100),
			wantErr:       true,
			wantMax:       200,
			wantLimit:     100,
			wantField:     "max_output_tokens",
		},
		{
			name: "max_output_tokens preferred when both present",
			body: map[string]any{
				"max_output_tokens": float64(9000),
				"max_tokens":        float64(50), // under limit; must not mask over-limit output tokens
			},
			contextLength: ptrInt64(8192),
			wantErr:       true,
			wantMax:       9000,
			wantLimit:     8192,
			wantField:     "max_output_tokens",
		},
		{
			name: "max_tokens used when max_output_tokens omitted",
			body: map[string]any{
				"max_tokens": float64(9000),
			},
			contextLength: ptrInt64(8192),
			wantErr:       true,
			wantMax:       9000,
			wantLimit:     8192,
			wantField:     "max_tokens",
		},
		{
			name: "unparseable max_output_tokens falls back to max_tokens",
			body: map[string]any{
				"max_output_tokens": "nope",
				"max_tokens":        float64(200),
			},
			contextLength: ptrInt64(100),
			wantErr:       true,
			wantMax:       200,
			wantLimit:     100,
			wantField:     "max_tokens",
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
				wantField := tt.wantField
				if wantField == "" {
					wantField = "max_tokens"
				}
				if !strings.Contains(err.Error(), wantField) || !strings.Contains(err.Error(), "context_length") {
					t.Fatalf("error message = %q, want clear %s/context_length wording", err.Error(), wantField)
				}
				if mtErr.Field != "" && mtErr.Field != wantField {
					t.Fatalf("err.Field = %q, want %q", mtErr.Field, wantField)
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
		"/v1/messages":                     true,
		"/messages":                        true,
		"/v1/messages/count_tokens":        false,
		"/v1/responses":                    true,
		"/responses":                       true,
		"/v1/responses/compact":            true,
		"/responses/compact":               true,
		"/openai/v1/responses":             true,
		"/openai/v1/responses/compact":     true,
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

func TestClaudeMessages_MaxTokensOverContextLengthReturns400(t *testing.T) {
	// Issue #409: Claude /v1/messages must reject max_tokens above positive route context_length.
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
			ActualModel:   "claude-sonnet-4-20250514",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/messages",
		`{"model":"claude-sonnet-4-20250514","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

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

func TestClaudeMessages_MaxTokensAtContextLengthPassesThrough(t *testing.T) {
	upstreamHits := 0
	var sawMaxTokens any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sawMaxTokens = payload["max_tokens"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_ok","type":"message","role":"assistant","content":[]}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(2048)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "claude-sonnet-4-20250514",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/messages",
		`{"model":"claude-sonnet-4-20250514","max_tokens":2048,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

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

func TestClaudeMessages_MaxTokensUnderContextLengthPassesThrough(t *testing.T) {
	upstreamHits := 0
	var sawMaxTokens any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sawMaxTokens = payload["max_tokens"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_ok","type":"message","role":"assistant","content":[]}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(8192)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "claude-sonnet-4-20250514",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/messages",
		`{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
	if sawMaxTokens != float64(100) {
		t.Fatalf("upstream max_tokens = %v (%T), want 100 (no clamp)", sawMaxTokens, sawMaxTokens)
	}
}

func TestClaudeMessages_NoContextLengthDoesNotEnforce(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_ok","type":"message","role":"assistant","content":[]}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 1, Enabled: true},
			Account:     store.Account{ID: 1, Status: "active"},
			Site:        store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "claude-sonnet-4-20250514",
			// ContextLength nil → no enforce
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/messages",
		`{"model":"claude-sonnet-4-20250514","max_tokens":999999,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
}

func TestClaudeMessages_ZeroContextLengthDoesNotEnforce(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_ok","type":"message","role":"assistant","content":[]}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(0)
	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:       store.RouteChannel{ID: 1, Enabled: true},
			Account:       store.Account{ID: 1, Status: "active"},
			Site:          store.Site{ID: 1, URL: upstream.URL, Status: "active"},
			TokenValue:    "upstream-token",
			ActualModel:   "claude-sonnet-4-20250514",
			ContextLength: &limit,
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	req := makeProxyReq("POST", "/v1/messages",
		`{"model":"claude-sonnet-4-20250514","max_tokens":999999,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	HandleClaudeMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
}

func TestBodyOutputTokenLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		body        map[string]any
		wantValue   int64
		wantField   string
		wantPresent bool
	}{
		{
			name:        "max_output_tokens present",
			body:        map[string]any{"max_output_tokens": float64(128)},
			wantValue:   128,
			wantField:   "max_output_tokens",
			wantPresent: true,
		},
		{
			name:        "max_tokens present",
			body:        map[string]any{"max_tokens": float64(64)},
			wantValue:   64,
			wantField:   "max_tokens",
			wantPresent: true,
		},
		{
			name: "prefer max_output_tokens",
			body: map[string]any{
				"max_output_tokens": float64(10),
				"max_tokens":        float64(99),
			},
			wantValue:   10,
			wantField:   "max_output_tokens",
			wantPresent: true,
		},
		{
			name:        "omitted skips",
			body:        map[string]any{"model": "gpt-4o"},
			wantPresent: false,
		},
		{
			name:        "null max_output_tokens skips",
			body:        map[string]any{"max_output_tokens": nil},
			wantPresent: false,
		},
		{
			name:        "unparseable max_output_tokens skips",
			body:        map[string]any{"max_output_tokens": "nope"},
			wantPresent: false,
		},
		{
			name: "fallback to max_tokens when max_output_tokens unparseable",
			body: map[string]any{
				"max_output_tokens": "nope",
				"max_tokens":        float64(42),
			},
			wantValue:   42,
			wantField:   "max_tokens",
			wantPresent: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, field, present := bodyOutputTokenLimit(tt.body)
			if present != tt.wantPresent {
				t.Fatalf("present = %v, want %v (got=%d field=%q)", present, tt.wantPresent, got, field)
			}
			if !tt.wantPresent {
				return
			}
			if got != tt.wantValue || field != tt.wantField {
				t.Fatalf("got (%d, %q), want (%d, %q)", got, field, tt.wantValue, tt.wantField)
			}
		})
	}
}

func TestResponses_MaxOutputTokensOverContextLengthReturns400(t *testing.T) {
	// Issue #450: OpenAI /v1/responses must reject max_output_tokens above positive route context_length.
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

	req := makeProxyReq("POST", "/v1/responses",
		`{"model":"gpt-4o","max_output_tokens":2048,"input":"hi"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "max_output_tokens") || !strings.Contains(body, "context_length") {
		t.Fatalf("body = %q, want clear max_output_tokens/context_length error", body)
	}
	if !strings.Contains(body, "invalid_request_error") {
		t.Fatalf("body = %q, want invalid_request_error", body)
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream was called %d times; expected 0 (must not silent-forward)", upstreamHits)
	}
}

func TestResponses_MaxTokensOverContextLengthReturns400(t *testing.T) {
	// Parity: accept max_tokens on /v1/responses as well.
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"should-not-reach"}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(512)
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

	req := makeProxyReq("POST", "/v1/responses",
		`{"model":"gpt-4o","max_tokens":1024,"input":"hi"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "max_tokens") {
		t.Fatalf("body = %q, want max_tokens field in error", rec.Body.String())
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream was called; expected rejection before forward")
	}
}

func TestResponsesCompact_MaxOutputTokensOverContextLengthReturns400(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"should-not-reach"}`))
	}))
	t.Cleanup(upstream.Close)

	limit := int64(256)
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

	req := makeProxyReq("POST", "/v1/responses/compact",
		`{"model":"gpt-4o","max_output_tokens":512,"input":"hi"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses/compact")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "max_output_tokens") {
		t.Fatalf("body = %q, want max_output_tokens wording", rec.Body.String())
	}
	if upstreamHits != 0 {
		t.Fatalf("upstream was called; expected rejection before forward")
	}
}

func TestResponses_MaxOutputTokensAtContextLengthPassesThrough(t *testing.T) {
	upstreamHits := 0
	var sawMaxOutput any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sawMaxOutput = payload["max_output_tokens"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"response","status":"completed"}`))
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

	req := makeProxyReq("POST", "/v1/responses",
		`{"model":"gpt-4o","max_output_tokens":2048,"input":"hi"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
	// No silent clamp: original max_output_tokens must be forwarded unchanged.
	if sawMaxOutput != float64(2048) {
		t.Fatalf("upstream max_output_tokens = %v (%T), want 2048 (no clamp)", sawMaxOutput, sawMaxOutput)
	}
}

func TestResponses_OmittedMaxOutputTokensDoesNotEnforce(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"response","status":"completed"}`))
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

	req := makeProxyReq("POST", "/v1/responses",
		`{"model":"gpt-4o","input":"hi"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
}

func TestResponses_NoContextLengthDoesNotEnforce(t *testing.T) {
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_ok","object":"response","status":"completed"}`))
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

	req := makeProxyReq("POST", "/v1/responses",
		`{"model":"gpt-4o","max_output_tokens":999999,"input":"hi"}`)
	rec := httptest.NewRecorder()
	HandleResponses(rec, req, "/v1/responses")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if upstreamHits != 1 {
		t.Fatalf("upstream hits = %d, want 1", upstreamHits)
	}
}
