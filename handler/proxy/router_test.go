package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/auth"
)

// makeProxyReq creates a new authenticated httptest request with JSON body.
func makeProxyReq(method, path, jsonBody string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	pac := &auth.ProxyAuthContext{
		Token:  "test-token",
		Source: "global",
		Policy: auth.EmptyDownstreamRoutingPolicy,
	}
	ctx := auth.WithProxyAuth(req.Context(), pac)
	return req.WithContext(ctx)
}

// makeProxyReqNoBody creates an authenticated request without a body (for GET/DELETE).
func makeProxyReqNoBody(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	pac := &auth.ProxyAuthContext{
		Token:  "test-token",
		Source: "global",
		Policy: auth.EmptyDownstreamRoutingPolicy,
	}
	ctx := auth.WithProxyAuth(req.Context(), pac)
	return req.WithContext(ctx)
}

// injectAuth is a chi middleware that injects a test auth context.
func injectAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pac := &auth.ProxyAuthContext{
			Token:  "test-token",
			Source: "global",
			Policy: auth.EmptyDownstreamRoutingPolicy,
		}
		ctx := auth.WithProxyAuth(r.Context(), pac)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- Route Registration ----

func TestRegisterProxyRoutes_AllPathsExist(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	paths := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/chat/completions", `{"model":"gpt-4o"}`},
		{"POST", "/messages", `{"model":"claude-3","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`},
		{"POST", "/messages/count_tokens", `{"messages":[{"role":"user","content":"hi"}]}`},
		{"POST", "/completions", `{"model":"gpt-3.5","prompt":"hi"}`},
		{"POST", "/responses", `{"model":"gpt-4o","input":"hi"}`},
		{"GET", "/responses", ``},
		{"POST", "/responses/compact", `{"model":"gpt-4o","input":"hi"}`},
		{"GET", "/models", ``},
		{"POST", "/embeddings", `{"model":"text-embedding","input":"hi"}`},
		{"POST", "/images/generations", `{"prompt":"test"}`},
		{"POST", "/images/edits", `{"image":"test","prompt":"edit"}`},
		{"POST", "/images/variations", `{"image":"test"}`},
		{"POST", "/videos", `{"model":"sora-2","prompt":"test"}`},
		{"POST", "/search", `{"query":"test"}`},
		{"GET", "/files", ``},
		{"POST", "/files", ``},
	}

	for _, p := range paths {
		t.Run(fmt.Sprintf("%s %s", p.method, p.path), func(t *testing.T) {
			req := makeProxyReq(p.method, p.path, p.body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code == 405 {
				t.Errorf("%s %s returned 405 (Method Not Allowed)", p.method, p.path)
			}
			if rec.Code == 404 {
				t.Errorf("%s %s returned 404 (route not registered)", p.method, p.path)
			}
		})
	}
}

// ---- Integration-style tests ----

func TestChatCompletionsRoute_Unauthorized(t *testing.T) {
	r := chi.NewRouter()
	RegisterProxyRoutes(r)

	req := httptest.NewRequest("POST", "/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletionsRoute_ModelMissing(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReq("POST", "/chat/completions", `{"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCompletionsRoute_ModelMissing(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReq("POST", "/completions", `{"prompt":"hello"}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEmbeddingsRoute_ModelMissing(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReq("POST", "/embeddings", `{"input":"test"}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSearchRoute_QueryMissing(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReq("POST", "/search", `{}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestImagesVariationsRoute_Always400(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReq("POST", "/images/variations", `{"image":"test"}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResponsesGETRoute_426(t *testing.T) {
	r := chi.NewRouter()
	RegisterProxyRoutes(r)

	req := httptest.NewRequest("GET", "/responses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 426 {
		t.Errorf("expected 426, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFilesListRoute_Returns200(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReqNoBody("GET", "/files")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFilesUploadRoute_501(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReqNoBody("POST", "/files")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 501 {
		t.Errorf("expected 501, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVideosCreateRoute_ModelRequired(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterProxyRoutes(r)

	req := makeProxyReq("POST", "/videos", `{}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNonV1Routes_Registration(t *testing.T) {
	r := chi.NewRouter()
	r.Use(injectAuth)
	RegisterNonV1ProxyRoutes(r)

	paths := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/chat/completions", `{"model":"gpt-4o"}`},
		{"POST", "/responses", `{"model":"gpt-4o","input":"hi"}`},
		{"POST", "/responses/compact", `{"model":"gpt-4o","input":"hi"}`},
	}
	for _, p := range paths {
		t.Run(fmt.Sprintf("%s %s", p.method, p.path), func(t *testing.T) {
			req := makeProxyReq(p.method, p.path, p.body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code == 404 {
				t.Errorf("%s %s returned 404", p.method, p.path)
			}
		})
	}
}

// Ensure imports used
var _ = io.ReadAll
var _ = json.Marshal
