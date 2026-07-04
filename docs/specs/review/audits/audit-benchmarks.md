# MetAPI Go Benchmark Baseline

**Date:** 2026-07-05  
**Machine:** Intel Core i7-14700HX (20 logical cores) — single CPU (`-cpu=1`)  
**Go version:** go1.x (windows/amd64)  
**Command:** `go test -bench=. -benchmem -count=5 -cpu=1`  
**Packages:** `./proxy/`, `./transform/canonical/`, `./handler/proxy/`, `./routing/`  
**Coverage:** routing/selector, proxy/endpoint_flow, transform/canonical, handler/proxy/chat

---

## Table of Contents

1. [Top-10 Slowest Benchmarks](#top-10-slowest-benchmarks)
2. [Top-10 Highest Allocations](#top-10-highest-allocations)
3. [Full Results by Package](#full-results-by-package)
   - [routing/selector](#routingselector)
   - [proxy/endpoint_flow](#proxyendpoint_flow)
   - [transform/canonical](#transformcanonical)
   - [handler/proxy/chat](#handlerproxychat)
4. [Summary Statistics](#summary-statistics)

---

## Top-10 Slowest Benchmarks

Sorted by mean ns/op (5-run average).

| Rank | Benchmark | Package | ns/op | B/op | allocs/op |
|------|-----------|---------|-------|------|-----------|
| 1 | BenchmarkCanonicalEnvelope_JSONRoundtrip | transform/canonical | 38,536 | 5,720 | 79 |
| 2 | BenchmarkCanonicalEnvelope_JSONUnmarshal | transform/canonical | 29,429 | 4,376 | 64 |
| 3 | BenchmarkCalculateWeightedSelection_10Candidates | routing/selector | 23,935 | 24,008 | 36 |
| 4 | BenchmarkHandleChatCompletions_Stream | handler/proxy/chat | 22,045 | 10,880 | 73 |
| 5 | BenchmarkHandleChatCompletions_NonStream | handler/proxy/chat | 21,328 | 11,860 | 95 |
| 6 | BenchmarkCalculateWeightedSelection_5Candidates | routing/selector | 11,148 | 11,864 | 29 |
| 7 | BenchmarkCanonicalContentPart_JSONRoundtrip | transform/canonical | 10,280 | 2,560 | 30 |
| 8 | BenchmarkCanonicalEnvelope_JSONMarshal | transform/canonical | 8,205 | 1,344 | 15 |
| 9 | BenchmarkOpenAIBody_ToCanonical_WithTools | transform/canonical | 8,031 | 2,408 | 44 |
| 10 | BenchmarkHandleChatCompletions_Unauthorized | handler/proxy/chat | 8,015 | 7,064 | 28 |

**Key observations:**

- JSON serialization dominates the slowest list, with JSON roundtrip (marshal+unmarshal) being ~2x slower than the next non-JSON benchmark.
- The weighted-selection algorithm scales poorly: going from 5 to 10 candidates costs ~2.1x time and ~2.0x memory.
- Chat handler benchmarks appear in both the slowest and highest-alloc lists, driven by the full HTTP handler lifecycle (request parsing, context preparation, dispatch).
- `BenchmarkOpenAIBody_ToCanonical_WithTools` at #9 is notable because tool deserialization into canonical form involves multiple map allocations per tool function.

---

## Top-10 Highest Allocations

Sorted by mean B/op (bytes allocated per operation).

| Rank | Benchmark | Package | B/op | ns/op | allocs/op |
|------|-----------|---------|------|-------|-----------|
| 1 | BenchmarkCalculateWeightedSelection_10Candidates | routing/selector | 24,008 | 23,935 | 36 |
| 2 | BenchmarkCalculateWeightedSelection_5Candidates | routing/selector | 11,864 | 11,148 | 29 |
| 3 | BenchmarkHandleChatCompletions_NonStream | handler/proxy/chat | 11,860 | 21,328 | 95 |
| 4 | BenchmarkHandleChatCompletions_Stream | handler/proxy/chat | 10,880 | 22,045 | 73 |
| 5 | BenchmarkHandleChatCompletions_Unauthorized | handler/proxy/chat | 7,064 | 8,015 | 28 |
| 6 | BenchmarkCanonicalEnvelope_JSONRoundtrip | transform/canonical | 5,720 | 38,536 | 79 |
| 7 | BenchmarkOpenAIBody_Roundtrip | transform/canonical | 5,672 | 7,750 | 48 |
| 8 | BenchmarkCanonicalEnvelope_JSONUnmarshal | transform/canonical | 4,376 | 29,429 | 64 |
| 9 | BenchmarkCalculateWeightedSelection_StableFirst | routing/selector | 3,136 | 4,922 | 25 |
| 10 | BenchmarkCanonical_ToOpenAIBody | transform/canonical | 3,128 | 4,219 | 37 |

**Key observations:**

- `CalculateWeightedSelection` is the memory hog: 10 candidates produce 24 KB/op, driven by per-candidate detail structs (`WeightedDetail`), sorted slices, and string formatting.
- Chat handler benchmarks allocate 7-12 KB/op regardless of stream/non-stream. The common path includes `PrepareCtx`, JSON body parsing, and dispatch machinery.
- The OpenAIBody roundtrip (to-canonical + back) allocates 5.7 KB from building the intermediate `CanonicalRequestEnvelope` with all its nested maps and slices.
- Even the unauthorized fast-path allocates 7 KB due to `PrepareCtx` validation and error-response formatting.

---

## Full Results by Package

### routing/selector

All benchmarks defined in `routing/weights_test.go`.

| Benchmark | ns/op (mean) | B/op | allocs/op |
|-----------|--------------|------|-----------|
| BenchmarkCalculateWeightedSelection_10Candidates | 23,935 | 24,008 | 36 |
| BenchmarkCalculateWeightedSelection_5Candidates | 11,148 | 11,864 | 29 |
| BenchmarkCalculateWeightedSelection_StableFirst (2 candidates) | 4,922 | 3,136 | 25 |
| BenchmarkCalculateWeightedSelection_SingleCandidate | 2,078 | 1,328 | 10 |
| BenchmarkResolveChannelRuntimeLoadMultiplier (5 snapshots) | 193 | 0 | 0 |
| BenchmarkEffectiveUnitCost_Catalog | 51 | 8 | 1 |
| BenchmarkEffectiveUnitCost_Observed | 26 | 0 | 0 |
| BenchmarkMakeCandidate | 0.5 | 0 | 0 |

**Scaling:** Weighted selection is approximately O(N*log N) due to sorting. Cost per candidate in the 5-candidate case is ~2,230 ns/op and ~2,373 B/op per candidate; in the 10-candidate case it is ~2,394 ns/op and ~2,401 B/op per candidate — roughly linear per-candidate with minor overhead from sorting.

### proxy/endpoint_flow

All benchmarks defined in `proxy/endpoint_flow_test.go`.

| Benchmark | ns/op (mean) | B/op | allocs/op |
|-----------|--------------|------|-----------|
| BenchmarkExecuteEndpointFlow_AllExhausted (3 endpoints) | 1,350 | 480 | 12 |
| BenchmarkExecuteEndpointFlow_Recovery | 1,085 | 568 | 12 |
| BenchmarkExecuteEndpointFlow_TimeoutFallback (2 endpoints) | 801 | 480 | 9 |
| BenchmarkExecuteEndpointFlow_ProxyURL | 433 | 216 | 5 |
| BenchmarkSummarizeUpstreamError_Long | 499 | 248 | 3 |
| BenchmarkSummarizeUpstreamError | 426 | 152 | 3 |
| BenchmarkExecuteEndpointFlow_Success | 402 | 208 | 4 |
| BenchmarkBuildUpstreamURL | 88 | 48 | 1 |

**Analysis:** The endpoint flow is lightweight per iteration. The success path takes ~400 ns with only 208 B allocated. The recovery path costs ~2.7x more (1,085 ns) because it involves the recovery callback and context-mutation path. The all-exhausted path (3 endpoints, each returning 500) takes ~1,350 ns — confirming the loop iterates cleanly with minimal per-iteration overhead. `BuildUpstreamURL` at ~88 ns is a good candidate for hot-path micro-optimization.

### transform/canonical

Benchmarks defined in `transform/canonical/types_test.go` and `transform/canonical/openai_bridge_test.go`.

| Benchmark | ns/op (mean) | B/op | allocs/op |
|-----------|--------------|------|-----------|
| BenchmarkCanonicalEnvelope_JSONRoundtrip | 38,536 | 5,720 | 79 |
| BenchmarkCanonicalEnvelope_JSONUnmarshal | 29,429 | 4,376 | 64 |
| BenchmarkCanonicalContentPart_JSONRoundtrip (3 parts) | 10,280 | 2,560 | 30 |
| BenchmarkCanonicalEnvelope_JSONMarshal | 8,205 | 1,344 | 15 |
| BenchmarkOpenAIBody_ToCanonical_WithTools | 8,031 | 2,408 | 44 |
| BenchmarkOpenAIBody_Roundtrip | 7,750 | 5,672 | 48 |
| BenchmarkCanonical_ToOpenAIBody | 4,219 | 3,128 | 37 |
| BenchmarkOpenAIBody_Roundtrip_WithImages | 3,971 | 2,840 | 26 |
| BenchmarkOpenAIBody_ToCanonical | 3,082 | 2,544 | 11 |
| BenchmarkOpenAIBody_ToCanonical_WithImages | 1,585 | 960 | 5 |
| BenchmarkNormalizeCanonicalContinuation_NonNil | 123 | 64 | 1 |
| BenchmarkNormalizeCanonicalContinuation_Empty | 116 | 64 | 1 |
| BenchmarkCreateCanonicalRequestEnvelope | 32 | 0 | 0 |

**Analysis:** JSON operations are the clear bottleneck in the canonical transform layer. The roundtrip (marshal+unmarshal) costs 38.5 us with 79 allocations. The `encoding/json` package's reflection-based approach is the root cause. The OpenAIBody-to-canonical conversion with tools (8,031 ns/44 allocs) is driven by recursive map parsing for tool function definitions. The factory function (`CreateCanonicalRequestEnvelope`) is allocation-free at 32 ns because it simply populates a struct — ideal for hot-path usage.

### handler/proxy/chat

Benchmarks defined in `handler/proxy/chat_test.go`. Uses `config.Set` from `TestMain` and a stub proxy server.

| Benchmark | ns/op (mean) | B/op | allocs/op |
|-----------|--------------|------|-----------|
| BenchmarkHandleChatCompletions_Stream | 22,045 | 10,880 | 73 |
| BenchmarkHandleChatCompletions_NonStream | 21,328 | 11,860 | 95 |
| BenchmarkHandleChatCompletions_Unauthorized | 8,015 | 7,064 | 28 |

**Analysis:** The full chat-completions handler path takes ~21-22 us per request. Stream and non-stream are within 3% of each other in wall time, but non-stream has more allocations (95 vs 73) due to the JSON response body assembly path. The unauthorized fast-path (8 us / 7 KB) represents the authentication+prepare-overhead baseline — about 37% of the full handler cost. The stub proxy used in testing returns small canned responses; production overhead with real upstream dispatch would be dominated by network I/O.

---

## Summary Statistics

| Package | # Benchmarks | Slowest (ns/op) | Fastest (ns/op) | Max Allocs (B/op) | Max allocs/op |
|---------|-------------|-----------------|-----------------|-------------------|---------------|
| routing/selector | 8 | 23,935 | 0.5 | 24,008 | 36 |
| proxy/endpoint_flow | 8 | 1,350 | 88 | 568 | 12 |
| transform/canonical | 13 | 38,536 | 32 | 5,720 | 79 |
| handler/proxy/chat | 3 | 22,045 | 8,015 | 11,860 | 95 |
| **Overall** | **32** | **38,536** | **0.5** | **24,008** | **95** |

### Near-Term Optimization Opportunities

1. **JSON (highest impact):** Switch from `encoding/json` to `github.com/goccy/go-json` or `github.com/bytedance/sonic` for `transform/canonical` to cut JSON marshal/unmarshal times by 40-60%.

2. **WeightedSelection:** Pre-allocate the `[]WeightedDetail` slice with `make([]WeightedDetail, 0, len(candidates))` to reduce the 24 KB/op (10 candidates) allocation budget. Pool intermediate maps and strings used in score computation.

3. **Handler allocation:** The 7-12 KB/op in the chat handler is largely from `PrepareCtx` and body parsing. Consider pooling the `Ctx` struct and pre-allocating the JSON decoder buffer.

4. **OpenAIBody roundtrip:** The tool-to-canonical conversion path allocates 44 allocations for a single tool definition. Flattening the map-walking logic with type-assertion caches could reduce this.

### Baseline Metadata

- **Date established:** 2026-07-05
- **Commit:** (current working tree)
- **Files with benchmarks:**
  - `routing/weights_test.go` — 8 benchmarks
  - `proxy/endpoint_flow_test.go` — 8 benchmarks
  - `transform/canonical/types_test.go` — 7 benchmarks
  - `transform/canonical/openai_bridge_test.go` — 6 benchmarks
  - `handler/proxy/chat_test.go` — 3 benchmarks
- **Re-run command:** `go test -bench=. -benchmem -count=5 -cpu=1 ./proxy/ ./transform/canonical/ ./handler/proxy/ ./routing/`
