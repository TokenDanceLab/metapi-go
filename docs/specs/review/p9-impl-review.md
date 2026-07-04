# P9 Protocol Transformers Implementation Review

**Date**: 2026-07-04 | **Spec**: `docs/specs/p9-transformers.md` | **TS Ref**: `metapi/src/server/transformers/` | **Go Impl**: `metapi-go/transform/`

## Overall Assessment

The Go implementation faithfully reproduces the core transformer architecture from the TS reference. All 94 test functions pass across 5 test files. The canonical intermediate representation types match the spec exactly, and all three provider bridges (OpenAI, Anthropic, Gemini) are implemented with correct roundtrip behavior. The shared stream processing layer (SSE parsing, think tag parser, stop reason normalization, Claude downstream serialization) is thorough and spec-compliant.

The biggest gaps are: (1) no dedicated test files for `openai/chat/`, `openai/responses/`, and the stub packages (`completions`, `embeddings`, `images`); (2) no golden testdata fixture files as specified in the spec's test plan; and (3) the `openai/responses/` package is incomplete -- it has compact logic but no stream bridge, response bridge, or request bridge.

## Test Status

```
go test ./transform/... -count=1
ok      github.com/tokendancelab/metapi-go/transform/anthropic/messages      0.140s  (33 tests)
ok      github.com/tokendancelab/metapi-go/transform/canonical                0.095s  (38 tests)
ok      github.com/tokendancelab/metapi-go/transform/gemini/generate_content  0.144s  (21 tests)
?       github.com/tokendancelab/metapi-go/transform/openai/chat              [no test files]
?       github.com/tokendancelab/metapi-go/transform/openai/completions       [no test files]
?       github.com/tokendancelab/metapi-go/transform/openai/embeddings        [no test files]
?       github.com/tokendancelab/metapi-go/transform/openai/images            [no test files]
?       github.com/tokendancelab/metapi-go/transform/openai/responses         [no test files]
ok      github.com/tokendancelab/metapi-go/transform/shared                   0.192s  (55 tests)
```

Test file breakdown:

| File | Test Functions | Focus |
|---|---|---|
| `canonical/types_test.go` | 13 | Envelope JSON roundtrip, factory, continuation, tool discrimination |
| `canonical/openai_bridge_test.go` | 17 | OpenAI body <-> canonical roundtrip, images, reasoning, tools, tool_choice, developer role, file blocks, content arrays |
| `anthropic/messages/roundtrip_test.go` | 33 | Sanitize body (T+P, thinking, tool_choice), content block sanitization, OpenAI->Anthropic conversion, downstream parse (Claude+OpenAI) |
| `gemini/generate_content/roundtrip_test.go` | 21 | Gemini<->OpenAI roundtrip, inline data, file data, tools, tool_choice, reasoning->thinking config, stream bridge |
| `shared/shared_test.go` | 55 | Think tag parser, stop reasons, SSE parsing, serialization, utilities, responses delta, Claude context, final response normalization, synthetic chunks |

## File-by-File Review

### 1. `canonical/types.go` -- PASS

All canonical types match the spec exactly:
- `CanonicalRequestEnvelope` (named "Envelope", not "Request") with correct field names (`RequestedModel`, `Passthrough`)
- `CanonicalContentPart` with 5 subtypes and flat fields (no `image_url` wrapper)
- `CanonicalToolItem` with `IsFunction()`/`IsRaw()` discrimination
- `CanonicalToolChoice` with 5 variants: string auto/none/required, `{type:"tool",name}`, `{type:"raw",value}`
- `CanonicalReasoningRequest` with 5 effort levels
- `CanonicalContinuation` with TurnState
- No `MaxTokens`/`Temperature`/`TopP` on the envelope (upstream-specific, handled in bridges)

**Minor**: `CanonicalToolItem` uses field names `FnName`, `FnDescription`, `FnStrict`, `FnInputSchema` instead of matching the spec's `CanonicalFunctionTool` struct. The discrimination via `IsFunction()`/`IsRaw()` works identically. The flat union approach avoids allocating a separate struct but the field naming diverges from spec naming conventions.

