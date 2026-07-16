# Backend Design Philosophy

**Status**: B0 (#16) — architecture truth for M-BACKEND  
**Authority**: package layout and import edges as they exist in this repo  
**Companion**: [`docs/architecture.md`](../architecture.md) (as-built map)

This document states the **non-negotiable backend principles** for metapi-go. Implementation work under M-BACKEND (package boundaries, concurrency, unified errors) must not violate these rules without an explicit design revision.

---

## 1. Principles

### 1.1 Single binary + `go:embed` SPA

- Ship **one** server binary from `cmd/server`.
- Frontend is pre-built into `web/dist` and embedded by `web` (`//go:embed dist`).
- Production images must not require Node/npm at runtime.
- SPA fallback and `/api` + `/v1` share the same process and port.

### 1.2 Dual dialect: SQLite + PostgreSQL

- Persistence goes through `store` only.
- Supported dialects: **SQLite** (dev/test/default) and **PostgreSQL** (production).
- Open via `store.Open(dialect, dsn)` (or equivalent store entrypoints); never hard-code SQLite-only SQL in callers.
- Placeholders and type differences are handled inside `store` (rebind / dialect helpers).
- MySQL is **out of scope** (dropped from TS).

### 1.3 camelCase JSON API parity with TS frontend

- Wire JSON uses **camelCase** field names identical to the original MetAPI frontend expectations.
- Struct tags on API models and payloads follow `json:"fooBar"`, not snake_case, for public admin/proxy contracts.
- Breaking renames require a versioned migration plan; silent format drift is a defect.

### 1.4 Fail-closed auth

- Unauthenticated or invalid credentials **deny** (401/403). There is no “open if misconfigured” path for protected routes.
- **Admin** (`auth.AdminAuth`): IP allowlist (when configured) + Bearer `AUTH_TOKEN`; public API routes are an explicit allowlist only.
- **Proxy** (`auth.ProxyAuth`): global `PROXY_TOKEN` or managed downstream keys; unknown/missing tokens fail closed.
- **Managed downstream keys**: empty model/route allowlists mean **deny all** (`DenyAllWhenEmpty=true`). Global proxy token remains the broad default only when intentionally used.
- Constant-time compare for secrets; do not log full tokens.

### 1.5 Channel isolation / cooldown / circuit breaker

Gateway resilience is **per channel / per site**, not process-wide suicide:

| Mechanism | Package | Intent |
|-----------|---------|--------|
| Fibonacci failure cooldown | `routing` | Back off a bad channel without blackholing the fleet |
| Recent-failure filters | `routing` selector | Prefer healthy candidates in the current pick |
| Site/model runtime breaker | `routing` runtime health | Open breaker after streak; tiered open windows |
| Conductor failover | `proxy` | Retry same channel, refresh auth, or move to next channel |
| Content failure judge | `proxy` | Detect “HTTP 200 but empty/error body” without trusting status alone |

Rules of thumb:

- A single upstream outage must not disable unrelated sites/channels.
- Breaker open ⇒ filter out of selection, do not crash the process.
- Recovery jobs (`scheduler` channel recovery, etc.) may heal state; they must not bypass auth or dialect boundaries.

### 1.6 Config via env (no prefix), matching TS names

- Configuration is environment-driven (`config.Load` / `config.Get`).
- Env var names match TS MetAPI **without** a `METAPI_` (or similar) prefix: e.g. `AUTH_TOKEN`, `PROXY_TOKEN`, `DB_TYPE`, `DB_URL`, `PORT`.
- Defaults live in `config`; production unsafety (default tokens, weak secrets) is warned/validated, not silently accepted as secure.
- Runtime settings that belong in DB stay in `store` settings; do not invent a second config language.

### 1.7 Feature parity with controlled evolution

- Behavioral parity with original MetAPI remains the default for existing APIs.
- Enterprise upgrades (M-BACKEND and later) may clarify structure and fix CRITICAL defects without changing wire contracts unless the issue explicitly allows it.

---

## 2. Package dependency rules

### 2.1 Layer diagram (allowed direction)

```
                    ┌──────────── cmd/server, cmd/migrate ────────────┐
                    │                     │                            │
                    ▼                     ▼                            │
                 config ◄──────────── store                            │
                    ▲                     ▲                            │
                    │                     │                            │
        ┌───────────┴───────────┬─────────┴──────────┬─────────────────┤
        │                       │                    │                 │
        ▼                       ▼                    ▼                 │
      auth                   platform             transform/*          │
        ▲                       ▲                    ▲                 │
        │                       │                    │                 │
        │                    service ───────────────┘ (prefer not;     │
        │                       ▲                     transform is     │
        │                       │                     leaf for now)    │
        │                  scheduler                                   │
        │                       ▲                                      │
        │                       │                                      │
      router ──► handler/* ──► proxy ──► routing ──────────────────────┘
        │            │           │
        │            │           └── proxy/profiles, proxy/types
        │            └── handler/shared
        └── web (embed only)
                 app (lifecycle + health/metrics glue)
```

Arrows mean **“may import”**. Edges not shown are forbidden unless listed under exceptions.

### 2.2 Dependency table (summary)

| Package | May import (internal) | Must not import |
|---------|----------------------|-----------------|
| `config` | — | everything else |
| `web` | — | everything else |
| `handler/shared` | — | domain packages |
| `store` | `config` | `handler`, `proxy`, `router`, `scheduler`, `service`, `auth` |
| `platform` | — (leaf adapters) | `store`, `handler`, `proxy`, `router`, `scheduler` |
| `transform/*` | `transform/canonical`, `transform/shared` | `handler`, `store`, `proxy`, `routing`, `service` |
| `proxy/types`, `proxy/profiles` | `proxy/types` only (profiles) | upper layers |
| `routing` | `config`, `store` | `handler`, `proxy`, `router`, `scheduler`, `service` |
| `auth` | `config`, `store` | `handler`, `proxy`, `router` |
| `service` (+ subpackages) | `config`, `store`, `platform`, other `service/*` | `handler`, `router`, `proxy` (keep domain free of HTTP/orchestration) |
| `proxy` | `config`, `store`, `routing`, `service`, `proxy/*` | `handler`, `router`, `scheduler` |
| `handler/*` | `auth`, `config`, `store`, `service`, `proxy`, `routing`, `platform`, `app` (sparingly), `handler/*` | `router` (router mounts handlers, not reverse) |
| `scheduler` | `config`, `store`, `service/*` | `handler`, `router`, `proxy` |
| `app` | `config`, `store`, and wiring deps as needed | becoming a junk drawer for business logic |
| `router` | `app`, `auth`, `config`, `handler/*`, `store`, `web` | `platform` internals, `transform` internals (handlers own surfaces) |
| `cmd/*` | composition root — may wire all layers | business logic bodies (keep `main` thin) |

### 2.3 Forbidden imports (hard rules)

1. **`store` must not import** `handler`, `proxy`, `routing`, `service`, `scheduler`, `router`, `auth`.
2. **`platform` must not import** `store`, `handler`, `proxy`, `router`, `scheduler` (adapters stay I/O-shaped; persistence is caller-side).
3. **`transform` must not import** `handler`, `store`, `proxy`, `routing`, `service`, `auth`.
4. **`routing` must not import** `proxy` or `handler` (selection stays pure relative to HTTP orchestration).
5. **`service` must not import** `handler` or `router` (no HTTP types in domain services).
6. **`handler` must not import** `scheduler` for request-path business logic except explicit admin ops that trigger jobs (prefer service facades).
7. **No new top-level package names** that revive TS layout (`proxycore`, `protocol`) — extend `proxy` / `transform` instead.
8. **No import cycles.** If a cycle appears, extract a small types/ports package rather than merging layers.

### 2.4 Composition root

Only `cmd/server` (and tests/e2e helpers) should construct the full graph: load config → open store → build services/router/schedulers → `app.Start`. Libraries under packages should accept dependencies via constructors/parameters, not hidden global grabs—except the existing `config.Get()` singleton pattern used for parity.

**As-built inventory:** package ownership, public entrypoints, and documented exception edges (e.g. admin checkin schedule → `app`/`scheduler`, `app.ConfigureProxyUpstream` → `handler/proxy`) are recorded in [`docs/analysis/package-boundaries.md`](../analysis/package-boundaries.md). New exceptions must be listed there (and justified against §2.3) rather than introduced silently.

---

## 3. Cross-cutting behavioral contracts

### 3.1 Errors

- Prefer typed/sentinel errors at package boundaries; map to HTTP in `handler` / `handler/shared`.
- Do not return HTTP 200 for auth or validation failure.
- Proxy surfaces should preserve upstream-compatible error shapes where TS parity requires it.

### 3.2 Concurrency

- Channel/session state mutations must be safe under concurrent proxy traffic.
- Cooldown and breaker updates must not race into “always open” or “never open” corruption.
- CRITICAL concurrency fixes under M-BACKEND stay inside owning packages (`routing`, `proxy`, …) without smuggling locks into `store` SQL.

### 3.3 Observability

- Use `log/slog` with request IDs from router middleware.
- Metrics/health live under `app` (`/health`, `/ready`, `/metrics` patterns already established).
- Do not log secrets, full `Authorization` headers, or raw account credentials.

### 3.4 Testing

- Dual dialect: store tests cover SQLite and PG where SQL diverges.
- Package tests stay inside the package; e2e covers full binary behavior.
- Local gate before push: `go vet ./...` and `go test ./... -count=1 -race` (see `AGENTS.md`).

---

## 4. Naming glossary (TS → Go)

| TS / informal name | Actual Go package |
|--------------------|-------------------|
| proxy-core / ProxyCore | `proxy` |
| transformers / protocol | `transform` (+ subdirs) |
| tokenRouter | `routing` |
| platform adapters | `platform` |
| admin routes | `handler/admin` |
| proxy routes | `handler/proxy` |
| embed web | `web` |

---

## 5. M-BACKEND follow-ons

| Issue | Focus | Status pointer |
|------:|-------|----------------|
| B0 | This file + architecture truth | Done in principle docs |
| B1 | Package boundary inventory / minimal ownership cleanup | **[`docs/analysis/package-boundaries.md`](../analysis/package-boundaries.md)** — ownership map, public entrypoints, import exceptions, cleanup queue |
| B2 | CRITICAL concurrency (leases, contexts, stub locks) | code under `routing` / `proxy` |
| B3 | Unified error model | prefer `handler/shared` + analysis notes |

When principles change, revise this file. When only layout/ownership facts change, update [`docs/architecture.md`](../architecture.md) and/or the B1 inventory.

---

## 6. Checklist for new backend code

- [ ] Belongs in an existing package (no drive-by top-level package)
- [ ] Imports only allowed edges (§2)
- [ ] JSON tags camelCase for public API
- [ ] Auth path fail-closed
- [ ] Channel failure isolated (cooldown/breaker/failover) when touching routing/proxy
- [ ] Dialect-safe SQL via `store`
- [ ] Env names match TS, no new prefix scheme
- [ ] Tests with `-race` for concurrent paths
