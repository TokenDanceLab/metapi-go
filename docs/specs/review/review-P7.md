# P7 Cross-Reference Review: Spec vs TypeScript Source

**Review date**: 2026-07-04
**Spec**: `docs/specs/p7-token-router.md`
**TS files reviewed**:
- `tokenRouter.ts` (3807 lines -- full read)
- `routeRoutingStrategy.ts` (15 lines)
- `routeCooldownService.ts` (143 lines)
- `routeRefreshWorkflow.ts` (44 lines)
- `routeDecisionRefreshService.ts` (104 lines)

---

## Accuracy Issues

### A1. Weight formula in spec is completely wrong

**Spec claim** (Section "Channel Selection", Step 6):
```
weight_score = channel.weight x config.BaseWeightFactor
value_score = account.value_score x config.ValueScoreFactor
cost_score = (1 / unit_cost) x config.CostWeight
balance_score = (balance / quota) x config.BalanceWeight
usage_score = (1 - usage_trend) x config.UsageWeight
Total = weight + value + cost + balance + usage
```

**TS reality** (tokenRouter.ts lines 3570-3637): The actual formula does NOT sum five separate terms. Instead:

1. A raw "value score" is computed: `costWeight * (1/unitCost) + balanceWeight * balance + usageWeight * (1/recentUsage)` -- note `usageWeight * (1/recentUsage)` not `(1 - usage_trend)`, and `costWeight * (1/unitCost)` not `(1/unit_cost) * config.CostWeight`.

2. This raw score is min-max normalized across all candidates: `normalizedVS = (v - minVS) / range`.

3. The base contribution is: `(weight + 10) * (baseWeightFactor + normalizedVS * valueScoreFactor)` -- note the `+ 10` offset on weight (no such offset in spec).

4. Contribution is then divided by `siteChannels` (a same-site channel count penalty, absent from spec).

5. Additional multipliers (absent from spec): `runtimeMultiplier`, `downstreamSiteMultiplier * siteGlobalWeight`, `siteHistoricalMultiplier`, `runtimeLoadMultiplier`, and a fallback cost penalty (`1 / unitCost` when source is 'fallback').

6. Final probability = `contribution / totalContribution` with weighted random selection.

**Severity**: BLOCKING. The spec formula and the implementation formula are entirely different. Any Go implementation following the spec will produce different routing decisions.

### A2. Cooldown is Fibonacci, not exponential

**Spec claim** (Section "Cooldown / Circuit Breaker"):
```
cooldownLevel = min(consecutiveFailCount, maxCooldownLevel)
cooldownUntil = now + min(backoff^cooldownLevel, TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC)
```

**TS reality** (tokenRouter.ts lines 310-344):
```typescript
const FAILURE_BACKOFF_BASE_SEC = 15;
const MAX_FAILURE_BACKOFF_SEC = 30 * 24 * 60 * 60; // 30 days

function fibonacciNumber(index) { ... }  // fib(1)=1, fib(2)=1, fib(3)=2, fib(4)=3, fib(5)=5, ...

function resolveFailureBackoffSec(failCount) {
  return Math.min(15 * fibonacciNumber(failCount), MAX_FAILURE_BACKOFF_SEC);
}
```

This is Fibonacci sequence with a multiplicative factor of 15, NOT `backoff^cooldownLevel` (exponential). Each failCount maps to: fail=1 -> 15s, fail=2 -> 15s, fail=3 -> 30s, fail=4 -> 45s, fail=5 -> 75s, fail=6 -> 120s, etc. There is no `cooldownLevel` in this formula -- it's driven by `failCount` directly.

Additionally, there is a separate config-driven ceiling `clampFailureCooldownMs()` which applies `tokenRouterFailureCooldownMaxSec` as a secondary cap on top of `MAX_FAILURE_BACKOFF_SEC`.

**Severity**: BLOCKING. Wrong algorithm produces wrong cooldown durations.

### A3. "Circuit Breaker" model is wrong

**Spec claim** (Section "Cooldown / Circuit Breaker"):
```
RecordFailure:
  - 如果 site 的全局失败率 > 阈值 -> open circuit breaker

isSiteBreakerOpen(site):
  - 断路器打开 -> 所有 channel 跳过
  - 30s 后尝试 half-open (放行一个请求探测)
```

