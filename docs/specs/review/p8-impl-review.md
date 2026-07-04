# P8 ProxyCore Implementation Review

**Date**: 2026-07-04 | **Spec**: `docs/specs/p8-proxy-core.md` | **TS Ref**: `metapi/src/server/proxy-core/` | **Go Impl**: `metapi-go/proxy/`

## Overall Assessment

The Go implementation faithfully reproduces the core orchestration primitives from the TS reference. All 57 test functions pass (7 test files, 0 failures). The building blocks -- channel selection, endpoint iteration, session leases, retry policy, failure detection, CLI profiles -- are solid and spec-compliant. There are several config default deviations from the spec and a handful of correctness issues. The biggest gap is that the spec describes several surface-level orchestration pieces (chat surface loop, SSE streaming, count_tokens surface, debug trace, OAuth recovery, service tier policy) that are NOT yet implemented in Go -- these are described in the spec's functional sections but absent from the Go module.

## Test Status

```
go test ./proxy/... -count=1
ok      github.com/tokendancelab/metapi-go/proxy      0.928s    (57 test functions, all PASS)
?       github.com/tokendancelab/metapi-go/proxy/profiles       [no test files]
?       github.com/tokendancelab/metapi-go/proxy/types          [no test files]
```

Test file breakdown:

| File | Test Functions | Focus |
|---|---|---|
| `channel_selection_test.go` | 6 | Tester, sticky, normal, route refresh, exclude list, error propagation |
| `endpoint_flow_test.go` | 12 | Success, no-candidates, timeout, recovery, abort, downgrade, proxy URL |
| `session_test.go` | 10 | Key construction, bind/clear/expire, lease acquire/release/timeout, load snapshot, reset |
| `failure_judge_test.go` | 5 | Keyword matching, empty content, upstream output detection, combined scenarios |
| `retry_policy_test.go` | 8 | All status codes, model-unsupported, non-retryable, channel-local, abort, max attempts |
| `profiles_test.go` | 7 | Claude Code, Codex, Gemini CLI, generic fallback, priority ordering, client context |
| `surface_test.go` | 9 | Convert channel, upstream/detected/execution/stream failure handling, sticky helpers, lease |

## File-by-File Review

### 1. `channel_selection.go` -- PASS

Faithful reproduction of the TS `selectProxyChannelForAttempt` with all three paths:

- **Tester forced channel**: Correctly returns nil immediately when `retryCount > 0`. Builds the specific Chinese error message. Loopback IP check covers `127.0.0.1`, `::1`, `::ffff:127.0.0.1`.
- **Sticky session preference**: Correctly skips preferred channel in exclude list. Route refresh + retry on unavailability. Clears binding after refresh if still unavailable.
- **Normal selection**: `SelectChannel` on first attempt, `SelectNextChannel` on retries.
- **Route refresh on empty**: Guarded by `retryCount === 0` AND `!refreshedRoutes` flag. Refresh errors silently swallowed (matches TS behavior).

Minor note: `CanRetryChannelSelection` accepts `maxRetries` as a parameter rather than reading from config. This is good for testability but the caller must remember to pass `GetProxyMaxChannelRetries(GetProxyMaxChannelAttempts(cfg.ProxyMaxChannelAttempts))`.

### 2. `endpoint_flow.go` -- PASS (with issues)

The core for-loop structure matches TS. Correct iteration order, correct continuation matrix for timeout/recovery/abort/downgrade.

**ISSUE -- First-byte timeout is dead code in the current dispatch path**: `ExecuteEndpointFlow` calls `input.DispatchRequest(request, targetURL)` which is a plain `func(BuiltEndpointRequest, string) (*ExecutorDispatchResult, error)`. The `input.FirstByteTimeoutMs` field is accepted but never passed to the dispatch function. The `RuntimeExecutor.WithObservedFirstByte` method exists but is never wired into `ExecuteEndpointFlow`. This means `IsObservedFirstByteTimeout(response)` will never return true in production -- the timeout path can only be triggered by tests that return a synthetic status-0 response.

