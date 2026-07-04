package proxy

import (
	"testing"
)

func TestParseInputFiles_Empty(t *testing.T) {
	files := ParseInputFiles(map[string]any{})
	if files != nil {
		t.Errorf("expected nil, got %d files", len(files))
	}
}

func TestParseInputFiles_WithBody(t *testing.T) {
	files := ParseInputFiles(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if files != nil {
		t.Errorf("expected nil (stub), got %d files", len(files))
	}
}

func TestResolveInputFile_Stub(t *testing.T) {
	file := InputFile{
		FileID:   "file-123",
		Filename: "test.txt",
	}
	data, err := ResolveInputFile(file)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data (stub), got %d bytes", len(data))
	}
}

func TestInputFile_Fields(t *testing.T) {
	f := InputFile{
		FileID:   "file-abc",
		FileURL:  "https://example.com/file",
		Filename: "document.pdf",
		Data:     []byte("content"),
	}

	if f.FileID != "file-abc" {
		t.Errorf("FileID = %q", f.FileID)
	}
	if f.FileURL != "https://example.com/file" {
		t.Errorf("FileURL = %q", f.FileURL)
	}
	if f.Filename != "document.pdf" {
		t.Errorf("Filename = %q", f.Filename)
	}
	if string(f.Data) != "content" {
		t.Errorf("Data = %q", string(f.Data))
	}
}
