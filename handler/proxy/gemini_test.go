package proxyhandler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		// Dynamic surface /gemini/{apiVersion}/models/...
		{"/gemini/v1alpha/models/gemini-2.5-pro:generateContent", "v1alpha", "gemini-2.5-pro", "generateContent"},
		{"/gemini/v1/models/gemini-2.5-flash:streamGenerateContent", "v1", "gemini-2.5-flash", "streamGenerateContent"},
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

func TestHandleGeminiModelsList_StubFallbackCatalog(t *testing.T) {
	// TestMain enables METAPI_ENABLE_PROXY_STUB; nil upstream uses last-resort
	// owned catalog (mixed OpenAI/Claude/Gemini) then filters to gemini-ish.
	SetUpstreamConfig(nil)
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
		t.Fatal("expected non-empty models list from stub fallback residual")
	}
	// Mixed stub catalog must filter to gemini-ish names only.
	for _, item := range models {
		entry := item.(map[string]any)
		name, _ := entry["name"].(string)
		if !strings.Contains(strings.ToLower(name), "gemini") {
			t.Errorf("stub fallback should filter non-gemini entries, got %q", name)
		}
		if !strings.HasPrefix(name, "models/") {
			t.Errorf("name must use models/ resource form, got %q", name)
		}
		if _, ok := entry["displayName"]; !ok {
			t.Error("missing displayName")
		}
		if _, ok := entry["supportedGenerationMethods"]; !ok {
			t.Error("missing supportedGenerationMethods")
		}
	}
}

func TestHandleGeminiModelsList_UsesOwnedRouterCatalog(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{
		models: []string{"gpt-4o", "gemini-2.5-flash", "claude-sonnet-4-20250514", "models/gemini-2.5-pro", "gemini-2.5-flash"},
	})
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
	if len(models) != 2 {
		t.Fatalf("expected 2 gemini models after filter/dedupe/sort, got %d: %v", len(models), models)
	}
	// normalizeModelCatalog sorts: flash before pro
	first := models[0].(map[string]any)
	second := models[1].(map[string]any)
	if first["name"] != "models/gemini-2.5-flash" {
		t.Errorf("first name = %v, want models/gemini-2.5-flash", first["name"])
	}
	if second["name"] != "models/gemini-2.5-pro" {
		t.Errorf("second name = %v, want models/gemini-2.5-pro", second["name"])
	}
	if first["displayName"] != "Gemini 2.5 Flash" {
		t.Errorf("displayName = %v, want Gemini 2.5 Flash", first["displayName"])
	}
	methods, _ := first["supportedGenerationMethods"].([]any)
	if len(methods) != 3 {
		t.Errorf("supportedGenerationMethods = %v, want 3 entries", methods)
	}
}

func TestHandleGeminiModelsList_EmptyCatalog(t *testing.T) {
	// Production safety: no router + stub disabled → empty models array.
	t.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)
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
	models, ok := m["models"].([]any)
	if !ok {
		t.Fatalf("models field missing or wrong type: %T", m["models"])
	}
	if len(models) != 0 {
		t.Fatalf("expected empty models list, got %v", models)
	}
}

func TestHandleGeminiModelsList_RouterErrorEmpty(t *testing.T) {
	withModelsRouter(t, &modelsTestRouter{err: errors.New("db down")})
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
	if len(models) != 0 {
		t.Fatalf("router listing error should yield empty models, got %v", models)
	}
}

