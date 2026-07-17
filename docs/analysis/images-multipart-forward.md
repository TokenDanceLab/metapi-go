# Images Multipart Forward (`POST /v1/images/edits`)

**Date**: 2026-07-17  
**Issue**: [#207](https://github.com/TokenDanceLab/metapi-go/issues/207)  
**Lane**: p84 images multipart

## Scope landed

`HandleImagesEdits` no longer short-circuits multipart with a fake
`https://example.com/edited-image.png` 200 body.

| Path | Behavior |
|------|----------|
| `POST /v1/images/edits` (JSON) | `PrepareCtx` → `dispatchUpstream` (unchanged) |
| `POST /v1/images/edits` (multipart) | `EnsureMultipartBufferParser` → `PrepareCtx` (sets `Ctx.Multipart`) → `dispatchUpstream` → `CloneMultipartBody` with `model` rewritten to selected channel `ActualModel` |
| `POST /v1/images/generations` | JSON-only generations path (unchanged) |
| `POST /v1/images/variations` | Still hard 400 unsupported |

Shared machinery already used by videos/files:

- `PrepareCtx` / `ParseMultipartFormData` for field extraction + model defaulting
- `dispatchUpstream` multipart branch + `CloneMultipartBody`
- Auth failure still returns 401 before dispatch; invalid multipart still 400

## Residuals / honest limits

1. **No image-specific response shaping** — upstream body is relayed as-is. Local
   `METAPI_ENABLE_PROXY_STUB` still returns the generic chat.completion demo body
   when upstream deps are unset (same as other surfaces); it is not an images
   payload and is test/demo-only.
2. **Mask / multi-file field contract** is whatever the selected upstream accepts
   (`image`, optional `mask`, etc.). MetAPI clones the parsed multipart form; it
   does not validate OpenAI image-edit field completeness beyond general multipart
   size/count limits (`PROXY_MAX_MULTIPART_*`).
3. **Platform support is uneven** — many non-OpenAI / chat-only sites will not
   implement `/v1/images/edits`. Operators should bind image models only to
   channels whose sites expose OpenAI-compatible image edits; failures surface as
   real upstream errors after channel selection rather than fake local success.
4. **`/v1/images/variations` remains unsupported** (explicit 400).
5. **Streaming / progressive image APIs** are out of scope; edits remain a single
   buffered multipart/JSON request.

## Tests

`handler/proxy/images_test.go`:

- multipart unauthorized → 401, no example.com body
- invalid multipart → 400, no example.com body
- multipart + stub env + nil upstream → generic dispatchUpstream stub, not
  example.com theater
- multipart + wired upstream → path `/v1/images/edits`, model rewrite, file
  payload preserved
- multipart + stub disabled + nil upstream → 503 unavailable

Verify:

```bash
go test ./handler/proxy -count=1 -run Image
```
