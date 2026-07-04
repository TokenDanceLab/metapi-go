package profiles

import (
	"strings"

	"github.com/tokendancelab/metapi-go/proxy/types"
)

func codexProfile() *types.CliProfileDefinition {
	return &types.CliProfileDefinition{
		ID: types.ProfileCodex,
		Capabilities: types.CliProfileCapabilities{
			SupportsResponsesCompact:             true,
			SupportsResponsesWebsocketIncremental: true,
			PreservesContinuation:                true,
			SupportsCountTokens:                  false,
			EchoesTurnState:                      true,
		},
		Detect: detectCodex,
	}
}

func isCodexPath(path string) bool {
	normalized := strings.ToLower(strings.TrimSpace(path))
	return normalized == "/v1/responses" ||
		strings.HasPrefix(normalized, "/v1/responses/") ||
		normalized == "/v1/chat/completions"
}

func getCodexSessionID(headers map[string]string) string {
	if v := getHeaderCI(headers, "session_id"); v != "" {
		return v
	}
	if v := getHeaderCI(headers, "session-id"); v != "" {
		return v
	}
	if v := getHeaderCI(headers, "conversation_id"); v != "" {
		return v
	}
	if v := getHeaderCI(headers, "conversation-id"); v != "" {
		return v
	}
	return ""
}

func hasHeaderPrefixCI(headers map[string]string, prefix string) bool {
	lower := strings.ToLower(prefix)
	for k, v := range headers {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(k)), lower) && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func isCodexOfficialClientHeaders(headers map[string]string) bool {
	ua := strings.ToLower(getHeaderCI(headers, "user-agent"))
	return strings.Contains(ua, "codex")
}

func isCodexRequest(input types.DetectInput) bool {
	if !isCodexPath(input.DownstreamPath) {
		return false
	}
	if input.Headers == nil {
		return false
	}
	if isCodexOfficialClientHeaders(input.Headers) {
		return true
	}
	if getHeaderCI(input.Headers, "openai-beta") != "" {
		return true
	}
	if hasHeaderPrefixCI(input.Headers, "x-stainless-") {
		return true
	}
	if getCodexSessionID(input.Headers) != "" {
		return true
	}
	if getHeaderCI(input.Headers, "x-codex-turn-state") != "" {
		return true
	}
	return false
}

func detectCodex(input types.DetectInput) *types.DetectedProfile {
	if !isCodexRequest(input) {
		return nil
	}

	sessionID := getCodexSessionID(input.Headers)

	var clientAppID, clientAppName, confidence string
	if isCodexOfficialClientHeaders(input.Headers) {
		clientAppID = "codex"
		clientAppName = "Codex"
		confidence = "exact"
	} else {
		clientAppID = "codex"
		clientAppName = "Codex"
		confidence = "heuristic"
	}

	result := &types.DetectedProfile{
		ID:               types.ProfileCodex,
		ClientAppID:      clientAppID,
		ClientAppName:    clientAppName,
		ClientConfidence: confidence,
	}
	if sessionID != "" {
		result.SessionID = sessionID
		result.TraceHint = sessionID
	}
	return result
}
