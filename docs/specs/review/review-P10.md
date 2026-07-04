# P10 Cross-Reference Review: Go Spec vs TypeScript Source

**Reviewed spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p10-proxy-routes.md`
**TS sources reviewed**: `router.ts`, `chat.ts`, `responses.ts`, `responsesWebsocket.ts`, `models.ts`, `files.ts`
**Additional TS sources consulted**: `completions.ts`, `downstreamClientContext.ts`, `routeAliases.test.ts`, `downstreamClientContext.test.ts`, `cliProfiles/registry.ts`, `cliProfiles/types.ts`
**Date**: 2026-07-04

---

## Accuracy Issues

### A1. Route alias for chat completions is wrong
- **Spec claim** (line 126): `/chat/completions → alias "chat" (OpenAI surface)`
- **Spec handler pattern** (line 93): `RouteAlias: "chat"`
- **TS actual** (`chat.ts` line 9-11): Both `/v1/chat/completions` and `/chat/completions` call `handleChatSurfaceRequest(request, reply, 'openai')`. The downstream format parameter is `'openai'`, not `'chat'`.
- **Impact**: The Go implementation will pass the wrong surface identifier to the transformer/core layer.

### A2. Route alias for responses is a full path, not an alias string
- **Spec claim** (line 128): `/v1/responses → alias "responses" (Codex surface)`
- **TS actual** (`responses.ts` line 29): `handleOpenAiResponsesSurfaceRequest(request, reply, '/v1/responses')` -- the parameter is the full downstream path `'/v1/responses'`, not a bare alias string `'responses'`.
- **Impact**: The Go spec treats this as a simple alias, but in TS the responses surface handler receives the full path as a routing disambiguator (used to distinguish `/v1/responses` from `/v1/responses/compact`). The Go implementation needs to pass the full path, not just `"responses"`.

### A3. Client kind "openai" does not exist in TS
- **Spec claim** (line 119): `detectClientKind` returns values including `"openai"`
- **TS actual** (`cliProfiles/types.ts` line 1-5): `CliProfileId = 'generic' | 'codex' | 'claude_code' | 'gemini_cli'`. There is no `'openai'` client kind. OpenAI-protocol requests that don't match a specific CLI profile fall through to `'generic'`.
- **Impact**: The Go implementation would have a spurious client kind that has no counterpart in the TS. This would affect CLI profile selection logic downstream.

### A4. Client kind "claude-code" uses wrong separator
- **Spec claim** (line 119): `"claude-code"` (hyphen)
- **TS actual** (`cliProfiles/types.ts` line 4): `'claude_code'` (underscore)
- **Impact**: String mismatch will break any switch/case or map lookup on client kind.

### A5. GET /responses is not a Codex passthrough
- **Spec claim** (line 55): Codex Passthrough is `POST/GET /responses*`
- **TS actual** (`responses.ts` lines 49-55, 56-67): `GET /responses` returns **426** (WebSocket upgrade required). `GET /responses/*` also returns **426** (with a path-specific message). Neither performs a passthrough.
- **Impact**: The spec implies GET is a working passthrough. The Go implementation must return 426 for all GET `/responses*` requests, not forward them upstream.

### A6. GET /v1/responses description is misleading
- **Spec claim** (line 53): `GET /v1/responses` described as "获取 response"
- **TS actual** (`responses.ts` lines 30-36): Returns `426` with `{ error: { message: 'WebSocket upgrade required for GET /v1/responses', type: 'invalid_request_error' } }`. It does NOT retrieve a response; it requires WebSocket upgrade.
- **Impact**: The endpoint table implies a data-fetching operation. The Go implementation should document that GET `/v1/responses` is exclusively a WebSocket upgrade endpoint.

---

## Missing Details

### M1. Client detection is far more sophisticated than described
The spec reduces client detection to 4 lines of pseudocode checking User-Agent/headers. The TS implementation (`downstreamClientContext.ts`, 287 lines) has:

- **CLI profile registry** with 4 profiles (`generic`, `codex`, `claude_code`, `gemini_cli`) registered in priority order: claude_code > codex > gemini_cli > generic.
- **Session ID extraction**: Claude Code extracts session UUID from `metadata.user_id` (AxonHub format); Codex extracts from `Session_id` or `conversation_id` headers.
- **App fingerprinting**: Registry-based rules for Cherry Studio, OpenCode, OpenClaw with priority-weighted matching and `exact`/`heuristic` confidence levels.
- **Explicit self-report**: Parses `x-openai-client-user-agent` JSON header for `client`/`name`/`app` fields, and `User-Agent: OpenClaw/...` patterns.
- **Body inspection**: Examines `system` prompt content for OpenCode identification and `metadata.user_id` for Claude Code session extraction.
- **Gemini CLI detection**: Via `/v1internal:generateContent` downstream path pattern.
- **Confidence model**: Each detection yields `'exact'` or `'heuristic'` confidence, used to decide whether to alter protocol behavior.

This entire subsystem needs to be ported with fidelity. The current spec gives zero detail.

### M2. WebSocket (responses) is severely under-specified
The spec covers WebSocket in one module name (`responses_ws.go`), one acceptance criterion, and one edge case. The TS `responsesWebsocket.ts` is **845 lines** of production logic. Missing from the spec:

| TS Feature | Lines | Description |
|---|---|---|
| Pre-warm synthesis | 299-332 | When `generate: false` is sent and incremental input is not supported, the proxy locally synthesizes `response.created` + `response.completed` events with zero-token usage, without hitting any upstream. |
| Incremental input detection | 133-138, 539-553 | Checks whether the selected channel supports incremental input mode (`websockets` flag in account extraConfig/providerData). Affects request normalization (whether to merge input across turns or treat each `response.create` independently). |
| Request normalization | 185-269 | Complex logic merging input arrays across turns, preserving `previous_response_id` for incremental mode, handling `response.create` vs `response.append` message types. |
| Codex WS runtime | 685-784 | Direct WebSocket-to-WebSocket forwarding via `codexWebsocketRuntime` when the upstream platform is Codex. Includes session key management (`buildCodexSessionResponseStoreKey`), OAuth provider headers, and the `buildUpstreamEndpointRequest` call. |
| HTTP fallback on WS error | 752-764 | When the Codex WS runtime fails with zero events, the proxy falls back to HTTP POST `/v1/responses` via `forwardResponsesRequestViaHttp`. This is not just "handshake failure" but any runtime error. |
| Turn state header | 810-813 | `x-codex-turn-state` header is echoed back on WS upgrade response headers. |
| Service tier policy in WS | 601-618, 656-682 | Applied twice: once before normalization (for early rejection) and once after channel selection (with actual model/platform context). |
| Managed key consumption | 637-639 | `consumeManagedKeyRequest` called for managed keys on each WS message. |
| Response output collection | 334-407 | `collectResponsesOutput` aggregates output items from WS event payloads across types: `response.output_item.added/done`, `response.completed/failed/incomplete`, bare `output` arrays, and `output_text` strings. |
| Message queue serialization | 570-571, 585-801 | All WS messages are serialized through a Promise chain (`messageQueue`) to prevent concurrent processing. |
| Session lifecycle | 572-583, 838-843 | Sessions are cleaned up on socket close. All sessions closed on app shutdown via `onClose` hook. |
| WS auth | 510-518, 820-836 | Token extraction from Authorization/x-api-key/x-goog-api-key headers or `key` query parameter. Upgrade rejection with proper HTTP error body. |
| `x-metapi-responses-websocket-mode` | 26, 420, 693 | Internal header marking incremental mode for the HTTP forward path. |
| `x-metapi-responses-websocket-transport` | 27, 419, 692 | Internal header marking that the request came through WS transport. |

### M3. Models endpoint dual-format support not documented
- **Spec claim** (line 56): `GET /v1/models` with description "模型列表"
- **TS actual** (`models.ts` lines 9-19):
  - Detects Claude format by checking for `anthropic-version` or `x-api-key` headers (`wantsClaudeFormat`).
  - Passes `responseFormat: wantsClaudeFormat ? 'claude' : 'openai'` to `listModelsSurface`.
  - Integrates `tokenRouter`, `refreshModelsAndRebuildRoutes`, and `isModelAllowedByPolicyOrAllowedRoutes`.
- **Missing**: The spec does not describe the dual OpenAI/Claude response format, nor the integration with token routing and route refresh workflow.

### M4. SSE Connection header omitted
- **Spec handler pattern** (lines 98-99): Sets `Content-Type: text/event-stream` and `Cache-Control: no-cache`.
- **TS actual** (`completions.ts` lines 117-121): Additionally sets `Connection: keep-alive`.
- The `Connection: keep-alive` header is standard practice for SSE and should be included.

### M5. Route alias subpath rejection not documented
- **TS actual** (`responses.ts` lines 16-23, 42-47): Unknown `/responses/*` subpaths (e.g., `/responses/other`) are explicitly rejected with 404 and `{ error: { message: 'Unknown /responses alias path', type: 'invalid_request_error' } }`. Only `/responses` and `/responses/compact` are recognized.
- The spec makes no mention of this guard. Without it, arbitrary subpaths would be silently forwarded or mishandled.

### M6. Gemini endpoint has no path or method in spec
- **Spec claim** (line 66): `Gemini | — | Gemini native 格式 | —`
- The Method and Path columns are empty. The TS router registers `geminiProxyRoute` but the spec provides zero information about what endpoints it serves.

### M7. Request body validation not shown in handler pattern
- **Spec handler pattern** (lines 70-115): Shows `json.NewDecoder(r.Body).Decode(&reqBody)` with no validation.
- **TS actual** (`completions.ts` lines 34-37): Validates that `body.model` exists and returns 400 if missing.
- **TS actual** (`responsesWebsocket.ts` lines 216-221): Validates that `model` is present in `response.create` messages.
- The handler pattern should include a validation step.

---

## Edge Cases Not Covered

### E1. Pre-warm (generate=false) local synthesis
When a Codex client sends `response.create` with `generate: false` and the channel does not support incremental input, the TS synthesizes local `response.created` + `response.completed` events without any upstream call. This is a deliberate optimization for the Codex file-search pre-warm pattern. Not mentioned anywhere in the spec.

### E2. Service tier policy enforcement in WS path
The TS applies `applyOpenAiServiceTierPolicy` twice in the WS message handler: once before normalization (to reject early), and once after channel selection (to apply model/platform-specific tier rules). The spec's acceptance criteria mention error response format compatibility but not service tier enforcement.

### E3. WS runtime error with partial events
When the Codex WS runtime produces some events but not a terminal event (`response.completed`/`response.failed`/`response.incomplete`), the TS sends the partial events and then a synthetic error (408). If the runtime produces a terminal event, no error is sent even if the runtime reported an error. This nuanced error handling is missing from the spec.

### E4. Managed key rate limiting in WS
The TS calls `consumeManagedKeyRequest` on each WS message for managed keys. The spec does not mention rate limiting or quota consumption in the WS path.

### E5. GET /responses/compact returns 426
The spec endpoint table does not document that GET `/v1/responses/compact` or GET `/responses/compact` return 426 (WebSocket upgrade required). The TS explicitly returns 426 for these paths.

### E6. Gemini internal route detection
The TS detects Gemini CLI via the `/v1internal:generateContent` path pattern. The spec's client detection pseudocode (checking User-Agent/headers) would miss this path-based detection entirely.

---

## Incorrect Details

### I1. Handler pattern RouteAlias value
- **Spec** (line 93): `RouteAlias: "chat"`
- **Correct**: Should be `"openai"` to match the TS surface format identifier passed to `handleChatSurfaceRequest`.

### I2. Handler pattern client detection result
- **Spec** (line 80): `clientKind := detectClientKind(r)` -- implies a single string return.
- **Correct**: `detectDownstreamClientContext` returns a structured object with `clientKind`, `sessionId`, `traceHint`, `clientAppId`, `clientAppName`, `clientConfidence`. The Go handler needs all of these fields for proxy logging and CLI profile selection.

### I3. Endpoint table Codex Passthrough method
- **Spec** (line 55): `POST/GET`
- **Correct**: Only `POST`. All GET requests to `/responses*` return 426.

### I4. Gemini endpoint entry is empty
- **Spec** (line 66): Method `—`, Path `—`, Description "Gemini native 格式"
- The endpoint table should list actual Gemini endpoints (at minimum whatever `gemini.ts` registers -- likely a catch-all or specific Gemini-native paths).

---

## Summary

| Category | Count |
|---|---|
| Accuracy Issues | 6 |
| Missing Details | 7 |
| Edge Cases Not Covered | 6 |
| Incorrect Details | 4 |
| **Total findings** | **23** |

**Verdict**: **NEEDS_REVISION**

The spec correctly captures the high-level endpoint surface (11 surfaces, correct HTTP methods for most paths) and the thin-handler architectural pattern. However, it contains concrete errors in route alias naming (`"chat"` vs `"openai"`), client kind identifiers (`"openai"` does not exist, `"claude-code"` should be `"claude_code"`), and the Codex passthrough behavior (GET returns 426, not a passthrough). More critically, the spec omits nearly all of the WebSocket subsystem logic (845 lines of TS reduced to a single module name), the full client detection framework (287 lines of TS with registry, app fingerprinting, confidence model), and the models endpoint dual-format support. The edge cases around pre-warm synthesis, WS error handling with partial events, and service tier policy in the WS path are entirely absent. The spec should be revised before Go implementation begins, with particular attention to:

1. Correcting route alias identifiers (A1, A2, I1)
2. Replacing client detection pseudocode with a detailed spec of the CLI profile registry, detection priority, and structured return type (A3, A4, M1, E6, I2)
3. Expanding the WebSocket section to cover all 12 features listed in M2
4. Correcting the Codex passthrough endpoint table entry to POST-only (A5, I3)
5. Documenting the models endpoint dual-format logic (M3)
6. Filling in the Gemini endpoint details (M6, I4)
