# P7: TokenRouter -- route selection engine

**S.U.P.E.R**: S (split!) -- U (break cycles!) -- P (interface contracts!) -- R (replaceable!) | **Depends on**: P3 + P4 | **Size**: XL

> S.U.P.E.R critical fix: the TS `tokenRouter.ts` is a 3800-line monolith with a circular dependency on `modelService`.
> The Go port MUST be split into independent modules with interface contracts and unidirectional dependencies.

## Original TS reference
- `D:\Code\TokenDance\metapi\src\server\services\tokenRouter.ts` (~3800 LOC, largest monolith)
- `D:\Code\TokenDance\metapi\src\server\services\routeRoutingStrategy.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeCooldownService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeRefreshWorkflow.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeDecisionRefreshService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeDecisionSnapshotStore.ts`
- `D:\Code\TokenDance\metapi\src\server\services\modelService.ts` (circular dependency)
- `D:\Code\TokenDance\metapi\src\server\services\modelPricingService.ts`

## Go module structure (split!)
```
routing/
  router.go              # TokenRouter: main entry point, composes sub-modules
  selector.go            # ChannelSelector: selectChannel, selectNextChannel, selectPreferredChannel
  matcher.go             # ModelMatcher: model name regex/glob matching, display-name matching
  cooldown.go            # CooldownManager: Fibonacci failure backoff, short-window-limit cooldown
  weights.go             # WeightCalculator: weighted random selection algorithm
  cache.go               # RouteCache: token_router_cache_ttl_ms caching (routes + route matches)
  runtime_health.go      # SiteRuntimeHealth: penalty scoring, decay, breaker, latency EMA, persistence
  stable_first.go        # StableFirstStrategy: pool plan, site rotation, observation gating
  round_robin.go         # RoundRobinStrategy: lastSelectedAt ordering, tiered cooldown
  route_units.go         # OAuthRouteUnitManager: member selection, eligibility, success/fail tracking
  workflow.go            # RouteRefreshWorkflow: rebuildRoutes, refreshModels
  decision.go            # RouteDecisionService: route decision query/batch/refresh
  snapshot.go            # RouteDecisionSnapshotStore: decision snapshot persistence
  pricing.go             # PricingReference: model pricing cost reference

  ports.go               # Interface contracts!
    type ModelProvider interface {
        GetAvailableModels(ctx, accountID) ([]ModelInfo, error)
        RefreshModelsForAccount(ctx, accountID) error
    }
    type TokenProvider interface {
        GetTokens(ctx, accountID) ([]Token, error)
        GetDefaultToken(ctx, accountID) (*Token, error)
    }
    type PricingProvider interface {
        GetReferenceCost(ctx, model, siteID) (float64, error)
    }
```

---

## 1. Routing strategies

The system supports three routing strategies, defined per route in `route.routingStrategy`. The default is `weighted`.

| Strategy       | Value          | Description |
|----------------|----------------|-------------|
| `weighted`     | `"weighted"`   | Priority-layered weighted random selection with contribution scoring |
| `round_robin`  | `"round_robin"`| Ordered by `lastSelectedAt || lastUsedAt` ascending, ignores priority tiers, separate cooldown |
| `stable_first` | `"stable_first"`| Primary/observation pool plan, site rotation within primary pool, success-rate-squared scoring |

Normalization: any unrecognized value falls back to `weighted`.

---

## 2. Channel Selection: `weighted` strategy (default)

### 2.1 Entry points

```
SelectChannel(requestedModel, downstreamPolicy) → SelectedChannel | null
SelectNextChannel(requestedModel, excludeChannelIds, downstreamPolicy) → SelectedChannel | null
SelectPreferredChannel(requestedModel, preferredChannelId, downstreamPolicy, excludeChannelIds) → SelectedChannel | null
```

All return `null` (not an error) when no channel is available.

### 2.2 Flow

**Step 1: Downstream policy pre-check**
```
if !isModelAllowedByDownstreamPolicy(requestedModel, downstreamPolicy):
    return null
```

`isModelAllowedByDownstreamPolicy` logic:
- If `downstreamPolicy.supportedModels` is non-empty: check if requested model matches any pattern.
  If yes, allow. If no BUT `downstreamPolicy.allowedRouteIds` is non-empty, allow (checked at route level later).
  If neither, check `downstreamPolicy.denyAllWhenEmpty`: if true, deny (return false); if false, allow (return true).
- If `supportedModels` is empty AND `allowedRouteIds` is empty: return `!(denyAllWhenEmpty === true)`.

**Step 2: Route matching**

Find a route matching the requested model. Match priority (first wins):

1. `explicit_group` route whose `displayName` equals the requested model exactly
2. Non-group route with an exact model pattern equal to the requested model
3. Non-group route whose `displayName` equals the requested model exactly
4. Non-group route whose `modelPattern` regex/glob matches the requested model

If `downstreamPolicy.allowedRouteIds` is non-empty and no `supportedModels` pattern matched, filter routes to only those in the allow set.

If no match, return `null` (no default route is created).

**Step 3: Load route match (channels + accounts + sites + tokens)**

Load all `route_channels` joined with `accounts`, `sites`, `accountTokens` for the matched route (and its source routes if `explicit_group`). Also load OAuth route unit summaries and members if any channels reference OAuth route units.

Source model resolution: use `channel.sourceModel` if set, otherwise fall back to the source route's exact model pattern.

**Step 4: Model mapping**

Apply `route.modelMapping` (JSON object of pattern -> target model name):
- Exact match on requested model first
- Then first pattern match via regex/glob
- If no mapping matches, use the requested model as-is

**Step 5: Candidate eligibility filtering**

Each channel must pass ALL of these checks (reasons accumulate; if any reason exists, the channel is ineligible):

