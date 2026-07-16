# Original MetAPI Gap Backlog (G3)

> Snapshot date: 2026-07-16  
> Source matrix: `docs/analysis/original-gap-matrix.md` (G2 #9)  
> Issue: [#10 G3](https://github.com/TokenDanceLab/metapi-go/issues/10)  
> Scope: **docs + GitHub backlog issues only** — no product implementation in this wave.

```
████████████████████████████████████████████████████████████████
█  THIS ROUND DOES NOT IMPLEMENT FIXES.                        █
█  Backlog issues are status:backlog-only tracking shells.     █
█  Implementation belongs to later feature/reliability waves.  █
████████████████████████████████████████████████████████████████
```

## Method

1. Read G2 matrix statuses and priorities.
2. Select every **P0** backlog row (`Backlog? = yes`) and every **high P1** backlog row (mandatory + additional product rows with Priority `P1` and `Backlog? = yes`).
3. Group into epics: **Routing & failover / Keys / Protocol / Admin UX / Ops-checkin / Stats**.
4. File one GitHub issue per selected gap (or tightly coupled pair) titled `[backlog] … (upstream #N)` with labels:
   - `program:gap-inventory`
   - `status:backlog-only`
   - `priority:P0` or `priority:P1`
   - `spec-driven`
5. Link issue numbers back into this document.

Merged rows:

| Matrix upstream | Reason merged |
| ---: | --- |
| #580 + #581 | Same Gemini `thought_signature` tool-history gap |

Not filed as G3 backlog issues:

| Bucket | Why |
| --- | --- |
| `present` / `Backlog? = no` | Already covered in metapi-go |
| P2–P5 product gaps | Deferred; listed under “Deferred inventory” for later waves |
| Ops-checkin P3 rows | No P0/P1 ops-checkin gaps in matrix; epic kept for structure |
| Architecture debt appendix | metapi-go-native debt; file separately when claimed by reliability/backend lanes |

## Summary counts

| Metric | Count |
| --- | ---: |
| Backlog issues created this round | **21** |
| P0 issues | 11 |
| P1 issues | 10 |
| Upstream numbers covered | 22 (incl. #580+#581 pair) |
| Epics with filed issues | 5 of 6 (Ops-checkin empty at P0/P1) |

Verify:

```bash
gh issue list -R TokenDanceLab/metapi-go --label status:backlog-only --limit 50
```

---

## Epic: Routing & failover

Correctness and recovery when channels, protocols, or health signals fail.

| Upstream | Title | Matrix status | Pri | metapi-go issue |
| ---: | --- | --- | --- | ---: |
| 568 | Relay keys force-marked expired | partial | P0 | [#36](https://github.com/TokenDanceLab/metapi-go/issues/36) |
| 585 | Channel failure cascade poison | partial | P0 | [#37](https://github.com/TokenDanceLab/metapi-go/issues/37) |
| 387 | Cross-protocol failover + first-token timeout | partial | P0 | [#38](https://github.com/TokenDanceLab/metapi-go/issues/38) |
| 359 | Expired connection still shown healthy | partial | P0 | [#39](https://github.com/TokenDanceLab/metapi-go/issues/39) |

### AC themes (shared)

- Fail closed only on confirmed auth-expiry; do not poison siblings.
- Exclude/cooldown scope explicit (channel vs site).
- Health badges respect `expired` status.
- Multi-protocol candidates attempted per product policy; timeout units documented.

---

## Epic: Keys

Downstream/account key lifecycle, quota, and sync correctness.

| Upstream | Title | Matrix status | Pri | metapi-go issue |
| ---: | --- | --- | --- | ---: |
| 565 | Token refresh overwrites key name/enable state | unknown-needs-runtime | P0 | [#40](https://github.com/TokenDanceLab/metapi-go/issues/40) |
| 405 | Clearing downstream key quota does not save | partial | P0 | [#41](https://github.com/TokenDanceLab/metapi-go/issues/41) |

### AC themes (shared)

- Sync preserves operator-set names and enable flags unless intentional.
- Null/clear quota semantics persist through admin API + UI.

### Deferred Keys (P2, not filed)

| Upstream | Title | status | Pri |
| ---: | --- | --- | --- |
| 579 | Downstream key multi-key / multi-site binding | partial | P2 |
| 578 | Per-key outbound proxy | missing | P2 |
| 547 | Per-key weight for load balancing | partial | P2 |
| 360 | Split sites across different downstream keys | partial | P2 |

---

## Epic: Protocol

Proxy/protocol compatibility (OpenAI / Anthropic / Gemini / Codex / Responses).

| Upstream | Title | Matrix status | Pri | metapi-go issue |
| ---: | --- | --- | --- | ---: |
| 580 / 581 | Gemini tool-history `thought_signature` | partial | P1 | [#47](https://github.com/TokenDanceLab/metapi-go/issues/47) |
| 591 | Add `/v1/rerank` endpoint | missing | P1 | [#48](https://github.com/TokenDanceLab/metapi-go/issues/48) |
| 571 | Codex OAuth gpt-5.5 support | unknown-needs-runtime | P1 | [#49](https://github.com/TokenDanceLab/metapi-go/issues/49) |
| 538 | Multi-turn `/v1/responses` reasoning content | partial | P1 | [#50](https://github.com/TokenDanceLab/metapi-go/issues/50) |
| 531 | Anthropic→OpenAI skill-call anomaly | partial | P1 | [#51](https://github.com/TokenDanceLab/metapi-go/issues/51) |
| 511 | Minimax thinking concatenated into content | partial | P1 | [#52](https://github.com/TokenDanceLab/metapi-go/issues/52) |
| 507 | `/v1/models` response shape parity | partial | P1 | [#53](https://github.com/TokenDanceLab/metapi-go/issues/53) |
| 504 | `previous_response_id` parameter handling | partial | P1 | [#54](https://github.com/TokenDanceLab/metapi-go/issues/54) |
| 489 | Codex OAuth first model discovery timeout | partial | P1 | [#55](https://github.com/TokenDanceLab/metapi-go/issues/55) |
| 340 | Responses-only site streaming preference | partial | P1 | [#56](https://github.com/TokenDanceLab/metapi-go/issues/56) |

### AC themes (shared)

- Client-facing protocol parity with original MetAPI / provider contracts.
- Prefer fixtures + e2e over live-only fixes; runtime probe first for `unknown-needs-runtime`.

### Deferred Protocol (P2, not filed)

| Upstream | Title | status | Pri |
| ---: | --- | --- | --- |
| 584 | Site custom header override priority | partial | P2 |
| 530 | Site custom request overrides not automatic | partial | P2 |

---

## Epic: Admin UX

Admin console correctness that looks like UX but is data/config integrity at P0.

| Upstream | Title | Matrix status | Pri | metapi-go issue |
| ---: | --- | --- | --- | ---: |
| 515 | Global model whitelist resets to empty | unknown-needs-runtime | P0 | [#45](https://github.com/TokenDanceLab/metapi-go/issues/45) |
| 409 | In-route model config resets | unknown-needs-runtime | P0 | [#46](https://github.com/TokenDanceLab/metapi-go/issues/46) |

### AC themes (shared)

- Concurrent/partial updates do not clobber unrelated fields.
- Repro-or-refute before large UI rewrites.

### Deferred Admin UX (P2/P4, not filed)

| Upstream | Title | status | Pri |
| ---: | --- | --- | --- |
| 520 | Model `context_length` + manual model deletion | partial | P2 |
| 534 | Bulk account import | missing | P4 |
| 497 | Show-enabled-only toggle | unknown-needs-runtime | P4 |
| 475 | Pagination when too many APIs freeze UI | partial | P4 |

---

## Epic: Ops-checkin

Upstream/public-site check-in and cookie session workflows.

**No P0/P1 backlog issues filed.** Matrix ops-checkin rows with `Backlog? = yes` are P3:

| Upstream | Title | status | Pri | Note |
| ---: | --- | --- | --- | --- |
| 577 | AnyRouter check-in and model list broken | partial | P3 | Residual runtime risk; inherit NewAPI adapter |
| 560 | Public-site check-in errors | partial | P3 | Platform variance; runtime |
| 548 | NewAPI check-in + downstream keys workflow | partial | P3 | Overlaps present #550 cookie path |

File implementation issues when an ops wave claims these; do not treat G3 as incomplete solely because this epic has no P0/P1 shells.

---

## Epic: Stats

Usage aggregation, token counting, and billing/cost correctness.

| Upstream | Title | Matrix status | Pri | metapi-go issue |
| ---: | --- | --- | --- | ---: |
| 555 | Token usage statistics inaccurate | partial | P0 | [#42](https://github.com/TokenDanceLab/metapi-go/issues/42) |
| 496 | Claude `cache_ratio` pricing fallback wrong | partial | P0 | [#43](https://github.com/TokenDanceLab/metapi-go/issues/43) |
| 491 | Some requests do not count tokens | partial | P0 | [#44](https://github.com/TokenDanceLab/metapi-go/issues/44) |

### AC themes (shared)

- Stream/partial/error usage extraction audited.
- Cost builder applies cache pricing with intentional fallback.
- Production never depends on stub zero usage.

---

## Deferred inventory (P2 product gaps with Backlog? = yes)

Not filed in G3 (priority below P0/high-P1). Keep for F0 / feature-complete / reliability waves.

### Routing (P2)

| Upstream | Title | status |
| ---: | --- | --- |
| 590 | Cannot adjust route order | partial |
| 594 | Per-site max concurrency / request control | missing |
| 588 | Pattern-group channels auto-sync after rebuild | missing |
| 526 | Existing route groups do not auto-add new sites | missing |
| 559 | Regex route groups miss newly added matching sites | missing |
| 572 | Passthrough endpoint order; skip providers on endpoint failure | partial |
| 566 | `MATCH_MAX_LATENCY_DELTA_MS` too small → actual_cost fallback | unknown-needs-runtime |
| 514 | Multi-tier ctx sizes per model to switch channels | missing |
| 513 | Model redirect/alias completeness | partial |
| 456 | Site heartbeat / health-check feature | partial |
| 417 | Group-level fallback | partial |
| 391 | Skip new-api monthly plan sites when balance is 0 | partial |
| 292 | Auto priority orchestration routing strategy | missing |

### Architecture debt (metapi-go native, non-upstream)

| Debt | Area | Suggested owner lane |
| --- | --- | --- |
| Route rebuild stub (`RebuildRoutesBestEffort`) | routing/ops | feature / schema |
| Session lease goroutines lifecycle | proxy session | reliability / backend-arch |
| Client disconnect cancel to upstream | proxy HTTP | reliability |
| Proxy surface stubs guardrails | proxy handlers | reliability |

---

## Issue index (created this round)

| # | Pri | Epic | Upstream | Title |
| ---: | --- | --- | ---: | --- |
| 36 | P0 | Routing & failover | 568 | Relay keys force-marked expired |
| 37 | P0 | Routing & failover | 585 | Channel failure cascade poison |
| 38 | P0 | Routing & failover | 387 | Cross-protocol failover and first-token timeout |
| 39 | P0 | Routing & failover | 359 | Expired connection still shown healthy |
| 40 | P0 | Keys | 565 | Token refresh overwrites key name/enable state |
| 41 | P0 | Keys | 405 | Clearing downstream key quota does not save |
| 42 | P0 | Stats | 555 | Token usage statistics inaccurate |
| 43 | P0 | Stats | 496 | Claude cache_ratio pricing fallback wrong |
| 44 | P0 | Stats | 491 | Some requests do not count tokens |
| 45 | P0 | Admin UX | 515 | Global model whitelist resets to empty |
| 46 | P0 | Admin UX | 409 | In-route model config resets |
| 47 | P1 | Protocol | 580/581 | Gemini tool-history thought_signature |
| 48 | P1 | Protocol | 591 | Add `/v1/rerank` endpoint |
| 49 | P1 | Protocol | 571 | Codex OAuth gpt-5.5 support |
| 50 | P1 | Protocol | 538 | Multi-turn `/v1/responses` reasoning content |
| 51 | P1 | Protocol | 531 | Anthropic to OpenAI skill-call anomaly |
| 52 | P1 | Protocol | 511 | Minimax thinking concatenated into content |
| 53 | P1 | Protocol | 507 | `/v1/models` response shape parity |
| 54 | P1 | Protocol | 504 | previous_response_id parameter handling |
| 55 | P1 | Protocol | 489 | Codex OAuth first model discovery timeout |
| 56 | P1 | Protocol | 340 | Responses-only site streaming preference |

---

## File ownership

| file | role |
| --- | --- |
| `docs/analysis/original-gap-sources.md` | G1 source list |
| `docs/analysis/original-gap-taxonomy.md` | G1 categories |
| `docs/analysis/original-gap-matrix.md` | G2 evidence matrix |
| `docs/plan/original-gap-backlog.md` | **This document (G3)** |
| GitHub issues #36–#56 | Backlog-only tracking shells |

## Exit for G3

- [x] `docs/plan/original-gap-backlog.md` written with epic grouping
- [x] Each P0 and high P1 gap has a `[backlog]` issue with required labels
- [x] Issue numbers linked in this document
- [x] No product code changes
