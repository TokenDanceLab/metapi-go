# Memory Efficiency Audit -- metapi-go

**Date:** 2026-07-04
**Scope:** Large buffer allocations, connection pooling, HTTP client reuse, unnecessary copies, unbounded growth
**Files audited:** 15 source files across 5 packages

---

## 1. Large Buffer Allocations in Streaming

### 1.1 StreamTransformContext map proliferation (transform/shared/utils.go:49-62, types.go:45-62)

**Severity: Medium**

Every stream creates a `StreamTransformContext` with six heap-allocated maps:

```go
ToolCalls:                         make(map[int]*ToolCallAccumulator),
ResponsesToolCallIndexByOutputIndex: make(map[int]int),
ResponsesToolCallIndexByID:         make(map[string]int),
ResponsesTextByIndex:               make(map[int]string),
ResponsesReasoningByIndex:          make(map[int]string),
```

In a non-Responses flow (the common case), four of these six maps are never populated but still allocate backing buckets (~8 entries each at Go's default map growth), wasting approximately 200-400 bytes per stream. For a gateway handling thousands of concurrent streams, this adds up.

**Recommendation:** Lazily initialize the Responses-specific maps only when the first Responses event is detected (e.g., in `NormalizeUpstreamStreamEvent` when `strings.HasPrefix(rt, "response.")`). Move them behind a pointer field that starts nil.

### 1.2 GeminiAggregateState.Parts unbounded accumulation (transform/gemini/generate_content/compatibility.go:758-768)

**Severity: Low (Medium for long-running streams)**

`GeminiAggregateState.Parts` is a `[]map[string]any` that grows with every stream chunk. The `coalesceGeminiParts` function (line 894) merges adjacent text parts with matching metadata, but the overall slice still grows proportionally to the total number of distinct content blocks emitted during a stream. For a very long generation with many content blocks (e.g., rich markdown with many alternating text/tool-call blocks), this slice can grow to thousands of elements. Each element is a `map[string]any` which carries map header overhead (~48 bytes) plus key-value storage.

On `SerializeDone` (line 868), this entire accumulated slice is serialized as `sb.State.Parts`.

**Recommendation:** Add a high-water-mark cap (e.g., 10,000 parts). Beyond the cap, simply drop oldest non-essential parts on insertion rather than growing unboundedly. Alternatively, consider serializing and flushing accumulated parts periodically if the slice exceeds a threshold.

### 1.3 ToolCallAccumulator.Arguments string concatenation (transform/shared/utils.go:45-47)

**Severity: Low**

```go
existing.Arguments += td.ArgumentsDelta
```

Repeated string concatenation in a loop creates a new string allocation for each delta. For tool calls with many argument deltas (streaming JSON), this is an O(n^2) memory pattern.

**Recommendation:** Use `strings.Builder` inside `ToolCallAccumulator` instead of a plain string, calling `builder.String()` only when the final value is needed.

### 1.4 StringifyUnknownValue and SafeJSONString called repeatedly (transform/shared/utils.go:162-193)

**Severity: Low-Medium**

`StringifyUnknownValue` and `SafeJSONString` allocate `[]byte` via `json.Marshal` and then convert to `string`. These are called repeatedly in `NormalizeUpstreamStreamEvent` (chatFormatsCore.go:901-1173) for every stream chunk, sometimes multiple times per event. Each call allocates a new byte buffer and new string.

Specific hot paths:
- Line 910: `StringifyUnknownValue(msg["content"])` 
- Line 1031: `SafeJSONString(cb["input"])`
- Line 1050/1071/1073: `StringifyUnknownValue(delta/cp/parts)`
- Lines 627, 629, 1186: `StringifyUnknownValue` for final response parsing

**Recommendation:** For `StringifyUnknownValue`, cache results when the input is not a string (the common case for map/array types in stream processing). Use a sync.Pool for the `json.Marshal` buffer.

---

## 2. Connection Pool Sizing

### 2.1 Hardcoded pool limits with no lifetime management (store/open.go:162-166)

**Severity: High**

```go
func configurePostgresPool(db *DB) error {
    db.SetMaxOpenConns(20)
    db.SetMaxIdleConns(5)
    return nil
}
```

Issues:
- **No `SetConnMaxLifetime`**: Connections live forever. On long-running deployments behind a load balancer or NAT gateway with idle-timeout enforcement, connections silently break. The next query attempt fails with a network error, and sqlx retries the query on a new connection, causing latency spikes.
- **No `SetConnMaxIdleTime`**: Idle connections never expire. If traffic is bursty, 5 idle connections sit open consuming database resources indefinitely.
- **No `SetConnMaxLifetime` override**: No configuration hook allows operators to tune these values for their environment.
- **Hardcoded 20/5**: These values are reasonable defaults but should be configurable. A high-traffic gateway with 1,000 concurrent requests needs more than 20 connections; a low-traffic deployment wastes resources on idle connections.
- **SQLite has no pool tuning at all**: `sqlx.Open("sqlite", ...)` creates a pool with unlimited max open connections by default. Since SQLite is single-writer, concurrent writes will hit `SQLITE_BUSY`. The `busy_timeout=5000` pragma mitigates this but does not solve it. Should call `db.SetMaxOpenConns(1)` for SQLite.

**Recommendation:**
```go
db.SetMaxOpenConns(cfg.DBMaxOpenConns)       // configurable, default 20
db.SetMaxIdleConns(cfg.DBMaxIdleConns)       // configurable, default 5
db.SetConnMaxLifetime(30 * time.Minute)      // hard cap connection lifetime
db.SetConnMaxIdleTime(5 * time.Minute)       // close idle conns after 5min
```

For SQLite, add:
```go
db.SetMaxOpenConns(1)  // SQLite serializes writes
```

---

## 3. HTTP Client Reuse

### 3.1 New transport/client on every explicit-proxy request (platform/site_proxy.go:118-148)

**Severity: High**

```go
func (sp *SiteProxy) doWithExplicitProxy(ctx context.Context, req *http.Request, proxyConfig *ProxyConfig) (*http.Response, error) {
    // ...
    transport := &http.Transport{     // NEW transport every call
        Proxy: http.ProxyURL(proxyURL),
        DialContext: (&net.Dialer{...}).DialContext,
        TLSHandshakeTimeout:   10 * time.Second,
        ResponseHeaderTimeout: 30 * time.Second,
    }
    client := &http.Client{           // NEW client every call
        Transport: transport,
        Timeout:   30 * time.Second,
    }
    return client.Do(req)
}
```

Every request with an explicit proxy URL creates a brand-new `http.Transport`. Each transport has its own connection pool. This means:
- **No TCP connection reuse**: Every request does a full TCP+TLS handshake.
- **No idle connection pooling**: Connections are closed immediately after use (garbage collected).
- **OS file descriptor churn**: Under load, this can exhaust ephemeral ports.
- **Default transport pool limits never apply**: `MaxIdleConns` (default 100) and `MaxIdleConnsPerHost` (default 2) are per-transport, but since each transport handles one request, these are meaningless.

The same pattern exists in `DoWithProxy()` (lines 152-183) where a new transport+client is created for every call with a proxy URL, AND a new `http.Client` is created for every call without a proxy.

**Recommendation:** Cache proxy-specific transports by proxy URL in a `sync.Map` with periodic cleanup. A warmed transport with connection pooling will reduce latency by 50-200ms per request (saved TLS handshake) and eliminate connection churn.

### 3.2 No connection pooling configuration on primary transports (platform/site_proxy.go:53-84)

**Severity: Medium**

The primary transports in `buildClients()` do not set:
- `MaxIdleConns` (defaults to 100)
- `MaxIdleConnsPerHost` (defaults to 2 -- too low for a gateway)
- `IdleConnTimeout` (defaults to 90s)
- `MaxConnsPerHost` (defaults to 0 = unlimited)
- `ForceAttemptHTTP2` (defaults to true)

For a proxy gateway that fans out to multiple upstream providers, `MaxIdleConnsPerHost: 2` means only 2 idle connections are kept per upstream host. Under load, excess connections are closed and reopened, causing latency jitter.

**Recommendation:**
```go
transport.MaxIdleConns = 100
transport.MaxIdleConnsPerHost = 10
transport.IdleConnTimeout = 90 * time.Second
transport.MaxConnsPerHost = 50  // prevent overwhelming any single upstream
```

### 3.3 Missing HTTP/2 configuration awareness

**Severity: Low**

`ForceAttemptHTTP2` defaults to `true`, which is usually desirable. However, when an explicit proxy is used (SOCKS5, HTTP proxy), HTTP/2 may not be supported or may cause issues with certain proxy implementations. No configuration flag exists to disable HTTP/2 per-proxy.

---

## 4. Unnecessary `[]byte` Copies

### 4.1 cloneJSONValue uses Marshal+Unmarshal double serialization (transform/canonical/openai_bridge.go:38-51, transform/gemini/generate_content/compatibility.go:939-954)

**Severity: Medium**

```go
func cloneJSONValue(v any) any {
    b, err := json.Marshal(v)        // serialize to []byte
    var dst any
    json.Unmarshal(b, &dst)           // deserialize back
    return dst
}
```

This is called extensively throughout request transformation:
- `CanonicalRequestFromOpenAiBody` (lines 104, 109): cloning metadata, attachments
- `parseCanonicalTools` (lines 498, 513, 515, 536, 538, 548): cloning function input schemas for every tool
- `CanonicalRequestToOpenAiChatBody` (lines 740, 747, 753, 786): cloning attachments, tool data, passthrough values
- `NormalizeRequest` in Gemini (multiple calls at lines 29-69): cloning every allowed key
- `BuildGeminiGenerateContentRequestFromOpenAi` uses `cloneJSONValue` through `NormalizeRequest`

Each call performs two full JSON serialization cycles. For a request with 50 tools each having a 2KB input schema, this is 50 x (Marshal + Unmarshal) = 100 JSON operations per request, producing ~200KB of temporary garbage.

**Recommendation:** Replace with a type-switch recursive deep copy:

```go
func cloneJSONValue(v any) any {
    switch x := v.(type) {
    case map[string]any:
        out := make(map[string]any, len(x))
        for k, val := range x {
            out[k] = cloneJSONValue(val)
        }
        return out
    case []any:
        out := make([]any, len(x))
        for i, val := range x {
            out[i] = cloneJSONValue(val)
        }
        return out
    default:
        return v  // strings, numbers, bools, nil are immutable
    }
}
```

This eliminates all Marshal/Unmarshal overhead for cloning.

### 4.2 `itoa` hand-rolled implementation with string concatenation (3 separate implementations)

**Severity: Low**

Three packages implement their own `itoa` using character-at-a-time string concatenation:

- `transform/canonical/openai_bridge.go:967-985`
- `transform/anthropic/messages/conversion.go:1281-1299`
- `transform/gemini/generate_content/compatibility.go:1078-1096`

Each iteration of the loop allocates a new string: `s = string(rune('0'+n%10)) + s`. For a number like 1234567890, this is 10 allocations.

Additionally, `transform/shared/utils.go:383-386` has its own `itoa` using `json.Marshal` + string replacement.

**Recommendation:** Replace all `itoa` implementations with `strconv.FormatInt(n, 10)`. This is a single allocation using an internal buffer.

### 4.3 String building via intermediate slices (widespread pattern)

**Severity: Low**

Many functions build strings by appending to `[]string` then calling `strings.Join`:

- `canonicalPartsToOpenAIContent` (openai_bridge.go:800-828): builds `visibleText` and `reasoningText` slices
- `toOpenAIContent` (compatibility.go:373-397): builds `textContent` via `+=` then prepends to slice
- `buildAnthropicDoneEvents` etc.: `JoinNonEmpty` calls

While `strings.Join` is efficient, the intermediate `[]string` slice growth causes reallocations.

**Recommendation:** For hot paths (streaming), use `strings.Builder` with `WriteString` to avoid the intermediate slice entirely.

---

## 5. Unbounded Map/Slice Growth

### 5.1 RouteCache.matchCache -- no eviction, no size limit (routing/cache.go:16, 62-84)

**Severity: Medium-High**

```go
type RouteCache struct {
    matchCache   map[int64]*routeMatchEntry  // grows unboundedly
}
```

The `matchCache` map grows with every distinct route that is matched. Entries are only removed via:
- `InvalidateRouteScopedCache(routeID)` -- explicit call, not guaranteed to happen
- `InvalidateAll()` -- clears everything, also explicit

There is no periodic cleanup goroutine, no max size cap, and no LRU/LFU eviction. Stale entries (TTL-expired) remain in the map consuming memory until the specific route is re-accessed (at which point `GetMatch` returns nil but does NOT delete the entry).

In a deployment with 10,000+ distinct routes (tokens), each entry holds a `*RouteMatch` with a slice of `SelectedChannel`, each of which embeds a `store.RouteChannel` (ID, name, base URL, key, models, weight, etc.). Conservatively, each entry is 500-2000 bytes. At 10,000 routes that's 5-20 MB of heap that never shrinks.

**Recommendation:**
1. Add a background goroutine that periodically scans and deletes TTL-expired entries (every 30s-60s).
2. Add a max cache size (e.g., 10,000 entries) with random eviction or LRU when exceeded.
3. In `GetMatch`, delete the entry when it is found to be expired rather than just returning nil.

### 5.2 ProxyChannelCoordinator.stickyBindings -- no periodic cleanup (proxy/session.go:99)

**Severity: Medium**

```go
type ProxyChannelCoordinator struct {
    stickyBindings    map[string]StickyEntry  // grows with every sticky session
}
```

`cleanupExpiredLocked` only runs when `GetStickyChannelID` or `BindStickyChannel` is called. If a sticky session key is never accessed again after expiry, its entry persists forever. The key is a composite string like `key:123|codex|/v1/chat|gpt-4|session-abc`, typically 40-80 bytes. At 100,000 stale entries, that's 4-8 MB plus map overhead.

**Recommendation:** Add a periodic cleanup goroutine (every 1-5 minutes) that scans and deletes expired entries. Consider using a time-wheel or bucketed map for more efficient expiry.

### 5.3 ProxyChannelCoordinator.channelStates -- orphaned state (proxy/session.go:100, 253-265)

**Severity: Medium**

```go
type ProxyChannelCoordinator struct {
    channelStates     map[int64]*channelRuntimeState  // grows with channel usage
}
```

Channel state entries are only deleted when `pruneAndMaybeDelete` finds `activeLeaseIDs == 0` AND all waiters cancelled. However, `getOrCreateChannelState` creates entries eagerly. If a channel is used once and never again, and its last waiter timed out but was not properly pruned (race condition between timer goroutine and `pruneCancelledWaitersLocked`), the state entry may persist indefinitely.

Additionally, `channelRuntimeState.queue` can grow unboundedly if `channelQueueWaitMs` is set high and many requests queue up. The queue slice is only pruned when `pruneCancelledWaitersLocked` runs (which only happens inside lock-acquiring code paths).

**Recommendation:**
1. Add a periodic GC pass that deletes channel states with zero active leases and zero non-cancelled waiters.
2. Add a hard cap on `state.queue` length (e.g., 100). Requests beyond the cap should get an immediate timeout rather than being queued.
3. In `touchLease`, avoid spawning a new goroutine for every touch -- the current implementation leaks goroutines (see section 6.1).

### 5.4 GeminiAggregateState.Citations, GroundingMetadata, ThoughtSignatures, Candidates (transform/gemini/generate_content/compatibility.go:757-768)

**Severity: Low**

These fields are declared but never populated in the streaming code except `Parts` and `Usage`. However, they exist as `[]map[string]any` and `[]string` fields that are never cleaned up between stream invocations. If `NewStreamBridge` is called per request (which it is), this is fine. But if a bridge were ever reused, these would accumulate.

### 5.5 StreamTransformContext.ResponsesTextByIndex unbounded index growth (transform/shared/utils.go:59)

**Severity: Low**

`ResponsesTextByIndex` is a `map[int]string` keyed by output index. For responses with many output items (function call results, multiple messages), this map grows proportionally. However, since the context is per-request and GC'd after the request, this is bounded by the request lifetime.

---

## 6. Additional Findings (Goroutine/Memory Leaks)

### 6.1 ChannelLease.Touch spawns unbounded goroutines (proxy/session.go:399-421)

**Severity: High (Goroutine leak)**

```go
func (c *ProxyChannelCoordinator) touchLease(lease *ChannelLease) {
    // ...
    go func() {  // NEW goroutine on EVERY Touch()
        timer := time.NewTimer(...)
        defer timer.Stop()
        for {
            select {
            case <-timer.C:       lease.Release(); return
            case <-lease.expiryCh: timer.Reset(...)
            case <-lease.doneCh:   return
            }
        }
    }()
}
```

`Touch()` is called by a keepalive ticker goroutine (line 383-394) every `keepaliveMs` milliseconds. Each call spawns a new goroutine that runs until a timer fires or the lease is released. The previous goroutine from the last `Touch()` is orphaned -- it's still blocked on `<-lease.expiryCh` or `<-lease.doneCh`, but `Touch()` only sends to `expiryCh` with `select { case lease.expiryCh <- ... default: }`, so at most one goroutine receives the signal. The others remain blocked.

At 1-second keepalive and 30-second TTL, a single lease accumulates ~30 goroutines over its lifetime, only one of which is "live".

**Recommendation:** Replace the per-touch goroutine with a single long-lived timer goroutine per lease. Use `timer.Reset()` on the existing timer instead of spawning a new goroutine.

```go
func (c *ProxyChannelCoordinator) touchLease(lease *ChannelLease) {
    lease.expiryTimerLock.Lock()
    if lease.expiryTimer != nil {
        lease.expiryTimer.Reset(ttl)
    }
    lease.expiryTimerLock.Unlock()
}
```

### 6.2 Sort implementation uses O(n^2) bubble sort (transform/shared/utils.go:388-396)

**Severity: Low (perf, not memory)**

```go
func sortInts(s []int) {
    for i := 0; i < len(s); i++ {
        for j := i + 1; j < len(s); j++ {
            if s[i] > s[j] {
                s[i], s[j] = s[j], s[i]
            }
        }
    }
}
```

**Recommendation:** Use `slices.Sort(s)` from the standard library.

---

## Summary of Findings

| # | Area | Severity | Impact |
|---|------|----------|--------|
| 1.1 | StreamTransformContext eager map allocation | Medium | ~200-400 bytes/stream wasted |
| 1.2 | GeminiAggregateState.Parts unbounded | Low-Medium | Linear growth with stream length |
| 1.3 | ToolCall string concat | Low | O(n^2) allocations for large tool calls |
| 1.4 | StringifyUnknownValue repeated Marshal | Low-Medium | GC pressure from repeated allocations |
| 2.1 | No connection lifetime management | High | Broken connections, latency spikes, resource waste |
| 3.1 | New transport per explicit-proxy request | High | No connection reuse, TCP/TLS handshake per request |
| 3.2 | Default pool limits too conservative | Medium | Connection churn under load |
| 4.1 | cloneJSONValue Marshal+Unmarshal | Medium | Double serialization on every transform |
| 4.2 | Hand-rolled itoa | Low | Unnecessary string allocations |
| 5.1 | RouteCache.matchCache no eviction | Medium-High | Unbounded growth with route count |
| 5.2 | stickyBindings no periodic cleanup | Medium | Memory leak for abandoned sessions |
| 5.3 | channelStates orphaned entries | Medium | Memory leak for retired channels |
| 6.1 | Goroutine leak in Touch | High | ~TTL/keepalive goroutines leaked per lease |
| 6.2 | O(n^2) bubble sort | Low | Not a memory issue |

**Highest priority fixes:**
1. Fix goroutine leak in `touchLease` (6.1) -- clear correctness bug
2. Cache proxy-specific HTTP transports (3.1) -- direct latency improvement
3. Add connection pool lifetime management (2.1) -- prevents production outages
4. Replace `cloneJSONValue` with recursive copy (4.1) -- significant GC reduction
5. Add periodic cleanup to `matchCache` and `stickyBindings` (5.1, 5.2) -- prevents gradual memory leaks