| Check | Condition for failure |
|-------|-----------------------|
| Source model match | `channel.sourceModel` does not support requested model (unless `bypassSourceModelCheck` is true; true for display-name-matched routes) |
| Channel enabled | `channel.enabled !== true` |
| Account status | For explicit-token channels: `account.status === 'disabled'`. For others: `account.status !== 'active'` |
| Site status | `site.status === 'disabled'` |
| Downstream exclusion | Site ID in `downstreamPolicy.excludedSiteIds`, or credential ref in `downstreamPolicy.excludedCredentialRefs` |
| Already tried | Channel ID in `excludeChannelIds` |
| Token available | No valid token value resolvable |
| Cooldown | `channel.cooldownUntil > now` |
| OAuth route unit member availability | No eligible members remain (checks member-level: status, token, site, cooldown) |

Special case for OAuth route units: when the outer channel ID is in `excludeChannelIds`, it is NOT excluded -- instead, failover happens within the same unit by switching to a different member.

**Step 6: Priority layering**

Group eligible candidates by `channel.priority`. Process in ascending priority order. For each priority layer:

a. Filter site runtime breaker-broken candidates via `filterSiteRuntimeBrokenCandidatesByModel`. Candidates whose site's global breaker OR model-level breaker is open are avoided. BUT if the filter would result in zero candidates, all original candidates proceed (breaker-broken channels are not completely skipped, just penalized).

b. Filter recently-failed candidates via `filterRecentlyFailedCandidates`. Candidates whose `isChannelRecentlyFailed()` returns true are avoided. BUT if all candidates are recently failed, all proceed.

c. Compute weighted random selection (see Section 7).

d. If a candidate is selected from this layer, stop. Otherwise try the next priority.

**Step 7: Finalize dispatch**

For the selected channel:
- If it is an OAuth route unit: select a member (see Section 9), resolve member token value
- Resolve `channel token value` (prefers account token over API token for OAuth; prefers token row over accessToken over apiToken)
- If no token value, return null
- Record selection: update `channel.lastSelectedAt` in DB and in-memory cache
- Return `SelectedChannel` with `tokenValue`, `tokenName`, `actualModel`

**Step 8: No caching of final selection**

Selection never caches the final chosen channel. Every call to `selectChannel` recomputes from fresh data (subject to route+match caching with TTL).

---

## 3. Channel Selection: `round_robin` strategy

Activated when `route.routingStrategy === 'round_robin'`.

### 3.1 Flow

1. Same eligibility filtering as weighted (Steps 1-5 in Section 2.2)
2. Filter breaker-broken by model (same as weighted)
3. Sort eligible candidates by `lastSelectedAt || lastUsedAt` ascending, then `lastUsedAt` ascending, then channel ID ascending
4. Select the first candidate in sorted order
5. Finalize dispatch (same as weighted Step 7)

### 3.2 Round-robin cooldown

Round-robin channels use a tiered cooldown based on consecutive failures, NOT the Fibonacci backoff:

- Threshold: 3 consecutive failures (`ROUND_ROBIN_FAILURE_THRESHOLD = 3`)
- On reaching threshold: increment `cooldownLevel` (0-3), reset `consecutiveFailCount` to 0
- Cooldown levels: `[0s, 10min, 1hr, 24hr]`
- Cooldown duration is clamped by `clampFailureCooldownMs()` (respects `tokenRouterFailureCooldownMaxSec`)

---

## 4. Channel Selection: `stable_first` strategy

Activated when `route.routingStrategy === 'stable_first'`.

### 4.1 Pool plan building (`buildStableFirstPoolPlan`)

1. Group candidates by site. Within each site, select a "leader" -- the candidate with the earliest `lastSelectedAt || lastUsedAt` (ties broken by channel ID).

2. For each site leader, compute health details:
   - `SiteRuntimeHealthDetails` via `getSiteRuntimeHealthDetails` (blended global + model-level runtime health)
   - Site historical health metrics via `buildSiteHistoricalHealthMetrics`

3. Compute `effectiveSuccessRate` for each site:
   ```
   effectiveSuccessRate = (recentSuccessRate * recentConfidence) + (fallbackRate * (1 - recentConfidence))
   ```
   where `fallbackRate = historicalSuccessRate ?? SITE_RECENT_SUCCESS_FALLBACK_RATE (0.5)`

4. Determine "trusted" sites: `recentConfidence >= 0.5` OR `historicalTotalCalls >= 8`

5. Sort sites by `effectiveSuccessRate` descending (tie: compare leader order).

6. Split into pools:
   - **Primary pool**: trusted sites whose `effectiveSuccessRate >= bestRate * STABLE_FIRST_PRIMARY_SUCCESS_RATE_RATIO (0.92)`
   - **Observation pool**: all other sites
   - If primary pool is empty, promote the top site from observation to primary

7. Observation candidates get a reason:
   - Trusted but rate below threshold: "观察池: 近期成功率暂时落后, 仅灰度真实流量会命中"
   - Not trusted: "观察池: 近期样本不足, 仅灰度真实流量会命中"

### 4.2 Rotation and observation gating

**Rotation key**: `{routeId}:{normalizedModelAlias}` (model name normalized: lowercase, strip vendor prefix)

**Observation interval**: Every 24 primary-pool requests (`STABLE_FIRST_OBSERVATION_REQUEST_INTERVAL`), one observation candidate may be used to test real traffic.

**Observation site cooldown**: An observation site that was just probed cannot be probed again for `STABLE_FIRST_OBSERVATION_SITE_COOLDOWN_MS` (30 minutes), tracked per `{rotationKey}:{siteId}`.

**Selection logic**:
1. If primary pool has candidates AND (not time for observation OR all observation sites in cooldown): use primary pool
2. If time for observation AND an eligible observation candidate exists: use observation pool
3. If primary pool is empty: use observation pool (with special message)

**Within the selected pool**: Apply `stable_first` weighted selection (see Section 7), then site rotation.

### 4.3 Site rotation within primary pool

Within the primary pool's site leaders, apply site-level rotation:
- Get `stableSiteLeaderIndices`: all site leaders whose contribution is close to best (within ratio `STABLE_FIRST_SITE_SCORE_RATIO = 0.92`)
- Order by priority, then channel ID
- Skip the last-selected site for this rotation key (using `stableFirstLastSelectedSiteByKey` map)
- Select the next site in order (wrapping around)
- From the selected site, pick the highest-ranked candidate

