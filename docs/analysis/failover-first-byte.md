# Failover first-byte timeout units (#38 / upstream #387)

**Date:** 2026-07-17  
**Lane:** feature / FE-FAILOVER  
**SSOT code:** `proxy/executor.go`, `proxy/endpoint_flow.go`, `handler/proxy/upstream.go`, `app/proxy_upstream.go`, `config/config.go`

## Units

| Surface | Unit | Notes |
| --- | --- | --- |
| Env / operator config `PROXY_FIRST_BYTE_TIMEOUT_SEC` | **seconds** | Parsed into `config.ProxyFirstByteTimeoutSec` |
| Internal dispatch / observation | **milliseconds** | `proxy.FirstByteTimeoutMs(sec) = sec * 1000` when `sec > 0` |
| `ExecuteEndpointFlowInput.FirstByteTimeoutMs` | **milliseconds** | Passed into `DispatchRequest(..., firstByteTimeoutMs)` |
| `RuntimeExecutor.WithObservedFirstByte` / `DoWithObservedFirstByte` | **milliseconds** | Timer-based first-byte (response headers) observation |

Original TS: `firstByteTimeoutMs = proxyFirstByteTimeoutSec * 1000` (`chatSurface.ts`).

`0` disables observed first-byte timeout.

## Production path (live wiring)

Before #38, production `handler/proxy/upstream.go` single-shot `sendUpstreamRequest` and never:

1. converted sec → ms for observed first-byte, or
2. iterated multi-protocol endpoint candidates.

Now:

1. `PROXY_FIRST_BYTE_TIMEOUT_SEC` → `proxy.FirstByteTimeoutMs` → `sendUpstreamRequest(..., firstByteTimeoutMs)`.
2. Chat-family paths use `proxy.ResolveEndpointCandidates` ordered list (`chat` / `messages` / `responses`).
3. First-byte timeout or protocol-mismatch (`ShouldDowngradeToNextEndpoint`) continues to the next candidate when `DISABLE_CROSS_PROTOCOL_FALLBACK` is false.
4. Intermediate protocol/timeout misses **do not** call `RecordFailure` so healthy siblings / the same channel are not poisoned by a single wrong protocol.
5. Terminal failure on the last candidate (or when cross-protocol fallback is disabled) records failure and may channel-failover via existing retry loop.

`DisableCrossProtocolFallback` (`DISABLE_CROSS_PROTOCOL_FALLBACK`) limits the candidate list to the primary endpoint only.

## Residual

- Full body transform between protocols (OpenAI chat ↔ Anthropic messages ↔ responses) is **not** part of this fix; path fallback reuses the same JSON body. Protocol transforms remain FE-PROTOCOL / transformer work.
- Multipart surfaces stay single-path (no multi-protocol rewrite).
- Site API endpoint pool / multi-base-URL iteration remains separate from this cross-protocol path list.
- Overall HTTP client timeout in `app.ConfigureProxyUpstream` is a safety ceiling (`max(90s, first-byte*2)`), not the observed first-byte unit.
