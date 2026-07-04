package proxy

import (
	"math"
	"net/http"
	"strings"
)

const (
	defaultSearchModel = "__search"
	defaultMaxResults  = 10
	maxMaxResults      = 20
)

// HandleSearch handles POST /v1/search.
// Validates: query required, stream=false required, max_results in [1,20].
func HandleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "search",
		DownstreamPath: "/v1/search",
		RequireModel:   false,
		DefaultModel:   defaultSearchModel,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	// Validate query
	query, _ := ctx.Body["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		writeJSONError(w, 400, "query is required", "invalid_request_error")
		return
	}

	// Streaming not supported
	if ctx.IsStream {
		writeJSONError(w, 400, "search does not support streaming", "invalid_request_error")
		return
	}

	// Validate max_results
	rawMaxResults := ctx.Body["max_results"]
	maxResults := defaultMaxResults
	if rawMaxResults != nil {
		switch v := rawMaxResults.(type) {
		case float64:
			if math.Trunc(v) == v && v >= 1 && v <= maxMaxResults {
				maxResults = int(v)
			} else {
				writeJSONError(w, 400, "max_results must be an integer between 1 and 20", "invalid_request_error")
				return
			}
		case int:
			if v >= 1 && v <= maxMaxResults {
				maxResults = v
			} else {
				writeJSONError(w, 400, "max_results must be an integer between 1 and 20", "invalid_request_error")
				return
			}
		default:
			writeJSONError(w, 400, "max_results must be an integer between 1 and 20", "invalid_request_error")
			return
		}
	}

	_ = maxResults

	dispatchUpstream(w, r, ctx)
}
