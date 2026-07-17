package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

func setupTestRoutes(t *testing.T) (*store.DB, *channelTestHandler, chi.Router) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{AuthToken: "test-routes-token"}
	channel := &channelTestHandler{db: db.DB, cfg: cfg}
	r := chi.NewRouter()
	// Register via the public registrar, then re-bind the shared channel instance
	// so tests can inject transport.
	RegisterTestRoutes(r, db.DB, cfg)
	// Replace the channel pointer used by handlers by re-registering with shared handler.
	// Chi last-match wins for identical method+path when we mount a second router group.
	// Instead, construct routes with the injectable handler directly.
	r = chi.NewRouter()
	h := &testHandler{channel: channel}
	r.Post("/api/test/proxy", h.proxyTest)
	r.Post("/api/test/proxy/stream", h.proxyTestStream)
	r.Post("/api/test/proxy/jobs", h.proxyTestJob)
	r.Get("/api/test/proxy/jobs/{jobId}", h.proxyTestJobStatus)
	r.Delete("/api/test/proxy/jobs/{jobId}", h.proxyTestJobCancel)
	r.Post("/api/test/chat", h.chatTest)
	r.Post("/api/test/chat/stream", h.chatTestStream)
	r.Post("/api/test/chat/jobs", h.chatTestJob)
	r.Get("/api/test/chat/jobs/{jobId}", h.chatTestJobStatus)
	r.Delete("/api/test/chat/jobs/{jobId}", h.chatTestJobCancel)
	return db, channel, r
}

