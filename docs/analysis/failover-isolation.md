# Failover Isolation (R1 / #25, upstream #585)

**Date:** 2026-07-16  
**Lane:** reliability  
**SSOT code:** `routing/cooldown.go`, `routing/round_robin.go`, `routing/runtime_health.go`, `routing/router.go` (`RecordFailure`), `routing/selector.go`, `proxy/conductor.go`, `proxy/retry_policy.go`, `proxy/failure_judge.go`

## Purpose

Prevent one upstream channel failure from poisoning sibling channels or unrelated sites. Isolation is layered: channel cooldown, selection soft-filters, site/model breakers, and proxy failover excludes.

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

These are **not** bugs of “mark sibling failed,” but residual fleet-level pressure:

| Residual | Behavior | Why |
|:---------|:---------|:----|
| Site/model breaker | 3 transient fails open breaker → **all** channels on that site/model filtered | Site-level systemic protection |
| Credential-scoped usage limit | Short window cools **all channels sharing the credential** | Shared quota / key truth |
| Empty-filter fallback | If every candidate is recently-failed or breaker-open, filters return the **full set** | Starvation prevention |
| No multi-channel production proof | Load-shaped systemic poison not proven e2e under production traffic | Unit/integration isolation proven; see gap matrix #585 residual |

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
- Existing: `routing/cooldown_test.go`, `routing/runtime_health_test.go`, `routing/algorithm_test.go` (partial/all breaker open)

## Non-goals (R1 / #299)

- Schema / UI changes
- Removing site breakers or empty-filter fallbacks
- Production multi-channel load proof
- WS / sticky product changes
- Inventing site-wide poison “fixes” that change product policy without tests
