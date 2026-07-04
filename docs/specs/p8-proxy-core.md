# P8: ProxyCore -- Proxy Orchestration Core

**S.U.P.E.R**: S (single responsibility) | **Depends on**: P7 (TokenRouter) | **Size**: L

## Original TS Reference

- `D:\Code\TokenDance\metapi\src\server\proxy-core\conductor\DefaultProxyConductor.ts` -- action-based retry conductor
- `D:\Code\TokenDance\metapi\src\server\proxy-core\orchestration\endpointFlow.ts` -- endpoint-level iteration
- `D:\Code\TokenDance\metapi\src\server\proxy-core\channelSelection.ts` -- sticky session + tester + route refresh
- `D:\Code\TokenDance\metapi\src\server\proxy-core\surfaces\chatSurface.ts` -- channel retry loop + SSE + count_tokens
- `D:\Code\TokenDance\metapi\src\server\proxy-core\surfaces\sharedSurface.ts` -- session/sticky/failure/log toolkits
- `D:\Code\TokenDance\metapi\src\server\proxy-core\cliProfiles\` -- 4 CLI profiles
- `D:\Code\TokenDance\metapi\src\server\proxy-core\conductor\retryPolicy.ts` -- action routing
- `D:\Code\TokenDance\metapi\src\server\services\proxyChannelCoordinator.ts` -- lease + sticky bindings
- `D:\Code\TokenDance\metapi\src\server\services\proxyChannelRetry.ts` -- max attempts/retries
- `D:\Code\TokenDance\metapi\src\server\services\proxyFailureJudge.ts` -- content-based failure detection
- `D:\Code\TokenDance\metapi\src\server\services\proxyRetryPolicy.ts` -- HTTP status + text pattern retry

## Go Module Structure

```
proxy/
  conductor.go             # ProxyConductor: action-based retry conductor (optional, for non-surface flows)
  endpoint_flow.go         # ExecuteEndpointFlow: endpoint-level iteration loop
  channel_selection.go     # Channel selection with sticky session, tester override, route refresh
  surface.go               # SharedSurface: sticky session, lease, failure toolkit, success recording
  session.go               # ProxyChannelCoordinator: session-scoped lease + sticky bindings
  failure_judge.go         # detectProxyFailure: content-based failure detection
  retry_policy.go          # shouldRetryProxyRequest: HTTP status + text pattern classification
  executor.go              # RuntimeExecutor: HTTP request dispatch
  profile.go               # CLIProfile interface + detection
  profiles/
    generic.go             # genericCliProfile -- unconditional fallback
    codex.go               # codexCliProfile -- codex-specific headers/session
    claude_code.go         # claudeCodeCliProfile -- claude-code user-agent/headers
    gemini_cli.go          # geminiCliProfile -- gemini CLI internal paths
```

## Architecture Overview

The proxy core has TWO nested loops. The outer loop (in the surface handler, e.g. `handleChatSurfaceRequest`) retries at the **channel level**. The inner loop (in `executeEndpointFlow`) iterates over **endpoint candidates** within a single channel/site.

```
HTTP request arrives
  |
  v
1. Client context detection (profile detection from headers + body + path)
2. Downstream policy resolution (routing rules per API key)
3. Web search simulation early-return (Claude format only)
4. Model allowlist check
5. Input file resolution (for authorized resource owners)
6. Conversation file summary (affects endpoint candidate selection)
  |
  v
OUTER LOOP: while retryCount <= maxRetries
  |
  +-- 7a. Channel selection (with sticky session preference on first attempt)
  |       - Tester forced channel (no retry, no sticky, single attempt)
  |       - Sticky preferred channel (first attempt only)
  |       - Normal selection via TokenRouter
  |       - Route refresh on empty selection (first attempt only)
  |  7b. Endpoint candidate resolution (per channel/site/model)
  |  7c. Debug trace: record selection + candidates
  |  7d. Session lease acquisition (only if stickySessionKey is present)
  |       - Queued wait with configurable timeout
  |       - On timeout: optionally retry another channel
  |
  +-- INNER LOOP: executeEndpointFlow (runWithSiteApiEndpointPool)
        |
        +-- 8a. Service tier policy check (may throw immediate fatal)
        |  8b. Build upstream request per endpoint candidate
        |  8c. Dispatch HTTP request with first-byte timeout
        |  8d. On success: record + return
        |  8e. On failure:
        |       - First-byte timeout: continue to next endpoint (if cross-protocol fallback enabled)
        |       - Try OAuth recovery (if 401/403 on OAuth account)
        |       - Check shouldAbortRemainingEndpoints (rate-limit, quota patterns)
        |       - Check shouldDowngrade (protocol switch, e.g. chat/completions -> responses)
        |       - Record endpoint failure to runtime memory
        |  8f. If all endpoints exhausted: throw SiteApiEndpointRequestError
  |
  +-- 9. On success:
  |       - Record usage (with self-log fallback if upstream reports none)
  |       - Bind sticky channel for session
  |       - Write proxy_log (status='success')
  |       - Finalize debug trace
  |       - Return response to downstream
  |
  +-- 9. On failure:
          - Clear sticky channel binding
          - Classify failure (service tier block / upstream / execution error)
          - Decide: retry (same channel, next iteration) or respond (terminal)
          - Write proxy_log (status='failed')
          - On terminal: reportProxyAllFailed alert + respond with error
