package completions

import (
	"testing"
)

func TestParseRequest_PreservesPromptAndModel(t *testing.T) {
	body := map[string]any{
		"model":  "gpt-3.5-turbo-instruct",
		"prompt": "Explain quantum computing in simple terms.",
		"max_tokens": 256,
		"temperature": 0.7,
	}

	result, err := ParseRequest(body)
	if err != nil {
		t.Fatalf("ParseRequest returned unexpected error: %v", err)
	}

	if result["model"] != "gpt-3.5-turbo-instruct" {
		t.Errorf("model mismatch: got %v, want %q", result["model"], "gpt-3.5-turbo-instruct")
	}
	if result["prompt"] != "Explain quantum computing in simple terms." {
		t.Errorf("prompt mismatch: got %v, want %q", result["prompt"], "Explain quantum computing in simple terms.")
	}
	if result["max_tokens"] != 256 {
		t.Errorf("max_tokens mismatch: got %v, want 256", result["max_tokens"])
	}
	if result["temperature"] != 0.7 {
		t.Errorf("temperature mismatch: got %v, want 0.7", result["temperature"])
	}
}

func TestParseRequest_EmptyPrompt(t *testing.T) {
	body := map[string]any{
		"model":  "text-davinci-003",
		"prompt": "",
	}

	result, err := ParseRequest(body)
	if err != nil {
		t.Fatalf("ParseRequest returned unexpected error for empty prompt: %v", err)
	}

	prompt, ok := result["prompt"].(string)
	if !ok {
		t.Fatalf("prompt field is not a string, got type %T", result["prompt"])
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
	if result["model"] != "text-davinci-003" {
		t.Errorf("model mismatch: got %v, want %q", result["model"], "text-davinci-003")
	}
}
