package canonical

import (
	"encoding/json"
	"testing"
)

// --- Test data (golden file fixtures) ---

func sampleEnvelopeJSON() string {
	return `{"operation":"generate","surface":"openai-chat","cliProfile":"generic","requestedModel":"gpt-4","stream":true,"messages":[{"role":"system","parts":[{"type":"text","text":"You are a helpful assistant."}]},{"role":"user","parts":[{"type":"text","text":"Hello!"}]},{"role":"assistant","parts":[{"type":"text","text":"Hi there! How can I help?"}]}],"reasoning":{"effort":"medium","budgetTokens":4096,"includeEncryptedContent":true},"tools":[{"name":"get_weather","description":"Get the weather for a city","strict":true,"inputSchema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"toolChoice":{"type":"auto"},"continuation":{"sessionId":"sess_abc123","previousResponseId":"resp_xyz789","promptCacheKey":"cache_key_42"}}`
}

func sampleEnvelopeWithImagesJSON() string {
	return `{"operation":"generate","surface":"openai-chat","cliProfile":"generic","requestedModel":"gpt-4o","stream":false,"messages":[{"role":"user","parts":[{"type":"text","text":"What is in this image?"},{"type":"image","dataUrl":"data:image/png;base64,iVBORw0KGgo=","mimeType":"image/png"}]}]}`
}

func sampleEnvelopeWithToolCallsJSON() string {
	return `{"operation":"generate","surface":"anthropic-messages","cliProfile":"claude_code","requestedModel":"claude-sonnet-4-20250514","stream":false,"messages":[{"role":"assistant","parts":[{"type":"tool_call","id":"toolu_01","name":"get_weather","argumentsJson":"{\"city\":\"San Francisco\"}"}]},{"role":"tool","parts":[{"type":"tool_result","toolCallId":"toolu_01","resultText":"Sunny, 72F"}]}]}`
}

func sampleEnvelopeWithAttachmentsJSON() string {
	mt := "application/pdf"
	att := CanonicalAttachment{
		Kind:       "file",
		SourceType: "file",
		FileID:     "file-abc",
		Filename:   "report.pdf",
		MimeType:   &mt,
	}
	env := CanonicalRequestEnvelope{
		Operation:      OpGenerate,
		Surface:        SurfaceOpenAIChat,
		CliProfile:     ProfileGeneric,
		RequestedModel: "gpt-4",
		Stream:         false,
		Messages: []CanonicalMessage{
			{Role: RoleUser, Parts: []CanonicalContentPart{{Type: PartText, Text: "Summarize this file."}}},
		},
		Attachments: []CanonicalAttachment{att},
	}
	b, _ := json.Marshal(env)
	return string(b)
}

// --- Roundtrip: JSON serialize -> deserialize ---

