# P7 Token Router Implementation Review

**Date**: 2026-07-04
**Review scope**: Cross-check Go implementation against TS reference and P7 spec for weight formula, Fibonacci backoff, and breaker state machine.
**Sources**:
- Spec: `D:/Code/TokenDance/metapi-go/docs/specs/p7-token-router.md`
- TS: `D:/Code/TokenDance/metapi/src/server/services/tokenRouter.ts`
- Go: `D:/Code/TokenDance/metapi-go/routing/*.go`

---

## Executive Summary

The Go port correctly implements the core algorithms (Fibonacci backoff, breaker state machine, most of the weight formula) but has **4 critical defects** that must be fixed before production use: hardcoded routing weights bypassing configuration, broken site rotation in stable_first, per-candidate model resolution in breaker filter collapsed to single-model, and incomplete JSON deserialization losing all runtime health state on restart.

---

## 1. Weight Formula Cross-Check

### 1.1 TS Formula (spec Section 7.1)

```
valueScore = costWeight * (1/unitCost) + balanceWeight * balance + usageWeight * (1/totalUsed)
baseContribution = (weight + 10) * (baseWeightFactor + normalizedVS * valueScoreFactor)
contribution = baseContribution / siteChannels
contribution *= siteGlobalWeight * downstreamSiteMultiplier  // "combinedSiteWeight"
contribution *= runtimeMultiplier                              // runtime health (Section 5.8)
contribution *= siteHistoricalMultiplier                       // historical health (Section 7.3)
contribution *= runtimeLoadMultiplier                          // channel load (Section 7.4)
if cost source === 'fallback': contribution *= 1 / max(1, unitCost)  // Section 7.1 Step 9
```

### 1.2 Go Implementation (weights.go)

The formula in `CalculateWeightedSelection` (weights.go lines 118-200) matches the TS formula exactly:

| Step | TS (lines 3570-3637) | Go (weights.go) | Match |
|------|----------------------|-----------------|-------|
| Value scores | `costWeight*(1/unitCost) + balanceWeight*balance + usageWeight*(1/min(totalUsed,1))` | Line 124: same | OK |
| Min-max normalization | `(v - minVS) / (maxVS - minVS \|\| 1)`, maxVS floored at 0.001 | Lines 128-137: same | OK |
| Base contribution | `(weight+10)*(baseWeightFactor + normalizedVS*valueScoreFactor)` | Line 143: same | OK |
| Site channel count | `/ max(1, siteChannelCount)` | Lines 147-150, 168: same | OK |
| Combined site weight | `siteGlobalWeight * downstreamSiteMultiplier` | Lines 174-186: same | OK |
| Runtime multiplier | `combinedMultiplier` (global * model clamped to [0.0064, 1]) | Line 188: same via `runtimeHealthDetails[i].CombinedMultiplier` | OK |
| Historical multiplier | Site historical health multiplier | Lines 189-191: same | OK |
| Runtime load multiplier | 0.18 to 1 range, same penalty coefficients | Lines 285-298: same | OK |
| Fallback cost penalty | `1 / max(1, unitCost)` when source === 'fallback' | Lines 194-197: same | OK |
| Weighted random | `Math.random() * totalContribution`, walk and subtract | Lines 223-233: same | OK |

**Verdict: PASS** -- the formula implementation is correct.

### 1.3 Stable-first Formula (spec Section 7.2)

```
contribution = max(1e-4, recentSuccessRate ** 2)
contribution *= runtimeMultiplier
contribution *= runtimeLoadMultiplier
contribution /= siteChannels
```

**Go (weights.go lines 160-169)**: Matches exactly. OK.

### 1.4 Site rotation within stable_first (spec Section 4.3)

TS `getStableFirstSiteLeaderIndices`:
1. Collect site leaders from ranked indices
2. Filter to leaders whose contribution is close to best (ratio 0.92)
3. If empty, fall back to all site leaders

Go `getStableFirstSiteLeaderIndices` (weights.go lines 426-463): Same logic. OK.

TS `getStableFirstOrderedSiteLeaderIndices`: Order by priority, then channel ID.
Go `getStableFirstOrderedSiteLeaderIndices` (weights.go lines 411-424): Same. OK.