### 4.4 Progress tracking

After each selection, update `stableFirstObservationProgressByKey`:
- If observation was used: reset progress counter to 0, set `lastObservationAtMs`, set site cooldown
- Otherwise: increment request count

State maps have maximum sizes:
- `MAX_STABLE_FIRST_ROTATION_KEYS = 1024`
- `MAX_STABLE_FIRST_OBSERVATION_PROGRESS_KEYS = 1024`
- `MAX_STABLE_FIRST_OBSERVATION_SITE_COOLDOWN_KEYS = 4096`

---

## 5. Site Runtime Health subsystem

This subsystem tracks per-site and per-(site, model) health state in memory, with debounced persistence to the `settings` table.

### 5.1 State structure

Each `SiteRuntimeHealthState` tracks:

| Field | Type | Description |
|-------|------|-------------|
| `penaltyScore` | float64 | Accumulated failure penalty, decayed over time |
| `latencyEmaMs` | float64 or null | Exponential moving average of latency |
| `transientFailureStreak` | int | Consecutive transient failures within window |
| `lastTransientFailureAtMs` | int64 or null | Timestamp of last transient failure |
| `recentSuccessCount` | float64 | Decayed success count |
| `recentFailureCount` | float64 | Decayed failure count |
| `recentWindowUpdatedAtMs` | int64 | Timestamp of last decay refresh |
| `breakerLevel` | int (0-3) | Current breaker severity level |
| `breakerUntilMs` | int64 or null | Breaker expiry timestamp |
| `lastUpdatedAtMs` | int64 | Timestamp of last mutation |
| `lastFailureAtMs` | int64 or null | Timestamp of last failure |
| `lastSuccessAtMs` | int64 or null | Timestamp of last success |

Two maps in memory:
- `siteRuntimeHealthStates: Map<siteId, SiteRuntimeHealthState>` -- global per-site state
- `siteModelRuntimeHealthStates: Map<siteId, Map<modelKey, SiteRuntimeHealthState>>` -- per-(site, model) state

### 5.2 Penalty scoring (`resolveSiteRuntimeFailurePenalty`)

Based on failure context (HTTP status + error text), assigns an additive penalty to `penaltyScore`:

| Failure category | Pattern match | Penalty |
|------------------|---------------|---------|
| Usage limit (429 + usage-limit patterns) | `usage_limit_reached`, `quota exceeded`, etc. | 0.4 |
| Model failure | `unsupported model`, `model not supported`, etc. | 0.9 |
| Protocol failure | `unsupported legacy protocol`, `unknown endpoint`, etc. | 0.6 |
| Validation failure | `invalid request body`, `validation`, `malformed`, etc. | 0.25 |
| Transient (5xx OR transient patterns) | `bad gateway`, `gateway timeout`, `service unavailable`, `econnreset`, etc. | 2.5 |
| 429 (rate limit, not usage limit) | status 429, not matching usage-limit text | 2.2 |
| 401 / 403 (auth) | status 401 or 403 | 1.8 |
| Other 4xx | status 400-499, else | 0.9 |
| Unknown | anything else | 1.2 |

Transient classification: 5xx, 429, or matches transient patterns -- but EXCLUDES usage limit, model failure, protocol failure, and validation failure (those are NOT transient even if they have 4xx/5xx status).

### 5.3 Penalty decay

Every time state is read, penalty is decayed with half-life:
```
halfLife = SITE_RUNTIME_HEALTH_DECAY_HALF_LIFE_MS (10 minutes)
decayFactor = 0.5 ^ (elapsedMs / halfLife)
penaltyScore = penaltyScore * decayFactor
```

### 5.4 Recent outcome tracking

Success and failure counts decay with half-life `SITE_RECENT_OUTCOME_HALF_LIFE_MS` (30 minutes).

Bayesian prior: `SITE_RECENT_SUCCESS_PRIOR_SUCCESSES = 1`, `SITE_RECENT_SUCCESS_PRIOR_FAILURES = 1`.
```
successRate = (successCount + 1) / (successCount + failureCount + 2)
confidence = clamp(sampleCount / SITE_RECENT_SUCCESS_CONFIDENCE_SAMPLES(12), 0, 1)
```

Blending global and model-level:
```
modelWeight = SITE_RECENT_MODEL_WEIGHT (0.65)
globalWeight = 0.35
blended = blend(globalSnapshot * globalWeight, modelSnapshot * modelWeight)
```
If model snapshot has 0 samples, use global only.

### 5.5 Latency EMA

- Alpha: `SITE_RUNTIME_LATENCY_EMA_ALPHA = 0.3`
- Baseline: `SITE_RUNTIME_LATENCY_BASELINE_MS = 2500ms`
- Window: `SITE_RUNTIME_LATENCY_WINDOW_MS = 30000ms`
- Max penalty: `SITE_RUNTIME_MAX_LATENCY_PENALTY = 0.35`

```
latencyEmaMs = firstCall ? rawLatencyMs : (previousEma * 0.7 + rawLatencyMs * 0.3)
latencyPenaltyRatio = clamp((latencyEmaMs - 2500) / 30000, 0, 1)
latencyFactor = 1 - (latencyPenaltyRatio * 0.35)
```

### 5.6 Breaker

Triggered by transient failures:
- Streak threshold: `SITE_RUNTIME_BREAKER_STREAK_THRESHOLD = 3` consecutive transient failures within `SITE_TRANSIENT_STREAK_WINDOW_MS` (5 minutes)
- 4 levels: `[0ms, 60s, 5min, 30min]`
- On trigger: `breakerLevel = min(breakerLevel + 1, 3)`, `breakerUntilMs = nowMs + breakerMs`
- While breaker open: `getRuntimeHealthMultiplier` returns `SITE_RUNTIME_MIN_MULTIPLIER` (0.08)
- When `breakerUntilMs` expires: breaker closes automatically -- NO half-open probing
- Non-transient failures reset the transient streak to 0

### 5.7 Health multiplier