func TestCanonicalRequestEnvelope_JSONRoundtrip(t *testing.T) {
	raw := sampleEnvelopeJSON()

	var env CanonicalRequestEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal canonical envelope: %v", err)
	}

	// Verify fields
	if env.Operation != OpGenerate {
		t.Errorf("expected operation %q, got %q", OpGenerate, env.Operation)
	}
	if env.Surface != SurfaceOpenAIChat {
		t.Errorf("expected surface %q, got %q", SurfaceOpenAIChat, env.Surface)
	}
	if env.CliProfile != ProfileGeneric {
		t.Errorf("expected cliProfile %q, got %q", ProfileGeneric, env.CliProfile)
	}
	if env.RequestedModel != "gpt-4" {
		t.Errorf("expected model %q, got %q", "gpt-4", env.RequestedModel)
	}
	if !env.Stream {
		t.Error("expected stream=true")
	}
	if len(env.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(env.Messages))
	}
	if env.Messages[0].Role != RoleSystem {
		t.Errorf("expected system role, got %q", env.Messages[0].Role)
	}
	if env.Messages[1].Role != RoleUser {
		t.Errorf("expected user role, got %q", env.Messages[1].Role)
	}
	if env.Messages[2].Role != RoleAssistant {
		t.Errorf("expected assistant role, got %q", env.Messages[2].Role)
	}
	if env.Reasoning == nil {
		t.Fatal("expected reasoning, got nil")
	}
	if env.Reasoning.Effort != ReasoningEffortMedium {
		t.Errorf("expected reasoning effort medium, got %q", env.Reasoning.Effort)
	}
	if env.Reasoning.BudgetTokens != 4096 {
		t.Errorf("expected budgetTokens 4096, got %d", env.Reasoning.BudgetTokens)
	}
	if len(env.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(env.Tools))
	}
	if !env.Tools[0].IsFunction() {
		t.Error("expected function tool")
	}
	if env.Tools[0].FnName != "get_weather" {
		t.Errorf("expected tool name get_weather, got %q", env.Tools[0].FnName)
	}
	if env.ToolChoice == nil {
		t.Fatal("expected toolChoice, got nil")
	}
	if env.ToolChoice.Type != "auto" {
		t.Errorf("expected tool_choice type auto, got %q", env.ToolChoice.Type)
	}
	if env.Continuation == nil {
		t.Fatal("expected continuation, got nil")
	}
	if env.Continuation.SessionID != "sess_abc123" {
		t.Errorf("expected sessionId sess_abc123, got %q", env.Continuation.SessionID)
	}
	if env.Continuation.PreviousResponseID != "resp_xyz789" {
		t.Errorf("expected previousResponseId resp_xyz789, got %q", env.Continuation.PreviousResponseID)
	}
	if env.Continuation.PromptCacheKey != "cache_key_42" {
		t.Errorf("expected promptCacheKey cache_key_42, got %q", env.Continuation.PromptCacheKey)
	}

	// Re-serialize and compare
	reSerialized, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Roundtrip: unmarshal re-serialized again
	var env2 CanonicalRequestEnvelope
	if err := json.Unmarshal(reSerialized, &env2); err != nil {
		t.Fatalf("unmarshal re-serialized: %v", err)
	}
	if env2.Operation != env.Operation {
		t.Error("roundtrip: operation mismatch")
	}
	if env2.RequestedModel != env.RequestedModel {
		t.Error("roundtrip: model mismatch")
	}
	if len(env2.Messages) != len(env.Messages) {
		t.Error("roundtrip: messages count mismatch")
	}
	if env2.Reasoning == nil || env.Reasoning == nil || env2.Reasoning.Effort != env.Reasoning.Effort {
		t.Error("roundtrip: reasoning mismatch")
	}
}

// --- Golden file: images roundtrip ---

func TestCanonicalRequestEnvelope_ImagesRoundtrip(t *testing.T) {
	raw := sampleEnvelopeWithImagesJSON()

	var env CanonicalRequestEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(env.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(env.Messages))
	}
	if len(env.Messages[0].Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(env.Messages[0].Parts))
	}
	if env.Messages[0].Parts[0].Type != PartText {
		t.Errorf("expected first part text, got %q", env.Messages[0].Parts[0].Type)
	}
	if env.Messages[0].Parts[1].Type != PartImage {
		t.Errorf("expected second part image, got %q", env.Messages[0].Parts[1].Type)
	}
	if env.Messages[0].Parts[1].DataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Errorf("unexpected dataUrl: %q", env.Messages[0].Parts[1].DataURL)
	}

	// Re-serialize
	reSerialized, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var env2 CanonicalRequestEnvelope
	if err := json.Unmarshal(reSerialized, &env2); err != nil {
		t.Fatalf("unmarshal re-serialized: %v", err)
	}
	if len(env2.Messages[0].Parts) != 2 {
		t.Fatal("roundtrip: parts count mismatch")
	}
}

// --- Golden file: tool calls roundtrip ---