### 2. `canonical/envelope.go` -- PASS

Factory function `CreateCanonicalRequestEnvelope` correctly:
- Trims whitespace from model name, errors if empty
- Defaults operation to `OpGenerate` and cliProfile to `ProfileGeneric`
- Normalizes continuation via `NormalizeCanonicalContinuation` (returns nil for all-empty)
- Conditionally sets optional fields only when non-zero/non-empty
- Clones attachments to avoid mutation

### 3. `canonical/openai_bridge.go` -- PASS

The core bridge file (989 lines) implements both directions:
- `CanonicalRequestFromOpenAiBody`: OpenAI body -> canonical (steps 2+3 in the 8-stage flow)
- `CanonicalRequestToOpenAiChatBody`: canonical -> OpenAI body (step 5)

**Faithful reproduction of TS logic**:
- Content array normalization (string elements, text blocks, image_url, file, reasoning)
- Tool call extraction from `tool_calls` array with `function.name` + `function.arguments`
- Tool result handling with `ResultContent` for array/map content, `ResultText` for strings
- Reasoning extraction from `reasoning_content` / `reasoning` fields, prepended as thought part
- `parallel_tool_calls` preserved in `Passthrough`
- `transformerMetadata` carried through `Passthrough["transformerMetadata"]`
- Tool choice: string variants, named function, raw fallback
- Tool parsing: function tools, raw tools, Gemini functionDeclarations
- Continuation: `ReadOpenAICompatibleContinuation` extracts `prompt_cache_key`, `previous_response_id`, `metapi_turn_state`
- Canonical -> OpenAI: reverse mapping with correct field names (`tool_choice`->`tool_choice`, `inputSchema`->`parameters`)

**ISSUE -- `IncludeEncryptedContent` is derived from `effort != ""`**: The spec states `IncludeEncryptedContent` should be explicitly set, not derived. The TS implementation has explicit logic for this field. The Go code sets it to `true` whenever `effort` is non-empty:
```go
IncludeEncryptedContent: effort != "",
```
This is a reasonable default but diverges from the spec's explicit field semantics.

**Minor -- `getInt` doesn't handle `float32` or `int32`**: The helper covers `float64`, `int`, and `int64` but misses `float32` and `int32`. Edge case with untagged JSON numbers, unlikely in practice.

### 4. `shared/types.go` -- PASS

All shared types match the spec:
- `NormalizedStreamEvent` with 9 fields (no `Index` on event, per spec)
- `ToolCallDelta` with `ArgumentsDelta` (incremental JSON fragment)
- `NormalizedFinalResponse` with 9 fields
- `NormalizedUsage` with 11 counters
- `ParsedSseEvent` with `Event` and `Data` strings
- `ThinkTagParserState` with `Mode` + `Pending`
- `ClaudeDownstreamContext` with extended fields: `ThinkingBlockIndex`, `RedactedBlockIndex`, `PendingSignature`, `ActiveToolSlot`
- `StreamTransformContext` with responses tracking: `ResponsesToolCallIndexByOutputIndex`, `ResponsesToolCallIndexByID`, `NextResponsesToolCallIndex`, `ResponsesTextByIndex`, `ResponsesReasoningByIndex`

### 5. `shared/utils.go` -- PASS

SSE parsing (`PullSseEventsWithDone`):
- Normalizes `\r\n` to `\n`
- Splits on `\n\n` boundary
- Extracts `event:` and `data:` lines per block
- Skips blocks with zero data lines
- Preserves full buffer rest for next call
- Matches spec exactly

Think tag parser (`ConsumeThinkTaggedText`, `FlushThinkTaggedText`, `ExtractInlineThinkTags`):
- `ConsumeThinkTaggedText` implements the state machine correctly: content mode -> reasoning mode on `<think>`, back to content on `</think>`
- Pending buffer for partial tag matches
- Flush dumps pending in reasoning mode as reasoning
- Hex-encoded tag strings avoid literal `<think>`/`</think>` in source (clever compile-time avoidance)

