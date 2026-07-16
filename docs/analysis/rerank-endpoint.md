# Rerank Endpoint Contract

Last updated: 2026-07-17

## Surface

| Item | Value |
|------|--------|
| Method / Path | `POST /v1/rerank` |
| Handler | `handler/proxy/rerank.go` → `HandleRerank` |
| Upstream path | `/v1/rerank` (passthrough) |
| Template | Same thin surface as `POST /v1/embeddings` |

## Auth / logging / errors

Matches other proxy routes:

- Proxy auth middleware (401 without token)
- `PrepareCtx` + `dispatchUpstream` path
- JSON error shape: `{"error":{"message":"...","type":"..."}}`

## Request body

OpenAI-compatible / Cohere-style JSON. MetAPI validates **model** only; remaining fields are forwarded raw (with standard model swap if routing rewrites the model name).

```json
{
  "model": "rerank-english-v3.0",
  "query": "What is the capital of France?",
  "documents": [
    "Paris is the capital of France.",
    "Berlin is in Germany."
  ]
}
```

| Field | Required by MetAPI | Notes |
|-------|--------------------|--------|
| `model` | yes | Channel selection key; empty → 400 `model is required` |
| `query` | no (upstream may require) | string |
| `documents` | no (upstream may require) | array of strings **or** objects |
| `top_n` / `return_documents` / etc. | no | passed through if present |
| `stream` | must not be true | `stream: true` → 400 `rerank does not support streaming` |

## Response

Upstream response is returned as-is. No dedicated response transform layer.

When proxy stub is enabled (tests / unconfigured upstream), the generic stub chat-style JSON is returned (same as embeddings).

## Routing

Channel selection uses the requested `model` only (no extra capability flag). Upstream platforms that expose rerank models typically name them with `rerank` in the model id; original MetAPI probed capability via model-name pattern `/(^|[-_/])rerank($|[-_/])/i` rather than a dedicated TS route.

## Non-goals

- No heavy Cohere ↔ OpenAI transform layer
- No stream / SSE support
- No dedicated capability matrix beyond model routing
