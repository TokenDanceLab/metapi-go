# P9 Spec Review: Cross-Reference Against TypeScript Source

**Review date**: 2026-07-04 | **Spec**: `docs/specs/p9-transformers.md` | **TS base**: `src/server/transformers/`

---

## Accuracy Issues

### A1. CanonicalMessage uses `parts` not `content`

**Spec says** (line 94): `Content []CanonicalContentPart`

**TS says** (`canonical/types.ts:72`): `parts: CanonicalContentPart[]`

The field name is `parts`, not `content`. Every consumer in the TS codebase accesses `message.parts`. Using `Content` in Go would break interop with any future canonical-serialized payloads.

### A2. CanonicalContentPart.Type values are wrong

**Spec says** (line 99): `Type string // "text" | "image_url" | "tool_use" | "tool_result"`

**TS says** (`canonical/types.ts:27-69`): `"text" | "image" | "file" | "tool_call" | "tool_result"`

Three errors:
- `"image_url"` should be `"image"` -- the canonical type uses `image`, not `image_url`. The `image_url` name exists only in the downstream `NormalizedContentBlockType` union, not canonical.
- `"tool_use"` should be `"tool_call"` -- canonical always uses `tool_call`. `tool_use` is the Anthropic-level name.
- `"file"` is entirely missing from the spec's type list but is a valid `CanonicalFilePart` type.

### A3. CanonicalImagePart has flat fields, not a nested ImageURL struct

**Spec says** (line 101): `ImageURL *ImageURL`

**TS says** (`canonical/types.ts:33-38`):
```ts
type CanonicalImagePart = {
  type: 'image';
  dataUrl?: string;
  url?: string;
  mimeType?: string | null;
};
```

No `ImageURL` wrapper struct exists. The URL/dataUrl/mimeType live directly on the part. Also, `dataUrl` is a critical field for inline base64 images that has no counterpart in the spec.

### A4. CanonicalRequest is not the envelope -- CanonicalRequestEnvelope is

**Spec says** (lines 81-91): A flat struct called `CanonicalRequest` with fields `Model, Messages, Stream, MaxTokens, Temperature, TopP, Tools, ToolChoice`.

**TS says** (`canonical/types.ts:99-113`): The type is `CanonicalRequestEnvelope` and it has these fields the spec entirely omits:

| Field | Type | Purpose |
|-------|------|---------|
| `operation` | `'generate' \| 'count_tokens'` | Distinguishes token counting from generation |
| `surface` | `'openai-chat' \| 'openai-responses' \| 'anthropic-messages' \| 'gemini-generate-content'` | Tracks originating API surface |
| `cliProfile` | `'generic' \| 'codex' \| 'claude_code' \| 'gemini_cli'` | CLI profile for routing decisions |
| `requestedModel` | `string` | Model name (not `Model`) |
| `reasoning` | `CanonicalReasoningRequest?` | Reasoning effort/budget/summary/encryptedContent |
| `continuation` | `CanonicalContinuation?` | Session/previousResponseId/promptCacheKey/turnState |
| `metadata` | `Record<string, unknown>?` | Arbitrary metadata passthrough |
| `passthrough` | `Record<string, unknown>?` | Provider-specific fields passthrough |
| `attachments` | `CanonicalAttachment[]?` | File attachments |

The `MaxTokens`, `Temperature`, `TopP` fields from the spec do not exist on the canonical envelope at all -- these are upstream-specific parameters handled during request bridge conversion.

### A5. Missing `developer` role

**Spec says** (line 94): `Role string // "system" | "user" | "assistant" | "tool"`

**TS says** (`canonical/types.ts:20-25`): `CanonicalMessageRole = 'system' | 'developer' | 'user' | 'assistant' | 'tool'`

