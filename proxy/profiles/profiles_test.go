package profiles

import (
	"testing"

	"github.com/tokendancelab/metapi-go/proxy/types"
)

// --- Claude Code detection tests ---

func TestDetectClaudeCode_ValidSessionID(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/messages",
		Headers: map[string]string{
			"user-agent":        "claude-cli/1.2.3",
			"anthropic-beta":    "messages-2023-12-15",
			"anthropic-version": "2023-06-01",
			"x-app":             "cli",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-20250514",
			"metadata": map[string]any{
				"user_id": "user_" + repeat('a', 64) + "_account__session_" +
					"12345678-1234-1234-1234-123456789abc",
			},
		},
	}

	result := detectClaudeCode(input)
	if result == nil {
		t.Fatal("expected detection result, got nil")
	}
	if result.ID != types.ProfileClaudeCode {
		t.Errorf("expected ID %q, got %q", types.ProfileClaudeCode, result.ID)
	}
	if result.ClientAppID != "claude_code" {
		t.Errorf("expected ClientAppID %q, got %q", "claude_code", result.ClientAppID)
	}
	if result.ClientConfidence != "exact" {
		t.Errorf("expected ClientConfidence %q, got %q", "exact", result.ClientConfidence)
	}
	if result.SessionID == "" {
		t.Error("expected non-empty SessionID")
	}
	if result.TraceHint != result.SessionID {
		t.Errorf("expected TraceHint %q to match SessionID %q", result.TraceHint, result.SessionID)
	}
}

func TestDetectClaudeCode_WrongPath(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/chat/completions",
		Headers: map[string]string{
			"user-agent":        "claude-cli/1.2.3",
			"anthropic-beta":    "messages-2023-12-15",
			"anthropic-version": "2023-06-01",
			"x-app":             "cli",
		},
	}

	result := detectClaudeCode(input)
	if result != nil {
		t.Errorf("expected nil for non-Claude path, got %+v", result)
	}
}

func TestDetectClaudeCode_HeaderFingerprintFallback(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/messages",
		Headers: map[string]string{
			"user-agent":        "claude-cli/2.0.0",
			"anthropic-beta":    "messages-2023-12-15",
			"anthropic-version": "2023-06-01",
			"x-app":             "cli",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-20250514",
		},
	}

	result := detectClaudeCode(input)
	if result == nil {
		t.Fatal("expected detection via header fingerprint, got nil")
	}
	if result.ID != types.ProfileClaudeCode {
		t.Errorf("expected ID %q, got %q", types.ProfileClaudeCode, result.ID)
	}
	if result.SessionID != "" {
		t.Errorf("expected empty SessionID without valid user_id, got %q", result.SessionID)
	}
}

func TestDetectClaudeCode_CountTokensPath(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/messages/count_tokens",
		Headers: map[string]string{
			"user-agent":        "claude-cli/1.2.3",
			"anthropic-beta":    "token-counting-2024-11-01",
			"anthropic-version": "2023-06-01",
			"x-app":             "cli",
		},
		Body: map[string]any{
			"model": "claude-sonnet-4-20250514",
			"metadata": map[string]any{
				"user_id": "user_" + repeat('a', 64) + "_account__session_" +
					"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			},
		},
	}

	result := detectClaudeCode(input)
	if result == nil {
		t.Fatal("expected detection on count_tokens path, got nil")
	}
	if result.ID != types.ProfileClaudeCode {
		t.Errorf("expected ID %q, got %q", types.ProfileClaudeCode, result.ID)
	}
}

func TestDetectClaudeCode_EmptyHeadersNoMatch(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/messages",
		Headers:        map[string]string{},
	}

	result := detectClaudeCode(input)
	if result != nil {
		t.Errorf("expected nil with empty headers and no body, got %+v", result)
	}
}

// --- Codex detection tests ---

func TestDetectCodex_OfficialClient(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/responses",
		Headers: map[string]string{
			"user-agent":  "codex-cli/1.0.0",
			"session_id":  "sess-abc-123",
			"openai-beta": "assistants=v2",
		},
	}

	result := detectCodex(input)
	if result == nil {
		t.Fatal("expected detection result, got nil")
	}
	if result.ID != types.ProfileCodex {
		t.Errorf("expected ID %q, got %q", types.ProfileCodex, result.ID)
	}
	if result.ClientConfidence != "exact" {
		t.Errorf("expected ClientConfidence %q, got %q", "exact", result.ClientConfidence)
	}
	if result.ClientAppName != "Codex" {
		t.Errorf("expected ClientAppName %q, got %q", "Codex", result.ClientAppName)
	}
	if result.SessionID != "sess-abc-123" {
		t.Errorf("expected SessionID %q, got %q", "sess-abc-123", result.SessionID)
	}
}

func TestDetectCodex_Heuristic(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/responses",
		Headers: map[string]string{
			"user-agent":        "some-other-client/1.0",
			"x-stainless-retry": "3",
		},
	}

	result := detectCodex(input)
	if result == nil {
		t.Fatal("expected heuristic detection, got nil")
	}
	if result.ClientConfidence != "heuristic" {
		t.Errorf("expected ClientConfidence %q, got %q", "heuristic", result.ClientConfidence)
	}
}

