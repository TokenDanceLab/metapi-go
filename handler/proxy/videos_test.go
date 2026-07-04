package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

// chiRouterForVideo creates a chi router that registers video routes with auth injection.
func chiRouterForVideo() chi.Router {
	r := chi.NewRouter()
	r.Use(injectAuth)
	r.Post("/v1/videos", HandleVideosCreate)
	r.Get("/v1/videos/{id}", HandleVideosGet)
	r.Delete("/v1/videos/{id}", HandleVideosDelete)
	return r
}

func TestHandleVideosCreate_JSONBody(t *testing.T) {
	r := chiRouterForVideo()
	req := makeProxyReq("POST", "/v1/videos", `{"model":"sora-2","prompt":"a cat walking"}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Fatal("expected non-nil response")
	}
	if m["model"] != "sora-2" {
		t.Errorf("model = %v", m["model"])
	}
}

func TestHandleVideosCreate_ModelRequired(t *testing.T) {
	r := chiRouterForVideo()
	req := makeProxyReq("POST", "/v1/videos", `{"prompt":"test"}`)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleVideosCreate_Unauthorized(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/v1/videos", HandleVideosCreate)
	req := httptest.NewRequest("POST", "/v1/videos", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandleVideosGet_Found(t *testing.T) {
	publicID := "video_test_get_123"
	SaveProxyVideoTask(&ProxyVideoTask{
		PublicID:        publicID,
		UpstreamVideoID: "upstream_" + publicID,
		RequestedModel:  "sora-2",
	})

	r := chiRouterForVideo()
	req := makeProxyReqNoBody("GET", "/v1/videos/"+publicID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// With upstream forwarding not wired, stub returns generic response.
	m := unmarshalResponse(t, rec)
	if m == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestHandleVideosGet_NotFound(t *testing.T) {
	r := chiRouterForVideo()
	req := makeProxyReqNoBody("GET", "/v1/videos/nonexistent_video_id")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleVideosDelete_Found(t *testing.T) {
	publicID := "video_test_delete_456"
	SaveProxyVideoTask(&ProxyVideoTask{
		PublicID:        publicID,
		UpstreamVideoID: "upstream_" + publicID,
		RequestedModel:  "sora-2",
	})

	r := chiRouterForVideo()
	req := makeProxyReqNoBody("DELETE", "/v1/videos/"+publicID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 204 {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted
	task := GetProxyVideoTaskByPublicID(publicID)
	if task != nil {
		t.Error("task should be deleted")
	}
}

func TestHandleVideosDelete_NotFound(t *testing.T) {
	r := chiRouterForVideo()
	req := makeProxyReqNoBody("DELETE", "/v1/videos/nonexistent_id")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- Video store helpers ----

func TestSaveAndGetProxyVideoTask(t *testing.T) {
	task := &ProxyVideoTask{
		PublicID:        "public_123",
		UpstreamVideoID: "up_456",
		RequestedModel:  "sora-2",
		ActualModel:     "sora-2",
		ChannelID:       1,
		AccountID:       2,
	}
	SaveProxyVideoTask(task)

	got := GetProxyVideoTaskByPublicID("public_123")
	if got == nil {
		t.Fatal("expected task to be saved")
	}
	if got.UpstreamVideoID != "up_456" {
		t.Errorf("UpstreamVideoID = %q", got.UpstreamVideoID)
	}
	if got.ChannelID != 1 {
		t.Errorf("ChannelID = %d", got.ChannelID)
	}

	// Delete
	DeleteProxyVideoTaskByPublicID("public_123")
	got = GetProxyVideoTaskByPublicID("public_123")
	if got != nil {
		t.Error("task should be deleted")
	}
}

func TestGetProxyVideoTaskByPublicID_NotFound(t *testing.T) {
	got := GetProxyVideoTaskByPublicID("never_exists")
	if got != nil {
		t.Error("expected nil for nonexistent task")
	}
}

func TestDeleteProxyVideoTaskByPublicID_Idempotent(t *testing.T) {
	// Deleting nonexistent should not panic
	DeleteProxyVideoTaskByPublicID("does_not_exist")
}

func TestProxyVideoTask_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "video_concurrent_" + itoa(int64(n))
			SaveProxyVideoTask(&ProxyVideoTask{PublicID: id})
			_ = GetProxyVideoTaskByPublicID(id)
			DeleteProxyVideoTaskByPublicID(id)
		}(i)
	}
	wg.Wait()
}

func TestProxyVideoTask_SiteURLAndToken(t *testing.T) {
	task := &ProxyVideoTask{
		PublicID:   "vid_url",
		SiteURL:    "https://api.openai.com",
		TokenValue: "sk-test",
	}
	SaveProxyVideoTask(task)
	got := GetProxyVideoTaskByPublicID("vid_url")
	if got.SiteURL != "https://api.openai.com" {
		t.Errorf("SiteURL = %q", got.SiteURL)
	}
	if got.TokenValue != "sk-test" {
		t.Errorf("TokenValue = %q", got.TokenValue)
	}
	DeleteProxyVideoTaskByPublicID("vid_url")
}

var _ = json.Marshal
var _ = http.MethodPost
