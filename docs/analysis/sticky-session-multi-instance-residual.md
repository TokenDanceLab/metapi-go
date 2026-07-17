# Residual: Sticky session multi-instance honesty (#237)

**Date**: 2026-07-17  
**Issue**: [#237](https://github.com/TokenDanceLab/metapi-go/issues/237)  
**Lane**: p87 / residual honesty  
**SSOT code**: `proxy/session.go` (`ProxyChannelCoordinator.stickyBindings`)

## Goal

Document the honest multi-instance residual for sticky sessions. This wave is
**documentation only** — no Redis/DB sticky store, no sticky selection semantics
change.

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

## Operator options (this wave)

| Option | Effort | Effect |
|--------|--------|--------|
| **LB affinity** (cookie / IP-hash / consistent hash to one MetAPI instance) | Ops config | Keeps process-local sticky effective without code change |
| **Accept degradation** | None | Documented residual; single-node or non-sticky-critical fleets OK |
| **Future Redis/DB sticky** | Product/engineering | Out of scope for #237 — do not fake shared sticky this wave |

Do **not** treat `REDIS_URL` / `docs/analysis/redis-shared-state.md` as sticky
storage today. Redis shared state currently covers downstream-key **RPM**
admission only (`internal/sharedcount`). Sticky bindings are not wired there.

## Out of scope

1. Implementing distributed sticky (Redis, DB table, or other shared store).
2. Changing sticky key shape, TTL, bind/clear semantics, or channel selection.
3. Claiming multi-instance sticky correctness without LB affinity or a future
   shared store.

## Cross-links

- Multi-instance audit (sticky section):  
  [`docs/specs/review/audits/audit-multi-instance.md`](../specs/review/audits/audit-multi-instance.md)
- Optional Redis shared state (RPM only today):  
  [`docs/analysis/redis-shared-state.md`](redis-shared-state.md)
- Code: `proxy/session.go` — `ProxyChannelCoordinator.stickyBindings` comment

## Verify

```bash
go test ./proxy -count=1 -run Sticky
test -f docs/analysis/sticky-session-multi-instance-residual.md
```
