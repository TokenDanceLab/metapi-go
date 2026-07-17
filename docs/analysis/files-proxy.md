# Files Proxy Surface (`/v1/files`)

Last updated: 2026-07-17

## Surface

| Item | Value |
|------|--------|
| Methods / Paths | `POST /v1/files`, `GET /v1/files`, `GET /v1/files/{fileId}`, `GET /v1/files/{fileId}/content`, `DELETE /v1/files/{fileId}` |
| Handler | `handler/proxy/files.go` |
| Upstream paths | Same OpenAI-shaped `/v1/files*` (passthrough) |
| Template | Auth + `PrepareCtx` + `dispatchUpstream` (multipart clone on upload) |

## Behavior

| Endpoint | Notes |
|----------|--------|
| `POST /v1/files` | Multipart/form-data required (`file` field). Forwards multipart to upstream; model field rewritten to selected channel `ActualModel` when present. |
| `GET /v1/files` | Proxies upstream list. With proxy stub enabled and no upstream config (tests), returns empty `{object:list,data:[]}`. |
| `GET /v1/files/{id}` | Proxies metadata JSON. |
| `GET /v1/files/{id}/content` | Proxies binary/content (Content-Type / Content-Disposition relayed). |
| `DELETE /v1/files/{id}` | Proxies delete; upstream OpenAI shape typically `{id,object,deleted:true}`. |

## Auth / routing / errors

- Proxy auth middleware (401 without token).
- Channel selection via TokenRouter using a **model key** (Files API is model-agnostic at OpenAI; MetAPI still needs a model for route selection).
- Model resolution order: body/multipart `model` → query `?model=` → header `X-Metapi-Files-Model` → default `gpt-4o`.
- JSON errors: `{"error":{"message":"...","type":"..."}}`. Upstream non-2xx bodies are relayed as-is (OpenAI-shaped when upstream is OpenAI-compatible).
- No local unbounded disk dump of customer files; content is streamed through memory-bounded multipart/buffered proxy paths only.

## Residual platforms (no native `/v1/files`)

Many non-OpenAI upstreams will not implement Files. Expect 404/501/unsupported from those sites after channel selection:

| Platform family | Typical `/v1/files` support |
|-----------------|-----------------------------|
| OpenAI / Azure OpenAI / OpenAI-compatible gateways | Yes (when account has Files API) |
| NewAPI / OneAPI-style multi-protocol gateways | Partial — only if backend maps Files |
| Anthropic Messages native | No dedicated Files REST surface |
| Gemini / gemini-cli / Antigravity | No OpenAI Files surface (use inline / Files API elsewhere) |
| Codex / responses-only sites | Usually no |
| MiniMax / vendor chat-only | Usually no |

Operators should bind `gpt-4o` (or a dedicated files model override) only to channels whose sites expose OpenAI-compatible Files endpoints. Failover will surface upstream errors rather than inventing local storage.

## Non-goals

- Local `proxy_files` durable store / TTL policy (TS MetAPI used owner-scoped local store; Go wave is **upstream proxy only** per issue #155).
- `/v1/input_files` (separate gap).
- Cross-protocol transform of file objects.
- Streaming uploads/downloads beyond standard buffered proxy limits.
