package proxyhandler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/auth"
)

// authRequest creates a request with a ProxyAuthContext in its context.
func authRequest(method, path string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	pac := &auth.ProxyAuthContext{
		Token:  "test-token",
		Source: "global",
		Policy: auth.EmptyDownstreamRoutingPolicy,
	}
	ctx := auth.WithProxyAuth(context.Background(), pac)
	return req.WithContext(ctx)
}

// ---- SurfConfig ----

func TestSurfConfig_Defaults(t *testing.T) {
	cfg := SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
	}
	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries default = %d, want 0", cfg.MaxRetries)
	}
	if cfg.RequireModel {
		t.Error("RequireModel should default to false")
	}
	if cfg.DefaultModel != "" {
		t.Error("DefaultModel should default to empty")
	}
}

// ---- PrepareCtx ----

func TestPrepareCtx_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
	})

	if ctx != nil {
		t.Error("ctx should be nil for unauthorized request")
	}
	if errResp == nil {
		t.Fatal("errResp should not be nil")
	}
	if errResp.Status != 401 {
		t.Errorf("status = %d, want 401", errResp.Status)
	}
}

func TestPrepareCtx_BasicRequest(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.Auth == nil {
		t.Fatal("auth should not be nil")
	}
	if ctx.RequestedModel != "gpt-4o" {
		t.Errorf("RequestedModel = %q, want gpt-4o", ctx.RequestedModel)
	}
	if ctx.Auth.Token != "test-token" {
		t.Errorf("token = %q", ctx.Auth.Token)
	}
}

func TestPrepareCtx_ModelRequired_Missing(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"messages":[{"role":"user","content":"hi"}]}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
	})

	if ctx != nil {
		t.Error("ctx should be nil when model is missing")
	}
	if errResp == nil {
		t.Fatal("errResp should not be nil")
	}
	if errResp.Status != 400 {
		t.Errorf("status = %d, want 400", errResp.Status)
	}
}

func TestPrepareCtx_ModelOptional(t *testing.T) {
	req := authRequest("POST", "/v1/images/generations", []byte(`{"prompt":"a cat"}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "images",
		DownstreamPath: "/v1/images/generations",
		RequireModel:   false,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.RequestedModel != "" {
		t.Errorf("RequestedModel = %q, want empty", ctx.RequestedModel)
	}
}

func TestPrepareCtx_DefaultModel(t *testing.T) {
	req := authRequest("POST", "/v1/images/generations", []byte(`{"prompt":"test"}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "images",
		DownstreamPath: "/v1/images/generations",
		RequireModel:   false,
		DefaultModel:   "gpt-image-1",
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.RequestedModel != "gpt-image-1" {
		t.Errorf("RequestedModel = %q, want gpt-image-1", ctx.RequestedModel)
	}
}

func TestPrepareCtx_DefaultModel_DoesNotOverride(t *testing.T) {
	req := authRequest("POST", "/v1/images/generations", []byte(`{"model":"custom-model"}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "images",
		DownstreamPath: "/v1/images/generations",
		RequireModel:   false,
		DefaultModel:   "gpt-image-1",
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.RequestedModel != "custom-model" {
		t.Errorf("RequestedModel = %q, want custom-model", ctx.RequestedModel)
	}
}

func TestPrepareCtx_StreamDetection(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStream bool
	}{
		{"no stream field", `{"model":"gpt-4o"}`, false},
		{"stream false", `{"model":"gpt-4o","stream":false}`, false},
		{"stream true", `{"model":"gpt-4o","stream":true}`, true},
		{"stream string true", `{"model":"gpt-4o","stream":"true"}`, true},
		{"stream string 1", `{"model":"gpt-4o","stream":"1"}`, true},
		{"stream string false", `{"model":"gpt-4o","stream":"false"}`, false},
		{"stream string 0", `{"model":"gpt-4o","stream":"0"}`, false},
		{"stream number 0", `{"model":"gpt-4o","stream":0}`, false},
		{"stream number 1", `{"model":"gpt-4o","stream":1}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := authRequest("POST", "/v1/chat/completions", []byte(tt.body))
			ctx, errResp := PrepareCtx(req, SurfConfig{
				Endpoint:       "chat",
				DownstreamPath: "/v1/chat/completions",
				RequireModel:   true,
			})
			if errResp != nil {
				t.Fatalf("unexpected error: %+v", errResp)
			}
			if ctx.IsStream != tt.wantStream {
				t.Errorf("IsStream = %v, want %v", ctx.IsStream, tt.wantStream)
			}
		})
	}
}

func TestPrepareCtx_InvalidJSON(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{invalid`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
	})

	if ctx != nil {
		t.Error("ctx should be nil for invalid JSON")
	}
	if errResp == nil {
		t.Fatal("errResp should not be nil")
	}
	if errResp.Status != 400 {
		t.Errorf("status = %d, want 400", errResp.Status)
	}
}

func TestPrepareCtx_EmptyBody(t *testing.T) {
	req := authRequest("POST", "/v1/models", []byte(``))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "models",
		DownstreamPath: "/v1/models",
		RequireModel:   false,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.Body == nil {
		t.Error("body should be non-nil map")
	}
	if len(ctx.Body) != 0 {
		t.Errorf("body should be empty, got %d keys", len(ctx.Body))
	}
}

func TestPrepareCtx_MaxRetries_FromConfig(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o"}`))

	// MaxRetries zero -> should use default from config
	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
		MaxRetries:     0,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.MaxRetries <= 0 {
		t.Errorf("MaxRetries = %d, want > 0 (from config default)", ctx.MaxRetries)
	}
}

