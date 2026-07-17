package proxyhandler

import (
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// ProxyVideoTask holds a video task mapping (publicId -> upstreamVideoId).
//
// Residual (#225): Create currently dispatches upstream but does not save this
// mapping or rewrite response id. GET/DELETE therefore treat the local store as
// an optional rewrite aid, not a hard gate — missing entries pass the client id
// through to upstream. See docs/analysis/videos-proxy-residual.md.
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
//
// Residual: does not yet persist ProxyVideoTask or rewrite upstream response id
// to a publicId. Clients that receive an upstream id can still GET/DELETE it via
// honest passthrough when the local mapping is absent.
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
//
// If a local mapping exists and UpstreamVideoID differs from the public path id,
// the upstream path uses UpstreamVideoID. When the mapping is missing, the
// client-provided id is passed through — no store-gated 404 theater.
func HandleVideosGet(w http.ResponseWriter, r *http.Request) {
	publicID := chi.URLParam(r, "id")
	if publicID == "" {
		writeJSONError(w, 400, "missing video id", "invalid_request_error")
		return
	}

	upstreamID := resolveVideoUpstreamID(publicID)

	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "videos",
		DownstreamPath: "/v1/videos/" + upstreamID,
		RequireModel:   false,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleVideosDelete handles DELETE /v1/videos/{id}.
//
// Clears any local mapping for the public id, then always dispatches DELETE to
// upstream (mapping is optional rewrite aid, not a hard gate). Prefer honest
// upstream status over a local-only 204 residual.
func HandleVideosDelete(w http.ResponseWriter, r *http.Request) {
	publicID := chi.URLParam(r, "id")
	if publicID == "" {
		writeJSONError(w, 400, "missing video id", "invalid_request_error")
		return
	}

	upstreamID := resolveVideoUpstreamID(publicID)
	// Best-effort local cleanup whether or not a mapping existed.
	DeleteProxyVideoTaskByPublicID(publicID)

	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "videos",
		DownstreamPath: "/v1/videos/" + upstreamID,
		RequireModel:   false,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// resolveVideoUpstreamID returns the upstream path id for a client-facing id.
// When a mapping exists with a non-empty UpstreamVideoID different from publicID,
// that upstream id is used; otherwise publicID is passed through unchanged.
func resolveVideoUpstreamID(publicID string) string {
	task := GetProxyVideoTaskByPublicID(publicID)
	if task == nil {
		return publicID
	}
	if task.UpstreamVideoID != "" && task.UpstreamVideoID != publicID {
		return task.UpstreamVideoID
	}
	return publicID
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
