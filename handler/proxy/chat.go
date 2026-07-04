package proxy

import (
	"net/http"
)

// HandleChatCompletions handles POST /v1/chat/completions and POST /chat/completions.
// Surface format: "openai".
func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "chat",
		DownstreamPath: r.URL.Path,
		RequireModel:   true,
		SurfaceFormat:  "openai",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	handleChatSurfaceRequest(w, r, ctx)
}

// handleChatSurfaceRequest is the internal implementation for chat proxy surfaces.
// Surface format is read from ctx.SurfaceFormat (set by caller via SurfConfig).
func handleChatSurfaceRequest(w http.ResponseWriter, r *http.Request, ctx *Ctx) {
	dispatchUpstream(w, r, ctx)
}
