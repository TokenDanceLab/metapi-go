# Performance Hot-Path Audit -- metapi-go

**Date:** 2026-07-04
**Scope:** Allocations in proxy request hot paths: `json.Marshal/Unmarshal`, string concatenation, map/slice proliferation, `time.Format`, and byte/string conversions.
**Files audited:** `handler/proxy/chat.go`, `handler/proxy/upstream.go`, `handler/proxy/surface.go`, `handler/proxy/router.go`, `handler/proxy/client_detect.go`, `handler/proxy/messages.go`, `proxy/endpoint_flow.go`, `proxy/channel_selection.go`, `proxy/failure_judge.go`, `routing/selector.go`, `routing/weights.go`, `routing/cache.go`, `routing/matcher.go`, `routing/cooldown.go`, `routing/round_robin.go`, `routing/ports.go`

---

## 1. Critical: Redundant json.Marshal on the Proxy Hot Path

### 1.1 Request body re-marshal in dispatchUpstream (upstream.go:79-80)

**Severity: Critical**

```go
forwardBody := cloneAndSetModel(ctx.Body, upstreamModel)   // line 79
forwardBytes, _ := json.Marshal(forwardBody)                // line 80
```

Every proxy request (chat, messages, completions, embeddings, images) does:

1. `json.Unmarshal` in `PrepareCtx` (surface.go:71) -- parses the incoming body into `map[string]any`.
2. `cloneAndSetModel` (upstream.go:258-271) -- allocates a new `map[string]any` and copies every key.
3. `json.Marshal(forwardBody)` (upstream.go:80) -- re-serializes the entire body back to `[]byte`.

The ONLY mutation between step 1 and step 3 is replacing the `"model"` field. For a typical 5 KB chat request body, this is **10+ KB of avoidable allocation per request** (the original bytes, the unmarshalled map, the cloned map, and the re-marshalled bytes all coexist).

**Impact:** For 1000 req/s, this alone generates ~10 MB/s of avoidable heap allocation.

**Fix:** Instead of `json.Unmarshal` -> clone -> `json.Marshal`, perform a surgical byte-level model replacement on the raw request body. The `"model"` field is always a top-level JSON string key. Use a lightweight approach:

```go
// Option A: Zero-allocation model swap (preferred)
func swapModelInJSON(body []byte, newModel string) []byte {
    // Find "model" key at top level and replace its string value
    // Use a small state machine; no full JSON parse needed
}

// Option B: json.RawMessage + partial decode
func buildForwardBody(bodyBytes []byte, upstreamModel string) ([]byte, error) {
    var raw map[string]json.RawMessage
    if err := json.Unmarshal(bodyBytes, &raw); err != nil {
        return nil, err
    }
    raw["model"] = json.RawMessage(strconv.Quote(upstreamModel))
    return json.Marshal(raw)
}
```

Option A is zero-allocation (except the output buffer). Option B at least avoids the intermediate `map[string]any` full decode and the `cloneAndSetModel` allocation. In either case, `cloneAndSetModel` can be removed.

### 1.2 bytesReader string conversion (upstream.go:273-278)

**Severity: High**

```go
func bytesReader(b []byte) io.Reader {
    if len(b) == 0 {
        return nil
    }
    return strings.NewReader(string(b))  // string(b) allocates a copy of the entire body
}
```

This converts the re-marshalled JSON bytes to a `string`, then to a `strings.Reader`. This is a **second copy** of the entire request body, wasted.

**Fix:** Use `bytes.NewReader(b)` which wraps the byte slice without copying:

```go
func bytesReader(b []byte) io.Reader {
    if len(b) == 0 {
        return nil
    }
    return bytes.NewReader(b)
}
```

Combined with fix 1.1 (eliminating `json.Marshal` and working directly with raw body bytes), this eliminates a third copy of the request body.

---

## 2. String Concatenation Hot Paths

### 2.1 Authorization header concatenation (upstream.go:96)

**Severity: Low**

