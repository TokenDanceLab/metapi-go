package proxyhandler

import (
	"net/http"
)

// HandleRerank handles POST /v1/rerank.
//
// OpenAI-compatible / Cohere-style rerank surface. Request body is passed
// through to upstream /v1/rerank after standard PrepareCtx model swap.
// Expected body fields (validated only for model; rest is passthrough):
//
//	{
//	  "model": "rerank-...",
//	  "query": "...",
//	  "documents": ["...", ...] // or array of objects
//	}
//
// Streaming is not supported.
func HandleRerank(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "rerank",
		DownstreamPath: "/v1/rerank",
		RequireModel:   true,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	if ctx.IsStream {
		writeJSONError(w, 400, "rerank does not support streaming", "invalid_request_error")
		return
	}

	dispatchUpstream(w, r, ctx)
}
