package proxyhandler

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// ProxyVideoTask holds a video task mapping (publicId -> upstreamVideoId).
type ProxyVideoTask struct {
	PublicID        string `json:"publicId"`
	UpstreamVideoID string `json:"upstreamVideoId"`
	SiteURL         string `json:"siteUrl"`
	TokenValue      string `json:"tokenValue"`
	RequestedModel  string `json:"requestedModel"`
	ActualModel     string `json:"actualModel"`
	ChannelID       int64  `json:"channelId"`
	AccountID       int64  `json:"accountId"`
}

var (
	videoTaskStore   = make(map[string]*ProxyVideoTask)
	videoTaskStoreMu sync.RWMutex
)

// HandleVideosCreate handles POST /v1/videos.
// Supports multipart/form-data or JSON body. Model is required.
func HandleVideosCreate(w http.ResponseWriter, r *http.Request) {
	EnsureMultipartBufferParser()

	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "videos",
		DownstreamPath: "/v1/videos",
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	if ctx.RequestedModel == "" {
		writeJSONError(w, 400, "model is required", "invalid_request_error")
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleVideosGet handles GET /v1/videos/{id}.
func HandleVideosGet(w http.ResponseWriter, r *http.Request) {
	publicID := chi.URLParam(r, "id")
	if publicID == "" {
		writeJSONError(w, 400, "missing video id", "invalid_request_error")
		return
	}

	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "videos",
		DownstreamPath: "/v1/videos/" + publicID,
		RequireModel:   false,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	videoTaskStoreMu.RLock()
	task, ok := videoTaskStore[publicID]
	videoTaskStoreMu.RUnlock()

	if !ok {
		writeJSONError(w, 404, "Video task not found", "not_found_error")
		return
	}

	_ = task
	_ = ctx
	dispatchUpstream(w, r, ctx)
}

// HandleVideosDelete handles DELETE /v1/videos/{id}.
func HandleVideosDelete(w http.ResponseWriter, r *http.Request) {
	publicID := chi.URLParam(r, "id")
	if publicID == "" {
		writeJSONError(w, 400, "missing video id", "invalid_request_error")
		return
	}

	videoTaskStoreMu.RLock()
	_, ok := videoTaskStore[publicID]
	videoTaskStoreMu.RUnlock()

	if !ok {
		writeJSONError(w, 404, "Video task not found", "not_found_error")
		return
	}

	videoTaskStoreMu.Lock()
	delete(videoTaskStore, publicID)
	videoTaskStoreMu.Unlock()

	w.WriteHeader(204)
}

// SaveProxyVideoTask saves a video task mapping.
func SaveProxyVideoTask(task *ProxyVideoTask) {
	videoTaskStoreMu.Lock()
	defer videoTaskStoreMu.Unlock()
	videoTaskStore[task.PublicID] = task
}

// GetProxyVideoTaskByPublicID retrieves a video task by publicId.
func GetProxyVideoTaskByPublicID(publicID string) *ProxyVideoTask {
	videoTaskStoreMu.RLock()
	defer videoTaskStoreMu.RUnlock()
	return videoTaskStore[publicID]
}

// DeleteProxyVideoTaskByPublicID deletes a video task by publicId.
func DeleteProxyVideoTaskByPublicID(publicID string) {
	videoTaskStoreMu.Lock()
	defer videoTaskStoreMu.Unlock()
	delete(videoTaskStore, publicID)
}

// Ensure json import is used
var _ = json.Marshal
