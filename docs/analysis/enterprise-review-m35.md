# Enterprise multi-lane residual review synthesis (M35)

**Date**: 2026-07-18  
**Issue**: [#388](https://github.com/TokenDanceLab/metapi-go/issues/388)  
**Scope**: inventory / prioritization only — **no product code** in this document  
**Baseline**: `origin/master` at **v0.8.25** (#382 IsValidHTTPURL metadata harden, #383 routes channel batch-load, #384 residual honesty; release docs via closed [#391](https://github.com/TokenDanceLab/metapi-go/issues/391)). Prior **v0.8.24**: #375–#377.

## Method honesty

This synthesis is intentionally **evidence-first**, not fleet-journal-complete.

| Input | Used how |
|:------|:---------|
| `docs/analysis/residual-next-candidates.md` | Primary residual queue + status labels |
| `docs/progress/MASTER.md` | Active Milestone / release pointer |
| `CHANGELOG.md` (through v0.8.25) | Shipped narrative; do not invent unreleased product claims |
| `docs/analysis/failover-isolation.md` | P0-585 shipped-vs-residual honesty |
| Code spot-checks on residual high-value paths | Confirm residual / partial / present claims |
| Closed Issues #389 / #390 / #391; PRs #395 / #396 | M35 follow-ons done on master; v0.8.25 release flip complete |

**Not claimed as complete inputs:** full multi-lane WF journals (security / backend SUPER / frontend UX / reliability-perf / docs-hygiene). Some parallel review lanes may have incomplete journals (rate limits). Absence of a lane journal is **not** treated as “no findings”; residual docs + code evidence still drive ranking.

## Status labels (same vocabulary as residual inventory)

| Status | Meaning |
|--------|---------|
| residual | Explicit non-product or 501/log-only path |
| partial | Related surface exists; upstream intent incomplete |
| present | Shipped enough to drop from active residual queue |
| present-with-residual | Core path shipped; named residual still honest |
| design-only | Evaluation/spike docs exist; no runtime path |

## Executive summary

1. **Security residual waves v0.8.22–v0.8.25 largely landed** (admin secret redaction surfaces, custom_headers deny-list, RuntimeExecutor redirect guard, site URL metadata block, routes/search secret redact, IsValidHTTPURL metadata harden).
2. **Milestone 34 / v0.8.25 shipped** (#382/#383 product + #384 residual honesty + closed #391 release flip). SEC-HTTPURL is **present**.
3. **M35 product follow-ons closed on master**: **SEC-ENDPOINT present** via #389/#396 (admin `normalizeAPIEndpointsInput` early reject); **PERF-ROUTES fully present** via #383/#386 + test residual closed #390/#395.
4. **Post-M35 residual board is M36 (#397–#400)** — not inventing WS/sticky/update-center. Highest-value remaining product slices: **#398** validator parity on base `IsValidAPIEndpointURL`, **#399** optional CTX-520 max_tokens reject, **#400** P0-555 missing-usage warn; optional reliability load-proof with dedicated ACs.
5. **Do not claim product for**: Responses WebSocket Codex path, Redis sticky Option B, update-center remote deploy/rollback, perfect billing accuracy, proxy max-token enforcement from `contextLength` until #399 lands.

## P0 — act next (security / honesty / release)

| ID | Finding | Evidence | Status | Recommended next Issue |
|----|---------|----------|--------|------------------------|
| SEC-ENDPOINT | Admin site `apiEndpoints` normalize path early-rejects metadata/link-local via `IsForbiddenSiteTargetURL` (merged #389/#396). Service `UpsertSiteAPIEndpoints` remains defense-in-depth. Residual: base `IsValidAPIEndpointURL` still scheme-only for other callers → M36 **#398**. | `handler/admin/sites.go` `normalizeAPIEndpointsInput` + `IsForbiddenSiteTargetURL`; tests in `sites_create_endpoints_test.go`; closed [#389](https://github.com/TokenDanceLab/metapi-go/issues/389) / PR [#396](https://github.com/TokenDanceLab/metapi-go/pull/396) | **present** (admin early-reject done) | Done #389/#396; optional validator parity **#398** |
| SEC-HTTPURL | `IsValidHTTPURL` now rejects metadata/link-local (merged #382). externalCheckin admin paths call it. Residual inventory flipped present with v0.8.25 / #391. | `service/site_endpoint_service.go` `IsValidHTTPURL`; `handler/admin/sites.go` externalCheckin checks; tests `TestIsValidHTTPURL_RejectsMetadata` | **present** | Done with v0.8.25 |
| PERF-ROUTES | `GET /api/routes` batch-loads channels once (merged #383). Multi-route assignment + secret-redact regression landed (#390/#395). | `handler/admin/token_routes.go` `listRoutes` batch SELECT + `channelsByRoute`; `TestListRoutes_MultiRouteBatchChannelLoadAndRedaction`; closed [#390](https://github.com/TokenDanceLab/metapi-go/issues/390) / PR [#395](https://github.com/TokenDanceLab/metapi-go/pull/395) | **present** (impl + regression test) | Done #383/#386 + #390/#395 |
| REL-DOCS | MASTER/CHANGELOG/residual flipped to **v0.8.25** via closed #391. M35 synthesis (#388) + product follow-ons #389/#390 closed. Post-M35 honesty board is M36 **#397–#400**. | `docs/progress/MASTER.md`; residual inventory; closed [#391](https://github.com/TokenDanceLab/metapi-go/issues/391); open [#397](https://github.com/TokenDanceLab/metapi-go/issues/397) | **present** (release docs); M36 residual honesty open | Keep MASTER slim; land #397 then #398–#400 |

## P1 — high-value residual (optional product / reliability polish)

| ID | Finding | Evidence | Status | Recommended next Issue / wave |
|----|---------|----------|--------|-------------------------------|
| P0-585 | Channel-scoped failover / cooldown isolation shipped; **not** fleet-wide “no cascade”. Residual: site/model breaker after 3 transient fails, credential-scoped usage-limit multi-channel cool, global empty-filter full-set fallback, **no production multi-channel load proof**. | `docs/analysis/failover-isolation.md` §P0-585 honesty; `proxy/conductor.go`; `routing/router.go` `RecordFailure`; soft-filter demotion #358/#368 | **partial** | Optional load-test / breaker policy Milestone — only with ACs; do **not** flip to present without load proof |
| P0-555 | Disconnect/failure usage + aggregation + OpenAI chat/completions `stream_options.include_usage` shipped. Residual: provider-ignored usage flag, media zeros, multi-instance lag, orphan site join — **not perfect billing**. | residual inventory P0-555; `scheduler/usage_aggregation.go`; CHANGELOG v0.8.20/v0.8.21 | **present-with-residual** | Residual polish Issues only with billing ACs |
| CTX-520 | Admin + OpenAI `/v1/models` contextLength wire present. Residual: **no proxy max-token enforcement**; Claude models path has no context_length field. | `handler/proxy/models.go`; residual inventory CTX-520 | **present-with-residual** | Dedicated enforce Milestone only if product wants hard limits |
| SEC-ADMIN | List/overview redaction present for accounts/tokens/keys/routes/search. Residual: create/update/rebind may still echo secrets once; intentional credential export paths. | residual SEC-ADMIN / SEC-KEY / SEC-ROUTE; CHANGELOG v0.8.22–v0.8.24 | **present-with-residual** | Only if a new dump surface appears; do not break intentional export |
| TEST-1 | Admin sync channel test harness present; stream + async job queue remain honest **501**. | `handler/admin/test.go` `writeNotImplementedResidual`; `docs/analysis/admin-channel-test-harness.md` | **residual** (stream/jobs) / sync harness **present** | Optional UX polish; keep 501 honest |

## P2 — known residuals / design-only (do **not** invent product)

| ID | Finding | Evidence | Status | Gate to productize |
|----|---------|----------|--------|--------------------|
| WS-1 | Responses WebSocket Codex path not implemented: plain GET **426**, upgrade **501**. No fake frames / Hijack-then-silent-close. | `handler/proxy/responses_ws.go`; `docs/analysis/responses-websocket-residual.md` (#217/#274) | **residual** | Dedicated Milestone + Codex interop ACs only |
| STICKY-B | Sticky bindings are **process-local** `stickyBindings`. Multi-instance without LB pin loses sticky preference. Redis Option B is design-only. | `proxy/session.go`; `docs/analysis/sticky-session-multi-instance-residual.md`; `docs/analysis/sticky-redis-design.md` (#237/#282/#292) | **design-only** (product path process-local) | Only if multi-instance sticky is product-critical and LB affinity unavailable; fail-open required |
| UC-1 | Update-center remote registry / deploy/rollback/SSE are honest **501** / log-only scheduler. No fake `updateAvailable` success. | `handler/admin/update_center.go`; `scheduler/update_center.go`; `docs/analysis/residual-update-center.md` (#283) | **residual** | Product Milestone with real registry client + ops safety ACs |
| P1-580 / P1-538 | Request-side Gemini thought_signature + Responses multi-turn content sanitize present. Residual: multi-instance aggregate store; full Responses→chat conversion; no server store; no WS. | residual inventory; transform/proxy sanitize paths | **present** (core) with named residual only | Session re-attach / conversion only if product needs |
| FE-CI | Historical frontend CI EnvironmentTeardownError flake hardened in v0.8.11-era notes; not re-audited as open P0 here without a fresh failing CI signal. | CHANGELOG #266/#268 | **present** (historical harden) | Reopen only on new CI flake evidence |

## Lane notes (compact)

### Security

- **Shipped (do not re-open as residual bugs):** SEC-KEY, SEC-HDR, SEC-REDIR, SEC-SITEURL, SEC-ROUTE; SEC-HTTPURL product on master (#382); SEC-ENDPOINT admin early-reject on master (#389/#396).
- **Still actionable (M36):** base `IsValidAPIEndpointURL` metadata/link-local parity (#398) — scheme-only helper residual after admin normalize path is present.
- **Honesty:** create-once credential echo and intentional export are residual-by-design, not list-surface leaks.

### Backend / reliability

- Failover channel isolation is **hardened and tested**; P0-585 remains **partial** because site/model breaker + global empty-filter fallback + missing production multi-channel load proof are intentional / open.
- Soft-filter priority demotion for weighted / RR / stable_first is **present** (#358/#368).
- Routes list N+1 kill is **present** (#383/#386); multi-route regression test residual **closed** (#390/#395).

### Frontend / UX

- No new frontend product residual invented in this synthesis.
- Admin stream/job harness remains 501-honest (TEST-1); sync forced-channel harness is the supported operator path.
- Sticky/WS/update-center UI must not claim cluster-wide or remote-deploy success.

### Docs / hygiene

- Residual inventory + MASTER are the day-to-day residual SSOT (`docs/README.md` map).
- Release flip for v0.8.25 landed via closed #391; this synthesis is inventory-only (no product code).
- This file is the M35 ranked backlog pointer; residual-next-candidates remains the living queue.

## Recommended next Milestone Issues (do not invent WS/sticky/update-center)

| Priority | Issue | Role | Notes |
|---------:|------:|------|-------|
| Done | [#389](https://github.com/TokenDanceLab/metapi-go/issues/389) / PR [#396](https://github.com/TokenDanceLab/metapi-go/pull/396) | security | Admin endpoint early-reject metadata/link-local (**closed** / present) |
| Done | [#390](https://github.com/TokenDanceLab/metapi-go/issues/390) / PR [#395](https://github.com/TokenDanceLab/metapi-go/pull/395) | reliability/test | Multi-route list regression for batch channel load (**closed** / present) |
| Done docs | [#391](https://github.com/TokenDanceLab/metapi-go/issues/391) | docs/release | CHANGELOG v0.8.25 + MASTER + residual status flip (**closed** with tag) |
| Done docs | [#388](https://github.com/TokenDanceLab/metapi-go/issues/388) | docs/review | M35 multi-lane residual review synthesis (**closed**) |
| Active M36 | [#397](https://github.com/TokenDanceLab/metapi-go/issues/397) | docs | Residual honesty post M35 + v0.8.26 board pointer |
| Active M36 | [#398](https://github.com/TokenDanceLab/metapi-go/issues/398) | security | `IsValidAPIEndpointURL` rejects metadata/link-local (validator parity) |
| Active M36 | [#399](https://github.com/TokenDanceLab/metapi-go/issues/399) | proxy | CTX-520 reject max_tokens above positive route context_length |
| Active M36 | [#400](https://github.com/TokenDanceLab/metapi-go/issues/400) | observability | P0-555 warn when stream ends without usage after include_usage inject |
| Optional later | *(new, only with ACs)* | reliability | P0-585 production multi-channel load proof / breaker policy knobs |
| Product Milestone only with ACs | — | protocol/ops | WS-1 Codex interop; STICKY-B Redis sticky; UC-1 update-center registry |

## Explicit non-goals (M35 synthesis + residual waves)

- Product code in this Issue / document.
- Claiming WS frames, cluster-wide sticky, or update-center deploy success.
- Flipping P0-585 from **partial** to **present** without load-proof + breaker policy ACs.
- Claiming perfect billing accuracy or proxy max-token enforcement without dedicated product ACs.
- Returning `success:true` for unimplemented admin stream/job queues.
- Inventing new residual IDs for features that are already present.

## Links

- Residual queue: [`residual-next-candidates.md`](./residual-next-candidates.md)
- Failover honesty: [`failover-isolation.md`](./failover-isolation.md)
- MASTER: [`../progress/MASTER.md`](../progress/MASTER.md)
- Gap matrix: [`original-gap-matrix.md`](./original-gap-matrix.md)
- Related issues: #274, #282, #283, #290, #291, #292, #298–#302, #309–#311, #318–#320, #327–#329, #334–#336, #345–#346, #350–#351, #355–#359, #366–#368, #375–#377, #382–#384, #388–#391, #395–#400
