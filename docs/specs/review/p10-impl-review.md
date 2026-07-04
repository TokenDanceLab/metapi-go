# P10 Implementation Review: Proxy Routes

**Date**: 2026-07-04
**Spec**: `docs/specs/p10-proxy-routes.md`
**Go impl**: `handler/proxy/`
**TS ref**: `src/server/routes/proxy/` + `proxy-core/surfaces/`

## Executive Summary

The P10 Go implementation correctly registers all 13 proxy surfaces with matching endpoint paths and aliases. Routing structure, surface configuration (`PrepareCtx`), client detection integration, request validation, and error format compliance all pass. However, **every handler returns hardcoded stub/synthetic responses** -- no upstream channel selection, no HTTP forwarding, no retry loop, no proxy logging, and no billing are connected. This is an architectural skeleton, not a working proxy. Additionally, 3 surface-specific semantic defects and 2 surface-level missing feature gaps were found.

## Severity Tally

| Severity | Count | Summary |
|----------|-------|---------|
| P0 (Blocker) | 1 | All handlers return synthetic stub responses; zero upstream forwarding |
| P1 (Critical gap) | 2 | `handleChatSurfaceRequest` discards `surfaceFormat`; downstream policy check absent from all handlers |
| P2 (Surface bug) | 3 | `currentUnix()` returns 0 for responses; multipart `CloneMultipartBody` does not override fields; `ftoa` loses non-integer precision |
| P3 (Missing feature) | 2 | WebSocket transport is entirely stubbed; files CRUD returns 501 for 4 of 5 endpoints |
| OK (Pass) | -- | Route registration, request validation, error format, client detection integration, model format detection, video task mapping, Gemini route count, search parameter validation |

---

## P0: BLOCKER -- All Handlers Return Synthetic Stubs

**Location**: `chat.go`, `messages.go`, `completions.go`, `responses.go`, `embeddings.go`, `images.go`, `videos.go`, `gemini.go`, `search.go`

**Problem**: Every handler that should call `selectProxyChannelForAttempt` + upstream `fetch` + `handleStreamResponse`/`handleNonStreamResponse` + `insertProxyLog` instead returns a hardcoded "Hello from MetAPI Go" stub. The spec defines a 10-step channel selection + retry loop pattern (spec section "Handler 模式"), and the TS reference implements it fully (see `search.ts` lines 31-214). The Go handlers implement none of it.

**Evidence**:
- `chat.go:34`: `"Hello from MetAPI Go"` string literal returned as chat completion content.
- `messages.go:63`: `"Hello from MetAPI Go proxy"` returned as Claude message content.
- `responses.go:35`: `"Hello from MetAPI Go"` returned as responses output.
- No call to `selectProxyChannelForAttempt`, `buildUpstreamURL`, `http.Post`, or `insertProxyLog` exists anywhere in `handler/proxy/`.
- The `surfaceFormat` parameter in `handleChatSurfaceRequest` is received but discarded (`_ = surfaceFormat`).

**Spec requires** (section "Handler 模式"):
1. Extract ProxyAuthContext -- DONE by `PrepareCtx`
2. Parse body, extract requestedModel -- DONE by `PrepareCtx`
3. Validate model required -- DONE by `PrepareCtx`
4. Downstream policy check -- NOT DONE
5. Client detection -- DONE by `PrepareCtx`
6. Channel selection + retry loop -- NOT DONE
7. Build upstream request (URL + body + token) -- NOT DONE
8. Send upstream request with first-byte timeout -- NOT DONE
9. Handle response (streaming vs non-streaming) -- NOT DONE
10. Write proxy_log + billing -- NOT DONE

**Fix**: Each handler must call into the P8 endpoint flow (`proxy.ExecuteEndpointFlow` or equivalent) with the prepared context, or implement the channel selection + upstream fetch + response handling loop directly. This is the single largest gap between spec and implementation.

---

## P1: CRITICAL -- `surfaceFormat` Discarded in Chat Handler

**Location**: `chat.go:25-27`