func TestDetectCodex_WrongPath(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/messages",
		Headers: map[string]string{
			"user-agent": "codex-cli/1.0.0",
		},
	}

	result := detectCodex(input)
	if result != nil {
		t.Errorf("expected nil for non-Codex path, got %+v", result)
	}
}

func TestDetectCodex_AlternateSessionHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{"session-id header", map[string]string{"user-agent": "codex-cli/1.0.0", "session-id": "sess-dash"}},
		{"conversation_id header", map[string]string{"user-agent": "codex-cli/1.0.0", "conversation_id": "conv-456"}},
		{"conversation-id header", map[string]string{"user-agent": "codex-cli/1.0.0", "conversation-id": "conv-dash"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := types.DetectInput{
				DownstreamPath: "/v1/responses",
				Headers:        tt.headers,
			}
			result := detectCodex(input)
			if result == nil {
				t.Fatal("expected detection, got nil")
			}
			if result.SessionID == "" {
				t.Error("expected non-empty SessionID")
			}
		})
	}
}

func TestDetectCodex_NilHeaders(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/responses",
		Headers:        nil,
	}

	result := detectCodex(input)
	if result != nil {
		t.Errorf("expected nil with nil headers, got %+v", result)
	}
}

// --- Gemini CLI detection tests ---

func TestDetectGeminiCli_Valid(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1internal:generatecontent",
		Body: map[string]any{
			"model":    "gemini-2.5-pro",
			"contents": []any{},
		},
	}

	result := detectGeminiCli(input)
	if result == nil {
		t.Fatal("expected detection result, got nil")
	}
	if result.ID != types.ProfileGeminiCli {
		t.Errorf("expected ID %q, got %q", types.ProfileGeminiCli, result.ID)
	}
	if result.ClientAppID != "gemini_cli" {
		t.Errorf("expected ClientAppID %q, got %q", "gemini_cli", result.ClientAppID)
	}
	if result.ClientConfidence != "exact" {
		t.Errorf("expected ClientConfidence %q, got %q", "exact", result.ClientConfidence)
	}
}

func TestDetectGeminiCli_WrongPath(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1/responses",
		Body: map[string]any{
			"model": "gemini-2.5-pro",
		},
	}

	result := detectGeminiCli(input)
	if result != nil {
		t.Errorf("expected nil for non-Gemini path, got %+v", result)
	}
}

func TestDetectGeminiCli_BadBodyShape(t *testing.T) {
	input := types.DetectInput{
		DownstreamPath: "/v1internal:generatecontent",
		Body: map[string]any{
			"unrelated": "value",
		},
	}

	result := detectGeminiCli(input)
	if result != nil {
		t.Errorf("expected nil for bad body shape, got %+v", result)
	}
}

func TestDetectGeminiCli_NilBody(t *testing.T) {
	// When body is nil the check is: input.Body != nil && !hasGeminiCliBodyShape
	// which evaluates to false, so detection proceeds and succeeds on path alone.
	input := types.DetectInput{
		DownstreamPath: "/v1internal:generatecontent",
		Body:           nil,
	}

	result := detectGeminiCli(input)
	if result == nil {
		t.Fatal("expected detection on valid Gemini path with nil body")
	}
	if result.ID != types.ProfileGeminiCli {
		t.Errorf("expected ID %q, got %q", types.ProfileGeminiCli, result.ID)
	}
}

// --- Registry tests ---

func TestAll_ReturnsAllProfiles(t *testing.T) {
	profiles := All()

	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profiles))
	}

	ids := make(map[types.CliProfileID]bool)
	for _, p := range profiles {
		if p.Detect == nil {
			t.Errorf("profile %q has nil Detect function", p.ID)
		}
		ids[p.ID] = true
	}

	expected := []types.CliProfileID{types.ProfileClaudeCode, types.ProfileCodex, types.ProfileGeminiCli}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("missing profile %q in All()", id)
		}
	}
}

func TestGeneric_ReturnsGenericProfile(t *testing.T) {
	generic := Generic()
	if generic == nil {
		t.Fatal("expected non-nil Generic profile")
	}
	if generic.ID != types.ProfileGeneric {
		t.Errorf("expected ID %q, got %q", types.ProfileGeneric, generic.ID)
	}
	if generic.Detect == nil {
		t.Fatal("generic profile has nil Detect function")
	}

	// Verify the generic detector always returns a result
	result := generic.Detect(types.DetectInput{})
	if result == nil {
		t.Fatal("generic detector returned nil")
	}
	if result.ID != types.ProfileGeneric {
		t.Errorf("expected generic detector to return ID %q, got %q", types.ProfileGeneric, result.ID)
	}
}

func TestGeneric_Capabilities(t *testing.T) {
	generic := Generic()
	caps := generic.Capabilities

	if caps.SupportsResponsesCompact {
		t.Error("generic should not support ResponsesCompact")
	}
	if caps.SupportsResponsesWebsocketIncremental {
		t.Error("generic should not support ResponsesWebsocketIncremental")
	}
	if caps.PreservesContinuation {
		t.Error("generic should not preserve continuation")
	}
	if caps.SupportsCountTokens {
		t.Error("generic should not support count tokens")
	}
	if caps.EchoesTurnState {
		t.Error("generic should not echo turn state")
	}
}

// helper: repeat a byte n times as a string
func repeat(b byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return string(buf)
}
