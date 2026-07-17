# Files proxy (#155)

## Behavior
OpenAI-compatible /v1/files routes use PrepareCtx + dispatchUpstream:
- POST /v1/files (multipart supported)
- GET /v1/files, GET /v1/files/{id}, GET /v1/files/{id}/content
- DELETE /v1/files/{id}

Default model for channel selection: gpt-4o-mini.

## Residual
- Upstream sites without a files API return upstream errors (not MetAPI 501).
- No local durable file store; MetAPI is pass-through only.
