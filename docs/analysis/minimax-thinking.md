# MiniMax thinking / reasoning separation

Last updated: 2026-07-17

## Issue

- GitHub: #52
- Upstream: cita-777/metapi #511
- Symptom: MiniMax assistant `content` includes raw think open/close tags (or orphan close) so clients render reasoning as user-visible text.

## Upstream response shapes

1. **OpenAI native (default)** — reasoning embedded in `message.content`:
   - Paired think tags + answer
   - Orphan close (common in playground / some final payloads): reasoning text, then close tag, then answer
2. **OpenAI `reasoning_split=true`** — `message.reasoning_details[].text` + clean `content`
3. **Explicit fields** — `reasoning_content` / `reasoning` when present
4. **Stream** — tags arrive in `choices[].delta.content` chunks; may split mid-tag

## Product contract (metapi-go)

When transforming upstream to normalized/OpenAI client response:

| Field | Rule |
|-------|------|
| `content` | Visible assistant text only; no raw think open/close tags |
| `reasoning_content` | Extracted thinking / `reasoning_details` text |
| Stream deltas | Same split; `finish_reason` flushes pending think-parser buffer |

Notes:

- Non-stream orphan close (no open tag) is fully supported.
- Stream multi-chunk orphan without a prior open tag cannot reclassify already-emitted SSE content; official MiniMax stream uses paired tags or `reasoning_details`.

## Implementation

Package `transform/shared`:

- `ExtractInlineThinkTags` — non-stream; paired + orphan close
- `ConsumeThinkTaggedText` / `FlushThinkTaggedText` — stream; progressive emit + partial-tag buffer + finish flush
- `ExtractReasoningDetailsText` — MiniMax `reasoning_details`
- `NormalizeUpstreamFinalResponse` / `NormalizeUpstreamStreamEvent` — OpenAI chat choices path
- `SerializeFinalResponse` — writes `reasoning_content` when present

## Fixtures

- `transform/shared/think_stream_test.go` — progressive stream parser + orphan close unit tests
- `transform/shared/shared_test.go` — MiniMax final/stream normalize + serialize fixtures

## Verification

```bash
go test ./transform/shared/ ./transform/openai/chat/ -count=1
```
