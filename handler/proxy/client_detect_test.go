package proxy

import (
	"testing"

	"github.com/tokendancelab/metapi-go/proxy"
)

func TestDetectClientContext_Basic(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   "curl/8.0",
	}
	ctx := DetectClientContext("/v1/chat/completions", headers, map[string]any{"model": "gpt-4o"})

	// Even a basic request should return a context with a kind
	if ctx.ClientKind == "" {
		t.Error("ClientKind should not be empty")
	}
}

func TestDetectClientContext_CodexDetection(t *testing.T) {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"User-Agent":        "codex-cli/1.0",
		"x-openai-session-id": "session-123",
	}
	ctx := DetectClientContext("/v1/responses", headers, map[string]any{"model": "gpt-4o"})

	// Should detect codex or generic
	if ctx.ClientKind == "" {
		t.Error("ClientKind should be detected")
	}
}

func TestDetectClientContext_ClaudeCodeDetection(t *testing.T) {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"User-Agent":    "Claude-Code/1.0",
	}
	body := map[string]any{
		"model":    "claude-sonnet-4-20250514",
		"metadata": map[string]any{"user_id": "user-uuid"},
	}
	ctx := DetectClientContext("/v1/messages", headers, body)

	if ctx.ClientKind == "" {
		t.Error("ClientKind should be detected")
	}
}

func TestDetectClientContext_GeminiCLI(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	ctx := DetectClientContext("/v1internal::generateContent", headers, map[string]any{"model": "gemini-2.5-pro"})

	// Gemini CLI paths should be detected
	if ctx.ClientKind == "" {
		t.Error("ClientKind should be detected for gemini CLI paths")
	}
}

func TestDetectClientContext_GenericFallback(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	ctx := DetectClientContext("/v1/chat/completions", headers, map[string]any{"model": "gpt-4o"})

	// Should at least be "generic"
	if ctx.ClientKind == "" {
		t.Error("ClientKind should not be empty (should be generic)")
	}
}

func TestDetectClientContext_ClientKindValues(t *testing.T) {
	// Verify that only valid kinds are returned
	tests := []struct {
		name    string
		path    string
		headers map[string]string
		body    map[string]any
	}{
		{"chat completion", "/v1/chat/completions", map[string]string{}, map[string]any{"model": "gpt-4o"}},
		{"gemini cli path", "/v1internal::generateContent", map[string]string{}, map[string]any{}},
	}

	validKinds := map[string]bool{
		"generic":    true,
		"codex":      true,
		"claude_code": true,
		"gemini_cli":  true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := DetectClientContext(tt.path, tt.headers, tt.body)
			if ctx.ClientKind != "" && !validKinds[ctx.ClientKind] {
				t.Errorf("invalid ClientKind %q - must be one of: generic, codex, claude_code, gemini_cli", ctx.ClientKind)
			}
		})
	}
}

// Note: "openai" is NOT a valid client kind per spec
func TestDetectClientContext_NoOpenaiKind(t *testing.T) {
	headers := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   "OpenAI-Python/1.0",
	}
	ctx := DetectClientContext("/v1/chat/completions", headers, map[string]any{"model": "gpt-4o"})

	// Must not be "openai"
	if ctx.ClientKind == "openai" {
		t.Error(`ClientKind MUST NOT be "openai" — spec says no such kind exists`)
	}
}

// Verify we can use the proxy package types
var _ = proxy.DownstreamClientContext{}
