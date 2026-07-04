package proxy

import (
	"testing"

	"github.com/tokendancelab/metapi-go/proxy/types"
)

func TestDetectCliProfile_ClaudeCode(t *testing.T) {
	t.Run("claude_code detected by headers", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers: map[string]string{
				"user-agent":       "claude-cli/1.2.3",
				"anthropic-beta":   "messages-2023-12-15",
				"anthropic-version": "2023-06-01",
				"x-app":            "cli",
			},
		})
		if profile.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code, got %s", profile.ID)
		}
		if profile.ClientAppID != "claude_code" {
			t.Errorf("expected clientAppID=claude_code, got %s", profile.ClientAppID)
		}
		if profile.ClientConfidence != "exact" {
			t.Errorf("expected exact confidence, got %s", profile.ClientConfidence)
		}
	})

	t.Run("claude_code detected by body metadata.user_id", func(t *testing.T) {
		// Use a valid hex user ID that matches the regex: user_[0-9a-f]{64}_account__session_[UUID]
		uid := "user_0000000000000000000000000000000000000000000000000000000000000001_account__session_12345678-1234-1234-1234-123456789abc"
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers:        map[string]string{},
			Body: map[string]any{
				"metadata": map[string]any{
					"user_id": uid,
				},
			},
		})
		if profile.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code, got %s", profile.ID)
		}
		if profile.SessionID != "12345678-1234-1234-1234-123456789abc" {
			t.Errorf("expected session extracted, got %q", profile.SessionID)
		}
	})

	t.Run("claude_code on /v1/messages/count_tokens path", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages/count_tokens",
			Headers: map[string]string{
				"user-agent":       "claude-cli/2.0.0",
				"anthropic-beta":   "messages-2023-12-15",
				"anthropic-version": "2023-06-01",
				"x-app":            "cli",
			},
		})
		if profile.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code, got %s", profile.ID)
		}
	})

	t.Run("claude_code on /anthropic/v1/messages path", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/anthropic/v1/messages",
			Headers: map[string]string{
				"user-agent":       "claude-cli/1.0.0",
				"anthropic-beta":   "messages-2023-12-15",
				"anthropic-version": "2023-06-01",
				"x-app":            "cli",
			},
		})
		if profile.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code, got %s", profile.ID)
		}
	})

	t.Run("claude_code not detected on non-claude path", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/chat/completions",
			Headers: map[string]string{
				"user-agent":       "claude-cli/1.2.3",
				"anthropic-beta":   "messages-2023-12-15",
				"anthropic-version": "2023-06-01",
				"x-app":            "cli",
			},
		})
		if profile.ID == types.ProfileClaudeCode {
			t.Errorf("expected NOT claude_code on /v1/chat/completions path")
		}
	})

	t.Run("claude_code not detected without full header fingerprint", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers: map[string]string{
				"user-agent": "claude-cli/1.2.3",
			},
		})
		if profile.ID == types.ProfileClaudeCode {
			t.Errorf("expected NOT claude_code with partial headers")
		}
	})

	t.Run("claude_code not detected without x-app: cli", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers: map[string]string{
				"user-agent":       "claude-cli/1.2.3",
				"anthropic-beta":   "messages-2023-12-15",
				"anthropic-version": "2023-06-01",
			},
		})
		if profile.ID == types.ProfileClaudeCode {
			t.Errorf("expected NOT claude_code without x-app:cli header")
		}
	})

	t.Run("claude_code detected by body on messages path with session extraction", func(t *testing.T) {
		uid := "user_0000000000000000000000000000000000000000000000000000000000000002_account__session_a1b2c3d4-e5f6-a7b8-c9d0-e1f2a3b4c5d6"
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers:        map[string]string{},
			Body: map[string]any{
				"metadata": map[string]any{
					"user_id": uid,
				},
			},
		})
		if profile.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code from body session, got %s", profile.ID)
		}
		if profile.TraceHint != "a1b2c3d4-e5f6-a7b8-c9d0-e1f2a3b4c5d6" {
			t.Errorf("expected trace hint from session, got %q", profile.TraceHint)
		}
	})
}