**TS reality**:
The TS has NO classic half-open circuit breaker. There are two separate mechanisms:

1. **Channel cooldown** (`channel.cooldownUntil`): Persisted on the DB row. Checked by `isChannelRecentlyFailed()` (line 1267) which compares `nowMs - failTs < cooldownMs` using the Fibonacci backoff formula. No half-open probing.

2. **Site runtime health breaker** (lines 689-710, 738-763): In-memory state with `breakerLevel` and `breakerUntilMs`. Triggered by `transientFailureStreak >= 3` within a 5-minute window. 4 levels: 0s, 60s, 5min, 30min. When the breaker is open, `getRuntimeHealthMultiplier` returns `SITE_RUNTIME_MIN_MULTIPLIER` (0.08) -- it does NOT skip the channel, it heavily penalizes it. When `breakerUntilMs` expires, the breaker closes automatically; there is no half-open probing with single-request pass-through.

**Severity**: HIGH. The spec describes a traditional half-open circuit breaker that does not exist. The TS uses a penalty-multiplier model instead.

### A4. "Sticky session" does not exist

**Spec claim** (Section "Channel Selection", Step 8):
```
如果 sticky session 启用 -> 检查 session 有无已绑定 channel, TTL 内复用
```

**TS reality**: There is NO sticky session by sessionID anywhere in the TS code. The closest mechanism is `stableFirstLastSelectedSiteByKey` (line 246), which tracks the last selected SITE for a rotation key `routeId:modelName` and rotates to the next site in round-robin fashion (line 3772-3796). This is site-level rotation, not session-based stickiness.

**Severity**: HIGH. The Go module `sticky.go` in the proposed structure would be implementing a feature that doesn't exist in the TS source.

### A5. Selection result is NOT cached

**Spec claim** (Section "Channel Selection", Step 9):
```
Cache 结果
```

**TS reality**: The TS caches routes (`routeCacheSnapshot`, line 1089) and route matches (`routeMatchCache`, line 1094), but NEVER caches the final selected channel. Every call to `selectChannel` recomputes the weighted selection from scratch. The two caches that do exist:

- `routeCacheSnapshot`: caches the list of enabled routes, TTL = `tokenRouterCacheTtlMs` (min 100ms, line 1102)
- `routeMatchCache`: per-route cache of channels+accounts+sites, TTL = `tokenRouterCacheTtlMs` (line 1142)

**Severity**: MEDIUM. The spec implies a result cache that the TS does not have.

### A6. Acceptance criterion for cooldown ceiling is wrong

**Spec claim** (Acceptance Criteria):
```
cooldown 指数退避正确: fail 1->cooldown 1, fail 2->cooldown 2, ... ceiling 30d
```

**TS reality**: The actual mapping is (in seconds): fail=1 -> 15, fail=2 -> 15, fail=3 -> 30, fail=4 -> 45, fail=5 -> 75, fail=6 -> 120, etc. (Fibonacci * 15), capped at 30 days = 2,592,000 seconds. The spec's "fail 1->cooldown 1, fail 2->cooldown 2" is incorrect on every count -- wrong algorithm, wrong values, wrong growth rate.

**Severity**: HIGH. This acceptance criterion, if tested literally, will fail against the correct algorithm.

---

## Missing Details

### M1. Site Runtime Health subsystem (entirely absent)

The spec mentions `siteRuntimeHealth` briefly in Step 5 but describes it only as "使用 siteRuntimeHealth 降低不可用站点的权重." The TS implementation (lines 79-997, ~900 lines) is a full-fledged health tracking system that is completely undescribed:

