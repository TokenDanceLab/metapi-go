# Gemini tool-history `thought_signature`

Last updated: 2026-07-17

Issue: TokenDanceLab/metapi-go#47  
Upstream: cita-777/metapi#580 / #581

## Problem

Official Gemini (especially Gemini 3.x) rejects multi-turn tool history when model `functionCall` parts lack `thoughtSignature`:

```text
Function call is missing a thought_signature in functionCall parts.
```

OpenAI-compatible clients usually cannot carry Gemini provider metadata, so replaying `assistant.tool_calls` without a bridge causes HTTP 400 on official Gemini chat / native generateContent tool history.

## Implementation (metapi-go)

Primary surface: `transform/gemini/generate_content/compatibility.go`

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

## Round-trip

1. Upstream stream/final Gemini parts may include `thoughtSignature` on `functionCall`.
2. Aggregate state collects unique signatures (`GeminiAggregateState.ThoughtSignatures`).
3. Gemini→OpenAI conversion preserves them on tool_calls provider fields.
4. Next OpenAI→Gemini or native normalize re-attaches real signatures (or dummy when required and missing).

## Models without signature support / caveats

| Model class | Signature behavior |
|-------------|--------------------|
| `gemini-3*` (incl. 3.5) | Required on tool-history functionCall parts; dummy injected when real sig missing |
| `gemini-2.5*` with thinking/reasoning | Dummy injected when thinking enabled and real sig missing |
| `gemini-2.5*` without thinking | No injection (matches upstream #135 baseline) |
| Non-`gemini-*` IDs routed through this bridge | No dummy; thinking config may be disabled if signature missing |
| Third-party “Gemini-compatible” proxies | Dummy may be rejected; only official Gemini model IDs are treated as dummy-safe |

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

## Out of scope (this issue)

- Proxy executor routing (`gemini-native` path selection) — upstream #581 also touched request builders; this Go issue focuses on transform/request-side signature inject/preserve.
- `web/**` and other protocol issues (#52/#53/#54).
