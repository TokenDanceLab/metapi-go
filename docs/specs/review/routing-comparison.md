# Routing Algorithm: TS vs Go Comparison

Generated: 2026-07-04

This document compares the routing algorithm between the TypeScript reference implementation (`metapi/src/server/services/tokenRouter.ts`) and the Go port (`metapi-go/routing/`). Each step of the algorithm is verified against the TS source.

---

## 1. Model Matching (Route Discovery)

**TS** (`findRoute`, line 3044-3051) uses chained `routes.find()` with 4 priorities:

1. `explicit_group` routes with matching `displayName`
2. Non-group routes with exact model pattern where trimmed pattern equals the model
3. Non-group routes with matching `displayName`
4. Non-group routes matching via `matchesModelPattern` (supports glob `*?` and regex `re:`)

**Go** (`findRoute`, selector.go line 392-427) implements the same 4-priority chain:

1. `IsExplicitGroupRoute(r.RouteMode) && IsRouteDisplayNameMatch(model, r.DisplayName)`
2. `!IsExplicitGroupRoute && IsExactRouteModelPattern(r.ModelPattern) && strings.TrimSpace(...) == model`
3. `!IsExplicitGroupRoute && IsRouteDisplayNameMatch(model, r.DisplayName)`
4. `!IsExplicitGroupRoute && MatchesModelPattern(model, r.ModelPattern)`

**Verdict: MATCH** -- exact same priority ordering and matching logic.

---

## 2. Candidate Eligibility Filtering

Both implementations run each channel through a list of checks:

| Check | TS | Go | Match? |
|---|---|---|---|
| Source model mismatch (unless bypass) | `channelSupportsRequestedModel` | `ChannelSupportsRequestedModel` | MATCH |
| Channel disabled | `!candidate.channel.enabled` | `!c.Channel.Enabled` | MATCH |
| OAuth route unit member unavailable | `getEligibleRouteUnitMembers().length === 0` | `getEligibleRouteUnitMembers(...)` | MATCH |
| Account status (token channels check `"disabled"`, others `!== "active"`) | `isExplicitTokenChannel ? status === 'disabled' : status !== 'active'` | Same logic | MATCH |
| Site disabled | `site.status === 'disabled'` | `c.Site.Status == "disabled"` | MATCH |
| Downstream exclusion (site IDs, credential refs) | `resolveDownstreamExclusionReason` | `resolveDownstreamExclusionReason` | MATCH |
| Already tried (excludeChannelIds) | `excludeChannelIds.includes(id)` | Iterates `excludeChannelIDs` | MATCH |
| Token unavailable | `resolveChannelTokenValue` returns null | `resolveChannelTokenValue` returns "" | MATCH |
| Cooldown active | `cooldownUntil > nowIso` | `cooldownUntil > nowISO` | MATCH |

**Verdict: MATCH** -- all checks equivalent.

---

## 3. Priority Layering (Weighted Strategy)

**TS** (line 2298-2363):
1. Group candidates by `priority` into a Map
2. Sort priorities ascending (`a - b`)
3. For each priority layer:
   a. Filter site runtime broken candidates (breaker)
   b. Filter recently failed candidates (Fibonacci backoff)
   c. Run weighted random selection
4. Stop at the first layer that produces a result (`break`)

**Go** (selector.go line 328-361):
1. Group candidates by `Channel.Priority` into a map
2. Sort priorities ascending (bubble sort)
3. For each priority layer:
   a. Filter site runtime broken candidates (breaker) via `GetBreakerFilteredCandidatesByModelResolver`
   b. Filter recently failed candidates via `FilterRecentlyFailedCandidates`
   c. Run weighted random selection via `weightedRandomSelect`
4. Stop at the first layer that produces a result (implicit via `continue`)

**Verdict: MATCH** -- same layering and filtering order.

---

## 4. Weight Scoring Algorithm (`calculateWeightedSelection`)

### 4.1 Effective Unit Cost Resolution

Both use the same cascade: observed (successCount>0) -> configured (unitCost) -> catalog (pricingFn) -> fallback.

| Step | TS | Go |
|---|---|---|
| Observed cost | `totalCost / successCount` | `totalCost/float64(successCount)` |
| Minimum clamp | `MIN_EFFECTIVE_UNIT_COST = 1e-6` | `minCost = 1e-6` |
| Configured check | `typeof configured === 'number' && Number.isFinite(configured) && configured > 0` | `candidate.Account.UnitCost != nil && *candidate.Account.UnitCost > 0 && isFiniteFloat(...)` |
| Catalog lookup | `getCachedModelRoutingReferenceCost({ siteId, accountId, modelName })` | `pricingFn(siteID, accountID, modelName)` |
| Fallback | `config.routingFallbackUnitCost \|\| 1` | `fallbackUnitCost` |

