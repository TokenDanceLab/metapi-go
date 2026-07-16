package responses

import (
	"fmt"
	"strings"
)

// PreviousResponseIDField is the OpenAI Responses continuity field name.
const PreviousResponseIDField = "previous_response_id"

// ContinuityAction describes how previous_response_id should be handled for an
// upstream attempt.
type ContinuityAction string

const (
	// ContinuityForward keeps previous_response_id on the upstream body.
	// Use when the target protocol is Responses and the platform is known to
	// accept continuity ids.
	ContinuityForward ContinuityAction = "forward"

	// ContinuityStrip removes previous_response_id without failing the request.
	// Use for chat/messages fallback, compact paths that cannot continue a
	// stored response, or Responses-shaped platforms that reject the field.
	ContinuityStrip ContinuityAction = "strip"

	// ContinuityReject returns a clear client error instead of forwarding a
	// body that would produce an opaque upstream 400.
	// Reserved for cases where the client requires continuity and no safe
	// strip/forward path exists.
	ContinuityReject ContinuityAction = "reject"
)

// UpstreamProtocol is the chat-family protocol used for one upstream attempt.
type UpstreamProtocol string

const (
	ProtocolResponses UpstreamProtocol = "responses"
	ProtocolChat      UpstreamProtocol = "chat"
	ProtocolMessages  UpstreamProtocol = "messages"
	ProtocolUnknown   UpstreamProtocol = "unknown"
)

// ContinuityPolicyInput selects how previous_response_id is handled.
type ContinuityPolicyInput struct {
	// SitePlatform is the selected site platform id (e.g. "openai", "codex").
	SitePlatform string
	// Protocol is the target upstream protocol for this attempt.
	Protocol UpstreamProtocol
	// UpstreamPath is optional; used when Protocol is empty to infer protocol.
	UpstreamPath string
	// IsCompactRequest is true for /v1/responses/compact (and aliases).
	IsCompactRequest bool
	// RequireContinuity forces ContinuityReject when the field cannot be
	// forwarded safely. Default false → strip instead of reject.
	RequireContinuity bool
}

// ContinuityDecision is the resolved action for previous_response_id.
type ContinuityDecision struct {
	Action ContinuityAction
	// Reason is a short machine-oriented explanation for logs/tests/docs.
	Reason string
	// ClientMessage is a clear error message when Action == ContinuityReject.
	ClientMessage string
}

// NormalizeUpstreamProtocol maps free-form protocol/path hints to a protocol.
func NormalizeUpstreamProtocol(protocol UpstreamProtocol, upstreamPath string) UpstreamProtocol {
	switch protocol {
	case ProtocolResponses, ProtocolChat, ProtocolMessages:
		return protocol
	}
	path := strings.ToLower(strings.TrimSpace(upstreamPath))
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimRight(path, "/")
	switch {
	case strings.HasSuffix(path, "/responses/compact") || path == "/responses/compact":
		return ProtocolResponses
	case strings.HasSuffix(path, "/v1/responses") || path == "/responses" || path == "/v1/responses" ||
		strings.Contains(path, "/responses/"):
		return ProtocolResponses
	case strings.HasSuffix(path, "/v1/chat/completions") || path == "/chat/completions" ||
		path == "/v1/chat/completions" || strings.Contains(path, "/chat/completions"):
		return ProtocolChat
	case strings.HasSuffix(path, "/v1/messages") || path == "/messages" || path == "/v1/messages" ||
		strings.Contains(path, "/messages"):
		return ProtocolMessages
	default:
		return ProtocolUnknown
	}
}

// SupportsResponsesPreviousResponseID reports whether a Responses-protocol
// upstream is expected to accept previous_response_id.
//
// Known-support (forward):
//   - openai / openai-responses / azure / azure-openai: official Responses API
//   - codex: Codex Responses surface uses continuity when store is available
//
// Known-reject / unknown (strip on Responses):
//   - sub2api and most NewAPI-family relays often proxy a reduced Responses
//     schema and return HTTP 400 "Unsupported parameter: previous_response_id"
//   - empty / unknown platforms: fail-open by stripping to avoid opaque 400s
//
// Chat and Messages never support the field (strip).
func SupportsResponsesPreviousResponseID(sitePlatform string) bool {
	n := strings.ToLower(strings.TrimSpace(sitePlatform))
	switch n {
	case "openai", "openai-responses", "azure", "azure-openai", "codex":
		return true
	default:
		return false
	}
}

