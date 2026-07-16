package responses

import (
	"strings"
	"testing"
)

func TestSanitizeResponsesInputItems_ReasoningRoundTrip(t *testing.T) {
	// Hermes/Codex second-turn shape from upstream #538:
	// reasoning has encrypted_content + summary, no content.
	input := []any{
		map[string]any{
			"type":    "message",
			"role":    "user",
			"content": []any{map[string]any{"type": "input_text", "text": "continue"}},
		},
		map[string]any{
			"type":              "reasoning",
			"id":                "rs_1",
			"encrypted_content": "enc_abc",
			"summary": []any{
				map[string]any{"type": "summary_text", "text": "step one"},
			},
		},
		map[string]any{
			"type":    "message",
			"role":    "assistant",
			"content": "",
		},
		map[string]any{
			"type":      "function_call",
			"call_id":   "call_1",
			"name":      "lookup",
			"arguments": `{"q":"x"}`,
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": "call_1",
			"output":  `{"ok":true}`,
		},
	}

	got, err := SanitizeResponsesInputItems(input)
	if err != nil {
		t.Fatalf("SanitizeResponsesInputItems: %v", err)
	}
	arr, ok := got.([]any)
	if !ok || len(arr) != 5 {
		t.Fatalf("len = %d type=%T", len(arr), got)
	}

	// User message preserved.
	user, _ := arr[0].(map[string]any)
	if user["role"] != "user" {
		t.Fatalf("user role = %v", user["role"])
	}

	// Reasoning: encrypted_content + summary kept; content injected from summary.
	reasoning, _ := arr[1].(map[string]any)
	if reasoning["type"] != "reasoning" {
		t.Fatalf("type = %v", reasoning["type"])
	}
	if reasoning["encrypted_content"] != "enc_abc" {
		t.Fatalf("encrypted_content dropped: %v", reasoning["encrypted_content"])
	}
	if _, ok := reasoning["summary"]; !ok {
		t.Fatal("summary dropped")
	}
	content, ok := reasoning["content"].(string)
	if !ok || content != "step one" {
		t.Fatalf("content = %#v, want summary text", reasoning["content"])
	}
	if reasoning["id"] != "rs_1" {
		t.Fatalf("id = %v", reasoning["id"])
	}

	// Empty assistant content preserved (not deleted).
	asst, _ := arr[2].(map[string]any)
	if asst["content"] != "" {
		t.Fatalf("assistant content = %#v", asst["content"])
	}

	// Tool lifecycle pass-through.
	fc, _ := arr[3].(map[string]any)
	if fc["type"] != "function_call" || fc["call_id"] != "call_1" {
		t.Fatalf("function_call = %#v", fc)
	}
	fco, _ := arr[4].(map[string]any)
	if fco["type"] != "function_call_output" {
		t.Fatalf("function_call_output = %#v", fco)
	}
}

func TestSanitizeResponsesInputItems_EncryptedOnlyInjectsEmptyContent(t *testing.T) {
	input := []any{
		map[string]any{
			"type":              "reasoning",
			"encrypted_content": "enc_only",
			"summary":           []any{},
		},
	}
	got, err := SanitizeResponsesInputItems(input)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	arr := got.([]any)
	r := arr[0].(map[string]any)
	if r["encrypted_content"] != "enc_only" {
		t.Fatalf("encrypted_content = %v", r["encrypted_content"])
	}
	if c, ok := r["content"].(string); !ok || c != "" {
		t.Fatalf("content = %#v, want empty string key present", r["content"])
	}
}

func TestSanitizeResponsesInputItems_MissingRequiredFieldsError(t *testing.T) {
	input := []any{
		map[string]any{"type": "message", "role": "user", "content": "hi"},
		map[string]any{
			"type":    "reasoning",
			"summary": []any{},
			// no encrypted_content, no content, empty summary
		},
	}
	_, err := SanitizeResponsesInputItems(input)
	if err == nil {
		t.Fatal("expected ReasoningInputError")
	}
	re, ok := err.(*ReasoningInputError)
	if !ok {
		t.Fatalf("err type = %T", err)
	}
	if re.Index != 1 {
		t.Fatalf("index = %d", re.Index)
	}
	if !strings.Contains(re.Error(), "input[1]") {
		t.Fatalf("message = %q", re.Error())
	}
	if !strings.Contains(re.Error(), "encrypted_content") {
		t.Fatalf("message should list required fields: %q", re.Error())
	}
}

