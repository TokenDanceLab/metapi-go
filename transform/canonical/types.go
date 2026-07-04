// Package canonical defines the protocol-agnostic intermediate representation
// used by MetAPI to convert between downstream client formats (OpenAI, Anthropic,
// Gemini) and upstream provider formats.
//
// All inter-provider conversion flows through the canonical types so that adding a
// new format only requires two bridges (to-canonical + from-canonical) instead of
// N*M direct conversions.
package canonical

// CanonicalOperation describes the logical operation the client is requesting.
type CanonicalOperation string

const (
	OpGenerate    CanonicalOperation = "generate"
	OpCountTokens CanonicalOperation = "count_tokens"
)

// CanonicalSurface identifies the downstream API surface the client hit.
type CanonicalSurface string

const (
	SurfaceOpenAIChat           CanonicalSurface = "openai-chat"
	SurfaceOpenAIResponses      CanonicalSurface = "openai-responses"
	SurfaceAnthropicMessages    CanonicalSurface = "anthropic-messages"
	SurfaceGeminiGenerateContent CanonicalSurface = "gemini-generate-content"
)

// CanonicalCliProfile identifies the downstream CLI client profile.
type CanonicalCliProfile string

const (
	ProfileGeneric   CanonicalCliProfile = "generic"
	ProfileCodex     CanonicalCliProfile = "codex"
	ProfileClaudeCode CanonicalCliProfile = "claude_code"
	ProfileGeminiCLI  CanonicalCliProfile = "gemini_cli"
)

// CanonicalMessageRole enumerates the valid message roles in canonical form.
type CanonicalMessageRole string

const (
	RoleSystem    CanonicalMessageRole = "system"
	RoleDeveloper CanonicalMessageRole = "developer"
	RoleUser      CanonicalMessageRole = "user"
	RoleAssistant CanonicalMessageRole = "assistant"
	RoleTool      CanonicalMessageRole = "tool"
)

// CanonicalContentPartType discriminates the 5 content-part subtypes.
type CanonicalContentPartType string

const (
	PartText       CanonicalContentPartType = "text"
	PartImage      CanonicalContentPartType = "image"
	PartFile       CanonicalContentPartType = "file"
	PartToolCall   CanonicalContentPartType = "tool_call"
	PartToolResult CanonicalContentPartType = "tool_result"
)

// CanonicalContentPart is a content block with a type discriminator.
// Fields are flat (no wrappers like image_url) — see spec.
type CanonicalContentPart struct {
	Type CanonicalContentPartType `json:"type"`

	// text
	Text    string `json:"text,omitempty"`
	Thought bool   `json:"thought,omitempty"`

	// image (flat, no image_url wrapper)
	DataURL  string  `json:"dataUrl,omitempty"`
	URL      string  `json:"url,omitempty"`
	MimeType *string `json:"mimeType,omitempty"`

	// file
	FileID   string `json:"fileId,omitempty"`
	FileURL  string `json:"fileUrl,omitempty"`
	FileData string `json:"fileData,omitempty"`
	Filename string `json:"filename,omitempty"`

	// tool_call
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	ArgumentsJSON string `json:"argumentsJson,omitempty"`

	// tool_result
	ToolCallID    string `json:"toolCallId,omitempty"`
	ResultText    string `json:"resultText,omitempty"`
	ResultJSON    any    `json:"resultJson,omitempty"`
	ResultContent any    `json:"resultContent,omitempty"`
}

// CanonicalMessage is a single message in the conversation.
type CanonicalMessage struct {
	Role               CanonicalMessageRole  `json:"role"`
	Parts              []CanonicalContentPart `json:"parts"`
	Phase              string                `json:"phase,omitempty"`
	ReasoningSignature string                `json:"reasoningSignature,omitempty"`
}

// CanonicalReasoningEffort defines the reasoning effort level.
type CanonicalReasoningEffort string

const (
	ReasoningEffortNone   CanonicalReasoningEffort = "none"
	ReasoningEffortLow    CanonicalReasoningEffort = "low"
	ReasoningEffortMedium CanonicalReasoningEffort = "medium"
	ReasoningEffortHigh   CanonicalReasoningEffort = "high"
	ReasoningEffortMax    CanonicalReasoningEffort = "max"
)