```go
func handleChatSurfaceRequest(w http.ResponseWriter, r *http.Request, ctx *Ctx, surfaceFormat string) {
    _ = surfaceFormat
```

**Problem**: Both `HandleChatCompletions` and `HandleClaudeMessages` call `handleChatSurfaceRequest` with `"openai"` and `"claude"` respectively, but the `surfaceFormat` is discarded. The spec requires that the surface identifier be passed through to the endpoint flow so the protocol transformer pipeline can apply the correct format-specific transformations (OpenAI chat format vs Claude messages format vs responses format).

**Spec states**: Chat surface passes surface format `"openai"`, Claude surface passes `"claude"`, Responses surface passes the full downstream path as the path parameter (section "端点清单" Tables 1-2, and Acceptance Criterion bullets 1-2).

**Fix**: Store `surfaceFormat` in the `Ctx` struct and pass it through to `ExecuteEndpointFlow` (or the inline channel selection loop). The TS code passes it to `handleChatSurfaceRequest(request, reply, 'openai')` which flows through to `chatSurface.ts`.

---

## P1: CRITICAL -- Downstream Policy Check Missing from All Handlers

**Location**: All handler files (`chat.go`, `messages.go`, `completions.go`, `responses.go`, `embeddings.go`, `search.go`, `videos.go`, `images.go`, `gemini.go`)

**Problem**: The spec's handler pattern (step 4) requires `ensureModelAllowedForDownstreamKey(r, requestedModel)`, returning 403 if the model is not allowed by downstream policy. The TS implementation calls this in every handler (e.g., `search.ts:63`). `IsModelAllowedByPolicy` is defined in `models.go` but is never invoked by any handler.

**Evidence**: A grep for `ensureModelAllowed` or `IsModelAllowedByPolicy` across all `handler/proxy/*.go` files shows the function is defined in `models.go:85` but has zero callers.

**Fix**: Add `if !IsModelAllowedByPolicy(ctx.RequestedModel, ctx.Policy) { ... 403 }` to `PrepareCtx` (centralized) or to each handler that requires it. The former is cleaner and matches the "all-in-one `PrepareCtx`" pattern already established.

---

## P2: Surface Bug -- `currentUnix()` Returns 0 in Responses Handler

**Location**: `responses.go:119-121`

```go
func currentUnix() int64 {
    return int64(0) // placeholder - use time.Now().Unix() in production
}
```

**Problem**: This placeholder is used in `HandleResponses` for `created_at` timestamps in every stub response. All response objects have `"created_at": 0`. The spec's responses format requires actual Unix timestamps.

**Fix**: Replace with `time.Now().Unix()`. The `var _ = time.Now` in `embeddings.go:45` suggests the import exists; just use it.

---

## P2: Surface Bug -- `CloneMultipartBody` Does Not Override Fields

**Location**: `multipart.go:54-98`

**Problem**: The spec requires `cloneFormDataWithOverrides(form, overrides)` to copy FormData while replacing specified field values (e.g., replacing `model` with upstream model). The Go implementation copies all form values verbatim with no override mechanism. This means when images/edits or videos are forwarded to upstream, the original model name (not the resolved upstream model) will be sent.

**Spec states** (section "FormData 操作"):
> `cloneFormDataWithOverrides(form, overrides map[string]string)`: 复制 FormData, 替换 overrides 中的字段值 (如替换 model 为 upstream model), 保留文件 entries.

**Fix**: Add an `overrides map[string]string` parameter to `CloneMultipartBody`, and before writing fields, check if a key exists in overrides and use the override value instead.

---

## P2: Surface Bug -- `ftoa` Loses Non-Integer Float Precision

**Location**: `messages.go:210-220`

```go
func ftoa(f float64) string {
    if f == float64(int64(f)) && f < 1e15 && f > -1e15 {
        return itoa(int64(f))
    }
    _ = import_strconv
    return itoa(int64(f))  // <-- drops fractional part
}
```

