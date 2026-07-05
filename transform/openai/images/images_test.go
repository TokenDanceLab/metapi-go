package images

import (
	"testing"
)

func TestParseRequest_PassThrough(t *testing.T) {
	body := map[string]any{
		"model":  "dall-e-3",
		"prompt": "A cat wearing a top hat",
		"n":      2,
		"size":   "1024x1024",
	}

	result, err := ParseRequest(body)
	if err != nil {
		t.Fatalf("ParseRequest returned unexpected error: %v", err)
	}

	if result["model"] != "dall-e-3" {
		t.Errorf("model mismatch: got %v, want %q", result["model"], "dall-e-3")
	}
	if result["prompt"] != "A cat wearing a top hat" {
		t.Errorf("prompt mismatch: got %v, want %q", result["prompt"], "A cat wearing a top hat")
	}
	if result["n"] != 2 {
		t.Errorf("n mismatch: got %v, want 2", result["n"])
	}
	if result["size"] != "1024x1024" {
		t.Errorf("size mismatch: got %v, want %q", result["size"], "1024x1024")
	}
}

func TestParseResponse_PassThrough(t *testing.T) {
	body := []byte(`{"created":1720000000,"data":[{"url":"https://example.com/img.png"}]}`)

	result, err := ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse returned unexpected error: %v", err)
	}

	if string(result) != string(body) {
		t.Errorf("ParseResponse returned modified body:\n got  %s\n want %s", string(result), string(body))
	}
}

func TestParseResponse_EmptyBody(t *testing.T) {
	body := []byte{}

	result, err := ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse returned unexpected error for empty body: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(result))
	}
}
