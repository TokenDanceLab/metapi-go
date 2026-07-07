package proxyhandler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- IsMultipartRequest ----

func TestIsMultipartRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"multipart", "multipart/form-data; boundary=---", true},
		{"multipart simple", "multipart/form-data", true},
		{"multipart mixed case", "Multipart/Form-Data; boundary=---", true},
		{"multipart invalid suffix", "multipart/form-datax; boundary=---", false},
		{"json", "application/json", false},
		{"empty", "", false},
		{"text", "text/plain", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", nil)
			req.Header.Set("Content-Type", tt.contentType)
			got := IsMultipartRequest(req)
			if got != tt.want {
				t.Errorf("IsMultipartRequest = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- ParseMultipartFormData ----

func TestParseMultipartFormData_JSONFallback(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	got, err := ParseMultipartFormData(req)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for JSON body (caller should fall back)")
	}
}

func TestParseMultipartFormData_Multipart(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("model", "gpt-4o")
	writer.WriteField("prompt", "a cat")
	part, _ := writer.CreateFormFile("image", "test.png")
	part.Write([]byte("fake-image-data"))
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	got, err := ParseMultipartFormData(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result for multipart body")
	}

	if model := got.GetField("model"); model != "gpt-4o" {
		t.Errorf("model field = %q, want gpt-4o", model)
	}
	if prompt := got.GetField("prompt"); prompt != "a cat" {
		t.Errorf("prompt field = %q, want 'a cat'", prompt)
	}
}

func TestParseMultipartFormData_EmptyContentType(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	got, err := ParseMultipartFormData(req)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for empty content type")
	}
}

// ---- MultipartFormData.GetField ----

func TestMultipartFormData_GetField(t *testing.T) {
	tests := []struct {
		name string
		fd   *MultipartFormData
		key  string
		want string
	}{
		{"nil fd", nil, "model", ""},
		{"nil Values", &MultipartFormData{}, "model", ""},
		{"existing", &MultipartFormData{Values: map[string][]string{"model": {"gpt-4o"}}}, "model", "gpt-4o"},
		{"missing", &MultipartFormData{Values: map[string][]string{"other": {"val"}}}, "model", ""},
		{"empty slice", &MultipartFormData{Values: map[string][]string{"model": {}}}, "model", ""},
		{"trimmed", &MultipartFormData{Values: map[string][]string{"model": {"  gpt-4o  "}}}, "model", "gpt-4o"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fd.GetField(tt.key)
			if got != tt.want {
				t.Errorf("GetField(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// ---- CloneMultipartBody ----

func TestCloneMultipartBody(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("model", "gpt-4o")
	part, _ := writer.CreateFormFile("image", "test.png")
	part.Write([]byte("fake-image-data"))
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Parse first
	_, err := ParseMultipartFormData(req)
	if err != nil {
		t.Fatalf("ParseMultipartFormData failed: %v", err)
	}

	// Clone
	clonedBody, newCT, err := CloneMultipartBody(req, nil)
	if err != nil {
		t.Fatalf("CloneMultipartBody failed: %v", err)
	}
	if clonedBody == nil {
		t.Fatal("expected non-nil cloned body")
	}
	if newCT == "" {
		t.Error("expected non-empty content type")
	}
	if !strings.HasPrefix(newCT, "multipart/form-data") {
		t.Errorf("content type = %q", newCT)
	}

	// Read cloned body
	data, err := io.ReadAll(clonedBody)
	if err != nil {
		t.Fatalf("reading cloned body: %v", err)
	}
	if len(data) == 0 {
		t.Error("cloned body is empty")
	}
}

func TestCloneMultipartBody_NoForm(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	_, _, err := CloneMultipartBody(req, nil)
	if err == nil {
		t.Error("expected error when no multipart form parsed")
	}
}

func TestCloneMultipartBodyPropagatesFileOpenError(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.MultipartForm = &multipart.Form{
		Value: map[string][]string{"model": {"gpt-4o"}},
		File: map[string][]*multipart.FileHeader{
			"image": {{Filename: "missing.png"}},
		},
	}

	clonedBody, _, err := CloneMultipartBody(req, nil)
	if err != nil {
		t.Fatalf("CloneMultipartBody returned early error: %v", err)
	}
	_, err = io.ReadAll(clonedBody)
	if err == nil {
		t.Fatal("reading cloned body succeeded, want propagated file open error")
	}
	if !strings.Contains(err.Error(), "open multipart file") {
		t.Fatalf("error = %v, want file open context", err)
	}
}

// ---- MultipartFormData with multiple files ----

func TestParseMultipartFormData_MultipleFiles(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("model", "sora-2")
	part1, _ := writer.CreateFormFile("video", "test1.mp4")
	part1.Write([]byte("video-data-1"))
	part2, _ := writer.CreateFormFile("audio", "test2.wav")
	part2.Write([]byte("audio-data"))
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	got, err := ParseMultipartFormData(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if len(got.Files) < 2 {
		t.Errorf("expected 2 files, got %d", len(got.Files))
	}
}

func TestParseMultipartFormDataRejectsTooManyFieldNames(t *testing.T) {
	t.Setenv("PROXY_MAX_MULTIPART_FIELD_NAMES", "2")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("model", "gpt-4o")
	writer.WriteField("prompt", "a cat")
	writer.WriteField("extra", "value")
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := ParseMultipartFormData(req)
	if err == nil {
		t.Fatal("expected multipart field-name limit error")
	}
	if !strings.Contains(err.Error(), "field names") {
		t.Fatalf("error = %v, want field names context", err)
	}
}

func TestParseMultipartFormDataRejectsTooManyValuesPerField(t *testing.T) {
	t.Setenv("PROXY_MAX_MULTIPART_VALUES_PER_FIELD", "2")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("tag", "one")
	writer.WriteField("tag", "two")
	writer.WriteField("tag", "three")
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := ParseMultipartFormData(req)
	if err == nil {
		t.Fatal("expected multipart values-per-field limit error")
	}
	if !strings.Contains(err.Error(), "value count") {
		t.Fatalf("error = %v, want value count context", err)
	}
}

func TestParseMultipartFormDataRejectsTooManyFiles(t *testing.T) {
	t.Setenv("PROXY_MAX_MULTIPART_FILES", "1")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("model", "sora-2")
	part1, _ := writer.CreateFormFile("video", "one.mp4")
	part1.Write([]byte("one"))
	part2, _ := writer.CreateFormFile("audio", "two.wav")
	part2.Write([]byte("two"))
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := ParseMultipartFormData(req)
	if err == nil {
		t.Fatal("expected multipart file count limit error")
	}
	if !strings.Contains(err.Error(), "file count") {
		t.Fatalf("error = %v, want file count context", err)
	}
}

func TestParseMultipartFormDataRejectsOversizedFile(t *testing.T) {
	t.Setenv("PROXY_MAX_MULTIPART_FILE_BYTES", "4")

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("image", "large.png")
	part.Write([]byte("12345"))
	writer.Close()

	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := ParseMultipartFormData(req)
	if err == nil {
		t.Fatal("expected multipart file size limit error")
	}
	if !strings.Contains(err.Error(), "size") {
		t.Fatalf("error = %v, want size context", err)
	}
}

// ---- Ensure imports used ----
var _ = http.MethodPost