**Problem**: For non-integer floats (e.g., `3.14`, `-0.5`), `ftoa` falls through to `itoa(int64(f))` which truncates to integer. `3.14` becomes `"3"`, `0.5` becomes `"0"`. This is exposed through `appendJSON` which is called by `writeJSON` for all response bodies. Any float field in a response (pricing, usage statistics, embeddings) will be corrupted.

**Fix**: Use `strconv.FormatFloat(f, 'f', -1, 64)` for non-integer values. The commented `import_strconv` suggests the intent was there.

---

## P3: MISSING FEATURE -- WebSocket Transport Entirely Stubbed

**Location**: `responses_ws.go:22-28`

```go
func EnsureResponsesWebsocketTransport(srv *http.Server, cfg WebSocketConfig) {
    slog.Info("registering responses WebSocket transport (stub)")
    _ = srv
    _ = cfg
}
```

**Problem**: The spec dedicates a large section ("WebSocket (Responses) 详细规格", ~200 lines) to the WebSocket transport implementation: upgrade handshake, auth token extraction, session lifecycle, message serialization, Codex WS runtime, HTTP fallback, pre-warm synthesis, service tier policy, and session cleanup. The Go implementation has only data type definitions (`ResponsesWSMessage`, `ParseResponsesWSMessage`, `ResponsesWSError`, `SynthesizePrewarmResponsePayloads`) and a no-op registration function.

The comment says "将在一轮补齐中完成" (will be completed in a follow-up pass). This is acceptable as a phased delivery but must be tracked.