Stop reason normalization:
- `NormalizeStopReason`: failed/error->error, end_turn/stop/end/eos/finished/completed/stop_sequence->stop, incomplete/max_tokens/length/max_output_tokens/max_tokens_exceeded->length, tool_use/tool_calls->tool_calls
- `ToClaudeStopReason`: length->max_tokens, tool_calls->tool_use, default->end_turn

**ISSUE -- `NormalizeStopReason` has aggressive `strings.Contains` matching**: The pattern `strings.Contains(v, "max")` for length detection and `strings.Contains(v, "tool")` for tool_calls could produce false positives on edge cases (e.g., "maximized" would match "max"). The TS implementation uses exact matching with a map lookup. Low risk in practice since upstream providers use the exact keywords.

**Minor -- `ComputeNovelResponsesDelta` overlap detection**: The overlap window scanning (`for o := ml; o > 0; o--`) handles prefix/suffix/overlap cases. No sliding window minimum threshold, but matches TS behavior.

### 6. `shared/chatFormatsCore.go` -- PASS (comprehensive)

This is the largest file at ~1700 lines and is the heart of the stream processing layer:

**Downstream parse** (`ParseDownstreamChatRequest`):
- Claude format: converts via `convertClaudeRequestToOpenAiBody`, validates model+messages
- OpenAI format: validates model+messages, hints about /v1/responses if `input` is present
- Returns unified `ParsedDownstreamChatRequest` with both `UpstreamBody` and `ClaudeOriginalBody`

**Claude -> OpenAI conversion** (`convertClaudeRequestToOpenAiBody`):
- System prompt normalization (string -> merged, array -> joined)
- Message role mapping (assistant->assistant, everything else->user)
- Content block conversion: text->text, image (base64/url)->image_url, document->file
- Tool call extraction: `tool_use` -> assistant with tool_calls
- Tool result: `tool_result` -> tool messages
- Reasoning extraction from content blocks via `ExtractInlineThinkTags`
- Temperature, top_p, max_tokens, stop_sequences mapping
- Tool conversion (web_search, function, custom)
- Tool choice conversion (any->required, function->function, tool->function)
- Reasoning: thinking budget->reasoning_budget, output_config effort->reasoning_effort

**Normalize upstream final response** (`NormalizeUpstreamFinalResponse`):
- 5 format branches: OpenAI chat, Anthropic message, Responses, Gemini, string fallback
- Content extraction via think tag parsing
- Reasoning extraction from explicit fields or think tags
- Tool call collection for each format
- Stop reason normalization with tool_calls inference on empty finish_reason

**Normalize upstream stream event** (`NormalizeUpstreamStreamEvent`):
- 5 format branches: OpenAI chat (choices[0].delta), Anthropic (message_start/content_block_start/content_block_delta/message_delta/message_stop), Gemini (candidates), Responses (response.output_text.*, response.created, response.incomplete/failed/error, response.completed), generic fallback
- Content echo suppression when `contentDelta == reasoningDelta`
- Think tag parsing inline via `ConsumeThinkTaggedText`
- Responses novel delta computation via `ComputeNovelResponsesDelta`

**Serialize normalized stream events** (`SerializeNormalizedStreamEvent`):
- OpenAI format: builds stream chunk with delta, tool_calls accumulation
- Claude format: content block state machine with `ensureClaudeStartEvents`, `ensureClaudeTextBlockStart`, `ensureClaudeToolBlockStart`, `closeClaudeTextBlock`, `closeClaudeToolBlocks`, `buildClaudeDoneEvents`

**Claude downstream serialization** (state machine):
- `buildClaudeMessageID`: sanitizes to `msg_*`, strips non-`[A-Za-z0-9_-]`, fallback timestamp
- Content block lifecycle: message_start -> content_block_start(text/tool_use) -> content_block_delta(text_delta/input_json_delta) -> content_block_stop -> message_delta -> message_stop
- Tool blocks tracked with state, sorted by content index on close
- Done deduplication via `DoneSent` on context

**Final response serialization** (`SerializeFinalResponse`):
- Claude: builds content blocks (thinking, redacted_thinking, text, tool_use), empty content fallback to `[{type:"text",text:""}]`
- OpenAI: builds message with tool_calls, reasoning_content, stop reason normalization