func TestCanonicalRequestEnvelope_ToolCallsRoundtrip(t *testing.T) {
	raw := sampleEnvelopeWithToolCallsJSON()

	var env CanonicalRequestEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(env.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(env.Messages))
	}
	if env.Messages[0].Role != RoleAssistant {
		t.Errorf("expected assistant role, got %q", env.Messages[0].Role)
	}
	if len(env.Messages[0].Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(env.Messages[0].Parts))
	}
	if env.Messages[0].Parts[0].Type != PartToolCall {
		t.Errorf("expected tool_call, got %q", env.Messages[0].Parts[0].Type)
	}
	if env.Messages[0].Parts[0].ID != "toolu_01" {
		t.Errorf("expected tool call id toolu_01, got %q", env.Messages[0].Parts[0].ID)
	}
	if env.Messages[0].Parts[0].Name != "get_weather" {
		t.Errorf("expected name get_weather, got %q", env.Messages[0].Parts[0].Name)
	}
	if env.Messages[0].Parts[0].ArgumentsJSON != `{"city":"San Francisco"}` {
		t.Errorf("unexpected argumentsJson: %q", env.Messages[0].Parts[0].ArgumentsJSON)
	}

	if env.Messages[1].Role != RoleTool {
		t.Errorf("expected tool role, got %q", env.Messages[1].Role)
	}
	if len(env.Messages[1].Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(env.Messages[1].Parts))
	}
	if env.Messages[1].Parts[0].Type != PartToolResult {
		t.Errorf("expected tool_result, got %q", env.Messages[1].Parts[0].Type)
	}
	if env.Messages[1].Parts[0].ToolCallID != "toolu_01" {
		t.Errorf("expected toolCallId toolu_01, got %q", env.Messages[1].Parts[0].ToolCallID)
	}
	if env.Messages[1].Parts[0].ResultText != "Sunny, 72F" {
		t.Errorf("unexpected resultText: %q", env.Messages[1].Parts[0].ResultText)
	}
}

// --- Test: attachments roundtrip ---

func TestCanonicalRequestEnvelope_AttachmentsRoundtrip(t *testing.T) {
	raw := sampleEnvelopeWithAttachmentsJSON()

	var env CanonicalRequestEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(env.Attachments))
	}
	if env.Attachments[0].Kind != "file" {
		t.Errorf("expected kind file, got %q", env.Attachments[0].Kind)
	}
	if env.Attachments[0].FileID != "file-abc" {
		t.Errorf("expected fileId file-abc, got %q", env.Attachments[0].FileID)
	}
}

// --- Envelope factory tests ---

func TestCreateCanonicalRequestEnvelope_Success(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		Operation:      OpGenerate,
		Surface:        SurfaceOpenAIChat,
		CliProfile:     ProfileGeneric,
		RequestedModel: "gpt-4",
		Stream:         true,
		Messages: []CanonicalMessage{
			{Role: RoleUser, Parts: []CanonicalContentPart{{Type: PartText, Text: "Hello"}}},
		},
	}

	env, err := CreateCanonicalRequestEnvelope(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.RequestedModel != "gpt-4" {
		t.Errorf("expected model gpt-4, got %q", env.RequestedModel)
	}
	if env.Operation != OpGenerate {
		t.Errorf("expected operation generate, got %q", env.Operation)
	}
	if !env.Stream {
		t.Error("expected stream=true")
	}
}

func TestCreateCanonicalRequestEnvelope_MissingModel(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		Operation:  OpGenerate,
		Surface:    SurfaceOpenAIChat,
		Messages:   []CanonicalMessage{},
	}

	_, err := CreateCanonicalRequestEnvelope(input)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if err != ErrMissingModel {
		t.Errorf("expected ErrMissingModel, got %v", err)
	}
}

func TestCreateCanonicalRequestEnvelope_EmptyModel(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		RequestedModel: "   ",
	}
	_, err := CreateCanonicalRequestEnvelope(input)
	if err == nil {
		t.Fatal("expected error for whitespace-only model")
	}
}

func TestCreateCanonicalRequestEnvelope_DefaultOperation(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		RequestedModel: "gpt-4",
	}
	env, err := CreateCanonicalRequestEnvelope(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Operation != OpGenerate {
		t.Errorf("expected default operation generate, got %q", env.Operation)
	}
}

func TestCreateCanonicalRequestEnvelope_DefaultCliProfile(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		RequestedModel: "gpt-4",
	}
	env, err := CreateCanonicalRequestEnvelope(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.CliProfile != ProfileGeneric {
		t.Errorf("expected default profile generic, got %q", env.CliProfile)
	}
}

func TestCreateCanonicalRequestEnvelope_NilContinuation(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		RequestedModel: "gpt-4",
		Continuation:   nil,
	}
	env, err := CreateCanonicalRequestEnvelope(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Continuation != nil {
		t.Error("expected nil continuation for nil input")
	}
}

