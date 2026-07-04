package profiles

import (
	"strings"

	"github.com/tokendancelab/metapi-go/proxy/types"
)

func geminiCliProfile() *types.CliProfileDefinition {
	return &types.CliProfileDefinition{
		ID: types.ProfileGeminiCli,
		Capabilities: types.CliProfileCapabilities{
			SupportsResponsesCompact:             false,
			SupportsResponsesWebsocketIncremental: false,
			PreservesContinuation:                false,
			SupportsCountTokens:                  true,
			EchoesTurnState:                      false,
		},
		Detect: detectGeminiCli,
	}
}

func isGeminiCliPath(path string) bool {
	normalized := strings.ToLower(strings.TrimSpace(path))
	return normalized == "/v1internal:generatecontent" ||
		normalized == "/v1internal:streamgeneratecontent" ||
		normalized == "/v1internal:counttokens"
}

func hasGeminiCliBodyShape(body any) bool {
	m, ok := body.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := m["model"].(string); ok {
		return true
	}
	if _, ok := m["contents"]; ok {
		return true
	}
	if req, ok := m["request"].(map[string]any); ok {
		if _, ok := req["contents"]; ok {
			return true
		}
		if _, ok := req["model"].(string); ok {
			return true
		}
	}
	return false
}

func detectGeminiCli(input types.DetectInput) *types.DetectedProfile {
	if !isGeminiCliPath(input.DownstreamPath) {
		return nil
	}
	if input.Body != nil && !hasGeminiCliBodyShape(input.Body) {
		return nil
	}
	return &types.DetectedProfile{
		ID:               types.ProfileGeminiCli,
		ClientAppID:      "gemini_cli",
		ClientAppName:    "Gemini CLI",
		ClientConfidence: "exact",
	}
}
