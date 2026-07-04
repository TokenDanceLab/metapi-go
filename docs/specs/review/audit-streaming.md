# SSE Streaming Correctness Audit: metapi-go vs metapi (TS reference)

**Date**: 2026-07-04
**Scope**: `handler/proxy/chat.go`, `handler/proxy/responses.go`, `handler/proxy/upstream.go`, `handler/proxy/surface.go`
**Reference**: `metapi/src/server/proxy-core/surfaces/chatSurface.ts`, `metapi/src/server/transformers/openai/chat/proxyStream.ts`, `metapi/src/server/transformers/shared/protocolLifecycle.ts`, `metapi/src/server/transformers/shared/chatFormatsCore.ts`
**Reviewers**: Claude Code automated audit

---

## Executive Summary

The Go implementation (`handleStreamUpstream`) performs a **raw byte-level TCP passthrough** of upstream SSE streams. It does zero SSE parsing, zero error injection, zero format transformation, and has no event-level awareness. The TypeScript reference implementation has a **full SSE parser** with event extraction, format-aware transformation, error detection, aggregate state tracking, deferred emission, and empty-content failure detection.

**Severity summary**:

| Issue | Severity | Status |
|---|---|---|
| No SSE error event on mid-stream failure | **HIGH** | Gap |
| No SSE-aware parsing (byte passthrough only) | **HIGH** | Gap |
| `latencyMs` discarded in both stream handlers | **MEDIUM** | Bug |
| Missing SSE headers (`charset=utf-8`, `X-Accel-Buffering`, `no-transform`) | **MEDIUM** | Gap |
| No `[DONE]` or terminal-chunk synthesis on abrupt upstream close | **MEDIUM** | Gap |
| No empty-content failure detection | **LOW** | Gap |
| Multi-byte UTF-8 splitting at buffer boundaries | **LOW** | Safe (passthrough) |
| Flush timing (per-Read, not per-event) | **INFO** | Acceptable |
| Chunk ordering preservation | **OK** | Correct |
| Concurrent stream safety | **OK** | Correct |
| SSE comment line handling | **OK** | Passthrough |

---

## 1. Chunk Ordering Preservation

### Go
`handleStreamUpstream` uses a strictly sequential loop:
```go
buf := make([]byte, 4096)
for {
    n, err := resp.Body.Read(buf)
    if n > 0 {
        w.Write(buf[:n])
        if flusher != nil { flusher.Flush() }
    }
    if err != nil { break }
}
```
Each `Read` returns bytes in TCP arrival order. Each `Write` happens in the same loop iteration before the next `Read`. No goroutines, channels, or reordering buffers exist between `Read` and `Write`. Chunk ordering is inherently preserved.

### TS Reference
`protocolLifecycle.run()` reads chunks sequentially via `reader.read()`, accumulates into an SSE buffer, extracts complete events via `pullSseEventsWithDone()`, and processes them in order via `handleEvent()`. Ordering is preserved.

### Verdict
**OK** -- both implementations preserve chunk ordering. No gap.

---

## 2. Flush Timing

### Go
Flush is called once per `Read` that returns data:
```go
if n > 0 {
    w.Write(buf[:n])
    if flusher != nil {
        flusher.Flush()
    }
}
```
Each successful `Read` triggers exactly one `Write` + `Flush`. Multiple SSE events that arrive in the same TCP segment are flushed together -- flush is not per-event. This is acceptable for SSE and matches typical reverse-proxy behavior.

