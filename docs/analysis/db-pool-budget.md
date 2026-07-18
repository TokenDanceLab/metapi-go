# PostgreSQL connection pool budget (#531)

**Date**: 2026-07-19  
**Status**: product (v0.8.44+)  
**Issue**: [#531](https://github.com/TokenDanceLab/metapi-go/issues/531)

## Problem

On shared small PostgreSQL (e.g. Azure Flexible Server B1ms with multi-service role budgets), a default `MaxOpenConns=20` plus scheduler advisory-lock leases can exceed the role `CONNECTION LIMIT` and produce `SQLSTATE 53300` storms.

Dedicated / large databases still need large pools.

## Design

### Profiles (`DB_PROFILE` / `METAPI_DB_PROFILE`)

| Profile | MaxOpen | MaxIdle | When |
|:--------|--------:|--------:|:-----|
| `shared-tiny` | 2 | 1 | Shared tiny role LIMIT 1–3 |
| `normal` (default) | 10 | 3 | Single-service small/medium |
| `dedicated` | 20 | 5 | Exclusive large PG (legacy default) |

Explicit `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` **always override** the profile.

Rule of thumb: **MaxOpen ≤ role CONNECTION LIMIT**.

### Application name

`application_name=metapi-<hostname>` is injected when the DSN does not already set it (`DB_APPLICATION_NAME` override). Visible in `pg_stat_activity`.

### Scheduler lease under pressure

- `MaxOpen ≤ 2` → process-local lease (no extra advisory-lock connection).
- On 53300 / too-many-connections: exponential backoff + log rate-limit; after repeated failures force local lease for process lifetime.
- Metrics: `metapi_db_connections_open`, `metapi_db_connections_in_use`, `metapi_db_conn_errors_total`.

### What we do **not** do

- Hardcode tiny pools for everyone.
- Fake multi-instance advisory locks when pool is tiny (honest local degrade).
- Fail-closed process exit on 53300 by default (optional future).

## Operator recipes

Shared Azure / multi-tenant:

```bash
DB_PROFILE=shared-tiny
# or explicit:
# DB_MAX_OPEN_CONNS=2
# DB_MAX_IDLE_CONNS=1
```

Dedicated large PG:

```bash
DB_PROFILE=dedicated
# or DB_MAX_OPEN_CONNS=50 DB_MAX_IDLE_CONNS=10
```

## Related

- `store/open.go` · `store/bootstrap.go` · `scheduler/lease.go`
- Residual: OPS-PG-BUDGET in `residual-next-candidates.md` / `high-value-next.md`