```go
req.Header.Set("Authorization", "Bearer "+selected.TokenValue)
```

This creates a new string via concatenation. For 1000 req/s, that is 1000 allocations. Token values are typically 50-200 chars.

**Fix:** Use `strings.Builder` or a pre-allocated buffer pool:

```go
var authBufPool = sync.Pool{
    New: func() any { return make([]byte, 0, 256) },
}
buf := authBufPool.Get().([]byte)[:0]
buf = append(buf, "Bearer "...)
buf = append(buf, selected.TokenValue...)
req.Header.Set("Authorization", string(buf))
authBufPool.Put(&buf)
```

However, since Go's `http.Header.Set` takes a string (which requires a `[]byte` to `string` conversion internally for HTTP/1 framing), the gain from pooling is marginal. This is low priority.

### 2.2 Upstream URL construction (proxy/endpoint_flow.go:98-104)

**Severity: Low**

```go
func BuildUpstreamURL(siteURL, path string) string {
    siteURL = strings.TrimRight(siteURL, "/")   // allocates
    if !strings.HasPrefix(path, "/") {
        path = "/" + path                        // allocates
    }
    return siteURL + path                        // allocates
}
```

Called 1-2 times per proxy request. `strings.TrimRight` always allocates a new string even when the suffix is not present.

**Fix:** Check for trailing slash before trimming, and use `strings.Builder`:

```go
func BuildUpstreamURL(siteURL, path string) string {
    if len(siteURL) > 0 && siteURL[len(siteURL)-1] == '/' {
        siteURL = siteURL[:len(siteURL)-1]
    }
    if len(path) == 0 || path[0] != '/' {
        var b strings.Builder
        b.Grow(len(siteURL) + len(path) + 1)
        b.WriteString(siteURL)
        b.WriteByte('/')
        b.WriteString(path)
        return b.String()
    }
    var b strings.Builder
    b.Grow(len(siteURL) + len(path))
    b.WriteString(siteURL)
    b.WriteString(path)
    return b.String()
}
```

### 2.3 JSON error body with string concatenation (router.go:81-86)

**Severity: Low**

```go
func writeJSONError(w http.ResponseWriter, status int, message, typ string) {
    body := `{"error":{"message":"` + jsonEscape(message) + `","type":"` + jsonEscape(typ) + `"}}`
    w.Write([]byte(body))
}
```

`jsonEscape` returns an allocated string, and the concatenation allocates again. `[]byte(body)` allocates a third time.

**Fix:** Use a `strings.Builder` with `Grow` and write the escaped bytes directly:

