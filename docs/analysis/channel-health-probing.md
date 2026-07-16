# Background channel health probing (#114)

**Date:** 2026-07-17  
**Lane:** competitive learn / reliability  
**SSOT code:** `scheduler/model_probe.go`, `routing/router.go` (`RecordProbeSuccess` / `RecordProbeFailure`), `routing/runtime_health.go` (probe stamps)  
**Peers:** LiteLLM background `health_check` polling; AxonHub channel health probing

## Goal

Detect dead or flaky upstream channels **before** user traffic fails, then bias routing away from them via existing runtime health / cooldown machinery.

This is **proactive** probing. Reactive Fibonacci cooldown still applies on real user traffic (`RecordFailure`).

## Operator config (`MODEL_AVAILABILITY_PROBE_*`)

| Env | Config field | Default | Notes |
|:----|:-------------|:--------|:------|
| `MODEL_AVAILABILITY_PROBE_ENABLED` | `ModelAvailabilityProbeEnabled` | `false` | Master gate. When false, scheduler does not start a timer. |
| `MODEL_AVAILABILITY_PROBE_INTERVAL_MS` | `ModelAvailabilityProbeIntervalMs` | `1800000` (30m) | Hard floor **60s** (`max(60000, value)`). |
| `MODEL_AVAILABILITY_PROBE_TIMEOUT_MS` | `ModelAvailabilityProbeTimeoutMs` | `15000` | Hard floor **3s**. Per-target probe timeout. |
| `MODEL_AVAILABILITY_PROBE_CONCURRENCY` | `ModelAvailabilityProbeConcurrency` | `1` | Clamped to **[1, 16]**. |

Also present in admin settings as `modelAvailabilityProbeEnabled` (DB setting key `model_availability_probe_enabled`) and listed in `.env.example`.

### Recommended starting point

```bash
MODEL_AVAILABILITY_PROBE_ENABLED=true
MODEL_AVAILABILITY_PROBE_INTERVAL_MS=300000   # 5 minutes
MODEL_AVAILABILITY_PROBE_TIMEOUT_MS=10000
MODEL_AVAILABILITY_PROBE_CONCURRENCY=2
```

Keep concurrency low and interval ≥ a few minutes in production so probe spend stays bounded. The scheduler caps each pass at **16** channel targets (cooling channels first, then least-recently-used active channels).

## Architecture

```
ModelProbeScheduler (config-gated ticker, multi-instance lease)
  loadProbeTargets(route_channels ∩ active accounts/sites, budget=16)
  account lease (in-memory) per account
  ChannelHealthProbe.ProbeChannel(target)     // injected; lightweight request
       │
       ├─ success  → ChannelHealthRecorder.RecordProbeSuccess
       │                → TokenRouter.RecordProbeSuccess
       │                     · clear credential-scoped cooldown
       │                     · RecordSiteRuntimeSuccess
       │                     · RecordSiteProbeOutcome(status=success)
       │
       ├─ failure  → ChannelHealthRecorder.RecordProbeFailure
       │                → TokenRouter.RecordProbeFailure
       │                     · cool ONLY the probed channel (no credential cascade)
       │                     · RecordSiteRuntimeFailure (breaker streak)
       │                     · RecordSiteProbeOutcome(status=failure)
       │                     · NEVER mark account/key expired
       │
       └─ inconclusive / skipped → no health / cooldown mutation
```

### Package boundaries

- `scheduler` must **not** import `routing` / `proxy` / `handler` (BACKEND.md).
- Probe execution and health recording are **injected interfaces**:
  - `scheduler.ChannelHealthProbe`
  - `scheduler.ChannelHealthRecorder`
- Composition root (`app` / `cmd/server`) wires a lightweight HTTP/protocol probe and a thin adapter over `routing.TokenRouter`.
- When either dependency is missing, targets are **skipped** (safe no-op) so enabling the flag never panics.

## Success / failure semantics

| Outcome | Channel cooldown | Runtime health | Probe stamp | Account/key expired? |
|:--------|:-----------------|:---------------|:------------|:---------------------|
| success | Clear credential-scoped cooldowns | `RecordSiteRuntimeSuccess` | `lastProbeStatus=success` | No |
| failure (transient 5xx/timeout) | Probed channel only (fib / RR tiers) | Failure + breaker streak | `lastProbeStatus=failure` | **No** |
| failure (auth-looking 401/403) | Probed channel only | Failure penalty | `lastProbeStatus=failure` | **No** (probe must not expire keys) |
| inconclusive / executor error | Unchanged | Unchanged | optional stamp only via direct helper | No |
| skipped (no deps / lease) | Unchanged | Unchanged | Unchanged | No |

### Why probe failure ≠ `RecordFailure`

User-path `RecordFailure` may expand short-window usage-limit cooldowns across **credential-scoped siblings** and participates in proxy expiry classification. Background probes are synthetic: cascading siblings or marking keys expired on a bad probe would poison healthy capacity. `RecordProbeFailure` therefore:

1. Always scopes channel cooldown to the **probed channel** (or OAuth member only).
2. Still feeds site/model runtime health so breakers can open after a transient streak.
3. Never writes account `status` / token `valueStatus`.

## Operator visibility (last probe status)

In-memory runtime health (`SiteRuntimeHealthState`) carries:

| Field | Meaning |
|:------|:--------|
| `lastProbeAtMs` | When the last background probe for this site completed |
| `lastProbeStatus` | `success` \| `failure` \| `inconclusive` |
| `lastProbeLatencyMs` | Observed latency on success |
| `lastProbeError` | Truncated error text on failure |
| `lastProbeModel` | Model name used for the probe |
| `lastProbeChannelId` | Channel id that was probed |

Read via `routing.GetSiteProbeStatus(siteID)`. Persistence uses the existing `token_router_site_runtime_health_v1` settings payload (JSON tags). Admin UI surfacing can read this stamp; full admin REST expansion is optional follow-up (web/** out of scope for this issue).

## Target selection budget

Each pass:

1. Enabled `route_channels` on **active** accounts + **active** sites with non-empty `source_model`.
2. Prefer rows with `cooldown_until > now` (recovering channels).
3. Fill remaining budget with non-cooling channels ordered by oldest `last_used_at`.
4. Hard cap **16** targets per pass.
5. Account-level in-memory lease prevents concurrent probes on the same account.
6. Multi-instance scheduler lease (`runWithSchedulerLease`) avoids duplicate fleet-wide passes on Postgres.

Out of scope for #114: paid full-size eval suites; probing every model on every site by default without budgets.

## Tests

- `routing/probe_health_test.go`
  - success clears credential-scoped cooldown + stamps probe status
  - failure cools only probed channel; updates health; no credential cascade
  - auth-looking probe failure does not mark expired / does not cascade
  - three transient probe failures open site breaker
  - inconclusive stamp does not invent success/failure samples
- `scheduler/model_probe_test.go`
  - `ApplyProbeOutcome` success/failure/inconclusive transitions
  - `probeOne` records via injected fakes
  - executor errors → inconclusive
  - missing deps → skipped
  - disabled config does not start ticker

## Non-goals

- Redis multi-instance health state (#118)
- Frontend admin pages (`web/**`)
- Replacing reactive cooldown / user-path `RecordFailure`
- Automatic route rebuild on every probe (availability-table rebuild remains a separate model-catalog concern)
