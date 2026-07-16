# TTFT / first-byte soft scoring for channel selection (#113)

**Date:** 2026-07-17  
**Lane:** M-COMPETE learn / L4  
**SSOT code:** `routing/runtime_health.go`, `routing/weights.go`, `routing/router.go`, `handler/proxy/upstream.go`, `proxy/channel_selection.go`

## Goal

Use observed first-token / first-byte latency (TTFT) as a **soft scoring** signal in channel selection so slow-but-alive channels are deprioritized, without replacing Fibonacci cooldown / breakers.

Peers: AxonHub FTTL/TTFT scoring, LiteLLM latency-based routing.

## Units

| Surface | Unit | Notes |
| --- | --- | --- |
| `PROXY_FIRST_BYTE_TIMEOUT_SEC` | seconds | Existing operator config (#38) |
| Observed first-byte on success | **milliseconds** | Header-arrival delta from request start |
| Runtime health EMA fields | **milliseconds** | `LatencyEMAMs` (E2E) + `FirstByteLatencyEMAMs` (TTFT) |
| Soft penalty constants | dimensionless | Multiplier ∈ `[min, 1]` |

## Signal path

```
upstream headers arrive
  → firstByteLatencyMsObs (ms)
  → recordUpstreamSuccess(..., latencyMs, &firstByteLatencyMsObs, ...)
  → TokenRouter.RecordSuccess(..., firstByteLatencyMs *float64)
  → RecordSiteRuntimeSuccess(site, e2eLatency, model, firstByte)
  → SiteRuntimeHealthState.FirstByteLatencyEMAMs (EMA α=0.3)
  → GetRuntimeHealthMultiplier() multiplies soft TTFT factor
  → CalculateWeightedSelection contribution *= runtimeMultiplier
```

Streaming and non-streaming success paths both observe header arrival as TTFT. Probe success may omit TTFT (`nil`) so it does not invent samples.

`proxy_logs.first_byte_latency_ms` stores the observed header latency separately from end-to-end `latency_ms`.

## Soft weight (non-breaking default)

```
ttftPenaltyRatio = clamp((firstByteEMA - 800) / 10000, 0, 1)
ttftFactor       = 1 - ttftPenaltyRatio * 0.12
```

Constants (`routing/runtime_health.go`):

| Constant | Value | Role |
| --- | --- | --- |
| `SiteRuntimeFirstByteLatencyBaselineMs` | 800 | Below this → no TTFT penalty |
| `SiteRuntimeFirstByteLatencyWindowMs` | 10000 | Span to full soft penalty |
| `SiteRuntimeMaxFirstByteLatencyPenalty` | **0.12** | Max 12% weight reduction |
| `SiteRuntimeFirstByteLatencyEMAAlpha` | 0.3 | Same EMA α as overall latency |

Overall latency soft penalty remains independent (max 35%). Combined:

```
multiplier = clamp(failureFactor * e2eLatencyFactor * ttftFactor, 0.08, 1)
```

## Compatibility / non-goals

- **No double-penalize with cooldown:** TTFT only updates on **success**. Failures still use penalty + breaker paths only.
- **No breaker from TTFT:** Slow first-byte never opens site/model breakers.
- **Missing samples neutral:** `nil` / non-positive first-byte EMA → factor `1.0`.
- **Does not replace** Fibonacci channel cooldown, sticky sessions, load multipliers, or cost weights.
- **Does not add** a new operator env flag in this slice; weight is a small fixed default. A future learn can expose config if operators need to disable/tune.

## Tests

- `TestResolveFirstByteLatencyFactor_*`
- `TestGetRuntimeHealthMultiplier_TTFTSoftOnly`
- `TestRecordSiteRuntimeSuccess_UpdatesFirstByteEMA`
- `TestRecordSiteRuntimeSuccess_TTFTDoesNotOpenBreaker`
- `TestCalculateWeightedSelection_PrefersLowerTTFT`

## Residual

- True first-**token** (SSE content) latency is not measured yet; header first-byte is the available production signal from #38.
- Runtime health (including TTFT EMA) remains process-local; multi-instance shared state is a separate future concern, same as other site runtime health fields.
