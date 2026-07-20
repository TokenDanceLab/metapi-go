# Original parity complete plan (ex-Electron)

**Date**: 2026-07-20  
**Mode**: GITHUB_FULL capable · docs-first until Issues scheduled  
**Scope**: metapi-go vs TokenDance/metapi **server+web** feature parity  
**Out of scope**: Electron desktop shell · MySQL · k3s chart · noise issues  

> **Living index**: [`../progress/MASTER.md`](../progress/MASTER.md) · shortlist [`../analysis/high-value-next.md`](../analysis/high-value-next.md)  
> **Evidence**: gap matrix · residual-next · responses-websocket-residual · 2026-07-20 4-way audit  

---

## 1. Intent (confirmed)

User goal: **原版功能在 Go 侧完整实现**（除 Electron）。

Decision log (2026-07-20):

| Topic | Decision |
|:------|:---------|
| Responses WebSocket | **完整 TS 对等**（upgrade + multi-turn + prewarm + optional upstream Codex wss + HTTP fallback） |
| Multi-instance sticky | **单实例诚实声明** — 不立即做 STICKY-B Redis；文档写清 multi-turn/WS 需单实例或 LB 亲和 |
| Update Center | **隐藏/外置部署** — 不假 `updateAvailable`；部署走 GHCR/ops；UI 诚实 501 或隐藏入口 |

---

## 2. Truth corrections (do not re-schedule as missing)