func TestRegisterTestRoutes_WiresPaths(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	r := chi.NewRouter()
	RegisterTestRoutes(r, db.DB, &config.Config{})

	// Missing body → 400 (validation), proves route is mounted.
	req := httptest.NewRequest(http.MethodPost, "/api/test/proxy", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("proxy mount status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestProxy_RequiresForcedChannelOrSite(t *testing.T) {
	_, _, r := setupTestRoutes(t)

	resp := doPostJSON(t, r, "/api/test/proxy", map[string]any{
		"method": "POST",
		"path":   "/v1/chat/completions",
		"model":  "gpt-4o-mini",
	})
	if resp.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s, want 501", resp.Code, resp.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["success"] != false {
		t.Fatalf("success=%v, want false (no fake success stub)", out["success"])
	}
	if msg, _ := out["message"].(string); msg == "" {
		t.Fatalf("expected residual message: %v", out)
	}
	if residual, _ := out["residual"].(string); residual == "" {
		t.Fatalf("expected residual field: %v", out)
	}
}

func TestTestProxy_ForcedChannelDelegatesToHarness(t *testing.T) {
	db, channel, r := setupTestRoutes(t)
	_, _, _, channelID := insertHarnessFixtures(t, db)

	var hits atomic.Int32
	var seenAuth string
	channel.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits.Add(1)
		seenAuth = req.Header.Get("Authorization")
		body := `{"id":"chatcmpl-proxy","choices":[{"message":{"role":"assistant","content":"proxy-ok"}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/test/proxy", map[string]any{
		"forcedChannelId": channelID,
		"method":          "POST",
		"path":            "/v1/chat/completions",
		"requestKind":     "json",
		"jsonBody": map[string]any{
			"model": "gpt-4o-mini",
			"messages": []map[string]string{
				{"role": "user", "content": "hello proxy harness"},
			},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("out=%v", out)
	}
	if out["channelId"].(float64) != float64(channelID) {
		t.Fatalf("channelId=%v want %d", out["channelId"], channelID)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
	if seenAuth != "Bearer sk-harness-api-token-xyz" {
		t.Fatalf("auth=%q", seenAuth)
	}
	if !strings.Contains(out["truncatedBody"].(string), "proxy-ok") {
		t.Fatalf("truncatedBody=%v", out["truncatedBody"])
	}
}

func TestTestChat_ChannelIDAliasHarness(t *testing.T) {
	db, channel, r := setupTestRoutes(t)
	_, _, _, channelID := insertHarnessFixtures(t, db)

	channel.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})

	resp := doPostJSON(t, r, "/api/test/chat", map[string]any{
		"channelId": channelID,
		"model":     "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "user", "content": "ping chat"},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["success"] != true {
		t.Fatalf("out=%v", out)
	}
	if out["mode"] != "chat" {
		t.Fatalf("mode=%v", out["mode"])
	}
}

func TestTestChat_SiteIDAndValidation(t *testing.T) {
	db, channel, r := setupTestRoutes(t)
	siteID, _, _, channelID := insertHarnessFixtures(t, db)

	channel.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	// Invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/test/chat", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json status=%d", rec.Code)
	}

	// siteId resolution
	resp := doPostJSON(t, r, "/api/test/chat", map[string]any{
		"siteId": siteID,
		"model":  "gpt-4o-mini",
		"prompt": "from site",
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["channelId"].(float64) != float64(channelID) {
		t.Fatalf("channelId=%v want %d", out["channelId"], channelID)
	}

	// unknown channel
	resp = doPostJSON(t, r, "/api/test/chat", map[string]any{
		"channelId": 999999,
		"model":     "gpt-4o-mini",
	})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing channel status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTestRoutes_StreamAndJobsHonest501(t *testing.T) {
	_, _, r := setupTestRoutes(t)

	cases := []struct {
		method          string
		path            string
		body            any
		want            int
		residualSnippet string
	}{
		{http.MethodPost, "/api/test/proxy/stream", map[string]any{"forcedChannelId": 1}, http.StatusNotImplemented, "no fake stream success theater"},
		{http.MethodPost, "/api/test/chat/stream", map[string]any{"channelId": 1}, http.StatusNotImplemented, "no fake stream success theater"},
		{http.MethodPost, "/api/test/proxy/jobs", map[string]any{"forcedChannelId": 1}, http.StatusNotImplemented, "no job registry or stub-job ids"},
		{http.MethodPost, "/api/test/chat/jobs", map[string]any{"channelId": 1}, http.StatusNotImplemented, "no job registry or stub-job ids"},
		{http.MethodGet, "/api/test/proxy/jobs/any", nil, http.StatusNotFound, "no in-process /api/test/proxy job queue"},
		{http.MethodGet, "/api/test/chat/jobs/any", nil, http.StatusNotFound, "no in-process /api/test/chat job queue"},
		{http.MethodDelete, "/api/test/proxy/jobs/any", nil, http.StatusNotFound, "no in-process /api/test/proxy job queue"},
		{http.MethodDelete, "/api/test/chat/jobs/any", nil, http.StatusNotFound, "no in-process /api/test/chat job queue"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			switch tc.method {
			case http.MethodPost:
				rec = doPostJSON(t, r, tc.path, tc.body)
			case http.MethodGet:
				rec = doGet(t, r, tc.path)
			case http.MethodDelete:
				rec = doDelete(t, r, tc.path)
			default:
				t.Fatalf("unsupported method %s", tc.method)
			}
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s want %d", rec.Code, rec.Body.String(), tc.want)
			}
			var out map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			// Never return success:true for unimplemented residual surfaces.
			if out["success"] == true {
				t.Fatalf("fake success on residual path: %v", out)
			}
			// Never invent stub job ids on residual job surfaces.
			if _, ok := out["jobId"]; ok {
				t.Fatalf("invented jobId on residual path: %v", out)
			}
			if residual, _ := out["residual"].(string); residual == "" {
				t.Fatalf("expected residual: %v", out)
			} else if !strings.Contains(residual, tc.residualSnippet) {
				t.Fatalf("residual=%q missing %q", residual, tc.residualSnippet)
			}
			if tc.want == http.StatusNotFound {
				errObj, _ := out["error"].(map[string]any)
				if errObj == nil {
					t.Fatalf("expected error object on job 404: %v", out)
				}
				if msg, _ := errObj["message"].(string); msg != "job not found" {
					t.Fatalf("error.message=%v", errObj["message"])
				}
			}
		})
	}
}

func TestMapFlexibleToChannelTest_ExtractsNestedFields(t *testing.T) {
	ch := int64(12)
	body := flexibleTestBody{
		ForcedChannelID: &ch,
		Path:            "/v1/chat/completions",
		JSONBody:        json.RawMessage(`{"model":"gpt-4o","messages":[{"role":"user","content":"nested hello"}]}`),
	}
	req, ok := mapFlexibleToChannelTest(body)
	if !ok {
		t.Fatal("expected mapping ok")
	}
	if req.ChannelID == nil || *req.ChannelID != 12 {
		t.Fatalf("channelId=%v", req.ChannelID)
	}
	if req.Model != "gpt-4o" {
		t.Fatalf("model=%q", req.Model)
	}
	if req.Prompt != "nested hello" {
		t.Fatalf("prompt=%q", req.Prompt)
	}
	if req.Mode != channelTestModeChat {
		t.Fatalf("mode=%q", req.Mode)
	}

	// models path detection
	site := int64(3)
	body2 := flexibleTestBody{SiteID: &site, Path: "/v1/models"}
	req2, ok := mapFlexibleToChannelTest(body2)
	if !ok {
		t.Fatal("expected site mapping")
	}
	if req2.Mode != channelTestModeModels {
		t.Fatalf("mode=%q want models", req2.Mode)
	}
}
