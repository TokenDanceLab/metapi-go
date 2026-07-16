# Package Boundary Inventory (B1 / #17)

**Status**: B1 inventory — as-built ownership, public entrypoints, import edges  
**Date**: 2026-07-17  
**Base**: `origin/master` @ inventory time  
**Authority companions**:
- [`docs/design/BACKEND.md`](../design/BACKEND.md) — principles + allowed import edges
- [`docs/architecture.md`](../architecture.md) — as-built package map and request flows

This document is the **B1 ownership inventory**. It does **not** rewrite packages. Prefer docs and tiny zero-behavior moves; large boundary refactors need their own issues.

---

## 1. Method

| Source | Use |
|--------|-----|
| `go list -json ./...` | Non-test internal import graph |
| Package `doc.go` / constructors | Public entrypoints |
| `docs/design/BACKEND.md` §2 | Allowed / forbidden edges |
| Static call-site scan | Singletons, exception edges, unused leaves |

**Scope**: Go packages under module `github.com/tokendancelab/metapi-go` (excluding pure frontend under `web/*` and VitePress).  
**Result snapshot**: 39 Go packages; **no import cycles**.

---

## 2. Ownership map (as-built)

| Package | Owns | Must not own | Primary consumers |
|---------|------|--------------|-------------------|
| `cmd/server` | Process bootstrap, healthcheck subcommand, composition root | Business rules, SQL, protocol transforms | binary |
| `cmd/migrate` | Offline SQLite→PG migration tool | HTTP, schedulers | CLI |
| `config` | Env load/defaults/validation, `Config` singleton | DB, HTTP, domain | almost all layers |
| `web` | `//go:embed dist` SPA assets | Backend logic | `cmd/server` → `router` |
| `store` | Dual-dialect open/migrate/schema/settings, `GetDB` | Auth, HTTP handlers, routing selection | auth, routing, service, scheduler, app, router |
| `auth` | Admin/proxy middleware, downstream key policy, rate limits | Proxy orchestration, admin CRUD | `router`, `handler/proxy` |
| `platform` | Upstream adapters + site HTTP proxy helpers | Persistence, route selection, admin HTTP | `service/*`, some `handler/*`, thin `proxy` |
| `transform/*` | Protocol conversion (canonical intermediate) | HTTP, store, routing | **currently leaf-only** (see §5.4) |
| `routing` | TokenRouter, matcher, weights, cooldown, site breaker, ports | HTTP handlers, proxy conductor | `proxy`, `handler/*`, `app` wiring |
| `service` (+ subpkgs) | Domain workflows (sites/accounts/checkin/balance/notify/oauth/backup/…) | Chi routes, proxy surface formatting | `handler/*`, `scheduler`, `proxy` |
| `proxy` (+ `profiles`, `types`) | Conductor, session leases, retry, failure judge, site concurrency | Admin REST, SPA | `handler/proxy`, `app` |
| `handler/shared` | Shared admin/proxy error + metrics helpers (HTTP-shaped) | Domain services | `handler/*`, `app` metrics export |
| `handler/admin` | `/api/*` REST registrars + payloads | Router middleware stack, SPA | `router` |
| `handler/proxy` | `/v1/*` and non-v1 proxy surfaces | Admin CRUD, cron registry | `router` |
| `scheduler` | Cron jobs + lease helpers | HTTP request path (except admin-triggered ops) | `app` |
| `app` | Server lifecycle, health/ready/metrics, background service start, proxy upstream wiring | Long-term business logic dump | `cmd/server`, `router` |
| `router` | Chi mount, middleware, SPA fallback | Platform/transform internals | `cmd/server` |
| `e2e` | Full-binary / integration tests | Production code | CI / local |
| `docs` | Hygiene tests for docs only | Runtime | CI |

### 2.1 `service` subpackage ownership