| Item | Old docs | Verified 2026-07-20 |
|:-----|:---------|:--------------------|
| **#534** bulk account import | matrix `missing` | **present** — `POST /api/accounts` + `accessTokens[]` batch create + UI paste (`handler/admin/accounts.go`, `web/shared/apiKeyBatch.ts`) |
| **#520 / CTX-520** | matrix partial | **present-with-residual** — multi-dialect enforce + TokenRoutes UI |
| **OAuth token refresh** | missing scheduler | **present** — `scheduler/oauth_refresh.go` (#251) |
| **Sub2API scheduler** | scan-only residual | **present** — parses extraConfig, due filter (300s lead), `balance.RefreshBalance` |
| **Electron** | “missing buttons” feel | **non-goal** product shape |

---

## 3. Must-do backlog for “原版完整（无 Electron）”

### Wave A — Product keys / protocol (parallel lanes)

| ID | Title | Status | Effort | Lane | AC sketch |
|:---|:------|:-------|:-------|:-----|:----------|
| **#547** | Per-downstream-key weight scalar | partial | M | keys | Schema + selector multiply + DownstreamKeys UI; tests on weighted pick |
| **#584** | Site custom header override priority | **present** | M | protocol | `custom_headers_override_request_headers` + ApplyCustomHeadersWithOptions + Sites UI |
| **#579** | Multi-credential bind on one downstream key | **present** | L | keys | allowed_site_ids + allowed_credential_refs allow-list; empty unrestricted; exclusions compose |
| **#514** | Multi-tier context → channel switch | **present** | L | routing | same-model multi context_length routes; estimate-driven tightest-fit |

### Wave B — Reliability honesty

| ID | Title | Status | Effort | Notes |
|:---|:------|:-------|:-------|:------|
| **P0-585** | Cascade production e2e | partial | M | Unit load-proof already; **do not flip present without prod e2e** |
| **P0-555** | Media zeros / multi-instance lag | present-with-residual | M | Keep residual honesty until ACs land |

### Wave C — Responses WebSocket (full TS parity)

| Phase | Scope | Effort | Depends |
|:------|:------|:-------|:--------|
| **C0** | Keep 426/501 residual until C1 ships | — | **done** (C1 shipped) |
| **C1** | Real upgrade + auth + turn-state echo + `response.create` single-turn + **in-process HTTP SSE→WS bridge** | L | **present** 2026-07-21 (`coder/websocket`) |
| **C2** | Multi-turn merge/append + prewarm on wire + per-message quota | L | C1 |
| **C3** | Codex upstream `wss` runtime + session store + `previous_response_id` recovery + dial→HTTP fallback | XL | **present** 2026-07-21 |

| **C4** | Docs: multi-instance honesty (single instance or LB pin); no STICKY-B unless reopened | S | C1+ |

**TS SSOT**: `metapi/src/server/routes/proxy/responsesWebsocket.ts` + `proxy-core/runtime/codexWebsocket*`  
**Go residual SSOT**: `handler/proxy/responses_ws.go` · `docs/analysis/responses-websocket-residual.md`  

**Reuse**: `HandleResponses` → `PrepareCtx` → `dispatchUpstream`; sticky process-local only.  
**Dep**: introduce one WS library only when C1 starts (prefer `coder/websocket` or `gorilla/websocket`).  
**Forbidden**: Hijack-silent-close · fake `response.completed` · claim multi-instance multi-turn without pin.

### Wave D — Update Center honesty

| Action | Detail |
|:-------|:-------|
| Default | Hide admin Update Center deploy UX **or** keep honest 501; deploy via GHCR + ops pin |
| Non-goal | Invent registry client / fake `updateAvailable` without product AC |

### Wave E — Runtime probes (live accounts)

| ID | Action |
|:---|:-------|
| **#571** | Codex OAuth gpt-5.5 live probe |
| **#577** | AnyRouter check-in/models live verify |

---

## 4. Sequencing (dependency)

```
Docs truth (#534/#520) ──► MASTER / high-value-next / matrix
        │
        ├─► A: #547 ──► #579
        │     #584 (parallel)
        │     #514 (own milestone)
        ├─► B: P0-585 e2e ∥ P0-555 residual ACs
        ├─► C: WS C1 → C2 → C3 → C4 (docs)
        └─► D: UC hide/external
Ops pin 0.8.45 ──► user-visible UI (not a code gate for A/C)
```

**Default execution order if scheduling Issues:**

1. Doc truth + MASTER schedule  
2. **#547** then **#584** (small product wins)  
3. **WS C1** start in parallel with **#579** design AC  
4. **WS C2/C3** after C1 green  
5. **#514** + reliability e2e as capacity allows  
6. UC hide/external (docs + UI honesty) anytime  

---

## 5. Delivery batches (when coding)

| Batch | Issues (planned) | Integration focus |
|:------|:-----------------|:------------------|
| **B-DOC** | matrix + MASTER + residual docs | no product code |
| **B-KEYS** | #547 · #584 · (#579 if AC ready) | schema + admin + routing tests |
| **B-WS1** | WS C1 (+ C2 if same PR risk OK) | proxy handler + bridge tests |
| **B-WS2** | WS C3 runtime | new `proxy/runtime/*` package |
| **B-ROUTE** | #514 | routing + tests |
| **B-REL** | P0-585 e2e harness / P0-555 AC | honesty tests only unless AC expands |

---

## 6. Acceptance for “parity complete (ex-Electron)”

- [x] Wave A items present or closed with explicit non-goal AC  
- [x] Wave C C1–C3 present (2026-07-21); C4 sticky docs residual  
- [ ] Wave C: Codex CLI can complete multi-turn over WS with honesty under single-instance/LB-pin docs  
- [ ] Wave D: no fake update-center success path  
- [ ] P0-585/555: status labels match evidence (no silent present)  
- [ ] Electron remains non-goal in STATE / formal-readiness  

---

## 7. Adaptive control

| Phase | Tasks (approx) | annotate | replan | rescope |
|:------|---------------:|---------:|-------:|--------:|
| A keys | 4 | 1 | 2 | 3 |
| B rel | 2 | 1 | 1 | 2 |
| C WS | 4 | 1 | 2 | 3 |
| D UC | 1 | 1 | 1 | 1 |

Drift storage: update this file + MASTER “Current Status” each session; GitHub Milestone adaptive YAML when Issues created.