// CanonicalReasoningRequest carries reasoning/thinking configuration.
type CanonicalReasoningRequest struct {
	Effort                  CanonicalReasoningEffort `json:"effort,omitempty"`
	BudgetTokens            int                      `json:"budgetTokens,omitempty"`
	Summary                 string                   `json:"summary,omitempty"`
	IncludeEncryptedContent bool                     `json:"includeEncryptedContent,omitempty"`
}

// CanonicalToolChoice variants match the TS spec exactly.
//   - nil               -> not present
//   - "auto" | "none" | "required" -> string variants
//   - {Type:"tool", Name:"x"}     -> named tool
//   - {Type:"raw", Value: any}    -> passthrough raw value
type CanonicalToolChoice struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Value any    `json:"value,omitempty"`
}

// CanonicalFunctionTool is the standard function-calling tool shape.
type CanonicalFunctionTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// CanonicalRawTool carries non-OpenAI tools (e.g. Gemini googleSearch).
type CanonicalRawTool struct {
	Type string         `json:"type"`
	Raw  map[string]any `json:"raw"`
}

// CanonicalContinuation carries session-continuation metadata.
type CanonicalContinuation struct {
	SessionID          string `json:"sessionId,omitempty"`
	PreviousResponseID string `json:"previousResponseId,omitempty"`
	PromptCacheKey     string `json:"promptCacheKey,omitempty"`
	TurnState          string `json:"turnState,omitempty"`
}

// CanonicalAttachment describes a file attachment.
type CanonicalAttachment struct {
	Kind       string  `json:"kind"`
	SourceType string  `json:"sourceType,omitempty"`
	FileID     string  `json:"fileId,omitempty"`
	FileURL    string  `json:"fileUrl,omitempty"`
	FileData   string  `json:"fileData,omitempty"`
	Filename   string  `json:"filename,omitempty"`
	MimeType   *string `json:"mimeType,omitempty"`
}

// CanonicalRequestEnvelope is the protocol-agnostic request model.
// Named "Envelope" per spec — NOT "CanonicalRequest".
type CanonicalRequestEnvelope struct {
	Operation      CanonicalOperation         `json:"operation"`
	Surface        CanonicalSurface           `json:"surface"`
	CliProfile     CanonicalCliProfile        `json:"cliProfile"`
	RequestedModel string                     `json:"requestedModel"`
	Stream         bool                       `json:"stream"`
	Messages       []CanonicalMessage         `json:"messages"`
	Reasoning      *CanonicalReasoningRequest `json:"reasoning,omitempty"`
	Tools          []CanonicalToolItem        `json:"tools,omitempty"`
	ToolChoice     *CanonicalToolChoice       `json:"toolChoice,omitempty"`
	Continuation   *CanonicalContinuation     `json:"continuation,omitempty"`
	Metadata       map[string]any             `json:"metadata,omitempty"`
	Passthrough    map[string]any             `json:"passthrough,omitempty"`
	Attachments    []CanonicalAttachment      `json:"attachments,omitempty"`
}

// CanonicalToolItem is either a function tool or a raw tool.
// Discriminate: if Name is non-empty -> CanonicalFunctionTool; else CanonicalRawTool.
type CanonicalToolItem struct {
	// function tool fields
	FnName        string `json:"name,omitempty"`
	FnDescription string `json:"description,omitempty"`
	FnStrict      bool   `json:"strict,omitempty"`
	FnInputSchema map[string]any `json:"inputSchema,omitempty"`

	// raw tool fields
	RawType string         `json:"-"`
	RawData map[string]any `json:"raw,omitempty"`
}

// IsFunction returns true when this tool item represents a function tool (has a name).
func (t CanonicalToolItem) IsFunction() bool { return t.FnName != "" }

// IsRaw returns true when this tool item represents a raw/passthrough tool.
func (t CanonicalToolItem) IsRaw() bool { return t.FnName == "" && t.RawType != "" }