| Subpackage | Responsibility |
|------------|----------------|
| `service` (root) | Site/account/token helpers, local time, proxy util shared by domain |
| `service/adapter` | Thin bridge over `platform.GetAdapter` for domain call sites |
| `service/checkin` | Check-in workflows + reward parsing |
| `service/balance` | Balance refresh |
| `service/notify` | Multi-channel notifications |
| `service/alert` | Alert rules (uses platform error classification) |
| `service/daily` | Daily summary |
| `service/backup` | Export/import style backup helpers |
| `service/oauth` | OAuth providers, sessions, route units, refresh |

### 2.2 `transform` subpackage ownership

| Subpackage | Responsibility |
|------------|----------------|
| `transform` | Package doc only (no runtime API yet at root) |
| `transform/canonical` | Intermediate types + OpenAI bridge |
| `transform/shared` | Shared chat format / error payload helpers |
| `transform/openai/*` | chat / completions / embeddings / images / responses |
| `transform/anthropic/messages` | Anthropic messages conversion |
| `transform/gemini/generate_content` | Gemini generateContent compatibility |

### 2.3 `proxy` subpackage ownership

| Subpackage | Responsibility |
|------------|----------------|
| `proxy` | Orchestration engine |
| `proxy/types` | CLI profile type contracts (leaf) |
| `proxy/profiles` | Profile detectors (Claude Code, Codex, Gemini CLI, …) |

---

## 3. Public entrypoints (by package)

Composition-root and package-facing APIs only (not exhaustive of every exported helper).

### 3.1 Process / lifecycle

| Package | Entrypoints |
|---------|-------------|
| `cmd/server` | `main` → `config.Load` → `store.EnsureRuntimeDatabase` / `Migrate` → `app.ConfigureProxyUpstream` → `router.New` → `app.StartBackgroundServices` → `app.New(...).Start` |
| `cmd/migrate` | `main` migration CLI |
| `app` | `New`, `Start`/`Stop` hooks, `Health`/`Ready`, `PrometheusHandler`/`MetricsHandler`, `StartBackgroundServices`/`StopBackgroundServices`, `UpdateCheckinSchedule`, `ConfigureProxyUpstream`, `StartDebugServer` |
| `router` | `New(cfg, webFS)` mounts middleware + admin + proxy + SPA |
| `config` | `Load`, `Get`/`Set`, `Validate`, defaults |
| `web` | `Dist` embed FS |

### 3.2 Security / persistence / routing core

| Package | Entrypoints |
|---------|-------------|
| `auth` | `AdminAuth`, `ProxyAuth`, `AdminRateLimit`, `OAuthRateLimit`, downstream policy helpers, context getters |
| `store` | `Open` / `OpenWithPostgresSSLMode`, `GetDB`, `Migrate`, `EnsureRuntimeDatabase`, `LoadRuntimeSettings`, `NewSettingsStore`, schema model types |
| `routing` | `NewTokenRouter`, `NewChannelSelector`, `NewRouteCache`, `NewRouteDecisionService`, `NewRouteRefreshWorkflow`, ports in `ports.go`, cooldown/breaker helpers |
| `platform` | `PlatformAdapter` + `Register`/`GetAdapter`, `NewSiteProxy`/`DoWithProxy`, detect helpers, `ClassifyUpstreamError` / expired helpers |
| `proxy` | `NewDefaultProxyConductor`, `NewProxyChannelCoordinator`, `NewRuntimeExecutor`, `NewSiteConcurrencyLimiter`, surface/failure toolkit helpers, profile registry |
| `proxy/types` | `CliProfileDefinition` and related structs |
| `proxy/profiles` | profile registration (init-style detectors) |

### 3.3 Domain + HTTP surfaces

| Package | Entrypoints |
|---------|-------------|
| `service` (+ sub) | Domain functions used by handlers/schedulers (account/site CRUD helpers, checkin/balance/notify/oauth/backup APIs) |
| `scheduler` | `NewRegistry`, `New*Scheduler` constructors, `ValidateCronExpr`, `UpdateCheckinSchedule` on checkin scheduler |
| `handler/admin` | `Register*Routes(r, …)` family (sites, accounts, settings, oauth, …) |
| `handler/admin/payloads` | Request/response DTO structs (leaf) |
| `handler/proxy` | `RegisterProxyRoutes`, `RegisterNonV1ProxyRoutes`, `SetUpstreamConfig`, surface/dispatch helpers |
| `handler/shared` | `APIError`, `WriteError`/`WriteErrorDetail`/`WriteAPIError`, metrics recorders + Prometheus writer |