// ResolvePreviousResponseIDPolicy decides forward / strip / reject.
func ResolvePreviousResponseIDPolicy(input ContinuityPolicyInput) ContinuityDecision {
	protocol := NormalizeUpstreamProtocol(input.Protocol, input.UpstreamPath)

	// Compact is a specialized Responses request. Continuity ids refer to a
	// prior stored response and are not meaningful on compact; strip cleanly.
	if input.IsCompactRequest || isCompactPath(input.UpstreamPath) {
		return ContinuityDecision{
			Action: ContinuityStrip,
			Reason: "compact_request_strips_previous_response_id",
		}
	}

	switch protocol {
	case ProtocolChat, ProtocolMessages:
		if input.RequireContinuity {
			return ContinuityDecision{
				Action: ContinuityReject,
				Reason: "chat_family_lacks_previous_response_id",
				ClientMessage: fmt.Sprintf(
					"previous_response_id is not supported on %s upstreams; use /v1/responses on a Responses-capable platform, or omit previous_response_id",
					protocol,
				),
			}
		}
		return ContinuityDecision{
			Action: ContinuityStrip,
			Reason: "chat_family_strips_previous_response_id",
		}
	case ProtocolResponses:
		if SupportsResponsesPreviousResponseID(input.SitePlatform) {
			return ContinuityDecision{
				Action: ContinuityForward,
				Reason: "responses_platform_supports_previous_response_id",
			}
		}
		if input.RequireContinuity {
			return ContinuityDecision{
				Action: ContinuityReject,
				Reason: "responses_platform_rejects_previous_response_id",
				ClientMessage: fmt.Sprintf(
					"previous_response_id is not supported by platform %q; omit previous_response_id or route to a Responses-capable platform (openai/azure/codex)",
					strings.TrimSpace(input.SitePlatform),
				),
			}
		}
		return ContinuityDecision{
			Action: ContinuityStrip,
			Reason: "responses_platform_strips_unsupported_previous_response_id",
		}
	default:
		// Unknown protocol: strip to avoid opaque upstream 400s.
		if input.RequireContinuity {
			return ContinuityDecision{
				Action:        ContinuityReject,
				Reason:        "unknown_protocol_previous_response_id",
				ClientMessage: "previous_response_id cannot be forwarded on this upstream protocol; use /v1/responses on a supported platform or omit previous_response_id",
			}
		}
		return ContinuityDecision{
			Action: ContinuityStrip,
			Reason: "unknown_protocol_strips_previous_response_id",
		}
	}
}

// ApplyPreviousResponseIDPolicy mutates a shallow copy of body according to
// the continuity policy. Returns the next body, the decision, and a non-nil
// error only when Action == ContinuityReject and the field was present.
func ApplyPreviousResponseIDPolicy(body map[string]any, input ContinuityPolicyInput) (map[string]any, ContinuityDecision, error) {
	decision := ResolvePreviousResponseIDPolicy(input)
	next := cloneBodyMap(body)
	id := previousResponseIDValue(next)
	// Absent or whitespace-only: never forward an empty continuity id.
	if id == "" {
		delete(next, PreviousResponseIDField)
		return next, decision, nil
	}
	switch decision.Action {
	case ContinuityForward:
		next[PreviousResponseIDField] = id
		return next, decision, nil
	case ContinuityStrip:
		delete(next, PreviousResponseIDField)
		return next, decision, nil
	case ContinuityReject:
		msg := decision.ClientMessage
		if msg == "" {
			msg = "previous_response_id is not supported on this upstream"
		}
		return next, decision, &ContinuityError{Message: msg, Reason: decision.Reason}
	default:
		delete(next, PreviousResponseIDField)
		return next, decision, nil
	}
}