### 1.5 CRITICAL DEFECT: Hardcoded Routing Weights

**File**: `D:/Code/TokenDance/metapi-go/routing/selector.go`, lines 673-717

The `weightedRandomSelect` and `stableFirstSelect` methods pass **hardcoded** routing weights:

```go
// selector.go:678-689 (weightedRandomSelect)
RoutingWeightsConfig{BaseWeightFactor: 0.5, ValueScoreFactor: 0.5, CostWeight: 0.4, BalanceWeight: 0.3, UsageWeight: 0.3}
```

The `TokenRouter` struct (router.go:46-52) correctly reads weights from config and stores them in `tr.routingWeights`, but this field is **never passed to `ChannelSelector`** and **never used** by the `weightedRandomSelect`/`stableFirstSelect` methods.

The `ChannelSelector` struct has no `routingWeights` field. All weighted selections use hardcoded values, making the `config.routingWeights.*` settings inert.

**TS reference** (line 3554): `const { baseWeightFactor, valueScoreFactor, costWeight, balanceWeight, usageWeight } = config.routingWeights;`

**Severity**: CRITICAL -- this silences user-configured routing weights.

**Fix**: Add `routingWeights RoutingWeightsConfig` to `ChannelSelector`, populate from `NewChannelSelector`, and use in `weightedRandomSelect`/`stableFirstSelect`.

---

## 2. Fibonacci Backoff Cross-Check

### 2.1 Formula Verification (spec Section 6.1)

```
fib(1)=1, fib(2)=1, fib(3)=2, fib(4)=3, fib(5)=5, fib(6)=8, ...
backoff = 15 * fib(failCount), ceiling = 30 days
```

**TS** (`resolveFailureBackoffSec`, line 326-329):
```typescript
const normalizedFailCount = Math.max(1, Math.trunc(failCount ?? 0));
return Math.min(FAILURE_BACKOFF_BASE_SEC * fibonacciNumber(normalizedFailCount), MAX_FAILURE_BACKOFF_SEC);
```

**Go** (`ResolveFailureBackoffSec`, cooldown.go lines 37-44):
```go
normalized := int64(1)
if failCount != nil && *failCount > 1 {
    normalized = *failCount
}
fib := FibonacciNumber(normalized)
return min64(FailureBackoffBaseSec*fib, MaxFailureBackoffSec)
```

### 2.2 Exhaustive Table Check

| failCount | TS (normalized) | TS fib | TS backoff | Go (normalized) | Go fib | Go backoff | Match |
|-----------|-----------------|--------|------------|-----------------|--------|------------|-------|
| null/undef| max(1,0) = 1 | 1 | 15s | 1 | 1 | 15s | OK |
| 0 | max(1,0) = 1 | 1 | 15s | 1 (0>1 false) | 1 | 15s | OK |
| 1 | max(1,1) = 1 | 1 | 15s | 1 (1>1 false) | 1 | 15s | OK |
| 2 | max(1,2) = 2 | 1 | 15s | 2 (2>1 true) | 1 | 15s | OK |
| 3 | max(1,3) = 3 | 2 | 30s | 3 | 2 | 30s | OK |
| 4 | max(1,4) = 4 | 3 | 45s | 4 | 3 | 45s | OK |
| 5 | max(1,5) = 5 | 5 | 75s | 5 | 5 | 75s | OK |
| 6 | max(1,6) = 6 | 8 | 120s | 6 | 8 | 120s | OK |
| 7 | max(1,7) = 7 | 13 | 195s | 7 | 13 | 195s | OK |
| 8 | max(1,8) = 8 | 21 | 315s | 8 | 21 | 315s | OK |

### 2.3 Fibonacci Function Verification

**TS** (line 310-320): Iterative, `fib(1)=1, fib(2)=1, fib(3)=2` -- standard Fibonacci (1-indexed).

**Go** (`FibonacciNumber`, cooldown.go lines 22-33): Same iterative algorithm, same base cases. OK.

### 2.4 Secondary Clamping

**TS** `clampFailureCooldownMs` (lines 337-340): Clamps to `configuredMaxSec`, floor 1000ms.
**Go** `ClampFailureCooldownMs` (cooldown.go lines 59-68): Same logic. OK.