```
isBreakerOpen: breakerUntilMs > nowMs

if breakerOpen:
    return SITE_RUNTIME_MIN_MULTIPLIER (0.08)

failurePenaltyFactor = 1 / (1 + penaltyScore)
latencyPenaltyRatio = clamp((latencyEmaMs - 2500) / 30000, 0, 1)
latencyFactor = 1 - (latencyPenaltyRatio * 0.35)

return clamp(failurePenaltyFactor * latencyFactor, 0.08, 1)
```

### 5.8 Combined (global + model) multiplier

```
globalMultiplier = getRuntimeHealthMultiplier(globalState)
modelMultiplier = getRuntimeHealthMultiplier(modelState) ?? 1
combinedMultiplier = clamp(globalMultiplier * modelMultiplier, 0.08^2, 1)
```

This combined multiplier is what gets applied to routing contributions.

### 5.9 Success recording

On success:
```
penaltyScore = max(0, penaltyScore * 0.2 - 0.3)
transientFailureStreak = 0
breakerLevel = 0
breakerUntilMs = null
recentSuccessCount += 1
update latency EMA
```

### 5.10 Failure recording

On failure:
```
penaltyScore += resolveSiteRuntimeFailurePenalty(context)
if transient:
    if lastTransientFailure was within 5min window: transientFailureStreak += 1
    else: transientFailureStreak = 1
    if transientFailureStreak >= 3: trigger breaker
else:
    transientFailureStreak = 0
recentFailureCount += 1
lastFailureAtMs = now
```

### 5.11 `filterSiteRuntimeBrokenCandidatesByModel`

Given a list of candidates and a model name:
1. For each candidate, get `getSiteRuntimeHealthDetails(siteId, modelName)`
2. If `globalBreakerOpen || modelBreakerOpen`, mark as avoided with reason
3. Return filtered healthy candidates. If the filtered list would be empty, return ALL original candidates (no exclusion -- just heavy penalties from the multiplier).

### 5.12 Persistence

- Setting key: `"token_router_site_runtime_health_v1"`
- Persistence payload: `{ version: 1, savedAtMs, globalBySiteId, modelBySiteId }`
- Debounce: `SITE_RUNTIME_HEALTH_PERSIST_DEBOUNCE_MS` (500ms)
- Stale TTL: 7 days -- states not touched in 7 days are NOT persisted
- Idle TTL: 12 hours -- states with no penalty/samples/latency beyond this are NOT persisted unless they meet other criteria (breaker open, penalty >= 0.02, sampleCount > 0.01, latency > 0)
- Guard: in-flight promise tracking prevents concurrent DB writes
- Load-on-demand: lazy loaded on first `selectChannel` call

---

## 6. Channel cooldown system

### 6.1 Failure backoff (for `weighted` and `stable_first` strategies)

Fibonacci backoff with base multiplier:
```
FAILURE_BACKOFF_BASE_SEC = 15
MAX_FAILURE_BACKOFF_SEC = 30 * 24 * 60 * 60  (30 days)

fib(1) = 1, fib(2) = 1, fib(3) = 2, fib(4) = 3, fib(5) = 5, fib(6) = 8, ...

resolveFailureBackoffSec(failCount):
    normalizedFailCount = max(1, trunc(failCount ?? 0))
    return min(15 * fib(normalizedFailCount), MAX_FAILURE_BACKOFF_SEC)
```

Actual cooldown (in seconds by failCount):
| failCount | fib | backoff |
|-----------|-----|---------|
| 1 | 1 | 15s |
| 2 | 1 | 15s |
| 3 | 2 | 30s |
| 4 | 3 | 45s |
| 5 | 5 | 75s |
| 6 | 8 | 120s |
| 7 | 13 | 195s |
| 8 | 21 | 315s |
| ... | ... | ... |
| ceiling | -- | 30 days |

Secondary cap: `clampFailureCooldownMs(cooldownMs)` clamps to `min(cooldownMs, tokenRouterFailureCooldownMaxSec * 1000)`, with a floor of 1000ms. The max from config has a ceiling of `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC_CEILING`.

### 6.2 Short-window limit cooldown

For 429 responses that match usage-limit patterns (`usage_limit_reached`, `quota exceeded`, `rate limit`, etc.):

1. Parse quota reset hint from response (`parseCodexQuotaResetHint`): extract `resetAt` timestamp from error text
2. If not available, check `oauth.quota.lastLimitResetAt` for Codex provider accounts
3. Fallback: `SHORT_WINDOW_LIMIT_COOLDOWN_MS` (5 minutes)

The cooldown is set to the reset time (or now+5min). This overrides Fibonacci: `failCount` is NOT incremented (set to 0), `consecutiveFailCount` is reset, `cooldownLevel` is reset.

When fanning out for short-window-limit: ALL credential-scoped channels (same `tokenId` or same `accountId` without a `tokenId`) get the cooldown, not just the failing channel.

### 6.3 Round-robin cooldown (separate path)

See Section 3.2.

### 6.4 `isChannelRecentlyFailed`

```
isChannelRecentlyFailed(channel, nowMs, avoidSec):
    avoidMs = clampFailureCooldownMs(avoidSec * 1000)
    if avoidMs <= 0 OR failCount <= 0 OR no lastFailAt: return false
    failTs = parseIso(lastFailAt)
    return nowMs - failTs < avoidMs
```

`avoidSec` defaults to `resolveFailureBackoffSec(channel.failCount)`.

### 6.5 `filterRecentlyFailedCandidates`

Filters a candidate list keeping only those where `isChannelRecentlyFailed` is false. If all are recently failed, returns all of them (do not exclude everything).

### 6.6 Channel eligibility cooldown check

In `getCandidateEligibilityReasons`, there is also a direct cooldown check:
```
if channel.cooldownUntil && channel.cooldownUntil > nowIso:
    reasonParts.push('冷却中')
```
This catches cooldowns set by the short-window-limit path (which uses `cooldownUntil` but may not increment `failCount`).

---

## 7. Weighted random selection algorithm

### 7.1 `calculateWeightedSelection` -- weighted mode

**Input config**: `routingWeights: { baseWeightFactor, valueScoreFactor, costWeight, balanceWeight, usageWeight }`

