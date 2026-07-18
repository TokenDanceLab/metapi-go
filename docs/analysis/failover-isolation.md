# Failover Isolation (R1 / #25, upstream #585)

**Date:** 2026-07-17  
**Lane:** reliability  
**Issues:** R1 [#25](https://github.com/TokenDanceLab/metapi-go/issues/25) → product harden [#299](https://github.com/TokenDanceLab/metapi-go/issues/299) / PR [#302](https://github.com/TokenDanceLab/metapi-go/pull/302); residual honesty [#336](https://github.com/TokenDanceLab/metapi-go/issues/336)  
**SSOT code:** `routing/cooldown.go`, `routing/round_robin.go`, `routing/runtime_health.go`, `routing/router.go` (`RecordFailure`), `routing/selector.go`, `proxy/conductor.go`, `proxy/retry_policy.go`, `proxy/failure_judge.go`  
**Inventory link:** [`residual-next-candidates.md`](./residual-next-candidates.md) (P0-585 row)

## Purpose

Prevent one upstream channel failure from poisoning sibling channels or unrelated sites. Isolation is layered: channel cooldown, selection soft-filters, site/model breakers, and proxy failover excludes.

## P0-585 honesty after #299 / #302 (shipped vs residual)

Upstream gap **#585** (“one channel failure cascades to other channels”) is **partial**, not closed. After R1 isolation tests and the request-path harden in **#299** (merged as **#302**), operators may treat channel-scoped failover as shipped — but must **not** claim fleet-wide “no cascade” under load.

| Bucket | What | Status after #299/#302 |
|--------|------|------------------------|
| **Shipped — channel isolation** | Non-usage-limit `RecordFailure` writes only the failed channel’s cooldown fields | **present** (R1 + unit tests) |
| **Shipped — same-site siblings clean** | Different-credential siblings do not receive `failCount` / `cooldownUntil` from a peer fail | **present** (`routing/failure_isolation_test.go`) |
| **Shipped — request-path exclude** | `excludeChannelIDs` is channel-ID only; never site-wide | **present** (`appendExcludedChannelID`, conductor tests) |
| **Shipped — 429 / timeout policy** | 429 fails over to siblings; 408/425 same-channel budget then failover | **present** (`failureActionOf`, `maxSameChannelRetries`) |
| **Shipped — selection soft filters** | Recent-failure + breaker filters prefer healthy candidates (all strategies) | **present** (R1 RR parity) |
| **Residual — site/model breaker** | Transient streak ≥3 opens site or site+model breaker → **all** channels on that scope soft-filtered | **intentional policy**, not a “sibling poison bug”; still a fleet-level cascade pressure |
| **Residual — credential usage-limit scope** | Short-window usage limit cools **all channels sharing the credential** | **intentional** shared-key truth |
| **Residual — empty-filter fallback** | Soft filters still return the **full set** when healthy is empty (**global** starvation guard). Weighted / round_robin / stable_first priority layers use **strict** soft-filter demotion first (`softFilterCandidatesStrict` / `selectAcrossPriorityLayers`): a soft-empty higher priority skips to the next priority; full-set fallback applies only after **all** layers are soft-empty (**#358**, **#368**) | Starvation prevention without pinning on a broken priority-0 layer; residual remains for global full-set re-exposure |
| **Residual — production multi-channel load proof** | No e2e / production-shaped load evidence that systemic poison stays contained under failure storms | **open** — unit/integration only |

**Do not claim:** “#585 is fully present” or “site-wide cascade is eliminated.”  
**Honest claim:** channel-scoped request exclude + cooldown write isolation are hardened and tested; site/model breaker and production load-proof remain residual (see table below and inventory P0-585).

### M39 reliability slices (shipped in v0.8.29)

Milestone **39** / **v0.8.29** closed three product reliability slices that were active after v0.8.28. Inventory IDs: **REL-PREFERRED-BREAKER** · **REL-COOLDOWN-TS** · **REL-CONDUCTOR-BUDGET** — all **present**.

| Issue | ID | Status | Evidence |
|------:|:---|:-------|:---------|
| [#423](https://github.com/TokenDanceLab/metapi-go/issues/423) | REL-PREFERRED-BREAKER | **present** (PR [#430](https://github.com/TokenDanceLab/metapi-go/pull/430)) | Preferred path checks Global/Model breaker directly; open breaker + siblings → nil / fall through |
| [#424](https://github.com/TokenDanceLab/metapi-go/issues/424) | REL-COOLDOWN-TS | **present** (PR [#427](https://github.com/TokenDanceLab/metapi-go/pull/427)) | `IsCooldownActive` parses millis ISO / RFC3339 both sides to ms; now+500ms still ineligible |
| [#425](https://github.com/TokenDanceLab/metapi-go/issues/425) | REL-CONDUCTOR-BUDGET | **present** (PR [#431](https://github.com/TokenDanceLab/metapi-go/pull/431)) | Hard MaxAttempts across same-channel + refresh + failover; cap RefreshAuth; nil/error RefreshAuth → failover + exclude |

Docs honesty for the board: [#426](https://github.com/TokenDanceLab/metapi-go/issues/426) / PR [#428](https://github.com/TokenDanceLab/metapi-go/pull/428). **P0-585 remains partial** (credential usage-limit scope, empty-filter global fallback, production multi-channel load proof).

### M40 note (v0.8.30) — not a cascade close

Milestone **40** / **v0.8.30** shipped **REL-SOURCE-MODEL** (#434 / PR #438): `loadRouteMatch` applies source route `model_pattern` as SourceModel fallback when channel SourceModel is blank/nil. That is a **selection mapping** fix for group/source eligibility + resolveModel — **not** a P0-585 cascade isolation close. **P0-585 stays partial.**

## Mechanism map (aligned with code)

| Layer | Mechanism | Code | Scope of poison |
|:------|:----------|:-----|:----------------|
| Channel cooldown | Fibonacci backoff from `failCount` | `ResolveFailureBackoffSec` / `ApplyFibonacciCooldown` (`routing/cooldown.go`, `routing/round_robin.go`) | **Failed channel only** (default) |
| Channel cooldown (RR) | Tiered 10m / 1h / 24h after 3 consecutive fails | `ApplyRoundRobinCooldown`, `RoundRobinCooldownLevelsSec` | **Failed channel only** |
| Soft recent-failure filter | Prefer candidates outside fib backoff window | `IsChannelRecentlyFailed`, `FilterRecentlyFailedCandidates` | Selection only; does not write state |
| Hard cooldown exclude | `cooldownUntil > now` | `getCandidateEligibilityReasons` (`routing/selector.go`) | Channel must already carry `cooldownUntil` |
| Short-window usage limit | 5m credential-scoped cooldown | `resolveShortWindowLimitCooldownTS`, `ShortWindowLimitCooldownMs`; `RecordFailure` expands via `LoadCredentialScopedChannelIDs` | **Same credential siblings only** |
| Site/model breaker | Transient streak ≥3 → 60s / 5m / 30m | `applyRuntimeHealthFailure`, `SiteRuntimeBreakerLevelsMs` (`routing/runtime_health.go`) | **All channels on that site (global) or site+model** |
| Breaker filter | Drop open-breaker candidates | `FilterSiteRuntimeBrokenCandidatesByModel*` | Soft; falls back to full set if all broken |
| OAuth route-unit members | Member-level cooldown + healthy filter on failover | `SelectRouteUnitMember`, member path in `RecordFailure` | **Member only** (outer channel fields untouched) |
| Proxy failover | Exclude tried **channel IDs only** | `SelectNextChannel(excludeChannelIDs)`, `proxy/channel_selection.go`, `proxy/conductor.go` (`appendExcludedChannelID`) | **Request-local, channel-scoped** — never site-wide |
| Conductor same-channel budget | Timeout-like 408/425 may retry same channel once, then failover | `maxSameChannelRetries`, `failureActionOf` (`proxy/conductor.go`) | Prevents one channel from starving same-site siblings |
| Retry vs terminal (surface) | Status/text classification for surface channel switch | `ShouldRetryProxyRequest` | No cross-channel state; not used for conductor same-channel pin |
| Same-site endpoint abort | Systemic 5xx/429 patterns | `ShouldAbortSameSiteEndpointFallback` | Endpoint list within one attempt — **not** sibling-channel poison |
| Content failure judge | HTTP 200 empty/keyword → record failure | `proxy/failure_judge.go` | Same as `RecordFailure` path |

### Strategy filter stack (selection)

All three strategies now share the same isolation order after eligibility:

1. Eligibility hard excludes (`cooldownUntil`, exclude IDs, account/site status, …)
2. Site/model breaker filter
3. **Recent-failure soft filter** (`FilterRecentlyFailedCandidates`)
4. Strategy pick (weighted / RR / stable-first)

> R1 fix: round-robin previously skipped step 3, so a single RR failure (no `cooldownUntil` yet) could be reselected immediately and starve healthy siblings. RR now applies the same recent-failure filter as weighted and stable-first.
>
> **#358 / #368 priority layers (weighted, round_robin, stable_first):** per-layer soft filters are **strict** (no full-set pin) via `selectAcrossPriorityLayers`. Soft-empty higher priority demotes to the next priority; only if every layer is soft-empty does the global full-set fallback apply.

### `RecordFailure` write scope

```
OAuth unit channel?
  yes → update ONLY route-unit member cooldown fields; RecordSiteRuntimeFailure(member.site)
  no  → short-window usage-limit?
          yes → UpdateChannelCooldownFields(credential-scoped IDs)
          no  → UpdateChannelCooldownFields([failedChannelID])
        RecordSiteRuntimeFailure(account.site)
```

Unrelated sites and non-scoped sibling channels **must not** receive `failCount` / `lastFailAt` / `cooldownUntil` updates.

## What is isolated (guarantees)

1. **One non-usage-limit channel failure** updates only that channel’s cooldown fields.
2. **Unrelated sites** never receive channel cooldown writes from another site’s failure.
3. **Same-site sibling channels** (different credentials) stay clean after a peer failure.
4. **Selection** prefers healthy siblings while any remain (recent-failure + breaker filters).
5. **OAuth member failure** mutates member state only, not sibling members’ channel rows.

## Request-path exclude contract (proxy conductor / selection)

Hardening for issue **#299** / upstream **#585** partial:

1. **Exclude list is channel-scoped only.** `DefaultProxyConductor.Execute` and `handler/proxy/upstream.go` append `selected.Channel.ID` only. Same-site siblings are **not** added.
2. **429 fails over**, it does not pin the same channel. Rate-limit is treated as channel/credential local pressure → try siblings.
3. **Timeout-like 408/425** may retry the same channel at most `maxSameChannelRetries` (1), then failover with channel-scoped exclude.
4. **Auth refresh failure** falls through to failover (siblings absorb) instead of terminal silent stop.
5. **`ShouldAbortSameSiteEndpointFallback`** only stops endpoint-list iteration on one attempt; it never expands `excludeChannelIDs` to a site.

Code evidence:

- `proxy/conductor.go` — `appendExcludedChannelID`, `failureActionOf`, `maxSameChannelRetries`
- `proxy/channel_selection.go` — `ChannelSelectionInput.ExcludeChannelIDs` docs
- `proxy/retry_policy.go` — `ShouldAbortSameSiteEndpointFallback` scope note
- `routing/router.go` `RecordFailure` — non-usage-limit writes `affectedChannelIDs = []int64{channelID}`

## Residual cascade (still intentional / partial)

These are **not** bugs of “mark sibling failed,” but residual fleet-level pressure that keeps **P0-585 = partial**:

| Residual | Behavior | Why still open |
|:---------|:---------|:----|
| Site/model breaker | 3 transient fails open breaker → **all** channels on that site/model filtered via `FilterSiteRuntimeBrokenCandidatesByModel*` | Intentional systemic protection; can look like cascade under multi-channel storms on one site |
| Credential-scoped usage limit | Short window cools **all channels sharing the credential** | Shared quota / key truth — not peer-channel poison, still multi-channel impact |
| Empty-filter fallback | Global filters return the **full set** when healthy is empty; weighted / RR / stable_first layers demote soft-empty priorities before that global fallback (**#358**, **#368**) | Global starvation prevention still reselects cooled channels when the whole fleet is degraded |
| Production multi-channel load proof | Load-shaped systemic poison not proven e2e under production traffic | Unit/integration isolation proven only; gap matrix #585 residual / inventory P0-585 |

### What would close (or further shrink) residual

| Residual item | Closing bar (product wave; out of #336 scope) |
|:--------------|:-----------------------------------------------|
| Site/model breaker “cascade look” | Product AC if breaker thresholds / model scope need operator knobs or different policy — **not** docs-only |
| Production multi-channel load proof | Load-test or production evidence plan with multi-channel same-site failure storms; metrics that sibling channels stay eligible while breakers stay closed for single-channel noise |
| Empty-filter fallback (global) | Removing global full-set fallback risks hard outage when all candidates are dirty; weighted / RR / stable_first per-layer demotion is shipped (**#358**, **#368**) |

## Tests

- `proxy/conductor_test.go` (**#299 multi-channel same-site request path**)
  - `TestConductor_FailoverExcludeIsChannelScopedNotSiteWide`
  - `TestConductor_RateLimitFailsOverToSameSiteSibling`
  - `TestConductor_TimeoutRetriesSameChannelThenFailsOver`
  - `TestConductor_MultiChannelSameSiteFailureIsolation`
  - `TestFailureActionOf_DoesNotPinRateLimitOnSameChannel`
- `routing/failure_isolation_test.go`
  - `TestRecordFailure_DoesNotCascadeToSiblingChannels`
  - `TestRecordFailure_UsageLimitScopesCredentialSiblingsOnly`
  - `TestRecordFailure_OAuthMemberIsolation`
  - `TestSelectionFilter_PrefersHealthySiblingAfterOneFailure` (weighted / RR / stable_first)
  - `TestSiteBreaker_DoesNotOpenOnSingleFailureAndDoesNotPoisonOtherSites`
  - `TestRoundRobinFilterStack_MatchesWeightedRecentFailurePolicy`
  - `TestWeightedSoftFilter_EmptyPriorityDemotesToNext` (**#358**)
  - `TestWeightedSoftFilter_AllLayersSoftEmptyAllowsGlobalFallback` (**#358**)
  - `TestRoundRobinSoftFilter_EmptyPriorityDemotesToNext` (**#368**)
  - `TestStableFirstSoftFilter_EmptyPriorityDemotesToNext` (**#368**)
  - `TestRoundRobinAndStableFirstSoftFilter_AllLayersSoftEmptyAllowsGlobalFallback` (**#368**)
  - `TestP0585Honesty_EmptyFilterFullSetStarvationGuard_AllPriorityLayersSoftUnhealthy` (**#476** honesty residual — global full-set fallback still intentional; not cascade-complete)
  - `TestP0585Honesty_PriorityLayerDemotes_DoesNotPinBrokenLayerViaFullSet` (**#476**)
- Existing: `routing/cooldown_test.go`, `routing/runtime_health_test.go`, `routing/algorithm_test.go` (partial/all breaker open)

## Non-goals (R1 / #299 / #336)

- Schema / UI changes
- Removing site breakers or empty-filter fallbacks in residual-docs waves
- Production multi-channel load proof (still open; product/load-test Milestone)
- WS / sticky product changes
- Inventing site-wide poison “fixes” that change product policy without tests
- Flipping matrix / inventory **#585 / P0-585** from **partial** to **present** without load-proof + explicit breaker policy ACs
