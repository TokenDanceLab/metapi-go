package profiles

import (
	"regexp"
	"strings"

	"github.com/tokendancelab/metapi-go/proxy/types"
)

var claudeCodeUserIDPattern = regexp.MustCompile(`^user_[0-9a-f]{64}_account__session_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var claudeCodeUserAgentPattern = regexp.MustCompile(`^claude-cli/\d+\.\d+\.\d+`)

func claudeCodeProfile() *types.CliProfileDefinition {
	return &types.CliProfileDefinition{
		ID: types.ProfileClaudeCode,
		Capabilities: types.CliProfileCapabilities{
			SupportsResponsesCompact:             false,
			SupportsResponsesWebsocketIncremental: false,
			PreservesContinuation:                true,
			SupportsCountTokens:                  true,
			EchoesTurnState:                      false,
		},
		Detect: detectClaudeCode,
	}
}

func isClaudeSurface(path string) bool {
	normalized := strings.ToLower(strings.TrimSpace(path))
	return normalized == "/v1/messages" ||
		normalized == "/anthropic/v1/messages" ||
		normalized == "/v1/messages/count_tokens"
}

func getHeaderCI(headers map[string]string, key string) string {
	lowerKey := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(strings.TrimSpace(k)) == lowerKey {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func hasClaudeCodeHeaderFingerprint(headers map[string]string) bool {
	ua := getHeaderCI(headers, "user-agent")
	if !claudeCodeUserAgentPattern.MatchString(ua) {
		return false
	}
	if getHeaderCI(headers, "anthropic-beta") == "" {
		return false
	}
	if getHeaderCI(headers, "anthropic-version") == "" {
		return false
	}
	return strings.ToLower(getHeaderCI(headers, "x-app")) == "cli"
}

func extractClaudeCodeSessionID(userID string) string {
	trimmed := strings.TrimSpace(userID)
	if !claudeCodeUserIDPattern.MatchString(trimmed) {
		return ""
	}
	idx := strings.LastIndex(trimmed, "__session_")
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(trimmed[idx+len("__session_"):])
}

func isRecord(v any) bool {
	if v == nil {
		return false
	}
	_, ok := v.(map[string]any)
	return ok
}

func detectClaudeCode(input types.DetectInput) *types.DetectedProfile {
	if !isClaudeSurface(input.DownstreamPath) {
		return nil
	}

	bodyMap, _ := input.Body.(map[string]any)
	var userID string
	if bodyMap != nil {
		if meta, ok := bodyMap["metadata"].(map[string]any); ok {
			if uid, ok := meta["user_id"].(string); ok {
				userID = strings.TrimSpace(uid)
			}
		}
	}
	sessionID := ""
	if userID != "" {
		sessionID = extractClaudeCodeSessionID(userID)
	}
	if sessionID == "" && !hasClaudeCodeHeaderFingerprint(input.Headers) {
		return nil
	}

	result := &types.DetectedProfile{
		ID:               types.ProfileClaudeCode,
		ClientAppID:      "claude_code",
		ClientAppName:    "Claude Code",
		ClientConfidence: "exact",
	}
	if sessionID != "" {
		result.SessionID = sessionID
		result.TraceHint = sessionID
	}
	return result
}
