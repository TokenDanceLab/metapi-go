# P9: Protocol Transformers -- OpenAI/Anthropic/Gemini/Codex Canonical + SSE Stream

**S.U.P.E.R**: S (single responsibility) -- P (ports-first) -- E (env-agnostic) -- R (replaceable) | **Depends on**: P8 | **Size**: XL

## Original TS Reference

- `src/server/transformers/canonical/` -- types.ts, tools.ts, attachments.ts, reasoning.ts, envelope.ts, continuationBridge.ts, openAiRequestBridge.ts
- `src/server/transformers/openai/chat/` -- inbound.ts, requestBridge.ts, responseBridge.ts, streamBridge.ts, aggregator.ts, helpers.ts
- `src/server/transformers/openai/responses/` -- inbound.ts, requestBridge.ts, responseBridge.ts, streamBridge.ts, aggregator.ts, codexCompatibility.ts
- `src/server/transformers/anthropic/messages/` -- inbound.ts, conversion.ts, requestBridge.ts, responseBridge.ts, streamBridge.ts, aggregator.ts
- `src/server/transformers/gemini/generate-content/` -- inbound.ts, compatibility.ts, requestBridge.ts, responseBridge.ts, streamBridge.ts, aggregator.ts, convert.ts
- `src/server/transformers/shared/` -- chatFormatsCore.ts (normalize+serialize), normalized.ts (types), thinkTagParser.ts, inputFile.ts, reasoningTransport.ts
- `src/server/proxy-core/capabilities/responsesCompact.ts`

## Go Module Structure

```
transform/
  canonical/
    types.go             # All canonical types in one file
    envelope.go          # createCanonicalRequestEnvelope factory
    openai_bridge.go     # canonicalRequestFromOpenAiBody / canonicalRequestToOpenAiChatBody
  openai/
    chat/
      inbound.go         # Parse + validate OpenAI chat request
      request_bridge.go  # CanonicalRequestEnvelope -> upstream OpenAI chat body
      response_bridge.go # Upstream final -> NormalizedFinalResponse; NormalizedFinal -> chat completion
      stream_bridge.go   # openAiChatStream: normalize/serialize, multi-choice handling
      aggregator.go      # buildSyntheticOpenAiChunks (non-streaming -> synthetic stream)
    responses/
      inbound.go         # Parse + validate responses request
      request_bridge.go  # CanonicalRequestEnvelope -> upstream responses body
      response_bridge.go # Upstream -> NormalizedFinalResponse; NormalizedFinal -> responses payload
      stream_bridge.go   # openAiResponsesStream: normalize/serialize
      compact.go         # responsesCompact: sanitize request, force headers, fallback
    completions/
      request.go
      response.go
    embeddings/
      request.go
      response.go
    images/
      request.go
      response.go
    shared/
      tokenizer.go
      mapper.go
  anthropic/
    messages/
      inbound.go         # sanitizeAnthropicMessagesBody: validate, normalize, cache_control
      conversion.go      # OpenAI body <-> Anthropic body conversion
      request_bridge.go  # CanonicalRequestEnvelope -> upstream Anthropic body
      response_bridge.go # Upstream -> NormalizedFinalResponse; NormalizedFinal -> Anthropic message
      stream_bridge.go   # anthropicMessagesStream: content block state machine, raw SSE passthrough
      aggregator.go      # Non-streaming -> synthetic message_start/block/delta/stop stream
    shared/
      tokenizer.go
  gemini/
    generate_content/
      inbound.go         # Gemini inbound parse + 4xx field filtering
      compatibility.go   # Gemini body -> OpenAI body (canonical pivot)
      request_bridge.go  # OpenAI body + canonical -> Gemini body, thought sig injection
      response_bridge.go # Aggregate state -> Gemini response/SSE
      stream_bridge.go   # geminiGenerateContentStream: dual-format, aggregate
      aggregator.go      # GeminiGenerateContentAggregateState
      convert.go         # reasoningEffort <-> thinkingConfig mapping
    shared/
      tokenizer.go
```

