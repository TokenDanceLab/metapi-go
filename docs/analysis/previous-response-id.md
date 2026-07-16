# `previous_response_id` continuity policy (#54 / upstream #504)

**Date:** 2026-07-17  
**Lane:** feature / FE-PROTOCOL  
**SSOT code:** `transform/openai/responses/previous_response_id.go`, `transform/openai/responses/compact.go`, `transform/canonical/openai_bridge.go` (`ApplyOpenAICompatibleContinuation` / `ApplyOpenAIResponsesContinuation`)

## Problem

Clients (notably Codex / Responses clients) send:

```json
{ "model": "...", "input": "...", "previous_response_id": "resp_..." }
```

Some upstreams accept the field (official OpenAI Responses, Azure OpenAI Responses, Codex). Many OpenAI-compatible relays and chat-only endpoints reject it with:

```text
HTTP 400: Unsupported parameter: previous_response_id
```

Before this fix:

1. Compact sanitize stripped `stream` / `stream_options` / conditional `store` but **not** `previous_response_id`.
2. Canonical → OpenAI **chat** body re-emitted `previous_response_id` via `ApplyOpenAICompatibleContinuation`.
3. Claude → OpenAI chat conversion copied `previous_response_id` onto chat payloads.
4. Multi-protocol path fallback could reuse a Responses JSON body against `/v1/chat/completions` with the field still present.

## Policy (forward / strip / reject)

| Target | Platform examples | Action | Reason |
| --- | --- | --- | --- |
| Responses | `openai`, `openai-responses`, `azure`, `azure-openai`, `codex` | **forward** | Official / Codex Responses continuity |
| Responses | `sub2api`, NewAPI-family, empty/unknown | **strip** (default) | Avoid opaque 400; field unsupported |
| Responses compact (`/v1/responses/compact`) | any | **strip** | Compact is not a stored-response continuation |
| Chat (`/v1/chat/completions`) | any | **strip** | Chat Completions has no Responses continuity |
| Messages (`/v1/messages`) | any | **strip** | Anthropic Messages has no `previous_response_id` |
| Any of the strip cases with `RequireContinuity=true` | — | **reject** | Clear client error instead of upstream 400 |

Default is **fail-open strip** when the field cannot be forwarded safely. Callers that must preserve multi-turn Responses state can set `RequireContinuity` and surface `ContinuityError` (clear `invalid_request_error` text).

### API surface (SSOT)

```go
// transform/openai/responses
ResolvePreviousResponseIDPolicy(input ContinuityPolicyInput) ContinuityDecision
ApplyPreviousResponseIDPolicy(body, input) (body, decision, error)
SanitizeResponsesRequestBody(body, input) (body, decision, error) // + compact stream/store sanitize
SupportsResponsesPreviousResponseID(sitePlatform string) bool
IsUnsupportedPreviousResponseIDError(rawErrText string) bool
StripPreviousResponseID(body) map[string]any
```

```go
// transform/canonical
ApplyOpenAICompatibleContinuation(...)  // chat-shaped: NO previous_response_id
ApplyOpenAIResponsesContinuation(...)   // responses-shaped: writes previous_response_id
```

## Compact / sanitize field matrix

| Field | Normal Responses | Compact Responses | Chat / Messages fallback |
| --- | --- | --- | --- |
| `stream` | pass-through (codex/sub2api may force stream outside compact) | **strip** | n/a (chat uses own stream) |
| `stream_options` | pass-through | **strip** | n/a |
| `store` | pass-through | **strip** for `codex` / `sub2api` only | n/a |
| `previous_response_id` | forward **or** strip per platform table | **always strip** | **always strip** |
| `prompt_cache_key` | pass-through | pass-through | pass-through (chat-compatible) |
| `model` / `input` / tools | pass-through | pass-through | protocol transform (separate) |

Helpers:

- `SanitizeCompactResponsesRequestBody` — compact-only field strip (includes `previous_response_id`).
- `SanitizeResponsesRequestBody` — continuity policy + optional compact strip in one call.

## Integration guidance (dispatch)

Per upstream attempt (after channel selection, once `site.platform` and target path are known):

1. Build attempt body (model swap, etc.).
2. Call `SanitizeResponsesRequestBody` with:
   - `SitePlatform = selected.Site.Platform`
   - `Protocol` / `UpstreamPath` for this candidate (`responses` / `chat` / `messages`)
   - `IsCompactRequest` when path ends with `/responses/compact`
3. On `ContinuityReject`, return **400** `invalid_request_error` with `ContinuityError.Message` (do not forward).
4. Optionally: if an upstream still returns `IsUnsupportedPreviousResponseIDError`, strip and retry once or fall through to the next protocol candidate **without** poisoning the channel.

Canonical chat builders must keep using `ApplyOpenAICompatibleContinuation` only; Responses builders use `ApplyOpenAIResponsesContinuation`.

## Acceptance mapping

| Criterion | Status |
| --- | --- |
| accepted / stripped / translated per upstream capabilities | Policy table + `ResolvePreviousResponseIDPolicy` / `ApplyPreviousResponseIDPolicy` |
| clear errors instead of opaque 400s | `ContinuityReject` + `ContinuityError`; chat no longer emits the field |
| compact/sanitize policy documented | This file + compact matrix |
| tests for presence / absence / strip | `transform/openai/responses/previous_response_id_test.go`, compact tests, canonical continuation tests |

## Residual

- Production multi-protocol dispatch should call `SanitizeResponsesRequestBody` **per candidate path** (same residual as #38 body transform). Until wired, library SSOT + chat emission strip still remove the most common opaque 400 paths (chat conversion + compact).
- No server-side store of prior Responses payloads: strip does not reconstruct history from `previous_response_id`.
- Translation of `previous_response_id` into Anthropic/Gemini session state is out of scope.
