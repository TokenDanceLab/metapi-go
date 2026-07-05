package embeddings

import (
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
	}{
		{"valid request", map[string]any{"model": "text-embedding-3-small", "input": "hello world"}},
		{"empty map", map[string]any{}},
		{"nil map", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRequest(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(tt.input) > 0 && got["model"] != tt.input["model"] {
				t.Errorf("model mismatch: got %v, want %v", got["model"], tt.input["model"])
			}
		})
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"valid response", []byte(`{"object":"list","data":[]}`)},
		{"empty", []byte{}},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if string(got) != string(tt.input) {
				t.Errorf("response mismatch: got %q, want %q", got, tt.input)
			}
		})
	}
}
