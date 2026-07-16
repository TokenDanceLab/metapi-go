# Optional Redis shared state (#118)

**Date:** 2026-07-17  
**Lane:** competitive learn / multi-instance  
**SSOT code:** `internal/redisx/*`, `auth/key_admission.go`, `routing/soft_cooldown.go`, `app/redis_shared.go`  
**Peers:** LiteLLM Redis dual-cache cooldown/rate limits; New API Redis usage

## Goal

Optional Redis backend so multiple metapi-go instances behind a load balancer share:

1. Downstream-key RPM/TPM admission counters (#116)
2. Soft channel cooldown markers (accelerate peer awareness of DB-backed cooldowns)

Default remains **in-process**. Redis is **never** a hard dependency for single-node installs.

## Config

| Env | Default | Meaning |
|-----|---------|---------|
| `REDIS_URL` | empty | `redis://[:password@]host:port[/db]` or `host:port`. Empty disables shared state. |

Parsed into `config.Config.RedisURL`. Wired at startup by `app.ConfigureSharedState`.

**TLS note:** the minimal client supports `redis://` only. `rediss://` is rejected with a startup warning and shared state stays process-local.

## Architecture

```
REDIS_URL empty
  └─ auth.GlobalKeyAdmission → process-local sliding 60s window
  └─ routing soft cooldown → NoopCooldown

REDIS_URL set
  └─ internal/redisx.Client (stdlib net, RESP, connection-per-command)
       ├─ RedisCounter → SharedCounter for admission RPM/TPM
       └─ RedisCooldown → soft markers metapi:cooldown:channel:{id}
```

No third-party Redis SDK is added to `go.mod`.

### Key layout

| Key | Purpose | TTL |
|-----|---------|-----|
| `metapi:admission:rpm:{keyID}` | fixed-window request counter | 60s (PEXPIRE on first INCR) |
| `metapi:admission:tpm:{keyID}` | fixed-window token counter | 60s |
| `metapi:cooldown:channel:{id}` | soft cooldown marker (`"1"`) | remaining DB cooldown TTL |

### Admission algorithm

- **Memory (default):** sliding 60s window of event timestamps (unchanged from #116).
- **Redis:** fixed-window approximation — `INCR`/`INCRBY` + `PEXPIRE` when the counter is first created in the window. Slightly coarser than sliding but cheap and multi-instance safe enough for soft admission.
- Over limit → **429** + `Retry-After` (same as process-local).

### Soft cooldown

- **Source of truth remains DB** `route_channels.cooldown_until` (and OAuth member cooldown).
- On `RecordFailure` / probe failure, instances also `SET PX` a soft marker so peers can filter the channel before their local route cache reloads from DB.
- Eligibility checks: `isChannelCoolingDown` = DB until in future **OR** soft marker active.
- On success / clear failure state, soft markers are deleted best-effort.

## Failure mode: fail-open

| Failure | Behavior |
|---------|----------|
| Invalid / empty `REDIS_URL` | Shared state disabled; process-local only |
| Startup `PING` failure | Shared backends still installed; runtime commands fail-open until Redis recovers |
| Runtime Redis error on admission | Fall back to **process-local** window for that request; increment `FailOpenCount`; log warn |
| Runtime Redis error on soft cooldown Active/Mark | Treat as **not cooling** / skip mark; never force cooldown; log debug |

**Rationale:** availability over strict global rate limits. Soft admission and soft cooldown stay optional — Redis outages must not black-hole traffic.

**Not fail-closed:** we deliberately do **not** reject requests when Redis is down.

## Tests

| Area | Coverage |
|------|----------|
| `internal/redisx` | URL parse, memory counter/cooldown, fake RESP server INCR/SET/EXISTS, dial failure |
| `auth` | shared RPM/TPM, fail-open to local, configure/nil |
| `routing` | soft marker enable/disable, fail-open Active, `isChannelCoolingDown` |
| `config` | `REDIS_URL` parse / default empty |

No live Redis required in CI.

## Residuals (out of scope for #118)

- Full session/sticky binding store in Redis
- Runtime health map dual-write
- Route cache invalidation bus
- `rediss://` TLS client
- Cluster mesh / service discovery
- Admin UI for Redis status

## Operator notes

```bash
# single node (default)
# REDIS_URL=

# multi-instance behind LB
REDIS_URL=redis://:password@redis:6379/0
```

Single-node operators can ignore Redis entirely. Multi-instance operators should still use PostgreSQL for durable cooldown and scheduler leases; Redis only tightens the admission/cooldown race window between cache reloads.
