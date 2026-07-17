# Residual: Sticky session multi-instance honesty (#237, #282)

**Date**: 2026-07-17  
**Issues**: [#237](https://github.com/TokenDanceLab/metapi-go/issues/237) (honesty residual), [#282](https://github.com/TokenDanceLab/metapi-go/issues/282) (product-path evaluation)  
**Lane**: p87 residual honesty → p90 product-path eval  
**SSOT code**: `proxy/session.go` (`ProxyChannelCoordinator.stickyBindings`)

## Goal

Document the honest multi-instance residual for sticky sessions, then evaluate
product paths for multi-instance sticky affinity. This document is
**documentation only** — no Redis/DB sticky store, no sticky selection semantics
change, no silent shared-sticky theater.

## What exists today

Sticky session affinity is process-local:

| Piece | Location | Storage |
|-------|----------|---------|
| Binding map | `ProxyChannelCoordinator.stickyBindings` | in-memory `map[string]StickyEntry` |
| Read | `GetStickyChannelID` | local map + TTL |
| Write | `BindStickyChannel` | local map |
| Clear | `ClearStickyChannel` | local map |
| Key | `BuildStickySessionKey` | clientKind + sessionId + model + path + downstreamAPIKeyID |

There is **no** shared cache, **no** DB table, and **no** distributed lock for
sticky bindings. Channel concurrency leases on the same coordinator are also
process-local; this residual is specifically about sticky affinity loss under
multi-instance load balancing.

Sticky is enabled by default (`ProxyStickySessionEnabled`).

## Multi-instance behavior

With 2+ MetAPI instances behind a load balancer that does **not** pin a client
session to one instance:

| Step | Instance A | Instance B | Result |
|------|------------|------------|--------|
| First request lands on A; channel 42 bound | `stickyBindings[key]={42,...}` | empty | OK on A |
| Retry / next turn routes to B | no shared binding | `GetStickyChannelID` → 0 | sticky preference **lost**; B falls back to normal channel selection |
| Later request returns to A while TTL valid | binding still present | — | sticky works again only if LB re-hits A |

### Degradation class

- **Behavioral degradation**, not data corruption.
- No error is raised when affinity is missing; selection silently falls back.
- DB-backed routing/channel rows remain consistent; only the in-process preference
  map is instance-local.
- Retry reliability may weaken: a client that was sticky to a working channel on A
  may get a different channel on B.

This matches the multi-instance audit finding for sticky affinity.

## Operator options (residual honesty wave / #237)

| Option | Effort | Effect |
|--------|--------|--------|
| **LB affinity** (cookie / IP-hash / consistent hash to one MetAPI instance) | Ops config | Keeps process-local sticky effective without code change |
| **Accept degradation** | None | Documented residual; single-node or non-sticky-critical fleets OK |
| **Future Redis/DB sticky** | Product/engineering | Out of scope for residual waves — do not fake shared sticky |

Do **not** treat `REDIS_URL` / `docs/analysis/redis-shared-state.md` as sticky
storage today. Redis shared state currently covers downstream-key **RPM/TPM**
admission only (`internal/sharedcount`). Sticky bindings are not wired there.

## Product-path evaluation (#282)

This section evaluates whether (and how) multi-instance sticky affinity should
graduate from residual honesty to product work. It does **not** change runtime
behavior.

### Options

| Option | Approach | Pros | Cons | Verdict for v0.8.x |
|--------|----------|------|------|--------------------|
| **A. Stay process-local + LB affinity ops guidance** | Keep `stickyBindings` in-memory; operators pin client sessions to one MetAPI instance via sticky cookie / IP-hash / consistent hash | Zero product risk; no new shared-state surface; matches current code truth; works for single-node and sticky-LB fleets | Without LB pin, multi-instance sticky silently degrades | **Recommended default** |
| **B. Redis sticky map (TTL, fail-open)** | Shared `GET/SET` (or `SET key channel EX`) for sticky keys; on Redis error fall back to process-local map, same fail-open pattern as `internal/sharedcount` admission | True multi-instance affinity without requiring LB pin; reuses Redis config story already used for RPM/TPM | New hot-path dependency; key churn + TTL race vs channel health; must not invent shared sticky without dedicated tests; still does **not** share channel concurrency leases | Only with **dedicated Milestone + tests** |
| **C. DB sticky table** | Persist bindings in SQL with TTL column / expiry sweeper | Durable, inspectable in admin DB | Latency on every sticky read/write; high churn under chat/retry traffic; lock/write amplification; weak fit for short-lived preference data | **Not recommended** |

### Option A ops guidance (recommended)

When multi-instance sticky preference matters:

1. Prefer **session stickiness at the LB** to one MetAPI instance (cookie preferred over pure IP-hash when clients share egress NAT).
2. Keep sticky TTL (`ProxyStickySessionTtlMs`) shorter than or comparable to the LB session affinity window so bindings expire before the pin drifts.
3. Treat sticky as a **preference**, not a correctness guarantee: missing binding falls back to normal channel selection.
4. Single-node / non-sticky-critical fleets may accept degradation without LB pin.

Do **not** claim cluster-wide sticky correctness under a non-pinning LB while Option A remains the product path.

### Option B design sketch (future Milestone only)

If multi-instance sticky becomes product-critical and LB affinity is unavailable:

1. **Key**: reuse `BuildStickySessionKey` string (or a stable hash thereof) under a Redis namespace such as `metapi:sticky:{key}`.
2. **Value**: channel ID only (or compact `StickyEntry` encoding); TTL from `ProxyStickySessionTtlMs` (same floor as local map).
3. **Fail-open**: Redis timeout / error → local map only (same class as sharedcount RPM/TPM). Never hard-fail the proxy request solely because sticky store is down.
4. **Write path**: bind on successful session-scoped channel use; clear on explicit clear; prefer TTL expiry over active cross-instance delete storms.
5. **Explicit non-goals of a first Redis sticky Milestone**:
   - Sharing `channelStates` / concurrency leases (still process-local unless a separate design lands).
   - Using DB as the sticky hot path.
   - Claiming perfect affinity if channel was force-disabled / cooldowned on another instance without selection revalidation.

**Forbidden in residual waves**: shipping Redis sticky product code, partial wiring without tests, or docs that imply `REDIS_URL` already enables sticky sharing.

### Option C rejection notes

DB sticky is a poor fit for preference maps that:

- are read/written on the proxy hot path,
- expire in tens of seconds to minutes,
- are safe to lose (fallback selection is already correct enough).

Keep durable multi-instance concerns (cooldown, route rows, usage projection leases) in DB; keep sticky ephemeral.

## Interaction with channel recovery coordinator and WS residual

Sticky affinity is only one process-local surface on `ProxyChannelCoordinator`.
Related residuals must stay honest and decoupled:

| Surface | Multi-instance truth today | Interaction with sticky |
|---------|----------------------------|-------------------------|
| **Sticky bindings** (`stickyBindings`) | Process-local map + TTL | Preference only; lost without LB pin or future shared store |
| **Channel concurrency leases** (`channelStates` / `ChannelLease`) | Process-local | Independent residual. Option B Redis sticky would **not** automatically share lease counts; an instance can still over-admit relative to another process's leases |
| **Channel recovery active set** (`GetActiveChannelIDs` → recovery scheduler) | Process-local active IDs; PG scheduler lease only suppresses duplicate sweeps | Recovery may probe channels that are "active" only on the lease-holder instance. Sticky preference on instance A does not guarantee recovery bookkeeping on instance B sees the same set ([`scheduler-residual-todos.md`](scheduler-residual-todos.md) #273 residual) |
| **Responses WebSocket residual** | GET 426 / upgrade 501; no real WS transport yet | Future Codex multi-turn WS (Option D in WS eval) would need **session affinity** decisions together with sticky; cluster-wide sticky for WS remains out of first WS MVP ([`responses-websocket-residual.md`](responses-websocket-residual.md) #274) |

Implications:

1. LB instance affinity (Option A) improves sticky **and** reduces WS/session churn if WS ever ships, but does not make recovery active-sets or leases global.
2. Redis sticky (Option B) alone does not fix recovery coordinator locality or lease fairness.
3. Do not couple a sticky shared-store Milestone to inventing WS transport or distributed leases in the same wave.

## Recommendation (v0.8.x)

**Choose Option A** for v0.8.x:

1. Keep sticky process-local.
2. Document LB session affinity / sticky cookie ops guidance for multi-instance fleets that care about affinity.
3. Accept documented degradation without LB pin.
4. **Do not** implement Redis sticky product code in residual/docs waves.

**Option B** only if multi-instance sticky becomes product-critical **and** a dedicated Milestone ships with:

1. Fail-open Redis map + local fallback tests (including Redis down / timeout).
2. Bind / get / clear / TTL expiry tests across two logical instances (or a shared fake store).
3. Explicit statement that channel leases remain process-local unless separately designed.
4. No silent theater: metrics/logs must not claim shared sticky when Redis is disabled.

**Option C** is rejected for the hot path.

## Out of scope

1. Implementing distributed sticky (Redis, DB table, or other shared store) in this wave.
2. Changing sticky key shape, TTL, bind/clear semantics, or channel selection.
3. Claiming multi-instance sticky correctness without LB affinity or a future
   shared store that is tested end-to-end.
4. Sharing channel concurrency leases or recovery active-sets as a side effect of sticky work.

## Cross-links

- Multi-instance audit (sticky section):  
  [`docs/specs/review/audits/audit-multi-instance.md`](../specs/review/audits/audit-multi-instance.md)
- Optional Redis shared state (RPM/TPM only today; fail-open pattern for any future sticky map):  
  [`docs/analysis/redis-shared-state.md`](redis-shared-state.md)
- Channel recovery residual (process-local active set + scheduler lease):  
  [`docs/analysis/scheduler-residual-todos.md`](scheduler-residual-todos.md)
- Responses WebSocket product-path eval (sticky/WS affinity note):  
  [`docs/analysis/responses-websocket-residual.md`](responses-websocket-residual.md)
- Code: `proxy/session.go` — `ProxyChannelCoordinator.stickyBindings` comment
- Issues: [#237](https://github.com/TokenDanceLab/metapi-go/issues/237), [#282](https://github.com/TokenDanceLab/metapi-go/issues/282)

## Verify

```bash
go test ./proxy -count=1 -run Sticky
test -f docs/analysis/sticky-session-multi-instance-residual.md
```