func TestCreateCanonicalRequestEnvelope_EmptyContinuation(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		RequestedModel: "gpt-4",
		Continuation:   &CanonicalContinuation{},
	}
	env, err := CreateCanonicalRequestEnvelope(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Continuation != nil {
		t.Error("expected nil continuation for empty fields")
	}
}

func TestCreateCanonicalRequestEnvelope_ContinuationWithSession(t *testing.T) {
	input := CreateCanonicalRequestEnvelopeInput{
		RequestedModel: "gpt-4",
		Continuation: &CanonicalContinuation{
			SessionID: "sess_123",
		},
	}
	env, err := CreateCanonicalRequestEnvelope(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Continuation == nil {
		t.Fatal("expected non-nil continuation")
	}
	if env.Continuation.SessionID != "sess_123" {
		t.Errorf("expected sessionId sess_123, got %q", env.Continuation.SessionID)
	}
}

// --- NormalizeCanonicalContinuation tests ---

func TestNormalizeCanonicalContinuation_Nil(t *testing.T) {
	if got := NormalizeCanonicalContinuation(nil); got != nil {
		t.Error("expected nil for nil input")
	}
}

func TestNormalizeCanonicalContinuation_AllEmpty(t *testing.T) {
	if got := NormalizeCanonicalContinuation(&CanonicalContinuation{}); got != nil {
		t.Error("expected nil for all-empty continuation")
	}
}

func TestNormalizeCanonicalContinuation_WhitespaceOnly(t *testing.T) {
	if got := NormalizeCanonicalContinuation(&CanonicalContinuation{SessionID: "   "}); got != nil {
		t.Error("expected nil for whitespace-only continuation")
	}
}

// --- CanonicalToolItem tests ---

func TestCanonicalToolItem_IsFunction(t *testing.T) {
	tool := CanonicalToolItem{FnName: "get_weather"}
	if !tool.IsFunction() {
		t.Error("expected IsFunction=true")
	}
}

func TestCanonicalToolItem_IsFunction_Empty(t *testing.T) {
	tool := CanonicalToolItem{}
	if tool.IsFunction() {
		t.Error("expected IsFunction=false for empty")
	}
}

func TestCanonicalToolItem_IsRaw(t *testing.T) {
	tool := CanonicalToolItem{RawType: "google_search"}
	if !tool.IsRaw() {
		t.Error("expected IsRaw=true")
	}
}

func TestCanonicalToolItem_IsRaw_NoType(t *testing.T) {
	tool := CanonicalToolItem{}
	if tool.IsRaw() {
		t.Error("expected IsRaw=false for empty")
	}
}

func TestCanonicalToolItem_IsRaw_FunctionTakesPriority(t *testing.T) {
	tool := CanonicalToolItem{FnName: "get_weather", RawType: "google_search"}
	if tool.IsFunction() != true {
		t.Error("expected IsFunction=true")
	}
	if tool.IsRaw() {
		t.Error("expected IsRaw=false when function present")
	}
}

// --- CanonicalContentPart field coverage ---

func TestCanonicalContentPart_AllTypes(t *testing.T) {
	types := []CanonicalContentPartType{PartText, PartImage, PartFile, PartToolCall, PartToolResult}
	for _, pt := range types {
		p := CanonicalContentPart{Type: pt}
		b, err := json.Marshal(p)
		if err != nil {
			t.Errorf("marshal %q: %v", pt, err)
			continue
		}
		var p2 CanonicalContentPart
		if err := json.Unmarshal(b, &p2); err != nil {
			t.Errorf("unmarshal %q: %v", pt, err)
			continue
		}
		if p2.Type != pt {
			t.Errorf("roundtrip %q: expected %q, got %q", pt, pt, p2.Type)
		}
	}
}

func TestCanonicalMessageRole_AllRoles(t *testing.T) {
	roles := []CanonicalMessageRole{RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool}
	for _, r := range roles {
		msg := CanonicalMessage{Role: r}
		b, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("marshal %q: %v", r, err)
			continue
		}
		var msg2 CanonicalMessage
		if err := json.Unmarshal(b, &msg2); err != nil {
			t.Errorf("unmarshal %q: %v", r, err)
			continue
		}
		if msg2.Role != r {
			t.Errorf("roundtrip role %q: expected %q, got %q", r, r, msg2.Role)
		}
	}
}
