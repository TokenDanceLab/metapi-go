package admin

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRequestRejectsDuplicateKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "top-level duplicate",
			body: `{"proxyToken":"sk-first","proxyToken":"sk-second"}`,
		},
		{
			name: "nested duplicate",
			body: `{"routingWeights":{"costWeight":1,"costWeight":2}}`,
		},
		{
			name: "array object duplicate",
			body: `{"items":[{"id":1,"id":2}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/test", strings.NewReader(tt.body))
			var dst map[string]any
			if err := decodeJSONRequest(req, &dst); err == nil {
				t.Fatalf("expected duplicate key error")
			}
		})
	}
}

func TestDecodeJSONRequestRejectsOversizedBody(t *testing.T) {
	previous := adminJSONBodyLimitBytes
	adminJSONBodyLimitBytes = 16
	t.Cleanup(func() { adminJSONBodyLimitBytes = previous })

	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"name":"0123456789abcdef"}`))
	var dst map[string]any
	err := decodeJSONRequest(req, &dst)
	if !errors.Is(err, errJSONRequestBodyTooLarge) {
		t.Fatalf("error = %v, want errJSONRequestBodyTooLarge", err)
	}
}
