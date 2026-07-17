# Gemini tool-history `thought_signature`

Last updated: 2026-07-17

Issue: TokenDanceLab/metapi-go#47 / #309  
Upstream: cita-777/metapi#580 / #581

## Problem

Official Gemini (especially Gemini 3.x) rejects multi-turn tool history when model `functionCall` parts lack `thoughtSignature`:

```text
Function call is missing a thought_signature in functionCall parts.
```

OpenAI-compatible clients usually cannot carry Gemini provider metadata, so replaying `assistant.tool_calls` without a bridge causes HTTP 400 on official Gemini chat / native generateContent tool history.

## Implementation (metapi-go)

### Transform surface

Primary: `transform/gemini/generate_content/compatibility.go`

| Concern | Behavior |
|--------|----------|
| Dummy sentinel | `DummyThoughtSignature` = base64(`skip_thought_signature_validator`) = `c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I=` |
| Safe models | `gemini-*` / `models/gemini-*` only |
| Required models | Gemini 3.x (`gemini-3…`) always require signature on tool-history `functionCall` parts, even without explicit thinking config |
| Thinking-enabled | For Gemini 2.5-class models, inject dummy only when thinking/reasoning is enabled |
| Non-Gemini | Never inject dummy; if thinking would otherwise be enabled and signature is missing, drop derived `thinkingConfig` |
| Preserve real sig | Prefer `provider_specific_fields.thought_signature` / part `thoughtSignature` over dummy |
| Split contents | When a model turn has both text and signed `functionCall` parts, emit separate `contents` entries (text first, then functionCall) |
| Native request | `NormalizeRequest` injects missing signatures into native Gemini `contents` for Gemini 3 / thinking-safe models |
| Stream collect | `StreamBridge.NormalizeEvent` records unique `ThoughtSignatures` into `GeminiAggregateState` |
| Next-turn inject | `ApplyThoughtSignaturesToFunctionCallParts` / `BuildSignedModelContentForToolHistory` re-attach aggregate signatures for follow-up requests |
| OpenAI bridge | Gemini→OpenAI stores signature under `tool_calls[].provider_specific_fields.thought_signature`; OpenAI→Gemini restores it |

### Proxy runtime wire (#309)

Live path: `handler/proxy/upstream.go` `sanitizeUpstreamJSONBody` (after model swap, per candidate path).

| Gate | Behavior |
|------|----------|
| Platform | `gemini` / `gemini-cli` / `google` only |
| Path | native `*generateContent*` / `*streamGenerateContent*` or `/v1internal*` (CLI) |
| Body markers | only when `"functionCall"` / `"function_call"` / `"tool_calls"` present (cheap skip) |
| Native `contents` | `generate_content.NormalizeRequest(body, model)` |
| CLI envelope | `{ request: { contents… } }` — normalize inner request |
| OpenAI `messages` on native path | `BuildGeminiGenerateContentRequestFromOpenAi` |
| Model source | upstream actual model → body/request model → path-parsed model |

OpenAI-compat `/v1/chat/completions` on Gemini OpenAI-compat bases is **not** rewritten (stays OpenAI-shaped; no native `thoughtSignature` field).

## Round-trip

1. Upstream stream/final Gemini parts may include `thoughtSignature` on `functionCall`.
2. Aggregate state collects unique signatures (`GeminiAggregateState.ThoughtSignatures`) inside the transform `StreamBridge` (test/helpers; not a multi-instance session store).
3. Gemini→OpenAI conversion preserves them on tool_calls provider fields.
4. Next OpenAI→Gemini or native normalize (via proxy sanitize or direct transform) re-attaches real signatures (or dummy when required and missing).

## Models without signature support / caveats

| Model class | Signature behavior |
|-------------|--------------------|
| `gemini-3*` (incl. 3.5) | Required on tool-history functionCall parts; dummy injected when real sig missing |
| `gemini-2.5*` with thinking/reasoning | Dummy injected when thinking enabled and real sig missing |
| `gemini-2.5*` without thinking | No injection (matches upstream #135 baseline) |
| Non-`gemini-*` IDs routed through this bridge | No dummy; thinking config may be disabled if signature missing |
| Third-party “Gemini-compatible” proxies | Dummy may be rejected; only official Gemini model IDs are treated as dummy-safe |

## Residual (honest)

- **No multi-instance / Redis session store** for aggregate `ThoughtSignatures`. Process-local stream aggregate helpers exist for tests and future session glue; live proxy does **not** re-attach prior-turn aggregate state across requests. Clients must echo `provider_specific_fields.thought_signature` or send native contents that `NormalizeRequest` can patch (dummy for Gemini 3 when missing).
- Full Responses WS and Redis sticky remain out of scope.
- OpenAI-compat chat path on Gemini is passthrough (no native inject).

## Tests

`transform/gemini/generate_content/thought_signature_test.go` covers:

- Preserve real provider signatures
- Text/functionCall split for signed parts
- Dummy inject for thinking-enabled and Gemini 3 without thinking
- No dummy for Gemini 2.5 without thinking / non-Gemini
- Multi-turn preservation
- Native `NormalizeRequest` inject/preserve
- Stream aggregate collection
- Aggregate → next request round-trip
- OpenAI↔Gemini tool-history signature round-trip

`handler/proxy/gemini_thought_signature_test.go` covers:

- Proxy sanitize injects dummy for Gemini 3 tool history on generateContent
- Preserves real signatures
- gemini-cli request envelope inject
- OpenAI messages → native rebuild with provider sig
- Non-gemini platform / no tool-history skip

## Out of scope

- Redis sticky / multi-instance aggregate re-attach
- Full Responses WebSocket
- `web/**` and unrelated protocol issues
