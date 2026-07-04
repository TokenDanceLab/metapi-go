// Package shared provides types and utilities shared across all transformer providers.
package shared

// --- NormalizedStreamEvent ---

// ToolCallDelta represents an incremental tool call update in a stream.
type ToolCallDelta struct {
	Index          int    `json:"index"`
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ArgumentsDelta string `json:"argumentsDelta,omitempty"`
}

// NormalizedStreamEvent is the universal stream event type.
type NormalizedStreamEvent struct {
	Role                     string          `json:"role,omitempty"`
	ContentDelta             string          `json:"contentDelta,omitempty"`
	ReasoningDelta           string          `json:"reasoningDelta,omitempty"`
	ReasoningSignature       string          `json:"reasoningSignature,omitempty"`
	RedactedReasoningContent string          `json:"redactedReasoningContent,omitempty"`
	ToolCallDeltas           []ToolCallDelta `json:"toolCallDeltas,omitempty"`
	FinishReason             string          `json:"finishReason,omitempty"`
	Done                     bool            `json:"done,omitempty"`
}

// --- NormalizedFinalResponse ---

// ToolCall is a complete tool call in a final response.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// NormalizedFinalResponse is the universal final response type.
type NormalizedFinalResponse struct {
	ID                       string    `json:"id"`
	Model                    string    `json:"model"`
	Created                  int64     `json:"created"`
	Content                  string    `json:"content"`
	ReasoningContent         string    `json:"reasoningContent"`
	ReasoningSignature       string    `json:"reasoningSignature,omitempty"`
	RedactedReasoningContent string    `json:"redactedReasoningContent,omitempty"`
	FinishReason             string    `json:"finishReason"`
	ToolCalls                []ToolCall `json:"toolCalls"`
}

// --- NormalizedUsage ---

// NormalizedUsage has 11 counters per spec.
type NormalizedUsage struct {
	PromptTokens             int `json:"promptTokens"`
	CompletionTokens         int `json:"completionTokens"`
	TotalTokens              int `json:"totalTokens"`
	CachedTokens             int `json:"cachedTokens"`
	CacheReadTokens          int `json:"cacheReadTokens"`
	CacheCreationTokens      int `json:"cacheCreationTokens"`
	ReasoningTokens          int `json:"reasoningTokens"`
	AudioInputTokens         int `json:"audioInputTokens"`
	AudioOutputTokens        int `json:"audioOutputTokens"`
	AcceptedPredictionTokens int `json:"acceptedPredictionTokens"`
	RejectedPredictionTokens int `json:"rejectedPredictionTokens"`
}

// --- ParsedSseEvent ---

// ParsedSseEvent is a single parsed SSE event (event line + data line).
type ParsedSseEvent struct {
	Event string // event: value (empty for OpenAI)
	Data  string // data: value
}

// --- ThinkTagParserState ---

// ThinkTagParserState tracks streaming think-tag parse state.
type ThinkTagParserState struct {
	Mode    string // "content" | "reasoning"
	Pending string // buffered partial tag match
}

// CreateThinkTagParserState creates a new parser in content mode.
func CreateThinkTagParserState() *ThinkTagParserState {
	return &ThinkTagParserState{Mode: "content"}
}

// DownstreamFormat identifies the downstream serialization format.
type DownstreamFormat string

const (
	FormatOpenAI DownstreamFormat = "openai"
	FormatClaude DownstreamFormat = "claude"
)

// TextReasoning is the return type for text/reasoning split functions.
type TextReasoning struct {
	Content   string
	Reasoning string
}
