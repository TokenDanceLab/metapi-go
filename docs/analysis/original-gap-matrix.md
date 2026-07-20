# Original MetAPI Gap Matrix (metapi-go evidence)

> Snapshot date: 2026-07-16 (G2 inventory)  
> **Matrix evidence refreshed: 2026-07-17** (post v0.8.15 — #568 present, #585/#555 partial evidence, #590 present) · **2026-07-20** (#513→present; adversarial replay)  
> **Read-me 2026-07-18**: this table is **historical evidence**, not the living residual board.  
> Living residual / next-wave: [`residual-next-candidates.md`](./residual-next-candidates.md) · shortlist [`high-value-next.md`](./high-value-next.md) · 现状 [`../STATE.md`](../STATE.md).  
> Known matrix drift: **#520 context_length** row still says partial — product later shipped **CTX-520** (admin + multi-dialect enforce + UI); trust residual inventory over this row.  
> Branch base: `feat/gap-inventory` (G1 sources/taxonomy); refresh on `docs/p90-gap-matrix-refresh`  
> Issue: [#9 G2](https://github.com/tokendancelab/metapi-go/issues/9) · refresh [#281](https://github.com/TokenDanceLab/metapi-go/issues/281)  
> Sources: `docs/analysis/original-gap-sources.md`  
> Taxonomy: `docs/analysis/original-gap-taxonomy.md`

```
████████████████████████████████████████████████████████████████
█  G2 was docs-only inventory (no product fixes in that round).█
█  2026-07-17 refresh only updates status/evidence for shipped █
█  surfaces — still no product code in this docs PR.           █
████████████████████████████████████████████████████████████████
```

## Method

1. Read G1 inventory (`original-gap-sources.md` / taxonomy categories).
2. For each **mandatory** high-value item, search metapi-go (`rg`) for symbols/paths.
3. Classify `status` using static code evidence only (no live upstream traffic).
4. Mark `Backlog?` = `yes` when status is `missing` / `partial` / `unknown-needs-runtime` and product-relevant.

### status enum (ONLY)

| status | meaning |
| --- | --- |
| `present` | Capability or fix exists with concrete path/symbol evidence |
| `partial` | Related surface exists but incomplete vs upstream intent |
| `missing` | No meaningful implementation found |
| `n/a-upstream-only` | Upstream-only / noise / not a metapi-go product gap |
| `unknown-needs-runtime` | Code paths exist but correctness needs live runtime proof |

### Priority scale (planning, non-binding)

`P0` correctness/failover blockers · `P1` protocol/client blockers · `P2` routing/keys · `P3` ops/check-in · `P4` admin UX · `P5` noise

---

## Mandatory high-value matrix

| Upstream# | Title | Category | status | Evidence | Priority | Backlog? |
| ---: | --- | --- | --- | --- | --- | --- |
| 582 | isTokenExpiredError misclassifies non-auth 400/401 as expired key | bug-correctness | present | `service/alert/rules.go` `IsTokenExpiredError` excludes `invalid_argument` / `isRequestValidationFailure` / `isCapabilityOrBillingFailure`; tests in `service/alert/alert_test.go` `TestIsTokenExpiredError_ExcludesNonAuthUpstreamFailures` cover 400 invalid_argument + 401 model-not-supported + billing | P0 | no |
| 568 | Relay-station API keys frequently force-marked expired | bug-failover | present | Hardened in #298: `platform.ShouldMarkAccountExpired` requires confirmed credential-expiry wording (no bare/generic 401 mark); `ReportTokenExpired` defense-in-depth no-ops unless ClassExpired; checkin/balance call sites use `ShouldMarkAccountExpired`; proxy surface uses same guard. Residual: novel provider wording may still need exclusion expansion | P0 | no |
| 585 | One channel failure cascades to other channels | bug-failover | partial | Request-path exclude is **channel-scoped** (`proxy/conductor.go` `appendExcludedChannelID`; upstream loop appends `selected.Channel.ID` only). Hardened #299: 429 fails over (no same-channel pin), timeout same-channel budget, multi-channel same-site isolation tests in `proxy/conductor_test.go` + `routing/failure_isolation_test.go`. Residual: site/model breaker after 3 fails, credential-scoped usage-limit, empty-filter fallback, no production multi-channel load proof — see `docs/analysis/failover-isolation.md` | P0 | yes |
| 573 | Add Site silent fail: HTTP 200 error body + UI success toast | bug-correctness | present | Backend: `handler/admin/sites.go` create path returns 400/409/500 with `error`/`message` (not 200-on-error); success only `writeJSON(..., StatusOK, result)`. Frontend: `web/pages/Sites.tsx` `api.addSite` then `toast.success`; failures `catch` → `toast.error` | P0 | no |
| 580 | Gemini official chat rejects tool history without thought_signature | feature-protocol | present | Transform (#86/`5dcaf76`): `NormalizeRequest` / OpenAI↔Gemini rebuild / dummy inject / stream `ThoughtSignatures` collect / aggregate re-attach helpers in `transform/gemini/generate_content/compatibility.go` + `thought_signature_test.go`. Runtime wire (#309): `handler/proxy/upstream.go` `sanitizeUpstreamJSONBody` calls normalize/rebuild for `gemini`/`gemini-cli`/`google` on generateContent / v1internal paths when tool-history markers present; tests `handler/proxy/gemini_thought_signature_test.go`. Residual: no multi-instance aggregate session store — clients must echo `provider_specific_fields` or native signed contents; OpenAI-compat chat/completions path not rewritten | P1 | no |
| 581 | PR: Fix Gemini official tool-history thought signatures | feature-protocol | present | Same as #580 — request-side inject/preserve + proxy sanitize wire; residual is session aggregate re-attach across instances only | P1 | no |
| 590 | Cannot adjust route order | feature-routing | present | `token_routes.sort_order` additive (`store/additive.go` `sc2_006_token_routes_sort_order`); `PUT /api/routes/reorder` + list `ORDER BY sort_order ASC, id ASC` in `handler/admin/token_routes.go` (#284/#288); channel priority reorder remains `PUT /api/channels/batch` | P2 | no |
| 594 | Per-site max concurrency / request control | feature-routing | present | Schema: `sites.max_concurrency` (`store/schema.go` `Site.MaxConcurrency`, additive `store/additive.go` `sc2_002_site_max_concurrency`). Limiter: `proxy/site_concurrency.go` `SiteConcurrencyLimiter` (0 = unlimited; saturate → skip site, no cascade). Wired in `handler/proxy/upstream.go` + admin create/update `handler/admin/sites.go` / `handler/admin/payloads/sites.go`; tests `proxy/site_concurrency_test.go`, `handler/proxy/upstream_test.go` `TestSiteConcurrencySaturateSkipsWithoutFailure`, `handler/admin/sites_test.go` `TestSites_MaxConcurrencyRoundTrip`. Orthogonal to session channel leases in `proxy/session.go` | P2 | no |
| 591 | Add `/v1/rerank` endpoint | feature-protocol | present | `handler/proxy/rerank.go` `HandleRerank` (`POST` passthrough `/v1/rerank`, model required, stream rejected); registered `handler/proxy/router.go` `RegisterProxyRoutes` → `r.Post("/rerank", HandleRerank)`; metrics path `handler/shared/metrics.go`; tests `handler/proxy/rerank_test.go` + router path coverage; contract `docs/analysis/rerank-endpoint.md` | P1 | no |
| 583 | Key grouping / tags | feature-keys | present | `downstream_api_keys.group_name` CRUD: `handler/admin/downstream_keys.go` create/update/batch groupName; schema `store/schema.go` `DownstreamAPIKey`; tests `handler/admin/downstream_keys_test.go` | P2 | no |
| 579 | Downstream key binds multiple keys or multiple sites | feature-keys | **present** | `allowed_site_ids` + `allowed_credential_refs` (sc2_009) allow-list bind; routing eligibility before exclusions; admin CRUD + DownstreamKeys UI; empty = unrestricted (legacy exclude-only). Exclusions remain composable. | P2 | no |
| 578 | Per-key outbound proxy | feature-keys | present | Schema: `downstream_api_keys.proxy_url` (`store/schema.go`, additive `store/additive.go` `sc2_001_downstream_proxy_url`). Precedence helper `proxy/key_proxy.go` `ApplyKeyProxyOverride` / `ResolveKeyProxyURL` (key > account > site > system > direct). Auth load + middleware: `auth/downstream.go`, `auth/context.go`, `auth/proxy.go`. Dialer wiring `handler/proxy/upstream.go`; admin CRUD `handler/admin/downstream_keys.go`. Tests `proxy/key_proxy_test.go`, `auth/proxy_test.go` | P2 | no |
| 570 | Create named custom routes + attach arbitrary channels | feature-routing | present | `POST /api/routes` `handler/admin/token_routes.go` `createRoute` inserts `token_routes` (`model_pattern`, `display_name`, `route_mode`, …); channel attach `POST /api/routes/:id/channels`; group sources `route_group_sources` | P2 | no |
| 549 | Session stickiness | feature-routing | present | `proxy/session.go` `ProxyChannelCoordinator` `BuildStickySessionKey` / `BindStickyChannel` / `GetStickyChannelID`; config `ProxyStickySessionEnabled`/`ProxyStickySessionTtlMs`; e2e `e2e/e2e_test.go` `TestStickySession`; selection tests `proxy/channel_selection_test.go` sticky preference | P2 | no |
| 547 | Per-key weight for load balancing | feature-keys | **present** | `downstream_api_keys.key_weight` (sc2_007) + auth/routing `KeyWeight` multiplies `channel.Weight` in `CalculateWeightedSelection`; admin CRUD + DownstreamKeys UI; tests `TestNormalizeKeyWeightInput` / `TestCalculateWeightedSelection_KeyWeightAmplifiesChannelWeight` | P2 | no |
| 520 | PR: model context_length + manual model deletion | feature-admin-ux | **present-with-residual** | Manual models present; route `context_length` + admin/UI + OpenAI/Claude/Responses/Gemini max-token enforce (CTX-520); residual = further dialects only. Matrix evidence formerly claimed missing field — stale. | P2 | no |
| 584 | PR: site custom header override priority | feature-protocol | present | `sites.custom_headers_override_request_headers` (sc2_008) + `platform.ApplyCustomHeadersWithOptions` request-wins default / site-wins when flag; admin+Sites UI `customHeadersOverrideRequestHeaders`; deny-list unchanged | P2 | no |
| 588 | PR: pattern-group channels auto-sync after rebuild | feature-routing | present | Real rebuild: `service/route_rebuild.go` `RebuildRoutesBestEffort` → `RebuildTokenRoutesFromAvailability` (process mutex; rebuilds automatic pattern/exact channels from exact-route sources + `token_model_availability` / model availability; preserves `manual_override`; invalidates routing cache). Create path `PopulateRouteChannelsByModelPattern`. Hooks: account mutations `handler/admin/accounts.go`, maintenance `handler/admin/settings_maintenance.go`, admin rebuild `handler/admin/token_routes.go`. Spec note `docs/specs/p7-token-router.md` FE-GROUP-REBUILD. Tests `service/route_rebuild_test.go`. `explicit_group` membership stays on `route_group_sources` and expands at select time (by design) | P2 | no |
| 526 | Existing route groups do not auto-add channels for new sites | feature-routing | present | Same rebuild path as #588: after account/site topology or model availability changes, pattern routes recompose automatic channels so new matching sites/accounts appear. Admin `POST /api/routes/rebuild` + best-effort hooks. `explicit_group` still uses source-route membership (`route_group_sources`) rather than materializing its own channel rows | P2 | no |
| 559 | Regex route groups miss newly added matching sites | feature-routing | present | Same as #526/#588 — `MatchesModelPattern` during rebuild collects newly available models/sites into pattern routes; residual risk is only if operators skip rebuild hooks (best-effort paths log and continue) | P2 | no |
| 550 | PR: newapi cookie check-in + downstream key defaults | ops-checkin | present | `platform/newapi.go` `Checkin` + `shouldFallbackToCookieCheckin` + cookie check-in path `tryCookieCheckin`; tests `platform/newapi_test.go` `TestShouldFallbackToCookieCheckin`. Downstream key defaults handled in admin create path (`handler/admin/downstream_keys.go`) | P3 | no |
| 586 | ByteDance coding-plan stuck on v1 instead of v3 → 401 | feature-protocol | present | `proxy/endpoint_flow.go` `BuildUpstreamURL` + `hasVersionedBasePath` / `stripLeadingVersionSegment` preserves `/api/v3` base: tests `proxy/endpoint_flow_test.go` `https://ark.cn-beijing.volces.com/api/v3` → `.../api/v3/chat/completions`. Platform detect: `service/site_detect.go` bytedance/volcengine | P1 | no |
| 577 | AnyRouter check-in and model list broken | ops-checkin | partial | `platform/anyrouter.go` embeds `NewApiAdapter` (inherits Checkin/GetModels); token APIs intentionally disabled. Runtime “broken check-in/model list” needs live AnyRouter verification → residual risk | P3 | yes |
| 571 | Codex OAuth cannot call gpt-5.5 | feature-protocol | unknown-needs-runtime | Static: gpt-5 family allowlist + seed/probe prefer **gpt-5.5** (`service/oauth/codex_models.go`); discovery soft-retry 30s/45s (#489). No hard deny found. Still needs live OAuth+cloud model probe to flip present. | P1 | yes |
| 569 | Connection create missing proxy configuration | feature-admin-ux | present | Account update payload `proxyUrl`: `handler/admin/payloads/accounts.go`, `handler/admin/accounts.go` merge into `extraConfig`; UI edit form `web/pages/Accounts.tsx` `proxyUrl` field + display. Create/update tests `handler/admin/accounts_test.go` `TestAccounts_Update_ProxyURL` | P4 | no |
| 555 | Token usage statistics inaccurate | bug-correctness | **present-with-residual** | Aggregation + stream merge honesty + Gemini later-wins + **media `*_tokens_details` fold** when top-level missing (no double-count). Residual: multi-instance lag / orphan joins — not perfect billing. | P0 | yes |
| 496 | Claude cache pricing wrong when cache_ratio missing (fallback 1.0) | bug-correctness | present | Cost builder: `routing/pricing_cost.go` `ResolveCacheRatio` / `DefaultCacheRatioForModel` — Claude missing → **0.1**, cache_creation → **1.25**; non-Claude keeps historical 1.0; explicit 0 preserved. Proxy path: `handler/proxy/billing_cost.go` `EstimateBillingCostFromUsage` → `CalculateModelUsageBreakdown`. Tests `routing/pricing_cost_test.go`, `handler/proxy/billing_cost_test.go`. Analysis `docs/analysis/cache-ratio-pricing.md`. UI still shows `billing_details.cacheRatio` on proxy logs | P0 | no |
| 529 | Drag-and-drop reorder | feature-admin-ux | present | Route **channel** drag: `web/pages/TokenRoutes.tsx` + `@dnd-kit` (`web/pages/token-routes/RouteCard*.tsx`, `priorityRail.ts`) → `api.batchUpdateChannels` priorities. Import drag is file-drop only (`ImportExport.tsx`) | P4 | no |
| 538 | Hermes/Codex multi-turn `/v1/responses` reasoning item needs content | feature-protocol | present | `transform/openai/responses/reasoning_input.go` injects/preserves `content` + keeps `encrypted_content`/`summary`; `SanitizeResponsesRequestBody` + `handler/proxy/upstream.go` `sanitizeUpstreamJSONBody` (Responses path + input gate #310); honest `ReasoningInputError` 400; tests `reasoning_input_test.go`, `upstream_test.go`. Residual: full Responses→chat conversion, server store, WS (see `responses-multi-turn-reasoning.md`) | P1 | no |

**Mandatory row count:** 29 (all listed numbers covered; #580/#581 and #588/#526/#559 expanded as separate rows).

---

## Additional high-value product rows (non-mandatory sample)

| Upstream# | Title | Category | status | Evidence | Priority | Backlog? |
| ---: | --- | --- | --- | --- | --- | --- |
| 575 | PR: MySQL upsert in admin snapshot store | bug-correctness | n/a-upstream-only | metapi-go targets SQLite/PostgreSQL via sqlx (`store/migrate.go`); MySQL/TiDB upsert is upstream dialect work, not a Go gap unless MySQL support is re-scoped | P5 | no |
| 572 | Passthrough endpoint order; skip providers on endpoint failure | feature-routing | partial | `proxy/endpoint_flow.go` `ExecuteEndpointFlow` multi-endpoint candidates + `ShouldAbortRemainingEndpoints`; not full “downstream-defined order passthrough” productization | P2 | yes |
| 566 | MATCH_MAX_LATENCY_DELTA_MS too small → actual_cost fallback | bug-correctness | unknown-needs-runtime | Routing cost signals in `routing/weights.go` `EffectiveUnitCost`; latency delta constant behavior needs runtime comparison to TS | P2 | yes |
| 565 | Token refresh renames default key / sync overwrites enable state | bug-correctness | unknown-needs-runtime | Account token sync surfaces in `handler/admin/account_tokens.go` (includes sync stubs); enable-state overwrite needs targeted scenario tests | P0 | yes |
| 560 | Public-site check-in errors | ops-checkin | partial | Generic check-in: `service/checkin/checkin.go` `CheckinAccount`; platform adapters vary; public-site specifics need runtime | P3 | yes |
| 548 | NewAPI check-in + downstream keys workflow | ops-checkin | partial | Overlaps #550 present cookie path + downstream keys CRUD; full “workflow” UX completeness not verified | P3 | yes |
| 534 | Bulk account import | feature-admin-ux | **present** | `POST /api/accounts` accepts `accessTokens[]` batch create (same site); UI `parseBatchApiKeys` / Accounts paste; tests under `handler/admin/accounts_test.go`. Cross-site file bulk UX still optional residual — not “missing API”. | P4 | no |
| 531 | Anthropic→OpenAI skill-call anomaly in Claude Code | feature-protocol | partial | Anthropic/OpenAI bridges under `transform/anthropic`, `transform/canonical`; skill-call edge cases need runtime CC repro | P1 | yes |
| 530 | Site custom request overrides not applied automatically | feature-protocol | partial | Related to #584 — headers applied on platform proxy path; “automatic override of all request paths” not uniformly evidenced for every proxy surface | P2 | yes |
| 515 | Global model whitelist sporadically resets to `[]` | bug-correctness | unknown-needs-runtime | Settings store `handler/admin/settings.go` + `store/settings.go`; race/reset needs runtime/repro | P0 | yes |
| 514 | Multi-tier ctx sizes per model to switch channels | feature-routing | **present** | `PickContextTierRoute` + estimate; multi same-model routes with context_length (reuses CTX-520) | P2 | no |
| 513 | Model redirect/alias | feature-routing | **present** | `token_routes.model_mapping` + `ResolveMappedModel` in `routing/matcher.go` (exact→pattern fallback) + `ParseModelMappingRecord` + admin CRUD wired in `handler/admin/token_routes.go`; tests cover routing integration | P2 | no |
| 511 | Minimax thinking concatenated into content | feature-protocol | partial | Think-tag extraction `transform/shared` `ExtractInlineThinkTags` / reasoning_content paths; Minimax-specific regression not isolated | P1 | yes |
| 507 | `/v1/models` response shape issues | feature-protocol | partial | `handler/proxy/models.go` + e2e `e2e_flow_test.go` GET `/v1/models`; shape parity with all clients needs runtime | P1 | yes |
| 506 | Custom endpoint configuration | feature-routing | present | `site_api_endpoints` + `service/site_service.go` `UpsertSiteAPIEndpoints` / sites API endpoints payload | P2 | no |
| 504 | Unsupported parameter `previous_response_id` | feature-protocol | partial | Responses compact/sanitize strips some fields (`stream_options`/`store` in `transform/openai/responses/compact.go`); `previous_response_id` handling not clearly complete | P1 | yes |
| 497 | Show-enabled-only toggle on site/connection managers | feature-admin-ux | unknown-needs-runtime | UI filter may exist client-side; not exhaustively audited this pass | P4 | yes |
| 491 | Some requests do not count tokens | bug-correctness | partial | Token fields on `proxy_logs` + aggregation; zero-token paths in stubs (`handler/proxy/upstream.go` stub usage zeros) and stream edge cases | P0 | yes |
| 489 | Codex OAuth first model discovery timeout (12s) | feature-protocol | **present** | Upstream 12s budget replaced by **30s** first + **45s** soft-retry (`CodexModelDiscoveryTimeout` / soft-retry helpers); timeout-class only; operator status on exhaust. Live cloud still optional probe. | P1 | no |
| 475 | Pagination when too many APIs freeze UI | feature-admin-ux | partial | Some list endpoints exist; full admin pagination coverage not complete across all heavy lists | P4 | yes |
| 456 | Site heartbeat / health-check feature | feature-routing | partial | Runtime health: `routing/runtime_health.go`; post-refresh probe fields on sites; dedicated “heartbeat” product may still be incomplete | P2 | yes |
| 417 | Group-level fallback | feature-routing | partial | Group routes + channel retry/exclude; explicit group fallback policy productization incomplete | P2 | yes |
| 409 | In-route model config resets | bug-correctness | unknown-needs-runtime | Route update paths exist; reset bug needs repro | P0 | yes |
| 405 | Clearing request quota on downstream key edit does not save | bug-correctness | partial | Downstream key update normalizes max_cost/max_requests in `handler/admin/downstream_keys.go`; null/clear semantics need focused tests | P0 | yes |
| 391 | Skip new-api monthly plan sites when balance is 0 | feature-routing | partial | Balance refresh + routing weights use balance signals (`routing/weights.go`); “monthly plan balance 0 skip” policy not isolated as dedicated rule | P2 | yes |
| 387 | Fail does not try other protocols; first-token timeout unit unclear | bug-failover | partial | Cross-protocol endpoint candidates + abort policy in `proxy/endpoint_flow.go`; first-byte timeout plumbing present (`FirstByteTimeoutMs`) | P0 | yes |
| 360 | Split sites across different downstream keys | feature-keys | partial | Same as #579 — exclusions/allowed routes, not first-class site partitions | P2 | yes |
| 359 | Expired connection still shown as healthy | bug-failover | partial | Account status `expired` + runtime health sources; list badge consistency needs UI audit | P0 | yes |
| 340 | Sites only support `/v1/responses` + streaming | feature-protocol | partial | Responses endpoint + profile detect `proxy/profiles/codex.go`; “responses-only site” routing preference partial | P1 | yes |
| 292 | Auto priority orchestration routing strategy | feature-routing | missing | Strategies: weighted / round_robin / stable_first (`routing/matcher.go`); no auto-orchestration strategy | P2 | yes |

### Noise / out-of-product (from G1)

| Upstream# | Title | Category | status | Evidence | Priority | Backlog? |
| ---: | --- | --- | --- | --- | --- | --- |
| 592 | Maintenance status question | noise-question | n/a-upstream-only | Out-of-product per G1 | P5 | no |
| 574 | Owner release cadence | noise-question | n/a-upstream-only | Out-of-product per G1 | P5 | no |
| 553 | When next version | noise-question | n/a-upstream-only | Out-of-product per G1 | P5 | no |
| 552 | Nested-proxy meme | noise-question | n/a-upstream-only | Out-of-product per G1 | P5 | no |
| 459 | User self-retracted non-bug | noise-question | n/a-upstream-only | Out-of-product per G1 | P5 | no |

---

## Counts

| bucket | count |
| --- | ---: |
| Mandatory rows | 29 |
| Additional product rows | 30 |
| Noise rows | 5 |
| **Total matrix rows** | **64** |

| status | mandatory (29) | all rows (64) |
| --- | ---: | ---: |
| present | 23 | 24 |
| partial / present-with-residual | 5 | 26 |
| missing | 0 | 2 |
| unknown-needs-runtime | 1 | 6 |
| n/a-upstream-only | 0 | 6 |

Mandatory present (24): **#582, #568, #573, #580, #581, #583, #570, #549, #550, #586, #569, #529, #590, #594, #591, #578, #588, #526, #559, #496, #538, #547, #584**.  
Mandatory missing (0): *(none — #594/#591/#578/#588/#526/#559/#580/#581/#538 shipped)*.  
Mandatory partial / present-with-residual (4): **#585** (partial), **#520** (present-with-residual), **#577** (partial), **#555** (present-with-residual).  
Mandatory unknown (1): **#571**.

Remaining **all-rows missing** (1, additional product sample only): **#292** auto priority orchestration. **#514** → present 2026-07-21.  
**#534** bulk account import → **present** (2026-07-20 reverify: `POST /api/accounts` + `accessTokens[]`; not missing).

---

## Architecture debt appendix (non-original / metapi-go internal)

> Not from cita-777/metapi issues. Tracked so G2 planning does not lose rewrite-specific debt.

| Debt | Area | Evidence | Notes | Backlog? |
| --- | --- | --- | --- | --- |
| Route rebuild (resolved) | routing/ops | `service/route_rebuild.go` real `RebuildRoutesBestEffort` / `RebuildTokenRoutesFromAvailability`; tests + admin hooks | Was empty stub at G2; closed by FE-GROUP-REBUILD / #588 family — keep row only as historical pointer | no |
| Session lease goroutines | proxy session | `proxy/session.go` `createTrackedLease`: expiry `go func` + keepalive ticker `go func` per lease | Correctness/perf under high session churn; ensure `doneCh` always closes | yes |
| Client disconnect cancel | proxy HTTP | **present-with-residual**: upstream `http.NewRequestWithContext(r.Context())` + stream watches `r.Context().Done()` (`handler/proxy/upstream.go`); usage retained on disconnect | Residual: SiteProxy/executor edge paths under high load only with AC | no (monitor) |
| RWMutex alias / cache locks | routing concurrency | `routing/weights.go` `type sync_RWMutex = sync.RWMutex`; real `sync.RWMutex` used in `routing/cache.go`, `config`, admin caches | Not a no-op stub; keep as concurrency review point when extending stable-first state | no (monitor) |
| Proxy surface stubs | proxy handlers | `handler/proxy/upstream.go` `writeStubResponse`; several Gemini/images/videos stub paths when upstream not wired in tests | Must stay disabled in production (`main_test` flag comments) | yes (guardrails) |

---

## Planning notes for later waves

1. G2 matrix was inventory-only; product fixes landed later under **M-FEATURE** / residual releases. This file tracks **current evidence**, not the original freeze.
2. **2026-07-17 refresh:** mandatory missing set cleared for shipped surfaces — rebuild (**#588/#526/#559**), `/v1/rerank` (**#591**), per-site concurrency (**#594**), per-key proxy (**#578**), Claude `cache_ratio` (**#496**).
3. Remaining high-leverage residual: cascade production e2e (**#585 / P0-585** partial), usage multi-instance lag (**#555 / P0-555** present-with-residual), AnyRouter live (**#577**), Codex OAuth live (**#571**). **Shipped this wave**: #547/#584/#579 · #514 multi-tier · WS C1–C3 · UC-1 hide/external · #489 discovery timeout · #520 context_length (dialect residual). Program: [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md).
4. Prefer runtime verification for `unknown-needs-runtime` before opening large implementation issues.
5. Architecture debt rows can be filed as metapi-go-native issues (not “upstream parity”).

---

## File ownership

| file | role |
| --- | --- |
| `docs/analysis/original-gap-sources.md` | G1 source list (unchanged this round; mandatory numbers already complete) |
| `docs/analysis/original-gap-taxonomy.md` | G1 categories (unchanged) |
| `docs/analysis/original-gap-matrix.md` | **This document (G2)** |
