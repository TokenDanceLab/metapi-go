# MetAPI Go

<div align="center">

**The proxy for proxies — aggregate all your AI API中转站 into one unified gateway**

Go rewrite of [MetAPI](https://github.com/cita-777/metapi). Single binary, no Node.js runtime, full feature parity.

<p align="center">
  <a href="README.md"><strong>中文</strong></a> |
  <a href="README_EN.md">English</a>
</p>

[![CI](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/ghcr-v0.4.0-blue?logo=docker)](https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go)
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
  -H "Authorization: Bearer sk-your-token" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

## Configuration

All env vars are identical to the TypeScript version.

| Variable | Default |
|----------|---------|
| `AUTH_TOKEN` | `change-me-admin-token` |
| `PROXY_TOKEN` | `change-me-proxy-sk-token` |
| `PORT` | `4000` |
| `DB_TYPE` | `sqlite` |

Full list: [`.env.example`](.env.example).

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