### TS Reference
TS uses `reply.raw.write(line)` (Fastify's raw socket write) within `writeLines()`. Fastify internally manages low-level socket flushing. There is no explicit per-event `Flush()` call visible in the application code. The TS code also batches multiple output lines into a single `writeLines()` call (e.g., `input.writeLines(lines)`).

### Verdict
**OK** -- Go's flush per `Read` is equivalent to or better than TS's implicit flush. The timing is prompt enough for SSE semantics.

---

## 3. Error Event Format During Mid-Stream Failures

### Go -- CRITICAL GAP

When the upstream connection fails mid-stream (`resp.Body.Read()` returns `io.EOF` or a network error), `handleStreamUpstream` simply breaks out of the loop:

```go
if err != nil {
    break  // silent exit
}
```

No SSE error event is injected. The downstream client experiences a **silent TCP connection close** with no structured error. The client must detect this as a connection drop (I/O error or zero-length read) and retry without any information about what went wrong.

When upstream returns a non-200 status with a JSON error body, the non-stream path handles it via `handleNonStreamUpstream` -> `proxy.DetectProxyFailure()`. But when streaming is active, the status code is already `200` and the body is an ongoing SSE stream -- any mid-stream failure is invisible to the error detection logic.

### TS Reference

The TS code handles mid-stream failures through several mechanisms:

1. **Structured upstream error events**: `handleEventBlock()` in `proxyStream.ts` detects `type: "response.failed"` or `type: "error"` payloads and calls `markFailed()`, then emits the error event to the client with `force: true`.

2. **Post-hijack stream failures**: In `chatSurface.ts` (line ~854), when a stream failure occurs after `streamStarted` is true (i.e., after `reply.hijack()`), the TS code does NOT attempt to send a JSON error response (which would be impossible after hijack). Instead, it relies on:
   - The proxy stream session having already emitted error lines
   - Or the TCP connection being closed, signaling failure to the client

3. **Abrupt EOF handling**: `protocolLifecycle.run()` calls `onEof` (which is `finalize()` or `closeOut()`) when the reader completes. For OpenAI format, `finalize()` sends `[DONE]` if conditions are met. For `responses` format, `closeOut()` checks if a terminal event was seen and fails if not (`stream closed before response.completed`).

### Verdict
**HIGH severity gap.** Go has zero error injection into SSE streams. The TS reference has structured error detection, error event forwarding, and graceful EOF handling. Go clients will see raw TCP disconnects; TS clients see either structured error events or `[DONE]` markers.

**Recommendation**: At minimum, add an SSE error event injection before closing the connection when `resp.Body.Read()` returns an error mid-stream. Format should match the request surface (`event: error\ndata: {"error": {"message": "...", "type": "upstream_error"}}\n\n` or equivalent).

---

## 4. Connection Close on Upstream Disconnect

### Go
When `resp.Body.Read()` returns an error, the loop breaks. `dispatchUpstream` then calls `resp.Body.Close()` and returns. The HTTP handler returns, and Go's `net/http` closes the downstream TCP connection.

This is correct but abrupt -- no `[DONE]` marker or terminal SSE event is sent.

### TS Reference
`protocolLifecycle.run()`: when `reader.read()` returns `done: true`, the loop breaks. Then:
```typescript
sseBuffer += decoder.decode();  // flush decoder
if (sseBuffer.trim().length > 0) {
    const flushed = await flushBuffer(`${sseBuffer}\n\n`);
}
if (!shouldStop) {
    await input.onEof?.();  // finalize() or closeOut()
}
reader.releaseLock();
input.response.end();
```
`finalize()` for OpenAI sends `[DONE]`. `closeOut()` for responses checks terminal-event presence and fails if missing.

### Verdict
**MEDIUM severity gap.** Go does a raw connection close. TS sends structured termination (either `[DONE]` or an error event). This matters for client-side stream integrity verification.

---

## 5. Buffering Behavior (bufio.Scanner vs raw Read)

### Go -- SIGNIFICANT DESIGN CHOICE

Go uses a fixed 4096-byte buffer with `resp.Body.Read()`:

```go
buf := make([]byte, 4096)
for {
    n, err := resp.Body.Read(buf)
    if n > 0 {
        w.Write(buf[:n])
        // ...
    }
}
```

This is a **byte-stream passthrough**, not an SSE-aware relay. It does not:
- Parse SSE line boundaries (`\n\n` event delimiters)
- Recognize `event:` and `data:` fields
- Join multi-line `data:` values
- Detect `[DONE]` markers
- Handle partial events (events split across `Read` boundaries)

**Consequence**: Go cannot transform SSE events between formats (e.g., Claude SSE to OpenAI SSE chunks, or Gemini CLI output to standard SSE). All streaming endpoints that go through `handleStreamUpstream` receive raw upstream bytes, regardless of the downstream surface format.

### TS Reference

The TS code implements a full SSE parser via `pullSseEventsWithDone()`:

```typescript
function pullSseEventsWithDone(buffer: string) {
    const normalized = buffer.replace(/\r\n/g, '\n');
    while (true) {
        const boundary = rest.indexOf('\n\n');
        if (boundary < 0) break;  // incomplete event, wait for more data
        const block = rest.slice(0, boundary);
        rest = rest.slice(boundary + 2);
        // Parse event: and data: lines
        // ...
    }
    return { events, rest };
}
```

This:
- Splits on `\n\n` to extract complete event blocks
- Leaves incomplete blocks in `rest` for next iteration
- Parses `event:` and `data:` prefixes
- Joins multi-line `data:` values
- Returns structured `ParsedSseEvent[]`

### Verdict
**HIGH severity gap.** This is by far the most significant architectural difference. Go's byte-passthrough means:
1. No format transformation is possible for any streaming endpoint
2. All streaming endpoints that rely on `dispatchUpstream` will behave as transparent TCP proxies
3. The Go codebase cannot implement per-event usage tracking, streaming failure detection, or format conversion without a full SSE parser

The TS code uses its SSE parser for every streaming endpoint (`chatSurface.ts`, `openAiResponsesSurface.ts`, `geminiSurface.ts`).

**Recommendation**: Implement an SSE parser in Go (similar to `pullSseEventsWithDone`). This is a prerequisite for format-aware streaming, error injection, and usage tracking.

---

## 6. Concurrent Stream Safety

### Go
Each HTTP request is handled in its own goroutine. `handleStreamUpstream` uses:
- A local `buf` slice (stack-allocated, not shared)
- The `w http.ResponseWriter` and `resp *http.Response` (both request-scoped)
- No package-level mutable state

No mutexes, shared buffers, or cross-goroutine communication exists. This is safe.

### TS Reference
TS is single-threaded (Node.js event loop). Each request is handled asynchronously within the event loop, with no shared mutable state between concurrent requests.

### Verdict
**OK** -- both implementations are concurrency-safe. No gap.

---

## 7. SSE Comment Line Handling

### Go
Raw byte passthrough -- comment lines (starting with `:`) are passed through as-is. Clients receive them exactly as the upstream sent them. This is correct for a transparent proxy.

### TS Reference
`pullSseEventsWithDone()` only extracts `event:` and `data:` lines. Comment lines and any other non-standard fields are silently discarded.

### Verdict
**OK** -- Go's passthrough behavior is correct for a transparent proxy. TS's stripping behavior is acceptable but technically lossy (comments sent as heartbeats by some upstreams will not reach clients). Neither behavior is a bug.

---

## 8. Multi-byte UTF-8 in Stream

### Go
The 4096-byte fixed buffer can split multi-byte UTF-8 characters at buffer boundaries. For example, a 4-byte emoji character (U+1F600 = `F0 9F 98 80`) could have 2 bytes at the end of chunk N and 2 bytes at the start of chunk N+1.

However, since Go is doing a **raw byte passthrough**, the split bytes are written to the downstream exactly as received. The downstream client's `TextDecoder` accumulates bytes and reassembles the complete character. End-to-end integrity is preserved.

**Risk**: If Go ever implements SSE parsing on raw bytes (splitting on `0x0A` bytes), multi-byte characters where one byte equals `0x0A` could cause incorrect line splitting. This affects UTF-8 sequences where the byte value `0x0A` appears (which can happen in multi-byte sequences, e.g., U+080A = `E0 A0 8A` where the third byte is `0x0A`). The TS code avoids this by decoding to string first, then operating on character boundaries.

### TS Reference
Uses `TextDecoder.decode(value, { stream: true })` for each chunk. The `stream: true` flag tells the decoder to buffer incomplete multi-byte sequences and only emit complete characters. All SSE parsing happens on JavaScript strings (decoded characters), so `\n` matching is always character-safe.

### Verdict
**LOW** -- passthrough mode is safe. If SSE parsing is ever added to Go, the bytes must be decoded to `string` (via `strings.NewReader` or `bytes.Buffer` + `utf8.DecodeRune`) before line splitting.

---

## 9. SSE Header Differences

### Go (`writeSSEHeaders` in `surface.go`)
```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
```

### TS (`startSseResponse` in `chatSurface.ts`)
```typescript
reply.raw.setHeader('Content-Type', 'text/event-stream; charset=utf-8');
reply.raw.setHeader('Cache-Control', 'no-cache, no-transform');
reply.raw.setHeader('Connection', 'keep-alive');
reply.raw.setHeader('X-Accel-Buffering', 'no');
```

Differences:
| Header | Go | TS |
|---|---|---|
| `Content-Type` | `text/event-stream` | `text/event-stream; charset=utf-8` |
| `Cache-Control` | `no-cache` | `no-cache, no-transform` |
| `X-Accel-Buffering` | Missing | `no` |

### Verdict
**MEDIUM severity gap.** Three header differences:

1. **`charset=utf-8`**: SSE events are always UTF-8. Explicitly declaring charset is best practice and prevents client misdetection. Should be added.

2. **`no-transform`**: Prevents intermediate proxies/CDNs from modifying the stream. Important for SSE integrity through Cloudflare or nginx. Should be added.

3. **`X-Accel-Buffering: no`**: Disables nginx proxy buffering. Critical for SSE delivery if the Go service is behind nginx. Without this header, nginx may buffer the entire SSE stream before forwarding, causing multi-second delays. Should be added.

---

## 10. Latency Metric Discarding

### Go
Both `handleStreamUpstream` and `handleNonStreamUpstream` receive `latencyMs int64` but discard it:
```go
_ = latencyMs
```

### TS Reference
Latency is logged and recorded at multiple points:
```typescript
const firstByteLatencyMs = getObservedResponseMeta(upstream)?.firstByteLatencyMs ?? null;
// ...
await recordStreamSuccess(latency);
// ...
await failureToolkit.log({ ..., latencyMs: latency, ... });
```

### Verdict
**MEDIUM severity bug.** Go calculates latency but discards it. This loses observability on upstream response times for all streaming and non-streaming requests. Should be plumbed into logging/metrics.

---

## 11. Format-Aware Streaming (Missing Feature)

### Go
All streaming endpoints route through `dispatchUpstream` -> `handleStreamUpstream`. There is zero format awareness in the streaming path. The following endpoints all receive raw upstream SSE bytes:
- `POST /v1/chat/completions` (OpenAI format)
- `POST /v1/messages` (Claude format)
- `POST /v1/completions` (legacy completions)
- `POST /v1/responses` (OpenAI responses)

### TS Reference
Each surface has format-aware streaming via `openAiChatTransformer.proxyStream.createSession()`:

```typescript
const streamSession = openAiChatTransformer.proxyStream.createSession({
    downstreamFormat,          // "openai" or "claude"
    modelName,
    successfulUpstreamPath,
    onParsedPayload: (payload) => { /* track usage */ },
    writeLines,                // SSE line writer
    writeRaw,                  // raw byte writer (fallback)
});
```

The `streamSession.run()` method:
1. Reads raw bytes via `reader`
2. Accumulates into SSE buffer with `TextDecoder`
3. Extracts complete SSE events with `pullSseEventsWithDone()`
4. Detects `[DONE]` and error-type events
5. Normalizes each event via `downstreamTransformer.transformStreamEvent()`
6. Serializes back to surface format via `downstreamTransformer.serializeStreamEvent()`
7. Writes output lines via `writeLines()`

### Verdict
**HIGH severity gap.** The Go implementation cannot support format conversion in streaming mode. If the upstream speaks a different format than the downstream expects, the Go proxy cannot bridge them. This limits the proxy to only relaying streams between same-format endpoints.

---

## 12. Specific Code Observations

### `handler/proxy/upstream.go:162-186` (`handleStreamUpstream`)

```go
func handleStreamUpstream(w http.ResponseWriter, resp *http.Response, latencyMs int64) {
    writeSSEHeaders(w)
    w.WriteHeader(200)
    flusher, _ := w.(http.Flusher)

    // Copy upstream headers that are relevant
    if ct := resp.Header.Get("Content-Type"); ct != "" && strings.Contains(ct, "text/event-stream") {
        // Already set by writeSSEHeaders
    }

    buf := make([]byte, 4096)
    for {
        n, err := resp.Body.Read(buf)
        if n > 0 {
            w.Write(buf[:n])
            if flusher != nil {
                flusher.Flush()
            }
        }
        if err != nil {
            break
        }
    }
    _ = latencyMs
}
```

Issues observed:
1. **Line 168-170**: Dead code -- `Content-Type` check does nothing. No headers from the upstream are forwarded to the downstream.
2. **Line 172**: Fixed 4096-byte buffer; no dynamic sizing based on upstream `Content-Length`.
3. **Line 185**: `latencyMs` discarded.
4. **Lines 176-180**: No error injection on read failure.
5. **Lines 164**: `w.WriteHeader(200)` is called unconditionally -- even if upstream returned a non-200 status with SSE content. The upstream status code is not checked before writing headers.

### `handler/proxy/upstream.go:107` (call site)

```go
if ctx.IsStream {
    handleStreamUpstream(w, resp, latencyMs)
}
```

The upstream response's `StatusCode` is never checked for the stream path. If upstream returns `401` with an SSE error stream, Go writes `200` headers then proxies the 401 SSE body. The downstream client sees `200 OK` but receives SSE error content.

### `handler/proxy/surface.go:138-141` (`writeSSEHeaders`)

Missing headers as documented in Section 9 above.

---

## 13. Test Coverage Gap

The current Go tests (`chat_test.go`) only cover the **stub** streaming path (when `upstreamCfg == nil`). There are no tests for `handleStreamUpstream` against a real or mocked upstream HTTP response. Specifically missing:

- Test: upstream SSE stream relayed correctly (byte-for-byte)
- Test: `Flusher.Flush()` called after each write
- Test: graceful handling of upstream mid-stream disconnect
- Test: SSE headers set correctly
- Test: non-200 upstream response in stream mode
- Test: large SSE event spanning multiple 4096-byte reads
- Test: `[DONE]` passthrough
- Test: concurrent stream requests

---

## Recommendations (Prioritized)

### P0 (Must fix)
1. **Add SSE error event injection on upstream disconnect.** Before closing the connection when `resp.Body.Read()` returns an error, write an SSE error event to the downstream: `event: error\ndata: {"error": {"message": "upstream disconnected", "type": "upstream_error"}}\n\n`.

2. **Add missing SSE headers.** Update `writeSSEHeaders` to include `charset=utf-8`, `no-transform`, and `X-Accel-Buffering: no`.

### P1 (Should fix)
3. **Implement SSE event parser.** Port `pullSseEventsWithDone` logic to Go. This is required for format-aware streaming, usage tracking, error detection, and `[DONE]` recognition.

4. **Wire latency metrics.** Pass `latencyMs` to a logger or metrics collector instead of discarding.

5. **Check upstream status code before starting SSE relay.** If upstream returns non-200, proxy the error as JSON (for non-streaming) or inject an SSE error event (for streaming).

### P2 (Nice to have)
6. **Add streaming test coverage.** Unit tests for `handleStreamUpstream` with mocked upstream HTTP responses covering normal relay, disconnect, large events, and error scenarios.
7. **Dynamic buffer sizing.** Consider using `io.CopyBuffer` with a larger buffer for high-throughput streams.

---

## Files Referenced

| File | Role |
|---|---|
| `D:/Code/TokenDance/metapi-go/handler/proxy/chat.go` | Chat completions entry point |
| `D:/Code/TokenDance/metapi-go/handler/proxy/responses.go` | Responses entry point |
| `D:/Code/TokenDance/metapi-go/handler/proxy/upstream.go` | Core upstream dispatch + `handleStreamUpstream` |
| `D:/Code/TokenDance/metapi-go/handler/proxy/surface.go` | SSE helpers (`writeSSEHeaders`, `sseEvent`, `sseDone`) |
| `D:/Code/TokenDance/metapi-go/handler/proxy/messages.go` | Claude messages entry point |
| `D:/Code/TokenDance/metapi-go/handler/proxy/completions.go` | Legacy completions entry point |
| `D:/Code/TokenDance/metapi-go/handler/proxy/chat_test.go` | Stub streaming tests only |
| `D:/Code/TokenDance/metapi-go/handler/proxy/helpers_test.go` | SSE header/event unit tests |
| `D:/Code/TokenDance/metapi/src/server/proxy-core/surfaces/chatSurface.ts` | TS reference: full streaming surface |
| `D:/Code/TokenDance/metapi/src/server/transformers/openai/chat/proxyStream.ts` | TS reference: stream session + event handler |
| `D:/Code/TokenDance/metapi/src/server/transformers/shared/protocolLifecycle.ts` | TS reference: SSE lifecycle (read loop + event extraction) |
| `D:/Code/TokenDance/metapi/src/server/transformers/shared/chatFormatsCore.ts` | TS reference: `pullSseEventsWithDone` SSE parser |