**No separate `codex/` directory.** Codex uses the OpenAI responses format with `cliProfile: 'codex'` on the canonical envelope. Codex-specific logic is in `openai/responses/codexCompatibility.ts`.

## Conversion Flow (8-stage precise architecture)

```
Downstream Client Request (any format)
    |
    v
[1] Inbound parse + validate (provider-specific)
    |   Anthropic: sanitizeAnthropicMessagesBody (content blocks, thinking, tool_choice, T+P exclusion, cache)
    |   Gemini: geminiGenerateContentInbound (9-key field filtering)
    |   OpenAI: model + messages presence
    |
    v
[2] Convert to OpenAI-chat body (non-OpenAI downstream formats)
    |   Anthropic: convertClaudeRequestToOpenAiBody
    |   Gemini: buildOpenAiBodyFromGeminiRequest
    |
    v
[3] Canonical envelope construction
    |   canonicalRequestFromOpenAiBody(body, surface, cliProfile, operation, metadata)
    |   Resolves: reasoning, continuation, tools, toolChoice, attachments
    |
    v
[4] ProxyCore routing (surface + cliProfile + requestedModel from envelope)
    |
    v
[5] Canonical -> upstream format conversion
    |   OpenAI: canonicalRequestToOpenAiChatBody
    |   Anthropic: buildAnthropicMessagesRequestFromCanonical
    |   Gemini: buildGeminiGenerateContentRequestFromOpenAi
    |
    v
[6] Upstream HTTP execution
    |
    v
[7] Stream normalization
    |   pullSseEventsWithDone -> parse event:/data: lines
    |   normalizeUpstreamStreamEvent -> NormalizedStreamEvent (inline think tag parsing)
    |
    v
[8] Downstream serialization
    |   serializeNormalizedStreamEvent(format, event, ctx, claudeCtx) -> []string
    |   serializeStreamDone(format, ctx, claudeCtx) -> []string
    |
    v
Downstream Client SSE Response
```

## Canonical Intermediate Types (precise, from TS source)

### CanonicalRequestEnvelope (NOT "CanonicalRequest")

```go
type CanonicalOperation string
const (
    OpGenerate    CanonicalOperation = "generate"
    OpCountTokens CanonicalOperation = "count_tokens"
)

type CanonicalSurface string
const (
    SurfaceOpenAIChat           CanonicalSurface = "openai-chat"
    SurfaceOpenAIResponses      CanonicalSurface = "openai-responses"
    SurfaceAnthropicMessages    CanonicalSurface = "anthropic-messages"
    SurfaceGeminiGenerateContent CanonicalSurface = "gemini-generate-content"
)

type CanonicalCliProfile string
const (
    ProfileGeneric   CanonicalCliProfile = "generic"
    ProfileCodex     CanonicalCliProfile = "codex"
    ProfileClaudeCode CanonicalCliProfile = "claude_code"
    ProfileGeminiCLI  CanonicalCliProfile = "gemini_cli"
)

type CanonicalRequestEnvelope struct {
    Operation      CanonicalOperation           `json:"operation"`
    Surface        CanonicalSurface             `json:"surface"`
    CliProfile     CanonicalCliProfile          `json:"cliProfile"`
    RequestedModel string                       `json:"requestedModel"` // NOT "Model"
    Stream         bool                         `json:"stream"`
    Messages       []CanonicalMessage           `json:"messages"`
    Reasoning      *CanonicalReasoningRequest   `json:"reasoning,omitempty"`
    Tools          []CanonicalTool              `json:"tools,omitempty"`
    ToolChoice     *CanonicalToolChoice         `json:"toolChoice,omitempty"`
    Continuation   *CanonicalContinuation       `json:"continuation,omitempty"`
    Metadata       map[string]any               `json:"metadata,omitempty"`
    Passthrough    map[string]any               `json:"passthrough,omitempty"`
    Attachments    []CanonicalAttachment        `json:"attachments,omitempty"`
}
```

NO `MaxTokens`, `Temperature`, or `TopP` fields. Those are upstream-specific, handled in bridge conversion.

