# P8 Cross-Reference Review: ProxyCore Spec vs TypeScript Source

**Date**: 2026-07-04
**Spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p8-proxy-core.md`
**TS Sources Reviewed**: DefaultProxyConductor.ts, endpointFlow.ts, channelSelection.ts, chatSurface.ts, sharedSurface.ts, registry.ts (CLI profiles), proxyChannelCoordinator.ts, proxyChannelRetry.ts, proxyFailureJudge.ts, proxyRetryPolicy.ts, retryPolicy.ts (conductor)

---

## Accuracy Issues

### A1. CLI Profile count is 4, not 5
**Spec claim** (lines 91-97): "5 个 CLI profile" listing Codex, Claude, Gemini CLI, Generic, Claude Code as separate profiles.
**TS reality**: `cliProfiles/registry.ts` defines exactly 4 profiles:
- `generic` (genericCliProfile)
- `codex` (codexCliProfile)
- `claude_code` (claudeCodeCliProfile)
- `gemini_cli` (geminiCliProfile)

There is no separate "Claude" profile. The `orderedProfiles` detection array is `[claudeCodeCliProfile, codexCliProfile, geminiCliProfile]` with generic as the unconditional fallback. The spec's "Claude" profile (anthropic-version, x-api-key) does not exist as a standalone profile -- that behavior is presumably folded into `claude_code`.

### A2. `ExecuteEndpointFlow` is endpoint-level, not channel-level
**Spec claim** (lines 40-77): Describes ExecuteEndpointFlow as "select channel -> execute -> record -> retry/downgrade" at the channel level.
**TS reality**: `executeEndpointFlow` in `endpointFlow.ts` iterates over `endpointCandidates` (multiple endpoints per single channel/site). It does not select channels, does not record to tokenRouter, and does not make retry/downgrade decisions at the channel level. Those concerns live in `chatSurface.ts` (channel retry loop) and `sharedSurface.ts` (failure toolkit).

The spec conflates channel-level retry orchestration (in chatSurface.ts's `while (retryCount <= maxRetries)` loop) with endpoint-level iteration (in endpointFlow.ts's `for` loop over endpointCandidates). These are two distinct nested loops in TS.

### A3. Retry classification is not a single `FailureJudge.Classify`
**Spec claim** (lines 99-108): "FailureJudge.Classify(error, response)" with a single classifier returning retryable/downgrade/fatal.
**TS reality**: Failure classification is split across three independent concerns:
1. **`detectProxyFailure`** (proxyFailureJudge.ts) -- content-based detection only: error keyword matching + empty content check. Does NOT look at HTTP status codes. Only called after a response body is received.
2. **`shouldRetryProxyRequest`** (proxyRetryPolicy.ts) -- HTTP status + error text pattern matching: `>=500`, 408/409/425/429, 401/403, model-unsupported patterns all retryable; 400/404/422 non-retryable; plus text pattern matching against RETRYABLE_CHANNEL_LOCAL_PATTERNS and NON_RETRYABLE_REQUEST_PATTERNS.
3. **`shouldDowngrade`** (endpoint strategy callback) -- decides whether to try a different endpoint protocol within the same site, NOT a different channel.

These do not form a single "Classify" function. The spec also mischaracterizes "downgrade" -- it is not a channel-level failover to another channel, but an endpoint-level protocol switch (e.g., chat/completions -> responses) within the same site.

### A4. Max retry attempts is config-driven, not hardcoded to 3
**Spec claim** (line 113, line 69): "最多重试 PROXY_MAX_CHANNEL_ATTEMPTS (3) 次".
**TS reality**: `proxyChannelRetry.ts` defines `getProxyMaxChannelAttempts()` which reads `config.proxyMaxChannelAttempts` with a default of **1** (when config value is 0 or missing). The value 3 is a reasonable deployment configuration but not the code default. `getProxyMaxChannelRetries()` returns `max(0, attempts - 1)`, meaning default max retries is 0 (only the initial attempt).

### A5. Session lease concurrency limit is conditional and config-driven
**Spec claim** (lines 80-88): Hardcodes `concurrencyLimit = 2`, `queueWaitMs = 1500`, `leaseTTLMs = 90000`, `keepaliveMs = 15000`.
**TS reality**: All values read from config:
- `proxySessionChannelConcurrencyLimit` (config-driven, spec's "2" is a deployment default)
- `proxySessionChannelQueueWaitMs` (config-driven, min 0)
- `proxySessionChannelLeaseTtlMs` (config-driven, **enforced minimum 5000**)
- `proxySessionChannelLeaseKeepaliveMs` (config-driven, **enforced minimum 1000**)

The spec's 90000ms leaseTTL does not match the TS minimum of 5000. If the intent is that these are "recommended defaults" rather than hardcoded, the spec should say so explicitly.

### A6. Session lease is conditional on stickySessionKey
**Spec claim** (lines 43-46): "Session Lease (如启用)" with `SessionLeaseManager.Acquire(sessionID, selectedChannel)`.
**TS reality**: `sharedSurface.ts` line 511-513: `acquireSurfaceChannelLease` passes `channelId: input.stickySessionKey ? input.selected.channel.id : 0`. When `stickySessionKey` is null/empty, channelId=0 which always returns an immediate noop lease (`acquireChannelLease` returns early for channelId <= 0). The lease mechanism is only active for requests that have a stable downstream session key. Requests without one keep pre-sticky-session parallel behavior. The spec does not capture this critical gating condition.

### A7. Lease is acquired AFTER channel selection, not before request building
**Spec claim** (lines 40-47): Flow order is "Channel Selection -> Session Lease -> Request Building".
**TS reality**: In `chatSurface.ts`, the actual order is:
1. Select channel (line 243)
2. Resolve endpoint candidates (line 285-303)
3. Write debug trace selection + candidates (lines 268-327)
4. Acquire session lease (line 511) -- just before execution
5. Execute within site API endpoint pool (line 550)

Request building happens inside the execution closure (lines 339-391), after lease acquisition but inside the try block. The spec's ordering is misleading -- endpoint resolution (a significant step) occurs between channel selection and lease acquisition.

### A8. DefaultProxyConductor uses an action-based retry model, not failure classification
**Spec claim**: Implies channel retry is driven by FailureJudge classification.
**TS reality**: `DefaultProxyConductor.ts` uses an entirely different retry model via `retryPolicy.ts`:
- `failureActionOf(result)` reads `result.action` from the attempt
- Actions: `terminal`, `retry_same_channel`, `refresh_auth`, `failover`
- `isTerminalFailure` -> return immediately
- `shouldRetrySameChannel` -> retry without changing channel
- `shouldRefreshAuth` -> OAuth refresh, then retry or failover
- `shouldFailover` -> exclude channel, select next

The `chatSurface.ts` uses yet another retry model (shouldRetryProxyRequest + canRetryChannelSelection). Neither uses a single "Classify" function as described in the spec. The spec should acknowledge both retry paths and their differences.

---

## Missing Details

### M1. Sticky session binding mechanism
The TS implements a full sticky session system in `proxyChannelCoordinator.ts`:
- `buildStickySessionKey(clientKind, sessionId, requestedModel, downstreamPath, downstreamApiKeyId)` -- constructs a deterministic session key
- `bindStickyChannel(key, channelId, accountIdentity)` -- binds a session to a channel with TTL expiry
- `getStickyChannelId(key)` -- retrieves preferred channel for a session
- `clearStickyChannel(key, channelId)` -- unbinds on failure

None of this appears in the spec. The sticky session influences channel selection (first attempt prefers the bound channel) and lease enforcement (only sticky sessions get concurrency-limited leases).

### M2. Proxy debug trace system
The TS has a comprehensive debug tracing system spanning `proxyDebugTraceRuntime.ts` and used extensively in `chatSurface.ts`:
- `startSurfaceProxyDebugTrace` / `safeFinalizeSurfaceProxyDebugTrace`
- `reserveSurfaceProxyDebugAttemptBase` / `safeInsertSurfaceProxyDebugAttempt` / `safeUpdateSurfaceProxyDebugAttempt`
- `safeUpdateSurfaceProxyDebugSelection` / `safeUpdateSurfaceProxyDebugCandidates`
- `captureSurfaceProxyDebugSuccessResponseBody` / `buildSurfaceProxyDebugResponseHeaders` / `parseSurfaceProxyDebugTextPayload`

This records: channel selection decisions, endpoint candidates with runtime state, per-attempt request/response details, downgrade decisions, recovery events, final status. The spec only mentions "写 proxy_debug_trace (如启用)" with no structural detail.

### M3. OAuth token refresh recovery
`sharedSurface.ts` `trySurfaceOauthRefreshRecovery` (lines 278-326):
- Detects 401/403 on OAuth-protected accounts
- Calls `refreshOauthAccessTokenSingleflight` (deduplicated refresh)
- Rebuilds request with fresh token
- Retries the same endpoint
- On failure, updates the original context's request/response/error text for downstream handling

The spec does not mention OAuth recovery at all, though it is a first-class recovery path in the endpoint flow.

### M4. Client context detection
`chatSurface.ts` calls `detectDownstreamClientContext` (line 132) to identify the downstream client (codex, claude-code, gemini-cli, etc.) from headers and body. This feeds into:
- Session key construction
- Debug trace metadata
- Proxy log client info
- The spec does not mention client detection.

### M5. Site API endpoint pool
`chatSurface.ts` wraps execution in `runWithSiteApiEndpointPool(selected.site, async (target) => {...})`. This provides multi-endpoint site API failover -- if a site has multiple API base URLs, the pool iterates through them. The spec does not mention this mechanism.

### M6. Service tier policy enforcement
`chatSurface.ts` lines 346-363: `applyOpenAiServiceTierPolicy` checks whether the requested model + account tier combination is allowed. If blocked (`serviceTierBlocked`), it throws a `SiteApiEndpointRequestError` that is caught as an immediate fatal error (no retry). The spec does not mention service tier blocking.

### M7. Claude count_tokens endpoint
`chatSurface.ts` exports `handleClaudeCountTokensSurfaceRequest` (lines 1097-1512), a complete second surface handler for the `/v1/messages/count_tokens` endpoint. It has its own:
- Channel selection loop with retry
- Session lease acquisition
- Endpoint candidate resolution (with `requestKind: 'claude-count-tokens'`)
- OAuth refresh recovery
- Debug trace recording

The spec does not mention this endpoint at all.

### M8. Upstream endpoint runtime memory
`chatSurface.ts` calls `recordUpstreamEndpointFailure` and `recordUpstreamEndpointSuccess` from `upstreamEndpointRuntimeMemory.ts`. This tracks per-endpoint success/failure history to influence future endpoint candidate ordering. The spec does not mention endpoint runtime state.

### M9. Cross-protocol fallback and endpoint downgrade
`endpointFlow.ts` has two distinct continuation mechanisms within the endpoint loop:
- **First byte timeout** (lines 169-183): if not last endpoint and `disableCrossProtocolFallback` is false, continues to next endpoint
- **Downgrade** (lines 241-248): if `shouldDowngrade` callback returns true and not last endpoint, calls `onDowngrade` hook and continues to next endpoint
- **Abort** (lines 231-240): if `shouldAbortRemainingEndpoints` returns true, breaks immediately

The spec conflates these into a single "downgrade" concept and does not distinguish between them.

### M10. Tester forced channel mechanism
`channelSelection.ts` implements a tester override:
- `x-metapi-tester-request: 1` header + loopback client IP enables tester mode
- `x-metapi-tester-forced-channel-id` header forces a specific channel
- Forced channels do not retry (retryCount > 0 returns null)
- Uses `selectPreferredChannel` instead of `selectChannel`/`selectNextChannel`

The spec does not mention forced channels.

### M11. Route refresh on empty channel selection
`channelSelection.ts` lines 106-116, 152-155: on the first attempt only, if channel selection returns null, the code triggers `routeRefreshWorkflow.refreshModelsAndRebuildRoutes()` and retries selection. The spec does not mention route refresh as part of channel selection.

### M12. SSE stream handling detail
The TS has extensive non-SSE response handling for stream mode:
- Responses SSE text detection (`looksLikeResponsesSseText`)
- Gemini CLI payload unwrapping (`unwrapGeminiCliPayload`)
- Single-chunk stream reader (`createSingleChunkStreamReader`)
- Stream session management with `onParsedPayload` usage tracking
- `recordStreamFailure` for stream-level failures (different from normal failure handling)
- The comment on line 865-868: "Once SSE has been hijacked and streamed downstream, we can no longer safely fall back to an HTTP error response or retry by switching the channel mid-flight"

The spec only mentions "SSE stream: 逐 chunk 读取, 应用 transformer" with no detail on these edge cases.

### M13. Proxy usage resolution with self-log fallback
`recordSurfaceSuccess` in sharedSurface.ts calls `resolveProxyUsageWithSelfLogFallback` to recover token usage from self-logged data when upstream does not report usage. The spec mentions "update billing counters" but does not describe the fallback mechanism.

### M14. Conversation file input handling
`chatSurface.ts` calls `resolveOpenAiBodyInputFiles` (file resolution for the resource owner) and `summarizeConversationFileInputsInOpenAiBody` (detects document files for endpoint compatibility decisions). These feed into `hasNonImageFileInput` and `wantsContinuationAwareResponses` which influence endpoint candidate resolution. Not mentioned in the spec.

### M15. Web search simulation
`chatSurface.ts` line 150-159: `maybeHandleWebSearchOnlySimulation` for Claude downstream format. If the request is a web-search-only simulation, it handles it inline and returns early. Not mentioned in the spec.

### M16. Responses compact force-stream
`chatSurface.ts` line 335-338: `shouldForceResponsesUpstreamStream` determines whether to force streaming for the responses endpoint based on site platform. Not mentioned in the spec.

### M17. The conductor's `previewSelectedChannel` method
`DefaultProxyConductor.ts` has a `previewSelectedChannel` method (lines 14-19) that allows previewing which channel would be selected without executing. Not mentioned in the spec.

### M18. Retry loop boundary condition
The TS retry loop uses `while (retryCount <= maxRetries)`, meaning up to `maxRetries + 1` total attempts. For the default `proxyMaxChannelAttempts = 1`, `maxRetries = 0`, so the loop runs exactly once. The spec says "最多重试 PROXY_MAX_CHANNEL_ATTEMPTS (3) 次" which is ambiguous -- does "3" mean 3 total attempts or 3 retries after the first?

---

## Edge Cases Not Covered

### E1. Stream hijacked -- cannot retry across channels
TS comment (chatSurface.ts lines 865-868): once SSE response headers are sent and chunks are being streamed, the proxy cannot switch channels or return an HTTP error. Stream-level failures must be handled in-band by the stream session. The spec does not address this constraint.

### E2. Non-SSE content type in stream mode
TS handles the case where the upstream returns a non-`text/event-stream` content type for a stream request (chatSurface.ts lines 644-802). This includes:
- Responses SSE text in a non-streaming response
- Gemini CLI payload unwrapping
- Detecting proxy failures from the non-streamed body
- The spec does not cover this edge case.

### E3. OAuth refresh failure during endpoint recovery
In `endpointFlow.ts` `tryRecover`, if OAuth refresh produces a non-ok response, the original context (request, response, rawErrText) is mutated for downstream error handling. The original endpoint's error processing continues. The spec does not describe this recovery-failure path.

### E4. Service tier blocked -- immediate fatal, no retry
When `serviceTierBlocked === true`, the code returns immediately with a 400 error (chatSurface.ts lines 1001-1014). It does not go through the normal retry decision flow. The spec does not mention this path.

### E5. No endpoint candidates for count_tokens
When `endpointCandidates.length === 0` in `handleClaudeCountTokensSurfaceRequest` (chatSurface.ts lines 1252-1269), it returns 501 if retries are exhausted. The spec does not mention this.

### E6. All channels exhausted
The spec mentions "所有 channel 尝试失败 -> 返回最终错误给客户端" as an edge case, but the TS also triggers `reportProxyAllFailed` alert before returning the error. The spec mentions alerting in the main flow but does not link it to this edge case.

### E7. Lease keepalive during long-running streams
The TS lease has a keepalive timer (`keepaliveTimer = setInterval(touch, keepaliveMs)`) that extends the lease while a stream is active. The spec does not discuss how the lease stays alive during streaming responses that may last minutes.

### E8. Claude continuation-aware responses selection
When downstream is Claude and the request body indicates a continuation (`shouldPreferResponsesForAnthropicContinuation`), the `wantsContinuationAwareResponses` flag influences endpoint candidate resolution. This is a subtle edge case for Claude's extended thinking/continuation feature. Not mentioned in the spec.

### E9. Web search simulation early return
When `maybeHandleWebSearchOnlySimulation` handles a request (returns true), the entire chat surface handler returns early without going through channel selection, lease, execution, or logging. The spec does not mention this early-exit path.

### E10. File resolution failure
When `resolveOpenAiBodyInputFiles` throws `ProxyInputFileResolutionError`, the handler returns the error immediately without any channel selection or retry. The spec does not address input file resolution failures.

---

## Incorrect Details

### I1. "Claude" as a separate CLI profile
Spec lines 94 claims Claude as a profile with `anthropic-version`, `x-api-key`. The TS has no separate "claude" profile. The `claude_code` profile handles Claude Code CLI requests. The spec should either remove "Claude" or clarify that Claude Code profiles handle anthropic-version headers.

### I2. Failure classification mapping
Spec lines 101-108 claim:
- "HTTP 4xx (非 429) -> 不重试 (fatal)"
- "HTTP 429 -> 重试 (retryable, 等待 Retry-After)"

TS `shouldRetryProxyRequest` actually treats:
- 401 and 403 as **retryable** (for OAuth token refresh)
- 400 and 404 and 422 as non-retryable **unless** the error text matches `RETRYABLE_CHANNEL_LOCAL_PATTERNS` (e.g., "invalid api key", "forbidden", "rate limit", "quota", "bad gateway")
- There is no explicit "Retry-After" header handling in the retry decision (the retry happens, but the wait is implicit in the loop)

The spec oversimplifies by dividing at 429 and incorrectly claims all non-429 4xx are fatal.

### I3. "Protocol conversion failure -> 降级尝试 (downgrade)"
Spec line 108 claims protocol conversion failure triggers downgrade. In TS, downgrade is determined by `shouldDowngrade` callback from the endpoint strategy -- it is not triggered by a specific "protocol conversion failure" classification. The `shouldDowngrade` logic lives in the transformer's endpoint strategy, not in any failure judge.

### I4. proxy_log status values
Spec line 133 says "首次 channel 失败, 重试成功 -> proxy_log.status = 'retried'". In TS, each attempt writes its own proxy log entry with status 'success' or 'failed'. There is no 'retried' status value. The retryCount field tracks the attempt number. The final successful attempt writes status='success' with the appropriate retryCount.

### I5. `SelectNextChannel(previousChannel)` signature
Spec line 69: "tokenRouter.SelectNextChannel(previousChannel)". The TS `tokenRouter.selectNextChannel` signature is `selectNextChannel(model, excludeChannelIds, downstreamPolicy)` -- it takes an array of excluded channel IDs, not a single "previous channel". The spec's parameter name is misleading.

### I6. Credential mode distinction for session-scoped channels
The TS `isSessionScopedChannel` checks `getCredentialModeFromExtraConfig === 'session' || hasOauthProvider`. Only session-credential accounts get sticky bindings and concurrency-limited leases. The spec's `SessionLeaseManager` has no concept of credential modes.

### I7. `recordSuccessfulAttempt` vs `tokenRouter.recordSuccess`
The spec (line 61) says "tokenRouter.RecordSuccess(channel)". In TS there are TWO success recording paths:
- `DefaultProxyConductor` calls `recordSuccessfulAttempt(this.deps, ...)` which is a dependency-injected hook
- `chatSurface` calls `tokenRouter.recordSuccess(...)` directly inside `recordSurfaceSuccess`

These are different call paths with different parameters. The spec should clarify which it refers to.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 8 |
| Missing Details | 18 |
| Edge Cases Not Covered | 10 |
| Incorrect Details | 7 |
| **Total Findings** | **43** |

**Verdict: NEEDS_REVISION**

The spec captures the high-level architecture correctly (channel selection, session lease, endpoint flow, retry) but misses substantial detail in every area. The most critical gaps are:

1. **CLI profiles**: Claims 5 profiles but TS has 4 -- remove the non-existent "Claude" profile.
2. **Retry classification**: Describes a single `FailureJudge.Classify` that does not exist. The TS has two separate retry mechanisms (conductor action-based + surface `shouldRetryProxyRequest`-based) and three separate concerns (content detection, status classification, endpoint downgrade). Rewrite this section to reflect the actual architecture.
3. **Sticky sessions**: Entirely absent. This is a fundamental mechanism that gates lease enforcement and influences channel selection. Must be added.
4. **ExecuteEndpointFlow scope**: Mischaracterized as channel-level when it is endpoint-level. Clarify the two nested loops: channel retry loop (chatSurface) and endpoint iteration loop (endpointFlow).
5. **Debug trace**: Mentioned in one sentence but the TS has a comprehensive tracing subsystem with 10+ functions. Either add structural detail or acknowledge it as out-of-scope with a forward reference.
6. **Config-driven values**: 4 hardcoded defaults should be marked as configurable with the actual TS minimums noted.
7. **Session lease gating**: Must document that leases only activate when `stickySessionKey` is present.
8. **Missing surfaces**: `handleClaudeCountTokensSurfaceRequest` is a complete second surface handler not described.
