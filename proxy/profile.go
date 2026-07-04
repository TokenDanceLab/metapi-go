// Package proxy implements the proxy orchestration core (P8).
// Dual loops: outer channel retry + inner endpoint iteration.
// Session lease with sticky bindings and concurrency limiting.
package proxy

import (
	"github.com/tokendancelab/metapi-go/proxy/types"
)

// Re-export types for convenience so callers don't need to import proxy/types.
type (
	CliProfileID             = types.CliProfileID
	CliProfileCapabilities   = types.CliProfileCapabilities
	DetectInput              = types.DetectInput
	DetectedProfile          = types.DetectedProfile
	CliProfileDefinition     = types.CliProfileDefinition
)

const (
	ProfileGeneric    = types.ProfileGeneric
	ProfileCodex      = types.ProfileCodex
	ProfileClaudeCode = types.ProfileClaudeCode
	ProfileGeminiCli  = types.ProfileGeminiCli
)

// orderedProfiles is the detection order: claude_code -> codex -> gemini_cli.
// generic is the unconditional fallback.
var orderedProfiles = []*types.CliProfileDefinition{}

var profileMap = map[types.CliProfileID]*types.CliProfileDefinition{}

// RegisterDetectionProfile adds a profile to the detection chain.
// Profiles must be registered in priority order: claude_code -> codex -> gemini_cli.
func RegisterDetectionProfile(p *types.CliProfileDefinition) {
	orderedProfiles = append(orderedProfiles, p)
	profileMap[p.ID] = p
}

// RegisterFallbackProfile registers a profile for map lookup only (not in detection chain).
func RegisterFallbackProfile(p *types.CliProfileDefinition) {
	profileMap[p.ID] = p
}

// GetProfileDefinition returns a profile by ID.
func GetProfileDefinition(id types.CliProfileID) *types.CliProfileDefinition {
	return profileMap[id]
}

// DetectCliProfile detects the CLI profile from the input.
// Returns the first matching profile, or generic as fallback.
func DetectCliProfile(input types.DetectInput) types.DetectedProfile {
	for _, p := range orderedProfiles {
		detected := p.Detect(input)
		if detected != nil {
			detected.Capabilities = p.Capabilities
			detected.ClientKind = string(p.ID)
			return *detected
		}
	}

	gp := profileMap[types.ProfileGeneric]
	return types.DetectedProfile{
		ID:           types.ProfileGeneric,
		Capabilities: gp.Capabilities,
		ClientKind:   string(types.ProfileGeneric),
	}
}