// SanitizeResponsesRequestBody applies Responses request sanitization for one
// upstream attempt:
//  1. previous_response_id forward/strip/reject policy
//  2. multi-turn input reasoning items: preserve required content fields
//     (encrypted_content / summary) and ensure top-level content is present
//     (#50 / upstream #538)
//  3. when isCompact: also remove stream / stream_options / conditional store
//
// Callers that fall back from Responses → chat/messages should pass
// ProtocolChat or ProtocolMessages so continuity is stripped cleanly.
//
// Reasoning input sanitization runs for every protocol: chat/messages fallback
// still benefits from explicit missing-content errors and content preservation
// before any later protocol conversion.
func SanitizeResponsesRequestBody(body map[string]any, input ContinuityPolicyInput) (map[string]any, ContinuityDecision, error) {
	next, decision, err := ApplyPreviousResponseIDPolicy(body, input)
	if err != nil {
		return next, decision, err
	}
	next, err = applyResponsesInputSanitization(next)
	if err != nil {
		return next, decision, err
	}
	if input.IsCompactRequest || isCompactPath(input.UpstreamPath) {
		next = SanitizeCompactResponsesRequestBody(next, input.SitePlatform)
	}
	return next, decision, nil
}

// applyResponsesInputSanitization rewrites body["input"] on a shallow body copy.
// Compact field stripping must not drop reasoning content; this runs before
// compact sanitize so stream/store cleanup cannot touch input items.
func applyResponsesInputSanitization(body map[string]any) (map[string]any, error) {
	if body == nil {
		return map[string]any{}, nil
	}
	raw, ok := body["input"]
	if !ok {
		return body, nil
	}
	sanitized, err := SanitizeResponsesInputItems(raw)
	if err != nil {
		return body, err
	}
	// Always write sanitized input via a shallow body copy. Do not compare
	// sanitized == raw: []any is not comparable and panics.
	next := cloneBodyMap(body)
	next["input"] = sanitized
	return next, nil
}

// ContinuityError is a clear client-facing validation error for continuity
// policy rejects (avoids opaque upstream 400 "Unsupported parameter").
type ContinuityError struct {
	Message string
	Reason  string
}

func (e *ContinuityError) Error() string {
	if e == nil {
		return "continuity error"
	}
	if e.Message != "" {
		return e.Message
	}
	return "previous_response_id is not supported on this upstream"
}

// IsUnsupportedPreviousResponseIDError reports whether an upstream error text
// indicates rejection of previous_response_id (for retry/strip helpers).
func IsUnsupportedPreviousResponseIDError(rawErrText string) bool {
	text := strings.ToLower(strings.TrimSpace(rawErrText))
	if text == "" {
		return false
	}
	if !strings.Contains(text, "previous_response_id") {
		return false
	}
	return strings.Contains(text, "unsupported parameter") ||
		strings.Contains(text, "unknown parameter") ||
		strings.Contains(text, "unexpected keyword") ||
		strings.Contains(text, "not supported") ||
		strings.Contains(text, "unrecognized") ||
		strings.Contains(text, "invalid parameter")
}

// StripPreviousResponseID removes previous_response_id from a body copy.
func StripPreviousResponseID(body map[string]any) map[string]any {
	next := cloneBodyMap(body)
	delete(next, PreviousResponseIDField)
	return next
}

// HasPreviousResponseID reports whether body carries a non-empty id.
func HasPreviousResponseID(body map[string]any) bool {
	return hasPreviousResponseID(body)
}

func hasPreviousResponseID(body map[string]any) bool {
	return previousResponseIDValue(body) != ""
}

func previousResponseIDValue(body map[string]any) string {
	if body == nil {
		return ""
	}
	v, ok := body[PreviousResponseIDField]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func isCompactPath(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	return strings.HasSuffix(p, "/responses/compact") || p == "/responses/compact"
}

func cloneBodyMap(body map[string]any) map[string]any {
	next := map[string]any{}
	if body == nil {
		return next
	}
	for k, v := range body {
		next[k] = v
	}
	return next
}
