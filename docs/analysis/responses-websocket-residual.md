# Residual: Responses WebSocket transport (#217)

**Date**: 2026-07-17  
**Issue**: [#217](https://github.com/TokenDanceLab/metapi-go/issues/217)  
**Lane**: p85 / honest residual  
**SSOT code**: `handler/proxy/responses_ws.go`, `handler/proxy/responses.go`, `app/app.go`

## Goal

Stop silent capability theater around Responses WebSocket:

- `EnsureResponsesWebsocketTransport` must be **called from app boot**, not only defined.
- Do **not** invent a full Codex multi-turn WebSocket product or fake WS completions.
- Prefer stdlib residual (no `gorilla/websocket` / `nhooyr` dependency) with clear HTTP status semantics.

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
| `status` | `not_implemented` |
| plain GET | `426 Upgrade Required` |
| upgrade attempt | `501 Not Implemented` |
| `doc` | `docs/analysis/responses-websocket-residual.md` |

## HTTP surface (honest)

| Request | Status | Meaning |
|---------|--------|---------|
| `GET /v1/responses` (no Upgrade) | **426** | Client must use WebSocket upgrade for GET; HTTP GET is not a responses read API |
| `GET /v1/responses` with `Upgrade: websocket` + `Connection: upgrade` | **501** | Residual: transport not implemented; no Hijack, no frames, no fake completions |
| `GET /responses` / alias compact | same 426 / 501 after alias resolve | Unknown alias still **404** |
| `POST /v1/responses` | existing HTTP responses path | Unchanged (not part of this residual) |

JSON error shape remains OpenAI-ish:

```json
{"error":{"message":"...","type":"invalid_request_error"}}
```

## What is **not** claimed

1. **No live WS server** — `EnsureResponsesWebsocketTransport` does not install `server.ConnState`, does not Hijack, and does not accept WebSocket handshakes.
2. **No fake completions on wire** — helpers such as `SynthesizePrewarmResponsePayloads` / `ParseResponsesWSMessage` exist for a future runtime and unit tests only; residual handlers never emit them to a client socket.
3. **Codex profile capability ≠ transport readiness** — `SupportsResponsesWebsocketIncremental` on the Codex CLI profile describes **client detection** (what Codex clients expect). It does **not** mean metapi-go currently serves incremental WS responses.
4. **No new websocket module in go.mod** — residual stays stdlib-only.

## Optional future scaffolding (out of scope for #217)

When implementing a real transport:

1. Keep boot call site: `App.WireResponsesWebsocketTransport` → `EnsureResponsesWebsocketTransport`.
2. Prefer `http.Hijacker` on `GET /v1/responses` after auth, or a dedicated upgrade mux — still no silent success without a protocol.
3. Message loop, turn-state echo, pre-warm `generate=false`, and HTTP fallback from an open socket are product work, not residual.

## Tests

| Area | Coverage |
|------|----------|
| Residual registration | `EnsureResponsesWebsocketTransport` sets registered flag |
| App boot wire | `App.WireResponsesWebsocketTransport` marks registered |
| Plain GET | `HandleResponsesGet426` → 426 |
| Upgrade GET | residual → 501, message mentions residual doc |
| Alias GET | 426 / 501 / 404 unchanged for unknown |
| Upgrade detection | `IsWebsocketUpgradeRequest` |

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