### 3.4 Protocol leaf

| Package | Entrypoints |
|---------|-------------|
| `transform/openai/chat` | `Inbound` (and related) |
| `transform/anthropic/messages` | conversion entrypoints |
| `transform/gemini/generate_content` | compatibility entrypoints |
| `transform/canonical`, `transform/shared` | shared IR / helpers |

---

## 4. Import graph (non-test, as-built)

### 4.1 Top-level may-import summary

```
cmd/server     → app, config, router, store, web
cmd/migrate    → (stdlib / external only at inventory)
router         → app, auth, config, handler/admin, handler/proxy, store
app            → config, handler/proxy, handler/shared, proxy, routing, scheduler, store
handler/admin  → app, config, handler/admin/payloads, handler/shared, platform,
                 routing, scheduler, service(+backup/balance/checkin), store
handler/proxy  → auth, config, handler/shared, platform, proxy, routing, service
proxy          → config, platform, proxy/profiles, proxy/types, routing, service
routing        → config, store
auth           → config, store
service*       → config, platform, store, other service/*
scheduler      → config, service/*, store
store          → config
platform       → (leaf)
transform/*    → transform/canonical, transform/shared (leaf cluster)
config, web, handler/shared, handler/admin/payloads, proxy/types → leaves
```

### 4.2 Reverse edges (who is imported)

| Package | Imported by (non-test) |
|---------|------------------------|
| `config` | nearly all runtime packages |
| `store` | app, auth, cmd/server, handler/admin, router, routing, scheduler, service* |
| `platform` | handler/admin, handler/proxy, proxy, service* |
| `service*` | handler/admin, handler/proxy, proxy, scheduler |
| `routing` | app, handler/admin, handler/proxy, proxy |
| `proxy` | app, handler/proxy |
| `handler/*` | app (proxy+shared), router (admin+proxy) |
| `scheduler` | app, **handler/admin** |
| `auth` | router, handler/proxy |
| `app` | cmd/server, router, **handler/admin** |
| `transform/*` | **no non-transform production importers** |
| `web` | cmd/server (router takes embed FS) |

### 4.3 Cycles

**None** among internal packages (DFS on `go list` import graph).

---

## 5. Inconsistent / special edges

Severity is relative to `BACKEND.md` §2 rules and long-term clarity—not a claim of production breakage.

### 5.1 [P1] `handler/admin` → `scheduler` (hard-rule tension)

- **Where**: `handler/admin/checkin_schedule.go` uses `scheduler.ValidateCronExpr`.
- **BACKEND.md**: “`handler` must not import `scheduler` for request-path business logic except explicit admin ops that trigger jobs (prefer service facades).”
- **Assessment**: This is an **explicit admin ops exception** for cron validation, not request-path proxy logic.
- **Cleanup (preferred)**: Move pure `ValidateCronExpr` to a leaf package (e.g. `config` already has parallel validation notes, or a tiny `cronutil`) so HTTP handlers never import `scheduler`. **Not done in B1** (behavior-neutral but multi-file).

### 5.2 [P1] `handler/admin` → `app` (lifecycle coupling)

- **Where**: `app.UpdateCheckinSchedule` after persisting schedule settings.
- **Assessment**: Admin HTTP mutates running process state through `app` glue. Works, but inverts “app is composition root only.”
- **Cleanup**: Introduce a small `scheduler`/`service` façade registered at boot (interface held by handler via `router` DI) instead of `handler` → `app`. **Docs-only in B1.**

### 5.3 [P1] `app` → `handler/proxy` (upstream wiring)

