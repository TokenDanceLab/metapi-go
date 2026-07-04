# Error Handling & Propagation Audit

**Scope:** `D:/Code/TokenDance/metapi-go`
**Date:** 2026-07-04
**Files audited:** `proxy/failure_judge.go`, `proxy/retry_policy.go`, `proxy/executor.go`, `handler/proxy/upstream.go`, `handler/proxy/surface.go`, `handler/proxy/router.go`, `handler/proxy/messages.go`, `handler/admin/account_tokens.go`, `handler/admin/accounts.go`, `handler/admin/sites.go`, `handler/admin/events.go`, `routing/runtime_health.go`, `routing/ports.go`

---

## 1. Error Wrapping with %w

**Verdict:** GOOD -- consistent use of `%w` across internal packages. No `errors.Wrap` / `errors.Is` / `errors.As` / custom sentinel errors exist.

The codebase uses `fmt.Errorf` with `%w` uniformly when propagating errors from lower layers. The `%w` pattern is present in 60+ call sites across:
- `cmd/migrate/main.go` (SQLite/PG open, ping, tx, scan)
- `store/` (bootstrap, open, migrate, setting_store, switch)
- `proxy/executor.go` (dispatch)
- `routing/` (decision, router, selector, snapshot)
- `service/` (oauth, notify, site, account)
- `platform/` (base, newapi, site_proxy)
- `scheduler/usage_aggregation.go`
- `auth/downstream.go`

**Gap:** No custom error types or sentinel errors (`var ErrX = errors.New(...)`) are defined anywhere. Callers cannot use `errors.Is()` to distinguish between error categories (e.g., "retryable upstream error" vs "permanent config error"). The codebase relies entirely on string pattern matching to classify errors, which is fragile.

**Severity:** LOW. The `%w` wrapping itself is correct. The absence of typed errors is a design choice aligned with the codebase's pattern-match classification approach, but it means error classification is string-based rather than type-based.

---

## 2. Internal Error Exposure to Clients

**Verdict:** CRITICAL -- raw Go error messages are exposed to HTTP clients in multiple places.

### 2.1 Proxy layer: `handler/proxy/upstream.go`

**Lines 86 and 101** -- raw `http.NewRequestWithContext` and `http.DefaultClient.Do` errors exposed verbatim:
```go
writeJSONError(w, 502, fmt.Sprintf("Upstream error: %v", err), "upstream_error")
```
This leaks internal details like DNS resolution failures, TLS handshake errors, socket addresses, and potentially internal hostnames to downstream API consumers. Example leak: `Upstream error: dial tcp 10.0.0.1:443: connection refused`.

**Line 199** -- content-based failure reason exposed verbatim:
```go
writeJSONError(w, failure.Status, failure.Reason, "upstream_error")
```
`DetectProxyFailure` generates reasons like `"Upstream response matched failure keyword: rate limit"` or `"Upstream returned empty content"`. These are operational details that downstream consumers should not receive -- they should get a generic 502 with a safe message.

**Line 192** -- hardcoded but safe:
```go
writeJSONError(w, 502, "Failed to read upstream response", "upstream_error")
```
This is safe.

### 2.2 Admin layer: multiple files

Admin handlers expose `err.Error()` directly to clients in 20+ locations:
- `handler/admin/accounts.go`: lines 95, 180, 225, 602, 621
- `handler/admin/account_tokens.go`: lines 52, 97, 233, 475, 565
- `handler/admin/events.go`: lines 57, 66, 84
- `handler/admin/sites.go`: lines 47, 235, 383, 407
- `handler/admin/site_announcements.go`: line 59

These leak database errors (PostgreSQL connection strings, column names, constraint violations), disk paths, and internal service details to authenticated admin API consumers.

**Severity:** CRITICAL for proxy layer (exposed to external API consumers). HIGH for admin layer (exposed to authenticated internal users, but still a leak vector).

### 2.3 Safe patterns observed

The `surface.go` `PrepareCtx` function uses hardcoded, safe messages:
- `"unauthorized"`, `"failed to read request body"`, `"invalid JSON body"`, `"model is required"`, `"model not allowed by downstream policy"`

