package proxy

// Input files parsing helpers.
// In the TS codebase, input files are resolved from multipart form data
// or JSON bodies containing file references (file_id, file_url, etc.).
// This file provides placeholders for that functionality.

// InputFile represents an input file reference.
type InputFile struct {
	FileID   string `json:"file_id,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	Filename string `json:"filename,omitempty"`
	Data     []byte `json:"-"`
}

// ParseInputFiles extracts input files from the request context.
// Stub — returns empty slice.
func ParseInputFiles(body map[string]any) []InputFile {
	// In production, extracts files from body.input arrays,
	// message content blocks, and system prompt attachments.
	_ = body
	return nil
}

// ResolveInputFile resolves an input file to its content.
// Stub — returns nil.
func ResolveInputFile(file InputFile) ([]byte, error) {
	_ = file
	return nil, nil
}