### CanonicalMessage

```go
type CanonicalMessageRole string
const (
    RoleSystem    CanonicalMessageRole = "system"
    RoleDeveloper CanonicalMessageRole = "developer" // OpenAI system-message alternative
    RoleUser      CanonicalMessageRole = "user"
    RoleAssistant CanonicalMessageRole = "assistant"
    RoleTool      CanonicalMessageRole = "tool"
)

type CanonicalMessage struct {
    Role               CanonicalMessageRole  `json:"role"`
    Parts              []CanonicalContentPart `json:"parts"` // NOT "content"
    Phase              string                `json:"phase,omitempty"`
    ReasoningSignature string                `json:"reasoningSignature,omitempty"`
}
```

### CanonicalContentPart (5 subtypes, discriminated by type)

```go
type CanonicalContentPartType string
const (
    PartText       CanonicalContentPartType = "text"       // NOT "image_url" here
    PartImage      CanonicalContentPartType = "image"
    PartFile       CanonicalContentPartType = "file"
    PartToolCall   CanonicalContentPartType = "tool_call"   // NOT "tool_use"
    PartToolResult CanonicalContentPartType = "tool_result"
)

type CanonicalContentPart struct {
    Type CanonicalContentPartType `json:"type"`

    // text
    Text    string `json:"text,omitempty"`
    Thought bool   `json:"thought,omitempty"` // reasoning/thinking marker

    // image (flat fields, NO ImageURL wrapper)
    DataURL  string  `json:"dataUrl,omitempty"` // base64 inline
    URL      string  `json:"url,omitempty"`
    MimeType *string `json:"mimeType,omitempty"`

    // file
    FileID   string `json:"fileId,omitempty"`
    FileURL  string `json:"fileUrl,omitempty"`
    FileData string `json:"fileData,omitempty"`
    Filename string `json:"filename,omitempty"`

    // tool_call
    ID            string `json:"id,omitempty"`
    Name          string `json:"name,omitempty"`
    ArgumentsJSON string `json:"argumentsJson,omitempty"` // full JSON string

    // tool_result
    ToolCallID    string `json:"toolCallId,omitempty"`
    ResultText    string `json:"resultText,omitempty"`
    ResultJSON    any    `json:"resultJson,omitempty"`
    ResultContent any    `json:"resultContent,omitempty"` // string | []string | []map[string]any
}
```

### CanonicalTool (function | raw union)

```go
type CanonicalFunctionTool struct {
    Name        string         `json:"name"`
    Description string         `json:"description,omitempty"`
    Strict      bool           `json:"strict,omitempty"`
    InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type CanonicalRawTool struct {
    Type string         `json:"type"`
    Raw  map[string]any `json:"raw"`
}
// Discriminate: if Name is non-empty -> function tool; else raw tool
```

CanonicalRawTool carries non-OpenAI tools: Gemini googleSearch/codeExecution, Anthropic web_search.

### CanonicalToolChoice (NOT interface{})

5 variants: `"auto"`, `"none"`, `"required"`, `{type:"tool", name: string}`, `{type:"raw", value: string|map[string]any}`.

The `{type:"raw"}` variant carries non-OpenAI values like Anthropic `{type:"any", disable_parallel_tool_use: true}`.

### CanonicalReasoningRequest

```go
type CanonicalReasoningEffort string
const (
    ReasoningEffortNone   CanonicalReasoningEffort = "none"
    ReasoningEffortLow    CanonicalReasoningEffort = "low"
    ReasoningEffortMedium CanonicalReasoningEffort = "medium"
    ReasoningEffortHigh   CanonicalReasoningEffort = "high"
    ReasoningEffortMax    CanonicalReasoningEffort = "max"
)

type CanonicalReasoningRequest struct {
    Effort                  CanonicalReasoningEffort `json:"effort,omitempty"`
    BudgetTokens            int                      `json:"budgetTokens,omitempty"`
    Summary                 string                   `json:"summary,omitempty"`
    IncludeEncryptedContent bool                     `json:"includeEncryptedContent,omitempty"`
}
```

