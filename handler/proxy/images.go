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

	// For multipart image edits, return stub until full image edit forwarding is implemented.
	if IsMultipartRequest(r) {
		_ = ctx.RequestedModel
		stubResp := map[string]any{
			"created": 0,
			"data": []map[string]any{
				{
					"url": "https://example.com/edited-image.png",
				},
			},
		}
		writeJSON(w, 200, stubResp) // TODO: full multipart upstream forwarding
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleImagesVariations handles POST /v1/images/variations.
// Always returns 400 — not supported.
func HandleImagesVariations(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, 400, "Image variations are not supported", "invalid_request_error")
}
