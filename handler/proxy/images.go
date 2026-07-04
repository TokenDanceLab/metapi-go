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

	// For multipart, return stub until full multipart upstream forwarding is implemented
	if IsMultipartRequest(r) {
		requestedModel := "gpt-image-1"
		mp, err := ParseMultipartFormData(r)
		if err == nil && mp != nil {
			if m := mp.GetField("model"); m != "" {
				requestedModel = m
			}
		}
		_ = requestedModel
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