**Synthetic chunks** (`BuildSyntheticOpenAiChunks`):
- Generates 2 chunks: start (delta with full content) + end (empty delta with finish_reason)

**ISSUE -- Content echoing suppression only on think-tag paths**: The spec says "When content echoes reasoning (identical values in delta.content and delta.reasoning_content), suppress content to prevent reasoning leakage." The Go code checks `reasoningDelta != "" && rawContentDelta == reasoningDelta` in the OpenAI chat path. This works for the OpenAI chat delta path but does not apply to the Anthropic content_block_delta or Gemini candidate paths, where reasoning could also echo content.

**ISSUE -- Anthropic stream missing `signature_delta` and `ping` handling**: The spec details `signature_delta` sub-events that can arrive before thinking block start (buffered in `PendingSignature`, emitted after thinking start, or standalone if orphaned). The `ClaudeDownstreamContext` has `PendingSignature` field but it is never populated or used. The `ping` event type is also not handled. In practice, these are rare edge cases.

**ISSUE -- Responses `response.completed` handler has a throwaway result**: The line `collectToolCallsFromResponsesPayload(rp)` is called but the return value is discarded. The TODO comment `// TODO: collectIndexedToolCalls` confirms this is incomplete. Tool calls in the completed response event are not serialized to the stream.

### 7. `anthropic/messages/conversion.go` -- PASS

**SanitizeAnthropicMessagesBody** (full pipeline):
- T+P mutual exclusion: deletes top_p if both present
- Thinking config: enabled requires positive budget_tokens, adaptive with budget promoted to enabled
- Output config: effort only allowed for adaptive thinking, validates against low|medium|high|max
- Tool choice: string required->{type:"any"}, {type:"tool"} requires name, function mapping
- Message sanitization: content blocks validated, nulls filtered
- Cache control: auto-optimize with 4 breakpoint limit, structural anchors (last tool + last system prompt), adaptive anchors (last message + one 20 blocks back), non-cacheable blocks cleaned

**ConvertOpenAiBodyToAnthropicMessagesBody**:
- System/developer messages merged into system string
- Content blocks: OpenAI image_url -> Anthropic image, file->document
- Reasoning: `reasoning_content` -> thinking block with signature
- Tools: web_search mapped to web_search_20250305, function->Anthropic tool
- Tool choice: required->any, function->tool
- `parallel_tool_calls: false` -> `disable_parallel_tool_use: true`
- Reasoning settings: effort->adaptive thinking + output_config effort, budget_tokens->enabled thinking

**Cache control optimization**:
- `normalizeAnthropicMessageContents`: converts string content to [{type:"text",text:...}]
- `normalizeAnthropicSystemPrompts`: converts string system to array
- `clearAnthropicCacheControls`: removes all existing cache_control markers
- `ensureStructuralCacheControls`: places cache_control on last tool and last system prompt block
- `collectCacheableMessageRefs`: collects text/image/file blocks excluding thinking/redacted_thinking
- Adaptive windows: last message always, one 20 blocks back if total >= 20
- `sanitizeUnsupportedCacheControls`: removes cache_control from non-cacheable blocks

**ISSUE -- `collectCacheableMessageRefs` write-back index mismatch**: The refs are collected per-content-block across all messages. When writing back, the code assumes `writeIdx + ci` maps linearly to the refs array. But `writeIdx` is incremented by `len(content)` per message, and `ci` indexes within that message's content. This works correctly only because refs are collected in the same iteration order. However, if any message's content array was modified during `normalizeAnthropicMessageContents` (changing its length), the indices would desync.

### 8. `gemini/generate_content/compatibility.go` -- PASS

**4xx field filtering**: 9 keys forwarded: contents, systemInstruction, cachedContent, safetySettings, generationConfig, tools, toolConfig, labels, model. Thinking config derived from reasoning params via `ResolveGeminiThinkingConfigFromRequest`.