### CanonicalContinuation

```go
type CanonicalContinuation struct {
    SessionID          string `json:"sessionId,omitempty"`
    PreviousResponseID string `json:"previousResponseId,omitempty"`
    PromptCacheKey     string `json:"promptCacheKey,omitempty"`
    TurnState          string `json:"turnState,omitempty"` // carried in metadata key "metapi_turn_state"
}
```

### CanonicalAttachment

```go
type CanonicalAttachment struct {
    Kind       string  `json:"kind"`                 // "file"
    SourceType string  `json:"sourceType,omitempty"` // "file" | "input_file"
    FileID     string  `json:"fileId,omitempty"`
    FileURL    string  `json:"fileUrl,omitempty"`
    FileData   string  `json:"fileData,omitempty"`
    Filename   string  `json:"filename,omitempty"`
    MimeType   *string `json:"mimeType,omitempty"`
}
```

## Shared Normalized Types (stream processing layer)

### NormalizedStreamEvent (replaces old non-existent "CanonicalDelta")

```go
type NormalizedStreamEvent struct {
    Role                     string          `json:"role,omitempty"`          // "assistant"
    ContentDelta             string          `json:"contentDelta,omitempty"`  // incremental
    ReasoningDelta           string          `json:"reasoningDelta,omitempty"`
    ReasoningSignature       string          `json:"reasoningSignature,omitempty"`
    RedactedReasoningContent string          `json:"redactedReasoningContent,omitempty"`
    ToolCallDeltas           []ToolCallDelta `json:"toolCallDeltas,omitempty"`
    FinishReason             string          `json:"finishReason,omitempty"` // "stop"|"length"|"tool_calls"|"error"
    Done                     bool            `json:"done,omitempty"`
}

type ToolCallDelta struct {
    Index          int    `json:"index"`
    ID             string `json:"id,omitempty"`
    Name           string `json:"name,omitempty"`
    ArgumentsDelta string `json:"argumentsDelta,omitempty"` // incremental JSON fragment
}
```

No `Index` field on the event. `ContentDelta` is incremental. `ArgumentsDelta` is partial JSON.

### NormalizedFinalResponse

```go
type NormalizedFinalResponse struct {
    ID                       string     `json:"id"`
    Model                    string     `json:"model"`
    Created                  int64      `json:"created"`
    Content                  string     `json:"content"`
    ReasoningContent         string     `json:"reasoningContent"`
    ReasoningSignature       string     `json:"reasoningSignature,omitempty"`
    RedactedReasoningContent string     `json:"redactedReasoningContent,omitempty"`
    FinishReason             string     `json:"finishReason"`
    ToolCalls                []ToolCall `json:"toolCalls"`
}

type ToolCall struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // full JSON
}
```

### NormalizedUsage (11 counters)

```go
type NormalizedUsage struct {
    PromptTokens             int `json:"promptTokens"`
    CompletionTokens         int `json:"completionTokens"`
    TotalTokens              int `json:"totalTokens"`
    CachedTokens             int `json:"cachedTokens"`
    CacheReadTokens          int `json:"cacheReadTokens"`
    CacheCreationTokens      int `json:"cacheCreationTokens"`
    ReasoningTokens          int `json:"reasoningTokens"`
    AudioInputTokens         int `json:"audioInputTokens"`
    AudioOutputTokens        int `json:"audioOutputTokens"`
    AcceptedPredictionTokens int `json:"acceptedPredictionTokens"`
    RejectedPredictionTokens int `json:"rejectedPredictionTokens"`
}
```

### StreamTransformContext

```go
type StreamTransformContext struct {
    ID                              string
    Model                           string
    Created                         int64
    RoleSent                        bool
    DoneSent                        bool
    ToolCalls                       map[int]*ToolCallAccumulator // accumulated across chunks
    ResponsesToolCallIndexByOutputIndex map[int]int
    ResponsesToolCallIndexByID       map[string]int  // "item:<id>" | "call:<id>"
    NextResponsesToolCallIndex       int
    ResponsesTextByIndex             map[int]string
    ResponsesReasoningByIndex        map[int]string
    ThinkTagParser                   *ThinkTagParserState
}

type ToolCallAccumulator struct {
    ID        string
    Name      string
    Arguments string // accumulated partial JSON
}
```