**Verdict: MATCH**

### 4.2 Value Scores

Both compute:
```
valueScore = costWeight * (1/unitCost) + balanceWeight * balance + usageWeight * (1/recentUsage)
```
where `recentUsage = max(successCount + failCount, 1)`.

**Verdict: MATCH**

### 4.3 Min-Max Normalization

Both normalize value scores:
```
normalizedVS = (valueScore - minVS) / (maxVS - minVS || 1)
```
with `maxVS = max(all, 0.001)` and `minVS = min(all, 0)`.

**Verdict: MATCH**

### 4.4 Base Contributions

Both compute:
```
baseContribution = (weight + 10) * (baseWeightFactor + normalizedVS * valueScoreFactor)
```

**Verdict: MATCH**

### 4.5 Site Channel Count Normalization

Both divide by `max(1, siteChannelCount)`. This prevents sites with many channels from dominating.

**Verdict: MATCH**

### 4.6 Final Contributions

**TS (weighted mode):**
```
contribution = baseContribution / siteChannels
             * combinedSiteWeight (siteGlobalWeight * downstreamSiteMultiplier)
             * runtimeHealthMultiplier
             * historicalHealthMultiplier
             * runtimeLoadMultiplier
             * (if fallback cost: 1/max(1, unitCost))
```

**Go (weighted mode):**
```
contribution = baseContribution / siteChannels
             * combinedSiteWeight (siteGlobalWeight * downstreamSiteMultiplier)
             * runtimeMultiplier
             * historicalHealthMultiplier
             * runtimeLoadMultiplier
             * (if fallback cost: 1.0 / max(1, unitCost))
```

**TS (stable_first mode):**
```
contribution = max(1e-4, recentSuccessRate ** 2)
             * runtimeHealthMultiplier
             * runtimeLoadMultiplier
             / siteChannels
```

**Go (stable_first mode):**
```
contribution = max(1e-4, recentSuccessRate * recentSuccessRate)
             * runtimeMultiplier
             * runtimeLoadMultiplier
             / siteChannels
```

**Verdict: MATCH**

### 4.7 Ranking

Both sort by contribution descending, with tie-breaking by `compareStableFirstCandidateOrder` (lastSelectedAt/lastUsedAt ascending -> lastUsedAt ascending -> channel ID ascending).

**Verdict: MATCH**

### 4.8 Random Selection (weighted mode)

**TS:**
```js
let rand = Math.random() * totalContribution;
selected = candidates[last];
for (i = 0; i < len; i++) {
    rand -= contributions[i];
    if (rand <= 0) { selected = candidates[i]; break; }
}
```

**Go:**
```go
r := rand.Float64() * totalContribution
selected = &candidates[len(candidates)-1]
for i := 0; i < n; i++ {
    r -= contributions[i]
    if r <= 0 {
        selected = &candidates[i]
        break
    }
}
```

**Verdict: MATCH** -- exact same weighted random selection algorithm.

### 4.9 Stable First Selection

Both sort site leaders by priority (stableFirstSiteOrder), then by channel ID. Both rotate to the next site after the last selected site.

**Verdict: MATCH**

---

## 5. Round Robin Strategy

**TS** (`getRoundRobinCandidates`, line 3381-3393):
- Sort by `lastSelectedAt || lastUsedAt` ascending
- Tie-break: `lastUsedAt` ascending
- Tie-break: channel ID ascending

**Go** (`GetRoundRobinCandidates`, round_robin.go line 8-34):
- Same sorting: `lastSelectedAt || lastUsedAt` ascending, then `lastUsedAt`, then `channel.ID`

**Verdict: MATCH**

---

## 6. Site Runtime Breaker Filtering

Both check two levels: global site breaker (`siteRuntimeHealthStates`) and per-model breaker (`siteModelRuntimeHealthStates`). A candidate is blocked if EITHER breaker is open. If all candidates are blocked, all are returned (as a fallback).

**TS:** `filterSiteRuntimeBrokenCandidatesByModel` (line 1031-1071)
**Go:** `FilterSiteRuntimeBrokenCandidatesByModel` / `FilterSiteRuntimeBrokenCandidatesByModelResolver` (runtime_health.go line 518-582)

