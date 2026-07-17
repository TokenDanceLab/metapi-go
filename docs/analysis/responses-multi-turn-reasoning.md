# Multi-turn `/v1/responses` reasoning content (#50 / upstream #538 / #310)

**Date:** 2026-07-17  
**Lane:** feature / FE-PROTOCOL / residual honesty  
**SSOT code:** `transform/openai/responses/reasoning_input.go`, `transform/openai/responses/compact.go`, `transform/openai/responses/previous_response_id.go` (`SanitizeResponsesRequestBody`), `handler/proxy/upstream.go` (`sanitizeUpstreamJSONBody`), `transform/shared/chatFormatsCore.go` (`parseResponsesReasoning`)

## Problem

Hermes/Codex clients (`api_mode: codex_responses`) replay prior Responses `output` into the next turn's `input`:

```json
{
  "model": "gpt-5.4",
  "input": [
    { "type": "message", "role": "user", "content": [...] },
    {
      "type": "reasoning",
      "id": "rs_...",
      "encrypted_content": "...",
      "summary": [{ "type": "summary_text", "text": "..." }]
    },
    { "type": "message", "role": "assistant", "content": "" },
    { "type": "function_call", "call_id": "...", "name": "...", "arguments": "..." },
    { "type": "function_call_output", "call_id": "...", "output": "..." }
  ]
}
```

Official OpenAI Responses accepts reasoning items with `encrypted_content` / `summary` and **no** top-level `content`. Some OpenAI-compatible gateways (and intermediate normalizers) validate every `input[n]` as if it were a message and return:

```text
Missing required parameter: 'input[1].content'
```

Upstream issue: [cita-777/metapi#538](https://github.com/cita-777/metapi/issues/538). Direct Hermes → upstream works; the proxy path must not drop reasoning payload fields and must make strict validators happy.

## Policy

| Item type | Action |
| --- | --- |
| `reasoning` with `encrypted_content` and/or non-empty summary/content | **preserve** all fields; ensure top-level `content` is present (`summary` text, else existing content, else `""` when only encrypted) |
| `reasoning` with empty summary, no content, no encrypted_content | **reject** with `ReasoningInputError` (explicit client message, not opaque upstream 400) |
| `message` with empty `content` | **preserve** empty string/array (tool-call assistant turns) |
| `function_call` / `function_call_output` / custom tool items | **pass-through** (no invented `content`) |
| Compact sanitize (`stream` / `store` / `previous_response_id`) | **must not touch** `input` / reasoning fields |

### API surface (SSOT)

```go
// transform/openai/responses
SanitizeResponsesInputItems(input any) (any, error)
SanitizeResponsesRequestBody(body, ContinuityPolicyInput) (body, ContinuityDecision, error)
//  → continuity policy + input reasoning sanitize + optional compact strip
HasReasoningInputItems(body map[string]any) bool

type ReasoningInputError struct { Message string; Index int; Reason string }
```

```go
// transform/shared
parseResponsesReasoning(m) // reads output[] or input[]; flattens summary/content; keeps encrypted_content as signature
```

### Dispatch wiring

`handler/proxy/upstream.go` `sanitizeUpstreamJSONBody` cheap-gates on:

- `"previous_response_id"`
- compact path (`/responses/compact`)
- `"type":"reasoning"` / `"type": "reasoning"` / `"encrypted_content"`
- **Responses path + `"input"`** — catches pretty-printed / spaced JSON where type markers are not contiguous bytes (#310)

then calls `SanitizeResponsesRequestBody` so second-turn reasoning bodies are rewritten even without continuity ids.

`ReasoningInputError` / `ContinuityError` map to **HTTP 400** `invalid_request_error` (no silent accept).

## Acceptance mapping

| Criterion | Status |
| --- | --- |
| Multi-turn Hermes/Codex second turn accepts prior reasoning with required content | `SanitizeResponsesInputItems` injects/preserves `content`; keeps `encrypted_content` + `summary` |
| Compact/sanitize does not drop required reasoning content | Compact only strips stream/store/previous_response_id; input sanitized before compact |
| Fixture covers reasoning item round-trip | `reasoning_input_test.go`, shared parse fixtures, `upstream_test.go` wire tests |
| Explicit error when client omits required fields | `ReasoningInputError` with `input[n]` index and required field list → 400 |
| Pretty-printed / spaced type markers still sanitize | Responses path + `"input"` cheap gate (#310) |

## Residual (honest)

| Item | Status |
|------|--------|
| Inject `content` on reasoning items for strict gateway validators | **present** (`SanitizeResponsesInputItems` + wire tests) |
| Preserve `encrypted_content` / `summary` through compact | **present** |
| Explicit 400 when reasoning has no content/summary/encrypted | **present** (`ReasoningInputError` → 400) |
| Pretty-printed / spaced JSON type markers | **present** (Responses path + `"input"` gate, #310) |
| Full Responses → chat conversion of reasoning items | **residual** — Go multi-protocol fallback still relies on passthrough + continuity strip (#54); no invent of chat-shaped reasoning |
| Server-side store of prior Responses payloads | **residual** — clients must replay items or use `previous_response_id` on supported platforms |
| Full Responses WebSocket multi-turn Codex path | **residual** — 426/501 only; see `responses-websocket-residual.md` (not this issue) |

## Verification

```bash
go test ./transform/openai/responses/ ./transform/shared/ ./handler/proxy -count=1 -run 'Response|Sanitize|Reasoning'
```