func TestHandleGeminiModelsList_AllOwnedWhenNoGeminiNames(t *testing.T) {
	// AC: prefer gemini-ish OR map all owned when none match.
	withModelsRouter(t, &modelsTestRouter{
		models: []string{"gpt-4o", "claude-sonnet-4-20250514"},
	})
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
	if len(models) != 2 {
		t.Fatalf("expected all owned models mapped when no gemini names, got %d", len(models))
	}
	first := models[0].(map[string]any)
	if first["name"] != "models/claude-sonnet-4-20250514" && first["name"] != "models/gpt-4o" {
		t.Errorf("unexpected mapped name %v", first["name"])
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

func TestBuildGeminiModelsResponse_Shape(t *testing.T) {
	resp := buildGeminiModelsResponse([]string{"gemini-2.5-pro", "models/gemini-2.5-flash", "  "})
	models, _ := resp["models"].([]map[string]any)
	if len(models) != 2 {
		t.Fatalf("expected 2 models (blank dropped), got %d", len(models))
	}
	if models[0]["name"] != "models/gemini-2.5-pro" {
		t.Errorf("name = %v", models[0]["name"])
	}
	if models[0]["displayName"] != "Gemini 2.5 Pro" {
		t.Errorf("displayName = %v", models[0]["displayName"])
	}
	if models[1]["name"] != "models/gemini-2.5-flash" {
		t.Errorf("name = %v", models[1]["name"])
	}
}

func TestSelectGeminiListModels(t *testing.T) {
	mixed := selectGeminiListModels([]string{"gpt-4o", "gemini-2.5-pro", "claude-x"})
	if len(mixed) != 1 || mixed[0] != "gemini-2.5-pro" {
		t.Fatalf("mixed filter = %v", mixed)
	}
	all := selectGeminiListModels([]string{"gpt-4o", "claude-x"})
	if len(all) != 2 {
		t.Fatalf("no-gemini should return all owned, got %v", all)
	}
	if len(selectGeminiListModels(nil)) != 0 {
		t.Fatal("nil input should yield empty")
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

	// Path-only model must populate RequestedModel for channel selection / stub.
	m := unmarshalResponse(t, rec)
	if m["model"] != "gemini-2.5-pro" {
		t.Errorf("model = %v, want path model gemini-2.5-pro", m["model"])
	}
	if m["object"] == nil {
		t.Error("expected valid response with object field")
	}
	// generateContent stays non-stream without body stream flag.
	ct := rec.Header().Get("Content-Type")
	if ct == "text/event-stream" {
		t.Errorf("generateContent without body stream must not be SSE, Content-Type = %q", ct)
	}
}

func TestHandleGeminiGenerateContent_PathOnlyModel(t *testing.T) {
	// AC #515: body omits model; path model becomes RequestedModel.
	req := makeProxyReq("POST", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	pathModel, forceStream := geminiPathModelAndStream(req.URL.Path)
	ctx, errResp := PrepareCtx(req, SurfConfig{
		Endpoint:       "gemini",
		DownstreamPath: req.URL.Path,
		RequireModel:   false,
		DefaultModel:   pathModel,
		ForceStream:    forceStream,
	})
	if errResp != nil {
		t.Fatalf("PrepareCtx error: %+v", errResp)
	}
	if ctx.RequestedModel != "gemini-2.5-flash" {
		t.Errorf("RequestedModel = %q, want gemini-2.5-flash from path", ctx.RequestedModel)
	}
	if ctx.IsStream {
		t.Error("generateContent path action must not force IsStream")
	}
}

func TestHandleGeminiGenerateContent_StreamActionForcesStream(t *testing.T) {
	// AC #515: :streamGenerateContent forces IsStream without body.stream.
	req := makeProxyReq("POST", "/v1beta/models/gemini-2.5-pro:streamGenerateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream for path stream action", ct)
	}
}

func TestHandleGeminiGenerateContent_Stream(t *testing.T) {
	// Body stream:true still works alongside path stream action.
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

func TestHandleGeminiGenerateContentDynamic_PathModelAndStream(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/gemini/{geminiApiVersion}/models/*", func(w http.ResponseWriter, r *http.Request) {
		HandleGeminiGenerateContentDynamic(w, r)
	})

	// Path-only model + stream action on dynamic surface.
	req := makeProxyReq("POST", "/gemini/v1alpha/models/gemini-2.5-flash:streamGenerateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("dynamic streamGenerateContent Content-Type = %q", rec.Header().Get("Content-Type"))
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
	// AC #515: CLI streamGenerateContent forces stream without body.stream.
	req := makeProxyReq("POST", "/v1internal::streamGenerateContent", `{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiCLIStreamGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("CLI streamGenerateContent Content-Type = %q, want text/event-stream", ct)
	}
}

func TestHandleGeminiCLIGenerateContent_NonStream(t *testing.T) {
	// generateContent CLI path stays non-stream without body stream flag.
	req := makeProxyReq("POST", "/v1internal::generateContent", `{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)
	rec := httptest.NewRecorder()
	HandleGeminiCLIGenerateContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") == "text/event-stream" {
		t.Error("CLI generateContent without body stream must not be SSE")
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
