// Package embeddings provides OpenAI Embeddings transformer — pass-through.
package embeddings

// ParseRequest passes through an embeddings request body.
func ParseRequest(body map[string]any) (map[string]any, error) {
	return body, nil
}

// ParseResponse passes through the embeddings response.
func ParseResponse(body []byte) ([]byte, error) {
	return body, nil
}
