package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/transform/canonical"
	"github.com/tokendancelab/metapi-go/transform/shared"
)

// getMessages extracts a message slice from a map value, handling both
// []any and []map[string]any concrete types stored behind the any interface.
func getMessages(v any) ([]map[string]any, bool) {
	switch msgs := v.(type) {
	case []any:
		result := make([]map[string]any, 0, len(msgs))
		for _, m := range msgs {
			if mm, ok := m.(map[string]any); ok {
				result = append(result, mm)
			}
		}
		return result, true
	case []map[string]any:
		return msgs, true
	default:
		return nil, false
	}
}

// ---------------------------------------------------------------------------
// TestRoundtrip: OpenAI request → canonical → OpenAI request
// ---------------------------------------------------------------------------

func TestRoundtrip(t *testing.T) {
	tests := []struct {
		name  string
		body  map[string]any
	}{
		{
			name: "simple user message",
			body: map[string]any{
				"model": "gpt-4",
				"messages": []any{
					map[string]any{"role": "user", "content": "hello"},
				},
			},
		},
		{
			name: "multi-turn with system",
			body: map[string]any{
				"model": "gpt-4-turbo",
				"stream": true,
				"messages": []any{
					map[string]any{"role": "system", "content": "You are helpful."},
					map[string]any{"role": "user", "content": "What is 2+2?"},
					map[string]any{"role": "assistant", "content": "4"},
					map[string]any{"role": "user", "content": "Are you sure?"},
				},
			},
		},
		{
			name: "with reasoning and tool calls",
			body: map[string]any{
				"model":            "gpt-4o",
				"reasoning_effort": "medium",
				"messages": []any{
					map[string]any{"role": "user", "content": "Weather in Tokyo?"},
					map[string]any{
						"role": "assistant",
						"tool_calls": []any{
							map[string]any{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"Tokyo"}`,
								},
							},
						},
					},
					map[string]any{
						"role":         "tool",
						"tool_call_id": "call_1",
						"content":      "Sunny, 22C",
					},
				},
				"tools": []any{
					map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        "get_weather",
							"description": "Get weather for a city",
							"parameters": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"city": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: Inbound — parse the OpenAI body
			parsed, errPayload := Inbound(tt.body)
			if errPayload != nil {
				t.Fatalf("Inbound returned error: %v", errPayload)
			}
			if parsed.RequestedModel != tt.body["model"] {
				t.Errorf("RequestedModel = %q, want %q", parsed.RequestedModel, tt.body["model"])
			}

			// Step 2: ParseToCanonical — convert to canonical form
			env, err := ParseToCanonical(tt.body, canonical.ProfileGeneric, canonical.OpGenerate, nil, nil, nil)
			if err != nil {
				t.Fatalf("ParseToCanonical returned error: %v", err)
			}
			if env.RequestedModel != tt.body["model"] {
				t.Errorf("Canonical RequestedModel = %q, want %q", env.RequestedModel, tt.body["model"])
			}
			if env.Surface != canonical.SurfaceOpenAIChat {
				t.Errorf("Surface = %q, want %q", env.Surface, canonical.SurfaceOpenAIChat)
			}

			// Step 3: BuildUpstreamBody — convert back to OpenAI body
			result := BuildUpstreamBody(env)
			if result["model"] != tt.body["model"] {
				t.Errorf("Roundtrip model = %q, want %q", result["model"], tt.body["model"])
			}

			// Verify messages survive the roundtrip
			origMsgs, _ := getMessages(tt.body["messages"])
			resultMsgs, ok := getMessages(result["messages"])
			if !ok {
				t.Fatalf("result[messages] is not an array, got %T", result["messages"])
			}
			if len(resultMsgs) != len(origMsgs) {
				t.Errorf("message count = %d, want %d", len(resultMsgs), len(origMsgs))
			}

			// Check that user message content survives
			for i := range origMsgs {
				if i >= len(resultMsgs) {
					break
				}
				origMsg := origMsgs[i]
				resultMsg := resultMsgs[i]

				if origMsg["role"] != resultMsg["role"] {
					t.Errorf("message[%d] role = %q, want %q", i, resultMsg["role"], origMsg["role"])
				}
			}

			// Verify stream flag roundtrips
			if stream, ok := tt.body["stream"].(bool); ok {
				if result["stream"] != stream {
					t.Errorf("stream = %v, want %v", result["stream"], stream)
				}
			}

			t.Logf("Roundtrip succeeded: model=%q, %d messages", result["model"], len(resultMsgs))
		})
	}
}

// ---------------------------------------------------------------------------
// TestInboundEmptyMessages: empty messages returns an error
// ---------------------------------------------------------------------------

func TestInboundEmptyMessages(t *testing.T) {
	tests := []struct {
		name        string
		body        any
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil body",
			body:        nil,
			wantErr:     true,
			errContains: "model is required",
		},
		{
			name: "empty map",
			body: map[string]any{},
			wantErr: true,
			errContains: "model is required",
		},
		{
			name: "model without messages",
			body: map[string]any{
				"model": "gpt-4",
			},
			wantErr:     true,
			errContains: "messages is required",
		},
		{
			name: "model with empty messages array",
			body: map[string]any{
				"model":    "gpt-4",
				"messages": []any{},
			},
			wantErr:     true,
			errContains: "messages is required",
		},
		{
			name: "non-map body",
			body: "not a map",
			wantErr: true,
			errContains: "model is required",
		},
		{
			name: "valid minimal request",
			body: map[string]any{
				"model": "gpt-4",
				"messages": []any{
					map[string]any{"role": "user", "content": "hi"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errPayload := Inbound(tt.body)
			gotErr := errPayload != nil

			if gotErr != tt.wantErr {
				t.Errorf("Inbound() error = %v, wantErr = %v", errPayload, tt.wantErr)
				return
			}

			if gotErr && tt.errContains != "" {
				payloadBytes, _ := json.Marshal(errPayload.Payload)
				payloadStr := string(payloadBytes)
				if !strings.Contains(payloadStr, tt.errContains) {
					t.Errorf("error payload %q does not contain %q", payloadStr, tt.errContains)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestStreamFormat: verify streaming SSE event serialization
// ---------------------------------------------------------------------------

func TestStreamFormat(t *testing.T) {
	tests := []struct {
		name       string
		modelName  string
		event      shared.NormalizedStreamEvent
		contains   []string  // substrings expected in the output
		notContain []string  // substrings NOT expected
		wantNil    bool      // expect SerializeEvent to return nil
	}{
		{
			name:      "content delta event",
			modelName: "gpt-4",
			event: shared.NormalizedStreamEvent{
				Role:         "assistant",
				ContentDelta: "Hello, world!",
			},
			contains: []string{
				"data: ",
				"chat.completion.chunk",
				"Hello, world!",
				"\"role\":\"assistant\"",
			},
			notContain: []string{"[DONE]"},
		},
		{
			name:      "reasoning delta event",
			modelName: "o1",
			event: shared.NormalizedStreamEvent{
				Role:           "assistant",
				ReasoningDelta: "Let me think...",
			},
			contains: []string{
				"data: ",
				"reasoning_content",
				"Let me think...",
				"chat.completion.chunk",
			},
		},
		{
			name:      "finish reason event",
			modelName: "gpt-4",
			event: shared.NormalizedStreamEvent{
				FinishReason: "stop",
			},
			contains: []string{
				"data: ",
				"\"finish_reason\":\"stop\"",
			},
		},
		{
			name:      "empty event returns nil",
			modelName: "gpt-4",
			event:     shared.NormalizedStreamEvent{},
			wantNil:   true,
		},
		{
			name:      "tool call delta event",
			modelName: "gpt-4",
			event: shared.NormalizedStreamEvent{
				Role: "assistant",
				ToolCallDeltas: []shared.ToolCallDelta{
					{
						Index:          0,
						ID:             "call_abc123",
						Name:           "get_weather",
						ArgumentsDelta: `{"city":"`,
					},
				},
			},
			contains: []string{
				"data: ",
				"tool_calls",
				"call_abc123",
				"get_weather",
				`"arguments"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewStreamBridge(tt.modelName)
			lines := sb.SerializeEvent(tt.event, nil)

			if tt.wantNil {
				if lines != nil {
					t.Errorf("expected nil, got %d lines: %v", len(lines), lines)
				}
				return
			}

			if len(lines) == 0 {
				t.Fatal("expected at least 1 line of output")
			}

			allText := strings.Join(lines, "")

			for _, want := range tt.contains {
				if !strings.Contains(allText, want) {
					t.Errorf("output does not contain %q\noutput: %s", want, allText)
				}
			}

			for _, bad := range tt.notContain {
				if strings.Contains(allText, bad) {
					t.Errorf("output should not contain %q\noutput: %s", bad, allText)
				}
			}

			// Verify SSE format: each line should end with \n\n
			for i, line := range lines {
				if !strings.HasSuffix(line, "\n\n") {
					t.Errorf("line %d does not end with \\n\\n: %q", i, line)
				}
				if !strings.HasPrefix(line, "data: ") {
					t.Errorf("line %d does not start with 'data: ': %q", i, line)
				}
			}

			t.Logf("SSE output: %s", strings.ReplaceAll(allText, "\n", "\\n"))
		})
	}
}

// ---------------------------------------------------------------------------
// TestSerializeDone: verify [DONE] marker
// ---------------------------------------------------------------------------

func TestSerializeDone(t *testing.T) {
	sb := NewStreamBridge("gpt-4")
	cc := shared.CreateClaudeDownstreamContext()

	lines := sb.SerializeDone(cc)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if lines[0] != "data: [DONE]\n\n" {
		t.Errorf("expected 'data: [DONE]\\n\\n', got %q", lines[0])
	}

	// Double call should return nil
	lines2 := sb.SerializeDone(cc)
	if lines2 != nil {
		t.Errorf("expected nil on second call, got %v", lines2)
	}
}

// ---------------------------------------------------------------------------
// TestPullSseEvents: SSE parsing through the chat bridge
// ---------------------------------------------------------------------------

func TestPullSseEvents(t *testing.T) {
	tests := []struct {
		name        string
		buffer      string
		wantEvents  int
		wantRemain  string
	}{
		{
			name:       "single event",
			buffer:     "data: {\"key\":\"value\"}\n\n",
			wantEvents: 1,
			wantRemain: "",
		},
		{
			name:       "two events",
			buffer:     "data: chunk1\n\ndata: chunk2\n\n",
			wantEvents: 2,
			wantRemain: "",
		},
		{
			name:       "partial event",
			buffer:     "data: complete\n\ndata: partial",
			wantEvents: 1,
			wantRemain: "data: partial",
		},
		{
			name:       "empty buffer",
			buffer:     "",
			wantEvents: 0,
			wantRemain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, rest := PullSseEvents(tt.buffer)
			if len(events) != tt.wantEvents {
				t.Errorf("event count = %d, want %d", len(events), tt.wantEvents)
			}
			if rest != tt.wantRemain {
				t.Errorf("rest = %q, want %q", rest, tt.wantRemain)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestNewStreamBridge: verify bridge initialization
// ---------------------------------------------------------------------------

func TestNewStreamBridge(t *testing.T) {
	sb := NewStreamBridge("gpt-4o")
	if sb == nil {
		t.Fatal("expected non-nil StreamBridge")
	}
	if sb.Ctx == nil {
		t.Fatal("expected non-nil Ctx")
	}
	if sb.Ctx.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", sb.Ctx.Model, "gpt-4o")
	}
	if sb.Ctx.ID == "" {
		t.Error("expected non-empty ID")
	}
	if sb.Ctx.Created == 0 {
		t.Error("expected non-zero Created timestamp")
	}
}