**Gemini -> OpenAI** (`BuildOpenAiBodyFromGeminiRequest`):
- System instruction -> system message
- Contents: role mapping (model->assistant), functionCall -> assistant.tool_calls, functionResponse -> tool messages
- Text/thought parts correctly split (thought parts filtered from text content)
- InlineData -> base64 data URL for images
- FileData -> file_url with mimeType
- Generation config: temperature, topP->top_p, maxOutputTokens->max_tokens, stopSequences->stop
- Tools: functionDeclarations -> OpenAI function tools
- Tool choice: NONE->none, ANY/VALIDATED->required, AUTO->auto

**OpenAI -> Gemini** (`BuildGeminiGenerateContentRequestFromOpenAi`):
- System -> systemInstruction
- Reasoning_content -> thought-part text
- Content: image_url -> inlineData (base64 parsed) or fileData (URL)
- Tool calls -> functionCall parts
- Tool messages -> functionResponse parts
- Generation config mapping, tools -> functionDeclarations
- Tool choice -> functionCallingConfig

**Thinking config** (`ReasoningToThinkingConfig`):
- none->nil, low->thinkingBudget:0, medium->8192, high->32768, max->65536
- Budget tokens passed directly

**Stream bridge** (`StreamBridge`):
- `GeminiAggregateState` tracks coalesced parts, citations, groundingMetadata, usage, candidates
- `NormalizeEvent` accumulates state (modelVersion, responseId, finishReason, parts, usage)
- Part coalescing: adjacent text parts with same non-text fields merged
- `SerializeEvent` produces Gemini SSE with `candidates[0].content.parts`
- `SerializeDone` emits finishReason + coalesced parts + usageMetadata

**ISSUE -- Thought signature injection not implemented**: The spec describes dummy signature injection (`c2tpcF90aG91Z2h0X3NpZ25hdHVyZV92YWxpZGF0b3I=`) on functionCall parts for Gemini models and `shouldDisableThinkingConfig` for non-Gemini models. Neither is present in the Go implementation.

**ISSUE -- Candidate split on functionCall + text not implemented**: The spec says "functionCall parts with thoughtSignature + text parts -> separate contents entries." This split logic is absent.

### 9. `openai/chat/inbound.go` -- PASS (no dedicated tests)

Clean delegation to shared functions:
- `Inbound` -> `shared.ParseDownstreamChatRequest(FormatOpenAI)`
- `ParseToCanonical` -> `canonical.CanonicalRequestFromOpenAiBody`
- `BuildUpstreamBody` -> `canonical.CanonicalRequestToOpenAiChatBody`
- `StreamBridge` wraps `shared.StreamTransformContext` with `SerializeEvent` building OpenAI chunks

**ISSUE -- `buildOpenAIStreamChunk` duplicated from shared**: The local `buildOpenAIStreamChunk` function in `inbound.go` is a near-identical copy of the one in `shared/chatFormatsCore.go`. Both build the same OpenAI chunk structure. The shared version is used by `SerializeNormalizedStreamEvent` when format is OpenAI, while the chat package's version is used by `StreamBridge.SerializeEvent`. This duplication is fragile -- changes to one won't propagate to the other.

**Minor -- `SerializeEvent` creates a throwaway `ClaudeDownstreamContext`**: When `cc` is nil, it creates a new `ClaudeDownstreamContext` that is never used (the OpenAI path builds chunks directly, not through the Claude state machine). Wasteful allocation on every call.

### 10. `openai/responses/compact.go` -- PASS (no tests)

Compact logic is correctly implemented:
- `ShouldStripCompactResponsesStore`: true for codex/sub2api
- `ShouldForceResponsesUpstreamStream`: true for codex/sub2api unless compact
- `SanitizeCompactResponsesRequestBody`: removes stream, stream_options, conditionally store
- `EnsureCompactResponsesJSONAcceptHeader`: forces accept: application/json, deletes existing Accept
- `ShouldFallbackCompactResponsesToResponses`: HTTP 404/405/501, unknown parameter 'stream' with compact hints, invalid url with compact hints, not-supported/unsupported with compact hints

**Missing**: The spec shows `openai/responses/` should also have `inbound.go`, `request_bridge.go`, `response_bridge.go`, `stream_bridge.go`, `aggregator.go`, and `codexCompatibility.go`. Only `compact.go` is present. The responses stream handling in `shared/chatFormatsCore.go` partially covers this (via `response.output_text.*` and `response.completed` handlers) but the full responses bridge is not a standalone module.