- **Penalty scoring**: Failure context analysis distinguishes 8 categories (usage limit 0.4, model failure 0.9, protocol failure 0.6, validation 0.25, 5xx transient 2.5, 429 rate limit 2.2, 401/403 auth 1.8, 4xx 0.9, unknown 1.2)
- **Penalty decay**: Half-life of 10 minutes (`SITE_RUNTIME_HEALTH_DECAY_HALF_LIFE_MS = 10 * 60 * 1000`)
- **Success reward**: `penaltyScore = max(0, penaltyScore * 0.2 - 0.3)` (line 768)
- **Latency EMA**: Alpha=0.3, baseline=2500ms, window=30000ms, max penalty=0.35
- **Breaker**: 4 levels with streak=3 threshold in 5-min window
- **Model-scoped state**: Separate health tracking per (site, model) pair
- **Recent outcomes**: Blended global+model with configurable model weight (0.65), half-life decay (30 min), Bayesian-style prior (1 success + 1 failure)
- **Persistence**: Debounced (500ms) serialization to DB `settings` table, stale TTL 7 days, idle TTL 12 hours

**Severity**: BLOCKING. The TS routing depends heavily on this subsystem for multiplier calculation. The spec needs a complete section on it.

### M2. Three routing strategies (spec only describes one)

The TS supports three strategies (`routeRoutingStrategy.ts`):
- `weighted` -- priority-layered weighted random (spec partially covers)
- `round_robin` -- ordered by `lastSelectedAt || lastUsedAt`, no weights
- `stable_first` -- primary/observation pool split with site rotation

The spec describes only the weighted flow. Both `round_robin` and `stable_first` are completely absent.

**Severity**: BLOCKING. Two of three routing strategies are undocumented.

### M3. Stable_first strategy details (entirely absent from spec)

The `stable_first` strategy (lines 1691-1836, 2129-2296) is approximately 400 lines of routing logic:

- **Pool plan building** (`buildStableFirstPoolPlan`): Groups candidates by site, selects a leader per site, computes `effectiveSuccessRate` from blended recent+historical data, determines "trusted" sites (confidence >= 0.5 or 8+ historical calls), splits into primary/observation pools based on `STABLE_FIRST_PRIMARY_SUCCESS_RATE_RATIO` (0.92)
- **Observation rotation**: Every 24 primary requests (`STABLE_FIRST_OBSERVATION_REQUEST_INTERVAL`), allows one observation candidate with a 30-minute site cooldown
- **Site rotation**: Within the primary pool, rotates through site leaders ordered by priority, skipping the last-selected site
- **Weighted selection within pool**: Uses a success-rate-squared formula (`recentSuccessRate ** 2`) not the standard weight formula

**Severity**: BLOCKING.

### M4. Round_robin strategy details (absent from spec)

The `round_robin` strategy (lines 94-100, 2057-2127):

- Uses `lastSelectedAt || lastUsedAt` ascending ordering
- Has its own cooldown: `ROUND_ROBIN_FAILURE_THRESHOLD = 3`, levels [0, 10min, 1h, 24h]
- Ignores priority tiers (runs globally across all eligible candidates)
- `filterSiteRuntimeBrokenCandidatesByModel` still applies

**Severity**: HIGH.

### M5. OAuth route unit management (absent from spec)

The TS has extensive OAuth route unit support (~200 lines, lines 1531-1540, 3073-3220):

- Members with individual cooldown, success/fail tracking
- Two strategies: `round_robin` and `stick_until_unavailable`
- Membership eligibility checks (cooling down, token availability)
- Member selection on dispatch
- Member-level success/failure recording
- Route unit member cooldown with Fibonacci backoff or round_robin tiered cooldown

**Severity**: HIGH. Unless OAuth route units are out of scope, this is a major gap.

### M6. DownstreamPolicy details (under-specified)

The spec mentions only `excludedSiteIds` and `excludedCredentialRefs`, but the TS also handles:

- `allowedRouteIds` (line 3039): Filter routes to only those in the allowed set
- `supportedModels` (line 3034): Pattern-based model allowlisting
- `denyAllWhenEmpty` (line 1450): When no supported models and no allowed routes, deny all (if true)
- `siteWeightMultipliers` (line 3612): Per-site weight multipliers applied to contribution
- `isModelAllowedByDownstreamPolicy` (line 1444): Pre-check before any route matching

**Severity**: HIGH.

### M7. Channel load / concurrency multiplier (absent from spec)

Lines 1555-1572 implement `resolveChannelRuntimeLoadMultiplier`:
- Active penalty: `activeRatio * 0.28`
- Waiting penalty: `waitingRatio * 0.32`
- Saturation penalty: `0.12`
- Clamped to [0.18, 1]

