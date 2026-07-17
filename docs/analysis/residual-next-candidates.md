# Residual next candidates (post v0.8.16)

**Date**: 2026-07-17  
**Issue**: [#318](https://github.com/TokenDanceLab/metapi-go/issues/318) (this refresh); inventory origin [#290](https://github.com/TokenDanceLab/metapi-go/issues/290)  
**Context**: After Enterprise residual **v0.8.16** (protocol + usage residual **#309–#311**: Gemini thought_signature wire, Responses multi-turn content sanitize, failure `proxy_logs` retain usage).  
**Scope**: inventory only — **no product code** in this document.  
**Map**: [`docs/README.md`](../README.md) · status [`docs/progress/MASTER.md`](../progress/MASTER.md)

## Purpose

Give the next residual / product wave a single honest backlog of high-leverage leftovers. Status labels:

| Status | Meaning |
|--------|---------|
| residual | Explicit non-product or 501/log-only path |
| partial | Related surface exists; upstream intent incomplete |
| present | Shipped enough to drop from active residual queue |
| design-only | Evaluation/spike docs exist; no runtime path |

## Candidates

| ID | Title | Status | Evidence | Recommended wave | Risk |
|----|-------|--------|----------|------------------|------|
| WS-1 | Full Responses WebSocket Codex path | residual | `handler/proxy/responses_ws.go` (426 plain GET / 501 upgrade); `docs/analysis/responses-websocket-residual.md` (#217/#274) | Dedicated Milestone after Codex interop ACs | High protocol + multi-instance sticky interaction |
| STICKY-B | Redis sticky map (option B) | design-only | `proxy/session.go` process-local `stickyBindings`; `docs/analysis/sticky-session-multi-instance-residual.md` (#237/#282); design spike #292 | Only if multi-instance sticky is product-critical and LB pin is unavailable | Hot-path Redis; must fail-open like sharedcount |
| UC-1 | Update-center remote registry / deploy | residual | `scheduler/update_center.go` log-only; admin deploy/rollback/SSE **501**; `docs/analysis/residual-update-center.md` (#283) | Product Milestone with real registry client | Ops safety; no fake updateAvailable |
| TEST-1 | Admin proxy/chat stream + job queue harness | residual | `handler/admin/test.go` stream/jobs **501** / job not-found; sync path aliases forced-channel harness; `docs/analysis/admin-channel-test-harness.md` (#291) | Optional UX polish; sync harness already present | Low if residual stays honest |
| P0-568 | Relay keys force-marked expired | **present** (#298/#301) | `ShouldMarkAccountExpired` + `ReportTokenExpired` ClassExpired guard; bare/generic 401 no longer marks | Done for mark path; novel wording residual only | Residual wording gaps |
| P0-585 | Channel failure cascade poison | **partial** (hardened #299/#302) | Channel-scoped exclude, 429 failover, same-channel timeout budget, isolation tests; residual site/model breaker + production multi-channel load proof | Optional load-test / breaker polish | Medium residual |
| P0-555 | Token usage statistics inaccurate | **present-with-residual** (#300/#311/#319) | Disconnect partial + failure proxy_logs with usage (#311); aggregation projects non-success tokens into `failed_calls` + `total_tokens` (#319 regression). Residual: stream_options policy, media zeros, multi-instance lag, orphan site join — not perfect billing | Residual polish only | Billing/ops trust residual |
| P1-580 | Gemini thought_signature tool history | **present** (#86 transform + #309 proxy wire) | `NormalizeRequest` / OpenAI↔Gemini rebuild + `sanitizeUpstreamJSONBody` on gemini native/cli generateContent; residual: no multi-instance aggregate store | Done for request-side; session re-attach only if product needs | Residual multi-instance only |
| P1-538 | Hermes/Codex multi-turn responses content | **present** (core; #50/#310) | `SanitizeResponsesInputItems` + `sanitizeUpstreamJSONBody` inject/preserve content; honest 400; residual: full Responses→chat conversion + no server store + no WS | Done for HTTP multi-turn content | Residual conversion/store/WS only |
| ROUTE-590 | Route list drag reorder | **present** (v0.8.13) | `token_routes.sort_order` + `PUT /api/routes/reorder` (#284/#288); list `ORDER BY sort_order, id` | Done — matrix row should flip present on next refresh | — |
| RERANK-591 | `/v1/rerank` | present | `handler/proxy/rerank.go` + router | Done (matrix #281) | — |
| SITE-594 | Site max concurrency | present | `proxy/site_concurrency.go` + `sites.max_concurrency` | Done (matrix #281) | — |
| KEY-578 | Per-key outbound proxy | present | `proxy/key_proxy.go` + `downstream_api_keys.proxy_url` | Done (matrix #281) | — |
| REBUILD-588 | Pattern/group rebuild | present | `service/route_rebuild.go` `RebuildRoutesBestEffort` | Done (matrix #281) | — |
| PRICE-496 | Claude cache_ratio defaults | present | `routing/pricing_cost.go` Claude 0.1 / 1.25 | Done (matrix #281) | — |

## Recommended sequencing (v0.8.17+)

1. **Docs honesty** (#318): residual inventory + MASTER after v0.8.16 — done on master.
2. **Observability** (#319 / P0-555): failed `proxy_logs` tokens project into aggregates — this PR; residual only policy/media/lag.
3. **Product surface honesty** (#320): `token_routes.context_length` admin/API round-trip — done on master (metadata-only; no proxy max-token enforce).
4. **Protocol partials** already **present** (P1-580 + P1-538 HTTP multi-turn); residual conversion/store/WS + multi-instance aggregate only.
5. **Product Milestones only with ACs**: WS-1 Codex interop, STICKY-B Redis sticky, UC-1 update-center registry.
6. **Do not** invent shared sticky, WS completions, or updateAvailable without the matching Milestone.

## Explicit non-goals for residual waves

- Fake WS frames or Hijack-then-silent-close.
- Claiming cluster-wide sticky while bindings remain process-local.
- Inventing update-center deploy/rollback success without a registry.
- Returning `success:true` for unimplemented admin stream/job queues.
- Claiming perfect billing accuracy without aggregation proof after #311.

## Links

- Release: [v0.8.16](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.16)
- Matrix: `docs/analysis/original-gap-matrix.md`
- Failover: `docs/analysis/failover-isolation.md`
- MASTER: `docs/progress/MASTER.md`
- Related issues: #274, #282, #283, #290, #291, #292, #298, #299, #300, #309, #310, #311, #318, #319, #320
