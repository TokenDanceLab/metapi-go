# HTTP Protocol Compliance Audit: metapi-go vs metapi (TS reference)

**Date:** 2026-07-04  
**Scope:** `handler/proxy/*.go`, `auth/*.go`, `router/middleware.go`, `router/router.go`  
**Reference:** `metapi/src/server/routes/proxy/`, `metapi/src/server/proxy-core/`, `metapi/src/server/middleware/auth.ts`, `metapi/src/server/transformers/`

---

## 1. SSE Format Compliance

### 1.1 `data:` Prefix and `\n\n` Terminator

| Aspect | Go (`handler/proxy/surface.go`) | TS (`chatFormatsCore.ts`) | Verdict |
|--------|--------------------------------|---------------------------|---------|
| Event line format | `"data: " + data + "\n\n"` (`sseEvent()`) | `"data: ${payload}\n\n"` (`serializeSse()`) | **PASS** -- identical |
| Multi-line events | Not supported (single data line) | Not used in chat paths | **PASS** -- same limitation |
| Named events | Not supported (`event:` prefix absent) | TS supports `event: <name>\ndata: ...\n\n` for Claude | **PASS for OpenAI** -- OpenAI SSE does not use event names. Claude `message_start`/`content_block_delta`/`message_stop` events use named events -- Go skips these properly by relaying raw upstream bytes. |

### 1.2 `[DONE]` Marker

| Aspect | Go | TS | Verdict |
|--------|----|----|---------|
| Format | `"data: [DONE]\n\n"` (`sseDone()`) | `serializeSse('', '[DONE]')` = `"data: [DONE]\n\n"` | **PASS** -- identical |
| Sent after last chunk | Yes, in `writeStubResponse()` | Yes, via `serializeStreamDone()` in `finalize()` | **PASS** |
| In live upstream relay | No (raw upstream bytes relayed as-is in `handleStreamUpstream`) | TS has full stream processing pipeline (parse/transform/serialize) | **INFO** -- raw relay is acceptable for transparent proxy; transformation layer is not yet implemented in Go |

### 1.3 SSE Line Parsing

Go `pullSseDataEvents()` in `proxy/failure_judge.go` correctly handles `\n`-delimited SSE lines. TS `pullSseEventsWithDone()` also strips `\r\n`. Minor edge: Go does not handle `\r\n` normalization, while TS does. If upstream sends `\r\n` separators, Go's `pullSseDataEvents` may miss lines. **LOW severity** because most OpenAI-compatible APIs use `\n`.

---

## 2. Content-Type Headers

### 2.1 Streaming Responses (SSE)

| Aspect | Go (`surface.go:writeSSEHeaders`) | TS (`chatSurface.ts:startSseResponse`) | Verdict |
|--------|-----------------------------------|----------------------------------------|---------|
| Content-Type | `text/event-stream` | `text/event-stream; charset=utf-8` | **ISSUE** -- missing `charset=utf-8` |
| Cache-Control | `no-cache` | `no-cache, no-transform` | **ISSUE** -- missing `no-transform` |
| Connection | `keep-alive` | `keep-alive` | **PASS** |
| X-Accel-Buffering | **NOT SET** | `no` | **ISSUE** -- missing nginx buffering control |

### 2.2 Error Responses (JSON)

| Aspect | Go (`router.go:writeJSONError`, `auth/admin.go:writeJSON`) | TS | Verdict |
|--------|------------------------------------------------------------|----|---------|
| Content-Type | `application/json` | `application/json; charset=utf-8` | **ISSUE** -- missing `; charset=utf-8` |

### 2.3 Non-Stream Success Responses

| Aspect | Go (`upstream.go:handleNonStreamUpstream`) | TS | Verdict |
|--------|-------------------------------------------|----|---------|
| Headers | Relays upstream headers, strips `Content-Length` and `Transfer-Encoding` | Relays upstream with Fastify | **PASS** -- stripping is correct for Go (body may differ after processing) |
| Status code | Relayed from upstream | Relayed from upstream | **PASS** |

---

## 3. CORS Headers

