// Package types holds shared type definitions for the proxy module.
// Both proxy/ and proxy/profiles/ import this package to avoid import cycles.
package types

// CliProfileID identifies a CLI client profile.
type CliProfileID string

const (
	ProfileGeneric    CliProfileID = "generic"
	ProfileCodex      CliProfileID = "codex"
	ProfileClaudeCode CliProfileID = "claude_code"
	ProfileGeminiCli  CliProfileID = "gemini_cli"
)

// CliProfileCapabilities describes what a CLI client supports.
type CliProfileCapabilities struct {
	SupportsResponsesCompact             bool
	SupportsResponsesWebsocketIncremental bool
	PreservesContinuation                bool
	SupportsCountTokens                  bool
	EchoesTurnState                      bool
}

// DetectInput is the input for profile detection.
type DetectInput struct {
	DownstreamPath string
	Headers        map[string]string
	Body           any
}

// DetectedProfile is the result of profile detection.
type DetectedProfile struct {
	ID               CliProfileID
	ClientAppID      string
	ClientAppName    string
	ClientConfidence string // "exact" or "heuristic"
	ClientKind       string
	SessionID        string
	TraceHint        string
	Capabilities     CliProfileCapabilities
}

// CliProfileDefinition is a CLI profile with detection logic.
type CliProfileDefinition struct {
	ID           CliProfileID
	Capabilities CliProfileCapabilities
	Detect       func(input DetectInput) *DetectedProfile
}