```go
func writeJSONError(w http.ResponseWriter, status int, message, typ string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    var b strings.Builder
    b.Grow(len(message) + len(typ) + 64)
    b.WriteString(`{"error":{"message":"`)
    writeJSONEscaped(&b, message)
    b.WriteString(`","type":"`)
    writeJSONEscaped(&b, typ)
    b.WriteString(`"}}`)
    io.WriteString(w, b.String())
}
```

### 2.4 SSE event construction (surface.go:144-146)

**Severity: Low (stub-only)**

```go
func sseEvent(data string) []byte {
    return []byte("data: " + data + "\n\n")
}
```

This is only in `writeStubResponse` (used when upstream config is not wired), so it is NOT on the production hot path. No action needed.

### 2.5 Custom time formatter string concatenation (cooldown.go:122-123, 137-143)

**Severity: Low**

```go
func formatInt2(v int64) string {
    s := formatInt(v)
    if len(s) == 1 {
        return "0" + s    // allocates
    }
    return s
}

func pad3(v int) string {
    s := formatInt(int64(v))
    for len(s) < 3 {
        s = "0" + s       // allocates on each iteration
    }
    return s
}
```

The `timeTime.Format` method (cooldown.go:102-127) is a custom implementation that calls `formatInt2` in a loop, each call potentially allocating. This is used by `timeFromMs` -> `timeMsToISO` -> `ApplyRoundRobinCooldown` and `ApplyFibonacciCooldown`.

**Fix:** Use `strconv.FormatInt` with padding, or pre-allocate a `[32]byte` buffer and format directly into it:

```go
func formatInt2(v int64) string {
    var buf [3]byte
    n := len(strconv.AppendInt(buf[:0], v, 10))
    if n == 1 {
        return string(buf[:2])  // won't work as-is; use a different approach
    }
    return string(buf[:n])
}
```

Better: replace the custom `timeTime` entirely with `time.Unix(sec, 0).Format(time.RFC3339)` since `time.Time.Format` is well-optimized and does fewer allocations than the custom implementation.

---

## 3. Map Allocations in Channel Selection

### 3.1 Per-selection map creations (selector.go)

**Severity: Medium**

The `selectFromMatch` and `SelectPreferredChannel` functions allocate numerous maps:

| Location | Map | Approx Size | Purpose |
|---|---|---|---|
| selector.go:328 | `map[int64][]RouteChannelCandidate` | N (priority layers) | Priority grouping |
| selector.go:379 | `map[int64]bool` | len(AllowedRouteIDs) | Route ID allow-set |
| selector.go:462 | `map[int64]bool` | len(unitIDs) | Unique unit ID tracking |
| selector.go:491 | `map[int64]string` | len(routeIDs) | Fallback source model |

For a typical route with 10-20 channels across 3-5 sites, this is ~5-10 small map allocations per channel selection.

**Fix for priority layer map (selector.go:328):** The priority layer grouping creates `N` slices inside a map. Since priorities are small integers, use a slice of slices indexed by priority after finding the max priority:

```go
maxPriority := int64(0)
for _, c := range available {
    if c.Channel.Priority > maxPriority {
        maxPriority = c.Channel.Priority
    }
}
layers := make([][]RouteChannelCandidate, maxPriority+1)
for _, c := range available {
    p := int(c.Channel.Priority)
    layers[p] = append(layers[p], c)
}
// Iterate in order without a separate priorities slice or bubble sort
for _, layer := range layers {
    if len(layer) == 0 {
        continue
    }
    // ... process layer
}
```

This replaces the map allocation + `priorities` slice + O(n^2) bubble sort with a single slice allocation and O(n) traversal.

**Fix for route allow-set (selector.go:379):** This is only created when `len(policy.AllowedRouteIDs) > 0`, so it is not always on the hot path. When it is created, the size is small (typically 1-5 IDs). Low priority to optimize.

### 3.2 CalculateWeightedSelection map allocations (weights.go)

**Severity: Medium**

| Location | Map | Purpose |
|---|---|---|
| weights.go:152 | `map[int64]int` | siteChannelCounts |
| weights.go:248 | `map[int64]bool` | stableSiteIDs |
| weights.go:259 | `map[int]int` | rankByIndex |

And transitively via `BuildStableFirstPoolPlan` (weights.go:592-719):

| Location | Map | Purpose |
|---|---|---|
| weights.go:598 | `map[int64]RouteChannelCandidate` | leaderBySiteID |
| weights.go:599 | `map[int64]StableFirstSitePoolState` | siteStateByID |
| weights.go:701 | `map[int64]bool` | primarySet |
| weights.go:705 | `map[int64]bool` | observationSet |

Plus `getStableFirstSiteLeaderIndices` (weights.go:436):
| Location | Map | Purpose |
|---|---|---|
| weights.go:436 | `map[int64]bool` | seenSiteIDs |

For a selection involving 20 candidates across 5 sites, `BuildStableFirstPoolPlan` alone creates 4 maps. Each map requires a hash table allocation (~48 bytes header + 8 buckets minimum).

**Fix:** For small maps (siteChannelCounts, seenSiteIDs, primarySet, observationSet), where keys are `int64` and the expected size is tiny (1-10 entries), use a sorted slice + binary search, or a simple linear scan. The linear scan is faster than a map for fewer than ~20 entries:

```go
// Instead of map[int64]bool for seen sites:
func int64SliceContains(s []int64, v int64) bool {
    for _, x := range s {
        if x == v {
            return true
        }
    }
    return false
}
```

### 3.3 Header map extraction per request (client_detect.go:14-21)

**Severity: Low**

```go
func HeaderMapFromRequest(headers map[string][]string) map[string]string {
    result := make(map[string]string, len(headers))
    for k, v := range headers {
        if len(v) > 0 {
            result[k] = v[0]
        }
    }
    return result
}
```

Every proxy request allocates a new `map[string]string` for headers (typically 10-20 entries). This is passed to `DetectClientContext`.

**Fix:** If client detection only needs a few specific headers, pass the `http.Header` directly and look up headers lazily. If conversion is unavoidable, consider storing the result on a pooled context object.

---

## 4. Slice Allocations in Weighted Selection

### 4.1 CalculateWeightedSelection per-selection slices (weights.go:87-278)

**Severity: Medium**

For N candidates, the function allocates:

| Slice | Type | Size |
|---|---|---|
| effectiveCosts | `[]CostSignal` | N |
| runtimeHealthDetails | `[]SiteRuntimeHealthDetails` | N |
| channelLoadSnapshots | `[]ChannelLoadSnapshot` | N |
| valueScores | `[]float64` | N |
| normalizedVS | `[]float64` | N |
| baseContributions | `[]float64` | N |
| contributions | `[]float64` | N |
| rankedIndices | `[]int` | N |

That is 8 slices for a single weighted selection. For N=20 candidates, this is ~160 elements across slices (~1.5 KB of slice headers + backing arrays).

**Fix:** Pool these slices using `sync.Pool` keyed by capacity bracket. Since the hot path only needs these slices briefly (within one function call), they can be recycled:

```go
type selectionSlices struct {
    costs       []CostSignal
    health      []SiteRuntimeHealthDetails
    loads       []ChannelLoadSnapshot
    scores      []float64
    normalized  []float64
    base        []float64
    contribs    []float64
    ranks       []int
}

var selectionSlicePool = sync.Pool{
    New: func() any { return &selectionSlices{} },
}

func getSelectionSlices(n int) *selectionSlices {
    s := selectionSlicePool.Get().(*selectionSlices)
    // grow slices only if needed
    if cap(s.costs) < n { s.costs = make([]CostSignal, n) } else { s.costs = s.costs[:n] }
    // ... repeat for each slice
    return s
}
```

A simpler approach: embed a single flat `[]float64` buffer sized to `n * 7` and slice it into the needed sub-arrays, avoiding 7 separate allocations:

```go
// One allocation instead of 8:
buf := make([]float64, n*7)
valueScores := buf[0*n : 1*n]
normalizedVS := buf[1*n : 2*n]
baseContributions := buf[2*n : 3*n]
// Convert to int for rankedIndices via unsafe or use float interpretation
```

### 4.2 GetRoundRobinCandidates copy allocation (round_robin.go:9-10)

**Severity: Low**

```go
func GetRoundRobinCandidates(candidates []RouteChannelCandidate) []RouteChannelCandidate {
    sorted := make([]RouteChannelCandidate, len(candidates))
    copy(sorted, candidates)
    sort.SliceStable(sorted, ...)
}
```

Copies the entire candidate slice before sorting. `RouteChannelCandidate` is a large struct (contains `store.RouteChannel`, `store.Account`, `store.Site`, pointers, and slices). For 20 candidates, this is 20 large struct copies.

**Fix:** Sort indices instead of copying the slice:

```go
func SelectRoundRobinCandidate(candidates []RouteChannelCandidate) *RouteChannelCandidate {
    if len(candidates) == 0 {
        return nil
    }
    // Sort indices to avoid struct copying
    indices := make([]int, len(candidates))
    for i := range indices {
        indices[i] = i
    }
    sort.SliceStable(indices, func(i, j int) bool {
        // compare candidates[indices[i]] with candidates[indices[j]]
    })
    return &candidates[indices[0]]
}
```

---

## 5. time.Now() and time.Format Hot Path

### 5.1 ISO timestamp formatting per selection (selector.go:184, 253)

**Severity: Medium**

```go
nowISO := time.Now().UTC().Format(time.RFC3339)
nowMs := time.Now().UnixMilli()
```

Called in `selectFromMatch` (line 253) and `SelectPreferredChannel` (line 184). The `Format` call allocates a new string (20 bytes for RFC 3339). `time.Now()` allocates a `time.Time` struct on the stack, so that is free.

**Fix:** If the ISO timestamp is only used for string comparison against `CooldownUntil`, consider storing cooldown timestamps as `int64` (Unix milliseconds) instead of strings. This would eliminate the `Format` call entirely and also eliminate the `time.Parse` call in `IsChannelRecentlyFailed` (cooldown.go:97).

### 5.2 time.Parse per candidate failure check (cooldown.go:97)

**Severity: Medium**

```go
func IsChannelRecentlyFailed(failCount *int64, lastFailAt *string, nowMs int64, ...) bool {
    failTs, err := time.Parse(time.RFC3339, *lastFailAt)
    // ...
}
```

`time.Parse` is expensive: it allocates a `time.Time`, parses the string, and does timezone math. This is called for every candidate in `FilterRecentlyFailedCandidates` (cooldown.go:117-137), which iterates all candidates on every selection.

**Fix:** Store `lastFailAt` as Unix milliseconds (`int64`) in the data model instead of ISO strings. This is the single change with the broadest impact across the codebase:

- Eliminates `time.Now().UTC().Format(time.RFC3339)` in selector.go
- Eliminates `time.Parse` in cooldown.go:97
- Eliminates custom `timeTime.Format` and its string concatenations
- Simplifies comparison to integer subtraction

If the DB schema must store ISO, do the conversion once at DB read time (in `loadRouteMatch`) and keep `int64` in memory.

---

## 6. JSON Unmarshal in Failure Detection

### 6.1 Full JSON parse for non-stream response (failure_judge.go:78-81)

**Severity: Medium**

```go
var parsed any
if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
    return hasCompletionContentFromPayload(parsed)
}
```

For every non-stream upstream response, the entire response body is parsed as `map[string]any` just to check if it contains meaningful content. This duplicates the work that the caller (`handleNonStreamUpstream`) already did by reading the body.

**Fix:** Move the content check BEFORE relaying the response. The `handleNonStreamUpstream` function already has `bodyBytes` and could pass it to `DetectProxyFailure` without the `string()` conversion:

```go
func DetectProxyFailureBytes(bodyBytes []byte, usage *UsageSummary) *FailureResult {
    // Directly parse from bytes, reusing the same approach
}
```

More importantly, avoid parsing the entire body into `map[string]any`. Use `json.Decoder` with `UseNumber()` disabled and scan only the top-level keys needed (`choices`, `output`, `content`, `delta`, `text`), returning early on first match. This is a streaming approach that avoids building the full intermediate map:

```go
func hasCompletionContentFast(body []byte) bool {
    dec := json.NewDecoder(bytes.NewReader(body))
    t, _ := dec.Token() // '{'
    for dec.More() {
        key, _ := dec.Token()
        ks, _ := key.(string)
        switch ks {
        case "choices":
            // check first choice only
        case "output":
            // check if non-empty array
        case "content":
            // check if non-empty
        // ... return true on first hit
        default:
            dec.Token() // skip value
        }
    }
    return false
}
```

---

## 7. Model Mapping String Overhead

### 7.1 ParseModelMappingRecord string operations (matcher.go:205-231)

**Severity: Low**

```go
func ParseModelMappingRecord(raw *string) map[string]string {
    // calls splitJSONPairs -> splitJSONPair -> unquoteJSON
    // unquoteJSON calls strings.ReplaceAll 5 times
}
```

`ResolveMappedModel` (matcher.go:300-319) calls `ParseModelMappingRecord` on every channel selection. `unquoteJSON` does 5 `strings.ReplaceAll` calls that each allocate a new string.

**Fix:** Cache the parsed mapping on the `RouteMatch` object after first parse. The model mapping does not change during a route's cached lifetime:

```go
type RouteMatch struct {
    Route           store.TokenRoute
    Channels        []RouteChannelCandidate
    parsedMapping   map[string]string  // cached
    mappingParsed   bool
}