This is applied as a multiplier on every contribution. Not mentioned in the spec.

**Severity**: MEDIUM.

### M8. Site historical health metrics (absent from spec)

Lines 1611-1689 implement `buildSiteHistoricalHealthMetrics`:
- Aggregates success/fail/latency from all channels per site
- Applies sample-based confidence factor (`totalCalls / 24`)
- Success penalty: `(1 - successRate) * 0.55 * sampleFactor`
- Latency penalty: based on baseline 2000ms, window 20000ms, max penalty 0.18
- Multiplier clamped to [0.45, 1]

**Severity**: MEDIUM.

### M9. Model mapping and display name matching (absent from spec)

- `resolveMappedModel` (line 1472): Applies route's `modelMapping` (JSON object of pattern->target) to transform requested model name
- Display name matching: Routes can be matched by display name in addition to model pattern
- Explicit group routes: Combine multiple source routes under a display name
- `bypassSourceModelCheck` and `useChannelSourceModelForCost` flags for display-name-matched routes

**Severity**: MEDIUM.

### M10. recordSuccess / recordFailure complexity (under-specified)

The spec describes simple counter operations:
```
RecordSuccess: consecutiveFailCount = 0, cooldownLevel = 0
RecordFailure: consecutiveFailCount++, cooldownLevel = min(consecutiveFailCount, maxCooldownLevel), cooldownUntil = ...
```

The TS has MUCH more:
- OAuth route unit member tracking (separate success/fail path)
- `recordProbeSuccess` (line 2532): Different success path for probes that clears cooldown for ALL credential-scoped channels, not just the one that succeeded. Also clears sibling channel cooldowns.
- `recordFailure` (line 2698): Short window limit cooldown for 429s, credential-scoped channel fan-out for short-window-limit, round_robin vs weighted cooldown logic branching
- `patchCachedChannel`: In-memory cache patching after DB writes
- `invalidateRouteScopedCache`: Cache invalidation after member state changes

**Severity**: HIGH.

### M11. Short window limit cooldown (absent from spec)

