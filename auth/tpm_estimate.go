package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"
)

// Best-effort TPM admission estimate (#495 / learn #116).
//
// Strategy (documented intentionally; not a tokenizer):
//  1. Prefer JSON body fields used by chat/completions & responses APIs:
//     sum string rune lengths under "messages" and "input", then chars/4.
//  2. Else fall back to Content-Length/4 when present (floor only).
//  3. Clamp positive estimates to [1, 128000].
//  4. Unreadable / empty / non-JSON with no Content-Length → 0 (skip TPM accounting).
//
// Never invent large defaults that false-block admission.
const (
	tpmEstimateMaxTokens int64 = 128_000
	// Cap how much of the body we peek for estimation so huge uploads do not
	// blow middleware memory. Content-Length floor still applies above the cap.
	tpmEstimateMaxBodyBytes int64 = 256 << 10 // 256 KiB
)

// estimateAdmissionTokens returns a best-effort token estimate for TPM admission.
// Safe to call on any request; restores r.Body when it was peeked.
// Returns 0 when no reliable signal is available (caller should skip TPM reserve).
func estimateAdmissionTokens(r *http.Request) int64 {
	if r == nil {
		return 0
	}

	chars := int64(0)
	if r.Body != nil && r.Body != http.NoBody {
		// Peek up to cap+1 so we can detect truncation without consuming the rest.
		peek, err := io.ReadAll(io.LimitReader(r.Body, tpmEstimateMaxBodyBytes+1))
		if err != nil {
			// Re-chain whatever was read with the remaining body; skip JSON path.
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peek), r.Body))
		} else if int64(len(peek)) > tpmEstimateMaxBodyBytes {
			// Partial peek only — re-attach full stream; do not parse partial JSON.
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peek), r.Body))
		} else {
			// Full body (within cap) was consumed; restore from bytes.
			r.Body = io.NopCloser(bytes.NewReader(peek))
			if n := sumMessagesInputStringRunes(peek); n > 0 {
				chars = n
			}
		}
	}

	if chars <= 0 {
		// Content-Length floor when JSON path yielded nothing.
		if r.ContentLength > 0 {
			chars = r.ContentLength
		} else {
			return 0
		}
	}

	// chars/4 ≈ tokens; clamp positive results to [1, 128000].
	tokens := chars / 4
	if tokens < 1 {
		tokens = 1
	}
	if tokens > tpmEstimateMaxTokens {
		tokens = tpmEstimateMaxTokens
	}
	return tokens
}

// sumMessagesInputStringRunes walks JSON "messages" / "input" and sums rune
// lengths of string leaves. Returns 0 for non-JSON or missing fields.
func sumMessagesInputStringRunes(raw []byte) int64 {
	raw = bytes.TrimSpace(raw)
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
	return total
}

// sumJSONStringRunes recursively sums rune counts of all JSON string values.
func sumJSONStringRunes(raw json.RawMessage) int64 {
	raw = bytes.TrimSpace(raw)
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
		for _, v := range obj {
			n += sumJSONStringRunes(v)
		}
		return n
	default:
		// numbers, bools, null — ignore
		return 0
	}
}
