# Product spike: Redis sticky map (Option B) design (#292)

**Date**: 2026-07-17  
**Issues**: [#292](https://github.com/TokenDanceLab/metapi-go/issues/292) (this spike), [#282](https://github.com/TokenDanceLab/metapi-go/issues/282) (product-path evaluation), [#237](https://github.com/TokenDanceLab/metapi-go/issues/237) (honesty residual)  
**Parent residual**: [`sticky-session-multi-instance-residual.md`](sticky-session-multi-instance-residual.md)  
**Related Redis today**: [`redis-shared-state.md`](redis-shared-state.md) (RPM/TPM admission only)  
**SSOT code today**: `proxy/session.go` (`ProxyChannelCoordinator.stickyBindings`) — process-local only

## Status

This document is a **product spike / design** only.

- **No Redis sticky product code** lands in this wave or in v0.8.14 unless separately approved.
- Option A (process-local + LB session affinity) remains the **recommended default** for v0.8.x per #282.
- Option B is the design target **if** multi-instance sticky becomes product-critical and LB pin is unavailable.

## Background (from #282)

| Option | Summary | v0.8.x verdict |
|--------|---------|----------------|
| **A** | Stay process-local `stickyBindings` + LB cookie / IP-hash / consistent-hash pin | **Recommended default** |
| **B** | Shared Redis sticky map (TTL, fail-open) | Dedicated Milestone + tests only |
| **C** | DB sticky table | **Not recommended** (hot-path churn) |

Today sticky affinity is an in-memory preference map. With 2+ MetAPI instances behind a non-pinning LB, `GetStickyChannelID` returns 0 on instances that never saw the bind — **behavioral degradation**, not data corruption. Selection silently falls back to normal channel selection.

`REDIS_URL` / `METAPI_REDIS_URL` currently wires **downstream-key RPM/TPM admission only** via `internal/sharedcount` (`auth.ConfigureSharedAdmissionFromRedisURL`). It does **not** enable sticky sharing. Docs or metrics must not imply otherwise.

## Goals for a future Option B Milestone

1. Multi-instance sticky **preference** without requiring LB session affinity.
2. Reuse the existing Redis config story (`REDIS_URL` / `METAPI_REDIS_URL`) and the **fail-open** class already used by sharedcount admission.
3. Keep sticky as a **preference**, never a hard correctness gate for proxy success.
4. Preserve local map behavior when Redis is empty/disabled/down (single-node and residual fleets unchanged).

## Non-goals (v0.8.14 and this spike)

Explicit non-goals for **v0.8.14 product code** (and for any residual/docs wave):

1. **No Redis sticky product implementation** — no store, no wiring in `ProxyChannelCoordinator`, no new env flags that claim shared sticky.
2. **No partial theater** — do not set metrics/logs that say “shared sticky” when Redis sticky is absent or disabled.
3. **No DB sticky table** (Option C remains rejected for the hot path).
4. **No sharing of channel concurrency leases** (`channelStates` / `ChannelLease`) — still process-local unless a separate design lands.
5. **No sharing of channel-recovery active sets** — independent residual ([`scheduler-residual-todos.md`](scheduler-residual-todos.md)).
6. **No change to sticky selection semantics** in residual waves (first-attempt preference, clear-on-unavailable, session-scoped gate).
7. **No Responses WebSocket transport** coupling — WS affinity remains a future joint decision ([`responses-websocket-residual.md`](responses-websocket-residual.md)).
8. **No inventing shared sticky without dedicated multi-instance tests**.

If product later approves Option B, it ships only as a **dedicated Milestone** with the test gates listed below — not as a drive-by residual.

## Redis key schema

### Logical session key (unchanged)

Reuse `BuildStickySessionKey` output exactly:

```text
{owner}|{clientKind}|{downstreamPath}|{requestedModel}|{sessionID}
```

Examples:

| Field | Source | Notes |
|-------|--------|-------|
| `owner` | `key:{downstreamAPIKeyID}` or `key:anonymous` | Same as local map |
| `clientKind` | lowercased (`codex`, `generic`, …) | |
| `downstreamPath` | lowercased path or `unknown` | |
| `requestedModel` | lowercased model | empty → no sticky key |
| `sessionID` | trimmed session id | empty → no sticky key |

Local map key today is this raw string. Redis must address the same logical identity.

### Redis key encoding

| Piece | Value |
|-------|--------|
| Namespace prefix | `metapi:sticky:` |
| Full Redis key | `metapi:sticky:{sessionKey}` **or** `metapi:sticky:{sha256_hex(sessionKey)}` |
| Type | Redis **string** |
| Value | decimal channel ID only, e.g. `42` |
| Expiry | Redis key TTL (see TTL section) — no separate expiry payload required |

**Encoding choice (recommended for first Milestone):**

| Variant | Pros | Cons | Recommendation |
|---------|------|------|----------------|
| **B1. Raw session key suffix** | Easy debug; exact match to local map key | Long keys; `|` and path chars in Redis keyspace | Acceptable if key length stays bounded |
| **B2. SHA-256 hex of session key** | Fixed 64-char suffix; no special-char surprises | Needs log correlation (hash ↔ logical key) | **Preferred default** for production |

If B2 is chosen, keep a debug helper (test/admin only) that can recompute the hash from `BuildStickySessionKey` inputs. Do **not** store a second reverse index in v1.

**Forbidden encodings for v1:**

- JSON blob of full `StickyEntry` (unnecessary; TTL is Redis-native).
- Including instance ID in the key (would defeat multi-instance share).
- Reusing `metapi:rpm:` / `metapi:tpm:` namespaces.

### Namespace map (fleet Redis)

| Prefix | Owner today | Purpose |
|--------|-------------|---------|
| `metapi:rpm:{keyID}` | `internal/sharedcount` | Downstream-key RPM fixed window |
| `metapi:tpm:{keyID}` | `internal/sharedcount` | Downstream-key TPM fixed window |
| `metapi:sticky:…` | **future** sticky map | Sticky channel preference |

Sticky and admission share config (`REDIS_URL`) but **must not** share key prefixes or counter semantics.

## TTL

| Source of truth | Rule |
|-----------------|------|
| Config | `PROXY_STICKY_SESSION_TTL_MS` (`config.ProxyStickySessionTtlMs`) |
| Floor | `max(30000, configured)` — same as `stickySessionTTLMs()` today |
| Default | 30 minutes (`DefaultProxyStickySessionTtlMs`) |
| Redis write | `SET key value PX {ttlMs}` (or `SET` + `PEXPIRE`) on every **bind** |
| Local write | `StickyEntry.ExpiresAtMs = nowMs + stickySessionTTLMs()` (unchanged) |

### Refresh policy

| Event | Redis | Local map |
|-------|-------|-----------|
| Successful session-scoped channel use → bind | `SET … PX ttlMs` (overwrite, refresh TTL) | upsert entry + new `ExpiresAtMs` |
| Read hit (`GET`) | **Do not** extend TTL on read (v1) | local cleanup of expired only |
| Explicit clear | `DEL` (best-effort) | delete if channel matches |
| TTL natural expiry | key disappears | entry removed on cleanup / miss |

**Rationale for no TTL refresh on read:** sticky is a preference window aligned to `ProxyStickySessionTtlMs`, not a lease that must stay alive while idle. Refresh-on-bind already extends when the session actively uses the channel. Avoids write amplification on every GET.

### TTL vs LB affinity (Option A coexistence)

When both LB pin and Redis sticky exist:

1. Prefer keeping sticky TTL ≤ LB affinity window so a drifted pin still sees a coherent preference expiry.
2. Redis sticky does not require LB pin; LB pin does not require Redis sticky.
3. Single-node fleets leave Redis sticky disabled (or `REDIS_URL` empty) and keep process-local only.

## Fail-open on Redis errors (not a hard dependency)

Mirror the **sharedcount admission** failure class (`docs/analysis/redis-shared-state.md`, `auth.ConfigureSharedAdmissionFromRedisURL`):

| Situation | Behavior |
|-----------|----------|
| `REDIS_URL` empty | Sticky = process-local only (today’s behavior). No Redis calls. |
| Bad URL at startup | Disable shared sticky mode; log warning; local map only. Same class as “redis admission: disabled (bad REDIS_URL)”. |
| Request-time timeout / network / RESP error on `GET` | Treat as miss → fall through to local map, then normal selection if still empty. Sticky Redis is not a hard dependency; never fail the proxy request solely because sticky store is down. |
| Request-time error on `SET` / `DEL` | Best-effort; local map still updated (bind/clear). Request continues. Sticky Redis is not required for success. |
| Redis returns non-integer / garbage value | Treat as miss; optional best-effort `DEL`; do not panic. |

### Read path (proposed)

```text
GetStickyChannelID(key):
  1. If shared sticky disabled → local map only (current code).
  2. Try Redis GET (short timeout).
     - OK + valid channelID > 0 → optionally warm local map; return channelID.
     - miss / error / invalid → continue.
  3. Local map lookup (current TTL cleanup).
  4. Return 0 → caller falls back to normal selection.
```

### Write path (proposed)

```text
BindStickyChannel(key, channelID, …):
  1. Session-scoped gate unchanged (extraConfig / oauthProvider).
  2. Always write local map (source of truth for this process).
  3. If shared sticky enabled → best-effort Redis SET PX ttlMs.
     Error → log at debug/warn rate-limited; do not fail caller.

ClearStickyChannel(key, channelID):
  1. Local conditional delete (channel match) unchanged.
  2. If shared sticky enabled → best-effort DEL (or GET+compare+DEL).
     Prefer conditional clear so instance A does not delete instance B’s
     newer re-bind to a different channel without match (see invalidation).
```

### Hard rules

1. Sticky store down ≠ proxy 5xx.
2. Sticky miss ≠ error; it is normal selection.
3. Metrics must distinguish: `sticky_local_hit`, `sticky_redis_hit`, `sticky_redis_error`, `sticky_miss` — and must not claim “shared sticky enabled” when the feature flag / Redis client is off.

## Multi-instance invalidation

### What “invalidation” means

Sticky is a **preference**, not a distributed lock. Invalidation means “stop preferring channel X for this session key,” not “abort in-flight requests.”

### Events that clear preference

| Event | Local | Redis (Option B) |
|-------|-------|------------------|
| Explicit `ClearStickyChannel` (e.g. preferred channel unavailable / excluded) | delete if channel matches | best-effort conditional delete |
| TTL expiry | cleanup on access | key auto-expire |
| Re-bind to new channel on successful use | overwrite local | `SET` overwrite + new TTL |
| Process restart | local map empty | Redis still holds key until TTL (good) |
| Channel force-disabled / cooldown on another instance | local may still prefer until clear/TTL | Redis may still prefer until clear/TTL — **selection must revalidate** |

### Conditional clear (required)

`ClearStickyChannel(key, channelID)` already only deletes when `channelID` matches (or channelID ≤ 0 clears any). Redis clear must preserve that:

```text
// conceptual — not product code in this spike
GET metapi:sticky:{k}
if value == expectedChannelID (or channelID <= 0):
  DEL metapi:sticky:{k}
```

Race: instance A binds channel 10; instance B clears channel 7 → Redis must **not** delete 10. Use GET+compare+DEL or Lua one-shot if the minimal RESP client grows a `Eval` helper later. v1 may use GET+DEL with compare in process; accept rare races as preference-only.

### Prefer TTL over delete storms

Do **not** broadcast “clear all sticky for channel X” across Redis on every admin channel edit in v1. Prefer:

1. Selection-time revalidation (channel still selectable / not excluded).
2. Existing clear on sticky path failure.
3. Natural TTL.

Admin bulk invalidation can be a later ops tool (`SCAN metapi:sticky:*` is expensive — out of v1).

### Split-brain / dual write notes

| Scenario | Outcome | Acceptable? |
|----------|---------|-------------|
| A binds 10 to Redis; B has stale local 7 | B should prefer Redis GET 10 if shared enabled | Yes — Redis wins over stale local when GET succeeds |
| Redis down mid-flight | each instance uses local only | Yes — fail-open, same as today multi-instance without share |
| A clears Redis; B still has local until TTL | B may prefer until local expiry or clear | Yes — preference only; revalidation on select |
| Two instances bind different channels near-simultaneously | last `SET` wins | Yes — last writer wins is enough for preference |

**Local cache warming:** on Redis GET hit, write-through to local map with `ExpiresAtMs` aligned to remaining TTL if known, else full configured TTL. Remaining TTL needs `PTTL`; if v1 RESP client lacks `PTTL`, warm with full TTL (slightly longer local preference — acceptable).

## Interaction with sharedcount admission pattern

| Dimension | RPM/TPM admission (`internal/sharedcount`) | Sticky Option B (design) |
|-----------|--------------------------------------------|---------------------------|
| Config | `REDIS_URL` / `METAPI_REDIS_URL` | Same URL; **separate enable story** (see below) |
| Client | `sharedcount.RedisCounter` (INCR/INCRBY/GET + PEXPIRE) | Needs string `GET`/`SET`/`DEL` (+ optional `PTTL`) — may extend minimal RESP client or add a small sticky store type |
| Namespace | `metapi:rpm:` / `metapi:tpm:` | `metapi:sticky:` |
| Semantics | Counters / fixed windows; over-limit → **429** | Preference map; miss → normal selection |
| Fail-open | Redis error → process-local sliding window | Redis error → process-local sticky map |
| Hot path cost | every admitted request | only when sticky key non-empty + sticky enabled |
| Coupling | none to channel selection | none to admission limits |

### Wiring sketch (future Milestone — not v0.8.14)

1. **Do not** overload `ConfigureSharedAdmissionFromRedisURL` to silently enable sticky. Admission and sticky are different products even if they share Redis.
2. Preferred enable matrix:

   | `REDIS_URL` | Sticky shared flag (future) | Admission | Sticky |
   |-------------|----------------------------|-----------|--------|
   | empty | any | local | local |
   | set | off / absent | shared RPM/TPM | **local** (default) |
   | set | on | shared RPM/TPM | **Redis + local** |

3. Optional future env (name TBD, **not introduced in this spike**): e.g. `PROXY_STICKY_REDIS_ENABLED=true` so operators can run shared admission without shared sticky.
4. Reuse one TCP client pool/instance if practical, but keep APIs separate (`WindowCounter` is the wrong interface for sticky strings).

### What not to copy from admission

- Sticky must **not** 429 or block when Redis is unhappy.
- Sticky must **not** use INCR windows; it is not a rate limit.
- Admin `Snapshot()` under-report residual for RPM/TPM does not apply 1:1; sticky has no admin usage snapshot requirement in v1.

## Selection revalidation (still needed even if sticky uses Redis)

Even with a perfect Redis hit, `SelectProxyChannelForAttempt` must keep existing guards:

1. Sticky only on first attempt (`RetryCount == 0`) — unchanged unless a later product change says otherwise.
2. Preferred channel still in route set / not excluded.
3. Session-scoped credential gate still applies on bind.
4. Unavailable sticky channel → clear + route refresh / normal selection (existing tests in `proxy/channel_selection_test.go`).

Redis does **not** make a dead or cooldown channel “correct”; it only shares the preference.

## Observability (Milestone requirements)

Minimum signals before claiming Option B shipped:

| Signal | Purpose |
|--------|---------|
| Config log at boot | sticky redis mode on/off; never imply on when off |
| `sticky_redis_error` counter / rate-limited warn | fail-open visibility |
| Hit/miss breakdown | local vs redis vs miss |
| No “shared sticky OK” admin copy when disabled | honesty |

## Test gates (before any product merge)

Dedicated Milestone must include:

1. **Fail-open**: Redis down / timeout / bad RESP → request succeeds; local map still works.
2. **Two logical instances** (or shared fake store): bind on A, get on B returns same channel ID within TTL.
3. **TTL expiry**: after TTL, both instances miss.
4. **Conditional clear**: clear channel 7 does not delete redis value 10.
5. **Re-bind overwrite**: later bind wins cluster-wide.
6. **Disabled paths**: empty `REDIS_URL` and shared-sticky-off leave pure process-local behavior (existing sticky tests still pass).
7. **No lease coupling**: tests assert concurrency leases remain process-local.
8. **Selection revalidation**: redis-preferred channel excluded / unavailable still falls back and clears appropriately.

Forbidden: shipping Redis sticky with only unit tests against the local map.

## Rollout recommendation

| Phase | Action |
|-------|--------|
| **v0.8.x default** | Option A — process-local + LB affinity ops ([residual doc](sticky-session-multi-instance-residual.md)) |
| **v0.8.14** | **Docs only** (this spike). No product code unless separately approved |
| **Future Milestone** | Option B behind explicit enable + tests + metrics honesty |
| **Never default-on** without | multi-instance tests + fail-open proof + operator docs |

## Ops guidance if Option B ships later

1. Size Redis for key churn: sticky keys ≈ concurrent sticky sessions (not total historical).
2. Memory: value is a small integer; key is hash or session string; TTL bounds growth.
3. Network: one GET per sticky-enabled first attempt; one SET per bind; keep timeouts short (admission-class, e.g. tens to low hundreds of ms — exact budget in implementation Milestone).
4. Security: Redis holds channel IDs and session-key material (or hashes). Treat Redis as sensitive as today for RPM keys; do not expose sticky keys on public admin APIs without auth.
5. Multi-tenant: session key already includes downstream API key id owner prefix — preserve that; never drop owner from the hash input.

## Relationship to other residuals

| Residual | Interaction |
|----------|-------------|
| Sticky multi-instance honesty (#237 / #282) | This spike expands Option B only; Option A remains default |
| Redis shared state (#118 / #245) | Pattern reference for fail-open + URL config; namespaces stay separate |
| Channel recovery / scheduler | Unchanged; Redis sticky does not globalize active sets |
| Responses WebSocket | Future WS multi-turn may want affinity; do not couple first Redis sticky Milestone to WS transport |
| Channel concurrency leases | Still process-local; separate design if product needs global concurrency |

## Cross-links

- Parent residual / Option A–C eval: [`sticky-session-multi-instance-residual.md`](sticky-session-multi-instance-residual.md)
- Redis admission today: [`redis-shared-state.md`](redis-shared-state.md)
- Downstream-key admission fields: [`downstream-key-admission.md`](downstream-key-admission.md)
- Scheduler / recovery residual: [`scheduler-residual-todos.md`](scheduler-residual-todos.md)
- Responses WebSocket residual: [`responses-websocket-residual.md`](responses-websocket-residual.md)
- Multi-instance audit: [`docs/specs/review/audits/audit-multi-instance.md`](../specs/review/audits/audit-multi-instance.md)
- Code: `proxy/session.go`, `internal/sharedcount`, `auth/key_admission.go`
- Issues: [#292](https://github.com/TokenDanceLab/metapi-go/issues/292), [#282](https://github.com/TokenDanceLab/metapi-go/issues/282), [#237](https://github.com/TokenDanceLab/metapi-go/issues/237)

## Verify (docs only)

```bash
test -f docs/analysis/sticky-redis-design.md
test -f docs/analysis/sticky-session-multi-instance-residual.md
# no product code required for #292
```