- **Where**: `app.ConfigureProxyUpstream` calls `proxyhandler.SetUpstreamConfig` and builds `routing`/`proxy` deps.
- **Assessment**: Composition glue living in `app` rather than only `cmd/server`. Creates production edge `app → handler`.
- **Cleanup options**:
  1. Keep as documented composition helper (current).
  2. Move `ConfigureProxyUpstream` into `cmd/server` or a `bootstrap` package that is allowed to import all layers.
- **B1 choice**: Document as **approved composition-root helper** living under `app` for historical reasons; do not expand this pattern.

### 5.4 [P1] `transform/*` is an unwired leaf cluster

- **Evidence**: Non-test imports of `transform/*` exist only inside `transform/*`.
- **Impact**: Architecture docs describe transform on the proxy path, but runtime conversion is not yet import-coupled from `proxy`/`handler/proxy` (transform may be incomplete relative to TS, or conversion is inlined elsewhere).
- **Cleanup**: When wiring begins, import direction must stay `handler/proxy` or `proxy` → `transform/*` only; never reverse. Track under feature/transform issues—not B1 code move.

### 5.5 [P2] Dual error writing styles in handlers

| Path | Style |
|------|-------|
| `handler/shared` | `WriteError` / `WriteAPIError` → `{"error","detail?"}` + non-2xx |
| `handler/admin` | thin wrappers `writeError`/`writeErrorDetail` → shared |
| `handler/proxy` / some admin paths | local `writeJSON` / success-envelope helpers still coexist |

- **Related**: B3 unified errors / R2 validation boundaries.
- **Cleanup**: Converge new code on `handler/shared`; leave legacy envelopes until B3/R* issues own the migration.

### 5.6 [P2] Global singletons vs constructor DI

| Singleton | Package | Notes |
|-----------|---------|-------|
| `config.Get()` | `config` | Documented parity pattern; allowed |
| `store.GetDB()` | `store` | Used by `router` and many call sites |
| `handler/proxy` `SetUpstreamConfig` | `handler/proxy` | Process-wide proxy deps |
| platform registry | `platform` | `Register`/`GetAdapter` |
| metrics collectors | `handler/shared` | scraped via `app` |

- **Inconsistency**: Some packages take `*sqlx.DB`/`*config.Config` parameters; others grab globals mid-request.
- **Cleanup**: Prefer parameters for new code; migrate hot paths opportunistically. No mass DI rewrite in B1.

### 5.7 [P2] `handler` ↔ domain ownership blur

- Many admin handlers execute SQL / assemble responses directly with `*sqlx.DB` instead of only calling `service/*`.
- `handler/admin` also imports `platform` for login/verify adapters (acceptable thin admin action, but duplicates `service/adapter` path).
- **Cleanup**: New admin features should go `handler → service → platform/store`. Existing handlers stay until feature work touches them.

### 5.8 [P2] `proxy` → `platform` thin coupling

- `proxy/surface.go` uses `platform.ShouldMarkAccountExpired` for failure classification.
- Allowed by broad proxy rules, but mixes orchestration with adapter policy.
- **Cleanup**: Prefer routing/service-level policy ports if this grows; keep single helper OK.

### 5.9 [P3] Naming / package-path consistency (already fixed at root)

- There is **no** `proxycore/` or `protocol/` package (B0 truth). Keep using `proxy` / `transform`.
- `handler/proxy` import alias `proxyhandler` is intentional to avoid clash with `proxy` engine package—keep this convention.

### 5.10 [P3] Test-only edges (not production violations)

Examples:

| Test package | Extra internal imports |
|--------------|------------------------|
| `app` tests | `auth` |
| `handler/proxy` tests | `store` |
| `proxy` tests | `store` |
| `router` tests | `web` |
| `e2e` | broad graph |

Test imports may be wider than production; do not “fix” production packages solely to satisfy tests.

---

## 6. Conformance vs `BACKEND.md` forbidden imports

