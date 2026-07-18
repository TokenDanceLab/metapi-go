package proxyhandler

import (
	"fmt"
	"strconv"
	"strings"
)

// maxTokensOverContextError is returned when body max_tokens / max_output_tokens
// / generationConfig.maxOutputTokens exceeds a positive route context_length.
// Callers must surface an honest 400 (no silent clamp).
type maxTokensOverContextError struct {
	MaxTokens     int64
	ContextLength int64
	// Field is the body key (or dotted nested path) that exceeded the limit
	// ("max_tokens", "max_output_tokens", "generationConfig.maxOutputTokens", …)
	// so error text matches the request dialect.
	Field string
}

func (e maxTokensOverContextError) Error() string {
	field := e.Field
	if field == "" {
		field = "max_tokens"
	}
	return fmt.Sprintf(
		"%s (%d) exceeds route context_length (%d)",
		field,
		e.MaxTokens,
		e.ContextLength,
	)
}

// enforceMaxTokensAgainstContextLength rejects max_tokens / max_output_tokens /
// Gemini generationConfig.maxOutputTokens above a positive route context_length
// for OpenAI chat/completions-style, Anthropic messages, OpenAI Responses, and
// Gemini generateContent bodies.
//
// Policy (issue #399 / #409 / #450 / #458 / CTX-520 residual):
//   - enforce only when context_length > 0
//   - enforce when a dialect-specific output-token field is present and parseable
//   - skip when all candidate fields are omitted, null, or unparseable
//   - never silent-clamp or invent tokens
//
// Returns maxTokensOverContextError when the request must be rejected with 400.
func enforceMaxTokensAgainstContextLength(body map[string]any, contextLength *int64) error {
	limit, ok := positiveContextLength(contextLength)
	if !ok {
		return nil
	}
	// Prefer nested Gemini generationConfig / OpenAI Responses max_output_tokens
	// when present; otherwise accept chat/messages max_tokens for parity.
	maxTokens, field, present := bodyOutputTokenLimit(body)
	if !present {
		return nil
	}
	if maxTokens > limit {
		return maxTokensOverContextError{
			MaxTokens:     maxTokens,
			ContextLength: limit,
			Field:         field,
		}
	}
	return nil
}

// shouldEnforceMaxTokensOnPath is true for OpenAI chat/completions (+ legacy
// completions), Anthropic Claude /v1/messages (issue #409), OpenAI
// /v1/responses (+ /compact) (issue #450), and Gemini generateContent /
// streamGenerateContent (issue #458). count_tokens / countTokens and other
// surfaces stay out.
func shouldEnforceMaxTokensOnPath(downstreamPath string) bool {
	p := strings.ToLower(strings.TrimSpace(downstreamPath))
	switch {
	case strings.HasSuffix(p, "/chat/completions"):
		return true
	case p == "/v1/completions" || p == "/completions" || strings.HasSuffix(p, "/v1/completions"):
		return true
	case p == "/v1/messages" || p == "/messages" || strings.HasSuffix(p, "/v1/messages"):
		return true
	// OpenAI Responses (+ compact). Alias /responses is resolved to /v1/responses*
	// before enforce; still accept suffix/alias-safe forms like the other matchers.
	case p == "/v1/responses" || p == "/responses" || strings.HasSuffix(p, "/v1/responses"):
		return true
	case p == "/v1/responses/compact" || p == "/responses/compact" || strings.HasSuffix(p, "/v1/responses/compact") || strings.HasSuffix(p, "/responses/compact"):
		return true
	// Gemini native generateContent / streamGenerateContent (v1beta models/*:action
	// and CLI /v1internal::generateContent). Substring match is action-safe:
	// "streamgeneratecontent" and "generatecontent" both contain "generatecontent";
	// countTokens does not and stays out.
	case strings.Contains(p, "generatecontent"):
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

// bodyOutputTokenLimit extracts a parseable output-token cap from the body.
// Preference order (first present + parseable wins):
//  1. Gemini nested generationConfig.maxOutputTokens (camel/snake variants)
//  2. Gemini CLI envelope request.generationConfig.*
//  3. OpenAI Responses top-level max_output_tokens
//  4. chat/completions + Claude messages top-level max_tokens
//
// Missing / null / empty / non-numeric → not present.
func bodyOutputTokenLimit(body map[string]any) (value int64, field string, present bool) {
	if body == nil {
		return 0, "", false
	}
	if n, field, ok := bodyGeminiMaxOutputTokens(body); ok {
		return n, field, true
	}
	// Gemini CLI internal envelope: { "model", "request": { generationConfig... } }
	if req, ok := body["request"].(map[string]any); ok && req != nil {
		if n, field, ok := bodyGeminiMaxOutputTokens(req); ok {
			return n, field, true
		}
	}
	if n, ok := parseBodyTokenField(body, "max_output_tokens"); ok {
		return n, "max_output_tokens", true
	}
	if n, ok := parseBodyTokenField(body, "max_tokens"); ok {
		return n, "max_tokens", true
	}
	return 0, "", false
}

// bodyGeminiMaxOutputTokens reads nested generationConfig.maxOutputTokens
// (and cheap camel/snake variants). Field is returned as a dotted path for
// honest error text. Missing / null / non-object / unparseable → not present.
func bodyGeminiMaxOutputTokens(body map[string]any) (value int64, field string, present bool) {
	if body == nil {
		return 0, "", false
	}
	for _, configKey := range []string{"generationConfig", "generation_config"} {
		raw, ok := body[configKey]
		if !ok || raw == nil {
			continue
		}
		gc, ok := raw.(map[string]any)
		if !ok || gc == nil {
			continue
		}
		for _, tokenKey := range []string{"maxOutputTokens", "max_output_tokens"} {
			if n, ok := parseBodyTokenField(gc, tokenKey); ok {
				return n, configKey + "." + tokenKey, true
			}
		}
	}
	return 0, "", false
}

// bodyMaxTokens extracts body["max_tokens"] when present as a finite whole number
// or numeric string. Missing / null / empty / non-numeric → not present.
// Kept for callers/tests that only care about the chat/messages field.
func bodyMaxTokens(body map[string]any) (int64, bool) {
	return parseBodyTokenField(body, "max_tokens")
}

// parseBodyTokenField reads body[key] as a finite whole number or numeric string.
func parseBodyTokenField(body map[string]any, key string) (int64, bool) {
	if body == nil {
		return 0, false
	}
	raw, ok := body[key]
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
