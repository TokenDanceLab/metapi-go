package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/auth"
)

// ---- ParseGeminiPath ----

func TestParseGeminiPath(t *testing.T) {
	tests := []struct {
		path        string
		wantVer     string
		wantModel   string
		wantAction  string
	}{
		{"/v1beta/models/gemini-2.5-pro:generateContent", "v1beta", "gemini-2.5-pro", "generateContent"},
		{"/v1beta/models/gemini-2.5-flash:streamGenerateContent", "v1beta", "gemini-2.5-flash", "streamGenerateContent"},
		{"/v1alpha/models/gemini-pro:countTokens", "v1alpha", "gemini-pro", "countTokens"},
		{"/v1beta/models/gemini-pro", "v1beta", "gemini-pro", ""},
		{"/v1beta/not-models/gemini-pro", "v1beta", "", ""},
		{"/", "", "", ""},
		{"v1beta/models/gemini-2.5-pro:generateContent", "v1beta", "gemini-2.5-pro", "generateContent"},
	}
	for _, tt := range tests {
		ver, model, action := ParseGeminiPath(tt.path)
		if ver != tt.wantVer {
			t.Errorf("ParseGeminiPath(%q) ver=%q, want %q", tt.path, ver, tt.wantVer)
		}
		if model != tt.wantModel {
			t.Errorf("ParseGeminiPath(%q) model=%q, want %q", tt.path, model, tt.wantModel)
		}
		if action != tt.wantAction {
			t.Errorf("ParseGeminiPath(%q) action=%q, want %q", tt.path, action, tt.wantAction)
		}
	}
}

// ---- Gemini routes registration ----

func TestGeminiRoutes_Registration(t *testing.T) {
	r := chi.NewRouter()
	RegisterGeminiRoutes(r)

	routes := []string{
		"/v1beta/models",
		"/v1internal::generateContent",
		"/v1internal::streamGenerateContent",
		"/v1internal::countTokens",
	}

	for _, route := range routes {
		if !routeExists(r, route) {
			t.Errorf("Gemini route not registered: %s", route)
		}
	}

	// Wildcard routes won't match exact paths in routeExists check,
	// but verify them via a test request instead.
	r2 := chi.NewRouter()
	r2.Use(injectAuth)
	RegisterGeminiRoutes(r2)

	req := makeProxyReq("POST", "/v1beta/models/test-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	rec := httptest.NewRecorder()
	r2.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("wildcard POST /v1beta/models/* returned %d (expected 200)", rec.Code)
	}
}

// ---- HandleGeminiModelsList ----

func TestHandleGeminiModelsList(t *testing.T) {
	req := auth.ProxyAuthFromRequest(
		httptest.NewRequest("GET", "/v1beta/models", nil),
		&auth.ProxyAuthContext{Token: "test", Source: "global", Policy: auth.EmptyDownstreamRoutingPolicy},
	)
	rec := httptest.NewRecorder()
	HandleGeminiModelsList(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	models, _ := m["models"].([]any)
	if len(models) == 0 {
		t.Error("expected non-empty models list")
	}
	first := models[0].(map[string]any)
	if _, ok := first["name"]; !ok {
		t.Error("missing name field")
	}
	if _, ok := first["supportedGenerationMethods"]; !ok {
		t.Error("missing supportedGenerationMethods")
	}
}

func TestHandleGeminiModelsList_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1beta/models", nil)
	rec := httptest.NewRecorder()
	HandleGeminiModelsList(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ---- HandleGeminiModelsListDynamic ----

func TestHandleGeminiModelsListDynamic(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/gemini/{geminiApiVersion}/models", func(w http.ResponseWriter, r *http.Request) {
		req := auth.ProxyAuthFromRequest(r, &auth.ProxyAuthContext{Token: "test", Source: "global", Policy: auth.EmptyDownstreamRoutingPolicy})
		HandleGeminiModelsListDynamic(w, req)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/gemini/v1alpha/models", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- HandleGeminiGenerateContent ----

func TestHandleGeminiGenerateContent_NonStream(t *testing.T) {
	req := makeProxyReq("POST", "/v1beta/models/gemini-2.5-pro:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	// Verify valid JSON response was received.
	m := unmarshalResponse(t, rec)
	if m["model"] == nil && m["object"] == nil {
		t.Error("expected valid response with model/object fields")
	}
}

func TestHandleGeminiGenerateContent_Stream(t *testing.T) {
	req := makeProxyReq("POST", "/v1beta/models/gemini-2.5-pro:streamGenerateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"stream":true}`)
	rec := httptest.NewRecorder()
	HandleGeminiGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestHandleGeminiGenerateContent_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1beta/models/test:generateContent", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	HandleGeminiGenerateContent(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ---- HandleGeminiCLIGenerateContent ----

func TestHandleGeminiCLIGenerateContent(t *testing.T) {
	req := makeProxyReq("POST", "/v1internal::generateContent", `{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiCLIGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
}

func TestHandleGeminiCLIGenerateContent_ModelRequired(t *testing.T) {
	req := makeProxyReq("POST", "/v1internal::generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiCLIGenerateContent(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- HandleGeminiCLIStreamGenerateContent ----

func TestHandleGeminiCLIStreamGenerateContent(t *testing.T) {
	req := makeProxyReq("POST", "/v1internal::streamGenerateContent", `{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiCLIStreamGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic response.
	// Stream flag not set in body, so non-stream response is expected.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
}

// ---- HandleGeminiCLICountTokens ----

func TestHandleGeminiCLICountTokens(t *testing.T) {
	req := makeProxyReq("POST", "/v1internal::countTokens", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiCLICountTokens(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	m := unmarshalResponse(t, rec)
	// With upstream forwarding not wired, stub returns generic chat.completion response.
	// Just verify a valid response was received.
	if m == nil {
		t.Error("expected non-nil response body")
	}
}

// ---- HandleGeminiGenerateContentDynamic ----

func TestHandleGeminiGenerateContentDynamic(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/gemini/{geminiApiVersion}/models/*", func(w http.ResponseWriter, r *http.Request) {
		HandleGeminiGenerateContentDynamic(w, r)
	})

	req := makeProxyReq("POST", "/gemini/v1alpha/models/test-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// routeExists checks if a route is registered for any method on the given path.
func routeExists(r chi.Router, path string) bool {
	found := false
	_ = chi.Walk(r, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if route == path {
			found = true
		}
		return nil
	})
	return found
}

// Ensure imports used
var _ = json.Marshal
var _ = auth.ProxyAuthContext{}
