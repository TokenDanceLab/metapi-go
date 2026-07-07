package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/tokendancelab/metapi-go/config"
)

var (
	errJSONRequestBodyTooLarge = errors.New("request body exceeds maximum size")
	adminJSONBodyLimitBytes    = int64(config.DefaultRequestBodyLimit)
)

// decodeJSONRequest decodes exactly one JSON value from an admin request body.
// Unknown fields remain allowed for API compatibility; trailing JSON is rejected
// so malformed or concatenated payloads cannot partially mutate admin state.
func decodeJSONRequest(r *http.Request, dst any) error {
	_, err := decodeJSONRequestRaw(r, dst)
	return err
}

func decodeJSONRequestRaw(r *http.Request, dst any) ([]byte, error) {
	raw, err := readAdminJSONBody(r.Body)
	if err != nil {
		return nil, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(dst); err != nil {
		return nil, err
	}
	var extra struct{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("request body must contain a single JSON value")
		}
		return nil, err
	}
	return raw, nil
}

func readAdminJSONBody(body io.Reader) ([]byte, error) {
	if adminJSONBodyLimitBytes <= 0 {
		return io.ReadAll(body)
	}
	limited := &io.LimitedReader{R: body, N: adminJSONBodyLimitBytes + 1}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > adminJSONBodyLimitBytes {
		return nil, errJSONRequestBodyTooLarge
	}
	return raw, nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := scanJSONValue(dec); err != nil {
		return err
	}
	if tok, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return fmt.Errorf("request body must contain a single JSON value, got extra token %v", tok)
	}
	return nil
}

func scanJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		return scanJSONObject(dec)
	case '[':
		return scanJSONArray(dec)
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func scanJSONObject(dec *json.Decoder) error {
	seen := make(map[string]struct{})
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("invalid JSON object key token %v", tok)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate JSON object key %q", key)
		}
		seen[key] = struct{}{}
		if err := scanJSONValue(dec); err != nil {
			return err
		}
	}

	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('}') {
		return fmt.Errorf("expected JSON object close, got %v", tok)
	}
	return nil
}

func scanJSONArray(dec *json.Decoder) error {
	for dec.More() {
		if err := scanJSONValue(dec); err != nil {
			return err
		}
	}

	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim(']') {
		return fmt.Errorf("expected JSON array close, got %v", tok)
	}
	return nil
}