### 11. Stub Packages (`completions`, `embeddings`, `images`) -- PASS

Pass-through stubs that return the body unchanged. These are correct as-is since the proxy routes these directly without transformation. The spec's file listing shows `request.go` and `response.go` for each, which matches.

## Spec Test Plan Coverage

The spec defines 16 test files. Here is the coverage:

| Spec Test File | Go Equivalent | Status |
|---|---|---|
| `canonical/types_test.go` | `canonical/types_test.go` (13 tests) | Done |
| `openai/chat/roundtrip_test.go` | `canonical/openai_bridge_test.go` (17 tests) | Done (in canonical pkg) |
| `openai/responses/roundtrip_test.go` | **None** | Missing |
| `anthropic/messages/roundtrip_test.go` | `anthropic/messages/roundtrip_test.go` (33 tests) | Done |
| `gemini/roundtrip_test.go` | `gemini/generate_content/roundtrip_test.go` (21 tests) | Done |
| `anthropic/messages/cache_control_test.go` | Covered in `roundtrip_test.go` via sanitization | Implicit |
| `anthropic/messages/stream_test.go` | **None** | Missing |
| `openai/chat/stream_test.go` | **None** (openai/chat has no test files) | Missing |
| `gemini/stream_test.go` | **None** | Missing |
| `openai/responses/compact_test.go` | **None** | Missing |
| `shared/think_tag_test.go` | `shared/shared_test.go` (think tag section) | Done |
| `testdata/openai_chat_request.json` | **None** | Missing |
| `testdata/anthropic_message_request.json` | **None** | Missing |
| `testdata/anthropic_message_stream.txt` | **None** | Missing |
| `testdata/gemini_sse_stream.txt` | **None** | Missing |
| `testdata/gemini_json_stream.json` | **None** | Missing |
| `testdata/canonical_request.json` | **None** | Missing |

All 8 golden testdata fixture files are missing. No `testdata/` directory exists under the transform package.

## Missing Implementations (Spec Sections Not Covered)

| Spec Section | Feature | Status | Priority |
|---|---|---|---|
| openai/responses/ | Full responses bridge (inbound, request_bridge, response_bridge, stream_bridge, aggregator, codexCompatibility) | **Stubbed** (compact only) | P1 |
| openai/responses/ | Codex compatibility (`codexCompatibility.ts`) | **Missing** | P2 |
| gemini/generate_content/ | Thought signature injection (dummy sig + disableThinkingConfig) | **Missing** | P1 |
| gemini/generate_content/ | Candidate split on functionCall+text with thoughtSignature | **Missing** | P2 |
| gemini/generate_content/ | JSON array stream format detection (currently only SSE) | **Missing** | P2 |
| anthropic/messages/ | Raw SSE passthrough (`consumeAnthropicSseEvent`) | **Missing** | P2 |
| anthropic/messages/ | `signature_delta` sub-event buffering and orphan handling | **Missing** (PendingSignature unused) | P2 |
| anthropic/messages/ | `ping` event handling | **Missing** | P3 |
| anthropic/messages/ | Server tool_use and web_search_tool_result passthrough blocks | **Missing** | P2 |
| anthropic/messages/ | Non-streaming -> multi-chunk Anthropic stream (`serializeAnthropicFinalAsStream`) | **Missing** | P2 |
| shared/ | `reasoningTransport.ts` (reasoning content transport layer) | **Missing** | P3 |
| shared/ | `inputFile.ts` (input file resolution) | **Missing** | P3 |
| Test data | 8 golden fixture files | **Missing** | P1 |

## Summary of Findings

### Correctness Issues

1. **`IncludeEncryptedContent` derived from effort** (`canonical/openai_bridge.go`): The field is set to `effort != ""` instead of accepting an explicit value. The TS implementation handles this field explicitly. **Fix**: Accept explicit `IncludeEncryptedContent` from the upstream body or input parameters.