**Step 1: Value scores**
```
For each candidate:
    unitCost = resolveEffectiveUnitCost(candidate, modelName).unitCost
    balance = account.balance || 0
    totalUsed = max(successCount + failCount, 1)

    valueScore = costWeight * (1/unitCost) + balanceWeight * balance + usageWeight * (1/totalUsed)
```

**Step 2: Min-max normalization**
```
maxVS = max(valueScores, 0.001)
minVS = min(valueScores, 0)
range = maxVS - minVS || 1
normalizedVS[i] = (valueScores[i] - minVS) / range
```

**Step 3: Base contribution**
```
baseContribution[i] = (weight + 10) * (baseWeightFactor + normalizedVS[i] * valueScoreFactor)
```
Note: the `+ 10` offset on weight means even weight=0 channels contribute. `weight` defaults to 10.

**Step 4: Site channel count normalization**
```
siteChannels = count of candidates for this site
baseContribution[i] /= siteChannels
```
This prevents sites with many tokens/channels from being over-favored.

**Step 5: Site weight**
```
downstreamSiteMultiplier = downstreamPolicy.siteWeightMultipliers[siteId] ?? 1
siteGlobalWeight = site.globalWeight (default 1)
combinedSiteWeight = siteGlobalWeight * downstreamSiteMultiplier
contribution *= combinedSiteWeight
```

**Step 6: Runtime health multiplier**
```
contribution *= getSiteRuntimeHealthDetails(siteId, modelName).combinedMultiplier
```
This is the combined global+model runtime health multiplier (Section 5.8).

**Step 7: Site historical health multiplier**
```
contribution *= siteHistoricalHealthMetrics[siteId].multiplier
```
See Section 7.3.

**Step 8: Runtime load multiplier**
```
contribution *= resolveChannelRuntimeLoadMultiplier(channelLoadSnapshot)
```
See Section 7.4.

**Step 9: Fallback cost penalty**
```
if effectiveUnitCost.source === 'fallback':
    contribution *= 1 / max(1, effectiveUnitCost.unitCost)
```
When upstream pricing is unknown, an explicit penalty reduces contribution.

**Step 10: Weighted random selection**
```
totalContribution = sum(contributions)
rand = Math.random() * totalContribution
Walk candidates in order, subtract contributions[i] from rand; when rand <= 0, select that candidate.
```

### 7.2 `calculateWeightedSelection` -- stable_first mode

In stable_first mode, the contribution formula is completely different:
```
recentSuccessRate = resolveStableFirstSuccessRate(runtimeHealthDetails[i], historicalSuccessRate)
contribution = max(1e-4, recentSuccessRate ** 2)
contribution *= runtimeMultiplier
contribution *= runtimeLoadMultiplier
contribution /= siteChannels
```

The `** 2` (square) amplifies the difference between good and bad sites.

Site weight + downstream multiplier + historical health + fallback penalty are NOT applied in stable_first mode (only runtime health, load, and success rate matter).

Selection is deterministic (not random): pick the highest-ranked candidate by contribution, tie-broken by `compareStableFirstCandidateOrder`.

Then site rotation within stable site leaders is applied (Section 4.3).

### 7.3 Site historical health metrics

Aggregated per site from all channel data:
```
For each candidate on a site:
    totalCalls += successCount + failCount
    totalLatencyMs += totalLatencyMs
    latencySamples += successCount

sampleFactor = clamp(totalCalls / SITE_HISTORICAL_HEALTH_MAX_SAMPLE(24), 0, 1)
successRate = successCount / totalCalls
successPenaltyFactor = 1 - ((1 - successRate) * 0.55 * sampleFactor)

avgLatencyMs = totalLatencyMs / latencySamples (if samples > 0)
latencyPenaltyRatio = clamp((avgLatencyMs - SITE_HISTORICAL_LATENCY_BASELINE_MS(2000)) / SITE_HISTORICAL_LATENCY_WINDOW_MS(20000), 0, 1) * sampleFactor
latencyFactor = 1 - (latencyPenaltyRatio * SITE_HISTORICAL_MAX_LATENCY_PENALTY(0.18))

multiplier = clamp(successPenaltyFactor * latencyFactor, SITE_HISTORICAL_HEALTH_MIN_MULTIPLIER(0.45), 1)
```

If `totalCalls <= 0`, multiplier defaults to 1.

### 7.4 Runtime load multiplier

Based on `ProxyChannelLoadSnapshot` from `proxyChannelCoordinator`:
```
if not sessionScoped or concurrencyLimit <= 0: return 1

activeRatio = clamp(activeLeaseCount / max(1, concurrencyLimit), 0, 1.5)
waitingRatio = clamp(waitingCount / max(1, concurrencyLimit), 0, 3)
activePenalty = activeRatio * 0.28
waitingPenalty = waitingRatio * 0.32
saturationPenalty = saturated ? 0.12 : 0

return clamp(1 - activePenalty - waitingPenalty - saturationPenalty, 0.18, 1)
```

This means heavily loaded channels can be penalized down to 18% of their normal contribution.

### 7.5 Effective unit cost resolution

Priority order (first available wins):

1. **Observed**: `totalCost / max(successCount, 1)` if both > 0
2. **Configured**: `account.unitCost` if a positive finite number
3. **Catalog**: `getCachedModelRoutingReferenceCost(siteId, accountId, modelName)` if positive
4. **Fallback**: `config.routingFallbackUnitCost || 1`

All values clamped to minimum `MIN_EFFECTIVE_UNIT_COST` (1e-6).

---

## 8. OAuth route unit management

### 8.1 Concepts

An OAuth route unit groups multiple OAuth accounts under one routing channel. The channel itself references the unit (`channel.oauthRouteUnitId`), and the unit has members (`oauthRouteUnitMembers`).

Two strategies:
- `round_robin`: Members ordered by `lastSelectedAt || lastUsedAt` ascending, then `sortOrder`, then account ID
- `stick_until_unavailable`: Prefers the most recently used member (by `lastSelectedAt || lastUsedAt` descending)

### 8.2 Member eligibility