func ResolveMappedModel(match *RouteMatch, requestedModel string) string {
    if !match.mappingParsed {
        match.parsedMapping = ParseModelMappingRecord(match.Route.ModelMapping)
        match.mappingParsed = true
    }
    // ... use cached match.parsedMapping
}
```

### 7.2 unquoteJSON redundant allocations (matcher.go:286-297)

**Severity: Low**

Each `strings.ReplaceAll` call in `unquoteJSON` allocates a new string, even when the replacement string is not present.

**Fix:** Use `strings.NewReplacer` once and reuse it, or scan byte-by-byte and build the output in a `strings.Builder`:

```go
var jsonUnescaper = strings.NewReplacer(
    `\"`, `"`,
    `\\`, `\`,
    `\n`, "\n",
    `\t`, "\t",
    `\/`, "/",
)

func unquoteJSON(s string) string {
    if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
        return jsonUnescaper.Replace(s[1 : len(s)-1])
    }
    return s
}
```

---

## 8. Global Map State with Unlimited Growth

### 8.1 Stable-first global maps (weights.go:329-333)

**Severity: Low (operational risk)**

```go
var (
    stableFirstLastSelectedSiteByKey              = make(map[string]int64)
    stableFirstObservationProgressByKey           = make(map[string]StableFirstObservationProgressState)
    stableFirstObservationSiteCooldownByKey       = make(map[string]int64)
)
```

These three maps are protected by `sync.RWMutex` and have eviction logic (max 1024/1024/4096 entries), but the eviction strategy is "delete a random key" (iterating the map, deleting the first key encountered). Go map iteration order is randomized, so this is effectively random eviction. Under high throughput with many distinct rotation keys, the mutex contention around stale entry cleanup becomes a bottleneck.

**Fix:** Replace with a proper LRU or use `sync.Map` for read-heavy workloads. Alternatively, since rotation keys use the format `routeID:modelName` and there are only `<num_routes> * <num_models>` valid keys, pre-size the maps and skip the eviction logic entirely for non-malicious workloads.

---

## 9. Minor: Endpoint Flow Context Copies

### 9.1 EndpointAttemptContext struct copying (proxy/endpoint_flow.go:186-309)

**Severity: Low**

```go
baseCtx := EndpointAttemptContext{...}     // line 186
timeoutCtx := baseCtx                      // line 203 — shallow copy
timeoutCtx.ErrText = errText               // line 204
failureCtx := baseCtx                      // line 264
failureCtx.ErrText = errText               // line 265
abortCtx := baseCtx                        // line 281
abortCtx.ErrText = errText                 // line 282
downgradeCtx := baseCtx                    // line 296
downgradeCtx.ErrText = errText             // line 297
```

`EndpointAttemptContext` contains a `map[string]string` (Headers field on `BuiltEndpointRequest`). Shallow copies share the map pointer, so this is only a struct copy (~80 bytes on the stack per copy) and not a heap concern. However, the pattern is repetitive.

**Fix:** None needed for GC. For readability, extract a helper:

```go
func (ctx EndpointAttemptContext) withErrText(text string) EndpointAttemptContext {
    ctx.ErrText = text
    return ctx
}
```

---

## 10. GC Pressure Estimation

### Per-request allocation breakdown (non-stream chat completions proxy)

**Unavoidable/per-request:**

| Source | Approx Size | Notes |
|---|---|---|
| `io.ReadAll` (request body) | 1-10 KB | surface.go:63 |
| `json.Unmarshal` into `map[string]any` | 2-15 KB | surface.go:71 — map + values |
| `io.ReadAll` (response body) | 1-50 KB | upstream.go:225 |
| `HeaderMapFromRequest` map | 0.5-1 KB | client_detect.go:14 |
| `Ctx` struct + body map | 0.5-1 KB | surface.go:107 |
| HTTP request/response overhead | 2-5 KB | stdlib |

**Subtotal unavoidable:** ~7-82 KB per request

**Avoidable (current code):**

| Source | Approx Size | Fix Win |
|---|---|---|
| `json.Marshal(forwardBody)` | 1-10 KB | **Remove entirely** |
| `cloneAndSetModel` map | 1-8 KB | **Remove entirely** |
| `bytesReader` string conversion | 1-10 KB | **Remove entirely** |
| `string(bodyBytes)` for failure detect | 1-50 KB | **Eliminate** |
| `time.Now().UTC().Format(RFC3339)` | 40-80 B | **Eliminate** |
| Priority layer map (selector.go:328) | 0.2-1 KB | Slice instead |
| `CalculateWeightedSelection` slices (8x) | 0.5-2 KB | Pool/flatten |
| `BuildStableFirstPoolPlan` maps (4x) | 0.5-2 KB | Inline/small-slice |
| Route allow-set map | 0.2-1 KB | Conditional, low freq |
| JSON parse in failure detect | 2-30 KB | Stream parse |

**Subtotal avoidable:** ~7-114 KB per request (worst case ~50% of total)

**After applying all fixes:**

| Source | Approx Size |
|---|---|
| Unavoidable (same) | ~7-82 KB |
| Remaining avoidable (pooled/cached) | ~1-5 KB |

### GC impact at 1000 req/s

| Scenario | Alloc Rate | GC Frequency (GOGC=100, 100MB heap) |
|---|---|---|
| Current | ~14-196 MB/s | Every 0.5-7 seconds |
| After fixes | ~8-87 MB/s | Every 1-14 seconds |

The primary win is eliminating the `json.Marshal` + `cloneAndSetModel` + `bytesReader string()` triple allocation (Section 1), which alone accounts for 30-50% of avoidable allocations per request.

### Streaming path

For streaming requests, the `accumulated bytes.Buffer` in `handleStreamUpstream` (upstream.go:180) grows with the entire response. The `accumulated.String()` call on line 202 allocates a copy of the full accumulated body for SSE parsing. For long streams (e.g., 100 KB response), this is a significant allocation. Post-stream analysis (`ParseAndAnalyzeSseStream`) should ideally work directly on the buffer.

---

## Summary of Recommendations by Priority

| Priority | Item | File | Savings/req |
|---|---|---|---|
| **P0** | Eliminate json.Marshal in dispatchUpstream + cloneAndSetModel | upstream.go:79-80, surface.go:258 | 3-28 KB |
| **P0** | Replace bytesReader string() with bytes.NewReader | upstream.go:273-278 | 1-10 KB |
| **P1** | Store timestamps as int64 ms instead of ISO strings | selector.go:184, cooldown.go:97, DB schema | Eliminates Format+Parse |
| **P1** | Use streaming JSON parse for DetectProxyFailure | failure_judge.go:78-81 | 2-30 KB |
| **P1** | Replace priority layer map with slice | selector.go:328-343 | 0.2-1 KB + bubble sort |
| **P1** | Cache parsed model mapping on RouteMatch | matcher.go:205, selector.go:242 | Eliminates parse per select |
| **P2** | Pool CalculateWeightedSelection slices | weights.go:99-217 | 0.5-2 KB |
| **P2** | Replace small maps with linear scan | weights.go:436, 598, 701 | 1-3 KB |
| **P2** | Replace pointer-based round-robin sort with index sort | round_robin.go:9-10 | 0.5-1 KB + copy |
| **P3** | strings.Builder for JSON error/write | router.go:81-86 | 0.1 KB |
| **P3** | Replacer for unquoteJSON | matcher.go:286-297 | 0.2 KB |
| **P3** | LRU/ring for stable-first global maps | weights.go:329-333 | Mutex contention |