func TestDetectCliProfile_Codex(t *testing.T) {
	t.Run("codex detected on /v1/responses", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"user-agent": "codex/1.0",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex, got %s", profile.ID)
		}
		if profile.ClientConfidence != "exact" {
			t.Errorf("expected exact confidence for official client, got %s", profile.ClientConfidence)
		}
	})

	t.Run("codex detected on /v1/responses/*", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses/cancel",
			Headers: map[string]string{
				"user-agent": "codex/2.0",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex, got %s", profile.ID)
		}
	})

	t.Run("codex detected on /v1/chat/completions with openai-beta header", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/chat/completions",
			Headers: map[string]string{
				"openai-beta": "responses_websockets=2026-02-06",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex via openai-beta header, got %s", profile.ID)
		}
	})

	t.Run("codex detected via x-stainless- headers", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"x-stainless-os":       "macOS",
				"x-stainless-arch":     "arm64",
				"x-stainless-lang":     "js",
				"x-stainless-package-version": "1.0.0",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex via x-stainless headers, got %s", profile.ID)
		}
		if profile.ClientConfidence != "heuristic" {
			t.Errorf("expected heuristic confidence for stainless headers, got %s", profile.ClientConfidence)
		}
	})

	t.Run("codex detected via session_id header", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"session_id": "sess_abc123",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex via session_id header, got %s", profile.ID)
		}
		if profile.SessionID != "sess_abc123" {
			t.Errorf("expected session ID 'sess_abc123', got %q", profile.SessionID)
		}
	})

	t.Run("codex detected via conversation_id header", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"conversation_id": "conv_xyz",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex via conversation_id, got %s", profile.ID)
		}
	})

	t.Run("codex detected via x-codex-turn-state header", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"x-codex-turn-state": "turn_1",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex via turn-state header, got %s", profile.ID)
		}
	})

	t.Run("codex not detected on non-codex path", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers: map[string]string{
				"user-agent": "codex/1.0",
			},
		})
		if profile.ID == types.ProfileCodex {
			t.Errorf("expected NOT codex on non-codex path")
		}
	})

	t.Run("codex session from session-id header", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"session-id": "dash_sess_456",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex via session-id, got %s", profile.ID)
		}
		if profile.SessionID != "dash_sess_456" {
			t.Errorf("expected session from session-id, got %q", profile.SessionID)
		}
	})

	t.Run("codex nil headers returns nil", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers:        nil,
		})
		if profile.ID == types.ProfileCodex {
			t.Errorf("expected NOT codex with nil headers")
		}
	})
}

func TestDetectCliProfile_GeminiCli(t *testing.T) {
	t.Run("gemini_cli detected on /v1internal:generatecontent", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1internal:generatecontent",
			Body: map[string]any{
				"model": "gemini-pro",
			},
		})
		if profile.ID != types.ProfileGeminiCli {
			t.Errorf("expected gemini_cli, got %s", profile.ID)
		}
		if profile.ClientConfidence != "exact" {
			t.Errorf("expected exact confidence, got %s", profile.ClientConfidence)
		}
	})

	t.Run("gemini_cli detected on /v1internal:streamgeneratecontent", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1internal:streamgeneratecontent",
			Body: map[string]any{
				"request": map[string]any{
					"model": "gemini-pro",
				},
			},
		})
		if profile.ID != types.ProfileGeminiCli {
			t.Errorf("expected gemini_cli, got %s", profile.ID)
		}
	})

	t.Run("gemini_cli detected on /v1internal:counttokens", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1internal:counttokens",
			Body: map[string]any{
				"contents": []any{"test"},
			},
		})
		if profile.ID != types.ProfileGeminiCli {
			t.Errorf("expected gemini_cli, got %s", profile.ID)
		}
	})

	t.Run("gemini_cli detected with nil body (treats nil as matching)", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1internal:generatecontent",
			Body:           nil,
		})
		if profile.ID != types.ProfileGeminiCli {
			t.Errorf("expected gemini_cli (nil body treated as matching), got %s", profile.ID)
		}
	})

	t.Run("gemini_cli not detected with non-matching body shape", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1internal:generatecontent",
			Body: map[string]any{
				"other_field": "value",
			},
		})
		if profile.ID == types.ProfileGeminiCli {
			t.Errorf("expected NOT gemini_cli with non-matching body")
		}
	})

	t.Run("gemini_cli not detected on non-gemini path", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/chat/completions",
			Body: map[string]any{
				"model": "gemini-pro",
			},
		})
		if profile.ID == types.ProfileGeminiCli {
			t.Errorf("expected NOT gemini_cli on non-gemini path")
		}
	})
}

func TestDetectCliProfile_Generic(t *testing.T) {
	t.Run("generic is the unconditional fallback", func(t *testing.T) {
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/some/unknown/path",
			Headers:        map[string]string{},
			Body:           nil,
		})
		if profile.ID != types.ProfileGeneric {
			t.Errorf("expected generic fallback, got %s", profile.ID)
		}
		if profile.ClientKind != "generic" {
			t.Errorf("expected clientKind=generic, got %s", profile.ClientKind)
		}
	})
}

