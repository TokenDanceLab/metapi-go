package proxyhandler

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

// ProxyVideoTask holds a video task mapping (publicId -> upstreamVideoId).
//
// Create (#235/#244) saves a process-local cache entry and dual-writes to
// `proxy_video_tasks` when store.GetDB() is available so multi-instance /
// restart can resolve publicId. GET/DELETE treat the store as an optional
// rewrite aid, not a hard gate — missing entries still pass the client id
// through to upstream. Sticky site/token pin remains residual.
// See docs/analysis/videos-proxy-residual.md.
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

// SaveProxyVideoTask saves a video task mapping to the process-local cache and
// dual-writes to proxy_video_tasks when a runtime DB is available (#244).
// Residual (#254): durable rows have no TTL/GC this wave — see docs/analysis/videos-proxy-retention-residual.md.
func SaveProxyVideoTask(task *ProxyVideoTask) {
	if task == nil || strings.TrimSpace(task.PublicID) == "" {
		return
	}
	// Store a copy so callers cannot mutate the map entry after return.
	cp := *task
	cp.PublicID = strings.TrimSpace(cp.PublicID)
	cp.UpstreamVideoID = strings.TrimSpace(cp.UpstreamVideoID)

	videoTaskStoreMu.Lock()
	videoTaskStore[cp.PublicID] = &cp
	videoTaskStoreMu.Unlock()

	if err := upsertProxyVideoTaskDB(&cp); err != nil {
		slog.Warn("proxy video task: durable upsert failed (memory cache still set)",
			"public_id", cp.PublicID, "error", err)
	}
}

// GetProxyVideoTaskByPublicID retrieves a video task by publicId.
// Memory cache first; cold miss falls back to proxy_video_tasks (#244).
func GetProxyVideoTaskByPublicID(publicID string) *ProxyVideoTask {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return nil
	}
	videoTaskStoreMu.RLock()
	task := videoTaskStore[publicID]
	if task != nil {
		cp := *task
		videoTaskStoreMu.RUnlock()
		return &cp
	}
	videoTaskStoreMu.RUnlock()

	loaded, err := loadProxyVideoTaskDB(publicID)
	if err != nil {
		slog.Debug("proxy video task: durable load failed", "public_id", publicID, "error", err)
		return nil
	}
	if loaded == nil {
		return nil
	}
	// Warm process-local cache.
	videoTaskStoreMu.Lock()
	videoTaskStore[loaded.PublicID] = loaded
	videoTaskStoreMu.Unlock()
	cp := *loaded
	return &cp
}

// DeleteProxyVideoTaskByPublicID deletes a video task by publicId (memory + DB).
func DeleteProxyVideoTaskByPublicID(publicID string) {
	publicID = strings.TrimSpace(publicID)
	if publicID == "" {
		return
	}
	videoTaskStoreMu.Lock()
	delete(videoTaskStore, publicID)
	videoTaskStoreMu.Unlock()

	if err := deleteProxyVideoTaskDB(publicID); err != nil {
		slog.Warn("proxy video task: durable delete failed", "public_id", publicID, "error", err)
	}
}

func upsertProxyVideoTaskDB(task *ProxyVideoTask) error {
	db := store.GetDB()
	if db == nil || task == nil {
		return nil
	}
	if strings.TrimSpace(task.PublicID) == "" || strings.TrimSpace(task.UpstreamVideoID) == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// store.DB rebinds ? → $N for postgres.
	const q = `
INSERT INTO proxy_video_tasks (
	public_id, upstream_video_id, site_url, token_value,
	requested_model, actual_model, channel_id, account_id,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(public_id) DO UPDATE SET
	upstream_video_id = excluded.upstream_video_id,
	site_url = excluded.site_url,
	token_value = excluded.token_value,
	requested_model = excluded.requested_model,
	actual_model = excluded.actual_model,
	channel_id = excluded.channel_id,
	account_id = excluded.account_id,
	updated_at = excluded.updated_at
`
	var reqModel, actModel any
	if strings.TrimSpace(task.RequestedModel) != "" {
		reqModel = task.RequestedModel
	}
	if strings.TrimSpace(task.ActualModel) != "" {
		actModel = task.ActualModel
	}
	var channelID, accountID any
	if task.ChannelID > 0 {
		channelID = task.ChannelID
	}
	if task.AccountID > 0 {
		accountID = task.AccountID
	}
	_, err := db.Exec(q,
		task.PublicID,
		task.UpstreamVideoID,
		task.SiteURL,
		task.TokenValue,
		reqModel,
		actModel,
		channelID,
		accountID,
		now,
		now,
	)
	return err
}

func loadProxyVideoTaskDB(publicID string) (*ProxyVideoTask, error) {
	db := store.GetDB()
	if db == nil {
		return nil, nil
	}
	const q = `
SELECT public_id, upstream_video_id, site_url, token_value,
       requested_model, actual_model, channel_id, account_id
FROM proxy_video_tasks
WHERE public_id = ?
LIMIT 1
`
	var (
		rowPublic, upstream, siteURL, token string
		reqModel, actModel                  *string
		channelID, accountID                *int64
	)
	err := db.QueryRow(q, publicID).Scan(
		&rowPublic, &upstream, &siteURL, &token,
		&reqModel, &actModel, &channelID, &accountID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	task := &ProxyVideoTask{
		PublicID:        rowPublic,
		UpstreamVideoID: upstream,
		SiteURL:         siteURL,
		TokenValue:      token,
	}
	if reqModel != nil {
		task.RequestedModel = *reqModel
	}
	if actModel != nil {
		task.ActualModel = *actModel
	}
	if channelID != nil {
		task.ChannelID = *channelID
	}
	if accountID != nil {
		task.AccountID = *accountID
	}
	return task, nil
}

func deleteProxyVideoTaskDB(publicID string) error {
	db := store.GetDB()
	if db == nil {
		return nil
	}
	_, err := db.Exec(`DELETE FROM proxy_video_tasks WHERE public_id = ?`, publicID)
	return err
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
