package proxyhandler

import (
	"net/http"
)

// HandleEmbeddings handles POST /v1/embeddings.
func HandleEmbeddings(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "embeddings",
		DownstreamPath: "/v1/embeddings",
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	if ctx.IsStream {
		writeJSONError(w, 400, "embeddings does not support streaming", "invalid_request_error")
		return
	}

	dispatchUpstream(w, r, ctx)
}