### 2.5 Short-Window Limit Cooldown (spec Section 6.2)

**TS** (lines 538-564): Extracts quota reset hint from `parseCodexQuotaResetHint`, checks OAuth `lastLimitResetAt`, falls back to 5min. Fans out to credential-scoped channels.

**Go** `resolveShortWindowLimitCooldownTS` (router.go lines 776-801): Defaults to `nowMs + ShortWindowLimitCooldownMs` (5min). **Does NOT parse quota reset hints from error text or OAuth quota data.** Falls back directly to 5min in all cases.

**Severity**: LOW -- the 5min default is correct, but parsing the upstream provider's reset time hint would give users better cooldown precision. Not a correctness defect, just incomplete feature parity.

### 2.6 Round-Robin Tiered Cooldown (spec Section 3.2)

Constants match: `[0s, 10min, 1h, 24h]`, threshold 3 consecutive failures. OK.

**Verdict: PASS** -- Fibonacci backoff is correct and matches TS behavior exactly. The short-window limit cooldown is correct in its default path.

---

## 3. Breaker State Machine Cross-Check

### 3.1 Constants Verification

| Constant | Spec/TS | Go | Match |
|----------|---------|-----|-------|
| Decay half-life | 10 min | `SiteRuntimeHealthDecayHalfLifeMs = 10*60*1000` | OK |
| Min multiplier | 0.08 | `SiteRuntimeMinMultiplier = 0.08` | OK |
| Latency baseline | 2500ms | `SiteRuntimeLatencyBaselineMs = 2500` | OK |
| Latency window | 30000ms | `SiteRuntimeLatencyWindowMs = 30000` | OK |
| Max latency penalty | 0.35 | `SiteRuntimeMaxLatencyPenalty = 0.35` | OK |
| Latency EMA alpha | 0.3 | `SiteRuntimeLatencyEMAAlpha = 0.3` | OK |
| Breaker streak threshold | 3 | `SiteRuntimeBreakerStreakThreshold = 3` | OK |
| Breaker levels | [0, 60s, 5min, 30min] | `{0, 60_000, 5*60_000, 30*60*1000}` | OK |
| Streak window | 5 min | `SiteTransientStreakWindowMs = 5*60*1000` | OK |
| Outcome half-life | 30 min | `SiteRecentOutcomeHalfLifeMs = 30*60*1000` | OK |
| Confidence samples | 12 | `SiteRecentSuccessConfidenceSamples = 12` | OK |
| Bayesian prior | succ=1, fail=1 | `SiteRecentSuccessPriorSuccesses=1, PriorFailures=1` | OK |
| Model blend weight | 0.65 | `SiteRecentModelWeight = 0.65` | OK |

### 3.2 Failure Penalty Categories (spec Section 5.2)

| Category | TS penalty | Go penalty | Match |
|----------|-----------|-----------|-------|
| Usage limit (429 + pattern) | 0.4 | 0.4 | OK |
| Model failure | 0.9 | 0.9 | OK |
| Protocol failure | 0.6 | 0.6 | OK |
| Validation failure | 0.25 | 0.25 | OK |
| Transient (5xx + patterns) | 2.5 | 2.5 | OK |
| 429 rate limit (non-usage) | 2.2 | 2.2 | OK |
| 401/403 auth | 1.8 | 1.8 | OK |
| Other 4xx | 0.9 | 0.9 | OK |
| Unknown | 1.2 | 1.2 | OK |

### 3.3 Transient Classification (spec Section 5.2)

TS `isTransientSiteRuntimeFailure` (lines 520-536): Excludes usage limit, model, protocol, and validation failures from being transient, even if they have 5xx/429 status. Then checks `status >= 500 || status === 429 || transient patterns`.

Go `IsTransientSiteRuntimeFailure` (lines 287-310): Same exclusion logic, same fall-through checks. OK.

### 3.4 Breaker Trigger Logic

**TS** `applyRuntimeHealthFailure` (lines 738-763):
- If transient: check if last transient within 5min -> continue streak else reset to 1
- If streak >= 3: `breakerLevel = min(level+1, 3)`, `breakerUntilMs = nowMs + breakerMs[level]`, `streak = 0`
- If not transient: `streak = 0, lastTransientAtMs = null`