### ClaudeDownstreamContext (Extended for Anthropic)

```go
type ClaudeDownstreamContext struct {
    MessageStarted        bool
    ContentBlockStarted   bool
    DoneSent              bool
    TextBlockIndex        *int
    NextContentBlockIndex int
    ToolBlocks            map[int]*ToolBlockState

    // Extended:
    ThinkingBlockIndex    *int
    RedactedBlockIndex    *int
    PendingSignature      string  // buffered signature_delta before thinking block
    ActiveToolSlot        *int
}

type ToolBlockState struct {
    ContentIndex int
    ID           string
    Name         string
    Open         bool
}
```

## SSE Parsing (shared)

```go
type ParsedSseEvent struct {
    Event string // event: value (empty for OpenAI)
    Data  string // data: value (joined multi-line)
}

// Buffer parsing: \n\n delimiter (normalized from \r\n)
// Extracts event: and data: lines per block. Skips blocks with zero data: lines.
func pullSseEventsWithDone(buffer string) (events []ParsedSseEvent, rest string)
```

## Provider-Specific SSE Formats and Processing

### OpenAI Chat SSE

**Downstream format**: `data: {json}\n\n`. Done: `data: [DONE]\n\n`.

Stream processing (`openAiChatStream` from `streamBridge.ts`):

1. Parse SSE via `parseSerializedSse(lines)` -- extract `data: ` JSON lines, skip `[DONE]`
2. `normalizeUpstreamStreamEvent(payload, ctx, model)` -> NormalizedStreamEvent (think tag parsing inline)
3. Multi-choice handling: extract per-choice events via `extractChatChoiceEvents`, annotations/citations from primary choice
4. `serializeEvent(event, ctx, downstreamCtx)`:
   - Multi-choice with index != 0 or >1 choices: custom chunk with all choices sorted, citations, usage
   - Otherwise: `serializeNormalizedStreamEvent('openai', event, ctx, claudeCtx)`
5. Inject annotations into `delta.annotations` of first choice, citations at chunk root, usage/usageDetails merged
6. Done: `serializeStreamDone('openai', ...)` -> `["data: [DONE]\n\n"]`

### Anthropic Messages SSE

