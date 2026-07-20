package routing

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/tokendancelab/metapi-go/store"
)

// Multi-tier context routing (#514 / cita-777/metapi#514).
//
// Product: multiple token_routes may share the same model pattern with different
// context_length ceilings (e.g. 32k token-bill route vs 128k req-bill route).
// When the inbound request's estimated context is known, pick the tightest
// positive ceiling that still fits; fall back to unsized (NULL/≤0) catch-alls.
//
// When estimate is 0 (unknown), callers keep pre-#514 first-match behavior.

// EstimateRequestContextTokens is a best-effort chars/4 estimate of inbound
// context from a parsed request body. Prefers messages/input string leaves;
// optionally adds max_tokens / max_output_tokens when parseable. Returns 0 when
// no reliable signal exists (caller must not invent tiers).
func EstimateRequestContextTokens(body map[string]any) int64 {
	if body == nil {
		return 0
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0
	}
	chars := sumMessagesInputStringRunes(raw)
	if chars <= 0 {
		return 0
	}
	tokens := chars / 4
	if tokens < 1 {
		tokens = 1
	}
	// Include declared generation budget when present so long-output plans
	// prefer a larger tier when the prompt alone is near a ceiling.
	if maxTok := parsePositiveTokenCap(body); maxTok > 0 {
		tokens += maxTok
	}
	const maxEst = 2_000_000
	if tokens > maxEst {
		tokens = maxEst
	}
	return tokens
}

func parsePositiveTokenCap(body map[string]any) int64 {
	for _, key := range []string{"max_output_tokens", "max_tokens"} {
		if v, ok := body[key]; ok {
			if n, ok := anyToPositiveInt64(v); ok {
				return n
			}
		}
	}
	// Gemini nested generationConfig.maxOutputTokens
	if gen, ok := body["generationConfig"].(map[string]any); ok {
		for _, key := range []string{"maxOutputTokens", "max_output_tokens"} {
			if v, ok := gen[key]; ok {
				if n, ok := anyToPositiveInt64(v); ok {
					return n
				}
			}
		}
	}
	if req, ok := body["request"].(map[string]any); ok {
		return parsePositiveTokenCap(req)
	}
	return 0
}

func anyToPositiveInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return int64(n), true
		}
	case int32:
		if n > 0 {
			return int64(n), true
		}
	case int64:
		if n > 0 {
			return n, true
		}
	case float64:
		if n > 0 && n < float64(int64(^uint64(0)>>1)) {
			return int64(n), true
		}
	case json.Number:
		i, err := n.Int64()
		if err == nil && i > 0 {
			return i, true
		}
	case string:
		// leave unparsed — avoid inventing
	}
	return 0, false
}

// sumMessagesInputStringRunes walks JSON "messages" / "input" and sums rune
// lengths of string leaves (same strategy as auth TPM estimate).
func sumMessagesInputStringRunes(raw []byte) int64 {
	raw = trimSpaceBytes(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return 0
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return 0
	}
	var total int64
	if m, ok := top["messages"]; ok {
		total += sumJSONStringRunes(m)
	}
	if in, ok := top["input"]; ok {
		total += sumJSONStringRunes(in)
	}
	// Claude / Gemini alternate envelopes
	if sys, ok := top["system"]; ok {
		total += sumJSONStringRunes(sys)
	}
	if contents, ok := top["contents"]; ok {
		total += sumJSONStringRunes(contents)
	}
	return total
}

func sumJSONStringRunes(raw json.RawMessage) int64 {
	raw = trimSpaceBytes(raw)
	if len(raw) == 0 {
		return 0
	}
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0
		}
		return int64(utf8.RuneCountInString(s))
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return 0
		}
		var n int64
		for _, el := range arr {
			n += sumJSONStringRunes(el)
		}
		return n
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return 0
		}
		var n int64
		for _, el := range obj {
			n += sumJSONStringRunes(el)
		}
		return n
	default:
		return 0
	}
}

func trimSpaceBytes(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}

// PickContextTierRoute selects among same-priority model matches by context
// ceiling. requestedTokens must be >0; otherwise the first route is returned
// unchanged (first-match honesty).
//
// Rules:
//  1. Among routes with positive context_length >= requestedTokens, pick the
//     **smallest** ceiling (tightest fit).
//  2. Else if any positive context_length exists, pick the **largest** ceiling
//     (best available still short of estimate).
//  3. Else prefer unsized (NULL/≤0) catch-all routes (first by id).
//  4. Empty input → nil.
func PickContextTierRoute(routes []store.TokenRoute, requestedTokens int64) *store.TokenRoute {
	if len(routes) == 0 {
		return nil
	}
	if requestedTokens <= 0 || len(routes) == 1 {
		r := routes[0]
		return &r
	}

	var (
		bestFit      *store.TokenRoute
		bestFitCeil  int64
		largest      *store.TokenRoute
		largestCeil  int64
		firstUnsized *store.TokenRoute
	)

	for i := range routes {
		r := &routes[i]
		ceil, ok := positiveContextLength(r.ContextLength)
		if !ok {
			if firstUnsized == nil {
				cp := *r
				firstUnsized = &cp
			}
			continue
		}
		if largest == nil || ceil > largestCeil {
			cp := *r
			largest = &cp
			largestCeil = ceil
		}
		if ceil >= requestedTokens {
			if bestFit == nil || ceil < bestFitCeil {
				cp := *r
				bestFit = &cp
				bestFitCeil = ceil
			}
		}
	}

	if bestFit != nil {
		return bestFit
	}
	// Prefer catch-all when nothing sized is large enough? Product intent:
	// oversized requests should use long-context / coding-plan tiers if present
	// (largest), else unsized.
	if largest != nil {
		return largest
	}
	if firstUnsized != nil {
		return firstUnsized
	}
	r := routes[0]
	return &r
}

func positiveContextLength(v *int64) (int64, bool) {
	if v == nil || *v <= 0 {
		return 0, false
	}
	return *v, true
}
