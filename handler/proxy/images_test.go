package proxyhandler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

// ---- HandleImagesGenerations ----

func TestHandleImagesGenerations_DefaultModel(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/generations", `{"prompt":"a beautiful sunset"}`)
	rec := httptest.NewRecorder()
	HandleImagesGenerations(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	// Verify valid JSON response was received.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
}

func TestHandleImagesGenerations_CustomModel(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/generations", `{"model":"dall-e-3","prompt":"a cat"}`)
	rec := httptest.NewRecorder()
	HandleImagesGenerations(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImagesGenerations_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/images/generations", nil)
	rec := httptest.NewRecorder()
	HandleImagesGenerations(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

// ---- HandleImagesEdits ----

func TestHandleImagesEdits_JSONBody(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/edits", `{"image":"base64data","prompt":"add a hat"}`)
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic chat.completion response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Error("expected non-nil response")
	}
	// Must not be the retired multipart theater payload.
	if body := rec.Body.String(); strings.Contains(body, "example.com/edited-image") {
		t.Fatalf("unexpected retired stub payload: %s", body)
	}
}

func TestHandleImagesEdits_Unauthorized(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/images/edits", nil)
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandleImagesEdits_MultipartUnauthorized(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("model", "gpt-image-1"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/images/edits", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "example.com/edited-image") {
		t.Fatalf("unexpected retired stub payload on unauthorized multipart: %s", body)
	}
}

func TestHandleImagesEdits_InvalidMultipartReturnsBadRequest(t *testing.T) {
	req := makeProxyReqNoBody("POST", "/v1/images/edits")
	req.Body = io.NopCloser(strings.NewReader("not a valid multipart body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing-boundary")
	rec := httptest.NewRecorder()

	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid multipart, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "example.com/edited-image") {
		t.Fatalf("unexpected retired stub payload: %s", body)
	}
}

func TestHandleImagesEdits_MultipartUsesDispatchUpstreamNotFakeImage(t *testing.T) {
	// Default TestMain enables METAPI_ENABLE_PROXY_STUB with nil upstream config.
	// Multipart edits must take the same dispatchUpstream path as JSON (generic
	// proxy stub), never the retired example.com image theater.
	SetUpstreamConfig(nil)
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("model", "gpt-image-1"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "add a hat"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	part, err := writer.CreateFormFile("image", "source.png")
	if err != nil {
		t.Fatalf("create image field: %v", err)
	}
	if _, err := part.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write image field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := makeProxyReqNoBody("POST", "/v1/images/edits")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via proxy stub path, got %d: %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "example.com/edited-image") || strings.Contains(raw, "https://example.com/") {
		t.Fatalf("retired multipart theater payload still returned: %s", raw)
	}
	m := unmarshalResponse(t, rec)
	// Generic METAPI_ENABLE_PROXY_STUB response (same family as JSON edits).
	if m["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion from dispatchUpstream stub", m["object"])
	}
	if m["model"] != "gpt-image-1" {
		t.Fatalf("model = %v, want gpt-image-1", m["model"])
	}
}

func TestHandleImagesEdits_MultipartForwardsToUpstream(t *testing.T) {
	var gotModel, gotPrompt, gotFilename string
	var gotImageBody string
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer upstream-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data;") {
			t.Errorf("Content-Type = %q, want multipart/form-data", got)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("parse upstream multipart: %v", err)
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		gotModel = r.FormValue("model")
		gotPrompt = r.FormValue("prompt")
		files := r.MultipartForm.File["image"]
		if len(files) != 1 {
			t.Errorf("image file count = %d, want 1", len(files))
		} else {
			gotFilename = files[0].Filename
			f, err := files[0].Open()
			if err != nil {
				t.Errorf("open image: %v", err)
			} else {
				defer f.Close()
				data, _ := io.ReadAll(f)
				gotImageBody = string(data)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"ZmFrZQ=="}]}`))
	}))
	t.Cleanup(upstream.Close)

	SetUpstreamConfig(&UpstreamConfig{
		Router: &upstreamTestRouter{selected: routing.SelectedChannel{
			Channel:     store.RouteChannel{ID: 42, Enabled: true},
			Account:     store.Account{ID: 7, Status: "active"},
			Site:        store.Site{ID: 3, URL: upstream.URL, Status: "active"},
			TokenValue:  "upstream-token",
			ActualModel: "gpt-image-1-upstream",
		}},
	})
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("model", "gpt-image-1"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "add a hat"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	part, err := writer.CreateFormFile("image", "source.png")
	if err != nil {
		t.Fatalf("create image field: %v", err)
	}
	if _, err := part.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write image field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := makeProxyReqNoBody("POST", "/v1/images/edits")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/images/edits" {
		t.Fatalf("upstream path = %q, want /v1/images/edits", gotPath)
	}
	if gotModel != "gpt-image-1-upstream" {
		t.Fatalf("upstream model = %q, want gpt-image-1-upstream (ActualModel rewrite)", gotModel)
	}
	if gotPrompt != "add a hat" {
		t.Fatalf("prompt = %q", gotPrompt)
	}
	if gotFilename != "source.png" {
		t.Fatalf("filename = %q", gotFilename)
	}
	if gotImageBody != "fake-png-bytes" {
		t.Fatalf("image body = %q", gotImageBody)
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "example.com/edited-image") {
		t.Fatalf("retired stub payload mixed with upstream response: %s", raw)
	}
	if !strings.Contains(raw, "ZmFrZQ==") {
		t.Fatalf("body = %q, want upstream edit payload", raw)
	}
}

func TestHandleImagesEdits_MultipartNoUpstreamNoStubReturnsUnavailable(t *testing.T) {
	t.Setenv("METAPI_ENABLE_PROXY_STUB", "0")
	SetUpstreamConfig(nil)
	t.Cleanup(func() { SetUpstreamConfig(nil) })

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("prompt", "add a hat"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	part, err := writer.CreateFormFile("image", "source.png")
	if err != nil {
		t.Fatalf("create image field: %v", err)
	}
	if _, err := part.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write image field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := makeProxyReqNoBody("POST", "/v1/images/edits")
	req.Body = io.NopCloser(body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	HandleImagesEdits(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when stub disabled and upstream unset, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "example.com/edited-image") {
		t.Fatalf("unexpected retired stub payload: %s", body)
	}
}

// ---- HandleImagesVariations ----

func TestHandleImagesVariations(t *testing.T) {
	req := makeProxyReq("POST", "/v1/images/variations", `{"image":"test"}`)
	rec := httptest.NewRecorder()
	HandleImagesVariations(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
