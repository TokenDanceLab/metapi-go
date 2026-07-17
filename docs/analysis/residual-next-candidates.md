# Residual next candidates (post M35 / v0.8.25)

**Date**: 2026-07-18  
**Issue**: inventory origin [#290](https://github.com/TokenDanceLab/metapi-go/issues/290); honesty refresh [#334](https://github.com/TokenDanceLab/metapi-go/issues/334); trail #318 / #329 + v0.8.18 product + v0.8.19 residual; post-M35 honesty [#397](https://github.com/TokenDanceLab/metapi-go/issues/397)  
**Context**: **post-M35** — **v0.8.25 shipped** (#382 IsValidHTTPURL metadata harden, #383 routes N+1 batch, #384 residual honesty). M35 closed: #388 review synthesis, **#389/#396** endpoint early reject, **#390/#395** multi-route list regression. Prior **v0.8.24**: #375–#377. Original P0 #405/#565/#515/#409 already-correct in code.  
**Scope**: inventory only — **no product code** in this document.  
**Map**: [`docs/README.md`](../README.md) · status [`docs/progress/MASTER.md`](../progress/MASTER.md)  
**M35 review synthesis**: [`enterprise-review-m35.md`](./enterprise-review-m35.md) (#388) — ranked P0/P1/P2 backlog after multi-lane residual review (historical; #389/#390 done)  
**Active wave**: Milestone 36 / **v0.8.26** board **#397–#400**

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
| P0-585 | Channel failure cascade poison | **partial** (hardened #299/#302; honesty #336) | **Shipped:** channel-scoped `excludeChannelIDs`, non-usage-limit cooldown write isolation, 429 failover, same-channel timeout budget, multi-channel same-site conductor + routing isolation tests. **Still open:** site/model breaker after 3 transient fails (intentional fleet filter), credential-scoped usage-limit multi-channel cool, empty-filter fallback, **no production multi-channel load proof**. Detail: `docs/analysis/failover-isolation.md` §P0-585 honesty | Optional load-test / breaker product AC only; soft-filter next-priority present via #358 | Medium residual — do not claim #585 present |
| P0-555 | Token usage statistics inaccurate | **present-with-residual** (#300/#311/#319/#345/#350) | Disconnect partial + failure proxy_logs with usage (#311); aggregation projects non-success tokens into `failed_calls` + `total_tokens` (#319 regression); OpenAI chat + legacy completions stream `stream_options.include_usage` inject (#345/#350). Residual: provider-ignored flag, media zeros, multi-instance lag, orphan site join — not perfect billing | Residual polish only | Billing/ops trust residual |
| P1-580 | Gemini thought_signature tool history | **present** (#86 transform + #309 proxy wire) | `NormalizeRequest` / OpenAI↔Gemini rebuild + `sanitizeUpstreamJSONBody` on gemini native/cli generateContent; residual: no multi-instance aggregate store | Done for request-side; session re-attach only if product needs | Residual multi-instance only |
| P1-538 | Hermes/Codex multi-turn responses content | **present** (core; #50/#310) | `SanitizeResponsesInputItems` + `sanitizeUpstreamJSONBody` inject/preserve content; honest 400; residual: full Responses→chat conversion + no server store + no WS | Done for HTTP multi-turn content | Residual conversion/store/WS only |
| ROUTE-590 | Route list drag reorder | **present** (v0.8.13) | `token_routes.sort_order` + `PUT /api/routes/reorder` (#284/#288); list `ORDER BY sort_order, id` | Done | — |
| RERANK-591 | `/v1/rerank` | present | `handler/proxy/rerank.go` + router | Done (matrix #281) | — |
| SITE-594 | Site max concurrency | present | `proxy/site_concurrency.go` + `sites.max_concurrency` | Done (matrix #281) | — |
| KEY-578 | Per-key outbound proxy | present | `proxy/key_proxy.go` + `downstream_api_keys.proxy_url` | Done (matrix #281) | — |
| REBUILD-588 | Pattern/group rebuild | present | `service/route_rebuild.go` `RebuildRoutesBestEffort` | Done (matrix #281) | — |
| PRICE-496 | Claude cache_ratio defaults | present | `routing/pricing_cost.go` Claude 0.1 / 1.25 | Done (matrix #281) | — |
| CTX-520 | Route contextLength admin + models wire | **present-with-residual** (#320 + #327) | Admin CRUD (#320) + OpenAI `/v1/models` prefers positive route `context_length` (max per exposed id) (#327). Residual: no proxy max-token enforce; Claude models path has no context_length field | Optional enforce Milestone only with ACs | Metadata vs enforcement |
| SEC-KEY | Admin downstream-keys plaintext redaction | **present** (#355/#361) | `redactDownstreamKeySecret` on list/summary/overview; `key` omitted, `keyMasked` only | Done for admin list surface | Residual: other admin dumps if any |
| SEC-HDR | custom_headers deny-list | **present** (#356/#364) | Shared `platform.ApplyCustomHeaders` / deny-list; Bearer after custom on upstream path | Done | Residual novel header names only |
| SEC-REDIR | RuntimeExecutor CheckRedirect SSRF | **present** (#357/#360) | `rejectCrossOriginRedirect` on RuntimeExecutor client | Done | Residual site-URL SSRF validation only if product AC |
| REL-SOFT | Weighted soft-filter empty → next priority | **present** (#358/#362) | Weighted path skips soft-empty priority layer and tries next; failover-isolation note | Done for weighted soft-filter | P0-585 empty-filter fallback residual separate |
| SEC-ADMIN | Remaining admin secret surfaces | **present-with-residual** (#367/#372) | Account list redacts accessToken/apiToken + passwordCipher strip; token list drops join secrets. Residual: create/update/rebind may still echo once; intentional credential export | Done for list/overview | Residual intentional export / create-once |
| REL-SOFT-RR | RR/stable_first soft-filter priority demotion | **present** (#368/#370) | RR/stable_first/least_* share priority-layer strict soft-filter demotion with weighted | Done | Global full-set fallback when all layers soft-empty (P0-585 residual) |
| SEC-ROUTE | Routes/search admin secret dumps | **present** (#375/#378) | Route channel `account` uses `routeChannelAccountPublic` (masked); search accounts/tokens redacted | Done | Residual: create-once / export paths intentional |
| SEC-SITEURL | Site URL metadata/link-local SSRF | **present** (#376/#379) | `IsForbiddenSiteTargetURL` blocks 169.254/16, IPv6 link-local, metadata hostnames on create/update/endpoint upsert | Done | RFC1918/localhost intentionally allowed |
| SEC-HTTPURL | IsValidHTTPURL / externalCheckin metadata | **present** (#382/#385) | `IsValidHTTPURL` rejects metadata/link-local class via `IsForbiddenSiteTargetURL`; externalCheckin uses hardened check | Done | RFC1918/localhost intentionally allowed |
| PERF-ROUTES | GET /api/routes N+1 channel queries | **present** (#383/#386; test residual closed #390/#395) | Single channels JOIN for listed routes, group in memory; response shape + #375 redact unchanged; multi-route list regression + secret redact covered by `TestListRoutes_MultiRouteBatchChannelLoadAndRedaction` | Done for routes list batch-load | Residual other admin list N+1 only if product AC |
| SEC-ENDPOINT | Site API endpoint admin normalize vs service upsert | **present** (#389/#396) | Admin `normalizeAPIEndpointsInput` rejects `IsForbiddenSiteTargetURL` targets with clear 400 before upsert; service path still defense-in-depth | Done for admin early-reject parity | Residual: base `IsValidAPIEndpointURL` scheme-only → M36 #398 validator parity |
| M35-REVIEW | Multi-lane residual review synthesis | **present** (docs #388) | `docs/analysis/enterprise-review-m35.md` ranked P0/P1/P2; #389/#390 follow-ons closed on master | Historical M35 pointer; active board M36 #397–#400 | Synthesis only |

## Recommended sequencing (v0.8.26+)

1. **Shipped in v0.8.25**: #382–#384 (PRs #385/#386/#387) — IsValidHTTPURL metadata harden · routes N+1 batch · residual honesty. Prior v0.8.24: #375–#377. Release docs flip #391 closed with tag **v0.8.25**.
2. **M35 closed**: #388 review synthesis; **#389/#396** endpoint early-reject present; **#390/#395** multi-route list regression present (test residual closed).
3. **Active M36 board (#397–#400)**: residual honesty flip (#397); **#398** `IsValidAPIEndpointURL` metadata/link-local validator parity; **#399** CTX-520 optional max_tokens reject when route `context_length` set; **#400** P0-555 stream missing-usage warn after include_usage inject.
4. **P0-555** stays **present-with-residual**. **CTX-520** / **P0-585** residual notes unchanged until #399 / load-proof ACs land.
5. **Optional product later**: P0-585 load-proof / site-model breaker; deeper billing polish (dedicated ACs only).
6. **Product Milestones only with ACs**: WS-1 Codex interop, STICKY-B Redis sticky, UC-1 update-center registry.
7. **Do not** invent shared sticky, WS completions, or updateAvailable without the matching Milestone.

## Explicit non-goals for residual waves

- Fake WS frames or Hijack-then-silent-close.
- Claiming cluster-wide sticky while bindings remain process-local.
- Inventing update-center deploy/rollback success without a registry.
- Returning `success:true` for unimplemented admin stream/job queues.
- Claiming perfect billing accuracy without aggregation proof after #311.
- Claiming proxy max-token enforcement from `contextLength` without a dedicated product AC.
- Returning full downstream API keys on admin list/summary/overview after #355.
- Allowing custom_headers to override Authorization/Host/hop-by-hop after #356.
- Following cross-origin/private RuntimeExecutor redirects after #357.

## Links

- Release: [v0.8.25](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.25) · prior [v0.8.24](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.24) · next residual board **v0.8.26+** (no tag until release gate)
- Matrix: `docs/analysis/original-gap-matrix.md`
- Failover: `docs/analysis/failover-isolation.md`
- M35 review synthesis: `docs/analysis/enterprise-review-m35.md` (#388)
- MASTER: `docs/progress/MASTER.md`
- Related issues: #382, #383, #384, #375, #376, #377, #274, #282, #283, #290, #291, #292, #298, #299, #300, #309, #310, #311, #318, #319, #320, #327, #328, #329, #334, #335, #336, #345, #346, #350, #351, #355, #356, #357, #358, #359, #366, #367, #368, #388, #389, #390, #391, #395, #396, #397, #398, #399, #400