func TestSanitizeResponsesInputItems_PreservesExistingContent(t *testing.T) {
	input := []any{
		map[string]any{
			"type":              "reasoning",
			"content":           "already here",
			"encrypted_content": "enc",
			"summary": []any{
				map[string]any{"type": "summary_text", "text": "sum"},
			},
		},
	}
	got, err := SanitizeResponsesInputItems(input)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	r := got.([]any)[0].(map[string]any)
	if r["content"] != "already here" {
		t.Fatalf("content overwritten: %v", r["content"])
	}
}

func TestSanitizeResponsesInputItems_StringPassthrough(t *testing.T) {
	got, err := SanitizeResponsesInputItems("hello")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got = %v", got)
	}
}

func TestSanitizeResponsesRequestBody_ReasoningAndCompact(t *testing.T) {
	body := map[string]any{
		"model":                "gpt-5.4",
		"stream":               true,
		"stream_options":       map[string]any{"include_usage": true},
		"store":                true,
		"previous_response_id": "resp_1",
		"input": []any{
			map[string]any{
				"type":              "reasoning",
				"encrypted_content": "enc_xyz",
				"summary": []any{
					map[string]any{"type": "summary_text", "text": "think"},
				},
			},
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "next turn",
			},
		},
	}

	next, decision, err := SanitizeResponsesRequestBody(body, ContinuityPolicyInput{
		SitePlatform:     "codex",
		Protocol:         ProtocolResponses,
		IsCompactRequest: true,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if decision.Action != ContinuityStrip {
		t.Fatalf("action = %q", decision.Action)
	}
	for _, k := range []string{"stream", "stream_options", "store", "previous_response_id"} {
		if _, ok := next[k]; ok {
			t.Fatalf("expected %q stripped", k)
		}
	}

	arr, ok := next["input"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("input = %#v", next["input"])
	}
	r := arr[0].(map[string]any)
	if r["encrypted_content"] != "enc_xyz" {
		t.Fatalf("encrypted_content dropped by compact: %v", r["encrypted_content"])
	}
	if r["content"] != "think" {
		t.Fatalf("content = %v", r["content"])
	}
	if _, ok := r["summary"]; !ok {
		t.Fatal("summary dropped by compact")
	}
}

func TestSanitizeResponsesRequestBody_ReasoningMissingError(t *testing.T) {
	body := map[string]any{
		"model": "gpt-5.4",
		"input": []any{
			map[string]any{"type": "reasoning"},
		},
	}
	_, _, err := SanitizeResponsesRequestBody(body, ContinuityPolicyInput{
		SitePlatform: "openai",
		Protocol:     ProtocolResponses,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*ReasoningInputError); !ok {
		t.Fatalf("err type = %T (%v)", err, err)
	}
}

func TestHasReasoningInputItems(t *testing.T) {
	if HasReasoningInputItems(nil) {
		t.Fatal("nil")
	}
	if HasReasoningInputItems(map[string]any{"input": "hi"}) {
		t.Fatal("string input")
	}
	if !HasReasoningInputItems(map[string]any{
		"input": []any{map[string]any{"type": "reasoning", "encrypted_content": "x"}},
	}) {
		t.Fatal("expected true")
	}
}

func TestSanitizeCompactResponsesRequestBody_DoesNotDropInput(t *testing.T) {
	body := map[string]any{
		"stream": true,
		"input": []any{
			map[string]any{
				"type":              "reasoning",
				"encrypted_content": "enc",
				"content":           "kept",
				"summary":           []any{map[string]any{"type": "summary_text", "text": "kept"}},
			},
		},
	}
	next := SanitizeCompactResponsesRequestBody(body, "codex")
	arr := next["input"].([]any)
	r := arr[0].(map[string]any)
	if r["encrypted_content"] != "enc" || r["content"] != "kept" {
		t.Fatalf("input mutated: %#v", r)
	}
}