**Verdict: MATCH**

---

## 7. Recent Failure Filtering

**TS** (`isChannelRecentlyFailed`, line 1267-1281):
- Check `failCount > 0` and `lastFailAt` is set
- Compute `avoidMs = clampFailureCooldownMs(resolveFailureBackoffSec(failCount) * 1000)`
- Check `nowMs - failTs < avoidMs`

**Go** (`IsChannelRecentlyFailed`, cooldown.go line 89-113):
- Same logic with equivalent constants
- Additional ISO 8601 space-separated format fallback in time parsing

**Verdict: MATCH**

**Fallback behavior** (`filterRecentlyFailedCandidates`):
If all candidates are recently failed, both implementations return ALL candidates (preventing starvation).

**Verdict: MATCH**

---

## 8. Stable First Pool Plan

**TS** (`buildStableFirstPoolPlan`, line 1691-1783):
1. Build historical health metrics per site
2. Find leader per site (by `compareStableFirstCandidateOrder`)
3. Compute effective success rate (blend recent + historical)
4. Trusted = recentConfidence >= 0.5 OR historicalCalls >= 8
5. Sort all site states by effective success rate descending
6. Leader pool = trusted entries (or all if none trusted)
7. Threshold rate = best rate * 0.92
8. Primary: leader pool members at or above threshold; Observation: everyone else
9. If primary empty, promote top site

**Go** (`BuildStableFirstPoolPlan`, weights.go line 585-708):
Same algorithm, step by step.

**Verdict: MATCH**

---

## 9. OAuth Route Unit Member Selection

Both select members within a route unit channel using:
- Round-robin strategy: sort by lastSelectedAt/lastUsedAt ascending, pick first
- Stick-until-unavailable: sort by lastSelectedAt/lastUsedAt descending, pick first

**Verdict: MATCH**

---

## 10. DISCREPANCIES FOUND

### DISCREPANCY 1 (MEDIUM): Per-candidate model resolution in stable_first pool plan -- FIXED

**TS** `selectFromMatch` (line 2895-2898):
```ts
const poolPlan = buildStableFirstPoolPlan(
    candidates,
    requestedByDisplayName ? runtimeModelResolver : mappedModel,  // FUNCTION or STRING
    nowMs,
);
```
Inside `buildStableFirstPoolPlan`, the `modelName` parameter is resolved per-candidate via:
```ts
const resolveModelName = typeof modelName === 'function' ? modelName : (() => modelName);
// Used per-site: getSiteRuntimeHealthDetails(siteId, resolveModelName(leader), nowMs)
```

**Go (FIXED)** `selectFromMatch` (line 296-297):
```go
poolPlan := BuildStableFirstPoolPlan(filteredCandidates, resolveModel)
```
`BuildStableFirstPoolPlan` now takes `modelResolver func(RouteChannelCandidate) string` (a function, matching TS behavior). The model name is resolved per-site using the site leader's candidate, so different sites with different source models get correct per-model runtime health lookups.

**Fix:** Changed `BuildStableFirstPoolPlan` signature from `modelName string` to `modelResolver func(RouteChannelCandidate) string`. Updated the call site to pass the `resolveModel` closure directly instead of pre-resolving from the first candidate.

**Verdict: FIXED** -- per-candidate model resolution now matches TS behavior.

### DISCREPANCY 2 (LOW): Per-candidate model resolution in weighted selection -- FIXED

**TS** `weightedRandomSelect` (line 3512-3518):
```ts
private weightedRandomSelect(candidates, modelName, ...) {
    return this.calculateWeightedSelection(candidates, modelName, ..., 'weighted').selected;
}
```
`modelName` can be a function for per-candidate resolution.

**Go (FIXED)** `weightedRandomSelect`:
```go
func (s *ChannelSelector) weightedRandomSelect(candidates []RouteChannelCandidate, modelResolver func(RouteChannelCandidate) string, ...) *RouteChannelCandidate {
    result := CalculateWeightedSelection(candidates, modelResolver, ...)
    return result.Selected
}
```
Now takes `modelResolver func(RouteChannelCandidate) string` (a function, matching TS behavior).

**Fix:** Changed `CalculateWeightedSelection` signature from `modelName string` to `modelResolver func(RouteChannelCandidate) string`. Each candidate's model is resolved individually for `EffectiveUnitCost` and `GetSiteRuntimeHealthDetails` calls. Updated all call sites (weightedRandomSelect, stableFirstSelect, selectFromMatch) to pass the `resolveModel` closure.