| Aspect | Go (`router/middleware.go:CORS`) | TS (Fastify `@fastify/cors` defaults) | Verdict |
|--------|--------------------------------|---------------------------------------|---------|
| AllowedOrigins | `["*"]` | `*` (same) | **PASS** |
| AllowedMethods | `["GET","POST","PUT","PATCH","DELETE","OPTIONS"]` | Standard OpenAPI methods | **PASS** |
| AllowedHeaders | `["*"]` | Mirrors request | **PASS** (Go is more permissive but acceptable for proxy) |
| ExposedHeaders | `["Link"]` | Fastify defaults (none) | **MINOR** -- consider adding `Retry-After`, `X-Request-Id` |
| AllowCredentials | `false` | `false` (default) | **PASS** -- correct with wildcard origin |
| MaxAge | `300` | Fastify defaults | **PASS** |
| Preflight handling | Via `go-chi/cors` middleware | Via `@fastify/cors` | **PASS** |

---

## 4. Error Status Codes: 401 vs 403 Distinction

### 4.1 Proxy Auth (`auth/downstream.go`)

| Scenario | Go Status | TS Status | Verdict |
|----------|-----------|-----------|---------|
| Empty/missing token | **401** (`"missing"`) | **401** (`"missing"`) | **PASS** |
| Unknown/invalid API key | **403** (`"invalid"`) | **403** (`"invalid"`) | **PASS** |
| Disabled key | **403** (`"disabled"`) | **403** (`"disabled"`) | **PASS** |
| Expired key | **403** (`"expired"`) | **403** (`"expired"`) | **PASS** |
| Over cost | **403** (`"over_cost"`) | **403** (`"over_cost"`) | **PASS** |
| Over requests | **403** (`"over_requests"`) | **403** (`"over_requests"`) | **PASS** |
| DB query failed | **500** | Would be 500 (unhandled) | **MINOR** -- Go explicitly returns 500, TS would throw; both result in 5xx |

### 4.2 Admin Auth (`auth/admin.go`)

| Scenario | Go Status | TS Status | Verdict |
|----------|-----------|-----------|---------|
| Missing Authorization header | **401** | **401** | **PASS** |
| Wrong Bearer token | **403** | **403** | **PASS** |
| IP not in allowlist | **403** | **403** | **PASS** |
| Empty allowlist | Allowed (bypass) | Allowed (bypass) | **PASS** |

### 4.3 Proxy Handler Layer (`handler/proxy/surface.go:PrepareCtx`)

| Scenario | Go Status | TS Status | Verdict |
|----------|-----------|-----------|---------|
| Missing auth context | 401 (`"unauthorized"`) | 401 (from middleware) | **PASS** |
| Model not allowed by policy | 403 (`"model not allowed"`) | 403 (`"Model not allowed..."`) | **PASS** |
| Body parse failure | 400 | 400 | **PASS** |
| Missing model | 400 | 400 | **PASS** |

### 4.4 Semantic 401 vs 403 Rule

Both codebases correctly follow:
- **401**: Not authenticated (missing credentials) -- allows client to retry with credentials
- **403**: Authenticated but not authorized (invalid/disabled/expired key, wrong permissions) -- retrying with same credentials will not help

Verdict: **PASS** -- full parity.

---

## 5. Retry-After Header on 429

### 5.1 Downstream-facing Retry-After

| Aspect | Go | TS | Verdict |
|--------|----|----|---------|
| Retry-After sent to downstream on 429 | **Not implemented** | **Not implemented** | **PASS** -- neither sends it |

Neither codebase sends a `Retry-After` header to downstream clients. Internal 429 handling is purely about channel cooldown.

### 5.2 Internal 429 Retry Logic

| Layer | Go | TS | Verdict |
|-------|----|----|---------|
| `ShouldRetryProxyRequest` | **Present** (`proxy/retry_policy.go:146`) -- 408/409/425/429 always retryable | **Present** (`proxyRetryPolicy.ts:87`) -- 408/409/425/429 always retryable | **PASS** -- identical |
| `SurfaceFailureToolkit` | **Present** (`proxy/surface.go:272`) -- calls `ShouldRetryProxyRequest` | **Present** (`sharedSurface.ts:578`) -- calls `shouldRetryProxyRequest` | **PASS** -- pattern exists |
| `dispatchUpstream` (handler) | **MISSING** -- does NOT call `SurfaceFailureToolkit`; only retries on network errors | Integrated via `SiteApiEndpointRequestError` which triggers retry | **ISSUE** -- handler-level dispatch bypasses retry framework |