**Downstream format**: `event: <name>\ndata: {json}\n\n`. Valid events: `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, `message_stop`, `ping`, `error`.

**4 content block kinds** (not just text + tool_use):
1. **text** -- visible text (text_delta)
2. **thinking** -- extended reasoning (thinking_delta + signature_delta sub-events)
3. **tool_use** -- tool calls (input_json_delta)
4. **redacted_thinking** -- safety-filtered reasoning (start with data field)

**2 special passthrough blocks**: `server_tool_use`, `web_search_tool_result` (content_block_start/stop pair).

**Content block state machine**:
- Close ordering when opening new block: redacted -> text -> thinking -> tool_use
- Starting tool_use closes previous tool_use if different slot
- `signature_delta` can arrive before thinking block: buffered in `pendingSignature`, emitted after thinking start. If no thinking block, emitted as standalone (start + signature + stop)

**Message ID construction**: if starts with `msg_` use as-is; otherwise sanitize `[^A-Za-z0-9_-]` -> `_`, prefix `msg_`. Fallback: `msg_<timestamp>`.

**Stop reason mapping**: `stop`->`end_turn`, `length`->`max_tokens`, `tool_calls`->`tool_use`, default->`end_turn`.

**Tool id/name fallbacks**: id -> `toolu_<slot>`, name -> `tool_<slot>`.

**Empty content fallback**: single empty `{type:"text", text:""}` block if all blocks empty.

**Non-streaming -> stream** (`serializeAnthropicFinalAsStream`): generates MULTIPLE chunks, not single.

**Raw SSE passthrough** (`consumeAnthropicSseEvent`): if event name or payload.type is a recognized Anthropic event, pass through via `serializeAnthropicRawSseEvent` (multi-line data preserved as separate `data:` lines). State sync via `syncAnthropicRawStreamStateFromEvent`.

### Gemini GenerateContent SSE

Two stream formats:
1. **SSE** (`text/event-stream`): `data: {json}` with `\r?\n\r?\n` boundary
2. **JSON array**: Raw JSON array payloads

Format detection: Content-Type header first, then JSON.parse attempt.

**Aggregate state machine** (`GeminiGenerateContentAggregateState`):

```go
type GeminiGenerateContentAggregateState struct {
    ResponseID        string
    ModelVersion      string
    FinishReason      string
    Parts             []GeminiPart                // coalesced
    Citations         []map[string]any            // deduplicated by JSON serialization
    GroundingMetadata []map[string]any            // deduplicated
    ThoughtSignatures []string
    Usage             GeminiUsageSummary
    Candidates        []GeminiCandidateAggregate
}
```

Adjacent text parts with same shape (all fields except `text`) are coalesced. Citations/groundingMetadata deduplicated by stable JSON serialize. Usage has 5 counters: promptTokenCount, candidatesTokenCount, totalTokenCount, cachedContentTokenCount, thoughtsTokenCount.

## Anthropic Request Processing (inbound.ts + conversion.ts)

### sanitizeAnthropicMessagesBody (full pipeline)

1. **T+P mutual exclusion**: if both temperature and top_p are finite numbers, delete top_p
2. **Thinking config validation**: `enabled` requires positive `budget_tokens`; `adaptive` with budget promoted to `enabled`
3. **Output config**: `effort` only allowed for `adaptive` thinking; validates against low|medium|high|max
4. **Tool choice sanitization**: string `"required"` -> `{type:"any"}`; `{type:"tool"}` requires `name`; `{type:"function",function:{name}}` -> `{type:"tool",name}`; valid types: auto,none,any,tool
5. **System prompt normalization**: string -> `[{type:"text", text: <string>}]`
6. **Single-string content**: `content:"hello"` -> `content:[{type:"text", text:"hello"}]`
7. **Content block sanitization** (sanitizeAnthropicContentBlock): text/input_text/output_text, image, file, thinking/reasoning (with signature), redacted_thinking (without cache_control), tool_use (requires id+name), tool_result (requires tool_use_id+content), tool_reference
8. **Tool_result content normalization**: string passthrough; array filters text/image/file/tool_reference; single image/file wrapped; JSON.stringify fallback
9. **Message sanitization**: returns null for empty messages; nulls filtered from array
10. **Cache control optimization** (autoOptimizeCacheControls=true):
    - MAX_ANTHROPIC_CACHE_CONTROL_BREAKPOINTS = 4
    - ADAPTIVE_ANTHROPIC_CACHE_CONTROL_BLOCK_WINDOW = 20
    - Structural anchors: last tool + last system prompt
    - Adaptive anchors: last message + one 20 blocks back (if >= 20 blocks)
    - Non-cacheable blocks (thinking, redacted_thinking): cache_control removed

### Anthropic -> OpenAI body conversion (convertClaudeRequestToOpenAiBody)

- System: merged into system message(s)
- Messages: role mapping; tool_use -> assistant.tool_calls; tool_result -> tool messages
- Content blocks: Anthropic image -> OpenAI image_url; Anthropic document -> OpenAI file
- Reasoning blocks -> assistant.reasoning_content
- Tools: web_search normalized to `{type:"web_search", name:"web_search"}`; others to `{type:"function",function:{...}}`
- Tool choice: `any` -> `required`; `{type:"tool",name}` -> `{type:"function",function:{name}}`
- Reasoning: thinking budget -> reasoning_budget; output_config effort -> reasoning_effort
- Metadata: only `user_id` forwarded to Anthropic metadata
- `parallel_tool_calls: false` -> `disable_parallel_tool_use: true` in Anthropic tool_choice

## Gemini Request Processing

### 4xx field filtering
Only 9 keys forwarded: contents, systemInstruction, cachedContent, safetySettings, generationConfig, tools, toolConfig, labels, model.

### Thought signature injection
1. Dummy signature `c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I=` (base64 for "skip_thought_signature_validator") injected on functionCall parts for Gemini models
2. Non-Gemini models with thinking + no real sig: `shouldDisableThinkingConfig = true`
3. Split: functionCall parts with thoughtSignature + text parts -> separate `contents` entries

### Tool format
OpenAI `tools[{type:"function",function:{...}}]` -> Gemini `tools[{functionDeclarations:[{name,description,parametersJsonSchema}]}]`. Tool choice -> `toolConfig.functionCallingConfig` (NONE/AUTO/ANY).

### Media conversion
`image_url` -> `inlineData{mime_type, data}`. File URIs -> `fileData{fileUri, mimeType}`. `input_audio` -> `inlineData{mime_type:"audio/wav", data}`.

### Reasoning -> thinkingConfig (convert.ts)
Gemini 3 models: `thinkingLevel` (none/low/medium/high) with fixed budgets (0/1024/8192/32768). Others: `thinkingBudget` (numeric, max=65536). `includeThoughts: true` synthesized from reasoning params.

### Model name fallback
Tries `body.model` first, falls back to `ctx?.metadata?.requestedModel`.

## Think Tag Parser (shared/thinkTagParser.ts)

Real-time `<think>...</think>` tag detection in streaming text. Separates reasoning from visible content.

```go
type ThinkTagParserState struct {
    Mode    string // "content" | "reasoning"
    Pending string // buffered partial tag match
}

