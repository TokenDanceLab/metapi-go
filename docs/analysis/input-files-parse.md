# Input Files Parse (`ParseInputFiles`)

**Date**: 2026-07-17  
**Issue**: [#228](https://github.com/TokenDanceLab/metapi-go/issues/228)  
**Lane**: p86 input_files parse

## Scope landed

`handler/proxy/input_files.go` no longer always returns `nil` from
`ParseInputFiles`. It walks OpenAI-shaped request bodies and extracts typed
file references.

| Surface | Behavior |
|---------|----------|
| `body["messages"]` | Chat Completions: each message `content` array is walked |
| `body["input"]` | Responses API: message items + direct content parts |
| Part types | `input_file`, `file` |
| Fields | `file_id` → `FileID`, `file_url` → `FileURL`, `filename` → `Filename` |
| Nested `file` object | `file.file_id` / `file.id`, `file.file_url` / `file.url`, `file.filename` / `file.name` |
| Empty / non-file parts | Ignored; function returns `nil` when nothing matched |

Aligned with `transform/canonical.canonicalAttachmentFromInputFileBlock` for
type/field names, but kept local to `handler/proxy` (no package import) so
proxy helpers stay dependency-light.

## Residuals / honest limits

1. **No `POST /v1/input_files` upload route** — TS MetAPI exposed a local
   owner-scoped input-file upload that inlined refs. Go does **not** add that
   route this wave. Durable file I/O goes through the existing **`/v1/files`**
   proxy surface (`docs/analysis/files-proxy.md`).
2. **`ResolveInputFile` remains a residual stub** — returns `(nil, nil)`.
   There is no multi-tenant local file vault, no disk dump of customer bytes,
   and no automatic fetch of `file_url` / decode of `file_data`. Callers that
   need content should use upstream `file_id` / `file_url` as-is or the
   `/v1/files` proxy.
3. **`file_data` is not materialized into `InputFile.Data`** — parse only
   surfaces identity fields (`FileID` / `FileURL` / `Filename`). Base64
   payloads stay in the original body for transform/upstream paths.
4. **No system-prompt attachment theater** — only `messages` and `input`
   trees are walked. Other body keys are ignored.
5. **No cross-protocol rewrite** — this helper extracts references; it does
   not convert Anthropic/Gemini file blocks or rewrite channel payloads.

## Tests

`handler/proxy/input_files_test.go`:

- empty / nil body → nil
- messages without file parts → nil
- nested message content with `file` + `input_file`
- top-level `input` array (nested content + direct part)
- nested `file` object identifiers
- empty typed parts / image_url ignored
- `ResolveInputFile` residual stub

Verify:

```bash
go test ./handler/proxy -count=1 -run InputFile
```

## Non-goals

- Inventing a local multi-tenant file store / TTL vault
- Adding `POST /v1/input_files` for API-compat theater
- Wiring `ParseInputFiles` into every chat/responses handler automatically
  (consumers can call it when a surface needs reference enumeration)
