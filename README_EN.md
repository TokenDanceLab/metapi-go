# MetAPI Go

<div align="center">

**The proxy for proxies — aggregate all your AI API中转站 into one unified gateway**

Go rewrite of [MetAPI](https://github.com/cita-777/metapi). Single binary, no Node.js runtime, full feature parity.

<p align="center">
  <a href="README.md"><strong>中文</strong></a> |
  <a href="README_EN.md">English</a>
</p>

[![CI](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/ghcr-v0.8.45-blue?logo=docker)](https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

</div>

## Quick Start

```bash
docker run -d -p 4000:4000 \
  -v ./data:/app/data \
  -e AUTH_TOKEN=your-token \
  -e PROXY_TOKEN=sk-your-token \
  ghcr.io/tokendancelab/metapi-go:latest
```

Open `http://localhost:4000`.

> **Unreleased tip (master)**: parity KEYS/WS/#514/UC-1 and P0-555 residual observability (OrphanLogs / stream missing-usage metric) are on tip; production pin may still lag at 0.8.44 (see ops STATE). P0-585 remains partial until production e2e.

## Features

- **Protocol proxy**: OpenAI, Anthropic, Gemini, Codex — with real-time format conversion
- **Routing engine**: Weighted random, round-robin, stable-first. Fibonacci backoff cooldown. Circuit breaker.
- **Account management**: 14 platform adapters, auto check-in, balance tracking, OAuth PKCE
- **Operations**: 5-channel notifications, backup/restore, rate limiting, 15 background schedulers
- **Performance**: 20MB memory, 15MB Docker image, <0.1s startup

## Why Go?

| | Node.js | Go |
|---|---|---|
| Memory | 85 MB | ~20 MB |
| Image | ~250 MB | ~15 MB |
| Startup | 5-10 s | <0.1 s |

## Proxy Usage

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer proxy-token" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

## Configuration

All env vars are identical to the TypeScript version.

| Variable | Default |
|----------|---------|
| `AUTH_TOKEN` | `change-me-admin-token` |
| `PROXY_TOKEN` | `change-me-proxy-sk-token` |
| `PROXY_MAX_BUFFERED_RESPONSE_BYTES` | `20971520`; maximum buffered non-streaming upstream response size |
| `METAPI_ENABLE_PROXY_STUB` | empty; test/demo-only local proxy stub. Keep empty in production so unconfigured upstream forwarding returns 503. |
| `PORT` | `4000` |
| `DB_TYPE` | `sqlite`; `postgres` is inferred when a PostgreSQL URL is provided |
| `DATABASE_URL` / `DB_URL` | empty; PostgreSQL connection string or SQLite file path. `DB_URL` takes precedence. |
| `DB_SSLMODE` | empty; PostgreSQL TLS mode. Supports `disable`, `allow`, `prefer`, `require`, `verify-ca`, and `verify-full`; non-empty values override `sslmode` in the connection string. |
| `DB_PROFILE` | `normal`; pool preset `shared-tiny` (2/1), `normal` (10/3), `dedicated` (20/5). Explicit `DB_MAX_*` override. |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | profile defaults; PostgreSQL application pool budget, must not exceed the database role connection limit. |
| `DB_CONN_MAX_LIFETIME_SEC` / `DB_CONN_MAX_IDLE_TIME_SEC` | `1800` / `300`; PostgreSQL connection lifetime and idle rotation in seconds. |
| `TRUSTED_PROXY_CIDRS` | empty; comma-separated reverse-proxy CIDRs allowed to supply `X-Forwarded-For` / `X-Real-IP`; forwarded headers are ignored by default |
| `ADMIN_CORS_ALLOWED_ORIGINS` | empty; comma-separated exact `http(s)` admin browser origins allowed to call `/api/*`; `*` is rejected |

Full list: [`.env.example`](.env.example).

The runtime supports two database modes: single-process SQLite and PostgreSQL for production deployments. In PostgreSQL mode, side-effecting schedulers such as external requests, notifications, uploads, cleanup, and sync jobs use PG advisory locks so multiple replicas do not run the same job batch at the same time. Optional `REDIS_URL` / `METAPI_REDIS_URL` enables multi-instance shared **RPM/TPM admission** counters only (`auth.ConfigureSharedAdmissionFromRedisURL` + `internal/sharedcount`; fail-open to process-local windows if Redis is unreachable). Leave empty for single-node — no Redis process required. Sticky sessions remain process-local and are **not** shared across instances via Redis today (STICKY-B is residual, not product). See [`docs/analysis/redis-shared-state.md`](docs/analysis/redis-shared-state.md).

Proxy forwarding returns HTTP 503 when routing and upstream dependencies are not configured. `METAPI_ENABLE_PROXY_STUB=1` is for tests and demos only.

## Operations Health Checks

- `GET /health` is liveness only.
- `GET /ready` is readiness and returns HTTP 503 when the database is unavailable or the process is draining for shutdown.
- Docker runs `metapi healthcheck`, which probes `http://127.0.0.1:${PORT}/ready` by default.
- Override with `METAPI_HEALTHCHECK_URL` or `METAPI_HEALTHCHECK_PATH`.

## CORS Policy

Admin API CORS is closed by default for cross-origin browser requests. Set `ADMIN_CORS_ALLOWED_ORIGINS=https://admin.example.com` when the admin frontend is hosted on a different origin. Proxy and health endpoints keep wildcard CORS for client compatibility.

Forwarded client IP headers are ignored unless `TRUSTED_PROXY_CIDRS` contains the direct reverse proxy address range. Set it only for proxies you control.

## Migration from TypeScript

```bash
# Stop old server, start Go version with same env vars
./metapi
```

Database schema is identical. Auto-migration runs on startup.

## Related Projects

- [MetAPI (TypeScript)](https://github.com/cita-777/metapi) — Original Node.js implementation
- [TokenDance Gateway](https://github.com/TokenDanceLab/tokendance-gateway) — Production NewAPI fork

## License

MIT
