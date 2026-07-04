// Package images provides OpenAI Images transformer — pass-through.
package images

// ParseRequest passes through an images request body.
func ParseRequest(body map[string]any) (map[string]any, error) {
	return body, nil
}

// ParseResponse passes through the images response.
func ParseResponse(body []byte) ([]byte, error) {
	return body, nil
}
