package responses

import (
	"strings"
	"testing"
)

func TestSupportsResponsesPreviousResponseID(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{"openai", true},
		{"OpenAI", true},
		{"  azure-openai  ", true},
		{"azure", true},
		{"codex", true},
		{"openai-responses", true},
		{"sub2api", false},
		{"newapi", false},
		{"anyrouter", false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			if got := SupportsResponsesPreviousResponseID(tt.platform); got != tt.want {
				t.Fatalf("SupportsResponsesPreviousResponseID(%q) = %v, want %v", tt.platform, got, tt.want)
			}
		})
	}
}

func TestNormalizeUpstreamProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol UpstreamProtocol
		path     string
		want     UpstreamProtocol
	}{
		{"explicit chat", ProtocolChat, "", ProtocolChat},
		{"explicit responses", ProtocolResponses, "/v1/chat/completions", ProtocolResponses},
		{"path responses", "", "/v1/responses", ProtocolResponses},
		{"path compact", "", "/v1/responses/compact", ProtocolResponses},
		{"path chat", "", "/v1/chat/completions", ProtocolChat},
		{"path messages", "", "/v1/messages", ProtocolMessages},
		{"unknown", "", "/v1/embeddings", ProtocolUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeUpstreamProtocol(tt.protocol, tt.path); got != tt.want {
				t.Fatalf("NormalizeUpstreamProtocol(%q, %q) = %q, want %q", tt.protocol, tt.path, got, tt.want)
			}
		})
	}
}

func TestResolvePreviousResponseIDPolicy(t *testing.T) {
	tests := []struct {
		name   string
		input  ContinuityPolicyInput
		action ContinuityAction
		reason string
	}{
		{
			name: "openai responses forwards",
			input: ContinuityPolicyInput{
				SitePlatform: "openai",
				Protocol:     ProtocolResponses,
			},
			action: ContinuityForward,
			reason: "responses_platform_supports_previous_response_id",
		},
		{
			name: "codex responses forwards",
			input: ContinuityPolicyInput{
				SitePlatform: "codex",
				Protocol:     ProtocolResponses,
			},
			action: ContinuityForward,
			reason: "responses_platform_supports_previous_response_id",
		},
		{
			name: "sub2api responses strips",
			input: ContinuityPolicyInput{
				SitePlatform: "sub2api",
				Protocol:     ProtocolResponses,
			},
			action: ContinuityStrip,
			reason: "responses_platform_strips_unsupported_previous_response_id",
		},
		{
			name: "chat strips",
			input: ContinuityPolicyInput{
				SitePlatform: "openai",
				Protocol:     ProtocolChat,
			},
			action: ContinuityStrip,
			reason: "chat_family_strips_previous_response_id",
		},
		{
			name: "messages strips",
			input: ContinuityPolicyInput{
				SitePlatform: "openai",
				Protocol:     ProtocolMessages,
			},
			action: ContinuityStrip,
			reason: "chat_family_strips_previous_response_id",
		},
		{
			name: "compact strips even on openai",
			input: ContinuityPolicyInput{
				SitePlatform:     "openai",
				Protocol:         ProtocolResponses,
				IsCompactRequest: true,
			},
			action: ContinuityStrip,
			reason: "compact_request_strips_previous_response_id",
		},
		{
			name: "compact path strips",
			input: ContinuityPolicyInput{
				SitePlatform: "codex",
				UpstreamPath: "/v1/responses/compact",
			},
			action: ContinuityStrip,
			reason: "compact_request_strips_previous_response_id",
		},
		{
			name: "chat require continuity rejects",
			input: ContinuityPolicyInput{
				SitePlatform:      "openai",
				Protocol:          ProtocolChat,
				RequireContinuity: true,
			},
			action: ContinuityReject,
			reason: "chat_family_lacks_previous_response_id",
		},
		{
			name: "unsupported platform require continuity rejects",
			input: ContinuityPolicyInput{
				SitePlatform:      "sub2api",
				Protocol:          ProtocolResponses,
				RequireContinuity: true,
			},
			action: ContinuityReject,
			reason: "responses_platform_rejects_previous_response_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePreviousResponseIDPolicy(tt.input)
			if got.Action != tt.action {
				t.Fatalf("action = %q, want %q (decision=%+v)", got.Action, tt.action, got)
			}
			if got.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.reason)
			}
			if got.Action == ContinuityReject && strings.TrimSpace(got.ClientMessage) == "" {
				t.Fatal("reject decision missing ClientMessage")
			}
		})
	}
}