**Go** `applyRuntimeHealthFailure` (runtime_health.go lines 633-662): Identical logic. OK.

### 3.5 Breaker Auto-Close

**TS** `isRuntimeHealthBreakerOpen` (lines 689-692): `breakerUntilMs > nowMs`. When expired, returns false automatically. No half-open state.

**Go** `isRuntimeHealthBreakerOpen` (lines 415-419): Same. OK.

### 3.6 Breaker as Multiplier, NOT Exclusion

**Spec Section 18**: "All channels breaker-broken: filterSiteRuntimeBrokenCandidatesByModel returns all (no exclusion); contribution heavily penalized via 0.08 multiplier"

**Go** `FilterSiteRuntimeBrokenCandidatesByModel` (runtime_health.go lines 517-546): If all are breaker-open, returns all candidates (not filtered). OK.

**Go** `GetRuntimeHealthMultiplier` (runtime_health.go lines 433-452): Returns 0.08 when breaker is open. OK.

### 3.7 Success Reward

**TS** `applyRuntimeHealthSuccess` (lines 765-779): `penalty = max(0, penalty*0.2 - 0.3)`, breaker reset, streak reset, latency EMA update.

**Go** `applyRuntimeHealthSuccess` (lines 664-681): Identical. OK.

### 3.8 Penalty Decay (spec Section 5.3)

**TS** `getDecayedSiteRuntimePenalty` (lines 589-595): `penalty * 0.5^(elapsed/halfLife)`, halfLife = 10min.

**Go** `getDecayedSiteRuntimePenalty` (lines 318-328): Same formula. OK.

### 3.9 MEDIUM DEFECT: Breaker Filter Per-Candidate Model Resolution

**File**: `D:/Code/TokenDance/metapi-go/routing/selector.go`, lines 277-278, 288, 345

In `selectFromMatch`, the breaker filter is called as:

```go
breakerHealthy, _ := GetBreakerFilteredCandidatesByModel(available, resolveModel(available[0]))
```

`GetBreakerFilteredCandidatesByModel` takes `modelName string` (not a function). This means ALL candidates are checked against the first candidate's resolved model.

**TS reference** (line 2853, 2872, 2892):
```typescript
const runtimeModelResolver = requestedByDisplayName
    ? ((candidate: RouteChannelCandidate) => normalizeChannelSourceModel(candidate.channel.sourceModel) || mappedModel)
    : mappedModel;
// ...
const breakerFiltered = filterSiteRuntimeBrokenCandidatesByModel(rawLayer, runtimeModelResolver, nowMs);
```

In TS, `filterSiteRuntimeBrokenCandidatesByModel` accepts `string | ((candidate) => string)`. When `requestedByDisplayName` is true, each candidate is resolved individually against its own channel's `sourceModel`.

**Impact**: When a route is display-name-matched and has channels with different source models, the Go code applies the same model's breaker state to ALL candidates. A channel whose source model is healthy could be incorrectly filtered out, or a channel whose source model is breaker-broken could incorrectly pass.

**Severity**: MEDIUM -- only affects display-name-matched routes with mixed source models. Standard model-pattern-matched routes are unaffected (all candidates share the same `mappedModel`).

**Fix**: Modify `GetBreakerFilteredCandidatesByModel` to accept a `func(RouteChannelCandidate) string` resolver, or add a separate per-candidate variant.

### 3.10 MEDIUM DEFECT: JSON Deserialization is Incomplete (Stub)

**File**: `D:/Code/TokenDance/metapi-go/routing/runtime_health.go`, lines 1357-1359

```go
func readHealthState() *SiteRuntimeHealthState {
    // Stub: the full deserialization is handled by unmarshalHealthPayload via encoding/json
    return nil
}
```

And `skipValue` (lines 1362-1369) does not actually skip values.

The hand-rolled JSON parser in `unmarshalPayload` calls `readHealthState()` for each health state entry, which always returns `nil`. This means:

1. **All runtime health state is lost on restart** -- site breaker levels, penalty scores, recent success/failure counts, latency EMAs, and transient failure streaks are all reset to zero.
2. After a restart, every site starts with no breaker, no penalty, and no historical data.
3. The `EnsureSiteRuntimeHealthStateLoaded` function (lines 939-980) will parse the settings JSON but populate zero entries due to `readHealthState()` returning nil.

Additionally, `skipValue()` (line 1362-1369) breaks immediately without consuming any input, which means the parser will misread any JSON with unknown keys -- though currently it only handles known keys.

**Severity**: MEDIUM -- runtime health persistence is the primary mechanism for maintaining breaker and penalty state across restarts. Without it, sites that were breaker-broken before a restart will receive full traffic immediately after restart.

**Fix**: Either implement `readHealthState()` to properly parse the state JSON, or replace the hand-rolled JSON with `encoding/json` standard library calls. The `encoding/json` approach is strongly recommended -- the hand-rolled serializer/parser is ~400 lines of error-prone code.

### 3.11 CRITICAL DEFECT: Stable-first Site Rotation Key Mismatch

**File**: `D:/Code/TokenDance/metapi-go/routing/selector.go`, lines 768-776 (in `finalizeDispatch`)

```go
// Go: always records under observationKey, conditionally under rotationKey
rememberStableFirstSiteSelectionForKey(
    stableFirstObservationKey,
    dispatchCandidate.Site.ID,
)
if usedObservation {
    rememberStableFirstSiteSelectionForKey(stableFirstRotationKey, dispatchCandidate.Site.ID)
}
```

**TS reference** (lines 3482-3491):
```typescript
// TS: records under the key that was actually used for selection
rememberStableFirstSiteSelectionForKey(
    usedObservation ? stableFirstObservationKey : stableFirstRotationKey,
    dispatchCandidate.site.id,
);
```

**Impact**:
- When selecting from the **primary pool** (`usedObservation = false`):
  - TS writes to `rotationKey` -> next primary selection correctly skips the last-used site
  - Go writes to `rotationKey:observe` -> `rotationKey` is NEVER updated -> next primary selection may select the same site again (rotation is broken)
- When selecting from the **observation pool** (`usedObservation = true`):
  - TS writes to `rotationKey:observe` -> next observation skips the last-used observation site
  - Go writes to both keys -> redundant but not harmful

**Severity**: CRITICAL -- the site rotation optimization in stable_first is effectively disabled. The primary pool will always select the highest-ranked site instead of rotating through sites, defeating the purpose of stable_first rotation.

**Fix**: Change Go to match TS:
```go
targetKey := stableFirstRotationKey
if usedObservation {
    targetKey = stableFirstObservationKey
}
rememberStableFirstSiteSelectionForKey(targetKey, dispatchCandidate.Site.ID)
```

### 3.12 MEDIUM DEFECT: clearChannelFailureState Does Not Clear Runtime Health

**File**: `D:/Code/TokenDance/metapi-go/routing/router.go`, lines 744-763

```go
runtimeHealthRows, _ := tr.db.LoadRuntimeHealthChannelRows(ctx, channelIDs)
_ = runtimeHealthRows // Track for health clearing if needed
```

**TS reference** (lines 2687-2692):
```typescript
if (clearRuntimeHealthStatesForChannels(runtimeHealthRows)) {
    await persistSiteRuntimeHealthState();
}
```

The Go code loads the runtime health rows but **never clears them**. When a user clears route cooldowns, the site runtime breaker/penalty state should also be cleared for the affected sites/models. This is missing in Go.

**Severity**: MEDIUM -- site runtime health state persists even after explicit cooldown clear by admin.

**Fix**: Implement `ClearRuntimeHealthStatesForChannels` function and call it, then trigger persistence.

---

## 4. Other Defects Found

### 4.1 LOW: `getCandidateEligibilityReasonsExplain` Missing Token/OAuth/Downstream Checks

**File**: `D:/Code/TokenDance/metapi-go/routing/router.go`, lines 365-401

The `getCandidateEligibilityReasonsExplain` function (used only for `ExplainSelection`) omits several eligibility checks present in the real `getCandidateEligibilityReasons` in `selector.go`:
- Missing `IsExplicitTokenChannel` account status differentiation
- Missing downstream policy exclusion check
- Missing token availability check
- Missing OAuth route unit member availability check