| Rule | Status | Notes |
|------|--------|-------|
| `store` ↛ handler/proxy/routing/service/scheduler/router/auth | **Pass** | only `config` |
| `platform` ↛ store/handler/proxy/router/scheduler | **Pass** | leaf |
| `transform` ↛ handler/store/proxy/routing/service/auth | **Pass** | leaf cluster |
| `routing` ↛ proxy/handler | **Pass** | only config/store |
| `service` ↛ handler/router/proxy | **Pass** | |
| `scheduler` ↛ handler/router/proxy | **Pass** | |
| `handler` ↛ router | **Pass** | |
| `handler` ↛ scheduler (except admin job ops) | **Exception** | §5.1 checkin schedule validation |
| No import cycles | **Pass** | |
| No revived `proxycore`/`protocol` top-level names | **Pass** | |

---

## 7. Recommended cleanups (ordered)

Do **not** batch these into B1 unless marked tiny/safe.

| Priority | Item | Type | Risk | Suggested owner |
|---------:|------|------|------|-----------------|
| 1 | Document approved exceptions (`handler→scheduler/app`, `app→handler/proxy`) | docs | none | B1 (this file) |
| 2 | Extract `ValidateCronExpr` to leaf util; drop `handler→scheduler` | tiny code | low | follow-up chore |
| 3 | Replace `handler→app.UpdateCheckinSchedule` with injected façade | small code | low | follow-up chore |
| 4 | Wire `transform/*` into proxy path with one-way imports | feature | medium | transform / proxy feature issues |
| 5 | Converge error writers on `handler/shared` | code | low–med | B3 / R2 |
| 6 | Reduce raw SQL in `handler/admin` via `service` | incremental | med | feature touch points |
| 7 | Optional `bootstrap` package for composition-only imports | structural | med | only if `app` keeps growing |
| 8 | Lint rule (e.g. `depguard` / custom `go list` check) enforcing §4 table in CI | tooling | low | later M-BACKEND |

### 7.1 Explicit non-goals for B1

- No package renames or directory reshuffles.
- No moving large handler SQL into services in this issue.
- No behavior changes to proxy selection, auth, or schema.
- No web work.

---

## 8. Tiny safe refactors considered

| Candidate | Decision |
|-----------|----------|
| Move `ValidateCronExpr` out of `scheduler` | **Deferred** — multi-call-site, better as dedicated follow-up with tests |
| Move `ConfigureProxyUpstream` to `cmd/server` | **Deferred** — pure move but expands `main` and churns tests |
| Delete unused transform packages | **Forbidden** — they are intentional protocol surface, not dead product code |

**B1 code change**: none required. Inventory + doc pointers satisfy AC when tests remain green (docs-only).

---

## 9. Checklist for future PRs touching boundaries

- [ ] New code lands in an existing package from §2
- [ ] Import edge appears in §4 or is listed as a new exception here + `BACKEND.md`
- [ ] Public entrypoint is a constructor/`Register*`/ports interface—not a new global if avoidable
- [ ] Handlers stay thin; domain in `service`/`routing`/`proxy`
- [ ] `transform` remains a leaf
- [ ] No new `handler → scheduler` edges without an admin-ops justification
- [ ] `go list` cycle check still clean
- [ ] `go test` for touched packages green (`-race` for concurrent paths)

---

## 10. Related docs

- [`docs/design/BACKEND.md`](../design/BACKEND.md) — principles + dependency rules
- [`docs/architecture.md`](../architecture.md) — as-built map
- [`docs/analysis/module-inventory.md`](./module-inventory.md) — historical TS module inventory (not Go authority)
- [`docs/analysis/error-model.md`](./error-model.md) / B3 — error boundary follow-on
- [`docs/plan/enterprise-program.md`](../plan/enterprise-program.md) — B1 under M-BACKEND

---

## 11. AC trace (Issue #17)

| AC | Evidence |
|----|----------|
| Inventory inconsistent package boundaries | §5 + §6 |
| Small refactors only where clarifying / no behavior change | §8 — none needed; docs preferred |
| Document public entrypoints per package | §3 |
| Tests green for touched packages | docs-only change; no package code touched |
