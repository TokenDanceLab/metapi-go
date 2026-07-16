# Feature-Complete Roadmap (F0)

> Snapshot date: 2026-07-16  
> Issue: [#23 F0](https://github.com/TokenDanceLab/metapi-go/issues/23)  
> Milestone: [M-FEATURE](https://github.com/TokenDanceLab/metapi-go/milestone/6)  
> Inputs: `docs/analysis/original-gap-matrix.md` (G2 #9), `docs/plan/original-gap-backlog.md` (G3 #10)  
> Lane: `lane-feature` · Program: `program:feature-complete`

```
████████████████████████████████████████████████████████████████
█  F0 IS DOCS + SCHEDULING ONLY.                               █
█  NO FEATURE CODE until a backlog issue is scheduled          █
█  (status:backlog-only REMOVED / replaced by scheduled).      █
█  Implementation fleets start only after wave gates below.    █
████████████████████████████████████████████████████████████████
```

## Purpose

Turn G2 evidence + G3 backlog shells into an **implementation roadmap**: waves, file-ownership lanes, and acceptance-criteria (AC) templates. This document does **not** implement product behavior.

| In scope (F0) | Out of scope (F0) |
| --- | --- |
| Epic / wave breakdown for P0 product features | Any Go/TS product code |
| File-ownership lanes for parallel feature WFs | Closing G3 backlog issues with code |
| AC templates per feature epic | Schema migrations (SCHEMA lane SC1/SC2) |
| Explicit scheduling gates before code | Reliability-only work already owned by R* (except handoff notes) |
| Mapping G3 issue numbers → feature fleets | Rewriting G1/G2 inventories |

## Sources of truth

| Doc / surface | Role |
| --- | --- |
| `docs/analysis/original-gap-matrix.md` | Capability status + priority evidence |
| `docs/plan/original-gap-backlog.md` | G3 `[backlog]` issue index (#36–#56) |
| `docs/plan/lane-charters.md` | Cross-lane file ownership |
| `docs/plan/enterprise-program.md` | Program waves E0–E4 |
| GitHub issues with `status:backlog-only` | Tracking shells only until scheduled |
| This file | Feature implementation plan (F0) |

## Hard rule: no feature code until scheduled

1. G3 issues ship with label **`status:backlog-only`**. That means **inventory / tracking only**.
2. A feature WF may write product code **only after** the issue is **scheduled**:
   - remove `status:backlog-only` (or add an explicit `status:scheduled` / `phase:waveN` implementation label), and
   - attach to **M-FEATURE** (or agreed sub-milestone), and
   - list allowed paths in the issue body / WF prompt.
3. Until then: docs, design notes, and issue triage only.
4. Schema columns required by a feature are owned by **SCHEMA (SC2)**; feature fleets must not edit `store/schema.go` without a SC2 dependency issue.
5. Reliability classification / cascade work stays on **RELIABILITY (R\*)** unless F0 explicitly hands a product surface to a feature fleet.

### Already landed (not feature backlog)

| Item | Status | Owner | Note |
| --- | --- | --- | --- |
| Upstream error classification + expired guards (upstream #582 class) | **Done (R0 #24)** | `lane-reliability` | `platform/error_classification.go`, alert rules hardened; foundational for expired-misclass / force-expired epics |
| Site create error not 200-on-failure (#573) | present in matrix | rewrite baseline | Do not re-open as F0 work |
| Key groups (#583), named custom routes (#570), sticky session (#549), channel DnD (#529), account proxyUrl (#569), ByteDance v3 path (#586) | present | rewrite baseline | Not F0 implementation targets |

---

## Epic map (implementation fleets)

Epics group G3 shells + deferred P2 product gaps that enterprise program E4 named as first-class feature work.

| Epic ID | Name | Primary priority | G3 / matrix anchors | Suggested WF fleet |
| --- | --- | --- | --- | --- |
| **FE-RERANK** | `/v1/rerank` endpoint | P1 (product P0 wave) | Upstream **#591** · [#48](https://github.com/TokenDanceLab/metapi-go/issues/48) | `feat-rerank` |
| **FE-SITE-CONC** | Per-site max concurrency | P2 missing → schedule as P0 product | Upstream **#594** (deferred in G3) | `feat-site-conc` |
| **FE-KEY-PROXY** | Per-key outbound proxy | P2 missing → schedule as P0 product | Upstream **#578** (deferred in G3) | `feat-key-proxy` |
| **FE-GROUP-REBUILD** | Pattern/group channel rebuild auto-sync | P2 missing → schedule as P0 product | Upstream **#588 / #526 / #559** + rebuild stub debt | `feat-group-rebuild` |
| **FE-STATS** | Usage / token / cache pricing accuracy | P0 | Upstream **#555 / #496 / #491** · [#42](https://github.com/TokenDanceLab/metapi-go/issues/42) [#43](https://github.com/TokenDanceLab/metapi-go/issues/43) [#44](https://github.com/TokenDanceLab/metapi-go/issues/44) | `feat-stats` |
| **FE-EXPIRED** | Expired classification + force-expire correctness | P0 (R0 foundation **done**) | Upstream **#568 / #359** · [#36](https://github.com/TokenDanceLab/metapi-go/issues/36) [#39](https://github.com/TokenDanceLab/metapi-go/issues/39); classification R0 | `feat-expired` / handoff from `lane-reliability` |
| **FE-FAILOVER** | Cascade / multi-protocol failover | P0 | Upstream **#585 / #387** · [#37](https://github.com/TokenDanceLab/metapi-go/issues/37) [#38](https://github.com/TokenDanceLab/metapi-go/issues/38) | `feat-failover` ∥ R1 |
| **FE-KEYS-CORR** | Key sync + quota clear correctness | P0 | Upstream **#565 / #405** · [#40](https://github.com/TokenDanceLab/metapi-go/issues/40) [#41](https://github.com/TokenDanceLab/metapi-go/issues/41) | `feat-keys-corr` |
| **FE-ADMIN-CORR** | Whitelist / route model config reset | P0 | Upstream **#515 / #409** · [#45](https://github.com/TokenDanceLab/metapi-go/issues/45) [#46](https://github.com/TokenDanceLab/metapi-go/issues/46) | `feat-admin-corr` |
| **FE-PROTOCOL** | Protocol parity pack (Gemini, Responses, Codex, …) | P1 | Upstream **#580/#581, #538, #571, #531, #511, #507, #504, #489, #340** · [#47](https://github.com/TokenDanceLab/metapi-go/issues/47)–[#56](https://github.com/TokenDanceLab/metapi-go/issues/56) excl. #48 | `feat-protocol-*` |

Enterprise E4 callouts (**rerank, site concurrency, per-key proxy, group rebuild, stats, expired classification**) map to **FE-RERANK / FE-SITE-CONC / FE-KEY-PROXY / FE-GROUP-REBUILD / FE-STATS / FE-EXPIRED**.

---

## Implementation waves

Waves are **scheduling order**, not permission to code. Each wave starts only when its **entry gate** is green and issues leave `backlog-only`.

### Wave F-Prep (docs / gates) — current

| Goal | Done when |
| --- | --- |
| F0 roadmap published | This file on default integration branch via PR |
| G2 matrix + G3 backlog available | #9 / #10 accepted or content merged |
| R0 classification landed | #24 merged (foundation for FE-EXPIRED) |
| No product feature commits under M-FEATURE | Working tree product packages unchanged by F0 |

**Code allowed:** none (docs only).

### Wave F1 — Correctness P0 (stats + expired + key/admin integrity)

| Epic | Issues to schedule | Depends on |
| --- | --- | --- |
| FE-STATS | #42, #43, #44 | none (existing aggregation surfaces) |
| FE-EXPIRED | #36, #39 | **R0 done**; optional R1 isolation |
| FE-KEYS-CORR | #40, #41 | runtime repro for #40 if still `unknown-needs-runtime` |
| FE-ADMIN-CORR | #45, #46 | repro-or-refute first |

**Parallelism:** FE-STATS ∥ FE-KEYS-CORR ∥ FE-ADMIN-CORR; FE-EXPIRED may pair with reliability R1 on disjoint paths (see lanes).

**Exit:** each scheduled issue has green tests per AC template; no silent stub zeros in production paths for stats.

### Wave F2 — Routing product P0 (site concurrency + group rebuild)

| Epic | Issues to create/schedule | Depends on |
| --- | --- | --- |
| FE-SITE-CONC | New or promote upstream **#594** from deferred inventory | **SC2** if new `sites` column (e.g. max concurrency); B2 lease primitives already present |
| FE-GROUP-REBUILD | Promote **#588/#526/#559** + replace `RebuildRoutesBestEffort` stub | SC2 only if membership schema changes; otherwise service + admin routes |

**Parallelism:** FE-SITE-CONC ∥ FE-GROUP-REBUILD if file lanes respected (site schema vs route rebuild service).

**Exit:** stub rebuild gone or gated; site concurrency enforced under concurrent proxy load tests.

### Wave F3 — Keys + protocol product

| Epic | Issues to schedule | Depends on |
| --- | --- | --- |
| FE-KEY-PROXY | Promote upstream **#578** | **SC2** for downstream key proxy fields; admin + proxy dialer wiring |
| FE-RERANK | #48 | proxy route registration + transform; optional UI later |
| FE-FAILOVER | #37, #38 | R1 failure isolation preferred first |
| FE-PROTOCOL (slice A) | #47 (Gemini thought_signature), #50 (responses reasoning) | fixtures |

**Parallelism:** FE-RERANK ∥ FE-KEY-PROXY (after SC2) ∥ FE-PROTOCOL slice A; FE-FAILOVER coordinates with reliability.

**Exit:** `/v1/rerank` registered and tested; per-key proxy used when set; failover AC themes met.

### Wave F4 — Remaining P1 protocol + deferred P2

| Epic | Issues | Notes |
| --- | --- | --- |
| FE-PROTOCOL (slice B) | #49, #51–#56 | Prefer runtime probe for `unknown-needs-runtime` (#49) before large code |
| Deferred routing/keys | #590 order, #579 multi-bind, #547 weights, #572, #514, #513, #456, #417, #391, #292, … | File/schedule only when product priority rises |
| Ops-checkin P3 | #577, #560, #548 | Not F1–F3; claim in ops wave |

**Exit:** M-FEATURE P0 checklist complete or explicitly deferred with issue + reason.

### Wave summary diagram

```
F-Prep  F0 docs + gates          ████ (this issue)
F1      stats / expired / keys   ░░░░ after schedule
F2      site-conc / rebuild      ░░░░ after SC2 as needed
F3      key-proxy / rerank / FO  ░░░░ after F1–F2 foundations
F4      protocol P1 + deferred   ░░░░
```

---

## File-ownership lanes (parallel feature WFs)

Global lane rules still apply (`docs/plan/lane-charters.md`). Below is the **feature sub-lane** matrix for concurrent fleets. Two WFs must never write the same path in the same wave.

| Sub-lane | Epic | Exclusive write paths | May read | Forbidden |
| --- | --- | --- | --- | --- |
| `feat-stats` | FE-STATS | `scheduler/usage_aggregation.go`, `handler/admin/stats.go`, `proxy` usage/billing extract helpers **only if not claimed by feat-rerank**, `web/pages` stats/ProxyLogs billing display as needed | `store/**`, `proxy/surface.go` | `store/schema.go`, `web/package.json` |
| `feat-expired` | FE-EXPIRED | `service/alert/**` (beyond R0), `service/checkin` / `service/balance` report paths that set `expired`, admin/UI health badge for expired | `platform/error_classification.go` (R0) | Replacing R0 classifier wholesale without reliability owner |
| `feat-failover` | FE-FAILOVER | `proxy/channel_selection.go`, `proxy/retry_policy.go`, `proxy/conductor.go` retry loop, `proxy/endpoint_flow.go` failover bits, `routing/cooldown*` | `routing/matcher.go` | Site concurrency lease redesign (feat-site-conc) |
| `feat-keys-corr` | FE-KEYS-CORR | `handler/admin/downstream_keys.go`, `handler/admin/account_tokens.go`, related tests, minimal `web/pages` key forms for clear-quota | `auth/downstream.go` | Per-key proxy schema (feat-key-proxy / SC2) |
| `feat-admin-corr` | FE-ADMIN-CORR | `handler/admin/settings.go`, `store/settings.go`, route model update paths in `handler/admin/token_routes.go` **only fields related to reset bugs** | `web/pages` settings/routes | Full TokenRoutes DnD rewrite |
| `feat-site-conc` | FE-SITE-CONC | `proxy/session.go` site-scoped limits (or new site concurrency module), site admin API fields, `web/pages/Sites.tsx` concurrency field | `config` proxy concurrency knobs | `RebuildRoutesBestEffort` |
| `feat-group-rebuild` | FE-GROUP-REBUILD | `service/site_service.go` `RebuildRoutesBestEffort`, `handler/admin/token_routes.go` rebuild + group sources, routing group membership sync | `route_group_sources` schema | Downstream key proxy |
| `feat-key-proxy` | FE-KEY-PROXY | Downstream key admin payloads + dialer selection for **key-level** `proxy_url`, `web` key forms | Site/account proxy for precedence rules | `sites` concurrency columns |
| `feat-rerank` | FE-RERANK | `handler/proxy/router.go` rerank route, new rerank handler/transform under `handler/proxy` / `transform/**` as needed, e2e rerank cases | existing proxy executor | Admin stats aggregation |
| `feat-protocol-*` | FE-PROTOCOL | Split by protocol family: `transform/gemini/**`, `transform/openai/responses/**`, `service/oauth/**`, `transform/anthropic/**`, `transform/shared/**` — **one family per WF** | `handler/proxy` | Unrelated admin CRUD |

### Cross-cutting ownership (not feature-owned)

| Path | Owner lane |
| --- | --- |
| `store/schema.go`, `cmd/migrate/**` | **SCHEMA** only (SC1/SC2) |
| `web/package.json`, lock, Vite/TS config | **STACK** |
| `docs/design/DESIGN.md`, shared components system | **UI** |
| `platform/error_classification.go` core taxonomy | **RELIABILITY** (feature may call APIs) |
| `docs/progress/MASTER.md` | Main session / `lane-gate` serial merge |

### Scheduling checklist (per issue before code)

```text
[ ] Issue body lists Allowed files / Forbidden files
[ ] status:backlog-only removed (or status:scheduled set)
[ ] Milestone M-FEATURE (or agreed)
[ ] Dependencies noted (SC2 / R1 / other epic)
[ ] AC section filled from templates below
[ ] WF prompt: Lane: lane-feature · Sub-lane: feat-* · Issue #N
```

---

## AC templates (copy into scheduled issues)

### Shared header (every feature issue)

```markdown
## Scheduling
- [ ] Left `status:backlog-only`
- [ ] Milestone: M-FEATURE
- [ ] Wave: F1 | F2 | F3 | F4
- [ ] Sub-lane: feat-…
- [ ] Depends on: (none | SC2 #… | R1 #… | …)

## Allowed files
- …

## Forbidden files
- store/schema.go (unless SCHEMA co-issue)
- web/package.json / lock
- …

## Non-goals
- …
```

### FE-RERANK — `/v1/rerank` (upstream #591 · #48)

```markdown
## Acceptance criteria
- [ ] `RegisterProxyRoutes` exposes `POST /v1/rerank` (and documented aliases if any)
- [ ] Request validation returns non-200 with clear error body (no silent 200)
- [ ] Happy path proxies to a channel that supports rerank OR returns explicit capability error
- [ ] Unit/e2e: route registered; sample body round-trip or fixture-based transform
- [ ] CHANGELOG notes new endpoint
- [ ] No change to unrelated chat/completions behavior
```

### FE-SITE-CONC — per-site max concurrency (upstream #594)

```markdown
## Acceptance criteria
- [ ] Site has configurable max concurrent requests (API + persistence; schema via SC2 if needed)
- [ ] Proxy rejects or queues beyond limit per product decision (document choice)
- [ ] Limit is **per site**, independent of session channel lease limits
- [ ] Concurrent load test: N+1th request does not starve other sites
- [ ] Admin UI or API documents the field; default preserves backward compat (unlimited or high default)
- [ ] Existing session channel concurrency still works
```

### FE-KEY-PROXY — per-key outbound proxy (upstream #578)

```markdown
## Acceptance criteria
- [ ] Downstream key model accepts optional proxy URL (SC2 additive column / JSON)
- [ ] Admin create/update/get round-trip preserves proxy field
- [ ] Outbound dial order documented: key proxy > account proxy > site proxy > direct (or agreed precedence)
- [ ] Empty/clear proxy persists as “inherit”
- [ ] Tests for precedence + invalid URL validation
- [ ] UI field on key editor (or explicit API-only defer with issue)
```

### FE-GROUP-REBUILD — pattern/group auto-sync (upstream #588/#526/#559)

```markdown
## Acceptance criteria
- [ ] `RebuildRoutesBestEffort` is no longer an empty stub (or is replaced by real rebuild service)
- [ ] Adding a site/account that matches a pattern/group source updates group channel membership without manual rebuild-only dead ends
- [ ] `POST /api/routes/rebuild` triggers the same sync path
- [ ] Tests: new matching site appears; non-matching site does not; concurrent rebuild safe
- [ ] Admin edge_cases tests updated (no longer document permanent stub)
- [ ] No destructive wipe of manually attached channels unless product policy says so (document)
```

### FE-STATS — usage / pricing / token count (#42/#43/#44 · #555/#496/#491)

```markdown
## Acceptance criteria
- [ ] Stream, partial, and error responses extract usage when provider sends it
- [ ] Aggregation (`site_day_usage` / `model_day_usage`) matches proxy_logs within documented tolerance
- [ ] Claude/Anthropic cache pricing: missing `cache_ratio` uses intentional fallback (not silent wrong 1.0 unless documented + tested)
- [ ] Production paths never rely on stub zero-token writers
- [ ] Dashboard/admin stats endpoints covered by unit or integration tests with fixtures
- [ ] Regression: non-cache requests unchanged
```

### FE-EXPIRED — force-expire + healthy badge (#36/#39 · #568/#359) · R0 foundation

```markdown
## Acceptance criteria
- [ ] `ReportTokenExpired` / status=`expired` only after classification confirms auth expiry (R0 helpers)
- [ ] Non-auth 400/401 (validation, billing, model-not-supported) do **not** force-expire keys
- [ ] Admin/UI does not show expired connections as healthy
- [ ] Tests cover false-positive bodies from matrix #582 class + relay false-expire scenarios
- [ ] No cascade: one expired key does not mark sibling channels expired
```

### FE-FAILOVER — cascade + cross-protocol (#37/#38 · #585/#387)

```markdown
## Acceptance criteria
- [ ] Channel failure exclude/cooldown scope is channel-level unless policy says site-level (document)
- [ ] One channel 5xx does not permanently poison other channels on same site without cooldown rules
- [ ] Multi-protocol / multi-endpoint candidates attempted per policy; abort rules tested
- [ ] First-byte / first-token timeout units documented and enforced consistently
- [ ] Load/integration test with multi-channel fixture
```

### FE-KEYS-CORR — sync + quota clear (#40/#41 · #565/#405)

```markdown
## Acceptance criteria
- [ ] Token refresh/sync does not rename operator-set default key or flip enable without intent
- [ ] Clearing max_cost / max_requests (null/0 per API contract) **persists** through reload
- [ ] Admin API + UI agree on clear semantics
- [ ] Focused tests for clear-quota and sync-overwrite scenarios
```

### FE-ADMIN-CORR — whitelist / route model reset (#45/#46 · #515/#409)

```markdown
## Acceptance criteria
- [ ] Repro-or-refute recorded (failing test or closed as cannot-reproduce with evidence)
- [ ] Concurrent or partial updates do not clobber unrelated settings/route model fields
- [ ] Global model whitelist empty array is only saved when operator explicitly clears
- [ ] Regression tests for update payloads that omit optional fields
```

### FE-PROTOCOL — pack template (use per issue #47–#56 except #48)

```markdown
## Acceptance criteria
- [ ] Provider/client contract covered by fixture (prefer) or documented runtime probe
- [ ] Transform/proxy change limited to named protocol surface
- [ ] Golden / unit test for before-after payload
- [ ] e2e or handler test if HTTP surface changes
- [ ] No broad rewrite of unrelated providers
```

---

## G3 issue → wave assignment (quick index)

| Issue | Upstream | Epic | Target wave | Notes |
| ---: | ---: | --- | --- | --- |
| 36 | 568 | FE-EXPIRED | F1 | After R0 |
| 37 | 585 | FE-FAILOVER | F3 | Prefer after R1 |
| 38 | 387 | FE-FAILOVER | F3 | |
| 39 | 359 | FE-EXPIRED | F1 | UI/health |
| 40 | 565 | FE-KEYS-CORR | F1 | Runtime first |
| 41 | 405 | FE-KEYS-CORR | F1 | |
| 42 | 555 | FE-STATS | F1 | |
| 43 | 496 | FE-STATS | F1 | |
| 44 | 491 | FE-STATS | F1 | |
| 45 | 515 | FE-ADMIN-CORR | F1 | |
| 46 | 409 | FE-ADMIN-CORR | F1 | |
| 47 | 580/581 | FE-PROTOCOL | F3 | Gemini |
| 48 | 591 | FE-RERANK | F3 | |
| 49 | 571 | FE-PROTOCOL | F4 | Probe first |
| 50 | 538 | FE-PROTOCOL | F3 | Responses |
| 51–56 | … | FE-PROTOCOL | F4 | Remaining P1 |
| — | 594 | FE-SITE-CONC | F2 | File issue when scheduled |
| — | 578 | FE-KEY-PROXY | F3 | Needs SC2 |
| — | 588/526/559 | FE-GROUP-REBUILD | F2 | File issue(s) when scheduled |

---

## Coordination with other lanes

| Lane | Interaction |
| --- | --- |
| **GAP (G4)** | Inventory acceptance must still claim **no product code** for G-track; feature code is M-FEATURE only after schedule |
| **SCHEMA (SC2)** | Additive columns for site concurrency, per-key proxy, model context_length, etc. |
| **BACKEND (B\*)** | Package boundaries / concurrency primitives reused; no drive-by architecture rewrites in feature WFs |
| **RELIABILITY (R1/R2)** | Failure isolation + observability; FE-FAILOVER / FE-EXPIRED share AC themes but split files |
| **UI (U\*)** | Shared components/tokens; feature WFs only touch page fields they own |
| **STACK** | No dependency bumps from feature fleets |

---

## Definition of Done — F0 (this issue #23)

- [x] Epic breakdown of P0/P1 (+ named product P0) gaps into implementable fleets
- [x] File-ownership lanes for parallel feature WFs
- [x] AC templates per feature epic
- [x] Explicit rule: **no feature code until scheduled issues leave backlog-only**
- [x] Document path: `docs/plan/feature-complete-roadmap.md`
- [ ] (PR merge) Optional MASTER link under Next Steps / Specs

## Definition of Done — M-FEATURE (later)

1. All F1 P0 issues scheduled and closed or explicitly deferred.
2. Enterprise E4 named set implemented or deferred with issue: **rerank, site concurrency, per-key proxy, group rebuild, stats, expired classification**.
3. No production dependence on route-rebuild stub or usage stubs.
4. CHANGELOG + tests per AC templates.

---

## File ownership (this doc)

| file | role |
| --- | --- |
| `docs/plan/feature-complete-roadmap.md` | **This document (F0 #23)** |
| `docs/plan/original-gap-backlog.md` | G3 backlog shells |
| `docs/analysis/original-gap-matrix.md` | G2 evidence |
| `docs/plan/lane-charters.md` | Cross-lane rules |
| `docs/progress/MASTER.md` | Progress SSOT (gate merges status only) |
