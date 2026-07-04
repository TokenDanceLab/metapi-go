package proxyhandler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// ---- ParseResponsesWSMessage ----

func TestParseResponsesWSMessage_Basic(t *testing.T) {
	raw := []byte(`{"type":"response.create","model":"gpt-4o","input":["hello"]}`)
	msg, err := ParseResponsesWSMessage(raw)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Type != "response.create" {
		t.Errorf("Type = %q", msg.Type)
	}
	if msg.Model != "gpt-4o" {
		t.Errorf("Model = %q", msg.Model)
	}
	if len(msg.Input) != 1 {
		t.Errorf("Input len = %d", len(msg.Input))
	}
}

func TestParseResponsesWSMessage_WithGenerate(t *testing.T) {
	raw := []byte(`{"type":"response.create","model":"gpt-4o","generate":true}`)
	msg, err := ParseResponsesWSMessage(raw)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Generate == nil || !*msg.Generate {
		t.Error("Generate should be true")
	}
}

func TestParseResponsesWSMessage_GenerateFalse(t *testing.T) {
	raw := []byte(`{"type":"response.create","model":"gpt-4o","generate":false}`)
	msg, err := ParseResponsesWSMessage(raw)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Generate == nil || *msg.Generate {
		t.Error("Generate should be false")
	}
}

func TestParseResponsesWSMessage_WithPreviousResponseID(t *testing.T) {
	raw := []byte(`{"type":"response.create","model":"gpt-4o","previous_response_id":"resp_abc123"}`)
	msg, err := ParseResponsesWSMessage(raw)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.PreviousResponseID != "resp_abc123" {
		t.Errorf("PreviousResponseID = %q", msg.PreviousResponseID)
	}
}

func TestParseResponsesWSMessage_RawAccess(t *testing.T) {
	raw := []byte(`{"type":"response.create","model":"gpt-4o","custom":"field"}`)
	msg, err := ParseResponsesWSMessage(raw)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Raw == nil {
		t.Fatal("Raw should not be nil")
	}
	if msg.Raw["custom"] != "field" {
		t.Errorf("Raw custom = %v", msg.Raw["custom"])
	}
}

func TestParseResponsesWSMessage_InvalidJSON(t *testing.T) {
	_, err := ParseResponsesWSMessage([]byte(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---- ResponsesWSError ----

func TestResponsesWSError(t *testing.T) {
	err := ResponsesWSError(400, "bad request")

	if err["type"] != "error" {
		t.Errorf("type = %v", err["type"])
	}
	if err["status"] != 400 {
		t.Errorf("status = %v", err["status"])
	}
	errDetail := err["error"].(map[string]any)
	if errDetail["type"] != "invalid_request_error" {
		t.Errorf("error.type = %v", errDetail["type"])
	}
	if errDetail["message"] != "bad request" {
		t.Errorf("error.message = %v", errDetail["message"])
	}
}

func TestResponsesWSError_AllStatusCodes(t *testing.T) {
	codes := []int{400, 401, 403, 404, 408, 429, 500, 503}
	for _, code := range codes {
		err := ResponsesWSError(code, "test error")
		if err["status"] != code {
			t.Errorf("status = %v, want %v", err["status"], code)
		}
	}
}

// ---- SynthesizePrewarmResponsePayloads ----

func TestSynthesizePrewarmResponsePayloads_Default(t *testing.T) {
	payloads := SynthesizePrewarmResponsePayloads("gpt-4o", "")

	if len(payloads) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(payloads))
	}

	// First: response.created
	p1 := payloads[0]
	if p1["type"] != "response.created" {
		t.Errorf("first type = %v", p1["type"])
	}
	resp1 := p1["response"].(map[string]any)
	if resp1["status"] != "in_progress" {
		t.Errorf("response.created status = %v", resp1["status"])
	}
	if resp1["model"] != "gpt-4o" {
		t.Errorf("response.created model = %v", resp1["model"])
	}

	// Second: response.completed
	p2 := payloads[1]
	if p2["type"] != "response.completed" {
		t.Errorf("second type = %v", p2["type"])
	}
	resp2 := p2["response"].(map[string]any)
	if resp2["status"] != "completed" {
		t.Errorf("response.completed status = %v", resp2["status"])
	}
	usage := resp2["usage"].(map[string]any)
	if v, ok := usage["input_tokens"]; !ok || fmtInt(v) != 0 {
		t.Errorf("input_tokens = %v", usage["input_tokens"])
	}
}

func TestSynthesizePrewarmResponsePayloads_CustomID(t *testing.T) {
	payloads := SynthesizePrewarmResponsePayloads("claude-sonnet", "resp_custom_123")

	resp1 := payloads[0]["response"].(map[string]any)
	if resp1["id"] != "resp_custom_123" {
		t.Errorf("id = %v", resp1["id"])
	}

	resp2 := payloads[1]["response"].(map[string]any)
	if resp2["id"] != "resp_custom_123" {
		t.Errorf("id = %v", resp2["id"])
	}
}

func TestSynthesizePrewarmResponsePayloads_DefaultID(t *testing.T) {
	payloads := SynthesizePrewarmResponsePayloads("", "")

	resp1 := payloads[0]["response"].(map[string]any)
	if resp1["id"] != "resp_prewarm_metapi" {
		t.Errorf("default id = %v", resp1["id"])
	}
}

func TestSynthesizePrewarmResponsePayloads_EmptyModel(t *testing.T) {
	payloads := SynthesizePrewarmResponsePayloads("", "resp_test")
	resp1 := payloads[0]["response"].(map[string]any)
	if resp1["model"] != "unknown" {
		t.Errorf("empty model should default to 'unknown', got %v", resp1["model"])
	}
}

// ---- extractWSHeaders ----

func TestExtractWSHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("X-Custom", "value")

	headers := extractWSHeaders(req)
	if headers["Authorization"] != "Bearer test" {
		t.Errorf("Authorization = %q", headers["Authorization"])
	}
	if headers["X-Custom"] != "value" {
		t.Errorf("X-Custom = %q", headers["X-Custom"])
	}
}

func TestExtractWSHeaders_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	headers := extractWSHeaders(req)
	if len(headers) != 0 {
		t.Errorf("expected empty headers, got %d", len(headers))
	}
}

// ---- extractWSTurnState ----

func TestExtractWSTurnState(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	req.Header.Set("x-codex-turn-state", "  abc123  ")
	got := extractWSTurnState(req)

	if got != "abc123" {
		t.Errorf("turnState = %q, want abc123", got)
	}
}

func TestExtractWSTurnState_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	got := extractWSTurnState(req)

	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ---- ResponsesWSMessage with instructions ----

func TestParseResponsesWSMessage_WithInstructions(t *testing.T) {
	raw := []byte(`{"type":"response.create","model":"gpt-4o","instructions":"You are helpful."}`)
	msg, err := ParseResponsesWSMessage(raw)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	instructions, ok := msg.Instructions.(string)
	if !ok || instructions != "You are helpful." {
		t.Errorf("Instructions = %v", msg.Instructions)
	}
}

// fmtInt converts an interface value to int, handling both float64 and int JSON unmarshal results.
func fmtInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	}
	return -1
}

// Ensure imports used
var _ = json.Marshal
