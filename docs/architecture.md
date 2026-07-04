# Architecture Overview

MetAPI Go is a ground-up rewrite of the TypeScript MetAPI proxy gateway in Go. This document describes the architecture, key design decisions, and differences from the TypeScript version.

## High-Level Architecture

```
                  ┌─────────────────┐
                  │   nginx/Caddy   │  (TLS termination)
                  └────────┬────────┘
                           │ :4000
                  ┌────────▼────────┐
                  │   chi Router    │
                  │  /api/*  /v1/*  │
                  └───┬────────┬────┘
                      │        │
         ┌────────────▼──┐  ┌──▼──────────────┐
         │  Admin API     │  │  Proxy Handlers  │
         │  (auth.Auth)   │  │  (auth.Proxy)    │
         │  CRUD + config │  │  Route + forward │
         └───────┬────────┘  └──┬───────────────┘
                 │              │
         ┌───────▼──────────────▼───────┐
         │         ProxyCore             │
         │  Profile → Session → Retry    │
         │  Selector → Endpoint → Judge  │
         └──────────────┬───────────────┘
                        │
         ┌──────────────▼───────────────┐
         │    Protocol Transformers      │
         │  OpenAI / Anthropic / Gemini  │
         │         / Codex               │
         └──────────────┬───────────────┘
                        │
         ┌──────────────▼───────────────┐
         │           Store               │
         │    SQLite (dev) / PG (prod)   │
         └──────────────────────────────┘
```

## Package Layout

```
metapi-go/
├── cmd/
│   ├── server/main.go      # Main server entry point
│   └── migrate/main.go     # Standalone SQLite-to-PG migration tool
├── app/                    # Application lifecycle (startup, shutdown, background services)
├── auth/                   # Admin + proxy authentication middleware
├── config/                 # Configuration loading and defaults
├── handler/
│   ├── admin/              # Admin API handlers (18 files, 110 endpoints)
│   └── proxy/              # Proxy endpoint handlers
├── proxycore/              # Core proxy pipeline
│   ├── profiles/           # Platform profile detection
│   ├── session/            # Request session management
│   ├── retry/              # Retry policy engine
│   ├── selector/           # Channel selection (weighted, cooldown-aware)
│   ├── endpoint/           # Endpoint flow execution
│   ├── failure/            # Failure classification
│   ├── surface/            # Response surface formatting
│   └── conductor/          # Orchestration conductor
├── routing/                # Route + channel matching and loading
├── platform/               # Platform-specific adapters
├── protocol/               # Protocol transformers (canonical ↔ provider)
├── scheduler/              # Background schedulers (15 schedulers)
├── store/                  # Database access layer
├── web/
│   ├── embed.go            # go:embed directive for frontend
│   └── dist/               # Built React SPA (gitignored, generated)
├── Dockerfile              # Multi-stage build
├── docker-compose.yml      # Development compose
├── docker-compose.prod.yml # Production compose
└── Makefile                # Build targets
```

## Data Flow

### Proxy Request Flow

```
Client → /v1/chat/completions
  → auth.ProxyAuth (validate PROXY_TOKEN)
  → ProxyHandler
  → TokenRouter.Match(requestedModel)
  → ChannelSelector.Pick(routeId, strategy)
  → ProxyCore.Execute(ctx, profile, session, retry)
    → PlatformAdapter.Detect()
    → EndpointFlow.Execute()  [with retry]
      → ProtocolTransformer.Request()   (canonical → provider)
      → HTTP request to upstream API
      → ProtocolTransformer.Response()  (provider → canonical)
    → FailureJudge.Assess()
    → Surface.Format()
  → Response to client
```

## TS vs Go Comparison

| Aspect | TypeScript | Go |
|--------|-----------|-----|
| Runtime | Node.js (Express) | Single static binary |
| Frontend | Express.static() | go:embed (embedded) |
| Database | Drizzle ORM (SQLite/MySQL/PG) | sqlx (SQLite/PG) |
| MySQL support | Yes | Dropped (SQLite + PG only) |
| CI | None | go test (SQLite + PG matrix) + lint |
| CD trigger | push to main | main + tags + workflow_dispatch |
| Image size | ~80MB+ (node base) | <25MB (alpine + static binary) |
| Background jobs | Node.js timers + node-cron | robfig/cron v3 + goroutines |
| Type safety | TypeScript types | Go static types |
| Concurrency | Single-threaded event loop | Goroutines (multi-core) |
| Dependencies | ~800 npm packages | ~10 Go modules |
| Migration tool | Integrated in server | Standalone `metapi-migrate` binary |

## Key Design Decisions

### 1. Embedded Frontend

The React SPA is compiled once and embedded into the Go binary via `//go:embed`. No separate static file server, no volume mounts for frontend assets. The binary is self-contained.

### 2. SQLite for Dev, PG for Prod

SQLite is the default database (zero-config, file-based, no external process). PostgreSQL is used for production deployments. The `metapi-migrate` tool handles data transfer between them.

### 3. Dialect-Aware Store

The `store` package abstracts database operations behind a common interface. SQLite and PostgreSQL queries differ slightly (e.g., `?` vs `$N` placeholders, `INTEGER` vs `BIGSERIAL`), handled by the dialect layer.

### 4. Protocol Canonical Model

All provider protocols (OpenAI, Anthropic, Gemini, Codex) are translated to/from a shared canonical representation. This enables transparent cross-provider routing -- an OpenAI client can use an Anthropic model without modification.

### 5. ProxyCore Pipeline

The proxy execution pipeline is decomposed into composable stages: profile detection, session management, channel selection, endpoint flow execution, retry policy, failure classification, and surface formatting. Each stage is independently testable.

### 6. Pure Go, No CGO

`CGO_ENABLED=0` produces a fully static binary with no C dependencies. The SQLite driver (`modernc.org/sqlite`) is a pure-Go implementation. This eliminates build complexity and ensures cross-platform portability.

## S.U.P.E.R. Compliance

The architecture follows the S.U.P.E.R. framework defined in the spec:

- **S** (small): Each package has a single responsibility
- **U** (understandable): Clear interfaces between packages
- **P** (pluggable): Platform adapters and protocol transformers are pluggable
- **E** (environment-agnostic): Works with SQLite (dev) or PostgreSQL (prod)
- **R** (replaceable): Each component can be replaced independently