**What IS implemented** (types + helpers):
- `ResponsesWSMessage` struct + `ParseResponsesWSMessage` -- OK
- `ResponsesWSError` -- OK
- `SynthesizePrewarmResponsePayloads` -- OK (matches spec's "Pre-warm 本地合成" section)
- `extractWSHeaders`, `extractWSTurnState` -- OK

**What is NOT implemented**:
- Actual `server.OnUpgrade` handler registration
- Auth token validation during upgrade (by priority: Bearer > x-api-key > x-goog-api-key > query param)
- WebSocket upgrade with `x-codex-turn-state` echo
- `handleResponsesWebsocketConnection` lifecycle
- Message queue serialization
- Request normalization (incremental vs non-incremental mode)
- Codex WS runtime path (`codexWebsocketRuntime.sendRequest`)
- HTTP fallback (`forwardResponsesRequestViaHttp`)
- Output collection (`collectResponsesOutput`)
- Session cleanup on close/shutdown

---

## P3: MISSING FEATURE -- Files CRUD Returns 501 for 4/5 Endpoints

**Location**: `files.go`

| Endpoint | Expected | Actual |
|----------|----------|--------|
| GET `/v1/files` | 200 with file list | 200 with empty list |
| POST `/v1/files` | File upload to upstream | 501 |
| GET `/v1/files/{id}` | File metadata | 501 |
| GET `/v1/files/{id}/content` | File download | 501 |
| DELETE `/v1/files/{id}` | File deletion | 501 |

**Problem**: The spec Table 11 says files are "由 proxy-core surfaces/filesSurface.ts 代理" -- delegated to the files surface module. The Go implementation has the routes registered but 4 of 5 return 501. Only the list endpoint returns a valid (but empty) response.

**Fix**: These should forward to upstream files endpoints using the same channel selection + upstream fetch pattern as other surfaces. The TS `filesSurface.ts` handles upload/download/info/delete by proxying to upstream with file content streaming.

---

## Detailed Per-Surface Verification

### Chat Surface (`chat.go`) -- SKELETON ONLY
| Check | Result |
|-------|--------|
| Routes: `/v1/chat/completions` + `/chat/completions` | PASS |
| Surface format `"openai"` | PASS (specified) but DISCARDED |
| `model` required validation | PASS |
| Stream detection | PASS |
| SSE headers: Content-Type, Cache-Control, Connection: keep-alive | PASS |
| SST format: data: ...\n\n | PASS |
| `[DONE]` marker | PASS |
| **Actual upstream forwarding** | **FAIL** -- returns stub |
| Downstream policy enforcement | **FAIL** -- not called |

### Claude Messages Surface (`messages.go`) -- SKELETON ONLY
| Check | Result |
|-------|--------|
| Routes: `/v1/messages` + `/v1/messages/count_tokens` | PASS |
| Surface format `"claude"` | PASS (specified) but DISCARDED |
| Count tokens separate handler | PASS |
| Claude SSE event format (message_start, content_block_start, delta, stop, message_delta, message_stop) | PASS |
| Claude non-streaming response format (id, type, role, content[], stop_reason, usage) | PASS |
| **Actual upstream forwarding** | **FAIL** -- returns stub |
| **count_tokens forwarding** | **FAIL** -- returns hardcoded `{"input_tokens": 42}` |

### Completions Surface (`completions.go`) -- SKELETON ONLY
| Check | Result |
|-------|--------|
| Route: `/v1/completions` | PASS |
| `model` required validation | PASS |
| SSE chunk format (`text_completion` object type) | PASS |
| Non-streaming format with `choices[].text` | PASS |
| **Actual upstream forwarding** | **FAIL** -- returns stub |

### Responses Surface (`responses.go`) -- SKELETON ONLY
| Check | Result |
|-------|--------|
| POST `/v1/responses` + `/v1/responses/compact` with full path | PASS |
| GET `/v1/responses` returns 426 | PASS |
| POST `/responses` alias maps to `/v1/responses` | PASS |
| POST `/responses/compact` alias maps to `/v1/responses/compact` | PASS |
| Unknown `/responses/*` returns 404 | PASS |
| GET `/responses` returns 426 | PASS |
| GET `/responses/compact` returns 426 with correct path | PASS |
| SSE event types (response.created, output_item.added, content_part.added, output_item.done, response.completed) | PASS |
| Non-streaming response format (id, object, status, model, output[], usage) | PASS |
| `currentUnix()` returns actual timestamp | **FAIL** -- returns 0 |
| **Actual upstream forwarding** | **FAIL** -- returns stub |

### Models Surface (`models.go`) -- MIXED
| Check | Result |
|-------|--------|
| Route: `/v1/models` | PASS |
| Detects `anthropic-version` header for Claude format | PASS |
| Detects `x-api-key` header for Claude format | PASS |
| OpenAI format: object=list, data[].id/object/created/owned_by | PASS |
| Claude format: data[].id/display_name/type | PASS |
| **Integrates tokenRouter for live model list** | **FAIL** -- hardcoded stub |
| **Filters by downstream policy** | **FAIL** -- policy parameter received but ignored |
| **Calls refreshModelsAndRebuildRoutes** | **FAIL** -- not connected |

### Embeddings Surface (`embeddings.go`) -- SKELETON ONLY
| Check | Result |
|-------|--------|
| Route: `/v1/embeddings` | PASS |
| `model` required validation | PASS |
| Stream rejection (400) | PASS |
| Response format (object=list, data[].embedding, usage) | PASS |
| **Actual upstream forwarding** | **FAIL** -- returns stub |

### Images Surface (`images.go`) -- SKELETON ONLY with correct validation
| Check | Result |
|-------|--------|
| `/v1/images/generations`: model defaults to `gpt-image-1` | PASS |
| `/v1/images/edits`: multipart + JSON dual mode | PASS |
| `/v1/images/edits`: model default `gpt-image-1` | PASS |
| `/v1/images/variations`: always 400 | PASS |
| **Multipart model extraction from edits** | PASS |
| **Actually forward multipart body upstream** | **FAIL** -- returns stub |
| **cloneFormDataWithOverrides for model replacement** | **FAIL** -- no override mechanism |

### Videos Surface (`videos.go`) -- SKELETON ONLY with correct mapping
| Check | Result |
|-------|--------|
| POST/GET/DELETE routes with chi.URLParam | PASS |
| `model` required validation (from multipart or JSON) | PASS |
| multipart + JSON dual mode | PASS |
| publicId -> upstreamVideoId mapping (in-memory store) | PASS |
| GET by publicId, 404 if not found | PASS |
| DELETE by publicId, 404 if not found, 204 on success | PASS |
| Thread-safe mapping store (sync.RWMutex) | PASS |
| **Actually forward to upstream** | **FAIL** -- returns stub |
| **Rewrite response `id` to publicId** | **FAIL** -- stub returns hardcoded publicId directly |
| **Save/refresh status snapshot** | **FAIL** -- stub only saves initial mapping |

### Search Surface (`search.go`) -- BEST IMPLEMENTED
| Check | Result |
|-------|--------|
| Route: `/v1/search` | PASS |
| `query` required (trim + empty check) | PASS |
| `stream=true` rejection (400) | PASS |
| `max_results` default 10, max 20 | PASS |
| `max_results` integer validation (float64 + int types) | PASS |
| `max_results` range [1,20] enforcement | PASS |
| `model` defaults to `__search` | PASS |
| Error message format matches OpenAI `{"error":{}}` | PASS |
| **Actual upstream forwarding** | **FAIL** -- returns stub |

### Gemini Surface (`gemini.go`) -- SKELETON ONLY with correct routing
| Check | Result |
|-------|--------|
| 7 routes total: models(2) + generateContent(2) + CLI(3) | PASS |
| `/v1beta/models` + `/gemini/:apiVersion/models` | PASS |
| `/v1beta/models/*` + `/gemini/:apiVersion/models/*` | PASS |
| `/v1internal::generateContent` + `::streamGenerateContent` + `::countTokens` | PASS |
| Gemini models response format (name, displayName, supportedGenerationMethods) | PASS |
| Gemini generateContent response format (candidates, usageMetadata) | PASS |
| Gemini CLI generateContent returns array format | PASS |
| Gemini CLI streaming (SSE) | PASS |
| Gemini CLI countTokens returns {totalTokens, totalBillableCharacters} | PASS |
| `ParseGeminiPath` helper (apiVersion, model, action) | PASS |
| **Direct Gemini-family upstream proxy** | **FAIL** -- returns stub |
| **Gemini compatibility mode (transform to OpenAI)** | **FAIL** -- returns stub |
| **countTokens on non-Gemini platform returns 501** | **FAIL** -- returns stub 200 |
| **401 OAuth token refresh** | **FAIL** -- not connected |
| **Model filtering by downstream policy** | **FAIL** -- not connected |

---

## Positive Findings

### 1. Route Registration Architecture -- Excellent
The split between `RegisterProxyRoutes` (under `/v1`) and `RegisterNonV1ProxyRoutes` (top-level paths) correctly handles the architectural requirement that some routes live outside `/v1` (e.g., `/chat/completions`, `/responses`, Gemini paths). This matches the TS pattern where `chat.ts` registers both `/v1/chat/completions` and `/chat/completions` in the same plugin.

### 2. `PrepareCtx` Centralized Context Extraction -- Well Designed
The `PrepareCtx` function in `surface.go` consolidates: auth context extraction, body parsing, model validation (with default fallback), stream detection, client detection, and retry config resolution. This avoids duplicated boilerplate across 13+ handlers and provides a single point for adding future checks (e.g., downstream policy enforcement).

### 3. Request Validation -- Spec-Compliant
All validation rules from the spec's "请求体验证规则" table are correctly implemented:
- Chat/Completions/Embeddings: model required
- Search: query required, stream rejected, max_results [1,20]
- Images/Videos: model with default fallback
- Images variations: always 400

### 4. Error Response Format -- OpenAI-Compatible
`writeJSONError` produces `{"error":{"message":"...","type":"..."}}` matching the spec requirement. The custom `jsonEscape` handles all JSON-special characters. Tests confirm valid JSON output for messages containing quotes, backslashes, and control characters.

### 5. Client Detection Integration -- Correct
`PrepareCtx` calls `DetectClientContext(downstreamPath, headers, body)` and stores the result. The test `TestDetectClientContext_NoOpenaiKind` explicitly verifies that `"openai"` is never returned. `HeaderMapFromRequest` correctly normalizes `http.Header` multi-value maps to single-value strings. The 4 valid kinds (`generic`, `codex`, `claude_code`, `gemini_cli`) are tested.

### 6. Models Format Dual-Output -- Correct
`HandleModels` correctly detects Claude format via `anthropic-version` or `x-api-key` headers, matching the spec. OpenAI format includes `object`, `created`, `owned_by` fields. Claude format uses `display_name` and `type` fields.

### 7. WebSocket Data Types -- Well Defined
Even though the transport is stubbed, the data types (`ResponsesWSMessage`, `WebSocketConfig`, `ResponsesWSError`), parsing function (`ParseResponsesWSMessage`), and pre-warm synthesis (`SynthesizePrewarmResponsePayloads`) are correctly structured and match the spec. Tests cover message parsing, generate flag, previous_response_id, and error formatting.

### 8. Video Task Mapping Store -- Thread-Safe
The in-memory `videoTaskStore` uses `sync.RWMutex` and provides `SaveProxyVideoTask`, `GetProxyVideoTaskByPublicID`, and `DeleteProxyVideoTaskByPublicID` helpers. The GET/DELETE handlers correctly return 404 for missing publicIds.

### 9. Test Coverage -- Good for Available Functionality
17 test files cover: routing registration, surface context preparation, client detection, multipart parsing, WebSocket message parsing, SSE formatting, JSON encoding helpers, and model policy matching. Tests verify route existence (no 404/405), validation rejection (400/401), and response format correctness.

### 10. Gemini Path Parsing -- Complete
`ParseGeminiPath` correctly handles paths like `/v1beta/models/gemini-2.5-pro:generateContent` and extracts apiVersion, model, and action. The 3 CLI internal paths (`/v1internal::*`) are all registered and handler endpoints match the spec exactly.

---

## Summary of Gaps vs Acceptance Criteria

From the spec's "Acceptance Criteria" (27 items):

| # | Criterion | Status |
|---|-----------|--------|
| 1 | 13 surfaces, all endpoints + aliases + gemini CLI paths | PASS |
| 2 | Chat surface passes `"openai"` format | PASS (registered) / **FAIL** (discarded in handler) |
| 3 | Claude surface passes `"claude"` format, count_tokens separate | PASS (registered) / **FAIL** (discarded) |
| 4 | Responses passes full path as downstream path param | PASS |
| 5 | Responses aliases: /responses, /compact, unknown -> 404 | PASS |
| 6 | GET /v1/responses + GET /responses* -> 426 | PASS |
| 7 | Models: dual format by header detection | PASS |
| 8 | Images edits + Videos: multipart + JSON dual mode | PASS |
| 9 | Images variations: 400 | PASS |
| 10 | Search: query required, stream rejected, max_results validated | PASS |
| 11 | Gemini: 7 routes, including 3 CLI internal paths | PASS |
| 12 | Client detection: 6-field DownstreamClientContext, no "openai" kind | PASS |
| 13 | Client kind identifiers (underscores) | PASS |
| 14 | Streaming endpoints: SSE headers + Connection: keep-alive | PASS |
| 15 | Non-streaming: collect full response, detect proxy failure | **FAIL** (stubs, no upstream) |
| 16 | WebSocket: upgrade, message queue, Codex WS runtime, HTTP fallback, pre-warm, service tier | **FAIL** (entirely stubbed) |
| 17 | Multipart: EnsureMultipartBufferParser, ParseMultipartFormData, cloneFormDataWithOverrides | PARTIAL (parser yes, override no) |
| 18 | Request validation: model required (chat/completions/embeddings/videos), search param checks | PASS |
| 19 | Channel retry: auto-retry next channel, GetProxyMaxChannelRetries limit | **FAIL** (no channel selection) |
| 20 | Token expiry: detect + reportTokenExpired | **FAIL** (no upstream) |
| 21 | Proxy log: write per request/retry with all client context fields | **FAIL** (no logging) |
| 22 | Error format: OpenAI-compatible `{"error":{"message":"...","type":"..."}}` | PASS |