**Severity**: LOW -- only affects `ExplainSelection` output, not actual routing. But could show a channel as "eligible" in the explanation when it actually isn't.

### 4.2 LOW: `buildVisibleEnabledRoutes` Incomplete for Groups

**File**: `D:/Code/TokenDance/metapi-go/routing/router.go`, lines 178-182

```go
if IsExplicitGroupRoute(cr.route.RouteMode) {
    // Check sourceRouteIds — we don't have them loaded here
    // Simplified: skip group route checks without full source info
    _ = cr
    continue
}
```

The spec says exact-model routes covered by explicit_group routes should be hidden. The Go code skips this check entirely due to missing sourceRouteIDs data in the function scope.

**Severity**: LOW -- some exact-model routes may appear in `getAvailableModels()` when they should be hidden under a group. Cosmetic issue only.

### 4.3 LOW: `ReadHealthState()` Is a Stub -- Actually Unused for Deserialization

The Go code has two deserialization paths:
1. `EnsureSiteRuntimeHealthStateLoaded` -> `unmarshalJSON` -> `unmarshalPayload` which uses `readHealthState()` (returns nil)
2. `unmarshalHealthPayload` (weights.go line 734) which uses `encoding/json`

Path 1 is the runtime path and always returns nil for health states. Path 2 exists but is never called from the loading code.

This reinforces Severity MEDIUM for defect 3.10.

---

## 5. Summary of All Defects

| # | Severity | File | Line(s) | Description |
|---|----------|------|---------|-------------|
| 1 | **CRITICAL** | `selector.go` | 678-717 | `weightedRandomSelect`/`stableFirstSelect` use hardcoded routing weights instead of config values |
| 2 | **CRITICAL** | `selector.go` | 768-776 | Stable-first site rotation records under wrong key, breaking primary pool rotation |
| 3 | **MEDIUM** | `selector.go` | 277-278, 288, 345 | Breaker filter resolves model per candidate list, not per individual candidate |
| 4 | **MEDIUM** | `runtime_health.go` | 1357-1359 | `readHealthState()` returns nil -- all persisted runtime health state lost on restart |
| 5 | **MEDIUM** | `router.go` | 754-755 | `ClearChannelFailureState` does not clear runtime health states |
| 6 | **LOW** | `router.go` | 776-801 | `resolveShortWindowLimitCooldownTS` does not parse quota reset hints from error text |
| 7 | **LOW** | `router.go` | 365-401 | `getCandidateEligibilityReasonsExplain` missing token/OAuth/downstream checks |
| 8 | **LOW** | `router.go` | 178-182 | `buildVisibleEnabledRoutes` skips explicit_group sourceRouteIds check |

---

## 6. What Is Correct

For avoidance of doubt, these areas are verified correct:

- **Fibonacci backoff**: Formula, clamping, round-robin tiered cooldown all match TS exactly
- **Breaker state machine**: Constants, trigger logic, auto-close, multiplier behavior all match TS
- **Weight formula** (algorithm only): The `CalculateWeightedSelection` function implements the correct formula end-to-end
- **Stable-first pool plan**: `BuildStableFirstPoolPlan` correctly splits candidates into primary/observation pools
- **Site historical health**: `BuildSiteHistoricalHealthMetrics` computes correct multiplier, success rate, and latency
- **Runtime load multiplier**: `ResolveChannelRuntimeLoadMultiplier` matches TS coefficients
- **Effective unit cost**: `EffectiveUnitCost` uses correct priority order (observed > configured > catalog > fallback)
- **Downstream policy**: Model allow/deny, route filtering, site exclusion, credential exclusion all implemented
- **Route matching**: 4-step priority order (explicit_group display name > exact model > display name > wildcard) correct
- **Model mapping**: `ResolveMappedModel` exact-then-pattern matching correct
- **OAuth route unit**: Member eligibility, round_robin and stick_until_unavailable strategies, member cooldown all correct
- **Cache invalidation**: Route cache, match cache, stable_first cache scoping correct
- **`recordSuccess`/`recordFailure`**: DB updates, cache patching, runtime health updates match TS
- **`recordProbeSuccess`**: Credential-scoped fanout, sibling channel cleanup match TS
