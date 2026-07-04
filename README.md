# MetAPI Go

[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://go.dev/)
[![Docker Image](https://img.shields.io/badge/ghcr-tokendancelab%2Fmetapi--go-blue)](https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go)

Go rewrite of [MetAPI](https://github.com/cita-777/metapi) — the meta-layer management and unified proxy for AI API aggregation platforms. This is the [TokenDance Lab](https://github.com/TokenDanceLab) Go port of the original TypeScript project, with full feature parity. Single static binary with embedded React SPA frontend, SQLite (dev) or PostgreSQL (production) storage, and a standalone SQLite-to-PG migration tool.

## Features

- **Unified proxy gateway** -- OpenAI, Anthropic, Gemini, and Codex protocol support with transparent protocol conversion
- **Multi-tenant routing** -- Token-based route matching with dynamic channel selection, failover, and load balancing
- **Account management** -- Site/account CRUD, checkin automation, balance tracking, OAuth loopback
- **Single binary** -- Go static binary (<25 MB) with embedded React frontend (no Node.js runtime needed)
- **Dual database** -- SQLite for local development, PostgreSQL for production; seamless migration tool included
- **CI/CD** -- GitHub Actions for lint, test (SQLite + PostgreSQL matrix), and Docker image push to GHCR

## Quick Start

### Local Development

```bash
# Prerequisites: Go 1.24+, Node.js 20+
git clone https://github.com/TokenDanceLab/metapi-go.git
cd metapi-go

# Build frontend
make web-build

# Run server
make run
# Server starts at http://localhost:4000
```

### Docker

```bash
# Build and run
docker compose up --build
# Server starts at http://localhost:4000
```

### Production

```bash
# Create .env with AUTH_TOKEN and PROXY_TOKEN
cp .env.example .env
# Edit .env with your tokens

# Pull and run
docker compose -f docker-compose.prod.yml up -d
```

## Migration from TypeScript

```bash
# 1. Build the migration tool
make migrate-build

# 2. Stop the old TS server

# 3. Migrate SQLite data to PostgreSQL
./metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host:5432/db --progress --verify

# 4. Start the Go server with PG
DATABASE_URL=postgres://user:pass@host:5432/db ./metapi
```

For detailed instructions, see [docs/migration.md](docs/migration.md).

## Documentation

| Document | Description |
|----------|-------------|
| [docs/deployment.md](docs/deployment.md) | Full deployment guide: env vars, Docker, nginx, TLS |
| [docs/architecture.md](docs/architecture.md) | Go architecture overview and TS comparison |
| [docs/api.md](docs/api.md) | Admin API reference |
| [docs/migration.md](docs/migration.md) | TS-to-Go migration guide |
| [docs/specs/](docs/specs/) | 14 specification documents (P0-P13) |

## Development

```make
make build        # Build server binary (requires web/dist/)
make test         # Run tests
make lint         # Run golangci-lint
make web-build    # Build React frontend
make migrate-build # Build migration tool
make clean        # Remove build artifacts
```

## License

MIT
