package messages

import (
	"encoding/json"
	"testing"
)

// Claude Code Skill multi-turn history often arrives as OpenAI tool_calls + tool results.
// Upstream #531 / issue #51: bridge must preserve tool_use id/name/input and tool_result linkage.
func TestConvertOpenAIToAnthropic_SkillCallMultiTurn(t *testing.T) {
	body := map[string]any{
		"model": "claude-sonnet-4",
		"messages": []any{
			map[string]any{"role": "user", "content": "use Skill"},
			map[string]any{
				"role": "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_skill_1",
						"type": "function",
						"function": map[string]any{
							"name":      "Skill",
							"arguments": `{"skill":"read_file","path":"README.md"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_skill_1",
				"content":      "file contents here",
			},
			map[string]any{"role": "user", "content": "summarize it"},
		},
	}

	out, err := ConvertOpenAiBodyToAnthropicMessagesBody(body, "claude-sonnet-4", false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	// Normalize via JSON so message slices are []any regardless of internal types.
	var normalized map[string]any
	if err := json.Unmarshal([]byte(mustJSON(out)), &normalized); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	msgs, _ := normalized["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("expected multi-turn messages, got %d: %s", len(msgs), mustJSON(out))
	}

	// Find assistant tool_use for Skill
	foundToolUse := false
	foundToolResult := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		role, _ := mm["role"].(string)
		content, _ := mm["content"].([]any)
		for _, c := range content {
			cm, _ := c.(map[string]any)
			switch cm["type"] {
			case "tool_use":
				if cm["name"] == "Skill" && cm["id"] == "call_skill_1" {
					foundToolUse = true
					input, _ := cm["input"].(map[string]any)
					if input["skill"] != "read_file" {
						t.Errorf("skill input not preserved: %#v", input)
					}
				}
			case "tool_result":
				if cm["tool_use_id"] == "call_skill_1" {
					foundToolResult = true
				}
			}
		}
		_ = role
	}
	if !foundToolUse {
		t.Fatalf("missing Skill tool_use in %s", mustJSON(msgs))
	}
	if !foundToolResult {
		t.Fatalf("missing tool_result linked to call_skill_1 in %s", mustJSON(msgs))
	}
}

func TestConvertOpenAITools_PreservesSkillParameters(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Skill",
				"description": "Invoke a skill",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"skill": map[string]any{"type": "string"},
					},
					"required": []any{"skill"},
				},
			},
		},
	}
	body := map[string]any{
		"model":    "claude-sonnet-4",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":    tools,
	}
	out, err := ConvertOpenAiBodyToAnthropicMessagesBody(body, "claude-sonnet-4", false)
	if err != nil {
		t.Fatal(err)
	}
	atools, _ := out["tools"].([]any)
	if len(atools) != 1 {
		t.Fatalf("tools=%s", mustJSON(out["tools"]))
	}
	tm, _ := atools[0].(map[string]any)
	if tm["name"] != "Skill" {
		t.Fatalf("name=%v", tm["name"])
	}
	schema, _ := tm["input_schema"].(map[string]any)
	if schema == nil {
		t.Fatalf("missing input_schema: %s", mustJSON(tm))
	}
	if _, ok := schema["required"]; !ok {
		t.Fatalf("required not preserved: %s", mustJSON(schema))
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