**ISSUE -- Post-recovery context mutation not re-read**: In the TS version, after recovery fails, the code explicitly does:
```ts
rawErrText = baseContext.rawErrText;  // re-read in case recovery mutated it
response = baseContext.response;
```
The Go version constructs `errText` using `baseCtx.RawErrText` and `response.Status` directly, which works if the `TryRecover` callback mutates `baseCtx` (since Go passes structs by value, the callback receives a copy -- but it receives a copy of `EndpointAttemptContext`, so mutations to the copy don't propagate). However, looking at how the spec describes OAuth recovery: "On OAuth recovery failure: the recovery function mutates the original context (request, response, rawErrText)" -- in Go, the `TryRecover` hook receives `EndpointAttemptContext` by value, so mutations to `baseCtx` within the callback won't be visible. The TS version achieves this through closure mutation of the outer `rawErrText`/`response` variables. This means OAuth recovery failure context mutation is not correctly implemented.

**Recommendation**: Either (a) pass `*EndpointAttemptContext` to `TryRecover`, or (b) have `TryRecover` return the mutated context in `RecoverResult`.

**Minor: Error dispatch fallback**: When `input.DispatchRequest` returns an error (non-nil), the code synthesizes a status-0 response. This status-0 is then treated identically to a first-byte timeout by `IsObservedFirstByteTimeout`. This conflates genuine dispatch errors (e.g., DNS resolution failure) with first-byte timeouts, potentially triggering unintended cross-protocol fallback.

### 3. `session.go` -- PASS

Comprehensive implementation of the dual-role `ProxyChannelCoordinator`:

- **Sticky session key construction**: Format matches spec exactly: `key:{apiKeyId}|{clientKind}|{downstreamPath}|{model}|{sessionId}`. Whitespace trimming, lowercasing, defaulting of empty fields.
- **Sticky bindings**: Bind only when enabled AND session-scoped. Get cleans up expired on read. Clear respects channelId matching.
- **Lease acquisition**: Noop when channelID <= 0 or not session-scoped or concurrency limit <= 0. Tracked leases when session-scoped and under limit.
- **Lease lifecycle**: Expiry timer resets on Touch(). Keepalive ticker extends via Touch(). Release drains queue and garbage-collects channel state. Double release is safe.
- **Queue management**: Cancelled waiters pruned. Timeout via `time.AfterFunc`. Queue drain on release promotes next waiter.

**ISSUE -- `drainQueueLocked` uses global concurrency limit, not per-channel**: The function reads `c.cfg.ProxySessionChannelConcurrencyLimit` directly, which is the global default. It should use the channel-specific limit (which depends on `extraConfig`/`oauthProvider` for that channel). Since `releaseLease` doesn't have access to the channel's scoping info, the drained lease might be created as a tracked lease for a channel that isn't session-scoped. In practice, only session-scoped channels get queues, so this may not cause runtime bugs, but the drain should use a noop lease for non-session-scoped channels.

**Minor: `createTrackedLease` goroutine leak potential**: The expiry timer and keepalive goroutines use `lease.doneCh` to exit, but `Release()` is the only closer. If the lease is garbage-collected without calling `Release()`, these goroutines leak. In the current usage pattern, all paths call `Release()`, but defensive programming would benefit from `runtime.SetFinalizer`.

### 4. `failure_judge.go` -- PASS

Content-based failure detection matches TS behavior:

- **Keyword matching**: Case-insensitive, empty keyword skipping, first match wins.
- **Empty content**: Only when `ProxyEmptyContentFailEnabled` AND `completionTokens <= 0` AND no upstream output detected.
- **Output detection**: JSON parse + SSE parse fallback. Checks choices, output arrays, content parts, tool calls, function calls, delta, text, refusal. Matches TS `hasCompletionContentFromPayload` coverage.
- **SSE parsing**: `pullSseDataEvents` extracts `data:` lines. `[DONE]` treated as empty. Non-JSON payloads count as output.

### 5. `retry_policy.go` -- PASS (with issues)

The retry decision matrix matches TS. All regex patterns are present.

**ISSUE -- `trimToLower` does not trim whitespace**: The TS `matchesAnyPattern` does `(rawMessage || '').trim()` before matching. The Go `trimToLower` only lowercases ASCII characters but performs no whitespace trimming. The function name is misleading. If `upstreamErrorText` has leading/trailing whitespace, patterns like `/^invalid\s+model/` would fail because `^` wouldn't match. In practice, error text from HTTP response bodies rarely has leading whitespace, but the mismatch exists.

**Fix**: Add `strings.TrimSpace` at the start of `isModelUnsupportedError` and `matchesAnyPattern` calls, or rename `trimToLower` and add trimming. Alternatively, change to:
```go
func isModelUnsupportedError(text string) bool {
    text = strings.TrimSpace(text)
    if text == "" {
        return false
    }
    return matchesAnyPattern(modelUnsupportedPatterns, strings.ToLower(text))
}
```

**ISSUE -- `ShouldRetryProxyRequest` is missing `Retry-After` header handling**: The spec states "There is no explicit Retry-After header handling in the retry decision -- the decision is binary retry/no-retry." This is noted as deliberate, not a bug.

**Note on regex flags**: TS patterns use `/pattern/i` (case-insensitive). Go patterns are case-sensitive but `trimToLower` lowercases input text before matching. This approach is correct for ASCII but differs for Unicode case folding. Not a practical concern for the patterns in use (all ASCII).

### 6. `profile.go` + `profiles/*.go` + `detect.go` -- PASS

All four profiles correctly implemented:
- **Claude Code** (`claude_code`): Path matching (`/v1/messages`, `/anthropic/v1/messages`, `/v1/messages/count_tokens`), header fingerprint (user-agent + anthropic-beta + anthropic-version + x-app:cli), body metadata.user_id session extraction. Regex for user ID format is correct.
- **Codex** (`codex`): Path matching (`/v1/responses`, `/v1/responses/*`, `/v1/chat/completions`), official client headers, openai-beta header, x-stainless-* headers, session/conversation headers, turn-state header. Confidence exact/heuristic split is correct.
- **Gemini CLI** (`gemini_cli`): Path matching (`/v1internal:generatecontent`, `:streamgeneratecontent`, `:counttokens`), body shape detection (model string, contents array, request.contents, request.model).
- **Generic**: Unconditional fallback.

Detection priority: claude_code -> codex -> gemini_cli -> generic (via `All()` registration order in `registry.go`). Matches spec.

Capabilities table matches spec exactly for all four profiles.

### 7. `surface.go` -- PASS

Provides the building blocks for surface handlers:
- `SurfaceFailureToolkit` with three failure handlers matching the spec's three-category classification: `HandleUpstreamFailure`, `HandleDetectedFailure`, `HandleExecutionError`.
- `SurfaceFailureResponse` with `Action: "retry"` or `Action: "respond"`.
- Sticky session surface helpers delegate to `ProxyChannelCoordinator`.
- `AcquireSurfaceChannelLease` correctly gates: channelId=0 (noop) when no sticky session key.
- `RecordStreamFailure` records both failure and proxy log.

**Missing**: The actual chat surface orchestration loop (spec section 5) and count_tokens surface (spec section 12) are not implemented. The spec describes an outer `while (retryCount <= maxRetries)` loop that wraps channel selection, endpoint flow, lease acquisition, and the three-category catch block. This orchestration is absent.

### 8. `conductor.go` -- PASS

Action-based retry model for non-surface flows. Matches the spec's `DefaultProxyConductor` specification:
- Action classification: `retry_same_channel`, `refresh_auth`, `failover`, `terminal`, `stop`.
- Loop structure: select -> attempt -> classify action -> retry/failover/terminal.
- `PreviewSelectedChannel` method for pre-flight channel inspection.

### 9. `executor.go` -- PASS

HTTP dispatch with `RuntimeExecutor`:
- `Dispatch`: Simple POST with header forwarding.
- `WithObservedFirstByte`: Creates a timeout context for first-byte observation. Returns status=0 marker on timeout.
- `IsObservedFirstByteTimeout`: Checks for status=0 marker.

**ISSUE -- SSE streaming not supported**: The executor reads entire bodies via `io.ReadAll`. For SSE streaming, the caller needs a body reader that streams chunks. The `ExecutorDispatchResult.BodyReader` field exists but is never populated. SSE handling (spec section 10) requires significant additional work.

## Config Default Deviations

| Config Key | Spec Default | Go Default | Impact |
|---|---|---|---|
| `proxyMaxChannelAttempts` | 1 | 3 | **Major**: Spec says "default maxRetries = 0, while-loop runs exactly once." Go default of 3 means up to 3 channel attempts per request by default, fundamentally changing the retry behavior. |
| `proxyStickySessionEnabled` | false | true | **Major**: Sticky sessions are opt-in per the spec. Go enables them unconditionally by default. |
| `proxyStickySessionTtlMs` | 0 (min 30000) | 1800000 (30 min) | **Moderate**: Spec says minimum 30s; Go default is 30 minutes. Much longer sticky bindings by default. |
| `proxySessionChannelConcurrencyLimit` | 0 (unlimited) | 2 | **Moderate**: Spec says 0 = unlimited (noop lease). Go defaults to concurrency limit of 2, which activates lease tracking even when the operator hasn't configured it. |
| `proxySessionChannelQueueWaitMs` | 0 | 1500 | **Moderate**: Spec says 0 = instant timeout. Go default 1.5s wait means requests queue by default. |
| `proxySessionChannelLeaseTtlMs` | 0 (min 5000) | 90000 | **Minor**: Longer default TTL (90s vs 5s minimum). |
| `proxySessionChannelLeaseKeepaliveMs` | 0 (min 1000) | 15000 | **Minor**: Longer keepalive interval (15s vs 1s minimum). |

These defaults appear to be production-oriented settings, but they diverge significantly from the spec's documented defaults. The spec explicitly states the rationale for `proxyMaxChannelAttempts = 1` (no retry by default) and `proxyStickySessionEnabled = false` (opt-in sticky sessions). If these are intentional production defaults, the spec should be updated.

## Missing Implementations (Spec Sections Not Covered)

The following spec sections describe functionality that is not present in the Go implementation:

| Spec Section | Feature | Status | Priority |
|---|---|---|---|
| 5 | Chat surface channel-level retry loop | **Missing** | P0 |
| 10 | SSE stream handling (hijack, stream session, Gemini CLI wrapping, non-SSE fallback) | **Missing** | P0 |
| 12 | Claude count_tokens surface | **Missing** | P1 |
| 8 | Debug trace system | **Missing** | P1 |
| 14 | Web search simulation early return | **Missing** | P2 |
| 11 | Service tier policy | **Missing** | P2 |
| 7 | OAuth refresh recovery (full implementation) | **Stubbed** (hook only) | P1 |
| 14 | Input file resolution | **Missing** | P2 |
| 14 | Conversation file input handling | **Missing** | P2 |
| 14 | Codex session cache key | **Missing** | P2 |
| 14 | Responses compact force-stream | **Missing** | P2 |
| 16 | proxy_log writing (schema defined, no writer) | **Stubbed** (interface only) | P1 |
| 17 | Session lease SQL persistence | **Missing** (in-memory only, matches TS default) | P3 |

The spec note on section 17 clarifies: "CRITICAL: The lease mechanism should work correctly without persistence (pure in-memory is the default TS behavior)." So the missing SQL persistence is acceptable -- the in-memory implementation is the baseline.

## Summary of Findings

### Correctness Issues

1. **`trimToLower` missing whitespace trim** (`retry_policy.go`): The function name implies trimming but only lowercases. Leading/trailing whitespace in error text could cause regex patterns (especially `^`-anchored) to not match. TS behavior trims first. **Fix**: Add `strings.TrimSpace` before matching.

2. **Post-recovery context mutation** (`endpoint_flow.go`): `TryRecover` receives `EndpointAttemptContext` by value. Spec says recovery failure should mutate the original context (request, response, rawErrText). **Fix**: Pass `*EndpointAttemptContext` or return mutated context in `RecoverResult`.

3. **First-byte timeout dead code** (`endpoint_flow.go`): `input.FirstByteTimeoutMs` is accepted but never passed to `DispatchRequest`. `RuntimeExecutor.WithObservedFirstByte` exists but is not wired in. **Fix**: Either pass timeout to dispatch or document as caller's responsibility.

4. **`drainQueueLocked` global limit** (`session.go`): Uses `c.cfg.ProxySessionChannelConcurrencyLimit` instead of channel-specific limit. **Fix**: Determine channel-specific scoping/concurrency in drain.

5. **Config defaults diverge from spec**: See table above. **Fix**: Either align defaults with spec or update spec to document production defaults.

### Test Coverage Gaps

- No test for `DefaultProxyConductor.Execute` (full action-based retry loop)
- No test for SSE stream handling (not implemented)
- No integration test stitching channel selection + endpoint flow + failure toolkit
- `profiles/` package has no dedicated test file (tests are in `profiles_test.go` in the proxy package)
- No test for `DetectProxyFailure` with actual SSE multi-event streams

### Architecture Notes

- The separation into `proxy/types/` package avoids import cycles between `proxy/` and `proxy/profiles/`. Clean design.
- The `TokenRouterInterface` and `RouteRefreshWorkflow` interfaces enable dependency injection for testing. Well done.
- The `SurfaceFailureToolkit` hooks (`ReportAllFailed`, `ReportTokenExpired`) are function-typed for DI. This follows TS patterns well.
- `runHook` helper catches panics in hook callbacks implicitly (Go doesn't have try/catch). Consider `defer/recover` for robustness in hook execution.

## Recommendations

1. **P0**: Fix `trimToLower` whitespace issue in `retry_policy.go`.
2. **P0**: Fix post-recovery context mutation in `endpoint_flow.go` (pointer or return value).
3. **P0**: Wire first-byte timeout into `ExecuteEndpointFlow` dispatch path.
4. **P1**: Align config defaults with spec OR update spec. The `proxyMaxChannelAttempts: 3` default is the most impactful deviation.
5. **P1**: Implement chat surface orchestration loop (spec section 5) -- this is the core proxy request handler.
6. **P1**: Implement SSE stream handling (spec section 10) -- required for production use.
7. **P2**: Implement count_tokens surface, debug trace, OAuth recovery, service tier policy.
8. **P2**: Add `DefaultProxyConductor.Execute` test.
9. **P3**: Add integration test for the full channel->endpoint->failure pipeline.
10. **P3**: Consider `recover()` in `runHook` for robustness.
