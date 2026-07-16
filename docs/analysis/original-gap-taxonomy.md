# Original MetAPI Gap Taxonomy

> Snapshot date: 2026-07-16  
> Companion inventory: `docs/analysis/original-gap-sources.md`  
> Purpose: stable category vocabulary for metapi-go G1 (#8) gap analysis. Docs only.

## Why taxonomy

The original `cita-777/metapi` backlog mixes correctness bugs, failover faults, routing/key/protocol features, admin UX, check-in ops, and pure noise. Categories below are product-oriented (not GitHub labels) so metapi-go can prioritize rewrite coverage without carrying maintenance chatter.

## Category definitions

| category | meaning | inclusion signals | exclusion |
| --- | --- | --- | --- |
| `bug-correctness` | Wrong result, bad classification, broken persistence, bad stats, or UI success that lies about backend state. Core functional correctness. | misclassified errors, silent save failures, wrong pricing/token math, model identity wrong, config reset | pure latency complaints without wrong answer; release-date nags |
| `bug-failover` | Failover, health, expired-key, cascade, or multi-channel recovery bugs. One bad upstream should not poison siblings. | force-expired keys, cascade channel failure, healthy badge on expired, recovery cooldown | intentional circuit-breaker design questions with no bug report |
| `feature-routing` | Routing strategy, groups, channel selection, priority/order, stickiness, weights, auto-include, custom routes. | route order, session stick, group regex auto-include, drag sort, custom named routes | pure UI cosmetics with no routing semantics |
| `feature-keys` | Downstream keys, key groups, key-site binding, per-key proxy/weight, quota on keys. | key groups, multi-site keys, per-key proxy, key weights | site-level only settings with no key surface |
| `feature-protocol` | Protocol/endpoint compatibility: OpenAI/Anthropic/Gemini/Codex bridges, rerank, responses, thought_signature, endpoint path versions. | `/v1/rerank`, thought_signature, previous_response_id, coding-plan v3 path, stream_options | generic “is project maintained” |
| `feature-admin-ux` | Admin console usability: editors, lists, pagination, toggles, bulk ops, drag, logs visibility, onboarding copy. | pagination, show-enabled-only, bulk import, OAuth manager freeze, debug log capture | backend-only schema bugs with no UX surface |
| `ops-checkin` | Upstream/public-site check-in, cookie session check-in, captcha, balance refresh around check-in. | AnyRouter/newapi check-in failures, captcha on check-in | general routing without check-in |
| `noise-question` | Out-of-product questions: maintenance status, release nagging, memes, self-retracted non-bugs, empty value. | “is project maintained”, “when next version”, joke posts, user closes as not-a-bug | product bug reports that merely include a maintenance complaint in the title (e.g. #585) |

## Mapping rules

1. Prefer **behavior domain** over GitHub label. A `question` that reports cascade failure is `bug-failover`, not `noise-question`.
2. If an item spans categories, pick the **primary failure mode** for prioritization; secondary tags may be noted in later gap docs.
3. Open **product PRs** inherit the category of the behavior they change (e.g. #581 → `feature-protocol` / correctness fix for Gemini tools; #588 → `feature-routing`; #550 → `ops-checkin` + keys defaults).
4. Empty-title or incomplete reports stay in the closest technical bucket if body/context allows; otherwise park under `noise-question` only when no product signal exists.
5. Docs/demo token issues (`docs` label) map to `feature-admin-ux` or stay as low-priority ops docs unless they block product understanding.

## Seed mapping for mandatory high-value sources

| number | primary category | rationale |
| ---: | --- | --- |
| 582 | `bug-correctness` | `isTokenExpiredError` false-positive on non-auth 400/401 |
| 568 | `bug-failover` | relay keys repeatedly force-expired |
| 585 | `bug-failover` | one channel failure cascades to others (not pure maintenance) |
| 573 | `bug-correctness` | Add Site HTTP 200 + error body; UI false success |
| 580 | `feature-protocol` | Gemini tool history thought_signature rejection |
| 590 | `feature-routing` | cannot adjust route order |
| 594 | `feature-routing` | per-site max concurrency control |
| 591 | `feature-protocol` | `/v1/rerank` support |
| 583 | `feature-keys` | key grouping |
| 579 | `feature-keys` | downstream key binds multi-key / multi-site |
| 578 | `feature-keys` | per-key proxy |
| 570 | `feature-routing` | create named custom routes + arbitrary channels |
| 549 | `feature-routing` | session stickiness |
| 547 | `feature-keys` | key weights |
| 520 | `feature-admin-ux` / protocol surface | PR: model `context_length` + manual model delete |
| 584 | `feature-protocol` | PR: site custom header override priority |
| 588 | `feature-routing` | PR: pattern-group channel auto-sync after rebuild |
| 550 | `ops-checkin` | PR: newapi cookie check-in + downstream key defaults |
| 586 | `feature-protocol` | ByteDance coding-plan endpoint stays on v1 → 401 |
| 577 | `ops-checkin` | AnyRouter check-in / model list broken |
| 571 | `feature-protocol` | Codex OAuth cannot call gpt-5.5 |
| 569 | `feature-admin-ux` | connection create missing proxy fields |
| 555 | `bug-correctness` | token statistics inaccurate |
| 526 | `feature-routing` | existing groups do not auto-add new site routes |
| 559 | `feature-routing` | regex groups miss newly added matching sites |
| 496 | `bug-correctness` | Claude cache price wrong when `cache_ratio` missing |
| 529 | `feature-admin-ux` | drag-and-drop reorder |
| 538 | `feature-protocol` | multi-turn `/v1/responses` reasoning content requirement |
| 581 | `feature-protocol` | PR: Gemini official tool history thought signatures |

## Representative non-mandatory examples

| category | examples |
| --- | --- |
| `bug-correctness` | #515 whitelist resets; #409 route model config reset; #276 user id wrong; #491 missing token counts |
| `bug-failover` | #359 expired still healthy; #387 fail does not try other protocols |
| `feature-routing` | #517 same-name channel pick; #514 ctx-tier channel switch; #417 group fallback; #292 auto priority orchestration |
| `feature-keys` | #548 newapi check-in + downstream keys; #405 quota clear not saved; #360 split sites per key |
| `feature-protocol` | #504 previous_response_id; #511 minimax thinking merge; #446 stream_options; #340 responses-only sites |
| `feature-admin-ux` | #475 pagination; #497 show-enabled-only; #462 OAuth bulk freeze; #534 bulk account import |
| `ops-checkin` | #560 public-site check-in error; #523 captcha on check-in |
| `noise-question` | #592 maintenance; #574 owner release; #553 when update; #552 meme; #459 self-retract |

## Priority guidance (non-binding)

For metapi-go rewrite planning, default priority within this taxonomy:

1. `bug-failover` + high-impact `bug-correctness` (key expiry false positives, cascade failure, silent persist errors)
2. `feature-protocol` blockers for mainstream clients (Gemini tools, Codex/Responses, endpoint version paths)
3. `feature-routing` + `feature-keys` (groups, stickiness, weights, multi-bind, auto-include)
4. `ops-checkin` (newapi/AnyRouter cookie/session flows)
5. `feature-admin-ux` (power-user efficiency, not launch blockers)
6. `noise-question` (ignore for product backlog; keep only for community signal)

## Out-of-product policy

- Mark pure maintenance questions **out-of-product** in the sources table summary.
- Do not open metapi-go product issues solely from `noise-question` items.
- If a noise-titled issue also contains a concrete product defect (#585), keep the product category and drop only the maintenance half.