func TestPrepareCtx_CustomMaxRetries(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o"}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
		MaxRetries:     3,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", ctx.MaxRetries)
	}
}

func TestPrepareCtx_ClientDetection(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"model":"gpt-4o"}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	// ClientCtx should be populated (at minimum "generic" kind)
	if ctx.ClientCtx.ClientKind == "" {
		t.Error("ClientCtx.ClientKind should not be empty")
	}
}

func TestPrepareCtx_ModelTrimWhitespace(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"model":"  gpt-4o  "}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}
	if ctx.RequestedModel != "gpt-4o" {
		t.Errorf("RequestedModel = %q, want gpt-4o (trimmed)", ctx.RequestedModel)
	}
}

// ---- SurfResult ----

func TestSurfResult_Defaults(t *testing.T) {
	sr := SurfResult{}
	if sr.OK {
		t.Error("SurfResult.OK should default to false")
	}
	if sr.Status != 0 {
		t.Errorf("SurfResult.Status = %d, want 0", sr.Status)
	}
}

// ---- Ctx ----

func TestCtx_AllFields(t *testing.T) {
	req := authRequest("POST", "/v1/chat/completions", []byte(`{"model":"claude-sonnet-4-20250514","stream":true}`))

	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: "/v1/chat/completions",
		RequireModel:   true,
		MaxRetries:     5,
	})

	if errResp != nil {
		t.Fatalf("unexpected error: %+v", errResp)
	}

	if ctx.Auth == nil {
		t.Error("Auth should be set")
	}
	if ctx.RequestedModel == "" {
		t.Error("RequestedModel should be set")
	}
	if !ctx.IsStream {
		t.Error("IsStream should be true")
	}
	if ctx.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d", ctx.MaxRetries)
	}
	if ctx.Body == nil {
		t.Error("Body should be set")
	}
	if ctx.Headers == nil {
		t.Error("Headers should be set")
	}
}

// ---- isStreamFromBody ----

func TestIsStreamFromBody_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want bool
	}{
		{"nil body", nil, false},
		{"empty body", map[string]any{}, false},
		{"stream string empty", map[string]any{"stream": ""}, false},
		{"stream string random", map[string]any{"stream": "yes"}, false},
		{"stream negative number", map[string]any{"stream": float64(-1)}, true}, // non-zero -> true
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStreamFromBody(tt.body)
			if got != tt.want {
				t.Errorf("isStreamFromBody = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- jsonSafeString (roundtrip test via writeJSON) ----

func TestJsonSafeString_Encoding(t *testing.T) {
	result := jsonSafeString(`"hello"`)
	if result != `\"hello\"` {
		t.Errorf("jsonSafeString = %q", result)
	}
	result = jsonSafeString("line1\nline2")
	if result != "line1\\nline2" {
		t.Errorf("jsonSafeString newline = %q", result)
	}
}

// ---- unmarshalResponse helper ----

func unmarshalResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON response: %v\nBody: %s", err, rec.Body.String())
	}
	return m
}

func unmarshalArrayResponse(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	var a []any
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("invalid JSON array response: %v\nBody: %s", err, rec.Body.String())
	}
	return a
}
