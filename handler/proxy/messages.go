package proxyhandler

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

// HandleClaudeMessages handles POST /v1/messages.
// Surface format: "claude".
func HandleClaudeMessages(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "messages",
		DownstreamPath: "/v1/messages",
		RequireModel:   true,
		SurfaceFormat:  "claude",
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// HandleClaudeCountTokens handles POST /v1/messages/count_tokens.
func HandleClaudeCountTokens(w http.ResponseWriter, r *http.Request) {
	ctx, errResp := PrepareCtx(r, SurfConfig{
		Endpoint:       "messages",
		DownstreamPath: "/v1/messages/count_tokens",
		RequireModel:   false,
	})
	if errResp != nil {
		writeJSONError(w, errResp.Status, errResp.Error, errResp.ErrorType)
		return
	}

	dispatchUpstream(w, r, ctx)
}

// Helper functions

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func jsonSafeString(s string) string {
	return jsonEscape(s)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Use JSON encoding
	buf := make([]byte, 0, 256)
	buf = appendJSON(buf, body)
	w.Write(buf)
}

func appendJSON(buf []byte, v any) []byte {
	switch val := v.(type) {
	case map[string]any:
		buf = append(buf, '{')
		first := true
		for k, vv := range val {
			if !first {
				buf = append(buf, ',')
			}
			first = false
			buf = append(buf, '"')
			buf = append(buf, []byte(jsonEscapeStr(k))...)
			buf = append(buf, '"', ':')
			buf = appendJSON(buf, vv)
		}
		buf = append(buf, '}')
	case []map[string]any:
		buf = append(buf, '[')
		for i, item := range val {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSON(buf, item)
		}
		buf = append(buf, ']')
	case []any:
		buf = append(buf, '[')
		for i, item := range val {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = appendJSON(buf, item)
		}
		buf = append(buf, ']')
	case string:
		buf = append(buf, '"')
		buf = append(buf, []byte(jsonEscapeStr(val))...)
		buf = append(buf, '"')
	case float64:
		buf = append(buf, []byte(ftoa(val))...)
	case int:
		buf = append(buf, []byte(itoa(int64(val)))...)
	case int64:
		buf = append(buf, []byte(itoa(val))...)
	case bool:
		if val {
			buf = append(buf, "true"...)
		} else {
			buf = append(buf, "false"...)
		}
	case nil:
		buf = append(buf, "null"...)
	case time.Time:
		buf = append(buf, []byte(itoa(val.Unix()))...)
	default:
		buf = append(buf, "null"...)
	}
	return buf
}

func jsonEscapeStr(s string) string {
	return jsonEscape(s)
}

func ftoa(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	// Simple float-to-string for JSON
	if f == float64(int64(f)) && f < 1e15 && f > -1e15 {
		return itoa(int64(f))
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
