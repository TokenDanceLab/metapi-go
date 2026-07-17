package proxyhandler

import (
	"fmt"
	"strconv"
	"strings"
)

// maxTokensOverContextError is returned when body max_tokens exceeds a positive
// route context_length. Callers must surface an honest 400 (no silent clamp).
type maxTokensOverContextError struct {
	MaxTokens     int64
	ContextLength int64
}

func (e maxTokensOverContextError) Error() string {
	return fmt.Sprintf(
		"max_tokens (%d) exceeds route context_length (%d)",
		e.MaxTokens,
		e.ContextLength,
	)
}

// enforceMaxTokensAgainstContextLength rejects max_tokens above a positive
// route context_length for OpenAI chat/completions-style and Anthropic messages bodies.
//
// Policy (issue #399 / #409 / CTX-520):
//   - enforce only when context_length > 0
//   - enforce only when max_tokens is present and parseable as an integer
//   - skip when max_tokens is omitted, null, or unparseable
//   - never silent-clamp or invent tokens
//
// Returns maxTokensOverContextError when the request must be rejected with 400.
func enforceMaxTokensAgainstContextLength(body map[string]any, contextLength *int64) error {
	limit, ok := positiveContextLength(contextLength)
	if !ok {
		return nil
	}
	maxTokens, present := bodyMaxTokens(body)
	if !present {
		return nil
	}
	if maxTokens > limit {
		return maxTokensOverContextError{MaxTokens: maxTokens, ContextLength: limit}
	}
	return nil
}

// shouldEnforceMaxTokensOnPath is true for OpenAI chat/completions (+ legacy completions)
// and Anthropic Claude /v1/messages (issue #409). count_tokens and other surfaces stay out.
func shouldEnforceMaxTokensOnPath(downstreamPath string) bool {
	p := strings.ToLower(strings.TrimSpace(downstreamPath))
	switch {
	case strings.HasSuffix(p, "/chat/completions"):
		return true
	case p == "/v1/completions" || p == "/completions" || strings.HasSuffix(p, "/v1/completions"):
		return true
	case p == "/v1/messages" || p == "/messages" || strings.HasSuffix(p, "/v1/messages"):
		return true
	default:
		return false
	}
}

func positiveContextLength(contextLength *int64) (int64, bool) {
	if contextLength == nil || *contextLength <= 0 {
		return 0, false
	}
	return *contextLength, true
}

// bodyMaxTokens extracts body["max_tokens"] when present as a finite whole number
// or numeric string. Missing / null / empty / non-numeric → not present.
func bodyMaxTokens(body map[string]any) (int64, bool) {
	if body == nil {
		return 0, false
	}
	raw, ok := body["max_tokens"]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		// JSON numbers unmarshal as float64.
		if v != v { // NaN
			return 0, false
		}
		if v > float64(^uint64(0)>>1) || v < float64(int64(-1<<63)) {
			return 0, false
		}
		// Only accept whole-number values (no fractional tokens).
		n := int64(v)
		if v != float64(n) {
			return 0, false
		}
		return n, true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		// encoding/json.Number is a string under the hood.
		if s, ok := raw.(fmt.Stringer); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(s.String()), 10, 64)
			if err == nil {
				return n, true
			}
		}
		return 0, false
	}
}