Each member is checked like a channel:
- Account status (active for most)
- Site status (not disabled)
- Token availability
- Downstream policy exclusion
- Cooldown (`member.cooldownUntil > now` check)

### 8.3 Member selection

```
if strategy === 'stick_until_unavailable':
    sort by lastSelectedAt || lastUsedAt DESC, pick first
    if none, fall back to round_robin ordering
else:  // round_robin
    sort by lastSelectedAt || lastUsedAt ASC, pick first
```

For failover (when channel is in excludeChannelIds):
- Filter healthy members (not recently failed)
- If none healthy, return empty (unlike normal channels which return all)
- Then apply strategy

### 8.4 Member success recording

On success for a route unit member:
- Increment member `successCount`, `totalLatencyMs`, `totalCost`
- Update member `lastUsedAt`
- Clear member cooldown: `cooldownUntil = null`, `lastFailAt = null`, `consecutiveFailCount = 0`, `cooldownLevel = 0`
- Record site runtime success for the member's site
- Invalidate route-scoped caches

### 8.5 Member failure recording

On failure for a route unit member:
- Short window limit cooldown applies (resolved per-member)
- Otherwise: Fibonacci backoff for `stick_until_unavailable`, round-robin tiered cooldown for `round_robin`
- Update member: `failCount`, `lastFailAt`, `consecutiveFailCount`, `cooldownLevel`, `cooldownUntil`
- Record site runtime failure for the member's site
- Invalidate route-scoped caches

### 8.6 Member selection recording

After selecting a member:
- Update `member.lastSelectedAt`
- Invalidate route match caches for all routes that reference this unit

---

## 9. Record success / failure

### 9.1 `recordSuccess(channelId, latencyMs, cost, modelName, actualAccountId)`

1. Load channel + account from DB
2. Compute next: `successCount + 1`, `totalLatencyMs + latencyMs`, `totalCost + cost`
3. If OAuth route unit member: update member row (see 8.4)
4. Record site runtime success: `recordSiteRuntimeSuccess(siteId, latencyMs, modelName)` (updates both global and model-scoped state)
5. Update channel row in DB: increment success, latency, cost; clear cooldown fields
6. Patch in-memory cache: `patchCachedChannel(channelId, ...)`
7. For OAuth route units: `invalidateRouteScopedCache(routeId)`

### 9.2 `recordProbeSuccess(channelId, latencyMs, modelName, actualAccountId)`

**Different from `recordSuccess`**: this clears cooldown for ALL credential-scoped channels (all channels sharing the same `tokenId` or `accountId`), not just the successful one. Also clears sibling channel cooldowns that exist in DB but weren't in the fan-out set.

1. Load channel + account
2. If OAuth route unit member: clear member cooldown fields, record success
3. Otherwise: resolve `affectedChannelIds` via `loadCredentialScopedChannelIds` (same token, or same account without token)
4. Clear cooldown fields for `affectedChannelIds` in DB
5. Patch in-memory cache for each affected channel
6. Check sibling channels (those in the fan-out set but were not already cleared in DB): clear them too
7. Record site runtime success

### 9.3 `recordFailure(channelId, context, actualAccountId)`

1. Load channel + account + route from DB
2. Normalize context: if context is a string, treat as `{ modelName: string }`
3. If OAuth route unit member:
   - Resolve short window limit cooldown
   - Compute failCount (0 if short limit, otherwise +1)
   - Determine strategy from unit
   - For `round_robin` strategy: increment consecutiveFailCount, if >= 3 apply tiered cooldown
   - For `stick_until_unavailable`: use Fibonacci effective cooldown
   - Update member row in DB
   - Record site runtime failure for member's site
   - Invalidate route-scoped caches
4. Otherwise (regular channel):
   - Resolve short window limit cooldown
   - Compute failCount (0 if short limit, otherwise +1)
   - Determine route strategy
   - `round_robin`: tiered cooldown (same as member)
   - `weighted`/`stable_first`: Fibonacci cooldown
   - Resolve `affectedChannelIds`: if short window limit, fan out to credential-scoped channels; otherwise just the failing channel
   - Update all affected channels in DB
   - Patch in-memory cache for each
5. Record site runtime failure: `recordSiteRuntimeFailure(siteId, context)` (updates both global and model-scoped state)

### 9.4 `clearChannelFailureState(channelIds)`

Used by cooldown-clear operations:
1. Resolve channels' site + model info for runtime health clearing
2. Reset `failCount`, `lastFailAt`, `consecutiveFailCount`, `cooldownLevel`, `cooldownUntil` in DB
3. Clear runtime health states for the affected sites/models
4. Invalidate all caches (full `invalidateTokenRouterCache()`)

---

## 10. Caching

### 10.1 Route cache

- Key: global (single instance)
- TTL: `config.tokenRouterCacheTtlMs`, minimum 100ms
- Content: all enabled routes with resolved `sourceRouteIds` for `explicit_group` routes
- Invalidation: `invalidateTokenRouterCache()` clears it fully

### 10.2 Route match cache

- Key: per `routeId` (Map)
- TTL: same as route cache (minimum 100ms)
- Content: `RouteMatch` (route + resolved channels with accounts, sites, tokens, route unit summaries)
- Invalidation:
  - `invalidateTokenRouterCache()`: clears all
  - `invalidateRouteScopedCache(routeId)`: clears one route's match + stable_first caches for that route

### 10.3 In-memory channel patching

`patchCachedChannel(channelId, mutator)` walks all route match cache entries and applies the mutator to the matching channel in-place. Used after DB writes to keep the cache consistent without invalidating it.

### 10.4 What is NOT cached

The final selected channel is NEVER cached. Every `selectChannel` call recomputes from scratch (subject to route/match cache freshness).

### 10.5 Cache invalidation triggers

| Trigger | Scope |
|---------|-------|
| Route changed (CRUD) | Full invalidation (`invalidateTokenRouterCache`) |
| `recordSuccess` | Cache patch (in-place mutation of channel row) |
| `recordFailure` | Cache patch (in-place mutation of channel row) |
| `recordProbeSuccess` | Cache patch (multiple channels) |
| Route cooldown cleared | Full invalidation |
| Token added/removed | Full invalidation |
| OAuth route unit member state change | Route-scoped invalidation |
| Site runtime health clear | Full invalidation (via `resetSiteRuntimeHealthState`) |

