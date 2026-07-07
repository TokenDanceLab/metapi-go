package proxyhandler

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	defaultMultipartMemoryBytes       int64 = 32 << 20
	defaultMaxMultipartFieldNames           = 128
	defaultMaxMultipartValuesPerField       = 32
	defaultMaxMultipartTotalValues          = 512
	defaultMaxMultipartFiles                = 16
	defaultMaxMultipartFileBytes      int64 = 20 << 20
)

var ErrMultipartLimitExceeded = errors.New("multipart form exceeded configured limit")

// MultipartFormData holds parsed multipart form data with file entries.
type MultipartFormData struct {
	Values map[string][]string
	Files  map[string][]*multipart.FileHeader
}

// ParseMultipartFormData parses multipart/form-data from a request.
// Returns nil if the request is not multipart (caller should fall back to JSON).
func ParseMultipartFormData(r *http.Request) (*MultipartFormData, error) {
	if !isMultipartFormDataContentType(r.Header.Get("Content-Type")) {
		return nil, nil
	}

	if err := r.ParseMultipartForm(defaultMultipartMemoryBytes); err != nil {
		return nil, fmt.Errorf("parse multipart form: %w", err)
	}

	if r.MultipartForm == nil {
		return nil, nil
	}
	if err := validateMultipartForm(r.MultipartForm); err != nil {
		return nil, err
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
	if err := validateMultipartForm(r.MultipartForm); err != nil {
		return nil, "", err
	}

	contentType := r.Header.Get("Content-Type")

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)

	go func() {
		var copyErr error
		defer func() {
			if err := writer.Close(); copyErr == nil && err != nil {
				copyErr = err
			}
			if copyErr != nil {
				_ = pipeWriter.CloseWithError(copyErr)
				return
			}
			_ = pipeWriter.Close()
		}()

		// Copy form values, applying overrides
		for key, values := range r.MultipartForm.Value {
			for _, val := range values {
				writeVal := val
				if overrides != nil {
					if ov, ok := overrides[key]; ok {
						writeVal = ov
					}
				}
				if err := writer.WriteField(key, writeVal); err != nil {
					copyErr = fmt.Errorf("write multipart field %q: %w", key, err)
					return
				}
			}
		}

		// For override keys not present in the original form, add them
		if overrides != nil {
			for key, ov := range overrides {
				if _, exists := r.MultipartForm.Value[key]; !exists {
					if err := writer.WriteField(key, ov); err != nil {
						copyErr = fmt.Errorf("write multipart override field %q: %w", key, err)
						return
					}
				}
			}
		}

		// Copy files
		for key, files := range r.MultipartForm.File {
			for _, fh := range files {
				part, err := writer.CreateFormFile(key, fh.Filename)
				if err != nil {
					copyErr = fmt.Errorf("create multipart file field %q: %w", key, err)
					return
				}
				file, err := fh.Open()
				if err != nil {
					copyErr = fmt.Errorf("open multipart file %q: %w", fh.Filename, err)
					return
				}
				if _, err := io.Copy(part, file); err != nil {
					copyErr = fmt.Errorf("copy multipart file %q: %w", fh.Filename, err)
					_ = file.Close()
					return
				}
				if err := file.Close(); err != nil {
					copyErr = fmt.Errorf("close multipart file %q: %w", fh.Filename, err)
					return
				}
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
	return isMultipartFormDataContentType(r.Header.Get("Content-Type"))
}

func isMultipartFormDataContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "multipart/form-data")
}

func validateMultipartForm(form *multipart.Form) error {
	if form == nil {
		return nil
	}

	maxFieldNames := positiveEnvInt("PROXY_MAX_MULTIPART_FIELD_NAMES", defaultMaxMultipartFieldNames)
	maxValuesPerField := positiveEnvInt("PROXY_MAX_MULTIPART_VALUES_PER_FIELD", defaultMaxMultipartValuesPerField)
	maxTotalValues := positiveEnvInt("PROXY_MAX_MULTIPART_TOTAL_VALUES", defaultMaxMultipartTotalValues)
	maxFiles := positiveEnvInt("PROXY_MAX_MULTIPART_FILES", defaultMaxMultipartFiles)
	maxFileBytes := positiveEnvInt64("PROXY_MAX_MULTIPART_FILE_BYTES", defaultMaxMultipartFileBytes)

	if len(form.Value) > maxFieldNames {
		return fmt.Errorf("%w: field names %d exceeds limit %d", ErrMultipartLimitExceeded, len(form.Value), maxFieldNames)
	}

	totalValues := 0
	for key, values := range form.Value {
		if len(values) > maxValuesPerField {
			return fmt.Errorf("%w: field %q value count %d exceeds limit %d", ErrMultipartLimitExceeded, key, len(values), maxValuesPerField)
		}
		totalValues += len(values)
		if totalValues > maxTotalValues {
			return fmt.Errorf("%w: total field values %d exceeds limit %d", ErrMultipartLimitExceeded, totalValues, maxTotalValues)
		}
	}

	totalFiles := 0
	for key, files := range form.File {
		totalFiles += len(files)
		if totalFiles > maxFiles {
			return fmt.Errorf("%w: file count %d exceeds limit %d", ErrMultipartLimitExceeded, totalFiles, maxFiles)
		}
		for _, fh := range files {
			if fh != nil && fh.Size > maxFileBytes {
				return fmt.Errorf("%w: file field %q size %d exceeds limit %d", ErrMultipartLimitExceeded, key, fh.Size, maxFileBytes)
			}
		}
	}

	return nil
}

func positiveEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func positiveEnvInt64(name string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