func TestDetectCliProfile_PriorityOrdering(t *testing.T) {
	t.Run("claude_code takes priority over codex", func(t *testing.T) {
		// Both claude_code and codex might match /v1/messages, but claude_code has higher priority
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/messages",
			Headers: map[string]string{
				"user-agent":       "claude-cli/1.2.3",
				"anthropic-beta":   "messages-2023-12-15",
				"anthropic-version": "2023-06-01",
				"x-app":            "cli",
			},
		})
		if profile.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code (higher priority), got %s", profile.ID)
		}
	})

	t.Run("codex takes priority over gemini_cli", func(t *testing.T) {
		// codex is registered before gemini_cli
		profile := DetectCliProfile(types.DetectInput{
			DownstreamPath: "/v1/responses",
			Headers: map[string]string{
				"user-agent": "codex/1.0",
			},
		})
		if profile.ID != types.ProfileCodex {
			t.Errorf("expected codex (higher priority), got %s", profile.ID)
		}
	})
}

func TestDetectClientContext(t *testing.T) {
	t.Run("detects claude_code context", func(t *testing.T) {
		ctx := DetectClientContext("/v1/messages", map[string]string{
			"user-agent":       "claude-cli/1.2.3",
			"anthropic-beta":   "messages-2023-12-15",
			"anthropic-version": "2023-06-01",
			"x-app":            "cli",
		}, nil)

		if ctx.ClientKind != "claude_code" {
			t.Errorf("expected claude_code kind, got %q", ctx.ClientKind)
		}
		if ctx.ClientAppName != "Claude Code" {
			t.Errorf("expected 'Claude Code' app name, got %q", ctx.ClientAppName)
		}
		if ctx.ClientConfidence != "exact" {
			t.Errorf("expected exact confidence, got %q", ctx.ClientConfidence)
		}
		if !ctx.Capabilities.SupportsCountTokens {
			t.Error("expected SupportsCountTokens=true for claude_code")
		}
		if !ctx.Capabilities.PreservesContinuation {
			t.Error("expected PreservesContinuation=true for claude_code")
		}
	})

	t.Run("detects codex context", func(t *testing.T) {
		ctx := DetectClientContext("/v1/responses", map[string]string{
			"user-agent": "codex/1.0",
		}, nil)

		if ctx.ClientKind != "codex" {
			t.Errorf("expected codex kind, got %q", ctx.ClientKind)
		}
		if ctx.ClientAppName != "Codex" {
			t.Errorf("expected 'Codex' app name, got %q", ctx.ClientAppName)
		}
		if ctx.ClientConfidence != "exact" {
			t.Errorf("expected exact confidence, got %q", ctx.ClientConfidence)
		}
		if !ctx.Capabilities.SupportsResponsesCompact {
			t.Error("expected SupportsResponsesCompact=true for codex")
		}
		if !ctx.Capabilities.SupportsResponsesWebsocketIncremental {
			t.Error("expected SupportsResponsesWebsocketIncremental=true for codex")
		}
		if !ctx.Capabilities.EchoesTurnState {
			t.Error("expected EchoesTurnState=true for codex")
		}
	})

	t.Run("detects gemini_cli context", func(t *testing.T) {
		ctx := DetectClientContext("/v1internal:generatecontent", nil, map[string]any{
			"model": "gemini-pro",
		})

		if ctx.ClientKind != "gemini_cli" {
			t.Errorf("expected gemini_cli kind, got %q", ctx.ClientKind)
		}
		if ctx.ClientAppName != "Gemini CLI" {
			t.Errorf("expected 'Gemini CLI' app name, got %q", ctx.ClientAppName)
		}
		if !ctx.Capabilities.SupportsCountTokens {
			t.Error("expected SupportsCountTokens=true for gemini_cli")
		}
		if ctx.Capabilities.PreservesContinuation {
			t.Error("expected PreservesContinuation=false for gemini_cli")
		}
	})

	t.Run("detects generic context as fallback", func(t *testing.T) {
		ctx := DetectClientContext("/unknown/path", map[string]string{}, nil)

		if ctx.ClientKind != "generic" {
			t.Errorf("expected generic kind, got %q", ctx.ClientKind)
		}
		if ctx.Capabilities.SupportsCountTokens {
			t.Error("expected SupportsCountTokens=false for generic")
		}
	})
}

func TestGetProfileDefinition(t *testing.T) {
	t.Run("returns registered profile", func(t *testing.T) {
		def := GetProfileDefinition(types.ProfileClaudeCode)
		if def == nil {
			t.Fatal("expected non-nil profile definition")
		}
		if def.ID != types.ProfileClaudeCode {
			t.Errorf("expected claude_code, got %s", def.ID)
		}
	})

	t.Run("returns nil for unregistered profile", func(t *testing.T) {
		def := GetProfileDefinition("nonexistent")
		if def != nil {
			t.Error("expected nil for unregistered profile")
		}
	})
}
