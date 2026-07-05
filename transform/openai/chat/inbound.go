// Package chat provides OpenAI Chat Completions API transformer.
package chat

import (
	"github.com/tokendancelab/metapi-go/transform/canonical"
	"github.com/tokendancelab/metapi-go/transform/shared"
)

// Inbound parses and validates an OpenAI chat request body.
func Inbound(body any) (*shared.ParsedDownstreamChatRequest, *shared.ErrorPayload) {
	return shared.ParseDownstreamChatRequest(body, shared.FormatOpenAI)
}

// ParseToCanonical converts an OpenAI chat request to canonical form.
func ParseToCanonical(body any, cliProfile canonical.CanonicalCliProfile, operation canonical.CanonicalOperation, metadata, passthrough map[string]any, continuation *canonical.CanonicalContinuation) (canonical.CanonicalRequestEnvelope, error) {
	parsed, err := Inbound(body)
	if err != nil {
		return canonical.CanonicalRequestEnvelope{}, err
	}
	return canonical.CanonicalRequestFromOpenAiBody(canonical.CanonicalRequestFromOpenAiBodyInput{
		Body:         parsed.UpstreamBody,
		Surface:      canonical.SurfaceOpenAIChat,
		CliProfile:   cliProfile,
		Operation:    operation,
		Metadata:     metadata,
		Passthrough:  passthrough,
		Continuation: continuation,
	})
}

// BuildUpstreamBody converts canonical to OpenAI chat body.
func BuildUpstreamBody(req canonical.CanonicalRequestEnvelope) map[string]any {
	return canonical.CanonicalRequestToOpenAiChatBody(req)
}

// StreamBridge provides SSE stream processing for OpenAI chat.
type StreamBridge struct {
	Ctx *shared.StreamTransformContext
}

// NewStreamBridge creates a new stream bridge.
func NewStreamBridge(modelName string) *StreamBridge {
	return &StreamBridge{Ctx: shared.CreateStreamTransformContext(modelName)}
}

// NormalizeEvent normalizes an upstream event.
func (sb *StreamBridge) NormalizeEvent(payload any) shared.NormalizedStreamEvent {
	return shared.NormalizeUpstreamStreamEvent(payload, sb.Ctx, sb.Ctx.Model)
}

// SerializeEvent serializes a normalized event to downstream SSE lines.
func (sb *StreamBridge) SerializeEvent(event shared.NormalizedStreamEvent, cc *shared.ClaudeDownstreamContext) []string {
	if cc == nil {
		cc = shared.CreateClaudeDownstreamContext()
	}
	_ = cc // may be updated inside SerializeEvent branches
	// OpenAI chat multi-choice handling: use direct serialization
	chunk := buildOpenAIStreamChunk(sb.Ctx, event)
	if chunk == nil {
		return nil
	}
	return []string{shared.SerializeSSE("", chunk)}
}

// SerializeDone writes the [DONE] marker.
func (sb *StreamBridge) SerializeDone(cc *shared.ClaudeDownstreamContext) []string {
	if cc == nil {
		cc = shared.CreateClaudeDownstreamContext()
	}
	_ = cc // may be updated inside SerializeEvent branches
	return shared.SerializeStreamDone(shared.FormatOpenAI, sb.Ctx, cc)
}

// PullSseEvents extracts SSE events from a buffer.
func PullSseEvents(buffer string) ([]shared.ParsedSseEvent, string) {
	return shared.PullSseEventsWithDone(buffer)
}

func buildOpenAIStreamChunk(ctx *shared.StreamTransformContext, event shared.NormalizedStreamEvent) map[string]any {
	delta := map[string]any{}
	isInitial := !ctx.RoleSent && event.Role == "assistant" && event.ContentDelta == "" && event.ReasoningDelta == ""

	if !ctx.RoleSent && (event.Role == "assistant" || event.ContentDelta != "" || event.ReasoningDelta != "") {
		delta["role"] = "assistant"
		ctx.RoleSent = true
	} else if event.Role == "assistant" {
		delta["role"] = "assistant"
		ctx.RoleSent = true
	}
	if event.ContentDelta != "" {
		delta["content"] = event.ContentDelta
	}
	if event.ReasoningDelta != "" {
		delta["reasoning_content"] = event.ReasoningDelta
	}
	if event.ReasoningSignature != "" {
		delta["reasoning_signature"] = event.ReasoningSignature
	}
	if len(event.ToolCallDeltas) > 0 {
		var tcs []map[string]any
		for _, td := range event.ToolCallDeltas {
			idx := td.Index
			if idx < 0 {
				idx = 0
			}
			existing := ctx.ToolCalls[idx]
			if existing == nil {
				existing = &shared.ToolCallAccumulator{}
				ctx.ToolCalls[idx] = existing
			}
			if td.ID != "" {
				existing.ID = td.ID
			}
			if td.Name != "" {
				existing.Name = td.Name
			}
			existing.Arguments += td.ArgumentsDelta
			fn := map[string]any{}
			if td.Name != "" {
				fn["name"] = td.Name
			}
			if td.ArgumentsDelta != "" {
				fn["arguments"] = td.ArgumentsDelta
			}
			stc := map[string]any{"index": idx}
			if td.ID != "" {
				stc["id"] = td.ID
			}
			if td.ID != "" || td.Name != "" {
				stc["type"] = "function"
			}
			if len(fn) > 0 {
				stc["function"] = fn
			}
			tcs = append(tcs, stc)
		}
		if len(tcs) > 0 {
			delta["tool_calls"] = tcs
		}
	}
	if isInitial {
		delta["content"] = ""
	}
	fr := event.FinishReason
	hasDelta := len(delta) > 0
	if !hasDelta && fr == "" {
		return nil
	}
	return map[string]any{
		"id":      ctx.ID,
		"object":  "chat.completion.chunk",
		"created": ctx.Created,
		"model":   ctx.Model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": fr}},
	}
}