### 5.3 Wiring Gap

The Go code has a fully implemented `SurfaceFailureToolkit` with proper 429 retry in `proxy/surface.go`, but the handler-layer dispatcher in `handler/proxy/upstream.go:dispatchUpstream` does not use it. The handler does a raw `http.DefaultClient.Do(req)` and only retries on `err != nil` (network errors). Any HTTP error response (429, 502, 503) is relayed as-is without retry.

The retry logic at the `proxy/` package level is correct and tested, but it is **not wired into the actual request path**.

---

## 6. Transfer-Encoding for Streaming

| Aspect | Go | TS | Verdict |
|--------|----|----|---------|
| Explicit header | Not set | Not set | **PASS** -- Go's `net/http` automatically adds `Transfer-Encoding: chunked` when using `Flusher` in HTTP/1.1 |
| Chunked response | Via `http.Flusher.Flush()` after each write | Via `reply.raw.write()` in hijacked stream | **PASS** |
| Header relay stripping | `handleNonStreamUpstream` strips both `Content-Length` and `Transfer-Encoding` when relaying | Fastify handles internally | **PASS** -- prevents stale Content-Length |

---

## 7. Additional Findings

### 7.1 Stub Response Compliance

`writeStubResponse()` in `handler/proxy/upstream.go` properly:
1. Sets SSE headers via `writeSSEHeaders()`
2. Calls `w.WriteHeader(200)` before writing body
3. Uses identical `ssEvent()` and `sseDone()` format as production path
4. Flushes after each event
Verdict: **PASS** -- the stub correctly exercises the SSE code path.

### 7.2 WebSocket Upgrade (426)

`HandleResponsesGet426` returns 426 `Upgrade Required` for GET `/v1/responses` and GET `/responses`. TS has the same behavior via `responsesWebsocket.ts`. Verdict: **PASS**.

### 7.3 Streaming Detection

`isStreamFromBody()` handles `bool`, `string` (`"true"`/`"1"`), and numeric (`!= 0`) values. TS checks the `stream` field via its parsed body. Go additionally handles `"1"` which is slightly more permissive but harmless. **PASS**.

### 7.4 204 Response for DELETE

`HandleVideosDelete` returns `w.WriteHeader(204)` with no body (no `Content-Type` set). This is correct per HTTP spec for 204 No Content. **PASS**.

### 7.5 SPA Fallback

`router.go:setupSPAFallback` correctly identifies API paths (`/api/*`, `/v1/*`) and returns 404 JSON; non-API paths fall through to `index.html`. TS uses a similar pattern with Fastify's `setNotFoundHandler`. **PASS**.

### 7.6 Charset on SPA HTML

SPA fallback sets `Content-Type: text/html; charset=utf-8` with `charset` -- this is the only place charset is set correctly in Go, ironically making JSON/SSE responses the outliers. **PASS** for SPA; highlights issue with JSON/SSE.

---

## 8. Summary of Issues

### HIGH Severity

| # | Issue | Location | Detail |
|---|-------|----------|--------|
| H1 | Retry framework not wired into handler dispatch | `handler/proxy/upstream.go:dispatchUpstream` | `SurfaceFailureToolkit` with proper 429/5xx retry exists in `proxy/surface.go` but is not used by the handler-level dispatcher. HTTP error responses from upstream (429, 502, 503) are relayed without retry. Only network errors trigger retry. |

### MEDIUM Severity