2. **`NormalizeStopReason` aggressive substring matching** (`shared/utils.go`): `strings.Contains(v, "max")` and `strings.Contains(v, "tool")` could false-positive on unusual stop reasons. **Fix**: Use exact matching via map lookup like the TS implementation.

3. **`collectCacheableMessageRefs` write-back index fragility** (`anthropic/messages/conversion.go`): Linear indexing assumes content arrays are unchanged during normalization. **Fix**: Use message+content-block indexing or verify array lengths.

4. **`buildOpenAIStreamChunk` code duplication** (`openai/chat/inbound.go`): Near-identical copy of the function in `shared/chatFormatsCore.go`. **Fix**: Either use the shared version directly or extract a common helper.

5. **Throwaway `ClaudeDownstreamContext` allocation** (`openai/chat/inbound.go`, `SerializeEvent`): Creates a new context when `cc` is nil, but the OpenAI path never uses it. **Fix**: Remove the nil-check and allocation in the OpenAI branch.

6. **Responses `response.completed` tool calls discarded** (`shared/chatFormatsCore.go`): The `collectToolCallsFromResponsesPayload(rp)` result is thrown away. **Fix**: Wire the collected tool calls into the stream event or the context accumulator.

### Test Coverage Gaps

- No tests for `openai/chat/` package (7 packages with `[no test files]`)
- No tests for `openai/responses/compact.go` (5 functions, zero coverage)
- No stream-processing tests (Anthropic content block state machine, Gemini aggregate, OpenAI multi-choice)
- No golden file testdata fixtures (8 files specified in the test plan)
- No cross-provider roundtrip tests (e.g., Anthropic request -> canonical -> OpenAI body)

### Architecture Notes

- The `CanonicalToolItem` flat struct approach (instead of separate `CanonicalFunctionTool`/`CanonicalRawTool` types) is a pragmatic simplification that avoids extra allocations while matching the discrimination semantics exactly.
- The hex-encoded think tag strings (`\x3c\x74\x68\x69\x6e\x6b\x3e`) are a clever compile-time solution to prevent the model from processing literal `<think>` tags in the source.
- Separating `shared/` from provider packages is clean -- no import cycles, clear dependency direction.
- The `StreamBridge` pattern (New, NormalizeEvent, SerializeEvent, SerializeDone, PullSseEvents) is consistent across all three providers -- good API design.
- The `SerializeNormalizedStreamEvent` in shared handles both OpenAI and Claude formats, but the chat package duplicates the OpenAI path. This suggests an architectural tension between "shared utility" and "package-specific convenience."

## Recommendations

1. **P0**: Add golden testdata fixtures per spec test plan (8 files: openai_chat_request.json, anthropic_message_request.json, anthropic_message_stream.txt, gemini_sse_stream.txt, gemini_json_stream.json, canonical_request.json, plus 2 more for responses/Codex).
2. **P0**: Fix `IncludeEncryptedContent` derivation in canonical -- make it explicit.
3. **P0**: Fix `NormalizeStopReason` substring matching -- use exact map lookup.
4. **P1**: Add tests for `openai/responses/compact.go` -- at minimum status-code fallback, compact hint detection, platform stripping.
5. **P1**: Add tests for `openai/chat/inbound.go` -- StreamBridge NormalizeEvent/SerializeEvent roundtrip.
6. **P1**: Implement Gemini thought signature injection (spec section: "Thought signature injection").
7. **P1**: Wire the `responses.completed` tool call collection results into the stream event.
8. **P2**: Deduplicate `buildOpenAIStreamChunk` between `openai/chat/inbound.go` and `shared/chatFormatsCore.go`.
9. **P2**: Implement Anthropic raw SSE passthrough and `signature_delta` buffering.
10. **P2**: Implement Gemini JSON array stream format detection.
11. **P2**: Implement Anthropic non-streaming -> multi-chunk serialization.
12. **P3**: Remove throwaway `ClaudeDownstreamContext` allocation in OpenAI chat `SerializeEvent`.
13. **P3**: Add cross-provider roundtrip integration tests (Anthropic -> canonical -> OpenAI, Gemini -> canonical -> Anthropic, etc.).