The `developer` role (OpenAI's newer system-message alternative) is absent from the spec.

### A6. CanonicalMessage missing `phase` and `reasoningSignature` fields

**TS** (`canonical/types.ts:71-76`):
```ts
type CanonicalMessage = {
  role: CanonicalMessageRole;
  parts: CanonicalContentPart[];
  phase?: string;
  reasoningSignature?: string;
};
```

Both `phase` and `reasoningSignature` are used in Anthropic/Gemini roundtrips. The spec mentions neither.

### A7. CanonicalTextPart missing `thought` field

**TS** (`canonical/types.ts:27-31`): `CanonicalTextPart` has `thought?: boolean` to mark reasoning/thinking text, which is used by Anthropic thinking blocks and Gemini thought signatures. The spec's text part has no such field.

### A8. CanonicalTool is a discriminated union, not a flat struct

**Spec says** (line 88): `Tools []CanonicalTool // function definitions` implying a flat struct.

**TS says** (`canonical/tools.ts:13-15`):
```ts
type CanonicalTool = CanonicalFunctionTool | CanonicalRawTool;

type CanonicalFunctionTool = {
  name: string;
  description?: string;
  strict?: boolean;
  inputSchema?: Record<string, unknown> | null;
};

type CanonicalRawTool = {
  type: string;
  raw: Record<string, unknown>;
};
```

`CanonicalRawTool` is critical for passthrough of non-OpenAI-function tools (Gemini's googleSearch/codeExecution, Anthropic's web_search). The `strict` boolean on `CanonicalFunctionTool` is also missing.

### A9. CanonicalToolChoice is a full union type, not `interface{}`

**Spec says** (line 89): `ToolChoice interface{}`

**TS says** (`canonical/tools.ts:17-28`): `'auto' | 'none' | 'required' | {type: 'tool'; name: string} | {type: 'raw'; value: string | Record<string, unknown>}`

The `{type: 'raw'}` variant is how non-OpenAI tool_choice values (e.g., Anthropic `{type: 'any', disable_parallel_tool_use: true}`) survive roundtrips. Typing this as `interface{}` loses the discriminated union semantics.

### A10. CanonicalStreamChunk / CanonicalDelta types do not exist in TS

**Spec says** (lines 106-117):
```go
type CanonicalStreamChunk struct {
    Index   int
    Delta   *CanonicalDelta
    FinishReason *string
    Usage   *UsageInfo
}
type CanonicalDelta struct {
    Role    *string
    Content *string
    ToolCalls []ToolCallDelta
}
```

**TS**: These types simply do not exist. The TS codebase uses `NormalizedStreamEvent` (`shared/chatFormatsCore.ts:48-62`) for stream events:
```ts
type NormalizedStreamEvent = {
  role?: 'assistant';
  contentDelta?: string;
  reasoningDelta?: string;
  reasoningSignature?: string;
  redactedReasoningContent?: string;
  toolCallDeltas?: Array<{
    index: number;
    id?: string;
    name?: string;
    argumentsDelta?: string;
  }>;
  finishReason?: string | null;
  done?: boolean;
};
```

Key differences:
- No `Index` field (choice index is per-provider in extended types)
- `ContentDelta` not `Content` (incremental, not full content)
- `reasoningDelta` field is missing from spec entirely
- `reasoningSignature` field is missing
- `redactedReasoningContent` field is missing (Claude safety filtering)
- `toolCallDeltas` uses `argumentsDelta` (incremental JSON fragments) not full arguments
- `done` flag for stream termination

### A11. CanonicalDecoder / SSEEncoder architecture does not exist

**Spec says** (lines 122-135): A unified `CanonicalDecoder` with `bufio.Scanner` and `UpstreamFormat`, and `SSEEncoder` with `io.Writer` and `DownstreamFormat`.

**TS**: No such unified decoder/encoder exists. Each provider has its own stream module with unique patterns:

| Provider | Stream Module | Key Methods |
|----------|--------------|-------------|
| Anthropic | `anthropicMessagesStream` | `consumeAnthropicSseEvent`, `normalizeEvent`, `serializeEvent`, `serializeDone`, `pullSseEvents` |
| OpenAI Chat | `openAiChatStream` | `normalizeEvent`, `serializeEvent`, `serializeDone`, `pullSseEvents` |
| Gemini | `geminiGenerateContentStream` | `parseGeminiStreamPayload`, `applyParsedPayloadToAggregate`, `serializeAggregatePayload` |

Anthropic's SSE is particularly complex with `event:` lines, content block state machine (text/thinking/tool_use/redacted_thinking blocks), and signature deltas. A generic `bufio.Scanner` approach would not correctly handle Anthropic's event-type lines.

### A12. Anthropic SSE format is `event: ...\ndata: ...\n\n`, not `data: ...\n\n`

The spec's acceptance criterion "所有 SSE chunk 格式: `data: {...}\n\n`" is wrong for Anthropic. Anthropic SSE uses named events: `event: message_start\ndata: {...}\n\n`. The TS handles this with `serializeAnthropicRawSseEvent` and `consumeAnthropicSseEvent`.

---

## Missing Details

### M1. CanonicalOperation and CanonicalSurface types

The TS has `CanonicalOperation = 'generate' | 'count_tokens'` and `CanonicalSurface = 'openai-chat' | 'openai-responses' | 'anthropic-messages' | 'gemini-generate-content'`. These drive routing decisions in ProxyCore. The spec mentions neither.

### M2. CanonicalReasoningRequest

TS (`canonical/types.ts:78-90`):
```ts
type CanonicalReasoningRequest = {
  effort?: 'none' | 'low' | 'medium' | 'high' | 'max';
  budgetTokens?: number;
  summary?: string;
  includeEncryptedContent?: boolean;
};
```

The `summary` field enables reasoning summarization requests. `includeEncryptedContent` controls whether encrypted reasoning content is requested. The spec mentions none of these.

### M3. CanonicalContinuation

TS (`canonical/types.ts:92-97`):
```ts
type CanonicalContinuation = {
  sessionId?: string;
  previousResponseId?: string;
  promptCacheKey?: string;
  turnState?: string;
};
```

This is used for multi-turn conversation continuation across providers, especially for Codex sessions. Entirely absent from spec.

### M4. CanonicalAttachment type

TS (`canonical/attachments.ts`) defines `CanonicalAttachment` for file upload handling. The `CanonicalFilePart` uses fileId/fileUrl/fileData/filename/mimeType. The spec's `CanonicalContentPart` has no `file` variant at all.

### M5. Anthropic cache_control optimization system

TS (`conversion.ts:528-701`) has an extensive automatic cache_control placement algorithm:
- `MAX_ANTHROPIC_CACHE_CONTROL_BREAKPOINTS = 4` (Anthropic's hard limit)
- `ADAPTIVE_ANTHROPIC_CACHE_CONTROL_BLOCK_WINDOW = 20`
- Structural anchors: last tool gets `cache_control`, last system prompt gets `cache_control`
- Adaptive message anchors: last message + one anchor 20 blocks back
- `sanitizeUnsupportedAnthropicCacheControls` removes cache_control from non-cacheable blocks (thinking, redacted_thinking)
- This is triggered by `autoOptimizeCacheControls` option

The spec mentions nothing about cache control at all.

### M6. Anthropic thinking config sanitization

TS (`conversion.ts:140-177`) validates and normalizes `thinking.type`:
- `enabled` requires positive `budget_tokens`
- `adaptive` with `budget_tokens` gets promoted to `enabled`
- `output_config.effort` is only allowed when thinking is `adaptive`

The spec has no mention of thinking config validation.

### M7. Anthropic tool_choice sanitization

TS (`conversion.ts:97-138`) handles:
- String `"required"` mapped to `{type: 'any'}`
- Object `{type: 'tool'}` requires `name`
- `disable_parallel_tool_use` passthrough from `parallel_tool_calls: false`
- `{type: 'function', function: {name}}` converted to `{type: 'tool', name}`

The spec's simple `tool_choice` passthrough is insufficient.

### M8. Anthropic content block types beyond text/tool_use

TS `streamBridge.ts` defines block kinds: `'thinking' | 'text' | 'tool_use' | 'redacted_thinking'`. The spec only mentions text and tool_use content blocks. `thinking` and `redacted_thinking` blocks are essential for Claude's extended thinking feature and safety filtering.

### M9. Anthropic signature_delta sub-event type

TS (`streamBridge.ts:281-286`) handles `signature_delta` events for Anthropic reasoning signatures. These are emitted as content_block_delta events with `delta.type = 'signature_delta'`. The spec does not mention reasoning signatures at all.

### M10. Anthropic server_tool_use and web_search_tool_result

TS (`streamBridge.ts:696-707`) handles two special content block types for Anthropic's built-in tools:
- `server_tool_use` -- emitted as content_block_start/stop pair
- `web_search_tool_result` -- emitted as content_block_start/stop pair

These are used when Anthropic conducts web searches server-side. Missing from spec.

### M11. Anthropic system prompt normalization

TS (`conversion.ts:545-554`) normalizes string system prompts to array format `[{type: 'text', text}]`. The spec says `system (string)` but doesn't mention this normalization.

### M12. Anthropic single-string content to content block array

TS (`conversion.ts:528-543`) converts `content: "hello"` to `content: [{type: 'text', text: "hello"}]`. Critical for message validation.

### M13. Anthropic tool_result content normalization

TS (`conversion.ts:240-295`) has complex tool_result content normalization:
- String content stays as string
- Array content with text/image/file/tool_reference blocks gets filtered
- Single image/file object gets wrapped in array
- Fallback to JSON.stringify for unrecognized content

### M14. Gemini aggregate state machine

TS (`aggregator.ts`) defines `GeminiGenerateContentAggregateState` with:
- `parts: GeminiRecord[]` -- accumulated text parts with coalescing
- `candidates: GeminiGenerateContentCandidateAggregate[]` -- per-candidate tracking
- `citations: GeminiRecord[]` -- deduplicated via stable JSON serialization
- `groundingMetadata: GeminiRecord[]` -- deduplicated
- `thoughtSignatures: string[]` -- accumulated
- `usage: GeminiGenerateContentUsageSummary` -- 5 token counters

The spec makes no mention of Gemini needing an aggregator (it's the only provider that does, because Gemini returns array-of-candidate responses that must be merged across chunks).

### M15. Gemini SSE vs JSON stream format detection

TS (`streamBridge.ts:82-127`): Gemini streams can arrive as SSE (`text/event-stream`) or JSON arrays. The TS detects format via Content-Type header or by attempting JSON.parse first. The spec only mentions `\r\n\r\n` CRLF handling.

### M16. Gemini thought signature injection (dummy signatures)

TS (`requestBridge.ts:39-46, 256-263`): When building Gemini requests with thinking enabled, the TS injects a dummy thought signature (`c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I=`) on functionCall parts for Gemini models. If the model doesn't support dummy signatures, `shouldDisableThinkingConfig` disables the thinking config. This is non-obvious behavior critical for Gemini tool calling with thinking.

### M17. Gemini functionDeclarations and toolConfig format

TS (`requestBridge.ts:124-156, 483-508`): Gemini uses `functionDeclarations` array (not OpenAI's `functions`) and `toolConfig.functionCallingConfig` with modes `NONE`/`AUTO`/`ANY` and `allowedFunctionNames`. The spec does not document this format mapping.

### M18. Gemini inlineData for images/audio

TS (`requestBridge.ts:95-107, 348-363`): OpenAI `image_url` gets converted to Gemini's `inlineData` format with `mime_type` and `data` (base64). The spec doesn't cover media format conversion.

### M19. OpenAI chat multi-choice tool call state

TS (`streamBridge.ts:14-31`): Uses a `WeakMap<StreamTransformContext, Record<string, {id?, name?, arguments?}>>` to track tool call state across choices when multiple choices are present. Missing from spec.

### M20. Responses compact logic detail

TS (`responsesCompact.ts`): The spec gives a vague 4-step description. The actual logic includes:
- `shouldStripCompactResponsesStore`: Removes `store` field for codex/sub2api platforms
- `sanitizeCompactResponsesRequestBody`: Strips `stream`, `stream_options`, and conditionally `store`
- `ensureCompactResponsesJsonAcceptHeader`: Forces `accept: application/json` header for codex/sub2api
- `shouldForceResponsesUpstreamStream`: Forces stream=true for codex/sub2api upstream (unless already compact)
- `shouldFallbackCompactResponsesToResponses`: HTTP 404/405/501 status → fallback; also checks error text for "unknown parameter: 'stream'", "not supported", "unsupported", and "invalid url" patterns with compact hints

### M21. Tokenizer and mapper details

The spec mentions `tokenizer.go` and `mapper.go` in shared directories but provides zero detail on:
- How token counting works (which library, which models)
- How model name mapping works (which names map to which)

### M22. ProtocolTransformer interface

The TS `contracts.ts:48-78` defines a `ProtocolTransformer` interface with 6 methods (`parseRequest`, `buildProtocolRequest`, `normalizeFinal`, `normalizeStreamEvent`, `serializeFinal`, `serializeStreamEvent`). The Go module structure in the spec (separate request.go/response.go/stream.go per provider) doesn't map to this interface pattern.

### M23. NormalizedContentBlock and NormalizedUsage types

TS (`shared/normalized.ts:36-64`) defines rich types used throughout the system:
- `NormalizedContentBlockType`: 11 variants including `input_audio`, `output_audio`, `reasoning`, `redacted_reasoning`
- `NormalizedUsage`: 11 token counters including `cachedTokens`, `cacheReadTokens`, `cacheCreationTokens`, `reasoningTokens`, `audioInputTokens`, `audioOutputTokens`, `acceptedPredictionTokens`, `rejectedPredictionTokens`

The spec's `UsageInfo` is too vague compared to this.

### M24. StreamTransformContext and ClaudeDownstreamContext

TS (`shared/chatFormatsCore.ts:19-46`): `StreamTransformContext` has 11 fields (including `thinkTagParser` for real-time `<｜end▁of▁thinking｜>I now have complete coverage. Let me write the review file.

<｜｜DSML｜｜tool_calls>
<｜｜DSML｜｜invoke name="Write">
<｜｜DSML｜｜parameter name="content" string="true"># P9 Spec Review: Cross-Reference Against TypeScript Source

**Review date**: 2026-07-04 | **Spec**: `docs/specs/p9-transformers.md` | **TS base**: `src/server/transformers/`
**Files reviewed**: 20 TS source files across canonical, openai/chat, openai/responses, anthropic/messages, gemini/generate-content, shared, and proxy-core/capabilities

---

## Accuracy Issues

### A1. CanonicalMessage field name: `parts` not `content`

**Spec line 94**: `Content []CanonicalContentPart`

**TS** (`canonical/types.ts:72`): `parts: CanonicalContentPart[]`

Every consumer in the TS codebase accesses `message.parts`. The field name `Content` would break interop with any future canonical-serialized payloads.

### A2. CanonicalContentPart.Type values are incorrect (3 errors)

**Spec line 99**: `"text" | "image_url" | "tool_use" | "tool_result"`

**TS** (`canonical/types.ts:27-69`): `"text" | "image" | "file" | "tool_call" | "tool_result"`

Three specific errors:
- `"image_url"` should be `"image"`. The `image_url` name exists only in `NormalizedContentBlockType`, not canonical.
- `"tool_use"` should be `"tool_call"`. Canonical always uses `tool_call`; `tool_use` is the Anthropic-level name.
- `"file"` (CanonicalFilePart) is entirely missing from the spec's type list.

### A3. CanonicalImagePart has no nested ImageURL struct

**Spec line 101**: `ImageURL *ImageURL` (nested struct)

**TS** (`canonical/types.ts:33-38`): Flat fields `dataUrl?: string`, `url?: string`, `mimeType?: string | null`

No `ImageURL` wrapper exists. `dataUrl` is critical for inline base64 images and has no counterpart in the spec.

### A4. CanonicalRequest struct is wrong -- should be CanonicalRequestEnvelope with many more fields

**Spec lines 81-91**: Flat struct `CanonicalRequest` with `Model, Messages, Stream, MaxTokens, Temperature, TopP, Tools, ToolChoice`.

**TS** (`canonical/types.ts:99-113`): The type is `CanonicalRequestEnvelope` with these fields the spec omits entirely: `operation` (`'generate'|'count_tokens'`), `surface` (4 provider surfaces), `cliProfile` (4 CLI profiles), `requestedModel` (not `Model`), `reasoning` (CanonicalReasoningRequest), `continuation` (CanonicalContinuation), `metadata`, `passthrough`, `attachments`.

The spec's `MaxTokens`, `Temperature`, `TopP` fields do not exist on the canonical envelope -- these are upstream-specific parameters handled during bridge conversion, not stored in canonical form.

### A5. Missing `developer` role in CanonicalMessageRole

**Spec line 94**: `"system" | "user" | "assistant" | "tool"`

**TS** (`canonical/types.ts:20-25`): `'system' | 'developer' | 'user' | 'assistant' | 'tool'`

`developer` is OpenAI's newer system-message alternative and is handled throughout the bridge code.

### A6. CanonicalMessage missing `phase` and `reasoningSignature` fields

**TS** (`canonical/types.ts:71-76`): `phase?: string; reasoningSignature?: string`

Both used in Anthropic/Gemini roundtrips. The spec mentions neither.

### A7. CanonicalTextPart missing `thought: boolean` field

**TS** (`canonical/types.ts:27-31`): `CanonicalTextPart` has `thought?: boolean` to mark reasoning text. Used by Anthropic thinking blocks and Gemini thought signatures. The spec's text part has no such field.

### A8. CanonicalTool is a discriminated union (CanonicalFunctionTool | CanonicalRawTool), not a flat struct

**Spec line 88**: Implies flat struct with `name, description, input_schema`.

**TS** (`canonical/tools.ts:13-15`):
```ts
type CanonicalTool = CanonicalFunctionTool | CanonicalRawTool;
type CanonicalFunctionTool = { name, description?, strict?: boolean, inputSchema? };
type CanonicalRawTool = { type: string; raw: Record<string, unknown> };
```

`CanonicalRawTool` is critical for non-OpenAI tools (Gemini googleSearch/codeExecution, Anthropic web_search). The `strict` boolean on `CanonicalFunctionTool` is also missing.

### A9. CanonicalToolChoice is not `interface{}`

**Spec line 89**: `ToolChoice interface{}`

**TS** (`canonical/tools.ts:17-28`): `'auto' | 'none' | 'required' | {type: 'tool'; name: string} | {type: 'raw'; value: string | Record<string, unknown>}`

The `{type: 'raw'}` variant carries non-OpenAI tool_choice values (e.g., Anthropic `{type: 'any', disable_parallel_tool_use: true}`). Typing as `interface{}` loses the discriminated union.

### A10. CanonicalStreamChunk and CanonicalDelta types do not exist in TS

**Spec lines 106-117**: Defines `CanonicalStreamChunk` and `CanonicalDelta` with `Index`, `Role`, `Content`, `ToolCalls`.

**TS**: These types do not exist. The TS uses `NormalizedStreamEvent` (`shared/chatFormatsCore.ts:48-62`):
```ts
type NormalizedStreamEvent = {
  role?: 'assistant';
  contentDelta?: string;          // incremental, not full Content
  reasoningDelta?: string;        // missing from spec entirely
  reasoningSignature?: string;    // missing from spec entirely
  redactedReasoningContent?: string; // missing from spec entirely
  toolCallDeltas?: Array<{
    index: number;
    id?: string;
    name?: string;
    argumentsDelta?: string;      // incremental JSON fragments, not full arguments
  }>;
  finishReason?: string | null;
  done?: boolean;                 // stream termination flag
};
```

Key differences: no `Index` field, `ContentDelta` not `Content`, `reasoningDelta`/`reasoningSignature`/`redactedReasoningContent`/`done` all missing from spec, tool calls use `argumentsDelta` (incremental JSON fragments).

### A11. Unified CanonicalDecoder / SSEEncoder architecture does not exist in TS

**Spec lines 122-135**: Describes a generic `CanonicalDecoder` with `bufio.Scanner` and `UpstreamFormat` + `SSEEncoder` with `io.Writer` and `DownstreamFormat`.

**TS**: No unified decoder/encoder exists. Each provider has its own stream module:

| Provider | Module | Key Pattern |
|----------|--------|-------------|
| Anthropic | `anthropicMessagesStream` | Named SSE events (`event: message_start\ndata:`), content block state machine, signature deltas |
| OpenAI Chat | `openAiChatStream` | Standard SSE + multi-choice tool call state via WeakMap |
| Gemini | `geminiGenerateContentStream` | Dual format (SSE + JSON array), aggregate state machine |

A generic `bufio.Scanner` would not correctly handle Anthropic's `event:` line prefix or Gemini's JSON array streams.

### A12. SSE format varies by provider -- not all use `data: {...}\n\n`

**Spec AC** (line 174): "所有 SSE chunk 格式: `data: {...}\n\n` 并以 `data: [DONE]\n\n` 结束"

**TS**: Anthropic uses `event: <name>\ndata: {...}\n\n` format (e.g., `event: message_start\ndata: {"type":"message_start",...}\n\n`). This acceptance criterion is factually incorrect for the Anthropic downstream path.

---

## Missing Details

### M1. CanonicalOperation type (`'generate' | 'count_tokens'`)

Drives whether this is a generation or token-counting request. Missing from spec.

### M2. CanonicalSurface type (4 provider surfaces)

`'openai-chat' | 'openai-responses' | 'anthropic-messages' | 'gemini-generate-content'`. Used for routing and metadata tracking.

### M3. CanonicalCliProfile type (4 CLI profiles)

`'generic' | 'codex' | 'claude_code' | 'gemini_cli'`. Critical for ProxyCore routing decisions about which upstream format to use.

### M4. CanonicalReasoningRequest (4 fields)

`effort` (5 levels), `budgetTokens`, `summary`, `includeEncryptedContent`. The `summary` field enables reasoning summarization; `includeEncryptedContent` controls encrypted reasoning content requests.

### M5. CanonicalContinuation (4 fields)

`sessionId`, `previousResponseId`, `promptCacheKey`, `turnState`. Used for multi-turn conversation continuation, especially for Codex sessions.

### M6. CanonicalAttachment type

File attachment handling with `CanonicalFilePart` (fileId/fileUrl/fileData/filename/mimeType).

### M7. Anthropic cache_control optimization (extensive)

TS (`conversion.ts:528-701`): Automatic cache_control placement algorithm with:
- `MAX_ANTHROPIC_CACHE_CONTROL_BREAKPOINTS = 4` (Anthropic hard limit)
- `ADAPTIVE_ANTHROPIC_CACHE_CONTROL_BLOCK_WINDOW = 20`
- Structural anchors (last tool, last system prompt)
- Adaptive message anchors (last message + one anchor ~20 blocks back)
- Non-cacheable block filtering (thinking, redacted_thinking)
- Controlled by `autoOptimizeCacheControls` option

### M8. Anthropic thinking config validation

TS (`conversion.ts:140-177`): Validates `thinking.type` (`enabled`/`disabled`/`adaptive`), requires positive `budget_tokens` for `enabled`, promotes `adaptive`+`budget_tokens` to `enabled`, validates `output_config.effort`.

### M9. Anthropic tool_choice sanitization

TS (`conversion.ts:97-138`): Maps `"required"` to `{type: 'any'}`, validates `{type: 'tool'}` requires name, handles `disable_parallel_tool_use` from `parallel_tool_calls: false`.

### M10. Anthropic block kinds: `thinking` and `redacted_thinking`

TS (`streamBridge.ts:20`): `'thinking' | 'text' | 'tool_use' | 'redacted_thinking'`. The spec only mentions text and tool_use. `thinking` blocks carry Claude's extended thinking. `redacted_thinking` blocks carry safety-filtered reasoning content.

### M11. Anthropic `signature_delta` sub-event

TS (`streamBridge.ts:276-286`): `content_block_delta` with `delta.type = 'signature_delta'` for reasoning signatures. Entirely absent from spec.

### M12. Anthropic `server_tool_use` and `web_search_tool_result` blocks

TS (`streamBridge.ts:696-707`): Special handling for Anthropic's server-side web search tool blocks. Emitted as content_block_start/stop pairs.

### M13. Anthropic system prompt normalization (string to array)

TS (`conversion.ts:545-554`): Converts `system: "text"` to `system: [{type: 'text', text: "text"}]`.

### M14. Anthropic single-string message content to array

TS (`conversion.ts:528-543`): Converts `content: "hello"` to `content: [{type: 'text', text: "hello"}]`.

### M15. Anthropic tool_result content normalization

TS (`conversion.ts:240-295`): Complex normalization of tool_result content -- string passthrough, array filtering for text/image/file/tool_reference blocks, single image/file wrapping, JSON fallback.

### M16. Anthropic stop_reason mapping

TS (`streamBridge.ts:758-762`): `stop`->`end_turn`, `length`->`max_tokens`, `tool_calls`->`tool_use`. Missing from spec.

### M17. Gemini aggregate state machine

TS (`aggregator.ts`): Full `GeminiGenerateContentAggregateState` with parts coalescing (adjacent text parts merged), candidate tracking, deduplicated citations/groundingMetadata (via stable JSON serialization), thought signature accumulation, 5-field usage summary. Gemini is the only provider requiring an aggregator because it returns array-of-candidate responses merged across chunks.

### M18. Gemini dual stream format (SSE + JSON array)

TS (`streamBridge.ts:82-127`): Detects format via Content-Type header or by attempting JSON.parse. SSE events separated by `\r?\n\r?\n`. JSON arrays parsed directly. The spec only mentions `\r\n\r\n` CRLF.

### M19. Gemini dummy thought signature injection

TS (`requestBridge.ts:39-46, 256-263`): Injects `c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I=` (base64 for "skip_thought_signature_validator") on functionCall parts when thinking is enabled. Disables thinking config entirely for non-Gemini models. This is critical for Gemini tool calling with thinking.

### M20. Gemini functionDeclarations and toolConfig.functionCallingConfig mapping

TS (`requestBridge.ts:124-156, 483-508`): OpenAI `tools[{type:'function',function:{name,...}}]` maps to Gemini `tools[{functionDeclarations:[{name,...}]}]`. OpenAI `tool_choice` maps to Gemini `toolConfig.functionCallingConfig` with modes `NONE`/`AUTO`/`ANY` and optional `allowedFunctionNames`.

### M21. Gemini inlineData media conversion

TS (`requestBridge.ts:95-107, 348-363`): OpenAI `image_url` to Gemini `inlineData{mime_type, data}`. Also `input_audio` handled. File URIs become `fileData{fileUri, mimeType}`.

### M22. OpenAI chat multi-choice tool call state tracking

TS (`streamBridge.ts:14-31`): `WeakMap<StreamTransformContext, Record<string, {id?, name?, arguments?}>>` to accumulate tool call state across multi-choice stream events.

### M23. OpenAI chat annotations/citations on stream chunks

TS (`streamBridge.ts:165-203`): Injects `annotations` into `delta.annotations`, `citations` onto the chunk root, and `usage`/`usageDetails` (prompt_tokens_details/completion_tokens_details) onto stream chunks.

### M24. Responses compact -- full logic significantly more detailed than spec

The spec gives a vague 4-line description. The actual TS (`responsesCompact.ts`) implements:

| Function | Behavior |
|----------|----------|
| `shouldStripCompactResponsesStore` | True for codex/sub2api platforms |
| `sanitizeCompactResponsesRequestBody` | Removes `stream`, `stream_options`, conditionally `store` |
| `ensureCompactResponsesJsonAcceptHeader` | Forces `accept: application/json` for codex/sub2api |
| `shouldForceResponsesUpstreamStream` | Forces stream=true for codex/sub2api upstream unless compact |
| `shouldFallbackCompactResponsesToResponses` | HTTP 404/405/501 -> fallback; also checks error text for "unknown parameter: 'stream'", "not supported", "unsupported", "invalid url" patterns with compact hints |

### M25. NormalizedUsage type (11 token counters)

TS (`shared/normalized.ts:52-64`): `promptTokens`, `completionTokens`, `totalTokens`, `cachedTokens`, `cacheReadTokens`, `cacheCreationTokens`, `reasoningTokens`, `audioInputTokens`, `audioOutputTokens`, `acceptedPredictionTokens`, `rejectedPredictionTokens`. The spec's undefined `UsageInfo` is far too vague.

### M26. StreamTransformContext and ClaudeDownstreamContext

TS (`shared/chatFormatsCore.ts:19-46`): `StreamTransformContext` has 11 fields including `thinkTagParser` for real-time `<｜end▁of▁thinking｜>...` tag parsing, `responsesToolCallIndexByOutputIndex` for OpenAI responses format tool call tracking. `ClaudeDownstreamContext` tracks Anthropic content block state (messageStarted, contentBlockStarted, textBlockIndex, nextContentBlockIndex, toolBlocks).

### M27. ProtocolTransformer interface

TS (`contracts.ts:48-78`): 6-method interface (`parseRequest`, `buildProtocolRequest`, `normalizeFinal`, `normalizeStreamEvent`, `serializeFinal`, `serializeStreamEvent`). The Go module structure in the spec (separate request.go/response.go/stream.go per provider) does not map to this interface.

### M28. Think tag parsing for streaming reasoning

TS (`shared/thinkTagParser.ts`): Real-time `<｜end▁of▁thinking｜>...` tag detection in streaming text to separate reasoning from visible content. Used by the generic `normalizeUpstreamStreamEvent` path.

### M29. NormalizedResponseEnvelope and NormalizedStreamEnvelope

TS (`shared/normalized.ts:100-114`): Wrap protocol info, model, content blocks, and metadata around final/stream events for cross-provider transport.

### M30. Codex as a surface (no separate codex/ directory in TS)

The spec shows `codex/responses/request.go` and `codex/responses/response.go` under the Go module structure. But there is no corresponding `codex/` directory in the TS transformers. Codex is handled through the OpenAI responses format with `cliProfile: 'codex'` in the canonical envelope. The Go module structure should reflect this.

---

## Edge Cases Not Covered

### E1. Anthropic thinking blocks do NOT "not generate chunks"

**Spec line 198**: "Anthropic `content_block_stop` 不产生 output chunk" and line 201: "空 content block → 不生成 chunk (Anthropic thinking 块)"

**TS reality**: Thinking blocks DO generate events -- `content_block_start` (type: thinking), `content_block_delta` (thinking_delta), `content_block_delta` (signature_delta), then `content_block_stop`. They are real SSE events, not silently skipped. The spec conflates two different edge cases (content_block_stop events and thinking blocks).

### E2. Anthropic redacted_thinking blocks

When Claude safety-filters reasoning, it emits `redacted_thinking` blocks with `data` field instead of `thinking`. The TS handles these as separate block starts/stops. Not mentioned in spec.

### E3. Anthropic block close ordering

TS (`streamBridge.ts:356-363, 527-555`): When opening a new content block, previously open blocks of incompatible types must be closed first. The close ordering is: redacted -> text -> thinking -> tool_use. Starting a new tool_use block also closes the previous tool_use block if it's from a different tool slot.

### E4. Anthropic pending signature buffering

TS (`streamBridge.ts:286-293`): `signature_delta` events can arrive before the `thinking` block starts (in `message_start`). The TS buffers these and emits them after the thinking block starts.

### E5. Anthropic message_id construction

TS (`streamBridge.ts:224-228, responseBridge.ts:43-47`): If the upstream id starts with `msg_`, use as-is. Otherwise sanitize non-alphanumeric chars and prefix with `msg_`. Fallback to `msg_<timestamp>` if empty after sanitization.

### E6. Anthropic tool_use id fallback

TS (`streamBridge.ts:456`): Tool blocks without an explicit id get `toolu_<slot>` as fallback. Similarly `name` defaults to `tool_<slot>`.

### E7. Anthropic empty content fallback

TS (`streamBridge.ts:145-150, responseBridge.ts:107-109`): If all content blocks are empty, a single empty text block `{type: 'text', text: ''}` is inserted to satisfy Anthropic's API requirement.

### E8. Anthropic temperature + top_p mutual exclusion

TS (`conversion.ts:732-736`): When both `temperature` and `top_p` are present (both are finite numbers), `top_p` is deleted because Anthropic only accepts one.

### E9. Gemini model name fallback

TS (`requestBridge.ts:518`): `parseGeminiGenerateContentRequestToCanonical` tries `body.model` first, then falls back to `ctx?.metadata?.requestedModel`. Missing from spec.

### E10. Gemini functionCall parts with thought signatures split from text parts

TS (`requestBridge.ts:267-282`): When functionCall parts have `thoughtSignature` and there are also text parts, they must be split into separate `contents` entries (text parts in one, functionCall parts in another) to satisfy Gemini's API requirement.

### E11. Gemini stream=false non-streaming response wrapping

**Spec line 200**: "上游返回非流式响应 (stream=false) → 包装为单 chunk SSE"

**TS reality**: Both Anthropic (`serializeAnthropicFinalAsStream`) and OpenAI (`buildSyntheticOpenAiChunks`) synthesize MULTIPLE chunks from a non-streaming response, not a single chunk. The Anthropic path generates: message_start, content_block_start(s), content_block_delta(s), content_block_stop(s), message_delta, message_stop.

### E12. OpenAI chat multi-choice non-zero-indexed choices

TS (`streamBridge.ts:91-155`): When multiple choices exist or the first choice has index != 0, the TS serializes all choices in a single chunk with a custom format, bypassing the normal `serializeNormalizedStreamEvent` path.

### E13. OpenAI parallel_tool_calls: false -> Anthropic disable_parallel_tool_use

TS (`conversion.ts:1072-1077`): When `parallel_tool_calls` is `false` in the OpenAI request, the TS adds `disable_parallel_tool_use: true` to the Anthropic tool_choice object. Missing from spec.

### E14. Anthropic metadata.user_id passthrough

TS (`conversion.ts:1047-1053`): Only `user_id` from OpenAI's `metadata` object is forwarded to Anthropic's `metadata`. Other metadata fields are dropped.

### E15. Gemini 4xx field filtering

TS (`inbound.ts:199-209`): Only 9 specific keys are forwarded to Gemini's API (`contents`, `systemInstruction`, `cachedContent`, `safetySettings`, `generationConfig`, `tools`, `toolConfig`, `labels`, `model`). Unknown fields cause upstream 400 errors. The spec does not mention this.

### E16. Anthropic empty message after sanitization

TS (`conversion.ts:447-465`): `sanitizeAnthropicMessage` returns `null` for messages that become empty after sanitizing content blocks (all blocks filtered out). These nulls are filtered from the messages array.

### E17. Anthropic temperature NaN/Infinity handling

TS (`conversion.ts:25-26`): `toFiniteNumber` returns `null` for `NaN`, `Infinity`, `-Infinity`. Non-finite temperature values are silently dropped rather than causing errors.

### E18. Empty finish_reason defaulting

TS (`responseBridge.ts:121`): When `finishReason` is empty/falsy and no tool calls exist, defaults to `'stop'`. When tool calls exist, defaults to `'tool_calls'`.

### E19. OpenAI chat chunk annotations placement

TS (`streamBridge.ts:165-203`): Annotations are injected into `delta.annotations` of the first choice. Citations are injected at the chunk root level. Usage details (`prompt_tokens_details`, `completion_tokens_details`) are merged into `usage`.

---

## Incorrect Details

### I1. "Gemini 用 `\r\n\r\n` 作为事件分隔符"

**Spec line 198**: States Gemini specifically uses `\r\n\r\n`.

**TS** (`streamBridge.ts:37`): Uses `/\r?\n\r?\n/` (any double-newline). This is standard SSE parsing, not Gemini-specific. The spec implies this is a Gemini quirk when it is universal.

### I2. "Responses Compact: 收集完整 response → 按 conversation 语义重新组织 → 合并冗余 content blocks → 减少 chunk 数量"

**Spec lines 161-165**: Describes compact as a content-level optimization.

**TS** (`responsesCompact.ts`): The actual compact logic is a request/header transformation, not a response transformation. It strips stream parameters, forces JSON accept headers, removes store for certain platforms, and has error-based fallback heuristics. There is no "collect response and reorganize" step.

### I3. Go module structure shows codex/responses/ but no TS codex/ directory

**Spec lines 56-61**: Lists `codex/responses/request.go` and `codex/responses/response.go`.

**TS**: There is no `codex/` directory under transformers. Codex is handled entirely through the OpenAI responses format with `cliProfile: 'codex'` on the canonical envelope. The codex-specific logic lives in `openai/responses/codexCompatibility.ts`.

### I4. Conversion flow diagram oversimplifies

**Spec lines 67-77**: Shows linear Request Transformer -> ProxyCore -> Response Transformer pipeline.

**TS reality**: The flow is more nuanced:
1. Inbound parse (provider-specific validation/normalization)
2. Conversion to OpenAI-chat body (for Anthropic/Gemini -> canonical pivot)
3. Canonical envelope construction (via `canonicalRequestFromOpenAiBody`)
4. ProxyCore routing (uses `surface` + `cliProfile` from envelope)
5. Canonical -> upstream format conversion (via `canonicalRequestToOpenAiChatBody` or provider-specific `buildCanonicalRequestTo...`)
6. Upstream execution
7. Stream normalization (NormalizedStreamEvent)
8. Downstream serialization (provider-specific SSE)

### I5. Tool call cross-protocol mapping oversimplified

**Spec AC**: "Tool calls 跨协议正确保留 (Anthropic tool_use ↔ OpenAI function call)"

**TS**: This is much more than name mapping. The TS:
- Converts `tool_use.input` (object) to `function.arguments` (JSON string)
- Handles partial JSON deltas in streaming (`argumentsDelta` field, `input_json_delta` event)
- Tracks tool call state across SSE chunks by index
- Maps `tool_use_id` to `tool_call_id` for tool_result
- Normalizes tool_result content (string/array/object)
- Converts web_search tools between Anthropic and OpenAI formats

### I6. "Anthropic SSE input format" list incomplete

**Spec line 157**: `event: message_start / content_block_start / content_block_delta / content_block_stop / message_delta / message_stop / ping`

**TS** (`streamBridge.ts:41-50`): Also includes `error` event. The `error` event type is critical for error propagation.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 12 |
| Missing Details | 30 |
| Edge Cases Not Covered | 19 |
| Incorrect Details | 6 |
| **Total findings** | **67** |

**Verdict**: **NEEDS_REVISION**

The spec is a reasonable high-level sketch but contains significant structural errors in the canonical type definitions (wrong field names, wrong type names, missing critical types like CanonicalReasoningRequest/CanonicalContinuation/CanonicalRawTool, and entirely invented types like CanonicalDecoder/SSEEncoder that don't exist in TS). The SSE handling section is the weakest -- it describes a unified decoder/encoder pattern that does not match the provider-specific stream modules in TS. The responses compact description is factually wrong (it describes response transformation but the TS implements request/header transformation). Key provider-specific behaviors (Anthropic cache_control, Gemini aggregate state machine, Anthropic content block state machine) are entirely absent. The Go module structure should be revised to reflect the actual TS architecture: no separate `codex/` directory, and each provider needs a stream module alongside request/response bridges.
