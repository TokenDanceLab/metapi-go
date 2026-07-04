package canonical

import "strings"

// CreateCanonicalRequestEnvelopeInput mirrors the TS CreateCanonicalRequestEnvelopeInput.
type CreateCanonicalRequestEnvelopeInput struct {
	Operation      CanonicalOperation
	Surface        CanonicalSurface
	CliProfile     CanonicalCliProfile
	RequestedModel string
	Stream         bool
	Messages       []CanonicalMessage
	Reasoning      *CanonicalReasoningRequest
	Tools          []CanonicalToolItem
	ToolChoice     *CanonicalToolChoice
	Continuation   *CanonicalContinuation
	Metadata       map[string]any
	Passthrough    map[string]any
	Attachments    []CanonicalAttachment
}

// CreateCanonicalRequestEnvelope builds a CanonicalRequestEnvelope from its input.
// Mirrors TS createCanonicalRequestEnvelope exactly.
func CreateCanonicalRequestEnvelope(input CreateCanonicalRequestEnvelopeInput) (CanonicalRequestEnvelope, error) {
	requestedModel := strings.TrimSpace(input.RequestedModel)
	if requestedModel == "" {
		return CanonicalRequestEnvelope{}, ErrMissingModel
	}

	operation := input.Operation
	if operation == "" {
		operation = OpGenerate
	}

	cliProfile := input.CliProfile
	if cliProfile == "" {
		cliProfile = ProfileGeneric
	}

	envelope := CanonicalRequestEnvelope{
		Operation:      operation,
		Surface:        input.Surface,
		CliProfile:     cliProfile,
		RequestedModel: requestedModel,
		Stream:         input.Stream,
		Messages:       input.Messages,
	}

	if input.Reasoning != nil {
		envelope.Reasoning = input.Reasoning
	}
	if len(input.Tools) > 0 {
		envelope.Tools = input.Tools
	}
	if input.ToolChoice != nil {
		envelope.ToolChoice = input.ToolChoice
	}
	normalizedCont := NormalizeCanonicalContinuation(input.Continuation)
	if normalizedCont != nil {
		envelope.Continuation = normalizedCont
	}
	if len(input.Metadata) > 0 {
		envelope.Metadata = input.Metadata
	}
	if len(input.Passthrough) > 0 {
		envelope.Passthrough = input.Passthrough
	}
	if len(input.Attachments) > 0 {
		envelope.Attachments = cloneAttachments(input.Attachments)
	}

	return envelope, nil
}

// NormalizeCanonicalContinuation returns nil if the continuation has no meaningful fields.
func NormalizeCanonicalContinuation(c *CanonicalContinuation) *CanonicalContinuation {
	if c == nil {
		return nil
	}
	trimmed := CanonicalContinuation{
		SessionID:          strings.TrimSpace(c.SessionID),
		PreviousResponseID: strings.TrimSpace(c.PreviousResponseID),
		PromptCacheKey:     strings.TrimSpace(c.PromptCacheKey),
		TurnState:          strings.TrimSpace(c.TurnState),
	}
	if trimmed.SessionID == "" && trimmed.PreviousResponseID == "" &&
		trimmed.PromptCacheKey == "" && trimmed.TurnState == "" {
		return nil
	}
	return &trimmed
}

// ErrMissingModel is returned when requestedModel is empty.
var ErrMissingModel = &envelopeError{"canonical request requires requestedModel"}

type envelopeError struct{ msg string }

func (e *envelopeError) Error() string { return e.msg }

func cloneAttachments(src []CanonicalAttachment) []CanonicalAttachment {
	dst := make([]CanonicalAttachment, len(src))
	copy(dst, src)
	return dst
}
