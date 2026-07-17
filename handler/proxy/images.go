package proxyhandler

import (
	"net/http"
)

// HandleImagesGenerations handles POST /v1/images/generations.
// Model defaults to "gpt-image-1".
func HandleImagesGenerations(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "images",
		DownstreamPath: "/v1/images/generations",
		RequireModel:   false,
		DefaultModel:   "gpt-image-1",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleImagesEdits handles POST /v1/images/edits.
// Supports multipart/form-data and JSON body. Model defaults to "gpt-image-1".
// Multipart is parsed in PrepareCtx and forwarded via CloneMultipartBody in dispatchUpstream.
func HandleImagesEdits(w http.ResponseWriter, r *http.Request) {
	EnsureMultipartBufferParser()

	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "images",
		DownstreamPath: "/v1/images/edits",
		RequireModel:   false,
		DefaultModel:   "gpt-image-1",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleImagesVariations handles POST /v1/images/variations.
// Always returns 400 — not supported.
func HandleImagesVariations(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, 400, "Image variations are not supported", "invalid_request_error")
}
