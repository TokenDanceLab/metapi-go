# MetAPI Go

<div align="center">

[![CI](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml)
[![CD](https://github.com/TokenDanceLab/metapi-go/actions/workflows/cd.yml/badge.svg)](https://github.com/TokenDanceLab/metapi-go/actions/workflows/cd.yml)
[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Docker](https://img.shields.io/badge/ghcr-v0.2.0-blue?logo=docker)](https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go)

**Meta-layer management and unified proxy for AI API aggregation platforms.**

Go rewrite of [MetAPI](https://github.com/cita-777/metapi) by [TokenDance Lab](https://github.com/TokenDanceLab).
Single static binary. No Node.js runtime. Full feature parity.

</div>

---

## What is MetAPI?

MetAPI is a unified proxy gateway that sits between your AI applications and multiple API providers. It manages API keys, performs automatic check-ins to earn free credits, balances load across providers, and transparently converts between AI protocol formats (OpenAI, Anthropic, Gemini, Codex).

**Use cases:**
- Aggregate free-tier API keys from multiple NewAPI/OneAPI sites into one endpoint
- Automate daily check-ins to maintain credit balances
- Route requests intelligently across providers based on cost, latency, and availability
- Deploy a single OpenAI-compatible `/v1/chat/completions` endpoint backed by dozens of upstream keys

## Why Go?

| | Node.js (TS) | Go |
|---|---|---|
| **Memory** | 85 MB | ~20 MB |
| **Docker image** | ~250 MB | ~15 MB |
| **Startup** | 5-10 s | <100 ms |
| **Binary** | Requires Node 25 runtime | Single 7 MB file |
| **Concurrency** | Event loop + worker threads | Goroutines (lightweight, multi-core) |
| **Deployment** | `npm rebuild` native addons | Copy one file |

## Features

### AI Protocol Proxy
- **OpenAI-compatible** `/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`, `/v1/images`, `/v1/models`
- **Anthropic** `/v1/messages`, `/v1/messages/count_tokens`
- **Gemini** native surface
- **Codex** responses passthrough with WebSocket support
- **Real-time protocol conversion** — send an Anthropic request, get OpenAI response, or vice versa
- **SSE streaming** with zero-buffered chunk transformation

### Routing Engine
- **3 strategies**: Weighted random, round-robin, stable-first with observation gating
- **Fibonacci backoff** cooldown: `15 × fib(failCount)`, ceiling 30 days
- **Circuit breaker** per site with half-open probing
- **Sticky sessions** for conversation continuity
- **Per-API-key routing policies**: model allowlists, site exclusions, weight multipliers
- **Session channel concurrency limits** with queueing

### Account Management
- **14 platform adapters**: NewAPI, OneAPI, OneHub, DoneHub, Veloera, Sub2API, OpenAI, Claude, Codex, Gemini, Gemini CLI, Antigravity, CliProxyAPI, AnyRouter
- **Automatic platform detection** via URL probing and title analysis
- **Daily check-in automation** (cron or interval mode) with reward parsing
- **Balance tracking** and value scoring
- **API key extraction** and synchronization
- **OAuth integration** (Codex, Claude, Gemini CLI, Antigravity) with PKCE S256

### Operations
- **5-channel notifications**: Webhook, Bark, Server酱, Telegram, SMTP
- **Backup/restore**: Full 27-table JSON export/import with WebDAV sync
- **SQLite→PostgreSQL migration** tool
- **Usage analytics**: Per-site, per-model, daily/hourly aggregation
- **Admin dashboard**: React SPA with 20+ pages
- **Rate limiting**: Per-IP token bucket (100 rps admin, 10 rps OAuth)

## Quick Start

### Docker (recommended)

```bash
docker run -d \
  -p 4000:4000 \
  -v ./data:/app/data \
  -e AUTH_TOKEN=your-admin-token \
  -e PROXY_TOKEN=sk-your-proxy-token \
  ghcr.io/tokendancelab/metapi-go:latest
```

Open `http://localhost:4000` in your browser.

### From source

```bash
# Prerequisites: Go 1.24+
git clone https://github.com/TokenDanceLab/metapi-go.git
cd metapi-go

# The frontend is pre-built and embedded — just build and run
go build -o metapi ./cmd/server
AUTH_TOKEN=admin PROXY_TOKEN=sk-proxy ./metapi
```

### Docker Compose

```bash
cp .env.example .env
# Edit .env with your tokens
docker compose up -d
```

## Proxy Usage

Point any OpenAI-compatible client at your MetAPI instance:

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-your-proxy-token" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

## Migration from TypeScript

```bash
# 1. Build the migration tool
go build -o metapi-migrate ./cmd/migrate

# 2. Stop the old TS server

# 3. Migrate SQLite → PostgreSQL (optional)
./metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host/db --progress --verify

# 4. Start with same env vars — drop-in replacement
AUTH_TOKEN=... PROXY_TOKEN=... ./metapi
```

See [docs/migration.md](docs/migration.md) for detailed instructions.

## Configuration

All configuration is via environment variables — identical to the TypeScript version.

| Variable | Default | Description |
|----------|---------|-------------|
| `AUTH_TOKEN` | `change-me-admin-token` | Admin API bearer token |
| `PROXY_TOKEN` | `change-me-proxy-sk-token` | Global proxy API key |
| `PORT` | `4000` | HTTP listen port |
| `DB_TYPE` | `sqlite` | `sqlite` or `postgres` |
| `DB_URL` | `./data/hub.db` | Database connection string |
| `CHECKIN_CRON` | `0 8 * * *` | Daily check-in schedule |
| `BALANCE_REFRESH_CRON` | `0 * * * *` | Balance refresh interval |

See [`.env.example`](.env.example) for all ~100 configuration options.

## Documentation

| Document | Description |
|----------|-------------|
| [docs/deployment.md](docs/deployment.md) | Deployment: Docker, nginx, TLS |
| [docs/architecture.md](docs/architecture.md) | Architecture overview |
| [docs/api.md](docs/api.md) | Admin API reference |
| [docs/migration.md](docs/migration.md) | TS → Go migration guide |
| [docs/specs/](docs/specs/) | 14 implementation specifications |

## Development

```bash
make build          # Build server binary
make test           # Run all tests
make lint           # Run golangci-lint
make docker-build   # Build Docker image
```

## Architecture

```
┌─ React SPA (embedded via go:embed) ────────────────────┐
├─ chi Router ───────────────────────────────────────────┤
│  /api/*      Admin REST API (~100 endpoints)            │
│  /v1/*       OpenAI-compatible proxy (11 surfaces)      │
│  /*          SPA fallback                               │
├─ Service Layer ────────────────────────────────────────┤
│  routing/    TokenRouter (Fibonacci + weighted random)  │
│  proxy/      ProxyCore (dual-loop orchestration)        │
│  platform/   14 upstream adapters                       │
│  transform/  4-protocol SSE conversion                  │
│  service/    Checkin, balance, notify, OAuth, backup    │
├─ Store (SQLite / PostgreSQL) ──────────────────────────┤
│  27 tables, auto-migration, dual dialect                │
└─ Schedulers (15 background jobs) ──────────────────────┘
```

## Related Projects

- [MetAPI (TypeScript)](https://github.com/cita-777/metapi) — Original Node.js implementation
- [TokenDance Gateway](https://github.com/TokenDanceLab/tokendance-gateway) — NewAPI fork for production routing

## License

MIT © [TokenDance Lab](https://github.com/TokenDanceLab)
