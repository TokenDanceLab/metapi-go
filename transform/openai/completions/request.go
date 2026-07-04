// Package completions provides OpenAI Completions (legacy) transformer — pass-through.
package completions

// ParseRequest passes through an OpenAI completions request body.
func ParseRequest(body map[string]any) (map[string]any, error) {
	// Completions endpoint is mostly pass-through; the proxy handles routing.
	return body, nil
}

// ParseResponse passes through the completions response.
func ParseResponse(body []byte) ([]byte, error) {
	return body, nil
}