func consumeThinkTaggedText(state *ThinkTagParserState, chunk string) (content string, reasoning string)
func flushThinkTaggedText(state *ThinkTagParserState) (content string, reasoning string)
```

When content echoes reasoning (identical values in delta.content and delta.reasoning_content), suppress content to prevent reasoning leakage.

## Responses Compact (responsesCompact.ts -- request transformation, not response)

| Function | Behavior |
|----------|----------|
| `shouldStripCompactResponsesStore` | True for codex/sub2api platforms |
| `sanitizeCompactResponsesRequestBody` | Removes `stream`, `stream_options`; conditionally removes `store` |
| `ensureCompactResponsesJsonAcceptHeader` | Forces `accept: application/json` for codex/sub2api |
| `shouldForceResponsesUpstreamStream` | Forces stream=true for codex/sub2api upstream (unless compact) |
| `shouldFallbackCompactResponsesToResponses` | HTTP 404/405/501 -> fallback; checks error text for "unknown parameter: 'stream'", "not supported", "unsupported", "invalid url" with compact hints |

## Stop Reason Normalization (shared/chatFormatsCore.ts)

```go
// normalizeStopReason maps raw stop reasons to normalized values:
// "failed"/"error" -> "error"
// "end_turn"/"stop"/"end"/"eos"/"finished"/"completed"/"stop_sequence" -> "stop"
// "incomplete"/"max_tokens"/"length"/"max_output_tokens"/"max_tokens_exceeded" -> "length"
// "tool_use"/"tool_calls" -> "tool_calls"
// All other values -> null (default to "stop" at call site)
```

Empty finish_reason with no tool calls -> "stop". Empty finish_reason with tool calls -> "tool_calls".

## Acceptance Criteria

- [ ] OpenAI Chat <-> Canonical <-> OpenAI Chat: lossless roundtrip
- [ ] Anthropic Messages <-> Canonical <-> Anthropic Messages: lossless roundtrip
- [ ] OpenAI Chat -> Canonical -> Anthropic SSE: correct per-chunk conversion
- [ ] Anthropic SSE -> Canonical -> OpenAI SSE: correct per-chunk conversion
- [ ] Gemini GenerateContent -> Canonical -> OpenAI SSE: correct conversion
- [ ] Codex Responses -> Canonical -> OpenAI Responses: correct conversion
- [ ] OpenAI SSE format: `data: {...}\n\n` ending with `data: [DONE]\n\n`
- [ ] Anthropic SSE format: `event: <name>\ndata: {...}\n\n` with proper content block lifecycle
- [ ] Tool calls preserved cross-protocol (Anthropic tool_use <-> OpenAI function_call, partial JSON deltas)
- [ ] Content blocks with images/file/audio correctly passed through
- [ ] Reasoning/thinking content preserved (Anthropic thinking, Gemini thought, OpenAI reasoning)
- [ ] Token usage mapped to downstream format (11 NormalizedUsage counters)
- [ ] Responses compact works correctly (request sanitization + header forcing + error fallback)
- [ ] Stream mid-stream error -> no corrupted chunks, proper error event
- [ ] Anthropic cache_control optimization preserves 4-breakpoint limit
- [ ] Gemini thought signature injection for tool calling with thinking
- [ ] Think tag parsing for real-time reasoning separation in streaming

## Edge Cases (comprehensive)

- SSE line not starting with `data:` -> skip (event:/comment line)
- Anthropic `content_block_stop` events -> subsumed into content block state machine, not standalone chunks
- Gemini dual format: `\r?\n\r?\n` SSE boundary + JSON array detection
- Tool call arguments: multi-chunk incremental JSON fragments, concatenated in context
- Non-streaming response (stream=false) -> synthetic MULTIPLE chunks (not single):
  - OpenAI: buildSyntheticOpenAiChunks -> 2 chunks (start + end)
  - Anthropic: serializeAnthropicFinalAsStream -> message_start + block_starts + block_deltas + block_stops + message_delta + message_stop
- Empty content block -> insert single empty text block (Anthropic)
- Thinking blocks DO generate SSE events (content_block_start/delta/stop) -- NOT silently skipped
- Redacted_thinking: separate block kind with data field, generated when Claude safety-filters reasoning
- signature_delta: can arrive before thinking block start (buffered); emitted as standalone if orphaned
- Temperature NaN/Infinity -> dropped silently (toFiniteNumber returns null)
- Anthropic empty message after sanitization -> filtered from messages array
- Empty finish_reason + no tool_calls -> "stop"; + tool_calls -> "tool_calls"
- OpenAI multi-choice (choice index != 0 or >1) -> custom serialization path
- Gemini functionCall+text split -> separate contents entries when thoughtSignature present
- OpenAI parallel_tool_calls:false -> Anthropic disable_parallel_tool_use:true
- Content echoing reasoning suppression: when delta.content == delta.reasoning_content, suppress content

## Test Plan

| File | Content |
|------|---------|
| `transform/canonical/types_test.go` | Canonical types serialization/roundtrip |
| `transform/openai/chat/roundtrip_test.go` | OpenAI <-> canonical golden files |
| `transform/openai/responses/roundtrip_test.go` | Responses <-> canonical |
| `transform/anthropic/messages/roundtrip_test.go` | Anthropic <-> canonical golden files |
| `transform/gemini/roundtrip_test.go` | Gemini <-> canonical |
| `transform/anthropic/messages/cache_control_test.go` | Cache control optimization algorithm |
| `transform/anthropic/messages/stream_test.go` | Anthropic content block state machine |
| `transform/openai/chat/stream_test.go` | OpenAI SSE encode/decode |
| `transform/gemini/stream_test.go` | Gemini aggregate state machine |
| `transform/openai/responses/compact_test.go` | Responses compact logic |
| `transform/shared/think_tag_test.go` | Think tag parser |
| `testdata/openai_chat_request.json` | Golden: OpenAI chat request |
| `testdata/anthropic_message_request.json` | Golden: Anthropic message request |
| `testdata/anthropic_message_stream.txt` | Golden: Anthropic SSE stream |
| `testdata/gemini_sse_stream.txt` | Golden: Gemini SSE stream |
| `testdata/gemini_json_stream.json` | Golden: Gemini JSON array stream |
| `testdata/canonical_request.json` | Golden: canonical format |