---

## 11. Model display name matching and explicit_group routes

### 11.1 Display name matching

Routes can match requests by `displayName` in addition to `modelPattern`. A display name match occurs when the requested model equals the route's `displayName` exactly.

When matched by display name (not model pattern):
- `bypassSourceModelCheck = true` (all channels pass source-model eligibility)
- `useChannelSourceModelForCost = true` (cost computed from channel's source model, not mapped model)
- `actualModel` for the selected channel is the channel's source model (not mapped model)

### 11.2 `explicit_group` routes

These combine multiple source routes under a display name. Matching: only by exact display name match.

When loading group route channels:
- Only source routes that are enabled, non-group, and have exact model patterns are included
- If no valid source routes, the group route has zero channels

### 11.3 Route visibility (`buildVisibleEnabledRoutes`)

Exact-model routes that are "covered" by either:
- An `explicit_group` route whose `sourceRouteIds` includes that route ID
- A wildcard route with a custom display name whose pattern covers the exact model
are hidden from `getAvailableModels()` output.

### 11.4 Route match priority order

1. `explicit_group` with exact display name match
2. Non-group with exact model pattern match
3. Non-group with display name match
4. Non-group with wildcard model pattern match

### 11.5 Model mapping

`route.modelMapping` is a JSON object `{ pattern: targetModel }`. Applied after route matching, before channel selection. Exact key match takes priority over pattern match.

---

## 12. Route refresh workflow

### 12.1 `rebuildRoutesOnly()`

Delegates to `modelService.rebuildTokenRoutesFromAvailability()`. The actual rebuild logic is in `modelService` (not in the token router files). The Go port should implement equivalent logic.

### 12.2 `refreshModelsAndRebuildRoutes()`

1. Refresh models for all active accounts concurrently
2. Update model availability data
3. Call `rebuildRoutesOnly()`

### 12.3 Route decision snapshot refresh (`refreshAllRouteDecisionSnapshots`)

1. Load all enabled routes
2. Separate into exact-model routes and wildcard routes
3. For exact models: call `explainSelectionForRoute` for each route, save snapshot
4. For wildcard routes: call `explainSelectionRouteWide` for each route, save snapshot
5. Snapshot saving: write to `tokenRoutes.decisionSnapshot` + `tokenRoutes.decisionRefreshedAt`

### 12.4 Route decision snapshot storage

- Snapshots serialized as JSON into `tokenRoutes.decisionSnapshot`
- `parseRouteDecisionSnapshot`: handles both object and string (JSON.parse) inputs
- `clearRouteDecisionSnapshot(routeId)`, `clearRouteDecisionSnapshots(routeIds)`, `clearAllRouteDecisionSnapshots()`

---

## 13. Route cooldown clear (`clearRouteCooldown`)

1. Load route + resolve source route IDs (for explicit_group, expand to enabled non-group source routes with exact patterns)
2. Find all channels belonging to resolved route IDs
3. Clear channel failure state via `tokenRouter.clearChannelFailureState`
4. Clear route decision snapshot for the target route
5. Clear decision snapshots for all affected route IDs
6. Propagate snapshot clears to dependent explicit_group routes (routes whose sourceRouteIds include the affected routes)

---

## 14. Pricing reference refresh

`refreshPricingReferenceCostsForMatch`:
1. For each candidate channel in the match, deduplicate by `{siteId}:{accountId}` key
2. Resolve the model name to use (channel source model for display-name matched routes, mapped model otherwise)
3. Call `refreshModelPricingCatalog({ site, account, modelName })` for each unique key
4. Uses `Promise.allSettled` for concurrent fetching

---

## 15. Channel token value resolution

Priority for resolving a channel's token value:
1. If `channel.tokenId` is set: use `accountToken.token` if usable (non-null, non-empty)
2. If the account has OAuth info (`getOauthInfoFromAccount`): use `account.accessToken`
3. Fallback: use `account.apiToken`

Returns null if nothing is available.

---

## 16. Downstream policy full specification

| Field | Type | Description |
|-------|------|-------------|
| `excludedSiteIds` | `number[]` | Site IDs to exclude |
| `excludedCredentialRefs` | `{ kind, tokenId, accountId, siteId }[]` | Credential refs to exclude. For account_token kind: match on tokenId+accountId+siteId. For others: match on accountId+siteId when channel has no tokenId |
| `allowedRouteIds` | `number[]` | Restrict routes to only these IDs (applied when no supportedModels pattern matched) |
| `supportedModels` | `string[]` | Model pattern allowlist |
| `denyAllWhenEmpty` | `boolean` | When no supportedModels and no allowedRouteIds, if true: deny all requests; if false: allow all |
| `siteWeightMultipliers` | `Record<siteId, number>` | Per-site weight multipliers applied to contributions |

---

## 17. Route visibility filtering

`buildVisibleEnabledRoutes`: hides exact-model routes that are "covered" by either:
- An explicit_group route whose `sourceRouteIds` includes the route AND whose displayName is not itself an exact model name
- A non-exact-model route with a custom displayName whose pattern covers the exact model

This prevents exact models from appearing in `getAvailableModels()` when they are already presented under a group or wildcard display name.

---

## 18. Zero-candidate and edge case behaviors

| Scenario | Behavior |
|----------|----------|
| All channels ineligible | Return `null` (not an error) |
| All channels in cooldown | `filterRecentlyFailedCandidates` returns all (no exclusion); selection proceeds |
| All channels breaker-broken | `filterSiteRuntimeBrokenCandidatesByModel` returns all (no exclusion); contribution heavily penalized via 0.08 multiplier |
| All channels excluded by downstream policy | Those channels are ineligible; if none remain, return `null` |
| All channels excluded by `excludeChannelIds` | Those channels are ineligible; if none remain, return `null` |
| No matching route | Return `null` |
| Explicit_group with no valid source routes | Route has zero channels; no selection possible |
| Weight all zero | `(weight + 10)` in contribution formula ensures baseline contribution; selection proceeds |
| No downstream policy supplied | Empty default policy (allows everything) |
| Site runtime health persistence fails | Logs error, continues with in-memory state |
| OAuth route unit with all members cooling down | Channel marked ineligible |
| Stable_first: no observation candidates eligible (all in site cooldown) | Falls back to primary pool |
| Stable_first: primary pool empty + no observation candidates | No selection possible |

---

## 19. Module interfaces

### 19.1 Network/DB dependencies (ports)
```go
type ModelProvider interface {
    GetAvailableModels(ctx context.Context, accountID int64) ([]ModelInfo, error)
    RefreshModelsForAccount(ctx context.Context, accountID int64) error
}

type TokenProvider interface {
    GetTokens(ctx context.Context, accountID int64) ([]Token, error)
    GetDefaultToken(ctx context.Context, accountID int64) (*Token, error)
}

type PricingProvider interface {
    GetReferenceCost(ctx context.Context, model string, siteID int64) (float64, error)
    RefreshModelPricingCatalog(ctx context.Context, site Site, account Account, modelName string) error
}

type CooldownClearer interface {
    ClearChannelFailureState(ctx context.Context, channelIDs []int64) (int64, error)
}

type SnapshotClearer interface {
    ClearRouteDecisionSnapshot(ctx context.Context, routeID int64) error
    ClearRouteDecisionSnapshots(ctx context.Context, routeIDs []int64) error
}
```

---

## 20. Configuration

| Config key | Default | Description |
|------------|---------|-------------|
| `tokenRouterCacheTtlMs` | 5000 | Route + match cache TTL (min 100ms) |
| `tokenRouterFailureCooldownMaxSec` | -- | Secondary cap on cooldown duration |
| `routingWeights.baseWeightFactor` | -- | Base weight multiplier in contribution formula |
| `routingWeights.valueScoreFactor` | -- | Value score multiplier in contribution formula |
| `routingWeights.costWeight` | -- | Cost weight in value score computation |
| `routingWeights.balanceWeight` | -- | Balance weight in value score computation |
| `routingWeights.usageWeight` | -- | Usage weight in value score computation |
| `routingFallbackUnitCost` | 1 | Fallback unit cost when nothing else available |

---

## Acceptance Criteria (corrected)

- [ ] `selectChannel` returns `null` (not error) when no channel available
- [ ] Weighted selection: contribution formula matches TS `calculateWeightedSelection` exactly -- `(weight+10)*(baseWeightFactor+normalizedVS*valueScoreFactor)/siteChannels * combinedSiteWeight * runtimeMultiplier * siteHistoricalMultiplier * runtimeLoadMultiplier * fallbackPenalty`, followed by weighted random
- [ ] Stable_first: pool plan splits by `effectiveSuccessRate * 0.92`, site rotation skips last-selected, observation gating every 24 primary requests with 30-min site cooldown
- [ ] Round_robin: ordered by `lastSelectedAt || lastUsedAt` ASC, tiered cooldown [0s, 10min, 1h, 24h] at 3 consecutive failures
- [ ] Cooldown: Fibonacci `15 * fib(failCount)` backoff, NOT exponential. failCount=1 -> 15s, failCount=2 -> 15s, failCount=3 -> 30s, ceiling 30d
- [ ] Short-window limit cooldown for 429 usage-limit: quota reset hint or 5min, fans out to all credential-scoped channels
- [ ] Site runtime health: 8 failure categories with distinct penalties, 10min half-life decay, success reward `max(0, penalty*0.2 - 0.3)`, breaker at 3 transient failures in 5min window, 4 levels [0s, 60s, 5min, 30min]
- [ ] Breaker is a penalty multiplier (0.08), NOT an exclusion mechanism. No half-open probing exists
- [ ] No sticky session: stable_first site rotation replaces the concept; rotation key is `{routeId}:{normalizedModelAlias}`
- [ ] Route match cache: TTL min 100ms, per-route, with in-place channel mutation after DB writes
- [ ] Final selected channel is NEVER cached
- [ ] Route rebuild delegates to model service (no circular dependency via provider interface)
- [ ] DownstreamPolicy: `excludedSiteIds`, `excludedCredentialRefs`, `allowedRouteIds`, `supportedModels`, `denyAllWhenEmpty`, `siteWeightMultipliers` all supported
- [ ] `selectNextChannel` takes `excludeChannelIds []int64`, not a previous channel object
- [ ] OAuth route unit: member eligibility, two strategies (round_robin, stick_until_unavailable), Fibonacci/round-robin cooldown per member
- [ ] Display name matching: bypassSourceModelCheck=true, useChannelSourceModelForCost=true, actualModel from channel source model
- [ ] Cost signal priority: observed > configured > catalog > fallback
- [ ] Site runtime health persistence: debounced 500ms, stale TTL 7d, idle TTL 12h, in-flight guard

## Test plan

| File | Content |
|------|---------|
| `routing/selector_test.go` | selectChannel full flow, with mock dependencies |
| `routing/selector_compare_test.go` | Golden file: same inputs as TS -> same selection result |
| `routing/matcher_test.go` | 50+ model name pattern matching tests |
| `routing/cooldown_test.go` | Fibonacci backoff values, ceiling, short-window-limit path, round-robin tiered |
| `routing/runtime_health_test.go` | Penalty scoring, decay, breaker state machine, success reward, EMA latency |
| `routing/weights_test.go` | Full contribution formula, all multipliers, stable_first vs weighted paths |
| `routing/stable_first_test.go` | Pool plan, site rotation, observation gating, observation site cooldown |
| `routing/cache_test.go` | TTL, in-place patching, invalidation scopes |
| `routing/workflow_test.go` | Route rebuild, model refresh, decision snapshot refresh |
| `routing/route_units_test.go` | Member eligibility, round_robin vs stick_until_unavailable, member cooldown |
| `routing/snapshot_test.go` | Snapshot serialization, clear, dependent group propagation |