| # | Issue | Location | Detail |
|---|-------|----------|--------|
| M1 | SSE Content-Type missing charset | `handler/proxy/surface.go:writeSSEHeaders` | TS uses `text/event-stream; charset=utf-8`; Go uses `text/event-stream` |
| M2 | JSON Content-Type missing charset | `router.go:writeJSONError`, `auth/admin.go:writeJSON`, `messages.go:writeJSON` | TS uses `application/json; charset=utf-8`; Go uses `application/json` |
| M3 | SSE Cache-Control missing `no-transform` | `handler/proxy/surface.go:writeSSEHeaders` | TS: `no-cache, no-transform`; Go: `no-cache`. The `no-transform` directive prevents intermediary proxies from re-compressing the SSE stream. |
| M4 | SSE missing `X-Accel-Buffering: no` | `handler/proxy/surface.go:writeSSEHeaders` | TS sets this header to prevent nginx from buffering SSE responses. Critical if metapi-go is deployed behind nginx. |

### LOW Severity

| # | Issue | Location | Detail |
|---|-------|----------|--------|
| L1 | `\r\n` not normalized in SSE parsing | `proxy/failure_judge.go:pullSseDataEvents` | TS normalizes `\r\n` to `\n`; Go only splits on `\n`. Upstreams sending `\r\n` may cause false-empty-content detection. |
| L2 | ExposedHeaders limited to `["Link"]` | `router/middleware.go:CORS` | Consider adding `Retry-After`, `X-Request-Id`, `X-RateLimit-*` for client visibility. |
| L3 | `http.DefaultClient` used for upstream | `handler/proxy/upstream.go:dispatchUpstream` | No timeout, no connection pooling tuning. TS uses a configured `dispatchRuntimeRequest` with proper timeouts. |

---

## 9. Comparison Matrix

| Category | Sub-category | Go Status | TS Status | Parity |
|----------|-------------|-----------|-----------|--------|
| SSE | `data:` prefix | Correct | Correct | PASS |
| SSE | `\n\n` terminator | Correct | Correct | PASS |
| SSE | `[DONE]` marker | Correct | Correct | PASS |
| SSE | `Content-Type` | `text/event-stream` | `text/event-stream; charset=utf-8` | GAP |
| SSE | `Cache-Control` | `no-cache` | `no-cache, no-transform` | GAP |
| SSE | `X-Accel-Buffering` | Missing | `no` | GAP |
| JSON Error | `Content-Type` | `application/json` | `application/json; charset=utf-8` | GAP |
| CORS | Origins/Methods/Headers | Full config | Fastify defaults | PASS |
| CORS | ExposedHeaders | `["Link"]` | None | MINOR |
| Auth | 401 vs 403 semantics | Exact parity | Reference | PASS |
| Auth | Admin IP allowlist | CIDR + exact match | CIDR + exact match | PASS |
| Auth | Token extraction priority | Exclusive Authorization | Exclusive Authorization | PASS |
| Retry | `ShouldRetryProxyRequest` | 408/409/425/429 retryable | 408/409/425/429 retryable | PASS |
| Retry | `SurfaceFailureToolkit` | Fully implemented | Reference | PASS |
| Retry | **Handler wiring** | **NOT WIRED** | Integrated | **GAP** |
| 429 | `Retry-After` downstream | Not sent | Not sent | PASS (neither) |
| Transfer-Encoding | Streaming | Auto via Flusher | Auto via raw.write | PASS |
| Transfer-Encoding | Non-stream relay | Stripped (correct) | Fastify handles | PASS |

---

## 10. Recommended Remediation Priority

1. **Wire retry framework into handler dispatch** -- The most impactful gap. Modify `dispatchUpstream` in `handler/proxy/upstream.go` to check response status codes and use `SurfaceFailureToolkit` for retry decisions, matching the TS behavior of retrying on 429/5xx from upstream.

2. **Add charset to Content-Type headers** -- One-line fix in `writeSSEHeaders`, `writeJSONError`, and `writeJSON` to append `; charset=utf-8`.

3. **Add SSE-specific headers** -- Add `no-transform` to Cache-Control and `X-Accel-Buffering: no` in `writeSSEHeaders`.

4. **Add `http.Client` with timeouts** -- Replace `http.DefaultClient` with a configured client that has connect/read/write timeouts matching the TS `firstByteTimeoutMs`.

5. **Normalize `\r\n` in SSE parsing** -- Add `strings.ReplaceAll(rawText, "\r\n", "\n")` before SSE line parsing in `pullSseDataEvents`.