**Verdict: FIXED** -- per-candidate model resolution now matches TS behavior.

### DISCREPANCY 3 (LOW): Round robin does NOT filter recently failed

**TS** `selectFromMatch` for round_robin (line 2871-2888):
Only applies breaker filter, then picks the first in round-robin order. Does NOT call `filterRecentlyFailedCandidates`.

**Go** `selectFromMatch` for round_robin (line 280-287):
```go
breakerHealthy, _ := GetBreakerFilteredCandidatesByModelResolver(available, resolveModel)
selected := SelectRoundRobinCandidate(breakerHealthy)
```
Same behavior -- no recent failure filtering.

**Verdict: MATCH** -- this is intentional design. Round-robin relies on tiered cooldown (via `consecutiveFailCount` / `cooldownLevel`) rather than Fibonacci backoff filtering. The cooldown is applied at `recordFailure` time, not at selection time.

### DISCREPANCY 4 (LOW): The `findRoute` function in Go does not apply downstream policy for route find-by-ID

**TS** `findRouteById` (line 3058-3061):
```ts
if (downstreamPolicy.allowedRouteIds.length > 0 && !downstreamPolicy.allowedRouteIds.includes(routeId)) {
    return null;
}
```

**Go** does not have a separate `findRouteById` on the selector (it's used in methods like `SelectPreferredChannel` that call `findRoute` which does apply the policy). This is equivalent in the public API.

**Verdict: EQUIVALENT** through different code paths.

### DISCREPANCY 5 (LOW): Stable first observation gating only when `recordSelection=true`

**TS** `selectFromMatch` stable_first path (line 2900-2908):
```ts
const shouldUseObservation = (
    poolPlan.observationCandidates.length > 0
    && (
        poolPlan.primaryCandidates.length <= 0
        || (recordSelection && shouldUseStableFirstObservationCandidate(...))
    )
);
```
The observation gating is only triggered when `recordSelection` is true.

**Go** (line 299-301):
```go
shouldUseObservation := len(poolPlan.ObservationCandidates) > 0 &&
    (len(poolPlan.PrimaryCandidates) == 0 ||
        (recordSelection && ShouldUseStableFirstObservationCandidate(rotationKey, poolPlan.ObservationCandidates)))
```
Same logic.

**Verdict: MATCH**

### DISCREPANCY 6 (LOW): TS `explainSelection` path does NOT filter recently failed for round_robin

In TS `explainSelectionFromMatch` (line 2018-2020):
```ts
const recentlyFailed = routeStrategy !== 'round_robin'
    ? isChannelRecentlyFailed(row.channel, nowMs)
    : false;
```
Round-robin candidates never get marked as `recentlyFailed` in the explanation output. This mirrors the selection behavior.

The Go code does not implement `explainSelectionFromMatch` -- it is deferred to a future iteration.

**Verdict: NOT IMPLEMENTED in Go** -- explanation/reporting is a documented TODO.

---

## 11. SUMMARY

| Algorithm Step | Status |
|---|---|
| Model matching (4-priority chain) | MATCH |
| Candidate eligibility filtering (9 checks) | MATCH |
| Priority layering | MATCH |
| Effective unit cost resolution | MATCH |
| Value score computation | MATCH |
| Min-max normalization | MATCH |
| Base contributions | MATCH |
| Site channel count normalization | MATCH |
| Final contributions (weighted mode) | MATCH |
| Final contributions (stable_first mode) | MATCH |
| Ranking and sort | MATCH |
| Weighted random selection | MATCH |
| Stable first site rotation | MATCH |
| Round robin ordering | MATCH |
| Site runtime breaker filtering | MATCH |
| Recent failure filtering (Fibonacci backoff) | MATCH |
| Stable first pool plan | MATCH |
| OAuth route unit member selection | MATCH |
| Per-candidate model resolution for display-name routes | **FIXED** (resolver function matches TS) |
| Explain/report selection | NOT IMPLEMENTED in Go |

**Overall: The Go port faithfully reproduces the TS routing algorithm.** The previously identified discrepancy in per-candidate model resolution for display-name-matched routes has been fixed: `CalculateWeightedSelection` and `BuildStableFirstPoolPlan` now accept `func(RouteChannelCandidate) string` resolvers, matching the TS implementation's polymorphic `string | ((candidate) => string)` parameter. Per-candidate model resolution ensures correct runtime health metrics and pricing lookups even when channels on the same display-name-matched route have different source models.