The `router.go` `writeJSONError` helper accepts arbitrary message strings, which makes the exposure risk immediate wherever it is called.

**Severity:** HIGH

---

## 3. 4xx/5xx Status Code Distinction

**Verdict:** GOOD -- correct and consistent across routing, retry, and circuit breaker layers.

### 3.1 Retry policy (`proxy/retry_policy.go:ShouldRetryProxyRequest`)

| Status range | Retryable | Rationale |
|---|---|---|
| >= 500 | YES | Server error, another channel may work |
| 408, 409, 425, 429 | YES | Timeout/conflict/too-early/rate-limit, retryable |
| 401, 403 | YES | Token refresh may resolve |
| 400, 404, 422 | NO | Bad request -- would fail on any channel |
| Other (incl. text override) | Depends | Model-unsupported text -> YES; validation text -> NO |

### 3.2 Same-site endpoint abort (`proxy/retry_policy.go:ShouldAbortSameSiteEndpointFallback`)

Correctly classifies: 500+ or 408/429 with transient/systemic text patterns as abort-worthy. Non-500 errors without systemic text are allowed to continue endpoint fallback.

### 3.3 Runtime failure classification (`routing/runtime_health.go`)

`ResolveSiteRuntimeFailurePenalty` applies differential penalties:
- 429 usage-limit: 0.4
- Model-unsupported text: 0.9
- Protocol-mismatch text: 0.6
- Validation text: 0.25 (low -- not site's fault)
- 500+ or transient text: 2.5 (high)
- 429 generic: 2.2
- 401/403: 1.8
- Generic 400-499: 0.9

`IsTransientSiteRuntimeFailure` correctly excludes model/protocol/validation/usage-limit failures from the transient category, so only genuine 500+ or 429 errors trigger the breaker streak.

### 3.3 Proxy response codes (`handler/proxy/upstream.go:dispatchUpstream`)

| Scenario | Code | Appropriate? |
|---|---|---|
| No channels available | 503 | YES |
| Request construction fails | 502 | YES (upstream-related, not client error) |
| HTTP request fails (network) | 502 | YES |
| Read upstream body fails | 502 | YES |
| Content-based failure detected | From failure.Status (502) | YES |
| All channels exhausted | 503 | YES |

**Severity:** NONE -- correct.

---

## 4. Error Response JSON Format Consistency

**Verdict:** CRITICAL -- three completely different JSON error formats across the codebase.

### 4.1 Proxy layer format (OpenAI-compatible)

`handler/proxy/router.go:writeJSONError`:
```json
{"error": {"message": "No available channels", "type": "server_error"}}
```
This is the OpenAI API error format. Used by all `/v1/*` proxy surfaces (chat, messages, completions, embeddings, images, videos, search, responses, files, gemini).

Error `type` values observed:
- `"invalid_request_error"` -- 400, 401, 403, 404
- `"server_error"` -- 501, 502, 503
- `"upstream_error"` -- 502 (content-based failures)
- `"not_found_error"` -- 404 (videos)

### 4.2 Admin layer format A (`handler/admin/sites.go`, `handler/admin/events.go`, `handler/admin/accounts.go`)

`writeJSON` with `map[string]string{"error": "..."}`:
```json
{"error": "database connection failed: ..."}
```

### 4.3 Admin layer format B (`handler/admin/account_tokens.go`)

`writeJSON` with `map[string]any{"success": false, "message": "..."}`:
```json
{"success": false, "message": "database connection failed: ..."}
```

And sometimes `map[string]any{"success": false, "error": "..."}`:
```json
{"success": false, "error": "Invalid account token payload."}
```

### 4.4 Auth/admin layer format C (`auth/admin.go`)

`writeJSON` with `jsonErrorBody{Error: msg}`:
```json
{"error": "some message"}
```

### Impact

- Admin API consumers must handle three different error envelope shapes.
- The proxy layer format is OpenAI-compatible (good), but `"upstream_error"` is a non-standard `type` value -- OpenAI uses `"api_error"` for internal/server errors.
- No consistent error code or identifier field across formats.

**Severity:** HIGH for admin layer (3 inconsistent formats). MEDIUM for proxy layer type values.

---

## 5. Upstream Error Classification (Retryable vs Fatal)

**Verdict:** GOOD -- comprehensive pattern-based classification with clear decision matrices.

### 5.1 Classification layers

The codebase has three independent pattern match layers operating at different scopes:

| Layer | File | Purpose | Granularity |
|---|---|---|---|
| Channel retry | `proxy/retry_policy.go` | Should we try another channel? | `ShouldRetryProxyRequest()` |
| Endpoint fallback | `proxy/retry_policy.go` | Should we abort same-site endpoint fallback? | `ShouldAbortSameSiteEndpointFallback()` |
| Runtime health | `routing/runtime_health.go` | Is failure transient? What penalty? | `IsTransientSiteRuntimeFailure()`, `ResolveSiteRuntimeFailurePenalty()` |

### 5.2 Pattern categories

| Category | Patterns | Purpose |
|---|---|---|
| Model-unsupported | Chinese + English "does not support model", "no such model", etc. | Retry different channel |
| Timeout | "timed out", "read timeout", "connection timed out" | Retry same/different channel |
| Channel-local | "invalid api key", "forbidden", "rate limit", "quota", "bad gateway", "cpu overloaded" | Retry different channel |
| Non-retryable request | "invalid request body", "validation", "malformed", "invalid json" | Do NOT retry |
| Site abort | "bad gateway", "rate limit", "connection reset/refused" | Abort endpoint fallback |
| Transient | "bad gateway", "service unavailable", "connection reset", "timeout" | Breaker streak |
| Model failure | "unsupported model", "no such model", "unknown model" | NOT transient |
| Protocol failure | "unsupported legacy protocol", "please use /v1/messages" | NOT transient |
| Validation | "invalid request body", "validation", "malformed" | NOT transient |
| Usage/rate limit | "usage_limit_reached", "quota exceeded", "rate limit" | NOT transient |

### 5.3 Correctness assessment

- `ShouldRetryProxyRequest`: 500+ retry, 400/404/422 non-retry, text overrides -- correct.
- `ShouldAbortSameSiteEndpointFallback`: 500+ or timeout with systemic patterns -- correct.
- `IsTransientSiteRuntimeFailure`: 500+/429 minus model/protocol/validation limit -- correct.
- Gap: `IsUsageLimitRateLimitFailure` only checks `status == 429`. A 500 with usage-limit text would NOT be classified as usage-limit failure. This is intentional (500 with usage-limit text means the server is broken, not rate-limited), but worth noting.

**Severity:** NONE -- correct classification.

---

## 6. Circuit Breaker Triggered on Correct Error Types

**Verdict:** GOOD -- breaker logic correctly isolates transient failures.

### 6.1 Breaker mechanics (`routing/runtime_health.go`)

- **Trigger:** 3+ transient failures within the `SiteTransientStreakWindowMs` (5 minutes).
- **Transient definition:** 500+ or 429, AND NOT model-unsupported, protocol, validation, or usage-limit.
- **Levels:** 0ms (off) -> 60s -> 5min -> 30min.
- **Escalation:** Each new transient streak advances the breaker level.
- **Reset:** Any success resets breaker to level 0.
- **Decay:** PenaltyScore decays with 10-minute half-life, independent of breaker.

### 6.2 Correctness

- Model-unsupported failures (e.g., "this model does not exist on this site") should NOT trigger the breaker -- correct, they are excluded from transient.
- Protocol mismatch failures ("please use /v1/responses") should NOT trigger the breaker -- correct.
- Validation failures (400 with "invalid JSON") should NOT trigger the breaker -- correct.
- Usage-limit/rate-limit failures should NOT trigger the breaker -- correct (classified as non-transient).
- 500 errors ARE transient -- correct.
- 429 errors ARE transient (unless usage-limit pattern matches) -- correct.

### 6.3 Gaps

1. **Streaming responses:** `handleStreamUpstream` discards `latencyMs` (line 185: `_ = latencyMs`). Streaming success/failure and latency are never recorded in runtime health. This means streaming failures cannot trigger the breaker.

2. **Persistence error silently dropped:** `persistSiteRuntimeHealthState` line 939: `_ = healthSettingsStore.Set(...)`. If the settings store fails, breaker state is lost on restart with no log.

3. **Manual JSON parser in `unmarshalPayload`:** The hand-rolled JSON parser (`readObj`, `readString`, `readNumber`, etc.) in `runtime_health.go` is ~200 lines of manual parsing. The `unmarshalHealthPayload` function exists but the code path that uses `encoding/json` (`unmarshalJSON` -> `json.Unmarshal`) is separate from the `unmarshalPayload` code path. The `readHealthState()` function that the manual parser calls is a STUB returning nil (line 1441-1443), while `readHealthState()` is never actually wired to deserialize real data. This suggests the manual parser path is incomplete/dead code, and the `encoding/json` path (lines 1046-1072 in `EnsureSiteRuntimeHealthStateLoaded`) is the actual deserialization path.

**Severity:** LOW for breaker correctness (logic is sound). MEDIUM for streaming gap (no breaker protection on streaming failures). LOW for persistence silent error.

---

## 7. Additional Findings

### 7.1 No structured error types

The codebase has zero custom error types. Classification is entirely string-based via regex patterns. This means:
- No `errors.Is()` or `errors.As()` support.
- Changing an upstream error message format (e.g., OpenAI changing "rate limit" wording) could silently break classification.
- Adding new error types requires duplicating regex patterns across multiple files.

**Severity:** MEDIUM -- works today, fragile to upstream changes.

### 7.2 `cloneAndSetModel` no validation

`handler/proxy/upstream.go:cloneAndSetModel` replaces the `model` field without validating the upstream model name. An empty or malformed upstream model name is passed directly to the upstream API. The caller does check `selected.ActualModel` for emptiness and falls back to `ctx.RequestedModel`, which is validated in `PrepareCtx`, so the risk is low in practice.

**Severity:** LOW.

### 7.3 SSE error path missing

For streaming responses (`handleStreamUpstream`), if the upstream returns a non-200 status, the handler already wrote `200` headers and started streaming before reading the upstream status. If the upstream returns 401/403/500 after headers are flushed, the downstream client has no way to know -- they only see an abrupt stream termination. The TS implementation likely checks the upstream status BEFORE writing downstream headers.

**Severity:** MEDIUM.

### 7.4 No request-scoped logging

Error paths in `dispatchUpstream` do not log any context (request ID, model, channel ID, retry count, status). Debugging production failures requires correlating HTTP access logs with error responses. The `slog` import in `surface.go` is present but unused (`var _ = slog.Info`).

**Severity:** LOW (operational concern, not correctness).

---

## Summary

| Category | Severity | Count |
|---|---|---|
| Error wrapping (%w) | GOOD | 60+ correct sites, 0 violations |
| Internal errors exposed | **CRITICAL** | 2 proxy sites + 20+ admin sites |
| 4xx/5xx distinction | GOOD | Correct at all layers |
| Error JSON format | **HIGH** | 3 inconsistent admin formats |
| Retryable vs fatal classification | GOOD | Correct classification matrix |
| Circuit breaker trigger | GOOD | Correct trigger logic |

### Priority fixes

1. **CRITICAL:** `handler/proxy/upstream.go` lines 86 and 101 -- replace `fmt.Sprintf("Upstream error: %v", err)` with generic message like `"Upstream request failed"`.
2. **CRITICAL:** `handler/proxy/upstream.go` line 199 -- replace `failure.Reason` with generic message like `"Upstream returned an error response"`.
3. **HIGH:** All admin handlers using `err.Error()` -- use generic messages and log the actual error server-side.
4. **HIGH:** Standardize admin error response format to a single envelope (recommend `{"error": {"message": "...", "type": "..."}}` to match proxy layer).
5. **MEDIUM:** Record streaming latency and failure in runtime health.
6. **MEDIUM:** Add request-scoped error logging in `dispatchUpstream`.
