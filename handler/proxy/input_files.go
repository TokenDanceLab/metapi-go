package proxyhandler

import (
	"strings"
)

// Input files parsing helpers.
// Extract OpenAI-shaped input_file / file references from request bodies.
// Resolution/upload remains residual: TS inlines local file refs; Go proxies
// durable files via /v1/files and does not invent a local multi-tenant vault.

// InputFile represents an input file reference.
type InputFile struct {
	FileID   string `json:"file_id,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	Filename string `json:"filename,omitempty"`
	Data     []byte `json:"-"`
}

// ParseInputFiles extracts input_file / file parts from an OpenAI-shaped body.
//
// Walk order:
//   - body["messages"] (chat completions)
//   - body["input"] (Responses API)
//
// For each message-like object, content arrays (and nested content maps) are
// walked. Parts with type "input_file" or "file" contribute FileID / FileURL /
// Filename when present. Returns nil when no references are found.
func ParseInputFiles(body map[string]any) []InputFile {
	if body == nil {
		return nil
	}

	var out []InputFile
	collectInputFilesFromValue(&out, body["messages"])
	collectInputFilesFromValue(&out, body["input"])
	if len(out) == 0 {
		return nil
	}
	return out
}

// ResolveInputFile resolves an input file to its content.
// Residual stub — no local vault / no durable fetch in this wave.
// Callers should use /v1/files proxy or upstream-native file_id/file_url.
func ResolveInputFile(file InputFile) ([]byte, error) {
	_ = file
	return nil, nil
}

func collectInputFilesFromValue(out *[]InputFile, v any) {
	switch node := v.(type) {
	case []any:
		for _, item := range node {
			collectInputFilesFromValue(out, item)
		}
	case map[string]any:
		if f, ok := inputFileFromPart(node); ok {
			*out = append(*out, f)
		}
		// Nested content arrays/maps on message-like items.
		if content, exists := node["content"]; exists {
			collectInputFilesFromValue(out, content)
		}
	}
}

func inputFileFromPart(item map[string]any) (InputFile, bool) {
	typeStr := strings.ToLower(strings.TrimSpace(anyString(item["type"])))
	if typeStr != "input_file" && typeStr != "file" {
		return InputFile{}, false
	}

	f := InputFile{
		FileID:   strings.TrimSpace(anyString(item["file_id"])),
		FileURL:  strings.TrimSpace(anyString(item["file_url"])),
		Filename: strings.TrimSpace(anyString(item["filename"])),
	}

	// OpenAI sometimes nests identifiers under a "file" object:
	// {"type":"file","file":{"file_id":"file-abc","filename":"a.pdf"}}
	if nested, ok := item["file"].(map[string]any); ok {
		if f.FileID == "" {
			f.FileID = strings.TrimSpace(anyString(nested["file_id"]))
		}
		if f.FileID == "" {
			f.FileID = strings.TrimSpace(anyString(nested["id"]))
		}
		if f.FileURL == "" {
			f.FileURL = strings.TrimSpace(anyString(nested["file_url"]))
		}
		if f.FileURL == "" {
			f.FileURL = strings.TrimSpace(anyString(nested["url"]))
		}
		if f.Filename == "" {
			f.Filename = strings.TrimSpace(anyString(nested["filename"]))
		}
		if f.Filename == "" {
			f.Filename = strings.TrimSpace(anyString(nested["name"]))
		}
	}

	if f.FileID == "" && f.FileURL == "" && f.Filename == "" {
		return InputFile{}, false
	}
	return f, true
}

func anyString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return ""
	}
}
