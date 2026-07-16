package generate_content

import (
	"testing"
)

func collectFunctionCallParts(contents any) []map[string]any {
	var out []map[string]any
	switch arr := contents.(type) {
	case []map[string]any:
		for _, content := range arr {
			switch parts := content["parts"].(type) {
			case []map[string]any:
				for _, p := range parts {
					if _, ok := p["functionCall"]; ok {
						out = append(out, p)
					}
				}
			case []any:
				for _, raw := range parts {
					if p, ok := raw.(map[string]any); ok {
						if _, ok := p["functionCall"]; ok {
							out = append(out, p)
						}
					}
				}
			}
		}
	case []any:
		for _, item := range arr {
			content, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch parts := content["parts"].(type) {
			case []map[string]any:
				for _, p := range parts {
					if _, ok := p["functionCall"]; ok {
						out = append(out, p)
					}
				}
			case []any:
				for _, raw := range parts {
					if p, ok := raw.(map[string]any); ok {
						if _, ok := p["functionCall"]; ok {
							out = append(out, p)
						}
					}
				}
			}
		}
	}
	return out
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_PreservesProviderThoughtSignature(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-3-flash-preview",
		"messages": []any{
			map[string]any{"role": "user", "content": "What is the weather?"},
			map[string]any{
				"role":    "assistant",
				"content": "Let me check.",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_123",
						"type": "function",
						"function": map[string]any{
							"name":      "get_weather",
							"arguments": `{"city":"Tokyo"}`,
						},
						"provider_specific_fields": map[string]any{
							"thought_signature": "real_sig_abc",
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_123", "content": `{"temp":"22C"}`},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "gemini-3-flash-preview")
	fcParts := collectFunctionCallParts(result["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	if fcParts[0]["thoughtSignature"] != "real_sig_abc" {
		t.Fatalf("expected real thoughtSignature, got %v", fcParts[0]["thoughtSignature"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_SplitsSignedFunctionCallFromText(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-3-flash-preview",
		"messages": []any{
			map[string]any{"role": "user", "content": "Read the file."},
			map[string]any{
				"role":    "assistant",
				"content": "I will read it.",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_456",
						"type": "function",
						"function": map[string]any{
							"name":      "Read",
							"arguments": `{"path":"/tmp/x"}`,
						},
						"provider_specific_fields": map[string]any{
							"thought_signature": "sig_split_test",
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_456", "content": "file content here"},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "gemini-3-flash-preview")
	contents, ok := result["contents"].([]map[string]any)
	if !ok {
		t.Fatalf("contents type = %T", result["contents"])
	}
	var modelMsgs []map[string]any
	for _, c := range contents {
		if c["role"] == "model" {
			modelMsgs = append(modelMsgs, c)
		}
	}
	if len(modelMsgs) != 2 {
		t.Fatalf("expected 2 model messages (text + signed functionCall), got %d", len(modelMsgs))
	}
	firstParts, _ := modelMsgs[0]["parts"].([]map[string]any)
	secondParts, _ := modelMsgs[1]["parts"].([]map[string]any)
	if len(firstParts) == 0 || firstParts[0]["text"] == nil {
		t.Fatalf("first model message should be text-only, got %#v", firstParts)
	}
	if _, ok := secondParts[0]["functionCall"]; !ok {
		t.Fatalf("second model message should be functionCall, got %#v", secondParts)
	}
	if secondParts[0]["thoughtSignature"] != "sig_split_test" {
		t.Fatalf("expected split signature, got %v", secondParts[0]["thoughtSignature"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_InjectsDummyWhenThinkingEnabled(t *testing.T) {
	oaiBody := map[string]any{
		"model":            "gemini-3-flash-preview",
		"reasoning_effort": "high",
		"messages": []any{
			map[string]any{"role": "user", "content": "Do something."},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_no_sig",
						"type": "function",
						"function": map[string]any{
							"name":      "Bash",
							"arguments": `{"command":"ls"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_no_sig", "content": "file1\nfile2"},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "gemini-3-flash-preview")
	fcParts := collectFunctionCallParts(result["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	sig, _ := fcParts[0]["thoughtSignature"].(string)
	if sig == "" {
		t.Fatal("expected dummy thoughtSignature when thinking enabled")
	}
	if sig != DummyThoughtSignature {
		t.Fatalf("expected dummy sentinel, got %q", sig)
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_InjectsDummyForGemini3WithoutThinking(t *testing.T) {
	// Official Gemini 3.x rejects tool history without thought_signature even without explicit thinking.
	oaiBody := map[string]any{
		"model": "gemini-3.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "List files."},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_ls",
						"type": "function",
						"function": map[string]any{
							"name":      "ls",
							"arguments": `{"path":"/tmp"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_ls", "content": "file1\nfile2"},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "gemini-3.5-flash")
	fcParts := collectFunctionCallParts(result["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	if fcParts[0]["thoughtSignature"] != DummyThoughtSignature {
		t.Fatalf("expected dummy thoughtSignature for Gemini 3 tool history, got %v", fcParts[0]["thoughtSignature"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_NoDummyWhenThinkingDisabledOnGemini25(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-2.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_no_think",
						"type": "function",
						"function": map[string]any{
							"name":      "Read",
							"arguments": `{"path":"/x"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_no_think", "content": "data"},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "gemini-2.5-flash")
	fcParts := collectFunctionCallParts(result["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	if _, ok := fcParts[0]["thoughtSignature"]; ok {
		t.Fatalf("did not expect thoughtSignature when thinking disabled on gemini-2.5, got %v", fcParts[0]["thoughtSignature"])
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_NonGeminiNoDummyDisablesThinking(t *testing.T) {
	oaiBody := map[string]any{
		"model":            "claude-sonnet-4-5",
		"reasoning_effort": "high",
		"messages": []any{
			map[string]any{"role": "user", "content": "Do something."},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_no_sig_non_gemini",
						"type": "function",
						"function": map[string]any{
							"name":      "Bash",
							"arguments": `{"command":"ls"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_no_sig_non_gemini", "content": "file1\nfile2"},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "claude-sonnet-4-5")
	fcParts := collectFunctionCallParts(result["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	if _, ok := fcParts[0]["thoughtSignature"]; ok {
		t.Fatalf("did not expect dummy signature for non-gemini model, got %v", fcParts[0]["thoughtSignature"])
	}
	if gc, ok := result["generationConfig"].(map[string]any); ok {
		if _, hasTC := gc["thinkingConfig"]; hasTC {
			t.Fatalf("expected thinkingConfig disabled for non-gemini missing signature, got %#v", gc["thinkingConfig"])
		}
	}
}

func TestBuildGeminiGenerateContentRequestFromOpenAi_MultiTurnToolHistoryPreservesSignatures(t *testing.T) {
	oaiBody := map[string]any{
		"model": "gemini-3.5-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "Read two files."},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_a",
						"type": "function",
						"function": map[string]any{
							"name":      "Read",
							"arguments": `{"path":"/a"}`,
						},
						"provider_specific_fields": map[string]any{"thought_signature": "sig_a"},
					},
					map[string]any{
						"id":   "call_b",
						"type": "function",
						"function": map[string]any{
							"name":      "Read",
							"arguments": `{"path":"/b"}`,
						},
						"provider_specific_fields": map[string]any{"thought_signature": "sig_b"},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_a", "content": "content a"},
			map[string]any{"role": "tool", "tool_call_id": "call_b", "content": "content b"},
			map[string]any{"role": "user", "content": "Summarize them."},
		},
	}

	result := BuildGeminiGenerateContentRequestFromOpenAi(oaiBody, "gemini-3.5-flash")
	fcParts := collectFunctionCallParts(result["contents"])
	if len(fcParts) != 2 {
		t.Fatalf("expected 2 functionCall parts, got %d", len(fcParts))
	}
	sigs := map[string]bool{}
	for _, p := range fcParts {
		sig, _ := p["thoughtSignature"].(string)
		sigs[sig] = true
	}
	if !sigs["sig_a"] || !sigs["sig_b"] {
		t.Fatalf("expected multi-turn signatures preserved, got %#v", sigs)
	}
}

func TestBuildOpenAiBodyFromGeminiRequest_PreservesThoughtSignatureInProviderFields(t *testing.T) {
	geminiBody := map[string]any{
		"model": "gemini-3.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "Get weather"},
				},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"functionCall": map[string]any{
							"id":   "call_weather",
							"name": "get_weather",
							"args": map[string]any{"city": "SF"},
						},
						"thoughtSignature": "stream_sig_1",
					},
				},
			},
		},
	}

	oaiBody := BuildOpenAiBodyFromGeminiRequest(geminiBody, "")
	msgs, ok := oaiBody["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages type = %T", oaiBody["messages"])
	}
	var toolCalls []any
	for _, msg := range msgs {
		if tcs, ok := msg["tool_calls"].([]map[string]any); ok {
			for _, tc := range tcs {
				toolCalls = append(toolCalls, tc)
			}
		} else if tcs, ok := msg["tool_calls"].([]any); ok {
			toolCalls = append(toolCalls, tcs...)
		}
	}
	if len(toolCalls) != 1 {
		// BuildOpenAiBody uses []map[string]any for tool_calls.
		for _, msg := range msgs {
			if tcs, ok := msg["tool_calls"].([]map[string]any); ok {
				if len(tcs) != 1 {
					t.Fatalf("expected 1 tool_call, got %d in %#v", len(tcs), msgs)
				}
				psf, _ := tcs[0]["provider_specific_fields"].(map[string]any)
				if psf == nil || psf["thought_signature"] != "stream_sig_1" {
					t.Fatalf("expected preserved thought_signature, got %#v", tcs[0])
				}
				return
			}
		}
		t.Fatalf("tool_calls missing in %#v", msgs)
	}
}

func TestNormalizeRequest_InjectsDummyThoughtSignatureForGemini3ToolHistory(t *testing.T) {
	body := map[string]any{
		"model": "gemini-3.5-flash",
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{map[string]any{"text": "List files."}},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"functionCall": map[string]any{
							"name": "ls",
							"args": map[string]any{"path": "/tmp"},
						},
					},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"functionResponse": map[string]any{
							"name":     "ls",
							"response": map[string]any{"result": "a\nb"},
						},
					},
				},
			},
		},
	}

	normalized := NormalizeRequest(body, "gemini-3.5-flash")
	fcParts := collectFunctionCallParts(normalized["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	if fcParts[0]["thoughtSignature"] != DummyThoughtSignature {
		t.Fatalf("expected dummy signature injected into native contents, got %v", fcParts[0]["thoughtSignature"])
	}
}

func TestNormalizeRequest_PreservesExistingThoughtSignature(t *testing.T) {
	body := map[string]any{
		"model": "gemini-3.5-flash",
		"contents": []any{
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"functionCall": map[string]any{
							"name": "ls",
							"args": map[string]any{},
						},
						"thoughtSignature": "keep_me",
					},
				},
			},
		},
	}

	normalized := NormalizeRequest(body, "gemini-3.5-flash")
	fcParts := collectFunctionCallParts(normalized["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall part, got %d", len(fcParts))
	}
	if fcParts[0]["thoughtSignature"] != "keep_me" {
		t.Fatalf("expected existing signature preserved, got %v", fcParts[0]["thoughtSignature"])
	}
}

func TestStreamBridge_CollectsThoughtSignatures(t *testing.T) {
	sb := NewStreamBridge("gemini-3.5-flash")
	payload := map[string]any{
		"responseId": "resp_sig",
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role": "model",
					"parts": []any{
						map[string]any{
							"functionCall": map[string]any{
								"id":   "call_1",
								"name": "read",
								"args": map[string]any{"path": "/tmp"},
							},
							"thoughtSignature": "stream_sig_xyz",
						},
					},
				},
			},
		},
	}
	sb.NormalizeEvent(payload)
	if len(sb.State.ThoughtSignatures) != 1 || sb.State.ThoughtSignatures[0] != "stream_sig_xyz" {
		t.Fatalf("expected stream collected signature, got %#v", sb.State.ThoughtSignatures)
	}
}

func TestApplyThoughtSignaturesToFunctionCallParts_RoundTripFromAggregate(t *testing.T) {
	prior := &GeminiAggregateState{
		ThoughtSignatures: []string{"agg_sig_1"},
	}
	parts := []map[string]any{
		{
			"functionCall": map[string]any{
				"name": "read",
				"args": map[string]any{"path": "/tmp"},
			},
		},
	}
	signed := ApplyThoughtSignaturesToFunctionCallParts(parts, "gemini-3.5-flash", prior)
	if signed[0]["thoughtSignature"] != "agg_sig_1" {
		t.Fatalf("expected aggregate signature applied, got %v", signed[0]["thoughtSignature"])
	}

	// Follow-up request content built from aggregate signatures should not be rejectable.
	content := BuildSignedModelContentForToolHistory(parts, "gemini-3.5-flash", prior)
	if content["role"] != "model" {
		t.Fatalf("expected model role, got %v", content["role"])
	}
	contentParts, _ := content["parts"].([]map[string]any)
	if contentParts[0]["thoughtSignature"] != "agg_sig_1" {
		t.Fatalf("expected signed model content, got %#v", contentParts)
	}
}

func TestOpenAiGeminiToolHistoryRoundTrip_PreservesThoughtSignature(t *testing.T) {
	// Gemini response parts with signature -> OpenAI tool_calls provider fields -> next Gemini request.
	geminiBody := map[string]any{
		"model": "gemini-3.5-flash",
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "List files."}},
			},
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"functionCall": map[string]any{
							"id":   "call_read",
							"name": "read",
							"args": map[string]any{"path": "/tmp/a"},
						},
						"thoughtSignature": "roundtrip_sig",
					},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{
						"functionResponse": map[string]any{
							"id":       "call_read",
							"name":     "read",
							"response": map[string]any{"result": "file content"},
						},
					},
				},
			},
		},
	}

	oai := BuildOpenAiBodyFromGeminiRequest(geminiBody, "gemini-3.5-flash")
	// Ensure messages is []any for the reverse converter (which type-asserts []any).
	msgs, ok := oai["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages type = %T", oai["messages"])
	}
	asAny := make([]any, 0, len(msgs))
	for _, m := range msgs {
		// Normalize nested tool_calls to []any for reverse conversion.
		if tcs, ok := m["tool_calls"].([]map[string]any); ok {
			nested := make([]any, 0, len(tcs))
			for _, tc := range tcs {
				nested = append(nested, tc)
			}
			m["tool_calls"] = nested
		}
		asAny = append(asAny, m)
	}
	oai["messages"] = asAny
	// Append a follow-up user turn so this looks like a multi-turn tool-history request.
	oai["messages"] = append(asAny, map[string]any{"role": "user", "content": "Thanks, continue."})

	nextGemini := BuildGeminiGenerateContentRequestFromOpenAi(oai, "gemini-3.5-flash")
	fcParts := collectFunctionCallParts(nextGemini["contents"])
	if len(fcParts) != 1 {
		t.Fatalf("expected 1 functionCall on follow-up, got %d (%#v)", len(fcParts), nextGemini["contents"])
	}
	if fcParts[0]["thoughtSignature"] != "roundtrip_sig" {
		t.Fatalf("expected round-trip thoughtSignature, got %v", fcParts[0]["thoughtSignature"])
	}
}
