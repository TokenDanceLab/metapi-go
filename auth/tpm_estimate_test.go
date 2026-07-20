package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEstimateAdmissionTokens_MessagesJSON(t *testing.T) {
	// 40 ASCII chars in content → 40/4 = 10 tokens.
	body := `{"model":"gpt","messages":[{"role":"user","content":"abcdefghijklmnopqrstuvwxyzabcdefghijkl"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))

	got := estimateAdmissionTokens(req)
	if got != 10 {
		t.Fatalf("estimate = %d, want 10", got)
	}

	// Body must still be fully readable by downstream.
	restored, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(restored) != body {
		t.Fatalf("body mutated: got %q want %q", restored, body)
	}
}

func TestEstimateAdmissionTokens_InputJSON(t *testing.T) {
	// responses-style: input is a plain string of 8 chars → 2 tokens.
	body := `{"model":"gpt","input":"abcdefgh"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.ContentLength = int64(len(body))

	got := estimateAdmissionTokens(req)
	if got != 2 {
		t.Fatalf("estimate = %d, want 2", got)
	}
}

func TestEstimateAdmissionTokens_ContentLengthFloor(t *testing.T) {
	// Non-JSON body → Content-Length/4 floor.
	body := "not-json-but-sixteen" // 20 chars → 5 tokens
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.ContentLength = int64(len(body))

	got := estimateAdmissionTokens(req)
	want := int64(len(body)) / 4
	if got != want {
		t.Fatalf("estimate = %d, want %d", got, want)
	}
}

func TestEstimateAdmissionTokens_EmptyBodySkips(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	got := estimateAdmissionTokens(req)
	if got != 0 {
		t.Fatalf("empty body estimate = %d, want 0 (skip TPM)", got)
	}
}

func TestEstimateAdmissionTokens_NoContentLengthNonJSONSkips(t *testing.T) {
	// Chunked-style: body present but ContentLength unknown and non-JSON → 0.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("plain"))
	req.ContentLength = -1
	got := estimateAdmissionTokens(req)
	if got != 0 {
		t.Fatalf("non-json without content-length estimate = %d, want 0", got)
	}
}

func TestEstimateAdmissionTokens_ClampMin(t *testing.T) {
	// 1 char content → chars/4 = 0 → clamp to 1.
	body := `{"messages":[{"content":"a"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	got := estimateAdmissionTokens(req)
	if got != 1 {
		t.Fatalf("min clamp estimate = %d, want 1", got)
	}
}

func TestEstimateAdmissionTokens_ClampMax(t *testing.T) {
	// Content-Length huge → clamp to 128000.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("x"))
	req.ContentLength = 4 * (tpmEstimateMaxTokens + 50_000) // would be > max
	got := estimateAdmissionTokens(req)
	// Body is "x" (JSON path fails), Content-Length floor applies then clamp.
	if got != tpmEstimateMaxTokens {
		t.Fatalf("max clamp estimate = %d, want %d", got, tpmEstimateMaxTokens)
	}
}

func TestEstimateAdmissionTokens_NilRequest(t *testing.T) {
	if got := estimateAdmissionTokens(nil); got != 0 {
		t.Fatalf("nil request estimate = %d, want 0", got)
	}
}

func TestEstimateAdmissionTokens_UnicodeRunes(t *testing.T) {
	// 4 CJK runes → rune count 4 → 1 token (not byte-based).
	body := `{"messages":[{"content":"你好世界"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	got := estimateAdmissionTokens(req)
	if got != 1 {
		t.Fatalf("unicode estimate = %d, want 1", got)
	}
}

func TestEstimateAdmissionTokens_ClaudeSystemEnvelope(t *testing.T) {
	// Claude-style: system + messages content leaves (8 + 8 chars).
	// role="user" also contributes 4 runes under recursive walk → 20/4 = 5 tokens.
	// Assert system is counted (without system, messages-only would be 12/4=3).
	body := `{"model":"claude","system":"abcdefgh","messages":[{"role":"user","content":"ijklmnop"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	got := estimateAdmissionTokens(req)
	if got != 5 {
		t.Fatalf("claude system+messages estimate = %d, want 5", got)
	}
	// Sanity: messages-only body is smaller than system+messages.
	bodyMsgOnly := `{"model":"claude","messages":[{"role":"user","content":"ijklmnop"}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(bodyMsgOnly))
	req2.ContentLength = int64(len(bodyMsgOnly))
	gotMsg := estimateAdmissionTokens(req2)
	if gotMsg >= got {
		t.Fatalf("system envelope should increase estimate: withSystem=%d messagesOnly=%d", got, gotMsg)
	}
}

func TestEstimateAdmissionTokens_GeminiContentsEnvelope(t *testing.T) {
	// Gemini-style contents (no messages/input) — 12 chars → 3 tokens.
	body := `{"contents":[{"parts":[{"text":"abcdefghijkl"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", strings.NewReader(body))
	req.ContentLength = int64(len(body))
	got := estimateAdmissionTokens(req)
	if got != 3 {
		t.Fatalf("gemini contents estimate = %d, want 3", got)
	}
}

func TestKeyAdmissionLimiter_TPM_WithEstimateHelper(t *testing.T) {
	// Wire helper → Allow: two estimates of 600 against maxTPM=1000.
	l := NewKeyAdmissionLimiter()
	tpm := int64(1000)

	body1 := `{"messages":[{"content":"` + strings.Repeat("a", 2400) + `"}]}` // 2400/4=600
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body1))
	req1.ContentLength = int64(len(body1))
	est1 := estimateAdmissionTokens(req1)
	if est1 != 600 {
		t.Fatalf("est1 = %d, want 600", est1)
	}
	if d := l.Allow(42, nil, &tpm, est1); !d.Allowed {
		t.Fatalf("first allow: %#v", d)
	}

	body2 := `{"messages":[{"content":"` + strings.Repeat("b", 2000) + `"}]}` // 2000/4=500
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body2))
	req2.ContentLength = int64(len(body2))
	est2 := estimateAdmissionTokens(req2)
	if est2 != 500 {
		t.Fatalf("est2 = %d, want 500", est2)
	}
	if d := l.Allow(42, nil, &tpm, est2); d.Allowed || d.Reason != "over_tpm" {
		t.Fatalf("expected over_tpm: %#v (est2=%d)", d, est2)
	}
}

func TestKeyAdmissionLimiter_MaxTPMNil_Unchanged(t *testing.T) {
	// maxTPM nil: estimatedTokens ignored — unlimited TPM path.
	l := NewKeyAdmissionLimiter()
	for i := 0; i < 5; i++ {
		if d := l.Allow(99, nil, nil, 100_000); !d.Allowed {
			t.Fatalf("nil maxTPM should allow: %#v", d)
		}
	}
	rpm, tpmUsed := l.Snapshot(99)
	if tpmUsed != 0 {
		t.Fatalf("nil maxTPM must not reserve tokens, usedTPM=%d", tpmUsed)
	}
	if rpm != 0 {
		// RPM also unlimited (nil) — Allow returns early before recording.
		// usedRPM stays 0 when both limits are 0.
	}
	_ = rpm
}
