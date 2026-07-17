package proxyhandler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/routing"
)

// ProxyVideoTask holds a video task mapping (publicId -> upstreamVideoId).
//
// Create (#235) saves a process-local mapping on successful POST /v1/videos and
// rewrites the response `id` to publicId. GET/DELETE treat the local store as
// an optional rewrite aid, not a hard gate — missing entries still pass the
// client id through to upstream. Multi-instance durable DB store and sticky
// site/token pin remain residual. See docs/analysis/videos-proxy-residual.md.
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
// On successful non-stream upstream 2xx, dispatch rewrites response `id` to a
// generated publicId and SaveProxyVideoTask (process-local). See #235.
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
	if task == nil || strings.TrimSpace(task.PublicID) == "" {
		return
	}
	videoTaskStoreMu.Lock()
	defer videoTaskStoreMu.Unlock()
	// Store a copy so callers cannot mutate the map entry after return.
	cp := *task
	videoTaskStore[cp.PublicID] = &cp
}

// GetProxyVideoTaskByPublicID retrieves a video task by publicId.
func GetProxyVideoTaskByPublicID(publicID string) *ProxyVideoTask {
	videoTaskStoreMu.RLock()
	defer videoTaskStoreMu.RUnlock()
	task := videoTaskStore[publicID]
	if task == nil {
		return nil
	}
	cp := *task
	return &cp
}

// DeleteProxyVideoTaskByPublicID deletes a video task by publicId.
func DeleteProxyVideoTaskByPublicID(publicID string) {
	videoTaskStoreMu.Lock()
	defer videoTaskStoreMu.Unlock()
	delete(videoTaskStore, publicID)
}

// maybeRewriteVideosCreateResponse rewrites a successful POST /v1/videos body so
// clients see an opaque publicId, and seeds the process-local mapping used by
// GET/DELETE path rewrite. Best-effort: any parse/shape miss returns body unchanged.
//
// Multi-instance durable store and sticky site pin are residual (#235 follow-ups).
func maybeRewriteVideosCreateResponse(
	ctx *Ctx,
	selected *routing.SelectedChannel,
	upstreamPath string,
	body []byte,
) []byte {
	if ctx == nil || selected == nil || len(body) == 0 {
		return body
	}
	path := strings.TrimSpace(upstreamPath)
	if path == "" {
		path = strings.TrimSpace(ctx.DownstreamPath)
	}
	path = strings.TrimSuffix(path, "/")
	if !strings.EqualFold(path, "/v1/videos") {
		return body
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	upstreamID, _ := payload["id"].(string)
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return body
	}

	publicID := newPublicVideoID()
	actualModel := selected.ActualModel
	if actualModel == "" {
		actualModel = ctx.RequestedModel
	}
	SaveProxyVideoTask(&ProxyVideoTask{
		PublicID:        publicID,
		UpstreamVideoID: upstreamID,
		SiteURL:         selected.Site.URL,
		TokenValue:      selected.TokenValue,
		RequestedModel:  ctx.RequestedModel,
		ActualModel:     actualModel,
		ChannelID:       selected.Channel.ID,
		AccountID:       selected.Account.ID,
	})

	payload["id"] = publicID
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func newPublicVideoID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "video_" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return "video_" + hex.EncodeToString(b[:])
}