func TestApplyPreviousResponseIDPolicy_PresenceAbsenceStrip(t *testing.T) {
	t.Run("absent field is no-op", func(t *testing.T) {
		body := map[string]any{"model": "gpt-4", "input": "hi"}
		next, decision, err := ApplyPreviousResponseIDPolicy(body, ContinuityPolicyInput{
			SitePlatform: "openai",
			Protocol:     ProtocolResponses,
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if decision.Action != ContinuityForward {
			t.Fatalf("action = %q", decision.Action)
		}
		if _, ok := next[PreviousResponseIDField]; ok {
			t.Fatal("did not expect previous_response_id key")
		}
		if next["model"] != "gpt-4" {
			t.Fatalf("model = %v", next["model"])
		}
		// original not mutated
		if _, ok := body[PreviousResponseIDField]; ok {
			t.Fatal("original body mutated")
		}
	})

	t.Run("forward preserves id", func(t *testing.T) {
		body := map[string]any{"model": "gpt-4", "previous_response_id": "  resp_abc  "}
		next, decision, err := ApplyPreviousResponseIDPolicy(body, ContinuityPolicyInput{
			SitePlatform: "openai",
			Protocol:     ProtocolResponses,
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if decision.Action != ContinuityForward {
			t.Fatalf("action = %q", decision.Action)
		}
		if next[PreviousResponseIDField] != "resp_abc" {
			t.Fatalf("previous_response_id = %v", next[PreviousResponseIDField])
		}
	})

	t.Run("chat strips id", func(t *testing.T) {
		body := map[string]any{"model": "gpt-4", "previous_response_id": "resp_abc"}
		next, decision, err := ApplyPreviousResponseIDPolicy(body, ContinuityPolicyInput{
			SitePlatform: "openai",
			Protocol:     ProtocolChat,
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if decision.Action != ContinuityStrip {
			t.Fatalf("action = %q", decision.Action)
		}
		if _, ok := next[PreviousResponseIDField]; ok {
			t.Fatal("expected previous_response_id stripped")
		}
		if body[PreviousResponseIDField] != "resp_abc" {
			t.Fatal("original body should keep field")
		}
	})

	t.Run("sub2api responses strips id", func(t *testing.T) {
		body := map[string]any{"model": "gpt-4", "previous_response_id": "resp_abc"}
		next, decision, err := ApplyPreviousResponseIDPolicy(body, ContinuityPolicyInput{
			SitePlatform: "sub2api",
			Protocol:     ProtocolResponses,
			UpstreamPath: "/v1/responses",
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if decision.Action != ContinuityStrip {
			t.Fatalf("action = %q", decision.Action)
		}
		if _, ok := next[PreviousResponseIDField]; ok {
			t.Fatal("expected previous_response_id stripped")
		}
	})

	t.Run("reject returns clear error", func(t *testing.T) {
		body := map[string]any{"model": "gpt-4", "previous_response_id": "resp_abc"}
		_, decision, err := ApplyPreviousResponseIDPolicy(body, ContinuityPolicyInput{
			SitePlatform:      "sub2api",
			Protocol:          ProtocolResponses,
			RequireContinuity: true,
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if decision.Action != ContinuityReject {
			t.Fatalf("action = %q", decision.Action)
		}
		msg := err.Error()
		if !strings.Contains(msg, "previous_response_id") {
			t.Fatalf("error should mention previous_response_id: %q", msg)
		}
		if strings.Contains(strings.ToLower(msg), "unsupported parameter") && !strings.Contains(msg, "platform") {
			t.Fatalf("error looks like opaque upstream 400: %q", msg)
		}
	})

	t.Run("whitespace-only id treated as absent on forward", func(t *testing.T) {
		body := map[string]any{"model": "gpt-4", "previous_response_id": "   "}
		next, _, err := ApplyPreviousResponseIDPolicy(body, ContinuityPolicyInput{
			SitePlatform: "openai",
			Protocol:     ProtocolResponses,
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if _, ok := next[PreviousResponseIDField]; ok {
			t.Fatal("whitespace-only id should be deleted")
		}
	})
}

func TestSanitizeResponsesRequestBody(t *testing.T) {
	t.Run("openai responses keeps continuity", func(t *testing.T) {
		body := map[string]any{
			"model":                "gpt-4",
			"previous_response_id": "resp_1",
			"input":                "hi",
		}
		next, decision, err := SanitizeResponsesRequestBody(body, ContinuityPolicyInput{
			SitePlatform: "openai",
			Protocol:     ProtocolResponses,
			UpstreamPath: "/v1/responses",
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if decision.Action != ContinuityForward {
			t.Fatalf("action = %q", decision.Action)
		}
		if next["previous_response_id"] != "resp_1" {
			t.Fatalf("id = %v", next["previous_response_id"])
		}
	})

	t.Run("chat fallback strips continuity", func(t *testing.T) {
		body := map[string]any{
			"model":                "gpt-4",
			"previous_response_id": "resp_1",
			"input":                "hi",
		}
		next, decision, err := SanitizeResponsesRequestBody(body, ContinuityPolicyInput{
			SitePlatform: "openai",
			Protocol:     ProtocolChat,
			UpstreamPath: "/v1/chat/completions",
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if decision.Action != ContinuityStrip {
			t.Fatalf("action = %q", decision.Action)
		}
		if _, ok := next["previous_response_id"]; ok {
			t.Fatal("expected strip")
		}
	})

	t.Run("compact also strips stream fields", func(t *testing.T) {
		body := map[string]any{
			"model":                "gpt-4",
			"stream":               true,
			"stream_options":       map[string]any{"include_usage": true},
			"store":                true,
			"previous_response_id": "resp_1",
		}
		next, decision, err := SanitizeResponsesRequestBody(body, ContinuityPolicyInput{
			SitePlatform:     "codex",
			Protocol:         ProtocolResponses,
			IsCompactRequest: true,
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if decision.Action != ContinuityStrip {
			t.Fatalf("action = %q", decision.Action)
		}
		for _, k := range []string{"stream", "stream_options", "store", "previous_response_id"} {
			if _, ok := next[k]; ok {
				t.Fatalf("expected %q stripped", k)
			}
		}
		if next["model"] != "gpt-4" {
			t.Fatalf("model = %v", next["model"])
		}
	})
}

func TestIsUnsupportedPreviousResponseIDError(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{`{"detail":"Unsupported parameter: previous_response_id"}`, true},
		{`unknown parameter: 'previous_response_id'`, true},
		{`previous_response_id is not supported`, true},
		{`bad request`, false},
		{`unsupported parameter: stream`, false},
		{``, false},
	}
	for _, tt := range tests {
		if got := IsUnsupportedPreviousResponseIDError(tt.text); got != tt.want {
			t.Errorf("IsUnsupportedPreviousResponseIDError(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestHasAndStripPreviousResponseID(t *testing.T) {
	if HasPreviousResponseID(nil) {
		t.Fatal("nil body")
	}
	if HasPreviousResponseID(map[string]any{"previous_response_id": "  "}) {
		t.Fatal("whitespace-only should be absent")
	}
	body := map[string]any{"previous_response_id": "resp_1", "model": "x"}
	if !HasPreviousResponseID(body) {
		t.Fatal("expected present")
	}
	stripped := StripPreviousResponseID(body)
	if HasPreviousResponseID(stripped) {
		t.Fatal("expected stripped")
	}
	if body["previous_response_id"] != "resp_1" {
		t.Fatal("strip must copy")
	}
}