```

Key insight: `retryCount` is the **channel-level retry counter**. `maxRetries = max(0, proxyMaxChannelAttempts - 1)`. Default `proxyMaxChannelAttempts = 1`, so default `maxRetries = 0`, meaning the while-loop runs exactly once (no retry) by default. A deployment config of `3` means `maxRetries = 2`, totaling up to 3 attempts.

## Functional Specification

### 1. Channel Selection (`channelSelection.go`)

```go
func SelectProxyChannelForAttempt(input ChannelSelectionInput) (*SelectedChannel, error)
```

**Input**:
- `requestedModel`: model name from downstream request
- `downstreamPolicy`: routing policy per API key
- `excludeChannelIds`: channels already failed (empty on first attempt)
- `retryCount`: current attempt index (0-based)
- `stickySessionKey`: optional session binding key (null if no session)
- `forcedChannelId`: optional tester override (from `x-metapi-tester-forced-channel-id` header)

**Tester Forced Channel**:
- Activated when `x-metapi-tester-request: 1` header is present AND client IP is loopback (`127.0.0.1`, `::1`, `::ffff:127.0.0.1`)
- `x-metapi-tester-forced-channel-id` forces a specific channel via `tokenRouter.SelectPreferredChannel`
- Forced channels never retry: if `retryCount > 0`, returns null immediately
- Unavailable forced channel returns a specific error message: "指定通道 #N 当前不可用，固定通道模式不会自动切换其他通道"

**Sticky Session Preference** (first attempt only, `retryCount === 0`):
- If `stickySessionKey` is present, calls `ProxyChannelCoordinator.GetStickyChannelId(key)` to find the preferred channel
- Uses `tokenRouter.SelectPreferredChannel` for that channel ID
- If the preferred channel is unavailable, triggers route refresh and retries once
- If still unavailable after refresh, clears the sticky binding

**Normal Selection**:
- First attempt (`retryCount === 0`): `tokenRouter.SelectChannel(model, policy)`
- Subsequent attempts (`retryCount > 0`): `tokenRouter.SelectNextChannel(model, excludeChannelIds, policy)`

**Route Refresh on Empty Selection** (first attempt only):
- If `SelectChannel` returns null and routes haven't been refreshed yet, triggers `routeRefreshWorkflow.RefreshModelsAndRebuildRoutes()` then retries `SelectChannel` once
- Only fires once per request (guarded by `refreshedRoutes` flag)

**CanRetryChannelSelection**:
```go
func CanRetryChannelSelection(retryCount int, forcedChannelId *int) bool
```
- Returns false if forced channel is set
- Otherwise delegates to `canRetryProxyChannel(retryCount)` which checks `retryCount < maxRetries`

### 2. Session Lease Manager (`session.go`)

The `ProxyChannelCoordinator` manages two orthogonal concerns: sticky session bindings and concurrency leases. Both are **conditional**: they only activate when the downstream request carries a valid sticky session key, AND the channel's account uses session-scoped credentials (`credential_mode === 'session'` or has an OAuth provider).

```
type ProxyChannelCoordinator struct {
    stickyBindings  map[string]StickyEntry    // key -> {channelId, expiresAtMs}
    channelStates   map[int]ChannelRuntimeState // channelId -> {activeLeaseIds, queue}
    nextLeaseId     int
}
```

**Config values** (all read from config, with enforced minimums):

| Config Key | Type | Default | Min | Description |
|---|---|---|---|---|
| `proxyStickySessionEnabled` | bool | false | -- | Master switch; if false, no sticky key is built and no binding occurs |
| `proxyStickySessionTtlMs` | int | 0 | 30000 | Sticky binding expiry, minimum 30s |
| `proxySessionChannelConcurrencyLimit` | int | 0 | 0 | Max concurrent requests per session-scoped channel. 0 = unlimited (noop lease) |
| `proxySessionChannelQueueWaitMs` | int | 0 | 0 | Max queue wait when channel is saturated. 0 = instant timeout |
| `proxySessionChannelLeaseTtlMs` | int | 0 | 5000 | Lease expiry since last touch, minimum 5s |
| `proxySessionChannelLeaseKeepaliveMs` | int | 0 | 1000 | Interval for lease touch timer, minimum 1s |

**Sticky Session Key Construction**:
```
buildStickySessionKey(clientKind, sessionId, requestedModel, downstreamPath, downstreamApiKeyId) -> string | null
```
- Returns null if `proxyStickySessionEnabled` is false, or sessionId/model are empty
- Format: `"key:{apiKeyId}|{clientKind}|{downstreamPath}|{requestedModel}|{sessionId}"`
- Keys are lexicographically comparable (pipe-delimited segments)

**Sticky Bindings**:
- `bindStickyChannel(key, channelId, accountIdentity)`: only binds if sticky sessions enabled AND account is session-scoped. Sets `expiresAtMs = now + stickySessionTtlMs`
- `getStickyChannelId(key)`: returns channelId if not expired, cleans up expired entries on read
- `clearStickyChannel(key, channelId?)`: removes binding. If channelId provided, only removes if it matches

**Session-Scoped Channel Check**:
```go
func isSessionScopedChannel(extraConfig, oauthProvider) bool
```
A channel is session-scoped when `getCredentialModeFromExtraConfig(extraConfig) === 'session'` OR `hasOauthProvider(oauthProvider)` is true. Only session-scoped channels get sticky bindings and concurrency-limited leases. Non-session-scoped channels get noop leases (always acquired, unlimited).

**Lease Acquisition**:
```go
func acquireChannelLease(channelId, accountExtraConfig, accountOauthProvider) -> AcquireResult
```

When `channelId <= 0` (no sticky session key present), returns an immediate noop lease -- preserving pre-sticky-session parallel behavior.

When `concurrencyLimit <= 0` or the account is not session-scoped, returns an immediate noop lease (labeled with the channel ID but no tracking).

Otherwise:
1. Get or create `ChannelRuntimeState` for this channel
2. Prune cancelled waiters from queue
3. If `activeLeaseIds.size < concurrencyLimit`: create a tracked lease and return immediately
4. If queue is at capacity and `queueWaitMs <= 0`: return timeout immediately (waitMs=0)
5. Otherwise: push a waiter onto the queue with a `queueWaitMs` timeout. On timeout, the waiter is cancelled and a `{status: 'timeout', waitMs}` result is resolved

**Tracked Lease Lifecycle**:
- Each lease gets a unique `leaseId` stored in `activeLeaseIds`
- On creation: starts an expiry timer (`leaseTtlMs`). On expiry, calls `release()`
- Keepalive timer: fires every `keepaliveMs`, calling `touch()` which resets the expiry timer. This keeps the lease alive during long-running streams
- On `release()`: clears both timers, removes leaseId from active set, drains queue (promotes waiting request if slots available), garbage-collects channel state if empty
- `isActive()`: returns `!released`

**Lease Result Types**:
```go
type AcquireResult struct {
    Status string  // "acquired" or "timeout"
    Lease  *Lease  // non-nil only for "acquired"
    WaitMs int     // wait duration before timeout (0 for instant timeout)
}

