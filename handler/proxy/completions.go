package proxy

import (
	"net/http"
)

// HandleCompletions handles POST /v1/completions (legacy non-chat completions).
func HandleCompletions(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "completions",
		DownstreamPath: "/v1/completions",
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}
