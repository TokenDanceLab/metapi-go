# Residual: Responses WebSocket transport (#217)

**Date**: 2026-07-17 · **program update**: 2026-07-20  
**Issue**: [#217](https://github.com/TokenDanceLab/metapi-go/issues/217)  
**Lane**: p85 / honest residual → **product scheduled** full TS parity  
**SSOT code**: `handler/proxy/responses_ws.go`, `handler/proxy/responses.go`, `app/app.go`  
**Program plan**: [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md) Wave C (C1→C3)  
**Shortlist**: [`high-value-next.md`](./high-value-next.md) WS-1

> **C1+C2+C3 shipped (2026-07-21)**: plain GET **426**; upgrade → real `coder/websocket` + auth + multi-turn HTTP SSE→WS bridge + local prewarm + per-message managed-key quota + model policy.  
> C3 runtime present: Codex upstream wss + session store + dial→HTTP fallback; multi-instance sticky still single-instance honesty (no STICKY-B).  
> **User decision 2026-07-20**: **完整 TS 对等** (not invent-frames). Sticky remains **process-local / single-instance honesty** (no STICKY-B now).

## Goal

Stop silent capability theater around Responses WebSocket:

- `EnsureResponsesWebsocketTransport` must be **called from app boot**, not only defined.
- Do **not** fake WS completions or Hijack-then-silent-close.
- **Until C1**: residual stays stdlib-only with clear HTTP status semantics (426 / 501).
- **From C1**: real upgrade + auth + single-turn bridge + tests; multi-turn/upstream wss in C2/C3 per plan.

## C1 shipped (2026-07-21)

| Item | Status |
|------|--------|
| Library | `github.com/coder/websocket` |
| Upgrade path | `HandleResponsesGet426` / alias → `HandleResponsesWebsocket` |
| Auth | ProxyAuth middleware + pre-Accept `GetProxyAuth` guard (401 if missing) |
| Turn-state | `x-codex-turn-state` captured/passthrough on bridge inject |
| Single-turn | `response.create` → in-process `HandleResponses` SSE→WS |
| Prewarm | `generate=false` first create → local created+completed (zero usage) |
| Residual | multi-instance pin honesty only (no STICKY-B) |


## C2 shipped (2026-07-21)

| Item | Status |
|------|--------|
| Multi-turn merge | last input + last assistant output + new input |
| Incremental path | client `previous_response_id` on `response.create` (no force-merge); bridge mode header `incremental` |
| Per-message quota | `auth.ConsumeManagedKeyRequest` after normalize; ProxyAuth **does not** bill WS upgrade |
| Model policy | `IsModelAllowedByPolicy` each turn |
| Status string | `c3_codex_upstream_wss` |
| Residual | multi-instance pin; optional capability probe refinement |


## C3 shipped (2026-07-21)

| Item | Status |
|------|--------|
| Upstream runtime | `handler/proxy/codex_ws_runtime.go` (dial/reuse, terminal wait, previous_response_id store) |
| Capability probe | platform=`codex` + `CodexUpstreamWebsocketEnabled` + optional extraConfig `websockets` |
| Session store | process-local response id map (TTL ≈ sticky TTL, min 5m; max 10k keys) |
| Fallback | dial/upgrade with **zero events** → HTTP SSE bridge (no fake terminals) |
| Wire | `responses_ws.go` `tryCodexUpstreamWSS` before bridgeHTTP |
| Status string | `c3_codex_upstream_wss` |
| Residual | multi-instance pin honesty only |

## Boot wiring

```
cmd/server
  app.New(cfg, router)
  a.Start()
    · newHTTPServer(...)
    · a.WireResponsesWebsocketTransport()
        · proxyhandler.EnsureResponsesWebsocketTransport(server, WebSocketConfig{})
```

`WireResponsesWebsocketTransport` is a thin composition-root call on `*app.App`. Residual registration sets an in-process flag (`ResponsesWebsocketTransportRegistered`) and logs:

| Field | Value |
|-------|--------|
| `status` | `c3_codex_upstream_wss` |
| plain GET | `426 Upgrade Required` |
| upgrade attempt | `101` (auth OK) / `401` (no auth) |
| `doc` | `docs/analysis/responses-websocket-residual.md` |

## HTTP surface (honest)

| Request | Status | Meaning |
|---------|--------|---------|
| `GET /v1/responses` (no Upgrade) | **426** | Client must use WebSocket upgrade for GET; HTTP GET is not a responses read API |
| `GET /v1/responses` with `Upgrade: websocket` + `Connection: upgrade` | **101** (auth OK) / **401** (no auth) | C1: `coder/websocket` accept + message loop; unauthenticated upgrades refused before Accept |
| `GET /responses` / alias compact | same 426 / 101(auth) after alias resolve | Unknown alias still **404** |
| `POST /v1/responses` | existing HTTP responses path | Unchanged (not part of this residual) |

JSON error shape remains OpenAI-ish:

```json
{"error":{"message":"...","type":"invalid_request_error"}}
```

## What is **not** claimed

1. **Live WS server** — upgrade via `coder/websocket`; default path HTTP SSE→WS bridge; C3 may dial Codex upstream wss when flagged.
2. **No fake completions for real turns** — only local prewarm (`generate=false` first create) synthesizes created+completed; real turns only forward events.
3. **Codex profile capability ≠ transport readiness** — `SupportsResponsesWebsocketIncremental` is client detection, not a claim that every channel has upstream wss.
4. **Single-instance honesty** — process-local previous_response_id / socket reuse; multi-instance multi-turn needs LB pin (no STICKY-B).

## Product implementation (supersedes “out of scope” for #217 residual-only)

Scheduled under parity program Wave C — see plan SSOT:

1. Keep boot call site: `App.WireResponsesWebsocketTransport` → `EnsureResponsesWebsocketTransport` (evolve residual → real transport).
2. C1: real upgrade after auth; turn-state echo; `response.create` single-turn; in-process HTTP SSE→WS bridge; reuse `PrepareCtx` / `dispatchUpstream`.
3. C2: multi-turn merge/append + prewarm on wire + per-message quota.
4. C3: Codex upstream `wss` + session store + `previous_response_id` + dial→HTTP fallback.
5. C4 docs: multi-instance honesty (single instance or LB pin); no STICKY-B unless reopened.
6. **Forbidden always**: Hijack-silent-close · fake `response.completed` · claim multi-instance multi-turn without pin · treat `SupportsResponsesWebsocketIncremental` as transport ready.

## Tests

| Area | Coverage |
|------|----------|
| Residual registration | `EnsureResponsesWebsocketTransport` sets registered flag |
| App boot wire | `App.WireResponsesWebsocketTransport` marks registered |
| Plain GET | `HandleResponsesGet426` → 426 |
| Upgrade GET | auth OK → 101; no auth → 401; plain GET → 426 |
| Alias GET | 426 / 501 / 404 unchanged for unknown |
| Upgrade detection | `IsWebsocketUpgradeRequest` |
| C3 runtime | URL/headers/body/store/continuation helpers (`codex_ws_runtime_test.go`) |
| Status string | `c3_codex_upstream_wss` |

Verify:

```bash
go test ./handler/proxy ./app ./cmd/server -count=1 -run 'Responses|Websocket|WS'
```

---

## Product path evaluation (#274)

**Date**: 2026-07-17  
**Issue**: [#274](https://github.com/TokenDanceLab/metapi-go/issues/274) (extends [#217](https://github.com/TokenDanceLab/metapi-go/issues/217))

This section evaluates how (and whether) to graduate from residual 426/501 to a real Responses WebSocket transport. It does **not** change runtime behavior.

### Options

| Option | Approach | Pros | Cons | Verdict for v0.8.x |
|--------|----------|------|------|--------------------|
| **A. Stay residual** | Keep plain GET **426**, upgrade **501**, boot still calls `EnsureResponsesWebsocketTransport` | Honest operators; zero protocol risk; no new deps; POST `/v1/responses` already serves non-WS clients | Codex CLI clients that require WS cannot use this surface | **Recommended default** |
| **B. stdlib Hijacker + minimal RFC6455** | After auth, Hijack `GET /v1/responses`, hand-roll frames | No third-party WS module | Frame/ping/close correctness burden; easy to ship silent half-broken sockets | Only if dedicated milestone + interop tests |
| **C. gorilla/websocket or nhooyr** | Add dependency, upgrade mux, message loop | Mature framing | New supply-chain surface; still need Codex semantics on top | Acceptable later if ACs require WS |
| **D. Full Codex multi-turn WS** | Message loop, turn-state echo, pre-warm `generate=false`, HTTP fallback from open socket | TS-parity product path | Large scope; sticky/session interaction; multi-instance sticky residual | Separate Milestone, not residual wave |

### Recommendation (v0.8.x residual waves)

**Remain Option A** until a dedicated Milestone ships with:

1. Codex client interop tests (at least single-turn upgrade + one response stream).
2. Explicit auth on upgrade (reuse proxy admission / API key path).
3. Clear error mapping (no Hijack-then-silent-close theater).
4. Decision recorded for multi-instance sticky + WS session affinity (see sticky residual docs).

Reasons:

- `POST /v1/responses` already covers non-WS Responses traffic.
- Residual registration is already explicit at boot (no silent missing wire).
- Full Codex WS (Option D) is product work, not a drive-by residual.

### MVP scope if later greenlit

If a future Milestone greenlights WS:

1. **In**: authenticated upgrade; single-turn Responses over WS; refuse multi-turn / unknown message types with structured WS or HTTP error; metrics/logs for upgrade reject vs accept.
2. **Out of first MVP**: multi-turn turn-state machine, pre-warm synthesis on wire, HTTP fallback from an open socket, cluster-wide sticky for WS.
3. **Still forbidden**: fake `response.completed` frames without upstream work; claiming Codex profile capability flags as transport readiness.

### Links

- Residual code: `handler/proxy/responses_ws.go`
- Boot wire: `app` → `WireResponsesWebsocketTransport`
- Issues: #217 (residual registration), #274 (this evaluation)