type Lease struct {
    ChannelId int
    IsActive() bool
    Release()
    Touch()
}
```

**Gating rule**: In `sharedSurface.acquireSurfaceChannelLease`, the lease is acquired with:
```go
channelId = stickySessionKey ? selected.channel.id : 0
```
This means: requests WITHOUT a sticky session key always get an immediate noop lease. Only requests that carry a valid downstream session key contend for the per-channel concurrency limit.

### 3. CLI Profiles

4 profiles defined in `registry.ts`. Detection uses ordered matching: `claude_code` -> `codex` -> `gemini_cli`, with `generic` as unconditional fallback.

```go
type CliProfileId string  // "generic" | "codex" | "claude_code" | "gemini_cli"

type CliProfileCapabilities struct {
    SupportsResponsesCompact           bool
    SupportsResponsesWebsocketIncremental bool
    PreservesContinuation              bool
    SupportsCountTokens                bool
    EchoesTurnState                    bool
}

type CliProfileDefinition struct {
    ID           CliProfileId
    Capabilities CliProfileCapabilities
    Detect(input DetectInput) *DetectedProfile
}
```

| Profile | ID | Detection | Capabilities |
|---|---|---|---|
| **Codex** | `codex` | Path is `/v1/responses`, `/v1/responses/*`, or `/v1/chat/completions` AND (official codex client headers, `openai-beta` header, `x-stainless-*` headers, codex session headers, or `x-codex-turn-state` header). Session from `session_id` / `session-id` / `conversation_id` / `conversation-id` header. Client confidence: exact if official headers match, heuristic otherwise. | responsesCompact: true, websocketIncremental: true, preservesContinuation: true, countTokens: false, echoesTurnState: true |
| **Claude Code** | `claude_code` | Path is `/v1/messages`, `/anthropic/v1/messages`, or `/v1/messages/count_tokens` AND (User-Agent matches `claude-cli/\d+\.\d+\.\d+` + `anthropic-beta` header + `anthropic-version` header + `x-app: cli`, OR body `metadata.user_id` matches `user_[0-9a-f]{64}_account__session_[UUID]`). Session extracted from `metadata.user_id` via regex. Confidence: exact. | responsesCompact: false, websocketIncremental: false, preservesContinuation: true, countTokens: true, echoesTurnState: false |
| **Gemini CLI** | `gemini_cli` | Path is `/v1internal:generatecontent`, `/v1internal:streamgeneratecontent`, or `/v1internal:counttokens` AND body shape has `model` string OR `contents` array OR `request.contents` array OR `request.model` string. | responsesCompact: false, websocketIncremental: false, preservesContinuation: false, countTokens: true, echoesTurnState: false |
| **Generic** | `generic` | Unconditional fallback (matches everything not caught above). | All false |

**Detection input**: `{downstreamPath, headers, body}` extracted from the incoming request. Detection feeds into client context which is used for:
- Sticky session key construction (clientKind segment)
- Debug trace metadata (clientKind, sessionId, traceHint)
- Proxy log client fields (clientFamily, clientAppId, clientAppName, clientConfidence)

### 4. ExecuteEndpointFlow (`endpoint_flow.go`)

This is the **endpoint-level** iteration loop. It is called INSIDE the channel retry loop, wrapped by `runWithSiteApiEndpointPool` which iterates over site API base URLs.

```go
func ExecuteEndpointFlow(input ExecuteEndpointFlowInput) EndpointFlowResult
```

**Input**:
- `siteUrl`: base URL of the upstream site
- `proxyUrl`: optional proxy override URL
- `disableCrossProtocolFallback`: if true, stops on first-byte timeout (no next-endpoint fallback)
- `endpointCandidates`: ordered list of upstream endpoints (e.g. `[responses, chat/completions, messages]`)
- `buildRequest`: factory that creates `BuiltEndpointRequest` for each endpoint candidate
- `dispatchRequest`: optional custom dispatch function (defaults to `fetch` with site proxy init)
- `firstByteTimeoutMs`: first-byte timeout in ms (0 = none)
- `tryRecover`: recovery hook (called before finalizing endpoint failure -- used for OAuth refresh)
- `shouldDowngrade`: callback to decide protocol downgrade to next endpoint
- `shouldAbortRemainingEndpoints`: callback to abort all remaining candidates (rate-limit, quota)
- `onDowngrade`: hook called before switching to next endpoint on downgrade
- `onAttemptFailure`: hook called on each endpoint attempt failure
- `onAttemptSuccess`: hook called on success

**Flow** (for-loop over endpointCandidates):
1. If `endpointCandidates.length == 0`: return `{ok: false, status: 502}` immediately
2. For each endpoint candidate at index `i`:
   a. Build request via `buildRequest(endpoint, i)`
   b. Construct target URL: `proxyUrl + path` if proxy configured, else `siteUrl + path`
   c. Dispatch HTTP request with first-byte timeout observation (`fetchWithObservedFirstByte`)
   d. If response.ok: call `onAttemptSuccess` hook, return `{ok: true, upstream, upstreamPath}`
   e. Read error body text: `readRuntimeResponseText(response)`
   f. **First-byte timeout** (response is timeout marker AND not last endpoint):
      - Call `onAttemptFailure`, store finalStatus=408
      - If `disableCrossProtocolFallback`: break (stop iterating)
      - Otherwise: continue to next endpoint (cross-protocol fallback)
   g. **Try recovery** (if `tryRecover` provided):
      - Call `tryRecover(baseContext)`. If recovery returns a successful upstream response, call `onAttemptSuccess` and return `{ok: true}`
      - On OAuth recovery failure: the recovery function mutates the original context (request, response, rawErrText) so the original endpoint's error processing continues with the tryRecover-failure state
   h. Call `onAttemptFailure` hook
   i. If `disableCrossProtocolFallback` AND not last endpoint: break
   j. **Abort remaining endpoints** (if `shouldAbortRemainingEndpoints` returns true): break immediately, no more candidates tried
      - Used for rate-limit (429), quota exceeded, bad gateway, service unavailable, connection reset patterns
   k. **Downgrade** (if `shouldDowngrade` returns true AND not last endpoint):
      - Call `onDowngrade` hook (which promotes the required endpoint candidate for next attempt)
      - Continue to next endpoint candidate
   l. Otherwise: break (terminal failure for this endpoint set)

**Endpoint continuation matrix**:

| Condition | Is last endpoint? | Action |
|---|---|---|
| Response.ok | -- | Return success immediately |
| First-byte timeout + !disableCrossProtocolFallback | No | Continue to next endpoint |
| First-byte timeout + disableCrossProtocolFallback | No | Break (stop iteration) |
| Recovery succeeded | -- | Return success immediately |
| Recovery failed | -- | Continue to next step in flow |
| shouldAbortRemainingEndpoints true | No | Break immediately |
| shouldDowngrade true | No | Continue to next endpoint |
| Any failure | Yes (last) | Break (terminal) |

### 5. Channel-Level Retry Loop (chatSurface / count_tokens)

The channel retry loop lives in the surface handler. It wraps the endpoint flow and handles channel-level failure decisions.

**maxRetries**: `getProxyMaxChannelRetries()` = `max(0, proxyMaxChannelAttempts - 1)`. Default `proxyMaxChannelAttempts = 1` gives `maxRetries = 0`.
**Loop**: `while (retryCount <= maxRetries)` -- this means up to `maxRetries + 1` total attempts.

#### 5a. Failure Classification (chatSurface level)

There is NO single "FailureJudge.Classify" function. The chatSurface catch block handles three distinct failure categories:

**1. Service Tier Blocked** (`err.serviceTierBlocked === true`):
- Thrown by `applyOpenAiServiceTierPolicy` when the requested model + account tier combination is blocked by policy rules
- Returns immediately with 400 (no retry, no channel switch)
- Does NOT go through normal retry decision flow

**2. Site API Endpoint Failure** (`isSiteApiEndpointFailure`):
- Covers: SiteApiEndpointRequestError, 500+ status, serviceTierBlocked
- Calls `failureToolkit.handleUpstreamFailure()` which:
  - Records failure to `tokenRouter.recordFailure`
  - Writes proxy_log (status='failed')
  - Checks `shouldRetryProxyRequest(status, errText)`
  - If retryable AND `retryCount < maxRetries`: returns `{action: 'retry'}`
  - Otherwise: reports `reportProxyAllFailed` alert, returns `{action: 'respond', status, payload}`
- If action is 'retry' AND `canRetryChannelSelection(retryCount, forcedChannelId)`: `retryCount++`, `continue` loop
- If action is 'retry' but cannot retry channel selection: return upstream error response

**3. Execution Error** (network failure, unexpected errors):
- Calls `failureToolkit.handleExecutionError()` which:
  - Records failure to `tokenRouter.recordFailure`
  - Writes proxy_log (status='failed', httpStatus=0)
  - If `retryCount < maxRetries`: returns `{action: 'retry'}`
  - Otherwise: reports alert, returns `{action: 'respond', status: 502}`

#### 5b. shouldRetryProxyRequest (HTTP Status + Text Pattern)

```go
func shouldRetryProxyRequest(status int, upstreamErrorText string) bool
```

Decision matrix:

| Status | Condition | Retryable? |
|---|---|---|
| >= 500 | Always | Yes |
| 408, 409, 425, 429 | Always | Yes |
| 401, 403 | Always (for OAuth token refresh) | Yes |
| Any | Error text matches MODEL_UNSUPPORTED_PATTERNS (e.g. "does not support the model") | Yes |
| Any | Error text matches NON_RETRYABLE_REQUEST_PATTERNS (e.g. "invalid request body", "validation", "malformed", "invalid json") | No |
| 400, 404, 422 | Error text does NOT match NON_RETRYABLE patterns | No |
| Any | Error text matches RETRYABLE_CHANNEL_LOCAL_PATTERNS (e.g. "unsupported legacy protocol", "invalid api key", "forbidden", "rate limit", "quota", "bad gateway", "gateway timeout", "service unavailable") | Yes |
| Other | -- | No |

Key insight: 401 and 403 are RETRYABLE because they may be resolved by OAuth token refresh. 400, 404, 422 are non-retryable UNLESS the error text matches retryable patterns (e.g. an upstream saying "please use /v1/responses" which is a protocol-switch hint, not a request validation error).

There is no explicit `Retry-After` header handling in the retry decision -- the decision is binary retry/no-retry.

#### 5c. Detected Failure (content-based, non-stream only)

When a non-stream response has no error HTTP status but the response body contains failure indicators, `detectProxyFailure` is called:

```go
func detectProxyFailure(rawText string, usage *UsageSummary) *FailureResult
```

This is PURELY content-based. It does NOT look at HTTP status codes. It is called AFTER a response body is received.

1. **Keyword matching**: If `config.proxyErrorKeywords` is non-empty, checks if any keyword appears (case-insensitive) in the raw response text. Returns `{status: 502, reason: "Upstream response matched failure keyword: {keyword}"}`
2. **Empty content check** (only if `config.proxyEmptyContentFailEnabled`): If `completionTokens <= 0` AND `detectHasUpstreamOutput(rawText)` returns false, returns `{status: 502, reason: "Upstream returned empty content"}`

`detectHasUpstreamOutput` parses JSON or SSE event streams and checks for actual content (choices with text/tool_calls, output array items, content parts, etc.). It considers tool calls and function calls as valid output. Pure `[DONE]` SSE streams without content are treated as empty.

When `detectProxyFailure` returns a failure:
- `failureToolkit.handleDetectedFailure()` is called (same pattern as upstream failure)
- The failure status (502) and reason are used in `shouldRetryProxyRequest` for the retry decision

### 6. Sticky Session Binding Lifecycle

**Binding** (on success):
- Non-stream: `bindSurfaceStickyChannel` called after `recordSurfaceSuccess`
- Stream: `bindSurfaceStickyChannel` called after stream completes successfully
- Only binds if sticky sessions enabled AND channel is session-scoped
- Sets expiry = `now + proxyStickySessionTtlMs` (minimum 30s)

**Clearing** (on failure):
- Called in catch blocks before retry/respond decisions
- Also called on lease timeout (so the session won't keep preferring a saturated channel)
- `clearSurfaceStickyChannel`: calls `proxyChannelCoordinator.clearStickyChannel(key, channel.id)`. Only clears if the channel ID matches

**Channel selection influence** (first attempt only):
- If sticky binding exists and channel is not in exclude list: use `selectPreferredChannel`
- If preferred channel unavailable after route refresh: clear the binding

### 7. OAuth Token Refresh Recovery

```go
func trySurfaceOauthRefreshRecovery(ctx, selected, siteUrl, buildRequest, dispatchRequest) *RecoverResult
```

Triggered in endpointFlow's `tryRecover` callback when the response status is 401 or 403 AND the selected account has OAuth credentials.

Flow:
1. Calls `refreshOauthAccessTokenSingleflight(accountId)` -- deduplicated concurrent refresh
2. Updates `selected.tokenValue` with the fresh access token
3. Updates `selected.account.accessToken` and `selected.account.extraConfig`
4. Rebuilds the request with the fresh token and dispatches it
5. If the refreshed request succeeds: returns the recovered upstream response
6. If the refreshed request fails: mutates the original context (`ctx.request`, `ctx.response`, `ctx.rawErrText`) so the original endpoint's error processing continues with the post-refresh failure state

The OAuth recovery is called BEFORE `endpointStrategy.tryRecover` in the endpoint flow. This ensures OAuth is handled as a first-class recovery before any protocol-level recovery.

### 8. Debug Trace System

A comprehensive debug tracing subsystem records every decision and attempt. The surface handler manages a debug trace object throughout the request lifecycle.

**Lifecycle methods**:
- `startSurfaceProxyDebugTrace`: initializes trace with client context, requested model, downstream API key, request headers/body
- `safeUpdateSurfaceProxyDebugSelection`: records channel selection decision (sticky hit, selected channel, route, account, site, platform)
- `safeUpdateSurfaceProxyDebugCandidates`: records endpoint candidates + runtime state snapshot + decision summary
- `reserveSurfaceProxyDebugAttemptBase`: reserves attempt index slots
- `safeInsertSurfaceProxyDebugAttempt`: inserts per-attempt detail (request/response headers/body, status, error text, recovery flag, downgrade decision, memory write result)
- `safeUpdateSurfaceProxyDebugAttempt`: updates an attempt (e.g. marking downgrade after the fact)
- `captureSurfaceProxyDebugSuccessResponseBody`: captures response body, with size limits
- `buildSurfaceProxyDebugResponseHeaders`: extracts response headers
- `parseSurfaceProxyDebugTextPayload`: parses text payload for debug display
- `safeFinalizeSurfaceProxyDebugTrace`: finalizes with final status, HTTP status, upstream path, response headers/body

All methods are "safe" -- they catch errors internally so debug trace failures never affect the proxy flow.

### 9. Proxy Usage Resolution with Self-Log Fallback

On success, `recordSurfaceSuccess` calls `resolveProxyUsageWithSelfLogFallback` to determine actual token usage:

- If upstream reports usage (`upstreamUsagePresent` or tokens > 0): source is 'upstream'
- If upstream reports no usage: attempts to recover from self-logged data (the proxy's own tracking of what was sent/received)
- Fallback returns `SurfaceResolvedUsageSummary` with `usageSource` field: 'upstream', 'self-log', or 'unknown'
- If usage source is 'unknown', the proxy_log records null tokens (not zero)

### 10. SSE Stream Handling

SSE streaming has two distinct code paths based on upstream content-type:

#### 10a. Standard SSE (`text/event-stream`)
- `reply.hijack()` the Fastify response
- Set SSE headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache, no-transform`, `Connection: keep-alive`, `X-Accel-Buffering: no`
- For Gemini CLI sites: wrap reader with `createGeminiCliStreamReader`
- Decode chunks through `TextDecoder`, accumulate `rawText`
- Stream through `openAiChatTransformer.proxyStream.createSession` which:
  - Calls `onParsedPayload` for usage extraction (merges incremental usage)
  - Writes transformed lines to downstream via `writeLines`
- CRITICAL: once SSE response headers are sent and chunks are being streamed downstream, the proxy CANNOT switch channels or return an HTTP error response. Stream-level failures must be handled in-band by the stream session.
- On stream failure: `failureToolkit.recordStreamFailure` (writes to `tokenRouter.recordFailure` + proxy_log), if stream hasn't started hijacking, can still return a 502 error response.

#### 10b. Non-SSE Response in Stream Mode
When upstream returns non-`text/event-stream` content type for a streaming request:
- Read the full response body as text
- **Responses SSE text detection**: if `looksLikeResponsesSseText(fallbackText)`, run through `createSingleChunkStreamReader` and process as SSE
- **JSON parse attempt**: try to parse as JSON, if fails keep as raw string
- **Gemini CLI unwrap**: if site platform is 'gemini-cli', call `unwrapGeminiCliPayload`
- **Failure detection**: call `detectProxyFailure({rawText, usage})` on the parsed data
- If failure detected: handle via `handleDetectedFailure` -> retry or respond
- If no failure: `streamSession.consumeUpstreamFinalPayload(fallbackData, fallbackText, streamResponse)` to emit as single SSE event
- Usage extraction: `parseProxyUsage(fallbackData)` + `hasProxyUsagePayload(fallbackData)`

#### 10c. Stream Session Management
- `onParsedPayload`: called for each parsed SSE chunk, extracts and merges usage
- `consumeUpstreamFinalPayload`: for non-streaming responses that need to be emitted as SSE, converts the final object into SSE format
- `recordStreamSuccess`: logs success AFTER stream completes (latency measured from request start)
- Usage is accumulated incrementally during streaming, then recorded at stream end

### 11. Service Tier Policy

`applyOpenAiServiceTierPolicy` is called during request building for `responses` endpoint requests only. It checks configurable `openAiServiceTierRules` against:
- `requestedModel`: the model the downstream asked for
- `actualModel`: the model the channel will actually use
- `sitePlatform`: the upstream platform
- `accountType`: the account's OAuth plan type

If blocked: throws `SiteApiEndpointRequestError` with `serviceTierBlocked = true`. This is caught in the chatSurface catch block and returns immediately with 400 -- no retry, no failover.

### 12. Claude count_tokens Surface

`handleClaudeCountTokensSurfaceRequest` is a complete second surface handler for `POST /v1/messages/count_tokens`. It follows the same pattern as chatSurface but simplified:

1. Validates request body is a JSON object with `model` field
2. Model allowlist check
3. Client context detection
4. Channel selection loop (same sticky/tester/retry logic, with `requestKind: 'claude-count-tokens'` in endpoint resolution)
5. If no endpoint candidates available: 501 "not implemented for this upstream"
6. Lease acquisition
7. Single endpoint execution with `runWithSiteApiEndpointPool`:
   - Build upstream request via `buildClaudeCountTokensUpstreamRequest`
   - Dispatch, with OAuth recovery on 401/403
   - Debug trace recording per attempt
   - On failure: throw `SiteApiEndpointRequestError`
8. On success: `tokenRouter.recordSuccess`, bind sticky channel, log, finalize debug trace
9. On failure: same three-category catch block (service tier, endpoint failure, execution error)

### 13. Upstream Endpoint Runtime Memory

`recordUpstreamEndpointFailure` and `recordUpstreamEndpointSuccess` track per-endpoint success/failure history:
- Keyed by `{siteId, modelName, downstreamFormat, requestedModelHint, requestCapabilities}`
- Failures record: endpoint type, HTTP status, error text
- This state is captured in debug trace snapshots and may influence future endpoint candidate ordering

### 14. Additional Edge Case Behaviors

**Web Search Simulation Early Return**:
- `maybeHandleWebSearchOnlySimulation` for Claude downstream format only
- If the request body indicates a web-search-only simulation, handles it inline and returns early
- Bypasses all channel selection, lease, execution, and logging

**Conversation File Input Handling**:
- `resolveOpenAiBodyInputFiles`: resolves file references in the OpenAI body for authorized resource owners
- `ProxyInputFileResolutionError` thrown on failure: caught before channel selection, returns error immediately
- `summarizeConversationFileInputsInOpenAiBody`: detects document files for endpoint compatibility decisions
- `hasNonImageFileInput` flag affects endpoint candidate resolution

**Claude Continuation-Aware Responses**:
- When downstream is Claude and `shouldPreferResponsesForAnthropicContinuation(claudeOriginalBody)` returns true
- Sets `wantsContinuationAwareResponses = true`
- This flag is passed into endpoint candidate resolution, potentially preferring `responses` endpoint for Claude continuation scenarios

**Responses Compact Force-Stream**:
- `shouldForceResponsesUpstreamStream({sitePlatform, isCompactRequest})` determines whether to force streaming for responses endpoint
- Based on site platform capabilities

**Codex Session Cache Key**:
- Derived from downstream body:
  - For Claude format: from `body.metadata.user_id` -> `"{model}:claude:{userId}"`
  - For responses format: from `body.prompt_cache_key` -> `"{model}:responses:{key}"`
  - Fallback: from proxy auth token -> `"{model}:proxy:{token}"`
- Passed into upstream request building for codex session caching

**Forced Channel Unavailable Message**:
- When tester forces a channel that is unavailable: specific Chinese error message
- When normal channel selection exhausted: generic "No available channels after retries"

**All Channels Exhausted**:
- `reportProxyAllFailed` alert is triggered BEFORE returning error to client
- Alert includes model name and reason

**Stream Hijacked Cannot Retry**:
- Once SSE streaming starts (headers sent + chunks flowing), the proxy cannot switch channels mid-flight
- Stream errors after hijack are recorded but HTTP response has already started -- client receives incomplete stream

**Lease Keepalive During Long Streams**:
- `keepaliveTimer = setInterval(touch, keepaliveMs)` extends the lease TTL while a stream is active
- Prevents lease expiry during multi-minute streaming responses
- Both expiry timer and keepalive timer use `unref()` to not block Node.js event loop exit

**First-Byte Timeout**:
- Config: `config.proxyFirstByteTimeoutSec` (converted to ms, min 0)
- Uses `fetchWithObservedFirstByte` which wraps the fetch with an AbortController that fires after the timeout
- Timeout response is detected by `isObservedFirstByteTimeoutResponse`
- In endpoint flow: timeout triggers cross-protocol fallback to next endpoint (unless disabled)

**OAuth Quota Headers**:
- On success: `recordOauthQuotaHeadersSnapshot` captures upstream response headers related to OAuth quotas
- On failure: `recordOauthQuotaResetHint` records quota reset hints from error responses

**Token Expired Alert**:
- If `isTokenExpiredError({status, message})`: fires `reportTokenExpired` alert with account details

### 15. DefaultProxyConductor (Alternative Retry Model)

`DefaultProxyConductor` provides an action-based retry model distinct from the chatSurface flow. It uses `AttemptFailure.action` values rather than HTTP status classification:

```
AttemptFailureAction: 'retry_same_channel' | 'refresh_auth' | 'failover' | 'terminal' | 'stop'
```

Flow:
1. `selectChannel` -> if null, return `{ok: false, reason: 'no_channel'}`
2. `while (selected != null)`:
   a. Call `input.attempt(selected, attemptIndex, excludeChannelIds)`
   b. If `result.ok`: call `recordSuccessfulAttempt`, return success
   c. Call `recordFailedAttempt`
   d. `action = failureActionOf(result)`
   e. If `isTerminalFailure(action)`: call `onTerminalFailure`, return terminal
   f. If `shouldRetrySameChannel(action)`: `continue` (same channel, no exclusion)
   g. If `shouldRefreshAuth(action)` and deps has refreshAuth: call `deps.refreshAuth`, if refreshed `continue`
   h. If `shouldFailover(action)`: push channel to exclude list, call `deps.selectNextChannel`
   i. If next channel is null: return failed

This conductor is dependency-injected and may be used by non-surface flows. The chatSurface does NOT use this conductor -- it has its own retry loop.

The conductor also has `previewSelectedChannel(model, downstreamPolicy?)` which returns the selected channel without executing any request.

### 16. proxy_log Schema

Each attempt writes its own proxy_log row. The `status` field is either `'success'` or `'failed'` -- there is no `'retried'` status. The `retryCount` field tracks the attempt number (0-based).

| Column | Type | Description |
|---|---|---|
| `routeId` | int? | Channel route ID |
| `channelId` | int? | Channel ID |
| `accountId` | int? | Account ID |
| `downstreamApiKeyId` | int? | Downstream API key ID |
| `modelRequested` | text | Model requested by downstream |
| `modelActual` | text? | Actual model used |
| `status` | text | 'success' or 'failed' |
| `httpStatus` | int | HTTP status (0 for network errors) |
| `isStream` | bool? | Whether response was streamed |
| `firstByteLatencyMs` | int? | Time to first byte |
| `latencyMs` | int | Total latency |
| `promptTokens` | int? | Prompt token count |
| `completionTokens` | int? | Completion token count |
| `totalTokens` | int? | Total token count |
| `estimatedCost` | float | Estimated cost |
| `billingDetails` | json? | Billing metadata |
| `clientFamily` | text? | Detected client kind |
| `clientAppId` | text? | Client app ID |
| `clientAppName` | text? | Client app name |
| `clientConfidence` | text? | Detection confidence ('exact'/'heuristic') |
| `errorMessage` | text? | Composed error message (includes clientKind/sessionId context) |
| `retryCount` | int | Attempt number (0-based) |
| `createdAt` | timestamp | UTC timestamp |

### 17. Session Lease SQL Schema (for persistence)

In the TS implementation, sticky bindings and channel runtime state are in-memory Maps. For the Go implementation that may restart, add optional persistence:

```sql
-- Sticky session bindings (with TTL expiration)
CREATE TABLE IF NOT EXISTS proxy_sticky_bindings (
    session_key    TEXT PRIMARY KEY,
    channel_id     INTEGER NOT NULL,
    expires_at_ms  BIGINT NOT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Active channel leases (for crash recovery -- leases auto-expire via TTL)
CREATE TABLE IF NOT EXISTS proxy_channel_leases (
    lease_id       INTEGER PRIMARY KEY,
    channel_id     INTEGER NOT NULL,
    session_key    TEXT,
    acquired_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at     TIMESTAMP NOT NULL,  -- acquired_at + leaseTtlMs
    released       BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_proxy_sticky_bindings_expires
    ON proxy_sticky_bindings(expires_at_ms);

CREATE INDEX IF NOT EXISTS idx_proxy_channel_leases_channel
    ON proxy_channel_leases(channel_id, released, expires_at);
```

CRITICAL: The lease mechanism should work correctly without persistence (pure in-memory is the default TS behavior). Persistence is only needed if the Go process may restart and you want lease continuity across restarts. Even with persistence, leases should be auto-cleaned by TTL expiry.

## Acceptance Criteria

- [ ] Channel selection: sticky session preference, tester forced channel, normal selection, route refresh on empty
- [ ] Endpoint flow: for-loop over candidates with first-byte timeout, recovery, abort, downgrade
- [ ] Session lease: only activated when stickySessionKey present; noop lease otherwise
- [ ] Session lease: concurrency limit, queue wait, TTL expiry, keepalive extension during streams
- [ ] Sticky session: key construction, bind on success, clear on failure, TTL expiry
- [ ] 4 CLI profiles correctly detected: codex, claude_code, gemini_cli, generic (in that priority order)
- [ ] shouldRetryProxyRequest: correct classification matrix for all HTTP status codes + text patterns
- [ ] detectProxyFailure: keyword matching + empty content detection (content-based only)
- [ ] Service tier policy: immediate fatal 400, no retry
- [ ] OAuth refresh recovery: singleflight dedup, token update, request rebuild, context mutation on failure
- [ ] SSE stream: hijack mode for text/event-stream; body parse + detectFailure for non-SSE; stream session usage accumulation
- [ ] Claude count_tokens surface: full channel selection + lease + execution loop
- [ ] Proxy log: status='success'/'failed' (not 'retried'), retryCount tracks attempt number
- [ ] Debug trace: selection, candidates, per-attempt details, finalize (all safe-wrapped)
- [ ] Web search simulation: early return, bypasses all proxy logic
- [ ] Input file resolution: error before channel selection
- [ ] All channels exhausted: reportProxyAllFailed before returning error
- [ ] Stream hijacked: cannot retry across channels mid-stream

## Test Plan

| File | Content |
|---|---|
| `proxy/channel_selection_test.go` | Tester forced channel, sticky preference, route refresh, exclude list |
| `proxy/endpoint_flow_test.go` | Candidate iteration, first-byte timeout, recovery, abort, downgrade |
| `proxy/session_test.go` | Lease acquire/release/timeout, keepalive, queue drain, sticky bind/clear/expire |
| `proxy/failure_judge_test.go` | detectProxyFailure: keyword match, empty content, SSE parsing |
| `proxy/retry_policy_test.go` | shouldRetryProxyRequest: all status codes, text pattern matching |
| `proxy/profiles_test.go` | 4 profile detection with real headers/body fixtures |
| `proxy/surface_test.go` | Full chatSurface flow: channel retry loop, lease, SSE, catch blocks |

## Edge Cases

### Channel Level
- All channels exhausted: `reportProxyAllFailed` -> return 503 with "No available channels" or forced-channel-specific message
- Tester forced channel unavailable: specific "指定通道 #N 当前不可用" message, no retry
- Tester forced channel in retry (retryCount > 0): returns null immediately
- Route refresh on empty selection: fires once per request on first attempt only

### Session/Lease Level
- Session lease wait timeout: return 503 "Channel busy", optionally retry another channel
- Session lease wait timeout + no more retries: return 503 to client
- No sticky session key: lease is noop (always acquired, unlimited concurrency)
- Non-session-scoped channel: no sticky binding, noop lease
- Sticky binding expired: treated as no binding, cleared on read
- Sticky binding mismatch on clear: only clears if channelId matches
- Lease keepalive during long stream: touch() extends TTL via setInterval
- Lease TTL minimum enforced at 5000ms, keepalive minimum at 1000ms

### Endpoint Level
- No endpoint candidates: endpointFlow returns 502; count_tokens returns 501 "not implemented"
- First-byte timeout + not last endpoint + cross-protocol fallback enabled: continue to next
- First-byte timeout + disableCrossProtocolFallback: stop iteration
- OAuth recovery success: return recovered response
- OAuth recovery failure: mutate original context, original error processing continues
- shouldAbortRemainingEndpoints (429/rate-limit/quota/bad-gateway patterns): immediate break
- shouldDowngrade (protocol switch): promote next endpoint candidate, continue
- All endpoints exhausted: throw SiteApiEndpointRequestError, caught by chatSurface

### Failure Classification
- Service tier blocked: immediate 400, no retry, no channel switch
- 401/403 with OAuth: try OAuth refresh recovery first; if recovery succeeds, success; if fails, since 401/403 are retryable in shouldRetryProxyRequest, may retry channel
- 400/404/422 with retryable text pattern (e.g. "please use /v1/responses"): treated as retryable
- 400/404/422 with non-retryable text pattern (e.g. "invalid request body"): terminal
- detectProxyFailure on non-stream: content keyword match or empty content -> handled as upstream failure
- detectProxyFailure on non-SSE stream response: same path, but with parsed usage from stream body
- Execution error (network failure): always retryable if retryCount < maxRetries

### Stream Level
- SSE hijacked, stream failure mid-flight: cannot return HTTP error, client receives incomplete stream
- SSE hijacked NOT yet started: can return 502 error response
- Non-SSE content-type in stream mode: full body read -> parse -> detectFailure -> consumeUpstreamFinalPayload
- Gemini CLI stream: wrapped with `createGeminiCliStreamReader` for payload transformation
- Gemini CLI non-SSE: `unwrapGeminiCliPayload` applied before failure detection

### Early Exits
- Web search simulation handled (Claude format): return early, no proxy logic
- Input file resolution failure (ProxyInputFileResolutionError): return error before channel selection
- Model not allowed for downstream key: return before any proxy work
- Request body transformation error: return parsed error envelope

### Conductor Level
- previewSelectedChannel: returns channel without executing
- No channel available at start: return `{ok: false, reason: 'no_channel'}`
- retry_same_channel action: same channel, does not add to exclude list
- refresh_auth action + no refreshAuth dep: treated as failover
