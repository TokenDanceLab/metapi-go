package proxyhandler

import (
	"testing"
)

func TestParseInputFiles_Empty(t *testing.T) {
	files := ParseInputFiles(map[string]any{})
	if files != nil {
		t.Errorf("expected nil, got %d files", len(files))
	}
	if ParseInputFiles(nil) != nil {
		t.Errorf("expected nil for nil body")
	}
}

func TestParseInputFiles_MessagesWithoutFiles(t *testing.T) {
	files := ParseInputFiles(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if files != nil {
		t.Errorf("expected nil when no file parts, got %d files", len(files))
	}
}

func TestParseInputFiles_NestedMessageContent(t *testing.T) {
	files := ParseInputFiles(map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Summarize this file."},
					map[string]any{
						"type":     "file",
						"file_id":  "file-xyz",
						"filename": "report.pdf",
					},
					map[string]any{
						"type":     "input_file",
						"file_url": "https://example.com/a.txt",
						"filename": "a.txt",
					},
				},
			},
		},
	})
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].FileID != "file-xyz" || files[0].Filename != "report.pdf" {
		t.Errorf("files[0] = %+v", files[0])
	}
	if files[1].FileURL != "https://example.com/a.txt" || files[1].Filename != "a.txt" {
		t.Errorf("files[1] = %+v", files[1])
	}
}

func TestParseInputFiles_TopLevelInputArray(t *testing.T) {
	files := ParseInputFiles(map[string]any{
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":     "input_file",
						"file_id":  "file-input-1",
						"filename": "notes.md",
					},
				},
			},
			// Direct content part at top-level input (Responses-style).
			map[string]any{
				"type":     "file",
				"file_id":  "file-input-2",
				"filename": "plain.txt",
			},
		},
	})
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(files), files)
	}
	if files[0].FileID != "file-input-1" || files[0].Filename != "notes.md" {
		t.Errorf("files[0] = %+v", files[0])
	}
	if files[1].FileID != "file-input-2" || files[1].Filename != "plain.txt" {
		t.Errorf("files[1] = %+v", files[1])
	}
}

func TestParseInputFiles_NestedFileObject(t *testing.T) {
	files := ParseInputFiles(map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "file",
						"file": map[string]any{
							"file_id":  "file-nested",
							"filename": "nested.pdf",
						},
					},
				},
			},
		},
	})
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].FileID != "file-nested" || files[0].Filename != "nested.pdf" {
		t.Errorf("files[0] = %+v", files[0])
	}
}

func TestParseInputFiles_IgnoresEmptyTypedParts(t *testing.T) {
	files := ParseInputFiles(map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "file"},
					map[string]any{"type": "input_file", "file_data": "only-data-no-id"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://x"}},
				},
			},
		},
	})
	if files != nil {
		t.Errorf("expected nil for empty/unsupported parts, got %+v", files)
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
