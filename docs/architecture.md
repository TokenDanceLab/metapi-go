# Architecture Overview

MetAPI Go is a ground-up rewrite of the TypeScript MetAPI proxy gateway in Go. This document describes the **as-built** package layout, request paths, and key design decisions. Design philosophy and package dependency rules live in [`docs/design/BACKEND.md`](design/BACKEND.md).

> **Naming truth (B0 / #16):** There is **no** `proxycore/` or `protocol/` package in this repository. The proxy engine is `proxy/` (with `proxy/profiles` and `proxy/types`). Protocol conversion is `transform/` (with `transform/canonical`, `openai`, `anthropic`, `gemini`, `shared`). Older docs or TS-era names that say “ProxyCore package” or “protocol package” refer to these real packages.

## High-Level Architecture

```
                  ┌─────────────────┐
                  │   nginx/Caddy   │  (TLS termination)
                  └────────┬────────┘
                           │ :4000
                  ┌────────▼────────┐
                  │  router (chi)   │
                  │  /api/*  /v1/*  │
                  │  SPA + health   │
                  └───┬────────┬────┘
                      │        │
         ┌────────────▼──┐  ┌──▼──────────────┐
         │  Admin API     │  │  Proxy Handlers  │
         │  handler/admin │  │  handler/proxy   │
         │  auth.Admin    │  │  auth.Proxy      │
         └───────┬────────┘  └──┬───────────────┘
                 │              │
         ┌───────▼──────────────▼───────┐
         │           proxy/              │
         │  Conductor → Session → Retry  │
         │  ChannelSelection → Endpoint  │
         │  FailureJudge → Surface       │
         └──────────────┬───────────────┘
                        │
         ┌──────────────▼───────────────┐
         │         routing/              │
         │  TokenRouter · Selector       │
         │  Cooldown · Runtime breaker   │
         └──────────────┬───────────────┘
                        │
         ┌──────────────▼───────────────┐
         │        transform/             │
         │  OpenAI / Anthropic / Gemini  │
         │  via canonical intermediate   │
         └──────────────┬───────────────┘
                        │
         ┌──────────────▼───────────────┐
         │   platform/  +  service/      │
         │  14 adapters · domain logic   │
         └──────────────┬───────────────┘
                        │
         ┌──────────────▼───────────────┐
         │           store/              │
         │    SQLite (dev) / PG (prod)   │
         └──────────────────────────────┘
```

## Package Layout (as-built)

```
metapi-go/
├── cmd/
│   ├── server/             # Main server entry point
│   └── migrate/            # Standalone SQLite→PG migration tool
├── app/                    # Lifecycle: start/shutdown, health, metrics, proxy upstream glue
├── auth/                   # Admin + proxy + downstream auth, policy, rate limit
├── config/                 # Env loading (no prefix), defaults, validation
├── router/                 # chi router mount, middleware, security headers, SPA fallback
├── handler/
│   ├── admin/              # Admin REST handlers (+ payloads/)
│   ├── proxy/              # /v1/* proxy surface handlers
│   └── shared/             # Shared API error helpers
├── proxy/                  # Dual-loop proxy orchestration (NOT "proxycore/")
│   ├── profiles/           # Client/profile detection (Claude Code, Codex, Gemini CLI, …)
│   └── types/              # Shared proxy types
├── routing/                # TokenRouter: match, weights, cooldown, site runtime breaker
├── platform/               # Upstream platform adapters (14) + site proxy
├── transform/              # Protocol transformers (NOT "protocol/")
│   ├── canonical/          # Shared intermediate representation
│   ├── openai/             # chat, completions, embeddings, images, responses
│   ├── anthropic/          # messages
│   ├── gemini/             # generate_content
│   └── shared/             # Cross-protocol helpers
├── service/                # Domain services (sites, accounts, checkin, balance, notify, oauth, backup, …)
├── scheduler/              # Background cron jobs (checkin, balance, recovery, retention, …)
├── store/                  # sqlx DB open, dual dialect, schema, settings
├── web/
│   ├── embed.go            # //go:embed dist
│   └── dist/               # Built React SPA (generated; embedded into binary)
├── e2e/                    # End-to-end tests
├── docs/                   # Specs, architecture, design philosophy
├── Dockerfile
├── docker-compose.yml
├── docker-compose.prod.yml
└── Makefile
```

### Package roles (one-liners)

| Package | Responsibility |
|---------|----------------|
| `cmd/server` | Wire config → store → app → router; process entry |
| `cmd/migrate` | Offline SQLite → PostgreSQL data move |
| `app` | HTTP server lifecycle, readiness, metrics, upstream executor glue |
| `router` | Route tree, CORS/security middleware, embed SPA |
| `auth` | Fail-closed admin/proxy auth, downstream key policy |
| `config` | Env → `Config` (names match TS; no `METAPI_` prefix) |
| `handler/admin` | Admin CRUD + settings + ops endpoints |
| `handler/proxy` | Protocol surfaces under `/v1/*` |
| `proxy` | Orchestration: channel pick, session lease, retry, failure judge |
| `routing` | Model/route match, weighted selection, Fibonacci cooldown, site breaker |
| `platform` | Per-upstream adapter behavior (detect, auth headers, admin APIs) |
| `transform` | Request/response (+ SSE) conversion via canonical model |
| `service` | Business workflows used by handlers and schedulers |
| `scheduler` | Cron-driven background work |
| `store` | Dual-dialect persistence |
| `web` | Embedded frontend assets |

## Data Flow

### Proxy request flow

```
Client → /v1/chat/completions (or messages / generateContent / …)
  → router middleware (RequestID, security headers, body limits where applied)
  → auth.ProxyAuth (PROXY_TOKEN or managed downstream key; fail-closed)
  → handler/proxy surface
  → routing.TokenRouter.Match(requestedModel) + policy filters
  → routing selector (weights / round-robin / stable-first)
      · skip cooldown channels
      · skip open site/model runtime breakers
  → proxy.Conductor.Execute
      → profile detect (proxy/profiles)
      → session lease / sticky preference
      → endpoint flow + retry policy
          → transform (client format ↔ provider format via canonical)
          → HTTP to upstream (platform-aware headers / site proxy)
          → transform response / SSE
      → failure judge (content + policy)
      → surface format to client
  → response
```

### Admin request flow

```
Browser SPA (embedded) → /api/*
  → auth.AdminAuth (optional IP allowlist + Bearer AUTH_TOKEN; fail-closed)
  → handler/admin → service/* → store
```

### Background work

```
scheduler (robfig/cron)
  → service (checkin / balance / notify / backup / oauth / …)
  → store + platform adapters
  → routing health / channel recovery feedback
```

## TS vs Go Comparison

| Aspect | TypeScript | Go |
|--------|-----------|-----|
| Runtime | Node.js | Single static binary (`CGO_ENABLED=0`) |
| Frontend | Static files from Node server | `web` + `//go:embed dist` |
| Database | Drizzle (SQLite/MySQL/PG) | sqlx (SQLite + PostgreSQL only) |
| MySQL support | Yes | Dropped |
| Proxy engine package | `proxy-core` (TS) | `proxy/` |
| Transformers package | `transformers` (TS) | `transform/` |
| Routing | `tokenRouter` service | `routing/` |
| Background jobs | timers + node-cron | `scheduler/` + robfig/cron |
| Image size | ~80MB+ node base | Alpine + static binary |
| Config env names | Unprefixed | Same unprefixed names |

## Key Design Decisions

### 1. Embedded frontend

React SPA is built once and embedded via `//go:embed`. Production image has no Node runtime and no separate static volume for the UI.

### 2. Dual dialect: SQLite + PostgreSQL

SQLite is default (zero-config dev/test). PostgreSQL is the production path. `store.Open(dialect, dsn)` and dialect rebinding hide `?` vs `$N` and type differences. MySQL was intentionally not ported.

### 3. camelCase JSON API parity

All admin/proxy JSON field names use camelCase tags matching the TS frontend (`externalCheckinUrl`, `useSystemProxy`, …). Do not introduce snake_case wire formats for existing APIs.

### 4. Fail-closed auth

Admin and proxy middleware deny by default on missing/invalid credentials. Managed downstream keys use deny-all-when-empty model allowlists. Public exceptions are explicit allowlists only (e.g. desktop health, OAuth callback).

### 5. Channel isolation, cooldown, circuit breaker

Routing isolates bad channels instead of cascading:

- **Per-channel cooldown** — Fibonacci backoff on failures (`routing` cooldown helpers).
- **Site runtime breaker** — streak-based open periods at site and model granularity (`SiteRuntimeBreakerLevelsMs`).
- **Selection filters** — open breakers and recent failures are removed before weighted/round-robin pick.
- **Proxy failover** — conductor can retry same channel, refresh auth, or failover to the next channel without taking the whole gateway down.

### 6. Config via env, TS-compatible names

`config.Load` reads the same env var names as TS MetAPI (`AUTH_TOKEN`, `PROXY_TOKEN`, `DB_TYPE`, …) with **no** project prefix. Defaults and validation live in `config/`.

### 7. Pure Go, no CGO

`modernc.org/sqlite` keeps the binary fully static and portable.

## Package dependency overview

High-level allowed direction (see [`docs/design/BACKEND.md`](design/BACKEND.md) for forbidden edges):

```
cmd → app, router, config, store, …
router → handler/*, auth, web, app, config, store
handler → service, proxy, routing, platform, auth, store, config
proxy → routing, service, store, config, proxy/profiles, proxy/types
routing → store, config
service → platform, store, config, service/*
scheduler → service/*, store, config
platform → (leaf; no store/handler imports)
transform/* → transform/canonical, transform/shared (leaf protocol layer)
store → config
config, web, handler/shared → leaves
```

**B1 ownership inventory:** as-built public entrypoints, import edges, approved exceptions, and recommended cleanups live in [`docs/analysis/package-boundaries.md`](analysis/package-boundaries.md). Prefer that file when deciding where new code belongs or whether an import edge is intentional.

## S.U.P.E.R. Compliance

- **S** (small): packages own one layer (HTTP, orchestration, routing, persistence, …)
- **U** (understandable): real names match code (`proxy`, `transform`, `routing`)
- **P** (pluggable): platform adapters and protocol transforms register/compose independently
- **E** (environment-agnostic): SQLite or PostgreSQL via dialect store
- **R** (replaceable): conductor dependencies and adapters are injectable/replaceable

## Related docs

- [`docs/design/BACKEND.md`](design/BACKEND.md) — backend design philosophy, dependency rules, forbidden imports
- [`docs/analysis/package-boundaries.md`](analysis/package-boundaries.md) — B1 package ownership inventory, public entrypoints, import exceptions
- [`docs/api.md`](api.md) — admin API reference
- [`docs/deployment.md`](deployment.md) — deploy guide
- [`docs/migration.md`](migration.md) — TS → Go migration notes
- [`docs/specs/`](specs/) — phase implementation specs (historical; package names in older specs may say ProxyCore/protocol — treat as-built names in this file as authority)
