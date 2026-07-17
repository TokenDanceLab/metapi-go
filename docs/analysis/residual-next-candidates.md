# Residual next candidates (post v0.8.27 / M38)

**Date**: 2026-07-18  
**Issue**: inventory origin [#290](https://github.com/TokenDanceLab/metapi-go/issues/290); honesty refresh [#334](https://github.com/TokenDanceLab/metapi-go/issues/334); trail #318 / #329 + v0.8.18 product + v0.8.19 residual; post-M35 honesty [#397](https://github.com/TokenDanceLab/metapi-go/issues/397); post-v0.8.26 honesty [#410](https://github.com/TokenDanceLab/metapi-go/issues/410); post-v0.8.27 honesty [#418](https://github.com/TokenDanceLab/metapi-go/issues/418)  
**Context**: **v0.8.27 shipped** (#407 opaque monitor session, #408 OldToken constant-time, #409 CTX-520 Claude max_tokens reject, #410 residual honesty; PRs #411–#414). Board was clean after M37. **Milestone 38 opened** with SSRF/client harden board **#416–#418**. Prior **v0.8.26**: #397–#400 (PRs #401–#404/#406). Prior **v0.8.25**: #382–#384. M35 closed: #388 review synthesis, **#389/#396** endpoint early reject, **#390/#395** multi-route list regression. Original P0 #405/#565/#515 already-correct in code.  
**Scope**: inventory only — **no product code** in this document.  
**Map**: [`docs/README.md`](../README.md) · status [`docs/progress/MASTER.md`](../progress/MASTER.md)  
**M35 review synthesis**: [`enterprise-review-m35.md`](./enterprise-review-m35.md) (#388) — ranked P0/P1/P2 backlog after multi-lane residual review (historical; #389/#390 done)  
**Active wave**: Milestone 38 / **v0.8.28** board **#416–#418** (SSRF/client harden; latest release remains **v0.8.27** until release gate)

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
| P0-555 | Token usage statistics inaccurate | **present-with-residual** (#300/#311/#319/#345/#350/#400) | Disconnect partial + failure proxy_logs with usage (#311); aggregation projects non-success tokens into `failed_calls` + `total_tokens` (#319 regression); OpenAI chat + legacy completions stream `stream_options.include_usage` inject (#345/#350); stream-end `slog.Warn` when include_usage requested but no usable tokens extracted (#400 — never invents counts). Residual: provider-ignored flag, media zeros, multi-instance lag, orphan site join — not perfect billing | Residual polish only | Billing/ops trust residual |
| P1-580 | Gemini thought_signature tool history | **present** (#86 transform + #309 proxy wire) | `NormalizeRequest` / OpenAI↔Gemini rebuild + `sanitizeUpstreamJSONBody` on gemini native/cli generateContent; residual: no multi-instance aggregate store | Done for request-side; session re-attach only if product needs | Residual multi-instance only |
| P1-538 | Hermes/Codex multi-turn responses content | **present** (core; #50/#310) | `SanitizeResponsesInputItems` + `sanitizeUpstreamJSONBody` inject/preserve content; honest 400; residual: full Responses→chat conversion + no server store + no WS | Done for HTTP multi-turn content | Residual conversion/store/WS only |
| ROUTE-590 | Route list drag reorder | **present** (v0.8.13) | `token_routes.sort_order` + `PUT /api/routes/reorder` (#284/#288); list `ORDER BY sort_order, id` | Done | — |
| RERANK-591 | `/v1/rerank` | present | `handler/proxy/rerank.go` + router | Done (matrix #281) | — |
| SITE-594 | Site max concurrency | present | `proxy/site_concurrency.go` + `sites.max_concurrency` | Done (matrix #281) | — |
| KEY-578 | Per-key outbound proxy | present | `proxy/key_proxy.go` + `downstream_api_keys.proxy_url` | Done (matrix #281) | — |
| REBUILD-588 | Pattern/group rebuild | present | `service/route_rebuild.go` `RebuildRoutesBestEffort` | Done (matrix #281) | — |
| PRICE-496 | Claude cache_ratio defaults | present | `routing/pricing_cost.go` Claude 0.1 / 1.25 | Done (matrix #281) | — |
| CTX-520 | Route contextLength admin + models wire + max_tokens reject | **present-with-residual** (OpenAI #320/#327/#399; Claude #409) | Admin CRUD (#320) + OpenAI `/v1/models` prefers positive route `context_length` (max per exposed id) (#327) + proxy rejects OpenAI chat/completions (and legacy completions) `max_tokens` above positive selected-route `context_length` with honest 400 (#399/#404) + Claude `/v1/messages` same reject when positive context_length (#409/#412). Residual: further dialects only with ACs; null/non-positive context_length still skips enforce; no silent clamp | Further dialect polish only with ACs | OpenAI + Claude enforce present; other dialects residual |
| SEC-MONITOR | Monitor session cookie embeds live AUTH_TOKEN | **present-with-residual** (opaque session #407/#414; logout residual **active** #417) | Opaque HMAC-SHA256 monitor session; `ensureMonitorAuth` constant-time compare; cookie scoped `Path=/monitor-proxy/` so capture cannot become full admin bearer. **Active residual:** admin logout / session clear may not set `Max-Age=0` for `meta_monitor_auth` with matching Path → M38 **#417** | M38 #417 logout cookie clear; rotate/invalidate on auth_token change only with product AC | Logout residual active; session token present |
| SEC-AUTH-TIMING | Admin token change OldToken compare | **present** (#408/#411) | `handler/admin/auth_settings.go` uses `subtle.ConstantTimeCompare` on equal-length normalized bytes; mismatched lengths rejected without leaking timing (parity with AdminAuth middleware) | Done for token-change path | Residual other non-constant compares only if product AC |
| SEC-KEY | Admin downstream-keys plaintext redaction | **present** (#355/#361) | `redactDownstreamKeySecret` on list/summary/overview; `key` omitted, `keyMasked` only | Done for admin list surface | Residual: other admin dumps if any |
| SEC-HDR | custom_headers deny-list | **present** (#356/#364) | Shared `platform.ApplyCustomHeaders` / deny-list; Bearer after custom on upstream path | Done | Residual novel header names only |
| SEC-REDIR | RuntimeExecutor CheckRedirect SSRF | **present-with-residual** (RuntimeExecutor #357/#360; bare clients residual **active** #416) | `rejectCrossOriginRedirect` on RuntimeExecutor client + platform DoWithProxy. **Active residual after #357:** bare probe/harness/default clients still use Go default redirect policy — `channel_health_probe.go` / `channel_test_harness.go` bare `&http.Client{}` when no site proxy, and `defaultUpstreamClient` (90s, no CheckRedirect) → M38 **#416** share rejectCrossOriginRedirect (or stronger) + regression tests public origin 302 → 169.254/loopback | **M38 P0 #416** — share CheckRedirect on probe/harness/defaultUpstreamClient | **P0** bare-client redirect SSRF residual |
| REL-SOFT | Weighted soft-filter empty → next priority | **present** (#358/#362) | Weighted path skips soft-empty priority layer and tries next; failover-isolation note | Done for weighted soft-filter | P0-585 empty-filter fallback residual separate |
| SEC-ADMIN | Remaining admin secret surfaces | **present-with-residual** (#367/#372) | Account list redacts accessToken/apiToken + passwordCipher strip; token list drops join secrets. Residual: create/update/rebind may still echo once; intentional credential export | Done for list/overview | Residual intentional export / create-once |
| REL-SOFT-RR | RR/stable_first soft-filter priority demotion | **present** (#368/#370) | RR/stable_first/least_* share priority-layer strict soft-filter demotion with weighted | Done | Global full-set fallback when all layers soft-empty (P0-585 residual) |
| SEC-ROUTE | Routes/search admin secret dumps | **present** (#375/#378) | Route channel `account` uses `routeChannelAccountPublic` (masked); search accounts/tokens redacted | Done | Residual: create-once / export paths intentional |
| SEC-SITEURL | Site URL metadata/link-local SSRF | **present** (#376/#379) | `IsForbiddenSiteTargetURL` blocks 169.254/16, IPv6 link-local, metadata hostnames on create/update/endpoint upsert | Done | RFC1918/localhost intentionally allowed |
| SEC-HTTPURL | IsValidHTTPURL / externalCheckin metadata | **present** (#382/#385) | `IsValidHTTPURL` rejects metadata/link-local class via `IsForbiddenSiteTargetURL`; externalCheckin uses hardened check | Done | RFC1918/localhost intentionally allowed |
| PERF-ROUTES | GET /api/routes N+1 channel queries | **present** (#383/#386; test residual closed #390/#395) | Single channels JOIN for listed routes, group in memory; response shape + #375 redact unchanged; multi-route list regression + secret redact covered by `TestListRoutes_MultiRouteBatchChannelLoadAndRedaction` | Done for routes list batch-load | Residual other admin list N+1 only if product AC |
| SEC-ENDPOINT | Site API endpoint admin normalize + service validator | **present** (#389/#396 + #398/#403) | Admin `normalizeAPIEndpointsInput` rejects forbidden targets with clear 400 before upsert; `IsValidAPIEndpointURL` itself rejects metadata/link-local (parity with `IsValidHTTPURL` / `IsForbiddenSiteTargetURL`) so any caller is safe by default | Done for admin early-reject + base validator parity | RFC1918/localhost intentionally allowed |
| M35-REVIEW | Multi-lane residual review synthesis | **present** (docs #388) | `docs/analysis/enterprise-review-m35.md` ranked P0/P1/P2; #389/#390 follow-ons closed on master | Historical M35 pointer | Synthesis only |

## Recommended sequencing (v0.8.28+)

1. **Shipped in v0.8.27**: #407–#410 (PRs #411–#414) — opaque monitor session · OldToken constant-time · CTX-520 Claude max_tokens reject · residual honesty. Prior v0.8.26: #397–#400. M35/M36/M37 closed.
2. **Active M38 board (#416–#418)** — Enterprise residual SSRF/client harden **v0.8.28** (latest tag stays **v0.8.27** until release gate):
   1. **#416 SEC-REDIR bare clients (P0)**: share `rejectCrossOriginRedirect` (or equivalent) on probe / harness / `defaultUpstreamClient`; regression tests for public-origin 302 → 169.254/loopback.
   2. **#417 SEC-MONITOR logout residual (P1)**: clear `meta_monitor_auth` with matching `Path=/monitor-proxy/` on admin logout / session clear (`Max-Age=0`).
   3. **#418 docs**: this residual honesty pass + MASTER M38 pointer (no product code).
3. **SEC-AUTH-TIMING** fully **present**. **SEC-REDIR** stays **present-with-residual** until #416 lands (RuntimeExecutor shipped; bare clients active). **SEC-MONITOR** stays **present-with-residual** until #417 lands (opaque session shipped; logout cookie clear active). **CTX-520** stays **present-with-residual** (OpenAI + Claude max_tokens reject shipped; further dialects / null-context residual). **P0-555** stays **present-with-residual** (warn shipped; provider-ignore / media / lag / orphan join residual).
4. **P0-585** residual notes unchanged until load-proof / breaker ACs land.
5. **Optional product later**: P0-585 load-proof / site-model breaker; deeper billing polish; further dialect context_length enforce (dedicated ACs only).
6. **Product Milestones only with ACs**: WS-1 Codex interop, STICKY-B Redis sticky, UC-1 update-center registry.
7. **Do not** invent shared sticky, WS completions, or updateAvailable without the matching Milestone.

## Explicit non-goals for residual waves

- Fake WS frames or Hijack-then-silent-close.
- Claiming cluster-wide sticky while bindings remain process-local.
- Inventing update-center deploy/rollback success without a registry.
- Returning `success:true` for unimplemented admin stream/job queues.
- Claiming perfect billing accuracy without aggregation proof after #311.
- Claiming all-dialect proxy max-token enforcement from `contextLength` without a dedicated product AC (OpenAI chat/completions after #399; Claude `/v1/messages` after #409; further dialects need ACs).
- Returning full downstream API keys on admin list/summary/overview after #355.
- Allowing custom_headers to override Authorization/Host/hop-by-hop after #356.
- Following cross-origin/private RuntimeExecutor redirects after #357 (bare probe/harness/defaultUpstreamClient residual via #416).
- Embedding live `AUTH_TOKEN` in monitor cookies after #407 (logout cookie clear residual via #417).
- Non-constant-time compares on admin token-change paths after #408.

## Links

- Release: [v0.8.27](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.27) · prior [v0.8.26](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.26) · next residual board **v0.8.28+** (no tag until release gate)
- Milestone: [Enterprise residual SSRF/client harden v0.8.28](https://github.com/TokenDanceLab/metapi-go/milestone/38) · board **#416–#418**
- Matrix: `docs/analysis/original-gap-matrix.md`
- Failover: `docs/analysis/failover-isolation.md`
- M35 review synthesis: `docs/analysis/enterprise-review-m35.md` (#388)
- MASTER: `docs/progress/MASTER.md`
- Related issues: #416, #417, #418, #407, #408, #409, #410, #411, #412, #413, #414, #397, #398, #399, #400, #401, #402, #403, #404, #406, #382, #383, #384, #375, #376, #377, #274, #282, #283, #290, #291, #292, #298, #299, #300, #309, #310, #311, #318, #319, #320, #327, #328, #329, #334, #335, #336, #345, #346, #350, #351, #355, #356, #357, #358, #359, #366, #367, #368, #388, #389, #390, #391, #395, #396
