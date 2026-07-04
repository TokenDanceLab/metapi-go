package proxyhandler

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// MultipartFormData holds parsed multipart form data with file entries.
type MultipartFormData struct {
	Values map[string][]string
	Files  map[string][]*multipart.FileHeader
}

// ParseMultipartFormData parses multipart/form-data from a request.
// Returns nil if the request is not multipart (caller should fall back to JSON).
func ParseMultipartFormData(r *http.Request) (*MultipartFormData, error) {
	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return nil, nil
	}

	// net/http ParseMultipartForm with a 32MB limit
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, fmt.Errorf("parse multipart form: %w", err)
	}

	if r.MultipartForm == nil {
		return nil, nil
	}

	return &MultipartFormData{
		Values: r.MultipartForm.Value,
		Files:  r.MultipartForm.File,
	}, nil
}

// GetField gets a form field value from multipart data. Returns "" if not found.
func (m *MultipartFormData) GetField(key string) string {
	if m == nil || m.Values == nil {
		return ""
	}
	vals := m.Values[key]
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}

// CloneMultipartBody clones the request body for forwarding to upstream.
// If overrides is non-nil, field values matching override keys are replaced
// (e.g., replacing "model" with the resolved upstream model).
// Returns the body reader, content type, and error.
func CloneMultipartBody(r *http.Request, overrides map[string]string) (io.Reader, string, error) {
	if r.MultipartForm == nil {
		return nil, "", fmt.Errorf("no multipart form parsed")
	}

	contentType := r.Header.Get("Content-Type")

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)

	go func() {
		defer writer.Close()
		defer pipeWriter.Close()

		// Copy form values, applying overrides
		for key, values := range r.MultipartForm.Value {
			for _, val := range values {
				writeVal := val
				if overrides != nil {
					if ov, ok := overrides[key]; ok {
						writeVal = ov
					}
				}
				writer.WriteField(key, writeVal)
			}
		}

		// For override keys not present in the original form, add them
		if overrides != nil {
			for key, ov := range overrides {
				if _, exists := r.MultipartForm.Value[key]; !exists {
					writer.WriteField(key, ov)
				}
			}
		}

		// Copy files
		for key, files := range r.MultipartForm.File {
			for _, fh := range files {
				part, err := writer.CreateFormFile(key, fh.Filename)
				if err != nil {
					return
				}
				file, err := fh.Open()
				if err != nil {
					return
				}
				io.Copy(part, file)
				file.Close()
			}
		}
	}()

	boundary := writer.Boundary()
	newContentType := fmt.Sprintf("multipart/form-data; boundary=%s", boundary)

	_ = contentType // Use new boundary from writer

	return pipeReader, newContentType, nil
}

// IsMultipartRequest checks if the request is multipart/form-data.
func IsMultipartRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}
