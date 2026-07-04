package proxy

import (
	"github.com/tokendancelab/metapi-go/proxy/profiles"
	"github.com/tokendancelab/metapi-go/proxy/types"
)

func init() {
	// Register detection profiles in priority order.
	for _, p := range profiles.All() {
		RegisterDetectionProfile(p)
	}
	// Register the generic fallback profile (not in detection chain).
	RegisterFallbackProfile(profiles.Generic())
}

// DownstreamClientContext holds the detected downstream client context.
type DownstreamClientContext struct {
	ClientKind       string
	ClientAppID      string
	ClientAppName    string
	ClientConfidence string
	SessionID        string
	TraceHint        string
	Capabilities     CliProfileCapabilities
}

// DetectClientContext detects the downstream client context from request metadata.
func DetectClientContext(downstreamPath string, headers map[string]string, body any) DownstreamClientContext {
	profile := DetectCliProfile(types.DetectInput{
		DownstreamPath: downstreamPath,
		Headers:        headers,
		Body:           body,
	})
	return DownstreamClientContext{
		ClientKind:       profile.ClientKind,
		ClientAppID:      profile.ClientAppID,
		ClientAppName:    profile.ClientAppName,
		ClientConfidence: profile.ClientConfidence,
		SessionID:        profile.SessionID,
		TraceHint:        profile.TraceHint,
		Capabilities:     profile.Capabilities,
	}
}