Lines 538-565: When a 429 response is detected as a usage limit (via `isUsageLimitRateLimitFailure`), the cooldown is set to the quota reset hint (from Codex's `lastLimitResetAt`) OR 5 minutes, rather than using Fibonacci backoff. Also fans out to all credential-scoped channels.

**Severity**: MEDIUM.

### M12. Route visibility filtering (absent from spec)

`buildVisibleEnabledRoutes` (line 1380): Hides exact-model routes that are covered by explicit_group routes or wildcard routes with custom display names. This affects `getAvailableModels()` output.

**Severity**: LOW.

### M13. Pricing reference refresh workflow (absent from spec)

Lines 1934-1964 and 2408-2445: `refreshPricingReferenceCosts` loads pricing catalog data for all candidate channels, with deduplication by `siteId:accountId` key. Used by `routeDecisionRefreshService.ts`.

**Severity**: MEDIUM.

### M14. Route match cache invalidation patterns (under-specified)

The spec says "清除路由缓存" but the TS has several granular invalidation patterns:
- `invalidateTokenRouterCache()` (line 1252): Full clear of routes + matches + stable_first caches
- `invalidateRouteScopedCache(routeId)` (line 1246): Clear match cache + stable_first caches for one route
- `patchCachedChannel` (line 1218): In-place mutation of cached channel data after DB updates
- `clearStableFirstCachesForRoute` (line 1227): Clear only stable_first rotation state for a route

**Severity**: LOW.

### M15. Route cooldown clear (routeCooldownService.ts, absent from spec)

The `clearRouteCooldown` function handles:
- Resolving cooldown-clear route IDs (including explicit_group source route expansion)
- Clearing channel failure state via `tokenRouter.clearChannelFailureState`
- Clearing decision snapshots
- Propagating snapshot clears to dependent explicit_group routes

**Severity**: LOW.

### M16. Cost signal resolution order (absent from spec)

`resolveEffectiveUnitCost` (lines 1574-1609) resolves cost in priority order:
1. `observed`: totalCost/successCount from channel metrics
2. `configured`: account.unitCost
3. `catalog`: pricing catalog via `getCachedModelRoutingReferenceCost`
4. `fallback`: `config.routingFallbackUnitCost || 1`

All clamped to min `MIN_EFFECTIVE_UNIT_COST` (1e-6).

**Severity**: MEDIUM.

---

## Edge Cases Not Covered

### E1. Zero-candidate scenarios
- All channels cooldown: spec says "返回错误, 不选任何 channel" -- TS returns `null` (not an error), which is correct behavior but the wording "返回错误" is ambiguous.
- All channels excluded by downstream policy: spec says "返回错误" -- TS returns `null`.
- All channels filtered by breaker: TS `filterSiteRuntimeBrokenCandidatesByModel` returns original list if all are broken (line 1067), so selection continues with broken candidates.

### E2. Excluded channel handling in route units
Spec doesn't address how `excludeChannelIds` interacts with OAuth route units. In TS, when a route unit channel is excluded (line 3339-3342), the code explicitly does NOT exclude it -- instead it switches to a different member within the same unit.

### E3. Weight zero behavior
Spec says "Weight 全 0 -> 按 priority 排序返回第一个." TS: `(weight + 10)` in contribution formula means weight=0 still contributes via the +10 offset. For `stable_first` strategy, weight is not used at all.

### E4. No token routes
Spec says "0 个 token_routes -> 创建默认 route." TS: No default route creation; simply returns null when no route matches.

### E5. Cache TTL boundary
Spec says "Cache TTL 内同一 session -> 使用相同 channel (即使权重变)." TS: There is NO session-based channel caching. Caches are route-level and route-match-level, not selection-result-level. The same rotation key (routeId:modelName) in stable_first will rotate through sites, not stick.

### E6. Explicit group with no valid source routes
TS `loadRouteMatch` (line 1148-1159) filters source routes to only enabled, non-group, exact-pattern routes. If none match, the route has zero channels -- spec doesn't cover this.

### E7. Concurrent siteRuntimeHealth persistence
TS uses a debounce timer + in-flight promise guard (lines 827-852) to prevent concurrent writes. Spec doesn't mention persistence at all.

### E8. Breaker open with all candidates broken
TS `filterSiteRuntimeBrokenCandidatesByModel` returns all candidates if the filtered list would be empty (line 1067: `healthy.length > 0 ? healthy : candidates`). The spec's half-open model would handle this differently.

### E9. Stable_first observation when no observation candidates eligible
When all observation candidates are still in site cooldown (`stableFirstObservationSiteCooldownByKey`), the system falls back to primary. Spec doesn't cover this.

### E10. Route display name match priority
TS `findRoute` (line 3044) has a specific priority order: explicit_group exact display name match > exact model match > display name match > wildcard pattern match. Spec doesn't describe match priority.

---

## Incorrect Details

### I1. Module structure `sticky.go`
The spec proposes `sticky.go` for `StickySession: session->channel 粘滞`. No such mechanism exists in the TS. Remove this module or redesign to match the `stable_first` site rotation tracking.

### I2. Module structure `circuit_breaker.go`
The spec proposes `circuit_breaker.go` for `CircuitBreaker: site 级断路器` with half-open semantics. The TS has no classic circuit breaker. The site runtime health breaker is fundamentally different (penalty multiplier, no half-open probing). Either rename/redesign this module or implement the TS model.

### I3. `CooldownManager.IsCooledDown(channel)` interface
Spec describes this as a separate service. In TS, cooldown check is `isChannelRecentlyFailed()` which reads `channel.failCount` and `channel.lastFailAt` from the DB row, not a separate service state. Additionally, eligibility check (line 3374) also checks `channel.cooldownUntil > nowIso` directly on the channel object.

### I4. `selectNextChannel` signature
Spec says: `SelectNextChannel(previousChannel, model, sessionID)` -- takes the previous channel object. TS `selectNextChannel` (line 1867) takes `excludeChannelIds: number[]`, not a channel object, and has no `sessionID` parameter.

### I5. Route matching step described as "查 token_routes WHERE model_pattern matches"
Spec says Step 2: "查 token_routes WHERE model_pattern matches 请求模型." TS matches are in-memory against the cached route list, not a DB query. More importantly, the match logic includes explicit_group display name matching and has a priority order (see E10).

### I6. `weight_score = channel.weight x config.BaseWeightFactor`
TS: `(weight + 10) * (baseWeightFactor + normalizedVS * valueScoreFactor)`. The `+ 10` offset and the multiplication with value-score-adjusted factor are not captured.

### I7. `account.value_score`
The spec uses `account.value_score` field in the formula. No such field exists in the TS schema or code. The TS computes a derived `valueScores` array from cost, balance, and usage.

### I8. `balance_score = (balance / quota) x config.BalanceWeight`
TS: `balanceWeight * balance` -- there is no division by quota. The `balance` field is used directly.

### I9. `usage_score = (1 - usage_trend) x config.UsageWeight`
TS: `usageWeight * (1 / Math.max(totalUsed, 1))` -- uses `1 / totalUsed`, not `1 - usage_trend`. There is no `usage_trend` concept.

### I10. "乘以 siteWeightMultipliers (来自 downstreamPolicy)"
TS: `siteWeightMultipliers` are multiplied by `siteGlobalWeight` to get `combinedSiteWeight`, then the combined weight is multiplied into contribution. The spec makes it sound like a standalone multiplier step.

### I11. Rebuild logic
Spec `RebuildRoutesOnly` Step 1: "删除所有自动生成的 route_channels (manual_override=false)." TS rebuild is delegated to `modelService.rebuildTokenRoutesFromAvailability()` -- the referenced TS files (`routeRefreshWorkflow.ts`) are thin wrappers. The actual rebuild logic is not in the reviewed files. The spec's description of rebuild may be aspirational (for the Go port) rather than a description of the TS behavior, but this should be explicitly noted.

### I12. RecordSuccess cooldownLevel = 0
Spec: `RecordSuccess: consecutiveFailCount = 0, cooldownLevel = 0`. TS `recordSuccess` (line 2509-2518) clears `cooldownUntil`, `lastFailAt`, `consecutiveFailCount`, `cooldownLevel` -- matches spec. But also updates `successCount`, `totalLatencyMs`, `totalCost`, `lastUsedAt`, which the spec omits.

---

## Summary

| Category | Count |
|---|---|
| Accuracy Issues | 6 (3 BLOCKING, 2 HIGH, 1 MEDIUM) |
| Missing Details | 16 (3 BLOCKING, 5 HIGH, 6 MEDIUM, 2 LOW) |
| Edge Cases Not Covered | 10 |
| Incorrect Details | 12 |

**Verdict**: **BLOCKED**

The spec describes a simplified routing system that fundamentally differs from the TS implementation in several critical areas:

1. **The weight formula is entirely wrong** -- the spec's additive 5-term formula is not what the TS computes (which is a multiplicative contribution with normalization, per-site channel counts, runtime multipliers, load multipliers, historical health multipliers, and fallback penalties).

2. **Fibonacci backoff, not exponential** -- the cooldown algorithm uses `15 * fib(failCount)`, not `backoff^level`.

3. **Three routing strategies, not one** -- `stable_first` and `round_robin` are undocumented but represent the majority of the routing code.

4. **The site runtime health subsystem (~900 lines)** is entirely absent from the spec, yet it is central to how multipliers are computed for every routing decision.

5. **The "circuit breaker" model is wrong** -- no half-open probing exists; the TS uses penalty multipliers, not binary open/closed states.

The spec cannot be approved until the weight formula, cooldown algorithm, routing strategies, and site runtime health subsystem are corrected and fully documented.

**Recommended next steps**:
1. Rewrite the weight calculation section to match the TS `calculateWeightedSelection` formula
2. Replace "exponential backoff" with the correct Fibonacci-based formula
3. Add complete sections for `stable_first` and `round_robin` strategies
4. Add a comprehensive section on the site runtime health subsystem (penalty scoring, decay, breaker, EMA latency, persistence)
5. Remove or redesign `sticky.go` (no sticky session exists)
6. Redesign `circuit_breaker.go` to match the penalty-multiplier model
7. Add OAuth route unit management section
8. Correct the `selectNextChannel` signature
9. Add downstream policy details (allowedRouteIds, supportedModels, denyAllWhenEmpty, siteWeightMultipliers)
10. Document the cost signal resolution order
